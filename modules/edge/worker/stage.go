package worker

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
)

// Staging fetches shallow (Glenn, 2026-11-08: "We should be cloning shallow
// not deep"). A pass fetches, per repo, only what its jobs check out: the
// commits their receipts name (base, prior head, head) and the tips of the
// branches they name (the stream's base, the card's branch), each at depth
// 1, never every branch. The bench's mirror is the one repo that reaches the
// clone URL; the worker's work clone borrows its objects (alternates) and
// fetches the same commits from it, locally. Where a job's head and its base
// share no commit in what was fetched (a card branch forked below the
// shallow boundary), both are deepened (shallowDeepens), then unshallowed,
// until they do: the rebase onto the stream and the reader's diff need that
// merge base, and the job's sandbox has no network to fetch it later.

// shallowDeepens are the --deepen steps tried, in turn, before --unshallow,
// while a job's head and its base share no fetched commit.
var shallowDeepens = []int{64, 1024}

// stageHop_internal is one repo staging fetches into: dir (its git dir is
// gitDir) from src, its branches under refPrefix; remote when src is the
// clone URL (the one credential), else a local path.
type stageHop_internal struct {
	dir, gitDir, src, refPrefix string
	remote                      bool
}

// stageHops_internal is the mirror (when the worker has one) then the work
// clone, each fetching from the one before it, the first from url.
func (g *OSGitOps) stageHops_internal(url, name, work string) []stageHop_internal {
	if g.MirrorRoot == "" {
		return []stageHop_internal{{dir: work, gitDir: filepath.Join(work, ".git"), src: url, refPrefix: "refs/remotes/origin/", remote: true}}
	}
	mirror := filepath.Join(g.MirrorRoot, name+".git")
	return []stageHop_internal{
		{dir: mirror, gitDir: mirror, src: url, refPrefix: "refs/heads/", remote: true},
		{dir: work, gitDir: filepath.Join(work, ".git"), src: mirror, refPrefix: "refs/remotes/origin/"},
	}
}

func (g *OSGitOps) hopGit_internal(ctx context.Context, h stageHop_internal, args ...string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if h.remote {
		return g.runRemoteGit_internal(ctx, to, h.dir, args...)
	}
	return g.runGit_internal(ctx, to, h.dir, args...)
}

// stageWants_internal is what js check out: the commits their receipts name
// and the branches whose tips they need, each once.
func stageWants_internal(js []Job) (shas, branches []string) {
	for _, j := range js {
		for _, s := range []string{j.BaseSHA, j.PriorHead, j.Head} {
			if s != "" && !slices.Contains(shas, s) {
				shas = append(shas, s)
			}
		}
		for _, b := range []string{j.Base, j.Branch} {
			if b != "" && !slices.Contains(branches, b) {
				branches = append(branches, b)
			}
		}
	}
	return shas, branches
}

// branchTips_internal is one ls-remote of branches on url: the tip of each
// that exists, by name.
func (g *OSGitOps) branchTips_internal(ctx context.Context, h stageHop_internal, branches []string) (map[string]string, error) {
	tips := map[string]string{}
	if len(branches) == 0 {
		return tips, nil
	}
	args := []string{"ls-remote", "--quiet", h.src}
	for _, b := range branches {
		args = append(args, "refs/heads/"+b)
	}
	out, err := g.hopGit_internal(ctx, h, args...)
	if err != nil {
		return nil, fmt.Errorf("list the branches %v: %w", branches, err)
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		sha, ref, ok := strings.Cut(sc.Text(), "\t")
		if b, isHead := strings.CutPrefix(ref, "refs/heads/"); ok && isHead && slices.Contains(branches, b) {
			tips[b] = strings.TrimSpace(sha)
		}
	}
	return tips, sc.Err()
}

// hopShallow_internal: h's repo is shallow, or has no ref yet (made by
// staging, never filled), so its next fetch is shallow too. A deep clone
// from before shallow staging stays deep while every hop before it is.
func (g *OSGitOps) hopShallow_internal(ctx context.Context, h stageHop_internal) bool {
	if fileExists_internal(filepath.Join(h.gitDir, "shallow")) {
		return true
	}
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), h.dir, "for-each-ref", "--count=1", "--format=%(refname)")
	return err != nil || strings.TrimSpace(out) == ""
}

// fetchHops_internal fetches shas into each hop, at depth 1 from the first
// shallow hop on (a shallow source can feed only a shallow fetch), then
// points each hop's branch refs at tips; a branch absent from the remote is
// removed, so BranchTip reads "".
func (g *OSGitOps) fetchHops_internal(ctx context.Context, hops []stageHop_internal, shas []string, branches []string, tips map[string]string) error {
	shallow := false
	for _, h := range hops {
		shallow = shallow || g.hopShallow_internal(ctx, h)
		args := []string{"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-auto-gc"}
		if shallow {
			args = append(args, "--depth=1")
		}
		if _, err := g.hopGit_internal(ctx, h, append(append(args, h.src), shas...)...); err != nil {
			return err
		}
		for _, b := range branches {
			upd := []string{"update-ref", "-d", h.refPrefix + b}
			if tips[b] != "" {
				upd = []string{"update-ref", h.refPrefix + b, tips[b]}
			}
			if _, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), h.dir, upd...); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergePairs_internal are the (base, tip) pairs that must share a commit in
// the work clone: each job's base with its prior head, its head and its
// stream's tip, so the rebase onto the stream and a reader's diff find it. A
// job with no base sha (a read) has its stream's tip as its base.
func mergePairs_internal(js []Job, tips map[string]string) [][2]string {
	var out [][2]string
	for _, j := range js {
		base := cmp.Or(j.BaseSHA, tips[j.Base])
		for _, t := range []string{j.PriorHead, j.Head, tips[j.Base]} {
			if p := [2]string{base, t}; base != "" && t != "" && t != base && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// missingMergeBase_internal: some pair shares no commit in dir's history.
func (g *OSGitOps) missingMergeBase_internal(ctx context.Context, dir string, pairs [][2]string) bool {
	for _, p := range pairs {
		if _, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), dir, "merge-base", p[0], p[1]); err != nil {
			return true
		}
	}
	return false
}

// deepenHops_internal deepens the shallow hops, in turn, until every pair
// shares a commit in the work clone (the last hop): --deepen by each of
// shallowDeepens, then --unshallow. Pairs that share none once nothing is
// shallow are left to the step that needs them, which says so.
func (g *OSGitOps) deepenHops_internal(ctx context.Context, hops []stageHop_internal, shas []string, pairs [][2]string) error {
	work := hops[len(hops)-1].dir
	var steps []string
	for _, n := range shallowDeepens {
		steps = append(steps, "--deepen="+strconv.Itoa(n))
	}
	for _, step := range append(steps, "--unshallow") {
		if !g.missingMergeBase_internal(ctx, work, pairs) {
			return nil
		}
		for _, h := range hops {
			if !fileExists_internal(filepath.Join(h.gitDir, "shallow")) {
				continue
			}
			args := append([]string{"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-auto-gc", step, h.src}, shas...)
			if _, err := g.hopGit_internal(ctx, h, args...); err != nil {
				return fmt.Errorf("deepen for a merge base (%s): %w", step, err)
			}
		}
	}
	return nil
}

// fetchShallow_internal is Fetch's body for js, under url's write lock and
// the mirror's lock.
func (g *OSGitOps) fetchShallow_internal(ctx context.Context, url, name, work string, js []Job) error {
	hops := g.stageHops_internal(url, name, work)
	shas, branches := stageWants_internal(js)
	tips, err := g.branchTips_internal(ctx, hops[0], branches)
	if err != nil {
		return err
	}
	for _, b := range branches {
		if t := tips[b]; t != "" && !slices.Contains(shas, t) {
			shas = append(shas, t)
		}
	}
	if len(shas) == 0 {
		return nil
	}
	if err := g.fetchHops_internal(ctx, hops, shas, branches, tips); err != nil {
		return err
	}
	return g.deepenHops_internal(ctx, hops, shas, mergePairs_internal(js, tips))
}

// lockMirror_internal is the bench-wide lock on name's mirror (several
// workers on a bench share it: concurrent fetches in one repo fail "cannot
// lock ref"); a no-op unlock when the worker has no mirror.
func (g *OSGitOps) lockMirror_internal(ctx context.Context, name string) (func() error, error) {
	if g.MirrorRoot == "" {
		return func() error { return nil }, nil
	}
	f, err := git.LockClone(ctx, filepath.Join(g.MirrorRoot, name+".git"))
	if err != nil {
		return nil, err
	}
	return f.Close, nil
}

// sharedClone_internal makes dest a clone of the work clone sharing its
// objects, as clone --shared would: an empty repo whose alternates are the
// work clone's objects and the work clone's own alternates, with the work
// clone's shallow boundary (git clone --shared of a shallow repo copies
// the objects its branches reach instead, and only those) and the work
// clone as origin. Nothing is checked out.
func (g *OSGitOps) sharedClone_internal(ctx context.Context, to time.Duration, work, dest string) error {
	if _, err := g.runGit_internal(ctx, to, "", "init", "--quiet", dest); err != nil {
		return err
	}
	absWork, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	alt := []byte(filepath.Join(absWork, ".git", "objects") + "\n")
	if more, err := os.ReadFile(filepath.Join(work, ".git", "objects", "info", "alternates")); err == nil {
		alt = append(alt, more...)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read the work clone's alternates: %w", err)
	}
	if err := writeGitFile_internal(filepath.Join(dest, ".git", "objects", "info", "alternates"), alt); err != nil {
		return err
	}
	if shallow, err := os.ReadFile(filepath.Join(work, ".git", "shallow")); err == nil {
		if err := writeGitFile_internal(filepath.Join(dest, ".git", "shallow"), shallow); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read the work clone's shallow boundary: %w", err)
	}
	_, err = g.runGit_internal(ctx, to, dest, "remote", "add", "origin", absWork)
	return err
}

func writeGitFile_internal(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
