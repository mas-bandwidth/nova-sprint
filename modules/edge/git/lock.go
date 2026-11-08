package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockPoll is how often a wait for a clone's lock tries again.
const lockPoll = 50 * time.Millisecond

// LockClone is the exclusive lock on the clone at dir: a flock on <dir>.lock
// beside it (closing the file unlocks), so the clone's clone and fetches
// take turns across goroutines and across processes on one state dir
// (concurrent git fetch in one repository fails "cannot lock ref"). A wait
// tries again every lockPoll until ctx ends.
func LockClone(ctx context.Context, dir string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	path := dir + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return f, nil
		case !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR):
			return nil, errors.Join(fmt.Errorf("lock %s: %w", path, err), f.Close())
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(fmt.Errorf("waiting for the lock %s: %w", path, ctx.Err()), f.Close())
		case <-time.After(lockPoll):
		}
	}
}
