package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryVerbTakesJSON holds every result verb of nova-card to the standard
// (docs/STANDARD.md section 3, "Every verb takes --json"): generate, lint and
// version each print the same result as one JSON object on stdout when --json
// is given, and each prints its text form unchanged when it is not. template
// prints the card template and help prints usage; the two are text, not a
// result, and what their --json does is the refusal the sibling card carries.
func TestEveryVerbTakesJSON(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("ab", 20)
	from := []string{"generate", "--from", "findings", "--file", "testdata/findings.tsv", "--repo", "o/r", "--base", "dev", "--sha", sha}
	dir := t.TempDir()

	// generate: the text form is the manifest and the CARDS OK line.
	dry := filepath.Join(dir, "dry")
	code, stdout, stderr := runCard(append(append([]string{}, from...), "--out", dry, "--dry-run")...)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "id\tfile\ttest\twave\tdeps\n")
	assert.Contains(t, stdout, "cards=2 waves=1 tier=pro dry-run=yes")
	assert.NotContains(t, stdout, "{", "the text form is not JSON")

	// generate --json: one object carrying the same facts, nothing written.
	code, stdout, stderr = runCard(append(append([]string{}, from...), "--out", dry, "--dry-run", "--json")...)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Empty(t, stderr)
	obj := oneJSONObject(t, stdout)
	assert.Equal(t, dry, obj["dir"])
	assert.Equal(t, float64(2), obj["cards"])
	assert.Equal(t, float64(1), obj["waves"])
	assert.Equal(t, "pro", obj["tier"])
	assert.Equal(t, true, obj["dry_run"])
	require.IsType(t, []any{}, obj["manifest"], "the manifest is a JSON array")
	assert.Len(t, obj["manifest"], 2)
	assert.NoDirExists(t, dry, "--dry-run wrote nothing")

	// generate really writes, and --json still prints one object.
	out := filepath.Join(dir, "cards")
	code, stdout, stderr = runCard(append(append([]string{}, from...), "--out", out, "--json")...)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Empty(t, stderr)
	assert.FileExists(t, filepath.Join(out, "manifest.tsv"))
	obj = oneJSONObject(t, stdout)
	assert.Equal(t, out, obj["dir"])
	assert.Equal(t, float64(2), obj["cards"])
	assert.Equal(t, false, obj["dry_run"])

	// lint --json on a generated card: an object with a findings array.
	code, stdout, stderr = runCard("lint", "--card", filepath.Join(out, "finding-internal-bus-send.md"), "--json")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Empty(t, stderr)
	obj = oneJSONObject(t, stdout)
	require.IsType(t, []any{}, obj["findings"], "the findings are a JSON array")
	assert.Empty(t, obj["findings"])

	// version: the text form is the one line of four tokens.
	code, stdout, stderr = runCard("version")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Empty(t, stderr)
	assert.Len(t, strings.Fields(stdout), 4, "the version line is four tokens: %q", stdout)
	assert.NotContains(t, stdout, "{", "the text form is not JSON")

	// version --json: one object with the same four facts.
	code, stdout, stderr = runCard("version", "--json")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Empty(t, stderr)
	obj = oneJSONObject(t, stdout)
	assert.Equal(t, "nova-card", obj["tool"])
	assert.NotEmpty(t, obj["version"])
	assert.NotEmpty(t, obj["platform"])
	assert.NotEmpty(t, obj["go_version"])
}

// oneJSONObject parses exactly one JSON object from a verb's stdout: trailing
// data (a second object one line later) makes json.Unmarshal fail, so this pins
// "one object" as well.
func oneJSONObject(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var v map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(stdout)), &v), "one JSON object on stdout: %q", stdout)
	require.NotNil(t, v)
	return v
}
