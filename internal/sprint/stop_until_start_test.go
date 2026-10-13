package sprint_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// TestAStopWithNoUntilStopsUntilTheNextStart pins a stop by hand without --until
// (docs/SPEC-SPRINT.md section 14): stop takes --reason alone, no --until means
// "until the next start", the machine record keeps Until zero, MachineLine and
// StoppedText say "STOPPED until the next start (<reason>)", the machine stays
// STOPPED across hours with moves due, and only explicit start starts it again.
func TestAStopWithNoUntilStopsUntilTheNextStart(t *testing.T) {
	t.Parallel()

	t.Run("stop with no reason is refused with the reason", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2030, 1, 2, 13, 4, 0, 0, time.UTC)
		_, err := sprint.StopArgs("", "", now)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--reason")
		assert.NotContains(t, err.Error(), "--until")

		_, err = sprint.StopArgs("   ", "", now)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--reason")
	})

	t.Run("stop with reason and no until stops until next start", func(t *testing.T) {
		t.Parallel()
		r := newReadyRig(t)
		ctx := context.Background()

		// start the machine first so it is RUNNING
		_, afterStart, _, err := r.st.SetMachine(ctx, true)
		require.NoError(t, err)
		require.True(t, afterStart.Running())
		assert.Equal(t, "machine: running", r.st.MachineLine(ctx))

		// stop --reason x with no --until succeeds
		until, err := sprint.StopArgs("seat requested pause", "", r.clock())
		require.NoError(t, err)
		assert.True(t, until.IsZero(), "until is zero when omitted")

		before, after, res, err := r.st.StopUntil(ctx, "seat requested pause", until)
		require.NoError(t, err)
		assert.True(t, before.Running())
		assert.False(t, after.Running())
		assert.True(t, after.Until.IsZero(), "record keeps until zero")
		assert.Equal(t, "seat requested pause", after.Reason)
		assert.Equal(t, 1, res.Notes, "stop wrote a happened note")

		// MachineLine and StoppedText say "STOPPED until the next start (<reason>)"
		m := r.machine()
		assert.Equal(t, "STOPPED until the next start (seat requested pause)", sprint.StoppedText(m.Who, m.Reason, m.Until, r.clock()))
		assert.Equal(t, "machine: STOPPED until the next start (seat requested pause)", r.st.MachineLine(ctx))

		// moves due on the STOPPED machine
		r.add("s1", 3)

		// simulate hours with moves due: machine stays STOPPED
		for h := 1; h <= 6; h++ {
			r.step(time.Hour)
			r.beat()
			tickRes, err := r.st.Tick(ctx)
			require.NoError(t, err)
			assert.Equal(t, store.Stopped, tickRes.State, "tick %dh: stays STOPPED", h)

			m = r.machine()
			assert.False(t, m.Running(), "hour %d: still not running", h)
			assert.True(t, m.Until.IsZero())
			assert.Equal(t, "machine: STOPPED until the next start (seat requested pause)", r.st.MachineLine(ctx))
		}

		// start runs it
		before, after, _, err = r.st.SetMachine(ctx, true)
		require.NoError(t, err)
		assert.False(t, before.Running())
		assert.True(t, after.Running())
		assert.Equal(t, "machine: running", r.st.MachineLine(ctx))
	})
}
