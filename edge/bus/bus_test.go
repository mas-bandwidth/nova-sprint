package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEdgeSendAndWhoOverFakeExec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fex := edge.NewFakeExec()
	assert.Equal(t, int64(1000), fex.Now())

	b := New(Config{Exec: fex})

	// 1. Confirmed send parses `<to> <id>`.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 0,
		Stdout:   "amd-1.ds 1794000001001-0\n",
	})
	id, err := b.Send(ctx, "amd-1.ds", SubjectDeal, "DEAL: 1 card for amd-1.ds (epoch 1)")
	require.NoError(t, err)
	assert.Equal(t, "1794000001001-0", id)

	// 2. Exit 0 without `<to> <id>` confirmation is refused so cursor never moves.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 0,
		Stdout:   "wrong-recipient 1794000001002-0\n",
	})
	_, err = b.Send(ctx, "amd-1.ds", SubjectDeal, "DEAL: 1 card for amd-1.ds (epoch 1)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing `<recipient> <id>` confirmation")

	// 3. Non-zero exit (`SEND FAILED`) is refused naming the fix.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 1,
		Stderr:   "SEND FAILED: sprint not hooked in\n",
	})
	_, err = b.Send(ctx, "amd-1.ds", SubjectDeal, "DEAL: 1 card for amd-1.ds (epoch 1)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nova-bus exited 1")
	assert.Contains(t, err.Error(), "nova-bus who")

	// 4. Who parses present rows and optional detail column.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 0,
		Stdout:   "WHO OK present=2\namd-1.ds\t14s\nrowan\t5m\trecv holding 99-0\n",
	})
	who, err := b.Who(ctx)
	require.NoError(t, err)
	require.Len(t, who, 2)
	assert.Equal(t, Presence{Name: "amd-1.ds", Uptime: "14s"}, who["amd-1.ds"])
	assert.Equal(t, Presence{Name: "rowan", Uptime: "5m", Detail: "recv holding 99-0"}, who["rowan"])

	// 5. BusReply never parses message bodies and points to `nova-sprint next --as <row>`.
	assert.Equal(t, "replies are verbs: run nova-sprint next --as amd-1.ds", BusReply("amd-1.ds"))
}

func seedBusTestStore_internal(t *testing.T) (*store.Fake, time.Time) {
	t.Helper()
	ctx := context.Background()
	st := store.NewFake()
	t0 := time.Unix(1000, 0).UTC()

	headA := strings.Repeat("a", 40)
	headB := strings.Repeat("b", 40)
	longFinding := strings.Repeat("x", 340)
	longQuestion := strings.Repeat("q", 350)

	apply := func(id string, writes ...store.Write) {
		t0 = t0.Add(time.Second)
		rec, err := st.Apply(ctx, store.Op{ID: id, Epoch: 1, Actor: "sprint", Kind: "test", At: t0, Writes: writes})
		require.NoError(t, err)
		require.True(t, rec.Accepted, "op %s refused: %s", id, rec.Reason)
	}

	apply("op-init",
		store.PutStream{Name: "s1", Repo: "repo", Base: "main", Pos: 1},
		store.PutRow{Name: "amd-1.ds"},
		store.SetRowState{Row: "amd-1.ds", State: "up"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"},
	)
	apply("op-prep-p1",
		store.SetCardFinding{Card: "p1", Finding: longFinding},
		store.SetCardReview{Card: "p1"},
	)

	// Deal work retry consumer and read consumer to amd-1.ds, plus w0 which will be withdrawn.
	apply("op-deals",
		store.PutCard{ID: "p1.w1", Kind: "work", Of: "p1", Stream: "s1", Holder: "amd-1.ds", Tier: "pro", Attempt: 1, Finding: longFinding},
		store.SetCardBranch{Card: "p1.w1", Branch: "sprint/s1/p1"},
		store.SetCardState{Card: "p1.w1", State: "working"},
		store.PutCard{ID: "p1.w2", Kind: "work", Of: "p1", Stream: "s1", Holder: "amd-1.ds", Tier: "pro", Attempt: 2, Finding: longFinding},
		store.SetCardPriorHead{Card: "p1.w2", Head: headA},
		store.SetCardState{Card: "p1.w2", State: "ready"},
		store.PutCard{ID: "p1.r1.1", Kind: "read", Of: "p1", Stream: "s1", Holder: "amd-1.ds", Tier: "pro", Attempt: 2, Review: 1},
		store.SetCardHead{Card: "p1.r1.1", Head: headB},
		store.SetCardState{Card: "p1.r1.1", State: "ready"},
	)

	// Withdraw p1.w1 and open two grouped judgements + land p1.
	apply("op-withdraw-judge-land",
		store.SetCardState{Card: "p1.w1", State: "fail"},
		store.SetCardReason{Card: "p1.w1", Reason: "preempted by blocker"},
		store.PutJudgement{ID: "j-1", Kind: "red", About: "p1", Group: "grp-red", Question: longQuestion},
		store.PutJudgement{ID: "j-2", Kind: "red", About: "p1", Group: "grp-red", Question: "second in group"},
		store.SetCardHead{Card: "p1", Head: headB},
		store.SetCardState{Card: "p1", State: "landed"},
	)
	return st, t0
}

func TestPushTickAllSubjectsAndCursorDiscipline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, now := seedBusTestStore_internal(t)
	fb := NewFake()
	assert.Equal(t, int64(1000), fb.Now())

	b := New(Config{Transport: fb, Coordinator: "rowan"})
	delivered, err := b.PushTick(ctx, st, "tick1", now.Add(time.Second))
	require.NoError(t, err)
	require.Len(t, delivered, 5)

	byKey := make(map[string]SentMessage)
	for _, m := range fb.Sent() {
		byKey[m.To+"/"+m.Subject] = m
	}

	// 1. DEAL to amd-1.ds includes both retry and read consumer deals and truncates finding at 300 bytes.
	deal := byKey["amd-1.ds/DEAL"]
	assert.Contains(t, deal.Body, "DEAL: 2 cards for amd-1.ds (epoch 1)")
	assert.Contains(t, deal.Body, "prior="+strings.Repeat("a", 40))
	assert.Contains(t, deal.Body, "full text: nova-sprint show p1.w2")
	assert.Contains(t, deal.Body, "kind=read head="+strings.Repeat("b", 40))
	assert.Contains(t, deal.Body, "run: nova-sprint take p1.w2@")

	// 2. WITHDRAWN to amd-1.ds names consumer, producer, branch, reason, and stop instruction.
	withdrawn := byKey["amd-1.ds/WITHDRAWN"]
	assert.Contains(t, withdrawn.Body, "card=p1.w1 of=p1 branch=sprint/s1/p1 reason=\"preempted by blocker\"")
	assert.Contains(t, withdrawn.Body, "stop; push committed work")

	// 3. NEEDS YOU to rowan groups j-1 and j-2 by group_key with n=2 size=2 and truncates question at 300 bytes.
	needsYou := byKey["rowan/NEEDS YOU"]
	assert.Contains(t, needsYou.Body, "group=grp-red n=2 size=2 ids=j-1,j-2")
	assert.Contains(t, needsYou.Body, "full text: nova-sprint show j-1")
	assert.Contains(t, needsYou.Body, "run: nova-sprint judge j-1 <choice> --reason \"<why>\"")

	// 4. LANDED and STREAM LANDED to rowan.
	landed := byKey["rowan/LANDED"]
	assert.Contains(t, landed.Body, "LANDED card=p1 stream=s1 head="+strings.Repeat("b", 40))
	assert.Contains(t, landed.Body, "run: nova-sprint show p1")

	streamLanded := byKey["rowan/STREAM LANDED"]
	assert.Contains(t, streamLanded.Body, "STREAM LANDED stream=s1 base=main head="+strings.Repeat("b", 40)+" cards=1")
	assert.Contains(t, streamLanded.Body, "run: nova-sprint show s1 --stitch")

	// 5. Cursors advanced in store; a second PushTick sends nothing.
	snap, err := st.Snapshot(ctx)
	require.NoError(t, err)
	assert.Greater(t, snap.Cursors[CursorName("amd-1.ds")], int64(0))
	assert.Greater(t, snap.Cursors[CursorName("rowan")], int64(0))

	again, err := b.PushTick(ctx, st, "tick2", now.Add(2*time.Second))
	require.NoError(t, err)
	assert.Empty(t, again)
}

func TestPushCursorFailureAndLostReplyRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, now := seedBusTestStore_internal(t)
	fb := NewFake()
	b := New(Config{Transport: fb, Coordinator: "rowan"})

	// 1. Send fails for amd-1.ds on first tick -> amd-1.ds cursor stays at 0 while rowan still advances.
	fb.FailNextSend(errors.New("SEND FAILED"))
	deliv1, err := b.PushTick(ctx, st, "tick-fail", now.Add(time.Second))
	require.Error(t, err)
	assert.Len(t, deliv1, 3)

	snap, err := st.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), snap.Cursors[CursorName("amd-1.ds")])
	assert.Greater(t, snap.Cursors[CursorName("rowan")], int64(0))

	// 2. Reply lost after message reaches wire -> amd-1.ds cursor stays at 0, next tick resends identically.
	fb.DropNextReply(1)
	_, err = b.PushTick(ctx, st, "tick-drop", now.Add(2*time.Second))
	require.Error(t, err)
	snap, err = st.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), snap.Cursors[CursorName("amd-1.ds")])
	require.Len(t, fb.WireMessages(), 4)

	// 3. Recovery tick succeeds and advances amd-1.ds cursor.
	delivered, err := b.PushTick(ctx, st, "tick-ok", now.Add(3*time.Second))
	require.NoError(t, err)
	assert.Len(t, delivered, 2)
	snap, err = st.Snapshot(ctx)
	require.NoError(t, err)
	assert.Greater(t, snap.Cursors[CursorName("amd-1.ds")], int64(0))
	assert.Greater(t, snap.Cursors[CursorName("rowan")], int64(0))
}

func TestDeal20CardAnd4KBCapUsesNextVerb(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.NewFake()
	t0 := time.Unix(1000, 0).UTC()

	writes := []store.Write{
		store.PutStream{Name: "s1", Repo: "repo", Base: "main", Pos: 1},
		store.PutRow{Name: "amd-1.ds"},
		store.SetRowState{Row: "amd-1.ds", State: "up"},
	}
	for i := 1; i <= 25; i++ {
		pID := fmt.Sprintf("p-%02d", i)
		wID := fmt.Sprintf("p-%02d.w1", i)
		writes = append(writes,
			store.PutCard{ID: pID, Kind: "producer", Stream: "s1", Tier: "pro"},
			store.PutCard{ID: wID, Kind: "work", Of: pID, Stream: "s1", Holder: "amd-1.ds", Tier: "pro", Attempt: 1},
			store.SetCardState{Card: wID, State: "ready"},
		)
	}
	rec, err := st.Apply(ctx, store.Op{
		ID: "op-25-deals", Epoch: 1, Actor: "sprint", Kind: "deal", At: t0, Writes: writes,
	})
	require.NoError(t, err)
	require.True(t, rec.Accepted)

	fb := NewFake()
	b := New(Config{Transport: fb, Coordinator: "rowan"})
	delivered, err := b.PushTick(ctx, st, "tick-cap", t0.Add(time.Second))
	require.NoError(t, err)
	require.Len(t, delivered, 1)

	body := delivered[0].Message.Body
	assert.LessOrEqual(t, len(body), MaxMessageBytes)
	assert.Contains(t, body, "and 5 more: nova-sprint next --as amd-1.ds")
	assert.NotContains(t, body, "nova-sprint queue")
}

func TestPushTickSkipsUnhookedRecipients(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, now := crSeed_internal(t, "emma", "stella")
	rec, err := st.Apply(ctx, store.Op{
		ID: "seed-hook", Epoch: 1, Actor: "rowan", Kind: "deal", At: now,
		Writes: []store.Write{
			store.PutRow{Name: "emma", Session: "worker:1", Role: "worker"},
			store.PutCard{ID: "c1", Stream: "s1", Kind: "producer", Tier: "pro"},
			store.PutCard{ID: "c1.w1", Of: "c1", Stream: "s1", Kind: "work", Holder: "emma", Tier: "pro", Attempt: 1},
			store.SetCardState{Card: "c1.w1", State: "ready"},
			store.PutCard{ID: "c2", Stream: "s1", Kind: "producer", Tier: "pro"},
			store.PutCard{ID: "c2.w1", Of: "c2", Stream: "s1", Kind: "work", Holder: "stella", Tier: "pro", Attempt: 1},
			store.SetCardState{Card: "c2.w1", State: "ready"},
		},
	})
	require.NoError(t, err)
	require.True(t, rec.Accepted)
	fb := NewFake()
	e := New(Config{Transport: fb, Coordinator: "rowan", RequireHooked: true})
	deliv, err := e.PushTick(ctx, st, "tick-hook", now)
	require.NoError(t, err)
	require.Len(t, deliv, 1)
	assert.Equal(t, "emma", deliv[0].Message.To)
}
