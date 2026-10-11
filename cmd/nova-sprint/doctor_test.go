package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
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
			p := sprint.MemberProbe{Member: m.Name, Host: "nova@" + m.Name, Reached: true, OS: "Linux", PathFrom: "loop", Tools: map[string]string{}, FreeKB: 100 << 20,
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
		p := sprint.MemberProbe{Member: m, Host: "nova@" + m, Reached: true, OS: "Linux", PathFrom: "loop", Tools: map[string]string{}, FreeKB: 100 << 20,
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
func TestDoctorProbeParsesItsLines(t *testing.T) {
	t.Parallel()
	var p sprint.MemberProbe
	parseProbe(&p, []byte("os Linux\npathfrom loop\ntool git /usr/bin/git\ntool sqlite3 -\ntool go -\nnear go /home/nova/sdk/go1.26.6/bin/go\nfree 900000000\npush PUSH-CREDENTIAL OK seat=swarm-b1\nsecrets 0 CHECK OK\nversion nova-sprint v1.2.6 linux/amd64 go1.26.6\n"))
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
}
