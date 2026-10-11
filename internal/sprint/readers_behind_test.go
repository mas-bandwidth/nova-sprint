package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNReadersBehindIsNotPushedAgain tests that when the readers-are-behind
// judgment is already open and the condition is unchanged, it is not raised
// again as a new note (it should be updated in place if anything changes, or
// not touched if nothing changes).
func TestNReadersBehindIsNotPushedAgain(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	toReview(w, "s1-1", "s1-2")

	// Set reader states to up
	w.s.ReaderStates = map[string]string{"reader-a": ReaderUp, "reader-b": ReaderUp, "reader-c": ReaderUp}

	// Ask reads - they should be asked of readers up
	w.must(Ask(w.s, AskReq{}))

	// Make readers behind by setting asked time to past the window
	for _, rd := range w.s.Readers.Column(Asked) {
		rd.Fields["asked"] = w.s.Now.Add(-20 * time.Minute).Format("2006-01-02T15:04:05Z")
	}

	// First tick - should raise the judgment
	plan, _ := TickAsk(w.s, TickReq{})
	var raised Note
	for _, n := range plan.Notes {
		if n.Type == NReadersBehind {
			raised = n
			break
		}
	}
	require.NotNil(t, raised, "the readers-are-behind judgment should be raised")

	// Apply the plan to add the judgment to Open
	w.must(plan)

	// Second tick - condition unchanged, should NOT raise again
	plan2, _ := TickAsk(w.s, TickReq{})
	for _, n := range plan2.Notes {
		if n.Type == NReadersBehind {
			t.Errorf("NReadersBehind should not be raised again when condition is unchanged")
		}
	}

	// Third tick - still unchanged
	plan3, _ := TickAsk(w.s, TickReq{})
	for _, n := range plan3.Notes {
		if n.Type == NReadersBehind {
			t.Errorf("NReadersBehind should not be raised again on third tick")
		}
	}
}
