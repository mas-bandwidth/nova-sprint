package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQueueNamesAnUnknownReaderAndRefusesMax pins two findings on the queue verb's
// --as answer (docs/SPEC-SPRINT.md section 6: a reader's own queue is its beat, so a
// name that is no reader's row writes no beat and answers `reader: false`). The text
// answer named neither, so a reader loop reading `QUEUE OK cards=0 epoch=N` could not
// tell an unknown name from a reader with no work; it now carries reader=<bool>, as
// --json already did. And --max, a cap of rows that would cut a queue packet (a card
// with its job text) in half, was registered and ignored: queue has a packet limiter
// (--packets), so the flag is refused with the packet remedy rather than removed.
func TestQueueNamesAnUnknownReaderAndRefusesMax(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")

	// a name that is no reader's row: the answer says so, and the OK stays (the verb ran)
	unknown := ta.ok("queue --as nosuch-ai")
	assert.Contains(t, unknown, "QUEUE OK", "the verb ran and said so: %s", unknown)
	assert.Contains(t, unknown, "reader=false", "an unknown --as name is no reader row: %s", unknown)

	// a reader's row: the answer says true, as --json already did
	assert.Contains(t, ta.ok("queue --as reader-a"), "reader=true", "a reader row is named")

	var q struct {
		Reader *bool `json:"reader"`
	}
	ta.json("queue --as nosuch-ai", &q)
	require.NotNil(t, q.Reader, "--json names whether --as is a reader row")
	assert.False(t, *q.Reader, "--json: an unknown name is no reader row")
	ta.json("queue --as reader-a", &q)
	require.NotNil(t, q.Reader, "--json names whether --as is a reader row")
	assert.True(t, *q.Reader, "--json: a reader row is named")

	// --max is refused: queue lists packets, not rows, and the remedy names --packets
	const remedy = "queue lists packets, not rows: pass --packets <n> to take fewer, or --json to read them whole"
	for _, line := range []string{"queue --as m1 --max 1", "queue --as reader-a --max 1"} {
		code, out, errs := ta.do(line)
		assert.Equal(t, 2, code, "%s refuses --max: %s%s", line, out, errs)
		assert.Empty(t, out, "%s is refused before the answer is written", line)
		assert.Contains(t, errs, remedy, "%s names the packet remedy", line)
	}
	// the same refusal asked for as --json is the one JSON refusal object on
	// stdout with an empty stderr, its reason the same words
	code, out, errs := ta.do("queue --as reader-a --json --max 1")
	assert.Equal(t, 2, code, "%s%s", out, errs)
	assert.Empty(t, errs, "a refusal under --json is JSON on stdout: %s", out)
	assert.Contains(t, out, `"status":"refused"`)
	assert.Contains(t, out, "queue lists packets, not rows")

	// a queue without --max still runs, with or without --json
	for _, line := range []string{"queue --as m1", "queue --as m1 --json"} {
		code, out, errs := ta.do(line)
		assert.Equal(t, 0, code, "%s runs: %s%s", line, out, errs)
		assert.NotEmpty(t, out, "%s answers", line)
	}
}
