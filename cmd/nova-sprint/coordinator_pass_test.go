package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A friend's beat carries her session's answer to a check her daemon asked (friend beat
// --check, then --pong naming it), the server takes them as a beat's flags, and the tick's
// coordinator's pass judges her session deaf from the last proof: the judgment is in the
// coordinator's inbox (docs/SPEC-SPRINT.md section 8, "The coordinator's pass").
func TestSetFriendFinishIsTheIdleWindow(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "friend amy", "amy")
	out := ta.ok("set --friend-finish 45m")
	assert.Contains(t, out, "friend-finish 45m")
	code, _, errs := ta.do("set --friend-finish soon")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "--friend-finish wants a duration")
	assert.Contains(t, ta.ok("set --friend-finish default"), "friend-finish default (30m0s)")
}
