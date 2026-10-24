package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/subproc"
)

// The binding of the deal's preconditions on BASE (internal/sprint/deal_base.go;
// tla/DealBase.tla): the run loop's tick asks for the BASE of every ready card
// (store.Store.DealFacts) and is answered at once from what this reader holds; the reader
// reads the bases asked for off the tick's path, every dealFactsEvery:
//
//   - the CI: the newest push commit on the base whose workflow runs all completed (the
//     forge's actions API through gh), and of its failed runs the failing jobs, each with the
//     platforms it runs on (sprint.CheckPlatforms, from the workflow, the job and its runner
//     labels). A completed run's jobs are read once.
//   - the tree: the base's tip in a blob-less bare mirror of its own under the land root
//     (dealfacts/<repo>.git), never the lander's clones, so no fetch of this reader's
//     contends with a landing: every path at the tip (git ls-tree), read once a tip.
//
// A read that fails keeps the last fact of that kind for the base and says so on the
// server's log; a tree is only ever the tip's, never an older one (a file the tip added
// is never missing).
const (
	dealFactsEvery  = time.Minute
	dealFactsForget = 10 * time.Minute // a base no tick asked for in this long is read no more
	dealFactsRuns   = 60               // the push runs read per base, newest first
)

type dealFactsReader struct {
	mu    sync.Mutex
	asked map[sprint.RepoBase]time.Time // when a tick last asked for the base
	read  map[sprint.RepoBase]time.Time // when it was last read
	ci    map[sprint.RepoBase]sprint.BaseCIFacts
	tree  map[sprint.RepoBase]sprint.BaseTree
	jobs  map[string][]sprint.BaseCheck // a completed run's failing jobs, by run id and conclusion
	// the edges, a test's fakes: gh (the forge), git (in a directory), the mirror's
	// directory for a repository, and the server's log line
	gh     func(ctx context.Context, args ...string) (string, error)
	git    func(ctx context.Context, dir string, args ...string) (string, error)
	mirror func(repo string) (string, error)
	say    func(format string, args ...any)
}

func newDealFactsReader() *dealFactsReader {
	return &dealFactsReader{asked: map[sprint.RepoBase]time.Time{}, read: map[sprint.RepoBase]time.Time{},
		ci: map[sprint.RepoBase]sprint.BaseCIFacts{}, tree: map[sprint.RepoBase]sprint.BaseTree{}, jobs: map[string][]sprint.BaseCheck{}}
}

// facts is the store's DealFacts at now: the facts held for the bases asked for, which are
// recorded for the next read. It never reads.
func (d *dealFactsReader) facts(now time.Time, bases []sprint.RepoBase) ([]sprint.BaseCIFacts, []sprint.BaseTree) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ci []sprint.BaseCIFacts
	var trees []sprint.BaseTree
	for _, b := range bases {
		d.asked[b] = now
		if f, ok := d.ci[b]; ok {
			ci = append(ci, f)
		}
		if t, ok := d.tree[b]; ok {
			trees = append(trees, t)
		}
	}
	return ci, trees
}

// due is the bases to read at now: asked within dealFactsForget and not read within
// dealFactsEvery. A base asked for no more is forgotten, its facts with it.
func (d *dealFactsReader) due(now time.Time) []sprint.RepoBase {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []sprint.RepoBase
	for b, at := range d.asked {
		if now.Sub(at) > dealFactsForget {
			delete(d.asked, b)
			delete(d.read, b)
			delete(d.ci, b)
			delete(d.tree, b)
			continue
		}
		if r, ok := d.read[b]; !ok || now.Sub(r) >= dealFactsEvery {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b sprint.RepoBase) int { return strings.Compare(a.Repo+"@"+a.Base, b.Repo+"@"+b.Base) })
	return out
}

// refresh reads every base due at now: its CI and its tree. A failed read keeps the last
// fact of its kind.
func (d *dealFactsReader) refresh(ctx context.Context, now time.Time) {
	for _, b := range d.due(now) {
		ci, cerr := d.readCI(ctx, b)
		tree, terr := d.readTree(ctx, b)
		d.mu.Lock()
		d.read[b] = now
		if cerr == nil && ci != nil {
			if prev, ok := d.ci[b]; ok && prev.Red && ci.Red && prev.FirstRed != "" {
				ci.FirstRed = prev.FirstRed // the red stretch began at the first red tip read
			}
			d.ci[b] = *ci
		}
		if terr == nil && tree != nil {
			d.tree[b] = *tree
		} else if terr != nil {
			delete(d.tree, b) // never an older tip's tree
		}
		d.mu.Unlock()
		if cerr != nil && d.say != nil {
			d.say("DEAL FACTS: %s@%s CI not read: %s", b.Repo, b.Base, oneline.Err(cerr))
		}
		if terr != nil && d.say != nil {
			d.say("DEAL FACTS: %s@%s tree not read (its brief paths are not checked): %s", b.Repo, b.Base, oneline.Err(terr))
		}
	}
}

// failedConclusion says a workflow run or job concluded red.
func failedConclusion(c string) bool {
	return c == "failure" || c == "timed_out" || c == "startup_failure"
}

// readCI is the base's CI fact: the newest push commit whose runs all completed, red when
// a run of it failed, with the failing jobs and their platforms. nil, nil when no commit's
// runs have all completed yet.
func (d *dealFactsReader) readCI(ctx context.Context, b sprint.RepoBase) (*sprint.BaseCIFacts, error) {
	out, err := d.gh(ctx, "api", fmt.Sprintf("repos/%s/actions/runs?branch=%s&event=push&per_page=%d", b.Repo, b.Base, dealFactsRuns),
		"--jq", `.workflow_runs[] | [(.id|tostring), .name, .head_sha, .status, (.conclusion // "")] | @tsv`)
	if err != nil {
		return nil, err
	}
	type run struct{ id, name, sha, status, conclusion string }
	var order []string
	bySha := map[string][]run{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 5 || f[2] == "" {
			continue
		}
		if _, ok := bySha[f[2]]; !ok {
			order = append(order, f[2])
		}
		bySha[f[2]] = append(bySha[f[2]], run{f[0], f[1], f[2], f[3], f[4]})
	}
	for _, sha := range order {
		runs := bySha[sha]
		if slices.ContainsFunc(runs, func(r run) bool { return r.status != "completed" }) {
			continue
		}
		fact := &sprint.BaseCIFacts{Repo: b.Repo, Base: b.Base, Tip: sha}
		for _, r := range runs {
			if !failedConclusion(r.conclusion) {
				continue
			}
			checks, err := d.failedJobs(ctx, b.Repo, r.id, r.name, r.conclusion)
			if err != nil {
				return nil, err
			}
			for _, c := range checks {
				fact.TipFailed = append(fact.TipFailed, c)
				fact.Failed = append(fact.Failed, c.Name)
			}
		}
		fact.Red = len(fact.TipFailed) > 0
		if fact.Red {
			fact.FirstRed = sha
		}
		return fact, nil
	}
	return nil, nil
}

// failedJobs is a failed run's failing jobs with their platforms, read once a run and
// conclusion. A run that failed with no failing job (a startup failure) is one check of
// its own name.
func (d *dealFactsReader) failedJobs(ctx context.Context, repo, id, workflow, conclusion string) ([]sprint.BaseCheck, error) {
	key := id + "\x00" + conclusion
	d.mu.Lock()
	cached, ok := d.jobs[key]
	d.mu.Unlock()
	if ok {
		return cached, nil
	}
	out, err := d.gh(ctx, "api", fmt.Sprintf("repos/%s/actions/runs/%s/jobs?per_page=100", repo, id),
		"--jq", `.jobs[] | [.name, (.conclusion // ""), ((.labels // []) | join(","))] | @tsv`)
	if err != nil {
		return nil, err
	}
	var checks []sprint.BaseCheck
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 2 || !failedConclusion(f[1]) {
			continue
		}
		var labels []string
		if len(f) > 2 && f[2] != "" {
			labels = strings.Split(f[2], ",")
		}
		checks = append(checks, sprint.BaseCheck{Name: workflow + " / " + f[0], Platforms: sprint.CheckPlatforms(workflow, f[0], labels)})
	}
	if len(checks) == 0 {
		checks = []sprint.BaseCheck{{Name: workflow, Platforms: sprint.CheckPlatforms(workflow, "", nil)}}
	}
	d.mu.Lock()
	d.jobs[key] = checks
	d.mu.Unlock()
	return checks, nil
}

// readTree is the base's tree at its tip in the reader's own mirror: fetched, then every
// path at the tip.
func (d *dealFactsReader) readTree(ctx context.Context, b sprint.RepoBase) (*sprint.BaseTree, error) {
	dir, err := d.mirror(b.Repo)
	if err != nil {
		return nil, err
	}
	ref := "refs/heads/" + b.Base
	if _, err := d.git(ctx, dir, "fetch", "-q", "--no-tags", "--filter=blob:none", "origin", "+"+ref+":"+ref); err != nil {
		return nil, err
	}
	sha, err := d.git(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return nil, err
	}
	sha = strings.TrimSpace(sha)
	d.mu.Lock()
	prev, ok := d.tree[b]
	d.mu.Unlock()
	if ok && prev.Sha == sha {
		return &prev, nil
	}
	out, err := d.git(ctx, dir, "ls-tree", "-r", "--name-only", "--full-tree", sha)
	if err != nil {
		return nil, err
	}
	t := sprint.BaseTreeOf(b.Repo, b.Base, sha, strings.Split(strings.TrimSpace(out), "\n"))
	return &t, nil
}

// dealFactsLoop is the reader's own loop beside the run loop's ticks: every
// dealFactsEvery/4 it reads the bases due.
func (a *app) dealFactsLoop(ctx context.Context, d *dealFactsReader) {
	for ctx.Err() == nil {
		d.refresh(ctx, a.now())
		select {
		case <-ctx.Done():
			return
		case <-a.after(dealFactsEvery / 4):
		}
	}
}

// newServerDealFacts is the run loop's reader, wired to gh, git and the land root.
func (a *app) newServerDealFacts() *dealFactsReader {
	d := newDealFactsReader()
	d.gh = func(ctx context.Context, args ...string) (string, error) {
		b := subproc.Prepare(ctx, subproc.GHBudget, "gh", args...)
		defer b.Cancel()
		b.Cmd.Env = a.forgeCommandEnv(a.gitEnv)
		var stdout, stderr bytes.Buffer
		b.Cmd.Stdout, b.Cmd.Stderr = &stdout, &stderr
		if err := b.Wrap("gh", b.Cmd.Run()); err != nil {
			return "", fmt.Errorf("gh %s: %s", args[1], oneLine(strings.TrimSpace(stderr.String())))
		}
		return stdout.String(), nil
	}
	d.git = func(ctx context.Context, dir string, args ...string) (string, error) {
		return gitrun.Output(ctx, gitrun.Options{C: dir, Env: a.gitEnv, OwnRepo: true}, args...)
	}
	d.mirror = func(repo string) (string, error) {
		root, err := a.landRoot()
		if err != nil {
			return "", err
		}
		url := "https://github.com/" + repo
		dir := filepath.Join(root, "dealfacts", repoDirName(url)+".git")
		if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
			return dir, nil
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := gitrun.Output(ctx, gitrun.Options{C: filepath.Dir(dir), Env: a.gitEnv}, "clone", "-q", "--bare", "--filter=blob:none", "--no-tags", url, dir); err != nil {
			return "", errors.Join(errors.New("the mirror of "+repo+" could not be made"), err)
		}
		return dir, nil
	}
	d.say = func(format string, args ...any) { a.serveSay(format, args...) }
	return d
}
