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

// TestTheFriendsTableIsNotHidden holds the friends panel visible: the owner hid the friends
// table for the v1.2.* series with one CSS rule (marked /* hide-friends-v1.2 */) and now asks
// for it back in v1.2.14 (the owner, 2026-10-12 ~12:30Z: "Please show the friends table in the
// sprint dashboard again"). The rule is gone, so the panel renders as it did before 2026-10-11;
// this test guards it: the friends panel is present, not hidden by attribute, and no hide-friends
// rule remains on the page.
func TestTheFriendsTableIsNotHidden(t *testing.T) {
	t.Parallel()
	index := string(file("index.html"))
	assert.Contains(t, index, `<section class="panel friends">`, "the friends panel is on the page")
	assert.NotContains(t, index, `<section class="panel friends" hidden`, "the friends panel is not hidden by attribute")
	assert.NotContains(t, index, "hide-friends", "no hide-friends rule remains on the page")
	assert.NotRegexp(t, `\.panel\.friends\s*\{\s*display:\s*none;?\s*\}`, index, "no CSS rule sets .panel.friends to display: none")
}
