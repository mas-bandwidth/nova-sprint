//go:build functional

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// missingBranchStore builds, on a private store, the live state of 2026-10-15 (cards
// vg-stone-shape-research-a, j16495, and virtualgo-reconstruction-a, j16496): s1-1 in review
// at attempt 2, its attempt-2 read closed broken on a branch origin does not hold while no
// branch of the work holds the head, so the read is retired branch-missing and the
// NReadBranchMissing judgment is open on the primary. Origin's branches are the fixture
// tips (the app's readTip and readHeads); the store is real. It returns the helper, the
// judgment's alias and the head.
func missingBranchStore(t *testing.T, tips *map[string]string) (*functionalTestApp, string, string) {
	t.Helper()
	const head = "0123456789abcdef0123456789abcdef01234567"
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	require.NoError(t, fn.Load(ctx, c))
	ta := newFunctionalTestApp(t, addr)
	run := ta.runVerb
	a := ta.app()
	*tips = map[string]string{}
	a.readTip = func(_ context.Context, _, branch string) (string, error) { return (*tips)[branch], nil }
	a.readHeads = func(_ context.Context, _, pattern string) (map[string]string, error) {
		prefix := strings.TrimSuffix(strings.TrimPrefix(pattern, "refs/heads/"), "*")
		out := map[string]string{}
		for b, tip := range *tips {
			if strings.HasPrefix(b, prefix) {
				out[b] = tip
			}
		}
		return out, nil
	}

	// the finishes find the head on origin (finishOrigin); the branches are gone before the read
	(*tips)["sprint/pushed"] = head
	run("init", "--readers", "reader-a,reader-b,reader-c", "--members", "m1:8")
	run("set", "--read-cards", "off") // the legacy readers table, as the unit-tier rig reads it
	brief := filepath.Join(t.TempDir(), "brief.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: a flash card (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\nTHE TASK.\nwork\n")), 0o600))
	run("add", "--stream", "s1", "s1-1", "--one", "--brief-file", brief)
	run("fleet", "beat", "m1")
	run("start")
	run("tick")
	run("tick")
	run("take", "--as", "m1", "--max", "1")
	run("finish", "--as", "m1", "s1-1.w1@1", "--epoch", "0", "--report", "done", "--head", head)
	for _, rd := range []string{"reader-a", "reader-b", "reader-c"} {
		run("queue", "--as", rd)
	}
	run("tick") // attempt 1 moves to review and its read is asked
	run("rework", "s1-1", "--fix", "'answer the finding'")
	run("tick")
	run("take", "--as", "m1", "--max", "1")
	run("finish", "--as", "m1", "s1-1.w2@1", "--epoch", "0", "--report", "done", "--head", head)
	run("tick") // attempt 2 moves to review and its read is asked
	delete(*tips, "sprint/pushed")

	var card cardView
	require.NoError(t, json.Unmarshal([]byte(run("card", "s1-1", "--json")), &card))
	var live *sprint.Card
	for _, rc := range card.Reads {
		if rc.Col == sprint.Asked || rc.Col == sprint.Reading {
			live = rc
		}
	}
	require.NotNil(t, live, "the fixture: an attempt-2 read is out: %s", run("card", "s1-1"))
	// the read closes broken on a branch origin does not hold (tips holds none)
	run("read", "--as", live.F("reader"), "--broken", live.ID, "--epoch", "0", "--finding", "'STEP 2: the branch this read names is not on origin; there is no commit to judge'")
	m := regexp.MustCompile(`alias=(j\d+)`).FindStringSubmatch(grepLine(run("inbox"), "read's branch is not on origin"))
	require.NotNil(t, m, "the fixture: the judgment is open: %s", run("inbox"))
	return ta, m[1], head
}

func grepLine(s, sub string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

// TestAMissingReadBranchOnAStore reproduces the three refusals of 2026-10-15 on a real store
// (rework --answers "resolves no obligation", ask --another "not asked yet", ask "asked
// already") and holds the fix: each verb the judgment's decisions name resolves it, and the
// two ask refusals are one line that names the judgment and the verbs that resolve it
// (tla/ReadBranchMissing.tla, OfferedDecisionResolves and AskRefusalsAgree).
func TestAMissingReadBranchOnAStore(t *testing.T) {
	t.Parallel()
	t.Run("rework --answers closes it", func(t *testing.T) {
		t.Parallel()
		var tips map[string]string
		ta, j, _ := missingBranchStore(t, &tips)
		out, code, errb := ta.runVerbOut("rework", "s1-1", "--fix", "'push the branch the work names'", "--answers", j)
		require.Zero(t, code, "rework --answers %s: %s%s", j, out, errb)
		require.Contains(t, ta.runVerb("inbox"), "judgments=0", "rework closed the judgment")
	})
	t.Run("ask and ask --another refuse in one voice until a branch holds the head", func(t *testing.T) {
		t.Parallel()
		var tips map[string]string
		ta, j, head := missingBranchStore(t, &tips)
		out, code, plain := ta.runVerbOut("ask", "s1-1")
		require.NotZero(t, code, "ask s1-1 must be refused: %s%s", out, plain)
		out, code, another := ta.runVerbOut("ask", "s1-1", "--another")
		require.NotZero(t, code, "ask s1-1 --another must be refused: %s%s", out, another)
		require.NotContains(t, plain, "asked already", "the live refusal of plain ask")
		require.NotContains(t, another, "not asked yet", "the live refusal of ask --another")
		require.Contains(t, plain, "judgment "+j)
		require.Contains(t, plain, "rework")
		require.Contains(t, plain, "drop")
		require.Equal(t, strings.TrimSpace(plain), strings.TrimSpace(another), "one state, one reason")
		require.Contains(t, ta.runVerb("inbox"), "JUDGMENT", "refused, the judgment stays open")

		// the seat pushes the work's head to a branch of the work: the ask asks the read again
		tips["sprint/s1-1.w2.g9.e0"] = head
		out, code, errb := ta.runVerbOut("ask", "s1-1", "--answers", j)
		require.Zero(t, code, "ask --answers %s: %s%s", j, out, errb)
		require.Contains(t, ta.runVerb("inbox"), "judgments=0", "the ask closed the judgment")
		var card cardView
		require.NoError(t, json.Unmarshal([]byte(ta.runVerb("card", "s1-1", "--json")), &card))
		live := 0
		for _, rc := range card.Reads {
			if rc.Col == sprint.Asked || rc.Col == sprint.Reading {
				live++
			}
		}
		require.Equal(t, 1, live, "the read is asked again at the card's attempt")
	})
}
