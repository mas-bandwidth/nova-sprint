package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseProcStatAndBusyPercent(t *testing.T) {
	t.Parallel()
	// user nice system idle iowait irq softirq steal guest guest_nice
	a, ok := parseProcStat("cpu  100 0 50 800 50 0 0 0 20 0\ncpu0 1 2 3 4 5 6 7 8 9 10\n")
	if !ok || a.total != 1000 || a.busy != 150 {
		t.Fatalf("a = %+v, %v; want busy 150 of 1000 (idle and iowait are not busy, guest is already in user)", a, ok)
	}
	b, _ := parseProcStat("cpu  200 0 100 900 50 0 0 0 40 0\n")
	if p, ok := busyPercent(a, b); !ok || p != 60 {
		t.Fatalf("busy %v, %v; want 60 (150 of 250 new ticks)", p, ok)
	}
	if _, ok := busyPercent(b, a); ok {
		t.Fatal("counters that went back are not a percent")
	}
	if _, ok := busyPercent(a, a); ok {
		t.Fatal("the same reading is not a percent")
	}
	for _, in := range []string{"", "cpu0 1 2 3 4 5 6 7 8\n", "cpu  1 2 3 x 5 6 7 8\n", "cpu  1 2 3\n"} {
		if _, ok := parseProcStat(in); ok {
			t.Errorf("parseProcStat(%q) accepted", in)
		}
	}
}

func TestParseTopCPUInterval(t *testing.T) {
	t.Parallel()
	two := "Processes: 1\nCPU usage: 20.00% user, 10.00% sys, 70.00% idle\nPhysMem: 1G\nProcesses: 1\nCPU usage: 40.00% user, 12.50% sys, 47.50% idle\n"
	if p, err := parseTopCPUInterval(two); err != nil || p != 52.5 {
		t.Fatalf("interval %v, %v; want 52.5 (the second line, not the average since boot)", p, err)
	}
	for _, in := range []string{"", "CPU usage: 20.00% user, 10.00% sys, 70.00% idle\n", "CPU usage: x\nCPU usage: y\n"} {
		if _, err := parseTopCPUInterval(in); err == nil {
			t.Errorf("parseTopCPUInterval(%q) accepted", in)
		}
	}
}

// The peak is the highest second of the last CPUWindow: a spike that is older
// than the window is gone, and a second in it is not.
func TestCPUSamplerPeakIsTheHighestOfTheLastTenSeconds(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1000, 0)
	pcts := []float64{10, 90, 20, 30}
	i := 0
	s := &CPUSampler{Second: func(context.Context) (float64, error) { i++; return pcts[i-1], nil }}
	if _, ok := s.Peak(t0); ok {
		t.Fatal("a peak before any sample")
	}
	for k := range pcts {
		s.Step(t.Context(), t0.Add(time.Duration(k)*time.Second))
	}
	at := func(sec int) float64 { p, ok := s.Peak(t0.Add(time.Duration(sec) * time.Second)); _ = ok; return p }
	if got := at(3); got != 90 {
		t.Fatalf("peak at +3s = %v, want 90", got)
	}
	if got := at(10); got != 90 {
		t.Fatalf("peak at +10s = %v, want 90 (the spike at +1s is 9s old)", got)
	}
	if got := at(11); got != 30 {
		t.Fatalf("peak at +11s = %v, want 30 (the spike at +1s is 10s old and out)", got)
	}
	if _, ok := s.Peak(t0.Add(30 * time.Second)); ok {
		t.Fatal("a peak from samples all older than the window")
	}
}

// With /proc/stat the first Step only records the counters; each later one is
// the busy share of the ticks since; a failed read adds nothing.
func TestCPUSamplerFromProcStat(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1000, 0)
	texts := []string{"cpu 0 0 0 100 0 0 0 0\n", "cpu 50 0 0 150 0 0 0 0\n", "", "cpu 50 0 0 250 0 0 0 0\n"}
	i := 0
	s := &CPUSampler{Stat: func() (string, error) {
		i++
		if texts[i-1] == "" {
			return "", errors.New("unreadable")
		}
		return texts[i-1], nil
	}}
	for k := range texts {
		s.Step(t.Context(), t0.Add(time.Duration(k)*time.Second))
	}
	if p, ok := s.Peak(t0.Add(3 * time.Second)); !ok || p != 50 {
		t.Fatalf("peak %v, %v; want 50 (50 busy of 100 new ticks; the idle-only second is 0)", p, ok)
	}
}

func TestCPUPeakOrLoad(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		peak  float64
		ok    bool
		load1 float64
		cores int
		want  float64
	}{{87.5, true, 1, 8, 87.5}, {0, true, 9, 8, 0}, {0, false, 2, 8, 25}, {0, false, 20, 8, 100}, {0, false, 2, 0, 0}} {
		if got := cpuPeakOrLoad(c.peak, c.ok, c.load1, c.cores); got != c.want {
			t.Errorf("cpuPeakOrLoad(%v, %v, %v, %d) = %v, want %v", c.peak, c.ok, c.load1, c.cores, got, c.want)
		}
	}
}
