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
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/subproc"
)

// The server is built from the sprint base and nothing else (docs/SPEC-SPRINT.md section 14,
// "server-from-base-only-w-ns-bb.w1"). On 2026-10-04 the live server ran a binary built from a side
// branch for a day, 1,052 commits off the base one way and 24 the other: two lander designs
// at once. The owner, 2026-10-05: "Prevention is better than cure". So the build commit a
// binary's version line names (buildinfo.Fields.Commit) must be an ancestor of origin's
// sprint base, fetched, by git merge-base --is-ancestor: ServerSwitch refuses a binary whose
// commit is not, or that names none, and the tick keeps one judgment while the running
// server's commit is not.

// NServerOffBase is the judgment that the running server's build commit is not on origin's
// sprint base: one for the sprint, closed by the tick when a check finds it back on.
const NServerOffBase = "the server runs off the sprint base"

// VersionWait bounds `<binary> version`: a version verb answers at once or not at all.
const VersionWait = 10 * time.Second

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
	ctx, cancel := context.WithTimeout(ctx, VersionWait)
	defer cancel()
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
// repo and asks git whether the commit is an ancestor of its tip. An error is a check that
// could not be made (no base or clone named, a fetch that failed); a line naming no commit,
// or a commit the clone does not hold after the fetch (so not in the base's history), is a
// check made, and not On.
func CheckServerBase(ctx context.Context, line, repo, base string) (ServerBase, error) {
	b := ServerBase{Line: line, Base: base}
	if err := baseNamed(repo, base); err != nil {
		return b, err
	}
	if f, ok := buildinfo.Parse(line); ok {
		b.Commit, b.Why = f.Commit()
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

// What is the judgment's line for a running server not On the base. It names the commit and
// the base, never the tip, so the base moving on is the same episode.
func (b ServerBase) What() string {
	commit := "no source commit (" + b.Why + ")"
	if b.Commit != "" {
		commit = "commit " + shortSHA(b.Commit)
	}
	return fmt.Sprintf("the running server was built from %s, not an ancestor of origin/%s; remedy: build nova-sprint from origin/%s at its tip, then run: nova-sprint server switch <that binary>",
		commit, b.Base, b.Base)
}

// TickServerBase is the tick's server-base judgment, run in TickCheck on the check the
// store's tick passes in (TickReq.ServerBase; docs/SPEC-SPRINT.md section 14,
// "server-from-base-only-w-ns-bb.w1"): one judgment while the running server's commit is not On the
// base, none written again while it stands, closed once it is On. A nil check is no fact
// this tick (none made yet, or one that could not be made): nothing raised, nothing closed.
func TickServerBase(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if r.ServerBase == nil {
		return p, 0
	}
	var conds []cond
	if !r.ServerBase.On {
		conds = append(conds, cond{typ: NServerOffBase, streamLevel: true, what: r.ServerBase.What()})
	}
	due := notify(&p, s, conds, []string{NServerOffBase}, r)
	return p, due
}

// SwitchFromBase is ServerSwitch behind the base check, the switch every verb that installs
// the server binary makes (docs/SPEC-SPRINT.md section 14, "server-from-base-only-w-ns-bb.w1"): the
// candidate's build commit is an ancestor of origin's opts.Base, fetched into opts.Repo, or
// the switch is refused with nothing on disk changed (an *OffBaseError naming the commit, the
// base and the remedy); a check that cannot be made, either unnamed or a fetch that fails, is
// a refusal too. A switch records the clone and base at <target>.base.json, so the server it
// installs watches the same base (BaseRecord, RunningBase).
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

// BaseWatchEvery is how often the running server's commit is checked against origin's base
// again: a running process's commit never changes, so only the base moving decides it, and
// a fetch every tick (one a second) would load the forge for nothing.
const BaseWatchEvery = 5 * time.Minute

// BaseWait bounds one check of the running server: the fetch and the ancestor test.
const BaseWait = 2 * time.Minute

// BaseWatch is the running server's base check, the fact the tick's judgment is raised and
// closed on (TickReq.ServerBase). Line is the version line of the process that ticks, read
// in process, never from the file on disk, which server switch may already have replaced.
// A tick never waits on git: Fact returns the last check made and starts the next in the
// background once the last is Every old; a check that could not be made is no fact.
type BaseWatch struct {
	Line, Repo, Base string
	Every            time.Duration // 0 is BaseWatchEvery
	Now              func() time.Time
	// Check is the check made; nil is CheckServerBase over Line, Repo and Base.
	Check func(ctx context.Context) (ServerBase, error)

	mu    sync.Mutex
	last  *ServerBase
	at    time.Time
	busy  bool
	begun bool
	err   error
}

// RunningBase is this process's watch, nil when it has none: the program sets it when it
// starts (cmd/nova-sprint/server_switch.go) from NOVA_SPRINT_SERVER_REPO and
// NOVA_SPRINT_BASE, else from the BaseRecord server switch left beside its binary. A nil
// watch is no fact, and the store's tick raises nothing.
var RunningBase *BaseWatch

// Fact is the last check made, nil before the first ends or when the last could not be
// made; it starts the next check when none is in flight and the last is Every old.
func (w *BaseWatch) Fact() *ServerBase {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	every := w.Every
	if every <= 0 {
		every = BaseWatchEvery
	}
	now := w.now()
	if !w.busy && (!w.begun || now.Sub(w.at) >= every) {
		w.busy, w.begun, w.at = true, true, now
		go w.refresh()
	}
	if w.last == nil {
		return nil
	}
	b := *w.last
	return &b
}

// Wait blocks until no check is in flight, for a caller (a test) that wants the fact it began.
func (w *BaseWatch) Wait(ctx context.Context) error {
	for {
		w.mu.Lock()
		busy := w.busy
		w.mu.Unlock()
		if !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Err is why the last check could not be made, nil when it was.
func (w *BaseWatch) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *BaseWatch) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), BaseWait)
	defer cancel()
	check := w.Check
	if check == nil {
		check = func(ctx context.Context) (ServerBase, error) { return CheckServerBase(ctx, w.Line, w.Repo, w.Base) }
	}
	b, err := check(ctx)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.busy, w.err = false, err
	if err != nil {
		w.last = nil
		return
	}
	w.last = &b
}

func (w *BaseWatch) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
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
