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
// (modules/edge/worker/jobmd.go, defect 68), so a brief that sends the harness to
// read it is right (D5, 2026-11-07); what is refused is a brief that has the
// harness write it.
const retiredFix = "remove it: the deal names the row, the take receipt is the job, a gate names the facts it needs (an OS, a tool) and never a host, and nothing is locked by hand; then nova-sprint add again"

const jobMDFix = "the worker writes JOB.md for its harness, so a brief reads it and never writes it; " + retiredFix

var (
	retiredLock = regexp.MustCompile(`(?i)\b(winlock|flock|lockfile|captain lock|bench lock)\b`)
	retiredSSH  = regexp.MustCompile(`(?i)\bssh\s+(to|into|on)\s+[A-Za-z0-9][A-Za-z0-9.-]*`)
	gateSSH     = regexp.MustCompile(`(?i)(^|[\s;&|(])ssh\s+(-\S+\s+)*[A-Za-z0-9_.@-]+`)
	// jobMDClause splits a line into clauses: at , ; : ! ? and a full
	// stop that ends a sentence (never the dot in JOB.md).
	jobMDClause = regexp.MustCompile(`[,;:!?]|\.(\s|$)`)
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
		case writesJobMD(line):
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

// The words writesJobMD reads: a verb that writes a file, its passive
// participle, the prepositions that tie a verb to the file it writes, the
// words that end the verb's reach, and the words that make the clause not
// an instruction to the harness to write (a negation, or the worker as the
// writer).
var (
	jobMDWriteVerbs = wordSet("write writes writing overwrite overwrites overwriting create creates creating update updates updating edit edits editing fill fills filling append appends appending produce produces producing generate generates generating commit commits committing save saves saving record records recording log logs logging put puts putting add adds adding emit emits emitting")
	jobMDWritten    = wordSet("written overwritten created updated edited filled appended produced generated committed saved recorded logged put added emitted")
	jobMDPassive    = wordSet("is are be been being gets get must should will shall can")
	jobMDTo         = wordSet("to into in")
	jobMDDeterminer = wordSet("the a an your its this that")
	jobMDBreak      = wordSet("then as when and or after before per until once if while unless so read follow see")
	jobMDNot        = wordSet("not never don't do-not no doesn't won't nor")
)

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// writesJobMD reports whether a line has the harness write JOB.md, the v1
// mechanic: a writing verb tied to JOB.md (directly, past a determiner, or
// through an object of at most four words and to/into/in), or JOB.md said
// to be written. A clause that negates the write or names the worker as the
// writer is not one; nor is a clause that only reads or names JOB.md.
func writesJobMD(line string) bool {
	for _, clause := range jobMDClause.Split(line, -1) {
		var words []string
		for _, w := range strings.Fields(strings.ToLower(clause)) {
			words = append(words, strings.Trim(w, "`'\"()[]*"))
		}
		if jobMDExempt(words) {
			continue
		}
		for k, w := range words {
			if w != "job.md" && !strings.HasSuffix(w, "/job.md") {
				continue
			}
			if jobMDVerbBefore(words[:k]) || jobMDWrittenAfter(words[k+1:]) {
				return true
			}
		}
	}
	return false
}

// jobMDExempt is a clause that negates the write or has the worker write.
func jobMDExempt(words []string) bool {
	for _, w := range words {
		if jobMDNot[w] || strings.HasPrefix(w, "worker") {
			return true
		}
	}
	return false
}

// jobMDVerbBefore: the words before JOB.md end in a writing verb tied to it.
func jobMDVerbBefore(before []string) bool {
	i := len(before) - 1
	for i >= 0 && jobMDDeterminer[before[i]] {
		i--
	}
	if i >= 0 && jobMDWriteVerbs[before[i]] {
		return true
	}
	if i < 0 || !jobMDTo[before[i]] {
		return false
	}
	if i > 0 && before[i] == "in" && jobMDWriteVerbs[before[i-1]] {
		return true // fill in JOB.md
	}
	for j, gap := i-1, 0; j >= 0 && gap <= 4; j, gap = j-1, gap+1 {
		switch {
		case jobMDWriteVerbs[before[j]]:
			return true
		case jobMDBreak[before[j]] || jobMDWritten[before[j]] || strings.HasSuffix(before[j], "ed"):
			return false
		}
	}
	return false
}

// jobMDWrittenAfter: the words after JOB.md say it is to be written.
func jobMDWrittenAfter(after []string) bool {
	i := 0
	for i < len(after) && i < 3 && jobMDPassive[after[i]] {
		i++
	}
	return i > 0 && i < len(after) && jobMDWritten[after[i]]
}
