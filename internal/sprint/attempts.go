package sprint

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
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

// EnvExitCounts is one member's count of its environment exits and its children in the
// window, as the doctor names them (EnvExitCheck) and `nova-sprint stats --children` prints
// them: how many of the member's children ended on its environment (Env) out of all its
// children that ended in the window (N), the first line of the last environment exit's
// harness tail (Last) and its primary to read (Card).
type EnvExitCounts struct {
	Member string
	Env    int
	N      int
	Last   string
	Card   string
	// Exits is the histogram of the environment exits by child exit code
	// (`environment exit=<n>`), so `nova-sprint stats --children` prints the
	// per-member histogram of child exit codes for the window.
	Exits map[string]int
}

// EnvExitChildren counts each member's environment exits over the attempt records on the
// work cards of the snapshot (the current epoch's: the sprint's whole state is keyed by
// epoch). A work card whose FieldEnvExit is set ended on the member's environment
// (`ended=environment exit=<n>`, written by the finish's envExited); N counts every work
// card that ended in the window (its finished or withdrawn stamp), so the doctor's rate is
// env/N and its evidence is the last exit's harness tail and primary
// (docs/SPEC-SPRINT.md, "An environment exit").
func EnvExitChildren(s *Snapshot, now time.Time, window time.Duration) []EnvExitCounts {
	if s == nil || s.Fleet == nil {
		return nil
	}
	since := now.Add(-window)
	within := func(v string) bool {
		t, err := time.Parse(time.RFC3339, v)
		return err == nil && !t.Before(since)
	}
	by := map[string]*EnvExitCounts{}
	lastAt := map[string]time.Time{}
	for _, wc := range s.Fleet.Cards() {
		if wc.F("kind") != "work" || IsFriendRow(wc.Row) {
			continue
		}
		member := wc.Row
		c := by[member]
		if c == nil {
			c = &EnvExitCounts{Member: member}
			by[member] = c
		}
		endAt := cmp.Or(wc.F("finished"), wc.F("withdrawn"))
		if !within(endAt) {
			continue
		}
		c.N++
		if !strings.HasPrefix(wc.F(FieldEnvExit), "environment exit=") {
			continue
		}
		c.Env++
		if c.Exits == nil {
			c.Exits = map[string]int{}
		}
		c.Exits[strings.TrimPrefix(wc.F(FieldEnvExit), "environment exit=")]++
		at, _ := time.Parse(time.RFC3339, endAt)
		if at.After(lastAt[member]) {
			lastAt[member] = at
			c.Last = strings.SplitN(strings.TrimSuffix(wc.F(FieldHarnessTail), "\n"), "\n", 2)[0]
			c.Card = wc.F("primary")
		}
	}
	out := make([]EnvExitCounts, 0, len(by))
	for _, m := range slices.Sorted(maps.Keys(by)) {
		if by[m].N == 0 {
			continue // no children ended in the window: nothing to say
		}
		out = append(out, *by[m])
	}
	return out
}
