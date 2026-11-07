package card

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The unit tests of the machine: every row of the three tables accepted once
// with its plan asserted exactly, refused from every other state, and every
// refusal the design lists with its reason. No store, no clock.

var (
	h1   = strings.Repeat("a", 40)
	h2   = strings.Repeat("b", 40)
	base = strings.Repeat("c", 40)

	producerStates = []string{Waiting, Ready, Working, Review, Merging, LandedState, Fail, Blocked}
	consumerStates = []string{Ready, Working, Done, Fail}
	nonFinal       = []string{Waiting, Ready, Working, Review, Merging, Blocked}
)

// pr is producer p1 in state: attempt 1, review 1, head h1 written by r2.
func pr(state string) Card {
	return Card{ID: "p1", Kind: "producer", Stream: "s1", State: state, Gen: 3, Attempt: 1, Priority: "normal", Tier: "pro", Score: 5,
		Needs: []string{"n1"}, Head: h1, Author: "r2", Reason: "old", Review: 1}
}

// wk is p1's work consumer of attempt 1, on r1.
func wk(state, result, head string) Card {
	return Card{ID: "p1.w1", Kind: "work", Of: "p1", Stream: "s1", State: state, Gen: 6, Attempt: 1, Tier: "pro", Holder: "r1",
		Branch: "b1", BaseSHA: base, Head: head, Result: result, Reason: "tests red"}
}

// rd is p1's first read of review 1, on r1.
func rd(state, result string) Card {
	return Card{ID: "p1.r1.1", Kind: "read", Of: "p1", Stream: "s1", State: state, Gen: 2, Attempt: 1, Tier: "pro", Holder: "r1",
		Head: h1, Author: "r2", Result: result, Review: 1}
}

func rows() map[string]Row {
	return map[string]Row{
		"r1": {Name: "r1", State: Up, Width: 2, Gen: 4, Tiers: []string{"pro"}},
		"r2": {Name: "r2", State: Up, Width: 1, Gen: 7, Tiers: []string{"pro"}},
		"r3": {Name: "r3", State: Down, Width: 1, Gen: 1, Tiers: []string{"pro"}},
		"r4": {Name: "r4", State: Up, Width: 1, Gen: 1, Held: true, Tiers: []string{"pro"}},
		"r5": {Name: "r5", State: Up, Width: 1, Gen: 1, Tiers: []string{"heavy"}},
	}
}

func needs(state string) map[string]Card {
	return map[string]Card{"n1": {ID: "n1", Kind: "producer", State: state}, "n2": {ID: "n2", Kind: "producer", State: Waiting, Gen: 9},
		"p9": {ID: "p9", Kind: "producer", State: Waiting}}
}

// pcase is one row of the producer table: the event, the states it moves
// from (nil: any non-final), the records, and the plan it must produce.
type pcase struct {
	event  Event
	from   []string
	kind   string
	cons   []Card
	held   bool
	need   string // n1's state
	expect []store.Expect
	writes []store.Write
}

func gen(id string, g int) store.Expect { return store.CardGen{Card: id, Gen: g} }

var p1 = gen("p1", 3)

func producerTable() []pcase {
	free, landed := store.StreamHeld{Stream: "s1"}, store.NeedsLanded{Card: "p1"}
	r1 := []store.Expect{store.RowGen{Row: "r1", Gen: 4}, store.RowIs{Row: "r1", State: Up}, store.RowHeld{Row: "r1"}}
	failed := store.CardState{Card: "n1", State: Fail}
	reads := []Card{wk(Done, "ok", h1), rd(Done, "ok")}
	fixes := []Card{wk(Done, "ok", h1), rd(Done, "fix")}
	readFence := []store.Expect{p1, gen("p1.w1", 6), gen("p1.r1.1", 2), store.CardAbsent{Card: "p1.r1.2"}}
	// the attempt's work consumer, withdrawn: in working the machine requires
	// it passed, and every no-live plan fences it
	gone := []Card{wk(Fail, "", "")}
	goneFence := []store.Expect{p1, gen("p1.w1", 6)}
	return []pcase{
		{event: Resolve{}, from: []string{Waiting}, expect: []store.Expect{p1, free, landed}, writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}}},
		{event: Park{Reason: "stream s1 is held"}, from: []string{Ready}, held: true, expect: []store.Expect{p1, store.StreamHeld{Stream: "s1", Held: true}},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Waiting}, store.SetCardReason{Card: "p1", Reason: "stream s1 is held"}}},
		{event: Reached{}, from: []string{Waiting}, kind: "sentinel", expect: []store.Expect{p1, landed}, writes: []store.Write{store.SetCardState{Card: "p1", State: LandedState}}},
		{event: Start{Row: "r1", Branch: "b2", Base: base}, from: []string{Ready}, cons: []Card{wk(Done, "fail", h2)},
			expect: append(append([]store.Expect{p1, free}, r1...), gen("p1.w1", 6), store.CardAbsent{Card: "p1.w2"}),
			writes: []store.Write{store.SetCardState{Card: "p1", State: Working}, store.SetCardAttempt{Card: "p1", Attempt: 2}, store.SetCardAuthor{Card: "p1"},
				store.PutCard{ID: "p1.w2", Kind: "work", Of: "p1", Stream: "s1", Holder: "r1", Tier: "pro", Attempt: 2},
				store.SetCardBranch{Card: "p1.w2", Branch: "b2"}, store.SetCardBaseSHA{Card: "p1.w2", SHA: base}, store.SetCardPriorHead{Card: "p1.w2", Head: h1},
				store.SetCardState{Card: "p1.w2", State: Ready}}},
		{event: Finished{Consumer: "p1.w1"}, from: []string{Working}, cons: []Card{wk(Done, "ok", h2)}, expect: []store.Expect{p1, gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Review}, store.SetCardReview{Card: "p1"}, store.SetCardHead{Card: "p1", Head: h2}, store.SetCardAuthor{Card: "p1", Author: "r1"}}},
		{event: Broken{Consumer: "p1.w1"}, from: []string{Working}, cons: []Card{wk(Done, "fail", h2)}, expect: []store.Expect{p1, gen("p1.w1", 6), store.StreamHeld{Stream: "s1"}},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "tests red"}, store.SetCardBounces{Card: "p1", Bounces: 1}, store.SetCardHead{Card: "p1", Head: h2}}},
		{event: Faulted{Consumer: "p1.w1"}, from: []string{Working}, cons: []Card{wk(Done, "fault", h2)}, expect: []store.Expect{p1, gen("p1.w1", 6), store.StreamHeld{Stream: "s1"}},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "tests red"}, store.SetCardHead{Card: "p1", Head: h2}}},
		{event: AskRead{Row: "r1"}, from: []string{Review}, cons: []Card{wk(Done, "ok", h1)},
			expect: append(append([]store.Expect{p1, free}, r1...), gen("p1.w1", 6), store.CardAbsent{Card: "p1.r1.1"}),
			writes: []store.Write{store.PutCard{ID: "p1.r1.1", Kind: "read", Of: "p1", Stream: "s1", Holder: "r1", Tier: "pro", Attempt: 1, Review: 1},
				store.SetCardHead{Card: "p1.r1.1", Head: h1}, store.SetCardAuthor{Card: "p1.r1.1", Author: "r2"}, store.SetCardState{Card: "p1.r1.1", State: Ready}}},
		{event: ReadYes{}, from: []string{Review}, cons: reads, expect: readFence, writes: []store.Write{store.SetCardState{Card: "p1", State: Merging}}},
		{event: ReadFix{Finding: "fix x"}, from: []string{Review}, cons: fixes, expect: append(slices.Clone(readFence), store.StreamHeld{Stream: "s1"}),
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardFinding{Card: "p1", Finding: "fix x"}, store.SetCardBounces{Card: "p1", Bounces: 1}}},
		{event: Landed{Head: h1}, from: []string{Merging}, expect: []store.Expect{p1}, writes: []store.Write{store.SetCardState{Card: "p1", State: LandedState}}},
		{event: Red{Head: h1, Reason: "ci red"}, from: []string{Merging}, expect: []store.Expect{p1},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Review}, store.SetCardReview{Card: "p1"}, store.SetCardReason{Card: "p1", Reason: "ci red"}, store.SetCardRed{Card: "p1", Red: true}}},
		{event: Released{}, from: []string{Working}, cons: []Card{{ID: "p1.w1", Kind: "work", Of: "p1", State: Fail, Gen: 6, Attempt: 1, Holder: "r1", Head: h2, Reason: "silent"}},
			expect: []store.Expect{p1, gen("p1.w1", 6), store.StreamHeld{Stream: "s1"}},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "silent"}, store.SetCardHead{Card: "p1", Head: h2}}},
		{event: Cancel{Reason: "no longer"}, cons: gone, expect: goneFence, writes: []store.Write{store.SetCardState{Card: "p1", State: Fail}, store.SetCardReason{Card: "p1", Reason: "no longer"}}},
		{event: Replace{By: "p9", Reason: "split"}, cons: gone, expect: []store.Expect{p1, gen("p9", 0), gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Fail}, store.SetCardReason{Card: "p1", Reason: "replaced by p9: split"}}},
		{event: NeedFailed{Need: "n1"}, need: Fail, cons: gone, expect: []store.Expect{p1, failed, gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Fail}, store.SetCardReason{Card: "p1", Reason: "need n1 failed"}}},
		{event: Waive{Need: "n1", Reason: "dropped"}, from: []string{Waiting}, need: Fail, expect: []store.Expect{p1, failed},
			writes: []store.Write{store.SetCardNeeds{Card: "p1", Needs: []string{}}, store.SetCardReason{Card: "p1", Reason: "dropped"}}},
		{event: Relink{Need: "n1", To: "n2", Reason: "moved"}, from: []string{Waiting}, need: Fail, expect: []store.Expect{p1, failed, gen("n2", 9)},
			writes: []store.Write{store.SetCardNeeds{Card: "p1", Needs: []string{"n2"}}, store.SetCardReason{Card: "p1", Reason: "moved"}}},
		{event: Edit{Prose: "new", Paths: []string{"card/"}, Reason: "scope"}, cons: gone, expect: goneFence,
			writes: []store.Write{store.SetCardProse{Card: "p1", Prose: "new"}, store.SetCardPaths{Card: "p1", Paths: []string{"card/"}}, store.SetCardReason{Card: "p1", Reason: "scope"}}},
		{event: Rescore{Score: 9}, expect: []store.Expect{p1}, writes: []store.Write{store.SetCardScore{Card: "p1", Score: 9}}},
		{event: SetPriority{Level: "blocker", Reason: "release"}, expect: []store.Expect{p1},
			writes: []store.Write{store.SetCardPriority{Card: "p1", Priority: "blocker"}, store.SetCardReason{Card: "p1", Reason: "release"}}},
		{event: Block{Reason: "bounced"}, from: []string{Ready, Review, Working}, cons: gone, expect: goneFence,
			writes: []store.Write{store.SetCardState{Card: "p1", State: Blocked}, store.SetCardReason{Card: "p1", Reason: "bounced"}}},
		{event: Unblock{Reason: "reviewed"}, from: []string{Blocked}, expect: []store.Expect{p1, store.StreamHeld{Stream: "s1"}},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Waiting}, store.SetCardReason{Card: "p1", Reason: "reviewed"}}},
	}
}

func (c pcase) move(state string) (Plan, *Refusal) {
	p := pr(state)
	if c.kind != "" {
		p.Kind = c.kind
	}
	return Move(p, c.cons, rows(), Stream{Name: "s1", Held: c.held}, needs(orElse(c.need, LandedState)), c.event)
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func TestProducerTable(t *testing.T) {
	t.Parallel()
	for _, c := range producerTable() {
		t.Run(name(c.event), func(t *testing.T) {
			from := cmpList(c.from, nonFinal)
			plan, r := c.move(from[len(from)-1])
			if c.event == (Block{Reason: "bounced"}) {
				plan, r = c.move(Working) // also from working once its consumer is withdrawn
			}
			require.Nil(t, r, "accepted from %s", from[len(from)-1])
			assert.Equal(t, c.expect, plan.Expect, "conditions")
			assert.Equal(t, c.writes, plan.Writes, "writes")
			for _, s := range from {
				if s == Review && c.event == (Block{Reason: "bounced"}) {
					continue // in review the fence also names the next read
				}
				_, r := c.move(s)
				assert.Nil(t, r, "accepted from %s", s)
			}
			for _, s := range producerStates {
				if slices.Contains(from, s) {
					continue
				}
				_, r := c.move(s)
				require.NotNil(t, r, "refused from %s", s)
				want := fmt.Sprintf("producer p1 is in %s, %s needs %s", s, name(c.event), orList(from))
				if s == LandedState || s == Fail {
					want = fmt.Sprintf("producer p1 is %s; nothing moves a %s card", s, s)
				}
				if c.kind == "sentinel" {
					want = strings.Replace(want, "producer", "sentinel", 1)
				}
				assert.Equal(t, want, r.Reason, "from %s", s)
			}
		})
	}
}

func cmpList(a, b []string) []string {
	if a != nil {
		return a
	}
	return b
}

// ccase is one row of the consumer table.
type ccase struct {
	event  Event
	from   []string
	c      Card
	expect []store.Expect
	writes []store.Write
}

func TestConsumerTable(t *testing.T) {
	t.Parallel()
	read := rd(Working, "")
	for _, c := range []ccase{
		{event: Take{Row: "r1"}, from: []string{Ready}, c: wk(Ready, "", ""), expect: []store.Expect{gen("p1.w1", 6), store.RowGen{Row: "r1", Gen: 4}, store.RowIs{Row: "r1", State: Up}},
			writes: []store.Write{store.SetCardState{Card: "p1.w1", State: Working}}},
		{event: Report{Row: "r1", Result: "ok", Head: h2, Reason: "done", Cost: 12, Priced: true}, from: []string{Working}, c: wk(Working, "", ""),
			expect: []store.Expect{gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1.w1", State: Done}, store.SetCardResult{Card: "p1.w1", Result: "ok"}, store.SetCardHead{Card: "p1.w1", Head: h2},
				store.SetCardReason{Card: "p1.w1", Reason: "done"}, store.SetCardCost{Card: "p1.w1", Cost: 12, Priced: true}}},
		{event: Report{Row: "r1", Result: "ok", Head: h2, Cost: 12, Priced: true, Usage: "input=3 output=4 model=deepseek/deepseek-flash billing=api charged=-"}, from: []string{Working}, c: wk(Working, "", ""),
			expect: []store.Expect{gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1.w1", State: Done}, store.SetCardResult{Card: "p1.w1", Result: "ok"}, store.SetCardHead{Card: "p1.w1", Head: h2},
				store.SetCardCost{Card: "p1.w1", Cost: 12, Priced: true}, store.SetCardUsage{Card: "p1.w1", Usage: "input=3 output=4 model=deepseek/deepseek-flash billing=api charged=-"}}},
		{event: Report{Row: "r1", Result: "ok", Head: h1}, from: []string{Working}, c: read, expect: []store.Expect{gen("p1.r1.1", 2), p1},
			writes: []store.Write{store.SetCardState{Card: "p1.r1.1", State: Done}, store.SetCardResult{Card: "p1.r1.1", Result: "ok"}}},
		{event: Withdraw{Reason: "silent", Head: h2, Stream: Stream{Name: "s1"}}, from: []string{Working}, c: wk(Working, "", ""),
			expect: []store.Expect{gen("p1.w1", 6), p1, store.StreamHeld{Stream: "s1"}},
			writes: []store.Write{store.SetCardState{Card: "p1.w1", State: Fail}, store.SetCardReason{Card: "p1.w1", Reason: "silent"}, store.SetCardHead{Card: "p1.w1", Head: h2},
				store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "silent"}, store.SetCardHead{Card: "p1", Head: h2}}},
	} {
		t.Run(name(c.event)+" "+c.c.Kind, func(t *testing.T) {
			k := c.c
			prod := pr(Review)
			if _, ok := c.event.(Withdraw); ok && c.c.Kind == "work" {
				prod = pr(Working) // the live work consumer releases its working producer
			}
			plan, r := MoveConsumer(k, prod, rows()["r1"], c.event)
			require.Nil(t, r)
			assert.Equal(t, c.expect, plan.Expect)
			assert.Equal(t, c.writes, plan.Writes)
			for _, s := range consumerStates {
				k.State = s
				_, r := MoveConsumer(k, prod, rows()["r1"], c.event)
				if slices.Contains(c.from, s) {
					assert.Nil(t, r, s)
					continue
				}
				want := fmt.Sprintf("consumer %s is in %s, %s needs %s", k.ID, s, name(c.event), orList(c.from))
				if s == Done || s == Fail {
					want = fmt.Sprintf("consumer %s is %s; nothing moves a %s card", k.ID, s, s)
				}
				if s == Ready {
					if v, ok := c.event.(Withdraw); ok && v.Head != "" && c.c.Kind == "work" {
						want = "consumer " + k.ID + " is ready; it has not pushed a head"
					}
				}
				require.NotNil(t, r, s)
				assert.Equal(t, want, r.Reason, s)
			}
		})
	}
}

func TestRowTable(t *testing.T) {
	t.Parallel()
	up, down := rows()["r1"], rows()["r3"]
	held := rows()["r4"]
	fence := func(r Row) []store.Expect { return []store.Expect{store.RowGen{Row: r.Name, Gen: r.Gen}} }
	for _, c := range []struct {
		event  Event
		r      Row
		writes []store.Write
	}{
		{Seen{}, down, []store.Write{store.SetRowState{Row: "r3", State: Up}}},
		{Gone{}, up, []store.Write{store.SetRowState{Row: "r1", State: Down}, store.SetRowFlaps{Row: "r1", Flaps: 1}}},
		{Hold{Reason: "maintenance"}, up, []store.Write{store.SetRowHeld{Row: "r1", Held: true, Reason: "maintenance"}}},
		{Hold{}, down, []store.Write{store.SetRowHeld{Row: "r3", Held: true}}},
		{Unhold{Reason: "back"}, held, []store.Write{store.SetRowHeld{Row: "r4", Held: false, Reason: "back"}}},
	} {
		plan, r := MoveRow(c.r, c.event)
		require.Nil(t, r, name(c.event))
		assert.Equal(t, fence(c.r), plan.Expect, name(c.event))
		assert.Equal(t, c.writes, plan.Writes, name(c.event))
	}
	for _, c := range []refusal{
		{rowMove(up, Seen{}), "row r1 is up, Seen needs down"},
		{rowMove(down, Gone{}), "row r3 is down, Gone needs up"},
		{rowMove(held, Hold{}), "row r4 is already held"},
		{rowMove(up, Unhold{}), "row r1 is already not held"},
		{rowMove(up, Resolve{}), "Resolve is not a row event; MoveRow takes Seen, Gone, Hold and Unhold"},
	} {
		c.check(t)
	}
}

func rowMove(r Row, e Event) *Refusal { _, ref := MoveRow(r, e); return ref }

// refusal is a move and the reason it must be refused with.
type refusal struct {
	r    *Refusal
	want string
}

func (c refusal) check(t *testing.T) {
	t.Helper()
	if assert.NotNil(t, c.r, c.want) {
		assert.Equal(t, c.want, c.r.Reason)
	}
}

// mv is Move of p1 in state with the given records; s1 not held, n1 landed.
func mv(state string, cons []Card, e Event) *Refusal {
	_, r := Move(pr(state), cons, rows(), Stream{Name: "s1"}, needs(LandedState), e)
	return r
}

func TestProducerRefusals(t *testing.T) {
	t.Parallel()
	heldStream := func(e Event) *Refusal {
		_, r := Move(pr(Ready), nil, rows(), Stream{Name: "s1", Held: true}, needs(LandedState), e)
		return r
	}
	sentinel := pr(Waiting)
	sentinel.Kind = "sentinel"
	_, sentResolve := Move(sentinel, nil, rows(), Stream{Name: "s1"}, needs(LandedState), Resolve{})
	sentinel.State = Ready
	_, sentStart := Move(sentinel, nil, rows(), Stream{Name: "s1"}, needs(LandedState), Start{Row: "r1", Branch: "b", Base: base})
	_, onConsumer := Move(wk(Ready, "", ""), nil, rows(), Stream{Name: "s1"}, nil, Resolve{})
	_, wrongStream := Move(pr(Waiting), nil, rows(), Stream{Name: "s2"}, needs(LandedState), Resolve{})
	_, missingNeed := Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, nil, Resolve{})
	_, waitingNeed := Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, needs(Waiting), Resolve{})
	start := func(row string) Event { return Start{Row: row, Branch: "b", Base: base} }
	live := []Card{wk(Working, "", "")}
	liveReason := "producer p1 has live consumer p1.w1 on row r1; withdraw it first"
	later := pr(Working)
	later.Attempt = 2
	stale := rd(Done, "ok") // cut before a Red: review 0
	stale.Review = 0
	_, oldAttempt := Move(later, []Card{wk(Done, "ok", h2)}, rows(), Stream{Name: "s1"}, nil, Finished{Consumer: "p1.w1"})
	for _, c := range []refusal{
		{onConsumer, "card p1.w1 is a work consumer; Move is for producers"},
		{mv(Waiting, nil, Take{Row: "r1"}), "Take is not a producer event; Move takes the producer table's events"},
		{sentResolve, "sentinel p1 is landed by Reached, never resolved"},
		{sentStart, "sentinel p1 never has a consumer"},
		{mv(Waiting, nil, Reached{}), "producer p1 is not a sentinel; Reached is for sentinels"},
		{wrongStream, "stream s2 is not producer p1's stream s1"},
		{missingNeed, "need n1 of producer p1 does not exist"},
		{waitingNeed, "need n1 of producer p1 is waiting, not landed"},
		{heldStream(Resolve{}), "producer p1 is in ready, Resolve needs waiting"},
		{mv(Ready, nil, Park{}), "stream s1 is not held; Park is for a held stream"},
		{heldStream(start("r1")), "stream s1 is held; no new consumers start in a held stream"},
		{mv(Ready, nil, start("r9")), "row r9 does not exist"},
		{mv(Ready, nil, start("r3")), "row r3 is down; a consumer is dealt only to a row that is up"},
		{mv(Ready, nil, start("r4")), "row r4 is held; nothing is dealt to a held row"},
		{mv(Ready, nil, start("r5")), "row r5 serves heavy; consumer needs pro"},
		{mv(Ready, nil, Start{Row: "r1", Branch: "b", Base: "abc"}), "base abc is not a full commit sha"},
		{mv(Ready, nil, Start{Row: "r1", Branch: "b"}), "base is empty; it needs a full commit sha"},
		{mv(Ready, nil, Start{Row: "r1", Base: base}), "Start of producer p1 needs a branch"},
		{mv(Ready, live, start("r1")), liveReason},
		{mv(Blocked, nil, start("r1")), "producer p1 is in blocked, Start needs ready"},
		{mv(Working, nil, Finished{Consumer: "p1.w9"}), "card p1.w9 is not producer p1's work consumer of attempt 1"},
		{mv(Working, []Card{rd(Done, "ok")}, Finished{Consumer: "p1.r1.1"}), "card p1.r1.1 is not producer p1's work consumer of attempt 1"},
		{oldAttempt, "card p1.w1 is not producer p1's work consumer of attempt 2"},
		{mv(Working, live, Finished{Consumer: "p1.w1"}), "consumer p1.w1 is working, not done; Finished waits for its report, or withdraw it"},
		{mv(Working, []Card{wk(Done, "fix", h2)}, Finished{Consumer: "p1.w1"}), "consumer p1.w1 reported fix, Finished needs ok"},
		{mv(Working, []Card{wk(Done, "fault", h2)}, Broken{Consumer: "p1.w1"}), "consumer p1.w1 reported fault, Broken needs fail"},
		{mv(Working, []Card{wk(Done, "fail", h2)}, Faulted{Consumer: "p1.w1"}), "consumer p1.w1 reported fail, Faulted needs fault"},
		{mv(Working, live, Released{}), "consumer p1.w1 is working; Released needs it withdrawn (fail)"},
		{mv(Working, nil, Released{}), "card p1.w1 is not producer p1's work consumer of attempt 1"},
		{mv(Review, nil, AskRead{Row: "r2"}), "r2 wrote producer p1's head and may not read it"},
		{mv(Review, []Card{wk(Working, "", "")}, AskRead{Row: "r1"}), "producer p1 has a live work consumer p1.w1; a read is cut only after it reports"},
		{mv(Review, []Card{rd(Done, "fix")}, ReadYes{}), "producer p1 has no read of review 1 that said ok; ReadYes needs one"},
		{mv(Review, []Card{rd(Fail, "")}, ReadYes{}), "producer p1 has no read of review 1 that said ok; ReadYes needs one"},
		{mv(Review, []Card{stale}, ReadYes{}), "producer p1 has no read of review 1 that said ok; ReadYes needs one"},
		{mv(Review, nil, ReadYes{}), "producer p1 has no read of review 1 that said ok; ReadYes needs one"},
		{mv(Review, []Card{rd(Working, "")}, ReadYes{}), "producer p1 has live consumer p1.r1.1 on row r1; withdraw it first"},
		{mv(Review, []Card{rd(Ready, "")}, ReadFix{}), "producer p1 has live consumer p1.r1.1 on row r1; withdraw it first"},
		{mv(Merging, nil, Landed{Head: h2}), "producer p1 is at head " + h1 + ", Landed names " + h2},
		{mv(Merging, nil, Red{Head: h2}), "producer p1 is at head " + h1 + ", Red names " + h2},
		{mv(Working, live, Cancel{}), liveReason},
		{mv(Working, live, Replace{By: "p9"}), liveReason},
		{mv(Ready, nil, Replace{}), "Replace of producer p1 needs the card it is replaced by"},
		{mv(Ready, nil, Replace{By: "p1"}), "producer p1 cannot be replaced by itself"},
		{mv(Ready, nil, Replace{By: "nope"}), "card nope does not exist (Replace takes the card it is replaced by among the cards passed)"},
		{mv(Working, live, Block{}), liveReason},
		{mv(Waiting, nil, NeedFailed{Need: "n9"}), "n9 is not a need of producer p1"},
		{mv(Waiting, nil, NeedFailed{Need: "n1"}), "need n1 of producer p1 is landed, not fail"},
		{mv(Waiting, nil, Waive{Need: "n1"}), "need n1 of producer p1 is landed, not fail"},
		{mv(Waiting, nil, Unblock{}), "producer p1 is in waiting, Unblock needs blocked"},
		{mv(Working, live, Edit{Prose: "x"}), "producer p1 is held by r1 in consumer p1.w1; message r1 instead"},
		{mv(Ready, nil, Edit{}), "Edit of producer p1 changes nothing"},
		{mv(Ready, nil, Rescore{Score: 5}), "producer p1 already has score 5"},
		{mv(Ready, nil, Rescore{Score: -1}), "score -1 is below 0"},
		{mv(Ready, nil, SetPriority{Level: "urgent"}), "priority urgent is not one of low, normal, high, critical or blocker"},
		{mv(Ready, nil, SetPriority{}), "producer p1 is already at priority normal"},
		{mv(LandedState, nil, Rescore{Score: 1}), "producer p1 is landed; nothing moves a landed card"},
		{mv(Fail, nil, SetPriority{Level: "high"}), "producer p1 is fail; nothing moves a fail card"},
	} {
		c.check(t)
	}
}

// Relink: the target must exist and be a live producer, and the new needs
// must make no cycle, named as the loop.
func TestRelinkRefusals(t *testing.T) {
	t.Parallel()
	relink := func(to string, graph map[string]Card) *Refusal {
		graph["n1"] = Card{ID: "n1", Kind: "producer", State: Fail}
		_, r := Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, graph, Relink{Need: "n1", To: to})
		return r
	}
	for _, c := range []refusal{
		{relink("n9", map[string]Card{}), "card n9 is not among the cards passed"},
		{relink("n2", map[string]Card{"n2": {ID: "n2", Kind: "producer", State: LandedState}}), "producer n2 is landed; a need is relinked only to a producer or a sentinel that is not final"},
		{relink("n2", map[string]Card{"n2": {ID: "n2", Kind: "work", State: Ready}}), "card n2 is a work consumer; a need is relinked only to a producer or a sentinel that is not final"},
		{relink("n2", map[string]Card{"n2": {ID: "n2", Kind: "producer", State: Waiting, Needs: []string{"p1"}}}), "p1 needs n2 needs p1"},
		{relink("n2", map[string]Card{"n2": {ID: "n2", Kind: "producer", State: Waiting, Needs: []string{"n3"}}, "n3": {ID: "n3", Kind: "producer", Needs: []string{"p1"}}}), "p1 needs n2 needs n3 needs p1"},
	} {
		c.check(t)
	}
	p := pr(Waiting)
	p.Needs = []string{"n1", "n2"}
	_, r := Move(p, nil, rows(), Stream{Name: "s1"}, map[string]Card{"n1": {ID: "n1", State: Fail}, "n2": {ID: "n2", Kind: "producer", State: Waiting}}, Relink{Need: "n1", To: "n2"})
	refusal{r, "producer p1 already needs n2"}.check(t)
}

func TestConsumerRefusals(t *testing.T) {
	t.Parallel()
	cm := func(c Card, e Event) *Refusal { _, r := MoveConsumer(c, pr(Review), rows()["r1"], e); return r }
	work, read := wk(Working, "", ""), rd(Working, "")
	other := pr(Review)
	other.ID = "p2"
	_, wrongProducer := MoveConsumer(work, other, rows()["r1"], Take{Row: "r1"})
	_, wrongRow := MoveConsumer(wk(Ready, "", ""), pr(Review), rows()["r2"], Take{Row: "r1"})
	for _, c := range []refusal{
		{cm(pr(Ready), Take{Row: "r1"}), "card p1 is a producer; MoveConsumer is for consumers"},
		{wrongProducer, "consumer p1.w1 belongs to p1, not p2"},
		{wrongRow, "MoveConsumer was passed row r2 for a take on r1; pass the consumer's holder row"},
		{cm(wk(Ready, "", ""), Take{Row: "r2"}), "consumer p1.w1 is held by r1, not r2"},
		{cm(work, Report{Row: "r2", Result: "ok", Head: h2}), "consumer p1.w1 is held by r1, not r2"},
		{cm(work, Report{Row: "r1", Result: "ok", Head: "abc"}), "head abc is not a full commit sha"},
		{cm(work, Report{Row: "r1", Result: "ok"}), "head is empty; it needs a full commit sha"},
		{cm(work, Report{Row: "r1", Result: "fail", Head: "xyz"}), "head xyz is not a full commit sha"},
		{cm(work, Report{Row: "r1", Result: "ok", Head: base}), "head " + base + " is the base; nothing was done"},
		{cm(work, Report{Row: "r1", Result: "maybe"}), `result "maybe" is not ok, fix, fail or fault`},
		{cm(read, Report{Row: "r1", Result: "ok", Head: h2}), "read p1.r1.1 was cut at " + h1 + ", the report names " + h2},
		{cm(work, Report{Row: "r1", Result: "fail", Head: base}), "head " + base + " is the base; nothing was done"},
		{cm(work, Resolve{}), "Resolve is not a consumer event; MoveConsumer takes Take, Report and Withdraw"},
	} {
		c.check(t)
	}
	plan, r := MoveConsumer(work, pr(Review), rows()["r1"], Report{Row: "r1", Result: "fault"})
	require.Nil(t, r, "a fault needs no head")
	assert.Equal(t, []store.Write{store.SetCardState{Card: "p1.w1", State: Done}, store.SetCardResult{Card: "p1.w1", Result: "fault"}}, plan.Writes)
}

// A Start in review fences the next read's id once; noLive fences every
// consumer it read.
func TestFences(t *testing.T) {
	t.Parallel()
	plan, r := Move(pr(Review), []Card{rd(Done, "ok"), wk(Done, "ok", h1)}, rows(), Stream{Name: "s1"}, nil, AskRead{Row: "r1"})
	require.Nil(t, r)
	n := 0
	for _, e := range plan.Expect {
		if e == (store.CardAbsent{Card: "p1.r1.2"}) {
			n++
		}
	}
	assert.Equal(t, 1, n, "the next read's id is fenced once")
	assert.Contains(t, plan.Expect, gen("p1.r1.1", 2))
	assert.Contains(t, plan.Expect, gen("p1.w1", 6))
}

func TestMerge(t *testing.T) {
	t.Parallel()
	a := Plan{Expect: []store.Expect{p1}, Writes: []store.Write{store.SetCardScore{Card: "p1", Score: 1}}}
	b := Plan{Expect: []store.Expect{gen("p2", 1)}, Writes: []store.Write{store.SetCardScore{Card: "p2", Score: 2}}}
	got, r := Merge(a, b)
	require.Nil(t, r)
	assert.Equal(t, Plan{Expect: append(a.Expect, b.Expect...), Writes: append(a.Writes, b.Writes...)}, got)
	dup := Plan{Writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}}}
	_, r = Merge(dup, dup)
	refusal{r, "merge of plan 1 and plan 2 refused: a second write to card p1 field state; one op writes a field once"}.check(t)
}

// Round 2, from Stella's and Johnny's reads of a822dfc7.

// Johnny's probe TestProbeAskReadLiveWorkBehindLiveRead: a producer in review
// with a working work consumer (a state the machine never makes, which it must
// still refuse to build on) and a ready read, in both orders.
func TestNoLiveReadsEveryConsumer(t *testing.T) {
	t.Parallel()
	work := Card{ID: "p1.w9", Kind: "work", Of: "p1", State: Working, Gen: 1, Attempt: 1, Holder: "r3"}
	read := rd(Ready, "")
	for _, cons := range [][]Card{{work, read}, {read, work}} {
		refusal{mv(Review, cons, AskRead{Row: "r1"}), "producer p1 has a live work consumer p1.w9; a read is cut only after it reports"}.check(t)
		for _, e := range []Event{Cancel{}, Replace{By: "p9"}, Block{}, ReadYes{}} {
			refusal{mv(Review, cons, e), "producer p1 has live consumer p1.r1.1 on row r1; withdraw it first"}.check(t)
		}
		refusal{mv(Working, cons, NeedFailed{Need: "n1"}), "need n1 of producer p1 is landed, not fail"}.check(t)
	}
	_, r := Move(pr(Waiting), []Card{read, work}, rows(), Stream{Name: "s1"}, needs(Fail), NeedFailed{Need: "n1"})
	refusal{r, "producer p1 has live consumer p1.r1.1 on row r1; withdraw it first"}.check(t)
	// the allowed path still fences every consumer it read, and the next read
	read.Holder = "r2" // a live read on another row
	plan, r := Move(pr(Review), []Card{read, wk(Done, "ok", h1)}, rows(), Stream{Name: "s1"}, nil, AskRead{Row: "r1"})
	require.Nil(t, r)
	assert.Subset(t, plan.Expect, []store.Expect{gen("p1.r1.1", 2), gen("p1.w1", 6), store.CardAbsent{Card: "p1.r1.2"}})
}

// Stella's P2: results by consumer kind.
func TestReportResultByKind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		c      Card
		result string
		want   string // "" accepted
	}{
		{wk(Working, "", ""), "ok", ""},
		{wk(Working, "", ""), "fail", ""},
		{wk(Working, "", ""), "fault", ""},
		{wk(Working, "", ""), "fix", "a work consumer reports ok, fail or fault; fix is a read's result"},
		{rd(Working, ""), "ok", ""},
		{rd(Working, ""), "fix", ""},
		{rd(Working, ""), "fault", ""},
		{rd(Working, ""), "fail", "a read consumer reports ok, fix or fault; fail is a work's result"},
	} {
		head := h2
		if c.c.Kind == "read" {
			head = h1
		}
		_, r := MoveConsumer(c.c, pr(Review), rows()["r1"], Report{Row: "r1", Result: c.result, Head: head})
		if c.want == "" {
			assert.Nil(t, r, "%s %s", c.c.Kind, c.result)
			continue
		}
		refusal{r, c.want}.check(t)
	}
}

// Stella's P1: a cut consumer carries the task, in slices of its own.
func TestCutCopiesTask(t *testing.T) {
	t.Parallel()
	p := pr(Ready)
	p.Brief, p.Prose, p.Paths, p.DoneWhen = "do p1", "write it", []string{"src/"}, []string{"tests pass"}
	plan, r := Move(p, nil, rows(), Stream{Name: "s1"}, nil, Start{Row: "r1", Branch: "b", Base: base})
	require.Nil(t, r)
	want := store.PutCard{ID: "p1.w2", Kind: "work", Of: "p1", Stream: "s1", Holder: "r1", Tier: "pro", Attempt: 2,
		Brief: "do p1", Prose: "write it", Paths: []string{"src/"}, DoneWhen: []string{"tests pass"}}
	assert.Equal(t, store.Write(want), plan.Writes[3])
	p.Paths[0], p.DoneWhen[0] = "changed/", "changed"
	assert.Equal(t, store.Write(want), plan.Writes[3], "the plan owns its copies")
	p.State, p.Author = Review, "r2"
	plan, r = Move(p, nil, rows(), Stream{Name: "s1"}, nil, AskRead{Row: "r1"})
	require.Nil(t, r)
	read := plan.Writes[0].(store.PutCard)
	assert.Equal(t, []any{"do p1", "write it", []string{"changed/"}, 1}, []any{read.Brief, read.Prose, read.Paths, read.Review})
}

// Stella's P1: Relink fences every card its cycle walk read, and refuses a
// walk that needs a card it was not given.
func TestRelinkFencesTheWalk(t *testing.T) {
	t.Parallel()
	graph := map[string]Card{
		"n1": {ID: "n1", Kind: "producer", State: Fail, Gen: 1},
		"q":  {ID: "q", Kind: "producer", State: Waiting, Gen: 4, Needs: []string{"r"}},
		"r":  {ID: "r", Kind: "producer", State: Waiting, Gen: 5, Needs: []string{"b"}},
		"b":  {ID: "b", Kind: "producer", State: Fail, Gen: 6},
	}
	plan, r := Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, graph, Relink{Need: "n1", To: "q"})
	require.Nil(t, r)
	assert.Equal(t, []store.Expect{p1, store.CardState{Card: "n1", State: Fail}, gen("q", 4), gen("r", 5), gen("b", 6)}, plan.Expect)
	delete(graph, "b")
	_, r = Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, graph, Relink{Need: "n1", To: "q"})
	refusal{r, "card b, needed by r, was not passed; Relink needs every card reachable through needs and after"}.check(t)
}

func TestHoldStream(t *testing.T) {
	t.Parallel()
	a, b := pr(Ready), pr(Ready)
	b.ID, b.Gen, b.Reason = "p2", 8, "stream s1 is held"
	plan, r := MoveStream(Stream{Name: "s1"}, HoldStream{Ready: []Card{a, b}})
	require.Nil(t, r)
	assert.Equal(t, []store.Expect{store.StreamHeld{Stream: "s1"}, store.AtSeq{}, p1, gen("p2", 8)}, plan.Expect)
	assert.Equal(t, []store.Write{store.SetStreamHeld{Stream: "s1", Held: true}, store.SetCardState{Card: "p1", State: Waiting},
		store.SetCardReason{Card: "p1", Reason: "stream s1 is held"}, store.SetCardState{Card: "p2", State: Waiting}}, plan.Writes)
	_, r = MoveStream(Stream{Name: "s1"}, HoldStream{Ready: []Card{a, a}})
	refusal{r, "producer p1 is listed twice in HoldStream; name each ready producer once"}.check(t)
	other := pr(Ready)
	other.Stream = "s2"
	hs := func(s Stream, cs ...Card) *Refusal { _, r := MoveStream(s, HoldStream{Ready: cs}); return r }
	for _, c := range []refusal{
		{hs(Stream{Name: "s1", Held: true}), "stream s1 is already held"},
		{hs(Stream{Name: "s1"}, pr(Working)), "producer p1 is in working, HoldStream parks ready producers"},
		{hs(Stream{Name: "s1"}, other), "producer p1 is in stream s2, not s1"},
		{hs(Stream{Name: "s1"}, wk(Ready, "", "")), "card p1.w1 is a work consumer; HoldStream parks producers"},
	} {
		c.check(t)
	}
	_, r = MoveStream(Stream{Name: "s1"}, Resolve{})
	refusal{r, "Resolve is not a stream event; MoveStream takes HoldStream and UnholdStream"}.check(t)
}

// Round 2, from the first cold read of layer 2 and Emma's read of a822dfc7.

func TestBlockFromWorking(t *testing.T) {
	t.Parallel()
	for result, apply := range map[string]string{"ok": "Finished", "fail": "Broken", "fault": "Faulted"} {
		refusal{mv(Working, []Card{wk(Done, result, h2)}, Block{}), "consumer p1.w1 reported " + result + "; apply it with " + apply + " before Block"}.check(t)
	}
	assert.Nil(t, mv(Working, []Card{wk(Fail, "", "")}, Block{Reason: "x"}), "withdrawn first")
	refusal{mv(Working, nil, Block{}), "producer p1 is working; its work consumer p1.w1 was not passed (the consumer list must be complete)"}.check(t)
}

// A Withdraw of the working producer's work consumer releases the producer
// in the same plan; any other withdraw does not.
func TestWithdrawReleases(t *testing.T) {
	t.Parallel()
	_, r := MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r1"], Withdraw{Reason: "preempted", Preempt: true})
	refusal{r, `Withdraw of p1.w1 releases producer p1; it needs p1's stream s1, not ""`}.check(t)
	plan, r := MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r1"], Withdraw{Reason: "preempted", Head: h2, Stream: Stream{Name: "s1", Held: true}, Preempt: true})
	require.Nil(t, r)
	assert.Equal(t, []store.Expect{gen("p1.w1", 6), p1, store.StreamHeld{Stream: "s1", Held: true}}, plan.Expect, "released into a held stream: parked")
	assert.Equal(t, []store.Write{store.SetCardState{Card: "p1.w1", State: Fail}, store.SetCardReason{Card: "p1.w1", Reason: "preempted"}, store.SetCardHead{Card: "p1.w1", Head: h2},
		store.SetCardState{Card: "p1", State: Waiting}, store.SetCardReason{Card: "p1", Reason: "held: stream s1 is held"}, store.SetCardHead{Card: "p1", Head: h2}}, plan.Writes)
	old := wk(Working, "", "")
	old.ID = "p1.w0"
	plan, r = MoveConsumer(old, pr(Working), rows()["r1"], Withdraw{Reason: "stray"})
	require.Nil(t, r)
	assert.Len(t, plan.Writes, 2, "not the attempt's consumer: nothing released")
	plan, r = MoveConsumer(rd(Working, ""), pr(Review), rows()["r1"], Withdraw{Reason: "slow"})
	require.Nil(t, r)
	assert.Len(t, plan.Writes, 2, "a read releases nothing")
	_, r = MoveConsumer(rd(Working, ""), pr(Review), rows()["r1"], Withdraw{Head: h2})
	refusal{r, "read p1.r1.1 is of head " + h1 + "; a read is withdrawn without a head of its own"}.check(t)
	_, r = MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r1"], Withdraw{Head: "abc", Stream: Stream{Name: "s1"}})
	refusal{r, "head abc is not a full commit sha"}.check(t)
	_, r = MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r1"], Withdraw{Head: base, Stream: Stream{Name: "s1"}})
	refusal{r, "head " + base + " is the base; nothing was done"}.check(t)
	ready := wk(Ready, "", "")
	_, r = MoveConsumer(ready, pr(Working), rows()["r1"], Withdraw{Reason: "x", Head: h2, Stream: Stream{Name: "s1"}})
	refusal{r, "consumer p1.w1 is ready; it has not pushed a head"}.check(t)
}

func TestNeedFailedInThisOp(t *testing.T) {
	t.Parallel()
	plan, r := Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, needs(Waiting), NeedFailed{Need: "n1", InThisOp: true})
	require.Nil(t, r)
	assert.Equal(t, []store.Expect{p1, store.CardState{Card: "n1", State: Fail}}, plan.Expect, "the fence refuses the plan applied alone; Merge drops it after the drop")
	plan, r = Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, needs(Ready), Waive{Need: "n1", InThisOp: true})
	require.Nil(t, r)
	assert.Equal(t, []store.Expect{p1, store.CardState{Card: "n1", State: Fail}}, plan.Expect)
	refusal{mv(Waiting, nil, NeedFailed{Need: "n1", InThisOp: true}), "need n1 of producer p1 is landed; it cannot fail in this op"}.check(t)
}

// Emma's defect 5, as ruled: sentinel guards, and Take on a down row.
func TestSentinelGuards(t *testing.T) {
	t.Parallel()
	g := pr(Waiting)
	g.Kind = "sentinel"
	sm := func(e Event, ns map[string]Card) *Refusal {
		_, r := Move(g, nil, rows(), Stream{Name: "s1"}, ns, e)
		return r
	}
	refusal{sm(Rescore{Score: 9}, nil), "sentinel p1 has no score of its own"}.check(t)
	refusal{sm(Edit{Prose: "x"}, nil), "sentinel p1 has no brief or prose to edit"}.check(t)
	assert.Nil(t, sm(SetPriority{Level: "blocker"}, nil), "a release gate may be a blocker")
	assert.Nil(t, sm(Relink{Need: "n1", To: "n2"}, needs(Fail)), "a sentinel has needs")
	gs := needs(Fail)
	gs["g2"] = Card{ID: "g2", Kind: "sentinel", State: Waiting}
	plan, r := Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, gs, Relink{Need: "n1", To: "g2"})
	require.Nil(t, r, "a sentinel may be the new need")
	assert.Equal(t, []string{"g2"}, plan.Writes[0].(store.SetCardNeeds).Needs)
	g.State = Review
	refusal{sm(AskRead{Row: "r1"}, nil), "sentinel p1 never has a consumer"}.check(t)
}

func TestTakeRowState(t *testing.T) {
	t.Parallel()
	c := wk(Ready, "", "")
	c.Holder = "r3"
	_, r := MoveConsumer(c, pr(Working), rows()["r3"], Take{Row: "r3"})
	refusal{r, "row r3 is down; it cannot take"}.check(t)
	c.Holder = "r4"
	plan, r := MoveConsumer(c, pr(Working), rows()["r4"], Take{Row: "r4"})
	require.Nil(t, r, "a hold stops new deals, not work already dealt")
	assert.Contains(t, plan.Expect, store.Expect(store.RowIs{Row: "r4", State: Up}))
}

// D10: layer 1's value rules are refusals, so no plan is ErrInvalid.
func TestValueRules(t *testing.T) {
	t.Parallel()
	far := pr(Ready)
	far.Review = math.MaxInt32
	far.State = Merging
	_, red := Move(far, nil, rows(), Stream{Name: "s1"}, nil, Red{Head: h1})
	_, cost := MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r1"], Report{Row: "r1", Result: "fail", Cost: -1})
	_, noName := MoveRow(Row{}, Hold{})
	bounced := pr(Review)
	bounced.Bounces = math.MaxInt32
	_, bounces := Move(bounced, []Card{rd(Done, "fix")}, rows(), Stream{Name: "s1"}, nil, ReadFix{Finding: "x"})
	for _, c := range []refusal{
		{mv(Ready, nil, Rescore{Score: math.MaxInt32 + 1}), "score 2147483648 is outside the range 0..2147483647"},
		{mv(Ready, nil, Start{Row: "r1", Branch: "b\x00", Base: base}), `branch "b\x00" is not valid UTF-8 or holds a NUL byte`},
		{mv(Ready, nil, Start{Row: "r1", Branch: "b\xff", Base: base}), `branch "b\xff" is not valid UTF-8 or holds a NUL byte`},
		{mv(Ready, nil, SetPriority{Level: "high", Reason: "\x00"}), `reason "\x00" is not valid UTF-8 or holds a NUL byte`},
		{red, "producer p1 is at review 2147483647, the most there is"},
		{cost, "cost -1 is below 0"},
		{noName, "a row needs a name"},
		{bounces, "bounces 2147483648 is outside the range 0..2147483647"},
	} {
		c.check(t)
	}
	long := pr(Ready)
	long.ID = strings.Repeat("p", store.MaxName-2)
	_, r := Move(long, nil, rows(), Stream{Name: "s1"}, nil, Start{Row: "r1", Branch: "b", Base: base})
	refusal{r, "producer " + long.ID + "'s id is too long to name its consumer " + long.ID + ".w2"}.check(t)
}

func TestAdd(t *testing.T) {
	t.Parallel()
	cards := map[string]Card{
		"n1": {ID: "n1", Kind: "producer", State: Waiting},
		"n2": {ID: "n2", Kind: "producer", State: Waiting, Needs: []string{"n1"}, After: []string{"g1"}},
		"g1": {ID: "g1", Kind: "sentinel", State: Waiting},
		"w1": {ID: "w1", Kind: "work", Of: "n1", State: Ready},
	}
	add := Add{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "high", Score: 3, Needs: []string{"n2"}, After: []string{"g1"},
		Brief: "do p1", Prose: "p", Paths: []string{"x/"}, DoneWhen: []string{"t"}}
	plan, r := MoveAdd(add, cards)
	require.Nil(t, r)
	assert.Equal(t, []store.Expect{store.CardAbsent{Card: "p1"}}, plan.Expect)
	assert.Equal(t, []store.Write{store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro",
		Priority: "high", Score: 3, Needs: []string{"n2"}, After: []string{"g1"}, Brief: "do p1", Prose: "p", Paths: []string{"x/"}, DoneWhen: []string{"t"}}}, plan.Writes)
	with := func(f func(a *Add)) *Refusal { a := add; f(&a); _, r := MoveAdd(a, cards); return r }
	for _, c := range []refusal{
		{with(func(a *Add) { a.Kind = "work" }), "Add makes a producer or sentinel, not a work; consumers are cut by Start and AskRead"},
		{with(func(a *Add) { a.ID = "n1" }), "card n1 exists"},
		{with(func(a *Add) { a.Kind, a.Score = "sentinel", 0 }), "sentinel p1 has no brief"},
		{with(func(a *Add) { a.Kind, a.Brief = "sentinel", "" }), "sentinel p1 has no score of its own"},
		{with(func(a *Add) { a.Priority = "urgent" }), "priority urgent is not one of low, normal, high, critical or blocker"},
		{with(func(a *Add) { a.Needs = []string{"n9"} }), "need n9 of producer p1 is not a card"},
		{with(func(a *Add) { a.Needs = []string{"n1", "n1"} }), "need n1 of producer p1 is named twice"},
		{with(func(a *Add) { a.Needs = []string{"p1"} }), "p1 needs p1"},
		{with(func(a *Add) { a.Needs = []string{"w1"} }), "need w1 of producer p1 is a work; a need is a producer or a sentinel"},
		{with(func(a *Add) { a.After = []string{"w1"} }), "after w1 of producer p1 is a work consumer; a card comes after a producer or sentinel"},
		{with(func(a *Add) { a.After = []string{"p1"} }), "p1 comes after p1"},
		{with(func(a *Add) { a.After = []string{"x9"} }), "after x9 of producer p1 is not a card"},
		{with(func(a *Add) { a.ID = "" }), "a card needs a name"},
		{with(func(a *Add) { a.Tier = "huge" }), `tier "huge" is not one of flash, pro, heavy or frontier`},
	} {
		c.check(t)
	}
	sentinelNeed := add
	sentinelNeed.Needs = []string{"g1"}
	_, r = MoveAdd(sentinelNeed, cards)
	require.Nil(t, r, "a sentinel may be a need")
	delete(cards, "n1")
	_, r = MoveAdd(add, cards)
	refusal{r, "card n1, reached from n2, was not passed; Add needs every card reachable through needs and after"}.check(t)
	// a loop through after: a card the store holds that comes after the new
	// one cannot exist, so a passed graph that claims one is refused as a cycle
	cards["n1"] = Card{ID: "n1", Kind: "producer", After: []string{"p1"}}
	_, r = MoveAdd(add, cards)
	refusal{r, "p1 needs n2 needs n1 needs p1"}.check(t)
}

// The card model's ruling: ReadFix needs a done fix read of this review, as
// ReadYes needs an ok one.
func TestReadFixNeedsAFixRead(t *testing.T) {
	t.Parallel()
	stale := rd(Done, "fix")
	stale.Review = 0
	for _, cons := range [][]Card{nil, {rd(Done, "ok")}, {stale}, {rd(Fail, "")}} {
		refusal{mv(Review, cons, ReadFix{Finding: "x"}), "producer p1 has no read of review 1 that said fix; ReadFix needs one"}.check(t)
	}
	assert.Nil(t, mv(Review, []Card{rd(Done, "fix")}, ReadFix{Finding: "x"}))
}
