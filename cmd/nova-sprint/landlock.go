package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/nova-sprint/internal/filelock"
	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

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
// cleanup and scoring (SPEC-SPRINT.md, land-one-lander-now-ns.w1; tla/FileLock.tla).
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
