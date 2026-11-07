// Package filestore provides a file-backed Fake for local manual testing.
// It does not provide PostgreSQL durability or production performance.
package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type Store struct{ path string }
type disk struct {
	Version  int            `json:"version"`
	Snapshot store.Snapshot `json:"snapshot"`
	Events   []store.Event  `json:"events"`
}

var _ store.Store = (*Store)(nil)

// Open validates the current file, if present. The parent directory must exist.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("filestore: empty path")
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s := &Store{path: p}
	err = s.withLock(context.Background(), func() error { _, err := s.read(); return err })
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) withLock(ctx context.Context, fn func() error) error {
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("filestore: open lock: %w", err)
	}
	defer f.Close()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return fmt.Errorf("filestore: lock: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func (s *Store) read() (disk, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return disk{Version: 1, Snapshot: store.Rebuild(nil)}, nil
	}
	if err != nil {
		return disk{}, fmt.Errorf("filestore: read: %w", err)
	}
	var d disk
	if err = json.Unmarshal(b, &d); err == nil {
		err = validate(d)
	}
	if err != nil {
		return disk{}, fmt.Errorf("filestore: invalid state in %s: %w; restore a valid backup or move the file aside to start fresh", s.path, err)
	}
	return d, nil
}

func validate(d disk) error {
	if d.Version != 1 {
		return fmt.Errorf("unsupported format version %d", d.Version)
	}
	seen := map[string]bool{}
	epoch := 1
	for i, e := range d.Events {
		if e.Seq != int64(i+1) || e.Op == "" || seen[e.Op] || e.Epoch != epoch || !consistent(e) {
			return fmt.Errorf("invalid event at index %d", i)
		}
		seen[e.Op] = true
		if e.Accepted && e.After != nil && e.After.Epoch != 0 {
			if e.After.Epoch != epoch+1 {
				return fmt.Errorf("invalid epoch at index %d", i)
			}
			epoch++
		}
	}
	if !reflect.DeepEqual(store.Rebuild(d.Events), d.Snapshot) {
		return errors.New("snapshot does not match event history")
	}
	_, err := store.NewFakeFrom(d.Snapshot, policies(d.Events)...)
	return err
}

func consistent(e store.Event) bool {
	r := e.Receipt
	if r.Op != e.Op || r.Seq != e.Seq || r.Accepted != e.Accepted || r.Actor != e.Actor || r.Kind != e.Kind || r.Reason != e.Reason || !r.At.Equal(e.At) || !reflect.DeepEqual(r.After, e.After) {
		return false
	}
	if !e.Accepted {
		return e.Before == nil && e.After == nil && r.NewEpoch == 0 && e.Reason != ""
	}
	if e.Reason != "" {
		return false
	}
	if e.After == nil {
		return r.NewEpoch == 0
	}
	return r.NewEpoch == e.After.Epoch
}

func policies(events []store.Event) []store.PolicyVersion {
	m := map[int]store.PolicyVersion{}
	for _, e := range events {
		if e.Accepted && e.After != nil {
			for k, p := range e.After.Policies {
				m[k] = p
			}
		}
	}
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]store.PolicyVersion, 0, len(m))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func (s *Store) save(d disk) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".nova-sprint-fake-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, s.path); err != nil {
		return fmt.Errorf("%w: filestore rename: %v", store.ErrUnknown, err)
	}
	return nil
}

func (s *Store) mutate(ctx context.Context, op store.Op, clear bool) (r store.Receipt, err error) {
	err = s.withLock(ctx, func() error {
		d, e := s.read()
		if e != nil {
			return e
		}
		for _, event := range d.Events {
			if event.Op == op.ID {
				r = event.Receipt
				return nil
			}
		}
		f, e := store.NewFakeFrom(d.Snapshot, policies(d.Events)...)
		if e != nil {
			return e
		}
		if clear {
			r, e = f.Clear(ctx, op)
		} else {
			r, e = f.Apply(ctx, op)
		}
		if e != nil {
			return e
		}
		events, e := f.Events(ctx, d.Snapshot.Seq, 1)
		if e != nil {
			return e
		}
		if len(events) != 1 {
			return errors.New("filestore: fake produced no event")
		}
		d.Events = append(d.Events, events[0])
		d.Snapshot, e = f.Snapshot(ctx)
		if e != nil {
			return e
		}
		return s.save(d)
	})
	return
}
func (s *Store) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	return s.mutate(ctx, op, false)
}
func (s *Store) Clear(ctx context.Context, op store.Op) (store.Receipt, error) {
	return s.mutate(ctx, op, true)
}
func (s *Store) Snapshot(ctx context.Context) (out store.Snapshot, err error) {
	err = s.withLock(ctx, func() error { d, e := s.read(); out = d.Snapshot; return e })
	return
}
func (s *Store) Replay(ctx context.Context, id string) (out store.Receipt, found bool, err error) {
	err = s.withLock(ctx, func() error {
		d, e := s.read()
		if e != nil {
			return e
		}
		for _, v := range d.Events {
			if v.Op == id {
				out, found = v.Receipt, true
				break
			}
		}
		return nil
	})
	return
}
func (s *Store) EventIdentity(ctx context.Context, seq int64) (op string, epoch int, found bool, err error) {
	err = s.withLock(ctx, func() error {
		d, e := s.read()
		if e != nil {
			return e
		}
		for _, v := range d.Events {
			if v.Seq == seq {
				op, epoch, found = v.Op, v.Epoch, true
				break
			}
		}
		return nil
	})
	return
}
func (s *Store) Events(ctx context.Context, after int64, max int) (out []store.Event, err error) {
	err = s.withLock(ctx, func() error {
		d, e := s.read()
		if e != nil {
			return e
		}
		if max <= 0 || max > store.MaxEvents {
			max = store.MaxEvents
		}
		for _, v := range d.Events {
			if v.Seq > after {
				out = append(out, v)
				if len(out) == max {
					break
				}
			}
		}
		return nil
	})
	return
}
func (s *Store) Policy(ctx context.Context, version int) (out store.PolicyVersion, err error) {
	err = s.withLock(ctx, func() error {
		d, e := s.read()
		if e != nil {
			return e
		}
		for _, p := range policies(d.Events) {
			if p.Version == version {
				out = p
				return nil
			}
		}
		return fmt.Errorf("%w: policy version %d", store.ErrNotFound, version)
	})
	return
}

// AcquireChooser grants a nonblocking process-lifetime lease, independent of
// transaction locks. Close releases it; the OS also releases it on crash.
func AcquireChooser(path string) (*os.File, error) {
	if path == "" {
		return nil, errors.New("filestore: empty path")
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p+".chooser.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("filestore: chooser lock: %w", err)
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("filestore: cannot acquire chooser lock for %s; stop the other chooser or check lock-file permissions: %w", p, err)
	}
	return f, nil
}
