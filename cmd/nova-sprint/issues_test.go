package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIssuesGroupIsRegistered: the issues group is in the verb table as three
// verbs whose first word is "issues", so the banner, help and the bare group's
// refusal all name them.
func TestIssuesGroupIsRegistered(t *testing.T) {
	t.Parallel()
	var names []string
	for _, v := range verbs {
		if v.name == "issues import" || v.name == "issues verify" || v.name == "issues roadmap" {
			names = append(names, v.name)
		}
	}
	require.ElementsMatch(t, []string{"issues import", "issues verify", "issues roadmap"}, names)
	assert.ElementsMatch(t, []string{"issues import", "issues verify", "issues roadmap"}, groupVerbs("issues"))
	assert.Contains(t, verbNames(), "issues")
}
