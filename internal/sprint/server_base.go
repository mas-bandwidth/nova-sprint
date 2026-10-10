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
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/buildinfo"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
)

// The server is built from the sprint base and nothing else (docs/SPEC-SPRINT.md section 14,
// "server-from-base-only-w-ns-bb.w1"): the build commit a binary's version line names
// (buildinfo.Fields) must be an ancestor of origin's sprint base, fetched, by git merge-base
// --is-ancestor, or ServerSwitch refuses it and the old server keeps running. On 2026-10-04
// the live server ran a binary built from a side branch for a day, a branch 1,052 commits off
// the base one way and 24 the other: two lander designs at once. The owner, 2026-10-05:
// "Prevention is better than cure".

// vcsStampRevisionLen is 12 hex digits, the length buildinfo writes a vcs revision the line
// carries in field two (<utc revision time>-<12 hex>[-dirty]).
const vcsStampRevisionLen = 12

// ServerBase is one binary's build commit checked against origin's sprint base.
type ServerBase struct {
	Line   string // the binary's version line, as it printed it
	Commit string // the full commit the line names; "" when it names none
	Why    string // when not On: why (no commit, and why none; or not an ancestor)
	Base   string // the sprint base, a branch on origin
	Tip    string // origin/<Base> as fetched
	On     bool   // Commit is an ancestor of Tip
}

// BinaryLine is the first line `<binary> version` prints.
func BinaryLine(ctx context.Context, binary string) (string, error) {
	var out, errs bytes.Buffer
	cmd := subproc.Context(ctx, binary, "version")
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s version failed: %v: %s", binary, err, strings.TrimSpace(errs.String()))
	}
	line, _, _ := strings.Cut(out.String(), "\n")
	return strings.TrimSpace(line), nil
}

// CheckServerBinary is CheckServerBase over the line `<binary> version` prints. A binary
// whose version verb fails names no commit: it is a check made, and not On.
func CheckServerBinary(ctx context.Context, binary, repo, base string) (ServerBase, error) {
	if err := baseNamed(repo, base); err != nil {
		return ServerBase{Base: base}, err
	}
	line, err := BinaryLine(ctx, binary)
	if err != nil {
		line = ""
	}
	b, cerr := CheckServerBase(ctx, line, repo, base)
	if err != nil && cerr == nil {
		b.Why = "its version line could not be read: " + err.Error()
	}
	return b, cerr
}

// CheckServerBase reads the build commit from line, fetches origin's base into the clone
// repo and asks git whether the commit is an ancestor of its tip (docs/SPEC-SPRINT.md section
// 14, "server-from-base-only-w-ns-bb.w1"). An error is a check that could not be made (no base
// or clone named, a fetch that failed); a line naming no commit, or a commit the clone does
// not hold after the fetch (so not in the base's history), is a check made, and not On.
func CheckServerBase(ctx context.Context, line, repo, base string) (ServerBase, error) {
	b := ServerBase{Line: line, Base: base}
	if err := baseNamed(repo, base); err != nil {
		return b, err
	}
	if f, ok := buildinfo.Parse(line); ok {
		b.Commit, b.Why = commitOf(f)
	} else {
		b.Why = "its version line " + strings.TrimSpace(line) + " is not a version line"
	}
	ref := "refs/remotes/origin/" + base
	if _, err := gitIn(ctx, repo, subproc.GitLongBudget, "fetch", "--quiet", "--no-tags", "origin", "+refs/heads/"+base+":"+ref); err != nil {
		return b, fmt.Errorf("fetching origin's %s into %s failed: %w", base, repo, err)
	}
	tip, err := gitIn(ctx, repo, 0, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return b, fmt.Errorf("origin/%s has no tip in %s: %w", base, repo, err)
	}
	b.Tip = tip
	if b.Commit == "" {
		return b, nil
	}
	full, err := gitIn(ctx, repo, 0, "rev-parse", "--verify", "--quiet", "--end-of-options", b.Commit+"^{commit}")
	if err != nil {
		b.Why = "its commit " + b.Commit + " is not in origin/" + base + "'s history, nor in any branch fetched into " + repo
		return b, nil
	}
	b.Commit = full
	_, err = gitIn(ctx, repo, 0, "merge-base", "--is-ancestor", full, tip)
	var ee *exec.ExitError
	switch {
	case err == nil:
		b.On, b.Why = true, ""
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		b.Why = "its commit " + shortSHA(full) + " is not an ancestor of origin/" + base
	default:
		return b, fmt.Errorf("git merge-base --is-ancestor %s origin/%s in %s failed: %w", shortSHA(full), base, repo, err)
	}
	return b, nil
}

// commitOf is the source commit the line names, read from the line alone, in this order: its
// `commit=` extra; the revision of a whole Source; the 12 hex of the vcs stamp in field two.
// commit is "" for a line that names none -- devel, a release tag or a module version with no
// extra -- and for a build from an edited tree, because a dirty build is not the commit it
// names; why then says which, for a refusal to quote. This is buildinfo's own reading, kept
// here because the shared package does not carry it (docs/SPEC-SPRINT.md section 14,
// "server-from-base-only-w-ns-bb.w1").
func commitOf(f buildinfo.Fields) (commit, why string) {
	if c, ok := f.Extra("commit"); ok {
		if rev, dirty := strings.CutSuffix(c, "-dirty"); dirty {
			return "", "built from an edited tree at " + rev + " (commit=" + c + ")"
		}
		if !isHex(c) {
			return "", "its commit=" + c + " is not a commit"
		}
		return c, ""
	}
	if src, ok := f.FindSource(); ok {
		switch {
		case src.Dirty:
			return "", "built from an edited tree at " + src.Revision + " (dirty=true)"
		case !isHex(src.Revision):
			return "", "its revision=" + src.Revision + " is not a commit"
		}
		return src.Revision, ""
	}
	v := f.Version
	if strings.HasSuffix(v, "-dirty") {
		return "", "built from an edited tree (" + v + ")"
	}
	stamp, rev, found := strings.Cut(v, "-")
	if found && len(stamp) == len("20060102150405") && isDigits(stamp) && len(rev) == vcsStampRevisionLen && isHex(rev) {
		return rev, ""
	}
	return "", "its build identity " + v + " names no source commit"
}

// isHex says s is a commit's lowercase hex, 7 to 64 digits (sha1 or sha256).
func isHex(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// baseNamed refuses a check with no base, or no clone, to make it in.
func baseNamed(repo, base string) error {
	if base == "" || strings.HasPrefix(base, "-") || strings.ContainsAny(base, " \t:~^?*[\\") {
		return fmt.Errorf("the sprint base %q is not a branch name", base)
	}
	if repo == "" {
		return errors.New("no clone named to fetch origin's sprint base into")
	}
	return nil
}

// Refusal is what the switch says of a binary not On the base: the commit (or that it names
// none, and why), the base and its tip, and the remedy.
func (b ServerBase) Refusal(binary string) string {
	commit := "no source commit"
	if b.Commit != "" {
		commit = "commit " + shortSHA(b.Commit)
	}
	return fmt.Sprintf("%s was built from %s, not from the sprint base origin/%s (tip %s): %s; the server is built from the base and nothing else; remedy: build nova-sprint from origin/%s at its tip, then run: nova-sprint server switch <that binary>",
		binary, commit, b.Base, shortSHA(b.Tip), b.Why, b.Base)
}

// OffBaseError is a switch refused because the candidate is not built from the sprint base:
// Error is Check.Refusal, which names the commit, the base and the remedy.
type OffBaseError struct {
	Binary string
	Check  ServerBase
}

func (e *OffBaseError) Error() string { return e.Check.Refusal(e.Binary) }

// BaseRecord is the clone and base server switch checked the binary it installed against,
// kept beside the target at <target>.base.json, so the server that binary runs checks
// itself against the same base with nothing more named.
type BaseRecord struct {
	Repo string `json:"repo"`
	Base string `json:"base"`
}

// ReadBaseRecord reads <target>.base.json; ok is false when there is none.
func ReadBaseRecord(target string) (BaseRecord, bool, error) {
	data, err := os.ReadFile(target + ".base.json")
	if errors.Is(err, os.ErrNotExist) {
		return BaseRecord{}, false, nil
	}
	if err != nil {
		return BaseRecord{}, false, err
	}
	var rec BaseRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return BaseRecord{}, false, fmt.Errorf("%s.base.json: %w", target, err)
	}
	return rec, rec.Repo != "" && rec.Base != "", nil
}

// SwitchFromBase is ServerSwitch behind the base check, the switch every verb that installs
// the server binary makes (docs/SPEC-SPRINT.md section 14, "server-from-base-only-w-ns-bb.w1"):
// the candidate's build commit is an ancestor of origin's opts.Base, fetched into opts.Repo, or
// the switch is refused with nothing on disk changed (an *OffBaseError naming the commit, the
// base and the remedy); a check that cannot be made, either unnamed or a fetch that fails, is
// a refusal too. A switch records the clone and base at <target>.base.json (BaseRecord).
func SwitchFromBase(ctx context.Context, opts ServerSwitchOptions) error {
	on, err := CheckServerBinary(ctx, opts.Binary, opts.Repo, opts.Base)
	if err != nil {
		return fmt.Errorf("server switch: the base check of %s could not be made: %w; nothing was changed; remedy: name the sprint base and a clone whose origin holds it (--base <branch> --repo <clone>, or NOVA_SPRINT_BASE and NOVA_SPRINT_SERVER_REPO), then run again", opts.Binary, err)
	}
	if !on.On {
		return &OffBaseError{Binary: opts.Binary, Check: on}
	}
	target, err := switchTarget(opts.Target)
	if err != nil {
		return err
	}
	opts.Target = target
	if err := ServerSwitch(ctx, opts); err != nil {
		return err
	}
	rec, err := json.Marshal(BaseRecord{Repo: opts.Repo, Base: opts.Base})
	if err != nil {
		return fmt.Errorf("server switch: failed to marshal the base record: %w", err)
	}
	if err := os.WriteFile(target+".base.json", rec, 0o644); err != nil {
		return fmt.Errorf("server switch: switched, and the base record %s.base.json was not written: %w", target, err)
	}
	return nil
}

// switchTarget is the target ServerSwitch replaces: the one named, else
// NOVA_SPRINT_SERVER_BIN, else this binary.
func switchTarget(target string) (string, error) {
	if target != "" {
		return target, nil
	}
	if t := os.Getenv("NOVA_SPRINT_SERVER_BIN"); t != "" {
		return t, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("server switch: cannot determine target binary: %w", err)
	}
	return exe, nil
}

// gitIn runs one git in the clone through the tree's git runner; budget 0 is its default.
func gitIn(ctx context.Context, repo string, budget time.Duration, args ...string) (string, error) {
	return gitrun.Output(ctx, gitrun.Options{C: repo, OwnRepo: true, Timeout: budget}, args...)
}

func shortSHA(sha string) string {
	switch {
	case sha == "":
		return "-"
	case len(sha) > 12:
		return sha[:12]
	}
	return sha
}
