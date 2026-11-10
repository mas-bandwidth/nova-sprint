package worker

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// streamFake answers the stream step's git as the worker's clone would:
// merge-base fails mergeBaseFails times first; merge exits mergeExit with
// mergeErr; MERGE_HEAD is there when inMerge; diff lists conflicts.
type streamFake struct {
	fx             *edge.FakeExec
	mergeBaseFails int
	mergeExit      int
	mergeErr       string
	inMerge        bool
	conflicts      string
}

func newStreamFake_internal(f *streamFake) *streamFake {
	f.fx = edge.NewFakeExec()
	f.fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[gitArg0_internal:]
		switch {
		case args[0] == "merge-base" && f.mergeBaseFails > 0:
			f.mergeBaseFails--
			return edge.FakeOutcome{ExitCode: 1}
		case args[0] == "merge" && args[1] == "-m":
			return edge.FakeOutcome{ExitCode: f.mergeExit, Stderr: f.mergeErr}
		case args[0] == "rev-parse" && slices.Contains(args, "MERGE_HEAD") && !f.inMerge:
			return edge.FakeOutcome{ExitCode: 1}
		case args[0] == "diff":
			return edge.FakeOutcome{Stdout: f.conflicts}
		}
		return edge.FakeOutcome{}
	})
	return f
}

func streamCalls_internal(fx *edge.FakeExec) []string {
	argv, _, _ := stageCalls_internal(fx)
	return argv
}

// A worker's clone shares the work clone's shallow boundary, and a stream
// tip a later staging fetched at depth 1 is a shallow root there: merged
// as it is, the card's head and the stream share no commit ("refusing to
// merge unrelated histories", rn-send-rate-one-constant.w2 on space,
// 2026-11-09). The stream step deepens a shallow clone, by 64 then 1024
// commits, then unshallows, until the two share a commit, then merges. A
// deep clone is never deepened.
func TestStreamStepDeepensUntilAMergeBase(t *testing.T) {
	t.Parallel()
	fetch := "fetch --quiet --no-recurse-submodules " + testCloneURL + " main"
	deepen := func(step string) string {
		return "fetch --quiet --no-tags --no-recurse-submodules " + step + " " + testCloneURL + " main"
	}
	tail := []string{"merge -m Merge main into the card FETCH_HEAD", "rev-parse HEAD"}
	for _, kase := range []struct {
		name    string
		shallow bool
		fails   int
		steps   []string
	}{
		{name: "shares a commit", shallow: true},
		{name: "deepened once", shallow: true, fails: 1, steps: []string{"--deepen=64"}},
		{name: "unshallowed", shallow: true, fails: 99, steps: []string{"--deepen=64", "--deepen=1024", "--unshallow"}},
		{name: "deep", fails: 1},
	} {
		t.Run(kase.name, func(t *testing.T) {
			t.Parallel()
			clone := filepath.Join(t.TempDir(), "sealed")
			if err := writeGitFile_internal(filepath.Join(clone, ".git", "HEAD"), []byte("ref: refs/heads/x\n")); err != nil {
				t.Fatal(err)
			}
			if kase.shallow {
				if err := writeGitFile_internal(filepath.Join(clone, ".git", "shallow"), []byte(shaTip+"\n")); err != nil {
					t.Fatal(err)
				}
			}
			f := newStreamFake_internal(&streamFake{mergeBaseFails: kase.fails})
			g := &OSGitOps{Exec: f.fx}
			if _, err := g.RebaseOntoStream(context.Background(), clone, testCloneURL, "main"); err != nil {
				t.Fatal(err)
			}
			want := []string{fetch}
			if kase.shallow {
				want = append(want, "merge-base HEAD FETCH_HEAD")
				for _, s := range kase.steps {
					want = append(want, deepen(s), "merge-base HEAD FETCH_HEAD")
				}
				if len(kase.steps) == 3 { // nothing shallow is left to deepen: the merge says what it finds
					want = want[:len(want)-1]
				}
			}
			want = append(want, tail...)
			if got := streamCalls_internal(f.fx); !slices.Equal(got, want) {
				t.Fatalf("git ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// A merge that fails before it starts (unrelated histories, a dirty tree)
// leaves no merge to abort: the step aborts only a merge in progress, so
// its error is the merge's own, with no failed cleanup beside it. A
// conflict is aborted and named.
func TestStreamStepAbortsOnlyAMergeInProgress(t *testing.T) {
	t.Parallel()
	clone := filepath.Join(t.TempDir(), "sealed")
	f := newStreamFake_internal(&streamFake{mergeExit: 128, mergeErr: "fatal: refusing to merge unrelated histories"})
	g := &OSGitOps{Exec: f.fx}
	_, err := g.RebaseOntoStream(context.Background(), clone, testCloneURL, "main")
	if err == nil || !strings.Contains(err.Error(), "refusing to merge unrelated histories") || strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("err %v, want the merge's own error and no cleanup", err)
	}
	if slices.Contains(streamCalls_internal(f.fx), "merge --abort") {
		t.Fatalf("aborted a merge that never started: %q", streamCalls_internal(f.fx))
	}
	f = newStreamFake_internal(&streamFake{mergeExit: 1, inMerge: true, conflicts: "a.go\n"})
	g = &OSGitOps{Exec: f.fx}
	_, err = g.RebaseOntoStream(context.Background(), clone, testCloneURL, "main")
	var ce *ConflictError
	if !errors.As(err, &ce) || !slices.Equal(ce.Files, []string{"a.go"}) || !slices.Contains(streamCalls_internal(f.fx), "merge --abort") {
		t.Fatalf("err %v, calls %q: want the conflict named and the merge aborted", err, streamCalls_internal(f.fx))
	}
}
