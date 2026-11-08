package verbs

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestMain serves the policy engine's child when the test binary is
// started as one (policy.NewEngine runs `<binary> policy-eval`).
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == policy.EvalArg {
		os.Exit(policy.ServeMain())
	}
	os.Exit(m.Run())
}

var (
	t0   = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	base = strings.Repeat("b", 40)
	head = strings.Repeat("c", 40)
	tip  = strings.Repeat("d", 40)
	gh   = "https://github.com/o/r.git" // o/r's clone address with forge github (git.CloneURL)
)

func applyTest(t *testing.T, f *store.Fake, op store.Op) {
	t.Helper()
	r, err := f.Apply(context.Background(), op)
	if err != nil || !r.Accepted {
		t.Fatalf("%+v %v", r, err)
	}
}

// setIdentity gives f the fixtures' commit identity (the coordinator's
// nova-sprint identity), as up's step 7 needs.
func setIdentity(t *testing.T, f *store.Fake) {
	t.Helper()
	applyTest(t, f, store.Op{ID: "identity", Epoch: 1, Actor: "alice", Kind: "identity", At: t0, Writes: []store.Write{store.SetIdentity{Name: testIdentity.Name, Email: testIdentity.Email}}})
}

// world is an Engine over store.Fake and fakes of every edge, with the
// sprint below: stream s; alice and bob workers, carol coordinator, all
// hooked; dave on the bus, not hooked. p1 is a producer with work consumer
// p1.w1 dealt to alice (ready, gen 1); p2 is in review with read consumer
// p2.r1 dealt to bob at alice's head.
type world struct {
	e      *Engine
	f      *store.Fake
	bus    *fakeBus
	origin *fakeOrigin
	doctor *fakeDoctor
	svc    *fakeServices
	arch   *fakeArchive
	roster *fakeRoster
	lander *fakeLander
	forge  *fakeForge
}

// testIdentity is the sprint's commit identity in the fixtures.
var testIdentity = store.Identity{Name: "Sprint Bot", Email: "1+sprint-bot@users.noreply.example.test"}

func sprint() store.Snapshot {
	row := func(n, role string) store.Row {
		return store.Row{Name: n, State: "up", Session: "s-" + n, Role: role, Gen: 1}
	}
	return store.Snapshot{Seq: 10, Epoch: 1,
		Streams:  map[string]store.Stream{"s": {Name: "s", Repo: "o/r", Base: "main", Mode: "auto"}},
		Identity: testIdentity,
		Rows:     map[string]store.Row{"alice": row("alice", "worker"), "bob": row("bob", "worker"), "carol": row("carol", "coordinator"), "dave": {Name: "dave", State: "up", Gen: 1}},
		Cards: map[string]store.Card{
			"p1":    {ID: "p1", Kind: "producer", Stream: "s", State: "working", Gen: 1, Attempt: 1, Tier: "pro", Priority: "normal", Brief: "tier: pro", Branch: "sprint/s/p1"},
			"p1.w1": {ID: "p1.w1", Kind: "work", Of: "p1", Stream: "s", State: "working", Gen: 1, Attempt: 1, Holder: "alice", Tier: "pro", Branch: "sprint/s/p1", BaseSHA: base, Brief: "do p1", Priority: "normal", Model: "inception/mercury-3"},
			"p2":    {ID: "p2", Kind: "producer", Stream: "s", State: "review", Gen: 1, Attempt: 1, Review: 1, Tier: "pro", Priority: "normal", Head: head, Author: "alice", Branch: "sprint/s/p2"},
			"p2.r1": {ID: "p2.r1", Kind: "read", Of: "p2", Stream: "s", State: "working", Gen: 1, Attempt: 1, Review: 1, Holder: "bob", Tier: "pro", Head: head, Author: "alice", Branch: "sprint/s/p2", Priority: "normal", Model: "inception/mercury-3"},
		},
		Judgements: map[string]store.Judgement{},
		Cursors:    map[string]int64{},
	}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	return worldFrom(t, sprint())
}

func worldFrom(t *testing.T, s store.Snapshot) *world {
	t.Helper()
	f, err := store.NewFakeFrom(s)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{f: f, bus: &fakeBus{present: map[string]bool{"alice": true, "bob": true, "carol": true, "dave": true}},
		origin: &fakeOrigin{tips: map[string]string{"o/r main": base, "o/r sprint/s/p1": head, gh + " main": base, gh + " sprint/s/p1": head}}, doctor: &fakeDoctor{tools: "v2.0.2", schema: true, run: run.ChooserStatus{Held: true, Operation: "run", Host: "studio", PID: 41}},
		svc: &fakeServices{}, arch: &fakeArchive{}, roster: newRoster(), lander: &fakeLander{merge: strings.Repeat("e", 40)}, forge: &fakeForge{}}
	n := 0
	w.e = &Engine{Store: f, Bus: w.bus, Roster: w.roster, Answers: fakeAnswers{}, Origin: w.origin, Forge: w.forge, Doctor: w.doctor, Services: w.svc, Archive: w.arch, Lander: w.lander,
		Now: func() time.Time { return t0 }, Version: "v2.0.0-test", ToolPin: "v2.0.2",
		Session: func() (string, error) { n++; return "minted-" + string(rune('0'+n)), nil }}
	return w
}

// do runs one command line as name.
func (w *world) do(name string, args ...string) Result {
	w.e.Actor = name
	return w.e.Execute(context.Background(), args)
}

func (w *world) snap(t *testing.T) store.Snapshot {
	t.Helper()
	s, err := w.f.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// want checks the exit and that the output holds every part; a refusal
// is one line.
func want(t *testing.T, r Result, exit int, parts ...string) {
	t.Helper()
	if r.Exit != exit {
		t.Fatalf("exit %d, want %d:\n%s", r.Exit, exit, r.Output)
	}
	for _, p := range parts {
		if !strings.Contains(r.Output, p) {
			t.Fatalf("output lacks %q:\n%s", p, r.Output)
		}
	}
	if exit != 0 && strings.Count(r.Output, "SPRINT REFUSED") != 1 {
		t.Fatalf("a failure is one refusal line:\n%s", r.Output)
	}
	if exit == 0 && r.Output != "" && !strings.Contains(r.Output, "\nnext: ") && !strings.HasPrefix(r.Output, "next: ") && !strings.HasPrefix(r.Output, "{") {
		t.Fatalf("a success ends with next:\n%s", r.Output)
	}
}

type fakeBus struct {
	present map[string]bool
	err     error
}

func (b *fakeBus) Who(context.Context) (map[string]bus.Presence, error) {
	out := map[string]bus.Presence{}
	for n, ok := range b.present {
		if ok {
			out[n] = bus.Presence{Name: n}
		}
	}
	return out, b.err
}

// fakeRoster is nova-config: alice and bob workers, carol and dave kin,
// every tier, width 1 (bob 2); a test changes ro or sets err.
type fakeRoster struct {
	ro  roster.Roster
	err error
}

func newRoster() *fakeRoster {
	all := []string{"flash", "pro", "heavy", "frontier"}
	return &fakeRoster{ro: roster.Roster{
		Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: all, Harness: roster.WorkerHarness}, "bob": {Name: "bob", Width: 2, Tiers: all, Harness: roster.WorkerHarness}},
		Kin:     map[string]roster.Kin{"carol": {Name: "carol", Model: "m-carol", Width: 1, Tiers: all, Harness: "claude"}, "dave": {Name: "dave", Model: "m-dave", Width: 1, Tiers: all, Harness: "codex"}},
	}}
}

func (r *fakeRoster) ReadRosterAt(context.Context, int) (roster.Roster, error) { return r.ro, r.err }

// fakeAnswers answers limits and timeouts, and prices() from prices.
type fakeAnswers struct{ prices map[string]policy.Price }

func (f fakeAnswers) AnswerContext(context.Context, policy.Versions, store.Snapshot, roster.Roster, policy.Questions) (policy.Answers, []policy.Failure) {
	return policy.Answers{Limits: policy.Limits{FleetWidthCap: 8}, Timeouts: policy.Timeouts{Consumer: map[string]int{"flash": 600, "pro": 1800, "heavy": 3600, "frontier": 7200}},
		Catalog: policy.Catalog{Models: map[string][]policy.Model{"inception": {{Name: "mercury-3", Tier: "pro"}}, "deepseek": {{Name: "deepseek-flash", Tier: "flash"}, {Name: "deepseek-flash", Tier: "pro"}}},
			Bench: map[string]string{"flash": "deepseek/deepseek-flash", "pro": "inception/mercury-3", "heavy": "deepseek/deepseek-flash", "frontier": "deepseek/deepseek-v4-pro"}}, Prices: f.prices}, nil
}

// fakeLander is the hand landing and the group branch's creation: Land
// returns merge (or landErr) and records each branch@head it landed.
type fakeLander struct {
	merge            string
	heads            map[string]string
	landErr, headErr error
	createErr        error
	landed, created  []string
}

func (l *fakeLander) Land(_ context.Context, _ store.Stream, branch, head string) (string, error) {
	if l.landErr != nil {
		return "", l.landErr
	}
	l.landed = append(l.landed, branch+"@"+head)
	return l.merge, nil
}

func (l *fakeLander) Heads(context.Context, store.Stream) (map[string]string, error) {
	return l.heads, l.headErr
}

func (l *fakeLander) CreateGroup(_ context.Context, s store.Stream, sha string) error {
	if l.createErr != nil {
		return l.createErr
	}
	l.created = append(l.created, s.Name+"@"+sha)
	return nil
}

type fakeOrigin struct {
	tips     map[string]string // "<clone address> branch" -> sha
	contains bool
	tree     []string     // every file at any base (nil: the root only)
	commits  []git.Commit // the head's commits since the base (nil: none)
	commitsE error
	err      error
}

func (o *fakeOrigin) Commits(context.Context, string, string, string, string) ([]git.Commit, error) {
	return o.commits, o.commitsE
}

func (o *fakeOrigin) Contains(context.Context, string, string, string) (bool, error) {
	return o.contains, o.err
}

func (o *fakeOrigin) Tree(context.Context, string, string) ([]string, error) {
	return o.tree, o.err
}

// Branches is the tips at the address under prefix.
func (o *fakeOrigin) Branches(_ context.Context, url, prefix string) (map[string]string, error) {
	out := map[string]string{}
	for k, sha := range o.tips {
		if name, ok := strings.CutPrefix(k, url+" "+prefix); ok {
			out[prefix+name] = sha
		}
	}
	return out, o.err
}

// fakeForge is the host's gh identity: act is CanAct's answer; prs maps a
// PR number to its head, merged lists the merged ones.
type fakeForge struct {
	act    error
	prs    map[int]string
	merged map[int]bool
	checks []string // the base's required checks
	checkE error
}

func (f *fakeForge) RequiredChecks(context.Context, string, string) ([]string, error) {
	return f.checks, f.checkE
}

func (f *fakeForge) CanAct(context.Context, string) error { return f.act }

func (f *fakeForge) PR(_ context.Context, _ string, n int) (string, bool, error) {
	h, ok := f.prs[n]
	if !ok {
		return "", false, errors.New("no pull requests found")
	}
	return h, f.merged[n], nil
}

func (o *fakeOrigin) Tip(_ context.Context, repo, branch string) (string, bool, error) {
	sha, ok := o.tips[repo+" "+branch]
	return sha, ok, o.err
}

type fakeDoctor struct {
	fleet, history error
	schemaErr      error
	tools          string
	schema         bool
	run            run.ChooserStatus
	runErr         error
	units          []WorkerUnit // the worker benches; nil: no bench
	unitsErr       error
}

func (d *fakeDoctor) Fleet(context.Context) error                  { return d.fleet }
func (d *fakeDoctor) ToolsVersion(context.Context) (string, error) { return d.tools, nil }
func (d *fakeDoctor) Schema(context.Context) (bool, error)         { return d.schema, d.schemaErr }
func (d *fakeDoctor) History(context.Context) error                { return d.history }
func (d *fakeDoctor) Run(context.Context) (run.ChooserStatus, error) {
	return d.run, d.runErr
}
func (d *fakeDoctor) Workers(context.Context) ([]WorkerUnit, error) { return d.units, d.unitsErr }
func (d *fakeDoctor) Worker(_ context.Context, machine string) (WorkerUnit, bool, error) {
	for _, u := range d.units {
		if u.Machine == machine {
			return u, true, d.unitsErr
		}
	}
	return WorkerUnit{}, false, d.unitsErr
}

type fakeServices struct {
	ran []string
	// worker, when set, is the worker's own start (Serve's Check on w).
	worker func(WorkerArgs) error
}

func (s *fakeServices) Run(context.Context, string) error { s.ran = append(s.ran, "run"); return nil }
func (s *fakeServices) Worker(_ context.Context, w WorkerArgs) error {
	s.ran = append(s.ran, "worker "+w.Row)
	if s.worker != nil {
		return s.worker(w)
	}
	return nil
}
func (s *fakeServices) Dash(context.Context, string, string) error {
	s.ran = append(s.ran, "dash")
	return nil
}

type fakeArchive struct {
	info    BackupInfo
	err     error
	loaded  string
	lockErr error
}

func (a *fakeArchive) Backup(context.Context, string) (BackupInfo, error) { return a.info, a.err }
func (a *fakeArchive) Verify(context.Context, string) (BackupInfo, error) { return a.info, a.err }
func (a *fakeArchive) AcquireRestore(context.Context) (run.RestoreLock, error) {
	if a.lockErr != nil {
		return nil, a.lockErr
	}
	return fakeLock{a}, nil
}

type fakeLock struct{ a *fakeArchive }

func (l fakeLock) Check(context.Context) error { return nil }
func (l fakeLock) Close(context.Context) error { return nil }
func (l fakeLock) Restore(_ context.Context, digest string, _ func(run.RestoreWriter) error) error {
	l.a.loaded = digest
	return nil
}

var errBroken = errors.New("connection refused")
