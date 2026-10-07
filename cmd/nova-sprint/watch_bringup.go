package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The bring-up and watch wrappers.
// This file's init runs after verbs.go's (which builds verbs), wrapping start,
// check, watch and run with their bring-up behaviors.
func init() {
	for i := range verbs {
		switch verbs[i].name {
		case "start":
			orig := verbs[i].run
			verbs[i].run = func(a *app, args []string, stdout, stderr io.Writer) int {
				return a.cmdStartBringUp(orig, args, stdout, stderr)
			}
		case "check":
			orig := verbs[i].run
			verbs[i].syntax = "[--bring-up]"
			verbs[i].run = func(a *app, args []string, stdout, stderr io.Writer) int {
				return a.cmdCheckBringUp(orig, args, stdout, stderr)
			}
		case "watch":
			orig := verbs[i].run
			verbs[i].syntax = "(--wake [--every <duration>] [--state <file>] [--check <duration>] [--judgment-every <duration>] [--merge-every <duration>] [--backlog-every <duration>] [--land-after <duration>] [--merge-over <n>] [--merging-over <n>] [--review-over <n>] | --events)"
			verbs[i].run = func(a *app, args []string, stdout, stderr io.Writer) int {
				return a.cmdWatchBringUp(orig, args, stdout, stderr)
			}
		case "run":
			orig := verbs[i].run
			verbs[i].run = func(a *app, args []string, stdout, stderr io.Writer) int {
				restore := a.hookRunBringUp(stderr)
				defer restore()
				return orig(a, args, stdout, stderr)
			}
		}
	}
}

// hookRunBringUp makes every pass of this process's run loop run the bring-up
// check, printing nothing. runLoop tells a.ticked after each tick; the store
// is the one run opens. restore puts the backend back. The tick hook stays,
// so a loop already started keeps checking.
func (a *app) hookRunBringUp(stderr io.Writer) (restore func()) {
	prevTick := a.ticked
	prevBackend := a.backend
	var mu sync.Mutex
	var addr string
	var have bool
	if prevBackend != nil {
		a.backend = func(ctx context.Context, got string, names sprint.Names) (store.Backend, error) {
			b, err := prevBackend(ctx, got, names)
			if err == nil && b != nil && got != "" {
				mu.Lock()
				addr, have = got, true
				mu.Unlock()
			}
			return b, err
		}
	}
	a.ticked = func(n int, began time.Time, why string) {
		if prevTick != nil {
			prevTick(n, began, why)
		}
		mu.Lock()
		got, ok := addr, have
		mu.Unlock()
		if !ok {
			return
		}
		st, err := a.store(common{verb: "run", redis: got, actor: sprint.MachineActor})
		if err != nil {
			fmt.Fprintf(stderr, "%s bring-up: %s\n", prog, oneline.Escape(err.Error()))
			return
		}
		a.observeBringUp(context.Background(), st, stderr)
	}
	return func() {
		a.backend = prevBackend
	}
}
