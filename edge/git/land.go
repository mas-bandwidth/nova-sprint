package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"slices"
	"strconv"
	"strings"
	"time"
)

type LandOutcome string

const (
	OutcomeLanded        LandOutcome = "landed"
	OutcomeMissingHead   LandOutcome = "missing_head"
	OutcomeRung3Conflict LandOutcome = "rung3_conflict"
	OutcomeLedgerFailed  LandOutcome = "ledger_failed"
	OutcomePushRefused   LandOutcome = "push_refused"
	OutcomeCIPending     LandOutcome = "ci_pending"
	OutcomeCIRed         LandOutcome = "ci_red"
	OutcomeCIFault       LandOutcome = "ci_fault"
	OutcomeNoLandingHost LandOutcome = "no_landing_host"
)

type LandRequest struct {
	RepoDir, Repo, Stream, Base, Mode string
	Heads, After, RemoteHosts         []string
	Ledgers                           []LedgerRule
	PassedSHAs                        map[string]bool
	Exec                              edge.Exec
}
type LandResult struct {
	Outcome                                                      LandOutcome
	BaseTip, StreamTip, GatedSHA, BlamedHead, MissingHead, CIURL string
	Reason                                                       string
	LandedHeads, GreenHeads, Conflicts, FailingCI                []string
	Remerged, ReusedCI                                           bool
	NoChecks                                                     bool // pending with no check reported on GatedSHA: no CI run has started
	PR                                                           int  // the group's PR into the base, once opened
}

func LandPass(ctx context.Context, g Git, fg forge.Forge, req LandRequest) (LandResult, error) {
	if len(req.Heads) == 0 || req.Stream == "" || req.Base == "" {
		return LandResult{}, fmt.Errorf("land refused: LandPass requires Stream, Base, and at least one Head")
	}
	for _, h := range req.Heads {
		if err := ValidateCardBranch(req.Stream, h); err != nil {
			return LandResult{}, err
		}
	}
	if len(req.RemoteHosts) > 0 {
		if _, err := RunRemoteLandStep(ctx, req.Exec, req.RemoteHosts, DefaultSSHConnectTimeoutSec, DefaultRebaseTimeout, "--stream", req.Stream, "--base", req.Base); err != nil {
			return LandResult{Outcome: OutcomeNoLandingHost, Reason: err.Error()}, err
		}
	}
	if strings.EqualFold(strings.TrimSpace(req.Mode), "hand") || fg == nil {
		return LandResult{}, fmt.Errorf("land refused: stream %s has no forge gate configured (use stream set --mode hand and land by hand)", req.Stream)
	}
	baseRef, streamRef := "refs/heads/"+req.Base, StreamRef(req.Stream)
	baseTip, streamOld, missing, err := fetchAndCheckHeads_internal(ctx, g, req, baseRef, streamRef)
	if err != nil {
		return LandResult{}, err
	}
	if missing != "" {
		return LandResult{Outcome: OutcomeMissingHead, MissingHead: missing, Reason: fmt.Sprintf("land refused: head %s is not on origin: git push origin <branch>", missing)}, nil
	}
	if allAncestors_internal(ctx, g, req.RepoDir, req.Heads, baseTip) {
		if streamOld != baseTip {
			_ = g.Push(ctx, req.RepoDir, baseTip, streamRef, streamOld)
		}
		return LandResult{Outcome: OutcomeLanded, BaseTip: baseTip, StreamTip: baseTip, GatedSHA: baseTip, LandedHeads: slices.Clone(req.Heads)}, nil
	}
	merged, res, done, err := mergeAndPushStream_internal(ctx, g, req, baseTip, streamRef, streamOld)
	if err != nil || done {
		return res, err
	}
	prNum, gateRes, done, err := gateMergedGroup_internal(ctx, g, fg, req, baseTip, streamRef, merged)
	if err != nil || done {
		return gateRes, err
	}
	return landAndRecord_internal(ctx, g, fg, req, baseRef, streamRef, baseTip, merged, prNum, gateRes.ReusedCI)
}
func fetchAndCheckHeads_internal(ctx context.Context, g Git, req LandRequest, baseRef, streamRef string) (string, string, string, error) {
	for _, h := range req.Heads {
		if _, ok, err := g.LsRemote(ctx, req.RepoDir, h); err != nil {
			return "", "", "", err
		} else if !ok {
			return "", "", h, nil
		}
	}
	baseTip, ok, err := g.LsRemote(ctx, req.RepoDir, baseRef)
	if err != nil || !ok {
		return "", "", "", cmp.Or(err, fmt.Errorf("land refused: base branch %s not on origin", baseRef))
	}
	streamOld, _, err := g.LsRemote(ctx, req.RepoDir, streamRef)
	if err != nil {
		return "", "", "", err
	}
	refs := append([]string{baseRef}, req.Heads...)
	if streamOld != "" {
		refs = append(refs, streamRef)
	}
	return baseTip, streamOld, "", g.Fetch(ctx, req.RepoDir, refs...)
}
func allAncestors_internal(ctx context.Context, g Git, repoDir string, heads []string, target string) bool {
	for _, h := range heads {
		if ok, err := g.IsAncestor(ctx, repoDir, h, target); err != nil || !ok {
			return false
		}
	}
	return true
}
func mergeAndPushStream_internal(ctx context.Context, g Git, req LandRequest, onto, streamRef, expectOld string) (MergeResult, LandResult, bool, error) {
	merged, err := g.MergeGroup(ctx, req.RepoDir, onto, req.Heads, req.Ledgers)
	if err != nil {
		return MergeResult{}, LandResult{}, true, err
	}
	switch merged.Kind {
	case MergeConflict:
		return merged, LandResult{Outcome: OutcomeRung3Conflict, BaseTip: onto, Conflicts: merged.Conflicts, Reason: fmt.Sprintf("rung 3 conflict merging stream %s onto %s: %s", req.Stream, onto, strings.Join(merged.Conflicts, ", "))}, true, nil
	case MergeLedgerFailed:
		return merged, LandResult{Outcome: OutcomeLedgerFailed, BaseTip: onto, Conflicts: merged.Conflicts, Reason: merged.Reason}, true, nil
	}
	if err := g.Push(ctx, req.RepoDir, merged.SHA, streamRef, expectOld); err != nil {
		if errors.Is(err, ErrPushRefused) {
			return merged, LandResult{Outcome: OutcomePushRefused, BaseTip: onto, GatedSHA: merged.SHA, Reason: err.Error()}, true, nil
		}
		return merged, LandResult{}, true, err
	}
	return merged, LandResult{}, false, nil
}
func gateMergedGroup_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, baseTip, streamRef string, merged MergeResult) (int, LandResult, bool, error) {
	// A stream with a forge (forge github) lands through a PR and its CI; one
	// without a forge never reaches here (LandPass refuses it: hand only).
	headBranch := StreamBranch(req.Stream)
	pr, ok, err := fg.PR(ctx, req.Repo, headBranch, req.Base)
	if err == nil && !ok {
		pr, err = fg.OpenPR(ctx, req.Repo, headBranch, req.Base, fmt.Sprintf("sprint/%s: land %d card(s)", req.Stream, len(req.Heads)), "Automated stream landing group.")
	}
	if err != nil {
		return 0, classifyForgeErr_internal(err, baseTip, merged.SHA), true, nil
	}
	prNum := pr.Number
	if len(req.Heads) == 1 && req.PassedSHAs[merged.SHA] {
		return prNum, LandResult{ReusedCI: true}, false, nil
	}
	rollups, err := fg.Checks(ctx, req.Repo, []string{merged.SHA})
	if err != nil {
		return prNum, classifyForgeErr_internal(err, baseTip, merged.SHA), true, nil
	}
	r := rollups[merged.SHA]
	switch r.State {
	case forge.CheckGreen:
		return prNum, LandResult{}, false, nil
	case forge.CheckPending, "":
		return prNum, LandResult{Outcome: OutcomeCIPending, BaseTip: baseTip, GatedSHA: merged.SHA, NoChecks: r.Runs == 0, PR: prNum, Reason: r.Reason}, true, nil
	case forge.CheckFault:
		return prNum, LandResult{Outcome: OutcomeCIFault, BaseTip: baseTip, GatedSHA: merged.SHA, CIURL: r.URL, PR: prNum, Reason: r.Reason}, true, nil
	default:
		return prNum, redResult_internal(req, baseTip, merged.SHA, r, prNum), true, nil
	}
}
func classifyForgeErr_internal(err error, baseTip, sha string) LandResult {
	return LandResult{Outcome: OutcomeCIFault, BaseTip: baseTip, GatedSHA: sha, Reason: err.Error()}
}
func landAndRecord_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, baseRef, streamRef, baseTip string, merged MergeResult, prNum int, reusedCI bool) (LandResult, error) {
	curBase, ok, err := g.LsRemote(ctx, req.RepoDir, baseRef)
	if err != nil || !ok {
		return LandResult{}, fmt.Errorf("land: ls-remote %s: %w", baseRef, err)
	}
	remerged := false
	if curBase != baseTip {
		var done bool
		var remRes LandResult
		if merged, remRes, done, err = remergeOnMovedBase_internal(ctx, g, fg, req, baseRef, streamRef, curBase, merged.SHA); err != nil || done {
			return remRes, err
		}
		baseTip, remerged = curBase, true
	}
	if prNum > 0 {
		if err := fg.Merge(ctx, req.Repo, prNum, merged.SHA, req.Base); err != nil {
			return LandResult{Outcome: OutcomePushRefused, BaseTip: baseTip, GatedSHA: merged.SHA, Remerged: remerged, Reason: err.Error()}, nil
		}
	}
	if err := g.Push(ctx, req.RepoDir, merged.SHA, baseRef, baseTip); err != nil {
		if errors.Is(err, ErrPushRefused) {
			return LandResult{Outcome: OutcomePushRefused, BaseTip: baseTip, GatedSHA: merged.SHA, Remerged: remerged, Reason: err.Error()}, nil
		}
		return LandResult{}, err
	}
	newBase, ok, err := g.LsRemote(ctx, req.RepoDir, baseRef)
	if err != nil || !ok {
		newBase = merged.SHA
	}
	_ = g.Fetch(ctx, req.RepoDir, baseRef)
	if err := g.Push(ctx, req.RepoDir, newBase, streamRef, merged.SHA); err != nil && !errors.Is(err, ErrPushRefused) {
		return LandResult{}, err
	}
	if !allAncestors_internal(ctx, g, req.RepoDir, req.Heads, newBase) {
		return LandResult{}, fmt.Errorf("land invariant M21 violated: not all group heads are ancestors of pushed base %s", newBase)
	}
	return LandResult{Outcome: OutcomeLanded, BaseTip: newBase, StreamTip: newBase, GatedSHA: merged.SHA, LandedHeads: slices.Clone(req.Heads), Remerged: remerged, ReusedCI: reusedCI}, nil
}
func remergeOnMovedBase_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, baseRef, streamRef, newBase, prevStreamSHA string) (MergeResult, LandResult, bool, error) {
	if err := g.Fetch(ctx, req.RepoDir, baseRef); err != nil {
		return MergeResult{}, LandResult{}, true, err
	}
	merged, res, done, err := mergeAndPushStream_internal(ctx, g, req, newBase, streamRef, prevStreamSHA)
	if err != nil || done {
		res.Remerged = true
		return merged, res, true, err
	}
	rollups, err := fg.Checks(ctx, req.Repo, []string{merged.SHA})
	if err != nil {
		r := classifyForgeErr_internal(err, newBase, merged.SHA)
		r.Remerged = true
		return merged, r, true, nil
	}
	r := rollups[merged.SHA]
	switch r.State {
	case forge.CheckGreen:
		return merged, LandResult{Remerged: true}, false, nil
	case forge.CheckPending, "":
		return merged, LandResult{Outcome: OutcomeCIPending, BaseTip: newBase, GatedSHA: merged.SHA, NoChecks: r.Runs == 0, Remerged: true, Reason: r.Reason}, true, nil
	case forge.CheckFault:
		return merged, LandResult{Outcome: OutcomeCIFault, BaseTip: newBase, GatedSHA: merged.SHA, CIURL: r.URL, Remerged: true, Reason: r.Reason}, true, nil
	default:
		res := redResult_internal(req, newBase, merged.SHA, r, 0)
		res.Remerged = true
		return merged, res, true, nil
	}
}

// redResult_internal is CI red on the group: the failing checks, no
// bisection and no blame across heads (INTENT rule 16, Glenn 2026-11-07:
// red on the group is the coordinator's, who decides which head to pull).
// A group of one names its head, the only head the red can be.
func redResult_internal(req LandRequest, baseTip, sha string, r forge.CheckRollup, pr int) LandResult {
	blamed := ""
	if len(req.Heads) == 1 {
		blamed = req.Heads[0]
	}
	return LandResult{Outcome: OutcomeCIRed, BaseTip: baseTip, GatedSHA: sha, BlamedHead: blamed, FailingCI: r.Failing, CIURL: r.URL, PR: pr, Reason: r.Reason}
}

type RemoteLandResult struct {
	Host, Stdout, Stderr string
	SkippedHosts         []string
	ExitCode             int
}

func RunRemoteLandStep(ctx context.Context, execRunner edge.Exec, hosts []string, connectTimeoutSec int, stepTimeout time.Duration, args ...string) (RemoteLandResult, error) {
	execRunner = cmp.Or[edge.Exec](execRunner, edge.NewOSExec())
	connectTimeoutSec = cmp.Or(connectTimeoutSec, DefaultSSHConnectTimeoutSec)
	stepTimeout = cmp.Or(stepTimeout, DefaultRebaseTimeout)
	var skipped []string
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" || strings.HasPrefix(host, "-") {
			continue
		}
		argv := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=" + strconv.Itoa(connectTimeoutSec), "-o", "ControlMaster=no", "-o", "ControlPath=none", host, shellQuote_internal("nova-sprint"), shellQuote_internal("land-step")}
		for _, a := range args {
			argv = append(argv, shellQuote_internal(a))
		}
		res, err := execRunner.Run(ctx, edge.Cmd{Argv: argv, Timeout: stepTimeout})
		if err != nil || res.ExitCode == 255 {
			skipped = append(skipped, host)
			continue
		}
		return RemoteLandResult{Host: host, SkippedHosts: skipped, Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, nil
	}
	return RemoteLandResult{SkippedHosts: skipped}, fmt.Errorf("%w: land refused: no landing host answered (tried: %s): check nova-fleet check", ErrNoLandingHost, strings.Join(skipped, ", "))
}
func shellQuote_internal(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
