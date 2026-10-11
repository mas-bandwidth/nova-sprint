package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestStreamSetDisplayCLI pins stream set --display (docs/SPEC-SPRINT.md section 11): the
// streams view and the where --json streams (StreamClocks) show the display name for the
// stream, default clears it and the shown name falls back, a release with no display shows
// point-fix-<release>, a stream with neither shows its key, a name over 60 bytes or with a
// newline is refused and nothing is written, and the stream's key is unchanged everywhere.
func TestStreamSetDisplayCLI(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1 --coordinator lead")
	ta.ok("add --stream v1210 --count 2 --actor lead")
	ta.ok("add --stream v1211 --count 2 --actor lead")

	out := ta.ok("stream set v1210 --display 'bus lock' --actor lead")
	assert.Contains(t, out, "MOVED stream v1210 display bus lock")

	// the streams view shows the shown name beside the release; the row is still keyed
	out = ta.ok("streams")
	assert.Contains(t, out, "STREAM v1210 ")
	assert.Contains(t, out, "display=bus lock")
	assert.Contains(t, out, "STREAM v1211 ")
	assert.Contains(t, out, "display=v1211")

	// the where --json streams (StreamClocks) carry the shown name
	var w whereView
	ta.json("where", &w)
	byName := map[string]sprint.StreamClock{}
	for _, c := range w.Streams {
		byName[c.Stream] = c
	}
	assert.Equal(t, "bus lock", byName["v1210"].Display)
	assert.Equal(t, "v1211", byName["v1211"].Display, "a stream with neither a display nor a release shows its key")

	// a release with no display shows point-fix-<release>
	out = ta.ok("stream set v1211 --release v1.2.12 --actor lead")
	assert.Contains(t, out, "MOVED stream v1211 release v1.2.12")
	ta.json("where", &w)
	for _, c := range w.Streams {
		if c.Stream == "v1211" {
			assert.Equal(t, "point-fix-v1.2.12", c.Display)
			assert.Equal(t, "v1211", c.Stream, "the key is kept beside the shown name")
		}
	}

	// default clears the display and the shown name falls back
	out = ta.ok("stream set v1210 --display default --actor lead")
	assert.Contains(t, out, "MOVED stream v1210 display none")
	out = ta.ok("streams")
	assert.Contains(t, out, "STREAM v1210 ")
	assert.Contains(t, out, "display=v1210")

	// a name over 60 bytes, or with a newline, is refused naming the bound, and nothing
	// is written
	before := ta.ok("streams")
	for _, bad := range []string{strings.Repeat("x", 61), "bus\nlock"} {
		code, _, errs := ta.do("stream set v1210 --display '" + bad + "' --actor lead")
		assert.NotEqual(t, 0, code, "%q is refused", bad)
		assert.Contains(t, errs, "60 bytes", "the refusal names the bound")
	}
	assert.Equal(t, before, ta.ok("streams"), "the refused sets wrote nothing")
	assert.NotContains(t, ta.ok("streams"), "display=xxxxx")
}
