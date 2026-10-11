package main

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAReadAskedThroughTheReviewPathBeginsAtTheGenerationItsQueueNames: a read the review
// path asks (sprint.Ask: the tick's ask while read cards are off) is handed to its reader at
// generation 1, in the queue's card and in its packet, and the reader's begin naming
// <id>@<gen> as it was handed is taken. The review path wrote no gen, the queue handed 0, and
// the begin of <id>@0 was refused ("a generation is a whole number from 1"): the reads never
// began and their primaries sat in review (2026-10-11).
func TestAReadAskedThroughTheReviewPathBeginsAtTheGenerationItsQueueNames(t *testing.T) {
	t.Parallel()
	r := askedRig(t, 2)
	q := r.queueWith("--as", "r", "--json")
	require.Len(t, q.Cards, 2)
	words := []string{"read", "--as", "r", "--begin"}
	for _, c := range q.Cards {
		require.Equal(t, "asked", c.Col, c.ID)
		assert.Equal(t, 1, c.Gen, "%s: the queue hands a read at generation 1", c.ID)
		var p struct {
			Gen int `json:"gen"`
		}
		require.NoError(t, json.Unmarshal(c.Packet, &p), c.ID)
		assert.Equal(t, 1, p.Gen, "%s: its packet names generation 1", c.ID)
		words = append(words, c.ID+"@"+strconv.Itoa(c.Gen))
	}
	res := r.one(append(words, "--epoch", "0")...)
	require.Equal(t, 0, res.Code, "the begin naming the generation the queue handed is taken: %s", res.Stderr)
	assert.Len(t, r.queueWith("--as", "r", "--json").ids("reading"), 2)
}
