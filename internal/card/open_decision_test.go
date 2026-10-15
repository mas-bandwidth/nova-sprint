package card

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sprint 21's not-done briefs left an owner decision to the worker: go-table-writers-
// refuse-what-their-readers-refuse ("decision owed: should a TABLE save refuse (A) or
// stay unchecked (B)"), replicant-emitted-code-compiles-against-delta ("owner wire
// decision required - pick (1) or (2) or (3)"), unsigned-range-implies-min-zero,
// layout-newer-only-where-the-lineage-proves-it and port-79fc559-restitution-stage-and-
// iterations-api. The open-decision check refuses such a brief at add unless it carries
// an OWNER RULING: line or its KIND is decision or research; a brief that reports a
// decision already made passes.
func TestATaskWithAnOpenDecisionIsRefusedAtAdd(t *testing.T) {
	t.Parallel()
	const refuse = "RESULT: %s sha=abc tier: pro\nREPO: o/r\nPATHS: internal/x/*.go\nTEST: ./internal/x TestY\n\nTHE TASK. %s\n"

	cases := []struct {
		name string
		id   string
		task string
	}{
		{
			name: "go-table-writers-refuse-what-their-readers-refuse",
			id:   "go-table-writers-refuse-what-their-readers-refuse.w1",
			task: "decision owed: should a TABLE save refuse (A), stay unchecked (B)?",
		},
		{
			name: "replicant-emitted-code-compiles-against-delta",
			id:   "replicant-emitted-code-compiles-against-delta.w1",
			task: "owner wire decision required - pick (1), (2), or (3).",
		},
		{
			name: "unsigned-range-implies-min-zero",
			id:   "unsigned-range-implies-min-zero.w1",
			task: "this is an OPEN QUESTION: does unsigned range imply min zero?",
		},
		{
			name: "port-79fc559-restitution-stage-and-iterations-api",
			id:   "port-79fc559-restitution-stage-and-iterations-api.w8",
			task: "parked awaiting the collision-policy decision.",
		},
		{
			name: "replicant-angular-velocity-decision-and-a-refusal-until-it-is-made",
			id:   "replicant-angular-velocity-decision-and-a-refusal-until-it-is-made.w1",
			task: "the owner wire decision remains UNSETTLED.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			brief := fmt.Sprintf(refuse, tc.id, tc.task)
			checks := checkNames(tc.id, brief)
			require.Contains(t, checks, OpenDecisionCheck)
			assert.Len(t, checks, 1, "only the open-decision finding")

			withRuling := brief + "OWNER RULING: TABLE saves refuse.\n"
			assert.Empty(t, Checks(tc.id, withRuling, Options{}), "an OWNER RULING: line settles the decision")

			withKind := brief + "KIND: decision\n"
			assert.Empty(t, Checks(tc.id, withKind, Options{}), "KIND: decision is a decision card")
		})
	}
}

// The word list is short and literal: a brief that reports a decision already made, or
// that uses the word "decision" without leaving one open, passes.
func TestABriefThatReportsADecisionAlreadyMadePasses(t *testing.T) {
	t.Parallel()
	clean := "RESULT: c sha=abc tier: pro\nREPO: o/r\nPATHS: internal/x/*.go\nTEST: ./internal/x TestY\n\nTHE TASK. The owner decided: TABLE saves refuse.\n"
	assert.Empty(t, Checks("c", clean, Options{}))

	clean = strings.Replace(clean, "The owner decided: TABLE saves refuse.", "the decision was already made in the review; do the work.", 1)
	assert.Empty(t, Checks("c", clean, Options{}), "the bare word 'decision' is no open decision")

	clean = strings.Replace(clean, "the decision was already made in the review; do the work.", "(1) add the check, (2) add the test, (3) run the gate.", 1)
	assert.Empty(t, Checks("c", clean, Options{}), "a numbered list of steps is no open decision")
}

func checkNames(id, brief string) []string {
	var out []string
	for _, f := range Checks(id, brief, Options{}) {
		out = append(out, f.Check)
	}
	return out
}
