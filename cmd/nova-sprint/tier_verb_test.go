package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
)

// tier <card> --set <t> --reason <why> (docs/SPEC-SPRINT.md section 11, the tier
// verb): the reason is required, recorded with the pin. A re-tier with no
// --reason, an empty one or one of blanks only is refused, exit 1, and writes
// nothing; with a reason the card is pinned and the record names the reason.
func TestTierRefusesAReTierWithoutAReason(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream a a-1 --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-1", "-", "internal/a1.go"))
	for _, line := range []string{
		"tier a-1 --set heavy",
		"tier a-1 --set heavy --reason ''",
		"tier a-1 --set heavy --reason '   '",
	} {
		code, _, errs := ta.do(line)
		assert.Equal(t, 1, code, line)
		assert.Contains(t, errs, "--reason", line)
	}
	assert.Equal(t, "", ta.primary("a-1").F(sprint.FieldTier), "a refused re-tier writes nothing")

	out := ta.ok("tier a-1 --set heavy --reason 'a heavy card'")
	assert.Contains(t, out, "reason: a heavy card")
	assert.Equal(t, "heavy", ta.primary("a-1").F(sprint.FieldTier))
}
