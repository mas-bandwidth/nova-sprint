package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var linearLandSubject = regexp.MustCompile(`^land ([^ ]+) \(sprint stream [^)]+\)$`)

// linearMergeHead adapts a forward-merge head to a linear base. It replays the
// forward branch's first-parent card commits, records durable resume trailers,
// and repairs the final tree to the reviewed head without putting a merge on
// the protected base.
func (l *lander) linearMergeHead(ctx context.Context, dir, stream string, c landCard) (card, env, note string) {
	before, err := l.git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", "the linear landing of " + c.id + " could not read its base: " + firstLine("", err), ""
	}
	if _, err = l.git(ctx, dir, "cat-file", "-e", c.head+"^{commit}"); err != nil {
		if _, ferr := l.fetched(ctx, dir, "fetch", "--no-tags", "origin", c.head); ferr != nil {
			if containsAny(ferr.Error(), notOnOrigin) {
				if _, all := l.fetched(ctx, dir, "fetch", "--no-tags", "origin", "+refs/heads/*:refs/remotes/origin/*"); all != nil {
					return "", "the fetch of origin's branches for the head " + c.head + " of " + c.id + " failed: " + firstLine("", all), ""
				}
				if _, still := l.git(ctx, dir, "cat-file", "-e", c.head+"^{commit}"); still != nil {
					return "the head " + c.head + " of " + c.id + " is missing: origin holds no such commit (" + firstLine("", ferr) + ")", "", ""
				}
			} else {
				return "", "the fetch of the head " + c.head + " of " + c.id + " failed: " + firstLine("", ferr), ""
			}
		}
	}

	parents, err := l.git(ctx, dir, "rev-list", "--parents", "-n", "1", c.head)
	if err != nil {
		return "", "the linear landing of " + c.id + " could not inspect its head: " + firstLine("", err), ""
	}
	fields := strings.Fields(parents)
	if len(fields) < 3 {
		return l.linearLandOne(ctx, dir, before, c.head, c.id, linearTitle(c), c)
	}
	sourceTip := fields[2]
	last := linearLastSource(l, ctx, dir)
	if last == "" {
		last, err = l.git(ctx, dir, "merge-base", "HEAD", sourceTip)
		if err != nil {
			return "", "the linear landing of " + c.id + " could not find its replay base: " + firstLine("", err), ""
		}
	}
	commits, err := l.git(ctx, dir, "rev-list", "--first-parent", "--reverse", last+".."+sourceTip)
	if err != nil {
		return "", "the linear landing of " + c.id + " could not list source commits: " + firstLine("", err), ""
	}
	landed, err := l.git(ctx, dir, "log", "--first-parent", "--format=%B%x00", "HEAD")
	if err != nil {
		return "", "the linear landing of " + c.id + " could not read resume trailers: " + firstLine("", err), ""
	}
	for _, source := range strings.Fields(commits) {
		subject, serr := l.git(ctx, dir, "show", "-s", "--format=%s", source)
		if serr != nil {
			return "", "the linear landing of " + c.id + " could not read source " + source + ": " + firstLine("", serr), ""
		}
		id := source
		if m := linearLandSubject.FindStringSubmatch(subject); len(m) == 2 {
			id = m[1]
		}
		if strings.Contains(landed, "Nova-Card: "+id) {
			continue
		}
		ps, perr := l.git(ctx, dir, "rev-list", "--parents", "-n", "1", source)
		if perr != nil {
			return "", "the linear landing of " + c.id + " could not inspect source " + source + ": " + firstLine("", perr), ""
		}
		args := []string{"cherry-pick", "--no-commit"}
		if len(strings.Fields(ps)) > 2 {
			args = append(args, "-m", "1")
		}
		args = append(args, source)
		if _, cerr := l.git(ctx, dir, args...); cerr != nil {
			paths, _ := l.git(ctx, dir, "diff", "--name-only", "--diff-filter=U")
			_, _ = l.git(ctx, dir, "cherry-pick", "--abort")
			if _, rerr := l.git(ctx, dir, "reset", "--hard", before); rerr != nil {
				return "", "the reset after the linear conflict of " + id + " failed: " + firstLine("", rerr), ""
			}
			return fmt.Sprintf("linear replay of %s for %s conflicted at %s%s", id, c.id, source, linearPaths(paths)), "", ""
		}
		msg := id + ": " + subject + "\n\nFinish report: nova-sprint card " + id + "\n\nNova-Card: " + id + "\nNova-Linear-Source: " + source
		if _, cerr := l.git(ctx, dir, "commit", "--allow-empty", "-m", msg); cerr != nil {
			_, _ = l.git(ctx, dir, "reset", "--hard", before)
			return "", "the linear replay commit of " + id + " failed: " + firstLine("", cerr), ""
		}
		landed += "\x00Nova-Card: " + id
	}

	want, err := l.git(ctx, dir, "rev-parse", c.head+"^{tree}")
	if err != nil {
		return "", "the linear landing of " + c.id + " could not read its reviewed tree: " + firstLine("", err), ""
	}
	got, _ := l.git(ctx, dir, "rev-parse", "HEAD^{tree}")
	if got != want {
		if _, err := l.git(ctx, dir, "read-tree", "--reset", "-u", c.head); err != nil {
			_, _ = l.git(ctx, dir, "reset", "--hard", before)
			return "", "the linear tree repair of " + c.id + " failed: " + firstLine("", err), ""
		}
		msg := c.id + ": " + linearTitle(c) + "\n\nFinish report: nova-sprint card " + c.id + "\n\nNova-Card: " + c.id + "\nNova-Linear-Source: " + c.head
		if _, err := l.git(ctx, dir, "commit", "-m", msg); err != nil {
			_, _ = l.git(ctx, dir, "reset", "--hard", before)
			return "", "the linear tree repair commit of " + c.id + " failed: " + firstLine("", err), ""
		}
	}
	return "", "", "forward-merge head replayed onto linear base"
}

func (l *lander) linearLandOne(ctx context.Context, dir, before, source, id, title string, c landCard) (string, string, string) {
	if _, err := l.git(ctx, dir, "merge", "--squash", source); err != nil {
		paths, _ := l.git(ctx, dir, "diff", "--name-only", "--diff-filter=U")
		_, _ = l.git(ctx, dir, "reset", "--hard", before)
		return "linear squash of " + id + " conflicted at " + source + linearPaths(paths), "", ""
	}
	msg := id + ": " + title + "\n\nFinish report: nova-sprint card " + id + "\n\nNova-Card: " + id + "\nNova-Linear-Source: " + source
	if _, err := l.git(ctx, dir, "commit", "-m", msg); err != nil {
		_, _ = l.git(ctx, dir, "reset", "--hard", before)
		return "", "the linear squash commit of " + id + " failed: " + firstLine("", err), ""
	}
	return "", "", "head squashed onto linear base"
}

func linearLastSource(l *lander, ctx context.Context, dir string) string {
	log, err := l.git(ctx, dir, "log", "--first-parent", "--format=%B", "HEAD")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(log, "\n") {
		if !strings.HasPrefix(line, "Nova-Linear-Source:") {
			continue
		}
		if v := strings.TrimSpace(strings.TrimPrefix(line, "Nova-Linear-Source:")); v != "" {
			return v
		}
	}
	return ""
}

func linearTitle(c landCard) string {
	for _, line := range strings.Split(c.brief, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		if strings.HasPrefix(line, "RESULT:") {
			if title := strings.TrimSpace(strings.TrimPrefix(line, "RESULT:")); title != "" {
				return title
			}
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
