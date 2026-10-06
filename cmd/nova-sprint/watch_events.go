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

var (
	appWatchLinesMu sync.Mutex
	appWatchLines   = map[*app]func(context.Context, *store.Store) ([]sprint.Line, error){}
)

func (a *app) setWatchLines(fn func(context.Context, *store.Store) ([]sprint.Line, error)) {
	appWatchLinesMu.Lock()
	defer appWatchLinesMu.Unlock()
	if fn == nil {
		delete(appWatchLines, a)
	} else {
		appWatchLines[a] = fn
	}
}

func (a *app) getWatchLines() func(context.Context, *store.Store) ([]sprint.Line, error) {
	appWatchLinesMu.Lock()
	defer appWatchLinesMu.Unlock()
	return appWatchLines[a]
}

// cmdWatchEvents is watch --events: one shot, the folded log, no bus.
func (a *app) cmdWatchEvents(c common, stdout, stderr io.Writer) int {
	ctx := context.Background()
	st, err := a.store(c)
	if err != nil {
		return refuse(stderr, "watch", err.Error())
	}
	var lines []sprint.Line
	fn := a.getWatchLines()
	if fn != nil {
		lines, err = fn(ctx, st)
	} else {
		lines, err = st.Log(ctx)
	}
	if err != nil {
		return a.readFailed("watch", err, stderr)
	}
	if err := a.markWatch(ctx, st); err != nil {
		fmt.Fprintf(stderr, "%s watch: the event watch was not recorded: %s\n", prog, oneline.Escape(err.Error()))
	}
	fmt.Fprint(stdout, sprint.FormatWatchEvents(lines))
	return 0
}

func (a *app) markWatch(ctx context.Context, st *store.Store) error {
	kv, ok := st.B.(store.KV)
	if !ok {
		return nil
	}
	return kv.SetKey(ctx, bringUpWatchKey, a.bringClock().UTC().Format(time.RFC3339))
}

func (a *app) cmdWatchBringUp(orig func(*app, []string, io.Writer, io.Writer) int, args []string, stdout, stderr io.Writer) int {
	hasEvents := false
	hasWake := false
	for _, arg := range args {
		if arg == "--events" || arg == "-events" {
			hasEvents = true
		}
		if arg == "--wake" || arg == "-wake" {
			hasWake = true
		}
	}
	if hasWake && hasEvents {
		return refuse(stderr, "watch", "--wake and --events are two verbs: run one of them; run: nova-sprint watch --wake")
	}
	if hasEvents {
		var remaining []string
		for _, arg := range args {
			if arg != "--events" && arg != "-events" {
				remaining = append(remaining, arg)
			}
		}
		fs, c := a.verbSetup("watch")
		pos, err := parse(fs, remaining)
		if err != nil || len(pos) > 0 {
			return refuse(stderr, "watch", argErr("takes no words ", err, pos...))
		}
		return a.cmdWatchEvents(*c, stdout, stderr)
	}
	return orig(a, args, stdout, stderr)
}
