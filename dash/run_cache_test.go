package dash

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestRunFeedMonitoringAndOrder(t *testing.T) {
	now := time.Unix(1000, 0)
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"p": {ID: "p", Kind: "producer", Stream: "s", State: "waiting"}}}
	c := NewFromFeed(Feed{Snap: s, At: now}, func() time.Time { return now })
	delete(s.Streams, "s") // the view was mapped at publish: a later change to the copy is not shown
	if len(c.view.Streams) != 1 {
		t.Fatal("the view aliases the published snapshot")
	}
	now = now.Add(31 * time.Second)
	c.CheckFreshness(now)
	if c.alarm {
		t.Fatal("alarmed early")
	}
	now = now.Add(time.Second)
	c.CheckFreshness(now)
	if !c.alarm {
		t.Fatal("no alarm without viewer")
	}
	fresh := store.Snapshot{Epoch: 1, Seq: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}}
	if err := c.Publish(Feed{Snap: fresh, At: now}); err != nil || c.alarm {
		t.Fatalf("publish=%v alarm=%v", err, c.alarm)
	}
	if err := c.Publish(Feed{Snap: s, At: now}); err == nil {
		t.Fatal("older snapshot accepted")
	}
}

// Drift row 4: the machine line says STOPPED and the reason; the human page
// strips the reason (app.js), /api/sprint carries it.
func TestRunFeedShowsStoppedWithReason(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewFromFeed(Feed{Snap: store.Snapshot{Epoch: 1, Seq: 3, Stopped: true, StopReason: "base red"}, At: now}, func() time.Time { return now })
	b, err := json.Marshal(c.view)
	if err != nil || !strings.Contains(string(b), `"machine":"machine: STOPPED base red"`) {
		t.Fatalf("machine line missing: %s %v", b, err)
	}
	if err := c.Publish(Feed{Snap: store.Snapshot{Epoch: 1, Seq: 4}, At: now}); err != nil || c.view.Machine != "machine: RUNNING" {
		t.Fatalf("running not shown: %q %v", c.view.Machine, err)
	}
}

// Stella's finding 6: a roster that failed is never shown as current: every
// row at width 0, the alarm raised, the error on the envelope; the store's
// lines stay fresh.
func TestRunFeedRosterFailureIsNotAnOldRoster(t *testing.T) {
	now := time.Unix(1000, 0)
	snap := store.Snapshot{Epoch: 1, Seq: 2, Stopped: true, StopReason: "x", Rows: map[string]store.Row{"amd-1": {Name: "amd-1", State: "up"}}}
	ro := roster.Roster{Workers: map[string]roster.Worker{"amd-1": {Name: "amd-1", Width: 8, Tiers: store.Tiers}}}
	c := NewFromFeed(Feed{Snap: snap, Roster: ro, At: now}, func() time.Time { return now })
	if w := c.view.Tables["fleet"].(map[string]*fleetRow)["amd-1"].Width; w != 8 {
		t.Fatalf("width %d, want the roster's 8", w)
	}
	snap.Seq = 3
	if err := c.Publish(Feed{Snap: snap, Roster: ro, RosterErr: errors.New("nova-config down"), At: now}); err != nil {
		t.Fatal(err)
	}
	if w := c.view.Tables["fleet"].(map[string]*fleetRow)["amd-1"].Width; w != 0 || !c.alarm || !strings.Contains(c.view.Error, "nova-config down") {
		t.Fatalf("width %d alarm %v error %q: an old roster shown as current", w, c.alarm, c.view.Error)
	}
	if c.view.Machine != "machine: STOPPED x" {
		t.Fatalf("store line not fresh: %q", c.view.Machine)
	}
}

// The run's board reads the ETA from the landings run feeds it, not from any
// poller: four in the last hour give an ETA over the hour, and a Publish that
// adds landings moves it.
func TestRunFeedCarriesLandingsForTheEta(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	snap := store.Snapshot{Epoch: 1, Seq: 5, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{}}
	for i := 0; i < 40; i++ { // 40 cards waiting
		id := string(rune('a'+i%26)) + string(rune('a'+i/26))
		snap.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: "s", State: "waiting"}
	}
	ls := []time.Time{now.Add(-50 * time.Minute), now.Add(-30 * time.Minute), now.Add(-20 * time.Minute), now.Add(-5 * time.Minute)}
	c := NewFromFeed(Feed{Snap: snap, Landings: ls, At: now}, func() time.Time { return now })
	d := c.pageSnapshot().Data.(envelope)
	if d.Eta != "10h0m" || d.EtaBasis != "last hour" || d.EtaLandings != 4 {
		t.Fatalf("eta=%q basis=%q landings=%d", d.Eta, d.EtaBasis, d.EtaLandings)
	}
	now = now.Add(time.Minute)
	snap.Seq = 6
	if err := c.Publish(Feed{Snap: snap, Landings: append(ls, now.Add(-time.Minute)), At: now}); err != nil {
		t.Fatal(err)
	}
	d = c.pageSnapshot().Data.(envelope)
	if d.EtaLandings != 5 || d.Eta != "8h0m" {
		t.Fatalf("after a landing: eta=%q landings=%d", d.Eta, d.EtaLandings)
	}
	// run could not read them: no ETA, never a guess; an answered empty list is known but still "-"
	now = now.Add(time.Minute)
	snap.Seq = 7
	if err := c.Publish(Feed{Snap: snap, At: now}); err != nil {
		t.Fatal(err)
	}
	if d = c.pageSnapshot().Data.(envelope); d.Eta != "-" || d.EtaBasis != "" {
		t.Fatalf("unknown landings: eta=%q basis=%q", d.Eta, d.EtaBasis)
	}
	now = now.Add(time.Minute)
	snap.Seq = 8
	if err := c.Publish(Feed{Snap: snap, Landings: []time.Time{}, At: now}); err != nil {
		t.Fatal(err)
	}
	if d = c.pageSnapshot().Data.(envelope); d.Eta != "-" || d.EtaBasis != "last hour" {
		t.Fatalf("empty known landings: eta=%q basis=%q", d.Eta, d.EtaBasis)
	}
}
