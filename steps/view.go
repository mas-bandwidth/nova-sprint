package steps

import (
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// View is the sprint as the tick reads it: the snapshot's records plus the
// indexes the steps need (producers by state and rung, consumers by
// producer and row, dependants by need, live paths by stream, the counters),
// advanced by Fold. After any fold it equals a fresh Build at the same seq
// (TestViewFoldEqualsBuild). The facts a snapshot does not carry (when a
// consumer was taken, when a stream last landed, when a row went down, how a
// row's consumers ended) are witnessed by the fold and are zero for records
// that predate the view; every bound they feed is then measured from Since.
//
// The pending sets are what changed since the last tick (guarantee 11): the
// waiting producers whose chance to resolve moved (a need landed, a stream
// was lifted, an overlapping producer left its paths, a card became
// waiting), and the failed producers whose dependants are owed a drop. A
// tick reads them and clears what it settled; a cold Build pends every
// waiting producer once.
type View struct {
	Seq    int64
	Epoch  int
	Policy int
	Since  time.Time // Build's now: the base of every bound no event dates
	Built  int64     // the seq of the snapshot the view was built from

	Stopped    bool // the sprint is stopped: a tick plans nothing (rule 19)
	StopReason string

	Streams    map[string]store.Stream
	Cards      map[string]store.Card
	Rows       map[string]store.Row
	Judgements map[string]store.Judgement

	byState    map[string]set            // producers and sentinels by state
	byRung     map[string]set            // producers and sentinels by priority
	consOf     map[string]set            // consumers by producer
	liveOnRow  map[string]set            // live (ready, working) consumers by row
	dependants map[string]set            // producers whose needs name the key
	paths      map[string]map[string]set // stream -> path -> producers ready or started
	waitPaths  map[string]map[string]set // stream -> path -> waiting producers
	waitingIn  map[string]set            // stream -> waiting producers
	bounced    set                       // producers in ready or review with a bounce
	count      map[string]map[string]int // stream -> producer state -> count
	cleanLand  map[string]int            // stream -> landed with no bounce

	resolvePending set // waiting producers whose chance to resolve moved
	dropPending    set // failed producers whose dependants are owed a drop

	TakenAt map[string]time.Time // consumer -> when it went working
	Landed  map[string]time.Time // stream -> when a producer of it last landed
	Down    map[string]time.Time // row -> when it last went down
	Ended   map[string][]ending  // row -> how its consumers ended, oldest first (the last endedKept)
	born    map[string]int64     // card -> the seq that put it (0: before the view)
	touched map[string]int64     // card -> the seq that last wrote it (0: before the view)
}

// ending is one consumer's end on a row: when, and whether it was a fault.
type ending struct {
	At    time.Time
	Fault bool
}

// endedKept is how many endings a row keeps: the rest window's bound.
const endedKept = 100

type set = map[string]struct{}

// started is the producer states that hold their paths against an overlap:
// ready and the started ones (working, review, merging).
var started = []string{"ready", "working", "review", "merging"}

// Build is the view of snapshot s at now.
func Build(s store.Snapshot, now time.Time) *View {
	v := &View{Since: now, Built: s.Seq}
	v.reset(s.Epoch)
	v.Seq, v.Policy = s.Seq, s.Policy
	v.Stopped, v.StopReason = s.Stopped, s.StopReason
	for _, st := range s.Streams {
		v.Streams[st.Name] = st
	}
	for _, r := range s.Rows {
		v.Rows[r.Name] = r
	}
	for _, j := range s.Judgements {
		v.Judgements[j.ID] = j
	}
	for _, c := range s.Cards {
		v.Cards[c.ID] = c
		v.index(c)
	}
	for p := range v.byState["waiting"] {
		v.resolvePending[p] = struct{}{}
	}
	for p := range v.byState["fail"] {
		if len(v.dependants[p]) > 0 {
			v.dropPending[p] = struct{}{}
		}
	}
	return v
}

func (v *View) reset(epoch int) {
	v.Epoch = epoch
	v.Streams, v.Cards = map[string]store.Stream{}, map[string]store.Card{}
	v.Rows, v.Judgements = map[string]store.Row{}, map[string]store.Judgement{}
	v.byState, v.byRung, v.consOf = map[string]set{}, map[string]set{}, map[string]set{}
	v.liveOnRow, v.dependants = map[string]set{}, map[string]set{}
	v.paths, v.waitPaths = map[string]map[string]set{}, map[string]map[string]set{}
	v.waitingIn, v.bounced = map[string]set{}, set{}
	v.count, v.cleanLand = map[string]map[string]int{}, map[string]int{}
	v.resolvePending, v.dropPending = set{}, set{}
	v.TakenAt, v.Landed, v.Down = map[string]time.Time{}, map[string]time.Time{}, map[string]time.Time{}
	v.Ended, v.born, v.touched = map[string][]ending{}, map[string]int64{}, map[string]int64{}
}

// Fold advances the view by events, in seq order: an accepted event lays its
// after-images over the records and the indexes follow; a clear starts the
// new epoch empty. A refused event moves only Seq.
func (v *View) Fold(events []store.Event) {
	for _, e := range events {
		v.Seq = e.Seq
		if !e.Accepted || e.After == nil {
			continue
		}
		if e.After.Epoch != 0 {
			v.reset(e.After.Epoch)
			v.Since = e.At
		}
		for _, st := range e.After.Streams {
			v.foldStream(st)
		}
		for _, r := range e.After.Rows {
			v.foldRow(r, e.At)
		}
		for _, j := range e.After.Judgements {
			v.Judgements[j.ID] = j
		}
		for _, c := range e.After.Cards {
			v.foldCard(c, e)
		}
		for ver, p := range e.After.Policies {
			if p.Active {
				v.Policy = ver
			}
		}
		if e.After.Stop != nil {
			v.Stopped, v.StopReason = e.After.Stop.Stopped, e.After.Stop.Reason
		}
	}
}

// foldStream: a lifted hold gives every waiting producer of the stream its
// chance to resolve.
func (v *View) foldStream(st store.Stream) {
	if old, ok := v.Streams[st.Name]; ok && old.Held && !st.Held {
		for p := range v.waitingIn[st.Name] {
			v.resolvePending[p] = struct{}{}
		}
	}
	v.Streams[st.Name] = st
}

func (v *View) foldRow(r store.Row, at time.Time) {
	if old, ok := v.Rows[r.Name]; ok && old.State == "up" && r.State == "down" {
		v.Down[r.Name] = at
	}
	v.Rows[r.Name] = r
}

func (v *View) foldCard(c store.Card, e store.Event) {
	old, had := v.Cards[c.ID]
	if had {
		v.unindex(old)
	} else {
		v.born[c.ID] = e.Seq
	}
	v.Cards[c.ID] = c
	v.touched[c.ID] = e.Seq
	v.index(c)
	if isConsumer(c) {
		v.foldConsumer(c, old, had, e.At)
		return
	}
	v.foldProducer(c, old, had, e.At)
}

func (v *View) foldConsumer(c, old store.Card, had bool, at time.Time) {
	switch {
	case c.State == "working" && (!had || old.State != "working"):
		v.TakenAt[c.ID] = at
	case c.State == "done" && (!had || old.State != "done"):
		v.Ended[c.Holder] = append(v.Ended[c.Holder], ending{At: at, Fault: c.Result == "fault"})
		if n := len(v.Ended[c.Holder]); n > endedKept {
			v.Ended[c.Holder] = v.Ended[c.Holder][n-endedKept:]
		}
	}
}

// foldProducer keeps the pending sets: a card that became waiting may
// resolve; a landed card frees its dependants and the waiting producers
// sharing its paths; a failed card owes its dependants a drop.
func (v *View) foldProducer(c, old store.Card, had bool, at time.Time) {
	switch {
	case c.State == "waiting" && (!had || old.State != "waiting"):
		v.resolvePending[c.ID] = struct{}{}
	case c.State == "landed" && (!had || old.State != "landed"):
		v.Landed[c.Stream] = at
		v.freed(c)
	case c.State == "fail" && (!had || old.State != "fail"):
		if len(v.dependants[c.ID]) > 0 {
			v.dropPending[c.ID] = struct{}{}
		}
		v.freed(c)
	}
}

// freed pends the dependants of c and the waiting producers of its stream
// sharing a path with it.
func (v *View) freed(c store.Card) {
	for d := range v.dependants[c.ID] {
		v.resolvePending[d] = struct{}{}
	}
	for _, path := range c.Paths {
		for q := range v.waitPaths[c.Stream][path] {
			v.resolvePending[q] = struct{}{}
		}
	}
}

func isConsumer(c store.Card) bool { return c.Kind == "work" || c.Kind == "read" }

func isLive(c store.Card) bool { return c.State == "ready" || c.State == "working" }

func (v *View) index(c store.Card) {
	if isConsumer(c) {
		add(v.consOf, c.Of, c.ID)
		if isLive(c) {
			add(v.liveOnRow, c.Holder, c.ID)
		}
		return
	}
	add(v.byState, c.State, c.ID)
	add(v.byRung, rung(c), c.ID)
	for _, n := range c.Needs {
		add(v.dependants, n, c.ID)
	}
	if v.count[c.Stream] == nil {
		v.count[c.Stream] = map[string]int{}
	}
	v.count[c.Stream][c.State]++
	if c.State == "landed" && c.Bounces == 0 && c.Kind == "producer" {
		v.cleanLand[c.Stream]++
	}
	if c.Bounces > 0 && (c.State == "ready" || c.State == "review") {
		v.bounced[c.ID] = struct{}{}
	}
	v.indexPaths(c, true)
}

func (v *View) unindex(c store.Card) {
	if isConsumer(c) {
		del(v.consOf, c.Of, c.ID)
		del(v.liveOnRow, c.Holder, c.ID)
		return
	}
	del(v.byState, c.State, c.ID)
	del(v.byRung, rung(c), c.ID)
	for _, n := range c.Needs {
		del(v.dependants, n, c.ID)
	}
	v.count[c.Stream][c.State]--
	if c.State == "landed" && c.Bounces == 0 && c.Kind == "producer" {
		v.cleanLand[c.Stream]--
	}
	delete(v.bounced, c.ID)
	v.indexPaths(c, false)
}

// indexPaths keeps the two path indexes (ready or started; waiting) and the
// waiting-by-stream index.
func (v *View) indexPaths(c store.Card, on bool) {
	var m map[string]map[string]set
	switch {
	case slices.Contains(started, c.State):
		m = v.paths
	case c.State == "waiting":
		m = v.waitPaths
		if on {
			add(v.waitingIn, c.Stream, c.ID)
		} else {
			del(v.waitingIn, c.Stream, c.ID)
		}
	default:
		return
	}
	if on {
		if m[c.Stream] == nil {
			m[c.Stream] = map[string]set{}
		}
		for _, p := range c.Paths {
			add(m[c.Stream], p, c.ID)
		}
		return
	}
	if sm := m[c.Stream]; sm != nil {
		for _, p := range c.Paths {
			del(sm, p, c.ID)
		}
		if len(sm) == 0 {
			delete(m, c.Stream)
		}
	}
}

// rung is a producer's level, normal when unset.
func rung(c store.Card) string {
	if c.Priority == "" {
		return "normal"
	}
	return c.Priority
}

func add(m map[string]set, k, id string) {
	if m[k] == nil {
		m[k] = set{}
	}
	m[k][id] = struct{}{}
}

func del(m map[string]set, k, id string) {
	if s := m[k]; s != nil {
		delete(s, id)
		if len(s) == 0 {
			delete(m, k)
		}
	}
}

// sorted is a set's members in id order, so every step is deterministic.
func sorted(s set) []string {
	out := make([]string, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Producers is every producer or sentinel in state, in id order.
func (v *View) Producers(state string) []string { return sorted(v.byState[state]) }

// Consumers is every consumer of producer p, in cut order (the seq that put
// each one; by id for those older than the view).
func (v *View) Consumers(p string) []store.Card {
	ids := sorted(v.consOf[p])
	slices.SortStableFunc(ids, func(a, b string) int { return cmpInt64(v.born[a], v.born[b]) })
	out := make([]store.Card, len(ids))
	for i, id := range ids {
		out[i] = v.Cards[id]
	}
	return out
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Live is the live consumers held by row, in id order.
func (v *View) Live(row string) []string { return sorted(v.liveOnRow[row]) }

// Working is how many consumers row has in working.
func (v *View) Working(row string) int {
	n := 0
	for id := range v.liveOnRow[row] {
		if v.Cards[id].State == "working" {
			n++
		}
	}
	return n
}

// Dependants is every producer whose needs name card, in id order.
func (v *View) Dependants(card string) []string { return sorted(v.dependants[card]) }

// Overlaps is every other producer of p's stream, ready or started, that
// shares a path with p (LANDING step 2; LESSONS 2.20), in id order.
func (v *View) Overlaps(p store.Card) []string {
	found := set{}
	for _, path := range p.Paths {
		for q := range v.paths[p.Stream][path] {
			if q != p.ID {
				found[q] = struct{}{}
			}
		}
	}
	return sorted(found)
}

// Count is how many producers of stream s are in state.
func (v *View) Count(s, state string) int { return v.count[s][state] }

// Open says whether row r may be dealt to: up on the bus, not held, and
// hooked in to the sprint (Glenn, 2026-11-06: the hook is the subscription
// to the sprint's pushes; a row not hooked in is never dealt to and nothing
// is pushed to it). The hook is the row's Session and Role (store round 22).
func (v *View) Open(r store.Row) bool { return r.State == "up" && !r.Held && Hooked(r) }

// Hooked says whether row r is hooked in to the sprint with a role that
// allows the deal: worker, or coordinator, who counts as a worker.
func Hooked(r store.Row) bool {
	return r.Session != "" && (r.Role == "worker" || r.Role == "coordinator")
}

// viewCore is what two equal views share: the records and the indexes, not
// the facts the fold alone witnesses.
type viewCore struct {
	Seq                                    int64
	Epoch, Policy                          int
	Streams                                map[string]store.Stream
	Cards                                  map[string]store.Card
	Rows                                   map[string]store.Row
	Judgements                             map[string]store.Judgement
	ByState, ByRung, ConsOf, Live, Depends map[string]set
	Paths, WaitPaths                       map[string]map[string]set
	WaitingIn                              map[string]set
	Bounced                                set
	Count                                  map[string]map[string]int
	CleanLand                              map[string]int
}

func (v *View) core() viewCore {
	v.trim()
	return viewCore{v.Seq, v.Epoch, v.Policy, v.Streams, v.Cards, v.Rows, v.Judgements, v.byState, v.byRung, v.consOf, v.liveOnRow, v.dependants,
		v.paths, v.waitPaths, v.waitingIn, v.bounced, v.count, v.cleanLand}
}

// trim drops the zero counters, so a folded view's counters equal a fresh
// build's.
func (v *View) trim() {
	for s, m := range v.count {
		for st, n := range m {
			if n == 0 {
				delete(m, st)
			}
		}
		if len(m) == 0 {
			delete(v.count, s)
		}
	}
	for s, n := range v.cleanLand {
		if n == 0 {
			delete(v.cleanLand, s)
		}
	}
}
