package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadDealRoundKeepsAnIndependentTierCursor pins the read-card deal's persisted fleet
// cursor (docs/SPEC-SPRINT.md section 1): each tier starts from its own epoch property and
// a placement moves the tier's cursor past the chosen reader.
func TestReadDealRoundKeepsAnIndependentTierCursor(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Fleet: NewTable(Fleet)}
	s.Fleet.SetProp(PropReadDealIndex("flash"), "7")
	units := []readUnit{{name: "m1"}, {name: "m2"}, {name: "m3"}, {name: "m4"}}
	flash := readDealRound(s, "flash", units)
	require.Equal(t, "m4", flash.scan(func(string) bool { return true }), "counter seven starts at the fourth reader")
	flash.moved("m4")
	require.Equal(t, "8", flash.value(), "the placement advances one past m4")
	pro := readDealRound(s, "pro", units)
	require.Equal(t, "m1", pro.scan(func(string) bool { return true }), "pro has an independent cursor")
}

// TestReadCardDealUsesAPersistedRoundRobinCursor pins docs/SPEC-SPRINT.md section 1 and
// README.md's rule that free capacity gets another ready card: the read-card deal keeps a
// cursor per tier in the fleet table, advances it with each read, and uses it on the next
// plan even when the next member has slightly less idle room than its peers.
func TestReadCardDealUsesAPersistedRoundRobinCursor(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 16, "m1", "m2", "m3", "m4")
	w.s.Work.SetRows(append(w.s.Work.Rows(), "s1"))
	w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), "worker"))
	w.s.Routes = []Route{{Name: "flash-a", Tier: "flash", Provider: "p", Model: "f", Enabled: true}}
	for i := 1; i <= 40; i++ {
		id := fmt.Sprintf("s1-%d", i)
		w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief("flash"), FieldTierNow: "flash"}})
		w.s.Fleet.Put(&Card{ID: WorkCardID(id, 1), Row: "worker", Col: Finished, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "work", "primary": id, "attempt": "1", "member": "worker"}})
	}
	first, _ := readCardsAsk(w.s, nil, routeIndexesOf(w.s))
	w.must(first)
	counts := map[string]int{}
	for _, u := range first.Units {
		for _, change := range u.Changes {
			if change.Entry.Create != nil {
				counts[change.Entry.Create.Row]++
			}
		}
	}
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		assert.Equal(t, 10, counts[m], "40 reads land ten apiece: %v", counts)
	}
	if got := len(first.Props); got == 0 {
		t.Fatal("the deal did not persist its read cursor")
	}
	w.s.Routes = append(w.s.Routes, Route{Name: "pro-a", Tier: "pro", Provider: "p", Model: "pr", Enabled: true})
	w.s.Work.Put(&Card{ID: "s1-pro", Row: "s1", Col: Review, Score: 40.5, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief("pro"), FieldTierNow: "pro"}})
	w.s.Fleet.Put(&Card{ID: WorkCardID("s1-pro", 1), Row: "worker", Col: Finished, Score: 40.5, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "s1-pro", "attempt": "1", "member": "worker"}})
	pro, _ := readCardsAsk(w.s, nil, routeIndexesOf(w.s))
	w.must(pro)
	proReaders := map[string]bool{}
	for _, u := range pro.Units {
		for _, change := range u.Changes {
			if change.Entry.Create != nil {
				proReaders[change.Entry.Create.Row] = true
			}
		}
	}
	assert.Equal(t, map[string]bool{"m1": true, "m2": true}, proReaders, "the pro cursor starts at m1 independently")
	w.s.Fleet.Put(&Card{ID: "other.w1", Row: "m1", Col: Working, Score: 50, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "other", "attempt": "1"}})
	id := "s1-41"
	w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: 41, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief("flash"), FieldTierNow: "flash"}})
	w.s.Fleet.Put(&Card{ID: WorkCardID(id, 1), Row: "worker", Col: Finished, Score: 41, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": id, "attempt": "1", "member": "worker"}})
	second, _ := readCardsAsk(w.s, nil, routeIndexesOf(w.s))
	w.must(second)
	for _, u := range second.Units {
		for _, change := range u.Changes {
			if change.Entry.Create != nil && change.Entry.ID == ReadCardID(id, 1, "m1") {
				return
			}
		}
	}
	t.Fatalf("the 41st read did not wrap to m1 after m4; got %+v", second.Units)
}

// TestReadDealCursorPersistsAcrossADeal pins docs/SPEC-SPRINT.md section 1: the read
// cursor is an epoch table property written with the read cards, so a later deal over the
// same table resumes after the last reader instead of starting over (a restart reads the
// property back, as every epoch table property is).
func TestReadDealCursorPersistsAcrossADeal(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2", "m3", "m4")
	w.s.Work.SetRows(append(w.s.Work.Rows(), "s1"))
	w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), "worker"))
	w.s.Routes = []Route{{Name: "flash-a", Tier: "flash", Provider: "p", Model: "f", Enabled: true}}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("s1-%d", i)
		w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief("flash"), FieldTierNow: "flash"}})
		w.s.Fleet.Put(&Card{ID: WorkCardID(id, 1), Row: "worker", Col: Finished, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "work", "primary": id, "attempt": "1", "member": "worker"}})
	}
	p, _ := readCardsAsk(w.s, nil, routeIndexesOf(w.s))
	w.must(p)
	index, ok := w.s.Fleet.Prop(PropReadDealIndex("flash"))
	require.True(t, ok, "the cursor is persisted on the fleet table")
	require.Equal(t, "3", index, "three reads dealt: the cursor advanced past m1, m2, m3")

	id := "s1-4"
	w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: 4, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief("flash"), FieldTierNow: "flash"}})
	w.s.Fleet.Put(&Card{ID: WorkCardID(id, 1), Row: "worker", Col: Finished, Score: 4, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": id, "attempt": "1", "member": "worker"}})
	next, _ := readCardsAsk(w.s, nil, routeIndexesOf(w.s))
	w.must(next)
	for _, u := range next.Units {
		for _, ch := range u.Changes {
			if ch.Entry.Create != nil && ch.Entry.ID == ReadCardID(id, 1, "m4") {
				return
			}
		}
	}
	t.Fatalf("the deal did not resume after m3: %+v", next.Units)
}

// TestReadCardDealSkipsAReaderWhoseReadWasRefusedAtStaging pins docs/SPEC-SPRINT.md
// section 1 and README.md's free-capacity rule: a machine that refused a read at staging
// is not selected again while an eligible reader with room exists.
func TestReadCardDealSkipsAReaderWhoseReadWasRefusedAtStaging(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2")
	w.s.Work.SetRows(append(w.s.Work.Rows(), "s1"))
	w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), "worker"))
	w.s.Routes = []Route{{Name: "flash-a", Tier: "flash", Provider: "p", Model: "f", Enabled: true}}
	w.s.Work.Put(&Card{ID: "s1-1", Row: "s1", Col: Review, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief("flash"), FieldTierNow: "flash"}})
	w.s.Fleet.Put(&Card{ID: WorkCardID("s1-1", 1), Row: "worker", Col: Finished, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "s1-1", "attempt": "1", "member": "worker"}})
	w.s.Fleet.Put(&Card{ID: ReadCardID("s1-1", 1, "m1"), Row: "m1", Col: Withdrawn, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "read", "primary": "s1-1", "reader": "m1", "attempt": "1", "gen": "1", "retired_by": RetiredByUnstarted,
			FieldStagingTake + "1": ProviderTake{Member: "m1"}.String()}})
	p, _ := readCardsAsk(w.s, nil, routeIndexesOf(w.s))
	w.must(p)
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Entry.Create != nil && ch.Entry.ID == ReadCardID("s1-1", 1, "m2") {
				return
			}
		}
	}
	t.Fatalf("the read was not dealt to m2 after m1 refused it at staging: %+v", p.Units)
}
