package verbs

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

type choiceTraceState struct {
	world, verified [9]bool
	answers         [4]bool
	asked           [4]int
	pc, disruptions int
	phase, services string
	keySink         bool
}
type choiceTraceRecord struct {
	action, question, value string
	k                       int
	state                   choiceTraceState
}
type choiceTrace struct {
	t       *testing.T
	state   choiceTraceState
	records []choiceTraceRecord
}

func (c *choiceTrace) record(action, q, value string) {
	c.records = append(c.records, choiceTraceRecord{action: action, question: q, value: value, state: c.state})
}
func (c *choiceTrace) observe(o SetupObservation) {
	if o.Scope != "walk" {
		return
	}
	switch o.Kind {
	case "begin":
		c.state.pc = 1
		c.state.phase = "probe"
		c.state.verified = [9]bool{}
		action := "Restart"
		if len(c.records) == 0 {
			action = "Init"
		}
		c.record(action, "", "")
	case "verify":
		if o.Failed || o.Step != c.state.pc || c.state.phase != "probe" {
			c.t.Fatalf("bad probe %+v", o)
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
		c.record("Probe", "", "")
	case "act":
		if o.Failed || o.Step != c.state.pc || c.state.phase != "repair" || !c.state.world[o.Step] {
			c.t.Fatalf("bad act %+v", o)
		}
		c.state.phase = "probe"
		c.record("Do", "", "")
	case "complete":
		if o.Failed || c.state.phase != "done" {
			c.t.Fatalf("bad completion %+v", o)
		}
	default:
		c.t.Fatalf("unexpected event %+v", o)
	}
}

type choiceTraceStore struct {
	store.Store
	trace  *choiceTrace
	choice string
}

func (s choiceTraceStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	r, err := s.Store.Apply(ctx, op)
	if err == nil && r.Accepted && op.Kind == "setup answer" && s.trace.state.phase == "repair" {
		value, found, readErr := (Choices{Store: s.Store}).Get(ctx, "services")
		if readErr != nil || !found {
			s.trace.t.Fatalf("saved services: %q %v %v", value, found, readErr)
		}
		mapped := "configured"
		if value == "none" {
			mapped = "none"
		} else if value != "deepseek" {
			s.trace.t.Fatalf("unexpected service %q", value)
		}
		if mapped != s.choice {
			s.trace.t.Fatalf("scenario %q saved %q", s.choice, value)
		}
		s.trace.state.answers[2] = found
		s.trace.state.services = mapped
		s.trace.record("ChooseServices", "", mapped)
	}
	return r, err
}

type choiceTraceSink struct {
	saved    bool
	trace    *choiceTrace
	terminal *terminalFake
}

func (s *choiceTraceSink) Has(_ context.Context, name string) (bool, error) {
	if name != "DEEPSEEK_API_KEY" {
		return false, fmt.Errorf("unexpected key name %q", name)
	}
	return s.saved, nil
}
func (s *choiceTraceSink) Set(_ context.Context, name string, r io.Reader) error {
	if name != "DEEPSEEK_API_KEY" {
		return fmt.Errorf("unexpected key name %q", name)
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		return err
	}
	s.saved = true
	s.trace.state.answers[3] = true
	s.trace.state.asked[3] = s.terminal.asks
	s.trace.state.keySink = true
	s.trace.record("Ask", "key:provider1", "")
	return nil
}

type choiceTraceProbe struct{}

func (choiceTraceProbe) Run(context.Context, string) error { return nil }

type choiceTraceSetup struct {
	trace   *choiceTrace
	runtime RuntimeSetup
}

func (s *choiceTraceSetup) Verify(ctx context.Context, k int, in SetupInput) (SetupCheck, error) {
	if k == 6 {
		return s.runtime.servicesCheck(ctx, in)
	}
	return SetupCheck{Verified: s.trace.state.world[k], Action: !s.trace.state.world[k]}, nil
}
func (s *choiceTraceSetup) Act(ctx context.Context, k int, in SetupInput) error {
	if k == 6 {
		if err := s.runtime.servicesAct(ctx, in); err != nil {
			return err
		}
		check, err := s.runtime.servicesCheck(ctx, in)
		if err != nil {
			return err
		}
		s.trace.state.world[k] = check.Verified
		return nil
	}
	s.trace.state.world[k] = true
	return nil
}
func (*choiceTraceSetup) Completion(context.Context) (string, error) {
	return "UP DONE simulation", nil
}

func TestSetupChoicesActualTrace(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("NOVA_SETUP_CHOICES_TRACE_DIR")
	if dir == "" {
		t.Skip("trace emission requested explicitly")
	}
	for _, choice := range []string{"none", "configured"} {
		t.Run(choice, func(t *testing.T) { emitChoiceTrace(t, dir, choice) })
	}
}
func emitChoiceTrace(t *testing.T, dir, choice string) {
	ctx := context.Background()
	trace := &choiceTrace{t: t}
	trace.state.services = "unknown"
	db := store.NewFake()
	c := Choices{Store: db, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }}
	for _, pair := range [][2]string{{"coordinator", "rowan"}, {"dashboard", "tailnet"}} {
		if err := c.Save(ctx, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range []string{"coordinator", "dashboard"} {
		_, found, err := c.Get(ctx, name)
		if err != nil || !found {
			t.Fatal(name, found, err)
		}
		trace.state.answers[i] = found
	}
	c.Store = choiceTraceStore{Store: db, trace: trace, choice: choice}
	term := &terminalFake{data: []byte("fake-key")}
	sink := &choiceTraceSink{trace: trace, terminal: term}
	work := &runtimeWork{}
	runtime := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: c}}, Workload: work, Keys: ServiceKey{Terminal: term, Sink: sink, Probe: choiceTraceProbe{}}}
	fake := &choiceTraceSetup{trace: trace, runtime: runtime}
	input := "none"
	if choice == "configured" {
		input = "deepseek"
	}
	e := Engine{Actor: "rowan", Store: upStore(true), Bus: upBus(), Now: upNow, Setup: fake, Lock: setupLeaseFake{}, SetupObserver: trace.observe}
	walkChoices(t, e, trace, work, input)
	if choice == "none" && (term.asks != 0 || sink.saved) {
		t.Fatal("none prompted for key")
	}
	if choice == "configured" && (term.asks != 1 || !sink.saved) {
		t.Fatal("configured key was not saved once")
	}
	if err := os.WriteFile(filepath.Join(dir, choice+".tla"), []byte(renderChoiceTrace(trace.records)), 0600); err != nil {
		t.Fatal(err)
	}
}
func walkChoices(t *testing.T, e Engine, trace *choiceTrace, work *runtimeWork, input string) {
	ctx := context.Background()
	rng := rand.New(rand.NewSource(1762466400))
	for k := 1; k <= 8; k++ {
		if k != 6 {
			trace.state.world[k] = rng.Intn(2) == 1
		}
	}
	for round := 0; round < 4; round++ {
		if r := e.Execute(ctx, []string{"up", "--services", input}); r.Exit != 0 {
			t.Fatal(r)
		}
		if round < 3 {
			k := 2 + rng.Intn(7)
			if !trace.state.world[k] {
				t.Fatalf("regress unready step %d", k)
			}
			trace.state.world[k] = false
			if k == 6 {
				work.services = false
			}
			trace.state.disruptions++
			trace.records = append(trace.records, choiceTraceRecord{action: "Regress", k: k, state: trace.state})
		}
	}
}
func renderChoiceTrace(records []choiceTraceRecord) string {
	var out strings.Builder
	out.WriteString("---- MODULE SetupChoicesTraceLog ----\nEXTENDS Naturals, Sequences, FiniteSets, TLC\nTraceLog == <<\n")
	for i, r := range records {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "  [i |-> %d, action |-> %q", i+1, r.action)
		if r.action == "Regress" {
			fmt.Fprintf(&out, ", k |-> %d", r.k)
		}
		if r.question != "" {
			fmt.Fprintf(&out, ", q |-> %q", r.question)
		}
		if r.action == "ChooseServices" {
			fmt.Fprintf(&out, ", value |-> %q, prompted |-> FALSE", r.value)
		}
		renderChoiceState(&out, r.state)
		out.WriteString("]")
	}
	out.WriteString("\n>>\n====\n")
	return out.String()
}
func renderChoiceState(out *strings.Builder, s choiceTraceState) {
	fmt.Fprintf(out, ", servicesChoice |-> %q, world |-> <<", s.services)
	for i := 1; i <= 8; i++ {
		if i > 1 {
			out.WriteString(", ")
		}
		fmt.Fprint(out, choiceBool(s.world[i]))
	}
	out.WriteString(">>, verified |-> {")
	sep := ""
	for i := 1; i <= 8; i++ {
		if s.verified[i] {
			fmt.Fprintf(out, "%s%d", sep, i)
			sep = ", "
		}
	}
	out.WriteString("}, answers |-> {")
	sep = ""
	names := []string{"coordinator", "dashboard", "services", "key:provider1"}
	for i, name := range names {
		if s.answers[i] {
			fmt.Fprintf(out, "%s%q", sep, name)
			sep = ", "
		}
	}
	out.WriteString("}, asked |-> (\"coordinator\" :> ")
	fmt.Fprint(out, s.asked[0])
	for i := 1; i < len(names); i++ {
		fmt.Fprintf(out, " @@ %q :> %d", names[i], s.asked[i])
	}
	fmt.Fprintf(out, "), pc |-> %d, phase |-> %q, disruptions |-> %d, keySinks |-> {", s.pc, s.phase, s.disruptions)
	if s.keySink {
		out.WriteString("\"secrets-stdin\"")
	}
	out.WriteString("}")
}
func choiceBool(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}
