package forge

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

type FakeWorld struct {
	mu          sync.Mutex
	nextPR      int
	prs         map[string][]PRInfo
	checks      map[string]map[string]CheckRollup
	baseTips    map[string]map[string]string
	prBaseAtSHA map[string]map[int]string
	authLogin   string
}

func NewFakeWorld(authLogin string) *FakeWorld {
	if authLogin == "" {
		authLogin = "rowan-claude"
	}
	return &FakeWorld{
		nextPR:      1,
		prs:         make(map[string][]PRInfo),
		checks:      make(map[string]map[string]CheckRollup),
		baseTips:    make(map[string]map[string]string),
		prBaseAtSHA: make(map[string]map[int]string),
		authLogin:   authLogin,
	}
}

type Fake struct {
	world    *FakeWorld
	identity Identity
	now      func() int64
	OnCall   func(op string)
}

func NewFake(id Identity, now func() int64) *Fake {
	return NewFakeWithWorld(NewFakeWorld(id.ExpectedLogin), id, now)
}

func NewFakeWithWorld(w *FakeWorld, id Identity, now func() int64) *Fake {
	if now == nil {
		t := edge.FakeClockStart
		now = func() int64 { return t }
	}
	return &Fake{world: w, identity: id, now: now}
}

func (f *Fake) SetAuthLogin(login string) {
	f.world.mu.Lock()
	f.world.authLogin = login
	f.world.mu.Unlock()
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

func (f *Fake) SetBaseTip(repo, base, sha string) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	if f.world.baseTips[repo] == nil {
		f.world.baseTips[repo] = make(map[string]string)
	}
	f.world.baseTips[repo][base] = sha
}

func (f *Fake) SetPRHead(repo string, prNum int, headSHA, gatedBaseTip string) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	for i := range f.world.prs[repo] {
		if f.world.prs[repo][i].Number == prNum {
			f.world.prs[repo][i].HeadSHA = headSHA
		}
	}
	if f.world.prBaseAtSHA[repo] == nil {
		f.world.prBaseAtSHA[repo] = make(map[int]string)
	}
	f.world.prBaseAtSHA[repo][prNum] = gatedBaseTip
}

func (f *Fake) gate_internal(op string) error {
	if f.OnCall != nil {
		f.OnCall(op)
	}
	name := f.identity.Name
	if name == "" {
		name = "unknown"
	}
	if strings.TrimSpace(f.identity.Token) == "" && strings.TrimSpace(f.identity.ConfigDir) == "" {
		return fmt.Errorf("forge refused: neither GH_TOKEN nor GH_CONFIG_DIR is set for identity %s: run nova-secrets exec or set GH_CONFIG_DIR", name)
	}
	f.world.mu.Lock()
	login := f.world.authLogin
	f.world.mu.Unlock()
	want := f.identity.ExpectedLogin
	if want == "" {
		want = name
	}
	if isForbiddenLogin_internal(want) || isForbiddenLogin_internal(login) || (f.identity.ExpectedLogin != "" && !strings.EqualFold(login, f.identity.ExpectedLogin)) {
		return fmt.Errorf("forge refused: gh auth login is %s, expected %s (the human's own credentials are never used): set GH_CONFIG_DIR for %s", login, want, want)
	}
	return nil
}

func (f *Fake) PR(_ context.Context, repo, head, base string) (PRInfo, bool, error) {
	if err := f.gate_internal("PR"); err != nil {
		return PRInfo{}, false, err
	}
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
	pr := PRInfo{Number: num, URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, num), HeadRef: head, BaseRef: base, State: "OPEN"}
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
	if err := f.gate_internal("Checks"); err != nil {
		return nil, err
	}
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	out := make(map[string]CheckRollup, len(shas))
	for _, sha := range shas {
		if r, ok := f.world.checks[repo][sha]; ok {
			out[sha] = r
		} else {
			out[sha] = CheckRollup{SHA: sha, State: CheckPending, Reason: "no check rollup reported yet"}
		}
	}
	return out, nil
}

func (f *Fake) Merge(_ context.Context, repo string, pr int, sha, base string) error {
	if pr <= 0 || !isFullSHA_internal(sha) || base == "" {
		return fmt.Errorf("forge refused: Merge requires pr > 0, 40-hex sha, and base")
	}
	if err := f.gate_internal("Merge"); err != nil {
		return err
	}
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	prs := f.world.prs[repo]
	idx := -1
	for i := range prs {
		if prs[i].Number == pr {
			idx = i
			break
		}
	}
	switch {
	case idx < 0 || prs[idx].State != "OPEN":
		return fmt.Errorf("forge refused: PR #%d is not open on %s", pr, repo)
	case prs[idx].BaseRef != base:
		return fmt.Errorf("%w: PR #%d base is %s, expected %s", ErrBaseMoved, pr, prs[idx].BaseRef, base)
	case prs[idx].HeadSHA != "" && !strings.EqualFold(prs[idx].HeadSHA, sha):
		return fmt.Errorf("%w: PR #%d head is %s, expected gated sha %s", ErrStaleHead, pr, prs[idx].HeadSHA, sha)
	}
	if gatedBase, curBase := f.world.prBaseAtSHA[repo][pr], f.world.baseTips[repo][base]; gatedBase != "" && curBase != "" && gatedBase != curBase {
		return fmt.Errorf("%w: base %s moved from %s to %s since gate", ErrBaseMoved, base, gatedBase, curBase)
	}
	prs[idx].State = "MERGED"
	prs[idx].HeadSHA = sha
	if f.world.baseTips[repo] == nil {
		f.world.baseTips[repo] = make(map[string]string)
	}
	f.world.baseTips[repo][base] = sha
	return nil
}
