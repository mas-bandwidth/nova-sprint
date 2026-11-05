package worker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/cardcontract"
	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// Proc is a started child: its process group, ended by Kill.
type Proc interface {
	Wait() (code int) // blocks until the child exits; -1 when it was killed
	Kill()            // ends the child's whole process group now
}

// StartProc starts argv in dir with env, its output appended to log, as the leader of its
// own process group.
type StartProc func(argv, env []string, dir string, log *os.File) (Proc, error)

// Usage reads what the harness spent so far from its data home; ok is false when it
// cannot be read (no database yet, no sqlite3).
type Usage func(dataHome string) (u cardcost.Usage, ok bool)

// Launch starts each card as a lane: staged from the bench mirror, framed, run under
// the wall with its deadline and token budget, its commit pushed from outside the wall.
type Launch struct {
	Bench    string // the machine's name, as the stage line names it
	Root     string // where the row's launches live: <Root>/slots/<launch>
	Home     string // the bench's home: the mirrors (~/nova-bench/mirror) and the toolchain
	Harness  string // the opencode binary
	Wall     string // the nova-sandbox binary
	Env      []string
	Identity swarm.StagingIdentity
	Catalog  string // the machine's models.json, seeded into each launch; "" for none
	Pusher   *Pusher
	Usage    Usage
	Spawn    StartProc
	// SampleEvery is how often a running lane's spend is read against its budget.
	SampleEvery time.Duration

	mu   sync.Mutex
	kept []string // failed launches kept, oldest first
}

// KeepFailed is how many failed launches are kept for reading.
const KeepFailed = 5

// LaunchName is a launch's directory name: <card>.g<gen>.e<epoch> for work,
// <card>.a<attempt>.e<epoch> for a read.
func LaunchName(p member.Packet) string {
	if p.Kind == "read" {
		return fmt.Sprintf("%s.a%d.e%d", p.Card, p.Attempt, p.Epoch)
	}
	return fmt.Sprintf("%s.g%d.e%d", p.Card, p.Gen, p.Epoch)
}

// procLane is one launch.
type procLane struct {
	l                    *Launch
	p                    member.Packet
	name, slot, job, log string
	data, tmp            string
	deadline             time.Duration
	tokens               int64
	// what staging and the result left, read by the lane's own goroutine only
	shared        bool   // the checkout borrows the mirror's objects
	mirror, carry string // the mirror staged from; a rework's carry line
	resultPath    string // the RESULT.md read, "" for the shim's finish

	finished      chan struct{} // closed when the lane is done
	mu            sync.Mutex
	proc          Proc
	started       bool // the child is running: its deadline counts from the first Poll that sees it
	deadlineAt    time.Time
	sampled       time.Time
	ended         string // why the worker ended it: member.EndDeadline, member.EndBudget
	budget        string
	done          bool
	res           member.Result
	push          member.Push
	killRequested bool
}

// Start begins a lane; its staging and child run on a goroutine of their own.
func (l *Launch) Start(p member.Packet, now time.Time) (Lane, error) {
	if p.Model == "" || p.Deadline <= 0 || p.Tokens == "" {
		return nil, fmt.Errorf("the packet names no route (model %q, tokens %q, deadline %ds): nothing is inferred", p.Model, p.Tokens, p.Deadline)
	}
	tokens := int64(0)
	if p.Tokens != "unmetered" {
		n, err := strconv.ParseInt(p.Tokens, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("the packet's tokens %q are neither a count nor unmetered", p.Tokens)
		}
		tokens = n
	}
	name := LaunchName(p)
	slot := filepath.Join(l.Root, "slots", name)
	if err := os.RemoveAll(slot); err != nil {
		return nil, err
	}
	job := filepath.Join(slot, "jobs", p.Card)
	ln := &procLane{l: l, p: p, name: name, slot: slot, job: job, log: filepath.Join(slot, "harness-output.log"),
		data: filepath.Join(slot, "data"), tmp: filepath.Join(slot, "tmp"),
		deadline: time.Duration(p.Deadline) * time.Second, tokens: tokens, sampled: now, finished: make(chan struct{})}
	if p.Kind == "read" {
		ln.tmp = filepath.Join(job, "tmp")
	}
	for _, d := range []string{job, ln.data, ln.tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	go ln.run()
	return ln, nil
}

// Poll ends the lane at its deadline or its budget, and says whether it has ended.
func (ln *procLane) Poll(now time.Time) bool {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	if ln.done {
		return true
	}
	if !ln.started || ln.ended != "" {
		return false
	}
	if ln.deadlineAt.IsZero() {
		ln.deadlineAt = now.Add(ln.deadline)
	}
	if !now.Before(ln.deadlineAt) {
		ln.endLocked(member.EndDeadline, "")
		return false
	}
	if ln.tokens > 0 && ln.l.Usage != nil && now.Sub(ln.sampled) >= ln.l.SampleEvery {
		ln.sampled = now
		if u, ok := ln.l.Usage(ln.data); ok && u.Tokens.Total() >= ln.tokens {
			ln.endLocked(member.EndBudget, fmt.Sprintf("budget: tokens %d of %d", u.Tokens.Total(), ln.tokens))
		}
	}
	return false
}

func (ln *procLane) endLocked(why, budget string) {
	ln.ended, ln.budget = why, budget
	if ln.proc != nil {
		ln.proc.Kill()
	}
}

func (ln *procLane) Printed() time.Time {
	fi, err := os.Stat(ln.log)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// Stop ends the child now; a lane still staging starts none.
func (ln *procLane) Stop() {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	ln.killRequested = true
	if ln.proc != nil && !ln.done {
		ln.proc.Kill()
	}
}

func (ln *procLane) Result() (member.Result, member.Push) {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	return ln.res, ln.push
}

// Remove deletes the launch; a failed one is kept, the oldest past KeepFailed removed.
func (ln *procLane) Remove(keep bool) {
	l := ln.l
	l.mu.Lock()
	defer l.mu.Unlock()
	if !keep {
		_ = os.RemoveAll(ln.slot)
		return
	}
	l.kept = append(l.kept, ln.slot)
	for len(l.kept) > KeepFailed {
		_ = os.RemoveAll(l.kept[0])
		l.kept = l.kept[1:]
	}
}

// Sweep removes what a stopped worker left under root's slots, but for the newest
// KeepFailed; it runs once, before any lane starts.
func (l *Launch) Sweep() (removed int) {
	dir := filepath.Join(l.Root, "slots")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	type ent struct {
		path string
		at   time.Time
	}
	var es []ent
	for _, e := range ents {
		if fi, err := e.Info(); err == nil {
			es = append(es, ent{filepath.Join(dir, e.Name()), fi.ModTime()})
		}
	}
	slices.SortFunc(es, func(a, b ent) int { return a.at.Compare(b.at) })
	for i, e := range es {
		if i < len(es)-KeepFailed && os.RemoveAll(e.path) == nil {
			removed++
		}
	}
	return removed
}

// run stages the card, starts its child and waits for it, then reads its result and
// pushes its commit. The lane is done when all of that is.
func (ln *procLane) run() {
	res, push := ln.launch()
	defer close(ln.finished)
	ln.mu.Lock()
	defer ln.mu.Unlock()
	switch {
	case res.End != "":
	case ln.ended != "":
		res.End, res.Budget = ln.ended, ln.budget
	}
	ln.res, ln.push, ln.done = res, push, true
}

func (ln *procLane) launch() (member.Result, member.Push) {
	p := ln.p
	staged, err := ln.stage()
	if err != nil {
		return member.Result{End: member.EndStaging, Staging: oneLine(err.Error()), Report: "no child ran"}, member.Push{}
	}
	argv, env, err := ln.command()
	if err != nil {
		return member.Result{End: member.EndStaging, Staging: oneLine(err.Error()), Report: "no child ran"}, member.Push{}
	}
	log, err := os.OpenFile(ln.log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return member.Result{End: member.EndStaging, Staging: err.Error(), Report: "no child ran"}, member.Push{}
	}
	defer log.Close()
	ln.mu.Lock()
	if ln.killRequested {
		ln.mu.Unlock()
		return member.Result{End: member.EndStaging, Staging: "stopped before its child started", Report: "no child ran"}, member.Push{}
	}
	proc, err := ln.l.Spawn(argv, env, ln.job, log)
	if err != nil {
		ln.mu.Unlock()
		return member.Result{End: member.EndStaging, Staging: "the child did not start: " + err.Error(), Report: "no child ran"}, member.Push{}
	}
	ln.proc, ln.started = proc, true
	ln.mu.Unlock()
	began := time.Now() // the wall the result reports; the deadline is the pass's clock
	code := proc.Wait()
	r := ln.result(code, time.Since(began))
	if p.Kind == "read" {
		return r, member.Push{}
	}
	return r, ln.pushOf(r, staged)
}

// stage stages the card's checkout and frames the job; it returns the staged commit.
func (ln *procLane) stage() (string, error) {
	p := ln.p
	cb := swarm.ReadCardBase([]byte(p.Brief))
	f := frameOf(p, cb)
	opts := swarm.StageOptions{Card: []byte(p.Brief), TargetDir: filepath.Join(ln.job, "repo"), JobDir: ln.job,
		BenchHome: ln.l.Home, BenchName: ln.l.Bench, Timeout: swarm.DefaultStageTimeout}
	if f.Repo != "" {
		url := swarm.CardRepoURL(f.Repo)
		if url == "" {
			url = f.Repo
		}
		opts.Base = &swarm.CardBase{Repo: url, Sha: f.StageSha, Ref: f.BaseRef, Named: f.Repo}
		opts.Branch = f.Branch
		if f.Kind == "work" && f.Attempt > 1 {
			opts.Rework = &swarm.Rework{Prev: f.PrevHead, From: f.PrevFrom}
		}
	}
	res, err := swarm.StageCard(opts)
	if err != nil {
		return "", fmt.Errorf("staging refused: %w", err)
	}
	if !res.Staged && swarm.CardNamesRepo([]byte(p.Brief)) {
		return "", errors.New("staging refused: the card names a repository and nothing was staged")
	}
	ln.shared = res.Shared
	ln.mirror = res.Mirror
	if res.Carry != nil {
		ln.carry = res.Carry.Line()
	}
	st := cardcontract.Staged{Job: ln.job, Repo: opts.TargetDir, Head: res.BaseSha, Carry: res.Carry}
	if st.Git, err = exec.LookPath("git"); err != nil {
		return "", err
	}
	if st.Git, err = filepath.Abs(st.Git); err != nil {
		return "", err
	}
	if res.Staged && p.Kind == "read" {
		if st.Start, err = readStart(st.Repo, f.ReviewBase); err != nil {
			return "", err
		}
		if st.Start != "" {
			changed, err := gitOut(st.Repo, "diff", "--name-only", "--no-renames", "-z", "--end-of-options", st.Start, "HEAD")
			if err != nil {
				return "", fmt.Errorf("the read's gate: %w", err)
			}
			if st.Gate, err = cardcontract.ReadGate(st.Repo, strings.FieldsFunc(changed, func(r rune) bool { return r == 0 })); err != nil {
				return "", err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(ln.slot, cardcontract.StagedName), []byte(res.BaseSha+"\n"), 0o644); err != nil {
		return "", err
	}
	if len(f.Stage) > 0 {
		f.Recipes = filepath.Join(ln.l.Root, cardcontract.RecipesName)
	}
	if err := cardcontract.StageRecipes(f, ln.job); err != nil {
		return "", err
	}
	if err := cardcontract.Install(cardcontract.For(cardcontract.FamilyOf(p.Model)), f, st, ln.shims()); err != nil {
		return "", err
	}
	return res.BaseSha, writeShellShims(ln.shims())
}

func (ln *procLane) shims() string { return filepath.Join(ln.slot, "shim") }

// frameOf is the launch's frame: what the packet says to stage and how the card ends.
func frameOf(p member.Packet, cb swarm.CardBase) cardcontract.Frame {
	f := cardcontract.Frame{Kind: p.Kind, Card: p.Card, Attempt: p.Attempt, Model: p.Model, Tier: p.Tier,
		Repo: cb.Repo, BaseRef: cb.Ref, StageSha: cb.Sha, Branch: p.Branch, Why: p.Why, Finding: p.Finding, Fix: p.Fix, Stage: cb.Stage}
	if f.Tier == "" {
		first, _, _ := strings.Cut(p.Brief, "\n")
		mh, _ := cardhdr.ReadModel(first)
		f.Tier = mh.Tier
	}
	if p.Kind == "read" {
		f.Branch, f.ReviewBase = p.WorkBranch, cb.Ref
		if p.WorkBase != "" {
			f.ReviewBase = p.WorkBase
		}
		if typedrec.IsFullSha(p.Head) {
			f.StageSha = p.Head
		}
		return f
	}
	if p.BaseHead != "" {
		f.StageSha, f.PrevHead, f.PrevFrom = p.BaseHead, p.BaseHead, p.BaseFrom
	}
	return f
}

// readStart is the commit a read's work started from: the merge base of the work's head
// and its review base, fetched from origin first, so the read sees only the work however
// far the base has moved since; "" when they share no history.
func readStart(repo, base string) (string, error) {
	if base == "" {
		return "", nil
	}
	ref := base
	if !typedrec.IsFullSha(base) {
		ref = "refs/remotes/origin/" + base
		if _, err := gitOut(repo, "fetch", "-q", "--no-tags", "--", "origin", "+refs/heads/"+base+":"+ref); err != nil {
			if _, terr := gitOut(repo, "rev-parse", "-q", "--verify", "--end-of-options", "refs/tags/"+base+"^{commit}"); terr != nil {
				return "", fmt.Errorf("staging refused: the read's start: the base %s could not be fetched from origin: %w", base, err)
			}
			ref = "refs/tags/" + base
		}
	}
	sha, err := gitOut(repo, "merge-base", "--end-of-options", "HEAD", ref)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("staging refused: the read's start: %w", err)
	}
	return sha, nil
}

// command is the wall's argv around the harness's, and the child's environment.
func (ln *procLane) command() (argv, env []string, err error) {
	p := ln.p
	card := member.CardText(p)
	if p.Rules != "" {
		rules, err := swarm.HeldRules(p.Rules)
		if err != nil {
			return nil, nil, err
		}
		q := p
		q.Brief = swarm.StagedBrief(p.Brief, rules)
		card = member.CardText(q)
	}
	if err := os.WriteFile(filepath.Join(ln.slot, "card.md"), []byte(card), 0o644); err != nil {
		return nil, nil, err
	}
	harness := []string{ln.l.Harness, "run", "--model", p.Model, "--title", p.Card, "--", cardcontract.Prompt(ln.job, card)}
	argv = []string{ln.l.Wall, "--read", ln.slot, "--write", ln.job, "--write", ln.data, "--write", ln.tmp,
		"--tmp", ln.tmp, "--cwd", ln.job, "--read", filepath.Dir(ln.l.Harness)}
	for _, r := range ln.l.readRoots() {
		argv = append(argv, r...)
	}
	if ln.shared && ln.mirror != "" {
		argv = append(argv, "--read-noexec", filepath.Join(ln.mirror, "objects"))
	}
	argv = append(append(argv, "--"), harness...)
	env, err = ln.env()
	return argv, env, err
}

// readRoots are the toolchain directories the wall lets a card read: the bench's sdk and
// Homebrew to run, the Go module cache to read only. A root the machine lacks is left out.
func (l *Launch) readRoots() [][]string {
	var out [][]string
	for _, r := range []struct {
		flag, dir string
	}{{"--read", filepath.Join(l.Home, "sdk")}, {"--read", "/opt/homebrew"}, {"--read-noexec", filepath.Join(l.Home, "go", "pkg", "mod")}} {
		if fi, err := os.Stat(r.dir); err == nil && fi.IsDir() {
			out = append(out, []string{r.flag, r.dir})
		}
	}
	return out
}

// keptEnv are the names a child keeps from the worker's environment; every other name,
// and every one that looks like a secret, is left out.
var keptEnv = []string{"PATH", "LANG", "TERM", "USER", "LOGNAME"}

var secretWords = []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "KEY", "CREDENTIAL", "AUTH"}

// ProviderKey is the one secret a route's child is handed: <PROVIDER>_API_KEY of its
// model's provider.
func ProviderKey(p member.Packet) string {
	provider, _, _ := strings.Cut(p.Model, "/")
	return strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_API_KEY"
}

func (ln *procLane) env() ([]string, error) {
	key := ProviderKey(ln.p)
	var env []string
	path := ""
	for _, kv := range ln.l.Env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == key:
			env = append(env, kv)
		case k == "PATH":
			path = v
		case slices.Contains(keptEnv, k) || strings.HasPrefix(k, "LC_"):
			if !slices.ContainsFunc(secretWords, func(w string) bool { return strings.Contains(strings.ToUpper(k), w) }) {
				env = append(env, kv)
			}
		}
	}
	shim := ln.shims()
	env = append(env, "PATH="+shim+string(os.PathListSeparator)+path, "SHELL="+filepath.Join(shim, "sh"),
		"HOME="+ln.data, "XDG_DATA_HOME="+ln.data, "XDG_CONFIG_HOME="+filepath.Join(ln.data, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(ln.data, ".cache"), "TMPDIR="+ln.tmp, "TMP="+ln.tmp, "TEMP="+ln.tmp,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "NOVA_SWARM_JOB="+ln.job,
		"GOFLAGS=-p=4", "MAKEFLAGS=-j4", "CMAKE_BUILD_PARALLEL_LEVEL=4")
	env = append(env, swarm.StagingGitEnv(ln.l.Identity)...)
	if ln.l.Catalog != "" {
		dst := filepath.Join(ln.data, "models.json")
		if b, err := os.ReadFile(ln.l.Catalog); err == nil && os.WriteFile(dst, b, 0o644) == nil {
			env = append(env, "OPENCODE_MODELS_PATH="+dst, "OPENCODE_DISABLE_MODELS_FETCH=1")
		}
	}
	return env, nil
}

// shellShim unsets every environment name that carries KEY, TOKEN or SECRET before it
// runs the real shell: the harness keeps its provider key, the shell it hands the model
// does not. No value is read: awk prints names. With no awk it refuses to start.
const shellShim = `#!/bin/sh
command -v awk >/dev/null 2>&1 || { echo 'worker shell shim: no awk to list the environment by name; refusing a shell that would carry the provider key' >&2; exit 127; }
for n in $(env | awk -F= '/^[A-Za-z_][A-Za-z0-9_]*=/{u=toupper($1);if(index(u,"KEY")||index(u,"TOKEN")||index(u,"SECRET"))print $1}'); do unset "$n"; done
unset n
exec '%s' "$@"
`

func writeShellShims(dir string) error {
	for _, name := range []string{"sh", "bash"} {
		real, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if real, err = filepath.EvalSymlinks(real); err != nil || strings.Contains(real, "'") {
			return fmt.Errorf("the %s shim: no plain path to the real shell", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf(shellShim, real)), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// result reads the child's outcome: the gh shim's finish first, else RESULT.md at the
// job's root or in its checkout; the head the git shim recorded when the result names none.
func (ln *procLane) result(code int, wall time.Duration) member.Result {
	r := member.Result{Wall: wall, Carry: ln.carry}
	if code > 0 {
		r.Exit = code
	}
	cr, ok := cardcontract.ReadFinish(ln.job)
	if !ok {
		for _, path := range []string{filepath.Join(ln.job, "RESULT.md"), filepath.Join(ln.job, "repo", "RESULT.md")} {
			if b, err := os.ReadFile(path); err == nil {
				cr, ok, ln.resultPath = typedrec.ParseCardResult(b), true, path
				break
			}
		}
	}
	if ok {
		r.Shaped, r.Head, r.Verdict, r.Report, r.Title, r.Body = cr.Shaped, cr.Head, cr.Verdict, cr.Report, cr.Title, cr.Body
	}
	if !typedrec.IsSha(r.Head) {
		if _, head := cardcontract.LastPushed(ln.job); head != "" {
			r.Head = head
		}
	}
	r.Ran = code == 0 && ok
	r.OK = r.Ran
	switch {
	case r.Report != "":
	case ok && cr.Fault != "":
		r.Report = "RESULT.md shape fault: " + cr.Fault
	case r.Ran:
		r.Report = "finished; the child published no one-line report"
	default:
		r.Report = fmt.Sprintf("the child ended without a result (exit %d)", code)
	}
	if b, err := os.ReadFile(ln.log); err == nil {
		r.HarnessOutput = string(b[max(len(b)-64<<10, 0):])
	}
	if ln.l.Usage != nil {
		if u, ok := ln.l.Usage(ln.data); ok {
			u.Wall = fmt.Sprintf("%.2fs", wall.Seconds())
			if ln.tokens > 0 {
				u.Budget = fmt.Sprintf("%d/%d", u.Tokens.Total(), ln.tokens)
			}
			u.Billing = ln.p.Billing
			r.Usage = u.String()
		}
	}
	return r
}

// pushOf pushes the result's commit; for a result that names none, says why not.
func (ln *procLane) pushOf(r member.Result, staged string) member.Push {
	switch {
	case !typedrec.IsSha(r.Head):
		return member.Push{None: "the child's result names no commit (no head: line, no push recorded)"}
	case ln.p.Branch == "":
		return member.Push{None: "the packet names no branch to push to"}
	case ln.l.Pusher == nil:
		return member.Push{None: "this worker has no pusher"}
	case !typedrec.IsFullSha(staged):
		return member.Push{None: "no staged commit is recorded: the push cannot be checked against it"}
	}
	url := swarm.CardRepoURL(frameOf(ln.p, swarm.ReadCardBase([]byte(ln.p.Brief))).Repo)
	if url == "" {
		return member.Push{None: "the card names no repository to push to"}
	}
	pu := ln.l.Pusher.Push(PushJob{Launch: ln.name, Checkout: filepath.Join(ln.job, "repo"), URL: url,
		Branch: ln.p.Branch, Head: r.Head, Base: staged, Result: ln.resultPath})
	if pu.Sha == "" && pu.None == "" && pu.Refused == "" {
		pu.Refused = "the pusher said nothing"
	}
	return pu
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, oneline.Escape(strings.TrimSpace(string(exit.Stderr))))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
