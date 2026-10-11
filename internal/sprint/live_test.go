package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLiveReadsCardsAtGenerationsAndHeldReports(t *testing.T) {
	t.Parallel()
	got, err := ParseLive("a-1@2,b.w1~3@1:held")
	require.NoError(t, err)
	assert.Equal(t, []string{"a-1@2", "b.w1~3@1:held"}, got)
	none, err := ParseLive(LiveNone)
	require.NoError(t, err)
	assert.NotNil(t, none, "the empty set is a set: its row is reconciled")
	assert.Empty(t, none)
	for _, bad := range []string{"", "a", "a@0", "a@-1", "a@01", "a@1:done", "a@1,a@2", "a b@1", "@1"} {
		_, err := ParseLive(bad)
		assert.Error(t, err, bad)
	}
}

func TestLiveCountKeysCountsRunningHeldAndStale(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 18, 25, 0, 0, time.UTC)
	keys := []string{"a@1", "b@2", "c@1", "d@3"}
	b := Beat{At: now.Add(-time.Second), LiveKnown: true, Live: []string{"a@1", "b@2:held", "d@2"}}
	run, held, stale := LiveCountKeys(keys, b, now)
	assert.Equal(t, [3]int{1, 1, 2}, [3]int{run, held, stale}, "d is live at another generation: its take at 3 is stale")
	b.At = now.Add(-2 * BeatDeadline)
	run, held, stale = LiveCountKeys(keys, b, now)
	assert.Equal(t, [3]int{0, 0, 4}, [3]int{run, held, stale}, "a stale beat names nothing live")
	run, held, stale = LiveCountKeys(keys, Beat{At: now}, now)
	assert.Equal(t, [3]int{4, 0, 0}, [3]int{run, held, stale}, "a beat with no live set leaves the table's count")
}

func TestRowBeatIsAFriendsOwnBeat(t *testing.T) {
	t.Parallel()
	beats := map[string]Beat{"zhi": {LiveKnown: true}, "hetzner": {Load: 1}}
	assert.True(t, RowBeat(beats, FriendRow("zhi")).LiveKnown)
	assert.Equal(t, 1.0, RowBeat(beats, "hetzner").Load)
}

func TestNextLiveKeepsSightingsAndTheFirstLiveBeat(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC)
	set, known, since, seen := NextLive(Beat{}, t0, []string{"a@1"})
	require.True(t, known)
	assert.Equal(t, t0, since)
	assert.Equal(t, t0, seen["a@1"])
	prev := Beat{Live: set, LiveKnown: known, LiveSince: since, LiveSeen: seen}
	_, _, since, seen = NextLive(prev, t0.Add(time.Minute), []string{})
	assert.Equal(t, t0, since, "the beats carried a set since the first")
	assert.Equal(t, t0, seen["a@1"], "a card's last sighting is kept after the beats stop naming it")
	_, known, _, seen = NextLive(prev, t0.Add(time.Minute), nil)
	assert.False(t, known, "a beat with no set carries none")
	assert.Nil(t, seen)
	_, _, _, seen = NextLive(prev, t0.Add(liveSeenKeep+time.Second), []string{})
	assert.Nil(t, seen, "a sighting older than the keep is forgotten")
}
