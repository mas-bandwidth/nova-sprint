package ci

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This is an offline record-integrity gate, not proof of forge state or migration.
func TestMootPRsAreClosedWithAPointer(t *testing.T) {
	b, err := os.ReadFile("../../docs/SPLIT-MOOT-PRS.md")
	require.NoError(t, err)
	require.NoError(t, validateMootPRRecord(string(b), strings.Fields("5015 5222 5227 5228 5230 5233 5234 5235 5236 5238 5239 5243 5246 5278 5306 5308 4956 5014 5149 5247 5266 5268 5275 5281 5300 5307 5258 5282 5283 5285 5287 5288 5289 5291 5293 5297 5298 5299 5301 5303 5304 5305 5309")))
}

func validateMootPRRecord(doc string, expected []string) error {
	seen := map[string]bool{}
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| PR ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		f := strings.Split(strings.Trim(line, "| "), " | ")
		if len(f) != 6 {
			return fmt.Errorf("expected six columns: %s", line)
		}
		pr, head, state, decision, evidence, pointer := f[0], f[1], f[2], f[3], f[4], f[5]
		if seen[pr] || !sha.MatchString(head) || evidence == "" {
			return fmt.Errorf("duplicate PR, invalid head or missing evidence: %s", pr)
		}
		seen[pr] = true
		wantState := map[string]string{"retain-unproved": "OPEN", "split-required": "OPEN", "promotion-order": "OPEN", "dependency-open": "OPEN", "historical-closed": "CLOSED", "historical-merged": "MERGED", "closed-this-run": "CLOSED"}[decision]
		if wantState == "" || state != wantState {
			return fmt.Errorf("inconsistent disposition: %s", pr)
		}
		url := "https://github.com/mas-bandwidth/nova-tools/pull/" + pr
		if pointer != url && !strings.HasPrefix(pointer, url+"#issuecomment-") {
			return fmt.Errorf("wrong PR pointer: %s", pr)
		}
		if decision == "closed-this-run" && (!strings.HasPrefix(pointer, url+"#issuecomment-") || !strings.HasPrefix(evidence, "confirmed-present:")) {
			return fmt.Errorf("new closure requires migration evidence and comment receipt: %s", pr)
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("inventory has %d PRs; want %d", len(seen), len(expected))
	}
	for _, pr := range expected {
		if !seen[pr] {
			return fmt.Errorf("missing candidate: %s", pr)
		}
	}
	return nil
}

func TestMootPRRecordRejectsIncompleteOrUnsafeDispositions(t *testing.T) {
	row := "| 5015 | 495e32563353a11dbb92677066667204c8972c47 | OPEN | retain-unproved | reverse patch failed; not proof of absence | https://github.com/mas-bandwidth/nova-tools/pull/5015 |"
	require.NoError(t, validateMootPRRecord(row, []string{"5015"}))
	for name, doc := range map[string]string{
		"missing": "", "duplicate": row + "\n" + row,
		"short head":       strings.Replace(row, "495e32563353a11dbb92677066667204c8972c47", "495e325", 1),
		"wrong pointer":    strings.Replace(row, "/pull/5015", "/pull/5014", 1),
		"false closure":    strings.Replace(row, "retain-unproved", "closed-this-run", 1),
		"unproved closure": strings.Replace(strings.Replace(row, "OPEN", "CLOSED", 1), "retain-unproved", "closed-this-run", 1),
	} {
		t.Run(name, func(t *testing.T) { require.Error(t, validateMootPRRecord(doc, []string{"5015"})) })
	}
}
