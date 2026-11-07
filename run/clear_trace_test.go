package run

// The Clear fixture binds one notice per batch and derives the cancelled old
// notice from the actual summary. It does not cover overflow or partial sends.
import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type clearTrace struct {
	t     *testing.T
	lines []string
	base  *store.Fake
	sent  []int
	calls int
}

func (r *clearTrace) add(action, fields string) {
	r.lines = append(r.lines, fmt.Sprintf("  [i |-> %d, action |-> %q%s]", len(r.lines)+1, action, fields))
}
func (r *clearTrace) global() string {
	r.t.Helper()
	ctx := context.Background()
	snap, e := r.base.Snapshot(ctx)
	if e != nil {
		r.t.Fatal(e)
	}
	events, e := r.base.Events(ctx, 0, 0)
	if e != nil {
		r.t.Fatal(e)
	}
	var a []string
	for _, ev := range events {
		if ev.Kind == "trace-notice" {
			a = append(a, fmt.Sprintf(`[kind |-> "notice", epoch |-> %d]`, ev.Epoch))
		}
		if ev.Kind == "clear" {
			a = append(a, fmt.Sprintf(`[kind |-> "clear", epoch |-> %d]`, ev.Epoch))
		}
	}
	return fmt.Sprintf(", epoch |-> %d, log |-> <<%s>>", snap.Epoch, strings.Join(a, ","))
}
func (r *clearTrace) confirm(n int) {
	r.sent = append(r.sent, n)
	value := "{" + strings.Trim(traceSeq(r.sent), "<>") + "}"
	r.add("Send", fmt.Sprintf(", n |-> %d, sent |-> %s, acknowledged |-> %s", n, value, value))
}

type clearNotice struct{}

func (clearNotice) ForEvent(e store.Event, _ store.Snapshot, _ string) ([]Notice, error) {
	if e.Kind == "trace-notice" {
		return []Notice{{Subject: "NOTICE", Line: e.Op}}, nil
	}
	return nil, nil
}

type clearSender struct{ r *clearTrace }

func (s clearSender) Send(_ context.Context, to, subject, body string) (Delivery, error) {
	r := s.r
	r.calls++
	if r.calls == 1 && (subject != "EPOCH CLEARED" || body != "epoch 1 cleared: 1 pending notices dropped (some may have reached you); run nova-sprint next") {
		return Delivery{}, fmt.Errorf("unexpected clear summary %q %q", subject, body)
	}
	if r.calls == 2 {
		r.confirm(2)
		if subject != "NOTICE" || body != "new" {
			return Delivery{}, fmt.Errorf("unexpected new notice %q %q", subject, body)
		}
	}
	if r.calls > 2 || to != "r1" {
		return Delivery{}, fmt.Errorf("unexpected send")
	}
	return Delivery{Recipient: to, ID: "1000-1"}, nil
}

type clearStore struct {
	store.Store
	r *clearTrace
}

func (s clearStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	if s.r.calls == 2 && len(s.r.sent) == 1 {
		s.r.confirm(3)
	}
	return s.Store.Replay(ctx, id)
}

func clearCollect(t *testing.T, r *clearTrace, c pushCollection) {
	t.Helper()
	if c.Recipient != "r1" || c.From != 0 || c.Through != 3 || c.End != 3 || c.Epoch != 2 || len(c.Batches) != 2 {
		t.Fatalf("unexpected collection %+v", c)
	}
	if c.Batches[0].subject != "EPOCH CLEARED" || len(c.Batches[0].lines) != 1 || c.Batches[0].lines[0] != "epoch 1 cleared: 1 pending notices dropped (some may have reached you); run nova-sprint next" || c.Batches[1].subject != "NOTICE" || len(c.Batches[1].lines) != 1 || c.Batches[1].lines[0] != "new" {
		t.Fatalf("unexpected batches %+v", c.Batches)
	}
	r.add("Collected", fmt.Sprintf(`, through |-> %d, scan |-> %d, needed |-> {2,3}, cancelled |-> {1}, phase |-> "send"`, c.Through, c.End))
}

func TestEmitClearTrace(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	r := &clearTrace{t: t, base: base}
	snap, err := base.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.add("Init", fmt.Sprintf(`%s, cursor |-> %d, sent |-> {}, phase |-> "idle"`, r.global(), snap.Cursors["push:r1"]))
	old := op("old")
	old.Kind = "trace-notice"
	receipt, err := base.Apply(ctx, old)
	if err != nil || !receipt.Accepted {
		t.Fatalf("old %+v %v", receipt, err)
	}
	r.add("AppendNotice", r.global())
	clear := op("clear")
	clear.Kind = "clear"
	receipt, err = base.Clear(ctx, clear)
	if err != nil || !receipt.Accepted {
		t.Fatalf("clear %+v %v", receipt, err)
	}
	r.add("Clear", r.global())
	newOp := op("new")
	newOp.Epoch = 2
	newOp.Kind = "trace-notice"
	receipt, err = base.Apply(ctx, newOp)
	if err != nil || !receipt.Accepted {
		t.Fatalf("new %+v %v", receipt, err)
	}
	r.add("AppendNotice", r.global())
	snap, err = base.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.add("Prepare", fmt.Sprintf(`, snapshotEpoch |-> %d, through |-> %d, scan |-> %d, phase |-> "scan"`, snap.Epoch, snap.Seq, snap.Cursors["push:r1"]))
	p := NewPusher(clearStore{Store: base, r: r}, clearNotice{}, clearSender{r: r})
	p.observeCollect = func(c pushCollection) { clearCollect(t, r, c) }
	if err = p.Push(ctx, "r1", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	after, err := base.Snapshot(ctx)
	if err != nil || after.Cursors["push:r1"] != snap.Seq || len(r.sent) != 2 {
		t.Fatalf("cursor %+v sent %v err %v", after.Cursors, r.sent, err)
	}
	r.add("SaveCursor", fmt.Sprintf(`, cursor |-> %d, phase |-> "idle"`, after.Cursors["push:r1"]))
	path := os.Getenv("NOVA_CLEAR_TRACE_PATH")
	if path == "" {
		path = filepath.Join(t.TempDir(), "RunClearTraceLog.tla")
	}
	content := "---------------- MODULE RunClearTraceLog ----------------\nEXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(r.lines, ",\n") + "\n>>\n=========================================================\n"
	if err = os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace: %s records=%d", path, len(r.lines))
}
