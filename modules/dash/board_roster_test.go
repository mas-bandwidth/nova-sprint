package dash

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The roster has rows the store lacks and the store has rows the roster
// lacks (the human, 2026-11-07: "we definitely need all that stuff back"):
// Fleet lists every roster worker and Friends every roster kin at the roster
// width, down when the store has no row; a store row nova-config does not
// name is in neither table (Glenn, 2026-11-15: removed machines must not show
// in the fleet), while one it names that is not a roster row (a machine that
// is not a bench) shows, down; the order is by name.
func TestBoardListsTheWholeRoster(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{
		Workers: map[string]roster.Worker{
			"b-bench": {Name: "b-bench", Resources: map[string]int{"cpu": 8, "memory": 16}},
			"a-bench": {Name: "a-bench", Resources: map[string]int{"cpu": 4, "memory": 8}},
			"c-bench": {Name: "c-bench", Resources: map[string]int{"cpu": 2, "memory": 4}},
		},
		Kin:    map[string]roster.Kin{"zed": {Name: "zed", Width: 3}, "amy": {Name: "amy", Width: 2}},
		Listed: map[string]bool{"studio": true},
	}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"a-bench": {Name: "a-bench", State: "up"}, "amy": {Name: "amy", State: "up", Held: true},
		"gone": {Name: "gone", State: "up"}, "gone-friend": {Name: "gone-friend", State: "down"}, "studio": {Name: "studio", State: "up"},
	}}
	d := mapSnapshot(snap, ros, time.Time{}, nil)
	fleet := d.Tables["fleet"].(map[string]*fleetRow)
	friends := d.Tables["friends"].(map[string]*fleetRow)
	want := map[string]struct {
		status string
		cpu    int
	}{
		"a-bench": {status: "up", cpu: 4}, "b-bench": {status: "down", cpu: 8}, "c-bench": {status: "down", cpu: 2},
		"studio": {status: "down", cpu: 0},
	}
	if len(fleet) != len(want) {
		t.Fatalf("fleet = %d rows, want %d: %+v", len(fleet), len(want), fleet)
	}
	for n, w := range want {
		if g := fleet[n]; g == nil || g.Status != w.status || g.CPU == nil || g.CPU.Total != w.cpu {
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
	if again, err = json.Marshal(mapSnapshot(snap, ros, time.Time{}, nil).Tables["fleet"]); err != nil || string(again) != string(body) {
		t.Fatalf("the order is not stable: %v\n%s\n%s", err, body, again)
	}
}

// Machines removed from nova-config (Glenn, 2026-11-15: aws-schema-bench-1..4
// stayed on the board as down) are in neither table, a bench's row and a
// friend's alike, whatever the store says of them, but a hooked coordinator
// (never retired) stays; a roster row with no store row is still there,
// down, at its roster width.
func TestBoardLeavesOutRowsNovaConfigDoesNotName(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{
		Workers: map[string]roster.Worker{
			"amd-1":     {Name: "amd-1", Resources: map[string]int{"cpu": 8, "memory": 16}},
			"new-bench": {Name: "new-bench", Resources: map[string]int{"cpu": 4, "memory": 8}},
		},
		Kin: map[string]roster.Kin{"stella": {Name: "stella", Width: 2}},
	}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"amd-1": {Name: "amd-1", State: "up"}, "stella": {Name: "stella", State: "up"},
		"aws-schema-bench-1": {Name: "aws-schema-bench-1", State: "up", CPUPeakDeci: 500, Version: "v3.0.0"},
		"aws-schema-bench-2": {Name: "aws-schema-bench-2", State: "down"},
		"old-friend":         {Name: "old-friend", State: "up", Session: "x", Role: "worker"},
		"rowan":              {Name: "rowan", State: "up", Session: "y", Role: "coordinator"},
	}}
	d := mapSnapshot(snap, ros, time.Time{}, nil)
	fleet := d.Tables["fleet"].(map[string]*fleetRow)
	friends := d.Tables["friends"].(map[string]*fleetRow)
	for _, n := range []string{"aws-schema-bench-1", "aws-schema-bench-2", "old-friend"} {
		if fleet[n] != nil || friends[n] != nil {
			t.Errorf("%s is on the board: fleet %+v friends %+v", n, fleet[n], friends[n])
		}
	}
	if g := fleet["new-bench"]; g == nil || g.Status != "down" || g.CPU == nil || g.CPU.Total != 4 {
		t.Errorf("new-bench = %+v, want down with cpu total 4", g)
	}
	if g := fleet["rowan"]; g == nil {
		t.Errorf("the hooked coordinator nova-config does not name is never retired (steps.Retirable), so it stays on the board")
	}
	if len(fleet) != 3 || len(friends) != 1 || fleet["amd-1"].Status != "up" || friends["stella"].Status != "up" {
		t.Fatalf("fleet %+v friends %+v", fleet, friends)
	}
}

// An empty roster (a failed read: run_cache passes none) judges nothing
// absent: every store row shows, down, at width 0, as before.
func TestBoardWithAnEmptyRosterShowsEveryRow(t *testing.T) {
	t.Parallel()
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{"amd-1": {Name: "amd-1", State: "up"}, "gone": {Name: "gone", State: "up"}}}
	fleet := mapSnapshot(snap, roster.Roster{}, time.Time{}, nil).Tables["fleet"].(map[string]*fleetRow)
	if len(fleet) != 2 || fleet["amd-1"].Status != "down" || fleet["gone"].Status != "down" {
		t.Fatalf("fleet = %+v", fleet)
	}
}

// OK% (directive 64a) is a worker's own finished work cards with result ok
// over those with result ok, fail or fix, known the moment a card is done. A
// fault, a close or release with no such result and every read are not in it.
func TestOKPercentIsKnownWhenTheCardIsDone(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{Kin: map[string]roster.Kin{"emma": {Name: "emma", Width: 2}, "alex": {Name: "alex", Width: 2}, "bob": {Name: "bob", Width: 2}}}
	card := func(id, kind, holder, author, result string) store.Card {
		return store.Card{ID: id, Kind: kind, Of: "p", Stream: "s", Holder: holder, Author: author, State: "done", Result: result, Tier: "pro"}
	}
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"w1": card("w1", "work", "emma", "emma", "ok"), "w2": card("w2", "work", "alex", "alex", "fault"),
		"w3": card("w3", "work", "alex", "alex", "ok"), "w4": card("w4", "work", "alex", "alex", "fail"),
		"w5": card("w5", "work", "emma", "emma", "fix"), "w6": card("w6", "work", "bob", "bob", "fault"),
		"w7": card("w7", "work", "bob", "bob", ""), "w8": card("w8", "work", "bob", "bob", "released"),
		"r1": card("r1", "read", "emma", "alex", "fix"), "r2": card("r2", "read", "alex", "emma", "ok"),
	}}
	d := mapSnapshot(snap, ros, time.Time{}, nil)
	friends := d.Tables["friends"].(map[string]*fleetRow)
	if e := friends["emma"]; e.OK != 1 || e.Failed != 1 || e.OKPct != 50 || e.Done != 3 {
		t.Errorf("emma = %+v: ok and fix work count, her read is her done and nothing else", e)
	}
	if a := friends["alex"]; a.OK != 1 || a.Failed != 1 || a.OKPct != 50 || a.Done != 4 {
		t.Errorf("alex = %+v: ok and fail count; the fault and emma's read of his work do not", a)
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
	if cells["bob"]["okpct"] != "-" || cells["emma"]["okpct"] != "50.0%" {
		t.Errorf("okpct cells: bob %q (want -), emma %q (want 50.0%%)", cells["bob"]["okpct"], cells["emma"]["okpct"])
	}
}

// The load cell is the row's last heartbeat (directive 63a): the highest CPU
// busy percent of its last 10 seconds across all the cores, v1's cell, with the
// slots in its title; a row that is down, or has never beaten, has none.
func TestLoadCellIsTheLastHeartbeat(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{Workers: map[string]roster.Worker{
		"a": {Name: "a", Resources: map[string]int{"cpu": 4, "memory": 8}},
		"b": {Name: "b", Resources: map[string]int{"cpu": 4, "memory": 8}},
		"c": {Name: "c", Resources: map[string]int{"cpu": 4, "memory": 8}},
	}}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"a": {Name: "a", State: "up", CPUPeakDeci: 873, Version: "v3.0.0"},
		"b": {Name: "b", State: "down", CPUPeakDeci: 873, Version: "v3.0.0"},
		"c": {Name: "c", State: "up"},
	}}
	body, err := json.Marshal(mapSnapshot(snap, ros, time.Time{}, nil).Tables["fleet"])
	if err != nil {
		t.Fatal(err)
	}
	var cells map[string]map[string]any
	if err := json.Unmarshal(body, &cells); err != nil {
		t.Fatal(err)
	}
	loadVal := ""
	if v, ok := cells["a"]["load"]; ok && v != nil {
		loadVal = v.(string)
	}
	if loadVal != "87.3%" {
		t.Errorf("a = %v: want load 87.3%%", cells["a"])
	}
	for _, n := range []string{"b", "c"} {
		if _, ok := cells[n]["load"]; ok {
			t.Errorf("%s shows a load, %v", n, cells[n])
		}
	}
}
