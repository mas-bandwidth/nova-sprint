package verbs

import (
	"testing"
)

// TestStreamAddReachesTheCloneAddress is the verb's check: the form
// the help prints (--repo <org>/<repo> --forge github) is checked at
// GitHub's https address and kept as <org>/<repo>; a form a forge cannot
// address, and a hand stream named <org>/<repo>, are refused naming the fix.
func TestStreamAddReachesTheCloneAddress(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "org/repo through github", as: "carol", args: []string{"stream", "add", "t", "--repo", "o/r", "--forge", "github"}, exit: 0,
			want: []string{"STREAM t repo=o/r base=main"}},
		{name: "a bare name", as: "carol", args: []string{"stream", "add", "t", "--repo", "nova-tools", "--forge", "github"}, exit: 1,
			want: []string{"neither <org>/<repo> nor a clone URL", "name the repository --repo <org>/<repo> with --forge github"}},
		{name: "org/repo with no forge", as: "carol", args: []string{"stream", "add", "t", "--repo", "o/x"}, exit: 2,
			prep: func(_ *testing.T, w *world) { w.origin.err = errBroken },
			want: []string{"git ls-remote of o/x failed", "an <org>/<repo> is cloned through its forge: give stream t --forge github, then add the stream again"}},
	})
}
