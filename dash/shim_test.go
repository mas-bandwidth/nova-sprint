package dash

import (
	"github.com/mas-bandwidth/nova-sprint/store"
	"testing"
	"time"
)

func TestMapRepresentative(t *testing.T) {
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Rows: map[string]store.Row{"kin": {Name: "kin", Kind: "kin", State: "up", Held: true, Width: 2}}, Cards: map[string]store.Card{
		"blocked": {ID: "blocked", Kind: "producer", Stream: "s", State: "blocked"},
		"failed":  {ID: "failed", Kind: "producer", Stream: "s", State: "fail"}, "fix": {ID: "fix", Kind: "producer", Stream: "s", State: "ready", Attempt: 1, Tier: "pro"},
		"work": {ID: "work", Kind: "work", Of: "fix", Stream: "s", Holder: "kin", State: "done", Tier: "pro", Cost: 1200000, Priced: true},
		"read": {ID: "read", Kind: "read", Of: "fix", Stream: "s", Holder: "kin", Author: "kin", State: "done", Result: "ok", Tier: "pro", Priced: false}}}
	d := mapSnapshot(s, time.Time{})
	w := d.Tables["work"].(map[string]*workRow)["s"]
	f := d.Tables["friends"].(map[string]*fleetRow)["kin"]
	if d.All != 3 || w.Blocked != 1 || w.Fail != 1 || w.Fix != 1 || w.Cost == nil || *w.Cost != 1.2 {
		t.Fatalf("work=%+v all=%d", w, d.All)
	}
	if f.Status != "held" || f.Done != 2 || f.OK != 1 || f.OKPct != 100 {
		t.Fatalf("kin=%+v", f)
	}
}

func TestStreamStates(t *testing.T) {
	s := store.Snapshot{Streams: map[string]store.Stream{"empty": {Name: "empty"}, "held": {Name: "held", Held: true}, "blocked": {Name: "blocked"}, "failed": {Name: "failed"}}, Cards: map[string]store.Card{"b": {ID: "b", Kind: "producer", Stream: "blocked", State: "blocked"}, "f": {ID: "f", Kind: "producer", Stream: "failed", State: "fail"}}}
	d := mapSnapshot(s, time.Time{})
	got := map[string]string{}
	for _, row := range d.Streams {
		got[row.Stream] = row.State
	}
	for k, v := range map[string]string{"empty": "waiting", "held": "held", "blocked": "waiting", "failed": "waiting"} {
		if got[k] != v {
			t.Errorf("%s state=%q want=%q", k, got[k], v)
		}
	}
}
