package dash

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestRunCacheMonitoringAndCopy(t *testing.T) {
	now := time.Unix(1000, 0)
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"p": {ID: "p", Kind: "producer", Stream: "s", State: "waiting", Needs: []string{"q"}}}}
	c := NewFromSnapshot(s, func() time.Time { return now })
	card := s.Cards["p"]
	card.Needs[0] = "changed"
	delete(s.Streams, "s")
	if c.state.Cards["p"].Needs[0] != "q" || c.state.Streams["s"].Name != "s" {
		t.Fatal("input snapshot aliases cache")
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
	if err := c.PublishSnapshot(fresh, now); err != nil || c.alarm {
		t.Fatalf("publish=%v alarm=%v", err, c.alarm)
	}
	if err := c.PublishSnapshot(s, now); err == nil {
		t.Fatal("older snapshot accepted")
	}
}
