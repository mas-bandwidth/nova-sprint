package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithPeggedBound(t *testing.T) {
	t.Parallel()

	t.Run("empty raw leaves plan untouched and non-empty refusal passes through", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		base := Plan{Units: []Unit{{Key: "set", Moved: "sprint max_inflight=4"}}}
		assert.Equal(t, base, WithPeggedBound(base, w.s, ""))

		refused := Plan{Refused: []Refusal{{Key: "set", Why: "bad flag"}}}
		assert.Equal(t, refused, WithPeggedBound(refused, w.s, "5m"))
	})

	t.Run("sets positive duration alone or alongside another sprint setting, and resets to default", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)

		// standalone --pegged-bound clears a "nothing to set" refusal from Set
		nothing := Plan{Refused: []Refusal{{Key: "set", Why: "nothing to set"}}}
		p := WithPeggedBound(nothing, w.s, "6m")
		w.must(p)
		require.Len(t, p.Units, 1)
		assert.Equal(t, "sprint pegged-bound 6m", p.Units[0].Moved)
		assert.Equal(t, 6*time.Minute, w.s.PeggedBound())

		// combined with an existing "sprint" or "sprint ..." set unit
		bare := Plan{Units: []Unit{{Key: "set", Moved: "sprint"}}}
		p2 := WithPeggedBound(bare, w.s, "8m")
		w.must(p2)
		assert.Equal(t, "sprint pegged-bound 8m", p2.Units[0].Moved)
		assert.Equal(t, 8*time.Minute, w.s.PeggedBound())

		existing := Plan{Units: []Unit{{Key: "set", Moved: "sprint max_inflight=2"}}}
		p3 := WithPeggedBound(existing, w.s, "default")
		w.must(p3)
		assert.Equal(t, "sprint max_inflight=2, pegged-bound default (10m0s)", p3.Units[0].Moved)
		assert.Equal(t, PeggedBoundDefault, w.s.PeggedBound())
	})

	t.Run("refuses non-positive or malformed duration", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		for _, bad := range []string{"0s", "-5m", "not-a-duration"} {
			p := WithPeggedBound(Plan{}, w.s, bad)
			require.Len(t, p.Refused, 1, "input=%q", bad)
			assert.Contains(t, p.Refused[0].Why, "--pegged-bound wants a duration above zero")
		}
	})
}

func TestTickPeggedLifecycleAndPeggedEnd(t *testing.T) {
	t.Parallel()

	t.Run("stamps pegged_since, raises judgment at bound, keeps episode across bound raise, and clears on each end", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		// nil Beats is a no-op
		pNil, dueNil := tickPegged(w.s, TickReq{})
		assert.Empty(t, pNil.Units)
		assert.Zero(t, dueNil)

		beatAt := func(load float64) TickReq {
			return TickReq{Beats: map[string]Beat{"m1": {At: w.s.Now, Load: load}}}
		}

		// first tick stamps PropPeggedSince("m1") without raising NMemberPegged
		tStart := w.s.Now
		p, _ := tickPegged(w.s, beatAt(95))
		w.must(p)
		since, ok := w.s.Fleet.Prop(PropPeggedSince("m1"))
		require.True(t, ok)
		assert.Equal(t, stamp(tStart), since)
		assert.False(t, peggedJudged(w.s, "m1"))
		assert.Equal(t, "", PeggedEnd(w.s, "m1", Beat{At: w.s.Now, Load: 95}))

		// advance past PeggedBoundDefault (10m): raises NMemberPegged with peggedWhat and peggedDecisions
		w.tick(PeggedBoundDefault + time.Minute)
		p, _ = tickPegged(w.s, beatAt(95))
		w.must(p)
		assert.True(t, peggedJudged(w.s, "m1"))
		open := openOf(w, NMemberPegged, StreamSubject(MemberSubject("m1")))
		require.Len(t, open, 1)
		assert.Equal(t, peggedWhat("m1", 95, 11*time.Minute, tStart), open[0].Note.What)
		assert.Equal(t, peggedDecisions("m1"), open[0].Note.Decisions)

		// raising PeggedBound above elapsed time while judgment is open (or acked) keeps episode standing
		w.must(WithPeggedBound(Plan{}, w.s, "30m"))
		w.s.Acked = append(w.s.Acked, open...)
		w.s.Open = nil
		assert.True(t, peggedJudged(w.s, "m1"))
		p, _ = tickPegged(w.s, beatAt(95))
		w.must(p)
		afterVal, stillThere := w.s.Fleet.Prop(PropPeggedSince("m1"))
		assert.True(t, stillThere)
		assert.Equal(t, stamp(tStart), afterVal, "acknowledged episode stays open across bound increase")

		// restore to Open so closing emits NAlarmCleared when load drops
		w.s.Open = w.s.Acked
		w.s.Acked = nil
		lowBeat := Beat{At: w.s.Now, Load: 70}
		assert.Equal(t, "m1 beats at 70.0%, under 90%", PeggedEnd(w.s, "m1", lowBeat))
		p, _ = tickPegged(w.s, beatAt(70))
		w.must(p)
		assert.False(t, peggedJudged(w.s, "m1"))
		afterVal, _ = w.s.Fleet.Prop(PropPeggedSince("m1"))
		assert.Empty(t, afterVal)
	})

	t.Run("PeggedEnd names missing member, held, down, working cards, and low load", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		hot := Beat{At: w.s.Now, Load: 95}

		// not on fleet table
		assert.Equal(t, "no-such-member is not on the fleet table", PeggedEnd(w.s, "no-such-member", hot))

		// held
		ctl := w.s.MemberCtl("m1")
		require.NotNil(t, ctl)
		ctl.Fields["held"] = "maintenance"
		assert.Equal(t, "m1 is held", PeggedEnd(w.s, "m1", hot))
		delete(ctl.Fields, "held")

		// down
		ctl.Fields["status"] = Down
		assert.Equal(t, "m1 is down", PeggedEnd(w.s, "m1", hot))
		ctl.Fields["status"] = Up

		// working on a sprint card
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		wc := w.s.Fleet.Card("s1-1.w1")
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
		assert.Equal(t, "m1 works 1 cards", PeggedEnd(w.s, "m1", hot))
	})
}
