package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// The same-second sprint-21 deal: the recorded landing must reach the packet
// even when the worker's independently refreshed mirror is still older.
func TestThePacketCarriesTheLandedStateOfItsNeeds(t *testing.T) {
	t.Parallel()
	const need = "fixed3d-perf-harness"
	const head = "08681b55602c7e01545d855432774c2576a47b97"
	spikes := []string{
		"spike-wide-joint-solve", "spike-invmul-fits-64-arm", "spike-task-block-sizing",
		"spike-solve3-across-scales-256-by-128-arm", "spike-integrate-velocities-and-positions-wide",
	}
	h := newHarness(t)
	h.setup(0)
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{need}, Brief: proBrief}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: spikes, Needs: []string{need}, Brief: proBrief}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{need}}}))
	h.finishAttempt(need, false, head)
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{need}}}))
	h.readAllOK(need)
	h.must(AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{need}}}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Landed: []sprint.LandedPin{{ID: need, Head: head, InBase: true}}}))
	h.must(ResolveStep(sprint.ResolveReq{}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: spikes}}))
	s := h.snap()
	require.Equal(t, sprint.Landed, s.Work.Card(need).Col)
	require.Equal(t, sprint.Merged, s.Merge.Card(need).Col)
	require.Equal(t, head, s.Work.Card(need).F("head"))
	require.Empty(t, s.Merge.Card(need).F("head"), "the existing merge record stores no head")
	var cards []*sprint.Card
	for _, id := range spikes {
		c := s.Fleet.Card(id + ".w1")
		require.NotNil(t, c, "each spike is dealt at attempt 1")
		require.Equal(t, s.Work.Card(need).F("landed"), c.F("dealt"), "landing and deal use the same fixture clock")
		cards = append(cards, c)
	}

	// Assert the wire shape without requiring the new Go field to compile: this
	// regression runs on the unchanged base and fails on the missing JSON field.
	check := func(cards []*sprint.Card, want string) {
		t.Helper()
		packets, err := h.st.Packets(h.ctx, cards)
		require.NoError(t, err)
		require.Len(t, packets, len(cards))
		for _, p := range packets {
			body, err := json.Marshal(p)
			require.NoError(t, err)
			var wire map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &wire))
			if want == "" {
				require.NotContains(t, wire, "need_heads", "read/no-needs packets omit need_heads")
				continue
			}
			require.Contains(t, wire, "need_heads", "packet %s must carry its needs' landed heads", p.Card)
			require.JSONEq(t, want, string(wire["need_heads"]), p.Card)
		}
	}
	check(cards, `[{"id":"`+need+`","state":"landed","head":"`+head+`"}]`)

	// Replaying stored edge states is intentionally independent of admission:
	// a packet must describe missing/dropped/merging needs, never manufacture a
	// landing from a primary's working head or a queued merge's head.
	h.m.mu.Lock()
	work := h.m.tables[h.st.Names.Table(sprint.Work)]
	merge := h.m.tables[h.st.Names.Table(sprint.Merge)]
	put := func(id, col, outcome, primaryHead, mergeCol, mergeHead string) {
		key := sprint.StoredID(id, s.Epoch)
		work.members[key] = &memMember{epoch: s.Epoch, placed: col != "", row: "s1", col: col, rev: 1,
			fields: map[string]string{"outcome": outcome, "head": primaryHead}}
		if mergeCol != "" {
			merge.members[key] = &memMember{epoch: s.Epoch, placed: true, row: "s1", col: mergeCol, rev: 1,
				fields: map[string]string{"head": mergeHead}}
		}
	}
	put("still-merging", sprint.Merging, "", head, sprint.Queued, head)
	put("dropped-need", "", "dropped", head, sprint.Merged, head)
	put("landed-no-record", sprint.Landed, "", head, "", "")
	put("landed-short-head", sprint.Landed, "", "08681b55", sprint.Merged, head)
	put("landed-primary-head", sprint.Landed, "", head, sprint.Merged, pushedA)
	work.members[sprint.StoredID(spikes[0], s.Epoch)].fields["needs"] = strings.Join([]string{
		need, "still-merging", "dropped-need", "absent-need", "landed-no-record", "landed-short-head", "landed-primary-head",
	}, ",")
	h.m.mu.Unlock()
	check(cards[:1], `[
		{"id":"fixed3d-perf-harness","state":"landed","head":"`+head+`"},
		{"id":"still-merging","state":"merging"},
		{"id":"dropped-need","state":"dropped"},
		{"id":"absent-need","state":"absent"},
		{"id":"landed-no-record","state":"landed"},
		{"id":"landed-short-head","state":"landed"},
		{"id":"landed-primary-head","state":"landed","head":"`+head+`"}
	]`)
	check(cards[1:], `[{"id":"`+need+`","state":"landed","head":"`+head+`"}]`)
	read := &sprint.Card{ID: spikes[0] + ".r1", Fields: map[string]string{
		"primary": spikes[0], "kind": "read", "attempt": "1",
	}}
	check([]*sprint.Card{read}, "")
	check([]*sprint.Card{{ID: "no-primary.w1", Fields: map[string]string{"primary": "no-primary", "kind": "work", "attempt": "1"}}}, "")

	// Read sets are batched across packets and shared needs, not one per need.
	before := h.m.Calls["readset"]
	_, err := h.st.Packets(h.ctx, cards)
	require.NoError(t, err)
	require.LessOrEqual(t, h.m.Calls["readset"]-before, 4, "five packets and seven needs require bounded table reads")

	// A failed need read is an error, not an apparently complete empty packet.
	for _, table := range []string{sprint.Work, sprint.Merge} {
		reads := 0
		h.m.Fail = func(point string) error {
			if point == "readset "+h.st.Names.Table(table) {
				reads++
				if table == sprint.Merge || reads == 2 { // primary read, then need read
					return errors.New("fixture need read failed")
				}
			}
			return nil
		}
		packets, err := h.st.Packets(h.ctx, cards)
		h.m.Fail = nil
		require.ErrorContains(t, err, "fixture need read failed", table)
		require.Empty(t, packets, "no partial packets on a need read failure")
	}
}
