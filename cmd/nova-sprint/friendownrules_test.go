package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A FRIEND CARD THAT CARRIES ITS OWN RULES LINE IS ACCEPTED AT ADD. A friend is not a
// child: her brief is not held for the child RULES paragraph (docs/SPEC-SPRINT.md
// section 1, a friend's card; the rules-by-reference paragraph: a card that names no
// held file carries its own rules, as every card did before). The rule- checks of
// pkg/swarm/lintchild.go are the child paragraph's presence checks; a friend card that
// carries its own RULES line carries its own rules instead, and is accepted. The step-
// scans still hold it: a line of the card outside its RULES paragraph that contradicts
// its carried rules is a finding under any rule set. A friend card with no RULES line of
// its own is still held to the paragraph, as a child's brief is.
func TestAddAcceptsAFriendCardThatCarriesItsOwnRules(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync --root " + t.TempDir())
	add := func(name, brief string) (int, string) {
		t.Helper()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(brief), 0o644))
		code, _, errs := ta.do("add --stream s1 --brief-dir " + dir)
		return code, errs
	}

	// her own rules, not the child paragraph's six sentences: accepted, where a child's
	// brief with them is refused
	own := "c1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\n\nThe task.\n\nRULES.\nNever start a server on this machine.\n"
	code, errs := add("c1.md", own)
	assert.Equal(t, 0, code, "a friend card with its own RULES line is accepted: %s", errs)

	child := "c4: a child's card\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n\nRULES.\nNever start a server on this machine.\n"
	code, errs = add("c4.md", child)
	assert.Equal(t, 2, code, "a child's brief is a child's whole brief")
	assert.Contains(t, errs, "rule-worktree: 1: missing:", "the child paragraph's presence checks hold it: %s", errs)

	// a friend card with no RULES line of its own is still held to the paragraph
	bare := "c2: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\n\nThe task.\n"
	code, errs = add("c2.md", bare)
	assert.Equal(t, 2, code, "a friend card with no RULES line of its own is still held")
	assert.Contains(t, errs, "rule-worktree: 1: missing:", "%s", errs)

	// the step- scans still hold a friend card that carries its own RULES line
	dirty := "c3: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\n\nThe task starts a redis-server for the test.\n\nRULES.\nNever start a server on this machine.\n"
	code, errs = add("c3.md", dirty)
	assert.Equal(t, 2, code, "a line that contradicts its carried rules still refuses it")
	assert.Contains(t, errs, "step-redis-server: 5:", "%s", errs)
	assert.NotContains(t, errs, "rule-", "no child-paragraph finding is drawn")
}
