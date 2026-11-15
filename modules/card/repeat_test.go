package card

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The repeated-fail hold and LastBounce (repeat.go): no store, no clock.

// at is p1's work consumer of attempt a in state, with result and reason.
func at(a int, state, result, reason string) Card {
	c := wk(state, result, "")
	c.ID, c.Attempt, c.Reason = fmt.Sprintf("p1.w%d", a), a, reason
	return c
}

// bounced is p1 working at attempt a with one bounce spent since its base.
func bounced(a int) Card {
	p := pr(Working)
	p.Attempt, p.Bounces = a, 1
	return p
}

const repeatX = HeldPrefix + "failed the same way twice running, p1.w1 and p1.w2: merge conflict in x.go"

// TestRepeatedFailHoldsOnTheSecond: Broken of a fail repeating the previous
// bounce's first line leaves p1 waiting held on both attempts, one bounce
// spent, head kept, under a held stream too; the first fail went back to
// ready.
func TestRepeatedFailHoldsOnTheSecond(t *testing.T) {
	t.Parallel()
	prev := at(1, Done, "fail", "merge conflict in x.go\nCONFLICT")
	cur := at(2, Done, "fail", "  merge conflict in x.go  \nsomething else")
	cur.Head = h2
	for _, s := range []Stream{{Name: "s1"}, {Name: "s1", Held: true}} {
		plan, r := Move(bounced(2), []Card{prev, cur}, rows(), s, nil, Broken{Consumer: "p1.w2"})
		require.Nil(t, r)
		assert.Contains(t, plan.Writes, store.Write(store.SetCardState{Card: "p1", State: Waiting}))
		assert.Contains(t, plan.Writes, store.Write(store.SetCardReason{Card: "p1", Reason: repeatX}))
		assert.Contains(t, plan.Writes, store.Write(store.SetCardBounces{Card: "p1", Bounces: 2}), "a repeat is a fail: one bounce")
		assert.Contains(t, plan.Writes, store.Write(store.SetCardHead{Card: "p1", Head: h2}), "the branch's head is kept")
	}
	held := bounced(2)
	held.State, held.Reason, held.Bounces = Waiting, repeatX, 2
	assert.True(t, Held(held), "the deal never takes it")
	assert.Equal(t, "held", TraceWhy(Waiting, repeatX))
	p, c, ok := RepeatHeld(held, []Card{prev, cur})
	require.True(t, ok)
	assert.Equal(t, [2]string{"p1.w1", "p1.w2"}, [2]string{p.ID, c.ID})

	first := at(1, Done, "fail", "merge conflict in x.go")
	plan, r := Move(pr(Working), []Card{first}, rows(), Stream{Name: "s1"}, nil, Broken{Consumer: "p1.w1"})
	require.Nil(t, r)
	assert.Contains(t, plan.Writes, store.Write(store.SetCardState{Card: "p1", State: Ready}), "the first fail is dealt again")
}

// TestRepeatedFailPassesOverNoVerdict: a fault or a withdraw between two
// same fails pushed no verdict on the brief and is passed over.
func TestRepeatedFailPassesOverNoVerdict(t *testing.T) {
	t.Parallel()
	for name, between := range map[string]Card{
		"a fault":    at(2, Done, "fault", "exit 137"),
		"a withdraw": at(2, Fail, "", "silent"),
	} {
		cons := []Card{at(1, Done, "fail", "x"), between, at(3, Done, "fail", "x")}
		prev, ok := RepeatedFail(bounced(3), cons, cons[2])
		require.True(t, ok, name)
		assert.Equal(t, "p1.w1", prev.ID, name)
	}
}

// TestRepeatedFailRefusals: every case that is no repeat goes back to ready
// as a plain bounce (Broken), or is no fail at all.
func TestRepeatedFailRefusals(t *testing.T) {
	t.Parallel()
	fail := func(a int, why string) Card { return at(a, Done, "fail", why) }
	unheld := bounced(2)
	unheld.BounceBase = 1
	spentNone := bounced(2)
	spentNone.Bounces = 0
	other := fail(1, "x")
	other.Of = "p2"
	cases := []struct {
		name string
		p    Card
		cons []Card
	}{
		{"a different first line", bounced(2), []Card{fail(1, "x"), fail(2, "y\nx")}},
		{"the same later lines only", bounced(2), []Card{fail(1, "a\nx"), fail(2, "b\nx")}},
		{"an empty reason", bounced(2), []Card{fail(1, ""), fail(2, "  \n")}},
		{"no earlier attempt", bounced(2), []Card{fail(2, "x")}},
		{"an ok between ends the run", bounced(3), []Card{fail(1, "x"), at(2, Done, "ok", "x"), fail(3, "x")}},
		{"the earlier fail refuted", bounced(2), []Card{fail(1, "refuted: x"), fail(2, "Refuted: x")}},
		{"an unhold since: a fresh budget", unheld, []Card{fail(1, "x"), fail(2, "x")}},
		{"no bounce spent", spentNone, []Card{fail(1, "x"), fail(2, "x")}},
		{"another producer's attempt", bounced(2), []Card{other, fail(2, "x")}},
	}
	for _, tc := range cases {
		cur := tc.cons[len(tc.cons)-1]
		_, ok := RepeatedFail(tc.p, tc.cons, cur)
		assert.False(t, ok, tc.name)
		plan, r := Move(tc.p, tc.cons, rows(), Stream{Name: "s1"}, nil, Broken{Consumer: cur.ID})
		require.Nil(t, r, tc.name)
		if Refuted(cur.Reason) {
			assert.Contains(t, plan.Writes, store.Write(store.SetCardReason{Card: "p1", Reason: HeldPrefix + cur.Reason}), "%s: held by the refutation, not the repeat", tc.name)
			continue
		}
		assert.Contains(t, plan.Writes, store.Write(store.SetCardState{Card: "p1", State: Ready}), tc.name)
	}
	// Not a fail: a fault with the same line spends nothing and holds nothing.
	cons := []Card{fail(1, "x"), at(2, Done, "fault", "x")}
	_, ok := RepeatedFail(bounced(2), cons, cons[1])
	assert.False(t, ok, "a fault is no fail")
	plan, r := Move(bounced(2), cons, rows(), Stream{Name: "s1"}, nil, Faulted{Consumer: "p1.w2"})
	require.Nil(t, r)
	assert.Contains(t, plan.Writes, store.Write(store.SetCardState{Card: "p1", State: Ready}))
	working := at(2, Working, "", "x")
	_, ok = RepeatedFail(bounced(2), []Card{fail(1, "x"), working}, working)
	assert.False(t, ok, "a consumer still working has not failed")
}

// TestRepeatHeldOnlyByTheRepeat: RepeatHeld names the attempts only while
// p1 waits on Broken's repeat reason; a coordinator's hold, a later attempt
// or a reason edited away is not it.
func TestRepeatHeldOnlyByTheRepeat(t *testing.T) {
	t.Parallel()
	cons := []Card{at(1, Done, "fail", "merge conflict in x.go"), at(2, Done, "fail", "merge conflict in x.go")}
	held := bounced(2)
	held.State, held.Reason = Waiting, repeatX
	_, _, ok := RepeatHeld(held, cons)
	assert.True(t, ok)
	for name, p := range map[string]Card{
		"the coordinator's hold": func() Card { p := held; p.Reason = "held: looking"; return p }(),
		"ready, not waiting":     func() Card { p := held; p.State = Ready; return p }(),
		"a later attempt":        func() Card { p := held; p.Attempt = 3; return p }(),
	} {
		_, _, ok := RepeatHeld(p, cons)
		assert.False(t, ok, name)
	}
	_, _, ok = RepeatHeld(held, cons[1:])
	assert.False(t, ok, "no earlier attempt")
}

// TestLastBounce: policy's last_bounce is "fail" after Broken, "fix" after
// a read said fix of the latest ok attempt's review, "" otherwise.
func TestLastBounce(t *testing.T) {
	t.Parallel()
	ready := func(a, review int) Card { p := pr(Ready); p.Attempt, p.Review = a, review; return p }
	fixRead := rd(Done, "fix")
	okRead := rd(Done, "ok")
	cases := []struct {
		name, want string
		p          Card
		cons       []Card
	}{
		{"no attempt yet", "", ready(0, 0), nil},
		{"a fail", "fail", ready(1, 0), []Card{at(1, Done, "fail", "x")}},
		{"a refuted fail", "fail", ready(1, 0), []Card{at(1, Done, "fail", "refuted: x")}},
		{"a read said fix", "fix", ready(1, 1), []Card{at(1, Done, "ok", ""), fixRead}},
		{"a fault after a fix passes over", "fix", ready(2, 1), []Card{at(1, Done, "ok", ""), fixRead, at(2, Done, "fault", "exit 137")}},
		{"dealt again after a fix", "fix", func() Card { p := ready(2, 1); p.State = Working; return p }(), []Card{at(1, Done, "ok", ""), fixRead, at(2, Working, "", "")}},
		{"a fail after a fix", "fail", ready(2, 1), []Card{at(1, Done, "ok", ""), fixRead, at(2, Done, "fail", "x")}},
		{"a Red: the review rose", "", ready(1, 2), []Card{at(1, Done, "ok", ""), fixRead}},
		{"in review, fix read not yet moved", "", func() Card { p := ready(1, 1); p.State = Review; return p }(), []Card{at(1, Done, "ok", ""), fixRead}},
		{"ok reads only", "", ready(1, 1), []Card{at(1, Done, "ok", ""), okRead}},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, LastBounce(tc.p, tc.cons), tc.name)
	}
}
