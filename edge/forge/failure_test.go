package forge

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// A failed gh names its command, exit code and stderr, and never the token
// in its environment.
func TestFailedGHCarriesCommandNotToken(t *testing.T) {
	t.Parallel()
	c := New(Config{
		Identity: Identity{Name: "sprint", Token: "ghp_SECRETsecret0123456789abcdef"},
		Now:      func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			if cmd.Argv[1] == "auth" {
				return edge.Result{Stdout: "Logged in to github.com account ci-bot (keyring)\n"}, nil
			}
			return edge.Result{ExitCode: 4, Stderr: "HTTP 404: Not Found (https://api.github.com/repos/x/y/pulls)\n"}, nil
		}),
	})
	_, _, err := c.PR(context.Background(), "x/y", "sprint/s/group", "main")
	if err == nil {
		t.Fatal("no error")
	}
	got := err.Error()
	for _, want := range []string{"gh pr list --repo x/y", "exit 4", `stderr "HTTP 404: Not Found`} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "SECRET") || strings.Contains(got, "ghp_") {
		t.Fatalf("%q leaks the token", got)
	}
}
