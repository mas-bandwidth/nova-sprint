package verbs

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runBeat is `beat`, the worker's heartbeat (R48: row liveness, internal CPU
// peak health for R40, probed harness tiers, and the worker binary's version;
// zero machine resource shadow ledger). A beat that repeats the last one writes
// nothing, so a quiet bench makes no events.
func runBeat(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	w, f := beatWrite(in)
	if f != nil {
		return "", f
	}
	w.Version = cmp.Or(e.WorkerBuild, e.Version)
	if r := snap.Rows[in.actor]; r.CPUPeakDeci == w.CPUPeakDeci && r.Tiers == w.Tiers && r.Version == w.Version {
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

// beatWrite parses the beat's flags into store.SetRowLoad for the actor's row.
func beatWrite(in *input) (store.SetRowLoad, *Fail) {
	w := store.SetRowLoad{Row: in.actor}
	if s := in.str("cpu-peak"); s != "" {
		peak, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(peak) || math.IsInf(peak, 0) || peak < 0 || peak > 100 {
			return w, misuse("beat", "--cpu-peak takes the CPU busy percent as a number from 0 to 100 ("+badNumber(s, err)+")")
		}
		w.CPUPeakDeci = int(math.Round(peak * 10))
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
