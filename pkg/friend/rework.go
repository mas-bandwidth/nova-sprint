package friend

import (
	"cmp"
	"fmt"
	"strings"
)

// A reworked card's brief opens with its fix (the night of 2026-10-05: lint-pkg-tlc-tbb came
// back five times, sec-rocketnet-server-dos-zhi four and presence-from-session-only five, each
// with the same finding: the server puts a rework's fix on a 'The coordinator asks:' line
// under the start, over a long card whose own STOP is the whole card's, and the next lane
// read the card and never reached the fix). The daemon writes such a brief (ReworkedBrief)
// with THE ONE THING LEFT, the fix, as the first line after STATUS and the reader's finding
// as the second, then the work it carries (the head of the attempt before, onto this
// attempt's branch) and how its report is checked, before RULES and the task; the card's
// STOP becomes the fix alone. A LAND whose report does not carry the fix's key words
// (FixAddressed) is finished as a HOLD by the outbox pass, so the same finding is never read
// twice (docs/SPEC-FRIEND.md, a reworked brief opens with the fix).

// The labels of a reworked brief's first lines, and of the server's lines it is made from.
const (
	OneThingLeft     = "THE ONE THING LEFT: "
	ReaderFoundLabel = "The reader found: "
	carriedLabel     = "The carried work: "
	checkedLabel     = "How it is checked: "
	asksLabel        = "The coordinator asks: "
	foundLabel       = "A reader found: "
	whyLabel         = "This attempt exists because: "
	startLabel       = "This attempt starts from "
	workInLabel      = "Work in "
)

// preludeLabels start the lines of a brief's prelude (STATUS to its first blank line); a
// prelude line that starts with none continues the one before it.
var preludeLabels = []string{OneThingLeft, ReaderFoundLabel, carriedLabel, checkedLabel, asksLabel, foundLabel, whyLabel, startLabel, workInLabel}

// preludeLine is one line of a brief's prelude, its continuation lines joined to it.
type preludeLine struct{ label, text string }

// splitBrief is a brief's STATUS line, its prelude (the lines after STATUS up to the first
// blank line) and the rest after that blank line (the card as the server hands it).
func splitBrief(brief string) (status string, prelude []preludeLine, body string, ok bool) {
	status, rest, _ := strings.Cut(brief, "\n")
	if !strings.HasPrefix(status, "STATUS: nova-sprint card ") {
		return "", nil, "", false
	}
	head, body, _ := strings.Cut(rest, "\n\n")
	for _, l := range strings.Split(head, "\n") {
		label := ""
		for _, p := range preludeLabels {
			if strings.HasPrefix(l, p) {
				label = p
				break
			}
		}
		if label == "" && len(prelude) > 0 {
			prelude[len(prelude)-1].text += "\n" + l
			continue
		}
		prelude = append(prelude, preludeLine{label, strings.TrimPrefix(l, label)})
	}
	return status, prelude, body, true
}

// ReworkedBrief is a reworked card's brief with its fix first: STATUS; THE ONE THING LEFT,
// the fix; The reader found, the finding (or why the attempt exists when no reader found
// anything); the carried head and the branch it is carried onto; how the report is checked;
// then the rest of the prelude as the server wrote it, and the card with its STOP the fix
// alone. A brief that asks no fix, and one already reworked, are answered as they are.
func ReworkedBrief(brief string) string {
	status, prelude, body, ok := splitBrief(brief)
	if !ok {
		return brief
	}
	var fix, finding, why, start string
	var kept []preludeLine
	for _, p := range prelude {
		switch p.label {
		case OneThingLeft:
			return brief
		case asksLabel:
			fix = oneLine(p.text, len(p.text))
		case foundLabel:
			finding = oneLine(p.text, len(p.text))
		default:
			switch p.label {
			case whyLabel:
				why = oneLine(p.text, len(p.text))
			case startLabel:
				start = p.text
			}
			kept = append(kept, p)
		}
	}
	if fix == "" {
		return brief
	}
	if finding == "" {
		finding = "no reader's finding; " + cmp.Or(why, "the attempt before was sent back")
	}
	branch := ""
	if m := statusBranchRE.FindStringSubmatch(status); m != nil {
		branch = m[1]
	}
	var b strings.Builder
	b.WriteString(status + "\n")
	b.WriteString(OneThingLeft + fix + "\n")
	b.WriteString(ReaderFoundLabel + finding + "\n")
	b.WriteString(carriedLabel + carriedText(start, branch) + "\n")
	b.WriteString(checkedLabel + checkedText(fix) + "\n")
	for _, p := range kept {
		b.WriteString(p.label + p.text + "\n")
	}
	b.WriteString("\n" + stopIsTheFix(body, fix))
	return b.String()
}

// carriedText says the work this attempt carries: the head of the attempt before (off the
// start line, its 'Carry the work of attempt N onto it yourself: its head, <sha>,'), carried
// onto this attempt's branch, or that nothing is carried.
func carriedText(start, branch string) string {
	onto := cmp.Or(branch, "your branch")
	const carry, its = "Carry the work of attempt ", " onto it yourself: its head, "
	if _, after, ok := strings.Cut(start, carry); ok {
		if n, rest, ok := strings.Cut(after, its); ok {
			if sha, _, ok := strings.Cut(rest, ","); ok && fullSha.MatchString(sha) {
				return fmt.Sprintf("attempt %s's head %s, carried onto %s from the tip of its base; the Head you report is on %s and holds that work and the fix.", n, sha, onto, onto)
			}
		}
	}
	return fmt.Sprintf("nothing carried: no attempt before this one pushed work; start %s from the tip of its base.", onto)
}

// checkedText says what the finish report on the fix says first.
func checkedText(fix string) string {
	return "your finish report says how THE ONE THING LEFT is done, first."
}

// stopIsTheFix is the card with its first STOP line the fix alone (a card with none is given
// one, first).
func stopIsTheFix(body, fix string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "STOP:") {
			lines[i] = "STOP: " + fix
			return strings.Join(lines, "\n")
		}
	}
	return "STOP: " + fix + "\n" + body
}
