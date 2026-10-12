//go:build functional

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// TestDevIsBehindCountsOnlyCardsLandedOnSprintBranches drives the "dev is behind" rule on a
// real store (docs/SPEC-SPRINT.md section 7): cards landed onto main and onto dev in two
// repositories with no sprint branch never count, and a card landed on a sprint branch of a
// repository that has a dev branch counts and is named by its id. The clock is injected so the
// 30-minute age threshold passes without waiting.
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

	// land drives the ids of one stream through the whole flow to landed.
	land := func(stream string, ids []string) {
		t.Helper()
		var workIDs []string
		for _, id := range ids {
			workIDs = append(workIDs, id+".w1@1")
		}
		run("tick")
		run("tick")
		run("take --as m1 --limit " + strconv.Itoa(len(ids)) + " --epoch 0")
		run("finish --as m1 " + strings.Join(workIDs, " ") + " --epoch 0")
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
	run("start")
	run("fleet beat m1")

	// Two repositories with no sprint branch: cards on main, and cards on dev. The mark
	// admits the protected bases (docs/SPEC-SPRINT.md section 7, add --land-protected).
	run("add --stream s1 --land-protected any --count 2 --brief-file " + briefFile("main.md", "tier: pro\nREPO: "+mainRepo+"\nBASE: main\n"))
	run("add --stream s2 --land-protected any --count 2 --brief-file " + briefFile("dev.md", "tier: pro\nREPO: "+devRepo+"\nBASE: dev\n"))
	land("s1", []string{"s1-1", "s1-2"})
	land("s2", []string{"s2-1", "s2-2"})

	// Past the age threshold, the main and dev cards still raise no judgment.
	clock = clock.Add(sprint.PromoteAge + time.Minute)
	run("tick")
	require.Empty(t, notesOf(a, sprint.NDevBehind), "cards landed on main and dev never count")

	// A sprint branch in a repository that has a dev branch: s2's repository already has dev
	// cards landed, so the sprint-branch card counts.
	run("add --stream s2 --count 2 --brief-file " + briefFile("sprint.md", "tier: pro\nREPO: "+devRepo+"\nBASE: sprint/base\n"))
	land("s2", []string{"s2-3", "s2-4"})
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
