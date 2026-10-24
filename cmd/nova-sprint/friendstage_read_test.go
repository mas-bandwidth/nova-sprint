package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// carriedHead is the head an earlier attempt of the primary carried (its CARRY line):
// where a later attempt would start, never the head a read judges.
const carriedHead = "fedcba9876543210fedcba9876543210fedcba98"

// A read card dealt to a friend is staged at the exact head under read, read-only:
// friend stage clones the primary's REPO, fetches the read's head (the head m1
// finished at, never the primary's CARRY head and never BASE, which a read with no
// NEXT used to stage, git exiting 128), checks it out detached, checks rev-parse,
// turns pushing off, and writes JOB.md naming the head, BASE, PATHS and the read
// verbs. It reads no config store (a friend holds no config password) and sets no
// git identity. The READ-OK line it writes, run as written, closes the read.
func TestAFriendReadCardIsStagedAtTheHeadUnderRead(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	home, prev := t.TempDir(), ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
		return prev(k)
	}
	cfg := config.NewMem()
	ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) {
		return cfg.List(ctx, config.KindFriend)
	}
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{
		Name: "amy", Fields: map[string]string{"tiers": "flash", "mode": "one-shot", "width": "1"},
	}, "t")
	require.NoError(t, err)
	ta.a.tip = tipIs(t, landHead)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	dir := t.TempDir()
	brief := "s1-1: the flash card (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nCARRY: s1-1 attempt 1 head=" + carriedHead + "\n\nAS A READ\nread the flash card\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief(brief)), 0o644))
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1 --head " + landHead + " --report done")
	ta.beatUp("amy")
	ta.ok("tick")
	id := sprint.ReadCardID("s1-1", 1, "amy")
	rc := ta.fleetCard(id)
	require.NotNil(t, rc, "the read is asked of amy")
	require.Equal(t, sprint.FriendRow("amy"), rc.Row)

	// the friend holds no config password: a stage that reads the config store fails
	ta.a.friends = func(context.Context, string) ([]config.Row, error) {
		t.Error("a read's stage read the config store")
		return nil, errors.New("the config store wants a password")
	}

	var calls [][]string
	ta.a.stageGit = func(_ context.Context, _ string, args ...string) (string, error) {
		calls = append(calls, args)
		switch args[0] {
		case "clone":
			require.NoError(t, os.MkdirAll(args[len(args)-1], 0o755))
		case "rev-parse":
			return landHead + "\n", nil
		}
		return "", nil
	}

	jobs := filepath.Join(root, "amy-working", "jobs")
	out := ta.ok(fmt.Sprintf("friend stage %s@1 --as friend.amy --dir %s", id, jobs))
	assert.Contains(t, out, "FRIEND-STAGE OK friend=amy card="+id+" gen=1 kind=read head="+landHead)

	// the exact head under read, fetched and checked out detached; never the carried
	// head, never BASE; pushing off; no identity set
	var fetched, checkedOut []string
	var pushOff bool
	for _, c := range calls {
		switch c[0] {
		case "fetch":
			fetched = c
		case "checkout":
			checkedOut = c
		case "config":
			require.Len(t, c, 3)
			assert.NotContains(t, c[1], "user.", "a read sets no git identity")
			if c[1] == "remote.origin.pushurl" {
				pushOff = true
			}
		}
		assert.NotContains(t, c, carriedHead, "the carried head is never staged for a read")
	}
	assert.Equal(t, []string{"fetch", "-q", "origin", landHead}, fetched)
	assert.Equal(t, []string{"checkout", "-q", "--detach", landHead}, checkedOut)
	assert.True(t, pushOff, "pushing is turned off in the read's clone")

	jobPath := filepath.Join(jobs, id, "JOB.md")
	b, err := os.ReadFile(jobPath)
	require.NoError(t, err)
	jobmd := string(b)
	for _, line := range []string{"CARD: " + id, "GEN: 1", "KIND: read", "PRIMARY: s1-1", "ATTEMPT: 1", "BASE: sprint/s1", "HEAD: " + landHead} {
		assert.Contains(t, jobmd, line+"\n", "JOB.md names %s", line)
	}
	assert.Regexp(t, `(?m)^PATHS: \S`, jobmd, "JOB.md names the card's PATHS")
	assert.NotContains(t, jobmd, carriedHead)
	assert.Contains(t, jobmd, "READ-OK: nova-sprint read --as friend.amy --ok "+id+"@1 --epoch 0 --finding ")
	assert.Contains(t, jobmd, "READ-BROKEN: nova-sprint read --as friend.amy --broken "+id+"@1 --epoch 0 --finding ")

	// a second stage of the same read is idempotent: the JOB.md path, no git
	before := len(calls)
	code, out, errs := ta.do(fmt.Sprintf("friend stage %s@1 --as friend.amy --dir %s", id, jobs))
	assert.Equal(t, 0, code, "%s", errs)
	assert.Contains(t, out, jobPath)
	assert.Equal(t, before, len(calls), "an idempotent stage runs no git")

	// the READ-OK line, run as JOB.md writes it, closes the read
	m := regexp.MustCompile(`(?m)^READ-OK: nova-sprint (read .* --finding) '[^']*'$`).FindStringSubmatch(jobmd)
	require.Len(t, m, 2, "JOB.md:\n%s", jobmd)
	ta.ok(m[1] + " checked")
	requireClosedOK(t, ta, id)
}
