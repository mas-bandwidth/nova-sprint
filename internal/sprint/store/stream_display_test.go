package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestStreamClocksCarryTheShownDisplayName pins the StreamClocks read (internal/sprint/store/ops.go,
// docs/SPEC-SPRINT-DASHBOARD.md, Work): a stream's clock's Display is the name the stream is shown
// by (sprint.StreamDisplay), so the page's Work table and the streams view never compute it: the
// display when set; else point-fix-<release>; else the key. The key stays Stream.
func TestStreamClocksCarryTheShownDisplayName(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1}))
	set := func(r sprint.SetReq) {
		r.Who = h.st.Actor
		h.must(SetStep(r))
	}
	clockOf := func(stream string) sprint.StreamClock {
		clocks, err := h.st.StreamClocks(h.ctx)
		require.NoError(t, err)
		for _, c := range clocks {
			if c.Stream == stream {
				return c
			}
		}
		t.Fatalf("no clock for %s", stream)
		return sprint.StreamClock{}
	}

	// neither a display nor a release: its key
	assert.Equal(t, "s1", clockOf("s1").Display)

	// a release alone: point-fix-<release>
	set(sprint.SetReq{Streams: []string{"s1"}, Release: "v1.2.12"})
	assert.Equal(t, "point-fix-v1.2.12", clockOf("s1").Display)

	// a display over the release
	set(sprint.SetReq{Streams: []string{"s1"}, Display: "bus lock"})
	assert.Equal(t, "bus lock", clockOf("s1").Display)
	assert.Equal(t, "v1.2.12", clockOf("s1").Release, "the release is kept beside the display")
	assert.Equal(t, "s1", clockOf("s1").Stream, "the key is kept beside the shown name")

	// default clears the display; the shown name falls back to the release
	set(sprint.SetReq{Streams: []string{"s1"}, Display: "default"})
	assert.Equal(t, "point-fix-v1.2.12", clockOf("s1").Display)
	assert.Equal(t, "v1.2.12", clockOf("s1").Release, "the release is kept")
}
