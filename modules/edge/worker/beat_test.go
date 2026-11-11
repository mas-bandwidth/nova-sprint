package worker

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestParseLoad1(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]float64{"0.52 0.58 0.59 1/467 12345\n": 0.52, "{ 14.12 16.17 15.94 }\n": 14.12, "3 2 1": 3} {
		got, err := ParseLoad1(in)
		if err != nil || got != want {
			t.Errorf("ParseLoad1(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "fast 1 2", "-1 0 0"} {
		if _, err := ParseLoad1(in); err == nil {
			t.Errorf("ParseLoad1(%q) accepted", in)
		}
	}
}

func TestParseReserveStatus(t *testing.T) {
	t.Parallel()
	out := "capacity=8 set_by_pid=1\nslot1 held pid=2\nslot2 free\ngate free # held by an --exclusive run\nall free\nfree=5/8\n"
	if used, total, err := ParseReserveStatus(out); err != nil || used != 3 || total != 8 {
		t.Fatalf("used %d of %d, %v; want 3 of 8", used, total, err)
	}
	for _, in := range []string{"", "slot1 free\n", "free=9/8\n"} {
		if _, _, err := ParseReserveStatus(in); err == nil {
			t.Errorf("ParseReserveStatus(%q) accepted", in)
		}
	}
}

// beatExec answers the two programs the meter runs, whatever the OS.
type beatExec struct{ reserve, sysctl string }

func (e beatExec) Run(_ context.Context, c edge.Cmd) (edge.Result, error) {
	switch {
	case strings.HasSuffix(c.Argv[0], "nova-reserve"):
		return edge.Result{Stdout: e.reserve}, nil
	case c.Argv[0] == "sysctl":
		return edge.Result{Stdout: e.sysctl}, nil
	}
	return edge.Result{ExitCode: 127, Stderr: "no such program " + c.Argv[0]}, nil
}

func TestOSMeterSample(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("no load average on windows")
	}
	t0 := time.Unix(1000, 0)
	cpu := &CPUSampler{Second: func(context.Context) (float64, error) { return 42.5, nil }}
	cpu.Step(t.Context(), t0)
	m := OSMeter{
		CPU: cpu, Now: func() time.Time { return t0.Add(2 * time.Second) },
		Exec:       beatExec{reserve: "capacity=4\nfree=1/4\n", sysctl: "{ 2.50 1.00 1.00 }"},
		ReserveBin: "/bin/nova-reserve",
		ReadFile:   func(string) ([]byte, error) { return []byte("2.50 1.00 1.00 1/9 7\n"), nil },
	}
	hb, err := m.Sample(t.Context())
	want := Heartbeat{Load1: 2.5, CPUPeak: 42.5, Cores: runtime.NumCPU(), SlotsUsed: 3, SlotsTotal: 4}
	if err != nil || hb != want {
		t.Fatalf("Sample = %+v, %v; want %+v", hb, err, want)
	}
	m.Exec = beatExec{reserve: "garbage", sysctl: "{ 2.50 1.00 1.00 }"}
	hb, err = m.Sample(t.Context())
	if err == nil || hb.Load1 != 2.5 || hb.SlotsTotal != 0 {
		t.Fatalf("a bench with no slots still reports its load: %+v, %v", hb, err)
	}
}

type beatRecorder struct {
	beats []Heartbeat
	err   error
}

func (b *beatRecorder) Beat(_ context.Context, row string, hb Heartbeat) error {
	b.beats = append(b.beats, hb)
	return b.err
}

type meterFake struct {
	hb  Heartbeat
	err error
}

func (m meterFake) Sample(context.Context) (Heartbeat, error) { return m.hb, m.err }

// The beat loop pushes at once and then once per period of the runner's own
// Sleep, never faster, until its context ends.
func TestBeatLoopBeatsEveryPeriod(t *testing.T) {
	t.Parallel()
	rec := &beatRecorder{}
	ctx, cancel := context.WithCancel(t.Context())
	var slept []time.Duration
	r := &Runner{Meter: meterFake{hb: Heartbeat{Load1: 1, Cores: 4, SlotsUsed: 1, SlotsTotal: 4}}, Beater: rec, Warn: func(string) {},
		Sleep: func(d time.Duration) {
			slept = append(slept, d)
			if len(slept) == 3 {
				cancel()
			}
		}}
	r.beatLoop_internal(ctx, "b1")
	if len(rec.beats) != 3 || slept[0] != BeatEvery {
		t.Fatalf("beats %d, slept %v; want 3 beats a BeatEvery apart", len(rec.beats), slept)
	}
}

// A failure is one warning per distinct cause, and a meter that read no
// cores at all sends no beat.
func TestBeatWarnsOncePerCause(t *testing.T) {
	t.Parallel()
	var warns []string
	rec := &beatRecorder{err: errors.New("worker: store unreachable")}
	r := &Runner{Meter: meterFake{hb: Heartbeat{Load1: 1, Cores: 4}}, Beater: rec, Warn: func(s string) { warns = append(warns, s) }}
	for range 3 {
		r.beat_internal(t.Context(), "b1")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "heartbeat") || !strings.Contains(warns[0], "store unreachable") {
		t.Fatalf("warnings: %q", warns)
	}
	rec.err = nil
	r.beat_internal(t.Context(), "b1")
	rec.err = errors.New("worker: store unreachable")
	r.beat_internal(t.Context(), "b1")
	if len(warns) != 2 {
		t.Fatalf("a cause that clears and returns is said again: %q", warns)
	}
	r.Meter = meterFake{err: errors.New("no load")}
	sent := len(rec.beats)
	r.beat_internal(t.Context(), "b1")
	if len(rec.beats) != sent || len(warns) != 3 {
		t.Fatalf("a meter with no cores sends no beat and says why: %d beats, %q", len(rec.beats)-sent, warns)
	}
}

// The real client's beat is `nova-sprint beat` with the numbers as flags, and
// a sprint that refuses it is ErrRefused.
func TestCLISprintClientBeatArgv(t *testing.T) {
	t.Parallel()
	var got []string
	cli := &CLISprintClient{Run: func(_ context.Context, argv []string) ([]byte, int, error) {
		got = argv
		return []byte("BEAT b1 applied"), 0, nil
	}}
	if err := cli.Beat(t.Context(), "b1", Heartbeat{Load1: 2.5, CPUPeak: 87.25, Cores: 8, SlotsUsed: 2, SlotsTotal: 8}); err != nil {
		t.Fatal(err)
	}
	if want := "nova-sprint beat --as b1 --load1 2.50 --cpu-peak 87.2 --cores 8 --slots-used 2 --slots-total 8"; strings.Join(got, " ") != want {
		t.Fatalf("argv %q, want %q", strings.Join(got, " "), want)
	}
	cli.Run = func(context.Context, []string) ([]byte, int, error) {
		return []byte("SPRINT REFUSED beat: hook in first"), 1, nil
	}
	if err := cli.Beat(t.Context(), "b1", Heartbeat{Load1: 1, Cores: 1}); !errors.Is(err, ErrRefused) {
		t.Fatalf("a refused beat: %v", err)
	}
}

// A worker process hands its own build to each verb it runs (BuildEnv), so
// the beat reports the process, not the nova-sprint on disk.
func TestCLISprintClientHandsItsBuildToTheBeat(t *testing.T) {
	t.Parallel()
	fx := edge.NewFakeExec()
	fx.On("*", func(edge.FakeCall) edge.FakeOutcome { return edge.FakeOutcome{Stdout: "BEAT b1 applied"} })
	cli := &CLISprintClient{Exec: fx, Version: "v2.0.36"}
	if err := cli.Beat(t.Context(), "b1", Heartbeat{Load1: 1, Cores: 8}); err != nil {
		t.Fatal(err)
	}
	calls := fx.Calls()
	if len(calls) != 1 || !slices.Equal(calls[0].Cmd.Env, []string{BuildEnv + "=v2.0.36"}) || calls[0].Cmd.CleanEnv {
		t.Fatalf("calls %+v", calls)
	}
}

// A beat through the store's own client lands in the row the board reads.
func TestBeatReachesTheRow(t *testing.T) {
	t.Parallel()
	fs := store.NewFake()
	if _, err := fs.Apply(t.Context(), store.Op{ID: "hook", Epoch: 1, Actor: "b1", Kind: "hook", At: time.Unix(1000, 0).UTC(), Writes: []store.Write{store.PutRow{Name: "b1", Session: "s", Role: "worker"}}}); err != nil {
		t.Fatal(err)
	}
	c := &StoreSprintClient{Store: fs}
	if err := c.Beat(t.Context(), "b1", Heartbeat{Load1: 1.26, Cores: 4, SlotsUsed: 1, SlotsTotal: 4}); err != nil {
		t.Fatal(err)
	}
	snap, _ := fs.Snapshot(t.Context())
	if r := snap.Rows["b1"]; r.Load1Centi != 126 || r.Cores != 4 || r.SlotsUsed != 1 || r.SlotsTotal != 4 {
		t.Fatalf("row %+v", r)
	}
}

// A bench whose load average cannot be read still beats: its cores, slots
// and tiers are what the deal needs, so the load goes as unknown (0, as
// unread slots go as 0 of 0) and the worker says why once.
func TestBeatWithoutLoadStillBeats(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("no load average on windows")
	}
	var warns []string
	rec := &beatRecorder{}
	r := &Runner{Beater: rec, Warn: func(s string) { warns = append(warns, s) }, advertised: "small,medium",
		Meter: OSMeter{
			Exec:       beatExec{reserve: "capacity=4\nfree=1/4\n", sysctl: "not a load"},
			ReserveBin: "/bin/nova-reserve",
			ReadFile:   func(string) ([]byte, error) { return nil, errors.New("permission denied") },
		}}
	for range 2 {
		r.beat_internal(t.Context(), "b1")
	}
	want := Heartbeat{Cores: runtime.NumCPU(), SlotsUsed: 3, SlotsTotal: 4, Tiers: "small,medium"}
	if len(rec.beats) != 2 || rec.beats[0] != want || rec.beats[1] != want {
		t.Fatalf("beats %+v; want two of %+v", rec.beats, want)
	}
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "WORKER WARNING heartbeat") || !strings.Contains(warns[0], "load average") {
		t.Fatalf("warnings %q; want one WORKER WARNING naming the load average", warns)
	}
}
