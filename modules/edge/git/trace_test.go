package git

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// The trace walk of tla/LandEdge.tla: LandPass (layer 4) against the git
// fake and a truthful forge, driven by a walk that plays the tick (layer
// 3's landing, as the model has it), outside commits to base, and the
// coordinator's answers. Each step's state is projected onto the model's
// variables and written as LandEdgeTraceLog.tla under NOVA_TRACE_DIR;
// tla/check-trace.sh LandEdge checks every step is one of the model's.
// The projection reads the trees themselves: a tree holds card c when it
// holds c's file, and its ext is the count of outside commits' files.

const (
	walkK        = 2 // LandEdgeTrace.cfg's K
	walkBackedUp = 2 // LandEdgeTrace.cfg's BackedUp
)

var (
	walkCards   = []string{"c1", "c2", "c3"}
	walkStreams = []string{"s1", "s2"}
)

func walkStream_internal(c string) string {
	if c == "c3" {
		return "s2"
	}
	return "s1"
}

type walkEntry_internal struct{ sha, how, gated string }

// walkReq_internal is a request as the model has it: its gate is the sha
// LandPass gated (projected to its tree), gbase the length of base when it
// was gated, ci and res its checks as the walk's CI has them, reran and
// late what the run (played by the walk) holds for it.
type walkReq_internal struct {
	cards         []string
	st, out       string
	rung, gbase   int
	gate, ci, res string
	reran, late   bool
}

func newWalkReq_internal(cards []string, st string) walkReq_internal {
	return walkReq_internal{cards: cards, st: st, out: "none", ci: "none", res: "none"}
}

type landWalk_internal struct {
	t      *testing.T
	rng    *rand.Rand
	g      *Fake
	ff     *walkForge_internal
	heads  map[string]string
	base   []walkEntry_internal
	cs     map[string]string
	req    map[string]walkReq_internal
	js     map[string]bool
	held   map[string]bool
	ticked bool
	passes int
}

// walkForge_internal is the fake forge with truthful CI that takes time:
// the request's gate (the sha the pass gated) has the checks the walk's
// CIStep moved it to (running, partial: a failure while others run, done
// green, red or stopped: every failing check killed), a sha new to the
// request is running, unless the walk chose absent. A green read of the
// gate may move base under it (an outside commit). A base tip is
// completed, red only on a red tree. old is the v2.0.36 forge for a
// breaker: a failure while others still run reads as completed, and a stop
// reads as a failure.
type walkForge_internal struct {
	*fakeForge_internal
	w                 *landWalk_internal
	gate, ci, res     string
	absent, move, old bool
}

const walkJob = "https://example.invalid/actions/runs/1/job/2"

func (f *walkForge_internal) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	f.call_internal("Checks")
	out := make(map[string]forge.CheckRollup, len(shas))
	for _, sha := range shas {
		r := forge.CheckRollup{SHA: sha, State: forge.CheckGreen, Failing: []string{"test"}, URL: walkJob}
		cards, _ := f.w.project_internal(sha)
		red := slices.Contains(cards, "c1") && slices.Contains(cards, "c3")
		ci, res := "done", map[bool]string{true: "red", false: "green"}[red]
		base := slices.ContainsFunc(f.w.base, func(e walkEntry_internal) bool { return e.sha == sha })
		switch {
		case sha == f.gate:
			ci, res = f.ci, f.res
		case !base:
			ci, res = "running", "none" // a gate new to the request
		}
		switch {
		case f.absent && !base:
			r.State = forge.CheckAbsent
		case ci == "running":
			r.State = forge.CheckPending
		case ci == "partial":
			r.State, r.Waiting, r.Pending = forge.CheckRed, !f.old, 1
		case res == "green":
			if f.move && sha == f.gate {
				f.w.extCommit_internal()
			}
		default:
			r.State = forge.CheckRed
		}
		out[sha] = r
	}
	return out, nil
}

// Failures is the gate's failing check, killed when its checks completed
// stopped (never, on the old forge).
func (f *walkForge_internal) Failures(_ context.Context, _, sha string) ([]forge.Failure, error) {
	f.call_internal("Failures")
	stop := ""
	if sha == f.gate && f.res == "stopped" && !f.old {
		stop = "killed (exit code 137)"
	}
	return []forge.Failure{{Name: "test", URL: walkJob, Stop: stop}}, nil
}

func newLandWalk_internal(t *testing.T, seed int64) *landWalk_internal {
	g := NewFake(nil)
	w := &landWalk_internal{t: t, rng: rand.New(rand.NewSource(seed)), g: g, heads: map[string]string{},
		cs: map[string]string{}, req: map[string]walkReq_internal{}, js: map[string]bool{}, held: map[string]bool{}}
	w.ff = &walkForge_internal{fakeForge_internal: newFakeForge_internal(), w: w}
	init := g.AddCommit("", map[string]string{"README.md": "init\n", "shared.txt": "init\n"}, "main")
	w.base = []walkEntry_internal{{sha: init, how: "init"}}
	for _, c := range walkCards {
		files := map[string]string{c + ".txt": c + "\n"}
		if c != "c1" { // c2 and c3 change one file: they do not merge together
			files["shared.txt"] = c + "\n"
		}
		w.heads[c] = g.AddCommit(init, files, "sprint/"+walkStream_internal(c)+"/"+c)
		w.cs[c] = "merging"
	}
	for _, s := range walkStreams {
		w.req[s] = newWalkReq_internal(nil, "none")
	}
	return w
}

// baseIndex_internal is the model's length of base when sha was its tip.
func (w *landWalk_internal) baseIndex_internal(sha string) int {
	for i := len(w.base) - 1; i >= 0; i-- {
		if w.base[i].sha == sha {
			return i + 1
		}
	}
	w.t.Fatalf("base tip %s is not in the walk's base", sha)
	return 0
}

// project_internal is a commit's tree as the model has it: the cards whose
// files it holds and the count of outside commits in it.
func (w *landWalk_internal) project_internal(sha string) ([]string, int) {
	w.g.world.mu.Lock()
	defer w.g.world.mu.Unlock()
	c, ok := w.g.world.commits[sha]
	if !ok {
		w.t.Fatalf("project: unknown commit %s", sha)
	}
	var cards []string
	for _, card := range walkCards {
		if _, ok := c.Files[card+".txt"]; ok {
			cards = append(cards, card)
		}
	}
	ext := 0
	for f := range c.Files {
		if strings.HasPrefix(f, "ext_") {
			ext++
		}
	}
	return cards, ext
}

func (w *landWalk_internal) tip_internal() string { return w.g.OriginRef("refs/heads/main") }

func (w *landWalk_internal) extCount_internal() int {
	_, ext := w.project_internal(w.tip_internal())
	return ext
}

// extCommit_internal is an outside commit to base.
func (w *landWalk_internal) extCommit_internal() {
	n := w.extCount_internal() + 1
	sha := w.g.AddCommit(w.tip_internal(), map[string]string{fmt.Sprintf("ext_%d.txt", n): "ext\n"}, "main")
	w.base = append(w.base, walkEntry_internal{sha: sha, how: "ext"})
}

// handMerge_internal is the coordinator's merge by hand: base's tip and
// the card's head, a conflict on shared.txt resolved by hand.
func (w *landWalk_internal) handMerge_internal(c string) {
	w.g.world.mu.Lock()
	tip := w.g.world.originRefs["refs/heads/main"]
	files := map[string]string{}
	for k, v := range w.g.world.commits[tip].Files {
		files[k] = v
	}
	for k, v := range w.g.world.commits[w.heads[c]].Files {
		if k == "shared.txt" && files[k] != "init\n" && files[k] != v {
			v = "resolved by hand\n"
		}
		files[k] = v
	}
	w.g.world.seq++
	sha := fmt.Sprintf("%040x", w.g.world.seq)
	w.g.world.commits[sha] = FakeCommit{SHA: sha, Parents: []string{tip, w.heads[c]}, Files: files, Tree: computeTreeHash_internal(files)}
	w.g.world.originRefs[sha] = sha
	w.g.world.mu.Unlock()
	w.g.SetOriginRef("refs/heads/main", sha)
	w.base = append(w.base, walkEntry_internal{sha: sha, how: "hand"})
}

// tick_internal is the model's Tick: consume the finished requests (landed
// moves the cards, anything else opens a land judgement for each card
// still merging), then ask for each stream that had none in flight.
func (w *landWalk_internal) tick_internal() {
	busy := map[string]bool{}
	for _, s := range walkStreams {
		r := w.req[s]
		busy[s] = r.st != "none"
		if r.st != "done" {
			continue
		}
		for _, c := range r.cards {
			if w.cs[c] != "merging" {
				continue
			}
			if r.out == "landed" {
				w.cs[c] = "landed"
			} else {
				w.js[c] = true
			}
		}
	}
	for _, s := range walkStreams {
		var e []string
		for _, c := range walkCards {
			if walkStream_internal(c) == s && w.cs[c] == "merging" && !w.js[c] {
				e = append(e, c)
			}
		}
		switch {
		case !busy[s] && len(e) > 0:
			if len(e) < walkBackedUp {
				e = e[:1]
			}
			w.req[s] = newWalkReq_internal(e, "sent")
		case w.req[s].st == "done":
			w.req[s] = newWalkReq_internal(nil, "none")
		}
	}
	w.held, w.ticked = busy, true
}

var walkOutcomes = map[LandOutcome]string{OutcomeLanded: "landed", OutcomeConflict: "conflict",
	OutcomeRed: "red", OutcomeAbsent: "absent", OutcomeRefused: "merge_refused", OutcomeNoVerdict: "noverdict"}

// pass_internal is one LandPass for stream s's request, with the walk's
// choice of what CI and the forge do, and what the run makes of its
// outcome (layer 6, as edges.land: pending and unfinished are asked again,
// unfinished after the wait is no verdict, a rerun is remembered).
func (w *landWalk_internal) pass_internal(s string) {
	w.passWith_internal(s, w.rng.Intn(8) == 0, w.extCount_internal() < walkK && w.rng.Intn(6) == 0, w.rng.Intn(6) == 0)
}

// passWith_internal is pass_internal with the forge's choices given: absent
// CI, a base that moves under a green gate, a refused merge.
func (w *landWalk_internal) passWith_internal(s string, absent, move, refuse bool) {
	r := w.req[s]
	req := LandRequest{Repo: "o/r", Stream: s, Base: "main", Reran: r.reran}
	for _, c := range r.cards {
		req.Heads = append(req.Heads, w.heads[c])
		req.Branches = append(req.Branches, "sprint/"+s+"/"+c)
	}
	w.ff.gate, w.ff.ci, w.ff.res = r.gate, r.ci, r.res
	w.ff.absent, w.ff.move = absent, move
	w.ff.OnMerge = func(sha string) (string, error) {
		if refuse {
			return "", fmt.Errorf("required reviews missing")
		}
		w.g.SetOriginRef("refs/heads/main", sha)
		return sha, nil
	}
	w.passes++
	res, err := LandPass(context.Background(), w.g, w.ff, req)
	if err != nil {
		w.t.Fatalf("LandPass %s: %v", s, err)
	}
	if res.GatedSHA != "" && (res.GatedSHA != r.gate || r.ci == "none") { // a gate new to the request: its CI starts
		r.gate, r.gbase, r.ci, r.res, r.late = res.GatedSHA, w.baseIndex_internal(res.BaseTip), "running", "none", false
	}
	switch res.Outcome {
	case OutcomeBaseMoved, OutcomePending:
		w.req[s] = r // asked again: the request stays in flight
		return
	case OutcomeUnfinished:
		if !r.late {
			w.req[s] = r
			return
		}
		res.Outcome = OutcomeNoVerdict // the wait passed (edges.landItem)
	case OutcomeRerun:
		r.ci, r.res, r.reran, r.late = "running", "none", true, false
		w.req[s] = r
		return
	}
	out, ok := walkOutcomes[res.Outcome]
	if !ok {
		w.t.Fatalf("LandPass %s: outcome %s is not one the walk drives (%s)", s, res.Outcome, res.Reason)
	}
	if res.Outcome == OutcomeLanded && res.MergeSHA != res.BaseTip {
		if tip := w.tip_internal(); tip != res.MergeSHA {
			w.t.Fatalf("LandPass %s: merged %s but base is at %s", s, res.MergeSHA, tip)
		}
		w.base = append(w.base, walkEntry_internal{sha: res.MergeSHA, how: "auto", gated: res.GatedSHA})
	}
	r.st, r.out, r.rung = "done", out, res.Rung
	w.req[s] = r
}

// ciStep_internal is the model's CIStep: stream s's gate's checks move on,
// truthfully (red only on a red tree; any tree can be stopped).
func (w *landWalk_internal) ciStep_internal(s string) {
	r := w.req[s]
	cards, _ := w.project_internal(r.gate)
	done := []string{"green", "stopped"}
	if slices.Contains(cards, "c1") && slices.Contains(cards, "c3") {
		done = []string{"red", "stopped"}
	}
	switch {
	case r.ci == "running" && w.rng.Intn(3) == 0:
		r.ci = "partial"
	case r.ci == "running":
		r.ci, r.res = "done", done[w.rng.Intn(2)]
	case done[0] == "green": // partial: never green once a check failed or was stopped
		r.ci, r.res = "done", "stopped"
	default:
		r.ci, r.res = "done", done[w.rng.Intn(2)]
	}
	w.req[s] = r
}

// answer_internal is the coordinator's answer to card c's land judgement.
func (w *landWalk_internal) answer_internal(c, choice string) {
	delete(w.js, c)
	switch choice {
	case "land":
		w.cs[c] = "landed"
		if cards, _ := w.project_internal(w.tip_internal()); !slices.Contains(cards, c) {
			w.handMerge_internal(c)
		}
	case "fix":
		w.cs[c] = "fix"
	}
}

type walkMove_internal struct{ act, arg, choice string }

// step_internal picks one enabled move at random, applies it and returns it.
func (w *landWalk_internal) step_internal() walkMove_internal {
	moves := []walkMove_internal{{act: "Tick"}}
	for _, s := range walkStreams {
		r := w.req[s]
		if r.st != "sent" {
			continue
		}
		moves = append(moves, walkMove_internal{act: "Pass", arg: s}, walkMove_internal{act: "Pass", arg: s})
		if r.ci == "running" || r.ci == "partial" {
			moves = append(moves, walkMove_internal{act: "CIStep", arg: s}, walkMove_internal{act: "CIStep", arg: s})
		}
		if r.ci == "partial" && !r.late {
			moves = append(moves, walkMove_internal{act: "Late", arg: s})
		}
	}
	if w.extCount_internal() < walkK && w.rng.Intn(8) == 0 {
		moves = append(moves, walkMove_internal{act: "BaseMoves"})
	}
	for _, c := range walkCards {
		if w.js[c] {
			choice := []string{"land", "land", "fix", "retry"}[w.rng.Intn(4)]
			moves = append(moves, walkMove_internal{act: "Answer", arg: c, choice: choice})
		}
	}
	m := moves[w.rng.Intn(len(moves))]
	switch m.act {
	case "Tick":
		w.tick_internal()
		return m
	case "Pass":
		w.pass_internal(m.arg)
	case "CIStep":
		w.ciStep_internal(m.arg)
	case "Late":
		r := w.req[m.arg]
		r.late = true
		w.req[m.arg] = r
	case "BaseMoves":
		w.extCommit_internal()
	case "Answer":
		w.answer_internal(m.arg, m.choice)
	}
	w.ticked = false
	return m
}

func (w *landWalk_internal) done_internal() bool {
	for _, c := range walkCards {
		if w.cs[c] == "merging" {
			return false
		}
	}
	return w.req["s1"].st == "none" && w.req["s2"].st == "none"
}

// walkLandEdge_internal walks one seed to the end (every card landed or
// sent to fix, no request in flight) and returns the trace log module.
func walkLandEdge_internal(t *testing.T, seed int64) (string, []string) {
	w := newLandWalk_internal(t, seed)
	records := []string{w.record_internal(1, walkMove_internal{act: "Init"})}
	acts := []string{"Init"}
	for i := 0; i < 1000 && !w.done_internal(); i++ {
		m := w.step_internal()
		records = append(records, w.record_internal(len(records)+1, m))
		acts = append(acts, m.act+":"+m.arg+":"+m.choice)
	}
	if !w.done_internal() {
		t.Fatalf("seed %d: the walk did not finish in 1000 steps: %v", seed, w.cs)
	}
	return traceModule_internal(records), acts
}

func traceModule_internal(records []string) string {
	return "---------------------------- MODULE LandEdgeTraceLog ----------------------------\n" +
		"EXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(records, ",\n") + "\n>>\n" +
		"=============================================================================\n"
}

// landEdgeBreakers are the old behaviours the trace check must refuse: the
// v2.0.36 forge (walkForge_internal.old) with the gate's CI at partial (a
// failure while others still run, judged red at once) or done stopped
// (every failing check killed, judged red).
var landEdgeBreakers = map[string]struct{ ci, res string }{
	"landedge-breaker-red-while-running": {"partial", "none"},
	"landedge-breaker-stopped-is-red":    {"done", "stopped"},
}

// breakerLandEdge_internal is the breaker's trace: a request for c1, its
// first pass (the gate's CI starts), CI moved to the breaker's state, and
// one pass on the old forge, which judges red: the record the model refuses.
func breakerLandEdge_internal(t *testing.T, name string) []string {
	w := newLandWalk_internal(t, 1)
	b := landEdgeBreakers[name]
	records := []string{w.record_internal(1, walkMove_internal{act: "Init"})}
	step := func(m walkMove_internal) { records = append(records, w.record_internal(len(records)+1, m)) }
	w.tick_internal()
	step(walkMove_internal{act: "Tick"})
	w.ticked = false
	w.passWith_internal("s1", false, false, false)
	step(walkMove_internal{act: "Pass", arg: "s1"})
	r := w.req["s1"]
	if b.ci == "done" {
		r.ci = "partial" // running -> done is one CIStep; partial first keeps both breakers one shape
		w.req["s1"] = r
		step(walkMove_internal{act: "CIStep", arg: "s1"})
	}
	r.ci, r.res = b.ci, b.res
	w.req["s1"] = r
	step(walkMove_internal{act: "CIStep", arg: "s1"})
	w.ff.old = true
	w.passWith_internal("s1", false, false, false)
	step(walkMove_internal{act: "Pass", arg: "s1"})
	if w.req["s1"].out != "red" {
		t.Fatalf("%s: the old forge's pass is %q, want red", name, w.req["s1"].out)
	}
	return records
}

// TestLandEdgeTraceRefusesOldBehaviours holds the breakers: each one's
// trace is the fixture tla/traces/<name>, which check-trace.sh must refuse
// at its last record (the old judgement), so the trace check would catch
// the v2.0.36 landing.
func TestLandEdgeTraceRefusesOldBehaviours(t *testing.T) {
	t.Parallel()
	cfg, err := os.ReadFile(filepath.Join("..", "..", "..", "tla", "LandEdgeTrace.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	for name := range landEdgeBreakers {
		records := breakerLandEdge_internal(t, name)
		files := map[string]string{
			"LandEdgeTraceLog.tla": traceModule_internal(records),
			"LandEdgeTrace.cfg": "\\* Generated by modules/edge/git/trace_test.go; do not edit.\n" +
				fmt.Sprintf("\\* expect: forbidden at %d\n", len(records)) + strings.Replace(string(cfg), "\\* expect: matched\n", "", 1),
		}
		for file, text := range files {
			if out := os.Getenv("NOVA_TRACE_DIR"); out != "" {
				if err := os.MkdirAll(filepath.Join(out, name), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, name, file), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(filepath.Join("..", "..", "..", "tla", "traces", name, file))
			if err != nil || string(want) != text {
				t.Errorf("tla/traces/%s/%s is stale or missing (NOVA_TRACE_DIR=tla/traces go test -run TestLandEdgeTraceRefusesOldBehaviours ./modules/edge/git/ writes it): %v", name, file, err)
			}
		}
	}
}

func tlaSet_internal(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = fmt.Sprintf("%q", v)
	}
	return "{" + strings.Join(q, ", ") + "}"
}

func tlaKeys_internal(m map[string]bool) string {
	var ks []string
	for k, v := range m {
		if v {
			ks = append(ks, k)
		}
	}
	slices.Sort(ks)
	return tlaSet_internal(ks)
}

func (w *landWalk_internal) tree_internal(sha string) string {
	if sha == "" {
		return "[cards |-> {}, ext |-> 0]"
	}
	cards, ext := w.project_internal(sha)
	return fmt.Sprintf("[cards |-> %s, ext |-> %d]", tlaSet_internal(cards), ext)
}

// record_internal is TraceLog record n: the move and the state after it.
func (w *landWalk_internal) record_internal(n int, m walkMove_internal) string {
	base := make([]string, len(w.base))
	for i, e := range w.base {
		base[i] = fmt.Sprintf("[tree |-> %s, how |-> %q, gated |-> %s]", w.tree_internal(e.sha), e.how, w.tree_internal(e.gated))
	}
	cs := make([]string, len(walkCards))
	for i, c := range walkCards {
		cs[i] = fmt.Sprintf("%s |-> %q", c, w.cs[c])
	}
	req := make([]string, len(walkStreams))
	for i, s := range walkStreams {
		r := w.req[s]
		req[i] = fmt.Sprintf("%s |-> [cards |-> %s, st |-> %q, out |-> %q, rung |-> %d, gate |-> %s, gbase |-> %d, ci |-> %q, res |-> %q, reran |-> %s, late |-> %s]",
			s, tlaSet_internal(r.cards), r.st, r.out, r.rung, w.tree_internal(r.gate), r.gbase, r.ci, r.res, strings.ToUpper(fmt.Sprint(r.reran)), strings.ToUpper(fmt.Sprint(r.late)))
	}
	return fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, choice |-> %q, base |-> << %s >>, cs |-> [%s], req |-> [%s], js |-> %s, held |-> %s, ticked |-> %s, passes |-> %d]",
		n, m.act, m.arg, m.choice, strings.Join(base, ", "), strings.Join(cs, ", "), strings.Join(req, ", "),
		tlaKeys_internal(w.js), tlaKeys_internal(w.held), strings.ToUpper(fmt.Sprint(w.ticked)), w.passes)
}

// TestLandEdgeTraceRefinementWalk walks eight seeds to the end and, with
// NOVA_TRACE_DIR set, writes each one's trace for tla/check-trace.sh. It
// fails when the walks together never reach a rung of the ladder.
func TestLandEdgeTraceRefinementWalk(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for seed := int64(1); seed <= 8; seed++ {
		text, acts := walkLandEdge_internal(t, seed)
		for _, a := range acts {
			seen[strings.SplitN(a, ":", 2)[0]] = true
		}
		seen["rung2"] = seen["rung2"] || strings.Contains(text, "rung |-> 2")
		seen["rung1"] = seen["rung1"] || strings.Contains(text, "rung |-> 1")
		seen["hand"] = seen["hand"] || strings.Contains(text, `how |-> "hand"`)
		seen["noverdict"] = seen["noverdict"] || strings.Contains(text, `out |-> "noverdict"`)
		seen["rerun"] = seen["rerun"] || strings.Contains(text, `reran |-> TRUE`)
		seen["red"] = seen["red"] || strings.Contains(text, `out |-> "red"`)
		if dir := os.Getenv("NOVA_TRACE_DIR"); dir != "" {
			sub := filepath.Join(dir, fmt.Sprintf("landedge-walk-%d", seed))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "LandEdgeTraceLog.tla"), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, want := range []string{"Tick", "Pass", "CIStep", "Late", "Answer", "BaseMoves", "rung1", "rung2", "hand", "noverdict", "rerun", "red"} {
		if !seen[want] {
			t.Errorf("the walks never reached %s", want)
		}
	}
}
