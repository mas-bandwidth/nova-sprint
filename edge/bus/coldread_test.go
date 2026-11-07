package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Cold read, 2026-11-06, at 649d1b4c (probes by Rowan's cold-read reviewer).
// One failing probe per confirmed defect.

func crApply_internal(t *testing.T, st *store.Fake, id string, at time.Time, writes ...store.Write) {
	t.Helper()
	rec, err := st.Apply(context.Background(), store.Op{ID: id, Epoch: 1, Actor: "sprint", Kind: "deal", At: at, Writes: writes})
	if err != nil || !rec.Accepted {
		t.Fatalf("apply %s: %v %+v", id, err, rec)
	}
}

func crSeed_internal(t *testing.T, rows ...string) (*store.Fake, time.Time) {
	t.Helper()
	st := store.NewFake()
	t0 := time.Unix(1000, 0).UTC()
	writes := []store.Write{store.PutStream{Name: "s1", Repo: "repo", Base: "main", Pos: 1}}
	for _, r := range rows {
		writes = append(writes, store.PutRow{Name: r}, store.SetRowState{Row: r, State: "up"})
	}
	crApply_internal(t, st, "op-seed", t0, writes...)
	return st, t0
}

func crDeal_internal(t *testing.T, st *store.Fake, at time.Time, row string, n int, finding string) {
	t.Helper()
	var prods, writes []store.Write
	for i := 1; i <= n; i++ {
		p := fmt.Sprintf("%s-p%02d", row, i)
		w := p + ".w2"
		prods = append(prods, store.PutCard{ID: p, Kind: "producer", Stream: "s1", Tier: "pro"})
		if finding != "" {
			writes = append(writes, store.SetCardFinding{Card: p, Finding: finding})
		}
		writes = append(writes,
			store.PutCard{ID: w, Kind: "work", Of: p, Stream: "s1", Holder: row, Tier: "pro", Attempt: 2, Finding: finding},
			store.SetCardState{Card: w, State: "ready"},
		)
	}
	crApply_internal(t, st, "op-prod-"+row, at, prods...)
	crApply_internal(t, st, "op-deal-"+row, at, writes...)
}

// Guarantee 3: "A bus message is at most 4 KB". packEntries_internal's size
// check is a `break` inside a switch (bus.go:466-472): it leaves the switch,
// never the loop, and is not even reached while entries remain. Twenty retry
// deals with 300-byte reasons build a body past 4096 bytes.
func TestColdreadDefectBusDealBodyExceeds4KB(t *testing.T) {
	t.Parallel()
	st, t0 := crSeed_internal(t, "amd-1.ds")
	crDeal_internal(t, st, t0.Add(time.Second), "amd-1.ds", 20, strings.Repeat("x", 400))
	snap, err := st.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	events, err := st.Events(context.Background(), 0, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range New(Config{Transport: NewFake()}).BuildRecipientMessages(snap, events, "amd-1.ds") {
		if len(m.Body) > MaxMessageBytes {
			t.Errorf("%s to %s is %d bytes, over the %d-byte cap; Send refuses it every tick and the cursor never moves", m.Subject, m.To, len(m.Body), MaxMessageBytes)
		}
	}
}

// Guarantee 2: "No message is skipped." PushTick returns at the first
// recipient whose send fails (bus.go:577-583), so one recipient that keeps
// failing (an oversized body above, a name nova-bus refuses) starves every
// recipient sorted after it, forever.
func TestColdreadDefectBusOneFailingRecipientStarvesTheRest(t *testing.T) {
	t.Parallel()
	st, t0 := crSeed_internal(t, "a-row", "z-row")
	crDeal_internal(t, st, t0.Add(time.Second), "a-row", 1, "")
	crDeal_internal(t, st, t0.Add(2*time.Second), "z-row", 1, "")
	fb := NewFake()
	fb.SetRecipientError("a-row", errors.New("SEND FAILED: a-row"))
	b := New(Config{Transport: fb, Coordinator: "rowan"})
	for i := 0; i < 3; i++ {
		_, _ = b.PushTick(context.Background(), st, fmt.Sprintf("tick%d", i), t0.Add(time.Duration(10+i)*time.Second))
	}
	for _, m := range fb.Sent() {
		if m.To == "z-row" {
			return
		}
	}
	t.Errorf("z-row's DEAL was never sent in 3 ticks because a-row's send fails first")
}

// Guarantee 2 again: the cursor moves only on a send, and PushRecipient reads
// one window of store.MaxEvents events past it (bus.go:513). With more than
// MaxEvents events after a row's cursor that carry nothing for it, the window
// never moves and a later DEAL is never delivered.
func TestColdreadDefectBusCursorStuckBehindIrrelevantWindow(t *testing.T) {
	t.Parallel()
	st, t0 := crSeed_internal(t, "amd-1.ds")
	for i := 0; i < store.MaxEvents+1; i++ {
		crApply_internal(t, st, fmt.Sprintf("op-noise-%d", i), t0, store.PutCard{ID: fmt.Sprintf("n%d", i), Kind: "producer", Stream: "s1", Tier: "pro"})
	}
	crDeal_internal(t, st, t0.Add(time.Second), "amd-1.ds", 1, "")
	fb := NewFake()
	b := New(Config{Transport: fb, Coordinator: "rowan"})
	for i := 0; i < 3; i++ {
		if _, err := b.PushTick(context.Background(), st, fmt.Sprintf("tick%d", i), t0.Add(time.Duration(10+i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range fb.Sent() {
		if m.To == "amd-1.ds" && m.Subject == SubjectDeal {
			return
		}
	}
	t.Errorf("DEAL to amd-1.ds never sent: its cursor stays at 0 behind %d events that carry nothing for it", store.MaxEvents+1)
}
