package forge

import (
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"strings"
	"sync"
)

type FakeWorld struct {
	mu          sync.Mutex
	nextPR      int
	prs         map[string][]PRInfo
	checks      map[string]map[string]CheckRollup
	baseTips    map[string]map[string]string
	prBaseAtSHA map[string]map[int]string
	strictBases map[string]map[string]bool
	authLogin   string
	rateLimit   int64
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
		strictBases: make(map[string]map[string]bool),
		authLogin:   authLogin,
	}
}

type Fake struct {
	world    *FakeWorld
	identity Identity
	now      func() int64
	mu       sync.Mutex
	restTill int64
	dedup    *edge.Deduplicator
	OnCall   func(op string)
}

func NewFake(id Identity, now func() int64) *Fake {
	login := id.ExpectedLogin
	if login == "" {
		login = "rowan-claude"
	}
	return NewFakeWithWorld(NewFakeWorld(login), id, now)
}
func NewFakeWithWorld(w *FakeWorld, id Identity, now func() int64) *Fake {
	if now == nil {
		t := edge.FakeClockStart
		now = func() int64 { return t }
	}
	return &Fake{world: w, identity: id, now: now, dedup: edge.NewDeduplicator()}
}
func (f *Fake) World() *FakeWorld  { return f.world }
func (f *Fake) RestedUntil() int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.restTill }
func (f *Fake) SetAuthLogin(login string) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	f.world.authLogin = login
}
func (f *Fake) SimulateRateLimit(retrySec int64) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	f.world.rateLimit = retrySec
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
	prs := f.world.prs[repo]
	for i := range prs {
		if prs[i].Number == prNum {
			prs[i].HeadSHA = headSHA
		}
	}
	if f.world.prBaseAtSHA[repo] == nil {
		f.world.prBaseAtSHA[repo] = make(map[int]string)
	}
	f.world.prBaseAtSHA[repo][prNum] = gatedBaseTip
}
func (f *Fake) SetBaseProtection(repo, base string, strict bool) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	if f.world.strictBases[repo] == nil {
		f.world.strictBases[repo] = make(map[string]bool)
	}
	f.world.strictBases[repo][base] = strict
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
	retrySec := f.world.rateLimit
	f.world.rateLimit = 0
	f.world.mu.Unlock()
	want := f.identity.ExpectedLogin
	if want == "" {
		want = name
	}
	if isForbiddenLogin_internal(want) || isForbiddenLogin_internal(login) || (f.identity.ExpectedLogin != "" && !strings.EqualFold(login, f.identity.ExpectedLogin)) {
		return fmt.Errorf("forge refused: gh auth login is %s, expected %s (Glenn's credentials are never used): set GH_CONFIG_DIR for %s", login, want, want)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	if now < f.restTill {
		return &FaultError{Kind: FaultRateLimit, Identity: name, RetryAfter: f.restTill, Reason: "identity is resting after rate limit"}
	}
	if retrySec > 0 {
		f.restTill = now + retrySec
		return &FaultError{Kind: FaultRateLimit, Identity: name, RetryAfter: f.restTill, Reason: "HTTP 429 rate limit exceeded"}
	}
	return nil
}
func (f *Fake) Query(ctx context.Context, repo string, req edge.EdgeRequest) (edge.EdgeResult, error) {
	return f.dedup.Do(ctx, req, func(ctx context.Context, r edge.EdgeRequest) (edge.EdgeResult, error) {
		out := edge.EdgeResult{ID: r.ID, Kind: r.Kind, Card: r.Card, SHA: r.SHA}
		m, err := f.Checks(ctx, repo, []string{r.SHA})
		if err != nil {
			var fe *FaultError
			if errors.As(err, &fe) {
				out.Outcome = string(CheckFault)
				out.Failure = string(fe.Kind)
				out.Reason = fe.Error()
				return out, nil
			}
			return out, err
		}
		cr := m[r.SHA]
		out.Outcome = string(cr.State)
		out.Reason = cr.Reason
		return out, nil
	})
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
		return nil, fmt.Errorf("forge refused: Checks batch size %d exceeds %d SHAs: split into batches of at most %d", len(shas), MaxChecksBatch, MaxChecksBatch)
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
		return fmt.Errorf("%w: PR #%d base is %s, expected %s: re-target PR to %s", ErrBaseMoved, pr, prs[idx].BaseRef, base, base)
	case prs[idx].HeadSHA != "" && !strings.EqualFold(prs[idx].HeadSHA, sha):
		return fmt.Errorf("%w: PR #%d head is %s, expected gated sha %s: re-gate before merge", ErrStaleHead, pr, prs[idx].HeadSHA, sha)
	}
	gatedBase := f.world.prBaseAtSHA[repo][pr]
	curBase := f.world.baseTips[repo][base]
	if gatedBase != "" && curBase != "" && gatedBase != curBase {
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
func (f *Fake) CheckBaseProtection(_ context.Context, repo, base string) error {
	if err := f.gate_internal("CheckBaseProtection"); err != nil {
		return err
	}
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	if !f.world.strictBases[repo][base] {
		return fmt.Errorf("SPRINT REFUSED stream add: base branch %s on %s does not require branches to be up to date before merging: enable strict status checks on %s/%s", base, repo, repo, base)
	}
	return nil
}
