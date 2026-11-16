package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// BeatEvery is how often a worker pushes its heartbeat while it runs (a
// guess, not measured: the board's load cell is for a human's eye, and a
// beat that repeats the last one writes nothing).
const BeatEvery = 30 * time.Second

type (
	// Heartbeat is what a bench says it carries: its 1-minute load average,
	// its logical cores, its measured resources and holds from nova-reserve (R48),
	// and its legacy slots in use and in all.
	Heartbeat struct {
		Load1 float64
		// CPUPeak is the highest CPU busy percent of the last CPUWindow, across all
		// the bench's cores (100: every core busy), the board's CPU cell.
		CPUPeak                      float64
		Cores, SlotsUsed, SlotsTotal int
		// Tiers are what the harness serves (its adapter's Needs.Tiers),
		// a comma list in store.Tiers' order: the roster deals by them.
		Tiers     string
		Resources store.RowResources
	}
	// Beater pushes a heartbeat to the sprint (CLISprintClient is one).
	Beater interface {
		Beat(ctx context.Context, row string, hb Heartbeat) error
	}
	// Meter measures the bench. What it could not read comes back as the
	// error beside the beat: an unread load as load 0, unread slots as 0 of
	// 0, so the cores and tiers the deal needs are still sent.
	Meter interface {
		Sample(ctx context.Context) (Heartbeat, error)
	}
	// OSMeter measures the real bench: the load average from the kernel
	// (/proc/loadavg on Linux, sysctl vm.loadavg on macOS and the BSDs) and
	// the resources and slots from `nova-reserve status --json`.
	OSMeter struct {
		Exec       edge.Exec
		ReserveBin string
		ReserveDir string
		ReadFile   func(string) ([]byte, error) // os.ReadFile when nil
		// CPU is the sampler whose peak the beat carries (nil: the load
		// average over the cores stands in for it); Now is the clock it is
		// read at (time.Now when nil).
		CPU           *CPUSampler
		Now           func() time.Time
		PauseTracker  *PauseTracker
		OnStateChange func()
	}
)

type reserveStatusJSON struct {
	Tags            []string                  `json:"tags"`
	UsableCores     int                       `json:"usable_cores"`
	Resources       map[string]int            `json:"resources"`
	HeldSprint      map[string]int            `json:"held_sprint"`
	HeldOther       map[string]int            `json:"held_other"`
	HeldByLabel     map[string]map[string]int `json:"held_by_label"`
	PausedByLabel   map[string]map[string]int `json:"paused_by_label"`
	AnyPaused       bool                      `json:"any_paused"`
	SprintConsumers []string                  `json:"sprint_consumers"`
	PausedConsumers []string                  `json:"paused_consumers"`
}

var reserveFree = regexp.MustCompile(`(?m)^free=(\d+)/(\d+)\s*$`)

// Sample reads the load, then nova-reserve status (--json).
func (m OSMeter) Sample(ctx context.Context) (Heartbeat, error) {
	load, lerr := m.load1_internal(ctx)
	hb := Heartbeat{Load1: load, Cores: runtime.NumCPU()}
	peak, ok := 0.0, false
	if m.CPU != nil {
		now := time.Now
		if m.Now != nil {
			now = m.Now
		}
		peak, ok = m.CPU.Peak(now())
	}
	hb.CPUPeak = cpuPeakOrLoad(peak, ok, load, hb.Cores)

	statusArgv := []string{resolveToolBin_internal(cmp.Or(m.ReserveBin, "nova-reserve")), "status", "--json"}
	if m.ReserveDir != "" {
		statusArgv = append(statusArgv, "--dir", m.ReserveDir)
	} else {
		statusArgv = append(statusArgv, "--dir", DefaultReserveDir)
	}
	res, err := cmp.Or[edge.Exec](m.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: statusArgv, Timeout: 5 * time.Second})
	if err != nil || res.ExitCode != 0 {
		// Fallback to plain status if --json failed (e.g. older mocks in unit tests)
		plainArgv := []string{resolveToolBin_internal(cmp.Or(m.ReserveBin, "nova-reserve")), "status"}
		if m.ReserveDir != "" {
			plainArgv = append(plainArgv, "--dir", m.ReserveDir)
		}
		resPlain, errPlain := cmp.Or[edge.Exec](m.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: plainArgv, Timeout: 5 * time.Second})
		if errPlain != nil || resPlain.ExitCode != 0 {
			return hb, errors.Join(lerr, fmt.Errorf("nova-reserve status: %w", edge.Failure([]string{"nova-reserve", "status"}, nil, res.ExitCode, res.Stdout, res.Stderr, err)))
		}
		used, total, perr := ParseReserveStatus(resPlain.Stdout)
		hb.SlotsUsed, hb.SlotsTotal = used, total
		return hb, errors.Join(lerr, perr)
	}

	var st reserveStatusJSON
	if jerr := json.Unmarshal([]byte(res.Stdout), &st); jerr == nil && (st.Resources != nil || len(st.Tags) > 0) {
		hb.Resources = store.RowResources{
			Total:           st.Resources,
			HeldSprint:      st.HeldSprint,
			HeldOther:       st.HeldOther,
			HeldByLabel:     st.HeldByLabel,
			PausedByLabel:   st.PausedByLabel,
			AnyPaused:       st.AnyPaused,
			SprintConsumers: st.SprintConsumers,
			PausedConsumers: st.PausedConsumers,
		}
		if st.UsableCores > 0 {
			hb.Cores = st.UsableCores
		}
		now := time.Now()
		if m.Now != nil {
			now = m.Now()
		}
		if m.PauseTracker != nil {
			changed := m.PauseTracker.UpdatePausedConsumers(st.PausedConsumers, now)
			hb.Resources.PausedMillis = m.PauseTracker.Snapshot(now)
			if changed && m.OnStateChange != nil {
				m.OnStateChange()
			}
		}
	} else {
		used, total, perr := ParseReserveStatus(res.Stdout)
		hb.SlotsUsed, hb.SlotsTotal = used, total
		return hb, errors.Join(lerr, perr)
	}
	return hb, lerr
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
	hb.Tiers = r.advertised
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

// TriggerBeat triggers an immediate edge-triggered heartbeat push.
func (r *Runner) TriggerBeat() {
	if r.beatWake != nil {
		select {
		case r.beatWake <- struct{}{}:
		default:
		}
	}
}

// beatLoop_internal pushes a heartbeat now and then every BeatEvery until
// ctx ends, waking immediately on edge triggers (R48).
func (r *Runner) beatLoop_internal(ctx context.Context, row string) {
	for ctx.Err() == nil {
		r.beat_internal(ctx, row)
		if r.beatWake == nil {
			r.Sleep(r.beatEvery_internal())
			continue
		}
		select {
		case <-r.beatWake:
			continue
		default:
		}
		if r.After != nil {
			select {
			case <-ctx.Done():
				return
			case <-r.beatWake:
				continue
			case <-r.After(r.beatEvery_internal()):
			}
		} else {
			r.Sleep(r.beatEvery_internal())
		}
	}
}

// advertise_internal is the worker's advertisement at hook (defect 74): the
// tiers its probe proved its harness serves (a tier whose policy models the
// adapter cannot reach is not among them) go in every heartbeat with the
// nova-reserve slots, and the first beat is pushed now, before the first
// pass, so the roster deals the row by its real width and tiers from its
// first tick. Without a Meter it sends none (the roster keeps nova-config's
// shape for the row).
func (r *Runner) advertise_internal(ctx context.Context, row string) {
	r.mu.Lock()
	r.advertised = r.tiers[row]
	r.mu.Unlock()
	if r.Meter != nil && r.Beater != nil {
		r.beat_internal(ctx, row)
	}
}

func (r *Runner) beatEvery_internal() time.Duration { return cmp.Or(r.BeatEvery, BeatEvery) }
