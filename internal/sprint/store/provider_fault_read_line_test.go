package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestAProviderFaultReadFollowsTheTierLine is the real-store read path behind attempt 4's
// finding: a member's read step prices with Step.Prices and holds NO tier array, so the
// read step must not draw the next route from tierArray's name-order fallback. It writes
// the provider's cooling and leaves the read unspent and unrouted; the coordinator's
// ask/tick (Routes loaded) re-asks it via readRouteOf on the next route in the tier's own
// line. Here the tier's line is b, a, c while name order is a, b, c: a read on b must be
// re-asked on a, never on c (docs/SPEC-SPRINT.md section 5, provider fallback;
// tla/ProviderFallback.tla Fallback, FaultSpendsNothing).
func TestAProviderFaultReadFollowsTheTierLine(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, m := range []string{"m1", "m2"} {
		h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: m, Width: sprint.MaxWidth}))
	}
	h.m.SetRoutes([]sprint.Route{route("a", "flash"), route("b", "flash"), route("c", "flash")})
	h.m.SetTiers(map[string][]string{"flash": {"b", "a", "c"}})
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	require.NoError(t, pinned.B.RowsAdd(h.ctx, pinned.Names.Table(sprint.Readers), []string{"reader-m1", "reader-m2"}))
	for _, rd := range []string{"reader-m1", "reader-m2"} {
		require.NoError(t, pinned.B.RowSet(h.ctx, pinned.Names.Table(sprint.Readers), rd, map[string]string{sprint.ReaderTiers: "flash"}))
	}
	h.beat()
	h.addReady("s1", 1, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	h.work("m1")
	require.Equal(t, sprint.Review, h.state("s1-1"))

	readOn := func(cols ...string) *sprint.Card {
		h.t.Helper()
		for _, c := range h.snap().Fleet.Column(cols...) {
			if c.F("kind") == "read" && c.F("primary") == "s1-1" {
				return c
			}
		}
		return nil
	}
	var rc *sprint.Card
	for i := 0; i < 6 && rc == nil; i++ {
		h.machine()
		rc = readOn(sprint.Ready, sprint.Working)
	}
	require.NotNil(t, rc, "a read card dealt")
	// the work card and the reads share the tier's rotation: the work card took b (the
	// array's first), so the read takes a. Its next route in the tier's line (b,a,c) is c;
	// name order (a,b,c) would take b.
	require.Equal(t, "a", rc.F(sprint.FieldRoute), "the tier's rotation after the work card's b")
	row := rc.Row
	h.must(TakeStep(sprint.TakeReq{As: row, Sel: sprint.Sel{IDs: []string{rc.ID}}, Gens: map[string]int{rc.ID: rc.Int("gen")}}))
	h.must(ReadStep(sprint.ReadReq{As: row, Return: true,
		Reason: "no verdict: provider: class=rate-limited status=429 msg=Too many requests",
		Sel:    sprint.Sel{IDs: []string{rc.ID}}}))

	// the read step holds no tier array: it wrote the cooling and left the read unspent;
	// the tick's own read deal (Routes loaded) draws the next route in the tier's line
	h.machine()
	again := readOn(sprint.Ready, sprint.Working)
	require.NotNil(t, again, "the read is asked again")
	assert.Equal(t, "c", again.F(sprint.FieldRoute), "the next route in the tier's line, not name order's b")
	assert.NotEqual(t, "b", again.F(sprint.FieldRoute))
	assert.Empty(t, again.F(sprint.FieldReasked), "the reader was never spent")
}
