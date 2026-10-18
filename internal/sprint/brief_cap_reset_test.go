package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 2026-10-18: a cosmetic brief edit (the tier on line 1, a routing line) printed
// "max attempts: decided at attempt N (the cap's count starts again)" and gave the card
// a fresh cap window, so a card the cap should have stopped ran on. The cap counts
// attempts since the coordinator last decided something about the task; an edit that
// leaves the task and its PATHS as they were decides nothing (taskOf hashes the body
// and the PATHS, NEW, SHARED, TEST and DONE-WHEN lines).
func TestACosmeticBriefEditKeepsTheAttemptCapCount(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		brief   string
		decided string // the card's FieldDecidedAttempt after the edit
		said    bool   // the edit says the cap's count starts again
	}{
		{"TheTierOnLineOneAlone", "tier: heavy\nPATHS: internal/x.go\n\nthe task", "1", false},
		{"ARoutingLineAlone", "tier: pro\nPATHS: internal/x.go\nPRIORITY: high\n\nthe task", "1", false},
		{"SpacingOnly", "tier: pro\nPATHS: internal/x.go\n\nthe  task", "1", false},
		{"ANewTask", "tier: pro\nPATHS: internal/x.go\n\na different task", "2", true},
		{"AChangedPaths", "tier: pro\nPATHS: internal/y.go\n\nthe task", "2", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := boundWorld(t) // a-1 in review at attempt 2, its brief last changed at attempt 1
			w.s.Work.Placed("a-1").Fields[FieldDecidedAttempt] = "1"
			p := w.must(Brief(w.s, briefOf("a-1", c.brief, "")))
			assert.Equal(t, c.decided, w.s.Work.Placed("a-1").F(FieldDecidedAttempt), "the cap window after the edit")
			assert.Equal(t, c.said, strings.Contains(strings.Join(p.Said, "\n"), "the cap's count starts again"), "%v", p.Said)
			if !c.said {
				assert.Contains(t, strings.Join(p.Said, "\n"), "max attempts: unchanged (the edit decides nothing about the task)", "%v", p.Said)
			}
		})
	}
}

// Whitespace inside quoted task literals and test selectors is meaningful; only
// outer formatting whitespace is cosmetic.
func TestABriefQuotedWhitespaceChangesTheTask(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, next string
	}{
		{
			name: "QuotedTestSelector",
			old:  "tier: pro\nPATHS: internal/x.go\nDONE-WHEN: go test -run '^TestA B$'\n\nthe task",
			next: "tier: pro\nPATHS: internal/x.go\nDONE-WHEN: go test -run '^TestA  B$'\n\nthe task",
		},
		{
			name: "QuotedBodyLiteral",
			old:  "tier: pro\nPATHS: internal/x.go\n\nthe task handles literal \"foo bar\"",
			next: "tier: pro\nPATHS: internal/x.go\n\nthe task handles literal \"foo  bar\"",
		},
		{
			name: "FencedCode",
			old:  "tier: pro\nPATHS: internal/x.go\n\n\x60\x60\x60go\nx  := 1\n\x60\x60\x60",
			next: "tier: pro\nPATHS: internal/x.go\n\n\x60\x60\x60go\nx := 1\n\x60\x60\x60",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := boundWorld(t)
			w.s.Work.Placed("a-1").Fields["brief"] = tc.old
			w.s.Work.Placed("a-1").Fields[FieldDecidedAttempt] = "1"
			p := w.must(Brief(w.s, briefOf("a-1", tc.next, "")))
			assert.Equal(t, "2", w.s.Work.Placed("a-1").F(FieldDecidedAttempt), "meaningful quoted whitespace changes the task")
			assert.Contains(t, strings.Join(p.Said, "\n"), "max attempts: decided at attempt 2")
		})
	}
}

// A scope-only widening stays on its dedicated in-place path; that path does not make a
// coordinator decision for the attempt cap even though it adds PATHS.
func TestABriefScopeWidenKeepsAttemptCapCount(t *testing.T) {
	t.Parallel()
	w := boundWorld(t)
	w.s.Work.Placed("a-1").Fields[FieldDecidedAttempt] = "1"
	p := w.must(Brief(w.s, briefOf("a-1", "tier: pro\nPATHS: internal/x.go,internal/y.go\n\nthe task", "")))
	assert.Equal(t, "1", w.s.Work.Placed("a-1").F(FieldDecidedAttempt), "scope widening does not reset the cap window")
	assert.NotContains(t, strings.Join(p.Said, "\n"), "max attempts: decided at attempt", "%v", p.Said)
}
