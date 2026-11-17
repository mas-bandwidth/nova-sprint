package worker

import (
	"cmp"
	"context"
	"time"
)

// BeatEvery is how often a worker pushes its heartbeat while it runs (R48:
// row liveness, harness tiers, and internal CPU peak health only).
const BeatEvery = 30 * time.Second

type (
	// Heartbeat carries only internal CPU peak health (R40) and harness tiers
	// (R48: zero machine resource shadow ledger on beat).
	Heartbeat struct {
		// CPUPeak is the highest CPU busy percent of the last CPUWindow, across all
		// the bench's cores (100: every core busy).
		CPUPeak float64
		// Tiers are what the harness serves, a comma list in store.Tiers' order.
		Tiers string
	}
	// Beater pushes a heartbeat to the sprint (CLISprintClient is one).
	Beater interface {
		Beat(ctx context.Context, row string, hb Heartbeat) error
	}
	// Meter samples internal bench CPU health.
	Meter interface {
		Sample(ctx context.Context) (Heartbeat, error)
	}
	// OSMeter reads the bench's CPUSampler peak for R40 health checks.
	OSMeter struct {
		CPU *CPUSampler
		Now func() time.Time
	}
)

// Sample reads the CPU busy peak over CPUWindow when CPU is configured.
func (m OSMeter) Sample(context.Context) (Heartbeat, error) {
	var hb Heartbeat
	if m.CPU != nil {
		now := time.Now
		if m.Now != nil {
			now = m.Now
		}
		if peak, ok := m.CPU.Peak(now()); ok {
			hb.CPUPeak = peak
		}
	}
	return hb, nil
}

// beat_internal samples the bench and pushes one heartbeat, logging each
// distinct failure once per cause.
func (r *Runner) beat_internal(ctx context.Context, row string) {
	var hb Heartbeat
	var merr, serr error
	if r.Meter != nil {
		hb, merr = r.Meter.Sample(ctx)
	}
	hb.Tiers = r.advertised
	if merr == nil && r.Beater != nil {
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

// beatLoop_internal pushes a heartbeat now and then every BeatEvery until ctx ends.
func (r *Runner) beatLoop_internal(ctx context.Context, row string) {
	for ctx.Err() == nil {
		r.beat_internal(ctx, row)
		if r.After != nil {
			select {
			case <-ctx.Done():
				return
			case <-r.After(r.beatEvery_internal()):
			}
		} else {
			r.Sleep(r.beatEvery_internal())
		}
	}
}

// advertise_internal records the probed harness tiers and sends the initial
// heartbeat before the first pass so the roster deals the row by its real tiers.
func (r *Runner) advertise_internal(ctx context.Context, row string) {
	r.mu.Lock()
	r.advertised = r.tiers[row]
	r.mu.Unlock()
	if r.Meter != nil && r.Beater != nil {
		r.beat_internal(ctx, row)
	}
}

func (r *Runner) beatEvery_internal() time.Duration { return cmp.Or(r.BeatEvery, BeatEvery) }
