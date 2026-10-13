package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A coordinator's hold carries its cause, and the tick ends the hold in the step that
// sees the cause end (hold_cause.go; tla/HoldCause.tla NoHoldOutlivesItsCause). Every
// time is passed in (w.tick), and each case runs at epoch 0, where a stored id is the
// plain id, and at an epoch past it, where it is id~epoch (StoredID).

// seedJudgment writes one open judgment on s1-1, as the store would, and returns its id.
func seedJudgment(w *world) string {
	n := judgment(NFleetFailure, "", w.s.Now, 0, "s1-1")
	n.SprintLevel, n.What = true, "failure \"no-tokens\": the same failure on 3 cards"
	w.note(n)
	return w.notes[len(w.notes)-1].ID
}

func TestAHoldOnAJudgmentEndsInTheTickThatSeesItAnswered(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := setup(t, 1)
		w.s.Epoch = epoch
		id := seedJudgment(w)
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "three idle-reaps in 15 minutes", Cause: "j100", CauseNote: id, Who: "rowan"}))
		ctl := w.s.MemberCtl("m1")
		require.NotEmpty(t, ctl.F("held"), "not held: %v", ctl.Fields)
		require.Equal(t, "j100", ctl.F(FieldHeldCause), "the cause was not kept: %v", ctl.Fields)
		require.Equal(t, id, ctl.F(FieldHeldCauseNote), "the cause's judgment id was not kept: %v", ctl.Fields)

		// the cause is open: the tick keeps the hold
		w.tick(time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "released while its cause is open")

		// the seat answers j100: the very next tick ends the hold and tells the seat
		w.closeAll(w.openOn(SprintSubject))
		w.tick(time.Second)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl = w.s.MemberCtl("m1")
		assert.Empty(t, ctl.F("held"), "the hold outlived its cause: %v", ctl.Fields)
		assert.Empty(t, ctl.F(FieldHeldCause), "the cause stayed behind: %v", ctl.Fields)
		assert.Equal(t, Up, ctl.F("status"), "m1 beats and is not up in that tick: %v", ctl.Fields)
		released := w.notesOf(NHoldReleased)
		require.Len(t, released, 1, "the release was not logged")
		assert.Contains(t, released[0].What, "released: cause j100 resolved")
		assert.Equal(t, w.s.Coordinator, released[0].To, "the release is not pushed to the seat")
	})
}

func TestAHoldOnADoctorCheckEndsWhenTheCheckPassesOnThatMember(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := setup(t, 1)
		w.s.Epoch = epoch
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "doctor FAIL member-env: no nova-push-credential", Cause: "doctor:member-env", Who: "rowan"}))
		require.Equal(t, "doctor:member-env", w.s.MemberCtl("m1").F(FieldHeldCause))

		probe := func(push bool) map[string]MemberProbe {
			tools := map[string]string{}
			for _, tool := range append(append([]string{}, MemberTools...), MemberHarnesses(w.s, "m1")...) {
				tools[tool] = "/usr/bin/" + tool
			}
			return map[string]MemberProbe{"m1": {Member: "m1", Host: "m1", Reached: true, Complete: true, Tools: tools, FreeKB: -1, PushOK: push, Push: "no nova-push-credential", SecretOK: true}}
		}
		run := func(push bool) []DoctorPass {
			in := DoctorInput{Snap: w.s, Probes: probe(push)}
			return DoctorPasses(in, DoctorMemberEnv(in))
		}

		// the doctor still fails it: nothing passes, and the tick keeps the hold
		require.Empty(t, run(false), "a failing check passed")
		w.tick(time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "released while the check fails")

		// the credential is in place: the doctor's pass is recorded, and the tick releases
		passes := run(true)
		require.Equal(t, []DoctorPass{{Member: "m1", Check: CheckMemberEnv}}, passes)
		w.tick(time.Second)
		w.must(DoctorPassPlan(w.s, passes, "doctor"))
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl := w.s.MemberCtl("m1")
		assert.Empty(t, ctl.F("held"), "the hold outlived its check: %v", ctl.Fields)
		assert.Equal(t, Up, ctl.F("status"), "m1 beats and is not up: %v", ctl.Fields)
		released := w.notesOf(NHoldReleased)
		require.Len(t, released, 1)
		assert.Contains(t, released[0].What, "released: cause doctor:member-env resolved")
	})
}

// A pass recorded before the hold is no pass of it: only the doctor's run after the hold
// releases it.
func TestADoctorPassBeforeTheHoldReleasesNothing(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Epoch = 2
	w.s.MemberCtl("m1").Fields[FieldDoctorPassed] = CheckMemberEnv + "=" + stamp(w.s.Now.Add(-time.Hour))
	w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "doctor FAIL", Cause: "doctor:member-env", Who: "rowan"}))
	w.tick(time.Minute)
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	assert.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "an old pass released a new hold")
}

func TestAHoldCauseIsRefusedWhenItCannotEnd(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	for _, c := range []struct {
		req  HoldReq
		want string
	}{
		{HoldReq{Names: []string{"m1"}, Reason: "x", Cause: "j7", CauseNote: "n-gone", Who: "rowan"}, "no open judgment"},
		{HoldReq{Names: []string{"m1"}, Reason: "x", Cause: "doctor:no-such-check", Who: "rowan"}, "no doctor check"},
		{HoldReq{Names: []string{"s1"}, Reason: "x", Cause: "doctor:member-env", Who: "rowan"}, "a cause names a fleet member's hold"},
	} {
		p := HoldNames(w.s, c.req)
		require.Len(t, p.Refused, 1, "%+v was not refused", c.req)
		assert.Contains(t, p.Refused[0].Why, c.want)
	}
}

// where says each held member's cause, its age and the verb that releases it.
func TestTheHoldLineSaysCauseAgeAndRelease(t *testing.T) {
	t.Parallel()
	now := t0
	h := HoldView{Kind: HoldMember, Name: "mini-m5", Reason: "three idle-reaps", Cause: "j100", Who: "rowan", At: stamp(now.Add(-10*time.Hour - 5*time.Minute))}
	line := HoldLine(h, now)
	for _, want := range []string{"mini-m5", "10h5m", "by rowan", "cause j100", "three idle-reaps", "release: nova-sprint unhold mini-m5"} {
		assert.Contains(t, line, want)
	}
	h.Cause = ""
	assert.True(t, strings.Contains(HoldLine(h, now), "no cause"), "a cause-less hold does not say so: %s", HoldLine(h, now))
}
