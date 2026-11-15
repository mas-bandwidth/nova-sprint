package steps

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The trace walk of tla/MergeNag.tla: mergingNag (the merge-queue nag)
// on a View the walk moves, its clock an argument. Cards enter and leave
// merging, time passes, the coordinator hooks on and drops off the bus,
// and each tick plans the nag; each step's state is projected onto the
// model's variables (a card is old when it has merged longer than
// timeouts merging_nag) and written as MergeNagTraceLog.tla under
// NOVA_TRACE_DIR; tla/check-trace.sh MergeNag checks every step is one of
// the model's.

var nagCards = []string{"c1", "c2", "c3"}

type nagWalk_internal struct {
	t      *testing.T
	rng    *rand.Rand
	now    time.Time
	v      *View
	seen   map[string]time.Time
	pushed int
	ticked bool
	// perCard is the breaker's nag: one line per card past the threshold.
	perCard bool
}

func newNagWalk_internal(t *testing.T, seed int64) *nagWalk_internal {
	now := time.Unix(1000, 0).UTC()
	v := &View{Since: now, Cards: map[string]store.Card{}, Rows: map[string]store.Row{
		"carol": {Name: "carol", Session: "hook-carol", Role: "coordinator"},
	}, Judgements: map[string]store.Judgement{}, byState: map[string]set{"merging": {}}, MergingAt: map[string]time.Time{}}
	for i, c := range nagCards {
		v.Cards[c] = store.Card{ID: c, Head: fmt.Sprintf("%040x", i+1)}
	}
	return &nagWalk_internal{t: t, rng: rand.New(rand.NewSource(seed)), now: now, v: v, seen: map[string]time.Time{}}
}

// coord is the model's coord: carol hooked and on the bus.
func (w *nagWalk_internal) coord_internal() bool {
	_, ok := w.seen["carol"]
	return ok
}

// state_internal is each card's model state at the walk's now.
func (w *nagWalk_internal) state_internal(c string) string {
	if _, ok := w.v.byState["merging"][c]; !ok {
		return "none"
	}
	if w.now.Sub(w.v.MergingAt[c]) > seconds(Engine.Timeouts.MergingNag) {
		return "old"
	}
	return "young"
}

func (w *nagWalk_internal) tick_internal() {
	if w.coord_internal() {
		w.seen["carol"] = w.now // the run keeps a hooked row's last sight
	}
	t := &tick{v: w.v, in: Inputs{Now: w.now, Seen: w.seen}, rowNames: []string{"carol"}}
	ns := t.mergingNag()
	w.pushed = len(ns)
	if w.perCard && len(ns) == 1 {
		w.pushed = 0
		for _, c := range nagCards {
			if w.state_internal(c) == "old" {
				w.pushed++
			}
		}
	}
	for _, n := range ns {
		if n.Card != MergingNagCard || n.Row != "carol" || n.Every != time.Second {
			w.t.Fatalf("nag %+v: want one line to carol every 1s", n)
		}
	}
	w.ticked = true
}

// step_internal plays one move and returns it, or "" when the move left
// the model's state as it was (time passed and no card crossed).
func (w *nagWalk_internal) step_internal() (string, string) {
	switch k := w.rng.Intn(10); {
	case k < 3:
		w.tick_internal()
		return "Tick", ""
	case k < 5:
		c := nagCards[w.rng.Intn(len(nagCards))]
		if _, ok := w.v.byState["merging"][c]; ok {
			delete(w.v.byState["merging"], c)
			delete(w.v.MergingAt, c)
			w.ticked = false
			return "Leave", c
		}
		w.v.byState["merging"][c] = struct{}{}
		w.v.MergingAt[c] = w.now
		w.ticked = false
		return "Enter", c
	case k < 6:
		if w.coord_internal() {
			delete(w.seen, "carol")
		} else {
			w.seen["carol"] = w.now
		}
		w.ticked = false
		return "Hook", ""
	default:
		before := make([]string, len(nagCards))
		for i, c := range nagCards {
			before[i] = w.state_internal(c)
		}
		w.now = w.now.Add(time.Duration(w.rng.Intn(200)+1) * time.Second)
		if w.coord_internal() {
			w.seen["carol"] = w.now
		}
		for i, c := range nagCards {
			if w.state_internal(c) != before[i] {
				w.ticked = false
				return "Ages", ""
			}
		}
		return "", ""
	}
}

func (w *nagWalk_internal) record_internal(n int, act, arg string) string {
	st := make([]string, len(nagCards))
	for i, c := range nagCards {
		st[i] = fmt.Sprintf("%s |-> %q", c, w.state_internal(c))
	}
	return fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, st |-> [%s], coord |-> %s, pushed |-> %d, ticked |-> %s]",
		n, act, arg, strings.Join(st, ", "), strings.ToUpper(fmt.Sprint(w.coord_internal())), w.pushed, strings.ToUpper(fmt.Sprint(w.ticked)))
}

func nagTraceModule_internal(records []string) string {
	return "---------------------------- MODULE MergeNagTraceLog ----------------------------\n" +
		"EXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(records, ",\n") + "\n>>\n" +
		"=============================================================================\n"
}

func walkMergeNag_internal(t *testing.T, seed int64, steps int, perCard bool) ([]string, map[string]bool) {
	w := newNagWalk_internal(t, seed)
	w.perCard = perCard
	records := []string{w.record_internal(1, "Init", "")}
	seen := map[string]bool{}
	for len(records) < steps {
		act, arg := w.step_internal()
		if act == "" {
			continue
		}
		records = append(records, w.record_internal(len(records)+1, act, arg))
		seen[act] = true
		if act == "Tick" && w.pushed == 1 {
			seen["pushed"] = true
		}
		if act == "Tick" && w.pushed == 0 && w.coord_internal() && seen["pushed"] {
			seen["stopped"] = true
		}
		if perCard && w.pushed > 1 {
			break // the breaker's last record: the model refuses it
		}
	}
	return records, seen
}

// TestMergeNagTraceWalk walks eight seeds and, with NOVA_TRACE_DIR set,
// writes each one's trace for tla/check-trace.sh; it fails when the walks
// together never nag, never stop, or skip a move. With the per-card nag
// (the breaker, a fixture under tla/traces) the trace must be refused at
// its last record.
func TestMergeNagTraceWalk(t *testing.T) {
	t.Parallel()
	all := map[string]bool{}
	for seed := int64(1); seed <= 8; seed++ {
		records, seen := walkMergeNag_internal(t, seed, 200, false)
		for k := range seen {
			all[k] = true
		}
		if dir := os.Getenv("NOVA_TRACE_DIR"); dir != "" {
			sub := filepath.Join(dir, fmt.Sprintf("mergenag-walk-%d", seed))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "MergeNagTraceLog.tla"), []byte(nagTraceModule_internal(records)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, want := range []string{"Tick", "Enter", "Leave", "Ages", "Hook", "pushed", "stopped"} {
		if !all[want] {
			t.Errorf("the walks never reached %s", want)
		}
	}

	cfg, err := os.ReadFile(filepath.Join("..", "..", "tla", "MergeNagTrace.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	var records []string
	for seed := int64(1); seed <= 64 && (len(records) == 0 || !strings.Contains(records[len(records)-1], "pushed |-> 2") && !strings.Contains(records[len(records)-1], "pushed |-> 3")); seed++ {
		records, _ = walkMergeNag_internal(t, seed, 400, true)
	}
	if last := records[len(records)-1]; !strings.Contains(last, "pushed |-> 2") && !strings.Contains(last, "pushed |-> 3") {
		t.Fatal("the per-card breaker never had two cards past the threshold at once")
	}

	files := map[string]string{
		"MergeNagTraceLog.tla": nagTraceModule_internal(records),
		"MergeNagTrace.cfg": "\\* Generated by modules/steps/nagtrace_test.go; do not edit.\n" +
			fmt.Sprintf("\\* expect: forbidden at %d\n", len(records)) + strings.Replace(string(cfg), "\\* expect: matched\n", "", 1),
	}
	for file, text := range files {
		if out := os.Getenv("NOVA_TRACE_DIR"); out != "" {
			if err := os.MkdirAll(filepath.Join(out, "mergenag-breaker-per-card"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, "mergenag-breaker-per-card", file), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(filepath.Join("..", "..", "tla", "traces", "mergenag-breaker-per-card", file))
		if err != nil || string(want) != text {
			t.Errorf("tla/traces/mergenag-breaker-per-card/%s is stale or missing (NOVA_TRACE_DIR=$PWD/tla/traces go test -run TestMergeNagTraceWalk ./modules/steps/ writes it): %v", file, err)
		}
	}
}
