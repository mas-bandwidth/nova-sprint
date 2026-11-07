// nova-sprint is the layer-six command. Public verbs are wired as their
// packages land; the bus callback is intentionally a separate hidden entry.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// binaryVersion is set by release builds with -ldflags.
var binaryVersion = "dev"

func main() { os.Exit(command(os.Args[1:])) }
func command(args []string) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h" || args[0] == "version") {
		return informational(args)
	}
	if os.Getenv("NOVA_SPRINT_STORE") != "" && len(args) > 0 && (strings.HasPrefix(args[0], "--internal-") || args[0] == "bus-reply") {
		fmt.Fprintln(os.Stderr, "SPRINT REFUSED: internal tools are unavailable in fake mode")
		return 2
	}
	if code, handled := internalCommand(context.Background(), args); handled {
		return code
	}
	if len(args) > 0 && args[0] == "dash" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := executeDash(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == "bus-reply" {
		return busReply()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) > 0 && args[0] == "up" && os.Getenv("NOVA_SPRINT_STORE") == "" {
		return runUpWrapper(ctx, args[1:])
	}
	return executeVerb(ctx, args, "")
}
func busReply() int {
	sender := os.Getenv("NOVA_BUS_FROM")
	id := os.Getenv("NOVA_BUS_ID")
	if sender == "sprint" {
		return 0
	}
	path, err := exec.LookPath("nova-bus")
	if err != nil {
		return 1
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return 1
	}
	self, err := os.Executable()
	if err != nil {
		return 1
	}
	b, err := run.NewNovaBus(run.BusConfig{Binary: path, Redis: os.Getenv("NOVA_BUS_REDIS"), User: os.Getenv("NOVA_BUS_USER"), Password: os.Getenv("NOVA_BUS_PASSWORD"), ReplyExecutable: self})
	if err != nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = b.ReplyTo(ctx, sender, id); err != nil {
		return 1
	}
	return 0
}

func informational(args []string) int {
	version := versionString()
	if len(args) == 1 && args[0] == "version" && version == "dev" {
		fmt.Fprintln(os.Stdout, "dev")
		return 0
	}
	result := (&verbs.Engine{Actor: os.Getenv("NOVA_SPRINT_AS"), Version: version}).Execute(context.Background(), args)
	if _, err := fmt.Fprint(os.Stdout, result.Output); err != nil {
		return 2
	}
	return result.Exit
}
