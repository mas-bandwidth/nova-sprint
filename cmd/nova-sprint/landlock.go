package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/nova-sprint/internal/filelock"
	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// serverRecord is the server's record as this process writes it on its
// heartbeat: its actor, whether it lands, and its pid on its host
// (SPEC-SPRINT.md, land-one-lander-now-nsb.w1).
func (a *app) serverRecord(actor string) store.ServerRecord {
	host, _ := os.Hostname()
	return store.ServerRecord{Actor: actor, Land: a.serverLands, PID: os.Getpid(), Host: host}
}

// serverLanding is why a land must not run beside the server, "" when it may:
// the server's land loop's own pass always may; any other pass is refused
// while a fresh server's record says --land, unless the record names a pid on
// this host that is gone (SPEC-SPRINT.md, land-one-lander-now-nsb.w1).
func (a *app) serverLanding(ctx context.Context, st *store.Store) (string, error) {
	if a.landLazy {
		return "", nil
	}
	r, ok, err := st.Server(ctx)
	if err != nil || !ok || !r.Land {
		return "", err
	}
	if host, _ := os.Hostname(); r.Host == host && r.PID > 0 && !swarm.Alive(r.PID, "") {
		return "", nil
	}
	return fmt.Sprintf("the server is running with --land (actor=%s pid=%d host=%s, its record written %s) and lands every %s itself: nothing was fetched, pushed or reported (let the server land, or restart it without --land); run: nova-sprint where",
		oneline.Field(r.Actor), r.PID, oneline.Field(r.Host), r.At.UTC().Format("15:04:05Z"), LandEvery), nil
}

// landClonePath resolves existing ancestors too, so an absent cached clone and
// a symlinked land root share one lock (SPEC-SPRINT.md, land-one-lander-now-ns.w1).
func landClonePath(dir string) (string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		tail = append(tail, filepath.Base(path))
		path = parent
	}
}

// holdClone keeps kernel ownership until the entire pass has ended, including
// cleanup and scoring (SPEC-SPRINT.md, land-one-lander-now-ns.w1; nova-tools tla/FileLock.tla, the model of internal/filelock).
func (l *lander) holdClone(dir string) (string, string) {
	if l.dry {
		return dir, ""
	}
	canonical, err := landClonePath(dir)
	if err != nil {
		return dir, "cannot resolve clone: " + oneline.Err(err)
	}
	if l.locks[canonical] != nil {
		return canonical, ""
	}
	path := canonical + ".land.lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return canonical, "cannot create lock directory: " + oneline.Err(err)
	}
	old, readErr := filelock.ReadStamp(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return canonical, "cannot read clone lock: " + oneline.Err(readErr)
	}
	verb := "nova-sprint land"
	if l.a.landLazy {
		verb = "nova-sprint run --land"
	}
	held, err := filelock.TryLock(path, verb)
	if err != nil {
		return canonical, oneline.Err(err) + "; wait for its land pass to finish, then run: nova-sprint land"
	}
	if l.locks == nil {
		l.locks = map[string]*filelock.FileLock{}
	}
	l.locks[canonical] = held
	if old.PID > 0 && !swarm.Alive(old.PID, "") {
		w := l.lockLog
		if w == nil {
			w = io.Discard
		}
		fmt.Fprintf(w, "LAND LOCK RECOVERED clone=%s previous=%s\n", oneline.Field(canonical), old.String())
	}
	return canonical, ""
}

func (l *lander) releaseClones() error {
	var errs []error
	for path, held := range l.locks {
		if err := held.Unlock(); err != nil {
			errs = append(errs, fmt.Errorf("clone %s unlock: %w", path, err))
		}
	}
	l.locks = nil
	return errors.Join(errs...)
}
