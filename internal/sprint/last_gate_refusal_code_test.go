package sprint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestTheLastGateRefusalCarriesItsCode pins the guard's refusal to a code rather than
// its words: the refusal names the member and says the same thing it always said, and
// another refusal of HoldNames carries no code. The CLI reads the code, so a change of
// the words never changes the exit.
func TestTheLastGateRefusalCarriesItsCode(t *testing.T) {
	t.Parallel()
	s := holdGateSnapshot(map[string]string{"m1": "down", "m2": "up"})
	p := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m2"}, Reason: "r"})
	require.Len(t, p.Refused, 1, "holding m2 with m1 machine-held leaves no bench: %+v", p)
	assert.Equal(t, sprint.RefusedLastGate, p.Refused[0].Code())
	assert.Equal(t, "m2", p.Refused[0].Key)
	assert.Equal(t, "holding m2 leaves no gate bench: every landing will be refused until one is unheld; --force-last holds it anyway", p.Refused[0].Why)

	q := sprint.HoldNames(s, sprint.HoldReq{Reason: "r"})
	require.Len(t, q.Refused, 1, "a hold naming nothing is refused: %+v", q)
	assert.Empty(t, q.Refused[0].Code(), "only the last-gate guard's refusal carries a code")
	assert.Equal(t, "names nothing: name a fleet member, a reader, a friend or a stream", q.Refused[0].Why)
}
