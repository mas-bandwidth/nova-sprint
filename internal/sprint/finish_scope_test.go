package sprint_test

import (
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

// JSON keeps this test runnable on the unchanged base, which ignores Scope.
func scopeFinish(t *testing.T, wc *sprint.Card, paths ...string) sprint.FinishReq {
	t.Helper()
	diff := ""
	for _, p := range paths {
		diff += fmt.Sprintf("diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -1 +1 @@\n-old\n+new\n", p, p, p, p)
	}
	evidence, err := json.Marshal(map[string]any{"base": "1111111111111111111111111111111111111111", "head": readHead, "diff": diff, "card": wc.ID, "gen": wc.Int("gen"), "epoch": 0, "tracked": []string{"delta_encoding.h", "src/a.c", "include/box3d/base.h", "internal/data/check/check.go"}})
	require.NoError(t, err)
	wire, err := json.Marshal(map[string]any{"Scope": string(evidence)})
	require.NoError(t, err)
	var req sprint.FinishReq
	require.NoError(t, json.Unmarshal(wire, &req))
	req.Sel = sprint.Sel{IDs: []string{wc.ID}}
	req.Gens = map[string]int{wc.ID: wc.Int("gen")}
	req.Head = readHead
	return req
}

func TestAFinishOutsidePathsIsJudgedBeforeReads(t *testing.T) {
	for _, tc := range []struct {
		name, paths, file string
		outside           bool
	}{
		{"delta-quaternions-2", "delta_encoding.h, delta_quaternion.cpp, delta_quaternion.h", "Makefile", true},
		{"fixed3d-perf-harness-7", "src/a.c", "test/main.c", true},
		{"port-9e5a4cd-1", "src/a.c", "test/test_name_cache.c", true},
		{"fixed3d-reference-1", "src/a.c", "test/main.c", true},
		{"delta-baseline-3", "delta_encoding.h", "Makefile", true},
		{"delta-build-2", "delta_encoding.h", "test/delta_codec_test.cpp", true},
		{"delta-history-5", "delta_encoding.h", "Makefile", true},
		{"port-fixes-8", "src/a.c", "include/box3d/base.h", true},
		{"port-fixes-8-collision", "src/a.c", "include/box3d/collision.h", true},
		{"port-constraint-12", "src/a.c", "src/constraint_graph.c", true},
		{"replicant-angular-3", "src/a.c", "internal/data/check/check.go", true},
		{"replicant-codegen-7", "src/a.c", "internal/data/codegen/cpp/cpp.go", true},
		{"inside", "src/a.c", "src/a.c", false},
		{"always-test", "src/a.c", "internal/other/a_test.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newConflictRig(t)
			r.must(store.AddStep(sprint.AddReq{Stream: "w", IDs: []string{"w-1"}, Brief: "tier: flash\nPATHS: " + tc.paths + "\n"}))
			wc := r.taken("w-1")
			r.must(store.FinishStep(scopeFinish(t, wc, tc.file)))
			s := r.snap()
			pr := s.Work.Card("w-1")
			require.Equal(t, sprint.Review, pr.Col)
			if tc.outside {
				assert.Equal(t, "paths", pr.F(sprint.FieldRuleRefused), "E12 must be recorded by finish, before ask")
				assert.Equal(t, pr.F("attempt"), pr.F(sprint.FieldRuleRedo))
				assert.Contains(t, pr.F(sprint.FieldRuleRefusal), tc.file)
				assert.Zero(t, sprint.ReadsWanted(s, pr), "no read may be asked of the refused head")
				got, err := r.st.Run(r.ctx, store.AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"w-1"}}}))
				require.NoError(t, err)
				assert.Empty(t, got.Moved)
				assert.Empty(t, r.snap().Readers.Of("w-1"))
			} else {
				assert.Empty(t, pr.F(sprint.FieldRuleRefused))
				assert.Positive(t, sprint.ReadsWanted(s, pr))
			}
		})
	}
}

func TestFinishScopeRejectsWrongBindingAndKeepsLegacy(t *testing.T) {
	for _, field := range []string{"card", "gen", "epoch", "head", "base", "tracked", "malformed", "bad-diff", "bad-hunk", "legacy"} {
		t.Run(field, func(t *testing.T) {
			r := newConflictRig(t)
			r.must(store.AddStep(sprint.AddReq{Stream: "w", IDs: []string{"w-1"}, Brief: "tier: flash\nPATHS: src/a.c\n"}))
			wc := r.taken("w-1")
			req := scopeFinish(t, wc, "Makefile")
			var e map[string]any
			require.NoError(t, json.Unmarshal([]byte(req.Scope), &e))
			switch field {
			case "card", "head", "base":
				e[field] = "wrong"
			case "gen", "epoch":
				e[field] = 999
			case "tracked":
				e[field] = nil
			case "bad-diff":
				e["diff"] = "garbage"
			case "bad-hunk":
				e["diff"] = "diff --git a/a b/a\n@@ +\n"
			}
			b, err := json.Marshal(e)
			require.NoError(t, err)
			req.Scope = string(b)
			if field == "malformed" {
				req.Scope = "{"
			}
			if field == "legacy" {
				req.Scope = ""
			}
			res, err := r.st.Run(r.ctx, store.FinishStep(req))
			require.NoError(t, err)
			if field == "legacy" {
				require.NotEmpty(t, res.Moved)
				require.Equal(t, sprint.Review, r.snap().Work.Card("w-1").Col)
			} else {
				require.NotEmpty(t, res.Refused)
				require.Equal(t, sprint.Working, r.snap().Work.Card("w-1").Col)
			}
		})
	}
}

func TestFinishScopeFeedsTheExistingConflictRule(t *testing.T) {
	r := newConflictRig(t)
	r.must(store.AddStep(sprint.AddReq{Stream: "w", IDs: []string{"w-1"}, Brief: "tier: flash\nPATHS: src/a.c\n"}))
	wc := r.taken("w-1")
	r.must(store.FinishStep(scopeFinish(t, wc, "unrelated/other.c")))
	for i := 0; i < 3 && r.snap().Work.Card("w-1").Int("attempt") == 1; i++ {
		r.tick()
	}
	pr := r.snap().Work.Card("w-1")
	require.NotEqual(t, sprint.Review, pr.Col, "existing conflict rule must advance the refused attempt")
	require.Empty(t, r.snap().Readers.Of("w-1"), "no reader spent on the refused head")
}

func TestZeroReadSettingCannotAcceptAnE12Finish(t *testing.T) {
	r := newConflictRig(t)
	r.must(store.AddStep(sprint.AddReq{Stream: "w", IDs: []string{"w-1"}, Brief: "tier: flash\nPATHS: src/a.c\n"}))
	wc := r.taken("w-1")
	r.must(store.FinishStep(scopeFinish(t, wc, "unrelated/other.c")))
	s := r.snap()
	s.Work.SetProp(sprint.PropReadsNeeded, "0")
	p, n := sprint.TickAccept(s, sprint.TickReq{})
	require.Empty(t, p.Units)
	require.Zero(t, n)
}

func TestFinishScopeWritesTheLandersE12Record(t *testing.T) {
	finished := newConflictRig(t)
	finished.must(store.AddStep(sprint.AddReq{Stream: "w", IDs: []string{"w-1"}, Brief: "tier: flash\nPATHS: src/a.c\n"}))
	wc := finished.taken("w-1")
	finished.must(store.FinishStep(scopeFinish(t, wc, "Makefile")))
	want := finished.snap().Work.Card("w-1")
	landed := newConflictRig(t)
	landed.must(store.AddStep(sprint.AddReq{Stream: "w", IDs: []string{"w-1"}, Brief: "tier: flash\nPATHS: src/a.c\n"}))
	landed.toMergingAt("w-1", readHead)
	landed.must(store.MergeStep(sprint.MergeReq{Stream: "w", Batch: 1, Conflict: "w-1", Note: want.F(sprint.FieldRuleRefusal)}))
	got := landed.snap().Work.Card("w-1")
	for _, field := range []string{sprint.FieldRuleRedo, sprint.FieldRuleRefused, sprint.FieldRuleRefusal, sprint.FieldFindings, sprint.FieldReturnedAttempt, "returns", "return_reason"} {
		require.Equal(t, got.F(field), want.F(field), field)
	}
}
