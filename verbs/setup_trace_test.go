package verbs

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
)

type traceSetup struct {
	world   [9]bool
	answers [4]bool
}

func (s *traceSetup) Verify(_ context.Context, k int, _ SetupInput) (SetupCheck, error) {
	return SetupCheck{Verified: s.world[k], Action: !s.world[k]}, nil
}
func (s *traceSetup) Act(_ context.Context, k int, _ SetupInput) error { s.world[k] = true; return nil }
func (*traceSetup) Completion(context.Context) (string, error)         { return "UP DONE simulation", nil }

type traceState struct {
	world           [9]bool
	answers         [4]bool
	verified        [9]bool
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
	if o.Scope != "walk" {
		return
	}
	switch o.Kind {
	case "begin":
		c.state.verified = [9]bool{}
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
			if o.Step == 8 {
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
	c.state.answers = c.setup.answers
	c.records = append(c.records, traceRecord{action: action, k: k, state: c.state})
}
func TestSetupActualTrace(t *testing.T) {
	path := os.Getenv("NOVA_SETUP_TRACE_OUT")
	if path == "" {
		t.Skip("trace emission requested explicitly")
	}
	rng := rand.New(rand.NewSource(1762466400))
	fake := &traceSetup{answers: [4]bool{true, true, true, true}}
	for k := 1; k <= 8; k++ {
		fake.world[k] = rng.Intn(2) == 1
	}
	capture := &setupTraceCapture{t: t, setup: fake}
	e := Engine{Actor: "rowan", Store: upStore(true), Bus: upBus(), Now: upNow, Setup: fake, Lock: setupLeaseFake{}, SetupObserver: capture.observe}
	for round := 0; round < 4; round++ {
		if r := e.Execute(context.Background(), []string{"up"}); r.Exit != 0 {
			t.Fatal(r)
		}
		if round < 3 {
			k := 2 + rng.Intn(7)
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
	for k := 1; k <= 8; k++ {
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
	for k := 1; k <= 8; k++ {
		if s.verified[k] {
			if !first {
				out.WriteString(", ")
			}
			fmt.Fprint(out, k)
			first = false
		}
	}
	out.WriteString("}, answers |-> {")
	first = true
	for i, name := range []string{"coordinator", "dashboard", "services", "key"} {
		if s.answers[i] {
			if !first {
				out.WriteString(", ")
			}
			fmt.Fprintf(out, "%q", name)
			first = false
		}
	}
	fmt.Fprintf(out, `}, asked |-> [q \in {"coordinator", "dashboard", "services", "key"} |-> 0], pc |-> %d, phase |-> %q, disruptions |-> %d, keySinks |-> {}`, s.pc, s.phase, s.disruptions)
}
