package sprint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Working means a live run (v1.2.6; tla/LiveRuns.tla). A worker's beat carries its live
// set: every card it runs a child for, and every card whose child finished and whose report
// waits in its outbox, each as <card>@<gen> (a held report as <card>@<gen>:held). The beat is
// the evidence for working: a card a row holds in working that the row's live set has not
// named for LiveGrace is returned to ready by the machine, RUNNING or STOPPED (the tick's
// reconcile, LiveReturns, one step of the one writer); the dashboard's working cell counts
// the row's live runs, never its takes (LiveCount). A row whose worker's beat carries no live
// set (a worker from before v1.2.6) is left as it was: its table is its word.
//
// 2026-10-10: the sprint STOPPED at ~15:00Z showed 30 working on six fleet hosts and 29 on
// friend.zhi at ~18:25Z with no harness run anywhere: a take whose run had gone stayed working,
// and a STOPPED machine ran no tick to notice.

const (
	// LiveGrace is how long a working card may go unnamed by its row's live set before the
	// machine returns it: four beat windows of a member's beat (BeatDeadline), sixty of a
	// friend's, and longer than a take's first beat takes to name it.
	LiveGrace = 60 * time.Second
	// LiveHeld marks a live-set entry whose child finished and whose report is held.
	LiveHeld = "held"
	// LiveNone is the live set with nothing in it, as a beat's --live says it.
	LiveNone = "-"
	// liveSeenKeep is how long a beat record keeps an entry's last sighting after the
	// beats stop naming it: past it the entry is forgotten (its card long returned or done).
	liveSeenKeep = 10 * LiveGrace
	// maxLiveEntries bounds a beat's live set (a member's width is at most 64 lanes).
	maxLiveEntries = 512
)

// LiveEntry is one entry of a beat's live set: the card, its generation, and whether its
// child finished with its report held (Held) rather than running.
type LiveEntry struct {
	ID   string
	Gen  int
	Held bool
}

// Key is the entry's card at its generation, as the beat record keeps its sightings.
func (e LiveEntry) Key() string { return LiveKey(e.ID, e.Gen) }

// String is the entry as a beat's --live says it.
func (e LiveEntry) String() string {
	if e.Held {
		return e.Key() + ":" + LiveHeld
	}
	return e.Key()
}

// LiveKey is a card at a generation, <card>@<gen>.
func LiveKey(id string, gen int) string { return id + "@" + strconv.Itoa(max(gen, 1)) }

// ParseLive reads a beat's --live: LiveNone for an empty set, else <card>@<gen>[:held]
// entries, comma separated, each card once. The entries come back in the order given.
func ParseLive(v string) ([]string, error) {
	if v == LiveNone {
		return []string{}, nil
	}
	if v == "" {
		return nil, fmt.Errorf("--live wants <card>@<gen>[:held],... or %s for none", LiveNone)
	}
	words := strings.Split(v, ",")
	if len(words) > maxLiveEntries {
		return nil, fmt.Errorf("--live names %d cards, more than %d", len(words), maxLiveEntries)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(words))
	// the refusals name the entry's place, never its words (a value given here is not echoed:
	// internal/sprint secrets_in_errors_test.go)
	for i, w := range words {
		e, ok := parseLiveEntry(w)
		if !ok {
			return nil, fmt.Errorf("--live wants <card>@<gen>[:held] entries, comma separated; entry %d is not one", i+1)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("--live names one card twice (entry %d)", i+1)
		}
		seen[e.ID] = true
		out = append(out, e.String())
	}
	return out, nil
}

func parseLiveEntry(w string) (LiveEntry, bool) {
	held := false
	if s, ok := strings.CutSuffix(w, ":"+LiveHeld); ok {
		w, held = s, true
	}
	id, g, ok := strings.Cut(w, "@")
	if !ok || !validCardID(id) {
		return LiveEntry{}, false
	}
	n, err := strconv.Atoi(g)
	if err != nil || n < 1 || strconv.Itoa(n) != g {
		return LiveEntry{}, false
	}
	return LiveEntry{ID: id, Gen: n, Held: held}, true
}

// validCardID is the shape of a card id in a live set: letters, digits and . _ - ~,
// at most 256 bytes.
func validCardID(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._-~", r) {
			return false
		}
	}
	return true
}

// LiveEntries is the beat's live set read back, nil when the beat carries none.
func (b Beat) LiveEntries() []LiveEntry {
	if !b.LiveKnown {
		return nil
	}
	out := make([]LiveEntry, 0, len(b.Live))
	for _, w := range b.Live {
		if e, ok := parseLiveEntry(w); ok {
			out = append(out, e)
		}
	}
	return out
}

// NextLive is the live part of a beat record written at now: the set given (nil: this
// beat carries none, and the record keeps no set, so the row is not reconciled), when the
// beats began carrying one (since: the record's, or now for the first), and each entry's
// last sighting, carried from the record before for liveSeenKeep and set to now for each
// entry named now.
func NextLive(prev Beat, now time.Time, live []string) (set []string, known bool, since time.Time, seen map[string]time.Time) {
	if live == nil {
		return nil, false, time.Time{}, nil
	}
	since = prev.LiveSince
	if !prev.LiveKnown || since.IsZero() {
		since = now.UTC().Truncate(time.Second)
	}
	seen = map[string]time.Time{}
	for k, at := range prev.LiveSeen {
		if now.Sub(at) <= liveSeenKeep {
			seen[k] = at
		}
	}
	at := now.UTC().Truncate(time.Second)
	for _, w := range live {
		if e, ok := parseLiveEntry(w); ok {
			seen[e.Key()] = at
		}
	}
	if len(seen) == 0 {
		seen = nil
	}
	return slices.Clone(live), true, since, seen
}

// rowLive is a row's live evidence at now: whether its beat carries a live set at all,
// and, while that beat is fresh, each entry named by card id.
type rowLive struct {
	known bool
	beat  Beat
	now   map[string]LiveEntry
}

func liveOf(b Beat, now time.Time) rowLive {
	r := rowLive{known: b.LiveKnown, beat: b}
	if !r.known || !b.Fresh(now) {
		return r // a beat gone stale names nothing live
	}
	r.now = map[string]LiveEntry{}
	for _, e := range b.LiveEntries() {
		r.now[e.ID] = e
	}
	return r
}

// names says the row's fresh live set names the card at its generation, and how.
func (r rowLive) names(c *Card) (LiveEntry, bool) {
	e, ok := r.now[c.ID]
	return e, ok && e.Gen == max(c.Int("gen"), 1)
}

// RowBeat is the beat that is a fleet row's evidence: a friend row's is her own beat (kept
// by her name, Store.fleetBeats), a member's its beat.
func RowBeat(beats map[string]Beat, row string) Beat {
	if f, ok := FriendOfRow(row); ok {
		return beats[f]
	}
	return beats[row]
}

// RowWorkingKeys is each fleet row's working cards as <card>@<gen>, the where record's
// (Store.keepWhere): what the dashboard counts against the rows' beats at each read
// (LiveCountKeys), so its working cell follows the live sets between two counts.
func RowWorkingKeys(s *Snapshot) map[string][]string {
	if s == nil || s.Fleet == nil {
		return nil
	}
	out := map[string][]string{}
	for _, row := range s.Fleet.Rows() {
		for _, c := range s.Fleet.Cell(row, Working) {
			out[row] = append(out[row], LiveKey(c.ID, c.Int("gen")))
		}
	}
	return out
}

// LiveCountKeys is a fleet row's working cell as the dashboard shows it (v1.2.6): of the
// row's working cards (keys, <card>@<gen>), those its fresh live set names running, those it
// names held (finished, the report waiting), and the stale rest, which it names neither way
// (the reconcile returns them within LiveGrace). A row whose beat carries no live set counts
// every working card running, as before.
func LiveCountKeys(keys []string, b Beat, now time.Time) (running, held, stale int) {
	live := liveOf(b, now)
	if !live.known {
		return len(keys), 0, 0
	}
	for _, k := range keys {
		id, g, _ := strings.Cut(k, "@")
		n, _ := strconv.Atoi(g)
		e, ok := live.now[id]
		switch {
		case !ok || e.Gen != max(n, 1):
			stale++
		case e.Held:
			held++
		default:
			running++
		}
	}
	return running, held, stale
}

// LiveHeldAt says the row's fresh live set names the card held at gen: its child finished
// and its report waits in the outbox (start's reading of a STOP debt entry, Store.
// unsettledStopDebt: the report is taken once the machine runs; tla/LiveRuns.tla BlocksStart).
func LiveHeldAt(b Beat, id string, gen int, now time.Time) bool {
	e, ok := liveOf(b, now).now[id]
	return ok && e.Held && e.Gen == max(gen, 1)
}

// LiveReturnVerb is the reconcile's step verb: the one writer's own return, which a STOP
// debt does not refuse (it is the owner's receipt, read off its beats).
const LiveReturnVerb = "live return"

// LiveReturns is the reconcile (tla/LiveRuns.tla Tick): every card working on a fleet row
// whose beat carries a live set that has not named it at its generation, running or held,
// for LiveGrace since it was taken or last named, goes back to ready on its row at its next
// generation, untaken, with the reason recorded on the card (lost_from_gen, lost_reason)
// and in its line. Its progress is kept for the next run (stopped_progress), as a
// stop-return keeps it. While the machine is STOPPED the return is also the STOP's receipt
// (stopped_from_gen), so start finds the debt settled. RUNNING or STOPPED alike: a STOPPED
// machine takes and lands nothing new, and still reconciles.
func LiveReturns(s *Snapshot, beats map[string]Beat, stopped bool) Plan {
	var p Plan
	if s == nil || s.Fleet == nil {
		return p
	}
	for _, row := range s.Fleet.Rows() {
		b := RowBeat(beats, row)
		live := liveOf(b, s.Now)
		if !live.known {
			continue
		}
		working := slices.Clone(s.Fleet.Cell(row, Working))
		SortCards(working)
		for _, c := range working {
			if _, ok := live.names(c); ok {
				continue
			}
			gen := max(c.Int("gen"), 1)
			// absent since the latest of: its take (or its deal, for a card never taken), the
			// first beat of this worker that carried a live set, and its last sighting
			since := b.LiveSince
			for _, t := range []time.Time{stampAt(c, "taken"), stampAt(c, "dealt"), b.LiveSeen[LiveKey(c.ID, gen)]} {
				if t.After(since) {
					since = t
				}
			}
			if since.IsZero() || s.Now.Sub(since) <= LiveGrace {
				continue
			}
			why := fmt.Sprintf("no live run: %s's beats have not named %s for %s", row, LiveKey(c.ID, gen), s.Now.Sub(since).Truncate(time.Second))
			if !b.Fresh(s.Now) {
				why += " (its beat stopped " + ago(s.Now.Sub(b.At)) + ")"
			}
			set := map[string]string{
				"gen": itoa(gen + 1), "lost_from_gen": itoa(gen), "lost_reason": cutText(why, MaxCardTextBytes),
				"untaken_since": stamp(s.Now),
			}
			if stopped {
				set["stopped_from_gen"], set["stopped_reason"] = itoa(gen), cutText(why, MaxCardTextBytes)
			}
			unset := []string{"taken", "begun", FieldStarted, FieldFriendDeadline}
			if progress := c.F(FieldProgress); progress != "" {
				set["stopped_progress"] = progress
				unset = append(unset, FieldProgress)
			}
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"),
				Changes: []Change{change(Fleet, moveEntry(c, c.Row, Ready, set, unset...))},
				Moved:   fmt.Sprintf("%s working -> ready on %s gen=%d (%s)", c.ID, row, gen+1, why)})
		}
	}
	return p
}
