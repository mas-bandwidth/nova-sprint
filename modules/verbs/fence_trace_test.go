package verbs

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The fence walk (tla/VerbFence.tla, VerbFenceTrace.tla): `replace p4 --by
// p9` on the Fake, with the run's deal, landing and hold of p9, a deal of
// p4 and a write to p2 (a card the verb names nowhere) landing between the
// verb's reads and its commits. Each step is a record of the model's
// variables; tla/check-trace.sh checks the recording is a behaviour of the
// model.

// fcard is a card as the model reads it.
type fcard struct {
	st  string
	gen int
}

func (c fcard) tla() string { return fmt.Sprintf("[st |-> %q, gen |-> %d]", c.st, c.gen) }

// fstate is the model's store: old p4, new p9, dep p3 (its need), x p2's gen.
type fstate struct {
	old, new fcard
	need     string
	depGen   int
	x        int
}

func (s fstate) tla() string {
	return fmt.Sprintf("[old |-> %s, new |-> %s, dep |-> [need |-> %q, gen |-> %d], x |-> %d]", s.old.tla(), s.new.tla(), s.need, s.depGen, s.x)
}

type frec struct {
	action, pc string
	cur, snap  fstate
	first      fcard
	tries, env int
	atNew      string
	atOld      int
}

// fenceWalk is the store the verb runs on: the Fake, recording the verb's
// reads, and at each apply of its op the other writers' moves first.
type fenceWalk struct {
	*store.Fake
	t     *testing.T
	rng   *rand.Rand
	recs  []frec
	tries int
	env   int
	ops   int
	snap  fstate
	first fcard
	read  bool
	ended bool
	atNew string
	atOld int
	pri   bool
}

// fenceMaxEnv is the walk's bound on the other writers (VerbFenceTrace.cfg
// MaxEnv): below fenceTries, so a refusal is always the verb's own.
const fenceMaxEnv = 4

func (w *fenceWalk) abs() fstate {
	s, err := w.Fake.Snapshot(context.Background())
	if err != nil {
		w.t.Fatal(err)
	}
	st := func(c store.Card) string {
		if c.State == card.Waiting && card.HeldReason(c.Reason) {
			return "held"
		}
		return c.State
	}
	need := "old"
	if n := s.Cards["p3"].Needs; len(n) == 1 && n[0] == "p9" {
		need = "new"
	}
	return fstate{old: fcard{st(s.Cards["p4"]), s.Cards["p4"].Gen}, new: fcard{st(s.Cards["p9"]), s.Cards["p9"].Gen},
		need: need, depGen: s.Cards["p3"].Gen, x: s.Cards["p2"].Gen}
}

func (w *fenceWalk) record(action, pc string) {
	w.recs = append(w.recs, frec{action: action, pc: pc, cur: w.abs(), snap: w.snap, first: w.first, tries: w.tries, env: w.env, atNew: w.atNew, atOld: w.atOld})
}

func (w *fenceWalk) Read(ctx context.Context, scope store.Scope) (store.Snapshot, error) {
	s, err := w.Fake.Read(ctx, scope)
	if err != nil || w.ended {
		return s, err
	}
	w.snap = w.abs()
	if !w.read {
		w.first, w.read = w.snap.old, true
	}
	w.record("Read", "?") // planned or refused: what the verb does next says (finish)
	return s, err
}

// other is one other writer's move, if any is enabled.
func (w *fenceWalk) other() {
	cur := w.abs()
	type move struct {
		name string
		ws   []store.Write
	}
	// the deal and the unrelated write twice over: they are what a retry
	// commits through, the others end the verb
	touch := move{"Touch", []store.Write{store.SetCardPriority{Card: "p2", Priority: map[bool]string{true: "normal", false: "high"}[w.pri]}}}
	moves := []move{touch, touch}
	if cur.new.st == "ready" {
		deal := move{"Deal", []store.Write{store.SetCardState{Card: "p9", State: "working"}}}
		moves = append(moves, deal, deal, move{"Hold", []store.Write{store.SetCardState{Card: "p9", State: "waiting"}, store.SetCardReason{Card: "p9", Reason: card.HeldPrefix + "walk"}}})
	}
	if cur.new.st == "working" {
		moves = append(moves, move{"Land", []store.Write{store.SetCardState{Card: "p9", State: "landed"}}})
	}
	if cur.old.st == "ready" {
		moves = append(moves, move{"Moved", []store.Write{store.SetCardState{Card: "p4", State: "working"}}})
	}
	m := moves[w.rng.Intn(len(moves))]
	if m.name == "Touch" {
		w.pri = !w.pri
	}
	w.ops++
	r, err := w.Fake.Apply(context.Background(), store.Op{ID: fmt.Sprintf("walk-%d", w.ops), Epoch: 1, Actor: "sprint", Kind: "walk", At: t0, Writes: m.ws})
	if err != nil || !r.Accepted {
		w.t.Fatalf("the walk's %s: %+v %v", m.name, r, err)
	}
	w.env++
	w.record(m.name, "planned")
}

func (w *fenceWalk) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind != "replace" {
		return w.Fake.Apply(ctx, op)
	}
	for k := w.rng.Intn(3); k > 0 && w.env < fenceMaxEnv; k-- {
		w.other()
	}
	before := w.abs()
	r, err := w.Fake.Apply(ctx, op)
	if err != nil {
		return r, err
	}
	w.tries++
	switch {
	case r.Accepted:
		w.atNew, w.atOld, w.ended = before.new.st, before.old.gen, true
		w.record("Apply", "done")
	case w.tries < fenceTries:
		w.record("Apply", "read")
	default:
		w.record("Apply", "refused")
	}
	return r, err
}

// finish sets each read's pc from what the verb did next: an apply follows
// a planned read; nothing follows a refused one.
func (w *fenceWalk) finish() {
	for i := range w.recs {
		if w.recs[i].action != "Read" {
			continue
		}
		w.recs[i].pc = "refused"
		for _, n := range w.recs[i+1:] {
			if n.action == "Apply" {
				w.recs[i].pc = "planned"
				break
			}
			if n.action == "Read" {
				break
			}
		}
	}
}

func (w *fenceWalk) log() string {
	lines := make([]string, len(w.recs))
	for i, r := range w.recs {
		lines[i] = fmt.Sprintf("  [i |-> %d, action |-> %q, old |-> %s, new |-> %s, dep |-> [need |-> %q, gen |-> %d], x |-> %d, pc |-> %q, snap |-> %s, first |-> %s, tries |-> %d, env |-> %d, at |-> [new |-> %q, old |-> %d]]",
			i+1, r.action, r.cur.old.tla(), r.cur.new.tla(), r.cur.need, r.cur.depGen, r.cur.x, r.pc, r.snap.tla(), r.first.tla(), r.tries, r.env, r.atNew, r.atOld)
	}
	return "---------------------------- MODULE VerbFenceTraceLog ----------------------------\n" +
		"EXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(lines, ",\n") + "\n>>\n" +
		"=============================================================================\n"
}

// walkFence runs one seed: p9 starts ready, dealt (working) or held, and
// replace runs to its end.
func walkFence(t *testing.T, seed int64) (*fenceWalk, Result) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	s := sprint()
	replacing(&s)
	switch rng.Intn(6) { // ready most often: it is the one the deal moves
	case 4:
		setCard("p9", func(c *store.Card) { c.State = "working" })(&s)
	case 5:
		setCard("p9", func(c *store.Card) { c.State, c.Reason = "waiting", card.HeldPrefix+"walk" })(&s)
	}
	w := worldFrom(t, s)
	fw := &fenceWalk{Fake: w.f, t: t, rng: rng, atNew: "none"}
	cur := fw.abs()
	fw.snap, fw.first = cur, cur.old
	fw.record("Init", "read")
	w.e.Store = fw
	r := w.do("carol", "replace", "p4", "--by", "p9", "--reason", "walk")
	fw.finish()
	last := fw.recs[len(fw.recs)-1]
	if (last.pc == "done") != (r.Exit == 0) || (last.pc != "done" && last.pc != "refused") {
		t.Fatalf("seed %d: the walk ended %s, the verb exit %d:\n%s", seed, last.pc, r.Exit, r.Output)
	}
	return fw, r
}

// fenceTraceCfg is VerbFenceTrace.cfg with the code's own bounds.
func fenceTraceCfg() string {
	return fmt.Sprintf("\\* expect: matched\nCONSTANTS\n  Tries = %d\n  MaxEnv = %d\n  Coarse = FALSE\n  Writers = {\"deal\", \"land\", \"hold\", \"old\", \"touch\"}\n  OwnCheck = TRUE\n"+
		"SPECIFICATION TraceSpec\nCHECK_DEADLOCK FALSE\nINVARIANTS\n  TypeOK\n  Follows\n  NoOrphan\n  TargetLive\n  OwnKept\n  NeverStuck\nPROPERTY TraceMatched\n", fenceTries, fenceMaxEnv)
}

// TestVerbFenceTraceWalk walks the seeds and, with NOVA_TRACE_DIR set,
// writes each one's trace for tla/check-trace.sh. It fails when the walks
// together never reach a move or an end the model names.
func TestVerbFenceTraceWalk(t *testing.T) {
	t.Parallel()
	seeds := int64(120)
	if testing.Short() {
		seeds = 40
	}
	seen := map[string]bool{}
	for seed := int64(1); seed <= seeds; seed++ {
		fw, _ := walkFence(t, seed)
		for _, r := range fw.recs {
			seen[r.action] = true
			seen[r.pc] = true
			if r.pc == "done" && r.tries > 1 {
				seen["retried"] = true
			}
		}
		if dir := os.Getenv("NOVA_TRACE_DIR"); dir != "" {
			sub := filepath.Join(dir, fmt.Sprintf("verbfence-walk-%d", seed))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "VerbFenceTraceLog.tla"), []byte(fw.log()), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "VerbFenceTrace.cfg"), []byte(fenceTraceCfg()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, want := range []string{"Read", "Apply", "Deal", "Land", "Hold", "Moved", "Touch", "done", "refused", "retried"} {
		if !seen[want] {
			t.Errorf("the walks never reached %s", want)
		}
	}
}
