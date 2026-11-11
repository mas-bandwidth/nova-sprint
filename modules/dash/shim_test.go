package dash

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestMapRepresentative(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Rows: map[string]store.Row{"kin": {Name: "kin", State: "up", Held: true}}, Cards: map[string]store.Card{
		"held":   {ID: "held", Kind: "producer", Stream: "s", State: "waiting", Reason: "held: wrong brief"},
		"failed": {ID: "failed", Kind: "producer", Stream: "s", State: "fail"}, "fix": {ID: "fix", Kind: "producer", Stream: "s", State: "ready", Attempt: 1, Tier: "pro"},
		"work": {ID: "work", Kind: "work", Of: "fix", Stream: "s", Holder: "kin", State: "done", Result: "ok", Tier: "pro", Cost: 1200000, Priced: true},
		"read": {ID: "read", Kind: "read", Of: "fix", Stream: "s", Holder: "kin", Author: "kin", State: "done", Result: "ok", Tier: "pro", Priced: false}}}
	d := mapSnapshot(s, testRoster, time.Time{}, nil)
	w := d.Tables["work"].(map[string]*workRow)["s"]
	f := d.Tables["friends"].(map[string]*fleetRow)["kin"]
	if d.All != 3 || w.Waiting != 1 || w.Fail != 1 || w.Fix != 1 || w.Cost == nil || w.Cost.Int64() != 120 {
		t.Fatalf("work=%+v all=%d", w, d.All)
	}
	if f.Status != "held" || f.Done != 2 || f.OK != 1 || f.Failed != 0 || f.OKPct != 100 {
		t.Fatalf("kin=%+v", f)
	}
}

func TestStreamStates(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Streams: map[string]store.Stream{"empty": {Name: "empty"}, "held": {Name: "held", Held: true}, "heldcard": {Name: "heldcard"}, "failed": {Name: "failed"}}, Cards: map[string]store.Card{"b": {ID: "b", Kind: "producer", Stream: "heldcard", State: "waiting", Reason: "held: x"}, "f": {ID: "f", Kind: "producer", Stream: "failed", State: "fail"}}}
	d := mapSnapshot(s, testRoster, time.Time{}, nil)
	got := map[string]string{}
	for _, row := range d.Streams {
		got[row.Stream] = row.State
	}
	for k, v := range map[string]string{"empty": "waiting", "held": "held", "heldcard": "waiting", "failed": "waiting"} {
		if got[k] != v {
			t.Errorf("%s state=%q want=%q", k, got[k], v)
		}
	}
}

// TestStreamStateIsNeverFail: a stream has no fail status. A stream whose
// cards all failed shows what an empty stream shows; the failed cards still
// count in the fail column.
func TestStreamStateIsNeverFail(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Streams: map[string]store.Stream{"one": {Name: "one"}, "two": {Name: "two"}, "mixed": {Name: "mixed"}, "empty": {Name: "empty"}}, Cards: map[string]store.Card{
		"o":  {ID: "o", Kind: "producer", Stream: "one", State: "fail"},
		"t1": {ID: "t1", Kind: "producer", Stream: "two", State: "fail"}, "t2": {ID: "t2", Kind: "producer", Stream: "two", State: "fail"},
		"m1": {ID: "m1", Kind: "producer", Stream: "mixed", State: "landed"}, "m2": {ID: "m2", Kind: "producer", Stream: "mixed", State: "fail"}}}
	d := mapSnapshot(s, testRoster, time.Time{}, nil)
	got := map[string]string{}
	for _, row := range d.Streams {
		got[row.Stream] = row.State
	}
	for k, v := range map[string]string{"one": "waiting", "two": "waiting", "mixed": "waiting", "empty": "waiting"} {
		if got[k] != v {
			t.Errorf("%s state=%q want=%q", k, got[k], v)
		}
	}
	if w := d.Tables["work"].(map[string]*workRow); w["one"].Fail != 1 || w["two"].Fail != 2 || w["mixed"].Fail != 1 || w["mixed"].Landed != 1 {
		t.Fatalf("fail counts one=%+v two=%+v mixed=%+v", w["one"], w["two"], w["mixed"])
	}
	states := []string{"waiting", "ready", "working", "review", "merging", "landed", "fail"}
	for _, held := range []bool{false, true} {
		for _, a := range states {
			for _, b := range states {
				s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s", Held: held}}, Cards: map[string]store.Card{
					"a": {ID: "a", Kind: "producer", Stream: "s", State: a}, "b": {ID: "b", Kind: "producer", Stream: "s", State: b}}}
				if st := mapSnapshot(s, testRoster, time.Time{}, nil).Streams[0].State; st == "fail" {
					t.Errorf("held=%v cards %s,%s: stream state %q", held, a, b, st)
				}
			}
		}
	}
}

// TestHeldCardsAreWaiting: a card with a problem has no column (Glenn, 2026-11-07:
// "waiting is where stuff goes where it's having problems"): a held card
// and one a missing route stopped count as waiting, and the wire carries no
// count or list of its own.
func TestHeldCardsAreWaiting(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p1": {ID: "p1", Kind: "producer", Stream: "s", State: "waiting", Reason: "held: wrong brief"},
		"p2": {ID: "p2", Kind: "producer", Stream: "s", State: "waiting", Reason: "missing-route: no adb"},
		"p3": {ID: "p3", Kind: "producer", Stream: "s", State: "waiting"}}}
	d := mapSnapshot(s, testRoster, time.Time{}, nil)
	b, err := json.Marshal(d.Tables["work"].(map[string]*workRow)["s"])
	if err != nil || !strings.Contains(string(b), `"waiting":"3"`) || strings.Contains(string(b), "blocked") {
		t.Fatalf("stream s: %s %v", b, err)
	}
	if d.Left != 3 {
		t.Fatalf("left=%d, want 3", d.Left)
	}
}
