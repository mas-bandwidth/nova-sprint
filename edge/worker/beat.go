package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// BeatEvery is how often a worker pushes its heartbeat while it runs (a
// guess, not measured: the board's load cell is for a human's eye, and a
// beat that repeats the last one writes nothing).
const BeatEvery = 30 * time.Second

type (
	// Heartbeat is what a bench says it carries: its 1-minute load average,
	// its logical cores, and its nova-reserve slots in use and in all (0 of 0
	// when the slots could not be read). It is the board's load cell.
	Heartbeat struct {
		Load1                        float64
		Cores, SlotsUsed, SlotsTotal int
	}
	// Beater pushes a heartbeat to the sprint (CLISprintClient is one).
	Beater interface {
		Beat(ctx context.Context, row string, hb Heartbeat) error
	}
	// Meter measures the bench. A failure to read the load is an error and
	// no beat; a failure to read the slots is the beat with 0 of 0 and the
	// error beside it (BeatReading).
	Meter interface {
		Sample(ctx context.Context) (Heartbeat, error)
	}
	// OSMeter measures the real bench: the load average from the kernel
	// (/proc/loadavg on Linux, sysctl vm.loadavg on macOS and the BSDs) and
	// the slots from `nova-reserve status`.
	OSMeter struct {
		Exec       edge.Exec
		ReserveBin string
		ReadFile   func(string) ([]byte, error) // os.ReadFile when nil
	}
)

var reserveFree = regexp.MustCompile(`(?m)^free=(\d+)/(\d+)\s*$`)

// Sample reads the load, then the slots. The slots' failure comes back with
// the beat the load made, so a bench without nova-reserve still reports.
func (m OSMeter) Sample(ctx context.Context) (Heartbeat, error) {
	load, err := m.load1_internal(ctx)
	if err != nil {
		return Heartbeat{}, err
	}
	hb := Heartbeat{Load1: load, Cores: runtime.NumCPU()}
	res, err := cmp.Or[edge.Exec](m.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: []string{resolveToolBin_internal(cmp.Or(m.ReserveBin, "nova-reserve")), "status"}, Timeout: 5 * time.Second})
	if err != nil || res.ExitCode != 0 {
		return hb, fmt.Errorf("nova-reserve status: %w", edge.Failure([]string{"nova-reserve", "status"}, nil, res.ExitCode, res.Stdout, res.Stderr, err))
	}
	used, total, perr := ParseReserveStatus(res.Stdout)
	hb.SlotsUsed, hb.SlotsTotal = used, total
	return hb, perr
}

func (m OSMeter) load1_internal(ctx context.Context) (float64, error) {
	switch runtime.GOOS {
	case "linux":
		read := m.ReadFile
		if read == nil {
			read = os.ReadFile
		}
		b, err := read("/proc/loadavg")
		if err != nil {
			return 0, fmt.Errorf("the load average: %w", err)
		}
		return ParseLoad1(string(b))
	case "darwin", "freebsd", "openbsd", "netbsd":
		res, err := cmp.Or[edge.Exec](m.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: []string{"sysctl", "-n", "vm.loadavg"}, Timeout: 5 * time.Second})
		if err != nil || res.ExitCode != 0 {
			return 0, fmt.Errorf("sysctl vm.loadavg: %w", edge.Failure([]string{"sysctl", "-n", "vm.loadavg"}, nil, res.ExitCode, res.Stdout, res.Stderr, err))
		}
		return ParseLoad1(res.Stdout)
	}
	return 0, fmt.Errorf("the load average is not read on %s: this bench shows no load on the board", runtime.GOOS)
}

// ParseLoad1 is the 1-minute load average in /proc/loadavg's form
// ("0.52 0.58 0.59 1/467 12345") or sysctl vm.loadavg's ("{ 0.52 0.58 0.59 }").
func ParseLoad1(s string) (float64, error) {
	f := strings.Fields(strings.Trim(strings.TrimSpace(s), "{}"))
	if len(f) == 0 {
		return 0, errors.New("the load average is empty")
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, fmt.Errorf("the load average %q is not a number: %w", edge.RedactN(f[0], 64), err)
	}
	if v < 0 {
		return 0, fmt.Errorf("the load average %q is below 0", edge.RedactN(f[0], 64))
	}
	return v, nil
}

// ParseReserveStatus is the slots in use and in all from the last line of
// `nova-reserve status` ("free=5/8").
func ParseReserveStatus(out string) (used, total int, err error) {
	m := reserveFree.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, fmt.Errorf("nova-reserve status has no free=n/N line: %q", edge.RedactN(strings.TrimSpace(out), 120))
	}
	free, _ := strconv.Atoi(m[1])
	total, _ = strconv.Atoi(m[2])
	if free > total {
		return 0, 0, fmt.Errorf("nova-reserve status says free=%d/%d", free, total)
	}
	return total - free, total, nil
}

// beat_internal samples the bench and pushes one heartbeat. Everything that
// goes wrong is a warning (the board shows the last beat), said once per
// distinct cause so a down store is one line, not one per period.
func (r *Runner) beat_internal(ctx context.Context, row string) {
	hb, merr := r.Meter.Sample(ctx)
	var serr error
	if hb.Cores > 0 {
		serr = r.Beater.Beat(ctx, row, hb)
	}
	for _, err := range []error{merr, serr} {
		if err != nil && err.Error() != r.lastBeatErr {
			r.lastBeatErr = err.Error()
			r.report("heartbeat", err)
		}
	}
	if merr == nil && serr == nil {
		r.lastBeatErr = ""
	}
}

// beatLoop_internal pushes a heartbeat now and then every BeatEvery until
// ctx ends. It is not run without a Meter. Time is the runner's Sleep.
func (r *Runner) beatLoop_internal(ctx context.Context, row string) {
	for ctx.Err() == nil {
		r.beat_internal(ctx, row)
		r.Sleep(r.beatEvery_internal())
	}
}

func (r *Runner) beatEvery_internal() time.Duration { return cmp.Or(r.BeatEvery, BeatEvery) }
