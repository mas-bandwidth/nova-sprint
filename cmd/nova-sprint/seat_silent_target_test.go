package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
)

// The owner escalation needs a target that exists: an owner that is no friend or
// machine row is warned of at start and sent nothing; a known owner is sent from
// the server's machine row. The bus is a fake: no socket, time passed in.
func TestTheOwnerEscalationNeedsAKnownTarget(t *testing.T) {
	t.Parallel()

	// an owner that is no row: the start gate warns and tellSeatSilent sends nothing
	b := &tokenBus{}
	ta, st, _ := seatSilentRig(t, b)
	ctx := context.Background()
	var errBuf bytes.Buffer
	warns, code := ta.a.startSettingsGate(ctx, st, &errBuf)
	require.Equal(t, 0, code)
	joined := strings.Join(warns, "\n") + errBuf.String()
	assert.Contains(t, joined, "WARN owner escalation: glenn is not a nova-config friend or machine row: the owner will not be told; set init --owner <row>", "the start gate warns: %v", warns)
	ta.a.tellSeatSilent(ctx, st)
	assert.Empty(t, b.attempts, "nothing is sent to a name that is no row")

	// the owner a friend row: the message's From is the server's machine row
	b2 := &tokenBus{}
	ta2 := newTestApp(t)
	ta2.a.hostNameFn = func() string { return "m1" }
	ta2.ok("init --readers reader-a,reader-b,reader-c --members m1 --owner amy")
	st2 := &store.Store{B: ta2.m, Names: sprint.Names{}, Now: ta2.a.now, Actor: "coordinator"}
	_, _, _, err := st2.SyncFriends(ctx, []store.FriendSpec{{Name: "amy", Width: 2, Class: "pro"}})
	require.NoError(t, err)
	// friend seats are read from the store's friend rows; amy is a friend row there
	_, err = st2.Run(ctx, store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NSeatSilent, StreamLevel: true,
		Primaries: []string{"s1-1"}, What: "the seat is not answering", Decisions: []string{sprint.SeatSilentDecision}}))
	require.NoError(t, err)
	ta2.a.bus = b2.send
	ta2.a.tellSeatSilent(ctx, st2)
	require.Len(t, b2.delivered, 1, "a known owner is told: %v", b2.attempts)
	assert.Equal(t, []string{"amy"}, b2.delivered[0].To)
	assert.Equal(t, "m1", b2.delivered[0].From, "the sender is the server's machine row, not nova-sprint")
	var _ = bus.KindBlocker
}

// An owner that is a row but a server host that is no fleet row: nothing is
// sent (the sender may not be guessed), and an unread snapshot verifies
// nothing (Stella's #211 finding 1).
func TestTheOwnerEscalationNeedsAKnownSenderAndAReadSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// unknown sender: the host names no fleet row
	b := &tokenBus{}
	ta, st, _ := seatSilentKnownRig(t, b)
	ta.a.hostNameFn = func() string { return "nowhere" }
	ta.a.tellSeatSilent(ctx, st)
	assert.Empty(t, b.attempts, "a sender that is no fleet row sends nothing")
	sender, warn := escalationTarget("glenn", nil, "m1")
	assert.Empty(t, sender, "an unread snapshot gives no sender")
	assert.Contains(t, warn, "could not be read")
	_, warn = escalationTarget("glenn", &sprint.Snapshot{}, "m1")
	assert.Contains(t, warn, "WARN owner escalation", "an empty snapshot verifies no owner")
}

// Delivery recovers on its own when the owner row appears, and the warning
// is said once a raise, not at every look (Stella's #211 findings 1 and 3).
func TestTheOwnerEscalationRecoversWhenTheRowAppears(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := &tokenBus{}
	ta, st, _ := seatSilentRig(t, b)
	var log bytes.Buffer
	ta.a.serveLog = &log
	ta.a.tellSeatSilent(ctx, st)
	ta.pastTheLook()
	ta.a.tellSeatSilent(ctx, st)
	assert.Empty(t, b.attempts, "nothing is sent while the owner is no row")
	assert.Equal(t, 1, strings.Count(log.String(), "WARN owner escalation"), "the warning is said once a raise: %q", log.String())
	_, _, _, err := st.SyncFriends(ctx, []store.FriendSpec{{Name: "glenn", Width: 2, Class: "pro"}})
	require.NoError(t, err)
	ta.pastTheLook()
	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, b.delivered, 1, "delivery resumes once the owner row appears")
	assert.Equal(t, "m1", b.delivered[0].From, "from the server's machine row")
}
