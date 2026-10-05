package sprint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
	"github.com/mas-bandwidth/nova-sprint/internal/subproc"
)

// The server is built from the sprint base and nothing else (docs/SPEC-SPRINT.md section 14,
// "server-from-base-only.w3"). On 2026-10-04 the live server ran a binary built from a side
// branch for a day, 1,052 commits off the base one way and 24 the other: two lander designs at
// once. The owner, 2026-10-05: "Prevention is better than cure". So the build commit a binary's
// version line names (internal/buildinfo, Fields.Commit) must be an ancestor of origin's sprint
// base, fetched, by git merge-base --is-ancestor: server switch refuses a binary whose commit is
// not, and the tick keeps one judgment while the running server's commit is not.

// NServerOffBase is the judgment that the running server's build commit is not on origin's
// sprint base, one for the sprint, closed when it is back on.
const NServerOffBase = "the server runs off the sprint base"

// VersionWait bounds `<binary> version`: a version verb answers at once or not at all.
const VersionWait = 10 * time.Second

// ServerBase is one binary's build commit checked against origin's sprint base.
type ServerBase struct {
	Line   string // the binary's version line, as it printed it
	Commit string // the full commit the line names; "" when it names none
	Why    string // with no Commit: why the line names none (buildinfo.Fields.Commit)
	Base   string // the sprint base, a branch on origin
	Tip    string // origin/<Base> as fetched
	On     bool   // Commit is an ancestor of Tip
}

// BinaryLine is the version line `<binary> version` prints, its first line.
func BinaryLine(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, VersionWait)
	defer cancel()
	var out, errs bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, "version")
	cmd.Stdout, cmd.Stderr = &out, &errs
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s version failed: %v: %s", binary, err, strings.TrimSpace(errs.String()))
	}
	line, _, _ := strings.Cut(out.String(), "\n")
	return strings.TrimSpace(line), nil
}

// CheckServerBinary is CheckServerBase over the line `<binary> version` prints: the check
// every verb that installs the server binary makes before anything on disk changes. A binary
// whose version verb fails is a check that could not be made.
func CheckServerBinary(ctx context.Context, binary, repo, base string) (ServerBase, error) {
	if err := baseNamed(repo, base); err != nil {
		return ServerBase{Base: base}, err
	}
	line, err := BinaryLine(ctx, binary)
	if err != nil {
		return ServerBase{Base: base}, err
	}
	return CheckServerBase(ctx, line, repo, base)
}

// CheckServerBase reads the build commit from line, fetches origin's base into repo and asks
// git whether the commit is an ancestor of its tip. An error is a check that could not be
// made (no base named, no clone, a fetch that failed); a line naming no commit, or a commit
// the clone does not hold, is a check made, and not On.
func CheckServerBase(ctx context.Context, line, repo, base string) (ServerBase, error) {
	b := ServerBase{Line: line, Base: base}
	if err := baseNamed(repo, base); err != nil {
		return b, err
	}
	f, ok := buildinfo.Parse(line)
	if !ok {
		b.Why = "its version line " + strings.TrimSpace(line) + " is not a version line"
	} else {
		b.Commit, b.Why = f.Commit()
	}
	ref := "refs/remotes/origin/" + base
	if _, err := gitIn(ctx, repo, "fetch", "--quiet", "origin", "+refs/heads/"+base+":"+ref); err != nil {
		return b, fmt.Errorf("git fetch origin %s in %s failed: %w", base, repo, err)
	}
	tip, err := gitIn(ctx, repo, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return b, fmt.Errorf("origin/%s has no tip in %s: %w", base, repo, err)
	}
	b.Tip = tip
	if b.Commit == "" {
		return b, nil
	}
	full, err := gitIn(ctx, repo, "rev-parse", "--verify", "--quiet", "--end-of-options", b.Commit+"^{commit}")
	if err != nil {
		b.Why = "its commit " + b.Commit + " is in no branch fetched from origin"
		return b, nil
	}
	b.Commit = full
	_, err = gitIn(ctx, repo, "merge-base", "--is-ancestor", "--end-of-options", full, tip)
	var ee *exec.ExitError
	switch {
	case err == nil:
		b.On = true
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		b.Why = "its commit " + shortSHA(full) + " is not an ancestor of origin/" + base
	default:
		return b, fmt.Errorf("git merge-base --is-ancestor %s origin/%s in %s failed: %w", shortSHA(full), base, repo, err)
	}
	return b, nil
}

// baseNamed refuses a check with no base, or no clone, to make it in.
func baseNamed(repo, base string) error {
	if base == "" || strings.HasPrefix(base, "-") || strings.ContainsAny(base, " \t:") {
		return fmt.Errorf("the sprint base %q is not a branch name", base)
	}
	if repo == "" {
		return errors.New("no clone of the repository named to fetch the sprint base into")
	}
	return nil
}

// Refusal is what server switch says of a binary not On the base: the commit (or why there
// is none), the base and its tip, and the remedy.
func (b ServerBase) Refusal(binary string) string {
	commit := "no source commit"
	if b.Commit != "" {
		commit = "commit " + shortSHA(b.Commit)
	}
	return fmt.Sprintf("%s was built from %s, not from the sprint base origin/%s (tip %s): %s; the server is built from the base and nothing else; remedy: build nova-sprint from origin/%s at its tip, then run: nova-sprint server switch <that binary>",
		binary, commit, b.Base, shortSHA(b.Tip), b.Why, b.Base)
}

// What is the judgment's line for a running server not On the base. It names the commit and
// the base and never the tip, so the base moving on is the same episode (notify keys this
// judgment by its line).
func (b ServerBase) What() string {
	commit := "no source commit (" + b.Why + ")"
	if b.Commit != "" {
		commit = "commit " + shortSHA(b.Commit)
	}
	return fmt.Sprintf("the running server was built from %s, not an ancestor of origin/%s; remedy: build nova-sprint from origin/%s at its tip, then run: nova-sprint server switch <that binary>",
		commit, b.Base, b.Base)
}

// TickServerBase is the tick's server-base judgment (docs/SPEC-SPRINT.md section 14,
// "server-from-base-only.w3"), run in TickCheck on the check the store's tick passes in
// (TickReq.ServerBase, from RunningBase): one judgment while the running server's commit is not On the
// base, none while it stands, and closed once it is. A nil check is no fact this tick (none
// made, or one that could not be made): nothing is raised and nothing closed.
func TickServerBase(s *Snapshot, r TickReq, b *ServerBase) (Plan, int) {
	var p Plan
	if b == nil {
		return p, 0
	}
	var conds []cond
	if !b.On {
		conds = append(conds, cond{typ: NServerOffBase, streamLevel: true, what: b.What(),
			decisions: TickDecisions[NServerOffBase]})
	}
	due := notify(&p, s, conds, []string{NServerOffBase}, r)
	return p, due
}

// BaseWatchEvery is how often the running server's commit is checked against origin's base
// again: the commit of a running process never changes, so only the base moving decides it,
// and a fetch every tick (one a second) would load the forge for nothing.
const BaseWatchEvery = 5 * time.Minute

// BaseWait bounds one check of the running server, the fetch and the ancestor test.
const BaseWait = 2 * time.Minute

// BaseWatch is the running server's base check, the fact the tick's judgment is raised and
// closed on (TickReq.ServerBase; docs/SPEC-SPRINT.md section 14, "server-from-base-only.w3").
// Line is the version line of the process that ticks, read in process and never from the file
// on disk, which server switch may already have replaced. A tick never waits on git: Fact
// returns the last check made and starts the next once the last is BaseWatchEvery old, in the
// background; a check that could not be made is no fact.
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

// RunningBase is this process's watch, nil when none is set: the server's binary sets it from
// NOVA_SPRINT_SERVER_REPO and NOVA_SPRINT_BASE (cmd/nova-sprint/server_switch.go), and a nil
// watch is no fact, so a tick with neither named raises nothing.
var RunningBase *BaseWatch

// Fact is the last check made, nil before the first ends or when the last could not be made;
// it starts the next check when none is in flight and the last is Every old.
func (w *BaseWatch) Fact() *ServerBase {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	every := w.Every
	if every <= 0 {
		every = BaseWatchEvery
	}
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

func gitIn(ctx context.Context, repo string, args ...string) (string, error) {
	var out, errs bytes.Buffer
	cmd := subproc.Context(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errs.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	if sha == "" {
		return "-"
	}
	return sha
}
