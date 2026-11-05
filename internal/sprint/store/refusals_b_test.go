package store

import (
	"context"
	"errors"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/require"
	"testing"
)

type refusalBackend struct {
	Backend
	failAcquire, failPlace, failSync bool
	committed                        *bool
}

func (b *refusalBackend) AtEpoch(e uint64, old bool) Backend {
	return &refusalBackend{Backend: b.Backend.AtEpoch(e, old), failAcquire: b.failAcquire, failPlace: b.failPlace, failSync: b.failSync, committed: b.committed}
}
func (b *refusalBackend) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	if b.failAcquire {
		return false, errors.New("acquire failed")
	}
	ok, err := b.Backend.Acquire(ctx, gen, op)
	if ok && b.committed != nil {
		*b.committed = true
	}
	return ok, err
}
func (b *refusalBackend) Place(ctx context.Context, table, row, col, id string, score float64) error {
	if b.failPlace {
		return errors.New("place failed")
	}
	return b.Backend.Place(ctx, table, row, col, id, score)
}
func (b *refusalBackend) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	if b.failSync && b.committed != nil && *b.committed {
		return nil, errors.New("sync failed")
	}
	return b.Backend.Shapes(ctx, tables)
}
func TestRefusalsBOnMem(t *testing.T) {
	t.Parallel()
	t.Run("R19", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.st.B = &refusalBackend{Backend: h.m, failPlace: true}
		step := Step{Verb: "probe", Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Places: []sprint.PlaceAgain{{ID: "control"}}, Units: []sprint.Unit{{Key: "probe"}}}
		}}
		res, err := h.st.Run(h.ctx, step)
		require.NoError(t, err)
		require.Len(t, res.Refused, 1)
		require.Equal(t, "the step cannot be written, nothing was written: placing control on its cell again did not hold: place failed; the stream's rows are on the table and its control card is not; no card was added; run it again", res.Refused[0].Why)
	})
	t.Run("R21", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.st.B = &refusalBackend{Backend: h.m, failAcquire: true}
		res, err := h.st.Run(h.ctx, AddStep(sprint.AddReq{Stream: "s1", Count: 1}))
		require.NoError(t, err)
		require.Len(t, res.Refused, 1)
		require.Regexp(t, `^the step cannot be written, nothing was written: the store did not take the step's record \([0-9]+ bytes\): acquire failed; nothing was changed$`, res.Refused[0].Why)
	})
	t.Run("R28", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		committed := false
		h.st.B = &refusalBackend{Backend: h.m, failSync: true, committed: &committed}
		step := AddStep(sprint.AddReq{Stream: "s1", Count: 1})
		step.Mirrors = true
		_, err := h.st.Run(h.ctx, step)
		var syncErr *SyncError
		require.ErrorAs(t, err, &syncErr)
		require.EqualError(t, syncErr.Cause, "sync failed")
	})
}
