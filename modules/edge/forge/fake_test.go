package forge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// FakeWorld is GitHub as the fake forge sees it: PRs, check rollups, and
// each PR's head sha.
type FakeWorld struct {
	mu     sync.Mutex
	nextPR int
	prs    map[string][]PRInfo
	checks map[string]map[string]CheckRollup
	lines  map[string][]string
}

func NewFakeWorld() *FakeWorld {
	return &FakeWorld{nextPR: 1, prs: map[string][]PRInfo{}, checks: map[string]map[string]CheckRollup{}, lines: map[string][]string{}}
}

// Fake is Forge over a FakeWorld.
type Fake struct {
	world  *FakeWorld
	now    func() int64
	OnCall func(op string)
}

func NewFake(now func() int64) *Fake { return NewFakeWithWorld(NewFakeWorld(), now) }

func NewFakeWithWorld(w *FakeWorld, now func() int64) *Fake {
	if now == nil {
		t := edge.FakeClockStart
		now = func() int64 { return t }
	}
	return &Fake{world: w, now: now}
}

func (f *Fake) call_internal(op string) {
	if f.OnCall != nil {
		f.OnCall(op)
	}
}

func (f *Fake) SetCheck(repo, sha string, rollup CheckRollup) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	if f.world.checks[repo] == nil {
		f.world.checks[repo] = make(map[string]CheckRollup)
	}
	rollup.SHA = sha
	f.world.checks[repo][sha] = rollup
}

// SetPRHead sets PR prNum's head sha (the branch moved).
func (f *Fake) SetPRHead(repo string, prNum int, headSHA string) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	for i := range f.world.prs[repo] {
		if f.world.prs[repo][i].Number == prNum {
			f.world.prs[repo][i].HeadSHA = headSHA
		}
	}
}

func (f *Fake) PR(_ context.Context, repo, head, base string) (PRInfo, bool, error) {
	f.call_internal("PR")
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	for _, pr := range f.world.prs[repo] {
		if pr.State == "OPEN" && pr.HeadRef == head && pr.BaseRef == base {
			return pr, true, nil
		}
	}
	return PRInfo{}, false, nil
}

func (f *Fake) OpenPR(ctx context.Context, repo, head, base, title, _ string) (PRInfo, error) {
	if repo == "" || head == "" || base == "" || title == "" {
		return PRInfo{}, fmt.Errorf("forge refused: OpenPR requires repo, head, base, and title")
	}
	if existing, ok, err := f.PR(ctx, repo, head, base); err != nil || ok {
		return existing, err
	}
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	num := f.world.nextPR
	f.world.nextPR++
	pr := PRInfo{Number: num, URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, num), HeadRef: head, BaseRef: base, State: "OPEN", Mergeable: "MERGEABLE"}
	f.world.prs[repo] = append(f.world.prs[repo], pr)
	return pr, nil
}

func (f *Fake) Checks(_ context.Context, repo string, shas []string) (map[string]CheckRollup, error) {
	if len(shas) > MaxChecksBatch {
		return nil, fmt.Errorf("forge refused: Checks batch size %d exceeds %d SHAs", len(shas), MaxChecksBatch)
	}
	for _, sha := range shas {
		if !isFullSHA_internal(sha) {
			return nil, fmt.Errorf("forge refused: invalid SHA %q: pass a 40-character hex commit SHA", sha)
		}
	}
	f.call_internal("Checks")
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	out := make(map[string]CheckRollup, len(shas))
	for _, sha := range shas {
		if r, ok := f.world.checks[repo][sha]; ok {
			out[sha] = r
		} else {
			out[sha] = CheckRollup{SHA: sha, State: CheckAbsent, Reason: "no check has been reported for this commit"}
		}
	}
	return out, nil
}

// Merge merges an open PR whose head is sha and returns a merge sha.
func (f *Fake) Merge(_ context.Context, repo string, pr int, sha string) (string, error) {
	if pr <= 0 || !isFullSHA_internal(sha) {
		return "", fmt.Errorf("forge refused: Merge requires pr > 0 and a 40-hex sha")
	}
	f.call_internal("Merge")
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	for i := range f.world.prs[repo] {
		p := &f.world.prs[repo][i]
		if p.Number != pr {
			continue
		}
		switch {
		case p.State != "OPEN":
			return "", fmt.Errorf("forge refused: PR #%d is not open on %s", pr, repo)
		case p.HeadSHA != "" && !strings.EqualFold(p.HeadSHA, sha):
			return "", fmt.Errorf("forge: PR #%d head is %s, expected gated sha %s", pr, p.HeadSHA, sha)
		}
		p.State = "MERGED"
		sum := sha256.Sum256([]byte("merged\x00" + sha))
		return hex.EncodeToString(sum[:])[:40], nil
	}
	return "", fmt.Errorf("forge refused: no PR #%d on %s", pr, repo)
}

func (f *Fake) SetLines(url string, lines []string) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	f.world.lines[url] = lines
}

func (f *Fake) FailedLines(_ context.Context, _, url string) ([]string, error) {
	f.call_internal("FailedLines")
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	if l, ok := f.world.lines[url]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("forge: %s names no Actions job, so its log cannot be read", url)
}

// Inheritance is every failing check of head's rollup as new red: the fake
// has no base runs.
func (f *Fake) Inheritance(_ context.Context, repo, _, baseTip, head string) (RedSplit, error) {
	f.call_internal("Inheritance")
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	split := RedSplit{MergeBase: baseTip}
	for _, name := range f.world.checks[repo][head].Failing {
		split.New = append(split.New, WorkflowRed{Workflow: name, Basis: "the merge base " + short(baseTip)})
	}
	return split, nil
}

// Authors is one fixed author.
func (f *Fake) Authors(_ context.Context, _ string, _ int) ([]string, error) {
	f.call_internal("Authors")
	return []string{"A Writer <a@example.invalid> (a-writer)"}, nil
}

var _ Forge = (*Fake)(nil)
