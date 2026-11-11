package verbs

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// takeoverWorld is the sprint with carol, its coordinator, gone from the
// bus and holding p1.w1, dave on the bus and not hooked, and up's walk
// faked; a test may put carol back on the bus.
func takeoverWorld(t *testing.T) *world {
	t.Helper()
	s := sprint()
	setCard("p1.w1", func(c *store.Card) { c.Holder = "carol" })(&s)
	w := worldFrom(t, s)
	w.bus.present["carol"] = false
	w.e.Setup = &setupFake{}
	w.e.Lock = setupLeaseFake{}
	return w
}

// TestUpTakesOverFromACoordinatorWhoIsGone: up --take-over moves the
// coordinator's seat from a coordinator absent from the bus (Glenn,
// 2026-11-10, "Add takeover to up"): the old coordinator unhooked as unhook
// does it, the caller hooked in as coordinator, both in one logged op
// whose note is the reason, with the old coordinator's hand returned to the
// queue in it (Glenn, 2026-11-10: a held or down row holds no card); then
// the walk goes on, and the new coordinator's verbs work.
func TestUpTakesOverFromACoordinatorWhoIsGone(t *testing.T) {
	t.Parallel()
	w := takeoverWorld(t)
	want(t, w.do("dave", "up", "--take-over", "--reason", "carol's host is down"), 0,
		"TOOK OVER from carol: carol's host is down; its hand returned to the queue, branches and heads kept: p1.w1@1", "HOOKED dave role=coordinator", "UP DONE", "coordinator rowan")
	s := w.snap(t)
	if r := s.Rows["carol"]; r.Session != "" || r.Role != "" {
		t.Fatalf("carol still hooked: %+v", r)
	}
	if r := s.Rows["dave"]; r.Session != "s-dave" || r.Role != "coordinator" {
		t.Fatalf("dave %+v", r)
	}
	if c := s.Cards["p1.w1"]; c.State != "fail" || c.Reason != card.ReleasedPrefix+card.Returned("carol", "unhooked by a take-over") {
		t.Fatalf("carol's card %+v, want returned to the queue", c)
	}
	events, err := w.f.Events(context.Background(), 10, 0)
	if err != nil || len(events) == 0 {
		t.Fatal(events, err)
	}
	ev := events[0]
	if ev.Kind != "take-over" || ev.Actor != "dave" || !ev.Accepted || ev.Receipt.Note != "took over from carol: carol's host is down" {
		t.Fatalf("log entry %+v", ev)
	}
	want(t, w.do("dave", "judge"), 0, "no open judgements")
	want(t, w.do("dave", "unhook", "alice"), 0, "UNHOOKED alice")
}

// realCoordinatorStep is up's walk faked except its coordinator step, which
// is the real one (SetupPrerequisites) on the sprint's store and the
// world's bus.
type realCoordinatorStep struct {
	*setupFake
	real SetupPrerequisites
}

func (s realCoordinatorStep) Verify(ctx context.Context, n int, in SetupInput) (SetupCheck, error) {
	if n == StepCoordinator {
		return s.real.Coordinator(ctx, in.Actor)
	}
	return s.setupFake.Verify(ctx, n, in)
}

func (s realCoordinatorStep) Act(ctx context.Context, n int, in SetupInput) error {
	if n == StepCoordinator {
		r := s.real
		r.Env.Choices.Actor = in.Actor
		return r.SaveCoordinator(ctx, in.Actor)
	}
	return s.setupFake.Act(ctx, n, in)
}

// TestUpTakeOverMovesTheSavedCoordinator: after a take-over, up's own
// coordinator step checks and records the new coordinator, not the one the
// first up saved: with the old coordinator on the bus, and after it leaves.
func TestUpTakeOverMovesTheSavedCoordinator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := sprint()
	s.Seq = 0 // the saved answers are read from the whole log
	w := worldFrom(t, s)
	w.e.Lock = setupLeaseFake{}
	who := func(context.Context, string, ...string) ([]byte, error) {
		var names []string
		for _, n := range []string{"carol", "dave"} {
			if w.bus.present[n] {
				names = append(names, n+"\t1m\n")
			}
		}
		return []byte("WHO OK present=" + string(rune('0'+len(names))) + "\n" + strings.Join(names, "")), nil
	}
	steps := realCoordinatorStep{&setupFake{}, SetupPrerequisites{Env: ExecEnvironment{Getenv: func(string) string { return "" }, Command: who,
		Choices: Choices{Store: w.f, Now: func() time.Time { return t0 }}}}}
	w.e.Setup = steps
	want(t, w.do("carol", "up"), 0, "UP 5/10 coordinator carol: VERIFIED")
	r := w.do("dave", "up", "--take-over", "--reason", "the seat was handed to dave")
	want(t, r, 0, "TOOK OVER from carol", "UP 5/10 coordinator dave: VERIFIED")
	if strings.Contains(r.Output, "coordinator carol") {
		t.Fatalf("up checked the old coordinator:\n%s", r.Output)
	}
	if saved, _, err := steps.real.Env.Choices.Get(ctx, "coordinator"); err != nil || saved != "dave" {
		t.Fatalf("saved coordinator %q %v, want dave", saved, err)
	}
	w.bus.present["carol"] = false
	want(t, w.do("dave", "up"), 0, "UP 5/10 coordinator dave: VERIFIED")
}

// inbox is the bus as the run's pusher sends on it: each recipient's
// messages, one "<subject>: <body>" line each.
type inbox map[string]string

func (i inbox) Send(_ context.Context, to, subject, body string) (string, error) {
	i[to] += subject + ": " + body + "\n"
	return "m-" + to, nil
}

func (inbox) Who(context.Context) (map[string]bus.Presence, error) { return nil, nil }

// TestUpTakesOverFromACoordinatorOnTheBus: the seat is handed over in
// conversation, so up --take-over goes through with the old coordinator
// still on the bus, as it does when that coordinator is gone, and the run's pusher
// tells the old coordinator who took the seat and why.
func TestUpTakesOverFromACoordinatorOnTheBus(t *testing.T) {
	t.Parallel()
	w := takeoverWorld(t)
	w.bus.present["carol"] = true
	want(t, w.do("dave", "up", "--take-over", "--reason", "the seat was handed to dave"), 0,
		"TOOK OVER from carol: the seat was handed to dave", "HOOKED dave role=coordinator", "UP DONE")
	s := w.snap(t)
	if r := s.Rows["carol"]; r.Session != "" || r.Role != "" {
		t.Fatalf("carol still hooked: %+v", r)
	}
	if r := s.Rows["dave"]; r.Session != "s-dave" || r.Role != "coordinator" {
		t.Fatalf("dave %+v", r)
	}
	events, err := w.f.Events(context.Background(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := run.NewPusher()
	for _, ev := range events {
		p.Observe(ev, s)
	}
	sent := inbox{}
	if failed := p.Push(context.Background(), sent, w.f, s.Epoch, t0); len(failed) != 0 {
		t.Fatalf("push failed: %+v", failed)
	}
	if !strings.Contains(sent["carol"], "TAKEN OVER by=dave: took over from carol: the seat was handed to dave") {
		t.Fatalf("carol was not told the seat moved: %q", sent["carol"])
	}
}

// A displaced coordinator that hooks back in (as a worker) before the pusher
// reads the take-over is one recipient of it, told once.
func TestTakeOverTellsARehookedCoordinatorOnce(t *testing.T) {
	t.Parallel()
	w := takeoverWorld(t)
	w.bus.present["carol"] = true
	want(t, w.do("dave", "up", "--take-over", "--reason", "the seat was handed to dave"), 0, "TOOK OVER from carol")
	s := w.snap(t)
	rows := maps.Clone(s.Rows)
	carol := rows["carol"]
	carol.Session, carol.Role = "s-other", "worker"
	rows["carol"] = carol
	s.Rows = rows
	events, err := w.f.Events(context.Background(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := run.NewPusher()
	for _, ev := range events {
		p.Observe(ev, s)
	}
	sent := inbox{}
	if failed := p.Push(context.Background(), sent, w.f, s.Epoch, t0); len(failed) != 0 {
		t.Fatalf("push failed: %+v", failed)
	}
	if n := strings.Count(sent["carol"], "TAKEN OVER by=dave"); n != 1 {
		t.Fatalf("carol told %d times: %q", n, sent["carol"])
	}
}

func TestUpTakeOverRefusals(t *testing.T) {
	t.Parallel()
	hint := `nova-sprint up --as dave --take-over --reason "<why>"`
	cases := []struct {
		name string
		prep func(t *testing.T, w *world)
		args []string
		exit int
		want []string
	}{
		{name: "no reason", args: []string{"up", "--take-over"}, exit: 1,
			want: []string{"SPRINT REFUSED up: --take-over needs --reason: " + hint}},
		{name: "an empty reason", args: []string{"up", "--take-over", "--reason", ""}, exit: 2,
			want: []string{"SPRINT REFUSED up: --reason needs one non-empty value"}},
		{name: "a reason without the flag", args: []string{"up", "--reason", "x"}, exit: 2,
			want: []string{"--reason goes with --take-over"}},
		{name: "without the flag, the old refusal names it", args: []string{"up"}, exit: 1,
			want: []string{"SPRINT REFUSED up: the sprint's coordinator is carol: ask carol to run nova-sprint unhook --as carol first; only when the operator says so, take the seat: " + hint}},
		{name: "hook names it too", args: []string{"hook", "--role", "coordinator"}, exit: 1,
			want: []string{"the sprint's coordinator is carol", "only when the operator says so, take the seat: " + hint}},
		{name: "the taker is hooked already", prep: func(t *testing.T, w *world) { w.ok(t, "dave", "hook", "--role", "worker") },
			args: []string{"up", "--take-over", "--reason", "carol is gone"}, exit: 1,
			want: []string{"dave is hooked in as worker; unhook first to change role", "nova-sprint unhook --as dave", "then " + hint}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := takeoverWorld(t)
			if c.prep != nil {
				c.prep(t, w)
			}
			want(t, w.do("dave", c.args...), c.exit, c.want...)
			if r := w.snap(t).Rows["dave"]; r.Role == "coordinator" {
				t.Fatalf("dave took the seat: %+v", r)
			}
		})
	}
}

// TestUpTakeOverWithNoCoordinator: nothing to take over is the plain hook,
// with no take-over logged.
func TestUpTakeOverWithNoCoordinator(t *testing.T) {
	t.Parallel()
	w := takeoverWorld(t)
	w.bus.present["carol"] = true
	w.ok(t, "carol", "unhook")
	w.bus.present["carol"] = false
	r := w.do("dave", "up", "--take-over", "--reason", "nobody holds it")
	want(t, r, 0, "HOOKED dave role=coordinator", "UP DONE")
	if strings.Contains(r.Output, "TOOK OVER") {
		t.Fatal(r.Output)
	}
}
