package sprint

import (
	"cmp"
	"strings"
	"time"
)

// The audit (docs/SPEC-SPRINT.md section 11, "audit"; the seat's hour of two
// hand tools, audit.sh and minuteloop.py, of 2026-10-12): one read of the work,
// fleet and readers tables answers "is anything blocked, stuck or looped". One
// row per unlanded primary names its id, stream, state, tier and attempt, its
// holder (member or friend, the lane and since when) or "-", the age in its
// current state and attempt (from the card's own stamps, never a watcher's
// memory), the ends of its last four attempts, and the needs not yet landed
// with their states, and flags what is wrong.

// AuditOpts narrows and tunes an audit.
type AuditOpts struct {
	Stream string        // only this stream, "" every stream
	Stuck  time.Duration // a card unchanged past this is STUCK; 0 is the default
}

// AuditStuckDefault is the age past which an unchanged card is STUCK.
const AuditStuckDefault = 10 * time.Minute

// The flags an audit row carries.
const (
	FlagNoHolder  = "NO-HOLDER" // ready/waiting past the dealt bound, or working with no holder
	FlagAttempt   = "ATT>=4"    // at its fourth attempt or beyond
	FlagFails     = "FAILS"     // six or more failed, idle, bound or brief-defect ends
	FlagStuck     = "STUCK"     // unchanged past the --stuck bound
	FlagNeedsGone = "NEEDS-GONE"
)

// AuditFailsBound is the count of failed ends at which FAILS is raised.
const AuditFailsBound = 6

// AuditAttempt is one attempt's end as the audit prints it: the attempt and its
// end ("ok", a failure class, "brief-defect", "idle", "bound", or "-").
type AuditAttempt struct {
	Attempt int    `json:"attempt"`
	End     string `json:"end"`
}

// AuditNeed is one of a primary's needs that has not landed.
type AuditNeed struct {
	ID    string `json:"id"`
	State string `json:"state"` // its state, "-" when it is no card
}

// AuditRow is one unlanded primary.
type AuditRow struct {
	ID         string         `json:"id"`
	Stream     string         `json:"stream"`
	State      string         `json:"state"`
	Tier       string         `json:"tier"`
	Attempt    int            `json:"attempt"`
	Holder     string         `json:"holder"`          // the member or friend, "-" when none
	Lane       string         `json:"lane,omitempty"`  // the holder's row/lane
	Since      string         `json:"since,omitempty"` // since when it has held it
	AgeState   string         `json:"age_state"`       // in its current state
	AgeAttempt string         `json:"age_attempt"`     // since its attempt began
	Ends       []AuditAttempt `json:"ends"`            // the last four attempts
	Needs      []AuditNeed    `json:"needs,omitempty"` // needs not yet landed
	Flags      []string       `json:"flags,omitempty"` // what is wrong
}

// AuditResult is the audit: every row and the counts.
type AuditResult struct {
	Cards   int        `json:"cards"`
	Flagged int        `json:"flagged"`
	Rows    []AuditRow `json:"rows"`
}

// Audit names every unlanded primary and what is wrong with it
// (docs/SPEC-SPRINT.md section 11). It reads the snapshot once, never the store.
func Audit(s *Snapshot, o AuditOpts) AuditResult {
	if o.Stuck <= 0 {
		o.Stuck = AuditStuckDefault
	}
	res := AuditResult{Rows: []AuditRow{}}
	bound := s.DealtMax()
	for _, pr := range statsPrimaries(s) {
		if o.Stream != "" && pr.Row != o.Stream {
			continue
		}
		res.Cards++
		row := auditRow(s, pr, bound, o.Stuck)
		if len(row.Flags) > 0 {
			res.Flagged++
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}

// auditRow is one primary's row, its flags raised by the rules below.
func auditRow(s *Snapshot, pr *Card, bound, stuck time.Duration) AuditRow {
	holder, lane, since := auditHolder(s, pr)
	attempt := pr.Int("attempt")
	if attempt < 1 {
		attempt = 1
	}
	stateAt := auditStateAt(s, pr)
	attemptAt := auditAttemptAt(s, pr, attempt)
	row := AuditRow{
		ID:         pr.ID,
		Stream:     pr.Row,
		State:      pr.Col,
		Tier:       CardTier(pr),
		Attempt:    attempt,
		Holder:     cmp.Or(holder, "-"),
		Lane:       lane,
		Since:      since,
		AgeState:   auditAge(s.Now, stateAt),
		AgeAttempt: auditAge(s.Now, attemptAt),
		Ends:       auditEnds(s, pr, attempt),
	}
	// NO-HOLDER: in a ready-to-run state past the dealt bound, or working with no holder.
	if (pr.Col == Waiting || pr.Col == Ready) && holder == "" && !stateAt.IsZero() && s.Now.Sub(stateAt) > bound {
		row.Flags = append(row.Flags, FlagNoHolder)
	} else if pr.Col == Working && holder == "" {
		row.Flags = append(row.Flags, FlagNoHolder)
	}
	// ATT>=4
	if attempt >= 4 {
		row.Flags = append(row.Flags, FlagAttempt)
	}
	// FAILS=<n>: six or more failed, idle-reaped, bound or brief-defect ends.
	if n := auditFailCount(s, pr, attempt); n >= AuditFailsBound {
		row.Flags = append(row.Flags, FlagFails+"="+itoa(n))
	}
	// STUCK: unchanged past the --stuck bound.
	if !stateAt.IsZero() && s.Now.Sub(stateAt) > stuck {
		row.Flags = append(row.Flags, FlagStuck)
	}
	// NEEDS-GONE: a need that is no card.
	gone := false
	for _, need := range auditNeeds(s, pr) {
		row.Needs = append(row.Needs, need)
		if need.State == "-" {
			gone = true
		}
	}
	if gone {
		row.Flags = append(row.Flags, FlagNeedsGone)
	}
	return row
}

// auditHolder is who holds the primary now: the member or friend whose fleet row
// holds its working card, the lane (the row) and since when (the card's taken,
// else dealt). "" when no one does.
func auditHolder(s *Snapshot, pr *Card) (holder, lane, since string) {
	for _, w := range s.Fleet.Of(pr.ID) {
		if w.Col != Working {
			continue
		}
		name := w.Row
		if f, ok := FriendOfRow(w.Row); ok {
			name = f
		}
		// the one deadline reads a work card's deal and take stamps; own is
		// "taken" while it works, "dealt" while it waits.
		field, _, _, own := WorkDeadline(s, w)
		at := w.F(cmp.Or(own, field))
		return name, w.Row, at
	}
	return "", "", ""
}

// auditStateAt is when the primary entered its current state, from its own
// stamps: waiting and ready from its admission, working from its take (else its
// deal), review and merging from its finish (else its take). Zero when none is
// there.
func auditStateAt(s *Snapshot, pr *Card) time.Time {
	switch pr.Col {
	case Working:
		if wc := s.Fleet.Card(cmp.Or(pr.F("work"), WorkCardID(pr.ID, pr.Int("attempt")))); wc != nil {
			if t := stampAt(wc, "taken"); !t.IsZero() {
				return t
			}
			if t := stampAt(wc, "dealt"); !t.IsZero() {
				return t
			}
		}
	}
	return lastStamp(pr, "finished", "accepted", "admitted", "ready_at", "dealt")
}

// auditAttemptAt is when the primary's attempt began: its work card's dealt,
// else its taken, else the primary's admission.
func auditAttemptAt(s *Snapshot, pr *Card, attempt int) time.Time {
	if wc := s.Fleet.Card(cmp.Or(pr.F("work"), WorkCardID(pr.ID, attempt))); wc != nil {
		if t := stampAt(wc, "dealt"); !t.IsZero() {
			return t
		}
		if t := stampAt(wc, "taken"); !t.IsZero() {
			return t
		}
	}
	return stampAt(pr, "admitted")
}

// auditEnds is the ends of the primary's last four attempts, oldest first.
func auditEnds(s *Snapshot, pr *Card, attempt int) []AuditAttempt {
	lo := max(1, attempt-3)
	out := []AuditAttempt{}
	for k := lo; k <= attempt; k++ {
		wc := s.Fleet.Card(WorkCardID(pr.ID, k))
		if wc == nil {
			continue
		}
		out = append(out, AuditAttempt{Attempt: k, End: auditEnd(wc)})
	}
	return out
}

// auditEnd is one attempt's end as its work card records it.
func auditEnd(wc *Card) string {
	if e := wc.F("end"); e != "" {
		return e
	}
	switch wc.Col {
	case "ok":
		return "ok"
	case DoneFailed:
		return cmp.Or(wc.F(FieldFailure), "failed")
	case DoneDefect:
		return "brief-defect"
	case "withdrawn":
		if wc.F(FieldFailureBound) != "" {
			return "bound"
		}
		return "withdrawn"
	}
	if wc.F("idle") != "" {
		return "idle"
	}
	return string(wc.Col)
}

// auditFailCount counts every failed, idle-reaped, bound or brief-defect end
// across the primary's attempts.
func auditFailCount(s *Snapshot, pr *Card, attempt int) int {
	var ends []AuditAttempt
	for k := 1; k <= attempt; k++ {
		if wc := s.Fleet.Card(WorkCardID(pr.ID, k)); wc != nil {
			ends = append(ends, AuditAttempt{Attempt: k, End: auditEnd(wc)})
		}
	}
	return auditFails(ends)
}

// auditFails counts the ends that are failed, idle-reaped, bound or
// brief-defect.
func auditFails(ends []AuditAttempt) int {
	n := 0
	for _, e := range ends {
		x := strings.ToLower(e.End)
		if x == "" || x == "ok" {
			continue
		}
		if strings.Contains(x, "fail") || strings.Contains(x, "idle") ||
			strings.Contains(x, "bound") || strings.Contains(x, "defect") {
			n++
		}
	}
	return n
}

// auditNeeds is the primary's named needs that have not landed, each with its
// state ("-" when no card holds it).
func auditNeeds(s *Snapshot, pr *Card) []AuditNeed {
	var out []AuditNeed
	for _, n := range Split(pr.F("needs")) {
		st := s.StateOf(n)
		if st == Landed {
			continue
		}
		if s.Work.Card(n) == nil {
			out = append(out, AuditNeed{ID: n, State: "-"})
			continue
		}
		out = append(out, AuditNeed{ID: n, State: string(st)})
	}
	return out
}

// auditAge is now - at as a duration to the second, "-" when at is zero.
func auditAge(now, at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	d := now.Sub(at)
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}
