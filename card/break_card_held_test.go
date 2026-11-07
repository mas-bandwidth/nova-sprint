package card

// The breaker's harness and the sequences that HELD (TestBreakHeld*). Every
// step goes through card.* on store.Fake and, once applied, every invariant
// the design lists is checked over the whole snapshot and over the
// transition (bk.inv). A step the store refuses must leave the snapshot as it
// was (half-applied check). Clock-free and socket-free: At is t0 plus the
// op's number. The findings are in break_card_test.go (TestBreakCard*).

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/require"
)

var (
	hA  = strings.Repeat("a", 40)
	hB  = strings.Repeat("b", 40)
	hC  = strings.Repeat("c", 40) // the base used by newWorld helpers (base)
	hD  = strings.Repeat("d", 40)
	hE  = strings.Repeat("e", 40)
	hF  = strings.Repeat("f", 40)
	h1x = strings.Repeat("1", 40)
	h2x = strings.Repeat("2", 40)
)

type bk struct {
	t      *testing.T
	w      *world
	landed map[string]int
	last   string // the reason the last step was refused
}

func newBk(t *testing.T, cards ...store.PutCard) *bk {
	t.Helper()
	b := &bk{t: t, w: newWorld(t, cards...), landed: map[string]int{}}
	b.inv(store.Snapshot{}, b.w.snap(), "seed", false)
	return b
}

func (b *bk) snap() store.Snapshot { return b.w.snap() }
func (b *bk) c(id string) Card     { b.t.Helper(); return b.w.card(id) }

// sameState is the cards, rows and streams of two snapshots equal, gens
// included (a refused op moves no gen).
func sameState(a, c store.Snapshot) bool {
	return reflect.DeepEqual(a.Cards, c.Cards) && reflect.DeepEqual(a.Rows, c.Rows) && reflect.DeepEqual(a.Streams, c.Streams)
}

// try applies a plan the machine built (or records its refusal). It returns
// whether the op was accepted; b.last is the reason otherwise.
func (b *bk) try(label string, plan Plan, ref *Refusal) bool {
	b.t.Helper()
	if ref != nil {
		b.last = ref.Reason
		return false
	}
	before := b.snap()
	rc, err := b.w.apply(strings.ToLower(strings.Fields(label)[0]), plan)
	require.NoError(b.t, err, label)
	after := b.snap()
	if !rc.Accepted {
		b.last = "store: " + rc.Reason
		if !sameState(before, after) {
			b.t.Errorf("%s: refused by the store (%s) but the snapshot moved", label, rc.Reason)
		}
		return false
	}
	b.last = ""
	b.inv(before, after, label, strings.HasPrefix(label, "merge "))
	return true
}

func (b *bk) step(s cold4Step) bool {
	b.t.Helper()
	plan, ref := s.plan(b.w)
	return b.try(s.label, plan, ref)
}

// ok requires the step accepted.
func (b *bk) ok(s cold4Step) {
	b.t.Helper()
	if !b.step(s) {
		b.t.Fatalf("%s refused: %s", s.label, b.last)
	}
}

// no requires the step refused, by the machine or the store, with want in the reason.
func (b *bk) no(s cold4Step, want string) {
	b.t.Helper()
	if b.step(s) {
		b.t.Fatalf("%s accepted; want a refusal containing %q", s.label, want)
	}
	if !strings.Contains(b.last, want) {
		b.t.Fatalf("%s refused %q; want it to contain %q", s.label, b.last, want)
	}
}

// merge builds every step from the one snapshot and applies Merge of them.
func (b *bk) merge(steps ...cold4Step) bool {
	b.t.Helper()
	var plans []Plan
	var labels []string
	for _, s := range steps {
		p, ref := s.plan(b.w)
		if ref != nil {
			b.last = "unbuildable " + s.label + ": " + ref.Reason
			return false
		}
		plans = append(plans, p)
		labels = append(labels, s.label)
	}
	m, ref := Merge(plans...)
	return b.try("merge "+strings.Join(labels, "+"), m, ref)
}

func (b *bk) okMerge(steps ...cold4Step) {
	b.t.Helper()
	if !b.merge(steps...) {
		b.t.Fatalf("merge refused: %s", b.last)
	}
}

func (b *bk) noMerge(want string, steps ...cold4Step) {
	b.t.Helper()
	if b.merge(steps...) {
		b.t.Fatalf("merge accepted; want a refusal containing %q", want)
	}
	if !strings.Contains(b.last, want) {
		b.t.Fatalf("merge refused %q; want it to contain %q", b.last, want)
	}
}

// ---- moves in words ----

func (b *bk) P(id string, e Event) cold4Step { return cold4P(id, e) }

func (b *bk) holder(cid string) string { return b.c(cid).Holder }

// C is a consumer move by its holder (Report carries the row), the row
// read when the step is planned.
func (b *bk) C(cid string, e Event) cold4Step {
	return cold4Step{name(e) + " " + cid, func(w *world) (Plan, *Refusal) {
		ev := e
		row := w.snap().Cards[cid].Holder
		if v, ok := e.(Report); ok {
			v.Row = row
			ev = v
		}
		return cold4C(cid, ev).plan(w)
	}}
}

func (b *bk) start(id, row string) string {
	b.t.Helper()
	b.ok(cold4P(id, Start{Row: row, Branch: "b-" + id, Base: base}))
	return workID(id, b.c(id).Attempt)
}

// take checks cid is in its row's hand: the deal is the take (defect 52).
func (b *bk) take(cid string) { b.t.Helper(); b.is(cid, Working) }
func (b *bk) report(cid, result, head, reason string) {
	b.t.Helper()
	b.ok(b.C(cid, Report{Result: result, Head: head, Reason: reason}))
}

// run starts id on row, takes and reports; the producer is left to the caller.
func (b *bk) run(id, row, result, head string) string {
	b.t.Helper()
	cid := b.start(id, row)
	b.take(cid)
	b.report(cid, result, head, "why "+result)
	return cid
}

// review takes a ready producer to review at head by row.
func (b *bk) review(id, row, head string) {
	b.t.Helper()
	b.ok(cold4P(id, Resolve{}))
	cid := b.run(id, row, "ok", head)
	b.ok(cold4P(id, Finished{Consumer: cid}))
}

// readOf asks row for a read of id, takes it and reports result.
func (b *bk) readOf(id, row, result string) string {
	b.t.Helper()
	before := b.snap().Cards
	b.ok(cold4P(id, AskRead{Row: row}))
	var rid string
	for k := range b.snap().Cards {
		if _, ok := before[k]; !ok {
			rid = k
		}
	}
	b.take(rid)
	b.report(rid, result, b.c(id).Head, "")
	return rid
}

// reportPlan is ReportPlan of cid's report, as layer 3 plans a row's report.
func (b *bk) reportPlan(cid string, r Report) cold4Step {
	return cold4Step{"ReportPlan " + cid, func(w *world) (Plan, *Refusal) {
		all, cons := w.all()
		c := all[cid]
		r.Row = c.Holder
		p := all[c.Of]
		return ReportPlan(c, p, cons[p.ID], w.rows(), StreamFromStore(w.snap().Streams[p.Stream]), all, r)
	}}
}

// land takes id (ready or waiting with its needs landed) through the happy path.
func (b *bk) land(id, head string) {
	b.t.Helper()
	b.review(id, "r1", head)
	b.readOf(id, "r2", "ok")
	b.ok(cold4P(id, ReadYes{}))
	b.ok(cold4P(id, Landed{Head: head}))
}

func (b *bk) hold()   { b.t.Helper(); b.ok(cold4Hold()) }
func (b *bk) unhold() { b.t.Helper(); b.ok(cold4Unhold()) }

func (b *bk) is(id, state string) {
	b.t.Helper()
	if got := b.c(id).State; got != state {
		b.t.Fatalf("%s is %s, want %s", id, got, state)
	}
}

func liveCards(s store.Snapshot, of string) []string {
	var out []string
	for _, c := range s.Cards {
		if c.Of == of && live(Card{State: c.State}) {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// ---- the invariants, over a snapshot and over the transition ----

func (b *bk) inv(prev, cur store.Snapshot, label string, multi bool) {
	t := b.t
	t.Helper()
	bad := func(f string, a ...any) { t.Helper(); t.Errorf(label+": "+f, a...) }
	working := map[string]int{}
	liveWork := map[string][]string{}
	ids := make([]string, 0, len(cur.Cards))
	for id := range cur.Cards {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := cur.Cards[id]
		was, had := prev.Cards[id]
		if had && final(was.Kind, was.State) && !reflect.DeepEqual(was, c) {
			bad("final card %s (%s) moved: %+v -> %+v", id, was.State, was, c)
		}
		if consumer(c.Kind) {
			p := cur.Cards[c.Of]
			if had && was.Holder != c.Holder {
				bad("consumer %s changed holder %s -> %s", id, was.Holder, c.Holder)
			}
			if c.State == Working {
				working[c.Holder]++
			}
			if c.Kind == "read" && c.Author != "" && c.Author == c.Holder {
				bad("read %s dealt to its author %s", id, c.Author)
			}
			if live(Card{State: c.State}) {
				switch c.Kind {
				case "work":
					liveWork[c.Of] = append(liveWork[c.Of], id)
					if p.State != Working || p.Attempt != c.Attempt || id != workID(p.ID, p.Attempt) {
						bad("live work %s under %s in %s attempt %d", id, p.ID, p.State, p.Attempt)
					}
				case "read":
					if p.State != Review || c.Review != p.Review || c.Head != p.Head {
						bad("live read %s (review %d head %.4s) under %s in %s review %d head %.4s", id, c.Review, c.Head, p.ID, p.State, p.Review, p.Head)
					}
				}
			}
			continue
		}
		if c.Holder != "" {
			bad("%s %s has a holder %s", c.Kind, id, c.Holder)
		}
		if c.Kind == "producer" && c.State == Ready && cur.Streams[c.Stream].Held {
			bad("producer %s is ready in held stream %s", id, c.Stream)
		}
		if c.Kind == "producer" && c.State == Working {
			wc, ok := cur.Cards[workID(id, c.Attempt)]
			switch {
			case !ok || wc.Of != id:
				bad("%s working with no work consumer of attempt %d", id, c.Attempt)
			case wc.State == Fail:
				bad("%s working with its work consumer %s withdrawn (a withdraw must release in its own plan)", id, wc.ID)
			}
		}
		if c.Kind != "producer" && (c.Red || c.Bounces != 0 || c.Review != 0 || c.Attempt != 0) {
			bad("%s %s carries producer columns: %+v", c.Kind, id, c)
		}
		if c.Kind == "sentinel" && c.State == LandedState {
			for _, n := range c.Needs {
				if cur.Cards[n].State != LandedState {
					bad("sentinel %s landed with need %s %s", id, n, cur.Cards[n].State)
				}
			}
		}
		if !had {
			continue
		}
		b.transition(was, c, prev, cur, bad, multi)
	}
	for p, cs := range liveWork {
		if len(cs) > 1 {
			bad("producer %s has %d live work consumers %v", p, len(cs), cs)
		}
	}
	rows := make([]string, 0, len(working))
	for n := range working {
		rows = append(rows, n)
	}
	sort.Strings(rows)
	for _, n := range rows {
		if working[n] > worldWidth[n] {
			bad("row %s holds %d working consumers, width %d", n, working[n], worldWidth[n])
		}
	}
	if loop := breakCycle(cur.Cards); loop != nil {
		bad("needs/after cycle %v", loop)
	}
}

func (b *bk) transition(was, c store.Card, prev, cur store.Snapshot, bad func(string, ...any), multi bool) {
	if c.Kind == "work" || c.Kind == "read" {
		return
	}
	id := c.ID
	if d := c.Attempt - was.Attempt; d != 0 && !(d == 1 && c.State == Working) {
		bad("%s attempt %d -> %d in state %s -> %s (attempt rises only on Start)", id, was.Attempt, c.Attempt, was.State, c.State)
	}
	// Unblock resets the count (directive 75: the coordinator's review
	// found the card sound).
	unblocked := was.State == Blocked && c.State != Blocked && c.Bounces == 0
	if d := c.Bounces - was.Bounces; !unblocked && (d < 0 || d > 1 || (d == 1 && was.State != Working && was.State != Review)) {
		bad("%s bounces %d -> %d from %s", id, was.Bounces, c.Bounces, was.State)
	}
	// A merge is several moves (ruling 11). Review may rise by one when an
	// intermediate Red or Finished entered review and a later move left it.
	// A single move still has to end in review to raise the counter.
	rose := c.Review == was.Review+1
	enteredReview := was.State != Review && c.State == Review
	// A Red sends merging to fix (ready, or waiting when held) and raises
	// the review on the way (intent rule 30).
	redFix := was.State == Merging && (c.State == Ready || c.State == Waiting) && rose
	if c.Review != was.Review && !(enteredReview && rose) && !(multi && rose) && !redFix {
		if enteredReview {
			bad("%s entered review at review %d -> %d", id, was.Review, c.Review)
		} else {
			bad("%s review %d -> %d without entering review (%s -> %s)", id, was.Review, c.Review, was.State, c.State)
		}
	}
	if was.Red && !c.Red && !(c.Attempt > was.Attempt || enteredReview || (multi && rose)) {
		bad("%s red cleared other than by Start or Finished (%s -> %s)", id, was.State, c.State)
	}
	if !was.Red && c.Red && !redFix && !(multi && rose && was.State == Merging) {
		bad("%s red set other than by Red (%s -> %s)", id, was.State, c.State)
	}
	if was.State != LandedState && c.State == LandedState {
		b.landed[id]++
		if b.landed[id] > 1 {
			bad("%s landed %d times", id, b.landed[id])
		}
	}
	if (was.State == Review || was.State == Ready || was.State == Waiting) && c.State == Merging {
		okAt := func(r int) bool {
			for _, x := range cur.Cards {
				if x.Of == id && x.Kind == "read" && x.Review == r && x.Head == c.Head && x.State == Done && x.Result == "ok" {
					return true
				}
			}
			return false
		}
		if !okAt(c.Review) && !(was.Red && c.Review == was.Review && c.Review > 0 && okAt(c.Review-1)) {
			bad("%s review -> merging at review %d without an ok read of it (nor a red waived)", id, c.Review)
		}
	}
	if was.Prose != c.Prose || !slices.Equal(was.Paths, c.Paths) || !slices.Equal(was.DoneWhen, c.DoneWhen) {
		if live := liveCards(prev, id); len(live) > 0 {
			bad("%s edited while consumers %v were live", id, live)
		}
	}
	if !slices.Equal(was.Needs, c.Needs) && was.State != Waiting {
		bad("%s needs changed in %s", id, was.State)
	}
	if was.Author != c.Author && !(c.Author == "" && c.Attempt > was.Attempt) && !enteredReview && !(multi && rose && c.Author != "") {
		bad("%s author %q -> %q other than by Start or Finished", id, was.Author, c.Author)
	}
	if c.State != was.State && was.State == Ready && c.State == Working && c.Attempt != was.Attempt+1 {
		bad("%s started without raising attempt", id)
	}
}

// breakCycle is a cycle through needs and after, or nil.
func breakCycle(cards map[string]store.Card) []string {
	state := map[string]int{}
	var path []string
	var walk func(id string) []string
	walk = func(id string) []string {
		switch state[id] {
		case 1:
			return append(slices.Clone(path), id)
		case 2:
			return nil
		}
		state[id] = 1
		path = append(path, id)
		for _, n := range append(slices.Clone(cards[id].Needs), cards[id].After...) {
			if loop := walk(n); loop != nil {
				return loop
			}
		}
		path = path[:len(path)-1]
		state[id] = 2
		return nil
	}
	ids := make([]string, 0, len(cards))
	for id := range cards {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if loop := walk(id); loop != nil {
			return loop
		}
	}
	return nil
}

// same is "a merged op equals applying its plans one by one": the setup
// builds two identical worlds, one takes Merge of steps built from one
// snapshot, the other each step planned afresh. It returns "" when both end
// equal, "merge refused: ..." when only the merge refused (an over-refusal the
// caller judges), and a diff when both were accepted and differ or the merge
// was accepted and the sequence was not.
func sameAsSequence(t *testing.T, setup func(*testing.T) *bk, build func(*bk) []cold4Step) string {
	t.Helper()
	a, s := setup(t), setup(t)
	steps := build(a)
	merged := a.merge(steps...)
	seqOK := true
	seqWhy := ""
	for _, st := range build(s) {
		if !s.step(st) {
			seqOK, seqWhy = false, st.label+": "+s.last
			break
		}
	}
	switch {
	case merged && !seqOK:
		return "merge accepted, one by one refused at " + seqWhy
	case !merged && seqOK:
		return "merge refused (" + a.last + ") but one by one accepted"
	case merged && seqOK:
		return cold4Diff(a.snap(), s.snap())
	}
	return ""
}

// has requires a refusal whose reason contains want.
func has(t *testing.T, r *Refusal, want string) {
	t.Helper()
	if r == nil {
		t.Fatalf("accepted; want a refusal containing %q", want)
	}
	if !strings.Contains(r.Reason, want) {
		t.Fatalf("refused %q; want it to contain %q", r.Reason, want)
	}
}

// addStep is Add of a producer in s1.
func (b *bk) addStep(id string, needs, after []string) cold4Step {
	return cold4Step{"Add " + id, func(w *world) (Plan, *Refusal) {
		all, _ := w.all()
		return MoveAdd(Add{ID: id, Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do " + id, Needs: needs, After: after}, all)
	}}
}

// sentinel is Add of a sentinel in s1.
func (b *bk) sentinel(id string, needs []string) cold4Step {
	return cold4Step{"Add " + id, func(w *world) (Plan, *Refusal) {
		all, _ := w.all()
		return MoveAdd(Add{ID: id, Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: needs}, all)
	}}
}

// land2 is land for a producer already resolved.
func (b *bk) land2(id, head string) {
	b.t.Helper()
	cid := b.run(id, "r1", "ok", head)
	b.ok(cold4P(id, Finished{Consumer: cid}))
	b.readOf(id, "r2", "ok")
	b.ok(cold4P(id, ReadYes{}))
	b.ok(cold4P(id, Landed{Head: head}))
}
