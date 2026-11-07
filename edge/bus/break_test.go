package bus

// Breaker findings (Opus breaker probe, 1794013063891-0) against the bus edge (unit: store.Fake and bus.Fake, no
// sockets, clocks from 1000). Each test fails on 6bb85564.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func breakApply_internal(t *testing.T, st *store.Fake, id string, at time.Time, writes ...store.Write) {
	t.Helper()
	rec, err := st.Apply(context.Background(), store.Op{ID: id, Epoch: 1, Actor: "sprint", Kind: "test", At: at, Writes: writes})
	if err != nil || !rec.Accepted {
		t.Fatalf("op %s: err=%v reason=%s", id, err, rec.Reason)
	}
}

// TestBreakDealOver4KBWedgesRecipient
//
// Sequence: one op deals 20 retry work consumers (attempt 2, a 300-byte
// finding each) to row amd-1.ds; PushTick builds the DEAL; then PushTick
// runs again, as every later tick would.
//
// Broken: Guarantee 3 ("A bus message is at most 4 KB") and the Built claim
// that the builders "enforce <= 4096 B": in packEntries_internal the size
// check is `case ...: break` inside a switch, which leaves the switch, not
// the loop, and is only reached for the last entry; 20 entries of about
// 420 bytes make a body over 8 KB. Send refuses it, PushRecipient returns
// the error before the cursor moves, and the same over-size body is rebuilt
// every tick, so this row (and every recipient sorted after it in PushTick)
// never receives a message again: Guarantee 2's cursor never passes the
// event.
func TestBreakDealOver4KBWedgesRecipient(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	t0 := time.Unix(1000, 0).UTC()
	finding := strings.Repeat("f", 300)
	writes := []store.Write{
		store.PutStream{Name: "s1", Repo: "repo", Base: "main", Pos: 1},
		store.PutRow{Name: "amd-1.ds", Kind: "worker", Width: 30, Tiers: []string{"pro"}},
		store.SetRowState{Row: "amd-1.ds", State: "up"},
	}
	var findings []store.Write
	for i := 1; i <= 20; i++ {
		p := fmt.Sprintf("p-%02d", i)
		writes = append(writes, store.PutCard{ID: p, Kind: "producer", Stream: "s1", Tier: "pro"})
		findings = append(findings, store.SetCardFinding{Card: p, Finding: finding})
	}
	breakApply_internal(t, st, "op-producers", t0, writes...)
	breakApply_internal(t, st, "op-findings", t0.Add(time.Millisecond), findings...)
	writes = nil
	for i := 1; i <= 20; i++ {
		p := fmt.Sprintf("p-%02d", i)
		w := p + ".w2"
		writes = append(writes,
			store.PutCard{ID: w, Kind: "work", Of: p, Stream: "s1", Holder: "amd-1.ds", Tier: "pro", Attempt: 2, Finding: finding},
			store.SetCardState{Card: w, State: "ready"},
		)
	}
	breakApply_internal(t, st, "op-deal-20", t0.Add(2*time.Millisecond), writes...)

	b := New(Config{Transport: NewFake(), Coordinator: "rowan"})
	snap, _ := st.Snapshot(ctx)
	evs, _ := st.Events(ctx, 0, store.MaxEvents)
	for _, m := range b.BuildRecipientMessages(snap, evs, "amd-1.ds") {
		if len(m.Body) > MaxMessageBytes {
			t.Errorf("%s to %s is %d bytes, over the %d-byte cap", m.Subject, m.To, len(m.Body), MaxMessageBytes)
		}
	}
	for tick := 1; tick <= 3; tick++ {
		_, err := b.PushTick(ctx, st, fmt.Sprintf("tick-%d", tick), t0.Add(time.Duration(tick)*time.Second))
		snap, _ = st.Snapshot(ctx)
		if snap.Cursors[CursorName("amd-1.ds")] == 0 {
			t.Fatalf("tick %d: cursor push:amd-1.ds still 0 (err=%v): the DEAL can never be sent", tick, err)
		}
	}
}

// TestBreakPushNeverReachesEventsPastFirstPage
//
// Sequence: 1,000 accepted ops that concern no row (a cursor write each),
// then one op that deals a consumer to amd-1.ds; PushRecipient for
// amd-1.ds runs five times.
//
// Broken: Guarantee 2 ("No message is skipped: a cursor passes an event only
// after nova-bus confirmed its send"), as liveness: PushRecipient reads one
// page (store.MaxEvents = 1000) after the cursor and advances the cursor
// only to the highest seq of a message it sent. A page with nothing for the
// recipient advances nothing, so the next tick reads the same page and the
// DEAL at seq 1002 is never reached.
func TestBreakPushNeverReachesEventsPastFirstPage(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	t0 := time.Unix(1000, 0).UTC()
	breakApply_internal(t, st, "op-init", t0,
		store.PutStream{Name: "s1", Repo: "repo", Base: "main", Pos: 1},
		store.PutRow{Name: "amd-1.ds", Kind: "worker", Width: 2, Tiers: []string{"pro"}},
		store.SetRowState{Row: "amd-1.ds", State: "up"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"},
	)
	for i := 1; i <= store.MaxEvents; i++ {
		breakApply_internal(t, st, fmt.Sprintf("op-noise-%d", i), t0.Add(time.Duration(i)*time.Millisecond),
			store.SetCursor{Name: "noise", Seq: int64(i)})
	}
	breakApply_internal(t, st, "op-deal", t0.Add(2*time.Second),
		store.PutCard{ID: "p1.w1", Kind: "work", Of: "p1", Stream: "s1", Holder: "amd-1.ds", Tier: "pro", Attempt: 1},
		store.SetCardState{Card: "p1.w1", State: "ready"},
	)
	fb := NewFake()
	b := New(Config{Transport: fb, Coordinator: "rowan"})
	for tick := 1; tick <= 5; tick++ {
		snap, _ := st.Snapshot(ctx)
		if _, err := b.PushRecipient(ctx, st, snap, "amd-1.ds", fmt.Sprintf("tick-%d", tick), t0.Add(time.Hour)); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	for _, m := range fb.Sent() {
		if m.To == "amd-1.ds" && m.Subject == SubjectDeal {
			return
		}
	}
	snap, _ := st.Snapshot(ctx)
	t.Fatalf("after 5 ticks no DEAL reached amd-1.ds (cursor %d, store seq %d): events past the first %d after the cursor are never read",
		snap.Cursors[CursorName("amd-1.ds")], snap.Seq, store.MaxEvents)
}
