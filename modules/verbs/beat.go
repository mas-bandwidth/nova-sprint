package verbs

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runBeat is `beat`, the worker's heartbeat (INTENT 7, push: the bench says
// what it is carrying; nobody asks it): the bench's 1-minute load average,
// the highest CPU busy percent of its last 10 seconds (--cpu-peak, across all
// cores, 100 is every core busy; the board's CPU cell), its logical cores, its nova-reserve slots in use and in all, and the
// tiers its harness serves (--tiers, defect 74), and this build's version (the
// binary that takes the row's cards; defect D3). It sets the row's load
// columns, which the board's load cell reads and the roster takes the row's
// width and tiers from (roster.Advertised); it moves nothing else. A beat that repeats the last one is a success that writes
// nothing, so a quiet bench makes no events.
func runBeat(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	w, f := beatWrite(in)
	if f != nil {
		return "", f
	}
	w.Version = e.Version // the build that takes this row's cards: the tick deals a row behind the run's release nothing (defect D3)
	if r := snap.Rows[in.actor]; r.Load1Centi == w.Load1Centi && r.Cores == w.Cores && r.SlotsUsed == w.SlotsUsed && r.SlotsTotal == w.SlotsTotal && r.CPUPeakDeci == w.CPUPeakDeci && r.Tiers == w.Tiers && r.Version == w.Version {
		return done(waitNext, "BEAT "+in.actor+" unchanged"), nil
	}
	op := e.newOp("beat", in.actor, snap, w)
	op.Writes = []store.Write{w}
	r, err := e.commit(ctx, op, "nova-sprint beat --as "+in.actor+" again")
	if err != nil {
		return "", err
	}
	return done(waitNext, fmt.Sprintf("BEAT %s applied at seq %d", in.actor, r.Seq)), nil
}

// beatWrite is the beat's flags as the store's write for the actor's row.
func beatWrite(in *input) (store.SetRowLoad, *Fail) {
	w := store.SetRowLoad{Row: in.actor}
	load, err := strconv.ParseFloat(in.str("load1"), 64)
	if err != nil || math.IsNaN(load) || math.IsInf(load, 0) || load < 0 || load > 1e6 {
		return w, misuse("beat", "--load1 takes the 1-minute load average as a number, 0 or more ("+badNumber(in.str("load1"), err)+")")
	}
	w.Load1Centi = int(math.Round(load * 100))
	if s := in.str("cpu-peak"); s != "" {
		peak, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(peak) || peak < 0 || peak > 100 {
			return w, misuse("beat", "--cpu-peak takes the CPU busy percent as a number from 0 to 100 ("+badNumber(s, err)+")")
		}
		w.CPUPeakDeci = int(math.Round(peak * 10))
	}
	for _, c := range []struct {
		flag string
		to   *int
		min  int
	}{{"cores", &w.Cores, 1}, {"slots-used", &w.SlotsUsed, 0}, {"slots-total", &w.SlotsTotal, 0}} {
		n, err := strconv.Atoi(in.str(c.flag))
		if err != nil || n < c.min || n > 1_000_000 {
			return w, misuse("beat", fmt.Sprintf("--%s takes a whole number, %d or more (%s)", c.flag, c.min, badNumber(in.str(c.flag), err)))
		}
		*c.to = n
	}
	if w.SlotsUsed > w.SlotsTotal {
		return w, misuse("beat", fmt.Sprintf("--slots-used %d is more than --slots-total %d", w.SlotsUsed, w.SlotsTotal))
	}
	names := store.SplitTiers(in.str("tiers"))
	for _, t := range names {
		if !slices.Contains(store.Tiers, t) {
			return w, misuse("beat", "--tiers takes a comma list of "+strings.Join(store.Tiers, ", ")+" (got "+in.str("tiers")+")")
		}
	}
	w.Tiers = store.JoinTiers(names)
	return w, nil
}
