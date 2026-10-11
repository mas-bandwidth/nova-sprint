package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The doctor's checks (doctor.go), one test per check, each on a twin of the sprint (the world:
// the steps' plans applied to the tables) seeded with the fault the night of 2026-10-10 measured,
// the check expected to FAIL on it with its evidence and its verb; and a healthy twin every check
// passes. The checks read only what the verb read (DoctorInput): no store, no host.

// doctorWorld is a twin with member m1 up at width 4, its reader reader-m1, and one opencode
// route of tier flash.
func doctorWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-m1")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 4}))
	w.s.Routes = []Route{{Name: "flash-oc", Tier: cardhdr.RouteFlash, Provider: "p", Model: "a", Enabled: true, Deadline: 600}}
	w.s.ReaderStates = map[string]string{"reader-m1": ReaderUp}
	return w
}

// fullProbe is a member host with every tool on its loop's PATH, its push credential and its
// seat good, 100 GB free and the declared build.
func fullProbe(m string) MemberProbe {
	tools := map[string]string{}
	for _, t := range append(MemberTools, OpenCode) {
		tools[t] = "/usr/bin/" + t
	}
	return MemberProbe{Member: m, Host: "nova@" + m, Reached: true, OS: "Linux", PathFrom: "loop", Tools: tools, FreeKB: 100 << 20,
		Push: "PUSH-CREDENTIAL OK", PushOK: true, Secrets: "CHECK OK", SecretOK: true, Version: "v1.2.6"}
}

// healthyInput is the doctor's read of a healthy twin: every loop beating since the server
// started, nova-config naming the fleet as it is, every host whole, the push loop proven.
func healthyInput(w *world) DoctorInput {
	now := w.s.Now
	return DoctorInput{
		Snap:          w.s,
		Req:           TickReq{Beats: map[string]Beat{"m1": {At: now.Add(-2 * time.Second)}}},
		ReaderBeats:   map[string]Beat{"reader-m1": {At: now.Add(-2 * time.Second)}},
		ServerStarted: now.Add(-10 * time.Minute), ServerHow: "test",
		Machines: []DoctorMachine{{Name: "m1", User: "nova", Seat: "swarm-m1", Width: 4}},
		Probes:   map[string]MemberProbe{"m1": fullProbe("m1")},
		Declared: "v1.2.6",
		Push:     PushM{Measured: true, Holder: "rowan", Record: PushRecord{Proven: now.Add(-2 * time.Minute)}, Recorded: true},
	}
}

// failsOn is the checks of the named check whose status is FAIL.
func failsOn(cs []DoctorCheck, check string) []DoctorCheck {
	var out []DoctorCheck
	for _, c := range cs {
		if c.Check == check && c.Status == DoctorFail {
			out = append(out, c)
		}
	}
	return out
}

// lines is the checks as the verb prints them.
func lines(cs []DoctorCheck) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Line() + "\n")
	}
	return b.String()
}

// A healthy sprint passes every check: every line OK, none WARN, DRIFT or FAIL.
func TestDoctorHealthyTwinIsAllOK(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1", "s1-2"}}}))
	w.tick(5 * time.Minute)
	in := healthyInput(w)
	cs := Doctor(in)
	require.NotEmpty(t, cs)
	for _, c := range cs {
		assert.Equal(t, DoctorOK, c.Status, "a healthy twin: %s", c.Line())
		assert.True(t, strings.HasPrefix(c.Line(), "DOCTOR OK "+c.Check+" "), c.Line())
	}
	for _, check := range []string{CheckReadyNotDealt, CheckRoutesPerTier, CheckReaders, CheckLoopsRedial, CheckMemberEnv, CheckLanderLoop, CheckFleetRows, CheckProviders, CheckSeat, CheckFinishFailures, CheckFriends} {
		found := false
		for _, c := range cs {
			found = found || c.Check == check
		}
		assert.True(t, found, "check %s says its line\n%s", check, lines(cs))
	}
}

// 1. Cards ready past the bound that no deal takes are named with the deal's own reason:
// the night's heavy cards whose every route ran under a harness no member named.
func TestDoctorReadyNotDealtNamesTheDealsReason(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.s.Routes = []Route{{Name: "flash-claude", Tier: cardhdr.RouteFlash, Provider: "subscription-claude", Model: "opus", Harness: "claude", Enabled: true, Deadline: 600}}
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 3}))
	w.tick(2 * time.Minute)
	cs := DoctorReadyNotDealt(healthyInput(w))
	fails := failsOn(cs, CheckReadyNotDealt)
	require.Len(t, fails, 1, "one group: one reason\n%s", lines(cs))
	f := fails[0]
	assert.Equal(t, "no-launcher", f.Subject, f.Line())
	assert.Contains(t, f.Evidence, "n=3", f.Line())
	assert.Contains(t, f.Evidence, "claude", "the deal's words name the harness: %s", f.Line())
	assert.Contains(t, f.Line(), "remedy=", f.Line())

	// the same cards, now dealable (an opencode route): the deal deals them, so the run loop has not
	w.s.Routes = append(w.s.Routes, Route{Name: "flash-oc", Tier: cardhdr.RouteFlash, Provider: "p", Model: "a", Enabled: true, Deadline: 600})
	cs = DoctorReadyNotDealt(healthyInput(w))
	fails = failsOn(cs, CheckReadyNotDealt)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, "not-ticked", fails[0].Subject, fails[0].Line())
}

// 2. A tier with ready cards and no route a member up can launch is FAIL, with the routes and why.
func TestDoctorRoutesPerTierFailsATierNoMemberCanDraw(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.s.Routes = []Route{{Name: "flash-claude", Tier: cardhdr.RouteFlash, Provider: "subscription-claude", Model: "opus", Harness: "claude", Enabled: true, Deadline: 600}}
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	cs := DoctorRoutesPerTier(healthyInput(w))
	fails := failsOn(cs, CheckRoutesPerTier)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, cardhdr.RouteFlash, fails[0].Subject)
	assert.Contains(t, fails[0].Evidence, "ready=2 enabled=1 resting=0 usable=0", fails[0].Line())
	assert.Contains(t, fails[0].Evidence, "no-member-launches=flash-claude(runs under claude)", fails[0].Line())
	assert.Contains(t, fails[0].Remedy, "--harnesses")
}

// 3. Reads wait while the reader loops beat no more (the night's Linux reader loops hung after
// the server restart): each such reader FAIL, and the readers as a whole FAIL.
func TestDoctorReadersFailsReadsWaitingOnHungLoops(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.s.Readers.SetRows([]string{"reader-b1", "reader-b2"})
	w.s.Readers.Put(&Card{ID: "s1-1.r1.reader-b1", Row: "reader-b1", Col: Asked, Fields: map[string]string{"kind": "read"}})
	w.s.Readers.Put(&Card{ID: "s1-2.r1.reader-b2", Row: "reader-b2", Col: Asked, Fields: map[string]string{"kind": "read"}})
	w.s.ReaderStates = map[string]string{"reader-b1": ReaderAway, "reader-b2": ReaderAway}
	in := healthyInput(w)
	in.ReaderBeats = map[string]Beat{"reader-b1": {At: w.s.Now.Add(-20 * time.Minute)}, "reader-b2": {At: w.s.Now.Add(-20 * time.Minute)}}
	cs := DoctorReaders(in)
	fails := failsOn(cs, CheckReaders)
	subjects := map[string]bool{}
	for _, f := range fails {
		subjects[f.Subject] = true
	}
	assert.True(t, subjects["reader-b1"] && subjects["reader-b2"] && subjects["all"], "each hung reader and the whole: %s", lines(cs))
	for _, f := range fails {
		if f.Subject == "all" {
			assert.Contains(t, f.Evidence, "reads wait and no reader is up", f.Line())
		}
	}
}

// 4. A member whose loop beat last before the server restarted did not dial it again: FAIL,
// with the verb that restarts its unit; a host that does not answer is down, WARN.
func TestDoctorLoopsRedialFailsALoopThatDidNotDialAgain(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := healthyInput(w)
	in.ServerStarted = w.s.Now.Add(-30 * time.Minute)
	in.Req.Beats = map[string]Beat{"m1": {At: w.s.Now.Add(-40 * time.Minute)}}
	cs := DoctorLoopsRedial(in)
	fails := failsOn(cs, CheckLoopsRedial)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, "member-m1", fails[0].Subject)
	assert.Equal(t, "ssh nova@m1 systemctl --user restart nova-loop-member-m1.service", fails[0].Remedy)
	assert.True(t, Doctor(in)[0].Area != "", "the doctor runs it")

	in.Probes = map[string]MemberProbe{"m1": {Member: "m1", Host: "nova@m1", Err: "ssh: connect timed out"}}
	cs = DoctorLoopsRedial(in)
	assert.Empty(t, failsOn(cs, CheckLoopsRedial), "a host that does not answer is down, not hung: %s", lines(cs))
	assert.Equal(t, DoctorWarn, cs[0].Status, lines(cs))
}

// 5. A member whose loop's PATH has no sqlite3 and whose push credential does not authenticate
// (the night's b1-b3): one FAIL each, naming the member, with the bench play.
func TestDoctorMemberEnvFailsAMissingToolAndNoPushCredential(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := healthyInput(w)
	p := fullProbe("m1")
	p.Tools["sqlite3"] = ""
	p.PushOK, p.Push = false, "PUSH-CREDENTIAL FAIL reason=no-credential"
	p.Version = "v1.2.5"
	in.Probes = map[string]MemberProbe{"m1": p}
	cs := DoctorMemberEnv(in)
	fails := failsOn(cs, CheckMemberEnv)
	require.Len(t, fails, 2, lines(cs))
	out := lines(cs)
	assert.Contains(t, out, "DOCTOR FAIL member-env m1 tool=sqlite3 is on no PATH entry of its cards (the loop's PATH from loop")
	assert.Contains(t, out, "remedy=make -C fleet join HOST=m1")
	assert.Contains(t, out, "push credential: PUSH-CREDENTIAL FAIL reason=no-credential")
	assert.Contains(t, out, "DOCTOR DRIFT member-env m1 installed=v1.2.5 declared=v1.2.6")
}

// 6. A head the landing refused, accepted again at the same head (the loop of epoch 16): FAIL.
func TestDoctorLanderLoopFailsARefusedHeadAcceptedAgain(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 1}))
	head := strings.Repeat("a1", 20)
	c := w.s.Work.Card("s1-1")
	require.NotNil(t, c)
	moved := &Card{ID: c.ID, Row: c.Row, Col: Merging, Score: c.Score, Fields: map[string]string{"head": head, FieldLandRefusedHead: head}}
	w.s.Work.Put(moved)
	cs := DoctorLanderLoop(healthyInput(w))
	fails := failsOn(cs, CheckLanderLoop)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, "s1-1", fails[0].Subject)
	assert.Contains(t, fails[0].Evidence, "the landing refused head a1a1a1a1a1a1 and it is accepted again", fails[0].Line())
}

// 7. A fleet row whose machine record is gone (the night's hetzner after its rename): FAIL,
// with fleet sync, and safe for a later doctor --fix.
func TestDoctorFleetRowsFailsARowWithNoMachineRecord(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "hetzner", Width: 16}))
	in := healthyInput(w)
	cs := Doctor(in)
	fails := failsOn(cs, CheckFleetRows)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, "hetzner", fails[0].Subject)
	assert.Contains(t, fails[0].Evidence, "has no machine record in nova-config")
	assert.Equal(t, "nova-sprint fleet sync", fails[0].Remedy)
	assert.True(t, fails[0].FixSafe, "a stale row is safe for doctor --fix")
}

// 8. A provider out of credit takes its routes from the deal: FAIL with the routes taken and
// the funded verb.
func TestDoctorProvidersFailsAProviderOutOfCredit(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.s.Routes = append(w.s.Routes, Route{Name: "pro-grok", Tier: cardhdr.RoutePro, Provider: "openrouter", Model: "x-ai/grok", Enabled: true, Deadline: 600})
	rest := RouteRest{At: w.s.Now.Add(-2 * time.Hour), Until: OpenUntil, Cards: []string{"s1-9.w3"}, Cause: RestCredit, Why: "class=out-of-credit status=402"}
	w.s.Fleet.SetProp(PropProviderRest("openrouter"), rest.value())
	cs := DoctorProviders(healthyInput(w))
	fails := failsOn(cs, CheckProviders)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, "openrouter", fails[0].Subject)
	assert.Contains(t, fails[0].Evidence, "routes-taken=1")
	assert.Equal(t, "nova-sprint funded openrouter --reason '<the payment>'", fails[0].Remedy)
}

// 9. The seat's push loop unproven past the bound, and judgments past their deadline: FAIL.
func TestDoctorSeatFailsAStalePushProofAndOverdueJudgments(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := healthyInput(w)
	in.Push.Record.Proven = w.s.Now.Add(-time.Hour)
	in.Seat = SeatWaits{Judgments: 54, Overdue: 50}
	cs := DoctorSeat(in)
	fails := failsOn(cs, CheckSeat)
	require.Len(t, fails, 2, lines(cs))
	assert.Equal(t, "push", fails[0].Subject)
	assert.Contains(t, fails[0].Evidence, "proven=1h0m0s ago")
	assert.Equal(t, "judgments", fails[1].Subject)
	assert.Contains(t, fails[1].Evidence, "open=54 overdue=50")
}

// 10. Members whose takes end mostly on one environment class in the hour (the night's b1-b3:
// native refused for want of sqlite3, and no result) are named, never their cards, and the
// fleet is named once beside them.
func TestDoctorFinishFailuresNamesTheMemberNotTheCard(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), "b1", "b2"))
	at := stamp(w.s.Now.Add(-10 * time.Minute))
	for i, m := range []string{"b1", "b1", "b1", "b1", "b2", "b2", "b2"} {
		id := WorkCardID("s1-"+itoa(i+1), 1)
		f := map[string]string{"kind": "work", "gen": "2", "attempt": "1"}
		f[FieldStagingTake+"1"] = ProviderTake{Route: "flash-oc", Member: m, Finished: at, Error: "native refused: budget: sqlite3 is on no PATH entry of this bench"}.String()
		f[FieldProviderTake+"1"] = ProviderTake{Route: "flash-oc", Member: m, Finished: at, Error: "no result: no RESULT.md shape"}.String()
		w.s.Fleet.Put(&Card{ID: id, Row: m, Col: Withdrawn, Fields: f})
	}
	cs := DoctorFinishFailures(healthyInput(w))
	fails := failsOn(cs, CheckFinishFailures)
	subjects := []string{}
	for _, f := range fails {
		subjects = append(subjects, f.Subject)
	}
	assert.ElementsMatch(t, []string{"b1", "b2", "fleet"}, subjects, lines(cs))
	assert.Contains(t, lines(cs), "remedy=make -C fleet join HOST=b1")
	assert.Equal(t, "missing tool", FinishClass("native refused: budget: sqlite3 is on no PATH entry of this bench"))
	assert.Equal(t, "native refused", FinishClass("staging refused: native refused: budget unverifiable: the usage source stopped answering"))
	assert.Equal(t, "staging refused", FinishClass("staging refused: head 46cdb6 is in neither the mirror nor origin"))
	assert.Equal(t, "push refused", FinishClass("push refused: the result's head \"(not committed)\" is not a sha"), "a push refused for the card's head is no environment")
	assert.Equal(t, "no result", FinishClass("no result: no RESULT.md shape"))
}

// The friends: a friend up whose daemon beats no more is FAIL; one held is WARN with its reason.
func TestDoctorFriendsFailsADaemonThatBeatsNoMore(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := healthyInput(w)
	in.Req.Friends = []FriendSeat{{Name: "emma", Status: Up, Width: 4}, {Name: "stella", Status: Held, Width: 16, Why: "out of Codex usage to 2026-10-16"}}
	in.FriendBeats = map[string]Beat{"emma": {At: w.s.Now.Add(-10 * time.Minute)}}
	cs := DoctorFriends(in)
	fails := failsOn(cs, CheckFriends)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, "emma", fails[0].Subject)
	assert.Contains(t, lines(cs), "DOCTOR WARN friends stella status=held")
	assert.Contains(t, lines(cs), "out of Codex usage")
}

// The line is one line in the agreed form, the subject one word.
func TestDoctorLineForm(t *testing.T) {
	t.Parallel()
	c := DoctorCheck{Check: CheckMemberEnv, Subject: "b 1", Status: DoctorFail, Evidence: "tool=sqlite3\nmissing", Remedy: "make -C fleet join HOST=b1"}
	assert.Equal(t, "DOCTOR FAIL member-env b_1 tool=sqlite3 missing remedy=make -C fleet join HOST=b1", c.Line())
	assert.Equal(t, "nova-config route add <name> --tier pro ...", remedyIn("no route: run nova-config route add <name> --tier pro ...; or pin the card", ""))
}
