package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// CPUWindow is how far back the beat looks for the CPU busy peak it carries:
// the highest one-second busy percent of the last CPUWindow, across all the
// bench's cores (100 is every core busy).
const CPUWindow = 10 * time.Second

type (
	// cpuTicks is one reading of the kernel's cumulative CPU counters: busy
	// is every tick but idle and iowait, total is every tick.
	cpuTicks struct{ busy, total uint64 }

	cpuSample struct {
		at  time.Time
		pct float64
	}

	// CPUSampler keeps the busy percent of each second of the last CPUWindow.
	// The percent comes from Stat (the text of /proc/stat: the change in its
	// counters since the last Step) or, where there is no /proc/stat, from
	// Second (a meter that takes its own second, as top on macOS does). Time
	// is the caller's: Step and Peak take it. Safe for the goroutine that
	// steps it and the one that reads it.
	CPUSampler struct {
		Stat   func() (string, error)
		Second func(context.Context) (float64, error)

		mu      sync.Mutex
		prev    *cpuTicks
		samples []cpuSample
	}
)

// parseProcStat is the aggregate cpu line of /proc/stat: user nice system idle
// iowait irq softirq steal. guest and guest_nice are left out: the kernel
// already counts them in user and nice.
func parseProcStat(s string) (cpuTicks, bool) {
	for line := range strings.Lines(s) {
		f := strings.Fields(line)
		if len(f) < 9 || f[0] != "cpu" {
			continue
		}
		var v [8]uint64
		for i := range v {
			n, err := strconv.ParseUint(f[i+1], 10, 64)
			if err != nil {
				return cpuTicks{}, false
			}
			v[i] = n
		}
		var t cpuTicks
		for _, n := range v {
			t.total += n
		}
		t.busy = t.total - v[3] - v[4]
		return t, true
	}
	return cpuTicks{}, false
}

// busyPercent is the busy share of the ticks between two readings; false when
// the counters did not move forward (the same reading, or a reboot between).
func busyPercent(prev, cur cpuTicks) (float64, bool) {
	if cur.total <= prev.total || cur.busy < prev.busy {
		return 0, false
	}
	return float64(cur.busy-prev.busy) / float64(cur.total-prev.total) * 100, true
}

// parseTopCPUInterval is the busy percent of macOS top's last "CPU usage:" line
// when top took two samples (`top -l 2`): the second line is the interval
// between them, the first the average since boot.
func parseTopCPUInterval(s string) (float64, error) {
	var idle string
	n := 0
	for line := range strings.Lines(s) {
		if _, rest, ok := strings.Cut(line, "CPU usage:"); ok {
			n++
			idle = ""
			for _, part := range strings.Split(rest, ",") {
				if w, ok := strings.CutSuffix(strings.TrimSpace(part), "% idle"); ok {
					idle = w
				}
			}
		}
	}
	v, err := strconv.ParseFloat(idle, 64)
	if err != nil {
		return 0, edge.Cause("top gave no idle percent on its last CPU usage line", err)
	}
	if n < 2 {
		return 0, errors.New("top gave no second CPU usage line")
	}
	return clampPercent(100 - v), nil
}

func clampPercent(p float64) float64 {
	if math.IsNaN(p) || p < 0 {
		return 0
	}
	return min(p, 100)
}

// Step takes one reading at now and, when it gives a percent, keeps it and drops
// what is older than CPUWindow. With Stat the first Step only records the
// counters.
func (s *CPUSampler) Step(ctx context.Context, now time.Time) {
	var pct float64
	switch {
	case s.Stat != nil:
		text, err := s.Stat()
		if err != nil {
			return
		}
		cur, ok := parseProcStat(text)
		if !ok {
			return
		}
		s.mu.Lock()
		prev := s.prev
		s.prev = &cur
		s.mu.Unlock()
		if prev == nil {
			return
		}
		if pct, ok = busyPercent(*prev, cur); !ok {
			return
		}
	case s.Second != nil:
		var err error
		if pct, err = s.Second(ctx); err != nil {
			return
		}
	default:
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.samples[:0]
	for _, x := range s.samples {
		if now.Sub(x.at) < CPUWindow {
			keep = append(keep, x)
		}
	}
	s.samples = append(keep, cpuSample{at: now, pct: clampPercent(pct)})
}

// Peak is the highest percent kept within CPUWindow before now; false when
// none is.
func (s *CPUSampler) Peak(now time.Time) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peak, ok := 0.0, false
	for _, x := range s.samples {
		if d := now.Sub(x.at); d >= 0 && d < CPUWindow {
			peak, ok = max(peak, x.pct), true
		}
	}
	return peak, ok
}

// Run steps once a second until ctx ends, on the clock and the wait it is given.
// A Second meter takes its own second, so it is not waited on again.
func (s *CPUSampler) Run(ctx context.Context, now func() time.Time, sleep func(time.Duration)) {
	for ctx.Err() == nil {
		s.Step(ctx, now())
		if s.Second == nil {
			sleep(time.Second)
		}
		if s.Stat == nil && s.Second == nil {
			return
		}
	}
}

// cpuPeakOrLoad is the CPU cell of a beat: the sampler's peak when it has one,
// else (the first second of a worker, or a bench with no CPU counters) the
// 1-minute load average over the cores as a percent, capped at 100.
func cpuPeakOrLoad(peak float64, ok bool, load1 float64, cores int) float64 {
	if ok {
		return peak
	}
	if cores < 1 {
		return 0
	}
	return clampPercent(load1 / float64(cores) * 100)
}

// NewOSCPUSampler is the sampler of this bench: /proc/stat on Linux, top on
// macOS (two samples a second apart, the second the interval), and a sampler
// that never has a sample elsewhere, so the beat falls back to the load average.
func NewOSCPUSampler(ex edge.Exec) *CPUSampler {
	switch runtime.GOOS {
	case "linux":
		return &CPUSampler{Stat: func() (string, error) {
			b, err := os.ReadFile("/proc/stat")
			return string(b), err
		}}
	case "darwin":
		return &CPUSampler{Second: func(ctx context.Context) (float64, error) {
			res, err := cmp.Or[edge.Exec](ex, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: []string{"/usr/bin/top", "-l", "2", "-n", "0", "-s", "1"}, Timeout: 5 * time.Second})
			if err != nil {
				return 0, edge.Cause("top: no CPU usage", err)
			}
			if res.ExitCode != 0 {
				return 0, fmt.Errorf("top: no CPU usage: exit %d", res.ExitCode)
			}
			return parseTopCPUInterval(res.Stdout)
		}}
	}
	return &CPUSampler{}
}
