package dash

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// The roster has rows the store lacks and the store has a row the roster
// lacks (the human, 2026-11-07: "we definitely need all that stuff back"):
// Fleet lists every roster worker and Friends every roster kin at the roster
// width, down when the store has no row; the store's row the roster no longer
// names stays, down; one refused row is shown refused with its reason; the
// order is by name.
func TestBoardListsTheWholeRoster(t *testing.T) {
	ros := roster.Roster{
		Workers: map[string]roster.Worker{"b-bench": {Name: "b-bench", Width: 8}, "a-bench": {Name: "a-bench", Width: 4}, "c-bench": {Name: "c-bench", Width: 2}},
		Kin:     map[string]roster.Kin{"zed": {Name: "zed", Width: 3}, "amy": {Name: "amy", Width: 2}},
		Refused: []roster.Refused{{Name: "d-bench", Kind: roster.KindWorker, Reason: "no cores in nova-config"}},
	}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"a-bench": {Name: "a-bench", State: "up"}, "amy": {Name: "amy", State: "up", Held: true}, "gone": {Name: "gone", State: "up"},
	}}
	d := mapSnapshot(snap, ros, time.Time{})
	fleet := d.Tables["fleet"].(map[string]*fleetRow)
	friends := d.Tables["friends"].(map[string]*fleetRow)
	want := map[string]fleetRow{
		"a-bench": {Status: "up", Width: 4}, "b-bench": {Status: "down", Width: 8}, "c-bench": {Status: "down", Width: 2},
		"d-bench": {Status: "refused", Reason: "no cores in nova-config"}, "gone": {Status: "down"},
	}
	if len(fleet) != len(want) {
		t.Fatalf("fleet = %d rows, want %d: %+v", len(fleet), len(want), fleet)
	}
	for n, w := range want {
		if g := fleet[n]; g == nil || *g != w {
			t.Errorf("fleet[%s] = %+v, want %+v", n, g, w)
		}
	}
	if len(friends) != 2 || friends["amy"].Status != "held" || friends["amy"].Width != 2 || friends["zed"].Status != "down" || friends["zed"].Width != 3 {
		t.Fatalf("friends = %+v", friends)
	}
	body, err := json.Marshal(d.Tables["fleet"])
	if err != nil {
		t.Fatal(err)
	}
	var again []byte
	if again, err = json.Marshal(mapSnapshot(snap, ros, time.Time{}).Tables["fleet"]); err != nil || string(again) != string(body) {
		t.Fatalf("the order is not stable: %v\n%s\n%s", err, body, again)
	}
}

// A hooked coordinator the roster lacks (an empty roster after a failed
// read) is still a friend, never a machine.
func TestEmptyRosterKeepsTheCoordinatorAmongFriends(t *testing.T) {
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{"boss": {Name: "boss", State: "up", Role: "coordinator"}}}
	d := mapSnapshot(snap, roster.Roster{}, time.Time{})
	if len(d.Tables["fleet"].(map[string]*fleetRow)) != 0 || d.Tables["friends"].(map[string]*fleetRow)["boss"].Status != "down" {
		t.Fatalf("tables = %+v", d.Tables)
	}
}

// OK% is known the moment a card is done: a finished work card counts for its
// holder at once (ok is OK, any other result Failed); a read that says fix
// counts against the author too, a read that says ok adds nothing more.
func TestOKPercentIsKnownWhenTheCardIsDone(t *testing.T) {
	ros := roster.Roster{Kin: map[string]roster.Kin{"emma": {Name: "emma", Width: 2}, "alex": {Name: "alex", Width: 2}}}
	card := func(id, kind, holder, author, result string) store.Card {
		return store.Card{ID: id, Kind: kind, Of: "p", Stream: "s", Holder: holder, Author: author, State: "done", Result: result, Tier: "pro"}
	}
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"w1": card("w1", "work", "emma", "emma", "ok"), "w2": card("w2", "work", "alex", "alex", "fault"),
		"w3": card("w3", "work", "alex", "alex", "ok"), "r1": card("r1", "read", "emma", "alex", "fix"),
		"r2": card("r2", "read", "alex", "emma", "ok"),
	}}
	friends := mapSnapshot(snap, ros, time.Time{}).Tables["friends"].(map[string]*fleetRow)
	if e := friends["emma"]; e.OK != 1 || e.Failed != 0 || e.OKPct != 100 {
		t.Errorf("emma = %+v: her ok work counts at once and an ok read adds nothing", e)
	}
	if a := friends["alex"]; a.OK != 1 || a.Failed != 2 || a.OKPct < 33.3 || a.OKPct > 33.4 {
		t.Errorf("alex = %+v: one ok, one fault, one read that said fix", a)
	}
}
