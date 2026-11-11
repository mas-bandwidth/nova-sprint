package dash

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestStoppedAndRosterProjection(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{Workers: map[string]roster.Worker{"worker": {Name: "worker", Width: 8}}, Kin: map[string]roster.Kin{"friend": {Name: "friend", Width: 2}}}
	snap := store.Snapshot{Epoch: 1, Stopped: true, StopReason: "Scheduled maintenance", Rows: map[string]store.Row{
		"worker": {Name: "worker", State: "up"}, "friend": {Name: "friend", State: "up", Held: true}, "removed": {Name: "removed", State: "up", Held: true},
	}}
	d := mapSnapshot(snap, ros, time.Time{}, nil)
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
		Machine string `json:"machine"`
	}
	if err = json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Stopped || got.Machine != "machine: STOPPED Scheduled maintenance" {
		t.Fatalf("stop projection: %s", body)
	}
	snap.Stopped = false
	snap.StopReason = "Restarted"
	d = mapSnapshot(snap, ros, time.Time{}, nil)
	if d.Stopped || d.Machine != "machine: RUNNING" {
		t.Fatalf("start projection: %+v", d)
	}
}

// A stopped sprint shows in the ETA tile and the machine line only: the page has
// no stop banner, and the server sends no reason for one.
func TestNoStopBanner(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"page/index.html", "page/app.js"} {
		b, err := page.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"stop-banner", "stop-reason", "stop_reason"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s still has %q", f, s)
			}
		}
	}
	snap := store.Snapshot{Epoch: 1, Stopped: true, StopReason: "x"}
	body, err := json.Marshal(mapSnapshot(snap, roster.Roster{}, time.Time{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "stop_reason") {
		t.Fatalf("the envelope carries a stop reason: %s", body)
	}
}
