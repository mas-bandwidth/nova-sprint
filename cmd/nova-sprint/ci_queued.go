package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
)

// THE QUEUED CI WITH NO RUNNER (docs/SPEC-SPRINT.md, "Drift alarms"; internal/sprint/ci_queued.go).
// The owner, 2026-10-13: "we need to catch these CI failures much much much earlier". Every
// rocketnet CI job needing a self-hosted runner sat queued for about three hours while the only
// machine carrying the labels was down and nothing alarmed. The binding reads a repository's
// queued workflow jobs and its online runners through gh, as the lander asks the merge queue
// (mergewindow.go), and the tick raises a judgment for each job queued past the bound. The
// read is kept for ciQueuedKeep so the tick does not ask the forge every round; a test gives a
// fake ciLister and asks no forge.

// ciQueuedKeep is how long the queued-CI read is kept: the tick asks at most once a minute.
const ciQueuedKeep = time.Minute

// ciQueuedJob is one workflow job queued at a forge, as the read names it.
type ciQueuedJob struct {
	RunID       string
	Run         string
	Job         string
	Labels      []string
	QueuedSince time.Time
}

// ciRunner is one self-hosted runner of a repository, as the read names it.
type ciRunner struct {
	Name   string
	Labels []string
}

// ciLister reads a repository's queued workflow jobs and its online runners.
// QueuedJobs' error is the job read's and yields no facts; RunnersOnline's error
// (a 403 or 404, say) is reported as RunnerOnline "unknown", never as an error.
type ciLister interface {
	QueuedJobs(ctx context.Context, repo string) ([]ciQueuedJob, error)
	RunnersOnline(ctx context.Context, repo string) ([]ciRunner, error)
}

// runnerHasAll says an online runner carries every label a job wants.
func runnerHasAll(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ciQueuedFactsOf maps a repository's queued jobs and online runners to the tick's
// facts (sprint.CIQueuedFacts): each queued job with whether a runner carrying all
// its labels is online ("yes"), none is ("no"), or the runners read failed
// ("unknown").
func ciQueuedFactsOf(ctx context.Context, l ciLister, repo string) []sprint.CIQueuedFacts {
	jobs, err := l.QueuedJobs(ctx, repo)
	if err != nil {
		return nil
	}
	runners, rerr := l.RunnersOnline(ctx, repo)
	var out []sprint.CIQueuedFacts
	for _, j := range jobs {
		online := "unknown"
		if rerr == nil {
			online = "no"
			for _, rn := range runners {
				if runnerHasAll(rn.Labels, j.Labels) {
					online = "yes"
					break
				}
			}
		}
		out = append(out, sprint.CIQueuedFacts{Repo: repo, RunID: j.RunID, Run: j.Run, Job: j.Job,
			Labels: j.Labels, QueuedSince: j.QueuedSince, RunnerOnline: online})
	}
	return out
}

// ciQueuedCache is the tick's queued-CI read: the facts of every live stream's
// repository, refreshed at most once a minute by the caller's clock, never a sleep.
type ciQueuedCache struct {
	lister ciLister
	st     *store.Store
	now    func() time.Time
	mu     sync.Mutex
	at     time.Time
	held   []sprint.CIQueuedFacts
}

// facts is the tick's read: the kept facts while they are fresh, a fresh read
// otherwise.
func (c *ciQueuedCache) facts() []sprint.CIQueuedFacts {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && c.now().Sub(c.at) < ciQueuedKeep {
		return c.held
	}
	c.held = c.read()
	c.at = c.now()
	return c.held
}

// read asks each live stream's repository for its queued jobs and online runners.
func (c *ciQueuedCache) read() []sprint.CIQueuedFacts {
	ctx := context.Background()
	s, err := c.st.Load(ctx, store.All, nil)
	if err != nil {
		return nil
	}
	var out []sprint.CIQueuedFacts
	for _, repo := range sprint.CIQueuedRepos(s) {
		out = append(out, ciQueuedFactsOf(ctx, c.lister, repo)...)
	}
	return out
}

// ciQueuedFn is the tick's read of queued CI jobs, as run and tick set it on the
// store (store.Store.CIQueued, sprint.TickReq.CIQueued).
func (a *app) ciQueuedFn(st *store.Store) func() []sprint.CIQueuedFacts {
	c := &ciQueuedCache{
		lister: ghCIQueued{host: githubHost, env: func() []string { return a.forgeCommandEnv(a.gitEnv) }},
		st:     st,
		now:    func() time.Time { return a.now() },
	}
	return c.facts
}

// ghCIQueued asks a repository's queued workflow jobs and its runners through gh,
// as the caller's gh is authenticated there (ghMergeQueue, mergewindow.go), in the
// caller's environment. A repository on any other host, or a path, is never asked.
type ghCIQueued struct {
	host string
	env  func() []string
}

func (g ghCIQueued) api(ctx context.Context, path string) ([]byte, error) {
	cmd, cancel := subproc.Command(ctx, subproc.GH, "gh", "api", "--hostname", g.host, path)
	defer cancel()
	if g.env != nil {
		cmd.Env = g.env()
	}
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, errors.New("gh api " + path + ": " + strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func (g ghCIQueued) QueuedJobs(ctx context.Context, repo string) ([]ciQueuedJob, error) {
	owner, name, ok := forgeRepo(g.host, repo)
	if !ok {
		return nil, nil
	}
	raw, err := g.api(ctx, "repos/"+owner+"/"+name+"/actions/runs?status=queued")
	if err != nil {
		return nil, err
	}
	var runs struct {
		WorkflowRuns []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(raw, &runs); err != nil {
		return nil, err
	}
	var out []ciQueuedJob
	for _, run := range runs.WorkflowRuns {
		rawl, err := g.api(ctx, "repos/"+owner+"/"+name+"/actions/runs/"+strconv.FormatInt(run.ID, 10)+"/jobs?filter=queued")
		if err != nil {
			return nil, err
		}
		var jobs struct {
			Jobs []struct {
				Name      string    `json:"name"`
				Status    string    `json:"status"`
				CreatedAt time.Time `json:"created_at"`
				Labels    []struct {
					Name string `json:"name"`
				} `json:"labels"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal(rawl, &jobs); err != nil {
			return nil, err
		}
		for _, j := range jobs.Jobs {
			if j.Status != "queued" {
				continue
			}
			var labels []string
			for _, l := range j.Labels {
				if l.Name != "" {
					labels = append(labels, l.Name)
				}
			}
			out = append(out, ciQueuedJob{RunID: strconv.FormatInt(run.ID, 10), Run: run.Name, Job: j.Name,
				Labels: labels, QueuedSince: j.CreatedAt})
		}
	}
	return out, nil
}

func (g ghCIQueued) RunnersOnline(ctx context.Context, repo string) ([]ciRunner, error) {
	owner, name, ok := forgeRepo(g.host, repo)
	if !ok {
		return nil, nil
	}
	raw, err := g.api(ctx, "repos/"+owner+"/"+name+"/actions/runners")
	if err != nil {
		return nil, err
	}
	var runners struct {
		Runners []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"runners"`
	}
	if err := json.Unmarshal(raw, &runners); err != nil {
		return nil, err
	}
	var out []ciRunner
	for _, r := range runners.Runners {
		if r.Status != "online" {
			continue
		}
		var labels []string
		for _, l := range r.Labels {
			if l.Name != "" {
				labels = append(labels, l.Name)
			}
		}
		out = append(out, ciRunner{Name: r.Name, Labels: labels})
	}
	return out, nil
}
