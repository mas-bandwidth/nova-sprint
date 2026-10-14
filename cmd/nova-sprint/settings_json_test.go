package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSettingsJSONIsOneObject is the unit proof that settings --json prints
// one JSON object whose settings array holds the same names as the text form.
func TestSettingsJSONIsOneObject(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1:2")

	textOut := ta.ok("settings")
	jsonOut := ta.ok("settings --json")

	// All JSON output decodes as exactly one value.
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &got), "settings --json stdout decodes as one JSON value")

	// The value is an object carrying the expected top-level fields.
	require.Equal(t, true, got["ok"], "top-level ok field is true")
	require.Equal(t, "settings", got["verb"], "top-level verb field is settings")
	settings, ok := got["settings"].([]any)
	require.True(t, ok, "top-level settings field is an array")

	// Collect names from the text form.
	var textNames []string
	for _, l := range strings.Split(textOut, "\n") {
		if !strings.HasPrefix(l, "SETTING ") {
			continue
		}
		name, _, found := strings.Cut(strings.TrimPrefix(l, "SETTING "), " ")
		require.True(t, found, "text line has a name: %s", l)
		textNames = append(textNames, name)
	}

	// Collect names from the JSON array.
	var jsonNames []string
	var byFlag []struct {
		Flag    string `json:"flag"`
		Prop    string `json:"prop"`
		Value   string `json:"value"`
		Default string `json:"default"`
		Set     bool   `json:"set"`
		Accessor string `json:"accessor"`
	}
	b, err := json.Marshal(settings)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &byFlag))
	for _, s := range byFlag {
		jsonNames = append(jsonNames, s.Flag)
	}

	// and the count of set items matches the text form.
	setCount, ok := got["set"].(float64)
	require.True(t, ok, "top-level set field is a number")
	assert.Equal(t, float64(strings.Count(textOut, " set=yes")), setCount, "top-level set count matches the text form")
	sort.Strings(textNames)
	sort.Strings(jsonNames)
	assert.Equal(t, textNames, jsonNames, "settings --json array has the same flag names as the text form")
}
