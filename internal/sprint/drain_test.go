package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A fresh beat that says it drains is a drain's no-room word (its own word after it), a stale
// one says nothing, and the drain's start is stamped at its first beat and kept after it.
func TestADrainingBeatIsADrainsWord(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	since := now.Add(-time.Hour)
	b := Beat{At: now, Draining: true, DrainingSince: since}
	assert.Equal(t, DrainWord(since), NoRoomNow(b, now))
	assert.True(t, IsDrainWord(NoRoomNow(b, now)))
	b.NoRoom = "free disk under the floor"
	assert.Equal(t, DrainWord(since)+"; free disk under the floor", NoRoomNow(b, now))
	assert.Empty(t, NoRoomNow(b, now.Add(2*BeatDeadline)), "a stale beat says nothing")
	assert.False(t, IsDrainWord("free disk under the floor"))

	s := &Snapshot{NoRoom: map[string]string{"m1": DrainWord(since)}}
	assert.True(t, s.Draining("m1"))
	assert.Equal(t, []string{"m2"}, notQuiet(s, []string{"m1", "m2"}), "a draining member is out of the deal")

	on, at := DrainingFrom(Beat{}, true, now)
	assert.True(t, on)
	assert.Equal(t, now, at, "the first drain beat stamps now")
	on, at = DrainingFrom(Beat{Draining: true, DrainingSince: since}, true, now)
	assert.True(t, on)
	assert.Equal(t, since, at, "a later drain beat keeps the stamp")
	on, at = DrainingFrom(Beat{Draining: true, DrainingSince: since}, false, now)
	assert.False(t, on)
	assert.True(t, at.IsZero(), "a beat that does not say it clears it")
}
