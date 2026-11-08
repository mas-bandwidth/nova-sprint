package verbs

import (
	"regexp"
	"strings"
)

// The retired mechanics (CARD-CONTRACT "Retired mechanics"): what v1's
// briefs carried that v2 does for itself. A brief naming one is refused at
// add, naming the line, because a worker that follows it does the wrong
// thing (writes a JOB.md nobody reads, waits for a lock nobody holds, ssh's
// to a bench the deal never picked). JOB.md itself is not retired: the
// worker writes it in the job directory for its harness to read
// (edge/worker/jobmd.go, defect 68), so a brief that sends the harness to
// read it is right (D5, 2026-11-07); what is refused is a brief that has the
// harness write it.
const retiredFix = "remove it: the deal names the row, the take receipt is the job, a gate names the facts it needs (an OS, a tool) and never a host, and nothing is locked by hand; then nova-sprint add again"

const jobMDFix = "the worker writes JOB.md for its harness, so a brief reads it and never writes it; " + retiredFix

var (
	retiredLock = regexp.MustCompile(`(?i)\b(winlock|flock|lockfile|captain lock|bench lock)\b`)
	retiredSSH  = regexp.MustCompile(`(?i)\bssh\s+(to|into|on)\s+[A-Za-z0-9][A-Za-z0-9.-]*`)
	gateSSH     = regexp.MustCompile(`(?i)(^|[\s;&|(])ssh\s+(-\S+\s+)*[A-Za-z0-9_.@-]+`)
	// writesJobMD is a clause that has the harness write JOB.md: a writing
	// verb before it, or JOB.md said to be written.
	writesJobMD = regexp.MustCompile(`(?i)\b(writes?|creates?|updates?|edits?|fills?( in)?|appends?( to)?|produces?|generates?|commits?)\b[^.;:]*\bJOB\.md\b|\bJOB\.md\b[^.;:]*\b(is|be|gets?)\s+(written|created|updated|edited|filled( in)?|produced|generated|committed)\b`)
)

// retiredMechanic is the first line of a brief that names a retired
// mechanic: its number, what it names, and the fix; what is "" for none.
// A gate (DONE-WHEN, TEST) that runs ssh is refused as well as prose that
// sends the worker to a host by name.
func retiredMechanic(lines []string) (int, string, string) {
	for i, line := range lines {
		key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		gate := key == "DONE-WHEN" || key == "TEST"
		switch {
		case writesJobMD.MatchString(line):
			return i + 1, "a JOB.md to write", jobMDFix
		case strings.HasPrefix(strings.TrimSpace(line), "WHO:"):
			return i + 1, "a WHO line", retiredFix
		case retiredLock.MatchString(line):
			return i + 1, "a lock (" + retiredLock.FindString(line) + ")", retiredFix
		case retiredSSH.MatchString(line):
			return i + 1, "a host to ssh to (" + retiredSSH.FindString(line) + ")", retiredFix
		case gate && gateSSH.MatchString(value):
			return i + 1, "a gate that runs on a named host (" + strings.TrimSpace(gateSSH.FindString(value)) + ")", retiredFix
		}
	}
	return 0, "", ""
}
