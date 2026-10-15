package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A judgment that names a friend no longer on the friends table retires with the
// friend, as a judgment retires with its card, so sprint check's rule 8 stops
// reporting it each tick.
func TestAFriendOffTheTableRetiresItsJudgments(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Friends = []FriendSeat{
		{Name: "aurora", Width: 2, Status: Up},
		{Name: "stella", Width: 2, Status: Up},
		{Name: "freddy", Width: 2, Status: Up},
	}

	const stalledType = "a friend stalled: two wakes unanswered"
	jAurora := Note{ID: "9bfad3ee", Kind: Judgment, Type: stalledType, Primaries: []string{"aurora"}, Count: 1, At: w.s.Now, What: "friend aurora stalled 30m: two wakes unanswered", Decisions: []string{"ack", "wait"}}
	jStella := Note{ID: "26da1b92", Kind: Judgment, Type: stalledType, Primaries: []string{"stella"}, Count: 1, At: w.s.Now, What: "friend stella stalled 30m: two wakes unanswered", Decisions: []string{"ack", "wait"}}
	jFreddy := Note{ID: "a9d81398", Kind: Judgment, Type: stalledType, Primaries: []string{"freddy"}, Count: 1, At: w.s.Now, What: "friend freddy stalled 30m: two wakes unanswered", Decisions: []string{"ack", "wait"}}
	jRow := Note{ID: "f000idle", Kind: Judgment, Type: NFriendIdle, Primaries: []string{FriendRow("aurora")}, Count: 1, At: w.s.Now, What: "friend aurora holds 1 working and finished none in 45m", Decisions: []string{"ack", "wait"}}
	for _, n := range []Note{jAurora, jStella, jFreddy, jRow} {
		w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, n.Primaries[0]), Note: n})
	}
	require.Len(t, w.s.Open, 4)

	// While a friend is on the friends table, its judgment stays open and Check reports no rule-8 violation.
	assert.Empty(t, Check(w.s, nil), "while friends are on the friends table, Check reports no rule-8 violation")
	var pStay Plan
	RetireJudgments(w.s, &pStay, MachineActor)
	assert.Empty(t, pStay.Closes, "while friends are on the friends table, RetireJudgments closes none of their judgments")

	// Remove the friends from the friends table.
	w.s.Friends = []FriendSeat{}

	// Before RetireJudgments runs, Check flags unretired judgments naming removed friends as rule 8.
	beforeViolations := Check(w.s, nil)
	assert.Len(t, beforeViolations, 4, "unretired judgments naming removed friends must be flagged by rule 8 before retirement")

	// Run the tick's judgment retirement.
	p := w.part(TickCheck, TickReq{})
	assert.Len(t, p.Closes, 4, "all four friend judgments must be closed on the tick")
	assert.Empty(t, w.s.Open, "no open judgment remains after the tick")

	decidedByID := map[string]Note{}
	for _, n := range p.Notes {
		if n.Kind == Decided {
			decidedByID[n.Answers] = n
		}
	}
	for _, want := range []struct {
		id     string
		friend string
	}{
		{"9bfad3ee", "aurora"},
		{"26da1b92", "stella"},
		{"a9d81398", "freddy"},
		{"f000idle", "aurora"},
	} {
		d, ok := decidedByID[want.id]
		if assert.True(t, ok, "missing Decided note answering %s", want.id) {
			assert.Equal(t, MachineActor, d.Who)
			assert.True(t, strings.HasPrefix(d.What, RetiredPrefix), "Decided note for %s must start with RetiredPrefix: %q", want.id, d.What)
			assert.Contains(t, d.What, want.friend, "Decided note for %s must name removed friend %s: %q", want.id, want.friend, d.What)
		}
	}

	assert.Empty(t, Check(w.s, nil), "after the tick retires the removed friends' judgments, Check reports no rule-8 violation")
}
