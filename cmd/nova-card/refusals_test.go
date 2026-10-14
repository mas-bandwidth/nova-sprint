package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusalObject is the JSON object a refusal prints on stdout under --json: the
// verb, the fleet's refused status, the exit code, why and the remedy (the tool
// the other tools print a refusal with). The test reads it with its own shape so
// that a missing object fails the assertion, not the build.
type refusalObject struct {
	Verb   string `json:"verb"`
	Status string `json:"status"`
	Exit   int    `json:"exit"`
	Reason string `json:"reason"`
	Remedy string `json:"remedy"`
}

// decodeRefusal reads the one JSON object a --json refusal owes stdout.
func decodeRefusal(t *testing.T, stdout string) refusalObject {
	t.Helper()
	var o refusalObject
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(stdout)), &o), "stdout is not one JSON object: %q", stdout)
	return o
}

// TestHelpUnknownRefusesAndRefusalsHonourJSON holds cmd/nova-card to the three
// cold-reader findings of its card (docs/STANDARD.md section 3, refusing to
// guess): `help <unknown>` refuses at exit 2 naming the nearest verb and the
// verb list instead of dumping the banner at exit 0; a refusal under --json
// writes the JSON refusal object on stdout, exit 2, with prose on stderr alone;
// and the banner leads with the verb that touches nothing before generate.
func TestHelpUnknownRefusesAndRefusalsHonourJSON(t *testing.T) {
	t.Parallel()

	t.Run("help-unknown-refuses", func(t *testing.T) {
		t.Parallel()
		exit, stdout, stderr := runCard("help", "generat")
		assert.Equal(t, 2, exit, "help <unknown>: exit %d", exit)
		assert.Empty(t, stdout, "help <unknown> dumped the banner: %q", stdout)
		require.NotEmpty(t, stderr, "help <unknown> printed no refusal")
		assert.Contains(t, stderr, "REFUSED")
		assert.Contains(t, stderr, `unknown verb "generat"`)
		assert.Contains(t, stderr, "generate", "the nearest verb is not named")
		for _, verb := range verbs {
			assert.Contains(t, stderr, verb, "the verb list omits %s", verb)
		}
	})

	t.Run("refusals-honour-json", func(t *testing.T) {
		t.Parallel()

		// lint: no --card refuses; the object is on stdout and stderr is empty
		exit, stdout, stderr := runCard("lint", "--json")
		assert.Equal(t, 2, exit)
		assert.Empty(t, stderr, "a --json refusal wrote prose on stderr: %q", stderr)
		obj := decodeRefusal(t, stdout)
		assert.Equal(t, "lint", obj.Verb)
		assert.Equal(t, "refused", obj.Status)
		assert.Equal(t, 2, obj.Exit)
		assert.NotEmpty(t, obj.Reason)
		assert.Contains(t, obj.Remedy, "nova-card help")

		// template takes no arguments, so --json is itself refused as JSON
		exit, stdout, stderr = runCard("template", "--json")
		assert.Equal(t, 2, exit)
		assert.Empty(t, stderr, "a --json refusal wrote prose on stderr: %q", stderr)
		obj = decodeRefusal(t, stdout)
		assert.Equal(t, "template", obj.Verb)
		assert.Equal(t, "refused", obj.Status)

		// generate: --json is not a verb flag yet, and the unknown flag is
		// refused in the object, not as prose on stderr
		exit, stdout, stderr = runCard("generate", "--json", "--from", "findings", "--file", "testdata/findings.tsv", "--repo", "o/r", "--base", "dev", "--sha", strings.Repeat("ab", 20))
		assert.Equal(t, 2, exit)
		assert.Empty(t, stderr, "a --json refusal wrote prose on stderr: %q", stderr)
		obj = decodeRefusal(t, stdout)
		assert.Equal(t, "generate", obj.Verb)
		assert.Equal(t, "refused", obj.Status)
		assert.NotEmpty(t, obj.Reason)
	})

	t.Run("banner-leads-with-the-safe-verb", func(t *testing.T) {
		t.Parallel()
		_, banner, _ := runCard("help")
		_, block, _ := strings.Cut(banner, "\nusage:\n")
		block, _, _ = strings.Cut(block, "\n\n")
		first := ""
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "  nova-card ") {
				first = strings.TrimSpace(line)
				break
			}
		}
		require.NotEmpty(t, first, "the usage block names no verb:\n%s", block)
		assert.Contains(t, first, "template", "the banner does not lead with the verb that touches nothing:\n%s", block)
	})
}
