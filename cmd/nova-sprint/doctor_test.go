package main

import (
	"encoding/json"
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
