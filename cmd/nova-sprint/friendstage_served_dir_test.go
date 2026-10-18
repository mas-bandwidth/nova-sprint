package main

import (
	"strings"
	"testing"
)

// TestAServedFriendStageRefusesADirOutsideTheJobRoot pins the sprint server's
// friend stage --dir check: a busy caller must not choose where the server
// writes on a fleet machine, so the served verb admits only the friend's job
// root on the bench (~/<friend>-jobs or $HOME/<friend>-jobs) or a directory
// beneath it, and refuses anything else.
func TestAServedFriendStageRefusesADirOutsideTheJobRoot(t *testing.T) {
	t.Parallel()
	base := []string{"friend", "stage", "s1-1.w1@1", "--as", "friend.amy", "--bench", "m1"}
	cases := []struct {
		name string
		dir  string
		give bool
		want bool
	}{
		{"refused /etc", "/etc", true, false},
		{"refused /tmp/x", "/tmp/x", true, false},
		{"refused ~/other-jobs", "~/other-jobs", true, false},
		{"refused a dot-dot element", "~/amy-jobs/../evil", true, false},
		{"refused $HOME", "$HOME", true, false},
		{"refused ~/amy-jobsX", "~/amy-jobsX", true, false},
		{"refused a quote", "~/amy-jobs/foo\"bar", true, false},
		{"refused a space", "~/amy-jobs/foo bar", true, false},
		{"admitted no --dir", "", false, true},
		{"admitted ~/amy-jobs", "~/amy-jobs", true, true},
		{"admitted $HOME/amy-jobs/a.w1", "$HOME/amy-jobs/a.w1", true, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			argv := append([]string{}, base...)
			if tc.give {
				argv = append(argv, "--dir", tc.dir)
			}
			as, why := friendStageServed(argv)
			if tc.want {
				if why != "" {
					t.Fatalf("expected admission, refused: %s", why)
				}
				if as != "amy" {
					t.Fatalf("expected --as friend.amy, got %s", as)
				}
				return
			}
			if why == "" {
				t.Fatal("expected refusal, admitted")
			}
			if want := "writes only under ~/amy-jobs on the bench"; !strings.Contains(why, want) {
				t.Fatalf("refusal %q does not say %q", why, want)
			}
		})
	}
}
