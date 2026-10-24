package sprint

import (
	"strings"
)

// A plain finish (no --failed) records the work ok and sends its primary to review. A
// report whose own verdict line says the work is not done (HOLD, not-done) is no ok
// finish: recorded ok, a reader finds it broken and the attempt is wasted (a cross-row
// review of 2026-10-24 found 17 of one row's 53 failed attempts were exactly this). So a
// plain finish whose report carries such a verdict line is refused, naming the line and
// the fix, a finish with --failed; a LAND, done or ok verdict, and a report with no
// verdict line, finish as before (docs/SPEC-SPRINT.md section 2, "A plain finish with a
// HOLD verdict"; tla/FinishVerdict.tla, NoHoldFinishedOk).

// NotDoneVerdictLine is the report's first verdict line when that verdict says the work is
// not done (HOLD, not-done, not done, notdone), trimmed, and "" when the first verdict line
// says anything else or the report has none. A verdict line is a line (markdown bullets,
// headings and emphasis trimmed, and a member's `pushed=<sha> to <branch>: ` prefix
// dropped) that begins with the word verdict followed by a colon or a space (`Verdict:
// HOLD`, `verdict not-done`), or a line that is the verdict alone (`NOT DONE`, `HOLD`).
// Case is ignored.
func NotDoneVerdictLine(report string) string {
	for _, raw := range strings.Split(report, "\n") {
		line := strings.TrimSpace(raw)
		body := verdictBody(line)
		if strings.HasPrefix(body, "pushed=") {
			if _, rest, ok := strings.Cut(body, ": "); ok {
				body = verdictBody(rest)
			}
		}
		word, rest, isVerdict := verdictWords(body)
		if !isVerdict {
			continue
		}
		if notDoneWord(word, rest) {
			return line
		}
		return ""
	}
	return ""
}

// verdictBody is a line with its markdown lead (bullets, headings, quotes, emphasis) and
// surrounding space trimmed.
func verdictBody(line string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#*-_>` \t"))
}

// verdictWords reads a verdict line: the verdict's first word and the words after it,
// lower case, and whether body is a verdict line at all.
func verdictWords(body string) (word string, rest []string, ok bool) {
	lower := strings.ToLower(body)
	if after, found := strings.CutPrefix(lower, "verdict"); found {
		if after == "" || (after[0] != ':' && after[0] != ' ' && after[0] != '\t' && after[0] != '*' && after[0] != '_') {
			return "", nil, false // "verdicts", "verdicted": prose, not a verdict line
		}
		f := strings.Fields(strings.TrimLeft(after, "*_: \t"))
		if len(f) == 0 {
			return "", nil, false
		}
		return trimVerdictWord(f[0]), trimAll(f[1:]), true
	}
	// a line that is the verdict alone: NOT DONE, not-done, HOLD
	f := strings.Fields(lower)
	if len(f) == 0 || len(f) > 2 {
		return "", nil, false
	}
	word, rest = trimVerdictWord(f[0]), trimAll(f[1:])
	alone := (word != "not" && len(rest) == 0) || (word == "not" && len(rest) == 1)
	if !alone || !notDoneWord(word, rest) {
		return "", nil, false // "hold on", "not done yet": prose
	}
	return word, rest, true
}

// notDoneWord says a verdict (its first word and the rest) is HOLD or not done.
func notDoneWord(word string, rest []string) bool {
	switch word {
	case "hold", "not-done", "not_done", "notdone":
		return true
	case "not":
		return len(rest) > 0 && rest[0] == "done"
	}
	return false
}

func trimVerdictWord(w string) string { return strings.Trim(w, "*_`.,;:!()[]\"'") }

func trimAll(ws []string) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, trimVerdictWord(w))
	}
	return out
}

// notDoneFinishWhy is the refusal of a plain finish of card id whose report's verdict line
// (NotDoneVerdictLine) says the work is not done; "" when the finish is --failed or the
// report says nothing of the kind.
func notDoneFinishWhy(id string, r FinishReq) string {
	if r.Failed {
		return ""
	}
	line := NotDoneVerdictLine(r.Report)
	if line == "" {
		return ""
	}
	return "the report's verdict line " + strings.Join(strings.Fields(cutText(line, 200)), " ") +
		" says the work is not done, and a plain finish records it ok: finish " + id + " with --failed and the same --report"
}
