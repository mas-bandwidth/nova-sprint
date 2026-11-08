package verbs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// healthy is the world with run's name on the bus (the run line is green).
func healthy(t *testing.T, w *world) {
	w.bus.present["sprint"] = true
	activePolicy(t, w)
}

// activePolicy stores and activates a policy version (the fake answers it).
func activePolicy(t *testing.T, w *world) {
	t.Helper()
	s := w.snap(t)
	applyTest(t, w.f, store.Op{ID: "policy-up", Epoch: s.Epoch, Actor: "carol", Kind: "policy", At: t0,
		Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "default", Body: "script"}, store.SetActivePolicy{Version: 1}}})
}

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
	t.Parallel()
	w := newWorld(t)
	healthy(t, w)
	r := w.do("", "check")
	wantLines := []string{"CHECK fleet green", "CHECK nova-tools v2.0.2 (pin v2.0.2)", "CHECK store at schema", "CHECK bus answering (5 present)",
		"CHECK stream s: o/r reachable, base main at bbbbbbbbbbbb",
		"CHECK identity: every commit is made as Sprint Bot <1+sprint-bot@users.noreply.example.test> (hook prints the two git config lines; finish refuses a commit by another email)",
		"CHECK policy v1 active (answered by v0; 0 ready producers asked)", "CHECK cards: none waiting or ready",
		"CHECK working: 2 cards, each on a hooked row that is up and on the bus (a job dead on its bench is not visible here: no per-job heartbeat reaches the store)",
		"CHECK workers: no bench in nova-config's inventory", "CHECK judgements: none open", "CHECK run on studio (pid 41)",
		"CHECK width: working 2 (2 dealt) of 4 open on 3 rows; 0 ready", "CHECK landings: none waiting", "next: nova-sprint next --as <you>"}
	if r.Exit != 0 || r.Output != strings.Join(wantLines, "\n")+"\n" {
		t.Fatalf("exit %d:\n%s", r.Exit, r.Output)
	}
}

// TestCheckRedLines is each red line, and that it makes the exit non-zero
// with every other line still printed.
func TestCheckRedLines(t *testing.T) {
	t.Parallel()
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
		{name: "no commit identity", args: []string{"check"}, exit: 1, prep: healthy, snap: func(s *store.Snapshot) { s.Identity = store.Identity{} },
			want: []string{"CHECK RED the sprint has no commit identity: no worker can hook and nothing can be committed: nova-sprint identity --name \"<git user.name>\" --email \"<git user.email>\" --reason \"<why>\""}},
		{name: "a bench tier with no bench model", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { w.e.Answers = noBench{"heavy", "frontier"} }),
			want: []string{"CHECK RED policy v1 names no bench model for tier frontier, heavy, so no bench is dealt a card of it: give the policy's catalog() a \"bench\": {\"<tier>\": \"<provider/model>\"} entry for each, then nova-sprint policy set <file> --reason \"<why>\""}},
		{name: "the store not set up skips the sprint", args: []string{"check"}, exit: 1, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.schema = false }),
			want: []string{"CHECK RED the store is not at this build's schema: nova-sprint up --as <coordinator>", "CHECK bus answering"}},
	})
}

// noBench answers as fakeAnswers with no bench model for its tiers (a
// policy stored before the catalog had a bench).
type noBench []string

func (n noBench) AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure) {
	a, fs := fakeAnswers{}.AnswerContext(ctx, vs, snap, ro, q)
	for _, t := range n {
		delete(a.Catalog.Bench, t)
	}
	return a, fs
}

// TestCheckWhyNothingDeals is the width line red with each cause the deal
// meets, the first one's fix on the refusal line.
func TestCheckWhyNothingDeals(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	merging := func(s *store.Snapshot) {
		c := s.Cards["p2"]
		c.State = "merging"
		s.Cards["p2"] = c
		delete(s.Cards, "p2.r1")
	}
	runCases(t, []tcase{
		{name: "waits for its landing", args: []string{"check"}, exit: 0, prep: healthy,
			snap: func(s *store.Snapshot) { merging(s); st := s.Streams["s"]; st.Forge = "github"; s.Streams["s"] = st },
			want: []string{"CHECK landing s: 1 merging (p2) waits for its landing PR's CI (rung 1 the card's branch, rung 2 sprint-group/s; run asks every 30s)"}},
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

// failingAnswers is a policy that fails the order question.
type failingAnswers struct{ fakeAnswers }

func (f failingAnswers) AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure) {
	a, _ := f.fakeAnswers.AnswerContext(ctx, vs, snap, ro, q)
	if len(q.Order) == 0 {
		return a, nil
	}
	return a, []policy.Failure{{Version: 1, Label: "v1", Question: "order", Subject: q.Order[0], Line: 3, Msg: "boom"}}
}

// TestCheckReprovesAdmission: check re-proves admission (intent rule 34)
// and says what directive 75 found it silent on: no policy active, a policy
// failing a question, a working card with no live job, a card that would
// not be admitted today, and a stream whose card branches cannot be made.
func TestCheckReprovesAdmission(t *testing.T) {
	t.Parallel()
	ready := func(s *store.Snapshot) {
		undealtSprint(s)
		c := s.Cards["p1"]
		c.Paths = []string{"z/q.go"}
		s.Cards["p1"] = c
	}
	unhookedHolder := func(s *store.Snapshot) {
		c := s.Cards["p1.w1"]
		c.State = "working"
		s.Cards["p1.w1"] = c
		r := s.Rows["alice"]
		r.Session, r.Role = "", ""
		s.Rows["alice"] = r
	}
	runCases(t, []tcase{
		{name: "no policy active", args: []string{"check"}, exit: 1, prep: func(_ *testing.T, w *world) { w.bus.present["sprint"] = true },
			want: []string{"CHECK RED no policy is active: the tick runs on the built-in default: " + policy.FirstPolicyFix}},
		{name: "a policy failing a question", args: []string{"check"}, exit: 1, snap: undealtSprint,
			prep: then(healthy, func(_ *testing.T, w *world) { w.e.Answers = failingAnswers{} }),
			want: []string{"CHECK RED policy v1 fails 1 question(s); the first: POLICY REFUSED v1 line 3 order(p1): boom", "nova-sprint policy check v1"}},
		{name: "a ready card not admitted today", args: []string{"check"}, exit: 1, snap: ready, prep: healthy,
			want: []string{"CHECK RED card p1: PATHS z/q.go: neither it nor its directory z/ is there at o/r's main (bbbbbbbbbbbb)"}},
		{name: "a working card with no live job", args: []string{"check"}, exit: 1, snap: unhookedHolder, prep: healthy,
			want: []string{"CHECK RED 1 working cards have no live job: p1.w1 on alice (not hooked)", "nova-sprint release <card>@<gen>"}},
		{name: "a working card off the bus", args: []string{"check"}, exit: 1, snap: func(s *store.Snapshot) {
			c := s.Cards["p1.w1"]
			c.State = "working"
			s.Cards["p1.w1"] = c
		}, prep: then(healthy, func(_ *testing.T, w *world) { w.bus.present["alice"] = false }),
			want: []string{"p1.w1 on alice (not on the bus)"}},
		{name: "a stream whose card branches cannot be made", args: []string{"check"}, exit: 1,
			prep: then(healthy, func(_ *testing.T, w *world) { w.origin.tips["o/r sprint/s"] = tip }),
			want: []string{"CHECK RED stream s: branch sprint/s is at o/r's origin"}},
	})
}

// TestCheckWorkers is each worker bench's service (D14, 2026-11-07: every
// bench's unit crash-looped for days, the installed worker refusing flags an
// earlier one took, while check said every working card was on a hooked row
// that was up and on the bus): red when the unit's last log line is a
// refusal, red when the bench is not hooked and up on the bus, one WARN for
// a log or an inventory that cannot be read, and show <row> carrying the
// same fact.
func TestCheckWorkers(t *testing.T) {
	t.Parallel()
	refused := "SPRINT REFUSED worker: unknown flag --work-clone: nova-sprint help worker"
	units := func(us ...WorkerUnit) func(*testing.T, *world) {
		return func(_ *testing.T, w *world) { w.doctor.units = us }
	}
	alice := func(line string) WorkerUnit {
		return WorkerUnit{Machine: "alice", Service: "sprint-worker-alice", LastLine: line}
	}
	fix := "nova-sprint up --as <coordinator> (it rewrites every bench's worker service record and applies it)"
	// The refusal quotes the unit's own refusal line, so this output names
	// SPRINT REFUSED more than once (want's one-refusal rule does not apply).
	w := newWorld(t)
	healthy(t, w)
	units(alice(refused), WorkerUnit{Machine: "bob", Service: "sprint-worker-bob"})(t, w)
	r := w.do("", "check")
	red := "CHECK RED worker alice: \"" + refused + "\" (its unit exits on it and is restarted, forever): " + fix
	if r.Exit != 1 || !strings.Contains(r.Output, red+"\n") || !strings.HasSuffix(r.Output, "SPRINT REFUSED check: "+strings.TrimPrefix(red, "CHECK RED ")+"\n") {
		t.Fatalf("exit %d:\n%s", r.Exit, r.Output)
	}
	runCases(t, []tcase{
		{name: "every bench serving", args: []string{"check"}, exit: 0, prep: then(healthy, units(alice("WORKER alice up"), WorkerUnit{Machine: "bob", Service: "sprint-worker-bob"})),
			want: []string{"CHECK workers: 2 benches, each hooked, up and on the bus, its unit's last log line no refusal"}},
		{name: "a bench not hooked", args: []string{"check"}, exit: 1, prep: then(healthy, units(alice("WORKER alice down"))),
			snap: func(s *store.Snapshot) { r := s.Rows["alice"]; r.Session, r.Role = "", ""; s.Rows["alice"] = r },
			want: []string{"CHECK RED worker alice: not hooked in the store (last log line \"WORKER alice down\"): read ~/.local/state/nova/services/sprint-worker-alice.log on alice and fix what it says, or take the bench role off a machine that is gone; then nova-sprint up --as <coordinator>"}},
		{name: "a bench off the bus", args: []string{"check"}, exit: 1, prep: then(healthy, units(alice("")), func(_ *testing.T, w *world) { w.bus.present["alice"] = false }),
			want: []string{"CHECK RED worker alice: not on the bus (the log is empty): read ~/.local/state/nova/services/sprint-worker-alice.log on alice"}},
		{name: "a bench with no row", args: []string{"check"}, exit: 1, prep: then(healthy, units(WorkerUnit{Machine: "eve", Service: "sprint-worker-eve", LastLine: "x"})),
			want: []string{"CHECK RED worker eve: no row in the store (last log line \"x\")"}},
		{name: "a log that cannot be read", args: []string{"check"}, exit: 0, prep: then(healthy, units(WorkerUnit{Machine: "alice", Service: "sprint-worker-alice", Unread: "unreadable over ssh to alice (no route)"})),
			want: []string{"CHECK WARN worker alice: its unit's last log line is unreadable (unreadable over ssh to alice (no route)): read ~/.local/state/nova/services/sprint-worker-alice.log on alice"}},
		// F1: a bench whose log cannot be read still has its row and bus
		// checked; a powered-off bench (ssh fails, off the bus) is red.
		{name: "an unreadable bench off the bus", args: []string{"check"}, exit: 1, prep: then(healthy, units(WorkerUnit{Machine: "alice", Service: "sprint-worker-alice", Unread: "unreadable over ssh to alice (no route)"}), func(_ *testing.T, w *world) { w.bus.present["alice"] = false }),
			want: []string{"CHECK RED worker alice: not on the bus (its log unreadable over ssh to alice (no route)): read ~/.local/state/nova/services/sprint-worker-alice.log on alice"}},
		// F2: a held bench is held on purpose, its row state no fault; an
		// unheld down row is named as the store's state.
		// (exit 1 is check working's line for alice's card, not a worker line)
		{name: "a held bench down", args: []string{"check"}, exit: 1, prep: then(healthy, units(alice("WORKER alice down"), WorkerUnit{Machine: "bob", Service: "sprint-worker-bob"})),
			snap: func(s *store.Snapshot) { r := s.Rows["alice"]; r.Held, r.State = true, "down"; s.Rows["alice"] = r },
			want: []string{"CHECK workers: 2 benches, each hooked, up and on the bus, its unit's last log line no refusal"}},
		{name: "a bench whose store row is down", args: []string{"check"}, exit: 1, prep: then(healthy, units(alice("WORKER alice up"))),
			snap: func(s *store.Snapshot) { r := s.Rows["alice"]; r.State = "down"; s.Rows["alice"] = r },
			want: []string{"CHECK RED worker alice: the store's row state is down (last log line \"WORKER alice up\")"}},
		{name: "benches that cannot be read", args: []string{"check"}, exit: 0, prep: then(healthy, func(_ *testing.T, w *world) { w.doctor.unitsErr = errBroken }),
			want: []string{"CHECK WARN workers: the benches cannot be read (connection refused): nova-config machine list --json on the control machine", "CHECK judgements: none open"}},
		{name: "show carries the fact", as: "carol", args: []string{"show", "alice"}, exit: 0, prep: units(alice(refused)),
			want: []string{`"role": "worker"`, `"service": "sprint-worker-alice"`, `"last_line": "` + refused + `"`, `"problem": "worker alice: \"` + refused + `\" (its unit exits on it and is restarted, forever)"`, `"fix": "nova-sprint up --as`}},
		{name: "show of a serving bench", as: "carol", args: []string{"show", "alice"}, exit: 0, prep: units(alice("WORKER alice up")),
			want: []string{`"last_line": "WORKER alice up"`}},
		{name: "show of a log that cannot be read", as: "carol", args: []string{"show", "alice"}, exit: 0, prep: units(WorkerUnit{Machine: "alice", Service: "sprint-worker-alice", Unread: "unreadable (no route)"}),
			want: []string{`"unread": "unreadable (no route)"`}},
	})
}

// A row that is no bench (dave, hooked from no bench) shows as the store's
// row alone, and a row whose bench the doctor cannot read says so.
func TestShowRowWithoutAWorker(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.doctor.units = []WorkerUnit{{Machine: "alice", Service: "sprint-worker-alice"}}
	if out := w.ok(t, "carol", "show", "dave"); strings.Contains(out, `"worker"`) {
		t.Fatal(out)
	}
	if out := w.ok(t, "carol", "show", "alice"); !strings.Contains(out, `"service": "sprint-worker-alice"`) || strings.Contains(out, `"problem"`) {
		t.Fatal(out)
	}
	w.doctor.unitsErr = errBroken
	want(t, w.do("carol", "show", "alice"), 0, `"unread": "connection refused"`)
}
