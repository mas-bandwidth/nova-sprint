// nova-sprint is layer 6's command (docs/LAYER-6-RUN.md): the verbs
// (package verbs) over the real store, bus, roster, policy engine and
// edges, opened here once (open.go). Hidden entries: the policy engine's
// child (policy-eval), the bus hook's reply (bus-reply) and the up walk's
// phases (--internal-*).
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/policy"
)

// binaryVersion is set with -ldflags -X main.binaryVersion=<version> by
// the Makefile and CI (git describe --always --dirty --tags) and by the
// release workflow (the tag); toolPin may be set the same way.
var (
	binaryVersion = "dev"
	toolPin       = "v2.0.2"
)

func main() { os.Exit(command(os.Args[1:])) }

func command(args []string) int {
	if len(args) > 0 && args[0] == policy.EvalArg {
		return policy.ServeMain()
	}
	if len(args) == 1 && args[0] == "bus-reply" {
		return busReply()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if code, handled := internalCommand(ctx, args); handled {
		return code
	}
	if len(args) > 0 && args[0] == "up" {
		return runUpWrapper(ctx, args[1:])
	}
	return executeVerb(ctx, args, "")
}

// executeVerb runs one verb on the engine open.go builds; phase is the up
// walk's ("phase1" reports its continuation through the handoff channel).
func executeVerb(ctx context.Context, args []string, phase string) int {
	e, closeAll := newEngine(args, phase)
	defer closeAll()
	r := e.Execute(ctx, args)
	if _, err := fmt.Fprint(os.Stdout, r.Output); err != nil {
		// stdout is gone: the result and why it was lost go to stderr
		fmt.Fprintf(os.Stderr, "nova-sprint: cannot write the result to stdout (%s); it was: %s", edge.RedactN(err.Error(), edge.CommandMax), r.Output)
		return 2
	}
	return r.Exit
}

// busReply answers a message sent to sprint (run's hook names this binary's
// bus-reply): a pointer at the verbs, its body unread.
func busReply() int {
	sender, id := os.Getenv("NOVA_BUS_FROM"), os.Getenv("NOVA_BUS_ID")
	if sender == "" || sender == "sprint" || id == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b := bus.New(bus.Config{Bin: busBinary(), Sender: "sprint", Env: toolEnvironment(os.Environ(), "nova-bus")})
	if _, err := b.Reply(ctx, sender, id, "REPLY", "replies are verbs: run nova-sprint next --as "+sender); err != nil {
		fmt.Fprintln(os.Stderr, "nova-sprint bus-reply: cannot reply to "+edge.Redact(sender)+": "+edge.RedactN(err.Error(), edge.CommandMax))
		return 1
	}
	return 0
}

// busBinary is nova-bus's absolute path, or its bare name when PATH has none.
func busBinary() string {
	path, err := exec.LookPath("nova-bus")
	if err != nil {
		return "nova-bus"
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// versionString is the stamped version, else a source build's
// dev+<12 hex> from its VCS stamp, else "dev" (no stamp at all: a backup
// refuses it, naming the build to make).
func versionString() string {
	if binaryVersion != "dev" {
		return binaryVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return sourceVersion(info.Settings)
	}
	return "dev"
}

// sourceVersion is dev+<first 12 hex of vcs.revision>, with -dirty when
// vcs.modified is not false: a modified tree never claims to be the commit.
func sourceVersion(settings []debug.BuildSetting) string {
	revision, clean := "", false
	for _, s := range settings {
		switch {
		case s.Key == "vcs.revision":
			revision = s.Value
		case s.Key == "vcs.modified" && s.Value == "false":
			clean = true
		}
	}
	if len(revision) < 12 {
		return "dev"
	}
	if !clean {
		return "dev+" + revision[:12] + "-dirty"
	}
	return "dev+" + revision[:12]
}
