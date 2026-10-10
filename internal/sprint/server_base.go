package sprint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/buildinfo"
)

type ServerBase struct {
	Line, Commit, Why, Base, Tip string
	On                           bool
}

func CheckServerBinary(ctx context.Context, binary, repo, base string) (ServerBase, error) {
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, "version")
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return ServerBase{}, fmt.Errorf("%s version failed: %v: %s", binary, err, strings.TrimSpace(stderr.String()))
	}
	line, _, _ := strings.Cut(out.String(), "\n")
	b, err := CheckServerBase(ctx, strings.TrimSpace(line), repo, base)
	if err != nil {
		return b, err
	}
	return b, nil
}

// CheckServerBase applies SPEC-SPRINT section 14's server-from-base-only rule:
// the candidate revision must be an ancestor of origin/<base>'s fetched tip.
func CheckServerBase(ctx context.Context, line, repo, base string) (ServerBase, error) {
	b := ServerBase{Line: line, Base: base}
	if repo == "" || base == "" || strings.HasPrefix(base, "-") || strings.ContainsAny(base, " \t:~^?*[\\") {
		return b, errors.New("the sprint base and repository clone are required")
	}
	if f, ok := buildinfo.Parse(line); ok {
		if commit, ok := f.Extra("commit"); ok {
			b.Commit = commit
		} else if src, ok := f.FindSource(); ok {
			b.Commit = src.Revision
			if src.Dirty {
				b.Why = "the candidate was built from an edited tree"
			}
		} else if strings.HasPrefix(f.Version, "devel") || f.Version == "" {
			b.Why = "its build identity names no source commit"
		} else if i := strings.LastIndex(f.Version, "-"); i >= 0 && len(f.Version[i+1:]) >= 12 {
			b.Commit = f.Version[i+1:]
		} else {
			b.Why = "its build identity names no source commit"
		}
	} else {
		b.Why = "its version line is not a version line"
	}
	ref := "refs/remotes/origin/" + base
	if _, err := git(ctx, repo, "remote", "get-url", "origin"); err == nil {
		if _, err := git(ctx, repo, "fetch", "--quiet", "--no-tags", "origin", "+refs/heads/"+base+":"+ref); err != nil {
			return b, fmt.Errorf("fetching origin's %s into %s failed: %w", base, repo, err)
		}
	}
	tip, err := git(ctx, repo, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		tip, err = git(ctx, repo, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	}
	if err != nil {
		return b, fmt.Errorf("origin/%s has no tip in %s: %w", base, repo, err)
	}
	b.Tip = tip
	if b.Commit == "" || b.Why != "" {
		return b, nil
	}
	full, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", "--end-of-options", b.Commit+"^{commit}")
	if err != nil {
		b.Why = "its commit is not in origin/" + base + "'s history"
		return b, nil
	}
	b.Commit = full
	_, err = git(ctx, repo, "merge-base", "--is-ancestor", "--end-of-options", full, tip)
	if err == nil {
		b.On = true
		return b, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		b.Why = "its commit is not an ancestor of origin/" + base
		return b, nil
	}
	return b, fmt.Errorf("git merge-base --is-ancestor in %s: %w", repo, err)
}

func git(ctx context.Context, repo string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

type OffBaseError struct {
	Binary string
	Check  ServerBase
}

func (e *OffBaseError) Error() string {
	commit := e.Check.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	tip := e.Check.Tip
	if len(tip) > 12 {
		tip = tip[:12]
	}
	return fmt.Sprintf("%s was built from commit %s, not from the sprint base origin/%s (tip %s): %s; remedy: build nova-sprint from origin/%s at its tip, then run: nova-sprint server switch <that binary>", e.Binary, commit, e.Check.Base, tip, e.Check.Why, e.Check.Base)
}

type BaseRecord struct {
	Repo string `json:"repo"`
	Base string `json:"base"`
}
