package sprintdash

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheFriendsTableIsNotHidden(t *testing.T) {
	t.Parallel()
	index := string(file("index.html"))
	assert.Contains(t, index, `<section class="panel friends">`, "the friends panel is on the page")
	assert.NotContains(t, index, `<section class="panel friends" hidden`, "the friends panel is not hidden by attribute")
	assert.NotContains(t, index, "hide-friends", "no hide-friends rule remains on the page")
	assert.NotRegexp(t, `\.panel\.friends\s*\{\s*display:\s*none;?\s*\}`, index, "no CSS rule sets .panel.friends to display: none")
}
