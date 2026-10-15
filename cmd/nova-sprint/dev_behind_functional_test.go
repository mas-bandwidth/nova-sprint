//go:build functional

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// TestDevIsBehindCountsOnlyCardsLandedOnSprintBranches drives the "dev is behind" rule on a
// real store (docs/SPEC-SPRINT.md section 7): cards landed onto main and onto dev in two
// repositories with no sprint branch never count, and a card landed on a sprint branch of a
// repository that has a dev branch counts and is named by its id. The clock is injected so the
// 30-minute age threshold passes without waiting. Each repository is a local bare origin the
// REPO: name resolves to (url.<origin>.insteadOf), and each card finishes at the full sha of
// a real commit pushed to it, so the finish guard (a head origin holds) passes as a member's
// finish does. No network.
func TestDevIsBehindCountsOnlyCardsLandedOnSprintBranches(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	// An injected clock, so the age threshold passes without a real wait.
	clock := time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return clock }
	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, strings.Fields(line), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}
	briefFile := func(name, text string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), name)
		require.NoError(t, os.WriteFile(p, []byte(text), 0o600))
		return p
	}

	const (
		mainRepo = "mas-bandwidth/nova-sprint"
		devRepo  = "mas-bandwidth/nova-tools"
	)

	// Each repository's origin is a local bare repository its REPO: name resolves to, and a
	// worker's clone of it makes and pushes each card's commit.
	gitEnv := append(slices.DeleteFunc(os.Environ(), func(e string) bool { return strings.HasPrefix(e, "GOFLAGS=") }),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=worker", "GIT_AUTHOR_EMAIL=worker@example.invalid", "GIT_COMMITTER_NAME=worker", "GIT_COMMITTER_EMAIL=worker@example.invalid")
	git := func(dir string, args ...string) string {
		t.Helper()
		res, err := gitrun.Run(context.Background(), gitrun.Options{C: dir, Env: gitEnv, OwnRepo: dir != ""}, args...)
		require.NoError(t, err, "git %v: %s", args, res.Stderr)
		return strings.TrimSpace(string(res.Stdout))
	}
	fixture := t.TempDir()
	workers := map[string]string{}
	var insteadOf []string
	for i, repo := range []string{mainRepo, devRepo} {
		name := filepath.Base(repo)
		remote, worker := filepath.Join(fixture, name+".git"), filepath.Join(fixture, name)
		git("", "init", "-q", "--bare", "-b", "main", remote)
		git(remote, "config", "receive.autogc", "false") // no detached writer outlives the TempDir
		git("", "clone", "-q", remote, worker)
		require.NoError(t, os.WriteFile(filepath.Join(worker, "README"), []byte("base\n"), 0o600))
		git(worker, "add", "README")
		git(worker, "commit", "-q", "-m", "base")
		git(worker, "push", "-q", "origin", "HEAD:refs/heads/main")
		workers[repo] = worker
		insteadOf = append(insteadOf,
			"GIT_CONFIG_KEY_"+strconv.Itoa(i)+"=url."+remote+".insteadOf",
			"GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+swarm.CardRepoURL(repo))
	}
	gitEnv = append(gitEnv, append([]string{"GIT_CONFIG_COUNT=" + strconv.Itoa(len(workers))}, insteadOf...)...)
	a.gitEnv = gitEnv

	// pushed is the card's work as a member makes it: a commit in repo's worker clone, pushed
	// to origin on the card's branch (the branch the finish guard names); its full sha.
	pushed := func(repo, id string) string {
		t.Helper()
		worker := workers[repo]
		require.NoError(t, os.WriteFile(filepath.Join(worker, id+".txt"), []byte(id+"\n"), 0o600))
		git(worker, "add", id+".txt")
		git(worker, "commit", "-q", "-m", "work of "+id)
		head := git(worker, "rev-parse", "HEAD")
		git(worker, "push", "-q", "origin", "HEAD:refs/heads/sprint/"+id+".w1.g1.e0")
		return head
	}

	// land drives the ids of one stream through the whole flow to landed, each card
	// finished at the full sha of its commit pushed to repo's origin.
	land := func(stream, repo string, ids []string) {
		t.Helper()
		run("fleet beat m1") // m1's cards are not redealt as stale
		run("tick")
		run("tick")
		run("take --as m1 --limit " + strconv.Itoa(len(ids)) + " --epoch 0")
		for _, id := range ids {
			run("finish --as m1 " + id + ".w1@1 --epoch 0 --head " + pushed(repo, id))
		}
		run("queue --as reader-a")
		run("queue --as reader-b")
		run("tick")
		run("read --as reader-a --begin --limit " + strconv.Itoa(len(ids)) + " --epoch 0")
		run("read --as reader-a --ok --limit " + strconv.Itoa(len(ids)) + " --epoch 0")
		run("read --as reader-b --begin --limit " + strconv.Itoa(len(ids)) + " --epoch 0")
		run("read --as reader-b --ok --limit " + strconv.Itoa(len(ids)) + " --epoch 0")
		run("accept " + strings.Join(ids, " "))
		recordMerge(t, a, stream, len(ids), "0")
		run("tick")
	}

	run("init --readers reader-a,reader-b --members m1")
	run("set --read-cards off") // this landing fixture consumes standalone legacy readers
	// one held card keeps the sprint open, so the dev-is-behind judgment still runs
	run("add --stream s0 --count 1 --one --held --land-protected any")
	run("start")
	run("fleet beat m1")

	// Two repositories with no sprint branch: cards on main, and cards on dev. The mark
	// admits the protected bases (docs/SPEC-SPRINT.md section 7, add --land-protected).
	// Stream s2 is added only after s1 lands: a take --limit takes per member, not per
	// stream, so the two streams' cards must not be dealt together.
	run("add --stream s1 --land-protected any --count 2 --brief-file " + briefFile("main.md", passingBrief("tier: pro\nREPO: "+mainRepo+"\nBASE: main\n")))
	land("s1", mainRepo, []string{"s1-1", "s1-2"})
	run("add --stream s2 --land-protected any --count 2 --brief-file " + briefFile("dev.md", passingBrief("tier: pro\nREPO: "+devRepo+"\nBASE: dev\n")))
	land("s2", devRepo, []string{"s2-1", "s2-2"})

	// Past the age threshold, the main and dev cards still raise no judgment.
	clock = clock.Add(sprint.PromoteAge + time.Minute)
	run("tick")
	require.Empty(t, notesOf(a, sprint.NDevBehind), "cards landed on main and dev never count")

	// A sprint branch in a repository that has a dev branch: s2's repository already has dev
	// cards landed, so the sprint-branch card counts.
	run("add --stream s2 --count 2 --brief-file " + briefFile("sprint.md", passingBrief("tier: pro\nREPO: "+devRepo+"\nBASE: sprint/base\n")))
	land("s2", devRepo, []string{"s2-3", "s2-4"})
	clock = clock.Add(sprint.PromoteAge + time.Minute)
	run("tick")

	behind := notesOf(a, sprint.NDevBehind)
	require.Len(t, behind, 1, "exactly one dev is behind judgment")
	require.Contains(t, behind[0].What, "sprint/base", "the branch is the sprint branch")
	require.Contains(t, behind[0].What, "s2-3", "the judgment names the card by its id")
	require.Contains(t, behind[0].What, "s2-4", "the judgment names the card by its id")
}

// notesOf reads the store's notes of one type, addressed to the coordinator.
func notesOf(a *app, typ string) []sprint.Note {
	_, cm := a.verbSetup("log")
	st, err := a.store(*cm)
	if err != nil {
		return nil
	}
	notes, _, err := st.B.NotesSince(context.Background(), "", 10000)
	if err != nil {
		return nil
	}
	var out []sprint.Note
	for _, n := range notes {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}
