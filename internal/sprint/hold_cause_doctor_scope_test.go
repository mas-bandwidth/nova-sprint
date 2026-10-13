package sprint

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Stella's second read of #206 (b4315f8f, 2026-10-13): a doctor line's scope and measurement
// are the generator's own fields (DoctorCheck.Member, DoctorCheck.Unmeasured), never read
// off the subject's spelling or the evidence text (doctorFailKeeps). A provider named like
// another member was read as that member's private failure and released the held one; the
// loops-redial "server's start was not measured" WARN was read as a pass. Every generator's
// actual output is classified below. Every time is passed in (w.tick).

// memberScopedChecks is every check whose lines name a fleet member (Member set on those
// lines): the doctor runs them per member. Every other check's lines are global.
var memberScopedChecks = []string{CheckMemberEnv, CheckFleetRows, CheckFinishFailures, CheckDraining, CheckLoopsRedial}

// A provider named m2 fails for credit: its line is the provider's, global, and keeps m1's
// hold on doctor:providers; the spelling of the subject decides nothing.
func TestAProviderNamedLikeAMemberKeepsTheHold(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Epoch = 3
	w.s.Routes = []Route{{Name: "pro-m2", Tier: "pro", Provider: "m2", Model: "a", Enabled: true, Deadline: 600}}
	w.s.Fleet.SetProp(PropProviderRest("m2"), RouteRest{At: w.s.Now.Add(-time.Hour), Until: OpenUntil, Cards: []string{"s1-9.w3"}, Cause: RestCredit, Why: "class=out-of-credit status=402"}.value())
	w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "doctor FAIL providers", Cause: CausePrefixDoctor + CheckProviders, Who: "rowan"}))
	in := DoctorInput{Snap: w.s}
	checks := DoctorProviders(in)
	fails := failsOn(checks, CheckProviders)
	require.Len(t, fails, 1, lines(checks))
	require.Equal(t, "m2", fails[0].Subject, "the provider's line does not name m2: the test proves nothing")
	require.Contains(t, w.s.Members(), "m2", "m2 is not a member: the test proves nothing")
	assert.Empty(t, fails[0].Member, "a provider's line is a member's")
	assert.Empty(t, DoctorPasses(in, checks), "a provider named m2 failed and m1 passed anyway")
	w.tick(time.Minute)
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "released while the provider fails")
}

// A member named identity: the git-identity check's identity line is global and keeps its
// hold on doctor:git-identity; its hold on doctor:member-env is released only by its own
// member-env line, and m1's member-env failure is m1's alone.
func TestAMemberNamedIdentityReleasesOnlyOnItsOwnLine(t *testing.T) {
	t.Parallel()
	// a world with a third member named identity
	identityWorld := func() *world {
		w := setup(t, 1)
		w.s.Epoch = 3
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "identity", Width: 4}))
		require.Contains(t, w.s.Members(), "identity")
		return w
	}
	w := identityWorld()

	// git-identity: the identity line fails, and the member named identity stays held
	w.must(HoldNames(w.s, HoldReq{Names: []string{"identity"}, Reason: "doctor FAIL git-identity", Cause: CausePrefixDoctor + CheckGitIdentity, Who: "rowan"}))
	w.s.GitIdentity = "not an identity"
	in := DoctorInput{Snap: w.s}
	checks := DoctorGitIdentity(in)
	require.Equal(t, "identity", checks[0].Subject)
	require.Equal(t, DoctorFail, checks[0].Status)
	assert.Empty(t, checks[0].Member, "the identity line is a member's")
	assert.Empty(t, DoctorPasses(in, checks), "the identity line failed and the member named identity passed anyway")
	w = identityWorld()

	// member-env: m1's failing line is m1's; identity's own line passes it
	w.must(HoldNames(w.s, HoldReq{Names: []string{"identity", "m1"}, Reason: "doctor FAIL member-env", Cause: CausePrefixDoctor + CheckMemberEnv, Who: "rowan"}))
	in = DoctorInput{Snap: w.s, Probes: map[string]MemberProbe{"identity": fullMemberProbe(w, "identity", true), "m1": fullMemberProbe(w, "m1", false)}}
	checks = DoctorMemberEnv(in)
	fails := failsOn(checks, CheckMemberEnv)
	require.Len(t, fails, 1, lines(checks))
	assert.Equal(t, "m1", fails[0].Member)
	assert.Equal(t, []DoctorPass{{Member: "identity", Check: CheckMemberEnv}}, DoctorPasses(in, checks))
}

// The loops-redial check with the server's start not measured says so (WARN all) and passes
// no one; once measured, with every loop dialled again, the held member passes and the tick
// releases.
func TestAnUnmeasuredLoopsRedialKeepsTheHold(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Epoch = 3
	w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "doctor FAIL loops-redial", Cause: CausePrefixDoctor + CheckLoopsRedial, Who: "rowan"}))

	in := DoctorInput{Snap: w.s, ServerHow: "no server address"}
	checks := DoctorLoopsRedial(in)
	require.Len(t, checks, 1, lines(checks))
	require.Equal(t, DoctorWarn, checks[0].Status)
	require.Contains(t, checks[0].Evidence, "was not measured", "the generator did not say unmeasured: the test proves nothing")
	assert.True(t, checks[0].Unmeasured, "the line reports no measurement and does not say so")
	passes := DoctorPasses(in, checks)
	require.Empty(t, passes, "an unmeasured loops-redial passed m1")
	w.tick(time.Minute)
	w.must(DoctorPassPlan(w.s, passes, "doctor"))
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "released on no measurement")

	// measured: the server started two hours ago and every loop beat since
	in = DoctorInput{Snap: w.s, ServerStarted: w.s.Now.Add(-2 * time.Hour), ServerHow: "test", Req: TickReq{Beats: freshBeats(w, "m1", "m2")}}
	checks = DoctorLoopsRedial(in)
	require.Empty(t, failsOn(checks, CheckLoopsRedial), lines(checks))
	for _, c := range checks {
		assert.False(t, c.Unmeasured, "a measured line says unmeasured: %s", c.Line())
	}
	passes = DoctorPasses(in, checks)
	require.Equal(t, []DoctorPass{{Member: "m1", Check: CheckLoopsRedial}}, passes, lines(checks))
	w.tick(time.Second)
	w.must(DoctorPassPlan(w.s, passes, "doctor"))
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	assert.Empty(t, w.s.MemberCtl("m1").F("held"), "the hold outlived its measured pass")
}

// Every generator's actual output, classified: a line's Member is set exactly when its check
// is one the doctor runs per member and the line is that member's (never read off the
// subject: a global line of a member-scoped check carries none); a line reports no
// measurement (Unmeasured) exactly on the inputs that measure nothing, and never on a healthy
// run. Each generator runs on the healthy input and, where it has one, on its unmeasured
// input.
func TestEveryDoctorGeneratorsOutputIsClassified(t *testing.T) {
	t.Parallel()
	type gen struct {
		check      string
		run        func(DoctorInput) []DoctorCheck
		unmeasured func(w *world) DoctorInput // nil: the check always measures
	}
	gens := []gen{
		{CheckReadyNotDealt, DoctorReadyNotDealt, func(w *world) DoctorInput { return DoctorInput{} }},
		{CheckRoutesPerTier, DoctorRoutesPerTier, nil},
		{CheckReaders, DoctorReaders, nil},
		{CheckLoopsRedial, DoctorLoopsRedial, func(w *world) DoctorInput {
			in := healthyInput(w)
			in.ServerStarted = time.Time{}
			return in
		}},
		{CheckMemberEnv, DoctorMemberEnv, func(w *world) DoctorInput {
			in := healthyInput(w)
			in.Probes, in.ProbeErr = nil, "probe not configured"
			return in
		}},
		{CheckMemberEnv + " (cut probe)", DoctorMemberEnv, func(w *world) DoctorInput {
			in := healthyInput(w)
			p := in.Probes["m1"]
			p.Complete = false
			in.Probes = map[string]MemberProbe{"m1": p}
			return in
		}},
		{CheckLanderLoop, DoctorLanderLoop, nil},
		{CheckFleetRows, DoctorFleetRows, func(w *world) DoctorInput {
			in := healthyInput(w)
			in.Machines, in.ConfigErr = nil, "no config store named"
			return in
		}},
		{CheckProviders, DoctorProviders, nil},
		{CheckSeat, DoctorSeat, func(w *world) DoctorInput {
			in := healthyInput(w)
			in.Hook = HookM{}
			return in
		}},
		{CheckGitIdentity, DoctorGitIdentity, nil},
		{CheckFinishFailures, DoctorFinishFailures, nil},
		{CheckFriends, DoctorFriends, nil},
		{CheckDraining, DoctorDraining, nil},
	}
	var seen []string
	for _, g := range gens {
		seen = append(seen, strings.Fields(g.check)[0])
	}
	for _, check := range doctorCheckNames {
		assert.Contains(t, seen, check, "the doctor cause %q names a check this test does not classify", check)
	}
	classify := func(t *testing.T, w *world, g gen, checks []DoctorCheck, wantUnmeasured bool) {
		t.Helper()
		require.NotEmpty(t, checks, "%s emitted nothing", g.check)
		members := w.s.Members()
		check := strings.Fields(g.check)[0]
		unmeasured := 0
		for _, c := range checks {
			require.Equal(t, check, c.Check, "a line of another check: %s", c.Line())
			if c.Member != "" {
				assert.Contains(t, members, c.Member, "Member names no fleet member: %s", c.Line())
				assert.Contains(t, memberScopedChecks, c.Check, "a global check's line names a member: %s", c.Line())
				if c.Check == CheckLoopsRedial {
					assert.Equal(t, "member-"+c.Member, c.Subject, "a member's loop line is not its loop: %s", c.Line())
				} else {
					assert.Equal(t, c.Member, c.Subject, "Member and the subject disagree: %s", c.Line())
				}
			} else if slices.Contains(memberScopedChecks, c.Check) {
				assert.NotContains(t, members, c.Subject, "a member-scoped check's line names a member and carries no Member: %s", c.Line())
				assert.False(t, c.Check == CheckLoopsRedial && strings.HasPrefix(c.Subject, "member-"), "a member's loop line carries no Member: %s", c.Line())
			}
			if c.Unmeasured {
				unmeasured++
				assert.NotEqual(t, DoctorFail, c.Status, "an unmeasured line is a FAIL: %s", c.Line())
			}
		}
		if wantUnmeasured {
			assert.Positive(t, unmeasured, "%s measured nothing and no line says so: %s", g.check, lines(checks))
		} else {
			assert.Zero(t, unmeasured, "%s on a healthy run says unmeasured: %s", g.check, lines(checks))
		}
	}
	for _, g := range gens {
		t.Run(g.check, func(t *testing.T) {
			t.Parallel()
			w := doctorWorld(t)
			classify(t, w, g, g.run(healthyInput(w)), false)
			if g.unmeasured != nil {
				classify(t, w, g, g.run(g.unmeasured(w)), true)
			}
		})
	}
}
