package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// rules lists every check of the work lint, one LINT line per token in the order the lint
// runs them, its remedy in the words a finding carries, before the RULES line; under
// --json the object's lint field (docs/SPEC-SPRINT.md section 6, the work lint).
func TestRulesListsEveryWorkLintCheck(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1,m2 --readers reader-a,reader-b,reader-c")
	out := ta.ok("rules")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.NotEmpty(t, lines)
	assert.True(t, strings.HasPrefix(lines[len(lines)-1], "RULES OK "), "the RULES line stays last: %q", lines[len(lines)-1])
	var lint []string
	for _, l := range lines {
		if strings.HasPrefix(l, "LINT ") {
			lint = append(lint, l)
		}
	}
	require.Len(t, lint, len(sprint.WorkLintRules), "one LINT line per check")
	for i, r := range sprint.WorkLintRules {
		assert.Equal(t, "LINT "+r.Token+": "+r.Refuses+" (remedy: "+r.Remedy+")", lint[i])
		f := sprint.LintFinding{Token: r.Token, What: "x"}
		assert.True(t, strings.HasSuffix(f.String(), "(remedy: "+r.Remedy+")"), "the listing's remedy is the finding's: %s", r.Token)
	}

	var v struct {
		Lint []sprint.WorkLintRule `json:"lint"`
		By   map[string]int        `json:"by"`
	}
	require.NoError(t, json.Unmarshal([]byte(ta.ok("rules --json")), &v))
	assert.Equal(t, sprint.WorkLintRules, v.Lint)
	assert.NotNil(t, v.By, "the answers' object keeps its fields")
}
