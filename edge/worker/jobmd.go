package worker

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// JobMDName is the file the worker writes in a card's job directory at
// stage (defect 68): what the card is, where its checkout is, and exactly
// how it finishes. The prompt points the harness at it and carries the same
// finish rules (finishRules_internal), so the two never disagree. It is the
// worker's file for the harness: a brief may send the harness to read it,
// and a brief that asks the harness to write one is refused at add as a
// retired v1 mechanic (verbs/brief_lint.go).
const JobMDName = "JOB.md"

// writeJobMD_internal writes j's JOB.md in its job directory.
func writeJobMD_internal(j Job) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nThis file is the worker's: it says what this card is and exactly how it finishes.\n\n", j.Consumer)
	fmt.Fprintf(&b, "card: %s (%s, tier %s) for %s in stream %s\n", j.Consumer, j.Kind, cmp.Or(j.Tier, "-"), cmp.Or(j.Producer, "-"), cmp.Or(j.Stream, "-"))
	fmt.Fprintf(&b, "repository: %s\nbase: %s at %s\n", j.Repo, cmp.Or(j.Base, "-"), cmp.Or(j.BaseSHA, "-"))
	if j.Kind == "read" {
		fmt.Fprintf(&b, "checkout: %s (detached at %s, the head under review)\n", j.WorktreeDir, j.Head)
	} else {
		fmt.Fprintf(&b, "checkout: %s (on branch %s, at %s)\n", j.WorktreeDir, j.Branch, cmp.Or(j.PriorHead, j.BaseSHA))
	}
	for _, kv := range [][2]string{{"prior head (continue from it)", j.PriorHead}, {"prior finding", j.Finding}, {"paths", strings.Join(j.Paths, ", ")}} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", kv[0], kv[1])
		}
	}
	for _, d := range j.DoneWhen {
		fmt.Fprintf(&b, "done-when: %s\n", d)
	}
	if body := strings.TrimSpace(cmp.Or(j.Prose, j.Brief)); body != "" {
		fmt.Fprintf(&b, "\n## The card\n\n%s\n", body)
	}
	fmt.Fprintf(&b, "\n## How this card finishes\n\n%s", finishRules_internal(j))
	if err := os.WriteFile(filepath.Join(j.JobDir, JobMDName), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s for %s: %w", JobMDName, j.Consumer, err)
	}
	return nil
}

// finishRules_internal is how j finishes on a bench, word for word the
// same in JOB.md and the prompt: what to leave in the checkout, the
// identity its commits carry, the forms of result.json, and what the worker
// does after (it pushes; the harness runs no git push and no gh, and the
// landing opens the pull request).
func finishRules_internal(j Job) string { return finishRules(j, nil) }

// KinFinish is a kin row's report verbs for one card: its <card>@<gen>,
// the row and the epoch.
type KinFinish struct {
	Ref, Row string
	Epoch    int
}

// KinFinishRules is how a card a kin row took finishes (take's text): the
// rules JOB.md carries, with the row pushing its own branch and reporting
// by finish, read or report in place of result.json.
func KinFinishRules(j Job, k KinFinish) string { return finishRules(j, &k) }

// finishRules is the one text of how j finishes: a bench's (k nil: the
// harness writes result.json, the worker pushes) or a kin row's (k: the row
// pushes and runs the verb).
func finishRules(j Job, k *KinFinish) string {
	res := filepath.Join(j.JobDir, "result.json")
	if k == nil {
		if j.Kind == "read" {
			return "1. Read the head checked out in " + j.WorktreeDir + " against the card. Edit no tracked file, commit nothing, run no git push and no gh.\n" +
				"2. Write " + res + ", then exit 0. Its forms:\n" +
				"   {\"result\":\"ok\"}: the head meets the card as written.\n" +
				"   {\"result\":\"fix\",\"finding\":\"<path>:<line>: <the defect and its repair>\"}: it does not; name every defect.\n" +
				"   {\"result\":\"fault\",\"reason\":\"<why>\"}: you could not judge it (a tool, a build, a missing input).\n" +
				routeFault_internal() +
				"A run that exits without " + res + " is reported a fault.\n"
		}
		return "1. Commit your work in " + j.WorktreeDir + " on branch " + j.Branch + " (git add, then git commit -F <message file>). Run no git push and no gh: the worker pushes the branch after you exit, and the landing opens the pull request.\n" +
			commitIdentity_internal(j) +
			"2. Run every done-when command in the checkout; each must exit 0.\n" +
			"3. Write " + res + ", then exit 0. Its forms:\n" +
			"   {\"result\":\"ok\"}: the work is committed and every done-when command is green.\n" +
			"   {\"result\":\"fail\",\"reason\":\"<what is not done, and why>\"}: you could not finish it; say what you found.\n" +
			"A run that exits without " + res + " is reported \"fail: no verdict\".\n"
	}
	tail := fmt.Sprintf(" --as %s --epoch %d", k.Row, k.Epoch)
	fault := "   nova-sprint report " + k.Ref + " --result fault --reason \"<why>\"" + tail + ": a tool, the harness or a route failed, not the work; a reason starting " + card.MissingRoute + " <route> leaves the card waiting, with that reason, for the coordinator.\n"
	if j.Kind == "read" {
		return "1. Check out " + j.Head + " detached in a fresh directory of your own and read it against the card. Edit no tracked file, commit nothing, push nothing.\n" +
			"2. Run every done-when command at that head; keep each command, exit code and log.\n" +
			"3. Answer with one of:\n" +
			"   nova-sprint read " + k.Ref + " yes" + tail + ": the head meets the card as written.\n" +
			"   nova-sprint read " + k.Ref + " fix --finding \"<path>:<line>: <the defect and its repair>\"" + tail + ": it does not; name every defect.\n" +
			fault
	}
	return "1. In a fresh clone of your own, commit your work on branch " + j.Branch + " (from the prior head when there is one, else the base), then git push origin " + j.Branch + ". Open no pull request: the landing opens it.\n" +
		kinIdentity(j) +
		"2. Run every done-when command at the head you pushed; each must exit 0.\n" +
		"3. Report with one of:\n" +
		"   nova-sprint finish " + k.Ref + " --head <sha>" + tail + ": the work is pushed at <sha> and every done-when command is green.\n" +
		"   nova-sprint finish " + k.Ref + " --failed --reason \"<what is not done, and why>\"" + tail + ": you could not finish it; say what you found.\n" +
		fault
}

// commitIdentity_internal is the sprint's commit identity rule for a work
// job (CommitRules), which the worker has already configured in the
// checkout; "" for a job with none (staging refuses it).
func commitIdentity_internal(j Job) string {
	if j.CommitName == "" || j.CommitEmail == "" {
		return ""
	}
	return "   This checkout is already configured; do not change it. " + strings.ReplaceAll(strings.TrimRight(CommitRules(j.CommitName, j.CommitEmail), "\n"), "\n", "\n   ") + "\n"
}

// kinIdentity is the commit identity rule for a kin's own clone, which
// nobody has configured for it.
func kinIdentity(j Job) string {
	if j.CommitName == "" || j.CommitEmail == "" {
		return ""
	}
	return "   " + strings.ReplaceAll(strings.TrimRight(CommitRules(j.CommitName, j.CommitEmail), "\n"), "\n", "\n   ") + "\n"
}

// routeFault_internal is the finish rule for a route or device this bench
// lacks: a fault whose reason starts card.MissingRoute leaves the card waiting,
// with that reason, for the coordinator instead of dealing it again (defect 48).
func routeFault_internal() string {
	return fmt.Sprintf("   {\"result\":\"fault\",\"reason\":\"%s <route> <what is missing>\"}: the work needs a route or device this bench lacks (a tool, a device, a network); <route> is one word, the name a brief declares with %s: <route>. The card waits for the coordinator instead of being dealt again.\n", card.MissingRoute, card.RouteField)
}
