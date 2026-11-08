package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"os"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// fileStore is store.Fake kept in a file across the processes of one test
// (the up walk's two phases run as children): every op it took, replayed
// on open. Only the writes and conditions the walk makes are registered.
type fileStore struct {
	*store.Fake
	path string
}

type loggedOp struct {
	Clear bool
	Op    store.Op
}

func init() {
	for _, v := range []any{store.AtSeq{}, store.RowGen{}, store.PutPolicy{}, store.PutRow{}, store.SetActivePolicy{}} {
		gob.Register(v)
	}
}

func openFileStore(path string) (*fileStore, error) {
	s := &fileStore{Fake: store.NewFake(), path: path}
	var log []loggedOp
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err == nil {
		err = gob.NewDecoder(bytes.NewReader(data)).Decode(&log)
	}
	for _, l := range log {
		if err != nil {
			break
		}
		write := s.Fake.Apply
		if l.Clear {
			write = s.Fake.Clear
		}
		_, err = write(context.Background(), l.Op)
	}
	return s, err
}

func (s *fileStore) keep(l loggedOp) error {
	var log []loggedOp
	if data, err := os.ReadFile(s.path); err == nil {
		if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&log); err != nil {
			return err
		}
	}
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(append(log, l)); err != nil {
		return fmt.Errorf("fileStore: %w", err)
	}
	return os.WriteFile(s.path, b.Bytes(), 0o600)
}

func (s *fileStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	r, err := s.Fake.Apply(ctx, op)
	if err == nil {
		err = s.keep(loggedOp{Op: op})
	}
	return r, err
}

func (s *fileStore) Clear(ctx context.Context, op store.Op) (store.Receipt, error) {
	r, err := s.Fake.Clear(ctx, op)
	if err == nil {
		err = s.keep(loggedOp{Clear: true, Op: op})
	}
	return r, err
}

// present is a bus where the names given are on it.
type present []string

func (p present) Who(context.Context) (map[string]bus.Presence, error) {
	out := map[string]bus.Presence{}
	for _, n := range p {
		out[n] = bus.Presence{Name: n}
	}
	return out, nil
}
