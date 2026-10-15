package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
)

// tokenBus is the owner's bus as pkg/bus answers a token (pkg/bus/token.go): a send under a
// token it has seen writes no second message. fails is how many sends fail before one goes.
type tokenBus struct {
	fails     int
	attempts  []bus.Message
	delivered []bus.Message
	seen      map[string]bool
}

func (b *tokenBus) send(_ context.Context, m bus.Message, _ func(string)) error {
	b.attempts = append(b.attempts, m)
	if b.fails > 0 {
		b.fails--
		return errors.New("the bus store is down")
	}
	if m.Token != "" && b.seen[m.Token] {
		return nil // the bus answers the message the token's record names: none written
	}
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	b.seen[m.Token] = true
	b.delivered = append(b.delivered, m)
	return nil
}

// seatSilentRig is a sprint with an owner and one open "the seat is not answering", the
// app's bus the token bus.
func seatSilentRig(t *testing.T, b *tokenBus) (*testApp, *store.Store, string) {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b,reader-c --members m1 --owner glenn")
	ta.a.bus = b.send
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "coordinator"}
	_, err := st.Run(context.Background(), store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NSeatSilent, StreamLevel: true,
		Primaries: []string{"s1-1"}, What: "the seat is not answering: 1 judgments hold 1 cards past their deadline", Decisions: []string{sprint.SeatSilentDecision}}))
	require.NoError(t, err)
	groups, err := st.OpenGroups(context.Background())
	require.NoError(t, err)
	for _, g := range groups {
		if g.Type == sprint.NSeatSilent {
			return ta, st, g.ID
		}
	}
	t.Fatal("no open seat-not-answering judgment")
	return nil, nil, ""
}

// pastTheLook moves the app's clock past the look's throttle.
func (ta *testApp) pastTheLook() {
	ta.mu.Lock()
	ta.now = ta.now.Add(seatSilentLook + time.Second)
	ta.mu.Unlock()
}

// A raise is told only once its send succeeded (Stella's read of #123): a send that fails is
// tried again at the next look, under the same token, and the owner gets the one message.
// Before, the raise was marked told before the send, and a failed send was never tried again.
func TestASeatSilentSendThatFailsIsTriedAgainUnderOneToken(t *testing.T) {
	t.Parallel()
	b := &tokenBus{fails: 1}
	ta, st, id := seatSilentRig(t, b)
	ctx := context.Background()

	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, b.attempts, 1, "the first look sends")
	require.Empty(t, b.delivered, "and the send failed")

	ta.pastTheLook()
	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, b.attempts, 2, "the next look tries again")
	require.Len(t, b.delivered, 1, "and the owner has it")
	es, err := st.EpochNow(ctx)
	require.NoError(t, err)
	want := seatSilentToken("", es.N, id, 0)
	assert.Equal(t, want, b.attempts[0].Token, "the raise's token: sprint, epoch, judgment, raise")
	assert.Equal(t, want, b.attempts[1].Token, "the retry is the same send")
	assert.Equal(t, []string{"glenn"}, b.delivered[0].To)

	ta.pastTheLook()
	ta.a.tellSeatSilent(ctx, st)
	assert.Len(t, b.attempts, 2, "a raise told is not sent again")
}

// A server restarted after a send (its told marks lost) sends the raise again under the same
// token, and the bus writes no second message: the owner is told once. Before, the send
// carried no token, and every restart told the owner again.
func TestARestartedServerTellsTheOwnerOfARaiseOnce(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta, st, _ := seatSilentRig(t, b)
	ctx := context.Background()
	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, b.delivered, 1)

	ta.a.seatSilent = seatSilentTold{} // the restart: the process's marks are gone
	ta.pastTheLook()
	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, b.attempts, 2, "the restarted server sends the raise again")
	assert.Equal(t, b.attempts[0].Token, b.attempts[1].Token, "under the one token")
	assert.NotEmpty(t, b.attempts[1].Token)
	assert.Len(t, b.delivered, 1, "the bus wrote one message: the owner is told once")
}

// The marks are of open raises only: a raise whose judgment closed, or that was raised again
// since, is forgotten at the next look (the coordinator's read of #123: told grew without
// bound).
func TestASeatSilentRaiseNoLongerOpenIsForgotten(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta, st, _ := seatSilentRig(t, b)
	ctx := context.Background()
	ta.a.seatSilent.told = map[string]bool{seatSilentToken("", 0, "gone", 3): true}
	ta.a.tellSeatSilent(ctx, st)
	ta.a.seatSilent.mu.Lock()
	defer ta.a.seatSilent.mu.Unlock()
	assert.Len(t, ta.a.seatSilent.told, 1, "only the open raise is kept: %v", ta.a.seatSilent.told)
	assert.False(t, ta.a.seatSilent.told[seatSilentToken("", 0, "gone", 3)], "the closed raise is forgotten")
}

func TestSeatSilentRequiresMatchingOwnerReceipt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		receipt string
		want    int
	}{
		{"no receipt", "", 1},
		{"matching receipt", "match", 0},
		{"old A receipt does not cover B", "old", 1},
		{"substring is not a receipt", "substring", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &tokenBus{}
			ta, st, _ := seatSilentRig(t, b)
			ctx := context.Background()
			_, _, _, err := st.SetMachine(ctx, true)
			require.NoError(t, err)
			_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBound, Primaries: []string{"s1-1"}, At: ta.a.now()}))
			require.NoError(t, err)
			_, err = st.Run(ctx, store.NoteStep("earlier A", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBriefWrong, Primaries: []string{"s1-1"}, At: ta.a.now()}))
			require.NoError(t, err)
			all, err := st.B.OpenNotes(ctx)
			require.NoError(t, err)
			id, earlierID := "", ""
			for _, o := range all {
				if o.Note.Type == sprint.NBound {
					id = o.Note.ID
				}
				if o.Note.Type == sprint.NBriefWrong {
					earlierID = o.Note.ID
				}
			}
			require.NotEmpty(t, id)
			require.NotEmpty(t, earlierID)
			// Unrelated overdue judgments must not prevent suppression for this group.
			_, err = st.Run(ctx, store.NoteStep("unrelated", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "unrelated", At: ta.a.now()}))
			require.NoError(t, err)
			ta.step(sprint.DeadlineJudgment)
			if tc.receipt != "" {
				set := id + "\x00" + earlierID
				if tc.receipt == "old" {
					set = earlierID
				}
				if tc.receipt == "substring" {
					set = "prefix-" + id
				}
				_, err = st.Run(ctx, store.NoteStep("receipt", sprint.Note{Kind: sprint.Acknowledged, SprintLevel: true, Type: sprint.NDecisionDelivery, DecisionDelivery: &sprint.DecisionDelivery{Lane: "owner", Set: set, SentAt: ta.a.now()}}))
				require.NoError(t, err)
			}
			ta.a.tellSeatSilent(ctx, st)
			require.Len(t, b.delivered, tc.want)
		})
	}
}
