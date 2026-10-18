package main

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
)

// refusingBus refuses every send as pkg/bus refuses a recipient it does not know.
type refusingBus struct {
	attempts int
	why      string
}

func (b *refusingBus) send(context.Context, bus.Message, func(string)) error {
	b.attempts++
	return &bus.Refusal{Problems: []string{b.why}}
}

// A raise the bus refuses (the owner is no known name, 2026-10-17) is sent again after a
// minute, then two, four, ... up to thirty, and its failure is said once until its text
// changes. Before, it was sent and said at every look: two thirds of the server's log.
func TestASeatSilentRefusalBacksOffAndIsSaidOnce(t *testing.T) {
	t.Parallel()
	rb := &refusingBus{why: "glenn is no known name"}
	ta, st, _ := seatSilentRig(t, &tokenBus{})
	ta.a.bus = rb.send
	var log bytes.Buffer
	ta.a.serveLog = &log
	ctx := context.Background()
	look := func(d time.Duration) {
		ta.step(d)
		ta.a.tellSeatSilent(ctx, st)
	}
	look(0)
	require.Equal(t, 1, rb.attempts)
	for range 9 {
		look(seatSilentLook + time.Second)
	}
	require.Equal(t, 1, rb.attempts, "no send inside the first minute")
	look(busRetryFirst - 9*(seatSilentLook+time.Second))
	require.Equal(t, 2, rb.attempts, "sent again at a minute")
	look(2*busRetryFirst - seatSilentLook - 2*time.Second)
	require.Equal(t, 2, rb.attempts, "the second wait is two minutes")
	look(seatSilentLook + 2*time.Second) // past the look's own throttle
	require.Equal(t, 3, rb.attempts)
	for range 8 {
		look(busRetryCap)
	}
	require.Equal(t, 11, rb.attempts, "the wait stops doubling at the cap")
	failed := strings.Count(log.String(), "the push to glenn failed")
	assert.Equal(t, 1, failed, "said once while its text stands: %s", log.String())
	assert.Equal(t, 1, strings.Count(log.String(), " raise="), "the raise is said once")

	rb.why = "glenn is no known name; another problem"
	look(busRetryCap)
	assert.Equal(t, 2, strings.Count(log.String(), "the push to glenn failed"), "a changed failure is said")

	// a bus that takes it again delivers it at its next due look, under the one token
	b := &tokenBus{}
	ta.a.bus = b.send
	look(busRetryCap)
	require.Len(t, b.delivered, 1)
	look(busRetryCap)
	require.Len(t, b.attempts, 1, "told: not sent again")
}

// A send the bus is down for (not a refusal) is sent again at the next look, as before.
func TestASeatSilentStoreDownIsSentAgainAtTheNextLook(t *testing.T) {
	t.Parallel()
	b := &tokenBus{fails: 3}
	ta, st, _ := seatSilentRig(t, b)
	ctx := context.Background()
	for range 4 {
		ta.a.tellSeatSilent(ctx, st)
		ta.pastTheLook()
	}
	assert.Len(t, b.attempts, 4)
	assert.Len(t, b.delivered, 1)
}

// A look with no judgment of the kinds it tells reads the open notes once and nothing else;
// a look with one reads them once, not twice, and never loads the work table.
func TestASeatSilentLookReadsTheOpenNotesOnce(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --owner glenn")
	b := &tokenBus{}
	ta.a.bus = b.send
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "coordinator"}
	_, err := st.Run(ctx, store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBound, SprintLevel: true, What: "unrelated"}))
	require.NoError(t, err)
	reads := func() map[string]int {
		before := maps.Clone(ta.m.Calls)
		ta.pastTheLook()
		ta.a.tellSeatSilent(ctx, st)
		diff := map[string]int{}
		for k, v := range ta.m.Calls {
			if v != before[k] {
				diff[k] = v - before[k]
			}
		}
		return diff
	}
	assert.Equal(t, map[string]int{"open": 1}, reads(), "no seat-silent judgment: the open notes alone")
	assert.Empty(t, b.attempts)

	_, err = st.Run(ctx, store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NSeatSilent, StreamLevel: true,
		Primaries: []string{"s1-1"}, What: "the seat is not answering", Decisions: []string{sprint.SeatSilentDecision}}))
	require.NoError(t, err)
	got := reads()
	assert.Equal(t, 1, got["open"], "the open notes read once: %v", got)
	assert.Len(t, b.delivered, 1, "and the raise told")
}

// At an epoch above 0 (a sprint cleared since it began) the look reads the epoch's open
// notes, not epoch 0's: the raise reaches the owner. The reader of #489 found the look silent
// at every epoch above 0 when it read the store's backend as it was handed.
func TestASeatSilentRaiseAfterAClearReachesTheOwner(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --owner glenn")
	ta.ok("clear --confirm sprint")
	b := &tokenBus{}
	ta.a.bus = b.send
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "coordinator"}
	es, err := st.EpochNow(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), es.N)
	_, err = st.Run(ctx, store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NSeatSilent, StreamLevel: true,
		Primaries: []string{"s1-1"}, What: "the seat is not answering", Decisions: []string{sprint.SeatSilentDecision}}))
	require.NoError(t, err)
	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, b.delivered, 1, "the owner is told at epoch 1")
	assert.Equal(t, []string{"glenn"}, b.delivered[0].To)
}

// A raise whose token went before with other arguments (its text or owner changed under the
// same token) is another message: it goes under a token of its own at once, not into a
// thirty-minute wait.
func TestASeatSilentRaiseWhoseTokenWentWithOtherArgumentsGetsANewToken(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta, st, _ := seatSilentRig(t, b)
	ctx := context.Background()
	var sent []string
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		sent = append(sent, m.Token)
		if len(sent) == 1 {
			return &bus.Refusal{Problems: []string{"the token \"" + m.Token + "\" already sent 01M at 2026-10-17T00:00:00Z with other arguments; a retry repeats them exactly, and a new message wants a new token"}}
		}
		return b.send(ctx, m, say)
	}
	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, sent, 2, "sent again at once")
	assert.NotEqual(t, sent[0], sent[1])
	assert.True(t, strings.HasPrefix(sent[1], sent[0]+":"), sent[1])
	require.Len(t, b.delivered, 1)
	assert.Empty(t, ta.a.seatSilent.retry, "no wait")
	ta.pastTheLook()
	ta.a.tellSeatSilent(ctx, st)
	assert.Len(t, sent, 2, "told")
}
