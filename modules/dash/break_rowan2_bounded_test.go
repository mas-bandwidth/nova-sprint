package dash

// Bounded adaptations of two probes by Rowan's second breaker (rowan2),
// origin/v2-break-rowan2 at 113e1fc07a1d87b9474a51d30d55ecb27702e064.
// They use immediate fakes and compare context deadlines, with no sleeps.

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestRowan2UnknownProducerCountsAndAlarms(t *testing.T) { // unitlint: serial, it captures the process's log output (log.SetOutput)
	f := store.NewFake()
	mustApply(t, f, "seed", 1, store.PutStream{Name: "s"}, store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro"}, store.PutCard{ID: "p2", Kind: "producer", Stream: "s", Tier: "pro"})
	mustApply(t, f, "move", 1, store.SetCardState{Card: "p1", State: "landed"}, store.SetCardState{Card: "p2", State: "done"})
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	c := boardOf(t, f)
	d := c.pageSnapshot().Data
	w := d.Tables["work"].(map[string]*workRow)["s"]
	if d.All != 2 || d.Done || w.Waiting != 1 || w.Landed != 1 {
		t.Fatalf("all=%d done=%v work=%+v", d.All, d.Done, w)
	}
	if text := logs.String(); !strings.Contains(text, "ALARM") || !strings.Contains(text, `"p2"`) || !strings.Contains(text, `"done"`) {
		t.Fatalf("missing card/state alarm: %q", text)
	}
}
