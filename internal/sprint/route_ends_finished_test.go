package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The window routeEnds builds reads the Finished column too: a take the provider
// failed on a work card that finished its work and waits for the readers' verdict is
// an ended take of its route, exactly as one on a card the verdict has moved on
// (tla/RouteRest.tla; docs/SPEC-SPRINT.md section 5, "each work card's own finish").
// Three transient 5xx take records within one window on route a rest it for
// RouteRestFor even when the third is on a card in Finished; without reading Finished
// that take is uncounted and the route serves.
func TestRestsDueReadsAFinishedCardsTake(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	at := func(m int) string { return stamp(t0.Add(time.Duration(m) * time.Minute)) }
	outage := func(m int) string {
		return ProviderTake{Route: "a", Finished: at(m), Error: "provider: class=provider-5xx status=503 msg=upstream unavailable"}.String()
	}
	f := NewTable(Fleet)
	f.SetRows([]string{"m1"})
	f.Put(&Card{ID: "c1", Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldProviderTake + "1": outage(1), FieldRoute: "a"}})
	f.Put(&Card{ID: "c2", Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldProviderTake + "1": outage(2), FieldRoute: "a"}})
	// the third 5xx is a take on a card that finished ok and waits in Finished: a
	// finished stamp after the take, its readers' verdict still to come
	f.Put(&Card{ID: "c3", Row: "m1", Col: Finished, Fields: map[string]string{FieldProviderTake + "1": outage(3), FieldRoute: "a", "ok": "yes", "finished": at(4)}})
	s := &Snapshot{Now: t0.Add(45 * time.Minute), Fleet: f, Routes: []Route{{Name: "a", Tier: "pro", Enabled: true}}}
	due := RestsDue(s)
	require.Len(t, due, 1, "the third 5xx is read")
	assert.Equal(t, RestProvider, due[0].Cause)
	assert.ElementsMatch(t, []string{"c1", "c2", "c3"}, due[0].Cards)
}

// A card's own finish in Finished is an ended take of its route, as one the verdict
// moved on to DoneOK is (the same take number, the card counts once wherever it sits:
// when it moves on from Finished it is the same card and the same record). Ten finishes
// later on route a fill the window, so three 5xxs before them are slid out and the
// route does not rest; without reading Finished the window holds only the three and it does.
func TestAFinishedCardsEndCountsAsAnEndedTake(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	at := func(m int) string { return stamp(t0.Add(time.Duration(m) * time.Minute)) }
	outage := func(m int) string {
		return ProviderTake{Route: "a", Finished: at(m), Error: "provider: class=provider-5xx status=503 msg=upstream unavailable"}.String()
	}
	f := NewTable(Fleet)
	f.SetRows([]string{"m1"})
	for i := 1; i <= RouteRestAfter; i++ {
		f.Put(&Card{ID: "x" + itoa(i), Row: "m1", Col: DoneOK, Fields: map[string]string{FieldProviderTake + "1": outage(i), FieldRoute: "a"}})
	}
	// RouteRestWindow finishes later on route a, each a card in Finished: they are the
	// window's last takes, an ok end on the route (okEnd), never a transient one
	for i := 1; i <= RouteRestWindow; i++ {
		f.Put(&Card{ID: "f" + itoa(i), Row: "m1", Col: Finished, Fields: map[string]string{FieldRoute: "a", "ok": "yes", "finished": at(10 + i)}})
	}
	s := &Snapshot{Now: t0.Add(45 * time.Minute), Fleet: f, Routes: []Route{{Name: "a", Tier: "pro", Enabled: true}}}
	assert.Empty(t, RestsDue(s), "the ten finishes slide the three 5xxs out of the window")
}
