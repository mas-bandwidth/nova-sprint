package worker

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestOSMeterSample(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1000, 0)
	cpu := &CPUSampler{Second: func(context.Context) (float64, error) { return 42.5, nil }}
	if err := cpu.Step(t.Context(), t0); err != nil {
		t.Fatal(err)
	}
	m := OSMeter{
		CPU: cpu,
		Now: func() time.Time { return t0.Add(2 * time.Second) },
	}
	hb, err := m.Sample(t.Context())
	want := Heartbeat{CPUPeak: 42.5}
	if err != nil || !reflect.DeepEqual(hb, want) {
		t.Fatalf("Sample = %+v, %v; want %+v", hb, err, want)
	}
}

type beatRecorder struct {
	beats []Heartbeat
	err   error
}

func (b *beatRecorder) Beat(_ context.Context, _ string, hb Heartbeat) error {
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
	r := &Runner{Meter: meterFake{hb: Heartbeat{CPUPeak: 25}}, Beater: rec, Warn: func(string) {},
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

// A failure is one warning per distinct cause, and a failed meter sample sends no beat.
func TestBeatWarnsOncePerCause(t *testing.T) {
	t.Parallel()
	var warns []string
	rec := &beatRecorder{err: errors.New("worker: store unreachable")}
	r := &Runner{Meter: meterFake{hb: Heartbeat{CPUPeak: 10}}, Beater: rec, Warn: func(s string) { warns = append(warns, s) }}
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
	r.Meter = meterFake{err: errors.New("cpu sample failed")}
	sent := len(rec.beats)
	r.beat_internal(t.Context(), "b1")
	if len(rec.beats) != sent || len(warns) != 3 {
		t.Fatalf("a failed meter sends no beat and says why: %d beats, %q", len(rec.beats)-sent, warns)
	}
}

// The real client's beat is `nova-sprint beat --as <row> --cpu-peak <pct> [--tiers <t>]`,
// with zero machine resource shadow ledger flags (R48).
func TestCLISprintClientBeatArgv(t *testing.T) {
	t.Parallel()
	var got []string
	cli := &CLISprintClient{Run: func(_ context.Context, argv []string) ([]byte, int, error) {
		got = argv
		return []byte("BEAT b1 applied"), 0, nil
	}}
	if err := cli.Beat(t.Context(), "b1", Heartbeat{CPUPeak: 87.25, Tiers: "flash,pro"}); err != nil {
		t.Fatal(err)
	}
	if want := "nova-sprint beat --as b1 --cpu-peak 87.2 --tiers flash,pro"; strings.Join(got, " ") != want {
		t.Fatalf("argv %q, want %q", strings.Join(got, " "), want)
	}
	cli.Run = func(context.Context, []string) ([]byte, int, error) {
		return []byte("SPRINT REFUSED beat: hook in first"), 1, nil
	}
	if err := cli.Beat(t.Context(), "b1", Heartbeat{CPUPeak: 10}); !errors.Is(err, ErrRefused) {
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
	if err := cli.Beat(t.Context(), "b1", Heartbeat{CPUPeak: 12.5}); err != nil {
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
	if err := c.Beat(t.Context(), "b1", Heartbeat{CPUPeak: 45.6, Tiers: "flash,pro"}); err != nil {
		t.Fatal(err)
	}
	snap, _ := fs.Snapshot(t.Context())
	if r := snap.Rows["b1"]; r.CPUPeakDeci != 456 || r.Tiers != "flash,pro" {
		t.Fatalf("row %+v", r)
	}
}
