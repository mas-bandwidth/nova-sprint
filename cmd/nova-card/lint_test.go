package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
)

func TestCardLintJSONOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Clean card: rendered via cardgen with valid header and tasks
	cleanH := cardgen.Header{Repo: "example/repo", Base: "dev", Sha: "0123456789abcdef0123456789abcdef01234567"}
	fs, skipped := cardgen.ParseFindings("internal/bus/send.go:12\tclean\tfix it\tinternal/bus TestClean\n")
	require.Empty(t, skipped)
	fp := cardgen.PlanFindings(fs, "", "", 0)
	require.NotEmpty(t, fp.Cards)
	cleanCard := fp.Cards[0]
	cleanBrief := cardgen.Render(cleanH, cleanCard)
	cleanPath := filepath.Join(dir, "clean.md")
	require.NoError(t, os.WriteFile(cleanPath, []byte(cleanBrief), 0o644))

	// Card with drift: line 1 missing tier, and has unfilled placeholder
	lines := strings.Split(cleanBrief, "\n")
	lines[0] = "RESULT: drift sha=0123456789ab"
	driftBrief := strings.Join(lines, "\n") + "\n<unfilled-placeholder>\n"
	driftPath := filepath.Join(dir, "drift.md")
	require.NoError(t, os.WriteFile(driftPath, []byte(driftBrief), 0o644))

	type jsonFinding struct {
		Rule     string `json:"rule"`
		RuleName string `json:"rule_name"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Excerpt  string `json:"excerpt"`
		Remedy   string `json:"remedy"`
	}

	type jsonOutput struct {
		Findings []jsonFinding `json:"findings"`
	}

	// 1. Plaintext output remains unchanged when --json is omitted.
	t.Run("plaintext output", func(t *testing.T) {
		code, out, errs := runCard("lint", "--card", cleanPath)
		assert.Equal(t, 0, code)
		assert.Empty(t, errs)
		assert.Contains(t, out, "LINT OK file="+cleanPath)

		code, out, errs = runCard("lint", "--card", driftPath)
		assert.Equal(t, 1, code)
		assert.Empty(t, errs)
		assert.Contains(t, out, "LINT DRIFT card=drift check=tier-line line=1:")
		assert.Contains(t, out, "LINT DRIFT card=drift check=placeholder")
	})

	// 2. Clean card with --json: exits 0, emits valid JSON with empty findings array.
	t.Run("clean card json", func(t *testing.T) {
		code, out, errs := runCard("lint", "--card", cleanPath, "--json")
		assert.Equal(t, 0, code, "clean card with --json must exit 0; err: %s", errs)
		assert.Empty(t, errs)

		var parsed jsonOutput
		err := json.Unmarshal([]byte(out), &parsed)
		require.NoError(t, err, "output must be valid JSON: %s", out)
		assert.NotNil(t, parsed.Findings, "findings array must be present")
		assert.Empty(t, parsed.Findings, "clean card must have 0 findings")
	})

	// 3. Card with drift under --json: exits 1, emits structured findings array.
	t.Run("drift card json", func(t *testing.T) {
		code, out, errs := runCard("lint", "--card", driftPath, "--json")
		assert.Equal(t, 1, code, "drift card with --json must exit 1; err: %s", errs)
		assert.Empty(t, errs)

		var parsed jsonOutput
		err := json.Unmarshal([]byte(out), &parsed)
		require.NoError(t, err, "output must be valid JSON: %s", out)
		require.NotEmpty(t, parsed.Findings, "findings array must contain entries")

		foundTier := false
		foundPlaceholder := false
		for _, f := range parsed.Findings {
			assert.Equal(t, driftPath, f.File, "finding file should match input card path")
			assert.Greater(t, f.Line, 0, "line must be positive")
			assert.NotEmpty(t, f.Excerpt, "excerpt must not be empty")
			assert.NotEmpty(t, f.Remedy, "remedy must not be empty")
			assert.NotEmpty(t, f.Rule, "rule must not be empty")

			if f.Rule == "tier-line" || f.RuleName == "tier-line" {
				foundTier = true
				assert.Equal(t, 1, f.Line)
			}
			if f.Rule == "placeholder" || f.RuleName == "placeholder" {
				foundPlaceholder = true
			}
		}
		assert.True(t, foundTier, "must find tier-line drift")
		assert.True(t, foundPlaceholder, "must find placeholder drift")
	})
}
