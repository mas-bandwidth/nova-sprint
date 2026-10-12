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
	// HeldGrace is how long a working read card may be named held by its row's fresh beat
	// before the machine returns it to the read deal: the member's child ended and its
	// report was not taken, so no one reads it. At most twice LiveGrace (docs/SPEC-SPRINT.md
	// section 6, the reads).
	HeldGrace = 2 * LiveGrace
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

// LiveNamedAt says the beat carries a live set and it names the card at gen, running or
// held, whether or not that beat is fresh: the owner's last beat since the STOP keeps its
// word for the settle (Store.settleStopDebtByBeat; tla/StopReturn.tla SettleByBeat, d.id
// \notin beat). After a STOP nothing new is taken, so a post-STOP beat that named the card
// stays true until the owner beats again or returns it; a stale beat read as an empty live
// set is the reversed witness (tla/LiveRuns.tla "staleasabsent"). A beat that carries no
// live set names nothing, and its count is read as before; the reconcile keeps the fresh-only
// reading (liveOf, LiveNamesAt).
func LiveNamedAt(b Beat, id string, gen int) bool {
	if !b.LiveKnown {
		return false
	}
	for _, e := range b.LiveEntries() {
		if e.ID == id && e.Gen == max(gen, 1) {
			return true
		}
	}
	return false
}

// LiveReturnVerb is the reconcile's step verb: the one writer's own return, which a STOP
// debt does not refuse (it is the owner's receipt, read off its beats).
const LiveReturnVerb = "live return"

// LiveReturns is the reconcile (tla/LiveRuns.tla Tick): every card working on a fleet row
// whose fresh beat carries a live set that has not named it at its generation, running or
// held, for LiveGrace since it was taken or last named, goes back to ready on its row at its next
// generation, untaken, with the reason recorded on the card (lost_from_gen, lost_reason)
// and in its line. Its progress is kept for the next run (stopped_progress), as a
// stop-return keeps it. While the machine is STOPPED the return is also the STOP's receipt
// (stopped_from_gen), so start finds the debt settled. RUNNING or STOPPED alike: a STOPPED
// machine takes and lands nothing new, and still reconciles.
// tla/AttemptsEpochs.tla GenStep.
func LiveReturns(s *Snapshot, beats map[string]Beat, stopped bool) Plan {
	var p Plan
	if s == nil || s.Fleet == nil {
		return p
	}
	ghosted := map[string][]string{} // the primary's ghosts retired lost this tick, for the judgment below
	for _, row := range s.Fleet.Rows() {
		b := RowBeat(beats, row)
		// a friend who has stopped beating is down: her daemon alone runs her lanes,
		// so unlike a silent machine, whose children may still run, her dealt and
		// taken cards and reads come back to the deal the moment she is past the beat
		// bound (docs/SPEC-SPRINT.md section 1, a friend's health; FriendReturn).
		if f, ok := FriendOfRow(row); ok && b.Beaten() && s.Now.Sub(b.At) > FriendBeatBound {
			p = friendStoppedReturn(s, p, f, row, b)
			continue
		}
		live := liveOf(b, s.Now)
		if !live.known || !b.Fresh(s.Now) {
			// only the owner's fresh beat is its word that a run is gone: a worker gone
			// silent (a partition, a hung beat, a store stall) may still run its children,
			// and a return here would let start deal the card again beside them
			// (tla/LiveRuns.tla Silence, Broken "staleasabsent"). A silent member is the
			// presence part's while running (down after MissedBeatsDown windows), and its
			// owner's stop-return's while STOPPED; the dashboard shows its takes stale.
			continue
		}
		working := slices.Clone(s.Fleet.Cell(row, Working))
		SortCards(working)
		for _, c := range working {
			if e, ok := live.names(c); ok {
				if !e.Held || !isRead(c) {
					continue // running, or a held work card (its report waits): alive
				}
				// a read card its row's beat names held: its child ended and its report was not
				// taken, so no one reads it (docs/SPEC-SPRINT.md section 6, the reads). Past
				// HeldGrace it is a ghost, retired lost and dealt again, as a read not named is
				// below (tla-readcards-review-has-a-live-read-or-a-judgment).
				gen := max(c.Int("gen"), 1)
				if c.F("held_since") == "" {
					p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"),
						Changes: []Change{change(Fleet, setEntry(c, map[string]string{"held_since": stamp(s.Now)}))},
						Moved:   fmt.Sprintf("%s held on %s (its report waits)", c.ID, row)})
					continue
				}
				held := stampAt(c, "held_since")
				if s.Now.Sub(held) <= HeldGrace {
					continue
				}
				why := fmt.Sprintf("held: %s's beats name %s held for %s", row, LiveKey(c.ID, gen), s.Now.Sub(held).Truncate(time.Second))
				p.Units = append(p.Units, readLostUnit(s, c, gen, why, stopped))
				ghosted[c.F("primary")] = append(ghosted[c.F("primary")], c.ID+" on "+orDash(c.F("reader"))+" ("+why+")")
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
			if isRead(c) {
				// a read card goes back to the read deal, not to this row's ready: retired
				// lost (RetiredByLost), spending no reader and marking no failure, so the next
				// read-card ask cuts it again at its next generation for whichever reader the
				// deal picks, this one included (v1.2.14, a-member-hands-back-a-read-it-is-not-
				// running; tla/ReadCards.tla LiveReturn, NamedOrWithinGrace)
				p.Units = append(p.Units, readLostUnit(s, c, gen, why, stopped))
				ghosted[c.F("primary")] = append(ghosted[c.F("primary")], c.ID+" on "+orDash(c.F("reader"))+" ("+why+")")
				continue
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
	// The second ghost at one primary's attempt pushes one judgment (docs/SPEC-SPRINT.md
	// section 6, the reads; tla-readcards-review-has-a-live-read-or-a-judgment): count the
	// read cards retired lost at each primary's current attempt, and judge the ones with two
	// or more, once, naming each card, row and reason. The judgment is an alarm, never a hold:
	// the deal asks the read again whatever its member's beats say.
	if s.Work != nil && s.Fleet != nil {
		for _, pr := range s.Work.Column(Review) {
			attempt := max(pr.Int("attempt"), 1)
			var lost []string
			for _, c := range s.Fleet.WithPrefix(pr.ID + ".r" + itoa(attempt) + ".") {
				if isRead(c) && c.F("retired_by") == RetiredByLost {
					lost = append(lost, c.ID+" on "+orDash(c.F("reader"))+" ("+orDash(c.F("lost_reason"))+")")
				}
			}
			lost = append(lost, ghosted[pr.ID]...)
			if len(lost) < 2 || openLostTwice(s, pr.ID) {
				continue
			}
			n := judgment(NLostTwice, pr.Row, s.Now, 0, pr.ID)
			n.What = "a read was lost twice at attempt " + itoa(attempt) + ": " + strings.Join(lost, "; ")
			p.Notes = append(p.Notes, n)
		}
	}
	return p
}

// friendStoppedReturn is the reconcile on a friend's row whose beat is past the beat bound:
// she is down, and every card working on her row comes back to the deal. A read card goes
// back to the read deal (readLostUnit: retired lost, spending no reader). A work card is
// retired off her row by FriendReturn, its primary moved working -> ready with its carry
// head kept, so the next taker gets it (docs/SPEC-SPRINT.md section 1, friend reconcile).
// It is the same plan the run loop's friend reconcile applies, run here by the tick because
// a friend who stopped beating cannot hand her own cards back.
func friendStoppedReturn(s *Snapshot, p Plan, f, row string, b Beat) Plan {
	why := "her daemon stopped beating " + ago(s.Now.Sub(b.At))
	working := slices.Clone(s.Fleet.Cell(row, Working))
	SortCards(working)
	var back []FriendReturnCard
	for _, c := range working {
		if isRead(c) {
			gen := max(c.Int("gen"), 1)
			p.Units = append(p.Units, readLostUnit(s, c, gen, why, false))
			continue
		}
		back = append(back, FriendReturnCard{ID: c.ID, Gen: c.Int("gen"), Why: why})
	}
	if len(back) > 0 {
		q := FriendReturn(s, FriendReturnReq{Friend: f, Who: LiveReturnVerb, Cards: back, Push: true})
		p.Units = append(p.Units, q.Units...)
		p.Notes = append(p.Notes, q.Notes...)
		p.Refused = append(p.Refused, q.Refused...)
	}
	return p
}

// openLostTwice says the primary already has the lost-twice judgment open on it: the
// judgment is raised once while it stands, never anew each tick.
func openLostTwice(s *Snapshot, id string) bool {
	for _, o := range s.Open {
		if o.Note.Type == NLostTwice && o.Subject() == id {
			return true
		}
	}
	return false
}

// readLostUnit is the reconcile's return of a read card its row's live set has not named for
// LiveGrace: the card leaves the fleet table retired lost (RetiredByLost: not a spent reader,
// not a failure), its generation and why kept on the record, and while STOPPED the return is
// the STOP's receipt too. The read-card ask then counts the primary's reads without it and
// deals the read again at the card's next generation (ReadCardID .g<n>).
func readLostUnit(s *Snapshot, c *Card, gen int, why string, stopped bool) Unit {
	set := map[string]string{
		"retired": stamp(s.Now), "retired_by": RetiredByLost,
		"lost_from_gen": itoa(gen), "lost_reason": cutText(why, MaxCardTextBytes),
	}
	if stopped {
		set["stopped_from_gen"], set["stopped_reason"] = itoa(gen), cutText(why, MaxCardTextBytes)
	}
	return Unit{Key: c.ID, Stream: c.F("stream"),
		Changes: []Change{change(Fleet, removeEntry(c, set))},
		Moved:   fmt.Sprintf("%s working -> the read deal off %s (%s)", c.ID, c.Row, why)}
}

// readCardGhost says a read card working on a fleet row is a ghost (no child of its row runs
// it) and why, reading the row's own beat (s.Beats, the tick's; docs/SPEC-SPRINT.md section 6,
// the reads; tla-readcards-review-has-a-live-read-or-a-judgment models the bound). A ghost is:
// the row's fresh beat names it held for more than HeldGrace (its child ended and its report
// was not taken), or has not named it for LiveGrace (as LiveReturns returns it), or the row is
// down, stale or held (its beat carries no fresh live set). A row whose beat is unknown names
// nothing: the read is not a ghost on that evidence alone.
func readCardGhost(s *Snapshot, c *Card) (bool, string) {
	if s == nil || c == nil || c.Row == "" {
		return false, ""
	}
	b, ok := s.Beats[c.Row]
	if !ok {
		// a friend row's beat is kept under her name, not her row (Store.fleetBeats,
		// RowBeat): resolve it so a friend-dealt read card is read as a ghost by its
		// own beat, as a member's is.
		if f, fok := FriendOfRow(c.Row); fok {
			b, ok = s.Beats[f]
		}
	}
	if !ok {
		return false, ""
	}
	live := liveOf(b, s.Now)
	gen := max(c.Int("gen"), 1)
	if !live.known || !b.Fresh(s.Now) {
		// a stale beat, or one that carries no live set, names nothing live: the row is
		// down, stale or held (docs/SPEC-SPRINT.md section 6, the reads)
		return true, fmt.Sprintf("%s's beat names nothing live (stale, down or held)", c.Row)
	}
	if e, named := live.names(c); named {
		if !e.Held || !isRead(c) {
			return false, "" // running, or a held work card (its report waits): alive
		}
		// a read card its row's beat names held: its child ended and its report was not
		// taken. Alive within HeldGrace (the report may still come); past it, a ghost, read
		// ghost here so the one predicate and the doctor see it, and the reconcile retires
		// it lost (docs/SPEC-SPRINT.md section 6, the reads; tla-readcards-review-has-a-live-
		// read-or-a-judgment).
		held := stampAt(c, "held_since")
		if held.IsZero() || s.Now.Sub(held) <= HeldGrace {
			return false, ""
		}
		return true, fmt.Sprintf("held: %s's beats name %s held for %s", c.Row, LiveKey(c.ID, gen), s.Now.Sub(held).Truncate(time.Second))
	}
	// not named at all: within LiveGrace the read may still be running; past it, a ghost
	since := b.LiveSince
	for _, t := range []time.Time{stampAt(c, "taken"), stampAt(c, "dealt"), b.LiveSeen[LiveKey(c.ID, gen)]} {
		if t.After(since) {
			since = t
		}
	}
	if since.IsZero() || s.Now.Sub(since) <= LiveGrace {
		return false, ""
	}
	return true, fmt.Sprintf("%s's beats have not named %s for %s", c.Row, LiveKey(c.ID, gen), s.Now.Sub(since).Truncate(time.Second))
}

// hasOpenJudgmentOn says a primary has an open judgment or an acknowledged
// condition on it: the doctor's readers check holds a primary open on
// something rather than silent (docs/SPEC-SPRINT.md section 6, the reads).
func hasOpenJudgmentOn(s *Snapshot, id string) bool {
	if s == nil {
		return false
	}
	for _, o := range s.Open {
		if o.Subject() == id {
			return true
		}
	}
	for _, o := range s.Acked {
		if o.Subject() == id {
			return true
		}
	}
	return false
}
