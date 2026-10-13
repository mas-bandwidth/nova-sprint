package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reader's P1 on #206 (Stella, 2026-10-13): a hold on a doctor check whose failing
// lines name a global subject (identity, a provider, hook, judgments, all) was released
// while the check still failed, because DoctorPasses kept the hold only for a failing line
// naming the member or all. Each case below holds m1 on a check, runs the check's own
// generator failing on its global subject, and expects no pass and the hold kept; then runs
// it passing and expects the pass and the release (hold_cause.go doctorFailKeeps;
// tla/HoldCause.tla Doctor, DoctorPassIsMeasured). Every time is passed in (w.tick).

// fullMemberProbe is m's host answering the probe in full, every tool present, the push
// credential as push says.
func fullMemberProbe(w *world, m string, push bool) MemberProbe {
	tools := map[string]string{}
	for _, tool := range append(append([]string{}, MemberTools...), MemberHarnesses(w.s, m)...) {
		tools[tool] = "/usr/bin/" + tool
	}
	return MemberProbe{Member: m, Host: m, Reached: true, Complete: true, Tools: tools, FreeKB: -1, PushOK: push, Push: "no nova-push-credential", SecretOK: true}
}

// liveHook is a hook proven two minutes ago, live for eight more.
func liveHook(now time.Time) HookM {
	return HookM{Measured: true, Holder: "rowan", Record: HookRecord{Name: "rowan", Session: "sess-1", State: HookHooked, Proven: now.Add(-2 * time.Minute), Live: now.Add(8 * time.Minute)}, Recorded: true}
}

func TestADoctorHoldStaysWhileTheChecksGlobalLineFails(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, check, subject string
		failing, passing     func(w *world) ([]DoctorCheck, DoctorInput)
	}{
		{
			name: "git-identity: the identity line", check: CheckGitIdentity, subject: "identity",
			failing: func(w *world) ([]DoctorCheck, DoctorInput) {
				w.s.GitIdentity = "not an identity"
				in := DoctorInput{Snap: w.s}
				return DoctorGitIdentity(in), in
			},
			passing: func(w *world) ([]DoctorCheck, DoctorInput) {
				w.s.GitIdentity = "Ops <ops@example.invalid>"
				in := DoctorInput{Snap: w.s}
				return DoctorGitIdentity(in), in
			},
		},
		{
			name: "providers: the provider's line", check: CheckProviders, subject: "p",
			failing: func(w *world) ([]DoctorCheck, DoctorInput) {
				w.s.Routes = []Route{{Name: "pro-p", Tier: "pro", Provider: "p", Model: "a", Enabled: true, Deadline: 600}}
				w.s.Fleet.SetProp(PropProviderRest("p"), RouteRest{At: w.s.Now.Add(-time.Hour), Until: OpenUntil, Cards: []string{"s1-9.w3"}, Cause: RestCredit, Why: "class=out-of-credit status=402"}.value())
				in := DoctorInput{Snap: w.s}
				return DoctorProviders(in), in
			},
			passing: func(w *world) ([]DoctorCheck, DoctorInput) {
				w.s.Routes = []Route{{Name: "pro-p", Tier: "pro", Provider: "p", Model: "a", Enabled: true, Deadline: 600}}
				// the payment landed: the rest ended an hour ago
				w.s.Fleet.SetProp(PropProviderRest("p"), RouteRest{At: w.s.Now.Add(-3 * time.Hour), Until: w.s.Now.Add(-time.Hour), Cards: []string{"s1-9.w3"}, Cause: RestCredit, Why: "class=out-of-credit status=402"}.value())
				in := DoctorInput{Snap: w.s}
				return DoctorProviders(in), in
			},
		},
		{
			name: "seat: the judgments line", check: CheckSeat, subject: "judgments",
			failing: func(w *world) ([]DoctorCheck, DoctorInput) {
				in := DoctorInput{Snap: w.s, Hook: liveHook(w.s.Now), Seat: SeatWaits{Judgments: 54, Overdue: 50}}
				return DoctorSeat(in), in
			},
			passing: func(w *world) ([]DoctorCheck, DoctorInput) {
				in := DoctorInput{Snap: w.s, Hook: liveHook(w.s.Now), Seat: SeatWaits{Judgments: 4}}
				return DoctorSeat(in), in
			},
		},
		{
			name: "seat: the hook line", check: CheckSeat, subject: "hook",
			failing: func(w *world) ([]DoctorCheck, DoctorInput) {
				h := liveHook(w.s.Now)
				h.Record.Proven, h.Record.Live = w.s.Now.Add(-time.Hour), w.s.Now.Add(-55*time.Minute)
				in := DoctorInput{Snap: w.s, Hook: h}
				return DoctorSeat(in), in
			},
			passing: func(w *world) ([]DoctorCheck, DoctorInput) {
				in := DoctorInput{Snap: w.s, Hook: liveHook(w.s.Now)}
				return DoctorSeat(in), in
			},
		},
		{
			name: "member-env: not measured at all", check: CheckMemberEnv, subject: "all",
			failing: func(w *world) ([]DoctorCheck, DoctorInput) {
				in := DoctorInput{Snap: w.s, ProbeErr: "probe not configured"}
				return DoctorMemberEnv(in), in
			},
			passing: func(w *world) ([]DoctorCheck, DoctorInput) {
				in := DoctorInput{Snap: w.s, Probes: map[string]MemberProbe{"m1": fullMemberProbe(w, "m1", true)}}
				return DoctorMemberEnv(in), in
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			w.s.Epoch = 3
			w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "doctor FAIL " + c.check, Cause: CausePrefixDoctor + c.check, Who: "rowan"}))

			// the check fails on its global subject: no pass, and the tick keeps the hold
			checks, in := c.failing(w)
			named := false
			for _, x := range checks {
				named = named || x.Subject == c.subject && (x.Status == DoctorFail || strings.HasPrefix(x.Evidence, "not measured"))
			}
			require.True(t, named, "the generator did not fail on %s: the test proves nothing: %+v", c.subject, checks)
			passes := DoctorPasses(in, checks)
			require.Empty(t, passes, "the %s check failed on %s and passed m1 anyway", c.check, c.subject)
			w.tick(time.Minute)
			w.must(DoctorPassPlan(w.s, passes, "doctor"))
			w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
			require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "released while the %s check fails on %s", c.check, c.subject)

			// the check passes: the pass is recorded and the tick releases
			checks, in = c.passing(w)
			passes = DoctorPasses(in, checks)
			require.Equal(t, []DoctorPass{{Member: "m1", Check: c.check}}, passes, "the passing check passed nothing: %+v", checks)
			w.tick(time.Second)
			w.must(DoctorPassPlan(w.s, passes, "doctor"))
			w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
			ctl := w.s.MemberCtl("m1")
			assert.Empty(t, ctl.F("held"), "the hold outlived its check: %v", ctl.Fields)
			require.Len(t, w.notesOf(NHoldReleased), 1, "the release was not logged")
		})
	}
}

// A failing line that names another member is that member's alone: m2's member-env
// failure does not keep m1's hold on the same check, and m2 is passed by nothing.
func TestAnotherMembersFailingLineKeepsNoHold(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Epoch = 3
	w.must(HoldNames(w.s, HoldReq{Names: []string{"m1", "m2"}, Reason: "doctor FAIL member-env", Cause: CausePrefixDoctor + CheckMemberEnv, Who: "rowan"}))
	in := DoctorInput{Snap: w.s, Probes: map[string]MemberProbe{"m1": fullMemberProbe(w, "m1", true), "m2": fullMemberProbe(w, "m2", false)}}
	checks := DoctorMemberEnv(in)
	named := false
	for _, x := range checks {
		named = named || x.Subject == "m2" && x.Status == DoctorFail
	}
	require.True(t, named, "m2 did not fail: the test proves nothing: %+v", checks)
	assert.Equal(t, []DoctorPass{{Member: "m1", Check: CheckMemberEnv}}, DoctorPasses(in, checks))
}
