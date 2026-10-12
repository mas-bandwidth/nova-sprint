package main

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// The doctor checks the definition of done (docs/SPEC-SPRINT.md, "The seat check"): each
// fault named in one line with its evidence, from the seat check's machinery verdicts, the
// coordinator's stop ledger, section 9's always-true rules, the deal dry and the seat's
// overdue waits. One fake store per fault, each planted alone, and a clean store that
// passes (exit 0).

func TestDoctorFaultsCleanStore(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	r := sprint.SeatCheckReport{}
	if got := doctorFaults(r, store.StopsRecord{}, nil, nil, now); len(got) != 0 {
		t.Fatalf("a clean store has faults: %v", got)
	}
}

func TestDoctorFaultsNamesSeatCheckEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	r := sprint.SeatCheckReport{Lines: []sprint.SeatCheckLine{{Thing: sprint.SeatCheckFleet, Facts: []string{"member=m1"}}}}
	got := doctorFaults(r, store.StopsRecord{}, nil, nil, now)
	if len(got) != 1 || got[0] != "MACHINERY fleet DOWN member=m1" {
		t.Fatalf("seat-check fault evidence = %v", got)
	}
}

func TestDoctorFaultsNamesAStop(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	stop := sprint.Stop{Kind: sprint.StopKindMemberDown, Subject: "m3", Since: now.Add(-5 * time.Minute), What: "member m3 is down", Effect: "its width idles", Undo: "start its member loop"}
	rec := store.StopsRecord{Stops: []sprint.Stop{stop}}
	got := doctorFaults(sprint.SeatCheckReport{}, rec, nil, nil, now)
	if len(got) != 1 || !strings.HasPrefix(got[0], "STOP member-down m3 age=5m0s") || !strings.Contains(got[0], "undo: start its member loop") {
		t.Fatalf("stop fault evidence = %v", got)
	}
}

func TestDoctorFaultsNamesAViolation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	v := []sprint.Violation{{Rule: 2, Detail: "s1-1 is working with no live work card"}}
	got := doctorFaults(sprint.SeatCheckReport{}, store.StopsRecord{}, v, nil, now)
	if len(got) != 1 || got[0] != "RULE rule 2: s1-1 is working with no live work card" {
		t.Fatalf("violation fault evidence = %v", got)
	}
}

func TestDoctorFaultsNamesOverdueSeatEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	rec := store.StopsRecord{Seat: sprint.SeatWaits{Judgments: 1, Overdue: 1}}
	got := doctorFaults(sprint.SeatCheckReport{}, rec, nil, nil, now)
	if len(got) != 1 || !strings.HasPrefix(got[0], "MACHINERY seat DOWN") {
		t.Fatalf("overdue seat fault evidence = %v", got)
	}
}

// A sprint with cards waiting and nothing ready is the deal dry: a fault named with the
// counts, and nothing else.
func TestDoctorFaultsNamesTheDealDry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})
	work.Put(&sprint.Card{ID: "s1-1.w1", Row: "s1", Col: sprint.Waiting})
	snap := &sprint.Snapshot{Work: work}
	got := doctorFaults(sprint.SeatCheckReport{}, store.StopsRecord{}, nil, snap, now)
	if len(got) != 1 || got[0] != "MACHINERY deal DOWN ready=0 waiting=1: nothing ready to feed the fleet" {
		t.Fatalf("deal-dry fault evidence = %v", got)
	}
}

// A dashboard serving a snapshot a second or more old is DOWN with its age, and the doctor
// names it (the seat check's dashboard line, section 9; the served snapshot's own time is
// the body's "fetchedAt").
func TestDoctorFaultsNamesADashboardServingOldData(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	m := sprint.SeatCheckMeasures{
		Server:    sprint.ServerM{Addr: "127.0.0.1:7399"},
		Store:     sprint.StoreM{Addr: "mem", DBSize: -1},
		Dashboard: sprint.DashM{Addr: "127.0.0.1:7390", Status: 200, Build: "nova-sprint dev", At: now.Add(-2 * time.Second)},
	}
	r := sprint.JudgeSeatCheck(m, now)
	got := doctorFaults(r, store.StopsRecord{}, nil, nil, now)
	var dash string
	for _, f := range got {
		if strings.Contains(f, "dashboard DOWN") {
			dash = f
		}
	}
	if dash == "" || !strings.Contains(dash, "age=2s") {
		t.Fatalf("dashboard-age fault evidence = %v", got)
	}
}

// A clean sprint is GREEN and exits 0: no machinery verdict is DOWN, no stop holds, no
// rule is broken and no judgment waits on the seat.
func TestDoctorCleanStoreExitsZero(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = mockHealthyOutside()

	code, out, errs := ta.do("doctor")
	require.Equal(t, 0, code, "a clean sprint is GREEN\n%s%s", out, errs)
	assert.NotContains(t, out, "DOCTOR fault ", "a clean sprint names no fault: %s", out)
	assert.Contains(t, out, "DOCTOR OK ", "the summary is OK: %s", out)
}

// A fleet member that never beats is a fault the doctor names with the seat check's own
// verdict and evidence, and exits 1.
func TestDoctorNamesTheFleetMemberDown(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2,m3") // ta.live is m1, m2: m3 never beats
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = mockHealthyOutside()

	code, out, errs := ta.do("doctor")
	require.Equal(t, 1, code, "a member down is RED\n%s%s", out, errs)
	assert.Contains(t, out, "MACHINERY fleet DOWN", "the fleet verdict is the seat check's: %s", out)
	assert.Contains(t, out, "m3", "the fault names the member: %s", out)
	assert.Contains(t, out, "DOCTOR RED ", "the summary is RED: %s", out)
}

// The JSON carries the same fault lines and the machinery evidence, and exits 1 when red.
func TestDoctorJSONCarriesTheFaults(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2,m3")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = mockHealthyOutside()

	code, out, errs := ta.do("doctor --json")
	require.Equal(t, 1, code, "a member down is RED\n%s%s", out, errs)
	var got struct {
		Status    string          `json:"status"`
		Exit      int             `json:"exit"`
		Faults    []string        `json:"faults"`
		Machinery json.RawMessage `json:"machinery"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got), "doctor --json: %s", out)
	assert.Equal(t, "red", got.Status)
	assert.Equal(t, 1, got.Exit)
	require.NotEmpty(t, got.Faults, "the faults are named: %s", out)
	assert.Contains(t, strings.Join(got.Faults, "\n"), "MACHINERY fleet DOWN", "the fault evidence: %v", got.Faults)
	assert.NotEmpty(t, got.Machinery, "the machinery evidence rides along")
}

// doctorOutside is the healthy outside with the doctor's reaches given: nova-config naming
// machines, each member's host probed as probe says, and the server started an hour ago.
func doctorOutside(ta *testApp, machines []string, probe func(m string) sprint.MemberProbe) outside {
	o := mockHealthyOutside()
	o.doctorConfig = func(context.Context) (doctorConfig, error) {
		cfg := doctorConfig{Loops: map[string][]string{}}
		for _, m := range machines {
			cfg.Machines = append(cfg.Machines, sprint.DoctorMachine{Name: m, User: "nova", Seat: "swarm-" + m, Width: 64}) // init's width
		}
		return cfg, nil
	}
	o.serverStarted = func(context.Context, string) (time.Time, string) { return ta.a.now().Add(-time.Hour), "test" }
	o.declared = func(context.Context) string { return "v1.2.6" }
	o.probeMembers = func(_ context.Context, ms []sprint.DoctorMachine, tools func(string) []string, _ string) map[string]sprint.MemberProbe {
		out := map[string]sprint.MemberProbe{}
		for _, m := range ms {
			p := sprint.MemberProbe{Member: m.Name, Host: "nova@" + m.Name, Reached: true, Complete: true, OS: "Linux", PathFrom: "loop", Tools: map[string]string{}, FreeKB: 100 << 20,
				Push: "PUSH-CREDENTIAL OK", PushOK: true, Secrets: "CHECK OK", SecretOK: true, Version: "v1.2.6"}
			for _, t := range tools(m.Name) {
				p.Tools[t] = "/usr/bin/" + t
			}
			if probe != nil {
				p = probe(m.Name)
				if p.Tools == nil {
					p.Tools = map[string]string{}
				}
			}
			out[m.Name] = p
		}
		return out
	}
	return o
}

// The fleet group against nova-config and each host: a fleet row whose machine record is gone
// (the night's hetzner after its rename) and a member whose loop's PATH has no sqlite3 and whose
// push credential fails (the night's b1-b3) are each a FAIL line with the verb, and exit 1.
func TestDoctorAreaFleetNamesAStaleRowAndAMemberMissingATool(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = doctorOutside(ta, []string{"m1"}, func(m string) sprint.MemberProbe {
		p := sprint.MemberProbe{Member: m, Host: "nova@" + m, Reached: true, Complete: true, OS: "Linux", PathFrom: "loop", Tools: map[string]string{}, FreeKB: 100 << 20,
			Push: "PUSH-CREDENTIAL FAIL reason=no-credential", Secrets: "CHECK OK", SecretOK: true, Version: "v1.2.6"}
		for _, t := range append(slices.Clone(sprint.MemberTools), sprint.OpenCode) {
			if t != "sqlite3" {
				p.Tools[t] = "/usr/bin/" + t
			}
		}
		return p
	})

	code, out, errs := ta.do("doctor --area fleet")
	require.Equal(t, 1, code, "a stale row and a broken member are RED\n%s%s", out, errs)
	assert.Contains(t, out, "DOCTOR FAIL fleet-rows m2 fleet row m2", out)
	assert.Contains(t, out, "remedy=nova-sprint fleet sync", out)
	assert.Contains(t, out, "DOCTOR FAIL member-env m1 tool=sqlite3 is on no PATH entry of its cards", out)
	assert.Contains(t, out, "DOCTOR FAIL member-env m1 push credential: PUSH-CREDENTIAL FAIL reason=no-credential", out)
	assert.NotContains(t, out, "DOCTOR routes GREEN", "--area fleet prints the fleet group alone: %s", out)
	assert.Contains(t, out, "DOCTOR RED ", out)
}

// The whole doctor on a healthy sprint with every reach given: no FAIL line, exit 0, and the
// JSON carries the checks for the dashboard.
func TestDoctorChecksHealthyStoreExitZeroAndJSON(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = doctorOutside(ta, []string{"m1"}, nil)

	code, out, errs := ta.do("doctor")
	require.Equal(t, 0, code, "a healthy sprint\n%s%s", out, errs)
	assert.NotContains(t, out, "DOCTOR FAIL ", out)
	assert.Contains(t, out, "DOCTOR OK member-env all", out)
	assert.Contains(t, out, "DOCTOR OK fleet-rows all", out)

	code, out, errs = ta.do("doctor --json --area fleet")
	require.Equal(t, 0, code, "%s%s", out, errs)
	var got struct {
		Checks  []sprint.DoctorCheck `json:"checks"`
		FixSafe []string             `json:"fix_safe"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	require.NotEmpty(t, got.Checks)
	for _, c := range got.Checks {
		assert.Equal(t, sprint.AreaFleet, c.Area, "--area fleet: %+v", c)
	}
	assert.Contains(t, got.FixSafe, sprint.CheckFleetRows)
}

// --area takes an area of the doctor, and nothing else.
func TestDoctorRefusesAnUnknownArea(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	code, _, errs := ta.do("doctor --area nowhere")
	require.NotEqual(t, 0, code)
	assert.Contains(t, errs, "which is no area")
}

// The probe's lines are read into what it found; the server's elapsed time is read from ps.
func TestDoctorDoesNotForwardWhenServerSet(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = doctorOutside(ta, []string{"m1"}, nil)

	code, out, errs := ta.do("doctor")
	require.Equal(t, 0, code, "doctor should run locally, not be forwarded\n%s%s", out, errs)
	assert.Contains(t, out, "DOCTOR OK member-env all", "doctor reads config from caller's env")
}

func TestDoctorProbeParsesItsLines(t *testing.T) {
	t.Parallel()
	var p sprint.MemberProbe
	parseProbe(&p, []byte("os Linux\npathfrom loop\ntool git /usr/bin/git\ntool sqlite3 -\ntool go -\nnear go /home/nova/sdk/go1.26.6/bin/go\nfree 900000000\npush PUSH-CREDENTIAL OK seat=swarm-b1\nsecrets 0 CHECK OK\nversion nova-sprint v1.2.6 linux/amd64 go1.26.6\nend\n"))
	assert.True(t, p.Complete, "the end line: answered in full")
	var cut sprint.MemberProbe
	parseProbe(&cut, []byte("os Linux\npathfrom loop\ntool git /usr/bin/git\n"))
	assert.False(t, cut.Complete, "no end line: cut by the budget")
	assert.Equal(t, "Linux", p.OS)
	assert.Equal(t, "/usr/bin/git", p.Tools["git"])
	assert.Equal(t, "", p.Tools["sqlite3"])
	_, said := p.Tools["sqlite3"]
	assert.True(t, said, "a tool the loop's PATH lacks is said, empty")
	assert.Equal(t, "", p.Tools["go"], "the toolchain off the loop's PATH does not count: the gate runs on that PATH")
	assert.Equal(t, "/home/nova/sdk/go1.26.6/bin/go", p.Near["go"], "but where it is is said")
	assert.Equal(t, int64(900000000), p.FreeKB)
	assert.True(t, p.PushOK)
	assert.True(t, p.SecretOK)
	assert.Equal(t, "v1.2.6", p.Version)
	for in, want := range map[string]time.Duration{"39:28": 39*time.Minute + 28*time.Second, "01:02:03": time.Hour + 2*time.Minute + 3*time.Second, "2-00:00:01": 48*time.Hour + time.Second} {
		got, ok := parseEtime(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	script := probeScript("b1", "swarm-b1", []string{"git", "sqlite3"})
	assert.Contains(t, script, "nova-loop-$L.service", "the loop unit's PATH, not the login shell's")
	assert.Contains(t, script, "nova-push-credential\" probe "+doctorPushURL)
	assert.NotContains(t, script, "exec ", "the probe runs nothing but reads")
	assert.True(t, strings.HasSuffix(script, "echo "+probeEnd+"\n"), "the end line is the script's last")
	odd := probeScript("b1;id", "swarm-b1", []string{"git;id"})
	assert.Contains(t, odd, "L='member-b1;id'", "the member's name is quoted")
	assert.Contains(t, odd, "for t in 'git;id';", "each tool's name is quoted")
}

// refusedAt plants one refused verb on the test app's own store, at the app's clock, the way
// the server records them (store.RecordRefused).
func refusedAt(t *testing.T, ta *testApp, ago time.Duration, verb, member, argv, why string) {
	t.Helper()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	require.NoError(t, st.RecordRefused(context.Background(), sprint.RefusedVerb{
		At: ta.now.Add(-ago), Verb: verb, Member: member, Argv: argv, Why: why,
	}))
}

// The doctor reads the server's own refused-verb record, never a member's log: a member whose
// beats the server refused RefusedBeatBound (3) or more times in the window is one member-beat
// FAIL naming the count, the last refusal and what it sent, before and independent of the
// member-down stop; a member with none is OK. (docs/SPEC-SPRINT.md, "The doctor's refused-verb
// and beat-form checks").
func TestDoctorNamesRefusedBeatsBeforeTheMemberDown(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = mockHealthyOutside()

	for i := range 3 {
		refusedAt(t, ta, time.Duration(i)*time.Minute, sprint.RefusedBeatVerb, "m1", "fleet beat m1", "a beat wants --load")
	}
	code, out, errs := ta.do("doctor")
	require.Equal(t, 1, code, "refused beats are RED\n%s%s", out, errs)
	assert.Contains(t, out, "DOCTOR FAIL member-beat m1: 3 beats refused in 10m0s, last: a beat wants --load, argv: fleet beat m1 remedy=",
		"the member-beat fault names the cause, not only the down symptom: %s", out)
	assert.Contains(t, out, "DOCTOR OK member-beat m2", "a member whose beats were not refused is OK: %s", out)
}

// The server records a form-refused beat under the member that sent it, and the doctor's
// member-beat check reads that record: `fleet beat <m>` with no --load, sent through the
// server's own step (serveCtx) RefusedBeatBound (3) times, is one DOCTOR FAIL member-beat.
// The record must be the server's own, written by the refusal, never planted by a test
// (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form checks").
func TestDoctorNamesRefusedBeatsTheServerWrote(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = mockHealthyOutside()
	ta.a.serveAddr = "mem:0"

	for range sprint.RefusedBeatBound {
		res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{{"fleet", "beat", "m1"}}}, false)
		require.Equal(t, 2, res.Results[0].Code, "a beat with no --load is refused: %s", res.Results[0].Stderr)
		require.Contains(t, res.Results[0].Stderr, "--load", "the refusal names the missing --load: %s", res.Results[0].Stderr)
	}
	code, out, errs := ta.do("doctor")
	require.Equal(t, 1, code, "the server's refused beats are RED\n%s%s", out, errs)
	assert.Contains(t, out, "DOCTOR FAIL member-beat m1: 3 beats refused in 10m0s, last: ",
		"the doctor names the member the server's record refused, from the refusal's own write: %s", out)
	assert.Contains(t, out, "argv: fleet beat m1 remedy=", "and what it sent: %s", out)
}

// The doctor prints the refused-beat FAIL before the member-down symptom: the cause is read
// above the symptom, though the checks are computed independently
// (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form checks").
func TestDoctorPrintsRefusedBeatsAboveTheMemberDownStop(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2,m3") // ta.live is m1, m2: m3 never beats
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = mockHealthyOutside()
	ta.a.serveAddr = "mem:0"

	for range sprint.RefusedBeatBound {
		res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{{"fleet", "beat", "m1"}}}, false)
		require.Equal(t, 2, res.Results[0].Code, res.Results[0].Stderr)
	}
	code, out, errs := ta.do("doctor")
	require.Equal(t, 1, code, "refused beats and a member down are RED\n%s%s", out, errs)
	beat := strings.Index(out, "DOCTOR FAIL member-beat m1")
	down := strings.Index(out, "MACHINERY fleet DOWN")
	require.GreaterOrEqual(t, beat, 0, "the refused-beat fault is printed: %s", out)
	require.GreaterOrEqual(t, down, 0, "the member-down symptom is printed: %s", out)
	assert.Less(t, beat, down, "the cause is printed above the symptom: %s", out)
}

// The refused-verbs check groups every verb word, not only beat: RefusedVerbBound (10) or more
// of one verb word in the window is one FAIL, from the same epoch-keyed record.
func TestDoctorNamesRefusedVerbsAtTen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	ta.ok("tick")
	ta.a.outside = mockHealthyOutside()

	for i := range 10 {
		refusedAt(t, ta, time.Duration(i)*time.Second, "queue", "m1", "queue --as m1", "the store refused it")
	}
	code, out, errs := ta.do("doctor")
	require.Equal(t, 1, code, "ten refused verbs are RED\n%s%s", out, errs)
	assert.Contains(t, out, "DOCTOR FAIL refused-verbs m1: queue x 10 in 10m0s, last: the store refused it",
		"the refused-verbs fault names the verb and its count: %s", out)
}

// The member-env beat-form probe reads the installed beat's dry run: a probe line with a
// non-zero exit is one DOCTOR FAIL member-env naming the pair and the refusal; an exit 0, a
// member down, or a member not probed is no line (docs/SPEC-SPRINT.md, "The doctor's
// refused-verb and beat-form checks").
func TestBeatFormChecksNameTheDisagreeingPair(t *testing.T) {
	t.Parallel()
	machines := []sprint.DoctorMachine{{Name: "m1"}, {Name: "m2"}, {Name: "m3"}}
	probes := map[string]sprint.MemberProbe{
		"m1": {Member: "m1", Reached: true, Version: "v1.2.11"},
		"m2": {Member: "m2", Reached: true, Version: "v1.2.12"},
		"m3": {Member: "m3", Reached: false},
	}
	forms := map[string]BeatForm{
		"m1": {Known: true, OK: false, Form: "nova-sprint fleet beat: flag provided but not defined: -load"},
		"m2": {Known: true, OK: true},
		"m3": {Known: false},
	}
	cs := beatFormChecks(machines, probes, forms, "v1.2.12")
	require.Len(t, cs, 1, "only the pair that disagrees is one FAIL")
	assert.Equal(t, sprint.CheckMemberEnv, cs[0].Check)
	assert.Equal(t, "m1", cs[0].Subject)
	assert.Equal(t, sprint.DoctorFail, cs[0].Status)
	assert.Contains(t, cs[0].Evidence, "beat form: the installed nova-sprint v1.2.11 sends a beat the server at v1.2.12 refuses: nova-sprint fleet beat:")
	assert.Contains(t, cs[0].Line(), "DOCTOR FAIL member-env m1 beat form:")
}

// parseBeatForm reads the probe's beat line into what it found; no line is Known false.
func TestParseBeatForm(t *testing.T) {
	t.Parallel()
	bf := parseBeatForm([]byte("os Linux\nbeat 2 nova-sprint fleet beat: flag provided but not defined: -load\nend\n"))
	assert.True(t, bf.Known)
	assert.False(t, bf.OK)
	assert.Equal(t, "nova-sprint fleet beat: flag provided but not defined: -load", bf.Form)
	ok := parseBeatForm([]byte("beat 0 FLEET-BEAT WOULD m1 load=0.0% dry-run: nothing written\n"))
	assert.True(t, ok.Known)
	assert.True(t, ok.OK)
	assert.False(t, parseBeatForm([]byte("os Linux\n")).Known, "no beat line: not measured")
}

// fleet beat --dry-run plans the beat and writes nothing: the words are read (the forwarding
// CLI's --actor dropped), a WOULD line is the answer, and no beat is written (docs/SPEC-SPRINT.md,
// "The doctor's refused-verb and beat-form checks").
func TestFleetBeatDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	ta.a.serveAddr = "mem:0"
	res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{
		{"fleet", "beat", "--actor", "", "m1", "--load", "0", "--dry-run"},
	}}, false)
	require.Equal(t, 0, res.Results[0].Code, res.Results[0].Stderr)
	assert.Contains(t, res.Results[0].Stdout, "FLEET-BEAT WOULD m1", "the plan is the answer: %s", res.Results[0].Stdout)
	assert.NotContains(t, res.Results[0].Stdout, "FLEET-BEAT OK", "nothing was written: %s", res.Results[0].Stdout)
	if as, _, why := workerVerb([]string{"fleet", "beat", "--actor", "", "m1", "--load", "0", "--dry-run"}); why != "" || as != "m1" {
		t.Fatalf("a dry-run beat with the CLI's --actor is a beat of m1: as=%q why=%q", as, why)
	}
}

// envExitFleet is a sprint whose member m1 holds ten taken work cards of stream s1: it
// finishes all but the last n of them ok, then each of the last n as a failed finish at
// exits[i] with reports[i] and harness[i], a minute apart and each drained by its own tick,
// so the last is the latest. It returns the cards' primaries in finish order of the
// environment exits.
func envExitFleet(ta *testApp, exits []int, reports, harness []string) []string {
	ta.t.Helper()
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 10")
	ta.ok("start")
	ta.ok("tick")
	var q struct{ Cards []queueCard }
	ta.json("queue --as m1", &q)
	require.Len(ta.t, q.Cards, 10, "m1's queue: %+v", q.Cards)
	ta.ok("take --as m1 --limit 100")
	n := len(exits)
	var ok []string
	for _, c := range q.Cards[:10-n] {
		ok = append(ok, c.ID+"@"+strconv.Itoa(c.Gen))
	}
	if len(ok) > 0 {
		ta.ok("finish --as m1 " + strings.Join(ok, " "))
		ta.ok("tick") // a finish is queued until the next tick drains it
	}
	var prs []string
	for i, c := range q.Cards[10-n:] {
		ta.later(time.Minute)
		ta.ok("finish --as m1 " + c.ID + "@" + strconv.Itoa(c.Gen) + " --failed --exit " + strconv.Itoa(exits[i]) +
			" --report '" + reports[i] + "' --harness '" + harness[i] + "'")
		ta.ok("tick")
		prs = append(prs, c.Primary)
	}
	ta.a.outside = doctorOutside(ta, []string{"m1"}, nil)
	return prs
}

// A member whose children ended on its environment over the rate is named under member-env
// (docs/SPEC-SPRINT.md, "An environment exit"): the counts come from the attempt records on
// the epoch's cards, and count the children that ended 134 or 137, as the line says (a
// no-result at exit 0 is recorded as an environment exit but is not one of them); the doctor
// names the member, the rate, the first line of the latest 134/137's tail and the card to
// read. Before this card the doctor had no member-env line at all.
func TestDoctorNamesTheMemberEnvExitRate(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	prs := envExitFleet(ta, []int{134, 137, 0},
		[]string{"no result: Error: Unknown: ChildProcess.spawn (opencode)", "no result: killed", "no result: no RESULT.md shape"},
		[]string{"an older tail", "Error: Unknown: ChildProcess.spawn (opencode)\nmore lines", "the no-result tail"})

	code, out, errs := ta.do("doctor --area fleet")
	require.Equal(t, 1, code, "a member over the rate is RED\n%s%s", out, errs)
	// the no-result at exit 0 is an environment exit but not a 134/137: the doctor's count is
	// what its words say, and its last is the latest 134/137's
	assert.Contains(t, out, "DOCTOR FAIL member-env m1 child exits: 2 of 10 children in the last hour ended 134/137 with no result (20%), last: Error: Unknown: ChildProcess.spawn (opencode) remedy=nova-sprint card "+prs[1]+"\n", out)
}

// A member whose environment exits are from 3% to 10% is a WARN, not RED; below is an OK
// line naming no card (docs/SPEC-SPRINT.md, "An environment exit").
func TestDoctorNamesTheMemberEnvExitWarn(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	prs := envExitFleet(ta, []int{137}, []string{"no result: killed"}, []string{"killed"})

	code, out, errs := ta.do("doctor --area fleet")
	require.Equal(t, 0, code, "a warn band is not RED\n%s%s", out, errs)
	assert.Contains(t, out, "DOCTOR WARN member-env m1 child exits: 1 of 10 children in the last hour ended 134/137 with no result (10%), last: killed remedy=nova-sprint card "+prs[0]+"\n", out)

	ok := newTestApp(t)
	envExitFleet(ok, nil, nil, nil)
	code, out, errs = ok.do("doctor --area fleet")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "DOCTOR OK member-env m1 child exits: 0 of 10 children in the last hour ended 134/137 with no result (0%), last: - remedy=-\n", out)
}

// An environment exit goes back to ready at the SAME attempt, spending nothing
// (docs/SPEC-SPRINT.md, "An environment exit"): the tick that drains the finish withdraws
// the work card with `ended=environment exit=<n>` and the harness tail and no take_ended,
// puts the primary back in ready at attempt 1 with the tail and the take's record on it,
// does not hold the member, and deals the same work card (s1-1.w1, attempt 1) again, to the
// same member, with no redeal counted. Before this card a no-result finish was a take that
// ended (a redeal spent) and a 134 at another report was failed work.
func TestAnEnvironmentExitGoesBackToReadyAtTheSameAttempt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1 --failed --exit 134 --report 'no result: aborted' --harness 'Error: Unknown: ChildProcess.spawn (opencode)\nAborted'")
	out := ta.ok("tick")
	assert.Contains(t, out, "s1-1.w1 working -> withdrawn gen=2, environment exit=134 ended on m1's environment (no attempt spent); s1-1 working -> ready", out)

	pr := ta.primary("s1-1")
	assert.Equal(t, "1", pr.F("attempt"), "the same attempt")
	assert.Empty(t, pr.F("failed"), "never failed work")
	assert.Equal(t, "Error: Unknown: ChildProcess.spawn (opencode)\nAborted", pr.F(sprint.FieldHarnessTail))
	assert.Contains(t, pr.F(sprint.FieldCostRecord+"s1-1.w1#g1"), "end=environment-exit=134", "the take's attempt record")
	wc := ta.fleetCard("s1-1.w1")
	require.NotNil(t, wc)
	assert.Equal(t, "environment exit=134", wc.F(sprint.FieldEnvExit))
	assert.Empty(t, wc.F(sprint.FieldTakeEnded), "no redeal spent")
	assert.Empty(t, wc.F("redeals"), "the redeal count is unspent")
	assert.Equal(t, "m1", wc.Row, "the member is not held: dealt to it again")

	var q struct{ Cards []queueCard }
	ta.json("queue --as m1", &q)
	require.Len(t, q.Cards, 1, "dealt again: %+v", q.Cards)
	assert.Equal(t, "s1-1.w1", q.Cards[0].ID, "the same work card")
	assert.Equal(t, 1, q.Cards[0].Attempt, "the same attempt number")
	assert.Equal(t, 3, q.Cards[0].Gen, "gen 2 withdrawn by the exit, gen 3 dealt again")
}

// stats --children prints the per-member histogram of child exit codes for the last hour
// from the same attempt records (docs/SPEC-SPRINT.md, "An environment exit"), so the seat
// reads the rate without ssh. Before this card stats had no --children.
func TestStatsChildrenPrintsThePerMemberExitHistogram(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	envExitFleet(ta, []int{134, 137, 0},
		[]string{"no result: a", "no result: b", "no result: c"},
		[]string{"first", "Error: Unknown: ChildProcess.spawn (opencode)\nmore", "third"})
	code, out, errs := ta.do("stats --children")
	require.Equal(t, 0, code, "%s%s", out, errs)
	// env counts the 134/137s, the buckets every environment exit by code
	assert.Equal(t, "CHILD m1 children=10 env=2 rate=20% exit_0=1 exit_134=1 exit_137=1 other=8 status=FAIL last: Error: Unknown: ChildProcess.spawn (opencode)\n", out)

	none := newTestApp(t)
	none.ok("init --readers reader-a --members m1")
	code, out, errs = none.do("stats --children")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Equal(t, "CHILDREN none in the last hour\n", out)
}

// An environment exit that spends nothing is bounded per card per attempt (docs/SPEC-SPRINT.md,
// "An environment exit"; the coordinator, 2026-10-12): the work card records each 134/137 by
// member and code (env_exit_history), and at sprint.EnvExitCardCap (3) the tick raises one
// bound judgment, a kind wait refuses (wait_refused.go), naming the card, the members and the
// exit codes, and deals the card no more until the seat reworks or drops it; a later tick
// raises it no second time (the deal step's own refusal: TestTheDealRefusesACardAtTheEnvironmentExitCap). Before this, a card whose every
// child died 134 was dealt again without a bound.
func TestThreeEnvironmentExitsRaiseTheBoundOnceAndStopTheRedeal(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.ok("tick")
	var takers []string
	for i, exit := range []int{134, 137, 134} {
		wc := ta.fleetCard("s1-1.w1")
		require.NotNil(t, wc)
		require.Equal(t, sprint.Ready, wc.Col, "exit %d: dealt again before the cap", i+1)
		at := "s1-1.w1@" + wc.F("gen")
		ta.ok("take --as " + wc.Row + " " + at)
		ta.ok("finish --as " + wc.Row + " " + at + " --failed --exit " + strconv.Itoa(exit) + " --report 'no result: aborted' --harness 'Error: Unknown: ChildProcess.spawn (opencode)'")
		ta.ok("tick")
		if !slices.Contains(takers, wc.Row) {
			takers = append(takers, wc.Row)
		}
	}
	bounds := func() []sprint.Note {
		t.Helper()
		st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
		require.NoError(t, err)
		notes, _, err := st.B.NotesSince(context.Background(), "", 10000)
		require.NoError(t, err)
		var out []sprint.Note
		for _, n := range notes {
			if n.Kind == sprint.Judgment && n.Type == sprint.NBound && slices.Contains(n.Primaries, "s1-1") {
				out = append(out, n)
			}
		}
		return out
	}
	wc := ta.fleetCard("s1-1.w1")
	require.NotNil(t, wc)
	assert.Equal(t, sprint.Withdrawn, wc.Col, "not dealt a fourth time")
	assert.Equal(t, "1", wc.F("attempt"), "the same attempt throughout")
	got := bounds()
	require.Len(t, got, 1, "one bound judgment: %+v", got)
	assert.Contains(t, got[0].What, "s1-1.w1: attempt 1's child ended on the member's environment 3 times (the cap is 3) and is not dealt again: members "+strings.Join(takers, ", ")+"; exits 134, 137, 134; last: Error: Unknown: ChildProcess.spawn (opencode)")
	assert.NotEmpty(t, sprint.WaitRefusal(got[0]), "a judgment that holds its card: wait is refused")

	ta.ok("tick")
	assert.Equal(t, sprint.Withdrawn, ta.fleetCard("s1-1.w1").Col, "still not dealt")
	assert.Len(t, bounds(), 1, "raised once")
}

// A rework resets the environment-exit count (docs/SPEC-SPRINT.md, "An environment exit"): the
// count lives on the attempt's own work card, so after the bound a rework's new attempt is
// dealt, and its own 134 goes back to ready and is dealt again, under the cap, with the bound
// judgment closed by the rework.
func TestAReworkResetsTheEnvironmentExitCount(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.ok("tick")
	die := func(card string) {
		t.Helper()
		wc := ta.fleetCard(card)
		require.NotNil(t, wc)
		require.Equal(t, sprint.Ready, wc.Col, "%s dealt", card)
		at := card + "@" + wc.F("gen")
		ta.ok("take --as " + wc.Row + " " + at)
		ta.ok("finish --as " + wc.Row + " " + at + " --failed --exit 134 --report 'no result: aborted' --harness 'aborted'")
		ta.ok("tick")
	}
	for range sprint.EnvExitCardCap {
		die("s1-1.w1")
	}
	require.Equal(t, sprint.Withdrawn, ta.fleetCard("s1-1.w1").Col, "at the cap")
	ta.ok("rework s1-1 --fix 'the wall: run outside it'")
	ta.ok("tick")
	require.Equal(t, "2", ta.primary("s1-1").F("attempt"), "a new attempt")
	die("s1-1.w2")
	w2 := ta.fleetCard("s1-1.w2")
	assert.Equal(t, sprint.Ready, w2.Col, "under the cap again: dealt again")
	assert.Equal(t, "m1=134", w2.F(sprint.FieldEnvExitHistory), "the new attempt's count starts at one")
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	read, err := st.DoctorRead(context.Background())
	require.NoError(t, err)
	for _, o := range read.Snap.Open {
		assert.False(t, o.Note.Type == sprint.NBound && slices.Contains(o.Note.Primaries, "s1-1"), "the rework closed the bound: %+v", o.Note)
	}
}
