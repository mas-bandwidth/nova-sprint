package verbs

import (
	"context"
	"strings"
	"testing"

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
// whose note is the reason; the old coordinator's cards stay (a row going
// down never takes its cards back); then the walk goes on, and the new
// coordinator's verbs work.
func TestUpTakesOverFromACoordinatorWhoIsGone(t *testing.T) {
	t.Parallel()
	w := takeoverWorld(t)
	want(t, w.do("dave", "up", "--take-over", "--reason", "carol's host is down"), 0,
		"TOOK OVER from carol: carol's host is down", "HOOKED dave role=coordinator", "UP DONE", "coordinator rowan")
	s := w.snap(t)
	if r := s.Rows["carol"]; r.Session != "" || r.Role != "" {
		t.Fatalf("carol still hooked: %+v", r)
	}
	if r := s.Rows["dave"]; r.Session != "minted-1" || r.Role != "coordinator" {
		t.Fatalf("dave %+v", r)
	}
	if h := s.Cards["p1.w1"].Holder; h != "carol" {
		t.Fatalf("carol's card moved to %q; the orphan judgement handles it", h)
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
		{name: "the coordinator is on the bus", prep: func(_ *testing.T, w *world) { w.bus.present["carol"] = true },
			args: []string{"up", "--take-over", "--reason", "carol is slow"}, exit: 1,
			want: []string{"SPRINT REFUSED up: carol is on the bus: ask carol to run nova-sprint unhook --as carol"}},
		{name: "no reason", args: []string{"up", "--take-over"}, exit: 1,
			want: []string{"SPRINT REFUSED up: --take-over needs --reason: " + hint}},
		{name: "a reason without the flag", args: []string{"up", "--reason", "x"}, exit: 2,
			want: []string{"--reason goes with --take-over"}},
		{name: "without the flag, the old refusal names it", args: []string{"up"}, exit: 1,
			want: []string{"SPRINT REFUSED up: the sprint's coordinator is carol: ask carol to run nova-sprint unhook --as carol first; if carol is gone: " + hint}},
		{name: "hook names it too", args: []string{"hook", "--role", "coordinator"}, exit: 1,
			want: []string{"the sprint's coordinator is carol", "if carol is gone: " + hint}},
		{name: "the taker is hooked already", prep: func(t *testing.T, w *world) { w.ok(t, "dave", "hook", "--role", "worker") },
			args: []string{"up", "--take-over", "--reason", "carol is gone"}, exit: 1,
			want: []string{"dave is already hooked in as worker", "nova-sprint unhook --as dave"}},
		{name: "the seat moved while the bus was asked", prep: func(t *testing.T, w *world) {
			calls := 0
			w.bus.during = func() { // on the take-over's own who (up's second: the first is dave's), carol comes back and hands the seat to erin
				if calls++; calls != 2 {
					return
				}
				w.bus.during = nil
				w.bus.present["carol"], w.bus.present["erin"] = true, true
				w.ok(t, "carol", "unhook")
				w.ok(t, "erin", "hook", "--role", "coordinator")
				w.bus.present["carol"] = false
			}
		}, args: []string{"up", "--take-over", "--reason", "carol is gone"}, exit: 1,
			want: []string{"the sprint's coordinator changed to erin meanwhile", hint + " again"}},
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
