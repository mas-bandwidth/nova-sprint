package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldBy is the conformance scenario of CardsHeldBy: on every store it is
// heldOf of the snapshot read beside it, and it holds the cards a row holds
// in ready or working with their producers, and nothing else.
func heldBy(h *harness) {
	h.seed()
	h.ok(h.op("w2", nil, PutRow{Name: "w2", Session: "sess-b", Role: "worker"}, SetIdentity{"Sprint", "sprint@example.com"},
		PutCard{Tier: "pro", ID: "u4", Kind: "work", Of: "c2", Stream: "s1", Holder: "w2"}, SetCardState{"u4", "working"},
		PutCard{Tier: "pro", ID: "u5", Kind: "read", Of: "c1", Stream: "s1", Holder: "w2"}, SetCardState{"u5", "ready"},
		PutCard{Tier: "pro", ID: "u6", Kind: "work", Of: "c2", Stream: "s1", Holder: "w1"}, SetCardState{"u6", "done"},
		SetCardState{"u1", "ready"}, SetCardState{"u2", "working"}))
	for row, want := range map[string][]string{"w1": {"c1", "u1", "u2"}, "w2": {"c1", "c2", "u4", "u5"}, "w9": nil, "": nil} {
		got := h.held(row)
		assert.Equal(h.t, want, slices.Sorted(maps.Keys(got.Cards)), row)
		assert.Equal(h.t, map[string]string{"w1": "w1", "w2": "w2"}[row], got.Row.Name, "the row, zero when there is none")
	}
	h.clear()
	got := h.held("w1")
	assert.Equal(h.t, 2, got.Epoch)
	assert.Equal(h.t, "w1", got.Row.Name, "a clear carries the rows")
	assert.Empty(h.t, got.Cards, "and none of the cards")
}

// held reads CardsHeldBy(row), checks it against heldOf of a snapshot taken
// at the same seq, and returns it.
func (h *harness) held(row string) Held {
	h.t.Helper()
	got, err := h.s.CardsHeldBy(h.ctx, row)
	require.NoError(h.t, err)
	s := h.snap()
	require.Equal(h.t, s.Seq, got.Seq, "nothing ran between the two reads")
	assert.Equal(h.t, heldOf(s, row), got, row)
	return got
}

// TestFunctionalHeldByReadsOnlyTheRow: on a store of 5,000 cards (four times
// the live sprint's 1,233) over 16 rows, each holding 15 cards in ready or
// working (the live most), with a 1 MB brief on another row's held card and
// on its producer, one row's read is its 15 held cards and their producers,
// under 64 KB as JSON. The read's time is logged: the plan
// adds an (epoch, holder) index only if it is over 10 ms here.
func TestFunctionalHeldByReadsOnlyTheRow(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.ok(h.op("stream", nil, PutStream{Name: "s1", Repo: "r", Base: "main"}))
	var rows []Write
	for r := range 16 {
		rows = append(rows, PutRow{Name: fmt.Sprintf("w%d", r)})
	}
	h.ok(h.op("rows", nil, rows...))
	brief, big := strings.Repeat("b", 512), strings.Repeat("x", 1<<20)
	const producers = 2500
	for start := 0; start < producers; start += 100 {
		var w []Write
		for i := start; i < start+100; i++ {
			b := brief
			if i == 1 {
				b = big // p1 is held through w1's u1, never by w0
			}
			u, k := fmt.Sprintf("u%d", i), i/16 // the row's k-th card: 15 ready or working, then waiting and done
			w = append(w, PutCard{Tier: "pro", Kind: "producer", ID: fmt.Sprintf("p%d", i), Stream: "s1", Brief: b},
				PutCard{Tier: "pro", Kind: "work", ID: u, Of: fmt.Sprintf("p%d", i), Stream: "s1", Holder: fmt.Sprintf("w%d", i%16), Brief: b})
			switch {
			case k < 15:
				w = append(w, SetCardState{u, []string{"ready", "working"}[k%2]})
			case k%2 == 1:
				w = append(w, SetCardState{u, "done"})
			}
		}
		h.ok(h.op("add", nil, w...))
	}
	ctx := context.Background()
	got, err := p.CardsHeldBy(ctx, "w0")
	require.NoError(t, err)
	assert.Equal(t, heldOf(h.snap(), "w0"), got)
	assert.Len(t, got.Cards, 30, "w0's 15 ready and working consumers, each with its producer")
	b, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Less(t, len(b), 64<<10, "the read is the row's, not the sprint's")
	other, err := p.CardsHeldBy(ctx, "w1")
	require.NoError(t, err)
	assert.Equal(t, big, other.Cards["p1"].Brief, "the big brief is w1's to read")
	var took []time.Duration
	for range 21 {
		start := time.Now()
		_, err := p.CardsHeldBy(ctx, "w0")
		require.NoError(t, err)
		took = append(took, time.Since(start))
	}
	slices.Sort(took)
	t.Logf("CardsHeldBy(w0) over %d cards: %d bytes as JSON, %d cards, median %v, max %v", 2*producers, len(b), len(got.Cards), took[len(took)/2], took[len(took)-1])
}
