package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-tools/pkg/swarm"
)

// The card lint's child scans read no text inside a fenced code block: a fenced block is a
// program or an example the card shows, not a command it runs. A card that documents its
// make-driven gate — a Makefile target whose body runs `go test` and `rm -rf` — is
// admitted. A walk above the job (`cd ../scratch`) is still a parent-path finding inside a
// fence: the wall refuses it wherever it is written (nova-tools pkg/swarm
// TestAWalkInsideAFenceIsStillAFinding, settled in nova-tools af9a13348). The same lines
// outside a fence are read by every rule, so the rules keep their teeth.
//
// This pins the child-scan half of swarm-lint-card-false-refusals.
func TestCardLintReadsNoTextInsideAFencedBlock(t *testing.T) {
	t.Parallel()
	rules := append(append([]swarm.ChildRule{}, swarm.DefaultChildRules...),
		swarm.ChildRule{Name: "go-test-timeout", Sentence: "Every `go test` gets `-timeout 600s`."})

	fenced := "STEP 3. The Makefile target's body is:\n" +
		"```\n" +
		"gate:\n" +
		"\tgo test ./internal/x\n" +
		"\trm -rf /tmp/cache\n" +
		"\tcd ../scratch && git push --force origin HEAD\n" +
		"```\n" +
		"STEP 4. Run the gate: make gate\n"
	for _, f := range swarm.LintCardChildWith([]byte(fenced), rules) {
		assert.Falsef(t, strings.HasPrefix(f.Check, "step-"),
			"the fenced block drew %s on line %d: %s", f.Check, f.Line, f.Excerpt)
	}
	assert.Equal(t, []int{6}, swarm.CardParentPaths(strings.Split(fenced, "\n")),
		"a walk inside a fenced block is still a walk")

	unfenced := "STEP 3. Run go test ./internal/x\n" +
		"STEP 4. Run the gate, then rm -rf /tmp/cache\n" +
		"STEP 5. cd ../scratch && git push --force origin HEAD\n"
	checks := map[string]bool{}
	for _, f := range swarm.LintCardChildWith([]byte(unfenced), rules) {
		checks[f.Check] = true
	}
	assert.True(t, checks["step-go-test-timeout"], "an unfenced go test with no -timeout is still read")
	assert.True(t, checks["step-rm-rf"], "an unfenced rm -rf of an absolute path is still read")
	assert.True(t, checks["step-force-push"], "an unfenced force-push is still read")
	assert.NotEmpty(t, swarm.CardParentPaths(strings.Split(unfenced, "\n")),
		"an unfenced parent path is still a walk")
}
