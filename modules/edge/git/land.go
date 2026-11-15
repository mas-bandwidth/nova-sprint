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
// only automatic move; pending, unfinished, absent, base-pending,
// base-moved and forge-rested are asked again by the caller (unfinished,
// absent and base-pending only until the request's wait); every other
// outcome is a judgement for the coordinator.
type LandOutcome string

const (
	OutcomeLanded     LandOutcome = "landed"
	OutcomePending    LandOutcome = "ci_pending"    // checks queued or running
	OutcomeUnfinished LandOutcome = "ci_unfinished" // a check failed while others still run: no verdict yet; the caller bounds the wait
	OutcomeNoVerdict  LandOutcome = "ci_no_verdict" // every failing check was stopped (killed, cancelled, timed out), not failed
	OutcomeAbsent     LandOutcome = "ci_absent"     // no check reported yet: the caller bounds the wait
	OutcomeBaseWait   LandOutcome = "base_pending"  // red, and the base's run to compare it with is not done yet: the caller bounds the wait
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
// Gate is the gate the last pass of this request kept (LandResult.Kept;
// zero: none).
type LandRequest struct {
	RepoDir, Repo, Stream, Base string
	Heads, Branches             []string
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
// gate's own failures are each compared with the base (LANDING.md "A red
// base"): BaseRed are the jobs red on the base too (the base's: a landing
// that merged past them names them), NotBase each other failure and why it
// is not the base's (green there, still running there, or not compared).
// Stopped names the failing checks GitHub says were stopped (killed,
// cancelled, timed out), apart from FailingCI, the ones that failed on
// their own; Pending is how many checks were still running. Gate is the
// gate the pass read; ConflictHead the head whose merge into the group
// failed, when the group's merge conflicted.
type LandResult struct {
	Outcome                                                           LandOutcome
	Rung, Pending                                                     int
	BaseTip, GroupTip, GatedSHA, MergeSHA, PR, CIURL, Reason          string
	LandedHeads, Conflicts, FailingCI, FailingLines, Stopped, NotBase []string
	BaseRed                                                           []SharedRed
	ConflictHead                                                      string
	Gate                                                              Gate
}

// SharedRed is one job red at the gated sha and on the base too: the job
// of the same name, in a run of the same workflow started by the same
// event (the base's probe PR, on the base branch Base), failed there. It is
// the base's, not the card's.
type SharedRed struct {
	Base, Workflow, Job string
	HeadRun, BaseRun    int64
}

// Kept is the gate the next pass of the same request reads: this pass's
// while its CI has no verdict yet (pending, unfinished, absent, red with
// the base's run to compare still going, a rested forge), else none: a
// verdict ends the request, and a green gate whose base moved is built
// again on the new tip.
func (r LandResult) Kept() Gate {
	switch r.Outcome {
	case OutcomePending, OutcomeUnfinished, OutcomeAbsent, OutcomeBaseWait, OutcomeRested:
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
	return gateAndMerge_internal(ctx, g, fg, LandRequest{RepoDir: req.RepoDir, Repo: req.Repo, Stream: req.Stream, Base: main, Heads: onBase, Branches: []string{req.Base}}, req.Base, res)
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
	if done := readChecks_internal(ctx, g, fg, req, &res); done {
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
// res's outcome) unless they are green, or red only where the base is red
// too. A verdict is taken only from checks that have all completed
// (LANDING.md "A verdict only from completed checks"): a failure while
// others still run is unfinished, asked again until the request's wait,
// and no log is read until its run completed.
func readChecks_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, res *LandResult) bool {
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
		return !verdict_internal(ctx, g, fg, req, res, r)
	}
	return true
}

// verdict_internal judges the gated sha's completed checks that did not
// pass, read once with how GitHub says each stopped (forge.Failures), and
// is true when the gate lands anyway: every own failure is red on the base
// too (compare_internal) and none was stopped. A stopped check (killed,
// cancelled, timed out) is no verdict about the code and is named apart.
// Every one stopped is the coordinator's no-verdict judgement at once, with
// the first stopped check's failing lines: the sprint never runs CI again
// (LESSONS-PRIORITY-AND-MERGE W19). Otherwise red (a fault when none of the
// own failures is a failed check) with the first own failure's lines that
// is not the base's, or no verdict when the own ones are all the base's and
// others were stopped, or still waiting for the base's run.
func verdict_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, res *LandResult, r forge.CheckRollup) bool {
	fails, err := fg.Failures(ctx, req.Repo, res.GatedSHA)
	if err != nil {
		if rested_internal(err, res) {
			return false
		}
		res.Reason += "; how each failing check stopped was not read: " + edge.RedactN(err.Error(), edge.FailureMax)
		fails = nil
	}
	own, stopped := split_internal(fails, r)
	for _, f := range stopped {
		res.Stopped = append(res.Stopped, f.Name+" "+f.Stop)
	}
	if len(own) == 0 {
		res.Outcome, res.FailingCI, res.CIURL = OutcomeNoVerdict, nil, stopped[0].URL
		res.Reason = "every failing check was stopped, not failed (no verdict about the code): " + strings.Join(res.Stopped, ", ")
		res.FailingLines = failedLines_internal(ctx, fg, req.Repo, stopped[0].URL)
		return false
	}
	res.FailingCI, res.CIURL = nil, own[0].URL
	for _, f := range own {
		if f.Name != "" {
			res.FailingCI = append(res.FailingCI, f.Name)
		}
	}
	if r.State == forge.CheckFault {
		res.Outcome = OutcomeCIFault
		return false
	}
	res.Outcome = OutcomeRed
	if compare_internal(ctx, g, fg, req, res, own) {
		if len(stopped) == 0 {
			return true
		}
		res.Outcome, res.CIURL = OutcomeNoVerdict, stopped[0].URL
		res.Reason = "every check that failed on its own is red on the base too; the others were stopped, not failed (no verdict about the code): " + strings.Join(res.Stopped, ", ")
		res.FailingLines = failedLines_internal(ctx, fg, req.Repo, stopped[0].URL)
		return false
	}
	if res.Outcome == OutcomeRed {
		first := own[0]
		for _, f := range own {
			if !slices.ContainsFunc(res.BaseRed, func(s SharedRed) bool { return s.Job == f.Name }) {
				first = f
				break
			}
		}
		res.CIURL, res.FailingLines = first.URL, failedLines_internal(ctx, fg, req.Repo, first.URL)
	}
	return false
}

// split_internal is a sha's failures as its own (failed) and stopped ones;
// with none read, the rollup's own failing list (maybe none), each failed
// on its own, the first at the rollup's URL.
func split_internal(fails []forge.Failure, r forge.CheckRollup) (own, stopped []forge.Failure) {
	for _, f := range fails {
		if f.Stop != "" {
			stopped = append(stopped, f)
		} else {
			own = append(own, f)
		}
	}
	if len(fails) == 0 {
		own = []forge.Failure{{URL: r.URL}}
		for i, n := range r.Failing {
			if i == 0 {
				own[0].Name = n
			} else {
				own = append(own, forge.Failure{Name: n})
			}
		}
	}
	return own, stopped
}

// compare_internal compares each own failure of the gated sha with the
// base (LANDING.md "A red base"): the job of the same name in the same
// workflow's run, started by the same event, on the base's probe PR
// (baseSide_internal). Red there too, it is the base's (res.BaseRed); green
// there, the card's; still running there, it waits; anything else is not
// compared, and res.NotBase says why. True when every own failure is the
// base's: the gate lands past them. Else res stays ci_red (a failure that
// is the card's or not compared), or is base_pending (the base's run still
// going) or forge_rested.
func compare_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, res *LandResult, own []forge.Failure) bool {
	heads, err := fg.Runs(ctx, req.Repo, res.GatedSHA)
	if err != nil {
		if !rested_internal(err, res) {
			res.NotBase = append(res.NotBase, "not compared with the base: the runs of "+res.GatedSHA+" were not read: "+edge.RedactN(err.Error(), edge.FailureMax))
		}
		return false
	}
	b := &baseSide_internal{ctx: ctx, g: g, fg: fg, req: req, tip: res.BaseTip, jobs: map[int64][]forge.Job{}}
	stops, waits := false, false
	for _, f := range own {
		v, shared, fact := b.job(f, heads)
		switch v {
		case jobRested:
			rested_internal(b.err, res)
			return false
		case jobShared:
			res.BaseRed = append(res.BaseRed, shared)
			continue
		case jobPending:
			waits = true
		default:
			stops = true
		}
		res.NotBase = append(res.NotBase, fact)
	}
	switch {
	case stops:
	case waits:
		res.Outcome = OutcomeBaseWait
		res.Reason = "red, and the base's run to compare it with is still going: " + strings.Join(res.NotBase, "; ")
	default:
		return true
	}
	return false
}

type jobVerdict int

const (
	jobShared     jobVerdict = iota // red on the base too: the base's
	jobPending                      // the base's run of it still going
	jobCard                         // green on the base: the card's
	jobUncompared                   // nothing on the base to compare it with
	jobRested                       // the forge rests: no fact
)

// baseSide_internal is the base side of one comparison, read once on
// first need: the probe (an empty commit on the base tip, pushed to
// ProbeBranch, its PR into the base opened, so the base's tree runs every
// pull-request workflow as the landing's PR did), its runs, and each run's
// jobs.
type baseSide_internal struct {
	ctx          context.Context
	g            Git
	fg           forge.Forge
	req          LandRequest
	tip          string
	read         bool
	sha, pr, why string
	err          error // a rate limit: no fact
	runs         []forge.Run
	jobs         map[int64][]forge.Job
}

// job is one own failure f of the gated sha (heads: its runs) as the base
// has it: the verdict, the shared red, or the fact that says why not.
func (b *baseSide_internal) job(f forge.Failure, heads []forge.Run) (jobVerdict, SharedRed, string) {
	not := f.Name + ": not compared with the base: "
	i := slices.IndexFunc(heads, func(r forge.Run) bool { return r.ID == forge.RunOf(f.URL) })
	if i < 0 {
		return jobUncompared, SharedRed{}, not + "it is no job of an Actions run of the gated sha"
	}
	run := heads[i]
	if run.Event != "pull_request" && run.Event != "pull_request_target" {
		return jobUncompared, SharedRed{}, fmt.Sprintf("%sits run %d was started by %s, and only a pull request's run has a base PR to compare with", not, run.ID, run.Event)
	}
	if b.probe(); b.err != nil {
		return jobRested, SharedRed{}, ""
	}
	if b.why != "" {
		return jobUncompared, SharedRed{}, not + b.why
	}
	on := fmt.Sprintf("the base's probe PR %s at %s", b.pr, b.sha)
	base, ok := baseRun_internal(b.runs, run)
	switch {
	case !ok && settled_internal(b.runs):
		return jobUncompared, SharedRed{}, fmt.Sprintf("%s%s ran no %s run of workflow %s", not, on, run.Event, run.Name)
	case !ok:
		return jobPending, SharedRed{}, fmt.Sprintf("%s: workflow %s has not started on %s yet", f.Name, run.Name, on)
	}
	jobs, err := b.jobsOf(base.ID)
	switch {
	case err != nil && b.err != nil:
		return jobRested, SharedRed{}, ""
	case err != nil:
		return jobUncompared, SharedRed{}, fmt.Sprintf("%sthe jobs of the base's run %d were not read: %s", not, base.ID, edge.RedactN(err.Error(), edge.FailureMax))
	}
	j := slices.IndexFunc(jobs, func(x forge.Job) bool { return x.Name == f.Name })
	switch {
	case j < 0 && base.Status != "completed", j >= 0 && jobs[j].Status != "completed":
		return jobPending, SharedRed{}, fmt.Sprintf("%s: still running in the base's run %d on %s", f.Name, base.ID, on)
	case j < 0:
		return jobUncompared, SharedRed{}, fmt.Sprintf("%sthe base's run %d of %s has no job of that name", not, base.ID, run.Name)
	}
	switch c := strings.ToLower(jobs[j].Conclusion); c {
	case "failure":
		return jobShared, SharedRed{Base: b.req.Base, Workflow: run.Name, Job: f.Name, HeadRun: run.ID, BaseRun: base.ID}, ""
	case "success", "neutral":
		return jobCard, SharedRed{}, fmt.Sprintf("%s: red in run %d here, green on the base in run %d: the card's", f.Name, run.ID, base.ID)
	default:
		return jobUncompared, SharedRed{}, fmt.Sprintf("%son the base it ended %s in run %d, no verdict", not, cmp.Or(c, "with no conclusion"), base.ID)
	}
}

// probe reads the base side once: the probe commit on the tip, pushed to
// the probe branch when it is not there (a lease on what was), its PR into
// the base found or opened, and the runs at the probe. A step that fails
// says why in b.why (nothing on the base to compare with); a rate limit
// is b.err.
func (b *baseSide_internal) probe() {
	if b.read {
		return
	}
	b.read = true
	ref := "refs/heads/" + ProbeBranch(b.req.Base)
	sha, err := b.g.Probe(b.ctx, b.req.RepoDir, b.tip)
	if err != nil {
		b.why = "the probe commit on " + b.tip + " was not made: " + edge.RedactN(err.Error(), edge.FailureMax)
		return
	}
	b.sha = sha
	old, _, err := b.g.LsRemote(b.ctx, b.req.RepoDir, ref)
	if err == nil && old != sha {
		err = b.g.Push(b.ctx, b.req.RepoDir, sha, ref, old)
	}
	if err != nil {
		b.why = "the probe branch " + ProbeBranch(b.req.Base) + " could not be pushed to " + sha + ": " + edge.RedactN(err.Error(), edge.FailureMax)
		return
	}
	pr, ok, err := b.fg.PR(b.ctx, b.req.Repo, ProbeBranch(b.req.Base), b.req.Base)
	if err == nil && !ok {
		pr, err = b.fg.OpenPR(b.ctx, b.req.Repo, ProbeBranch(b.req.Base), b.req.Base, b.req.Base+": its tip, for CI to compare a landing's red with",
			"Opened by nova-sprint's landing: an empty commit on "+b.req.Base+"'s tip, so its pull-request CI runs the base's tree as a landing's PR runs it. A job red here and on a landing's PR is the base's, not the card's. Never merged.")
	}
	if err == nil {
		b.pr = cmp.Or(pr.URL, fmt.Sprintf("#%d", pr.Number))
		b.runs, err = b.fg.Runs(b.ctx, b.req.Repo, sha)
	}
	b.fail_internal(err, "the base's probe PR or its runs could not be read")
}

// jobsOf is run's jobs, read once.
func (b *baseSide_internal) jobsOf(run int64) ([]forge.Job, error) {
	if jobs, ok := b.jobs[run]; ok {
		return jobs, nil
	}
	jobs, err := b.fg.Jobs(b.ctx, b.req.Repo, run)
	if err != nil {
		b.fail_internal(err, "")
		return nil, err
	}
	b.jobs[run] = jobs
	return jobs, nil
}

// fail_internal records err: a rate limit as b.err, else, with what, as
// b.why.
func (b *baseSide_internal) fail_internal(err error, what string) {
	var fe *forge.FaultError
	switch {
	case err == nil:
	case errors.As(err, &fe) && fe.Kind == forge.FaultRateLimit:
		b.err = err
	case what != "":
		b.why = what + ": " + edge.RedactN(err.Error(), edge.FailureMax)
	}
}

// baseRun_internal is the base's run to compare head's with: of the runs
// of head's workflow started by head's event, the newest that completed
// other than cancelled (a run cancelled because the probe moved is no
// verdict), else the newest still going, else the newest; ok is false when
// there is none.
func baseRun_internal(runs []forge.Run, head forge.Run) (forge.Run, bool) {
	var done, going, newest forge.Run
	for _, r := range runs {
		if r.Workflow != head.Workflow || r.Event != head.Event {
			continue
		}
		newest = maxRun_internal(newest, r)
		switch {
		case r.Status != "completed":
			going = maxRun_internal(going, r)
		case r.Conclusion != "cancelled":
			done = maxRun_internal(done, r)
		}
	}
	for _, r := range []forge.Run{done, going, newest} {
		if r.ID != 0 {
			return r, true
		}
	}
	return forge.Run{}, false
}

// maxRun_internal is the newer of a and b (the higher id).
func maxRun_internal(a, b forge.Run) forge.Run {
	if b.ID > a.ID {
		return b
	}
	return a
}

// settled_internal is whether runs have started and all completed: a
// workflow not among them now will not start for this sha.
func settled_internal(runs []forge.Run) bool {
	return len(runs) > 0 && !slices.ContainsFunc(runs, func(r forge.Run) bool { return r.Status != "completed" })
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
