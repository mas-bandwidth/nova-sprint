package verbs

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
)

// The per-card audit record (Glenn, 2026-11-09: "we used to track every
// aspect of work on a card, tokens, models, $$$"): a bench's read under a
// policy whose prices() is {} (the live sprint's) is priced from the
// built-in table, exactly, and its stored words say who ran it (the
// roster's harness), how long (the row's wall), when it was reported (the
// engine's clock), its tokens by class, its model and its billing; check's
// cost line sums it. A faulted run that reported no token keeps its wall,
// harness and time, unpriced=no-tokens.
func TestReportIsTheAuditRecord(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	takeR1(t, w)
	words := "input=101488 cache_read=7906944 cache_write=0 output=17506 reasoning=31275 model=deepseek/deepseek-flash wall=612"
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--usage", words), 0, "READ p2.r1 head="+head+" result=ok")
	c := w.snap(t).Cards["p2.r1"]
	if c.Cost != 136_425 || !c.Priced || c.Usage != words+" harness=nova-worker at=2026-11-06T12:00:00Z billing=api charged=-" {
		t.Fatalf("read card cost %d priced %t usage %q", c.Cost, c.Priced, c.Usage)
	}
	out := w.do("bob", "check")
	if !strings.Contains(out.Output, "CHECK cost: $0.136425 API over 1 priced runs (1 priced at the report, 0 from the built-in table at read; 8.1M tokens); 0 unpriced") {
		t.Fatalf("check:\n%s", out.Output)
	}

	w = newWorld(t)
	takeR1(t, w)
	want(t, w.do("bob", "report", "p2.r1@1", "--result", "fault", "--reason", "bench down", "--usage", "wall=9 harness=claude"), 0, "READ p2.r1 head="+head+" result=fault")
	if c := w.snap(t).Cards["p2.r1"]; c.Result != "fault" || c.Priced || c.Usage != "wall=9 harness=nova-worker at=2026-11-06T12:00:00Z billing=api charged=- unpriced=no-tokens" {
		t.Fatalf("faulted card %+v (the row's own harness word is never kept)", c)
	}
}

// A kin the roster names with no harness is still a kin: billed as a
// subscription seat, as before the harness stamp (the cold read of PR 759).
func TestKinWithNoHarnessIsStillASeat(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.roster.ro.Kin["bob"] = roster.Kin{Name: "bob", Model: "m-bob", Width: 2, Tiers: []string{"pro"}}
	delete(w.roster.ro.Workers, "bob")
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--usage", "input=10 output=5 model=deepseek/deepseek-flash"), 0, "READ p2.r1")
	if c := w.snap(t).Cards["p2.r1"]; c.Cost != 0 || !c.Priced || c.Usage != "input=10 output=5 model=deepseek/deepseek-flash at=2026-11-06T12:00:00Z billing=subscription charged=-" {
		t.Fatalf("read card cost %d priced %t usage %q", c.Cost, c.Priced, c.Usage)
	}
}

// show <card> prints a consumer's dollars as the board counts them, a run
// stored unpriced=no-price under an empty prices() included.
func TestShowPrintsTheCardsDollars(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--usage", "input=101488 cache_read=7906944 cache_write=0 output=17506 reasoning=31275 model=deepseek/deepseek-flash"), 0, "READ p2.r1")
	want(t, w.do("bob", "show", "p2.r1"), 0, `"cost_usd": "0.136425"`, `"usage": "input=101488`)
	want(t, w.do("bob", "show", "p2"), 0)
	if out := w.do("bob", "show", "p2").Output; strings.Contains(out, "cost_usd") {
		t.Fatalf("a producer has no run to price:\n%s", out)
	}
}
