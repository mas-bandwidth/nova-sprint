package verbs

import (
	"strings"
	"testing"
)

func TestParseSetupBoundsAndDuplicates(t *testing.T) {
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
		{[]string{"--dash", "--services", "none"}, "alice", false},
		{[]string{"--services", strings.Repeat("a", 512)}, "alice", true},
		{[]string{"--services", strings.Repeat("a", 513)}, "alice", false},
		{[]string{"--as", "bob"}, "", true},
	}
	for _, tc := range cases {
		_, ok := ParseSetup(tc.args, tc.actor)
		if ok != tc.ok {
			t.Fatalf("ParseSetup(%q, %q) = %v", tc.args, tc.actor, ok)
		}
	}
}
