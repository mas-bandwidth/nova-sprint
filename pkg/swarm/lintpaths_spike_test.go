package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spikeFindings is the spike-paths-research findings LintBrief draws on raw, with no tree
// handed over (the rule reads the header alone).
func spikeFindings(t *testing.T, raw string) []CardHeaderFinding {
	t.Helper()
	fs, _ := LintBrief([]byte(raw), BriefBase{})
	var out []CardHeaderFinding
	for _, f := range fs {
		if f.Check == "spike-paths-research" {
			out = append(out, f)
		}
	}
	return out
}

// The spike-paths-research rule (Glenn's tracer-bullet rule: a tracer is never the product):
// a KIND: spike or tracer brief names in PATHS and NEW only paths under the research roots.
// Replayed from sprint 21: the two schema-tracer briefs, which landed or nearly landed
// production code, are refused naming each path outside the roots, with the remedy naming
// the experiment.patch delivery; the fixed3d spike-wide-joint-solve brief, whose code
// travels as perf/experiments/<id>/experiment.patch, passes.
func TestASpikeBriefNamesOnlyResearchPaths(t *testing.T) {
	t.Parallel()

	// schema-tracer-done-record-fixed, its header as the store holds it.
	doneRecord := "RESULT: schema-tracer-done-record-fixed sha=<sha12> tier: heavy\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"BASE: v1.3/comms\n" +
		"KIND: spike\n" +
		"PATHS: internal/sprint/store/done.go, internal/sprint/store/done_test.go\n" +
		"NEW: schemas/store/done.schema, internal/sprint/store/donewire/, internal/sprint/store/done_schema_test.go, docs/research/schema-tracer/done-record.md\n" +
		"TEST: internal/sprint/store TestDoneRecordSchema\n\n" +
		"THE TASK. a tracer of the done record's fixed schema.\n"
	fs := spikeFindings(t, doneRecord)
	require.Len(t, fs, 1, "the done-record tracer is refused")
	assert.Equal(t, 5, fs[0].Line, "the finding points at PATHS, the first line outside the roots")
	for _, p := range []string{"internal/sprint/store/done.go", "internal/sprint/store/done_test.go", "schemas/store/done.schema", "internal/sprint/store/donewire/", "internal/sprint/store/done_schema_test.go"} {
		assert.Contains(t, fs[0].Excerpt, p)
	}
	assert.NotContains(t, fs[0].Excerpt, "docs/research/schema-tracer/done-record.md", "a path under docs/research/ is the spike's own")

	// its sibling tracer on nova-tools.
	pongWire := "RESULT: schema-tracer-pong-fixed sha=<sha12> tier: heavy\n" +
		"REPO: mas-bandwidth/nova-tools\n" +
		"BASE: dev\n" +
		"KIND: spike\n" +
		"PATHS: pkg/friend/machine.go\n" +
		"NEW: pkg/friend/pongwire/\n\n" +
		"THE TASK. a tracer of the pong's fixed schema.\n"
	fs = spikeFindings(t, pongWire)
	require.Len(t, fs, 1, "the pong tracer is refused")
	assert.Contains(t, fs[0].Excerpt, "pkg/friend/machine.go")
	assert.Contains(t, fs[0].Excerpt, "pkg/friend/pongwire/")

	// spike-wide-joint-solve: research paths only, its code a patch file.
	wideJoint := "RESULT: spike-wide-joint-solve sha=1df2e22f8899 tier: heavy\n" +
		"REPO: mas-bandwidth/fixed3d\n" +
		"BASE: sprint/fixed3d-box3d-ports\n" +
		"KIND: spike\n" +
		"START: src/joint.c (read), src/solver.c (read)\n" +
		"NEW: perf/findings/spike-wide-joint-solve.md, perf/experiments/spike-wide-joint-solve/experiment.patch, perf/experiments/spike-wide-joint-solve/raw.txt\n" +
		"TEST: none because the suites are C and not a Go package\n\n" +
		"THE TASK. wide joint arithmetic.\n"
	assert.Empty(t, spikeFindings(t, wideJoint), "the fixed3d spike names only research paths")
	assert.Empty(t, spikeFindings(t, "card x tier: heavy\nKIND: spike\nPATHS: perf/findings/x.md\nNEW: rnd/x/, benchmark/experiments/x/experiment.patch, benchmark/findings/x.md, docs/research/x.md\n\nTHE TASK. x\n"),
		"every research root passes, on PATHS and on NEW")

	// KIND: tracer is held too, in any case; another KIND is not this rule's.
	fs = spikeFindings(t, "card x tier: heavy\nKIND: Tracer\nPATHS: perf/x.md\nNEW: cmd/x/main.go\n\nTHE TASK. x\n")
	require.Len(t, fs, 1)
	assert.Equal(t, 4, fs[0].Line, "PATHS is clean, so the finding points at NEW")
	assert.Empty(t, spikeFindings(t, "card x tier: pro\nKIND: fix\nPATHS: cmd/x/main.go\n\nTHE TASK. x\n"))
	assert.Empty(t, spikeFindings(t, "card x tier: pro\nPATHS: cmd/x/main.go\n\nTHE TASK. x\n"), "no KIND is no spike")

	// a near miss of a root is no root.
	require.Len(t, spikeFindings(t, "card x tier: heavy\nKIND: spike\nNEW: perfx/a.md, docs/researcher.md\n\nTHE TASK. x\n"), 1)

	remedy := BriefRemedy("spike-paths-research")
	assert.Contains(t, remedy, "experiment.patch", "the remedy names the patch delivery")
	assert.Equal(t, BriefBaseRemedies["spike-paths-research"], remedy)
}
