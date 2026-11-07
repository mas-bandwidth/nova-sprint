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
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/run"
)

// binaryVersion is set by release builds with -ldflags.
var binaryVersion = "dev"

func main() { os.Exit(command(os.Args[1:])) }
func command(args []string) int {
	if len(args) > 0 && args[0] == "dash" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := dash.Command(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == "bus-reply" {
		return busReply()
	}
	fmt.Fprintln(os.Stderr, "SPRINT REFUSED: public command integration is not yet available: use the released nova-sprint")
	return 2
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
