package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A merge --note over the card text bound is refused at the verb, loudly and whole,
// before it can be written into a card's why and poison a tick step the store refuses
// on every tick (2026-10-11: a 40 KiB lander note froze the drain for twenty minutes).
func TestMergeNoteOverTheBoundIsRefusedAtTheVerb(t *testing.T) {
	t.Parallel()
	w := stoppedForConflict(t)
	note := strings.Repeat("x", MaxCardTextBytes+1)
	p := MergeStep(w.s, MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-3", Note: note, Who: "lander"})
	require.Len(t, p.Refused, 1)
	require.Equal(t, "s1", p.Refused[0].Key)
	require.Contains(t, p.Refused[0].Why, "--note is 65537 bytes, over the bound of 65536 bytes")
	require.Contains(t, p.Refused[0].Why, "nothing was changed")
	require.Empty(t, p.Units, "a refused note writes nothing")
}

// The bound is the one the store enforces on a card's text fields: 64 KiB.
func TestCardTextBoundIs64KiB(t *testing.T) {
	t.Parallel()
	require.Equal(t, 64<<10, MaxCardTextBytes)
}
