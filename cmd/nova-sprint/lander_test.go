package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// handForge is a hand landing's GitHub: the card's PR, open at head, its
// checks read by a real forge.Client whose gh is a fake answering one
// GraphQL rollup (no network), and the merge recorded, never made.
type handForge struct {
	forge.Forge
	checks        *forge.Client
	reads, merges int
	head          string
}

func (f *handForge) PR(context.Context, string, string, string) (forge.PRInfo, bool, error) {
	return forge.PRInfo{Number: 7, URL: "https://github.com/o/r/pull/7", HeadSHA: f.head}, true, nil
}
func (f *handForge) Checks(ctx context.Context, repo string, shas []string) (map[string]forge.CheckRollup, error) {
	f.reads++
	return f.checks.Checks(ctx, repo, shas)
}
func (f *handForge) Merge(context.Context, string, int, string) (string, error) {
	f.merges++
	return strings.Repeat("c", 40), nil
}

// newHandForge is a handForge whose gh reports rollup state with nodes at
// head; a gh call other than the checks' GraphQL read fails the test.
func newHandForge(t *testing.T, head, state string, nodes []map[string]string) *handForge {
	ns := []any{}
	for _, n := range nodes {
		ns = append(ns, n)
	}
	body, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"c0": map[string]any{
		"oid": head, "statusCheckRollup": map[string]any{"state": state, "contexts": map[string]any{"totalCount": len(nodes), "nodes": ns}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	gh := forge.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		if len(cmd.Argv) < 3 || cmd.Argv[1] != "api" || cmd.Argv[2] != "graphql" {
			t.Errorf("unexpected gh call %v", cmd.Argv)
			return edge.Result{ExitCode: 1}, nil
		}
		return edge.Result{Stdout: string(body)}, nil
	})
	return &handForge{head: head, checks: forge.New(forge.Config{Exec: gh, Now: func() int64 { return 1000 }})}
}

// handGit is the lander's git as a fake (no process): base main at tip,
// head not on it, ls-tree answering trees[rev] (exit 128 when absent), and
// the work-tree checkout and the push recorded (the checkout then fails,
// so a forge-less landing that gets past its CI check stops there).
func handGit(tip string, trees map[string][]string, ops *[]string) *git.Client {
	return git.New(git.Config{Exec: git.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		switch {
		case slices.Contains(cmd.Argv, "ls-remote"):
			return edge.Result{Stdout: tip + "\trefs/heads/main\n"}, nil
		case slices.Contains(cmd.Argv, "merge-base"):
			return edge.Result{ExitCode: 1}, nil
		case slices.Contains(cmd.Argv, "ls-tree"):
			paths, ok := trees[cmd.Argv[len(cmd.Argv)-1]]
			if !ok {
				return edge.Result{ExitCode: 128, Stderr: "fatal: not a tree object"}, nil
			}
			return edge.Result{Stdout: strings.Join(paths, "\x00") + "\x00"}, nil
		case slices.Contains(cmd.Argv, "checkout"):
			*ops = append(*ops, "checkout")
			return edge.Result{ExitCode: 1, Stderr: "stop here"}, nil
		case slices.Contains(cmd.Argv, "push"):
			*ops = append(*ops, "push")
		}
		return edge.Result{}, nil
	})})
}

func checkNode(name, status, conclusion string) map[string]string {
	return map[string]string{"__typename": "CheckRun", "name": name, "status": status, "conclusion": conclusion}
}

// TestHandLandMergesOnlyOnGreen: `nova-sprint land` and a judgement
// answered land read the card's checks at its head once and merge only on
// green, the same verdict the automatic landing takes. Pending, failed, all
// skipped and no checks at all are refused, naming the checks and what to
// run next, exiting nonzero, never waiting or polling. On 11-16 and 11-17,
// 41 sprint/ PRs were merged into schema main about 5 s after opening,
// every check skipped. gh and git are fakes: no network, no clock.
func TestHandLandMergesOnlyOnGreen(t *testing.T) {
	t.Parallel()
	tip, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, c := range []struct {
		name, state string
		nodes       []map[string]string
		merges      bool
		says        []string
	}{
		{"pending", "PENDING", []map[string]string{checkNode("unit", "IN_PROGRESS", ""), checkNode("lint", "COMPLETED", "SUCCESS")}, false, []string{"unit", "pending", "gh pr checks"}},
		{"failed", "FAILURE", []map[string]string{checkNode("unit", "COMPLETED", "FAILURE"), checkNode("lint", "COMPLETED", "SUCCESS")}, false, []string{"unit", "red", "fix"}},
		{"all skipped", "SUCCESS", []map[string]string{checkNode("unit", "COMPLETED", "SKIPPED"), checkNode("lint", "COMPLETED", "SKIPPED")}, false, []string{"no check ran green", "skipped"}},
		{"zero checks", "SUCCESS", nil, false, []string{"no check ran green"}},
		{"green", "SUCCESS", []map[string]string{checkNode("unit", "COMPLETED", "SUCCESS"), checkNode("lint", "COMPLETED", "SKIPPED")}, true, nil},
	} {
		var ops []string
		fg := newHandForge(t, head, c.state, c.nodes)
		l := &lander{clones: &origin{dir: t.TempDir(), git: handGit(tip, nil, &ops)}, forge: fg}
		s := store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"}
		sha, err := l.Land(context.Background(), s, "sprint/s1/x", head)
		if fg.reads != 1 {
			t.Errorf("%s: the checks at the head were read %d times, want once (never polled)", c.name, fg.reads)
		}
		if c.merges {
			if err != nil || fg.merges != 1 || sha != strings.Repeat("c", 40) {
				t.Errorf("%s: landed %q, err %v, %d merges; want the merge", c.name, sha, err, fg.merges)
			}
			continue
		}
		var oe *verbs.OperationError
		if fg.merges != 0 || !errors.As(err, &oe) || oe.Exit == 0 {
			t.Errorf("%s: %d merges, err %v; want a refusal exiting nonzero and no merge", c.name, fg.merges, err)
			continue
		}
		for _, w := range c.says {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: refusal %q does not say %q", c.name, err, w)
			}
		}
	}
}

// TestHandLandNoForgeRefusesARepoWithCI: a stream with no forge lands by
// pushing straight onto base, which reads no check, so it is refused when
// base or the card's head carries CI configuration, and when the trees
// cannot be read (it cannot tell). With no CI it goes on to merge. git is a
// fake: no process.
func TestHandLandNoForgeRefusesARepoWithCI(t *testing.T) {
	t.Parallel()
	tip, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	plain := []string{"README.md", "main.go"}
	for _, c := range []struct {
		name   string
		trees  map[string][]string
		pushes bool
		says   []string
	}{
		{"workflow on base", map[string][]string{tip: append([]string{".github/workflows/ci.yml"}, plain...), head: plain}, false, []string{".github/workflows/ci.yml", "has CI", "--forge github"}},
		{"CI added by the card", map[string][]string{tip: plain, head: append([]string{".gitlab-ci.yml"}, plain...)}, false, []string{".gitlab-ci.yml", "has CI"}},
		{"trees unreadable", map[string][]string{}, false, []string{"cannot tell whether", "not a tree object"}},
		{"no CI", map[string][]string{tip: plain, head: plain}, true, nil},
	} {
		var ops []string
		l := &lander{clones: &origin{dir: t.TempDir(), git: handGit(tip, c.trees, &ops)}}
		ctx := git.WithIdentity(context.Background(), "Test", "test@example.invalid")
		_, err := l.Land(ctx, store.Stream{Name: "s1", Repo: "https://example.invalid/r.git", Base: "main"}, "sprint/s1/x", head)
		if c.pushes {
			if !slices.Equal(ops, []string{"checkout"}) {
				t.Errorf("%s: ops %v (err %v), want the merge begun", c.name, ops, err)
			}
			continue
		}
		var oe *verbs.OperationError
		if len(ops) != 0 || !errors.As(err, &oe) || oe.Exit == 0 {
			t.Errorf("%s: ops %v, err %v; want a refusal exiting nonzero before any checkout or push", c.name, ops, err)
			continue
		}
		for _, w := range c.says {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: refusal %q does not say %q", c.name, err, w)
			}
		}
	}
}
