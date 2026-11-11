// Package verbs is layer 6's command line (docs/LAYER-6-RUN.md section 3):
// every verb of nova-sprint, each with its prerequisite table, its one-line
// refusals naming the fix, and its `next:` line. One table names the verbs
// (table.go), one parser reads their flags (flags.go), one check says who
// may act (hooked.go), one path applies an op (apply.go), and one form
// prints a refusal (result.go). The processes behind run, worker and dash
// are the binary's (cmd/nova-sprint), reached through Services.
package verbs

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Engine is the verbs and what they read and write. The binary builds one
// per process (cmd/nova-sprint, open.go); tests build one over store.Fake
// and fakes of the rest. A nil dependency a verb needs is an exit 2 naming
// it, never a panic.
type Engine struct {
	Store    store.Store // opened on first use
	Bus      Presence    // nova-bus who
	Roster   Roster      // nova-config, at the active policy's width cap
	Answers  Answerer    // the active policy's limits and timeouts
	Policy   *policy.Engine
	Origin   Origin   // a stream's repository at origin
	Lander   Lander   // the hand landing and the group branch
	Forge    Forge    // the host's gh identity, as admission proves it
	Doctor   Doctor   // nova-fleet, nova-tools and the store's schema
	Archive  Archive  // backup and restore
	Services Services // run, worker and dash
	Setup    Setup    // up's steps
	Lock     SetupLock

	// ReportContinuation, SetupObserver: the up walk's handoff and trace (setup.go).
	ReportContinuation func(context.Context, string, SetupInput) error
	SetupObserver      func(SetupObservation)

	Now     func() time.Time
	Stdin   io.Reader // wake's bus line
	Actor   string    // NOVA_SPRINT_AS
	Version string    // this build
	ToolPin string    // the lowest nova-tools it runs on
}

// Presence is who is on the bus (edge/bus's Who).
type Presence interface {
	Who(ctx context.Context) (map[string]bus.Presence, error)
}

// Roster reads nova-config at a width cap (run.ConfigRoster).
type Roster interface {
	ReadRosterAt(ctx context.Context, widthCap int) (roster.Roster, error)
}

// Answerer is the policy engine as the verbs ask it: limits and timeouts
// (policy.Engine).
type Answerer interface {
	AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure)
}

// Origin is a stream's repository as the verbs check it (layer 4's git
// edge, adapted in cmd/nova-sprint): the tip of a branch at the clone
// address (git.CloneURL of the stream's repo and forge).
type Origin interface {
	Tip(ctx context.Context, url, branch string) (sha string, ok bool, err error)
	Contains(ctx context.Context, url, ancestor, head string) (bool, error)
	// Tree is every file path at commit sha (git ls-tree -r --name-only).
	Tree(ctx context.Context, url, sha string) ([]string, error)
	// File is the text of the file at path at commit sha.
	File(ctx context.Context, url, sha, path string) (string, error)
	// Branches is every branch at origin under prefix (a/b/ lists a/b/*),
	// by name, with its tip: one ls-remote.
	Branches(ctx context.Context, url, prefix string) (map[string]string, error)
	// Commits is every commit on head that is neither on the base branch at
	// origin nor reachable from since ("" none), with its author and
	// committer: finish's check of the sprint's commit identity.
	Commits(ctx context.Context, url, head, base, since string) ([]git.Commit, error)
}

// Doctor is what `check` and `next` look at outside the store.
type Doctor interface {
	Fleet(ctx context.Context) error                    // nil when nova-fleet check is green; else a *Fail naming its fix
	ToolsVersion(ctx context.Context) (string, error)   // nova-tools' release
	Schema(ctx context.Context) (bool, error)           // the store at this build's schema
	History(ctx context.Context) error                  // the event log rebuilds the tables (read only)
	Run(ctx context.Context) (run.ChooserStatus, error) // who holds the chooser lock (show run)
	// Workers is every worker bench's service (the benches in nova-config's
	// inventory, each unit's last log line); Worker is machine's, false
	// when it is no bench (check_workers.go).
	Workers(ctx context.Context) ([]WorkerUnit, error)
	Worker(ctx context.Context, machine string) (WorkerUnit, bool, error)
}

// Services are the long-running verbs, the binary's.
type Services interface {
	Run(ctx context.Context, listen string) error
	Worker(ctx context.Context, w WorkerArgs) error
	Dash(ctx context.Context, listen, pull string) error
}

// WorkerArgs are `nova-sprint worker`'s flags: the worker's own Config
// (layer 4), so this verb and nova-worker are one worker.
type WorkerArgs = worker.Config

// Execute runs one command line. No arguments is `next` (CORRECT-USE: the
// tool's name alone says what to do now).
func (e *Engine) Execute(ctx context.Context, args []string) Result {
	if len(args) == 0 {
		args = []string{"next"}
	}
	v, rest := lookup(args)
	if v == nil {
		return failed("nova-sprint", broken("unknown verb "+args[0], "nova-sprint help"))
	}
	in, f := parse(v, rest, e.Actor)
	if f != nil {
		// a verb that acts as a name refuses an absent name before its usage
		if in != nil && v.actsAsName() {
			if err := e.onBus(ctx, in.actor); err != nil {
				return failed(v.name, classify(err))
			}
		}
		return failed(v.name, f)
	}
	out, err := e.call(ctx, v, in)
	if err != nil {
		if errors.Is(err, ErrNotSetUp) {
			return failed(v.name, notSetUp(in.actor))
		}
		if errors.Is(err, ErrNoStore) {
			return failed(v.name, NoStore(in.actor))
		}
		return failed(v.name, classify(err))
	}
	return Result{Output: out, Exit: exitDone}
}

func (e *Engine) call(ctx context.Context, v *verb, in *input) (string, error) {
	if v.role == roleWorker || v.role == roleCoordinator {
		snap, err := e.hooked(ctx, in, v.role, v.scope(in))
		if err != nil {
			return "", err
		}
		if err = epochNow(in.epoch(), snap); err != nil {
			return "", err
		}
		out, err := v.run(e, ctx, in, snap)
		if err != nil {
			return "", err
		}
		// every hooked verb's output tells the coordinator its pushes go unseen
		return e.withUnseen(ctx, in, out), nil
	}
	return v.run(e, ctx, in, store.Snapshot{})
}

func failed(verb string, f *Fail) Result {
	return Result{Output: f.Detail + f.line(verb), Exit: f.Exit}
}
