package sprint_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/hostload"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The work lint on the twin store and a twin repository (docs/SPEC-SPRINT.md section 6,
// the work lint): one attempt per token, each pushed to a bare origin, finished, and held
// to the lint by the tick before any reader is asked of it.

// lintRepo is a bare origin with main, a developer's clone, and the clone the lint reads.
type lintRepo struct {
	t                 *testing.T
	origin, dev, lint string
	env               []string
}

var lintBaseFiles = map[string]string{
	"internal/sprint/a.go":                         "package sprint\n\n// A is one.\nfunc A() int { return 1 }\n",
	"internal/sprint/a_test.go":                    "package sprint\n\nimport \"testing\"\n\nfunc TestNamed(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"A\")\n\t}\n}\n",
	"internal/ci/testdata/dead_code_allowlist.txt": "internal/old.go:Old\n",
	"cmd/nova-sprint/verbs.go":                     "package main\n\nvar verbs = []verb{\n\t{\"look\", \"<id>\", \"look x\", nil},\n}\n",
	"cmd/nova-sprint/coordinator.go":               "package main\n\nvar verbClasses = map[string]string{\"look\": classRead, \"zap\": classCoordinator}\n",
	"conflict.txt":                                 "base\n",
}

func newLintRepo(t *testing.T) *lintRepo {
	t.Helper()
	root := t.TempDir()
	r := &lintRepo{t: t, origin: filepath.Join(root, "origin.git"), dev: filepath.Join(root, "dev"), lint: filepath.Join(root, "lint"),
		env: append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=lint-test", "GIT_AUTHOR_EMAIL=lint-test@example.com",
			"GIT_COMMITTER_NAME=lint-test", "GIT_COMMITTER_EMAIL=lint-test@example.com")}
	r.git(root, "init", "-q", "--bare", "-b", "main", r.origin)
	r.git(r.origin, "config", "uploadpack.allowAnySHA1InWant", "true")
	r.git(root, "clone", "-q", r.origin, r.dev)
	for p, body := range lintBaseFiles {
		r.write(p, body)
	}
	r.git(r.dev, "add", "-A")
	r.git(r.dev, "commit", "-q", "-m", "the base")
	r.git(r.dev, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(root, "clone", "-q", "--single-branch", r.origin, r.lint)
	return r
}

func (r *lintRepo) git(dir string, args ...string) string {
	r.t.Helper()
	cmd := exec.CommandContext(r.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func (r *lintRepo) write(p, body string) {
	r.t.Helper()
	full := filepath.Join(r.dev, p)
	require.NoError(r.t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(r.t, os.WriteFile(full, []byte(body), 0o644))
}

// branch is one attempt's commit on its own branch from main: the files written, the
// commit made with msg (empty when files is nil and empty is set), pushed; its head.
func (r *lintRepo) branch(name string, files map[string]string, msg string) string {
	r.t.Helper()
	r.git(r.dev, "fetch", "-q", "origin")
	r.git(r.dev, "checkout", "-q", "-B", name, "main")
	for p, body := range files {
		r.write(p, body)
	}
	r.git(r.dev, "add", "-A")
	r.git(r.dev, "commit", "-q", "--allow-empty", "-m", msg)
	r.git(r.dev, "push", "-q", "origin", "HEAD:refs/heads/"+name)
	return r.git(r.dev, "rev-parse", "HEAD")
}

// lintRig is the twin store with member m1 and three readers, linting in the twin
// repository's lint clone.
type lintRig struct {
	t    *testing.T
	ctx  context.Context
	st   *store.Store
	repo *lintRepo
	mu   sync.Mutex
	now  time.Time
}

func newLintRig(t *testing.T) *lintRig {
	t.Helper()
	r := &lintRig{t: t, ctx: t.Context(), repo: newLintRepo(t), now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	m := store.NewMem()
	n := 0
	lint := sprint.NewWorkLinter(sprint.WorkLintGit{Clone: func(string) (string, error) { return r.repo.lint, nil }, Env: r.repo.env,
		Now: func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }})
	r.st = &store.Store{B: m, Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator",
		Now:     func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID:   func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep:   func(time.Duration) {},
		Updates: sprint.WorkLintTables(lint)}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, m.RowsAdd(r.ctx, "t-readers", []string{"reader-a", "reader-b", "reader-c"}))
	require.NoError(t, m.SetCoordinator(r.ctx, "coordinator"))
	r.beat()
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 32}))
	return r
}

func (r *lintRig) must(step store.Step) store.Result {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, step)
	require.NoError(r.t, err, step.Verb)
	require.Empty(r.t, res.Refused, "%s refused", step.Verb)
	return res
}

func (r *lintRig) tick() {
	r.t.Helper()
	r.mu.Lock()
	r.now = r.now.Add(time.Second)
	r.mu.Unlock()
	r.beat()
	_, err := r.st.Tick(r.ctx)
	require.NoError(r.t, err)
}

// beat is one beat of the member and the readers: each of them is there.
func (r *lintRig) beat() {
	r.t.Helper()
	require.NoError(r.t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	_, err := r.st.Beat(r.ctx, "m1", &zero, hostload.Source{})
	require.NoError(r.t, err)
}

func (r *lintRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, store.All, nil)
	require.NoError(r.t, err)
	return s
}

// lintBrief is a card's brief on the twin repository.
func lintBrief(id, origin string) string {
	return "c: " + id + " tier: flash\nbase-repo: " + origin + "\nBASE: main\nPATHS: internal/sprint/*.go,cmd/nova-sprint/*.go\nTEST: ./internal/sprint TestNamed\n\nThe task."
}

// lintUsage is the usage every attempt's finish reports: a run on claude sonnet.
const lintUsage = "model=anthropic/claude-sonnet-5-5 input=100 output=10"

func TestAnAttemptThatFailsWorkLintIsReworkedWithoutARead(t *testing.T) {
	t.Parallel()
	r := newLintRig(t)
	repo := r.repo
	clean := map[string]string{"internal/sprint/a.go": "package sprint\n\n// A is two.\nfunc A() int { return 2 }\n"}
	type attempt struct {
		id, token string
		files     map[string]string
		msg       string
		noHead    bool
	}
	attempts := []attempt{
		{id: "clean", files: clean, msg: "a clean change\n\nBy: rowan-personal\nCo-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"},
		{id: "nohead", token: sprint.LintNoHead, noHead: true},
		{id: "empty", token: sprint.LintEmpty, msg: "nothing"},
		{id: "paths", token: sprint.LintPaths, files: map[string]string{"other/x.go": "package other\n"}, msg: "outside"},
		{id: "ledger", token: sprint.LintLedger, files: map[string]string{"internal/ci/testdata/dead_code_allowlist.txt": "internal/old.go:Old\ninternal/new.go:New\n"}, msg: "a row"},
		{id: "test", token: sprint.LintTestAbsent, files: map[string]string{"internal/sprint/a_test.go": strings.Replace(lintBaseFiles["internal/sprint/a_test.go"], "TestNamed", "TestRenamed", 1)}, msg: "renamed"},
		{id: "gofmt", token: sprint.LintGofmt, files: map[string]string{"internal/sprint/a.go": "package sprint\n\n// A is two.\nfunc A() int {return 2}\n"}, msg: "unformatted"},
		{id: "trailer", token: sprint.LintTrailer, files: clean, msg: "change\n\nCo-Authored-By: GPT-5 <noreply@openai.com>"},
		{id: "merge", token: sprint.LintMerge, files: map[string]string{"conflict.txt": "branch\n", "internal/sprint/a.go": clean["internal/sprint/a.go"]}, msg: "conflicting"},
		{id: "verb", token: sprint.LintVerbDryRun, files: map[string]string{"cmd/nova-sprint/verbs.go": "package main\n\nvar verbs = []verb{\n\t{\"look\", \"<id>\", \"look x\", nil},\n\t{\"zap\", \"<id>\", \"zap x\", nil},\n}\n"}, msg: "a verb"},
		{id: "clock", token: sprint.LintTestClock, files: map[string]string{"internal/sprint/b_test.go": "package sprint\n\nimport (\n\t\"testing\"\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc TestB(t *testing.T) {\n\ttime.Sleep(time.Millisecond)\n\tsynctest.Test(t, func(t *testing.T) {\n\t\ttime.Sleep(time.Second)\n\t})\n}\n"}, msg: "a sleep"},
	}
	var cards []sprint.CardAdd
	for _, a := range attempts {
		cards = append(cards, sprint.CardAdd{ID: a.id, Brief: lintBrief(a.id, repo.origin)})
	}
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Cards: cards}))
	heads := map[string]string{}
	for _, a := range attempts {
		if !a.noHead {
			heads[a.id] = repo.branch("work/"+a.id, a.files, a.msg)
		}
	}
	// the base moves under the merge attempt: the same file, another line
	repo.git(repo.dev, "checkout", "-q", "-B", "main", "main")
	repo.write("conflict.txt", "main moved\n")
	repo.git(repo.dev, "commit", "-q", "-am", "the base moves")
	repo.git(repo.dev, "push", "-q", "origin", "HEAD:refs/heads/main")

	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.tick()
	s := r.snap()
	for _, a := range attempts {
		wc := s.Fleet.Card(sprint.WorkCardID(a.id, 1))
		require.NotNil(t, wc, a.id)
		if wc.Col != sprint.Working {
			r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Who: "m1"}))
		}
		wc = r.snap().Fleet.Card(wc.ID)
		r.must(store.FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")},
			Head: heads[a.id], Report: "done", Usage: lintUsage, Who: "m1"}))
	}
	r.tick()
	r.tick()
	s = r.snap()
	for _, a := range attempts {
		pr := s.Work.Card(a.id)
		require.NotNil(t, pr, a.id)
		if a.token == "" {
			assert.Equal(t, 1, pr.Int("attempt"), "%s: the clean attempt stays at attempt 1", a.id)
			assert.Equal(t, sprint.Review, pr.Col, a.id)
			assert.NotEmpty(t, s.Readers.Of(a.id), "%s: the clean attempt is asked as today", a.id)
			assert.Empty(t, pr.F(sprint.FieldLintReworks), a.id)
			continue
		}
		assert.Equal(t, 2, pr.Int("attempt"), "%s: reworked at once", a.id)
		assert.Contains(t, pr.F("fix"), a.token+":", "%s: the finding is its fix", a.id)
		assert.Contains(t, pr.F("fix"), "remedy: ", a.id)
		assert.Equal(t, "1", pr.F(sprint.FieldLintReworks), a.id)
		assert.Equal(t, "1", pr.F("broken_reads"), "%s: a lint rework counts toward the bound as a broken read", a.id)
		for _, rc := range s.Readers.Of(a.id) {
			assert.NotEqual(t, "1", rc.F("attempt"), "%s: no reader is asked of the attempt the lint refused", a.id)
		}
		if wc := s.Fleet.Card(sprint.WorkCardID(a.id, 2)); assert.NotNil(t, wc, a.id) {
			assert.Contains(t, wc.F("finding"), a.token, "%s: the next attempt is told the finding", a.id)
		}
	}
	// each finding names the file and line where it has one
	gofmt := s.Work.Card("gofmt").F("fix")
	assert.Contains(t, gofmt, "internal/sprint/a.go:4: gofmt:")
	clock := s.Work.Card("clock").F("fix")
	assert.Contains(t, clock, "internal/sprint/b_test.go:10: test-clock: time.Sleep")
	assert.Equal(t, 1, strings.Count(clock, "test-clock:"), "the sleep in the synctest bubble is not refused")
	assert.Contains(t, s.Work.Card("merge").F("fix"), "conflict.txt")
	assert.Contains(t, s.Work.Card("verb").F("fix"), "cmd/nova-sprint/verbs.go:5: verb-dry-run:")
	assert.Contains(t, s.Work.Card("paths").F("fix"), "other/x.go: outside-paths:")
}

// The same lint finding on two attempts of one brief is the brief's bound: the second is
// not reworked again but judged, and still asked of no reader.
func TestTheSameWorkLintFindingTwiceIsTheBriefsBound(t *testing.T) {
	t.Parallel()
	r := newLintRig(t)
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "gofmt", Brief: lintBrief("gofmt", r.repo.origin)}}}))
	head := r.repo.branch("work/gofmt", map[string]string{"internal/sprint/a.go": "package sprint\n\nfunc A() int {return 2}\n"}, "unformatted")
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	for attempt := 1; attempt <= 2; attempt++ {
		r.tick()
		wc := r.snap().Fleet.Card(sprint.WorkCardID("gofmt", attempt))
		require.NotNil(t, wc, "attempt %d", attempt)
		if wc.Col != sprint.Working {
			r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Who: "m1"}))
			wc = r.snap().Fleet.Card(wc.ID)
		}
		r.must(store.FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")},
			Head: head, Report: "done", Usage: lintUsage, Who: "m1"}))
		r.tick()
	}
	r.tick()
	s := r.snap()
	pr := s.Work.Card("gofmt")
	assert.Equal(t, 2, pr.Int("attempt"), "not reworked a third time")
	assert.Equal(t, sprint.Review, pr.Col)
	assert.Empty(t, s.Readers.Of("gofmt"), "asked of no reader")
	var judged bool
	for _, o := range s.Open {
		judged = judged || o.Note.Type == sprint.NBriefWrong && strings.Contains(o.Note.What, "gofmt")
	}
	assert.True(t, judged, "the brief's bound is the coordinator's judgment: %v", s.Open)
}
