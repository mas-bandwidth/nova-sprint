package git

import (
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
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
	Heads                             []string
	Ledgers                           []LedgerRule
	RequirePR, BisectOnRed            bool
	PassedSHAs, PassedTrees           map[string]bool
	After                             []string
}
type LandResult struct {
	Outcome                      LandOutcome
	BaseTip, StreamTip, GatedSHA string
	LandedHeads, GreenHeads      []string
	BlamedHead, MissingHead      string
	Conflicts, FailingCI         []string
	CIURL                        string
	Remerged, ReusedCI           bool
	Reason                       string
}
type Lander struct {
	Git    Git
	Forge  forge.Forge
	dedup  *edge.Deduplicator
	OnStep func(step string)
}

func NewLander(g Git, fg forge.Forge) *Lander {
	return &Lander{Git: g, Forge: fg, dedup: edge.NewDeduplicator()}
}
func (l *Lander) Execute(ctx context.Context, req edge.EdgeRequest, land LandRequest) (edge.EdgeResult, error) {
	if req.Kind == "" {
		req.Kind = edge.FactLand
	}
	if req.SHA == "" && len(land.Heads) > 0 {
		req.SHA = strings.Join(land.Heads, ",")
	}
	return l.dedup.Do(ctx, req, func(ctx context.Context, r edge.EdgeRequest) (edge.EdgeResult, error) {
		if l.OnStep != nil {
			l.OnStep(r.Kind)
		}
		lr, err := LandPass(ctx, l.Git, l.Forge, land)
		if err != nil {
			return edge.EdgeResult{ID: r.ID, Kind: r.Kind, Card: r.Card, SHA: r.SHA}, err
		}
		return toEdgeResult_internal(r, lr), nil
	})
}
func toEdgeResult_internal(r edge.EdgeRequest, lr LandResult) edge.EdgeResult {
	out := edge.EdgeResult{
		ID:        r.ID,
		Kind:      r.Kind,
		Card:      r.Card,
		SHA:       r.SHA,
		Outcome:   string(lr.Outcome),
		BaseTip:   lr.BaseTip,
		StreamTip: lr.StreamTip,
		Landed:    lr.LandedHeads,
		Files:     lr.Conflicts,
		Reason:    lr.Reason,
	}
	switch lr.Outcome {
	case OutcomeRung3Conflict:
		out.Outcome = "refused"
		out.Failure = "conflict"
	case OutcomeMissingHead, OutcomeLedgerFailed, OutcomePushRefused, OutcomeNoLandingHost:
		out.Outcome = "refused"
		out.Failure = string(lr.Outcome)
	case OutcomeCIPending:
		out.Outcome = "pending"
	case OutcomeCIRed:
		out.Outcome = "red"
	case OutcomeCIFault:
		out.Outcome = "fault"
	}
	return out
}
func LandPass(ctx context.Context, g Git, fg forge.Forge, req LandRequest) (LandResult, error) {
	if len(req.Heads) == 0 || req.Stream == "" || req.Base == "" {
		return LandResult{}, fmt.Errorf("land refused: LandPass requires Stream, Base, and at least one Head")
	}
	baseRef := "refs/heads/" + req.Base
	streamRef := "refs/heads/sprint/" + req.Stream
	baseTip, streamOld, missing, err := fetchAndCheckHeads_internal(ctx, g, req, baseRef, streamRef)
	if err != nil {
		return LandResult{}, err
	}
	if missing != "" {
		return LandResult{
			Outcome:     OutcomeMissingHead,
			MissingHead: missing,
			Reason:      fmt.Sprintf("land refused: head %s is not on origin: git push origin <branch>", missing),
		}, nil
	}
	if allAncestors_internal(ctx, g, req.RepoDir, req.Heads, baseTip) {
		if streamOld != baseTip {
			_ = g.Push(ctx, req.RepoDir, baseTip, streamRef, streamOld)
		}
		return LandResult{Outcome: OutcomeLanded, BaseTip: baseTip, StreamTip: baseTip, GatedSHA: baseTip, LandedHeads: append([]string(nil), req.Heads...)}, nil
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
	switch {
	case err != nil:
		return "", "", "", err
	case !ok:
		return "", "", "", fmt.Errorf("land refused: base branch %s not on origin", baseRef)
	}
	streamOld, _, err := g.LsRemote(ctx, req.RepoDir, streamRef)
	if err != nil {
		return "", "", "", err
	}
	refs := append([]string{baseRef}, req.Heads...)
	if streamOld != "" {
		refs = append(refs, streamRef)
	}
	if err := g.Fetch(ctx, req.RepoDir, refs...); err != nil {
		return "", "", "", err
	}
	return baseTip, streamOld, "", nil
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
	if merged.Kind == MergeConflict {
		return merged, LandResult{
			Outcome:   OutcomeRung3Conflict,
			BaseTip:   onto,
			Conflicts: merged.Conflicts,
			Reason:    fmt.Sprintf("rung 3 conflict merging stream %s onto %s: %s", req.Stream, onto, strings.Join(merged.Conflicts, ", ")),
		}, true, nil
	}
	if merged.Kind == MergeLedgerFailed {
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
	var prNum int
	if req.RequirePR && fg != nil {
		headBranch := "sprint/" + req.Stream
		pr, ok, err := fg.PR(ctx, req.Repo, headBranch, req.Base)
		if err != nil {
			return 0, classifyForgeErr_internal(err, baseTip, merged.SHA), true, nil
		}
		if !ok {
			pr, err = fg.OpenPR(ctx, req.Repo, headBranch, req.Base, fmt.Sprintf("sprint/%s: land %d card(s)", req.Stream, len(req.Heads)), "Automated stream landing group.")
			if err != nil {
				return 0, classifyForgeErr_internal(err, baseTip, merged.SHA), true, nil
			}
		}
		prNum = pr.Number
	}
	if len(req.Heads) == 1 && (req.PassedSHAs[merged.SHA] || (merged.Tree != "" && req.PassedTrees[merged.Tree])) {
		return prNum, LandResult{ReusedCI: true}, false, nil
	}
	if fg == nil {
		return prNum, LandResult{}, false, nil
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
		return prNum, LandResult{Outcome: OutcomeCIPending, BaseTip: baseTip, GatedSHA: merged.SHA, Reason: r.Reason}, true, nil
	case forge.CheckFault:
		return prNum, LandResult{Outcome: OutcomeCIFault, BaseTip: baseTip, GatedSHA: merged.SHA, CIURL: r.URL, Reason: r.Reason}, true, nil
	default:
		if len(req.Heads) > 1 && req.BisectOnRed {
			bRes, bErr := BisectRedGroup(ctx, g, fg, req, baseTip, streamRef, merged.SHA)
			return prNum, bRes, true, bErr
		}
		blamed := ""
		if len(req.Heads) == 1 {
			blamed = req.Heads[0]
		}
		return prNum, LandResult{Outcome: OutcomeCIRed, BaseTip: baseTip, GatedSHA: merged.SHA, BlamedHead: blamed, FailingCI: r.Failing, CIURL: r.URL, Reason: r.Reason}, true, nil
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
		merged, remRes, done, err = remergeOnMovedBase_internal(ctx, g, fg, req, baseRef, streamRef, curBase, merged.SHA)
		if err != nil || done {
			return remRes, err
		}
		baseTip = curBase
		remerged = true
	}
	if req.RequirePR && fg != nil {
		if err := fg.Merge(ctx, req.Repo, prNum, merged.SHA, req.Base); err != nil {
			if errors.Is(err, forge.ErrStaleHead) || errors.Is(err, forge.ErrBaseMoved) {
				return LandResult{Outcome: OutcomePushRefused, BaseTip: baseTip, GatedSHA: merged.SHA, Remerged: remerged, Reason: err.Error()}, nil
			}
			return classifyForgeErr_internal(err, baseTip, merged.SHA), nil
		}
	} else if err := g.Push(ctx, req.RepoDir, merged.SHA, baseRef, ""); err != nil {
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
	return LandResult{
		Outcome:     OutcomeLanded,
		BaseTip:     newBase,
		StreamTip:   newBase,
		GatedSHA:    merged.SHA,
		LandedHeads: append([]string(nil), req.Heads...),
		Remerged:    remerged,
		ReusedCI:    reusedCI,
	}, nil
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
	if fg == nil {
		return merged, LandResult{Remerged: true}, false, nil
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
		return merged, LandResult{Outcome: OutcomeCIPending, BaseTip: newBase, GatedSHA: merged.SHA, Remerged: true, Reason: r.Reason}, true, nil
	case forge.CheckFault:
		return merged, LandResult{Outcome: OutcomeCIFault, BaseTip: newBase, GatedSHA: merged.SHA, CIURL: r.URL, Remerged: true, Reason: r.Reason}, true, nil
	default:
		return merged, LandResult{Outcome: OutcomeCIRed, BaseTip: newBase, GatedSHA: merged.SHA, FailingCI: r.Failing, CIURL: r.URL, Remerged: true, Reason: r.Reason}, true, nil
	}
}
func BisectRedGroup(ctx context.Context, g Git, fg forge.Forge, req LandRequest, baseTip, streamRef, curStreamSHA string) (LandResult, error) {
	candidates := append([]string(nil), req.Heads...)
	streamSHA := curStreamSHA
	for len(candidates) > 1 {
		mid := len(candidates) / 2
		left, right := candidates[:mid], candidates[mid:]
		state, nextSHA, res, done, err := gateSubset_internal(ctx, g, fg, req, baseTip, streamRef, streamSHA, left)
		if err != nil || done {
			return res, err
		}
		streamSHA = nextSHA
		if state == forge.CheckRed {
			candidates = left
			continue
		}
		if len(right) == 1 {
			rState, rSHA, rRes, rDone, rErr := gateSubset_internal(ctx, g, fg, req, baseTip, streamRef, streamSHA, right)
			if rErr != nil || rDone {
				return rRes, rErr
			}
			streamSHA = rSHA
			if rState != forge.CheckRed {
				return LandResult{Outcome: OutcomeCIRed, BaseTip: baseTip, GatedSHA: streamSHA, Reason: "cross-card red interaction"}, nil
			}
		}
		candidates = right
	}
	blamed := candidates[0]
	var green []string
	for _, h := range req.Heads {
		if h != blamed {
			green = append(green, h)
		}
	}
	return LandResult{
		Outcome:    OutcomeCIRed,
		BaseTip:    baseTip,
		GatedSHA:   streamSHA,
		BlamedHead: blamed,
		GreenHeads: green,
		Reason:     fmt.Sprintf("bisected red batch to head %s", blamed),
	}, nil
}
func gateSubset_internal(ctx context.Context, g Git, fg forge.Forge, req LandRequest, baseTip, streamRef, expectOld string, subset []string) (forge.CheckState, string, LandResult, bool, error) {
	subReq := req
	subReq.Heads = subset
	merged, res, done, err := mergeAndPushStream_internal(ctx, g, subReq, baseTip, streamRef, expectOld)
	if err != nil || done {
		return "", expectOld, res, true, err
	}
	rollups, err := fg.Checks(ctx, req.Repo, []string{merged.SHA})
	if err != nil {
		return "", merged.SHA, classifyForgeErr_internal(err, baseTip, merged.SHA), true, nil
	}
	r := rollups[merged.SHA]
	switch r.State {
	case forge.CheckFault:
		return r.State, merged.SHA, LandResult{Outcome: OutcomeCIFault, BaseTip: baseTip, GatedSHA: merged.SHA, CIURL: r.URL, Reason: r.Reason}, true, nil
	case forge.CheckPending, "":
		return r.State, merged.SHA, LandResult{Outcome: OutcomeCIPending, BaseTip: baseTip, GatedSHA: merged.SHA, Reason: r.Reason}, true, nil
	default:
		return r.State, merged.SHA, LandResult{}, false, nil
	}
}

type RemoteLandResult struct {
	Host           string
	SkippedHosts   []string
	Stdout, Stderr string
	ExitCode       int
}

func RunRemoteLandStep(ctx context.Context, execRunner edge.Exec, hosts []string, connectTimeoutSec int, stepTimeout time.Duration, args ...string) (RemoteLandResult, error) {
	if execRunner == nil {
		execRunner = edge.NewOSExec()
	}
	if connectTimeoutSec <= 0 {
		connectTimeoutSec = DefaultSSHConnectTimeoutSec
	}
	if stepTimeout <= 0 {
		stepTimeout = DefaultRebaseTimeout
	}
	var skipped []string
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" || strings.HasPrefix(host, "-") {
			continue
		}
		argv := append([]string{
			"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=" + strconv.Itoa(connectTimeoutSec),
			"-o", "ControlMaster=no", "-o", "ControlPath=none", host, "nova-sprint", "land-step",
		}, args...)
		res, err := execRunner.Run(ctx, edge.Cmd{Argv: argv, Timeout: stepTimeout})
		if err != nil || res.ExitCode == 255 {
			skipped = append(skipped, host)
			continue
		}
		return RemoteLandResult{Host: host, SkippedHosts: skipped, Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, nil
	}
	return RemoteLandResult{SkippedHosts: skipped}, fmt.Errorf("%w: land refused: no landing host answered (tried: %s): check nova-fleet check", ErrNoLandingHost, strings.Join(skipped, ", "))
}
