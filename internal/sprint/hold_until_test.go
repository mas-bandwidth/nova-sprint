package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fleet member held with --for <duration> or --until <RFC3339> carries held_until on
// its control card (ctl-<member>), stays held while s.Now is before held_until, and is
// released by the tick in the first step whose s.Now is at or after held_until: its
// status becomes down (when it has no fresh beat) or up (when its beat is alive), every
// holdFields key is unset, and a hold_released note records that the hold ran its time.
// When both a cause and a release time are set, whichever condition is met first
// releases the hold once.
func TestAMemberHoldReleasesItselfAtItsTime(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		// 1. --for 30m, released with no fresh beat -> down
		w := setup(t, 1)
		w.s.Epoch = epoch
		start := w.s.Now.UTC().Truncate(time.Second)
		wantUntil := start.Add(30 * time.Minute).Format(time.RFC3339)
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "cooling fan swap", For: 30 * time.Minute, Who: "rowan"}))
		ctl := w.s.MemberCtl("m1")
		require.NotEmpty(t, ctl.F("held"))
		require.Equal(t, "cooling fan swap", ctl.F(FieldHeldReason))
		require.Equal(t, wantUntil, ctl.F(FieldHeldUntil), "held_until was not written on ctl-m1: %v", ctl.Fields)
		require.Equal(t, Held, PresenceStatus(ctl.F("held") != "", Beat{}, time.Time{}))

		// 29m59s: still before held_until, so the hold stands
		w.tick(29*time.Minute + 59*time.Second)
		w.part(TickDeal, TickReq{})
		ctl = w.s.MemberCtl("m1")
		require.NotEmpty(t, ctl.F("held"), "released 1s before held_until")
		require.Equal(t, wantUntil, ctl.F(FieldHeldUntil))
		require.Empty(t, w.notesOf(NHoldReleased))

		// 30m00s: at held_until with no beat -> released to down, all holdFields cleared
		w.tick(time.Second)
		w.part(TickDeal, TickReq{})
		ctl = w.s.MemberCtl("m1")
		assert.Equal(t, Down, ctl.F("status"), "m1 has no beat so it releases to down: %v", ctl.Fields)
		for _, f := range append(append([]string(nil), holdFields...), FieldHeldUntil) {
			assert.Empty(t, ctl.F(f), "field %s stayed behind after release: %v", f, ctl.Fields)
		}
		released := w.notesOf(NHoldReleased)
		require.Len(t, released, 1, "expected one hold_released note")
		assert.Contains(t, released[0].What, "m1 released: hold ran its time (until "+wantUntil+"); it is down until it beats")
		assert.Equal(t, w.s.Coordinator, released[0].To)

		// 2. --until <RFC3339>, released with a fresh beat -> up
		untilTime := w.s.Now.UTC().Truncate(time.Second).Add(15 * time.Minute)
		untilText := untilTime.Format(time.RFC3339)
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "kernel reboot", Until: untilText, Who: "rowan"}))
		ctl = w.s.MemberCtl("m1")
		require.Equal(t, untilText, ctl.F(FieldHeldUntil))

		w.tick(14*time.Minute + 59*time.Second)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "released before --until")

		w.tick(time.Second)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl = w.s.MemberCtl("m1")
		assert.Equal(t, Up, ctl.F("status"), "m1 beats so it releases to up: %v", ctl.Fields)
		assert.Empty(t, ctl.F("held"))
		assert.Empty(t, ctl.F(FieldHeldUntil))
		released = w.notesOf(NHoldReleased)
		require.Len(t, released, 2)
		assert.Contains(t, released[1].What, "m1 released: hold ran its time (until "+untilText+"); it is up")

		// 3. Both --cause and --for: whichever is met first releases once and clears both
		id := seedJudgment(w)
		bothUntil := w.s.Now.UTC().Truncate(time.Second).Add(10 * time.Minute).Format(time.RFC3339)
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "investigating j100", Cause: "j100", CauseNote: id, For: 10 * time.Minute, Who: "rowan"}))
		ctl = w.s.MemberCtl("m1")
		require.Equal(t, bothUntil, ctl.F(FieldHeldUntil))
		require.Equal(t, "j100", ctl.F(FieldHeldCause))

		// time expires while j100 is still open: released by time, and closing j100 later does not release again
		w.tick(10 * time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl = w.s.MemberCtl("m1")
		assert.Empty(t, ctl.F("held"))
		assert.Empty(t, ctl.F(FieldHeldUntil))
		assert.Empty(t, ctl.F(FieldHeldCause))
		require.Len(t, w.notesOf(NHoldReleased), 3)

		w.closeAll(w.openOn(SprintSubject))
		w.tick(time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		require.Len(t, w.notesOf(NHoldReleased), 3, "a hold already released by time must not release again when its cause closes")
	})
}

// An indefinite member hold (--indefinite) writes no held_until and is never released by
// the tick, however many hours pass.
func TestAnIndefiniteHoldIsNeverReleasedByTheTick(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := setup(t, 1)
		w.s.Epoch = epoch
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "psu replacement on order", Indefinite: true, RequireEnd: true, Who: "rowan"}))
		ctl := w.s.MemberCtl("m1")
		require.NotEmpty(t, ctl.F("held"))
		require.Equal(t, "psu replacement on order", ctl.F(FieldHeldReason))
		require.Empty(t, ctl.F(FieldHeldUntil), "an indefinite hold must write no held_until")

		w.tick(24 * time.Hour)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl = w.s.MemberCtl("m1")
		assert.NotEmpty(t, ctl.F("held"), "the tick released an indefinite hold")
		assert.Equal(t, "psu replacement on order", ctl.F(FieldHeldReason))
		assert.Equal(t, Held, PresenceStatus(ctl.F("held") != "", Beat{}, time.Time{}))
		assert.Empty(t, ctl.F(FieldHeldUntil))
		assert.Empty(t, w.notesOf(NHoldReleased), "an indefinite hold must never emit hold_released from the tick")
	})
}

// --extend <duration> adds the duration to the member's current held_until (never from
// s.Now, never shortening it), leaving all other hold fields intact.
func TestExtendPushesTheTimeOut(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := setup(t, 1)
		w.s.Epoch = epoch
		start := w.s.Now.UTC().Truncate(time.Second)
		firstUntil := start.Add(30 * time.Minute).Format(time.RFC3339)
		extendedUntil := start.Add(45 * time.Minute).Format(time.RFC3339)

		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "memtest pass", For: 30 * time.Minute, Who: "rowan"}))
		require.Equal(t, firstUntil, w.s.MemberCtl("m1").F(FieldHeldUntil))

		// 10m in, extend by 15m -> held_until moves from +30m to +45m (not +25m from now)
		w.tick(10 * time.Minute)
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Extend: 15 * time.Minute, Who: "rowan"}))
		ctl := w.s.MemberCtl("m1")
		require.Equal(t, extendedUntil, ctl.F(FieldHeldUntil), "extend must add to the existing held_until")
		require.NotEmpty(t, ctl.F("held"))
		require.Equal(t, "memtest pass", ctl.F(FieldHeldReason), "extend must preserve the hold reason")
		require.Equal(t, "rowan", ctl.F(FieldHeldWho), "extend must preserve held_who")

		// at 30m00s from start (+20m more): still held because extend pushed it to 45m00s
		w.tick(20 * time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "released at old held_until after --extend")
		require.Empty(t, w.notesOf(NHoldReleased))

		// at 45m00s from start (+15m more): released
		w.tick(15 * time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl = w.s.MemberCtl("m1")
		assert.Empty(t, ctl.F("held"), "not released at extended held_until")
		assert.Empty(t, ctl.F(FieldHeldUntil))
		assert.Equal(t, Up, ctl.F("status"))
		released := w.notesOf(NHoldReleased)
		require.Len(t, released, 1)
		assert.Contains(t, released[0].What, "until "+extendedUntil)
	})
}
