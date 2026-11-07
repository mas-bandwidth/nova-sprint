package dash

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestStoppedAndRosterProjection(t *testing.T) {
	ros := roster.Roster{Workers: map[string]roster.Worker{"worker": {Name: "worker", Width: 8}}, Kin: map[string]roster.Kin{"friend": {Name: "friend", Width: 2}}}
	snap := store.Snapshot{Epoch: 1, Stopped: true, StopReason: "Scheduled maintenance", Rows: map[string]store.Row{
		"worker": {Name: "worker", State: "up"}, "friend": {Name: "friend", State: "up", Held: true}, "removed": {Name: "removed", State: "up", Held: true},
	}}
	d := mapSnapshot(snap, ros, time.Time{})
	fleet := d.Tables["fleet"].(map[string]*fleetRow)
	friends := d.Tables["friends"].(map[string]*fleetRow)
	if fleet["worker"].Status != "up" || fleet["worker"].Width != 8 || friends["friend"].Status != "held" || friends["friend"].Width != 2 {
		t.Fatalf("configured rows: fleet=%+v friends=%+v", fleet, friends)
	}
	if fleet["removed"].Status != "down" || fleet["removed"].Width != 0 {
		t.Fatalf("removed roster row: %+v", fleet["removed"])
	}
	body, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stopped bool   `json:"stopped"`
		Reason  string `json:"stop_reason"`
		Machine string `json:"machine"`
	}
	if err = json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Stopped || got.Reason != "Scheduled maintenance" || got.Machine != "STOPPED" {
		t.Fatalf("stop projection: %s", body)
	}
	snap.Stopped = false
	snap.StopReason = "Restarted"
	d = mapSnapshot(snap, ros, time.Time{})
	if d.Stopped || d.Machine != "" || d.StopReason != "Restarted" {
		t.Fatalf("start projection: %+v", d)
	}
}
