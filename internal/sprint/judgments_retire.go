package sprint

import (
	"fmt"
	"strings"
)

// A judgment retires with its card (docs/SPEC-SPRINT.md section 8, "A judgment
// retires with its card"): an open judgment whose every card has left the table
// (landed, dropped, or dropped replaced by its twin) is closed by the tick that
// sees it gone, answered by the machine with the card's event, so the inbox never
// shows a judgment about a card no longer on the table. A judgment on a primary
// retires on that primary; a tick-kept judgment of a stream-level condition that
// names cards (a tier no route serves, a starving fleet's sentinel) retires when
// every card it names has left, and the tick raises it again on the cards still
// waiting. A stream's own judgment (a stop, a red base) is its stream's, never a
// card's, and stays.
//
// A judgment is also outlived by its card's move (the 2026-10-12 j127: a brief
// defect still open after its card was re-briefed and dealt on to attempt 5):
// one raised at an attempt the card has since left, or on a brief the card has
// since replaced, closes the same way, answered by the card's move. The card's
// state, not the tick alone, is what closes it: one still at the attempt and
// brief it was raised for stays.

// RetiredPrefix opens the decided note of a judgment retired with its card.
const RetiredPrefix = "retired with its card: "

// MovedOnPrefix opens the decided note of a judgment its card has moved past:
// the card reached another attempt, or its brief was replaced, since the
// judgment was raised.
const MovedOnPrefix = "answered by the card's move: "

// retiresWithCards are the stream-level judgments the tick keeps on the cards
// they name: each retires once every card it names has left the table.
var retiresWithCards = []string{NNoRoute, NStarving}

// aboutTheLanding are the judgments about a landed card by design: its landing
// is their cause, not their end. NInvariant is the check's own, closed when its
// rule holds again.
var aboutTheLanding = []string{NScoredLow, NInvariant}

// offTable is the event a card left the table by: "<id> landed", "<id> dropped
// (<reason>)", "<id> replaced by <twin>"; "" while it is on the table, or when the
// work table never had it (a subject that is not a card).
func offTable(s *Snapshot, id string) string {
	c := s.Work.Card(id)
	switch {
	case c == nil:
		return ""
	case c.Placed() && c.Col == Landed:
		return id + " landed"
	case c.Placed():
		return ""
	}
	reason := c.F("reason")
	if strings.HasPrefix(reason, "replaced by ") {
		return id + " " + reason
	}
	if c.F("outcome") == "dropped" {
		if reason == "" {
			return id + " dropped"
		}
		return id + " dropped (" + reason + ")"
	}
	return id + " off the table (" + orDash(c.F("outcome")) + ")"
}

// friendSubject reports whether a judgment or subject names a friend, and
// returns the friend's bare name.
func friendSubject(s *Snapshot, n Note, sub string) (string, bool) {
	trimmed := strings.TrimPrefix(sub, "stream:")
	trimmed = strings.TrimPrefix(trimmed, "member:")
	if f, ok := FriendOfRow(trimmed); ok {
		return f, true
	}
	if f, ok := FriendOfRow(strings.TrimPrefix(n.Stream, "member:")); ok {
		return f, true
	}
	if sub == "" || sub == SprintSubject || strings.ContainsAny(sub, ":@/") {
		return "", false
	}
	if s != nil && s.Work != nil && s.Work.Card(sub) != nil {
		return "", false
	}
	if s != nil {
		for _, seat := range s.Friends {
			if seat.Name == sub {
				return sub, true
			}
		}
		if s.Fleet != nil && s.Fleet.HasRow(FriendRow(sub)) {
			return sub, true
		}
	}
	if strings.HasPrefix(n.Type, "a friend ") || strings.HasPrefix(n.Type, "an up friend ") || strings.HasPrefix(n.What, "friend "+sub+" ") {
		return sub, true
	}
	return "", false
}

// friendOnTable reports whether friend is currently on the friends table.
func friendOnTable(s *Snapshot, n Note, sub, friend string) bool {
	if s == nil || s.FriendsOff() || friend == "" {
		return false
	}
	for _, seat := range s.Friends {
		if seat.Name == friend {
			return true
		}
	}
	if s.Friends != nil {
		return false
	}
	trimmed := strings.TrimPrefix(sub, "stream:")
	trimmed = strings.TrimPrefix(trimmed, "member:")
	if IsFriendRow(trimmed) {
		if s.Fleet != nil && s.Fleet.HasRow(FriendRow(friend)) {
			return true
		}
		if n.What == "" && IsFriendRow(n.Stream) {
			return true
		}
	}
	return false
}

// friendOffTable is the event a friend left the friends table by; "" while the
// friend is on the friends table or when the subject is not a friend.
func friendOffTable(s *Snapshot, n Note, sub string) string {
	f, ok := friendSubject(s, n, sub)
	if !ok || friendOnTable(s, n, sub, f) {
		return ""
	}
	if sub != f {
		return fmt.Sprintf("friend %s (%s) is not on the friends table", f, sub)
	}
	return fmt.Sprintf("friend %s is not on the friends table", f)
}

// movedOn is the event a judgment's card moved past it by: "<id> moved to attempt
// <m> (was <n>)", or "<id> was re-briefed" for a judgment about the brief itself;
// "" while the card is still the one the judgment was raised for, the judgment
// names no attempt, or the work table never had it (docs/SPEC-SPRINT.md section 8,
// "A judgment retires with its card"). A judgment records the
// attempt it was raised at (its Attempt), and the card's current attempt past it
// is the card moved on. A brief-defect judgment is outlived once the card no
// longer carries the defect (FieldBriefDefect, unset by the re-brief: briefInPlace),
// and a brief-bound judgment once the card's brief was replaced at or after the
// attempt it was raised at (FieldBriefAttempt). A reader's finding on a brief
// edited before it is not outlived by the edit (the same attempt's new work).
func movedOn(s *Snapshot, n Note, id string) string {
	c := s.Work.Placed(id)
	if c == nil || n.Attempt <= 0 {
		return ""
	}
	if now := c.Int("attempt"); now > n.Attempt {
		return fmt.Sprintf("%s moved to attempt %d (was %d)", id, now, n.Attempt)
	}
	switch n.Type {
	case NBriefDefect:
		if c.F(FieldBriefDefect) == "" {
			return id + " was re-briefed"
		}
	case NBriefWrong:
		if at := c.Int(FieldBriefAttempt); at > 0 && at >= n.Attempt {
			return fmt.Sprintf("%s was re-briefed at attempt %d", id, at)
		}
		// the attempt cap's judgment is outlived by the coordinator's decision on the card
		// (brief_bound.go, FieldDecidedAttempt: a rework, a re-tier, a continue)
		if strings.Contains(n.What, "without a coordinator decision") {
			if at := c.Int(FieldDecidedAttempt); at > 0 && at >= n.Attempt {
				return fmt.Sprintf("%s was decided on at attempt %d", id, at)
			}
		}
	}
	return ""
}

// outlived is the card's-move event when every subject of a judgment has left the
// table or moved past the state it was raised for (docs/SPEC-SPRINT.md section 8,
// "A judgment retires with its card"): the judgment holds no card that still
// needs a decision, so the coordinator may ack it (Ack, steps_ack.go), and the
// tick retires it (RetireJudgments).
func outlived(s *Snapshot, n Note, entries []Open) (string, bool) {
	if len(entries) == 0 {
		return "", false
	}
	var events []string
	for _, o := range entries {
		e := offTable(s, o.Subject())
		if e == "" {
			e = friendOffTable(s, n, o.Subject())
		}
		if e == "" {
			e = movedOn(s, n, o.Subject())
		}
		if e == "" {
			return "", false
		}
		events = append(events, e)
	}
	return strings.Join(events, "; "), true
}

// RetireJudgments is the closes and the machine's answers of every open judgment
// whose cards have all left the table or moved past the state the judgment was
// raised for (docs/SPEC-SPRINT.md section 8, "A judgment retires with its card"),
// skipping those the plan closes already: one decided note a judgment, naming
// every card's event.
func RetireJudgments(s *Snapshot, p *Plan, who string) {
	if s.Work == nil {
		return
	}
	closing := map[string]bool{}
	for _, o := range p.Closes {
		closing[o.Key] = true
	}
	var order []string
	events := map[string][]string{}
	moved := map[string]bool{}
	prims := map[string][]string{}
	subject := map[string]Open{}
	for _, o := range s.Open {
		n := o.Note
		if n.Kind != Judgment || closing[o.Key] || n.SprintLevel || contains(aboutTheLanding, n.Type) || n.Type == NNeedBlocked {
			continue
		}
		cards := []string{o.Subject()}
		if n.StreamLevel {
			if !contains(retiresWithCards, n.Type) || len(n.Primaries) == 0 {
				continue
			}
			cards = n.Primaries
		}
		var es []string
		didMove := false
		for _, id := range cards {
			e := offTable(s, id)
			if e == "" {
				e = friendOffTable(s, n, id)
			}
			if e == "" {
				if e = movedOn(s, n, id); e == "" {
					es = nil
					break
				}
				didMove = true
			}
			es = append(es, e)
		}
		if len(es) == 0 {
			continue
		}
		closing[o.Key] = true
		p.Closes = append(p.Closes, o)
		if _, seen := events[n.ID]; !seen {
			order = append(order, n.ID)
			subject[n.ID] = o
		}
		events[n.ID] = append(events[n.ID], es...)
		if didMove {
			moved[n.ID] = true
		}
		if !n.StreamLevel {
			prims[n.ID] = append(prims[n.ID], o.Subject())
		}
	}
	for _, id := range order {
		o := subject[id]
		prefix := RetiredPrefix
		if moved[id] {
			prefix = MovedOnPrefix
		}
		d := decided(o, prefix, who, s.Now, prims[id]...)
		d.What = prefix + strings.Join(events[id], "; ")
		p.Notes = append(p.Notes, d)
	}

	// An orphan judgment retires with one decided note naming its epoch
	// (tla/JudgmentRetire.tla NoOrphanAfterTick; docs/SPEC-SPRINT.md section 8,
	// "A judgment retires with its card").
	for _, o := range OrphanJudgments(s) {
		if closing[o.Open.Key] {
			continue
		}
		closing[o.Open.Key] = true
		p.Closes = append(p.Closes, o.Open)
		what := fmt.Sprintf("retired: %s is not on the table of epoch %d", o.Target, s.Epoch)
		if e := offTable(s, o.Target); e != "" {
			what = RetiredPrefix + e
		}
		if e := friendOffTable(s, o.Open.Note, o.Target); e != "" {
			what = RetiredPrefix + e
		}
		d := decided(o.Open, what, MachineActor, s.Now)
		p.Notes = append(p.Notes, d)
	}
	if len(p.Closes) > 0 {
		retiredIDs := map[string]bool{}
		for _, c := range p.Closes {
			if c.Note.ID != "" {
				retiredIDs[c.Note.ID] = true
			}
		}
		isRetiredRule8 := func(n Note) bool {
			if n.Kind != Judgment || n.Type != NInvariant || !strings.HasPrefix(n.What, "rule 8: judgment ") {
				return false
			}
			rest := strings.TrimPrefix(n.What, "rule 8: judgment ")
			id, _, ok := strings.Cut(rest, " ")
			return ok && retiredIDs[id]
		}
		kept := p.Notes[:0]
		for _, n := range p.Notes {
			if !isRetiredRule8(n) {
				kept = append(kept, n)
			}
		}
		p.Notes = kept
		for _, o := range s.Open {
			if !closing[o.Key] && isRetiredRule8(o.Note) {
				closing[o.Key] = true
				p.Closes = append(p.Closes, o)
			}
		}
	}
}

// Orphan is an open judgment naming an off-table or absent card or stream
// (tla/JudgmentRetire.tla NoOrphanAfterTick; docs/SPEC-SPRINT.md section 8,
// "A judgment retires with its card", and section 9).
type Orphan struct {
	ID     string
	Type   string
	Target string
	Open   Open
	Absent bool
}

// isNonCardSubject reports whether sub is not a card (docs/SPEC-SPRINT.md
// section 8, "A judgment retires with its card"): FriendRow(f) and member rows,
// StreamSubject(TierSubject(t)), ProviderSubject(p), and SprintSubject.
func isNonCardSubject(s *Snapshot, sub string) bool {
	if sub == "" || sub == SprintSubject || strings.HasPrefix(sub, "sprint:") {
		return true
	}
	if strings.Contains(sub, "@") || strings.Contains(sub, "/") {
		return true
	}
	if f, ok := FriendOfRow(strings.TrimPrefix(strings.TrimPrefix(sub, "stream:"), "member:")); ok {
		return friendOnTable(s, Note{}, sub, f)
	}
	if strings.HasPrefix(sub, "stream:friend") {
		return true
	}
	if s != nil && (s.Work == nil || s.Work.Card(sub) == nil) && friendOnTable(s, Note{}, sub, sub) {
		return true
	}
	if strings.HasPrefix(sub, "timer:") || strings.HasPrefix(sub, "stream:timer:") {
		return true
	}
	if strings.HasPrefix(sub, "tier:") || strings.HasPrefix(sub, "stream:tier:") {
		return true
	}
	if strings.HasPrefix(sub, "provider:") || strings.HasPrefix(sub, "stream:provider:") {
		return true
	}
	if strings.HasPrefix(sub, "member:") || strings.HasPrefix(sub, "stream:member:") {
		return true
	}
	if strings.HasPrefix(sub, "reader:") || strings.HasPrefix(sub, "stream:reader:") {
		return true
	}
	if s != nil && s.Fleet != nil && (s.Fleet.HasRow(sub) || (strings.HasPrefix(sub, "stream:") && s.Fleet.HasRow(strings.TrimPrefix(sub, "stream:")))) {
		return true
	}
	if s != nil && s.Readers != nil && (s.Readers.HasRow(sub) || (strings.HasPrefix(sub, "stream:") && s.Readers.HasRow(strings.TrimPrefix(sub, "stream:")))) {
		return true
	}
	if !strings.HasPrefix(sub, "stream:") && strings.Contains(sub, ":") {
		return true
	}
	return false
}

// OrphanJudgments reports each open judgment naming an off-table or absent card
// or stream (tla/JudgmentRetire.tla NoOrphanAfterTick; docs/SPEC-SPRINT.md section 8
// and section 9). On a snapshot that loaded the named cards, any judgment returned
// is one the tick has failed to retire.
func OrphanJudgments(s *Snapshot) []Orphan {
	if s == nil || s.Work == nil {
		return nil
	}
	var out []Orphan
	seen := map[string]bool{}
	for _, o := range s.Open {
		n := o.Note
		if n.Kind != Judgment || n.SprintLevel || contains(aboutTheLanding, n.Type) || n.Type == NNeedBlocked {
			continue
		}
		if seen[n.ID] {
			continue
		}
		sub := o.Subject()
		if f, ok := friendSubject(s, n, sub); ok {
			if !friendOnTable(s, n, sub, f) {
				seen[n.ID] = true
				out = append(out, Orphan{ID: n.ID, Type: n.Type, Target: sub, Open: o, Absent: true})
			}
			continue
		}

		// Stream-level judgments:
		if n.StreamLevel {
			// A stream-level judgment that retires with cards (e.g. NNoRoute, NStarving on tier:frontier)
			// whose cards have no record once loaded.
			if contains(retiresWithCards, n.Type) && len(n.Primaries) > 0 {
				allAbsent := true
				target := ""
				for _, pid := range n.Primaries {
					c := s.Work.Card(pid)
					if c != nil {
						allAbsent = false
						break
					}
					if target == "" {
						target = pid
					}
				}
				if allAbsent && target != "" {
					seen[n.ID] = true
					out = append(out, Orphan{ID: n.ID, Type: n.Type, Target: target, Open: o, Absent: true})
				}
				continue
			}

			// A judgment naming a removed or absent work stream (s.StreamCtl(st) == nil).
			// Only true work streams (non-empty, no colons, not a member/reader/friend/provider/tier row).
			st := n.Stream
			if st == "" && strings.HasPrefix(sub, "stream:") {
				st = strings.TrimPrefix(sub, "stream:")
			}
			if st == "" || isNonCardSubject(s, st) || strings.Contains(st, ":") {
				continue
			}
			if s.StreamCtl(st) == nil {
				seen[n.ID] = true
				out = append(out, Orphan{ID: n.ID, Type: n.Type, Target: st, Open: o, Absent: true})
			}
			continue
		}

		// Card-level judgments (!n.StreamLevel):
		// Subjects that are not cards are never orphans.
		if isNonCardSubject(s, sub) || strings.Contains(sub, ":") || sub == "" {
			continue
		}
		c := s.Work.Card(sub)
		if c == nil {
			seen[n.ID] = true
			out = append(out, Orphan{ID: n.ID, Type: n.Type, Target: sub, Open: o, Absent: true})
		}
	}
	return out
}
