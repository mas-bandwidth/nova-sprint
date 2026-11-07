package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// The exact verb list (LAYER-6 section 3); clear is kept beside restore.
func TestTheVerbsAreExactlyTheList(t *testing.T) {
	want := []string{"next", "check", "up", "hook", "unhook", "help", "version",
		"take", "finish", "read", "report", "release", "beat", "show", "queue",
		"stream add", "stream set", "add", "judge", "land", "hold", "unhold", "priority", "drop", "relink", "replace",
		"policy set", "policy activate", "policy show", "policy diff", "policy check",
		"run", "worker", "dash", "stop", "start", "backup", "restore", "clear"}
	var got []string
	for _, v := range table {
		got = append(got, v.name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("verbs:\n%v\nwant\n%v", got, want)
	}
}

// The role matrix: who may run each verb.
func TestRoleMatrix(t *testing.T) {
	coordinator := map[string]bool{"stream add": true, "stream set": true, "add": true, "judge": true, "land": true, "hold": true, "unhold": true,
		"priority": true, "drop": true, "relink": true, "replace": true, "policy set": true, "policy activate": true,
		"stop": true, "start": true, "backup": true, "clear": true}
	worker := map[string]bool{"beat": true, "take": true, "finish": true, "read": true, "report": true, "release": true, "show": true, "queue": true,
		"policy show": true, "policy diff": true, "policy check": true}
	for _, v := range table {
		want := roleCold
		if coordinator[v.name] {
			want = roleCoordinator
		} else if worker[v.name] {
			want = roleWorker
		}
		if v.role != want {
			t.Errorf("%s is %v, want %v", v.name, v.role, want)
		}
	}
}

func TestEveryVerbHasHelp(t *testing.T) {
	w := newWorld(t)
	for _, v := range table {
		r := w.do("", append([]string{"help"}, strings.Fields(v.name)...)...)
		want(t, r, 0, "nova-sprint "+v.name, "who: ")
		if v.about == "" {
			t.Errorf("%s has no about", v.name)
		}
	}
	want(t, w.do("", "help"), 0, "coordinate:", "take", "help playbook")
	want(t, w.do("", "help", "policy"), 0, "policy set", "policy check")
	want(t, w.do("", "help", "playbook", "worker"), 0)
	want(t, w.do("", "-h"), 0, "coordinate:")
}

func TestCoreRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "unknown verb", args: []string{"edit"}, exit: 2, want: []string{"SPRINT REFUSED nova-sprint: unknown verb edit: nova-sprint help"}},
		{name: "help of no verb", args: []string{"help", "edit"}, exit: 2, want: []string{"no verb edit"}},
		{name: "help of no playbook", args: []string{"help", "playbook", "owner"}, exit: 2, want: []string{"worker and coordinator"}},
		{name: "unknown flag", as: "alice", args: []string{"queue", "--bogus"}, exit: 2, want: []string{"unknown flag --bogus"}},
		{name: "a switch given a value", as: "alice", args: []string{"next", "--json=yes"}, exit: 2, want: []string{"--json takes no value"}},
		{name: "a flag with no value", as: "alice", args: []string{"queue", "--stream"}, exit: 2, want: []string{"--stream needs a value"}},
		{name: "a flag twice", as: "alice", args: []string{"queue", "--stream", "s", "--stream", "s"}, exit: 2, want: []string{"needs one non-empty value"}},
		{name: "too many arguments", as: "alice", args: []string{"queue", "x"}, exit: 2, want: []string{"queue takes"}},
		{name: "a dash argument", as: "carol", args: []string{"hold", "-x", "--reason", "r"}, exit: 2, want: []string{"unknown argument -x"}},
		{name: "no name", args: []string{"queue"}, exit: 1, want: []string{"no name given", "--as <you>"}},
		{name: "a bad name", as: "a b", args: []string{"queue"}, exit: 1, want: []string{"is not a name the sprint takes"}},
		{name: "not on the bus", as: "erin", args: []string{"queue"}, exit: 1, want: []string{"erin is not on the bus", "nova-bus hook --as erin"}},
		{name: "on the bus, not hooked", as: "dave", args: []string{"queue"}, exit: 1, want: []string{"hook in first", "nova-sprint hook --as dave --role worker"}},
		{name: "a worker on a coordinator verb", as: "alice", args: []string{"stop", "--reason", "r"}, exit: 1, want: []string{"stop is the coordinator's, and alice is a worker", "ask carol"}},
		{name: "a coordinator verb with no coordinator", as: "alice", args: []string{"stop", "--reason", "r"}, exit: 1,
			snap: setRow("carol", func(r *store.Row) { r.Session, r.Role = "", "" }), want: []string{"the sprint has none"}},
		{name: "the bus fails", as: "alice", args: []string{"queue"}, exit: 2, prep: func(_ *testing.T, w *world) { w.bus.err = errBroken }, want: []string{"nova-bus who failed"}},
		{name: "no bus", as: "alice", args: []string{"queue"}, exit: 2, prep: func(_ *testing.T, w *world) { w.e.Bus = nil }, want: []string{"nova-bus is not configured"}},
		{name: "no store", as: "alice", args: []string{"queue"}, exit: 2, prep: func(_ *testing.T, w *world) { w.e.Store = nil }, want: []string{"the store is not configured"}},
		{name: "the store fails", as: "alice", args: []string{"queue"}, exit: 2, prep: func(_ *testing.T, w *world) { w.e.Store = failingStore{w.f} }, want: []string{"the store did not answer"}},
	})
}

// failingStore is a store whose reads fail.
type failingStore struct{ *store.Fake }

func (failingStore) Snapshot(context.Context) (store.Snapshot, error) {
	return store.Snapshot{}, errBroken
}

func TestVersion(t *testing.T) {
	want(t, newWorld(t).do("", "version"), 0, "nova-sprint v2.0.0-test")
}

func TestHook(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("dave", "hook", "--role", "worker"), 0, "HOOKED dave role=worker", "next: nova-sprint next --as dave")
	if r := w.snap(t).Rows["dave"]; r.Session != "minted-1" || r.Role != "worker" {
		t.Fatalf("row %+v", r)
	}
}

func TestHookRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no role", as: "dave", args: []string{"hook"}, exit: 2, want: []string{"--role is worker or coordinator"}},
		{name: "a bad role", as: "dave", args: []string{"hook", "--role", "owner"}, exit: 2, want: []string{"--role is worker or coordinator"}},
		{name: "not on the bus", as: "erin", args: []string{"hook", "--role", "worker"}, exit: 1, want: []string{"erin is not on the bus"}},
		{name: "a second hook", as: "alice", args: []string{"hook", "--role", "worker"}, exit: 1, want: []string{"alice is already hooked in as worker", "nova-sprint unhook --as alice"}},
		{name: "a second coordinator", as: "dave", args: []string{"hook", "--role", "coordinator"}, exit: 1, want: []string{"the sprint's coordinator is carol", "nova-sprint unhook --as carol"}},
		{name: "no session", as: "dave", args: []string{"hook", "--role", "worker"}, exit: 2,
			prep: func(_ *testing.T, w *world) {
				w.e.Session = func() (string, error) { return "", broken("no randomness", "again") }
			}, want: []string{"no randomness"}},
		{name: "the store without its schema", as: "dave", args: []string{"hook", "--role", "worker"}, exit: 1,
			prep: func(_ *testing.T, w *world) { w.e.Store = failingStore{w.f}; w.doctor.schema = false },
			want: []string{"SPRINT REFUSED hook: the sprint is not set up: nova-sprint up --as dave"}},
		{name: "no sprint credential", as: "dave", args: []string{"hook", "--role", "worker"}, exit: 1,
			prep: func(_ *testing.T, w *world) { w.e.Store = notSetUpStore{w.f} },
			want: []string{"SPRINT REFUSED hook: the sprint is not set up: nova-sprint up --as dave"}},
		{name: "no sprint credential in the process", as: "dave", args: []string{"hook", "--role", "worker"}, exit: 1,
			prep: func(_ *testing.T, w *world) { w.e.Store = credentiallessStore{w.f} },
			want: []string{"SPRINT REFUSED hook: this process has no sprint credential: " + CredentialFix}},
		{name: "a stream finish could not read", as: "dave", args: []string{"hook", "--role", "worker"}, exit: 1,
			prep: func(_ *testing.T, w *world) { delete(w.origin.tips, "o/r main") },
			want: []string{"finish and read could not reach stream s from this process: branch main is not at o/r's origin"}},
		{name: "a stream finish could not reach", as: "dave", args: []string{"hook", "--role", "worker"}, exit: 2,
			prep: func(_ *testing.T, w *world) { w.origin.err = errBroken },
			want: []string{"finish and read could not reach stream s from this process: git ls-remote of o/r failed", "then hook again"}},
		{name: "no sprint credential, next", as: "dave", args: []string{"next"},
			prep: func(_ *testing.T, w *world) { w.e.Store = notSetUpStore{w.f} },
			want: []string{"NEXT the sprint is not set up", "next: nova-sprint up --as dave"}},
	})
}

// credentiallessStore is the store as a process without
// NOVA_SPRINT_PG_PASSWORD reads it (cmd/nova-sprint: NoCredential).
type credentiallessStore struct{ *store.Fake }

func (credentiallessStore) Snapshot(context.Context) (store.Snapshot, error) {
	return store.Snapshot{}, NoCredential()
}

type notSetUpStore struct{ *store.Fake }

func (notSetUpStore) Snapshot(context.Context) (store.Snapshot, error) {
	return store.Snapshot{}, ErrNotSetUp
}

func TestUnhook(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("alice", "unhook"), 0, "UNHOOKED alice", "next: nova-sprint hook --as alice --role worker")
	want(t, w.do("alice", "unhook"), 0, "nothing to unhook")
	want(t, w.do("carol", "unhook", "bob"), 0, "UNHOOKED bob")
	if s := w.snap(t); s.Rows["alice"].Session != "" || s.Rows["bob"].Session != "" {
		t.Fatalf("rows %+v", s.Rows)
	}
}

func TestUnhookRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no name", args: []string{"unhook"}, exit: 1, want: []string{"no name given"}},
		{name: "another row as a worker", as: "alice", args: []string{"unhook", "bob"}, exit: 1, want: []string{"unhook is the coordinator's"}},
		{name: "another row not hooked", as: "carol", args: []string{"unhook", "dave"}, exit: 1, want: []string{"dave is not hooked in", "nova-sprint show dave"}},
	})
}

func TestNextGuides(t *testing.T) {
	runCases(t, []tcase{
		{name: "no name", args: []string{"next"}, want: []string{"choose the name", "next: nova-sprint next --as <name>"}},
		{name: "not on the bus", as: "erin", args: []string{"next"}, want: []string{"erin is not on the bus", "next: hook in to the bus first"}},
		{name: "not hooked", as: "dave", args: []string{"next"}, want: []string{"not hooked in", "next: nova-sprint hook --as dave --role worker"}},
		{name: "a dealt card", as: "alice", args: []string{"next"}, want: []string{"work p1.w1 is dealt with alice", "next: nova-sprint take p1.w1@1 --as alice --epoch 1"}},
		{name: "a card in hand", as: "alice", args: []string{"next"}, snap: setCard("p1.w1", func(c *store.Card) { c.State = "working" }),
			want: []string{"in hand", "next: nova-sprint finish p1.w1@1 --head <sha> --as alice --epoch 1"}},
		{name: "a read in hand", as: "bob", args: []string{"next"}, snap: setCard("p2.r1", func(c *store.Card) { c.State = "working" }),
			want: []string{"next: nova-sprint read p2.r1@1 yes|fix --as bob --epoch 1"}},
		{name: "nothing owed", as: "carol", args: []string{"next"}, want: []string{"nothing needs you", "next: " + waitNext}},
		{name: "a judgement owed", as: "carol", args: []string{"next"}, snap: withJudgement("blocked.p1.1", "", "blocked", "p1", "unblock | drop"),
			want: []string{"judgement blocked.p1.1 needs you", "next: nova-sprint judge blocked.p1.1"}},
		{name: "no stream", as: "carol", args: []string{"next"}, snap: func(s *store.Snapshot) { s.Streams = map[string]store.Stream{}; s.Cards = map[string]store.Card{} },
			want: []string{"no stream exists", "next: nova-sprint stream add"}},
		{name: "an empty stream", as: "carol", args: []string{"next"}, snap: func(s *store.Snapshot) { s.Cards = map[string]store.Card{} },
			want: []string{"stream s has no cards", "next: nova-sprint add --stream s"}},
		{name: "the bus fails", as: "alice", args: []string{"next"}, exit: 2, prep: func(_ *testing.T, w *world) { w.bus.err = errBroken }, want: []string{"nova-bus who failed"}},
		{name: "the store fails", as: "alice", args: []string{"next"}, exit: 2, prep: func(_ *testing.T, w *world) { w.e.Store = failingStore{w.f} }, want: []string{"the store did not answer"}},
		{name: "the store not set up", as: "carol", args: []string{"next"}, prep: func(_ *testing.T, w *world) { w.e.Store = failingStore{w.f}; w.doctor.schema = false },
			want: []string{"the sprint is not set up", "next: nova-sprint up --as carol"}},
		{name: "the fleet down", as: "carol", args: []string{"next"}, prep: func(_ *testing.T, w *world) {
			w.e.Store = failingStore{w.f}
			w.doctor.fleet = refuse("the fleet is down", "nova-fleet up")
		}, want: []string{"the fleet is down", "next: nova-fleet up"}},
	})
}

func TestNextJSONIsTheInventory(t *testing.T) {
	w := newWorld(t)
	r := w.do("alice", "next", "--json")
	want(t, r, 0)
	var v nextView
	if err := json.Unmarshal([]byte(r.Output), &v); err != nil {
		t.Fatal(err)
	}
	if v.Width != 1 || len(v.Ready) != 1 || v.Ready[0].ID != "p1.w1" || v.Working == nil || v.Final == nil || v.Judgements == nil {
		t.Fatalf("%+v", v)
	}
	// A bench's model by tier is the one the deal names, the catalog's bench
	// model in provider/model form (the probe's input); a friend's, its own.
	if got := v.TierModels["pro"]; !slices.Equal(got, []string{"inception/mercury-3"}) || len(v.TierModels) != 4 || len(v.TierModels["heavy"]) != 0 {
		t.Fatalf("tier_models %v", v.TierModels)
	}
	want(t, w.do("carol", "next", "--json"), 0, `"tier_models":{"flash":["m-carol"]`)
	w.roster.err = errors.New("nova-config down")
	want(t, w.do("alice", "next", "--json"), 0, `"width":0`, "roster_error", `"tier_models":{}`)
}

func withJudgement(id, group, kind, about, choices string) func(*store.Snapshot) {
	return func(s *store.Snapshot) {
		s.Judgements[id] = store.Judgement{ID: id, Kind: kind, About: about, Group: group, State: "open", OpenedAt: t0,
			Question: "what now?", Command: "nova-sprint judge " + id + " <choice> --reason <why>   choices: " + choices}
	}
}
