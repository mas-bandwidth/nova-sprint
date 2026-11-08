package worker

import (
	"cmp"
	"strings"
)

// ReadOutcome is a read's report: ok, fix or fault, and nothing else. The
// store refuses "fail" for a read ("a read consumer reports ok, fix or
// fault", defect 67: a read whose harness wrote no verdict became "fail",
// was refused at every send, and its card sat working with no process), so
// a read's report is built only from a ReadOutcome, and none holds "fail":
// its value is unexported and the three below are the only ones.
type ReadOutcome struct{ v string }

var (
	ReadOK    = ReadOutcome{"ok"}
	ReadFix   = ReadOutcome{"fix"}
	ReadFault = ReadOutcome{"fault"}
)

func (r ReadOutcome) String() string { return r.v }

// ReadOutcomeOf is a read job's classified outcome as a read's: ok and fix
// are themselves, and fault is a fault; a harness's own verdict of fail or
// broken is a fix with its reason as the finding (the head does not meet
// the card); no verdict at all (no result.json, one that does not parse or
// names no verdict this worker knows, a kill) is a fault of the harness,
// bounded by the fault limit and never a verdict on the head. A record
// written before NoVerdict existed says so in its reason (it names
// result.json), so a stale record heals at its next send.
func ReadOutcomeOf(o Outcome) (ReadOutcome, Outcome) {
	switch o.Result {
	case "ok":
		return ReadOK, o
	case "fix":
		o.Finding = cmp.Or(o.Finding, o.Reason)
		return ReadFix, o
	case "fail":
		if o.NoVerdict || strings.Contains(o.Reason, "result.json") {
			o.Result, o.NoVerdict = "fault", true
			return ReadFault, o
		}
		o.Result, o.Finding = "fix", cmp.Or(o.Finding, o.Reason)
		return ReadFix, o
	}
	o.Result = "fault"
	o.Reason = cmp.Or(o.Reason, "the read's harness gave no verdict")
	return ReadFault, o
}

// reportOutcome_internal is the outcome a record reports: a read's through
// ReadOutcomeOf (so a read never reports "fail"), a work job's as it is.
func reportOutcome_internal(rec JobRecord) Outcome {
	if rec.Kind != "read" {
		return rec.Outcome
	}
	_, o := ReadOutcomeOf(rec.Outcome)
	return o
}

// noVerdict_internal is the outcome of a harness that wrote no verdict
// (no result.json): "fail: no verdict" for a work job (the harness note of
// 2026-11-07), with why and the harness's last output as the reason. A read
// maps it to a fault (ReadOutcomeOf).
func noVerdict_internal(why, tail string) Outcome {
	reason := "no verdict: " + why
	if tail = strings.TrimSpace(tail); tail != "" {
		reason += "; the harness's last output: " + tail
	}
	return Outcome{Result: "fail", Reason: reason, NoVerdict: true}
}
