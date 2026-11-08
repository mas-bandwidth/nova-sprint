package forge

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// The facts of a red landing (LANDING.md, "Red that is not the card's"):
// which of the head's failing workflows are red on the base too, and, for
// the ones that are not, the failed jobs, their runners and the first error
// lines of their logs.

const (
	// LogMaxBytes caps one job log read: a failed job's log runs to
	// megabytes, and its first error lines can sit anywhere in it (a guess,
	// not measured).
	LogMaxBytes = 8 << 20
	// NewJobsMax is the failed jobs of one new red workflow whose logs are
	// read, LineMax the bytes kept of one log line (both guesses).
	NewJobsMax = 3
	LineMax    = 240
)

var (
	logStampRe = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[0-9:.]+Z\s?`)
	// claRe is a check named like a contributor or license agreement (a
	// CLA, a CAA, "Contributor Assignment Agreement", license/cla).
	claRe = regexp.MustCompile(`(?i)\b(cla|caa)\b|contributor.{0,30}agreement`)
)

// FailedJob is one failed job of a workflow run at the head: its name, the
// runner that ran it, its page, and (for new red only) the first error
// lines of its log.
type FailedJob struct {
	ID                int64
	Name, Runner, URL string
	Lines             []string
}

// WorkflowRed is one workflow failing at the head. Basis says what the
// base's side was read from: the merge base's run, the base branch's last
// completed run of the same workflow (the merge base ran none), or nothing
// (no run on the base to compare with: new). Running: its run at the head
// is still queued or in progress.
type WorkflowRed struct {
	Workflow, Basis string
	Running         bool
	Jobs            []FailedJob
}

// RedSplit is a head's failing workflows split by the base: Inherited are
// red on the base too (not the card's), New are not.
type RedSplit struct {
	MergeBase      string
	Inherited, New []WorkflowRed
}

// IsAgreement is whether a check's name is a contributor or license
// agreement's (directive 63): its red is about who wrote the commits.
func IsAgreement(check string) bool { return claRe.MatchString(check) }

type actionsRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	WorkflowID int64  `json:"workflow_id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"html_url"`
}

type actionsJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	Runner     string `json:"runner_name"`
	URL        string `json:"html_url"`
}

const (
	runsJQ = `[.workflow_runs[] | {id, name, workflow_id, status, conclusion, html_url}]`
	jobsJQ = `[.jobs[] | {id, name, conclusion, runner_name, html_url}]`
)

// Inheritance compares head's failing Actions workflows with the base
// (directives 59 and 76): the merge base of baseTip and head (gh api
// repos/<r>/compare), each side's workflow runs (actions/runs?head_sha=),
// and, for a workflow with no completed run at the merge base, the base
// branch's last completed run of the same workflow. A workflow is failing
// at the head when a run of it there failed, or is still running with a
// failed job. Only new red's logs are read.
func (c *Client) Inheritance(ctx context.Context, repo, base, baseTip, head string) (RedSplit, error) {
	mb, err := c.mergeBase_internal(ctx, repo, baseTip, head)
	if err != nil {
		return RedSplit{}, err
	}
	headRuns, err := c.runs_internal(ctx, repo, "actions/runs?per_page=100&head_sha="+head)
	if err != nil {
		return RedSplit{}, err
	}
	baseRuns, err := c.runs_internal(ctx, repo, "actions/runs?per_page=100&head_sha="+mb)
	if err != nil {
		return RedSplit{}, err
	}
	split := RedSplit{MergeBase: mb}
	for _, name := range runNames_internal(headRuns) {
		w, failing, err := c.failingAtHead_internal(ctx, repo, name, headRuns)
		if err != nil || !failing {
			if err != nil {
				return RedSplit{}, err
			}
			continue
		}
		red, basis, err := c.redOnBase_internal(ctx, repo, base, mb, name, headRuns, baseRuns)
		if err != nil {
			return RedSplit{}, err
		}
		w.Basis = basis
		if red {
			split.Inherited = append(split.Inherited, w)
			continue
		}
		c.readLines_internal(ctx, repo, &w)
		split.New = append(split.New, w)
	}
	return split, nil
}

func (c *Client) mergeBase_internal(ctx context.Context, repo, baseTip, head string) (string, error) {
	rp, err := RepoPath(repo)
	if err != nil {
		return "", err
	}
	stdout, _, err := c.runGH_internal(ctx, "api", "repos/"+rp+"/compare/"+baseTip+"..."+head, "--jq", ".merge_base_commit.sha")
	if err != nil {
		return "", err
	}
	mb := strings.ToLower(strings.TrimSpace(stdout))
	if !isFullSHA_internal(mb) {
		return "", fmt.Errorf("forge: gh api compare %s...%s printed no merge base: %s", baseTip, head, edge.ParseRefused("not a 40-hex sha", 0, []byte(stdout)))
	}
	return mb, nil
}

func (c *Client) runs_internal(ctx context.Context, repo, path string) ([]actionsRun, error) {
	rp, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	stdout, _, err := c.runGH_internal(ctx, "api", "repos/"+rp+"/"+path, "--jq", runsJQ)
	if err != nil {
		return nil, err
	}
	var runs []actionsRun
	if err := json.Unmarshal([]byte(stdout), &runs); err != nil {
		return nil, fmt.Errorf("forge: decode gh api %s: %s", path, edge.ParseFailure(err, []byte(stdout)))
	}
	return runs, nil
}

func (c *Client) jobs_internal(ctx context.Context, repo string, run int64) ([]actionsJob, error) {
	path := "actions/runs/" + strconv.FormatInt(run, 10) + "/jobs?per_page=100"
	rp, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	stdout, _, err := c.runGH_internal(ctx, "api", "repos/"+rp+"/"+path, "--jq", jobsJQ)
	if err != nil {
		return nil, err
	}
	var jobs []actionsJob
	if err := json.Unmarshal([]byte(stdout), &jobs); err != nil {
		return nil, fmt.Errorf("forge: decode gh api %s: %s", path, edge.ParseFailure(err, []byte(stdout)))
	}
	return jobs, nil
}

// runNames_internal is the workflows of runs, each once, in first-seen order.
func runNames_internal(runs []actionsRun) []string {
	var names []string
	for _, r := range runs {
		if !slices.Contains(names, r.Name) {
			names = append(names, r.Name)
		}
	}
	return names
}

// failingAtHead_internal is workflow name at the head: failing when a run of
// it failed or is still running with a failed job, with its failed jobs.
func (c *Client) failingAtHead_internal(ctx context.Context, repo, name string, runs []actionsRun) (WorkflowRed, bool, error) {
	w := WorkflowRed{Workflow: name}
	failing := false
	for _, r := range runs {
		if r.Name != name {
			continue
		}
		running := r.Status != "completed"
		w.Running = w.Running || running
		if !running && r.Conclusion != "failure" {
			continue
		}
		jobs, err := c.jobs_internal(ctx, repo, r.ID)
		if err != nil {
			return w, false, err
		}
		for _, j := range jobs {
			if j.Conclusion == "failure" {
				w.Jobs = append(w.Jobs, FailedJob{ID: j.ID, Name: j.Name, Runner: j.Runner, URL: j.URL})
			}
		}
		failing = failing || r.Conclusion == "failure" || len(w.Jobs) > 0
	}
	return w, failing, nil
}

// redOnBase_internal is whether workflow name is red on the base, and what
// that was read from: the merge base's newest completed run of it, else the
// base branch's last completed run of the same workflow, else nothing.
func (c *Client) redOnBase_internal(ctx context.Context, repo, base, mb, name string, headRuns, baseRuns []actionsRun) (bool, string, error) {
	for _, r := range baseRuns {
		if r.Name == name && r.Status == "completed" {
			return r.Conclusion == "failure", "the merge base " + short(mb), nil
		}
	}
	i := slices.IndexFunc(headRuns, func(r actionsRun) bool { return r.Name == name })
	path := "actions/workflows/" + strconv.FormatInt(headRuns[i].WorkflowID, 10) + "/runs?per_page=1&status=completed&branch=" + url.QueryEscape(base)
	last, err := c.runs_internal(ctx, repo, path)
	if err != nil {
		return false, "", err
	}
	if len(last) == 0 {
		return false, "no completed run on " + base + " to compare with", nil
	}
	return last[0].Conclusion == "failure", fmt.Sprintf("%s's last completed run %d (the merge base %s ran none)", base, last[0].ID, short(mb)), nil
}

// readLines_internal reads the first error lines of each of w's first
// NewJobsMax failed jobs; a log that cannot be read says so in its lines.
func (c *Client) readLines_internal(ctx context.Context, repo string, w *WorkflowRed) {
	for i := range w.Jobs[:min(len(w.Jobs), NewJobsMax)] {
		j := &w.Jobs[i]
		lines, err := c.JobLines(ctx, repo, j.ID)
		if err != nil {
			lines = []string{"(the log was not read: " + edge.RedactN(err.Error(), edge.FailureMax) + ")"}
		}
		j.Lines = lines
	}
}

// JobLines is the first five error lines of Actions job id's log (gh api
// actions/jobs/<id>/logs, readable once the job ends, even while its run
// goes on), each without its timestamp and cut to LineMax.
func (c *Client) JobLines(ctx context.Context, repo string, id int64) ([]string, error) {
	rp, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	stdout, _, err := c.runGHBytes_internal(ctx, LogMaxBytes, "api", "repos/"+rp+"/actions/jobs/"+strconv.FormatInt(id, 10)+"/logs")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, l := range strings.Split(stdout, "\n") {
		l = logStampRe.ReplaceAllString(strings.TrimSpace(l), "")
		b.WriteString(l[:min(len(l), LineMax)] + "\n")
	}
	return FirstFailing(b.String(), 5), nil
}

// Authors is the distinct commit author identities of PR pr, as "name
// <email> (login)" in first-seen order (directive 63: a failing agreement
// check names who must sign).
func (c *Client) Authors(ctx context.Context, repo string, pr int) ([]string, error) {
	stdout, _, err := c.runGH_internal(ctx, "pr", "view", strconv.Itoa(pr), "--repo", repo, "--json", "commits", "--jq", `[.commits[].authors[] | {login, name, email}]`)
	if err != nil {
		return nil, err
	}
	var as []struct{ Login, Name, Email string }
	if err := json.Unmarshal([]byte(stdout), &as); err != nil {
		return nil, fmt.Errorf("forge: decode gh pr view %d --json commits: %s", pr, edge.ParseFailure(err, []byte(stdout)))
	}
	var out []string
	for _, a := range as {
		id := cmp.Or(a.Name, a.Login, "an author with no name")
		if a.Email != "" {
			id += " <" + a.Email + ">"
		}
		if a.Login != "" && a.Login != id {
			id += " (" + a.Login + ")"
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// short is a sha's first 12 characters, as the judgement shows it.
func short(sha string) string { return sha[:min(len(sha), 12)] }
