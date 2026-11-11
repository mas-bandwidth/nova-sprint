package dash

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Cold read, 2026-11-08, at 032afd657 (modules/dash): one failing probe per
// confirmed defect.

// run_cache.go publish: a roster that failed set the stale alarm's latch
// with nothing said, so the stale ALARM never fired. The failure is said
// once and the stale alarm still fires.
func TestColdread6RosterFailureIsSaidAndKeepsTheStaleAlarm(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	snap := store.Snapshot{Epoch: 1, Seq: 2, Rows: map[string]store.Row{"amd-1": {Name: "amd-1", State: "up"}}}
	c := NewFromFeed(Feed{Snap: snap, Roster: roster.Roster{}, RosterErr: errors.New("nova-config down"), At: now}, func() time.Time { return now })
	if c.alarm {
		t.Errorf("a roster failure took the stale alarm's latch: the stale ALARM never fires")
	}
	if !strings.Contains(c.rosterNote.last, "nova-config down") {
		t.Errorf("the roster failure was never said (noted %q)", c.rosterNote.last)
	}
	if page := c.pageSnapshot(); page.Error == nil {
		t.Errorf("the page does not say the roster failed")
	}
	c.CheckFreshness(now.Add(40 * time.Second))
	if !c.alarm {
		t.Errorf("40s with no fresh data and no stale alarm")
	}
}

// shim.go countWork logged an unknown producer state on every mapping (each
// poll, every card): once per card and state is enough.
func TestColdread6UnknownStateAlarmsOnce(t *testing.T) { // unitlint: serial, it captures the process's log output (log.SetOutput)
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"cr6-p9": {ID: "cr6-p9", Kind: "producer", Stream: "s", State: "bogus"}}}
	for range 3 {
		mapSnapshot(s, testRoster, time.Time{}, nil)
	}
	if n := strings.Count(logs.String(), "ALARM"); n != 1 {
		t.Errorf("%d ALARM lines for one card's unknown state over three polls, want 1: %q", n, logs.String())
	}
}

// A stream's unpriced_runs counted every consumer with no price, the ones
// still waiting or working (not yet run) and subscription runs (free, never
// priced) among them. It is now the top strip's count, per stream: finished
// API runs with no price.
func TestColdread6StreamUnpricedCountsFinishedAPIRunsOnly(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p":  {ID: "p", Kind: "producer", Stream: "s", State: "working", Tier: "pro"},
		"w1": {ID: "w1", Kind: "work", Of: "p", Stream: "s", Tier: "pro", State: "done", Usage: "billing=api charged=- unpriced=no-tokens"},
		"w2": {ID: "w2", Kind: "work", Of: "p", Stream: "s", Tier: "pro", State: "waiting"},
		"w3": {ID: "w3", Kind: "work", Of: "p", Stream: "s", Tier: "pro", State: "working"},
		"r1": {ID: "r1", Kind: "read", Of: "p", Stream: "s", Tier: "pro", State: "done", Usage: "input=5 billing=subscription"},
	}}
	cache := &Cache{view: mapSnapshot(s, testRoster, time.Time{}, nil), at: t1000, now: at1000}
	b, err := json.Marshal(cache.pageSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var v2 map[string]any
	if err = json.Unmarshal(b, &v2); err != nil {
		t.Fatal(err)
	}
	sc := v2["data"].(map[string]any)["stream_costs"].(map[string]any)["s"].(map[string]any)
	if sc["unpriced_runs"] != float64(1) {
		t.Fatalf("one finished API run has no price; the stream says unpriced_runs=%v", sc["unpriced_runs"])
	}
}
