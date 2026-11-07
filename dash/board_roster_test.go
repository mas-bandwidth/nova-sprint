package dash

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestBoardListsTheWholeRoster covers a roster with rows the store lacks and
// a store with a row the roster lacks: Fleet lists every roster worker and
// Friends every roster kin at the roster width, down when the store has no
// row; a store row the roster does not name stays, down; the order is by
// name.
func TestBoardListsTheWholeRoster(t *testing.T) {
	ros := roster.Roster{
		Workers: map[string]roster.Worker{"b-bench": {Name: "b-bench", Width: 8}, "a-bench": {Name: "a-bench", Width: 4}, "c-bench": {Name: "c-bench", Width: 2}},
		Kin:     map[string]roster.Kin{"zed": {Name: "zed", Width: 3}, "amy": {Name: "amy", Width: 2}},
	}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"a-bench": {Name: "a-bench", State: "up"}, "amy": {Name: "amy", State: "up", Held: true}, "gone": {Name: "gone", State: "up"},
	}}
	d := mapSnapshot(snap, ros, time.Time{})
	fleet := d.Tables["fleet"].(map[string]*fleetRow)
	friends := d.Tables["friends"].(map[string]*fleetRow)
	want := map[string]fleetRow{
		"a-bench": {Status: "up", Width: 4}, "b-bench": {Status: "down", Width: 8}, "c-bench": {Status: "down", Width: 2},
		"gone": {Status: "down"},
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

// TestOKPercentIsKnownWhenTheCardIsDone: OK% is a worker's own finished work
// cards with result ok over those with result ok, fail or fix, known the
// moment a card is done. A fault, a close or release with no such result and
// every read are not in it.
func TestOKPercentIsKnownWhenTheCardIsDone(t *testing.T) {
	ros := roster.Roster{Kin: map[string]roster.Kin{"friendC": {Name: "friendC", Width: 2}, "friendA": {Name: "friendA", Width: 2}, "bob": {Name: "bob", Width: 2}}}
	card := func(id, kind, holder, author, result string) store.Card {
		return store.Card{ID: id, Kind: kind, Of: "p", Stream: "s", Holder: holder, Author: author, State: "done", Result: result, Tier: "pro"}
	}
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"w1": card("w1", "work", "friendC", "friendC", "ok"), "w2": card("w2", "work", "friendA", "friendA", "fault"),
		"w3": card("w3", "work", "friendA", "friendA", "ok"), "w4": card("w4", "work", "friendA", "friendA", "fail"),
		"w5": card("w5", "work", "friendC", "friendC", "fix"), "w6": card("w6", "work", "bob", "bob", "fault"),
		"w7": card("w7", "work", "bob", "bob", ""), "w8": card("w8", "work", "bob", "bob", "released"),
		"r1": card("r1", "read", "friendC", "friendA", "fix"), "r2": card("r2", "read", "friendA", "friendC", "ok"),
	}}
	d := mapSnapshot(snap, ros, time.Time{})
	friends := d.Tables["friends"].(map[string]*fleetRow)
	if e := friends["friendC"]; e.OK != 1 || e.Failed != 1 || e.OKPct != 50 || e.Done != 3 {
		t.Errorf("friendC = %+v: ok and fix work count, her read is her done and nothing else", e)
	}
	if a := friends["friendA"]; a.OK != 1 || a.Failed != 1 || a.OKPct != 50 || a.Done != 4 {
		t.Errorf("friendA = %+v: ok and fail count; the fault and friendC's read of his work do not", a)
	}
	if b := friends["bob"]; b.OK != 0 || b.Failed != 0 || b.Done != 3 {
		t.Errorf("bob = %+v: a fault, a close with no result and a release count for nothing", b)
	}
	body, err := json.Marshal(d.Tables["friends"])
	if err != nil {
		t.Fatal(err)
	}
	var cells map[string]map[string]string
	if err := json.Unmarshal(body, &cells); err != nil {
		t.Fatal(err)
	}
	if cells["bob"]["okpct"] != "-" || cells["friendC"]["okpct"] != "50.0%" {
		t.Errorf("okpct cells: bob %q (want -), friendC %q (want 50.0%%)", cells["bob"]["okpct"], cells["friendC"]["okpct"])
	}
}

// TestLoadCellIsTheLastHeartbeat: the load cell is the row's last heartbeat,
// the 1-minute load average over the cores as a percent of all of them, with
// the slots in its title; a row that is down, or has never beaten, has none.
func TestLoadCellIsTheLastHeartbeat(t *testing.T) {
	ros := roster.Roster{Workers: map[string]roster.Worker{"a": {Name: "a", Width: 4}, "b": {Name: "b", Width: 4}, "c": {Name: "c", Width: 4}}}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"a": {Name: "a", State: "up", Load1Centi: 252, Cores: 8, SlotsUsed: 3, SlotsTotal: 8},
		"b": {Name: "b", State: "down", Load1Centi: 252, Cores: 8, SlotsUsed: 3, SlotsTotal: 8},
		"c": {Name: "c", State: "up"},
	}}
	body, err := json.Marshal(mapSnapshot(snap, ros, time.Time{}).Tables["fleet"])
	if err != nil {
		t.Fatal(err)
	}
	var cells map[string]map[string]string
	if err := json.Unmarshal(body, &cells); err != nil {
		t.Fatal(err)
	}
	if cells["a"]["load"] != "31.5%" || cells["a"]["slots"] != "3 / 8" {
		t.Errorf("a = %v: want load 31.5%% and slots 3 / 8", cells["a"])
	}
	for _, n := range []string{"b", "c"} {
		if _, ok := cells[n]["load"]; ok {
			t.Errorf("%s shows a load, %v", n, cells[n])
		}
	}
}
