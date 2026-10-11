package sprint

import (
	"fmt"
	"strings"
)

// An environment exit is a child that ended 134 or 137 (the opencode harness aborting under
// the landlock sandbox: `Error: Unknown: ChildProcess.spawn (...)`) or that left no result
// at all, whatever its line: it did not judge the card, it is an event of the member's
// environment, and it must not consume the card's attempts or spend its reader slot. The
// finish records it on the attempt as `ended=environment exit=<n>` and keeps the last
// HarnessTailLines of the harness output on the card, so the cause is on the card a reader
// reads (docs/SPEC-SPRINT.md, "An environment exit"; a-card-carries-its-consumer-history).

// The doctor's thresholds over the attempt records, keyed by epoch (docs/SPEC-SPRINT.md,
// "An environment exit"): a member whose environment exits are over 10% of its children is
// a FAIL, 3% to 10% a WARN, below an OK; a rate over fewer than EnvExitMinN children is
// never read (a small sample says nothing).
const (
	EnvExitMinN    = 10
	EnvExitFailPct = 10
	EnvExitWarnPct = 3
)

// HarnessTailLines is how many lines of the harness output the finish keeps on the card.
const HarnessTailLines = 40

// EnvironmentExit is the attempt's word for an environment exit: `environment exit=<n>`.
func EnvironmentExit(exit int) string { return "environment exit=" + itoa(exit) }

// EnvExit classifies a finish's child exit as an environment exit: the ended word
// (EnvironmentExit) and true when the exit is 134 or 137, or when the report is a child
// that left no result (IsNoResult) and the exit is non-zero; "", false otherwise. A
// verdict is the child's, a clean end is the card's: neither is the environment's.
func EnvExit(report string, exit int) (ended string, env bool) {
	switch {
	case exit == 134, exit == 137, exit != 0 && IsNoResult(report):
		return EnvironmentExit(exit), true
	}
	return "", false
}

// EnvExitStatus is the doctor's verdict for a member whose attempt records hold env
// environment exits out of n children: FAIL over EnvExitFailPct%, WARN from EnvExitWarnPct%
// to EnvExitFailPct%, OK below or when n is under EnvExitMinN (the counts are integers, so
// the rate is compared without rounding: env*100 against n*pct).
func EnvExitStatus(env, n int) string {
	if n < EnvExitMinN {
		return DoctorOK
	}
	switch {
	case env*100 > n*EnvExitFailPct:
		return DoctorFail
	case env*100 >= n*EnvExitWarnPct:
		return DoctorWarn
	}
	return DoctorOK
}

// EnvExitCheck is the member-env check of a member whose attempt records hold env
// environment exits out of n children in the window (docs/SPEC-SPRINT.md, "An environment
// exit"): `child exits: <k> of <n> children in the last hour ended 134/137 with no result
// (<rate>%), last: <first line of the harness tail>`, remedy `nova-sprint card <id>`; the
// status is EnvExitStatus. A rate that is OK is no check (false), so the doctor names only
// the members whose environment is eating their children. last is the first line of the
// harness tail on the last card, id the card to read.
func EnvExitCheck(member string, env, n int, last, id string) (DoctorCheck, bool) {
	if status := EnvExitStatus(env, n); status != DoctorOK {
		return DoctorCheck{Area: AreaFleet, Check: CheckMemberEnv, Subject: member, Status: status,
			Evidence: fmt.Sprintf("child exits: %d of %d children in the last hour ended 134/137 with no result (%d%%), last: %s", env, n, env*100/n, last),
			Remedy:   "nova-sprint card " + id}, true
	}
	return DoctorCheck{}, false
}

// EnvExitRecord is the finish's whole record of an environment exit on the attempt: the
// ended word (`environment exit=<n>`) and the harness tail the finish keeps on the card
// (HarnessTailLines), so the cause rides the card a reader reads (docs/SPEC-SPRINT.md, "An
// environment exit"; a-card-carries-its-consumer-history). The finish writes these on the
// withdrawn work card WITHOUT ending the take, so the attempt and the reader slot stand.
type EnvExitRecord struct {
	Ended   string
	Harness string
}

// EnvironmentExitRecord is the record of an environment exit at exit with the child's whole
// harness output: the ended word EnvironmentExit(exit) and the last HarnessTailLines of the
// output. It is the one place the finish and its tests build the pair, so the tail length is
// never named twice.
func EnvironmentExitRecord(exit int, harnessOut string) EnvExitRecord {
	return EnvExitRecord{Ended: EnvironmentExit(exit), Harness: HarnessTail(harnessOut, HarnessTailLines)}
}

// HarnessTail is the last n lines of a child's harness output, so the finish can keep the
// cause on the card (HarnessTailLines). Fewer than n lines keeps all; an empty output is
// empty; a trailing newline is preserved.
func HarnessTail(out string, n int) string {
	if out == "" || n <= 0 {
		return ""
	}
	lines := strings.Split(out, "\n")
	trailing := lines[len(lines)-1] == ""
	if trailing {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	s := strings.Join(lines, "\n")
	if trailing {
		s += "\n"
	}
	return s
}
