package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stageCheckout with a fake git (no real git, no network): a full sha checks out as is,
// never probing a remote ref; a BASE name the fresh clone holds only as origin/<BASE>
// checks out from origin/<BASE>, the shape that exited 128 live on 2026-11-05 under
// v1.6.27 ("checkout of sprint/<card>.w1.g1.e2 at sprint/vg-ports"); a local name with no
// remote-tracking ref checks out as the local name.
func TestStageCheckoutByAFakeGit(t *testing.T) {
	t.Parallel()
	const sha = "0123456789abcdef0123456789abcdef01234567"
	fake := func(remote map[string]bool, calls *[]string) func(context.Context, string, ...string) (string, error) {
		return func(_ context.Context, _ string, args ...string) (string, error) {
			*calls = append(*calls, strings.Join(args, " "))
			if args[0] == "rev-parse" {
				if remote[args[len(args)-1]] {
					return sha + "\n", nil
				}
				return "", errors.New("git exited with status 1")
			}
			return "", nil
		}
	}
	for _, tc := range []struct {
		name, head string
		remote     map[string]bool
		want       []string
	}{
		{"a full sha", sha, map[string]bool{}, []string{"checkout -q -B b " + sha}},
		{"a remote-only base", "sprint/vg-ports", map[string]bool{"refs/remotes/origin/sprint/vg-ports": true},
			[]string{"rev-parse --verify -q refs/remotes/origin/sprint/vg-ports", "checkout -q -B b origin/sprint/vg-ports"}},
		{"a local name", "main", map[string]bool{},
			[]string{"rev-parse --verify -q refs/remotes/origin/main", "checkout -q -B b main"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			require.NoError(t, stageCheckout(context.Background(), fake(tc.remote, &calls), "/job", "b", tc.head))
			require.Equal(t, tc.want, calls)
		})
	}
}
