package policy

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Fuzz probes (2026-11-06), from /Volumes/nova/ai/rowan/working/handoff/
// policy-fuzz_break_test.go, copied here; Rowan changed two in round 3, each
// marked "Round 3": the ruling that a refusal before storage names the file,
// and the 2,000-file survey runs only with NOVA_POLICY_FUZZ=1 (it reports
// counts and asserts nothing). Three helpers were split out of mutate and
// outOfDomain, unchanged, to keep every function under 60 lines.
//
// The random fuzz of the Starlark policy engine: default.star with random
// mutations, each file through Set (the verb's validation on a small seed)
// and then Answer on the suite's largest snapshot (TestFunctionalLoad's
// 5,000 producers over 50 rows), with a 5-second wall clock per file.

const (
	fuzzFiles    = 2000
	fuzzSeed     = 20261106
	fuzzWorkers  = 8
	fuzzWallTime = 5 * time.Second
)

// fuzzSeedWrites is the small seed of helpers_test.go's world.seed.
func fuzzSeedWrites() []store.Write {
	writes := []store.Write{
		store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1},
		store.PutStream{Name: "s2", Repo: "git@example.com:r.git", Base: "main", Pos: 2},
		store.PutRow{Name: "r1", Kind: "worker", Width: 2, Tiers: []string{"flash", "pro", "heavy"}, Model: "m-heavy"},
		store.PutRow{Name: "r2", Kind: "worker", Width: 1, Tiers: []string{"flash"}, Model: "m-flash"},
		store.PutRow{Name: "r3", Kind: "kin", Width: 1, Tiers: []string{"frontier"}, Model: "m-frontier"},
		store.PutRow{Name: "r4", Kind: "worker", Width: 1, Tiers: []string{"pro"}, Model: "m-pro"},
	}
	for _, p := range [][2]string{{"p1", "pro"}, {"p2", "heavy"}, {"p3", "flash"}, {"p4", "pro"}, {"p5", "pro"}} {
		stream := map[bool]string{true: "s2", false: "s1"}[p[0] == "p3" || p[0] == "p4"]
		writes = append(writes, store.PutCard{ID: p[0], Kind: "producer", Stream: stream, Tier: p[1], Score: 10, Brief: "do " + p[0], Prose: "do " + p[0]})
	}
	return writes
}

// fuzzSeedStore is a fresh Fake with the seed applied and default.star active as v1.
func fuzzSeedStore() (store.Store, error) {
	ctx := context.Background()
	st := store.NewFake()
	steps := [][]store.Write{
		fuzzSeedWrites(),
		{store.SetRowState{Row: "r1", State: "up"}, store.SetRowState{Row: "r2", State: "up"}, store.SetRowState{Row: "r3", State: "up"},
			store.SetRowState{Row: "r4", State: "up"}, store.SetRowHeld{Row: "r4", Held: true, Reason: "test"},
			store.SetCardState{Card: "p1", State: "ready"}, store.SetCardState{Card: "p2", State: "ready"},
			store.SetCardBounces{Card: "p2", Bounces: 2}, store.SetCardHead{Card: "p2", Head: "aaaa"},
			store.SetCardState{Card: "p3", State: "review"}, store.SetCardReview{Card: "p3"}, store.SetCardAuthor{Card: "p3", Author: "r2"},
			store.SetCardState{Card: "p5", State: "review"}, store.SetCardAuthor{Card: "p5", Author: "r1"}},
		{store.PutCard{ID: "p3-r1", Kind: "read", Of: "p3", Stream: "s2", Tier: "flash", Holder: "r1", Review: 1}},
		{store.SetCardState{Card: "p3-r1", State: "done"}, store.SetCardResult{Card: "p3-r1", Result: "ok"}},
	}
	for i, writes := range steps {
		snap, err := st.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		r, err := st.Apply(ctx, store.Op{ID: fmt.Sprintf("seed-%d", i), Epoch: snap.Epoch, Actor: "test", Kind: "test", At: t0.Add(time.Duration(i+1) * time.Second), Writes: writes})
		if err != nil {
			return nil, err
		}
		if !r.Accepted {
			return nil, errors.New("seed refused: " + r.Reason)
		}
	}
	if _, err := Up(ctx, st, Caller{Actor: "rowan", Coordinator: true, OpID: "fuzz-up", At: t0.Add(time.Hour)}); err != nil {
		return nil, err
	}
	return st, nil
}

// fuzzBigSnapshot is TestFunctionalLoad's snapshot: the largest the suite builds.
func fuzzBigSnapshot() store.Snapshot {
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{}}
	for i := range 10 {
		snap.Streams[fmt.Sprintf("s%d", i)] = store.Stream{Name: fmt.Sprintf("s%d", i)}
	}
	for i := range 50 {
		name := fmt.Sprintf("r%02d", i)
		snap.Rows[name] = store.Row{Name: name, Kind: "worker", Width: 2, State: "up", Tiers: store.Tiers[i%4:]}
	}
	for i := range 5000 {
		id := fmt.Sprintf("p%04d", i)
		snap.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%d", i%10), State: "ready", Tier: store.Tiers[i%4], Score: i, Priority: "normal"}
	}
	return snap
}

// ---- mutations ----

var (
	fuzzNumRE  = regexp.MustCompile(`\b\d+(\.\d+)?\b`)
	fuzzListRE = regexp.MustCompile(`\[[^\[\]\n]*\]`)
	fuzzLineRE = regexp.MustCompile(`line \d+`)
)

// fuzzNums are the numeric literal replacements: huge, negative, zero, float, string.
var fuzzNums = []string{"(1 << 200)", "99999999999999999999999999999999", "-3", "0", "0.5", `"7"`}

// fuzzRet is a pool of wrong-typed or out-of-range return values.
var fuzzRet = []string{"None", "[]", "{}", "()", "(1, 2, 3, 4, 5)", `"x"`, "1.5", "-1", "99999999999999999999999999", "True",
	`("a", 1)`, `["s1"]`, `"yes"`, "2", `{"work": 1, "ahead": 1}`, `[{"x": 1}]`}

// fuzzDicts are the dict-returning questions' full keys and values.
var fuzzDicts = map[string][][2]string{
	"limits":   {{"bounces", "6"}, {"faults", "3"}, {"same_finding", "2"}, {"reports_per_tick", "1000"}, {"route_rest_window", "10"}, {"route_rest_after", "3"}},
	"timeouts": {{"row_silent_gone", "600"}, {"row_silent_withdraw", "1800"}, {"landing_stall", "60"}, {"base_regate", "[120, 300]"}, {"judgement", "3600"}, {"route_rest_for", "1800"}},
	"landing":  {{"backed_up", "4"}, {"flowing", "1"}},
	"width":    {{"work", "row.width"}, {"ahead", "row.width"}},
}

type fdef struct {
	name       string
	start, end int // lines [start, end): the def line and its body
}

func fuzzDefs(lines []string) []fdef {
	var out []fdef
	for i, l := range lines {
		if !strings.HasPrefix(l, "def ") {
			continue
		}
		end := i + 1
		for end < len(lines) && (lines[end] == "" || strings.HasPrefix(lines[end], " ")) {
			end++
		}
		for end > i+1 && lines[end-1] == "" {
			end--
		}
		out = append(out, fdef{name: l[4:strings.Index(l, "(")], start: i, end: end})
	}
	return out
}

func splice(lines []string, start, end int, repl ...string) []string {
	out := append([]string{}, lines[:start]...)
	out = append(out, repl...)
	return append(out, lines[end:]...)
}

// mutate applies one random mutation to body; it names what it did.
func mutate(rng *rand.Rand, body string) (string, string) {
	lines := strings.Split(body, "\n")
	defs := fuzzDefs(lines)
	kinds := []string{"num", "list", "del", "rename", "raise", "loop", "dict", "ret"}
	kind := kinds[rng.Intn(len(kinds))]
	pick := func() fdef { return defs[rng.Intn(len(defs))] }
	if kind == "num" && len(fuzzNumRE.FindStringIndex(body)) == 0 || kind == "list" && len(fuzzListRE.FindStringIndex(body)) == 0 || kind != "num" && kind != "list" && kind != "dict" && len(defs) == 0 {
		return body, "noop"
	}
	switch kind {
	case "num":
		ms := fuzzNumRE.FindAllStringIndex(body, -1)
		m := ms[rng.Intn(len(ms))]
		repl := fuzzNums[rng.Intn(len(fuzzNums))]
		return body[:m[0]] + repl + body[m[1]:], fmt.Sprintf("num %q -> %s", body[m[0]:m[1]], repl)
	case "list":
		return mutateList(rng, body)
	case "del":
		d := pick()
		return strings.Join(splice(lines, d.start, d.end), "\n"), "delete def " + d.name
	case "rename":
		d := pick()
		nl := append([]string{}, lines...)
		nl[d.start] = "def " + d.name + "X" + nl[d.start][4+len(d.name):]
		return strings.Join(nl, "\n"), "rename def " + d.name
	case "raise":
		d := pick()
		return strings.Join(splice(lines, d.start+1, d.start+1, "    _boom = 1 // 0"), "\n"), "raise in " + d.name
	case "loop":
		d := pick()
		return strings.Join(splice(lines, d.start+1, d.start+1, "    for _i in range(100000000):", "        pass"), "\n"), "loop in " + d.name
	case "dict":
		return mutateDict(rng, body, lines, defs)
	default: // ret
		d := pick()
		v := fuzzRet[rng.Intn(len(fuzzRet))]
		return strings.Join(splice(lines, d.start+1, d.end, "    return "+v), "\n"), "return " + v + " from " + d.name
	}
}

// mutateList is mutate's "list" case (Round 3: split out of mutate, which
// was over the 60-line limit, unchanged).
func mutateList(rng *rand.Rand, body string) (string, string) {
	ms := fuzzListRE.FindAllStringIndex(body, -1)
	m := ms[rng.Intn(len(ms))]
	inner := body[m[0]+1 : m[1]-1]
	var elems []string
	if strings.TrimSpace(inner) != "" {
		elems = strings.Split(inner, ", ")
	}
	var op string
	switch {
	case len(elems) == 0:
		op = "add"
		elems = []string{`"x"`}
	default:
		switch rng.Intn(3) {
		case 0:
			op = "dup"
			i := rng.Intn(len(elems))
			ne := append([]string{}, elems[:i+1]...)
			elems = append(ne, elems[i:]...)
		case 1:
			op = "remove"
			i := rng.Intn(len(elems))
			elems = append(elems[:i], elems[i+1:]...)
		default:
			op = "replace"
			other := []string{"None", "{}", "1", `"x"`, "()", "[]", "True"}
			elems[rng.Intn(len(elems))] = other[rng.Intn(len(other))]
		}
	}
	return body[:m[0]] + "[" + strings.Join(elems, ", ") + "]" + body[m[1]:], "list " + op + " in " + body[m[0]:m[1]]
}

// mutateDict is mutate's "dict" case (Round 3: split out, unchanged).
func mutateDict(rng *rand.Rand, body string, lines []string, defs []fdef) (string, string) {
	var names []string
	for _, d := range defs {
		if _, ok := fuzzDicts[d.name]; ok {
			names = append(names, d.name)
		}
	}
	if rng.Intn(4) == 0 || len(names) == 0 {
		if len(names) == 0 && !slices.ContainsFunc(defs, func(d fdef) bool { return d.name == "prices" }) {
			return body, "noop"
		}
		// prices: an empty dict, or a bad entry
		for _, d := range defs {
			if d.name == "prices" {
				bad := []string{"{}", `{"haiku": {"in": 1, "out": 2}}`, `{"haiku": {"in": -1, "out": 2}}`, `{"haiku": {"in": 1}}`, `{7: {"in": 1, "out": 2}}`}
				return strings.Join(splice(lines, d.start+1, d.end, "    return "+bad[rng.Intn(len(bad))]), "\n"), "dict prices"
			}
		}
	}
	want := names[rng.Intn(len(names))]
	var d fdef
	for _, x := range defs {
		if x.name == want {
			d = x
			break
		}
	}
	kv := slices.Clone(fuzzDicts[d.name])
	var how string
	switch rng.Intn(3) {
	case 0:
		i := rng.Intn(len(kv))
		how = "missing " + kv[i][0]
		kv = append(kv[:i], kv[i+1:]...)
	case 1:
		how = "extra zz"
		kv = append(kv, [2]string{"zz", "1"})
	default:
		how = "wrong value"
		kv[rng.Intn(len(kv))][1] = `"x"`
	}
	parts := make([]string, len(kv))
	for i, p := range kv {
		parts[i] = fmt.Sprintf("%q: %s", p[0], p[1])
	}
	return strings.Join(splice(lines, d.start+1, d.end, "    return {"+strings.Join(parts, ", ")+"}"), "\n"), "dict " + how + " in " + d.name
}

// ---- domain check ----

// outOfDomain names the first answer, of those the validator passes, that is
// outside its domain against snap, or "" when every answer is in domain.
func outOfDomain(a Answers, snap store.Snapshot) string {
	for name, w := range a.Width {
		r := snap.Rows[name]
		if w.Work < 0 || w.Work > r.Width || w.Ahead < 0 || w.Ahead > 4*r.Width {
			return fmt.Sprintf("width(%s) = %+v", name, w)
		}
	}
	for q, names := range a.Rows {
		c := snap.Cards[q.Card]
		for _, n := range names {
			r := snap.Rows[n]
			if !slices.Contains(r.Tiers, c.Tier) || r.State != "up" || r.Held {
				return fmt.Sprintf("rows(%s %s) names %s", q.Card, q.Kind, n)
			}
		}
	}
	for id, n := range a.Reads {
		if n < 1 || n > 5 {
			return fmt.Sprintf("reads(%s) = %d", id, n)
		}
	}
	for id, v := range a.Verdict {
		if v != "yes" && v != "fix" && v != "retry" {
			return fmt.Sprintf("verdict(%s) = %q", id, v)
		}
	}
	for id, o := range a.Order {
		if len(o) > 4 {
			return fmt.Sprintf("order(%s) has %d elements", id, len(o))
		}
	}
	for s, l := range a.Landing {
		if l.Flowing < 0 || l.Flowing >= l.BackedUp || l.BackedUp > 1000 {
			return fmt.Sprintf("landing(%s) = %+v", s, l)
		}
	}
	return outOfDomainOnce(a)
}

// outOfDomainOnce is outOfDomain for the argument-free answers (Round 3:
// split out of outOfDomain, which was over the 60-line limit, unchanged).
func outOfDomainOnce(a Answers) string {
	l, t := a.Limits, a.Timeouts
	if l.Bounces < 1 || l.Bounces > 100 || l.Faults < 1 || l.Faults > 100 || l.SameFinding < 1 || l.SameFinding > 10 ||
		l.ReportsPerTick < 1 || l.ReportsPerTick > 10000 || l.RouteRestWindow < 1 || l.RouteRestWindow > 100 ||
		l.RouteRestAfter < 1 || l.RouteRestAfter > l.RouteRestWindow {
		return fmt.Sprintf("limits = %+v", l)
	}
	for _, s := range []int{t.RowSilentGone, t.LandingStall, t.Judgement, t.RouteRestFor, t.RowSilentWithdraw} {
		if s < 1 || s > maxSeconds {
			return fmt.Sprintf("timeouts = %+v", t)
		}
	}
	if t.RowSilentWithdraw < t.RowSilentGone || len(t.BaseRegate) == 0 {
		return fmt.Sprintf("timeouts = %+v", t)
	}
	for i := range t.BaseRegate {
		if t.BaseRegate[i] < 1 || t.BaseRegate[i] > maxSeconds || (i > 0 && t.BaseRegate[i] <= t.BaseRegate[i-1]) {
			return fmt.Sprintf("timeouts.base_regate = %v", t.BaseRegate)
		}
	}
	for m, p := range a.Prices {
		if p.In < 0 || p.Out < 0 {
			return fmt.Sprintf("prices(%s) = %+v", m, p)
		}
	}
	return ""
}

// ---- one file ----

type fuzzResult struct {
	idx   int
	ops   string
	class string // see TestFuzzPolicyRandom2000
	msg   string
}

// runOne runs one file through Set, then Answer on the big snapshot, with a
// wall clock of fuzzWallTime. A panic or a hang is its own class.
func runOne(dir string, idx int, body, ops string) fuzzResult {
	out := make(chan fuzzResult, 1)
	go func() {
		res := fuzzResult{idx: idx, ops: ops}
		defer func() {
			if p := recover(); p != nil {
				res.class, res.msg = "panic", fmt.Sprint(p)
				out <- res
			}
		}()
		out <- classify(dir, idx, body, res)
	}()
	select {
	case r := <-out:
		return r
	case <-time.After(fuzzWallTime):
		return fuzzResult{idx: idx, ops: ops, class: "hang", msg: "over 5 s wall clock"}
	}
}

func classify(dir string, idx int, body string, res fuzzResult) fuzzResult {
	ctx := context.Background()
	st, err := fuzzSeedStore()
	if err != nil {
		res.class, res.msg = "harness-error", err.Error()
		return res
	}
	path := filepath.Join(dir, fmt.Sprintf("fuzz-%04d.star", idx))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		res.class, res.msg = "harness-error", err.Error()
		return res
	}
	r, err := Set(ctx, st, NewEngine(), Caller{Actor: "rowan", Coordinator: true, OpID: "set", At: t0.Add(2 * time.Hour)}, path, "fuzz")
	if err != nil {
		var rf *Refusal
		if !errors.As(err, &rf) {
			res.class, res.msg = "set-store-error", err.Error()
			return res
		}
		res.msg = rf.Error()
		switch {
		case strings.HasSuffix(strings.TrimSpace(rf.What), ":") || strings.TrimSpace(rf.What) == "":
			res.class = "refused-empty"
		case !fuzzLineRE.MatchString(rf.What):
			res.class = "refused-no-line"
		default:
			res.class = "refused-named"
		}
		return res
	}
	big := fuzzBigSnapshot()
	a, fs := NewEngine().Answer(Versions{Active: pv(r.Version, body)}, big, FullQuestions(big, nil))
	switch {
	case len(fs) > 0:
		res.class, res.msg = "accepted-then-fails", fs[0].Error()
	case a.Version != r.Version:
		res.class, res.msg = "accepted-then-fails", fmt.Sprintf("answered by version %d", a.Version)
	default:
		if v := outOfDomain(a, big); v != "" {
			res.class, res.msg = "out-of-domain", v
		} else {
			res.class = "stored-in-domain"
		}
	}
	return res
}

type fuzzJob struct{ body, ops string }

// fuzzJobs is the deterministic set of fuzzFiles mutated policies.
func fuzzJobs() []fuzzJob {
	rng := rand.New(rand.NewSource(fuzzSeed))
	jobs := make([]fuzzJob, fuzzFiles)
	for i := range jobs {
		body := defaultBody
		n := 1 + rng.Intn(3)
		var ops []string
		for k := 0; k < n; k++ {
			var o string
			body, o = mutate(rng, body)
			ops = append(ops, o)
		}
		jobs[i] = fuzzJob{body, strings.Join(ops, "; ")}
	}
	return jobs
}

// TestFuzzPolicyReplay reruns files 5 and 76 of the 2000 one at a time, and the
// default alone, with the tick timed, to tell a policy fault from load.
func TestFuzzPolicyReplay(t *testing.T) {
	dir := t.TempDir()
	jobs := fuzzJobs()
	for _, i := range []int{5, 76} {
		t.Logf("file %d ops %s", i, jobs[i].ops)
		r := runOne(dir, i, jobs[i].body, jobs[i].ops)
		t.Logf("file %d serial: %s %s", i, r.class, r.msg)
	}
	big := fuzzBigSnapshot()
	for k := 0; k < 3; k++ {
		start := time.Now()
		_, fs := NewEngine().Answer(Versions{Active: pv(1, defaultBody)}, big, FullQuestions(big, nil))
		t.Logf("default cold tick %d: %s, refusals %d", k, time.Since(start), len(fs))
	}
}

// TestFuzzPolicyRandom2000 generates fuzzFiles mutated policies and runs each.
func TestFuzzPolicyRandom2000(t *testing.T) {
	if os.Getenv("NOVA_POLICY_FUZZ") != "1" {
		t.Skip("Round 3: a survey of 2,000 files; set NOVA_POLICY_FUZZ=1 to run it")
	}
	dir, err := os.MkdirTemp("", "fuzzpol")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	jobs := fuzzJobs()
	results := make([]fuzzResult, fuzzFiles)
	sem := make(chan struct{}, fuzzWorkers)
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = runOne(dir, i, jobs[i].body, jobs[i].ops)
		}(i)
	}
	wg.Wait()
	counts := map[string]int{}
	for _, r := range results {
		counts[r.class]++
	}
	t.Logf("outcome counts over %d files: %v", fuzzFiles, counts)
	for _, r := range results {
		switch r.class {
		case "stored-in-domain", "refused-named":
		default:
			t.Logf("file %d (%s) class %s: %s", r.idx, r.ops, r.class, r.msg)
		}
	}
}

// fuzzWithoutDef is the built-in default with question name's def removed: the
// minimal file that a question is missing.
func fuzzWithoutDef(name string) string {
	lines := strings.Split(defaultBody, "\n")
	for _, d := range fuzzDefs(lines) {
		if d.name == name {
			return strings.Join(splice(lines, d.start, d.end), "\n")
		}
	}
	panic("no def " + name)
}

// setMissing runs Set on the default minus limits and returns the refusal.
func setMissing(t *testing.T) *Refusal {
	st, err := fuzzSeedStore()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "limits-missing.star")
	if err := os.WriteFile(path, []byte(fuzzWithoutDef("limits")), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Set(context.Background(), st, NewEngine(), Caller{Actor: "rowan", Coordinator: true, OpID: "set", At: t0.Add(time.Hour)}, path, "fuzz")
	var rf *Refusal
	if !errors.As(err, &rf) {
		t.Fatalf("want a refusal, got %v", err)
	}
	return rf
}

// TestFuzzPolicyRefusalNamesVersion: a set refusal names the version, as the
// doc's form "POLICY REFUSED v7 line 31 ...". The default minus limits is
// refused naming the file (fuzz-*.star) because the next version is not
// assigned yet; none of the random run's 1,875 refusals named a version.
// Round 3: the ruling is that a refusal before storage names the file
// (documented in the question table), so this holds it to the file's name,
// and to ruling 2's form for what is missing, "line N, end of file".
func TestFuzzPolicyRefusalNamesVersion(t *testing.T) {
	rf := setMissing(t)
	if !regexp.MustCompile(`POLICY REFUSED limits-missing\.star line \d+, end of file: `).MatchString(rf.What) {
		t.Errorf("refusal names no file: %s", rf.Error())
	}
}

// TestFuzzPolicyRefusalNamesLine: a refusal names the line. The default minus
// limits is refused with no line at all (246 of the random run's refusals).
func TestFuzzPolicyRefusalNamesLine(t *testing.T) {
	rf := setMissing(t)
	if !fuzzLineRE.MatchString(rf.What) {
		t.Errorf("refusal names no line: %s", rf.Error())
	}
}

// TestFuzzPolicyDefaultTickUnderLoad: the built-in's cold tick on the suite's
// largest snapshot must fit TickTime while the machine is busy. Eight ticks at
// once (the fuzz's own parallelism) on the default: none may fail.
func TestFuzzPolicyDefaultTickUnderLoad(t *testing.T) {
	big := fuzzBigSnapshot()
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			_, fs := NewEngine().Answer(Versions{Active: pv(1, defaultBody)}, big, FullQuestions(big, nil))
			if len(fs) > 0 {
				errs <- fmt.Sprintf("%s (after %s)", fs[0].Error(), time.Since(start))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("default tick failed under load: %s", e)
	}
}
