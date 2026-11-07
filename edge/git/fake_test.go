package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"slices"
	"strings"
	"sync"
)

type fakeForgeWorld_internal struct {
	mu     sync.Mutex
	checks map[string]forge.CheckRollup
}

func newFakeForgeWorld_internal() *fakeForgeWorld_internal {
	return &fakeForgeWorld_internal{checks: make(map[string]forge.CheckRollup)}
}

type fakeForge_internal struct {
	world  *fakeForgeWorld_internal
	OnCall func(op string)
}

func newFakeForge_internal() *fakeForge_internal {
	return newFakeForgeWithWorld_internal(newFakeForgeWorld_internal())
}

func newFakeForgeWithWorld_internal(w *fakeForgeWorld_internal) *fakeForge_internal {
	return &fakeForge_internal{world: w}
}

func (f *fakeForge_internal) SetCheck(repo, sha string, r forge.CheckRollup) {
	f.world.mu.Lock()
	r.SHA = sha
	f.world.checks[repo+"\x00"+sha] = r
	f.world.mu.Unlock()
}

func (f *fakeForge_internal) PR(_ context.Context, _, head, base string) (forge.PRInfo, bool, error) {
	if f.OnCall != nil {
		f.OnCall("PR")
	}
	return forge.PRInfo{Number: 1, HeadRef: head, BaseRef: base, State: "OPEN"}, true, nil
}

func (f *fakeForge_internal) OpenPR(_ context.Context, _, head, base, _, _ string) (forge.PRInfo, error) {
	if f.OnCall != nil {
		f.OnCall("OpenPR")
	}
	return forge.PRInfo{Number: 1, HeadRef: head, BaseRef: base, State: "OPEN"}, nil
}

func (f *fakeForge_internal) Checks(_ context.Context, repo string, shas []string) (map[string]forge.CheckRollup, error) {
	if f.OnCall != nil {
		f.OnCall("Checks")
	}
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	out := make(map[string]forge.CheckRollup, len(shas))
	for _, sha := range shas {
		if r, ok := f.world.checks[repo+"\x00"+sha]; ok {
			out[sha] = r
		} else {
			out[sha] = forge.CheckRollup{SHA: sha, State: forge.CheckPending}
		}
	}
	return out, nil
}

func (f *fakeForge_internal) Merge(_ context.Context, _ string, _ int, _, _ string) error {
	if f.OnCall != nil {
		f.OnCall("Merge")
	}
	return nil
}

type FakeCommit struct {
	SHA     string
	Parents []string
	Files   map[string]string
	Tree    string
}
type FakeWorld struct {
	mu           sync.Mutex
	seq          int
	commits      map[string]FakeCommit
	originRefs   map[string]string
	fetched      map[string]bool
	ledgerScript map[string]func(files map[string]string) (string, error)
}

func NewFakeWorld() *FakeWorld {
	return &FakeWorld{
		commits:      make(map[string]FakeCommit),
		originRefs:   make(map[string]string),
		fetched:      make(map[string]bool),
		ledgerScript: make(map[string]func(files map[string]string) (string, error)),
	}
}

type Fake struct {
	world *FakeWorld
	now   func() int64
}

func NewFake(now func() int64) *Fake { return NewFakeWithWorld(NewFakeWorld(), now) }
func NewFakeWithWorld(w *FakeWorld, now func() int64) *Fake {
	if now == nil {
		t := edge.FakeClockStart
		now = func() int64 { return t }
	}
	return &Fake{world: w, now: now}
}
func (f *Fake) World() *FakeWorld { return f.world }
func (f *Fake) AddCommit(parent string, changes map[string]string, originRef string) string {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	f.world.seq++
	sha := fmt.Sprintf("%040x", f.world.seq)
	files := make(map[string]string)
	var parents []string
	if parent != "" {
		parents = []string{parent}
		if p, ok := f.world.commits[parent]; ok {
			for k, v := range p.Files {
				files[k] = v
			}
		}
	}
	for k, v := range changes {
		files[k] = v
	}
	tree := computeTreeHash_internal(files)
	f.world.commits[sha] = FakeCommit{SHA: sha, Parents: parents, Files: files, Tree: tree}
	if originRef != "" {
		if !strings.HasPrefix(originRef, "refs/heads/") {
			originRef = "refs/heads/" + originRef
		}
		f.world.originRefs[originRef] = sha
	}
	f.world.originRefs[sha] = sha
	return sha
}
func (f *Fake) SetOriginRef(ref, sha string) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	if !strings.HasPrefix(ref, "refs/heads/") && !isFullSHA_internal(ref) {
		ref = "refs/heads/" + ref
	}
	f.world.originRefs[ref] = sha
}
func (f *Fake) OriginRef(ref string) string {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	if !strings.HasPrefix(ref, "refs/heads/") && !isFullSHA_internal(ref) {
		ref = "refs/heads/" + ref
	}
	return f.world.originRefs[ref]
}
func (f *Fake) RemoveFromOrigin(ref string) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	delete(f.world.originRefs, ref)
}
func (f *Fake) SetLedgerScript(path string, fn func(files map[string]string) (string, error)) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	f.world.ledgerScript[path] = fn
}
func computeTreeHash_internal(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var h uint64 = 14695981039346656037
	for _, k := range keys {
		for _, s := range []string{k, "=", files[k], "\n"} {
			for i := 0; i < len(s); i++ {
				h ^= uint64(s[i])
				h *= 1099511628211
			}
		}
	}
	return fmt.Sprintf("tree-%016x", h)
}
func (f *Fake) Fetch(_ context.Context, _ string, refs ...string) error {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	for _, r := range refs {
		if r == "" {
			continue
		}
		key := r
		if !strings.HasPrefix(key, "refs/heads/") && !isFullSHA_internal(key) {
			key = "refs/heads/" + key
		}
		sha, ok := f.world.originRefs[key]
		if !ok {
			return fmt.Errorf("%w: %s", ErrMissingRef, r)
		}
		f.world.fetched[sha] = true
	}
	return nil
}
func (f *Fake) LsRemote(_ context.Context, _, ref string) (string, bool, error) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	key := ref
	if !strings.HasPrefix(key, "refs/heads/") && !isFullSHA_internal(key) {
		key = "refs/heads/" + key
	}
	sha, ok := f.world.originRefs[key]
	return sha, ok, nil
}
func (f *Fake) IsAncestor(_ context.Context, _, ancestor, descendant string) (bool, error) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	return f.isAncestorLocked_internal(ancestor, descendant), nil
}
func (f *Fake) isAncestorLocked_internal(ancestor, descendant string) bool {
	switch {
	case ancestor == "" || descendant == "":
		return false
	case ancestor == descendant:
		return true
	}
	seen := map[string]bool{descendant: true}
	queue := []string{descendant}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, p := range f.world.commits[cur].Parents {
			if p == ancestor {
				return true
			}
			if !seen[p] {
				seen[p] = true
				queue = append(queue, p)
			}
		}
	}
	return false
}
func (f *Fake) TreeHash(_ context.Context, _, rev string) (string, error) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	c, ok := f.world.commits[rev]
	if !ok {
		return "", fmt.Errorf("unknown commit %s", rev)
	}
	return c.Tree, nil
}
func (f *Fake) MergeGroup(_ context.Context, _, onto string, heads []string, ledgers []LedgerRule) (MergeResult, error) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	baseCommit, ok := f.world.commits[onto]
	if !ok {
		return MergeResult{}, fmt.Errorf("unknown onto commit %s", onto)
	}
	if len(heads) == 1 && f.isAncestorLocked_internal(onto, heads[0]) {
		hc := f.world.commits[heads[0]]
		return MergeResult{Kind: MergeOK, SHA: hc.SHA, Tree: hc.Tree, ReusedHead: true}, nil
	}
	ledgerMap := make(map[string]LedgerRule, len(ledgers))
	for _, l := range ledgers {
		ledgerMap[l.Path] = l
	}
	cur := baseCommit
	for _, head := range heads {
		hc, exists := f.world.commits[head]
		if !exists {
			return MergeResult{}, fmt.Errorf("unknown head commit %s", head)
		}
		next, res, done := f.mergeTwoLocked_internal(cur, hc, ledgerMap)
		if done {
			return res, nil
		}
		cur = next
	}
	return MergeResult{Kind: MergeOK, SHA: cur.SHA, Tree: cur.Tree}, nil
}
func (f *Fake) mergeTwoLocked_internal(ours, theirs FakeCommit, ledgerMap map[string]LedgerRule) (FakeCommit, MergeResult, bool) {
	anc := f.commonAncestorFilesLocked_internal(ours.SHA, theirs.SHA)
	mergedFiles := make(map[string]string, len(ours.Files))
	for k, v := range ours.Files {
		mergedFiles[k] = v
	}
	var nonLedger, ledgerConflicts []string
	for path, theirVal := range theirs.Files {
		baseVal := anc[path]
		ourVal, ourHas := ours.Files[path]
		if theirVal == baseVal || (ourHas && ourVal == theirVal) {
			continue
		}
		if !ourHas || ourVal == baseVal {
			mergedFiles[path] = theirVal
			continue
		}
		if _, isLedger := ledgerMap[path]; isLedger {
			ledgerConflicts = append(ledgerConflicts, path)
		} else {
			nonLedger = append(nonLedger, path)
		}
	}
	if len(nonLedger) > 0 {
		slices.Sort(nonLedger)
		return FakeCommit{}, MergeResult{Kind: MergeConflict, Conflicts: nonLedger, Reason: fmt.Sprintf("merge conflict on %s: %s", theirs.SHA, strings.Join(nonLedger, ", "))}, true
	}
	for _, p := range ledgerConflicts {
		if fn, ok := f.world.ledgerScript[p]; ok {
			val, err := fn(mergedFiles)
			if err != nil {
				slices.Sort(ledgerConflicts)
				return FakeCommit{}, MergeResult{Kind: MergeLedgerFailed, Conflicts: ledgerConflicts, Reason: err.Error()}, true
			}
			mergedFiles[p] = val
		} else {
			mergedFiles[p] = unionLines_internal(ours.Files[p], theirs.Files[p])
		}
	}
	tree := computeTreeHash_internal(mergedFiles)
	sum := sha256.Sum256([]byte("merge\x00" + ours.SHA + "\x00" + theirs.SHA + "\x00" + tree))
	sha := hex.EncodeToString(sum[:])[:40]
	c := FakeCommit{SHA: sha, Parents: []string{ours.SHA, theirs.SHA}, Files: mergedFiles, Tree: tree}
	f.world.commits[sha] = c
	f.world.originRefs[sha] = sha
	return c, MergeResult{}, false
}
func (f *Fake) commonAncestorFilesLocked_internal(a, b string) map[string]string {
	bestSHA := ""
	var best map[string]string
	for sha, c := range f.world.commits {
		if sha > bestSHA && f.isAncestorLocked_internal(sha, a) && f.isAncestorLocked_internal(sha, b) {
			bestSHA = sha
			best = c.Files
		}
	}
	if best == nil {
		return map[string]string{}
	}
	return best
}
func (f *Fake) Rebase(_ context.Context, worktree, onto string) (RebaseResult, error) {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	ours, ok1 := f.world.commits[onto]
	theirs, ok2 := f.world.commits[worktree]
	if !ok1 || !ok2 {
		return RebaseResult{}, fmt.Errorf("unknown rebase commits onto=%s head=%s", onto, worktree)
	}
	next, res, conflict := f.mergeTwoLocked_internal(ours, theirs, nil)
	if conflict {
		return RebaseResult{OK: false, Conflicts: res.Conflicts, Reason: fmt.Sprintf("rebase conflict onto %s: %s", onto, strings.Join(res.Conflicts, ", "))}, nil
	}
	next.Parents = []string{onto}
	f.world.commits[next.SHA] = next
	return RebaseResult{OK: true, SHA: next.SHA}, nil
}
func (f *Fake) Push(_ context.Context, _, sha, ref, expectOld string) error {
	f.world.mu.Lock()
	defer f.world.mu.Unlock()
	var isStream bool
	var err error
	ref, isStream, err = classifyPushRef_internal(ref)
	if err != nil {
		return err
	}
	if !isStream && expectOld != "" && !f.isAncestorLocked_internal(expectOld, sha) {
		return fmt.Errorf("git refused: force-with-lease is forbidden on base branch %s: base pushes must fast-forward", ref)
	}
	cur := f.world.originRefs[ref]
	if isStream || expectOld != "" {
		if cur != expectOld {
			return &PushRefusedError{Ref: ref, SHA: sha, Reason: fmt.Sprintf("stale lease on %s: got %s, expected %s", ref, cur, expectOld)}
		}
		f.world.originRefs[ref] = sha
		return nil
	}
	if cur != "" && !f.isAncestorLocked_internal(cur, sha) {
		return &PushRefusedError{Ref: ref, SHA: sha, Reason: fmt.Sprintf("non-fast-forward push to base %s", ref)}
	}
	f.world.originRefs[ref] = sha
	return nil
}
