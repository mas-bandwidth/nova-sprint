package git

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// TestMergeCommitsTakeTheStoreIdentity: the landing's merge commits are made
// as the identity the landing passes (the store's, round 24), and with none
// MergeGroup refuses before it makes a commit, never a made-up author
// (fix list L4.8; defect 80: the CAA refused "nova-sprint <sprint@...>").
func TestMergeCommitsTakeTheStoreIdentity(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("b", 40)
	var seen []edge.Cmd
	runner := ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		seen = append(seen, cmd)
		switch {
		case len(cmd.Argv) > 1 && cmd.Argv[1] == "show":
			return edge.Result{Stdout: "2026-11-07T00:00:00Z\n"}, nil
		case len(cmd.Argv) > 1 && cmd.Argv[1] == "rev-parse":
			return edge.Result{Stdout: sha + "\n"}, nil
		}
		return edge.Result{}, nil
	})
	c := New(Config{Exec: runner})
	heads := []string{strings.Repeat("c", 40), strings.Repeat("d", 40)}
	if _, err := c.MergeGroup(context.Background(), "/repo", strings.Repeat("a", 40), heads, nil); err == nil || !strings.Contains(err.Error(), "no commit identity") {
		t.Fatalf("MergeGroup with no identity: err %v, want a refusal naming the identity", err)
	}
	if len(seen) != 0 {
		t.Fatalf("MergeGroup with no identity ran git: %v", seen[0].Argv)
	}
	ctx := WithIdentity(context.Background(), "Sprint Bot", "bot@example.invalid")
	if res, err := c.MergeGroup(ctx, "/repo", strings.Repeat("a", 40), heads, nil); err != nil || res.Kind != MergeOK {
		t.Fatalf("MergeGroup: %+v %v", res, err)
	}
	merges := 0
	for _, cmd := range seen {
		if cmd.Argv[1] != "merge" {
			continue
		}
		merges++
		env := strings.Join(cmd.Env, "\n") + "\n"
		for _, want := range []string{"GIT_AUTHOR_NAME=Sprint Bot\n", "GIT_AUTHOR_EMAIL=bot@example.invalid\n", "GIT_COMMITTER_NAME=Sprint Bot\n", "GIT_COMMITTER_EMAIL=bot@example.invalid\n"} {
			if !strings.Contains(env, want) {
				t.Fatalf("merge env lacks %q:\n%s", want, env)
			}
		}
		if strings.Contains(env, "sprint@mas-bandwidth") {
			t.Fatalf("merge env carries the old literal identity:\n%s", env)
		}
	}
	if merges != len(heads) {
		t.Fatalf("%d merges, want %d", merges, len(heads))
	}
}
