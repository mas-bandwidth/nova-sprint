package verbs

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
)

// traceSetup is the world of steps 1, 2 and 4 to 7; step 3's is the
// store's coordinator row. Step 7 has no act, as in production.
type traceSetup struct {
	world [8]bool
}

func (s *traceSetup) Verify(_ context.Context, k int, _ SetupInput) (SetupCheck, error) {
	return SetupCheck{Verified: s.world[k], Action: !s.world[k] && k != 7}, nil
}
func (s *traceSetup) Act(_ context.Context, k int, _ SetupInput) error { s.world[k] = true; return nil }
func (*traceSetup) Completion(context.Context, SetupInput) (string, error) {
	return "UP DONE simulation", nil
}

type traceState struct {
	world           [8]bool
	verified        [8]bool
	pc, disruptions int
	phase           string
}
type traceRecord struct {
	action string
	k      int
	state  traceState
}
type setupTraceCapture struct {
	t       *testing.T
	setup   *traceSetup
	state   traceState
	records []traceRecord
}

func (c *setupTraceCapture) observe(o SetupObservation) {
	switch o.Kind {
	case "begin":
		c.state.verified = [8]bool{}
		c.state.pc = 1
		c.state.phase = "probe"
		action := "Restart"
		if len(c.records) == 0 {
			action = "Init"
		}
		c.record(action, 0)
	case "verify":
		if o.Failed || o.Step != c.state.pc || c.state.phase != "probe" {
			c.t.Fatalf("unexpected verification %+v state %+v", o, c.state)
		}
		if o.Verified {
			c.state.verified[o.Step] = true
			c.state.pc++
			if o.Step == 7 {
				c.state.phase = "done"
			}
		} else {
			c.state.phase = "repair"
		}
		c.record("Probe", 0)
	case "act":
		if o.Failed || o.Step != c.state.pc || c.state.phase != "repair" || !c.setup.world[o.Step] {
			c.t.Fatalf("unexpected action %+v", o)
		}
		c.state.phase = "probe"
		c.record("Do", 0)
	case "store-only":
		if c.state.pc != 3 || c.state.phase != "probe" {
			c.t.Fatalf("unexpected handoff %+v state %+v", o, c.state)
		}
		c.state.phase = "handoff"
		c.state.disruptions++
		c.record("Handoff", 0)
	case "stop":
		// Step 7 refuses (no act); the world settles it when step 6 holds.
		if o.Step != 7 || c.state.phase != "repair" {
			c.t.Fatalf("unexpected stop %+v state %+v", o, c.state)
		}
		c.setup.world[7] = c.setup.world[7] || c.setup.world[6]
		c.state.phase, c.state.pc, c.state.verified = "stopped", 1, [8]bool{}
		c.record("Settle", 0)
	case "complete":
		if o.Failed || c.state.phase != "done" {
			c.t.Fatalf("unexpected completion %+v", o)
		}
	default:
		c.t.Fatalf("unexpected setup trace event %+v", o)
	}
}
func (c *setupTraceCapture) record(action string, k int) {
	c.state.world = c.setup.world
	c.records = append(c.records, traceRecord{action: action, k: k, state: c.state})
}
func TestSetupActualTrace(t *testing.T) {
	t.Parallel()
	path := os.Getenv("NOVA_SETUP_TRACE_OUT")
	if path == "" {
		t.Skip("trace emission requested explicitly")
	}
	rng := rand.New(rand.NewSource(1762466400))
	fake := &traceSetup{}
	for k := 1; k <= 7; k++ {
		fake.world[k] = rng.Intn(2) == 1
	}
	fake.world[3] = true // the fixture's coordinator row is hooked
	capture := &setupTraceCapture{t: t, setup: fake}
	e := Engine{Actor: "rowan", Store: upStore(true), Bus: upBus(), Now: upNow, Setup: fake, SetupObserver: capture.observe, UpStoreOnly: true}
	if r := e.Execute(context.Background(), []string{"up"}); r.Exit != 0 || r.Output != "" {
		t.Fatal(r)
	}
	e.UpStoreOnly = false
	for round := 0; round < 4; round++ {
		r := e.Execute(context.Background(), []string{"up"})
		for tries := 0; r.Exit != 0 && tries < 2; tries++ {
			r = e.Execute(context.Background(), []string{"up"})
		}
		if r.Exit != 0 {
			t.Fatal(r)
		}
		if round < 3 {
			k := []int{2, 4, 5, 6, 7}[rng.Intn(5)]
			if !fake.world[k] {
				t.Fatal("regression requires true world")
			}
			fake.world[k] = false
			capture.state.disruptions++
			capture.record("Regress", k)
		}
	}
	if err := os.WriteFile(path, []byte(renderSetupTrace(capture.records)), 0600); err != nil {
		t.Fatal(err)
	}
}

func renderSetupTrace(records []traceRecord) string {
	var out strings.Builder
	out.WriteString("---- MODULE SetupTraceLog ----\nEXTENDS Naturals, Sequences, FiniteSets\nTraceLog == <<\n")
	for i, r := range records {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "  [i |-> %d, action |-> %q", i+1, r.action)
		if r.action == "Regress" {
			fmt.Fprintf(&out, ", k |-> %d", r.k)
		}
		renderTraceState(&out, r.state)
		out.WriteString("]")
	}
	out.WriteString("\n>>\n====\n")
	return out.String()
}
func renderTraceState(out *strings.Builder, s traceState) {
	out.WriteString(", world |-> <<")
	for k := 1; k <= 7; k++ {
		if k > 1 {
			out.WriteString(", ")
		}
		if s.world[k] {
			out.WriteString("TRUE")
		} else {
			out.WriteString("FALSE")
		}
	}
	out.WriteString(">>, verified |-> {")
	first := true
	for k := 1; k <= 7; k++ {
		if s.verified[k] {
			if !first {
				out.WriteString(", ")
			}
			fmt.Fprint(out, k)
			first = false
		}
	}
	fmt.Fprintf(out, `}, answers |-> {"identity"}, asked |-> [q \in {"identity"} |-> 0], pc |-> %d, phase |-> %q, disruptions |-> %d`, s.pc, s.phase, s.disruptions)
}
