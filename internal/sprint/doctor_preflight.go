package sprint

import (
	"cmp"
	"strings"
)

// The doctor's preflight (the owner, 2026-10-11: "Also consider sprint doctor in this
// process"; docs/SPEC-SPRINT.md section 14, "The server"). `doctor --preflight
// <candidate build>` is the release gate's frame, run before any server switch or
// adopt: it holds the candidate's `version` and its `tick --shadow --json` against
// the store this doctor reads, and it names the checks of the gate whose cards have
// not landed yet, so a preflight always says what it did not measure. This is its
// first check, the store's tables against the candidate's TABLES.lock (the v1.2.10
// server ran on a store whose fleet table lacked a locked column); the member,
// mirror, link and stable checks are later cards and fill in preflightLaterChecks.
//
// The rows are a pure function over the facts the verb read (PreflightInput): the
// candidate's version answer and its shadow tick's code and text, so the rule is
// tested without a store or a binary.

// AreaPreflight is the doctor's preflight area, the release gate's frame.
const AreaPreflight = "preflight"

// The preflight's own checks, by name.
const (
	CheckPreflightVersion = "version"
	CheckPreflightSchema  = "schema"
)

// preflightLockedShapeMark is the mark a locked-shape refusal carries
// (internal/sprint/store/locked_shape.go through cmd/nova-sprint/shadow.go): the
// store's tables lack a column TABLES.lock names, and the error names the exact
// nova-table commands that mend it.
const preflightLockedShapeMark = "(internal/sprint/TABLES.lock)"

// PreflightInput is what `doctor --preflight` read: the candidate's version answer
// and its shadow tick's result, all of it facts. A field left zero was not answered
// and its row says so.
type PreflightInput struct {
	// Binary is the candidate build's path, the subject of a version row when the
	// candidate did not answer.
	Binary string
	// Version is the candidate's version answer (its version word), "" when it did
	// not answer; VersionErr says why.
	Version    string
	VersionErr string
	// ShadowCode is `tick --shadow --json`'s exit: 0 made the plan, another number
	// refused it. ShadowText is its error text whole, the locked-shape refusal naming
	// the columns and the nova-table commands.
	ShadowCode int
	ShadowText string
}

// DoctorPreflight is the release gate's frame over one read: the candidate's version,
// its shadow tick against the store's schema, and the gate's later checks each WARN
// until its card lands (docs/SPEC-SPRINT.md section 14, "The server").
func DoctorPreflight(in PreflightInput) []DoctorCheck {
	out := []DoctorCheck{preflightVersionRow(in), preflightSchemaRow(in)}
	for _, later := range preflightLaterChecks {
		out = append(out, DoctorCheck{Area: AreaPreflight, Check: later.check, Subject: "-", Status: DoctorWarn,
			Evidence: "not measured by this build: " + later.card, Remedy: "-"})
	}
	return out
}

// preflightVersionRow is `<candidate> version`: OK naming the answer, FAIL when the
// candidate did not answer.
func preflightVersionRow(in PreflightInput) DoctorCheck {
	if in.VersionErr != "" {
		return DoctorCheck{Area: AreaPreflight, Check: CheckPreflightVersion, Subject: cmp.Or(in.Binary, "-"),
			Status: DoctorFail, Evidence: "the candidate did not answer version: " + in.VersionErr,
			Remedy: "build the candidate and run: " + cmp.Or(in.Binary, "<binary>") + " version"}
	}
	return okCheck(AreaPreflight, CheckPreflightVersion, in.Version, "the candidate answered version "+in.Version)
}

// preflightSchemaRow is the candidate's shadow tick against the store: OK when it
// planned the tick; a locked-shape refusal one FAIL whose evidence is the shadow's
// error text whole and whose remedy is the commands it names and the adopt's schema
// step; any other shadow failure one FAIL with its text.
func preflightSchemaRow(in PreflightInput) DoctorCheck {
	subject := cmp.Or(in.Version, "-")
	if in.ShadowCode == 0 {
		return okCheck(AreaPreflight, CheckPreflightSchema, subject, "the shadow tick passed: the store's tables hold every column TABLES.lock names")
	}
	c := DoctorCheck{Area: AreaPreflight, Check: CheckPreflightSchema, Subject: subject, Status: DoctorFail,
		Evidence: cmp.Or(in.ShadowText, "the shadow tick failed with no text"), Remedy: "-"}
	if strings.Contains(in.ShadowText, preflightLockedShapeMark) {
		c.Remedy = "run the commands above on a STOPPED machine, or the adopt's schema step"
	}
	return c
}

// preflightLaterChecks is the release gate's later checks, one row each saying this
// build does not measure it, until the card that measures it fills the check in:
// members (their tools, keys, holds and version), mirrors, links, stable. One table,
// so --preflight's output always says what it did not check.
var preflightLaterChecks = []struct{ check, card string }{
	{"members", "doctor-preflight-checks-member-prerequisites"},
	{"mirrors", "doctor-preflight-checks-bench-mirrors"},
	{"links", "doctor-names-a-member-whose-link-flapped"},
	{"stable", "doctor-stable-checks-the-stable-pair"},
}
