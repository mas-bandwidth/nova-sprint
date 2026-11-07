package run

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// cloneGit records what run clones and ls-remotes.
type cloneGit struct {
	git.Git
	cloned []string
}

func (g *cloneGit) Clone(_ context.Context, repo, _ string) error {
	g.cloned = append(g.cloned, repo)
	return nil
}
func (g *cloneGit) LsRemote(context.Context, string, string) (string, bool, error) {
	return strings.Repeat("a", 40), true, nil
}

// TestHeadsCloneTheStreamsCloneAddress is defect 21 at run: a stream named
// <org>/<repo> with forge github is cloned from GitHub's https address; a
// clone address is cloned as it is; a repo a forge cannot address is the
// stream's heads failure, said once, with no head.
func TestHeadsCloneTheStreamsCloneAddress(t *testing.T) {
	t.Parallel()
	g := &cloneGit{}
	var said []string
	e := &Edges{Git: g, ReposDir: t.TempDir(), Fault: func(what string, err error) { said = append(said, what+": "+err.Error()) }}
	e.readHeads(context.Background(), map[string]store.Stream{
		"tools": {Name: "tools", Repo: "mas-bandwidth/nova-tools", Base: "main", Forge: "github"},
		"url":   {Name: "url", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"},
		"bad":   {Name: "bad", Repo: "nova-tools", Base: "main", Forge: "github"},
	})
	want := map[string]bool{"https://github.com/mas-bandwidth/nova-tools.git": true, "https://example.invalid/r.git": true}
	if len(g.cloned) != 2 || !want[g.cloned[0]] || !want[g.cloned[1]] {
		t.Fatalf("cloned %q", g.cloned)
	}
	heads := e.Heads()
	if len(heads) != 2 || heads["bad"] != "" {
		t.Fatalf("heads %v", heads)
	}
	if len(said) != 1 || !strings.Contains(said[0], "heads stream bad: clone: git refused") {
		t.Fatalf("said %q", said)
	}
}
