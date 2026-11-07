package verbs

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// runBeat is `beat`, the worker's heartbeat (INTENT 7, push: the bench says
// what it is carrying; nobody asks it): the bench's 1-minute load average,
// its logical cores and its nova-reserve slots in use and in all. It sets
// the row's load columns, which the board's load cell reads; it moves
// nothing else. A beat that repeats the last one is a success that writes
// nothing, so a quiet bench makes no events.
func runBeat(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	w, f := beatWrite(in)
	if f != nil {
		return "", f
	}
	if r := snap.Rows[in.actor]; r.Load1Centi == w.Load1Centi && r.Cores == w.Cores && r.SlotsUsed == w.SlotsUsed && r.SlotsTotal == w.SlotsTotal {
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
	return w, nil
}
