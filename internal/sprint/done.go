package sprint

import "time"

// A row's done is every card it completed (the owner, 2026-10-11: "Done is every card
// completed."; docs/SPEC-SPRINT.md section 1, done and ok%): each kind of card a row is dealt
// counts once it completes, by the kind's own rule, registered in DoneKinds. A kind is counted
// from the log line that completes it (the record its completion leaves: the card, the row
// that completed it, when), never from the row's cells, so a card that leaves the row after
// (a read card retires off its row with its verdict; a tidy moves done work off) still counts.
// A new kind (a merge card, once merges are dealt cards) needs only its entry here.
//
// ok% is the work's measure alone (the readers' verdicts on the row's work, its ok over its ok
// and failed cells) and is not counted from here.

// Completion is one card a row completed: its kind, the card, the fleet row whose done it
// counts in, and when.
type Completion struct {
	Kind string    `json:"kind"`
	Card string    `json:"card"`
	Row  string    `json:"row"`
	At   time.Time `json:"at"`
}

// DoneKind is one kind of card a row completes: Of says the cards the log line completes of
// this kind, and the row each counts in (none when the line completes no card of the kind).
type DoneKind struct {
	Name string
	Of   func(l Line) []Completion
}

// The kinds a row's done counts.
const (
	DoneKindWork = "work" // a work card at its first attempt the readers' verdict moved to ok
	DoneKindFix  = "fix"  // a work card at a later attempt (a rework, dealt with its fix) moved to ok
	DoneKindRead = "read" // a read card its reader completed with a verdict, ok or broken
)

// DoneKinds is every kind a row's done counts, in the order where names them.
var DoneKinds = []DoneKind{
	{Name: DoneKindWork, Of: workDone(func(attempt int) bool { return attempt == 1 }, DoneKindWork)},
	{Name: DoneKindFix, Of: workDone(func(attempt int) bool { return attempt > 1 }, DoneKindFix)},
	{Name: DoneKindRead, Of: readDone},
}

// CompletionsOf is every card the log line completes, of every kind in DoneKinds.
func CompletionsOf(l Line) []Completion {
	if l.Note != nil {
		return nil
	}
	var out []Completion
	for _, k := range DoneKinds {
		out = append(out, k.Of(l)...)
	}
	return out
}

// doneRowCol is the row and the column of a move line's place ("member:column").
func doneRowCol(p string) (row, col string) {
	i := len(p) - 1
	for i >= 0 && p[i] != ':' {
		i--
	}
	if i < 0 {
		return p, ""
	}
	return p[:i], p[i+1:]
}

// workDone is a work kind: a move of a work card into its row's ok cell, the readers' ok
// verdict (TickVerdicts; a log from before the verdicts counted moved it there at the finish),
// at an attempt the kind takes.
func workDone(takes func(attempt int) bool, kind string) func(Line) []Completion {
	return func(l Line) []Completion {
		if l.Table != Fleet || l.Removed {
			return nil
		}
		row, col := doneRowCol(l.To)
		if col != DoneOK || row == "" {
			return nil
		}
		if _, from := doneRowCol(l.From); from == DoneOK {
			return nil // a move within ok is no completion
		}
		var out []Completion
		for _, id := range l.Names() {
			if _, attempt, ok := ParseWorkCard(id); ok && takes(attempt) {
				out = append(out, Completion{Kind: kind, Card: id, Row: row, At: l.At})
			}
		}
		return out
	}
}

// readDone is the read kind: a read card its reader closed with a verdict, ok or broken. On
// the fleet table the read retires off its reader's row with its verdict (the row it leaves
// is the reader's: a friend's friend.<name>, a member's <name>); on the readers table it moves
// to the reader's ok or broken cell, and counts on the fleet row of the reader's machine
// (reader-<m> reads for m). A read handed back (--return), taken back or let pass carries no
// verdict and completes nothing.
func readDone(l Line) []Completion {
	var row string
	switch {
	case l.Table == Fleet && l.Removed:
		if v := l.Set["verdict"]; v != "ok" && v != "broken" {
			return nil
		}
		row, _ = doneRowCol(l.From)
	case l.Table == Readers && !l.Removed:
		r, col := doneRowCol(l.To)
		if col != OK && col != Broken {
			return nil
		}
		if _, from := doneRowCol(l.From); from == OK || from == Broken {
			return nil
		}
		m, ok := ReaderMachine(r)
		if !ok {
			return nil
		}
		row = m
	default:
		return nil
	}
	if row == "" {
		return nil
	}
	var out []Completion
	for _, id := range l.Names() {
		if _, _, _, ok := ParseReadCard(id); ok {
			out = append(out, Completion{Kind: DoneKindRead, Card: id, Row: row, At: l.At})
		}
	}
	return out
}

// DoneCounts is each fleet row's completions by kind.
type DoneCounts map[string]map[string]int

// CountDone is the completions after since (every one when since is zero: a stats reset's mark
// counts what completes after it, so a reset shows done 0), each card of
// a kind counted once (a line read again, or a card completed twice, is one completion), by
// row and kind.
func CountDone(cs []Completion, since time.Time) DoneCounts {
	out := DoneCounts{}
	seen := map[[3]string]bool{}
	for _, c := range cs {
		if !since.IsZero() && !c.At.After(since) {
			continue
		}
		k := [3]string{c.Row, c.Kind, c.Card}
		if seen[k] {
			continue
		}
		seen[k] = true
		if out[c.Row] == nil {
			out[c.Row] = map[string]int{}
		}
		out[c.Row][c.Kind]++
	}
	return out
}

// Total is the row's done: its completions of every kind.
func (d DoneCounts) Total(row string) int {
	n := 0
	for _, k := range DoneKinds {
		n += d[row][k.Name]
	}
	return n
}
