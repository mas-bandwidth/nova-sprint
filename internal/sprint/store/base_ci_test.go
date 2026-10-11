package store

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// The store passes the base's CI facts the binding read into the tick's TickReq
// (Store.BaseCI; base_ci.go): the machine's tick raises one judgment per red
// (Repo, Base), shown in the coordinator's inbox; with BaseCI nil the tick
// raises none.
func TestTheStorePassesTheBaseCIFactsToTheTick(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.startMachine()
	h.st.BaseCI = func() []sprint.BaseCIFacts {
		return []sprint.BaseCIFacts{{
			Repo:     "mas-bandwidth/nova-tools",
			Base:     "main",
			Tip:      "1111111111111111111111111111111111111111",
			Red:      true,
			FirstRed: "2222222222222222222222222222222222222222",
			Failed:   []string{"internal/ci TestHarnessWatchIsWired"},
		}}
	}
	h.machine()
	open := h.openOf(sprint.NBaseCIRed)
	require.Len(t, open, 1, "the store passes the facts: %+v", open)
	require.Equal(t, "mas-bandwidth/nova-tools@main", open[0].Subject(), "the judgment names the repo and base")
	require.Contains(t, open[0].Note.What, "TestHarnessWatchIsWired", "and the failing check")
	require.Contains(t, open[0].Note.What, "222222222222", "and the first red commit")

	v, err := h.st.Inbox(h.ctx, time.Hour, 0, 100)
	require.NoError(t, err)
	found := false
	for _, g := range v.Groups {
		if g.Type == sprint.NBaseCIRed {
			found = true
			require.Equal(t, sprint.Judgment, g.Kind, "the judgment is the coordinator's: %+v", g)
		}
	}
	require.True(t, found, "the red-base judgment is in the inbox: %+v", v.Groups)

	// BaseCI nil: no facts read, and no judgment raised
	h2 := newHarness(t)
	h2.startMachine()
	h2.machine()
	require.Empty(t, h2.openOf(sprint.NBaseCIRed), "with BaseCI nil the tick raised a judgment")
}
