package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// Cold read 3, 2026-11-08, at 032afd657: one failing probe per confirmed
// defect.

// git.go Fetch called every non-zero exit ErrMissingRef "ref not on
// origin": a failed login, an unknown host or a timeout too. Only git's own
// missing-ref words are a missing ref; anything else is the failure with
// git's text.
func TestColdread3FetchMissingRefOnlyFromGitsWords(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		stderr  string
		missing bool
	}{
		"missing ref": {stderr: "fatal: couldn't find remote ref refs/heads/sprint/s1/c9\n", missing: true},
		"missing sha": {stderr: "fatal: remote error: upload-pack: not our ref 0123456789abcdef0123456789abcdef01234567\n", missing: true},
		"dns":         {stderr: "fatal: unable to access 'https://github.com/o/r.git/': Could not resolve host: github.com\n"},
		"auth":        {stderr: "remote: Invalid username or token.\nfatal: Authentication failed for 'https://github.com/o/r.git/'\n"},
	} {
		ex := edge.NewFakeExec()
		ex.Enqueue("git", edge.FakeOutcome{ExitCode: 128, Stderr: tc.stderr})
		err := New(Config{Exec: ex}).Fetch(context.Background(), t.TempDir(), "sprint/s1/c9")
		if err == nil || errors.Is(err, ErrMissingRef) != tc.missing {
			t.Errorf("%s: err = %v, want missing ref %v", name, err, tc.missing)
		}
		if first, _, _ := strings.Cut(strings.TrimSpace(tc.stderr), "\n"); err != nil && !strings.Contains(err.Error(), strings.TrimPrefix(first, "fatal: ")[:20]) {
			t.Errorf("%s: err = %v lacks git's words", name, err)
		}
	}
}
