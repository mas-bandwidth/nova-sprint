package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinishRefusesHeadNotOnOrigin(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	r := newServerRig(t,
		"nova-sprint init --members m1:1",
		"set --read-cards off",
		"nova-sprint add --one --stream s1 --count 1 --brief-file "+writeBrief(t, "tier: flash\nREPO: mas-bandwidth/nova-tools"),
		"nova-sprint start",
		"nova-sprint tick",
	)
	dealOriginFixture(t, r, 1)
	card := taken(t, r.one("take", "--as", "m1", "--limit", "1", "--epoch", "0", "--json"))[0]
	branch := "sprint/s1-1.w1.g1.e0"
	listed := false
	r.a.readHeads = func(_ context.Context, repo, pattern string) (map[string]string, error) {
		assert.Equal(t, friendRepo, repo)
		assert.Equal(t, "refs/heads/*", pattern)
		if listed {
			return map[string]string{"alternate-branch": head}, nil
		}
		return map[string]string{}, nil
	}
	refused := r.one("finish", "--as", "m1", card, "--epoch", "0", "--head", head, "--branch", branch)
	require.Equal(t, 1, refused.Code, refused.Stderr)
	assert.Contains(t, refused.Stderr, head)
	assert.Contains(t, refused.Stderr, branch)
	assert.Contains(t, refused.Stderr, "git push origin HEAD:refs/heads/"+branch)
	assert.Len(t, r.queue("m1")["working"], 1, "a refused finish remains with its worker")

	listed = true
	accepted := r.one("finish", "--as", "m1", card, "--epoch", "0", "--head", head, "--branch", branch)
	require.Equal(t, 0, accepted.Code, accepted.Stderr)
	assert.Contains(t, accepted.Stdout, "working -> done ok")
	assert.Empty(t, r.queue("m1")["working"])
}

func TestFriendCollectionRefusesHeadLookupFailure(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"collect", "friend sync"} {
		t.Run(verb, func(t *testing.T) {
			const head = "fedcba9876543210fedcba9876543210fedcba98"
			ta, root := friendCardApp(t, "friend amy", "amy")
			ta.a.tip = tipIs(t, head) // friendFinish's existing branch-tip check passes
			ta.a.readHeads = func(context.Context, string, string) (map[string]string, error) {
				return nil, errors.New("origin unavailable")
			}
			ta.ok("tick")
			ta.startFriend("amy", 1)
			outboxReport(t, root, "amy", "s1-1.w1", "Verdict: LAND\nHead: "+head+"\n\nPushed.\n")
			out := ta.ok(verb + " --root " + root)
			assert.Contains(t, out, "REFUSED")
			assert.Contains(t, out, "origin unavailable")
			assert.Contains(t, out, "retry the finish")
			assert.NotContains(t, out, "git push")
			var c cardView
			ta.json("card s1-1", &c)
			assert.Equal(t, "working", c.Primary.Col, "failed origin lookup leaves the finish with its worker")
			ta.a.readHeads = func(context.Context, string, string) (map[string]string, error) {
				return map[string]string{"sprint/s1-1.w1.g1.e0": head}, nil
			}
			accepted := ta.ok(verb + " --root " + root)
			assert.NotContains(t, accepted, "REFUSED")
			if verb == "collect" {
				assert.Contains(t, accepted, "landed=1")
			} else {
				assert.Contains(t, accepted, "FINISHED")
			}
			ta.ok("tick")
			ta.json("card s1-1", &c)
			assert.Equal(t, "review", c.Primary.Col)
		})
	}
}

func TestFinishOriginValidatesSuccessfulRepositoryHeadsOnly(t *testing.T) {
	for _, tc := range []struct {
		name, brief, head    string
		failed, stale, noGit bool
		wantCode, wantCalls  int
	}{
		{name: "empty", brief: "REPO: mas-bandwidth/nova-tools", wantCode: 1},
		{name: "short", brief: "REPO: mas-bandwidth/nova-tools", head: "0123456", wantCode: 1},
		{name: "failed", brief: "REPO: mas-bandwidth/nova-tools", failed: true},
		{name: "stale", brief: "REPO: mas-bandwidth/nova-tools", head: "0123456789abcdef0123456789abcdef01234567", stale: true, wantCode: 1},
		{name: "no repository", head: "legacy"},
		{name: "malformed repository", brief: "REPO: not-a-repository", wantCode: 1},
		{name: "no git", brief: "REPO: mas-bandwidth/nova-tools", noGit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newServerRig(t, "init --members m1:1", "set --read-cards off", "add --one --stream s1 --count 1 --brief-file "+writeBrief(t, "tier: flash\n"+tc.brief), "start", "tick")
			dealOriginFixture(t, r, 1)
			card := taken(t, r.one("take", "--as", "m1", "--limit", "1", "--epoch", "0", "--json"))[0]
			calls := 0
			if !tc.noGit {
				r.a.readHeads = func(context.Context, string, string) (map[string]string, error) {
					calls++
					return nil, errors.New("must not look up")
				}
			}
			args := []string{"finish", "--as", "m1", card, "--epoch", "0", "--head", tc.head}
			if tc.failed {
				args = append(args, "--failed", "--report", "tests failed")
			}
			if tc.stale {
				args[5] = "99"
			}
			out := r.one(args...)
			assert.Equal(t, tc.wantCode, out.Code, out.Stderr)
			assert.Equal(t, tc.wantCalls, calls)
			if tc.name == "empty" || tc.name == "short" {
				assert.Contains(t, out.Stderr, "full commit SHA")
				assert.Len(t, r.queue("m1")["working"], 1)
			}
		})
	}
}

func TestFinishOriginRefusalIsAtomicAcrossCards(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	r := newServerRig(t, "init --members m1:2", "set --read-cards off", "add --one --stream s1 --count 2 --brief-file "+writeBrief(t, "tier: flash\nREPO: mas-bandwidth/nova-tools"), "start", "tick")
	dealOriginFixture(t, r, 2)
	cards := taken(t, r.one("take", "--as", "m1", "--limit", "2", "--epoch", "0", "--json"))
	require.Len(t, cards, 2)
	calls := 0
	r.a.readHeads = func(context.Context, string, string) (map[string]string, error) {
		calls++
		if calls == 1 {
			return map[string]string{"other": head}, nil
		}
		return nil, nil
	}
	out := r.one("finish", "--as", "m1", cards[0], cards[1], "--epoch", "0", "--head", head)
	require.Equal(t, 1, out.Code, out.Stderr)
	assert.Len(t, r.queue("m1")["working"], 2)
	assert.Equal(t, 2, calls)
}

func TestFinishOriginChecksPlannedPassedReworkHead(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.ok("add --one --stream s1 --count 1 --brief-file " + writeBrief(t, "tier: flash\nREPO: mas-bandwidth/nova-tools"))
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	snap, err := st.Load(context.Background(), store.All, nil)
	require.NoError(t, err)
	c := snap.Work.Card("s1-1")
	require.NotNil(t, c)
	_, err = ta.m.Apply(context.Background(), ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: "0",
		ExpectedTableRevision: fmt.Sprint(snap.Work.Revision), OperationID: "passed-head-fixture",
		Members: []ntable.BatchMemberEntry{{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: map[string]string{sprint.FieldPassedHead: head}}}})
	require.NoError(t, err)
	present := false
	calls := 0
	ta.a.readHeads = func(context.Context, string, string) (map[string]string, error) {
		calls++
		if present {
			return map[string]string{"earlier-attempt": head}, nil
		}
		return nil, nil
	}
	command := "finish --as m1 s1-1.w1@1 --failed --report '" + cardhdr.EndNothing + ": already done'"
	code, _, stderr := ta.do(command)
	require.Equal(t, 1, code, stderr)
	assert.Contains(t, stderr, head)
	require.Equal(t, 1, calls)
	present = true
	assert.Contains(t, ta.ok(command), "done ok")
	assert.Equal(t, 2, calls)
}

func TestFinishOriginLookupErrorPreservesCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("origin temporarily unavailable")
	a := &app{readHeads: func(context.Context, string, string) (map[string]string, error) { return nil, cause }}
	err := a.finishOrigin(context.Background(), "example/repo", "sprint/card", "0123456789abcdef0123456789abcdef01234567")
	require.ErrorIs(t, err, cause)
	assert.NotContains(t, err.Error(), "held by no branch")
	assert.NotContains(t, err.Error(), "git push")
}

func dealOriginFixture(t *testing.T, r *serverRig, n int) {
	t.Helper()
	st, err := r.a.store(common{redis: r.a.serveAddr, actor: "boss"})
	require.NoError(t, err)
	res, err := st.Run(context.Background(), dealStep(sprint.DealReq{Sel: sprint.Sel{Limit: n}}))
	require.NoError(t, err)
	require.Empty(t, res.Refused)
}
