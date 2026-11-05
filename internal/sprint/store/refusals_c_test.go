package store

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

func TestRefusalsCOnMem(t *testing.T) {
	t.Parallel()
	t.Run("R29", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.m.Fail = func(point string) error {
			if point == "acquire" {
				return errors.New("injected")
			}
			return nil
		}
		_, err := h.st.Run(h.ctx, Step{Verb: "probe", Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: "probe", At: s.Now}}}
		}})
		require.Error(t, err)
		require.Regexp(t, regexp.MustCompile(`^the step cannot be written, nothing was written: the store did not take the step\x27s record \([0-9]+ bytes\): acquire: the store did not answer: injected; nothing was changed$`), err.Error())
	})
	t.Run("R30", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		m := NewMem()
		moved, err := m.AdvanceEpoch(ctx, 0, time.Time{})
		require.NoError(t, err)
		require.True(t, moved)
		for _, tc := range []struct {
			backend Backend
			outcome GuardOutcome
			message string
		}{
			{m.AtEpoch(0, false), GuardMoved, "the sprint left the epoch the delete pinned"},
			{m.AtEpoch(1, false), GuardPending, "the epoch\x27s restore is pending"},
		} {
			_, err := tc.backend.RowsDelIf(ctx, "fleet", nil)
			var guard *GuardError
			require.ErrorAs(t, err, &guard)
			require.Equal(t, tc.outcome, guard.Outcome)
			require.EqualError(t, err, tc.message)
		}
	})
	t.Run("R32", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		st := &Store{B: NewMem()}
		id := sprint.StoredID("judgment", 1)
		err := st.SetReview(ctx, id, time.Time{})
		require.EqualError(t, err, sprint.OtherEpoch(id, 1, 0))
	})
}
