package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type noticeMap map[string][]Notice

func (m noticeMap) ForEvent(e store.Event, _ store.Snapshot, _ string) ([]Notice, error) {
	return m[e.Kind], nil
}

type testSender struct {
	sends      []string
	failAt     int
	badReceipt bool
}

func (s *testSender) Send(_ context.Context, to, subject, body string) (Delivery, error) {
	s.sends = append(s.sends, subject+":"+body)
	if s.failAt == len(s.sends) {
		return Delivery{}, errors.New("send failed")
	}
	if s.badReceipt {
		return Delivery{Recipient: to, ID: "invalid"}, nil
	}
	return Delivery{Recipient: to, ID: "1000-1"}, nil
}
func pushSeed(t *testing.T, s store.Store, id, kind string) {
	t.Helper()
	o := op(id)
	o.Kind = kind
	r, err := s.Apply(context.Background(), o)
	if err != nil || !r.Accepted {
		t.Fatalf("seed %s: %+v %v", id, r, err)
	}
}
func TestPushBatchesOnlyAdvanceAfterAllConfirm(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	pushSeed(t, base, "a", "deal")
	pushSeed(t, base, "b", "judgement")
	pushSeed(t, base, "c", "deal")
	snap, err := base.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sender := &testSender{failAt: 2}
	p := NewPusher(base, noticeMap{"deal": {{Subject: "DEAL", Line: "one"}}, "judgement": {{Subject: "NEEDS YOU", Line: "judge"}}}, sender)
	if err = p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err == nil {
		t.Fatal("partial failure accepted")
	}
	after, _ := base.Snapshot(ctx)
	if after.Cursors["push:alice"] != 0 {
		t.Fatal("advanced after partial send")
	}
	if len(sender.sends) != 2 || sender.sends[0] != "DEAL:one\none" {
		t.Fatalf("batches=%v", sender.sends)
	}
	sender.failAt = 0
	if err = p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	after, _ = base.Snapshot(ctx)
	if after.Cursors["push:alice"] != 3 || len(sender.sends) != 4 {
		t.Fatalf("cursor=%d sends=%v", after.Cursors["push:alice"], sender.sends)
	}
}
func TestPushRequiresReceiptAndNoUnrelatedHeartbeat(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	pushSeed(t, base, "a", "quiet")
	snap, _ := base.Snapshot(ctx)
	sender := &testSender{}
	p := NewPusher(base, noticeMap{}, sender)
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	after, _ := base.Snapshot(ctx)
	if after.Seq != snap.Seq || len(sender.sends) != 0 {
		t.Fatal("unrelated events created a send or cursor write")
	}
	pushSeed(t, base, "b", "deal")
	snap, _ = base.Snapshot(ctx)
	p.notices = noticeMap{"deal": {{Subject: "DEAL", Line: "take"}}}
	sender.badReceipt = true
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err == nil {
		t.Fatal("bad receipt accepted")
	}
	after, _ = base.Snapshot(ctx)
	if after.Cursors["push:alice"] != 0 {
		t.Fatal("invalid receipt advanced cursor")
	}
}
func TestPushUnknownCursorReplaysWithoutResend(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	pushSeed(t, base, "a", "deal")
	snap, _ := base.Snapshot(ctx)
	unknown := &unknownStore{Store: base}
	sender := &testSender{}
	p := NewPusher(unknown, noticeMap{"deal": {{Subject: "DEAL", Line: "take"}}}, sender)
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); !errors.Is(err, store.ErrUnknown) {
		t.Fatalf("err=%v", err)
	}
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	after, _ := base.Snapshot(ctx)
	if after.Cursors["push:alice"] != 1 || len(sender.sends) != 1 {
		t.Fatalf("cursor=%d sends=%v", after.Cursors["push:alice"], sender.sends)
	}
}
func TestPushCursorNameLongAndSeparate(t *testing.T) {
	a, err := pushCursorName(strings.Repeat("a", 123))
	if err != nil || len(a) != 128 || !strings.HasPrefix(a, "push:") {
		t.Fatalf("name=%q err=%v", a, err)
	}
	b, err := pushCursorName(strings.Repeat("a", 128))
	if err != nil || len(b) > 128 || !strings.HasPrefix(b, "push-hash:") {
		t.Fatalf("name=%q err=%v", b, err)
	}
}

func TestPushBoundsOneEventAndGroups(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	pushSeed(t, base, "a", "batch")
	snap, _ := base.Snapshot(ctx)
	notices := []Notice{}
	for i := 0; i < 25; i++ {
		notices = append(notices, Notice{Subject: "DEAL", Line: fmt.Sprintf("card-%d", i)})
	}
	notices = append(notices, Notice{Subject: "NEEDS YOU", Group: "g1", Line: "judge one"}, Notice{Subject: "NEEDS YOU", Group: "g2", Line: "judge two"})
	sender := &testSender{}
	p := NewPusher(base, noticeMap{"batch": notices}, sender)
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if len(sender.sends) != 3 {
		t.Fatalf("groups: %v", sender.sends)
	}
	_, body, _ := strings.Cut(sender.sends[0], ":")
	lines := strings.Split(body, "\n")
	if len(lines) != 21 || lines[20] != "and 5 more: nova-sprint next --as alice" || len(body) > 4096 {
		t.Fatalf("lines=%d body=%q", len(lines), body)
	}
	fresh, _ := base.Snapshot(ctx)
	if fresh.Cursors["push:alice"] != 1 {
		t.Fatalf("cursor=%d", fresh.Cursors["push:alice"])
	}
}
func TestPushOversizeSingleLineSummarized(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	pushSeed(t, base, "a", "batch")
	snap, _ := base.Snapshot(ctx)
	sender := &testSender{}
	p := NewPusher(base, noticeMap{"batch": {{Subject: "DEAL", Line: strings.Repeat("x", 5000)}}}, sender)
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if len(sender.sends) != 1 || sender.sends[0] != "DEAL:and 1 more: nova-sprint next --as alice" {
		t.Fatalf("sends=%v", sender.sends)
	}
	fresh, _ := base.Snapshot(ctx)
	if fresh.Cursors["push:alice"] != 1 {
		t.Fatalf("cursor=%d", fresh.Cursors["push:alice"])
	}
}
