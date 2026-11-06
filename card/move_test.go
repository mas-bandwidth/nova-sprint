package card

import (
	"fmt"
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
	return map[string]Card{"n1": {ID: "n1", Kind: "producer", State: state}, "n2": {ID: "n2", Kind: "producer", State: Waiting, Gen: 9}}
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
	readFence := []store.Expect{p1, gen("p1.w1", 6), gen("p1.r1.1", 2), store.CardAbsent{Card: "p1.r1.2"}}
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
				store.SetCardReason{Card: "p1.w2", Reason: "old"}, store.SetCardState{Card: "p1.w2", State: Ready}}},
		{event: Finished{Consumer: "p1.w1"}, from: []string{Working}, cons: []Card{wk(Done, "ok", h2)}, expect: []store.Expect{p1, gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Review}, store.SetCardReview{Card: "p1"}, store.SetCardHead{Card: "p1", Head: h2}, store.SetCardAuthor{Card: "p1", Author: "r1"}}},
		{event: Broken{Consumer: "p1.w1"}, from: []string{Working}, cons: []Card{wk(Done, "fail", h2)}, expect: []store.Expect{p1, gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "tests red"}, store.SetCardBounces{Card: "p1", Bounces: 1}, store.SetCardHead{Card: "p1", Head: h2}}},
		{event: Faulted{Consumer: "p1.w1"}, from: []string{Working}, cons: []Card{wk(Done, "fault", h2)}, expect: []store.Expect{p1, gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "tests red"}, store.SetCardHead{Card: "p1", Head: h2}}},
		{event: AskRead{Row: "r1"}, from: []string{Review}, cons: []Card{wk(Done, "ok", h1)},
			expect: append(append([]store.Expect{p1, free}, r1...), gen("p1.w1", 6), store.CardAbsent{Card: "p1.r1.1"}),
			writes: []store.Write{store.PutCard{ID: "p1.r1.1", Kind: "read", Of: "p1", Stream: "s1", Holder: "r1", Tier: "pro", Attempt: 1, Review: 1},
				store.SetCardHead{Card: "p1.r1.1", Head: h1}, store.SetCardAuthor{Card: "p1.r1.1", Author: "r2"}, store.SetCardState{Card: "p1.r1.1", State: Ready}}},
		{event: ReadYes{}, from: []string{Review}, cons: reads, expect: readFence, writes: []store.Write{store.SetCardState{Card: "p1", State: Merging}}},
		{event: ReadFix{Reason: "fix x"}, from: []string{Review}, cons: reads, expect: readFence,
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "fix x"}, store.SetCardBounces{Card: "p1", Bounces: 1}}},
		{event: Landed{Head: h1}, from: []string{Merging}, expect: []store.Expect{p1}, writes: []store.Write{store.SetCardState{Card: "p1", State: LandedState}}},
		{event: Red{Head: h1, Reason: "ci red"}, from: []string{Merging}, expect: []store.Expect{p1},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Review}, store.SetCardReview{Card: "p1"}, store.SetCardReason{Card: "p1", Reason: "ci red"}}},
		{event: Released{}, from: []string{Working}, cons: []Card{{ID: "p1.w1", Kind: "work", Of: "p1", State: Fail, Gen: 6, Attempt: 1, Holder: "r1", Head: h2, Reason: "silent"}},
			expect: []store.Expect{p1, gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Ready}, store.SetCardReason{Card: "p1", Reason: "silent"}, store.SetCardHead{Card: "p1", Head: h2}}},
		{event: Cancel{Reason: "no longer"}, expect: []store.Expect{p1}, writes: []store.Write{store.SetCardState{Card: "p1", State: Fail}, store.SetCardReason{Card: "p1", Reason: "no longer"}}},
		{event: Replace{By: "p9", Reason: "split"}, expect: []store.Expect{p1},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Fail}, store.SetCardReason{Card: "p1", Reason: "replaced by p9: split"}}},
		{event: NeedFailed{Need: "n1"}, need: Fail, expect: []store.Expect{p1, failed},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Fail}, store.SetCardReason{Card: "p1", Reason: "need n1 failed"}}},
		{event: Waive{Need: "n1", Reason: "dropped"}, from: []string{Waiting}, need: Fail, expect: []store.Expect{p1, failed},
			writes: []store.Write{store.SetCardNeeds{Card: "p1", Needs: []string{}}, store.SetCardReason{Card: "p1", Reason: "dropped"}}},
		{event: Relink{Need: "n1", To: "n2", Reason: "moved"}, from: []string{Waiting}, need: Fail, expect: []store.Expect{p1, failed, gen("n2", 9)},
			writes: []store.Write{store.SetCardNeeds{Card: "p1", Needs: []string{"n2"}}, store.SetCardReason{Card: "p1", Reason: "moved"}}},
		{event: Edit{Prose: "new", Paths: []string{"card/"}, Reason: "scope"}, expect: []store.Expect{p1},
			writes: []store.Write{store.SetCardProse{Card: "p1", Prose: "new"}, store.SetCardPaths{Card: "p1", Paths: []string{"card/"}}, store.SetCardReason{Card: "p1", Reason: "scope"}}},
		{event: Rescore{Score: 9}, expect: []store.Expect{p1}, writes: []store.Write{store.SetCardScore{Card: "p1", Score: 9}}},
		{event: SetPriority{Level: "blocker", Reason: "release"}, expect: []store.Expect{p1},
			writes: []store.Write{store.SetCardPriority{Card: "p1", Priority: "blocker"}, store.SetCardReason{Card: "p1", Reason: "release"}}},
		{event: Block{Reason: "bounced"}, from: []string{Ready, Review, Working}, cons: []Card{wk(Done, "fail", h2)}, expect: []store.Expect{p1, gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Blocked}, store.SetCardReason{Card: "p1", Reason: "bounced"}}},
		{event: Unblock{Reason: "reviewed"}, from: []string{Blocked}, expect: []store.Expect{p1},
			writes: []store.Write{store.SetCardState{Card: "p1", State: Waiting}, store.SetCardReason{Card: "p1", Reason: "reviewed"}}},
	}
}

func (c pcase) move(state string) (Plan, *Refusal) {
	p := pr(state)
	if c.kind != "" {
		p.Kind = c.kind
	}
	return Move(p, c.cons, rows(), Stream{Name: "s1", Held: c.held}, needs(cmp(c.need, LandedState)), c.event)
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func TestProducerTable(t *testing.T) {
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
	read := rd(Working, "")
	for _, c := range []ccase{
		{event: Take{Row: "r1"}, from: []string{Ready}, c: wk(Ready, "", ""), expect: []store.Expect{gen("p1.w1", 6), store.RowGen{Row: "r1", Gen: 4}},
			writes: []store.Write{store.SetCardState{Card: "p1.w1", State: Working}}},
		{event: Report{Row: "r1", Result: "ok", Head: h2, Reason: "done", Cost: 12, Priced: true}, from: []string{Working}, c: wk(Working, "", ""),
			expect: []store.Expect{gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1.w1", State: Done}, store.SetCardResult{Card: "p1.w1", Result: "ok"}, store.SetCardHead{Card: "p1.w1", Head: h2},
				store.SetCardReason{Card: "p1.w1", Reason: "done"}, store.SetCardCost{Card: "p1.w1", Cost: 12, Priced: true}}},
		{event: Report{Row: "r1", Result: "ok", Head: h1}, from: []string{Working}, c: read, expect: []store.Expect{gen("p1.r1.1", 2), p1},
			writes: []store.Write{store.SetCardState{Card: "p1.r1.1", State: Done}, store.SetCardResult{Card: "p1.r1.1", Result: "ok"}}},
		{event: Withdraw{Reason: "silent"}, from: []string{Ready, Working}, c: wk(Working, "", ""), expect: []store.Expect{gen("p1.w1", 6)},
			writes: []store.Write{store.SetCardState{Card: "p1.w1", State: Fail}, store.SetCardReason{Card: "p1.w1", Reason: "silent"}}},
	} {
		t.Run(name(c.event)+" "+c.c.Kind, func(t *testing.T) {
			k := c.c
			plan, r := MoveConsumer(k, pr(Review), rows()["r1"], c.event)
			require.Nil(t, r)
			assert.Equal(t, c.expect, plan.Expect)
			assert.Equal(t, c.writes, plan.Writes)
			for _, s := range consumerStates {
				k.State = s
				_, r := MoveConsumer(k, pr(Review), rows()["r1"], c.event)
				if slices.Contains(c.from, s) {
					assert.Nil(t, r, s)
					continue
				}
				want := fmt.Sprintf("consumer %s is in %s, %s needs %s", k.ID, s, name(c.event), orList(c.from))
				if s == Done || s == Fail {
					want = fmt.Sprintf("consumer %s is %s; nothing moves a %s card", k.ID, s, s)
				}
				require.NotNil(t, r, s)
				assert.Equal(t, want, r.Reason, s)
			}
		})
	}
}

func TestRowTable(t *testing.T) {
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
		{Width{N: 3}, up, []store.Write{store.SetRowWidth{Row: "r1", Width: 3}}},
		{Width{N: 3}, down, []store.Write{store.SetRowWidth{Row: "r3", Width: 3}}},
		{Hold{Reason: "maintenance"}, up, []store.Write{store.SetRowHeld{Row: "r1", Held: true, Reason: "maintenance"}}},
		{Hold{}, down, []store.Write{store.SetRowHeld{Row: "r3", Held: true}}},
		{Unhold{Reason: "back"}, held, []store.Write{store.SetRowHeld{Row: "r4", Held: false, Reason: "back"}}},
	} {
		plan, r := MoveRow(c.r, c.event)
		require.Nil(t, r, name(c.event))
		assert.Equal(t, Plan{Expect: fence(c.r), Writes: c.writes}, plan, name(c.event))
	}
	for _, c := range []refusal{
		{rowMove(up, Seen{}), "row r1 is up, Seen needs down"},
		{rowMove(down, Gone{}), "row r3 is down, Gone needs up"},
		{rowMove(up, Width{N: 2}), "row r1 already has width 2"},
		{rowMove(up, Width{N: -1}), "width -1 is below 0"},
		{rowMove(held, Hold{}), "row r4 is already held"},
		{rowMove(up, Unhold{}), "row r1 is already not held"},
		{rowMove(up, Resolve{}), "Resolve is not a row event; MoveRow takes Seen, Gone, Width, Hold and Unhold"},
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
		{mv(Working, live, Finished{Consumer: "p1.w1"}), "consumer p1.w1 is working, not done"},
		{mv(Working, []Card{wk(Done, "fix", h2)}, Finished{Consumer: "p1.w1"}), "consumer p1.w1 reported fix, Finished needs ok"},
		{mv(Working, []Card{wk(Done, "fault", h2)}, Broken{Consumer: "p1.w1"}), "consumer p1.w1 reported fault, Broken needs fail"},
		{mv(Working, []Card{wk(Done, "fail", h2)}, Faulted{Consumer: "p1.w1"}), "consumer p1.w1 reported fail, Faulted needs fault"},
		{mv(Working, live, Released{}), "consumer p1.w1 is working; Released needs it withdrawn (fail)"},
		{mv(Working, nil, Released{}), "card p1.w1 is not producer p1's work consumer of attempt 1"},
		{mv(Review, nil, AskRead{Row: "r2"}), "r2 wrote producer p1's head and may not read it"},
		{mv(Review, []Card{wk(Working, "", "")}, AskRead{Row: "r1"}), liveReason},
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
	relink := func(to string, graph map[string]Card) *Refusal {
		graph["n1"] = Card{ID: "n1", Kind: "producer", State: Fail}
		_, r := Move(pr(Waiting), nil, rows(), Stream{Name: "s1"}, graph, Relink{Need: "n1", To: to})
		return r
	}
	for _, c := range []refusal{
		{relink("n9", map[string]Card{}), "card n9 does not exist"},
		{relink("n2", map[string]Card{"n2": {ID: "n2", Kind: "producer", State: LandedState}}), "card n2 is a landed producer; a need is relinked only to a producer that is not final"},
		{relink("n2", map[string]Card{"n2": {ID: "n2", Kind: "work", State: Ready}}), "card n2 is a ready work; a need is relinked only to a producer that is not final"},
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
	cm := func(c Card, e Event) *Refusal { _, r := MoveConsumer(c, pr(Review), rows()["r1"], e); return r }
	work, read := wk(Working, "", ""), rd(Working, "")
	other := pr(Review)
	other.ID = "p2"
	_, wrongProducer := MoveConsumer(work, other, rows()["r1"], Take{Row: "r1"})
	_, wrongRow := MoveConsumer(wk(Ready, "", ""), pr(Review), rows()["r2"], Take{Row: "r1"})
	for _, c := range []refusal{
		{cm(pr(Ready), Take{Row: "r1"}), "card p1 is a producer; MoveConsumer is for consumers"},
		{wrongProducer, "consumer p1.w1 belongs to p1, not p2"},
		{wrongRow, "row r2 is not the row the take names (r1)"},
		{cm(wk(Ready, "", ""), Take{Row: "r2"}), "consumer p1.w1 is held by r1, not r2"},
		{cm(work, Report{Row: "r2", Result: "ok", Head: h2}), "consumer p1.w1 is held by r1, not r2"},
		{cm(work, Report{Row: "r1", Result: "ok", Head: "abc"}), "head abc is not a full commit sha"},
		{cm(work, Report{Row: "r1", Result: "ok"}), "head is empty; it needs a full commit sha"},
		{cm(work, Report{Row: "r1", Result: "fail", Head: "xyz"}), "head xyz is not a full commit sha"},
		{cm(work, Report{Row: "r1", Result: "ok", Head: base}), "head " + base + " is the base; nothing was done"},
		{cm(work, Report{Row: "r1", Result: "maybe"}), `result "maybe" is not ok, fix, fail or fault`},
		{cm(read, Report{Row: "r1", Result: "ok", Head: h2}), "producer p1 is at head " + h1 + ", the read was of " + h2},
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
	a := Plan{Expect: []store.Expect{p1}, Writes: []store.Write{store.SetCardScore{Card: "p1", Score: 1}}}
	b := Plan{Expect: []store.Expect{gen("p2", 1)}, Writes: []store.Write{store.SetCardScore{Card: "p2", Score: 2}}}
	assert.Equal(t, Plan{Expect: append(a.Expect, b.Expect...), Writes: append(a.Writes, b.Writes...)}, Merge(a, b))
}
