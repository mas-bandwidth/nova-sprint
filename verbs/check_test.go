package verbs

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// healthy is the world with run's name on the bus (the run line is green).
func healthy(_ *testing.T, w *world) { w.bus.present["sprint"] = true }

// then runs each prep in order.
func then(preps ...func(*testing.T, *world)) func(*testing.T, *world) {
	return func(t *testing.T, w *world) {
		for _, p := range preps {
			p(t, w)
		}
	}
}

// undealtSprint is the sprint with p1 ready and no consumer dealt.
func undealtSprint(s *store.Snapshot) {
	delete(s.Cards, "p1.w1")
	delete(s.Cards, "p2.r1")
	c := s.Cards["p1"]
	c.State = "ready"
	s.Cards["p1"] = c
}

func judgement(id, kind, about string, at time.Time) store.Judgement {
	return store.Judgement{ID: id, Kind: kind, About: about, Question: "q " + id, State: "open", OpenedAt: at,
		Command: "nova-sprint judge " + id + " <choice> --reason <why>   choices: retry"}
}

// TestCheckAllGreen is every line of a healthy sprint, in order, exit 0.
func TestCheckAllGreen(t *testing.T) {
	w := newWorld(t)
	healthy(t, w)
	r := w.do("", "check")
	wantLines := []string{"CHECK fleet green", "CHECK nova-tools v2.0.2 (pin v2.0.2)", "CHECK store at schema", "CHECK bus answering (5 present)",
		"CHECK stream s: o/r reachable, base main at bbbbbbbbbbbb", "CHECK judgements: none open", "CHECK run on studio (pid 41)",
		"CHECK width: working 2 (2 dealt) of 4 open on 3 rows; 0 ready", "CHECK landings: none waiting", "next: nova-sprint next --as <you>"}
	if r.Exit != 0 || r.Output != strings.Join(wantLines, "\n")+"\n" {
		t.Fatalf("exit %d:\n%s", r.Exit, r.Output)
	}
}

// TestCheckRedLines is each red line, and that it makes the exit non-zero
// with every other line still printed.
func TestCheckRedLines(t *testing.T) {
	at := t0.Add(-time.Hour)
	runCases(t, []tcase{
		{name: "a repository git cannot read", args: []string{"check"}, exit: 2, prep: then(healthy, func(_ *testing.T, w *world) { w.origin.err = errBroken }),
			want: []string{"CHECK RED stream s: git ls-remote of o/r failed (connection refused): an <org>/<repo> is cloned through its forge: give stream s --forge github, then nova-sprint check", "CHECK landings: none waiting"}},
		{name: "a base not at origin", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { delete(w.origin.tips, "o/r main") }),
			want: []string{"CHECK RED stream s: branch main is not at o/r's origin (o/r): push it, or pass --base <branch>, then nova-sprint check"}},
		{name: "a github stream is read at its https address", args: []string{"check"}, exit: 0, prep: healthy,
			snap: func(s *store.Snapshot) { st := s.Streams["s"]; st.Forge = "github"; s.Streams["s"] = st },
			want: []string{"CHECK stream s: o/r reachable, base main at bbbbbbbbbbbb"}},
		{name: "open judgements by kind", args: []string{"check"}, exit: 1, prep: healthy,
			snap: func(s *store.Snapshot) {
				for i, about := range []string{"a", "b", "c", "d", "e", "f"} {
					id := "engine." + about + ".1"
					s.Judgements[id] = judgement(id, "engine", about, at.Add(time.Duration(i)*time.Minute))
				}
				s.Judgements["flap.alice.1"] = judgement("flap.alice.1", "flap", "alice", at)
			},
			want: []string{"CHECK RED 6 engine judgements are open (engine.a.1, engine.b.1, engine.c.1, engine.d.1, engine.e.1 and 1 more); the oldest: q engine.a.1: nova-sprint judge <id> <choice> --reason <why>   choices: retry (each id; nova-sprint next lists them)",
				"CHECK RED judgement flap.alice.1 is open: q flap.alice.1: nova-sprint judge flap.alice.1 <choice>", "SPRINT REFUSED check: 2 lines are red"}},
		{name: "nothing runs", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.run = run.ChooserStatus{} }),
			want: []string{"CHECK RED nothing runs the sprint: the chooser lock is free"}},
		{name: "a restore holds the lock", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) {
			w.doctor.run = run.ChooserStatus{Held: true, Operation: "restore", Host: "db"}
		}),
			want: []string{"CHECK RED the chooser lock is held by restore on db, not run"}},
		{name: "run off the bus", args: []string{"check"}, exit: 1,
			want: []string{"CHECK RED run holds the lock on studio but the name sprint is not on the bus"}},
		{name: "run unreadable", args: []string{"check"}, exit: 2, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.runErr = errBroken }),
			want: []string{"CHECK RED who runs the sprint cannot be read (connection refused): nova-sprint show run"}},
		{name: "tools below the pin", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.tools = "v2.0.1" }),
			want: []string{"CHECK RED nova-tools v2.0.1 is below the pin v2.0.2: nova-fleet install --tag v2.0.2 <machine>", "CHECK landings: none waiting"}},
		{name: "tools above the pin warn", args: []string{"check"}, exit: 0, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.tools = "v2.0.3" }),
			want: []string{"CHECK WARN nova-tools v2.0.3 is above the pin v2.0.2 (this build was checked against v2.0.2): install the nova-sprint release that pins v2.0.3, or nova-fleet install --tag v2.0.2 <machine>"}},
		{name: "the store not set up skips the sprint", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.schema = false }),
			want: []string{"CHECK RED the store is not at this build's schema: nova-sprint up --as <coordinator>", "CHECK bus answering"}},
	})
}

// TestCheckWhyNothingDeals is the width line red with each cause the deal
// meets, the first one's fix on the refusal line.
func TestCheckWhyNothingDeals(t *testing.T) {
	width := "nothing deals: width: working 0 (0 dealt) of 4 open on 3 rows; 1 ready; why: "
	runCases(t, []tcase{
		{name: "a held stream", args: []string{"check"}, exit: 1, prep: healthy,
			snap: func(s *store.Snapshot) { undealtSprint(s); st := s.Streams["s"]; st.Held = true; s.Streams["s"] = st },
			want: []string{"CHECK RED " + width + "1 ready in held streams (s): nova-sprint unhold <stream>"}},
		{name: "no cause visible", args: []string{"check"}, exit: 1, prep: healthy, snap: undealtSprint,
			want: []string{"CHECK RED " + width + "1 ready with no cause visible from here: nova-sprint show run, then the run service's log"}},
		{name: "no base head", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { delete(w.origin.tips, "o/r main") }), snap: undealtSprint,
			want: []string{"CHECK RED " + width + "1 ready in streams with no base head (s): fix each stream's red line above"}},
		{name: "run has no head", args: []string{"check"}, exit: 1, prep: healthy,
			snap: func(s *store.Snapshot) {
				undealtSprint(s)
				s.Judgements["engine.s.1"] = judgement("engine.s.1", "engine", "s", t0)
			},
			want: []string{"1 ready in streams with no base head (s)"}},
		{name: "a tier no row serves", args: []string{"check"}, exit: 1, snap: undealtSprint,
			prep: then(healthy, func(_ *testing.T, w *world) {
				for n, wk := range w.roster.ro.Workers {
					wk.Tiers = []string{"flash"}
					w.roster.ro.Workers[n] = wk
				}
				for n, k := range w.roster.ro.Kin {
					k.Tiers = []string{"flash"}
					w.roster.ro.Kin[n] = k
				}
			}),
			want: []string{width + "1 ready at tiers no open row serves (pro): hook a row that serves the tier"}},
		{name: "stopped, with the steps' own words", args: []string{"check"}, exit: 1, prep: healthy,
			snap: func(s *store.Snapshot) {
				undealtSprint(s)
				s.Stopped, s.StopReason = true, "deploy"
				s.Judgements["idle.fleet.1"] = judgement("idle.fleet.1", "idle", "fleet", t0)
			},
			want: []string{width + "the sprint is stopped (deploy); the steps say: q idle.fleet.1: nova-sprint start --reason"}},
		{name: "no open row", args: []string{"check"}, exit: 1, prep: healthy,
			snap: func(s *store.Snapshot) {
				undealtSprint(s)
				for n, r := range s.Rows {
					r.Held = true
					s.Rows[n] = r
				}
			},
			want: []string{"nothing deals: width: working 0 (0 dealt) of 0 open on 0 rows; 1 ready; why: no row is open (up, unheld and hooked); 1 ready at tiers no open row serves (pro)"}},
	})
}

// TestCheckLandingWaits is each landing's wait, red when a person or a
// stopped piece must act.
func TestCheckLandingWaits(t *testing.T) {
	merging := func(s *store.Snapshot) {
		c := s.Cards["p2"]
		c.State = "merging"
		s.Cards["p2"] = c
		delete(s.Cards, "p2.r1")
	}
	runCases(t, []tcase{
		{name: "waits for the merge", args: []string{"check"}, exit: 0, prep: healthy,
			snap: func(s *store.Snapshot) { merging(s); st := s.Streams["s"]; st.Forge = "github"; s.Streams["s"] = st },
			want: []string{"CHECK landing s: 1 merging (p2) waits for run to merge its heads into sprint/s/group"}},
		{name: "waits for CI at the group tip", args: []string{"check"}, exit: 0,
			prep: then(healthy, func(_ *testing.T, w *world) { w.origin.tips[gh+" sprint/s/group"] = tip }),
			snap: func(s *store.Snapshot) { merging(s); st := s.Streams["s"]; st.Forge = "github"; s.Streams["s"] = st },
			want: []string{"CHECK landing s: 1 merging (p2) waits for CI on sprint/s/group at dddddddddddd (run asks every 30s)"}},
		{name: "a hand stream", args: []string{"check"}, exit: 0, prep: healthy,
			snap: func(s *store.Snapshot) { merging(s); st := s.Streams["s"]; st.Mode = "hand"; s.Streams["s"] = st },
			want: []string{"CHECK landing s: 1 merging (p2) waits for a hand landing (mode hand, forge none)"}},
		{name: "a stall judgement", args: []string{"check"}, exit: 1, prep: healthy,
			snap: func(s *store.Snapshot) {
				merging(s)
				s.Judgements["stall.s.1"] = judgement("stall.s.1", "stall", "s", t0)
			},
			want: []string{"CHECK RED landing s: 1 merging (p2) waits for judgement stall.s.1 (stall, open since 2026-11-06 12:00:00): q stall.s.1: nova-sprint judge stall.s.1 <choice>"}},
		{name: "a held stream", args: []string{"check"}, exit: 1, prep: healthy,
			snap: func(s *store.Snapshot) { merging(s); st := s.Streams["s"]; st.Held = true; s.Streams["s"] = st },
			want: []string{"CHECK RED landing s: 1 merging (p2) waits for the stream's hold to end: nova-sprint unhold s"}},
		{name: "nothing runs", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.run = run.ChooserStatus{} }), snap: merging,
			want: []string{"CHECK RED landing s: 1 merging (p2) waits for run, and nothing runs the sprint"}},
	})
}
