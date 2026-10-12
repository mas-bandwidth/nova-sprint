package refmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The coordinator's rework is a decision on the card, as sprint.Rework records it
// (FieldDecidedAttempt at the old attempt): the model's Rework opens a fresh window of
// attempts, so the next failure is the first of the new window, never past the cap.
// Found by a cold read's probe: the model moved review/attempt 4 to attempt 5 and left
// DecidedAt at 0, so the model held the card at its brief bound where the real sprint did not.
func TestTheCoordinatorsReworkOpensAFreshWindowOfAttempts(t *testing.T) {
	t.Parallel()
	s := New(nil, []string{"m"}, "c")
	s.Members["m"] = Up
	s.Primaries["p"] = Primary{Stream: "s", Kind: KindPrimary, State: Review, Attempt: 4}
	n, err := Rework(s, "p", "m")
	require.NoError(t, err)
	assert.Equal(t, 5, n.Primaries["p"].Attempt)
	assert.Equal(t, 4, n.Primaries["p"].DecidedAt, "the rework decides at the old attempt, as sprint.Rework does")
	assert.False(t, n.AtBriefBound("p", "a different failure"), "the next failure is the first attempt of the new window")
}
