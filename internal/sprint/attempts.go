package sprint

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The attempt record (docs/SPEC-SPRINT.md, the bounds): one line per attempt saying
// whether the worker ran and reported, and, when it did not, the fault class the
// attempt ended on. An attempt that ended without a worker report (an idle reap, a
// harness fault, a lane refused, a push refused, a friend lane that never started)
// says nothing about the brief: the lane-broken bound (AtLaneBroken, bounds.go) reads the
// record to tell a broken lane from a wrong brief. Since 2026-10-12 every attempt counts
// toward the attempt cap whatever ended it (brief_bound.go, AttemptsSince): the owner's
// bound is on the machine's retries between two coordinator decisions.

// FieldRanAttempts is the primary's record of whether each attempt's worker ran and
// reported, one line per attempt ("attempt <n>: yes" or "attempt <n>: no: <fault
// class>"), written as each attempt ends. An attempt that ended without a worker
// report is recorded no with its fault class, which the lane-broken bound reads. Absent,
// every attempt is counted as having run.
const FieldRanAttempts = "ran_attempts"

// attemptRun is one attempt's record: whether its worker ran and reported, and, when
// it did not, the fault class the attempt ended on.
type attemptRun struct {
	ran   bool
	fault string
}

// ranAttempts parses the primary's attempt records (FieldRanAttempts): "attempt <n>:
// yes" or "attempt <n>: no: <fault class>". Attempts with no record are not in the map.
func ranAttempts(c *Card) map[int]attemptRun {
	out := map[int]attemptRun{}
	for _, l := range strings.Split(c.F(FieldRanAttempts), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		rest, after, ok := strings.Cut(l, ":")
		if !ok || !strings.HasPrefix(rest, "attempt ") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(rest, "attempt ")))
		if err != nil {
			continue
		}
		status := strings.TrimSpace(after)
		if strings.HasPrefix(strings.ToLower(status), "no") {
			fault := ""
			if i := strings.Index(status, ": "); i >= 0 {
				fault = strings.TrimSpace(status[i+2:])
			}
			out[n] = attemptRun{ran: false, fault: fault}
		} else {
			out[n] = attemptRun{ran: true}
		}
	}
	return out
}

// recordRan is FieldRanAttempts with attempt n's record set: "attempt <n>: yes" when
// the worker ran and reported, "attempt <n>: no: <fault class>" when it ended without
// a worker report. An existing record for the attempt is replaced; other attempts'
// records are kept (docs/SPEC-SPRINT.md, the bounds). prev is the field as the primary
// holds it, "" when it is new.
func recordRan(prev string, n int, ran bool, fault string) string {
	out := map[int]string{}
	for _, l := range strings.Split(prev, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		rest, after, ok := strings.Cut(l, ":")
		if !ok || !strings.HasPrefix(rest, "attempt ") {
			continue
		}
		old, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(rest, "attempt ")))
		if err != nil {
			continue
		}
		if old != n {
			out[old] = strings.TrimSpace(after)
		}
	}
	status := "yes"
	if !ran {
		status = "no"
		if fault != "" {
			status += ": " + fault
		}
	}
	out[n] = status
	sorted := make([]int, 0, len(out))
	for k := range out {
		sorted = append(sorted, k)
	}
	sort.Ints(sorted)
	var lines []string
	for _, k := range sorted {
		lines = append(lines, fmt.Sprintf("attempt %d: %s", k, out[k]))
	}
	return strings.Join(lines, "\n")
}
