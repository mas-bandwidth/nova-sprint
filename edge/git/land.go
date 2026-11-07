package git

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

// LandOutcome is what one landing pass found (LANDING.md). landed is the
// only automatic move; pending, absent and base-moved are asked again by the
// caller; every other outcome is a judgement for the coordinator.
type LandOutcome string

const (
	OutcomeLanded     LandOutcome = "landed"
	OutcomePending    LandOutcome = "ci_pending"       // checks queued or running
	OutcomeAbsent     LandOutcome = "ci_absent"        // no check reported yet: the caller bounds the wait
	OutcomeBaseMoved  LandOutcome = "base_moved"       // base moved after the gate: the pass runs again
	OutcomeRed        LandOutcome = "ci_red"           // a check failed that is not red on the base (new red), or one not compared
	OutcomeInherited  LandOutcome = "ci_red_inherited" // the run ended and every failing check is red on the base too
	OutcomeCIFault    LandOutcome = "ci_fault"         // a check was cancelled, timed out or never started
	OutcomeConflict   LandOutcome = "conflict"         // the heads do not merge onto base
	OutcomeRefused    LandOutcome = "merge_refused"    // GitHub refused the merge
	OutcomeUnopenable LandOutcome = "pr_unopenable"    // the PR could not be opened or read
	OutcomeBranch     LandOutcome = "branch_failed"    // the group branch could not be created or pushed
	OutcomeMissing    LandOutcome = "missing_head"     // a card's branch is not on origin
	OutcomeMoved      LandOutcome = "head_moved"       // a card's branch is no longer at the head the read passed
)

// LandRequest is one landing: the stream's heads (in landing order) and the
// card branches they are the tips of. One head is rung 1, more is rung 2.
type LandRequest struct {
	RepoDir, Repo, Stream, Base string
	Heads, Branches             []string
	Ledgers                     []LedgerRule
}

// LandResult is one pass's outcome with the facts a judgement shows. A red
// one carries the split by the base (directive 59): MergeBase, the
// workflows red on the base too (Inherited) and the ones that are not
// (NewRed: failed jobs, runners, first error lines; a failing check that is
// no Actions workflow is new, since nothing on the base compares with it);
// with a failing agreement check, Unsigned is the PR's commit authors.
type LandResult struct {
	Outcome                                          LandOutcome
	Rung                                             int
	BaseTip, GroupTip, GatedSHA, MergeSHA, PR, CIURL string
	Reason, MergeBase                                string
	LandedHeads, Conflicts, FailingCI, FailingLines  []string
	Inherited, NewRed                                []forge.WorkflowRed
	Unsigned                                         []string
}

// LandPass is one pass of the landing (LANDING.md, "The ladder"). Heads all
// on base already have landed. Otherwise the PR's head is the one card's
// branch when it is a single head that already contains the base tip (rung
// 1), else the stream's group branch, rebuilt from the base tip with every
// head merged in (rung 2; and one head behind base, so that what CI passes
// contains the base it merges onto). The PR is found or opened against
// base, its checks read at the exact gated sha, and only a green one is
// merged, by the repository's allowed method, while base is still the tip
// it was gated on. Every failure is an outcome naming its facts, never a
// retry here; an error is a fault of git or the forge themselves.
func LandPass(ctx context.Context, g Git, fg forge.Forge, req LandRequest) (LandResult, error) {
	if err := checkRequest_internal(req, fg); err != nil {
		return LandResult{}, err
	}
	res := LandResult{Rung: min(len(req.Heads), 2)}
	baseTip, tips, err := fetchHeads_internal(ctx, g, req)
	if err != nil {
		return res, err
	}
	res.BaseTip = baseTip
	for i, b := range req.Branches {
		if tips[i] == "" {
			res.Outcome, res.Reason = OutcomeMissing, fmt.Sprintf("branch %s is not on origin: push it, then answer retry", b)
			return res, nil
		}
	}
	if all, err := allAncestors_internal(ctx, g, req.RepoDir, req.Heads, baseTip); err != nil || all {
		if all {
			res.Outcome, res.MergeSHA, res.LandedHeads = OutcomeLanded, baseTip, slices.Clone(req.Heads)
		}
		return res, err
	}
	prHead, gated, done, err := gatedHead_internal(ctx, g, req, baseTip, tips, &res)
	if err != nil || done {
		return res, err
	}
	res.GatedSHA = gated
	return gateAndMerge_internal(ctx, g, fg, req, prHead, res)
}

func checkRequest_internal(req LandRequest, fg forge.Forge) error {
	if len(req.Heads) == 0 || len(req.Heads) != len(req.Branches) || req.Stream == "" || req.Base == "" || req.Repo == "" {
		return fmt.Errorf("land refused: LandPass needs a repo, a stream, a base and one branch per head")
	}
	if fg == nil {
		return fmt.Errorf("land refused: stream %s has no forge, so nothing lands automatically: land each card by hand with nova-sprint land <card> --reason <why>", req.Stream)
	}
	for _, b := range req.Branches {
		if err := ValidateCardBranch(req.Stream, b); err != nil {
			return err
		}
	}
	return nil
}

// fetchHeads_internal reads base's tip and each card branch's tip ("" when
// the branch is not on origin), and fetches base and the heads.
func fetchHeads_internal(ctx context.Context, g Git, req LandRequest) (string, []string, error) {
	baseRef := "refs/heads/" + req.Base
	baseTip, ok, err := g.LsRemote(ctx, req.RepoDir, baseRef)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("land refused: base branch %s is not on origin", req.Base)
	}
	tips := make([]string, len(req.Branches))
	for i, b := range req.Branches {
		if tips[i], _, err = g.LsRemote(ctx, req.RepoDir, "refs/heads/"+b); err != nil {
			return "", nil, err
		}
	}
	if slices.Contains(tips, "") {
		return baseTip, tips, nil
	}
	return baseTip, tips, g.Fetch(ctx, req.RepoDir, append([]string{baseRef}, req.Heads...)...)
}

// gatedHead_internal is the PR's head branch and the sha CI must pass: the
// card's own branch at its head (rung 1, a head containing the base tip),
// else the group branch, rebuilt and pushed (done with its outcome when the
// merge or the push failed).
func gatedHead_internal(ctx context.Context, g Git, req LandRequest, baseTip string, tips []string, res *LandResult) (string, string, bool, error) {
	if len(req.Heads) == 1 {
		up, err := g.IsAncestor(ctx, req.RepoDir, baseTip, req.Heads[0])
		if err != nil {
			return "", "", true, err
		}
		if up {
			if tips[0] != req.Heads[0] {
				res.Outcome = OutcomeMoved
				res.Reason = fmt.Sprintf("branch %s is at %s, not at the head %s the read passed", req.Branches[0], tips[0], req.Heads[0])
				return "", "", true, nil
			}
			return req.Branches[0], req.Heads[0], false, nil
		}
	}
	res.Rung = 2 // the group branch gates, one head or more
	sha, done, err := buildGroup_internal(ctx, g, req, baseTip, res)
	return GroupBranch(req.Stream), sha, done, err
}

// buildGroup_internal merges the heads onto the base tip and pushes the
// result to the group branch (force, leased on its tip as read). The merge
// is deterministic (MergeGroup dates its commits at the base's), so a pass
// that runs again with the same base and heads pushes nothing new and CI is
// not restarted.
func buildGroup_internal(ctx context.Context, g Git, req LandRequest, baseTip string, res *LandResult) (string, bool, error) {
	merged, err := g.MergeGroup(ctx, req.RepoDir, baseTip, req.Heads, req.Ledgers)
	if err != nil {
		return "", true, err
	}
	if merged.Kind != MergeOK {
		res.Outcome, res.Conflicts = OutcomeConflict, merged.Conflicts
		res.Reason = cmp.Or(merged.Reason, "the heads do not merge onto "+baseTip)
		return "", true, nil
	}
	old, _, err := g.LsRemote(ctx, req.RepoDir, GroupRef(req.Stream))
	if err != nil {
		return "", true, err
	}
	res.GroupTip = merged.SHA
	if old == merged.SHA {
		return merged.SHA, false, nil
	}
	if err := g.Push(ctx, req.RepoDir, merged.SHA, GroupRef(req.Stream), old); err != nil {
		res.Outcome, res.GroupTip = OutcomeBranch, old
		res.Reason = fmt.Sprintf("the group branch %s could not be pushed to %s: %s", GroupBranch(req.Stream), merged.SHA, edge.RedactN(err.Error(), edge.FailureMax))
		return "", true, nil
	}
	return merged.SHA, false, nil
}

// gateAndMerge_internal is the PR against base, its checks at the gated
// sha, and the merge of a green one while base is still the gated tip.
func gateAndMerge_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, prHead string, res LandResult) (LandResult, error) {
	pr, ok, err := fg.PR(ctx, req.Repo, prHead, req.Base)
	if err == nil && !ok {
		pr, err = fg.OpenPR(ctx, req.Repo, prHead, req.Base, fmt.Sprintf("%s: land %d card(s)", req.Stream, len(req.Heads)), "Opened by nova-sprint's landing. "+strings.Join(req.Branches, ", "))
	}
	if err != nil {
		res.Outcome, res.Reason = OutcomeUnopenable, fmt.Sprintf("the PR %s -> %s could not be opened or read: %s", prHead, req.Base, edge.RedactN(err.Error(), edge.FailureMax))
		return res, nil
	}
	res.PR = cmp.Or(pr.URL, fmt.Sprintf("#%d", pr.Number))
	switch {
	case pr.HeadSHA != "" && !strings.EqualFold(pr.HeadSHA, res.GatedSHA):
		res.Outcome, res.Reason = OutcomePending, "the PR's head is "+pr.HeadSHA+", not yet the gated "+res.GatedSHA
		return res, nil
	case pr.Mergeable == "CONFLICTING":
		res.Outcome, res.Reason = OutcomeConflict, "GitHub says "+res.PR+" conflicts with "+req.Base
		return res, nil
	}
	if done := readChecks_internal(ctx, fg, req, pr.Number, &res); done {
		return res, nil
	}
	now, ok, err := g.LsRemote(ctx, req.RepoDir, "refs/heads/"+req.Base)
	if err != nil || !ok {
		return res, fmt.Errorf("land: ls-remote %s before the merge: %w", req.Base, cmp.Or(err, ErrMissingRef))
	}
	if now != res.BaseTip {
		res.Outcome, res.Reason = OutcomeBaseMoved, "base moved from "+res.BaseTip+" to "+now+" after the gate"
		return res, nil
	}
	sha, err := fg.Merge(ctx, req.Repo, pr.Number, res.GatedSHA)
	if err != nil {
		res.Outcome, res.Reason = OutcomeRefused, "GitHub refused the merge of "+res.PR+": "+edge.RedactN(err.Error(), edge.FailureMax)
		return res, nil
	}
	res.Outcome, res.MergeSHA, res.LandedHeads = OutcomeLanded, sha, slices.Clone(req.Heads)
	return res, nil
}

// readChecks_internal reads the gated sha's checks into res: true (with
// res's outcome) unless they are green. A red one is split by the base
// (splitRed_internal); while the run goes on and every red so far is
// inherited, the pass is pending (directive 76).
func readChecks_internal(ctx context.Context, fg forge.Forge, req LandRequest, pr int, res *LandResult) bool {
	rollups, err := fg.Checks(ctx, req.Repo, []string{res.GatedSHA})
	if err != nil {
		res.Outcome, res.Reason = OutcomeCIFault, "the checks of "+res.GatedSHA+" could not be read: "+edge.RedactN(err.Error(), edge.FailureMax)
		return true
	}
	r := rollups[res.GatedSHA]
	res.CIURL, res.FailingCI = r.URL, r.Failing
	res.Reason = cmp.Or(r.Reason, string(r.State))
	switch r.State {
	case forge.CheckGreen:
		return false
	case forge.CheckPending:
		res.Outcome = OutcomePending
	case forge.CheckAbsent, "":
		res.Outcome = OutcomeAbsent
	case forge.CheckFault:
		res.Outcome = OutcomeCIFault
	default:
		res.Outcome = splitRed_internal(ctx, fg, req, pr, r, res)
	}
	return true
}

// splitRed_internal is a red rollup's outcome and facts. New red (or red
// that could not be compared) is ci_red at once, even while the run goes
// on; red that is all inherited waits for the run (pending), then is
// ci_red_inherited, still a judgement: red never lands by itself. A
// failing agreement check names the PR's commit authors.
func splitRed_internal(ctx context.Context, fg forge.Forge, req LandRequest, pr int, r forge.CheckRollup, res *LandResult) LandOutcome {
	if slices.ContainsFunc(r.Failing, forge.IsAgreement) {
		who, err := fg.Authors(ctx, req.Repo, pr)
		if err != nil {
			who = []string{"(the PR's commit authors were not read: " + edge.RedactN(err.Error(), edge.FailureMax) + ")"}
		}
		res.Unsigned = who
	}
	split, err := fg.Inheritance(ctx, req.Repo, req.Base, res.BaseTip, res.GatedSHA)
	if err != nil {
		res.Reason = "not compared with the base (" + edge.RedactN(err.Error(), edge.FailureMax) + "): " + res.Reason
		if r.Waiting {
			return OutcomePending
		}
		res.FailingLines = failedLines_internal(ctx, fg, req.Repo, r.URL)
		return OutcomeRed
	}
	res.MergeBase, res.Inherited = split.MergeBase, split.Inherited
	res.NewRed = append(split.New, unmatched_internal(r, split)...)
	switch {
	case len(res.NewRed) > 0:
		res.Reason = "new red: " + workflows_internal(res.NewRed)
		return OutcomeRed
	case r.Waiting:
		res.Reason = "waiting for the run: the red so far is all inherited (" + workflows_internal(res.Inherited) + ")"
		return OutcomePending
	}
	res.Reason = "only inherited red: " + workflows_internal(res.Inherited)
	return OutcomeInherited
}

// failedLines_internal is the first failing lines of the check at url; a
// log that cannot be read is said in the lines, never dropped.
func failedLines_internal(ctx context.Context, fg forge.Forge, repo, url string) []string {
	lines, err := fg.FailedLines(ctx, repo, url)
	if err != nil {
		return []string{"(the failing log was not read: " + edge.RedactN(err.Error(), edge.FailureMax) + ")"}
	}
	return lines
}

// unmatched_internal is each failing check of the rollup that is no failed
// job of a workflow in split: a status or another app's check, which
// nothing on the base compares with, so new red.
func unmatched_internal(r forge.CheckRollup, split forge.RedSplit) []forge.WorkflowRed {
	var out []forge.WorkflowRed
	for _, name := range r.Failing {
		known := false
		for _, w := range append(slices.Clone(split.Inherited), split.New...) {
			known = known || w.Workflow == name || slices.ContainsFunc(w.Jobs, func(j forge.FailedJob) bool { return j.Name == name })
		}
		if !known {
			job := forge.FailedJob{Name: name}
			if name == r.Failing[0] {
				job.URL = r.URL // the rollup's URL is its first failing check's
			}
			out = append(out, forge.WorkflowRed{Workflow: name, Basis: "a check that is no Actions workflow, not compared with the base", Jobs: []forge.FailedJob{job}})
		}
	}
	return out
}

// workflows_internal is ws's names, each with what the base was read from.
func workflows_internal(ws []forge.WorkflowRed) string {
	var names []string
	for _, w := range ws {
		names = append(names, w.Workflow+" ("+w.Basis+")")
	}
	return strings.Join(names, ", ")
}

func allAncestors_internal(ctx context.Context, g Git, repoDir string, heads []string, target string) (bool, error) {
	for _, h := range heads {
		if ok, err := g.IsAncestor(ctx, repoDir, h, target); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}
