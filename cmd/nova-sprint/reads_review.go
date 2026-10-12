package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// reads is a new verb name beside the worker's read (verbs.go): `read` is the
// worker's report of its own reads, `reads --review` is the coordinator's read
// of who holds each read of every primary in review.
func init() { verbClasses["reads"] = classRead }

// ReviewRead is one read card of a primary in review at its attempt: who holds
// it (a member's reader row, or a friend), in what state, for how long, and its
// last read outcome.
type ReviewRead struct {
	Card    string `json:"card"`
	Reader  string `json:"reader"`
	State   string `json:"state"`
	For     string `json:"for"`
	Outcome string `json:"outcome"`
}

// ReviewRow is one primary in review and each of its read cards at its attempt.
type ReviewRow struct {
	ID        string       `json:"id"`
	Stream    string       `json:"stream"`
	Attempt   int          `json:"attempt"`
	Reads     []ReviewRead `json:"reads,omitempty"`
	NoReadOut bool         `json:"no_read_out,omitempty"`
}

// cmdReads is one answer to "who holds each review read" (docs/SPEC-SPRINT.md
// section 11, reads --review): one line per primary in review with its attempt,
// then each of its read cards at that attempt (the reader, its state, for how
// long, and its last read outcome), and NO-READ-OUT on a card with no read out
// and no open judgment. It reads the tables once, never one card at a time.
func (a *app) cmdReads(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("reads")
	review := fs.Bool("review", false, "every primary in review and each of its read cards at its attempt: who holds each read, in what state, for how long, and the last read outcome")
	stream := fs.String("stream", "", "only this stream's primaries")
	if pos, err := parse(fs, args); err != nil || len(pos) > 0 {
		return refuse(stderr, "reads", argErr("takes no words ", err, pos...))
	}
	if !*review {
		return refuse(stderr, "reads", "wants --review: it lists every primary in review and who holds each of its reads")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "reads", err.Error())
	}
	s, err := st.Load(context.Background(), []string{sprint.Work, sprint.Fleet, sprint.Readers}, func(s *sprint.Snapshot) map[string][]string {
		out := map[string][]string{}
		if fleet := sprint.ReadCardExtras(s); len(fleet) > 0 {
			out[sprint.Fleet] = fleet
		}
		if readers := reviewReaderExtras(s); len(readers) > 0 {
			out[sprint.Readers] = readers
		}
		return out
	})
	if err != nil {
		return a.readFailed("reads", err, stderr)
	}
	rows := reviewRows(s, *stream)
	if c.json {
		for _, row := range rows {
			b, err := json.Marshal(row)
			if err != nil {
				return a.readFailed("reads", err, stderr)
			}
			fmt.Fprintln(stdout, string(b))
		}
		return 0
	}
	noReadOut := 0
	for _, row := range rows {
		line := row.ID + " stream=" + row.Stream + " attempt=" + itoa(row.Attempt)
		if row.NoReadOut {
			noReadOut++
			line += " NO-READ-OUT"
		}
		fmt.Fprintln(stdout, line)
		for _, rc := range row.Reads {
			fmt.Fprintf(stdout, "  %s reader=%s state=%s for=%s outcome=%s\n",
				rc.Card, rc.Reader, rc.State, rc.For, rc.Outcome)
		}
	}
	fmt.Fprintf(stdout, "READS OK review=%d no-read-out=%d\n", len(rows), noReadOut)
	return 0
}

// reviewRows is every primary in review (one stream's with stream set), each
// with its read cards at the attempt, in stream then id order.
func reviewRows(s *sprint.Snapshot, stream string) []ReviewRow {
	var rows []ReviewRow
	for _, pr := range s.Work.Column(sprint.Review) {
		if stream != "" && pr.Row != stream {
			continue
		}
		attempt := pr.Int("attempt")
		if attempt < 1 {
			attempt = 1
		}
		row := ReviewRow{ID: pr.ID, Stream: pr.Row, Attempt: attempt, Reads: []ReviewRead{}}
		for _, rc := range readsAtAttempt(s, pr, attempt) {
			row.Reads = append(row.Reads, ReviewRead{
				Card:    rc.ID,
				Reader:  rc.F("reader"),
				State:   readState(rc),
				For:     readFor(s, rc),
				Outcome: readOutcome(rc),
			})
		}
		if len(row.Reads) == 0 && !hasOpenJudgment(s, pr.ID) {
			row.NoReadOut = true
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b ReviewRow) int {
		if c := strings.Compare(a.Stream, b.Stream); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return rows
}

// reviewReaderExtras is the readers-table read card ids of every primary in
// review at its attempt, plain and second identity, that are not placed: the
// retired reads a table load would not otherwise carry.
func reviewReaderExtras(s *sprint.Snapshot) []string {
	if s == nil || s.Readers == nil || s.Work == nil {
		return nil
	}
	var ids []string
	for _, pr := range s.Work.Column(sprint.Review) {
		attempt := pr.Int("attempt")
		if attempt < 1 {
			attempt = 1
		}
		for _, r := range s.Readers.Rows() {
			for _, id := range sprint.ReadCardIDs(pr.ID, attempt, r) {
				if s.Readers.Card(id) == nil {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// readsAtAttempt is the primary's read cards at its attempt, both tables: the
// readers table's (asked, reading, and retired) and the fleet table's read
// cards (a friend's, and a member's while read cards are on).
func readsAtAttempt(s *sprint.Snapshot, pr *sprint.Card, attempt int) []*sprint.Card {
	var out []*sprint.Card
	if s.Readers != nil {
		for _, r := range s.Readers.Rows() {
			for _, id := range sprint.ReadCardIDs(pr.ID, attempt, r) {
				if c := s.Readers.Card(id); c != nil {
					out = append(out, c)
				}
			}
		}
	}
	if s.Fleet != nil {
		for _, c := range s.Fleet.WithPrefix(pr.ID + ".r" + strconv.Itoa(attempt) + ".") {
			if c.F("kind") == "read" {
				out = append(out, c)
			}
		}
	}
	slices.SortFunc(out, func(a, b *sprint.Card) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// readState is a read card's state: asked (out, not begun), reading (in
// flight), returned (handed back with no verdict), withdrawn (taken back by the
// machine or a hold), or ended (a verdict, or the reader's environment ended
// it).
func readState(rc *sprint.Card) string {
	if rc.Placed() && rc.F(sprint.FieldReturned) != "" {
		return "returned"
	}
	switch {
	case rc.Col == sprint.Withdrawn:
		return "withdrawn"
	case rc.Placed():
		switch rc.Col {
		case sprint.Asked, sprint.Ready:
			return "asked"
		case sprint.Reading, sprint.Working:
			return "reading"
		case sprint.OK, sprint.Broken:
			return "ended"
		}
		return string(rc.Col)
	}
	switch rc.F("retired_by") {
	case sprint.RetiredByReturned:
		return "returned"
	case sprint.RetiredByEnvironment:
		return "ended"
	}
	if rc.F("verdict") != "" || rc.F("read") != "" {
		return "ended"
	}
	return "withdrawn"
}

// readOutcome is a read card's last read outcome: its verdict, else the reason
// its reader returned it (which is `no verdict (ran=false) ...` for a read that
// never ran) or a hold took it back, else how it was retired, else "-".
func readOutcome(rc *sprint.Card) string {
	if v := rc.F("verdict"); v != "" {
		return v
	}
	if r := rc.F("reason"); r != "" {
		return r
	}
	if by := rc.F("retired_by"); by != "" {
		return by
	}
	return "-"
}

// readFor is how long a read card has been in its current state, from its begin
// (else its ask) to its end (its read, return or retirement), or to now while it
// is still out.
func readFor(s *sprint.Snapshot, rc *sprint.Card) string {
	start := stampOf(rc, "begun")
	if start.IsZero() {
		start = stampOf(rc, "asked")
	}
	if start.IsZero() {
		return "-"
	}
	end := s.Now
	for _, f := range []string{"read", "retired", sprint.FieldReturned} {
		if t := stampOf(rc, f); !t.IsZero() {
			end = t
			break
		}
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}

// stampOf is a read card's stamp field as a time, zero when absent or unreadable.
func stampOf(rc *sprint.Card, field string) time.Time {
	t, _ := time.Parse(time.RFC3339, rc.F(field))
	return t
}

// hasOpenJudgment says an open judgment names the primary as its subject or as
// one of the primaries it is about.
func hasOpenJudgment(s *sprint.Snapshot, id string) bool {
	for _, o := range s.Open {
		if o.Subject() == id || slices.Contains(o.Note.Primaries, id) {
			return true
		}
	}
	return false
}
