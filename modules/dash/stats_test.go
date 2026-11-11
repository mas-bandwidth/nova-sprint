package dash

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// After a stats reset (store.StatsCursor) the Fleet and Friends done, ok,
// failed and ok% count only the consumers done after the mark; tokens and
// cost count every card as before, and the board carries the mark's time.
// With no mark the counts are the epoch's.
func TestFleetStatsCountOnlyWhatFinishedAfterTheMark(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{Workers: map[string]roster.Worker{"bench": {Name: "bench", Width: 4}}, Kin: map[string]roster.Kin{"amy": {Name: "amy", Width: 2}}}
	card := func(id, kind, holder, result string) store.Card {
		return store.Card{ID: id, Kind: kind, Of: "p", Stream: "s", Holder: holder, State: "done", Result: result, Tier: "pro", Priced: true, Cost: 1_000_000,
			Usage: "input=10 billing=api"}
	}
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"old1": card("old1", "work", "bench", "fail"), "old2": card("old2", "work", "bench", "fail"), "old3": card("old3", "read", "amy", "ok"),
		"new1": card("new1", "work", "bench", "ok"), "new2": card("new2", "work", "bench", "fix"), "new3": card("new3", "read", "bench", "ok"),
		"new4": card("new4", "work", "amy", "ok"),
	}}
	at := time.Date(2026, 11, 10, 19, 12, 0, 0, time.UTC)
	mark := &store.StatsMark{Seq: 40, At: at, Done: map[string]int64{"new1": 41, "new2": 42, "new3": 43, "new4": 44}}

	d := mapSnapshot(snap, ros, time.Time{}, mark)
	fleet, friends := d.Tables["fleet"].(map[string]*fleetRow), d.Tables["friends"].(map[string]*fleetRow)
	if b := fleet["bench"]; b.Done != 3 || b.OK != 1 || b.Failed != 1 || b.OKPct != 50 || b.Tokens != 50 {
		t.Errorf("bench = %+v: done, ok, failed and ok%% since the mark; tokens of every card", b)
	}
	if a := friends["amy"]; a.Done != 1 || a.OK != 1 || a.Failed != 0 || a.OKPct != 100 || a.Tokens != 20 {
		t.Errorf("amy = %+v: her read before the mark is not counted", a)
	}
	if d.StatsSince == nil || !d.StatsSince.Equal(at) {
		t.Errorf("stats_since = %v, want %v", d.StatsSince, at)
	}
	if w := d.Tables["work"].(map[string]*workRow)["s"]; w.Cost == nil || w.Cost.Int64() != 700 {
		t.Errorf("work cost = %v: every card's cost, mark or not", w.Cost)
	}
	body, err := json.Marshal(d)
	if err != nil || !strings.Contains(string(body), `"stats_since":"2026-11-10T19:12:00Z"`) {
		t.Fatalf("the envelope carries the mark's time: %v\n%s", err, body)
	}

	life := mapSnapshot(snap, ros, time.Time{}, nil)
	if b := life.Tables["fleet"].(map[string]*fleetRow)["bench"]; b.Done != 5 || b.OK != 1 || b.Failed != 3 {
		t.Errorf("no mark: bench = %+v, the epoch's counts", b)
	}
	if life.StatsSince != nil {
		t.Errorf("no mark, no since: %v", life.StatsSince)
	}
	if body, err := json.Marshal(life); err != nil || strings.Contains(string(body), "stats_since") {
		t.Fatalf("no mark, no stats_since: %v", err)
	}
}

// The page says the mark beside the Fleet and Friends heads ("since 7:12 PM"),
// so the counts are read as what they are.
func TestPageShowsTheStatsMark(t *testing.T) {
	t.Parallel()
	js, err := os.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"function sinceWord(", "d.stats_since", `sideWord(d.fleet_work, null) + sinceWord(d)`, `sideWord(d.friends_work, d.friends_tiers) + sinceWord(d)`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}

// The run feed's mark reaches the board.
func TestFeedCarriesTheStatsMark(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{Workers: map[string]roster.Worker{"bench": {Name: "bench", Width: 1}}}
	snap := store.Snapshot{Seq: 50, Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Rows: map[string]store.Row{"bench": {Name: "bench", State: "up"}}, Cards: map[string]store.Card{
		"a": {ID: "a", Kind: "work", Of: "p", Stream: "s", Holder: "bench", State: "done", Result: "ok", Tier: "pro"},
		"b": {ID: "b", Kind: "work", Of: "p", Stream: "s", Holder: "bench", State: "done", Result: "ok", Tier: "pro"},
	}}
	at := time.Unix(1000, 0).UTC()
	c := NewFromFeed(Feed{Snap: snap, Roster: ros, At: at, Stats: &store.StatsMark{Seq: 49, At: at, Done: map[string]int64{"b": 50}}}, func() time.Time { return at })
	d := c.pageSnapshot().Data
	if b := d.Tables["fleet"].(map[string]*fleetRow)["bench"]; b.Done != 1 || b.OK != 1 {
		t.Errorf("bench = %+v: only b finished after the mark", b)
	}
	if d.StatsSince == nil || !d.StatsSince.Equal(at) {
		t.Errorf("stats_since = %v", d.StatsSince)
	}
}
