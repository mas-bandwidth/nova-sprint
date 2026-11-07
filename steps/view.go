package steps

import (
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// View is the sprint as the tick reads it: the snapshot's records plus the
// indexes the steps need (producers by state and rung, consumers by
// producer and row, dependants by need, live paths by stream), advanced by
// Fold. After any fold it equals a fresh Build at the same seq
// (TestViewFoldEqualsBuild). The facts a snapshot does not carry (when a
// consumer was taken, when a stream last landed, when a row went down, when
// a row faulted) are witnessed by the fold and are zero for records that
// predate the view; every bound they feed is then measured from Since.
type View struct {
	Seq    int64
	Epoch  int
	Policy int
	Since  time.Time // Build's now: the base of every bound no event dates

	Streams    map[string]store.Stream
	Cards      map[string]store.Card
	Rows       map[string]store.Row
	Judgements map[string]store.Judgement

	byState    map[string]set            // producers and sentinels by state
	byRung     map[string]set            // producers and sentinels by priority
	consOf     map[string]set            // consumers by producer
	liveOnRow  map[string]set            // live (ready, working) consumers by row
	dependants map[string]set            // producers whose needs name the key
	paths      map[string]map[string]set // stream -> file -> producers ready or started (concrete paths only)

	TakenAt map[string]time.Time   // consumer -> when it went working: when it was dealt
	ReadyAt map[string]time.Time   // producer -> when it last went ready
	Landed  map[string]time.Time   // stream -> when a producer of it last landed
	Down    map[string]time.Time   // row -> when it last went down
	Faults  map[string][]time.Time // row -> when its consumers reported fault
	acks    set                    // the acknowledgement cursors written (AckCursor)
	born    map[string]int64       // card -> the seq that put it (0: before the view)
	touched map[string]int64       // card -> the seq that last wrote it (0: before the view)
}

type set = map[string]struct{}

// started is the producer states that hold their paths against an overlap:
// ready and the started ones (working, review, merging).
var started = []string{"ready", "working", "review", "merging"}

// Build is the view of snapshot s at now.
func Build(s store.Snapshot, now time.Time) *View {
	v := &View{Since: now}
	v.reset(s.Epoch)
	v.Seq, v.Policy = s.Seq, s.Policy
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
	for name := range s.Cursors {
		v.acks[name] = struct{}{}
	}
	return v
}

func (v *View) reset(epoch int) {
	v.Epoch = epoch
	v.Streams, v.Cards = map[string]store.Stream{}, map[string]store.Card{}
	v.Rows, v.Judgements = map[string]store.Row{}, map[string]store.Judgement{}
	v.byState, v.byRung, v.consOf = map[string]set{}, map[string]set{}, map[string]set{}
	v.liveOnRow, v.dependants = map[string]set{}, map[string]set{}
	v.paths = map[string]map[string]set{}
	v.TakenAt, v.Landed, v.Down = map[string]time.Time{}, map[string]time.Time{}, map[string]time.Time{}
	v.ReadyAt = map[string]time.Time{}
	v.Faults, v.born, v.touched = map[string][]time.Time{}, map[string]int64{}, map[string]int64{}
	if v.acks == nil { // the store keeps cursors across epochs; so does the view
		v.acks = set{}
	}
}

// Acked says whether consumer c's row has acknowledged it (its take wrote
// AckCursor(c) in this epoch).
func (v *View) Acked(c string) bool { _, ok := v.acks[AckCursor(v.Epoch, c)]; return ok }

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
			v.Streams[st.Name] = st
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
		for name := range e.After.Cursors {
			v.acks[name] = struct{}{}
		}
		for ver, p := range e.After.Policies {
			if p.Active {
				v.Policy = ver
			}
		}
	}
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
	switch {
	case isConsumer(c) && c.State == "working" && (!had || old.State != "working"):
		v.TakenAt[c.ID] = e.At
	case isConsumer(c) && c.State == "done" && c.Result == "fault" && (!had || old.State != "done"):
		v.Faults[c.Holder] = append(v.Faults[c.Holder], e.At)
	case !isConsumer(c) && c.State == "ready" && (!had || old.State != "ready"):
		v.ReadyAt[c.ID] = e.At
	case !isConsumer(c) && c.State == "landed" && (!had || old.State != "landed"):
		v.Landed[c.Stream] = e.At
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
	if slices.Contains(started, c.State) {
		for _, p := range c.Paths {
			if !Concrete(p) {
				continue
			}
			if v.paths[c.Stream] == nil {
				v.paths[c.Stream] = map[string]set{}
			}
			add(v.paths[c.Stream], p, c.ID)
		}
	}
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
	if m := v.paths[c.Stream]; m != nil {
		for _, p := range c.Paths {
			del(m, p, c.ID)
		}
		if len(m) == 0 {
			delete(v.paths, c.Stream)
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

// Concrete says whether a listed path names one file: no glob character
// (* ? [) and no trailing slash. Overlap is a file both cards edit, never a
// directory or glob both add to (defect 50: ten audits that each add their
// own files under one corpus glob were serialised behind one); a true
// conflict under a shared directory is the landing's rebase and the read's.
func Concrete(path string) bool {
	return path != "" && !strings.ContainsAny(path, "*?[") && !strings.HasSuffix(path, "/")
}

// Overlaps is every other producer of p's stream, ready or started, that
// names a file p names (Concrete; LANDING step 2; LESSONS 2.20), in id order.
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
	Paths                                  map[string]map[string]set
	Acks                                   set
}

func (v *View) core() viewCore {
	return viewCore{v.Seq, v.Epoch, v.Policy, v.Streams, v.Cards, v.Rows, v.Judgements, v.byState, v.byRung, v.consOf, v.liveOnRow, v.dependants, v.paths, v.acks}
}
