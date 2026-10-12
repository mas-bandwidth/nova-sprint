package main

import "testing"

// tier <card> --set <t> --reason <why> (docs/SPEC-SPRINT.md section 11, the tier
// verb): the reason is required, recorded with the pin. The rule is shared with
// brief --tier and tabled over the verb in brief_verb_test.go.
func TestTierRefusesAReTierWithoutAReason(t *testing.T) {
	t.Parallel()
	assertReTierNeedsAReason(t, "tier --set")
}
