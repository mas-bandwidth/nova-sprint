package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// LandOutcome is what one landing pass found (LANDING.md). landed is the
// only automatic move; pending, unfinished, absent, rerun, base-moved and
// forge-rested are asked again by the caller (unfinished and absent only
// until the request's wait); every other outcome is a judgement for the
// coordinator.
type LandOutcome string

const (
	OutcomeLanded     LandOutcome = "landed"
	OutcomePending    LandOutcome = "ci_pending"    // checks queued or running
	OutcomeUnfinished LandOutcome = "ci_unfinished" // a check failed while others still run: no verdict yet; the caller bounds the wait
	OutcomeNoVerdict  LandOutcome = "ci_no_verdict" // every failing check was stopped (killed, cancelled, timed out), not failed
	OutcomeRerun      LandOutcome = "ci_rerun"      // the stopped jobs were asked to run again: the pass runs again
	OutcomeAbsent     LandOutcome = "ci_absent"     // no check reported yet: the caller bounds the wait
	OutcomeBaseMoved  LandOutcome = "base_moved"    // base moved after the gate: the pass runs again
	OutcomeRested     LandOutcome = "forge_rested"  // GitHub rate-limited the forge's identity: the pass runs again
	OutcomeRed        LandOutcome = "ci_red"        // a check failed
	OutcomeCIFault    LandOutcome = "ci_fault"      // a check faulted with no stop GitHub names (e.g. never started)
	OutcomeConflict   LandOutcome = "conflict"      // the heads do not merge onto base
	OutcomeRefused    LandOutcome = "merge_refused" // GitHub refused the merge
	OutcomeUnopenable LandOutcome = "pr_unopenable" // the PR could not be opened or read
	OutcomeBranch     LandOutcome = "branch_failed" // the group branch could not be created or pushed
	OutcomeMissing    LandOutcome = "missing_head"  // a card's branch is not on origin
	OutcomeMoved      LandOutcome = "head_moved"    // a card's branch is no longer at the head the read passed
	OutcomeBehind     LandOutcome = "base_behind"   // a promotion's base does not contain main's tip
)

// LandRequest is one landing: the stream's heads (in landing order) and the
// card branches they are the tips of. One head is rung 1, more is rung 2.
// Reran says the stopped jobs of this landing were already run again once
// (OutcomeRerun): a second no-verdict is the coordinator's. Gate is the
// gate the last pass of this request kept (LandResult.Kept; zero: none).
type LandRequest struct {
	RepoDir, Repo, Stream, Base string
	Heads, Branches             []string
	Reran                       bool
	Gate                        Gate
}

// Gate is what a landing's CI gates: the PR's head branch, the sha, the
// base tip it was built on and its rung. The run keeps a pass's gate for
// the next pass of the same request, so a gate whose CI has no verdict
// yet is read to its verdict and never rebuilt or pushed again because
// the base moved (LANDING.md, "A group in flight").
type Gate struct {
	Branch, SHA, Base string
	Rung              int
}

// LandResult is one pass's outcome with the facts a judgement shows. A red
// one carries BaseFailing, the base tip's own failing checks when the base
// is red too, read from the base's completed checks only: a red base is the
// coordinator's whole-picture judgement, never classified here (LANDING.md
// "A red base"). Stopped names the failing checks GitHub says were stopped
// (killed, cancelled, timed out), apart from FailingCI, the ones that failed
// on their own; Pending is how many checks were still running. Gate is
// the gate the pass read; ConflictHead the head whose merge into the group
// failed, when the group's merge conflicted.
type LandResult struct {
	Outcome                                                               LandOutcome
	Rung, Pending                                                         int
	BaseTip, GroupTip, GatedSHA, MergeSHA, PR, CIURL, Reason              string
	LandedHeads, Conflicts, FailingCI, FailingLines, BaseFailing, Stopped []string
	ConflictHead                                                          string
	Gate                                                                  Gate
}

// Kept is the gate the next pass of the same request reads: this pass's
// while its CI has no verdict yet (pending, unfinished, absent, stopped
// jobs run again, a rested forge), else none: a verdict ends the request,
// and a green gate whose base moved is built again on the new tip.
func (r LandResult) Kept() Gate {
	switch r.Outcome {
	case OutcomePending, OutcomeUnfinished, OutcomeAbsent, OutcomeRerun, OutcomeRested:
		return r.Gate
	}
	return Gate{}
}

// LandPass is one pass of the landing (LANDING.md, "The ladder"). Heads all
// on base already have landed. Otherwise the PR's head is the one card's
// branch when it is a single head that already contains the base tip (rung
// 1), else the stream's group branch, rebuilt from the base tip with every
// head merged in (rung 2; and one head behind base, so that what CI passes
// contains the base it merges onto). The PR is found or opened against
// base, its checks read at the exact gated sha, and only a green one is
// merged, by the repository's allowed method, while base is still the tip
// it was gated on. A gate the request kept (req.Gate) is read as it is,
// on the base it was built on, while its branch is still at it; GitHub
// saying it conflicts with a base that moved lets it go, and the pass
// builds again on the new tip. Every failure is an outcome naming its
// facts, never a retry here; an error is a fault of git or the forge
// themselves.
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
	if moved_internal(req, tips, &res) {
		return res, nil
	}
	kept, err := keeps_internal(ctx, g, req)
	if err != nil {
		return res, err
	}
	if kept {
		out, err := gateAndMerge_internal(ctx, g, fg, req, req.Gate.Branch, keptResult_internal(req.Gate, res))
		if err != nil || out.Outcome != OutcomeConflict || req.Gate.Base == baseTip {
			return out, err
		}
	}
	prHead, gated, done, err := gatedHead_internal(ctx, g, req, baseTip, &res)
	if err != nil || done {
		return res, err
	}
	res.GatedSHA = gated
	res.Gate = Gate{Branch: prHead, SHA: gated, Base: baseTip, Rung: res.Rung}
	return gateAndMerge_internal(ctx, g, fg, req, prHead, res)
}

// moved_internal is whether a card branch is not at the head the read
// passed, on either rung: a moved branch is the coordinator's judgement,
// never landed at a sha its branch has left (the deployed line's check,
// 2026-11-07).
func moved_internal(req LandRequest, tips []string, res *LandResult) bool {
	for i, tip := range tips {
		if tip != req.Heads[i] {
			res.Outcome = OutcomeMoved
			res.Reason = fmt.Sprintf("branch %s is at %s, not at the head %s the read passed", req.Branches[i], tip, req.Heads[i])
			return true
		}
	}
	return false
}

// keeps_internal is whether the request's kept gate is still the one to
// read: its branch is still at it (the card's branch at its one head; the
// group branch, read on origin).
func keeps_internal(ctx context.Context, g Git, req LandRequest) (bool, error) {
	switch {
	case req.Gate.SHA == "":
		return false, nil
	case req.Gate.Branch != GroupBranch(req.Stream):
		return len(req.Heads) == 1 && req.Gate.SHA == req.Heads[0] && req.Gate.Branch == req.Branches[0], nil
	}
	tip, _, err := g.LsRemote(ctx, req.RepoDir, GroupRef(req.Stream))
	return err == nil && tip == req.Gate.SHA, err
}

// keptResult_internal is res for a pass that reads the kept gate: its
// rung, the base it was built on, its sha.
func keptResult_internal(gate Gate, res LandResult) LandResult {
	res.Rung, res.BaseTip, res.GatedSHA, res.Gate = gate.Rung, gate.Base, gate.SHA, gate
	if gate.Rung == 2 {
		res.GroupTip = gate.SHA
	}
	return res
}

// PromoteRequest is one promotion of a stream's base to main (LANDING.md,
// "Landed means on main"): Merges are the base merges of the cards on
// base (a card's head where none is recorded).
type PromoteRequest struct {
	RepoDir, Repo, Stream, Base string
	Merges                      []string
	Reran                       bool // as LandRequest.Reran
}

// PromotePass is one pass of a promotion. Merges already on main's tip are
// landed there (so a base deleted, or a promotion another stream on the
// same base made, is no fault). Otherwise, of the merges on base's tip, a
// base that contains main's tip is gated as rung 1 gates a card's branch:
// the one PR base -> main, its checks at base's tip, merged green by the
// repository's allowed method while main is still the tip it was gated
// on, landing those merges. No merge on base (base force-pushed or reset)
// and a base behind main are the coordinator's judgement; so is anything
// gateAndMerge does not merge, as on rung 1.
func PromotePass(ctx context.Context, g Git, fg forge.Forge, req PromoteRequest) (LandResult, error) {
	if len(req.Merges) == 0 || req.Stream == "" || req.Base == "" || req.Repo == "" {
		return LandResult{}, fmt.Errorf("promote refused: PromotePass needs a repo, a stream, a base and the merges")
	}
	if fg == nil {
		return LandResult{}, fmt.Errorf("promote refused: stream %s has no forge, so nothing lands automatically: land each card by hand with nova-sprint land <card> --reason <why>", req.Stream)
	}
	main, mainTip, err := g.Main(ctx, req.RepoDir)
	if err != nil {
		return LandResult{}, err
	}
	res := LandResult{Rung: 1, BaseTip: mainTip}
	baseTip, based, err := g.LsRemote(ctx, req.RepoDir, "refs/heads/"+req.Base)
	if err != nil {
		return res, err
	}
	refs := []string{"refs/heads/" + main}
	if based {
		refs = append(refs, "refs/heads/"+req.Base)
	}
	if err := g.Fetch(ctx, req.RepoDir, refs...); err != nil {
		return res, err
	}
	on, err := onTip_internal(ctx, g, req.RepoDir, req.Merges, mainTip)
	switch {
	case err != nil:
		return res, err
	case len(on) > 0:
		res.Outcome, res.MergeSHA, res.LandedHeads = OutcomeLanded, mainTip, on
		return res, nil
	case !based:
		return res, fmt.Errorf("promote refused: base branch %s is not on origin", req.Base)
	}
	onBase, err := onTip_internal(ctx, g, req.RepoDir, req.Merges, baseTip)
	if err != nil {
		return res, err
	}
	if len(onBase) == 0 {
		res.Outcome, res.Reason = OutcomeMissing, fmt.Sprintf("none of the merges %s is on %s at %s: was it force-pushed or reset?", strings.Join(req.Merges, ", "), req.Base, baseTip)
		return res, nil
	}
	up, err := g.IsAncestor(ctx, req.RepoDir, mainTip, baseTip)
	if err != nil {
		return res, err
	}
	if !up {
		res.Outcome, res.Reason = OutcomeBehind, fmt.Sprintf("%s at %s does not contain %s at %s: merge %s into it, then answer retry", req.Base, baseTip, main, mainTip, main)
		return res, nil
	}
	res.GatedSHA = baseTip
	return gateAndMerge_internal(ctx, g, fg, LandRequest{RepoDir: req.RepoDir, Repo: req.Repo, Stream: req.Stream, Base: main, Heads: onBase, Branches: []string{req.Base}, Reran: req.Reran}, req.Base, res)
}

// onTip_internal is the shas that are tip or its ancestors.
func onTip_internal(ctx context.Context, g Git, repoDir string, shas []string, tip string) ([]string, error) {
	var on []string
	for _, s := range shas {
		ok, err := g.IsAncestor(ctx, repoDir, s, tip)
		if err != nil {
			return nil, err
		}
		if ok {
			on = append(on, s)
		}
	}
	return on, nil
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
func gatedHead_internal(ctx context.Context, g Git, req LandRequest, baseTip string, res *LandResult) (string, string, bool, error) {
	if len(req.Heads) == 1 {
		up, err := g.IsAncestor(ctx, req.RepoDir, baseTip, req.Heads[0])
		if err != nil {
			return "", "", true, err
		}
		if up {
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
	merged, err := g.MergeGroup(ctx, req.RepoDir, baseTip, req.Heads)
	if err != nil {
		return "", true, err
	}
	if merged.Kind != MergeOK {
		res.Outcome, res.Conflicts, res.ConflictHead = OutcomeConflict, merged.Conflicts, merged.Head
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
		if !rested_internal(err, &res) {
			res.Outcome, res.Reason = OutcomeUnopenable, fmt.Sprintf("the PR %s -> %s could not be opened or read: %s", prHead, req.Base, edge.RedactN(err.Error(), edge.FailureMax))
		}
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
	if done := readChecks_internal(ctx, fg, req, &res); done {
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

// rested_internal is whether err is GitHub rate-limiting the forge's
// identity (forge.FaultRateLimit), and if so makes res forge_rested: no
// fact about the landing, so the run asks the pass again as it does a
// pending CI (LAYER-4-EDGES "Rate limits": a fault that spends nothing),
// never a judgement per card. Only the reads before the merge come here: a
// merge GitHub did not answer stays merge_refused, the coordinator's.
func rested_internal(err error, res *LandResult) bool {
	var fe *forge.FaultError
	if !errors.As(err, &fe) || fe.Kind != forge.FaultRateLimit {
		return false
	}
	res.Outcome, res.Reason = OutcomeRested, "GitHub rate-limited the forge's identity: "+edge.RedactN(err.Error(), edge.FailureMax)
	return true
}

// readChecks_internal reads the gated sha's checks into res: true (with
// res's outcome) unless they are green. A verdict is taken only from checks
// that have all completed (LANDING.md "A verdict only from completed
// checks"): a failure while others still run is unfinished, asked again
// until the request's wait, and no log is read until its run completed.
func readChecks_internal(ctx context.Context, fg forge.Forge, req LandRequest, res *LandResult) bool {
	rollups, err := fg.Checks(ctx, req.Repo, []string{res.GatedSHA})
	if err != nil {
		if !rested_internal(err, res) {
			res.Outcome, res.Reason = OutcomeCIFault, "the checks of "+res.GatedSHA+" could not be read: "+edge.RedactN(err.Error(), edge.FailureMax)
		}
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
	default:
		if r.Waiting {
			res.Outcome, res.Pending = OutcomeUnfinished, r.Pending
			res.Reason = fmt.Sprintf("%d checks still pending; failing so far: %s", r.Pending, strings.Join(r.Failing, ", "))
			return true
		}
		verdict_internal(ctx, fg, req, res, r)
	}
	return true
}

// verdict_internal judges the gated sha's completed checks that did not
// pass, read once with how GitHub says each stopped (forge.Failures). A
// stopped check (killed, cancelled, timed out) is no verdict about the code
// and is named apart. Every one stopped is no verdict: the stopped jobs are
// run again once (OutcomeRerun) unless req.Reran, and then it is the
// coordinator's. Otherwise red (a fault when none of the own failures is a
// failed check) with the first own failure's lines and, on red, the base
// tip's own completed red beside it.
func verdict_internal(ctx context.Context, fg forge.Forge, req LandRequest, res *LandResult, r forge.CheckRollup) {
	fails, err := fg.Failures(ctx, req.Repo, res.GatedSHA)
	if err != nil {
		if rested_internal(err, res) {
			return
		}
		res.Reason += "; how each failing check stopped was not read: " + edge.RedactN(err.Error(), edge.FailureMax)
		fails = nil
	}
	var own, stopped []forge.Failure
	for _, f := range fails {
		if f.Stop != "" {
			stopped = append(stopped, f)
			res.Stopped = append(res.Stopped, f.Name+" "+f.Stop)
		} else {
			own = append(own, f)
		}
	}
	if len(fails) == 0 { // nothing read: the rollup's own list (maybe none), each failed on its own
		own = []forge.Failure{{URL: r.URL}}
		for i, n := range r.Failing {
			if i == 0 {
				own[0].Name = n
			} else {
				own = append(own, forge.Failure{Name: n})
			}
		}
	}
	if len(own) == 0 {
		res.Outcome, res.FailingCI, res.CIURL = OutcomeNoVerdict, nil, stopped[0].URL
		res.Reason = "every failing check was stopped, not failed (no verdict about the code): " + strings.Join(res.Stopped, ", ")
		if !req.Reran {
			rerun_internal(ctx, fg, req, res, stopped)
		} else {
			res.Reason += "; its stopped jobs were already run again once"
		}
		return
	}
	res.FailingCI, res.CIURL = nil, own[0].URL
	for _, f := range own {
		if f.Name != "" {
			res.FailingCI = append(res.FailingCI, f.Name)
		}
	}
	if r.State == forge.CheckFault {
		res.Outcome = OutcomeCIFault
		return
	}
	res.Outcome = OutcomeRed
	res.FailingLines = failedLines_internal(ctx, fg, req.Repo, own[0].URL)
	baseRed_internal(ctx, fg, req, res)
}

// rerun_internal runs the stopped jobs again, once per Actions run they are
// in (gh run rerun --failed): OutcomeRerun when every run was asked; a run
// that cannot be asked leaves the no-verdict judgement, saying why.
func rerun_internal(ctx context.Context, fg forge.Forge, req LandRequest, res *LandResult, stopped []forge.Failure) {
	var runs []string
	for _, f := range stopped {
		if run := forge.RunOf(f.URL); run != "" && !slices.Contains(runs, run) {
			runs = append(runs, run)
			if err := fg.Rerun(ctx, req.Repo, f.URL); err != nil {
				if !rested_internal(err, res) {
					res.Reason += "; run " + run + " could not be run again: " + edge.RedactN(err.Error(), edge.FailureMax)
				}
				return
			}
		}
	}
	if len(runs) == 0 {
		res.Reason += "; none of them is in an Actions run, so none was run again"
		return
	}
	res.Outcome = OutcomeRerun
	res.Reason += "; asked GitHub to run its stopped jobs again (run " + strings.Join(runs, ", ") + ")"
}

// baseRed_internal reads the base tip's checks into res.BaseFailing when
// the base is red too and its checks have all completed; a base whose
// checks still run is said as such, with what is red so far, and never
// compared; one that cannot be read is said in the reason.
func baseRed_internal(ctx context.Context, fg forge.Forge, req LandRequest, res *LandResult) {
	base, err := fg.Checks(ctx, req.Repo, []string{res.BaseTip})
	if err != nil {
		res.Reason += "; the checks of the base tip " + res.BaseTip + " could not be read: " + edge.RedactN(err.Error(), edge.FailureMax)
		return
	}
	switch b := base[res.BaseTip]; {
	case b.State == forge.CheckPending || b.Waiting:
		red := []string{}
		if b.State == forge.CheckRed {
			red = b.Failing
		}
		res.Reason += fmt.Sprintf("; base CI still running at its tip %s: %d red so far", res.BaseTip, len(red))
		if len(red) > 0 {
			res.Reason += " (" + strings.Join(red, ", ") + ")"
		}
	case b.State == forge.CheckRed:
		res.BaseFailing = b.Failing
	}
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

func allAncestors_internal(ctx context.Context, g Git, repoDir string, heads []string, target string) (bool, error) {
	for _, h := range heads {
		if ok, err := g.IsAncestor(ctx, repoDir, h, target); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}
