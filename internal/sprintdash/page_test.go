package sprintdash

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTheCoinGlyphIsTheOnlyAddition pins the speaker glyph and the inline synthesis
// (docs/SPEC-SPRINT-DASHBOARD.md, "The coin"): the page carries one small speaker glyph beside
// the landed count, off until clicked, and the coin is synthesized in the app.js it already
// fetches — no audio file, no separate script, no network request the page did not already make.
func TestTheCoinGlyphIsTheOnlyAddition(t *testing.T) {
	t.Parallel()
	index := string(file("index.html"))
	app := string(file("app.js"))
	assert.Contains(t, index, `id="coin"`, "one speaker glyph beside the landed count")
	assert.NotContains(t, index, "sound.js", "the sound rides the already-fetched app.js: no extra request")
	assert.NotContains(t, index, "audio/", "no audio file joins the repository")
	for _, s := range []string{"AudioContext", "createOscillator", "createGain", "setValueAtTime(0.15", "0.25", "coinSnapshot"} {
		assert.True(t, strings.Contains(app, s), "the coin is synthesized in app.js (%q)", s)
	}
	served := get(t, newRig(t).s, "/").Body.String()
	assert.Contains(t, served, `id="coin"`, "the served page carries the glyph")
}
