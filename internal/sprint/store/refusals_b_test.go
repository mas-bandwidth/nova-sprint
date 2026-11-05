package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// refusalBackend drives Mem to the refusals whose condition no ordinary Mem
// call reaches: a place that does not hold, and a display sync that fails
// after the step committed. Its AtEpoch keeps the wrapper in place when a
// store is pinned.
type refusalBackend struct {
	Backend
	failPlace bool
	failSync  bool
	committed *bool
}

func (b *refusalBackend) AtEpoch(epoch uint64, old bool) Backend {
	return &refusalBackend{Backend: b.Backend.AtEpoch(epoch, old), failPlace: b.failPlace, failSync: b.failSync, committed: b.committed}
}

func (b *refusalBackend) Place(ctx context.Context, table, row, col, id string, score float64) error {
	if b.failPlace {
		return errors.New("place failed")
	}
	return b.Backend.Place(ctx, table, row, col, id, score)
}

func (b *refusalBackend) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	ok, err := b.Backend.Acquire(ctx, gen, op)
	if ok && b.committed != nil {
		*b.committed = true
	}
	return ok, err
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
			return sprint.Plan{
				Places: []sprint.PlaceAgain{{ID: "control"}},
				Units:  []sprint.Unit{{Key: "probe"}},
			}
		}}
		res, err := h.st.Run(h.ctx, step)
		require.NoError(t, err)
		require.Len(t, res.Refused, 1)
		require.Equal(t, "the step cannot be written, nothing was written: placing control on its cell again did not hold: place failed; the stream's rows are on the table and its control card is not; no card was added; run it again", res.Refused[0].Why)
	})

	t.Run("R20", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		// JSON writes a control byte as six bytes (\u0000), so this string is
		// over MaxOpRecord once the record is marshalled.
		what := strings.Repeat("\x00", MaxOpRecord/6+1)
		step := Step{Verb: "probe", Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{
				Notes: []sprint.Note{{Kind: sprint.Happened, Type: "probe", Stream: "s1", At: s.Now, What: what}},
				Units: []sprint.Unit{{Key: "probe"}},
			}
		}}
		res, err := h.st.Run(h.ctx, step)
		require.NoError(t, err)
		require.Len(t, res.Refused, 1)
		require.Regexp(t, `^the step cannot be written, nothing was written: the step's record is [0-9]+ bytes, over the bound of 268435456 bytes one write to the store takes; nothing was changed; do it in parts \(fewer cards at once\)$`, res.Refused[0].Why)
	})

	t.Run("R21", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.m.Fail = func(point string) error {
			if point == "acquire" {
				return errors.New("acquire failed")
			}
			return nil
		}
		res, err := h.st.Run(h.ctx, AddStep(sprint.AddReq{Stream: "s1", Count: 1}))
		require.NoError(t, err)
		require.Len(t, res.Refused, 1)
		require.Regexp(t, `^the step cannot be written, nothing was written: the store did not take the step's record \([0-9]+ bytes\): acquire: the store did not answer: acquire failed; nothing was changed$`, res.Refused[0].Why)
	})

	t.Run("R28", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		committed := false
		h.st.B = &refusalBackend{Backend: h.m, failSync: true, committed: &committed}
		step := AddStep(sprint.AddReq{Stream: "s1", Count: 1})
		step.Mirrors = true
		res, err := h.st.Run(h.ctx, step)
		var syncErr *SyncError
		require.ErrorAs(t, err, &syncErr)
		require.EqualError(t, syncErr.Cause, "sync failed")
		require.True(t, committed, "the step committed before the display sync failed")
		require.NotEmpty(t, res.Moved, "the step committed before the display sync failed")
	})
}
