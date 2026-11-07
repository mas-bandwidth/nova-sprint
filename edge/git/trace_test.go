package git

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

type landBaseEntry_internal struct {
	sha, parent        int
	stream             string
	heads              []string
	gate               string
	gatedTip, gatedSHA int
}

type landGatedAt_internal struct {
	baseTip, sha int
	gate         string
	blamed       []string
	chased       bool
	faults       int
}

type landTraceStep_internal struct {
	action  string
	arg     string
	base    []landBaseEntry_internal
	stream  map[string]int
	group   map[string][]string
	step    map[string]string
	gatedAt map[string]landGatedAt_internal
	landed  []string
}

type landWalkHarness_internal struct {
	g         *Fake
	fg        *fakeForge_internal
	shaToID   map[string]int
	idToSHA   map[int]string
	base      []landBaseEntry_internal
	group     map[string][]string
	headSHAs  map[string]string
	step      map[string]string
	gatedAt   map[string]landGatedAt_internal
	mergedSHA map[string]string
	landed    []string
}

func newLandWalkHarness_internal(t *testing.T) *landWalkHarness_internal {
	t.Helper()
	now := int64(1000)
	gf := NewFake(func() int64 { return now })
	ff := newFakeForge_internal()
	initSHA := gf.AddCommit("", map[string]string{"README.md": "init\n"}, "main")
	gf.SetOriginRef(StreamRef("s1"), initSHA)
	gf.SetOriginRef(StreamRef("s2"), initSHA)
	h1SHA := gf.AddCommit(initSHA, map[string]string{"README.md": "init\n", "s1_h1.txt": "h1\n"}, "sprint/s1/h1")
	h2SHA := gf.AddCommit(initSHA, map[string]string{"README.md": "init\n", "s1_h2.txt": "h2\n"}, "sprint/s1/h2")
	h3SHA := gf.AddCommit(initSHA, map[string]string{"README.md": "init\n", "s2_h3.txt": "h3\n"}, "sprint/s2/h3")
	return &landWalkHarness_internal{
		g:       gf,
		fg:      ff,
		shaToID: map[string]int{initSHA: 1},
		idToSHA: map[int]string{1: initSHA},
		base: []landBaseEntry_internal{
			{sha: 1, parent: 0, stream: "init", gate: "green", gatedTip: 0, gatedSHA: 1},
		},
		group:     map[string][]string{"s1": {"h1", "h2"}, "s2": {"h3"}},
		headSHAs:  map[string]string{"h1": h1SHA, "h2": h2SHA, "h3": h3SHA},
		step:      map[string]string{"s1": "idle", "s2": "idle"},
		gatedAt:   map[string]landGatedAt_internal{"s1": {gate: "none"}, "s2": {gate: "none"}},
		mergedSHA: map[string]string{},
	}
}

func candidateModelID_internal(s string, baseTip int, groupLen int) int {
	if s == "s1" {
		return 100 + baseTip + groupLen
	}
	return 200 + baseTip + groupLen
}

// refinementLandEdgeState_internal projects the real git.Fake + forge state onto
// LandEdge.tla variables <<base, stream, group, step, gatedAt, landed>>.
func refinementLandEdgeState_internal(t *testing.T, h *landWalkHarness_internal, action, arg string) landTraceStep_internal {
	t.Helper()
	ctx := context.Background()
	streamMap := make(map[string]int, 2)
	for _, s := range []string{"s1", "s2"} {
		sha, ok, err := h.g.LsRemote(ctx, "", StreamRef(s))
		if err != nil || !ok {
			t.Fatalf("LsRemote stream %s: ok=%v err=%v", s, ok, err)
		}
		streamMap[s] = h.shaToID[sha]
	}
	stepMap := map[string]string{"s1": h.step["s1"], "s2": h.step["s2"]}
	gatedMap := map[string]landGatedAt_internal{"s1": h.gatedAt["s1"], "s2": h.gatedAt["s2"]}
	groupMap := map[string][]string{
		"s1": append([]string(nil), h.group["s1"]...),
		"s2": append([]string(nil), h.group["s2"]...),
	}
	return landTraceStep_internal{
		action:  action,
		arg:     arg,
		base:    append([]landBaseEntry_internal(nil), h.base...),
		stream:  streamMap,
		group:   groupMap,
		step:    stepMap,
		gatedAt: gatedMap,
		landed:  append([]string(nil), h.landed...),
	}
}

func (h *landWalkHarness_internal) headSHAsFor_internal(s string) []string {
	var out []string
	for _, hid := range h.group[s] {
		out = append(out, h.headSHAs[hid])
	}
	return out
}

func (h *landWalkHarness_internal) execStreamAction_internal(t *testing.T, s, act string) {
	t.Helper()
	ctx := context.Background()
	ga := h.gatedAt[s]
	switch act {
	case "Fetch":
		baseSHA, ok, err := h.g.LsRemote(ctx, "", "refs/heads/main")
		if err != nil || !ok {
			t.Fatalf("Fetch main: %v", err)
		}
		ga.baseTip = h.shaToID[baseSHA]
		h.gatedAt[s] = ga
		h.step[s] = "fetched"
	case "Merge":
		baseSHA := h.idToSHA[ga.baseTip]
		heads := h.headSHAsFor_internal(s)
		m1, err := h.g.MergeGroup(ctx, "", baseSHA, heads, nil)
		if err != nil || m1.Kind != MergeOK {
			t.Fatalf("MergeGroup %s: %+v err=%v", s, m1, err)
		}
		m2, err := h.g.MergeGroup(ctx, "", baseSHA, heads, nil)
		if err != nil || m2.SHA != m1.SHA {
			t.Fatalf("MergeGroup non-deterministic: %s vs %s", m1.SHA, m2.SHA)
		}
		cid := candidateModelID_internal(s, ga.baseTip, len(h.group[s]))
		h.shaToID[m1.SHA] = cid
		h.idToSHA[cid] = m1.SHA
		h.mergedSHA[s] = m1.SHA
		ga.sha = cid
		ga.gate = "none"
		ga.blamed = nil
		h.gatedAt[s] = ga
		h.step[s] = "merged"
	case "PushStream":
		oldSHA, _, _ := h.g.LsRemote(ctx, "", StreamRef(s))
		if err := h.g.Push(ctx, "", h.mergedSHA[s], StreamRef(s), oldSHA); err != nil {
			t.Fatalf("PushStream %s: %v", s, err)
		}
		h.step[s] = "pushed"
	case "Gate":
		ga.gate = "none"
		ga.blamed = nil
		h.gatedAt[s] = ga
		h.step[s] = "gating"
	case "CIGreen", "CIRed", "CIFault":
		h.applyCIOutcome_internal(t, s, act)
	case "Land":
		h.applyLand_internal(t, s)
	case "Remerge":
		h.applyRemerge_internal(t, s)
	case "Refuse":
		h.step[s] = "refused"
	}
}

func (h *landWalkHarness_internal) applyCIOutcome_internal(t *testing.T, s, act string) {
	t.Helper()
	ctx := context.Background()
	ga := h.gatedAt[s]
	msha := h.mergedSHA[s]
	switch act {
	case "CIGreen":
		h.fg.SetCheck("org/repo", msha, forge.CheckRollup{State: forge.CheckGreen})
		rep, err := h.fg.Checks(ctx, "org/repo", []string{msha})
		if err != nil || rep[msha].State != forge.CheckGreen {
			t.Fatalf("Checks green: %+v err=%v", rep, err)
		}
		ga.gate = "green"
		ga.blamed = nil
		h.step[s] = "green"
	case "CIRed":
		h.fg.SetCheck("org/repo", msha, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}})
		ga.gate = "red"
		ga.blamed = []string{h.group[s][0]}
		h.step[s] = "red"
	case "CIFault":
		h.fg.SetCheck("org/repo", msha, forge.CheckRollup{State: forge.CheckFault})
		ga.gate = "fault"
		ga.blamed = nil
		ga.faults++
		h.step[s] = "fault"
	}
	h.gatedAt[s] = ga
}

func (h *landWalkHarness_internal) applyLand_internal(t *testing.T, s string) {
	t.Helper()
	ctx := context.Background()
	ga := h.gatedAt[s]
	msha := h.mergedSHA[s]
	res, err := LandPass(ctx, h.g, h.fg, LandRequest{Stream: s, Repo: "org/repo", Base: "main", Heads: h.headSHAsFor_internal(s)})
	if err != nil || res.Outcome != OutcomeLanded || res.GatedSHA != msha {
		t.Fatalf("LandPass %s: res=%+v err=%v want=%s", s, res, err, msha)
	}
	h.base = append(h.base, landBaseEntry_internal{
		sha:      ga.sha,
		parent:   ga.baseTip,
		stream:   s,
		heads:    append([]string(nil), h.group[s]...),
		gate:     ga.gate,
		gatedTip: ga.baseTip,
		gatedSHA: ga.sha,
	})
	h.landed = append(h.landed, h.group[s]...)
	h.step[s] = "landed"
}

func (h *landWalkHarness_internal) applyRemerge_internal(t *testing.T, s string) {
	t.Helper()
	ctx := context.Background()
	baseSHA, _, _ := h.g.LsRemote(ctx, "", "refs/heads/main")
	baseTip := h.shaToID[baseSHA]
	heads := h.headSHAsFor_internal(s)
	m, err := h.g.MergeGroup(ctx, "", baseSHA, heads, nil)
	if err != nil || m.Kind != MergeOK {
		t.Fatalf("Remerge %s: %+v err=%v", s, m, err)
	}
	cid := candidateModelID_internal(s, baseTip, len(h.group[s]))
	h.shaToID[m.SHA] = cid
	h.idToSHA[cid] = m.SHA
	h.mergedSHA[s] = m.SHA
	oldStream, _, _ := h.g.LsRemote(ctx, "", StreamRef(s))
	if err := h.g.Push(ctx, "", m.SHA, StreamRef(s), oldStream); err != nil {
		t.Fatalf("Remerge push stream %s: %v", s, err)
	}
	ga := h.gatedAt[s]
	ga.baseTip = baseTip
	ga.sha = cid
	ga.gate = "none"
	ga.blamed = nil
	ga.chased = false
	h.gatedAt[s] = ga
	h.step[s] = "pushed"
}

func (h *landWalkHarness_internal) applyBaseMove_internal(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	oldSHA, _, _ := h.g.LsRemote(ctx, "", "refs/heads/main")
	oldID := h.shaToID[oldSHA]
	nextID := oldID + 10
	extSHA := h.g.AddCommit(oldSHA, map[string]string{
		"README.md":                       "init\n",
		fmt.Sprintf("ext_%d.txt", nextID): "ext\n",
	}, "")
	h.shaToID[extSHA] = nextID
	h.idToSHA[nextID] = extSHA
	if err := h.g.Push(ctx, "", extSHA, "refs/heads/main", oldSHA); err != nil {
		t.Fatalf("BaseMoves push: %v", err)
	}
	h.base = append(h.base, landBaseEntry_internal{
		sha:      nextID,
		parent:   oldID,
		stream:   "ext",
		gate:     "green",
		gatedTip: oldID,
		gatedSHA: nextID,
	})
}

func walkLandEdgeTrace_internal(t *testing.T, seed int64) []landTraceStep_internal {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	h := newLandWalkHarness_internal(t)
	steps := []landTraceStep_internal{refinementLandEdgeState_internal(t, h, "Init", "none")}
	extMoves := 0
	for i := 0; i < 32; i++ {
		st := steps[len(steps)-1]
		if (st.step["s1"] == "landed" || st.step["s1"] == "refused") &&
			(st.step["s2"] == "landed" || st.step["s2"] == "refused") {
			steps = append(steps, refinementLandEdgeState_internal(t, h, "Done", "none"))
			break
		}
		act, arg := pickLandMove_internal(t, rng, h, st, &extMoves)
		steps = append(steps, refinementLandEdgeState_internal(t, h, act, arg))
	}
	return steps
}

func pickLandMove_internal(t *testing.T, rng *rand.Rand, h *landWalkHarness_internal, st landTraceStep_internal, extMoves *int) (string, string) {
	t.Helper()
	type move struct{ act, arg string }
	var moves []move
	baseTip := st.base[len(st.base)-1].sha
	for _, s := range []string{"s1", "s2"} {
		switch st.step[s] {
		case "idle":
			moves = append(moves, move{"Fetch", s})
		case "fetched":
			if st.gatedAt[s].baseTip != baseTip {
				moves = append(moves, move{"Remerge", s})
			} else {
				moves = append(moves, move{"Merge", s})
			}
		case "merged":
			moves = append(moves, move{"PushStream", s})
		case "pushed":
			if st.gatedAt[s].baseTip != baseTip {
				moves = append(moves, move{"Remerge", s})
			} else {
				moves = append(moves, move{"Gate", s})
			}
		case "gating":
			moves = append(moves, move{"CIGreen", s})
			if st.gatedAt[s].faults < 1 {
				moves = append(moves, move{"CIFault", s})
			}
		case "fault":
			moves = append(moves, move{"Gate", s})
		case "green":
			if st.gatedAt[s].baseTip == baseTip {
				moves = append(moves, move{"Land", s})
			} else {
				moves = append(moves, move{"Remerge", s})
			}
		case "red":
			moves = append(moves, move{"Refuse", s})
		}
	}
	if *extMoves < 1 && (st.step["s1"] != "landed" || st.step["s2"] != "landed") {
		moves = append(moves, move{"BaseMoves", "none"})
	}
	m := moves[rng.Intn(len(moves))]
	if m.act == "BaseMoves" {
		*extMoves++
		h.applyBaseMove_internal(t)
		return m.act, m.arg
	}
	h.execStreamAction_internal(t, m.arg, m.act)
	return m.act, m.arg
}

func formatLandEdgeTraceTLA_internal(steps []landTraceStep_internal) string {
	var b strings.Builder
	b.WriteString("-------------------------- MODULE LandEdgeTraceLog --------------------------\n")
	b.WriteString("EXTENDS Naturals, Sequences\nTraceLog == <<\n")
	for i, s := range steps {
		comma := ","
		if i == len(steps)-1 {
			comma = ""
		}
		b.WriteString(fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, base |-> %s, stream |-> [s1 |-> %d, s2 |-> %d], group |-> [s1 |-> <<\"h1\", \"h2\">>, s2 |-> <<\"h3\">>], step |-> [s1 |-> %q, s2 |-> %q], gatedAt |-> [s1 |-> %s, s2 |-> %s], landed |-> %s]%s\n",
			i+1, s.action, s.arg,
			formatBaseSeqTLA_internal(s.base),
			s.stream["s1"], s.stream["s2"],
			s.step["s1"], s.step["s2"],
			formatGatedAtTLA_internal(s.gatedAt["s1"]),
			formatGatedAtTLA_internal(s.gatedAt["s2"]),
			formatStringSeqTLA_internal(s.landed),
			comma))
	}
	b.WriteString(">>\n")
	b.WriteString("=============================================================================\n")
	return b.String()
}

func formatBaseSeqTLA_internal(entries []landBaseEntry_internal) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("[sha |-> %d, parent |-> %d, stream |-> %q, heads |-> %s, gate |-> %q, gatedTip |-> %d, gatedSHA |-> %d]",
			e.sha, e.parent, e.stream, formatStringSetTLA_internal(e.heads), e.gate, e.gatedTip, e.gatedSHA)
	}
	return "<< " + strings.Join(parts, ", ") + " >>"
}

func formatGatedAtTLA_internal(g landGatedAt_internal) string {
	ch := "FALSE"
	if g.chased {
		ch = "TRUE"
	}
	return fmt.Sprintf("[baseTip |-> %d, sha |-> %d, gate |-> %q, blamed |-> %s, chased |-> %s, faults |-> %d]",
		g.baseTip, g.sha, g.gate, formatStringSetTLA_internal(g.blamed), ch, g.faults)
}

func formatStringSetTLA_internal(vals []string) string {
	if len(vals) == 0 {
		return "{}"
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func formatStringSeqTLA_internal(vals []string) string {
	if len(vals) == 0 {
		return "<< >>"
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "<< " + strings.Join(parts, ", ") + " >>"
}

func TestLandEdgeTraceRefinementWalk(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 8; seed++ {
		steps := walkLandEdgeTrace_internal(t, seed)
		last := steps[len(steps)-1]
		if last.action != "Done" {
			t.Fatalf("seed %d: did not reach Done: %+v", seed, last)
		}
		tlaText := formatLandEdgeTraceTLA_internal(steps)
		if traceDir := os.Getenv("NOVA_TRACE_DIR"); traceDir != "" && seed <= 3 {
			sub := filepath.Join(traceDir, fmt.Sprintf("landedge-walk-%d", seed))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "LandEdgeTraceLog.tla"), []byte(tlaText), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		wantSeed := int64(1)
		if raw := os.Getenv("NOVA_TRACE_SEED"); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				wantSeed = parsed
			}
		}
		if seed == wantSeed {
			out := os.Getenv("NOVA_LANDEDGE_TRACE_OUT")
			if out == "" {
				out = os.Getenv("NOVA_TRACE_OUT")
			}
			if out != "" {
				if err := os.WriteFile(out, []byte(tlaText), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
