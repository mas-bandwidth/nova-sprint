package run

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Health is the chooser's check: the advisory lock's session and the
// sprint hook are both alive.
type Health interface{ Check(context.Context) error }

// Asking is layer 5 as run calls it (policy.Engine).
type Asking interface {
	AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure)
}

// TickCounts are one tick's numbers, for the tick line.
type TickCounts struct{ Folded, Applied, Refused, Edges int }

// Ticker is run's tick (LAYER-6 section 2): one steps View built once at
// the cold start and folded after, the folded snapshot beside it for the
// pusher and the board, and what layer 3 needs between ticks. Ticks are
// serial: Tick is called by one goroutine.
type Ticker struct {
	store      store.Store
	health     Health
	policy     Asking
	view       *steps.View
	snap       store.Snapshot
	inbox      []steps.InboxItem
	inflight   map[string]steps.EdgeRequest
	refused    []steps.RefusedOp
	seen       map[string]time.Time
	last       time.Time // the last tick's Now
	prev       int
	counts     TickCounts
	push       *Pusher
	landings   []time.Time // the epoch's landings (not the v1 import's), from the events run folds
	landingsOK bool
	statsMark  int64           // the stats cursor (store.StatsCursor) in the folded snapshot, 0 when unset
	stats      store.StatsMark // the consumers done after it (StatsSince), while statsOK
	statsOK    bool
	limits     policy.Limits
	timeouts   policy.Timeouts
	kin        kinLife
}

// Limits and Timeouts are the last answered tick's (the active policy's):
// run reads the roster at Limits.FleetWidthCap and bounds a tick by
// Timeouts.Tick. Before the first answer they are zero; Serve seeds them.
func (t *Ticker) Limits() policy.Limits     { return t.limits }
func (t *Ticker) Timeouts() policy.Timeouts { return t.timeouts }

// Push sends this tick's envelopes (Pusher.Push) and returns the
// recipients whose send failed.
func (t *Ticker) Push(ctx context.Context, b bus.Bus, now time.Time) []PushFailure {
	failed := t.push.Push(ctx, b, t.store, t.snap.Epoch, now)
	for _, f := range failed {
		t.kin.failed(f.To)
	}
	return failed
}

// judgementOp is the tick's policy judgements (policy.Judgements): one per
// failing version that did not answer (the answering version's failed
// questions are the steps' policy judgements, one each; a row the roster
// does not name is the steps'), as one op on the snapshot the questions
// were asked on; its id is that seq's, so a replay applies it once.
func judgementOp(a *asker, o Observed) (store.Op, bool) {
	js := policy.Judgements(a.snap, o.Roster, slices.DeleteFunc(slices.Clone(a.failures), func(f policy.Failure) bool { return f.Version == a.answers.Version }))
	if len(js) == 0 {
		return store.Op{}, false
	}
	op := store.Op{ID: fmt.Sprintf("judge.policy.%d.%d", a.snap.Epoch, a.snap.Seq), Epoch: a.snap.Epoch, Actor: "sprint", Kind: "judgement", At: o.Now}
	for _, j := range js {
		op.Writes = append(op.Writes, j)
	}
	return op, true
}

// RosterUnread opens the engine judgement for a failed roster read, once
// while one is open: the tick planned nothing (an empty roster would judge
// every row unknown), so nothing is dealt until nova-config reads again. It
// carries the judgement deadline and expires there (steps' judgements). The
// refusals the last plan left are kept for the next plan.
func (t *Ticker) RosterUnread(ctx context.Context, now time.Time, cause error) error {
	n := 1
	for _, j := range t.snap.Judgements {
		if j.Kind != "engine" || j.About != "roster" {
			continue
		}
		if j.State == "open" {
			return nil
		}
		n++
	}
	tmo := t.timeouts.Judgement
	if tmo <= 0 {
		tmo = steps.Engine.Timeouts.Judgement
	}
	id := fmt.Sprintf("engine.roster.%d", n)
	op := store.Op{ID: fmt.Sprintf("judge.engine.roster.%d.%d", t.snap.Epoch, t.snap.Seq), Epoch: t.snap.Epoch, Actor: "sprint", Kind: "judgement", At: now,
		Writes: []store.Write{store.PutJudgement{ID: id, Kind: "engine", About: "roster", Group: "engine",
			Question: "the roster read from nova-config failed, so nothing is planned or dealt: " + edge.RedactN(cause.Error(), edge.FailureMax),
			Command:  "nova-sprint judge " + id + " <choice> --reason <why>   choices: retry", Deadline: now.Add(time.Duration(tmo) * time.Second)}}}
	refused := t.refused
	_, err := t.apply(ctx, []store.Op{op}, false)
	t.refused = refused
	return err
}

// NewTicker reads the one cold snapshot; later reads are Events only
// (LAYER-1 "Scale"; no walk from seq 0, drift row 23).
func NewTicker(ctx context.Context, st store.Store, h Health, p Asking, now time.Time) (*Ticker, error) {
	if err := h.Check(ctx); err != nil {
		return nil, err
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("run: cold snapshot: %w", err)
	}
	t := &Ticker{store: st, health: h, policy: p, view: steps.Build(snap, now), snap: snapshotCopy(snap),
		inflight: map[string]steps.EdgeRequest{}, seen: map[string]time.Time{}, push: NewPusher()}
	if err := witness(ctx, st, t.view, now); err != nil {
		return nil, err
	}
	// the epoch's landing times for the board's ETA, up to the cold snapshot's
	// seq; a store that cannot answer leaves them unknown (the ETA is "-")
	if r, ok := st.(interface {
		Landings(ctx context.Context, epoch int, upToSeq int64) ([]time.Time, error)
	}); ok {
		if at, err := r.Landings(ctx, snap.Epoch, snap.Seq); err == nil {
			t.landings, t.landingsOK = append([]time.Time{}, at...), true
		}
	}
	t.statsMark = snap.Cursors[store.StatsCursor]
	t.readStats(ctx)
	return t, nil
}

// witness folds the epoch's events up to the view's seq into the facts a
// snapshot does not carry (when a consumer was dealt, a producer went
// ready, a stream landed, a row went down, a row's last finishes), so a restart
// dates every bound as the run before it did instead of from now. The
// epoch's first event is found by bisection on single-event reads: the
// epoch is read, never a walk from seq 0 (drift row 23).
func witness(ctx context.Context, st store.Store, v *steps.View, now time.Time) error {
	epochOf := func(after int64) (int, error) { // the epoch of the first event above after
		es, err := st.Events(ctx, after, 1)
		if err != nil || len(es) == 0 {
			return v.Epoch, err
		}
		return es[0].Epoch, nil
	}
	lo, hi := int64(0), v.Seq // the epoch's events begin above lo
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		e, err := epochOf(mid - 1)
		if err != nil {
			return fmt.Errorf("run: witnesses: %w", err)
		}
		if e < v.Epoch {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	w := steps.Build(store.Snapshot{Epoch: v.Epoch}, now)
	for after := lo; after < v.Seq; {
		events, err := st.Events(ctx, after, store.MaxEvents)
		if err != nil {
			return fmt.Errorf("run: witnesses: %w", err)
		}
		if len(events) == 0 {
			break
		}
		full := len(events) == store.MaxEvents
		after = events[len(events)-1].Seq
		w.Fold(slices.DeleteFunc(events, func(e store.Event) bool { return e.Seq > v.Seq }))
		if !full {
			break
		}
	}
	v.TakenAt, v.ReadyAt, v.Landed, v.Down, v.Ends = w.TakenAt, w.ReadyAt, w.Landed, w.Down, w.Ends
	return nil
}

// Landings is the epoch's landing times as run has folded them, the caller's
// to keep; nil when the store could not give them.
func (t *Ticker) Landings() []time.Time {
	if !t.landingsOK {
		return nil
	}
	return append([]time.Time{}, t.landings...)
}

// readStats reads the stats mark at statsMark from the store's events (none
// when 0); a store that cannot answer leaves it unread, and the next fold
// reads again. Until then the board counts the epoch's.
func (t *Ticker) readStats(ctx context.Context) {
	t.statsOK = false
	r, ok := t.store.(interface {
		StatsSince(ctx context.Context, mark int64) (store.StatsMark, error)
	})
	if t.statsMark == 0 || !ok {
		return
	}
	if m, err := r.StatsSince(ctx, t.statsMark); err == nil {
		t.stats, t.statsOK = m, true
	}
}

// Stats is the stats reset mark as run has folded it, the caller's to keep;
// nil when none is set or it is not read yet.
func (t *Ticker) Stats() *store.StatsMark {
	if t.statsMark == 0 || !t.statsOK {
		return nil
	}
	m := t.stats
	m.Done = maps.Clone(t.stats.Done)
	return &m
}

// Snapshot is run's folded copy, the caller's to keep.
func (t *Ticker) Snapshot() store.Snapshot { return snapshotCopy(t.snap) }

// Counts are the last tick's numbers.
func (t *Ticker) Counts() TickCounts { return t.counts }

// Fold reads the events since the last fold, in pages, into the View and
// the copy. A clear (a new epoch) drops everything layer 3 carried for the
// old one: the inbox, the requests in flight and the refusals (Stella's
// finding 2: an old effect never reaches the new epoch).
func (t *Ticker) Fold(ctx context.Context) error {
	for {
		events, err := t.store.Events(ctx, t.snap.Seq, store.MaxEvents)
		if err != nil {
			return fmt.Errorf("run: fold: %w", err)
		}
		if len(events) == 0 {
			return nil
		}
		epoch := t.snap.Epoch
		t.view.Fold(events)
		for _, e := range events {
			if t.landingsOK {
				if e.Kind == "clear" {
					t.landings = nil
				}
				for i := store.LandedIn(e); i > 0; i-- {
					t.landings = append(t.landings, e.At)
				}
			}
			t.snap = store.Fold(t.snap, []store.Event{e})
			if m := t.snap.Cursors[store.StatsCursor]; m != t.statsMark {
				t.statsMark, t.statsOK = m, false
			} else if t.statsOK {
				t.stats.Add(e)
			}
			t.push.Observe(e, t.snap)
			t.kin.fold(e)
			if p, ok := policy.PrevOf(e); ok {
				t.prev = p
			}
		}
		if !t.statsOK {
			t.readStats(ctx) // a new mark, or one the last read could not give: from the store, to the log's end
		}
		if t.snap.Epoch != epoch {
			t.inbox, t.refused, t.inflight = nil, nil, map[string]steps.EdgeRequest{}
		}
		t.counts.Folded += len(events)
		if len(events) < store.MaxEvents {
			return nil
		}
	}
}

// Observed is what run read outside the store this tick: who is on the bus
// (as row names), the roster, the stream heads, and the edge results that
// finished since the last tick.
type Observed struct {
	Now     time.Time
	Present []string
	Unread  bool // the who read failed: Present is unknown, not empty
	Roster  roster.Roster
	Heads   map[string]string
	Results []steps.InboxItem
	Version string // the run's release: a worker row behind it is dealt nothing (roster.Advertised); "" pins nothing
	// Unadmitted is each producer the latest admission proof of its stream
	// refused, and why (Admission.Unadmitted).
	Unadmitted map[string]string
	// Bases is each watched repository and base's last completed CI read
	// (BaseWatch.Read).
	Bases map[string]steps.BaseCI
}

// Tick is one tick: check the lock, fold, and, unless the sprint is stopped,
// plan with layer 3 and apply the plan's ops in order (LAYER-6 section 2).
// It returns the edge requests to dispatch: those whose After op was
// accepted. While stopped it plans and applies nothing and returns none
// (drift row 4; Run.tla NoTickAfterObservedStop): the results that arrive
// wait in the inbox for the start. Only raiseSeen runs while stopped: a
// row's presence moves no card (Store.tla: an op not fenced on Running
// lands while stopped), and up's steps 9-10 wait on a fresh, never-started
// store for the hooked rows to come up.
func (t *Ticker) Tick(ctx context.Context, o Observed) ([]steps.EdgeRequest, error) {
	t.counts = TickCounts{}
	t.inbox = append(t.inbox, t.fresh(o.Results)...) // kept even if this tick ends early
	if err := t.health.Check(ctx); err != nil {
		return nil, err
	}
	if err := t.Fold(ctx); err != nil {
		return nil, err
	}
	if o.Unread && !t.last.IsZero() { // a failed read is no evidence of silence: every row's silence holds across it
		for name, at := range t.seen {
			t.seen[name] = at.Add(o.Now.Sub(t.last))
		}
	}
	t.last = o.Now
	for _, name := range o.Present {
		t.seen[name] = o.Now
	}
	challenges := t.kin.observe(t.snap.Rows, o.Roster, o.Present, o.Now) // kin answer while stopped too
	if err := t.raiseSeen(ctx, o); err != nil {                          // presence deals nothing, so it runs while stopped too
		return nil, err
	}
	if t.snap.Stopped {
		t.push.Nudge(challenges)
		return nil, nil
	}
	o.Roster = o.Roster.Advertised(t.snap.Rows, o.Version) // a worker's own width and tiers (defect 74), none when its binary is behind the run's (D3)
	in := steps.Inputs{Now: o.Now, Inbox: t.inbox, Inflight: t.requests(), Heads: maps.Clone(o.Heads),
		Seen: maps.Clone(t.seen), Alive: t.kin.evidence(), Refused: t.refused, Roster: o.Roster, Unadmitted: o.Unadmitted, Bases: o.Bases}
	a := t.asker(ctx, o.Roster)
	plan := steps.Tick(t.view, in, a)
	t.push.Nudge(append(plan.Nudges, challenges...))
	if a.asked {
		t.limits, t.timeouts = a.answers.Limits, a.answers.Timeouts
	}
	ops := plan.Ops
	if j, ok := judgementOp(a, o); ok {
		ops = append(ops, j)
	}
	accepted, err := t.apply(ctx, ops, true)
	if err != nil {
		return nil, err
	}
	t.retire(plan, accepted)
	return t.dispatch(plan.Edges, accepted), nil
}

// Seen says whether any row has been seen on the bus since the cold start.
func (t *Ticker) Seen() bool { return len(t.seen) > 0 }

// raiseSeen is the run loop's Seen (steps' runEvents: presence is the bus's
// fact; Steps.tla EnvMove's flip up): each row down in the store and present
// on the bus this tick comes up, one op each, applied before the plan so a
// row that has just hooked in is open for this tick's deal. Gone, the flip
// down, is the steps' silence rule. Without this a hooked row stayed down
// and nothing was ever dealt (the end-to-end test, 2026-11-07). A kin row
// comes up only on a pong since it last went down (kinLife): its bus hook
// can be present with no session behind it, so presence alone raises none.
func (t *Ticker) raiseSeen(ctx context.Context, o Observed) error {
	var ops []store.Op
	for _, name := range o.Present {
		if r, ok := t.snap.Rows[name]; !ok || r.State != card.Down {
			continue
		}
		if _, isKin := o.Roster.Kin[name]; isKin && !t.kin.answeredSince(name, t.view.Down[name], o.Now) {
			continue
		}
		op, why := steps.Verb(t.view, steps.Inputs{Now: o.Now}, steps.Request{Actor: steps.Actor, Row: name, Event: card.Seen{}})
		if why != nil {
			continue
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return nil
	}
	_, err := t.apply(ctx, ops, false)
	return err
}

// fresh is results not already in the inbox and for a request in flight
// (dedup by id within the epoch; a result for no request is a repeat).
func (t *Ticker) fresh(results []steps.InboxItem) []steps.InboxItem {
	var out []steps.InboxItem
	for _, r := range results {
		dup := slices.ContainsFunc(t.inbox, func(i steps.InboxItem) bool { return i.ID == r.ID })
		if _, ok := t.inflight[r.Request]; ok && !dup {
			out = append(out, r)
		}
	}
	return out
}

func (t *Ticker) requests() []steps.EdgeRequest {
	out := make([]steps.EdgeRequest, 0, len(t.inflight))
	for _, k := range slices.Sorted(maps.Keys(t.inflight)) {
		out = append(out, t.inflight[k])
	}
	return out
}

// apply applies ops in order, one Apply each, folding after each. A refused
// op is kept for the next tick's Inputs.Refused. An unknown outcome is
// replayed by id before anything else (layer 1: an op id applies once).
func (t *Ticker) apply(ctx context.Context, ops []store.Op, tick bool) (map[string]bool, error) {
	t.refused = nil
	accepted := map[string]bool{}
	for _, op := range ops {
		if err := t.health.Check(ctx); err != nil {
			return accepted, err
		}
		// A stop that commits after the fold refuses a tick op
		// (store.Running; Run.tla NoAcceptedTickWhileStopped).
		if tick {
			op.Expect = append(slices.Clone(op.Expect), store.Running{})
		}
		r, err := t.store.Apply(ctx, op)
		if errors.Is(err, store.ErrUnknown) {
			var found bool
			r, found, err = t.store.Replay(ctx, op.ID)
			if err == nil && !found {
				err = fmt.Errorf("run: op %s: outcome unknown and not in the log: %w", op.ID, store.ErrUnknown)
			}
		}
		if err != nil {
			return accepted, fmt.Errorf("run: apply %s: %w", op.ID, err)
		}
		if r.Accepted {
			accepted[op.ID] = true
			t.counts.Applied++
		} else {
			t.counts.Refused++
			t.refused = append(t.refused, steps.RefusedOp{ID: op.ID, Reason: r.Reason, Groups: steps.OpGroups(op)})
		}
		if err := t.Fold(ctx); err != nil {
			return accepted, err
		}
	}
	return accepted, nil
}

// retire sets the next inbox: the plan's carry first, then each item the
// plan grouped into a refused op (it is answered again next tick); an item
// neither carried nor in a refused op was consumed, and its request leaves
// flight.
func (t *Ticker) retire(plan steps.Plan, accepted map[string]bool) {
	refusedItem := map[string]bool{}
	for _, op := range plan.Ops {
		if accepted[op.ID] {
			continue
		}
		for _, g := range steps.OpGroups(op) {
			refusedItem[g.Item] = g.Item != ""
		}
	}
	carried := map[string]bool{}
	next := slices.Clone(plan.Carry)
	for _, c := range plan.Carry {
		carried[c.ID] = true
	}
	for _, it := range t.inbox {
		switch {
		case carried[it.ID]:
		case refusedItem[it.ID]:
			next = append(next, it)
		default:
			delete(t.inflight, it.Request)
		}
	}
	t.inbox = next
}

// dispatch is the plan's edge requests whose After op was accepted, now in
// flight; one whose After op was refused is not kept (steps plan it again by
// the same id). A dispatched request leaves flight when a result for it is
// consumed; a stop whose send failed has a fault result and is not reissued
// (its withdrawal landed; the row learns of it from its WITHDRAWN notice).
func (t *Ticker) dispatch(edges []steps.EdgeRequest, accepted map[string]bool) []steps.EdgeRequest {
	var out []steps.EdgeRequest
	for _, e := range edges {
		if _, busy := t.inflight[e.ID]; busy || (e.After != "" && !accepted[e.After]) {
			continue
		}
		t.inflight[e.ID] = e
		out = append(out, e)
	}
	t.counts.Edges = len(out)
	return out
}
