package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recoveryHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1, Class: "flash"}, {Name: "bob", Width: 1, Class: "flash"}})
	require.NoError(t, err)
	h.up("amy")
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: "Task.\nREPO: mas-bandwidth/nova-tools\nWHO: friend\n\nTask."}, {ID: "s1-2", Brief: "Task.\nREPO: mas-bandwidth/nova-tools\nWHO: friend\n\nTask."}}}))
	h.startMachine()
	h.machine()
	require.Equal(t, sprint.FriendRow("amy"), h.snap().Fleet.Card("s1-1.w1").Row)
	return h
}

func downAmy(h *harness) {
	h.t.Helper()
	h.tick(time.Second)
	_, _, _, err := h.health("amy", "tester", sprint.Down, h.now, 1)
	require.NoError(h.t, err)
	require.Equal(h.t, sprint.Down, h.friendStatus("amy"))
}

func TestStoreTickRecoversAllDownCardsAndCarriesBoundHead(t *testing.T) {
	t.Parallel()
	h := recoveryHarness(t)
	h.start("amy", 1)
	before := h.snap().Fleet.Card("s1-1.w1").Int("gen")
	_, err := h.st.FriendBeatReport(h.ctx, "amy", sprint.FriendReport{Running: []string{"s1-1"}}, nil)
	require.NoError(t, err, "primary alias must not suppress recovery or its push read")
	downAmy(h)
	h.up("bob")
	head := strings.Repeat("a", 40)
	var requested []sprint.RecoveryHeadRequest
	h.st.RecoveryHeads = func(_ context.Context, rs []sprint.RecoveryHeadRequest) []sprint.RecoveryHead {
		requested = append(requested, rs...)
		var out []sprint.RecoveryHead
		for _, r := range rs {
			f := sprint.RecoveryHead{RecoveryHeadRequest: r}
			if r.Card == "s1-1.w1" {
				f.Head = head
			}
			out = append(out, f)
		}
		return out
	}
	h.machine()
	s := h.snap()
	for _, id := range []string{"s1-1.w1", "s1-2.w1"} {
		c := s.Fleet.Card(id)
		require.Equal(t, sprint.FriendRow("bob"), c.Row)
		require.Equal(t, 3, c.Int("gen"))
		assert.Empty(t, c.F(sprint.FieldRecoveryPending))
		assert.Empty(t, c.F(sprint.FieldTakeEnded))
		assert.Equal(t, 1, s.Work.Card(c.F("primary")).Int("attempt"))
	}
	require.Len(t, requested, 2)
	assert.Equal(t, before, requested[0].Gen)
	assert.Equal(t, 2, requested[0].CurrentGen)
	assert.Equal(t, head, s.Fleet.Card("s1-1.w1").F(sprint.FieldCarryHead))
	assert.Equal(t, "1", s.Fleet.Card("s1-1.w1").F(sprint.FieldCarryGen))
	r := h.run(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{"s1-1.w1"}}, As: sprint.FriendRow("amy"), Gens: map[string]int{"s1-1.w1": before}, Head: head}))
	require.NotEmpty(t, r.Refused)
	assert.Empty(t, r.Moved)
	assert.Empty(t, sprint.Check(h.snap(), nil))
}

func TestStoreMissingHeadReaderRevokesOnceAndLeavesVisibleDebt(t *testing.T) {
	t.Parallel()
	h := recoveryHarness(t)
	downAmy(h)
	first := h.machine()
	for _, id := range []string{"s1-1.w1", "s1-2.w1"} {
		c := h.snap().Fleet.Card(id)
		require.Equal(t, sprint.Withdrawn, c.Col)
		assert.Equal(t, 2, c.Int("gen"))
		assert.NotEmpty(t, c.F(sprint.FieldRecoveryPending))
		assert.Equal(t, sprint.Ready, h.snap().Work.Card(c.F("primary")).Col)
	}
	require.Contains(t, strings.Join(first.Moved(), "\n"), "recovery pending")
	second := h.machine()
	assert.NotContains(t, strings.Join(second.Moved(), "\n"), "withdrawn gen=")
	assert.NotContains(t, strings.Join(second.Moved(), "\n"), "recovery pending")
	assert.Equal(t, 2, h.snap().Fleet.Card("s1-1.w1").Int("gen"))
}

type recoveryProofTrigger struct {
	fired     bool
	failFirst bool
	fn        func()
}
type proofAtRecovery struct {
	*Mem
	trigger *recoveryProofTrigger
}

func (p *proofAtRecovery) AtEpoch(e uint64, old bool) Backend {
	return &proofAtRecovery{Mem: p.Mem.AtEpoch(e, old).(*Mem), trigger: p.trigger}
}
func (p *proofAtRecovery) Acquire(ctx context.Context, g uint64, op OpRecord) (bool, error) {
	if op.Verb == "tick "+sprint.PartFriendRecover && !p.trigger.fired {
		if p.trigger.failFirst {
			p.trigger.failFirst = false
			return false, nil
		}
		p.trigger.fired = true
		p.trigger.fn()
	}
	return p.Mem.Acquire(ctx, g, op)
}
func (p *proofAtRecovery) Relock(ctx context.Context, held string, op OpRecord) (bool, error) {
	if op.Verb == "tick "+sprint.PartFriendRecover && !p.trigger.fired {
		p.trigger.fired = true
		p.trigger.fn()
	}
	return p.Mem.Relock(ctx, held, op)
}

func TestFreshProofBeforeRecoveryReservationPreservesLiveLease(t *testing.T) {
	t.Parallel()
	for _, lock := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquire", true: "relock"}[lock], func(t *testing.T) {
			h := recoveryHarness(t)
			downAmy(h)
			trigger := &recoveryProofTrigger{failFirst: lock, fn: func() {
				b, err := json.Marshal(sprint.Beat{At: h.now, Proof: h.now})
				require.NoError(t, err)
				require.NoError(t, h.m.SetKey(h.ctx, friendBeatKey("amy"), string(b)))
			}}
			h.st.B = &proofAtRecovery{Mem: h.m, trigger: trigger}
			h.st.LockAfterLoss = lock
			step := TickPartStep(sprint.PartFriendRecover, sprint.TickFriendRecover, sprint.TickReq{}, nil, nil, nil)
			step.RecoveryFriends, step.Halts, step.Twin = true, true, h.st.twin()
			result, err := h.st.Run(h.ctx, step)
			require.NoError(t, err)
			require.True(t, trigger.fired)
			assert.Empty(t, result.Moved)
			c := h.snap().Fleet.Card("s1-1.w1")
			assert.Equal(t, 1, c.Int("gen"))
			assert.Equal(t, sprint.Ready, c.Col)
			assert.Equal(t, sprint.Up, h.friendStatus("amy"))
		})
	}
}

type proofAfterReserve struct {
	*Mem
	trigger *recoveryProofTrigger
}

func (p *proofAfterReserve) AtEpoch(e uint64, old bool) Backend {
	return &proofAfterReserve{Mem: p.Mem.AtEpoch(e, old).(*Mem), trigger: p.trigger}
}
func (p *proofAfterReserve) Apply(ctx context.Context, m ntable.BatchManifest) (ntable.Receipt, error) {
	if !p.trigger.fired {
		p.trigger.fired = true
		p.trigger.fn()
	}
	return p.Mem.Apply(ctx, m)
}
func TestFreshProofAfterReservationDoesNotCancelDurableRevocation(t *testing.T) {
	t.Parallel()
	h := recoveryHarness(t)
	downAmy(h)
	trigger := &recoveryProofTrigger{fn: func() {
		b, err := json.Marshal(sprint.Beat{At: h.now, Proof: h.now})
		require.NoError(t, err)
		require.NoError(t, h.m.SetKey(h.ctx, friendBeatKey("amy"), string(b)))
	}}
	h.st.B = &proofAfterReserve{Mem: h.m, trigger: trigger}
	step := TickPartStep(sprint.PartFriendRecover, sprint.TickFriendRecover, sprint.TickReq{}, nil, nil, nil)
	step.RecoveryFriends = true
	r, err := h.st.Run(h.ctx, step)
	require.NoError(t, err)
	require.True(t, trigger.fired)
	require.NotEmpty(t, r.Moved)
	c := h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Withdrawn, c.Col)
	assert.Equal(t, 2, c.Int("gen"))
	assert.Equal(t, sprint.Up, h.friendStatus("amy"))
}

func TestCompletionBeforeRecoveryReservationWins(t *testing.T) {
	t.Parallel()
	h := recoveryHarness(t)
	h.start("amy", 1)
	downAmy(h)
	head := strings.Repeat("b", 40)
	trigger := &recoveryProofTrigger{fn: func() {
		result, err := h.st.Run(h.ctx, FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{"s1-1.w1"}}, As: sprint.FriendRow("amy"), Gens: map[string]int{"s1-1.w1": 1}, Head: head}))
		require.NoError(t, err)
		require.Empty(t, result.Refused)
	}}
	h.st.B = &proofAtRecovery{Mem: h.m, trigger: trigger}
	step := TickPartStep(sprint.PartFriendRecover, sprint.TickFriendRecover, sprint.TickReq{}, nil, nil, nil)
	step.RecoveryFriends = true
	_, err := h.st.Run(h.ctx, step)
	require.NoError(t, err)
	require.True(t, trigger.fired)
	c := h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Finished, c.Col, "completion committed first is not withdrawn")
	assert.Equal(t, "yes", c.F("ok"))
	assert.Equal(t, 1, c.Int("gen"))
	assert.Equal(t, head, c.F("head"))
	assert.Empty(t, c.F(sprint.FieldRecoveryPending))
}
