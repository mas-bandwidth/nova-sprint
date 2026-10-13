package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A small live backlog keeps these retirement tests running while frontier waits.
func orphanJudgmentFundedWork(h *harness) {
	h.t.Helper()
	for _, member := range []string{"m1", "m2"} {
		h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: member, Width: 1}))
	}
	h.m.SetProviders(map[string]sprint.Provider{"prov-flash-a": {Name: "prov-flash-a", Billing: "metered", Funded: true}, "prov-flash-b": {Name: "prov-flash-b", Billing: "metered", Funded: true}, "prov-pro-a": {Name: "prov-pro-a", Billing: "metered", Funded: true}, "prov-pro-b": {Name: "prov-pro-b", Billing: "metered", Funded: true}})
	h.addReady("funded-control", 2*sprint.DealAhead+1, briefOf("pro", ""))
}

// A dropped card's judgment retires on a sparse tick (docs/SPEC-SPRINT.md section 8,
// "A judgment retires with its card"): the tick loads the cards its open judgments
// name, sees the dropped card off the table, and retires the judgment with the decided
// note, rather than leaving it open for hours.
func TestADroppedCardsJudgmentRetiresOnASparseTick(t *testing.T) {
	t.Parallel()
	h := flashAndPro(t)
	orphanJudgmentFundedWork(h)
	h.addReady("s1", 1, "tier: frontier\n\nThe task.\n")
	h.startMachine()
	h.machine()

	open := h.openOf(sprint.NNoRoute)
	require.Len(t, open, 1, "NNoRoute is raised on tier:frontier")
	jid := open[0].Note.ID
	assert.Equal(t, sprint.StreamSubject(sprint.TierSubject("frontier")), open[0].Subject())
	assert.Equal(t, []string{"s1-1"}, open[0].Note.Primaries)

	// A second frontier card keeps tier:frontier unserved, so TickDeal does not close the tier's judgment.
	h.addReady("s1", 1, "tier: frontier\n\nSecond task.\n")

	h.must(DropStep(sprint.DropReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Reason: "landed by hand"}))
	h.machine()

	// At BASE, s1-1 is not loaded by tickExtras, so offTable sees nil and the judgment stays open.
	for _, o := range h.openOf(sprint.NNoRoute) {
		if o.Note.ID == jid {
			require.Fail(t, fmt.Sprintf("judgment %s on dropped card s1-1 must retire on sparse tick, but remained open: %+v", jid, o))
		}
	}

	notes, _, err := h.m.NotesSince(h.ctx, "", 1000)
	require.NoError(t, err)
	var decided *sprint.Note
	for _, n := range notes {
		if n.Kind == sprint.Decided && n.Answers == jid {
			decided = &n
			break
		}
	}
	require.NotNil(t, decided, "no decided note answering %s", jid)
	assert.Equal(t, sprint.MachineActor, decided.Who)
	assert.True(t, strings.HasPrefix(decided.What, sprint.RetiredPrefix), "must have retired prefix: %s", decided.What)
	assert.Contains(t, decided.What, "s1-1 dropped (landed by hand)")
}

// A dropped card's judgment retires on a sparse tick at an epoch >= 1:
// proving that the loader path strips ~epoch and loads the card record so the
// judgment retires instead of remaining open.
func TestADroppedCardsJudgmentRetiresAtEpochOne(t *testing.T) {
	t.Parallel()
	h := flashAndPro(t)
	// Clear advances epoch from 0 to 1
	_, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	h.st, err = h.st.Pinned(h.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), h.st.PinnedEpoch())
	h.m = h.st.B.(*Mem)

	orphanJudgmentFundedWork(h)
	h.addReady("s1", 1, "tier: frontier\n\nThe task.\n")
	h.startMachine()
	h.machine()

	open := h.openOf(sprint.NNoRoute)
	require.Len(t, open, 1, "NNoRoute is raised on tier:frontier")
	jid := open[0].Note.ID
	assert.Equal(t, sprint.StreamSubject(sprint.TierSubject("frontier")), open[0].Subject())
	assert.Equal(t, []string{"s1-1"}, open[0].Note.Primaries)

	// A second frontier card keeps tier:frontier unserved, so TickDeal does not close the tier's judgment.
	h.addReady("s1", 1, "tier: frontier\n\nSecond task.\n")

	h.must(DropStep(sprint.DropReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Reason: "landed by hand"}))
	h.machine()

	for _, o := range h.openOf(sprint.NNoRoute) {
		if o.Note.ID == jid {
			require.Fail(t, fmt.Sprintf("judgment %s on dropped card s1-1 must retire on sparse tick at epoch 1, but remained open: %+v", jid, o))
		}
	}

	notes, _, err := h.m.NotesSince(h.ctx, "", 1000)
	require.NoError(t, err)
	var decided *sprint.Note
	for _, n := range notes {
		if n.Kind == sprint.Decided && n.Answers == jid {
			decided = &n
			break
		}
	}
	require.NotNil(t, decided, "no decided note answering %s", jid)
	assert.Equal(t, sprint.MachineActor, decided.Who)
	assert.True(t, strings.HasPrefix(decided.What, sprint.RetiredPrefix), "must have retired prefix: %s", decided.What)
	assert.Contains(t, decided.What, "s1-1 dropped (landed by hand)")
}
