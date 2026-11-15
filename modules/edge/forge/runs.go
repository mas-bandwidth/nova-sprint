package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// Run is one GitHub Actions workflow run that has not finished: queued
// (waiting for a runner) or in progress.
type Run struct {
	ID      int64
	Branch  string // its head branch
	HeadSHA string
	Status  string // queued, in_progress, waiting, requested, pending
	Event   string // pull_request, push, ...
	Created time.Time
	PRs     []int // the pull requests it runs for (none for a push)
}

// RunsPage is how many runs one read returns (GitHub's largest page), and
// RunsPages how many pages of each status Runs reads at most: a backlog of
// up to 2000 runs of each status is read whole (the backlog of 2026-11-15
// was about 1800 queued), a longer one its newest 2000.
const (
	RunsPage  = 100
	RunsPages = 20
)

// Runs is repo's unfinished workflow runs, queued and in progress, read a
// page at a time until a short page or RunsPages pages: the landing cancels
// from it the runs that do not gate a landing, and check counts what a
// landing PR's runs wait behind (LANDING.md, "The landing's CI comes
// first").
func (c *Client) Runs(ctx context.Context, repo string) ([]Run, error) {
	if repo == "" {
		return nil, fmt.Errorf("forge refused: Runs requires a repo")
	}
	var out []Run
	for _, status := range []string{"queued", "in_progress"} {
		for page := 1; page <= RunsPages; page++ {
			stdout, _, err := c.runGHBytes_internal(ctx, 4<<20, "api", "--method", "GET", "repos/"+repo+"/actions/runs",
				"-f", "status="+status, "-f", "per_page="+strconv.Itoa(RunsPage), "-f", "page="+strconv.Itoa(page))
			if err != nil {
				return nil, err
			}
			runs, err := parseRuns_internal(stdout)
			if err != nil {
				return nil, err
			}
			out = append(out, runs...)
			if len(runs) < RunsPage {
				break
			}
		}
	}
	return out, nil

}

// RunsAt is every workflow run of branch at sha (any status, completed
// included), the newest RunsPage: GitHub's head_sha and branch filters,
// one read.
func (c *Client) RunsAt(ctx context.Context, repo, branch, sha string) ([]Run, error) {
	if repo == "" || branch == "" || sha == "" {
		return nil, fmt.Errorf("forge refused: RunsAt requires a repo, a branch and a sha")
	}
	stdout, _, err := c.runGHBytes_internal(ctx, 4<<20, "api", "--method", "GET", "repos/"+repo+"/actions/runs",
		"-f", "head_sha="+sha, "-f", "branch="+branch, "-f", "per_page="+strconv.Itoa(RunsPage))
	if err != nil {
		return nil, err
	}
	return parseRuns_internal(stdout)
}

// parseRuns_internal decodes one page of GET /repos/{repo}/actions/runs.

func parseRuns_internal(stdout string) ([]Run, error) {
	var v struct {
		Runs []struct {
			ID      int64     `json:"id"`
			Branch  string    `json:"head_branch"`
			HeadSHA string    `json:"head_sha"`
			Status  string    `json:"status"`
			Event   string    `json:"event"`
			Created time.Time `json:"created_at"`
			PRs     []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		return nil, fmt.Errorf("forge: decode actions/runs: %s", edge.ParseFailure(err, []byte(stdout)))
	}
	out := make([]Run, 0, len(v.Runs))
	for _, r := range v.Runs {
		run := Run{ID: r.ID, Branch: r.Branch, HeadSHA: r.HeadSHA, Status: r.Status, Event: r.Event, Created: r.Created}
		for _, p := range r.PRs {
			run.PRs = append(run.PRs, p.Number)
		}
		out = append(out, run)

	}
	return out, nil
}

// CancelRun cancels workflow run id of repo. A run that finished meanwhile
// is GitHub's 409, which is no failure: there is nothing left to cancel.
func (c *Client) CancelRun(ctx context.Context, repo string, id int64) error {
	if repo == "" || id <= 0 {
		return fmt.Errorf("forge refused: CancelRun requires a repo and a run id")
	}
	stdout, stderr, err := c.runGH_internal(ctx, "api", "--method", "POST", "repos/"+repo+"/actions/runs/"+strconv.FormatInt(id, 10)+"/cancel")
	if err != nil && finished_internal(stdout+stderr) {
		return nil
	}
	return err
}

// finished_internal is whether gh's output is GitHub's answer for a run
// that can no longer be cancelled (it completed).
func finished_internal(out string) bool {
	return strings.Contains(out, "Cannot cancel a workflow run that is completed") || strings.Contains(out, "HTTP 409")
}
