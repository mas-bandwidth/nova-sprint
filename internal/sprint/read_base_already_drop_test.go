package sprint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// 2026-10-17: a read that finds the card's work already at the base (its test
// passes unchanged there) was reworked by rule, and the rework cannot succeed:
// the schema-go timestamp any-length card (dropped) spent four attempts after its duplicate
// landed. The rule leaves such a finding to the coordinator as a drop, never a
// rework.
func TestABrokenReadThatFindsTheWorkAtTheBaseIsADropNotARework(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	r.readCard()
	r.brokenRead("r-1", "internal/x/a.go:1 nothing to do: the base already has this card's change (TestX passes unchanged at the base); drop the card")
	var answer *sprint.RuleAnswer
	for _, a := range sprint.RuleAnswers(r.snap(), sprint.TickReq{}) {
		if a.Type == sprint.NReadBroken && a.Subject == "r-1" {
			answer = &a
		}
	}
	require.NotNil(t, answer, "the read-broken judgment is open")
	assert.Equal(t, sprint.ActLeft, answer.Act, "never reworked by rule: %s", answer.Why)
	assert.True(t, strings.Contains(answer.Why, "drop"), "the reason names the drop: %s", answer.Why)
	r.tick()
	assert.Equal(t, 1, r.snap().Work.Card("r-1").Int("attempt"), "no second attempt")
	assert.Len(t, r.openOnCard(sprint.NReadBroken, "r-1"), 1, "the judgment stays, the coordinator's")
}
