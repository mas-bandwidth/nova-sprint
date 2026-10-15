package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Evidence of a friend's work dated after the server's clock counts as the server's now, and
// is logged once per stamp (FriendWorked, the fact the view's friend idle item reports).
func TestFutureDatedEvidenceCountsAsNow(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"))
	now := w.s.Now
	past := now.Add(-time.Minute)
	future := now.Add(48 * time.Hour).UTC().Truncate(time.Second)

	at, what := FriendWorked(w.s, "amy", FriendWork{Active: past})
	assert.True(t, at.Equal(past), "a proof before now is that time, got %s", at)
	assert.Equal(t, "session write", what)
	assert.Equal(t, 0, futureEvidenceLogCount("amy", past))

	at, what = FriendWorked(w.s, "amy", FriendWork{Active: future})
	assert.True(t, at.Equal(now), "a future proof counts as the server's now, got %s", at)
	assert.Equal(t, "session write", what)
	assert.Equal(t, 1, futureEvidenceLogCount("amy", future), "the future stamp is logged once")

	at, what = FriendWorked(w.s, "amy", FriendWork{Active: future})
	assert.True(t, at.Equal(now), "a future proof still counts as now")
	assert.Equal(t, "session write", what)
	assert.Equal(t, 1, futureEvidenceLogCount("amy", future), "the same future stamp is not logged again")

	row := FriendRow("amy")
	w.s.Fleet.Put(&Card{
		ID: "s1-1.w1", Row: row, Col: Working, Rev: 1,
		Fields: map[string]string{"kind": "work", FieldReported: stamp(future.Add(time.Hour))},
	})
	reportAt := future.Add(time.Hour)
	at, what = FriendWorked(w.s, "amy", FriendWork{})
	assert.True(t, at.Equal(now), "a future report counts as the server's now, got %s", at)
	assert.Equal(t, "report", what)
	assert.Equal(t, 1, futureEvidenceLogCount("amy", reportAt), "the future report is logged once")
	at, _ = FriendWorked(w.s, "amy", FriendWork{})
	assert.True(t, at.Equal(now))
	assert.Equal(t, 1, futureEvidenceLogCount("amy", reportAt), "the same future report is not logged again")
}

// futureEvidenceLogCount is how many times friend f's evidence stamped at
// stamped was logged for being after the server's clock. FriendWorked
// logs each such stamp once.
func futureEvidenceLogCount(friend string, stamped time.Time) int {
	v, ok := futureEvidenceOnce.Load(futureEvidenceKey(friend, stamped))
	if !ok {
		return 0
	}
	n, _ := v.(int)
	return n
}
