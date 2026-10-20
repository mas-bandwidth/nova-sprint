package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWaitForFactRefusesWhatItCannotPark: wait <card> --for-fact <fact>[,<fact>]
// --until <RFC3339> is the coordinator's wait on the environment (docs/CLI.md;
// docs/SPEC-SPRINT.md section 5, the fleet); it refuses a word that is not a fact
// word, a missing or malformed --until, --for, --group, and more than one card, and
// on a card no judgment holds it refuses to park it.
func TestWaitForFactRefusesWhatItCannotPark(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 2")
	until := "--until 2030-01-02T05:04:05Z"
	for _, tc := range []struct{ line, wants string }{
		{"wait s1-1 --for-fact bad! " + until, "fact words"},
		{"wait s1-1 --for-fact libfuzzer", "wants --until"},
		{"wait s1-1 --for-fact libfuzzer --until notatime", "RFC3339"},
		{"wait s1-1 --for-fact libfuzzer " + until + " --for 1h", "takes one card and --until"},
		{"wait s1-1 s1-2 --for-fact libfuzzer " + until, "takes one card and --until"},
		{"wait s1-1 --for-fact libfuzzer " + until, "held by no judgment"},
	} {
		code, out, errs := ta.do(tc.line)
		assert.NotZero(t, code, tc.line)
		assert.Contains(t, out+errs, tc.wants, tc.line)
	}
}
