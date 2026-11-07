package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// lazyStore does not connect until a verb actually needs store data.
type lazyStore struct {
	mu            sync.Mutex
	dsn, password string
	pg            *store.PG
}

func (s *lazyStore) get(ctx context.Context) (*store.PG, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pg != nil {
		return s.pg, nil
	}
	if s.password == "" {
		return nil, fmt.Errorf("store unavailable: NOVA_SPRINT_PG_PASSWORD is required")
	}
	if s.dsn == "" {
		return nil, fmt.Errorf("store unavailable: configure NOVA_PG_DSN")
	}
	dsn, err := setupRoleDSN(s.dsn, "nova_sprint")
	if err != nil {
		return nil, fmt.Errorf("store unavailable: configure a passwordless NOVA_PG_DSN")
	}
	p, err := store.Open(ctx, dsn, s.password)
	if err != nil {
		return nil, err
	}
	s.pg = p
	return p, nil
}
func (s *lazyStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pg != nil {
		s.pg.Close()
	}
}
func (s *lazyStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	p, e := s.get(ctx)
	if e != nil {
		return store.Snapshot{}, e
	}
	return p.Snapshot(ctx)
}
func (s *lazyStore) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	p, e := s.get(ctx)
	if e != nil {
		return store.Receipt{}, e
	}
	return p.Apply(ctx, o)
}
func (s *lazyStore) Clear(ctx context.Context, o store.Op) (store.Receipt, error) {
	p, e := s.get(ctx)
	if e != nil {
		return store.Receipt{}, e
	}
	return p.Clear(ctx, o)
}
func (s *lazyStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	p, e := s.get(ctx)
	if e != nil {
		return store.Receipt{}, false, e
	}
	return p.Replay(ctx, id)
}
func (s *lazyStore) Events(ctx context.Context, seq int64, max int) ([]store.Event, error) {
	p, e := s.get(ctx)
	if e != nil {
		return nil, e
	}
	return p.Events(ctx, seq, max)
}
func (s *lazyStore) EventIdentity(ctx context.Context, seq int64) (string, int, bool, error) {
	p, e := s.get(ctx)
	if e != nil {
		return "", 0, false, e
	}
	return p.EventIdentity(ctx, seq)
}
func (s *lazyStore) Restored(ctx context.Context) (time.Time, bool, error) {
	p, e := s.get(ctx)
	if e != nil { return time.Time{}, false, e }
	return p.Restored(ctx)
}
func (s *lazyStore) Policy(ctx context.Context, v int) (store.PolicyVersion, error) {
	p, e := s.get(ctx)
	if e != nil {
		return store.PolicyVersion{}, e
	}
	return p.Policy(ctx, v)
}
