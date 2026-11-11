package main

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// run finds the board's reads on its store by interface, so the store the
// binary opens must forward them, or the board loses them without a word
// (2026-11-07: no ETA): the landings and the stats mark.
func TestLazyStoreForwardsTheBoardReads(t *testing.T) {
	t.Parallel()
	var s store.Store = &lazyStore{}
	if _, ok := s.(interface {
		Landings(ctx context.Context, epoch int, upToSeq int64) ([]time.Time, error)
	}); !ok {
		t.Error("lazyStore does not forward Landings")
	}
	if _, ok := s.(interface {
		StatsSince(ctx context.Context, mark int64) (store.StatsMark, error)
	}); !ok {
		t.Error("lazyStore does not forward StatsSince")
	}
}
