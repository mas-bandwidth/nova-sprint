package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// audit is a read: any actor may run it (coordinator.go's verb classes).
func init() { verbClasses["audit"] = classRead }

// cmdAudit is one answer to "is anything blocked, stuck or looped": every
// unlanded primary with its holders, ages, ends and needs, and its flags
// (docs/SPEC-SPRINT.md section 11; sprint.Audit). One line per primary, then
// `AUDIT OK cards=<n> flagged=<n>`; --json is one object a line.
func (a *app) cmdAudit(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("audit")
	stream := fs.String("stream", "", "only this stream's primaries")
	stuck := fs.Duration("stuck", sprint.AuditStuckDefault, "a primary unchanged in its state for longer is STUCK")
	if pos, err := parse(fs, args); err != nil || len(pos) > 0 {
		return refuse(stderr, "audit", argErr("takes no words ", err, pos...))
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "audit", err.Error())
	}
	s, err := st.Load(context.Background(), []string{sprint.Work, sprint.Fleet, sprint.Readers}, nil)
	if err != nil {
		return a.readFailed("audit", err, stderr)
	}
	res := sprint.Audit(s, sprint.AuditOpts{Stream: *stream, Stuck: *stuck})
	if c.json {
		for _, row := range res.Rows {
			b, err := json.Marshal(row)
			if err != nil {
				return a.readFailed("audit", err, stderr)
			}
			fmt.Fprintln(stdout, string(b))
		}
		return 0
	}
	for _, row := range res.Rows {
		fmt.Fprintln(stdout, auditLine(row))
	}
	fmt.Fprintf(stdout, "AUDIT OK cards=%d flagged=%d\n", res.Cards, res.Flagged)
	return 0
}

// auditLine is one primary's row as its text line.
func auditLine(r sprint.AuditRow) string {
	words := []string{
		r.ID,
		"stream=" + r.Stream,
		"state=" + r.State,
		"tier=" + r.Tier,
		"attempt=" + itoa(r.Attempt),
		"holder=" + r.Holder,
		"lane=" + orDash(r.Lane),
		"since=" + orDash(r.Since),
		"age_state=" + r.AgeState,
		"age_attempt=" + r.AgeAttempt,
		"ends=" + auditEndsText(r.Ends),
		"needs=" + auditNeedsText(r.Needs),
	}
	if len(r.Flags) > 0 {
		words = append(words, "flags="+strings.Join(r.Flags, ","))
	} else {
		words = append(words, "flags=-")
	}
	return strings.Join(words, " ")
}

// auditEndsText is the ends as "k:end,...".
func auditEndsText(ends []sprint.AuditAttempt) string {
	if len(ends) == 0 {
		return "-"
	}
	var out []string
	for _, e := range ends {
		out = append(out, itoa(e.Attempt)+":"+orDash(e.End))
	}
	return strings.Join(out, ",")
}

// auditNeedsText is the needs as "id:state,...".
func auditNeedsText(needs []sprint.AuditNeed) string {
	if len(needs) == 0 {
		return "-"
	}
	var out []string
	for _, n := range needs {
		out = append(out, n.ID+":"+n.State)
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

// orDash is s, "-" when it is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// itoa is an int as text.
func itoa(n int) string { return strconv.Itoa(n) }
