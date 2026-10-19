package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

var linearLandSubject = regexp.MustCompile(`^land ([^ ]+) \(sprint stream [^)]+\)$`)
var linearURL = regexp.MustCompile(`https://[^\s]+`)

type linearGitAdapter struct {
	l           *lander
	ctx         context.Context
	dir, before string
}

func (g *linearGitAdapter) LatestTrailer(_, key string) (string, error) {
	out, err := g.l.git(g.ctx, g.dir, "log", "--first-parent", "--format=%(trailers:key="+key+",valueonly)", "HEAD")
	if err != nil {
		return "", err
	}
	if v := strings.Fields(out); len(v) > 0 {
		return v[0], nil
	}
	return "", nil
}
func (g *linearGitAdapter) MergeBase(_, tip string) (string, error) {
	return g.l.git(g.ctx, g.dir, "merge-base", "HEAD", tip)
}
func (g *linearGitAdapter) FirstParentRange(from, to string) ([]sprint.LinearSourceCommit, error) {
	out, err := g.l.git(g.ctx, g.dir, "rev-list", "--first-parent", "--reverse", from+".."+to)
	if err != nil {
		return nil, err
	}
	var commits []sprint.LinearSourceCommit
	for _, sha := range strings.Fields(out) {
		subject, err := g.l.git(g.ctx, g.dir, "show", "-s", "--format=%s", sha)
		if err != nil {
			return nil, err
		}
		parents, err := g.l.git(g.ctx, g.dir, "rev-list", "--parents", "-n", "1", sha)
		if err != nil {
			return nil, err
		}
		id := sha
		if m := linearLandSubject.FindStringSubmatch(subject); len(m) == 2 {
			id = m[1]
		}
		commits = append(commits, sprint.LinearSourceCommit{SHA: sha, Card: id, Subject: subject, Merge: len(strings.Fields(parents)) > 2})
	}
	return commits, nil
}
func (g *linearGitAdapter) CardOnBase(_ string, card string) (bool, error) {
	out, err := g.l.git(g.ctx, g.dir, "log", "--first-parent", "--format=%(trailers:key=Nova-Card,valueonly)", "HEAD")
	if err != nil {
		return false, err
	}
	for _, id := range strings.Fields(out) {
		if id == card {
			return true, nil
		}
	}
	return false, nil
}
func (g *linearGitAdapter) CherryPick(sha string, mainline bool) error {
	args := []string{"cherry-pick", "--no-commit"}
	if mainline {
		args = append(args, "-m", "1")
	}
	args = append(args, sha)
	_, err := g.l.git(g.ctx, g.dir, args...)
	if err != nil {
		paths, _ := g.l.git(g.ctx, g.dir, "diff", "--name-only", "--diff-filter=U")
		return fmt.Errorf("%w%s", err, linearPaths(paths))
	}
	return nil
}
func (g *linearGitAdapter) Squash(head string) error {
	_, err := g.l.git(g.ctx, g.dir, "merge", "--squash", head)
	if err != nil {
		paths, _ := g.l.git(g.ctx, g.dir, "diff", "--name-only", "--diff-filter=U")
		return fmt.Errorf("%w%s", err, linearPaths(paths))
	}
	return nil
}
func (g *linearGitAdapter) SetTree(head string) error {
	_, err := g.l.git(g.ctx, g.dir, "read-tree", "--reset", "-u", head)
	return err
}
func (g *linearGitAdapter) Tree(ref string) (string, error) {
	return g.l.git(g.ctx, g.dir, "rev-parse", ref+"^{tree}")
}
func (g *linearGitAdapter) Commit(message string) error {
	_, err := g.l.git(g.ctx, g.dir, "commit", "--allow-empty", "-m", message)
	return err
}
func (g *linearGitAdapter) Abort() error {
	_, _ = g.l.git(g.ctx, g.dir, "cherry-pick", "--abort")
	_, err := g.l.git(g.ctx, g.dir, "reset", "--hard", g.before)
	return err
}
func (g *linearGitAdapter) Tip() (string, error) { return g.l.git(g.ctx, g.dir, "rev-parse", "HEAD") }
func (g *linearGitAdapter) MergeCommits(_, tip string) ([]string, error) {
	out, err := g.l.git(g.ctx, g.dir, "rev-list", "--min-parents=2", g.before+".."+tip)
	return strings.Fields(out), err
}
func (g *linearGitAdapter) Push(string, string) error { return nil }

func (l *lander) linearMergeHead(ctx context.Context, dir, _ string, c landCard) (card, env, note string) {
	before, err := l.git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", "the linear landing of " + c.id + " could not read its base: " + firstLine("", err), ""
	}
	if _, err = l.git(ctx, dir, "cat-file", "-e", c.head+"^{commit}"); err != nil {
		if _, ferr := l.fetched(ctx, dir, "fetch", "--no-tags", "origin", c.head); ferr != nil {
			return "", "the fetch of the head " + c.head + " of " + c.id + " failed: " + firstLine("", ferr), ""
		}
	}
	parents, err := l.git(ctx, dir, "rev-list", "--parents", "-n", "1", c.head)
	if err != nil {
		return "", "the linear landing of " + c.id + " could not inspect its head: " + firstLine("", err), ""
	}
	fields := strings.Fields(parents)
	forward, source := len(fields) >= 3, c.head
	if forward {
		source = fields[2]
	}
	pr := ""
	if u := linearURL.FindString(c.result); strings.Contains(u, "/pull/") {
		pr = u
	}
	g := &linearGitAdapter{l: l, ctx: ctx, dir: dir, before: before}
	err = sprint.LandLinear(g, c.repo, c.base, source, []sprint.LinearCard{{ID: c.id, Title: linearTitle(c), Head: c.head, Source: source, PullRequest: pr, Forward: forward}})
	if err != nil {
		_ = g.Abort()
		return err.Error(), "", ""
	}
	return "", "", "head replayed onto linear base"
}

func linearTitle(c landCard) string {
	for _, line := range strings.Split(c.brief, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		if strings.HasPrefix(line, "RESULT:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "RESULT:"))
		}
	}
	return c.id
}
func linearPaths(paths string) string {
	paths = strings.Join(strings.Fields(paths), ", ")
	if paths == "" {
		return ""
	}
	return " in " + paths
}
