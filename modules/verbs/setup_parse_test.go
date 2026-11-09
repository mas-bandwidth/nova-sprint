package verbs

import (
	"strings"
	"testing"
)

func TestParseSetupBoundsAndDuplicates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args  []string
		actor string
		ok    bool
	}{
		{nil, "alice", true},
		{nil, "", false},
		{nil, strings.Repeat("a", 129), false},
		{nil, "alice/other", false},
		{[]string{"--as", "bob", "--as", "eve"}, "alice", false},
		{[]string{"--dash", "public=" + strings.Repeat("a", 505)}, "alice", true},
		{[]string{"--dash", "public=" + strings.Repeat("a", 506)}, "alice", false},
		{[]string{"--services", "none"}, "alice", false},
		{[]string{"--run-on", "studio"}, "alice", false},
		{[]string{"--version", "v2.0.0"}, "alice", false},
		{[]string{"--as", "bob"}, "", true},
	}
	for _, tc := range cases {
		_, ok := ParseSetup(tc.args, tc.actor)
		if ok != tc.ok {
			t.Fatalf("ParseSetup(%q, %q) = %v", tc.args, tc.actor, ok)
		}
	}
}
