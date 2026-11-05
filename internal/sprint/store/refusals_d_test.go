package store

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/require"
)

func TestRefusalsDOnMem(t *testing.T) {
	t.Parallel()
	t.Run("R35", func(t *testing.T) {
		t.Parallel()
		_, err := ReadOnly(NewMem()).Apply(context.Background(), ntable.BatchManifest{})
		require.ErrorIs(t, err, ErrReadOnly)
		require.EqualError(t, err, ErrReadOnly.Error())
	})
	t.Run("R36", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		_, _, _, err := h.st.StopUntil(h.ctx, "stop", time.Time{})
		require.ErrorIs(t, err, ErrNothingToStop)
		require.EqualError(t, err, ErrNothingToStop.Error())
	})
	t.Run("R37", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m, _, err := h.st.Machine(h.ctx)
		require.NoError(t, err)
		m.RunSeq = ^uint64(0)
		require.NoError(t, h.st.putMachine(h.ctx, m))
		_, _, _, err = h.st.SetMachine(h.ctx, true)
		require.EqualError(t, err, "machine start generation is exhausted; nothing was changed")
	})
}
