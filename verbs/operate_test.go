package verbs

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestStopAndStart(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("carol", "stop", "--reason", "deploy"), 0, "STOPPED applied at seq", "next: nova-sprint start --as carol")
	if s := w.snap(t); !s.Stopped || s.StopReason != "deploy" {
		t.Fatalf("snapshot %+v", s)
	}
	want(t, w.do("carol", "stop", "--reason", "again"), 0, "STOPPED already: the sprint is already stopped (deploy)", "next: nova-sprint start --as carol")
	want(t, w.do("carol", "start"), 0, "STARTED applied at seq")
	want(t, w.do("carol", "start"), 0, "RUNNING already: the sprint is already running", "next: nova-sprint next --as carol")
	want(t, w.do("carol", "stop"), 1, "a stop needs its reason")
}

func TestCheck(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	healthy(t, w)
	want(t, w.do("", "check", "--integrity"), 0, "CHECK fleet green", "CHECK nova-tools v2.0.2 (pin v2.0.2)", "CHECK bus answering (5 present)", "CHECK history rebuilds the tables")
}

func TestCheckRefusals(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "the fleet down", args: []string{"check"}, exit: 1, prep: func(_ *testing.T, w *world) { w.doctor.fleet = refuse("the fleet is down", "nova-fleet up") }, want: []string{"the fleet is down: nova-fleet up"}},
		{name: "tools below the pin", args: []string{"check"}, exit: 1, prep: func(_ *testing.T, w *world) { w.doctor.tools = "v2.0.1" }, want: []string{"nova-tools v2.0.1 is below the pin v2.0.2: nova-fleet install --tag v2.0.2 <machine>"}},
		{name: "tools unknown", args: []string{"check"}, exit: 1, prep: func(_ *testing.T, w *world) { w.doctor.tools = "v2.1.0-rc1" }, want: []string{"nova-tools' version is unknown (\"v2.1.0-rc1\" is not a release vMAJOR.MINOR.PATCH): nova-fleet install --tag v2.0.2 <machine>"}},
		{name: "the store not set up", args: []string{"check"}, exit: 1, prep: func(_ *testing.T, w *world) { w.doctor.schema = false }, want: []string{"not at this build's schema", "nova-sprint up"}},
		{name: "the bus fails", args: []string{"check"}, exit: 2, prep: func(_ *testing.T, w *world) { w.bus.err = errBroken }, want: []string{"nova-bus who failed"}},
		{name: "the history differs", args: []string{"check", "--integrity"}, exit: 2, prep: func(_ *testing.T, w *world) { w.doctor.history = errBroken }, want: []string{"connection refused"}},
		{name: "no doctor", args: []string{"check"}, exit: 2, prep: func(_ *testing.T, w *world) { w.e.Doctor = nil }, want: []string{"the doctor is not configured"}},
	})
}

func TestBackup(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.arch.info = BackupInfo{Path: "/b/1.dump", Epoch: 1, Seq: 10}
	want(t, w.do("carol", "backup"), 0, "BACKUP /b/1.dump epoch=1 seq=10 verified")
	w.arch.err = refuse("pg_dump is older than the server", "install pg_dump 17")
	want(t, w.do("carol", "backup"), 1, "pg_dump is older than the server")
	w.e.Archive = nil
	want(t, w.do("carol", "backup"), 2, "backup is not configured")
}

// restored is a store that already holds the dump with digest.
type restored struct {
	*store.Fake
	digest string
}

func (r restored) Restored(context.Context) (string, time.Time, bool, error) {
	return r.digest, t0, true, nil
}

func emptySprint() store.Snapshot {
	return store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{},
		Judgements: map[string]store.Judgement{}, Cursors: map[string]int64{}}
}

func dumped(w *world) {
	w.arch.info = BackupInfo{Path: "/b/1.dump", Epoch: 1, Seq: 10, Verified: &VerifiedDump{digest: "d1"}}
}

func TestRestore(t *testing.T) {
	t.Parallel()
	w := worldFrom(t, emptySprint())
	dumped(w)
	want(t, w.do("dave", "restore", "/b/1.dump"), 0, "RESTORED /b/1.dump epoch=1 seq=10 digest=d1", "HOOKED dave role=coordinator")
	if w.arch.loaded != "d1" {
		t.Fatalf("loaded %q", w.arch.loaded)
	}
	w.e.Store = restored{w.f, "d1"}
	want(t, w.do("dave", "restore", "/b/1.dump"), 0, "(as restored)")
}

// TestRestoreHoldsCardsToAdmission: a restored producer not yet dealt is
// held to today's admission; the restore stands and the line names the card
// and its fix.
func TestRestoreHoldsCardsToAdmission(t *testing.T) {
	t.Parallel()
	s := emptySprint()
	s.Streams["s"] = store.Stream{Name: "s", Repo: "o/r", Base: "main", Mode: "auto"}
	s.Cards["q1"] = store.Card{ID: "q1", Kind: "producer", Stream: "s", State: "ready", Tier: "pro", Priority: "normal", Brief: "tier: pro", Paths: []string{"gone/x.go"}}
	w := worldFrom(t, s)
	dumped(w)
	w.origin.tree = []string{"README.md"}
	want(t, w.do("dave", "restore", "/b/1.dump"), 0, "RESTORED /b/1.dump", "ADMIT RED card q1: PATHS gone/x.go: neither it nor its directory gone/ is there")
	delete(w.origin.tips, "o/r main")
	want(t, w.do("dave", "restore", "/b/1.dump"), 0, "ADMIT RED card q1: stream s: branch main is not at o/r's origin")
}

func TestRestoreRefusals(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "not on the bus", as: "erin", args: []string{"restore", "/b/1.dump"}, exit: 1, prep: func(_ *testing.T, w *world) { dumped(w) }, want: []string{"erin is not on the bus"}},
		{name: "another dump", as: "dave", args: []string{"restore", "/b/1.dump"}, exit: 1, prep: func(_ *testing.T, w *world) {
			dumped(w)
			w.e.Store = restored{w.f, "d0"}
		}, want: []string{"the store holds another dump's data", "nova-sprint clear"}},
		{name: "a run holds the lock", as: "dave", args: []string{"restore", "/b/1.dump"}, exit: 1, snap: func(s *store.Snapshot) { *s = emptySprint() },
			prep: func(_ *testing.T, w *world) { dumped(w); w.arch.lockErr = refuse("a run holds the lock", "stop it") }, want: []string{"a run holds the lock: stop it"}},
		{name: "the dump fails its check", as: "dave", args: []string{"restore", "/b/1.dump"}, exit: 2,
			prep: func(_ *testing.T, w *world) { w.arch.err = errBroken }, want: []string{"connection refused"}},
		{name: "no file", as: "dave", args: []string{"restore"}, exit: 2, want: []string{"restore takes"}},
		{name: "no archive", as: "dave", args: []string{"restore", "/b/1.dump"}, exit: 2, prep: func(_ *testing.T, w *world) { w.e.Archive = nil }, want: []string{"restore is not configured"}},
	})
}

// quiet is the sprint with no live consumers.
func quiet(s *store.Snapshot) {
	delete(s.Cards, "p1.w1")
	delete(s.Cards, "p2.r1")
}

func TestClear(t *testing.T) {
	t.Parallel()
	s := sprint()
	quiet(&s)
	w := worldFrom(t, s)
	w.arch.info = BackupInfo{Path: "/b/1.dump", Epoch: 1, Seq: 10}
	want(t, w.do("carol", "clear", "--confirm", "1", "--dry-run"), 0, "CLEAR epoch=1 cards=2 (dry run)")
	want(t, w.do("carol", "clear", "--confirm", "1"), 0, "CLEARED epoch=2 opened; backup /b/1.dump")
	if got := w.snap(t); got.Epoch != 2 || len(got.Cards) != 0 {
		t.Fatalf("snapshot %+v", got)
	}
}

func TestClearRefusals(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "no confirm", as: "carol", snap: quiet, args: []string{"clear"}, exit: 1, want: []string{"clear confirms the epoch it ends, 1", "nova-sprint clear --confirm 1"}},
		{name: "a wrong epoch", as: "carol", snap: quiet, args: []string{"clear", "--confirm", "2"}, exit: 1, want: []string{"--confirm 1"}},
		{name: "live consumers", as: "carol", args: []string{"clear", "--confirm", "1"}, exit: 1, want: []string{"live consumers: p1.w1@1,p2.r1@1"}},
		{name: "the store moved", as: "carol", snap: quiet, args: []string{"clear", "--confirm", "1"}, exit: 1,
			prep: func(_ *testing.T, w *world) { w.arch.info = BackupInfo{Epoch: 1, Seq: 9} }, want: []string{"the store changed while the backup was made"}},
		{name: "no archive", as: "carol", snap: quiet, args: []string{"clear", "--confirm", "1"}, exit: 2, prep: func(_ *testing.T, w *world) { w.e.Archive = nil }, want: []string{"clear needs one"}},
		{name: "a worker", as: "alice", args: []string{"clear", "--confirm", "1"}, exit: 1, want: []string{"clear is the coordinator's"}},
	})
}

func TestServices(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("", "run"), 0, "RUN ended")
	want(t, w.do("alice", "worker", "--work-root", "/w", "--mirror-root", "/m", "--providers", "deepseek=https://api.example@DEEPSEEK_API_KEY"), 0, "WORKER alice ended")
	want(t, w.do("", "dash", "--listen", "127.0.0.1:0"), 0, "DASH ended")
	if !slices.Equal(w.svc.ran, []string{"run", "worker alice", "dash"}) {
		t.Fatalf("ran %v", w.svc.ran)
	}
}

// A friend's row is hooked by nova-bus, so its machine runs the worker with
// --beat-only: the flag reaches the service.
func TestWorkerBeatOnlyReachesTheService(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("alice", "worker", "--beat-only"), 0, "WORKER alice ended")
	if !slices.Equal(w.svc.ran, []string{"worker alice", "beat-only"}) {
		t.Fatalf("ran %v", w.svc.ran)
	}
}

func TestServicesRefusals(t *testing.T) {
	t.Parallel()
	none := func(_ *testing.T, w *world) { w.e.Services = nil }
	runCases(t, []tcase{
		{name: "a worker with no name", args: []string{"worker", "--work-root", "/w"}, exit: 2, want: []string{"worker takes --as <row>"}},
		{name: "a worker given a removed flag", as: "alice", args: []string{"worker", "--coordinator-hosts", "c1"}, exit: 2, want: []string{"unknown flag --coordinator-hosts"}},
		{name: "a dash with no listen", args: []string{"dash"}, exit: 2, want: []string{"dash takes --listen <addr>"}},
		{name: "no run", args: []string{"run"}, exit: 2, prep: none, want: []string{"run is not configured"}},
		{name: "no worker", as: "alice", args: []string{"worker"}, exit: 2, prep: none, want: []string{"worker is not configured"}},
		{name: "no dash", args: []string{"dash", "--listen", "x"}, exit: 2, prep: none, want: []string{"dash is not configured"}},
	})
}
