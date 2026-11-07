package run

// This bounded walk chooses fixture actions from observed store state, never
// from the model. It uses one recipient, two notices and one Clear; each
// emitted batch has one obligation, and overflow/failed sends are excluded.
import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/store"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type walkTrace struct {
	t       *testing.T
	base    *store.Fake
	lines   []string
	sent    map[int]bool
	ack     []int
	pending int
	events  []store.Event
}

func (w *walkTrace) add(action, fields string) {
	w.lines = append(w.lines, fmt.Sprintf("  [i |-> %d, action |-> %q%s]", len(w.lines)+1, action, fields))
}
func (w *walkTrace) read() store.Snapshot {
	w.t.Helper()
	ctx := context.Background()
	s, e := w.base.Snapshot(ctx)
	if e != nil {
		w.t.Fatal(e)
	}
	all, e := w.base.Events(ctx, 0, 0)
	if e != nil {
		w.t.Fatal(e)
	}
	w.events = nil
	for _, v := range all {
		if v.Accepted && (v.Kind == "walk-notice" || v.Kind == "clear") {
			w.events = append(w.events, v)
		}
	}
	return s
}
func (w *walkTrace) rank(seq int64) int {
	n := 0
	for _, e := range w.events {
		if e.Seq <= seq {
			n++
		}
	}
	return n
}
func (w *walkTrace) global(s store.Snapshot) string {
	var out []string
	for _, e := range w.events {
		kind := "notice"
		if e.Kind == "clear" {
			kind = "clear"
		}
		out = append(out, fmt.Sprintf(`[kind |-> %q, epoch |-> %d]`, kind, e.Epoch))
	}
	return fmt.Sprintf(", epoch |-> %d, log |-> <<%s>>", s.Epoch, strings.Join(out, ","))
}
func walkSet(v map[int]bool) string {
	var a []string
	for i := 1; i <= 3; i++ {
		if v[i] {
			a = append(a, strconv.Itoa(i))
		}
	}
	return "{" + strings.Join(a, ",") + "}"
}
func (w *walkTrace) confirm() {
	if w.pending == 0 {
		return
	}
	w.sent[w.pending] = true
	w.ack = append(w.ack, w.pending)
	ack := map[int]bool{}
	for _, n := range w.ack {
		ack[n] = true
	}
	w.add("Send", fmt.Sprintf(", n |-> %d, sent |-> %s, acknowledged |-> %s", w.pending, walkSet(w.sent), walkSet(ack)))
	w.pending = 0
}

type walkNotices struct{}

func (walkNotices) ForEvent(e store.Event, _ store.Snapshot, _ string) ([]Notice, error) {
	if e.Kind == "walk-notice" {
		return []Notice{{Subject: e.Op, Line: e.Op}}, nil
	}
	return nil, nil
}

type walkSender struct{ w *walkTrace }

func (s walkSender) Send(_ context.Context, to, subject, body string) (Delivery, error) {
	w := s.w
	w.confirm()
	if to != "r1" {
		return Delivery{}, fmt.Errorf("recipient %q", to)
	}
	for i, e := range w.events {
		if (e.Kind == "walk-notice" && e.Op == subject && body == e.Op) || (e.Kind == "clear" && subject == "EPOCH CLEARED") {
			w.pending = i + 1
			return Delivery{Recipient: to, ID: "1000-1"}, nil
		}
	}
	return Delivery{}, fmt.Errorf("unmapped batch %q %q", subject, body)
}

type walkStore struct {
	store.Store
	w *walkTrace
}

func (s walkStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	s.w.confirm()
	return s.Store.Replay(ctx, id)
}

func (w *walkTrace) collected(c pushCollection) {
	w.t.Helper()
	from, end := w.rank(c.From), w.rank(c.End)
	needed := map[int]bool{}
	cancelled := map[int]bool{}
	if c.Recipient != "r1" || c.End != c.Through || end != len(w.events) {
		w.t.Fatalf("partial collection %+v", c)
	}
	for i := from; i < end; i++ {
		e := w.events[i]
		if e.Kind == "walk-notice" && e.Epoch < c.Epoch {
			cancelled[i+1] = true
		} else {
			needed[i+1] = true
		}
	}
	observed := map[int]bool{}
	for _, b := range c.Batches {
		if len(b.lines) != 1 {
			w.t.Fatalf("batch has %d obligations", len(b.lines))
		}
		for i, e := range w.events {
			if e.Kind == "walk-notice" && e.Op == b.subject && b.lines[0] == e.Op {
				observed[i+1] = true
			}
			if e.Kind == "clear" && b.subject == "EPOCH CLEARED" {
				expected := fmt.Sprintf("epoch %d cleared: %d pending notices dropped (some may have reached you); run nova-sprint next", e.Epoch, len(cancelled))
				if b.lines[0] != expected {
					w.t.Fatalf("summary %q expected %q", b.lines[0], expected)
				}
				observed[i+1] = true
			}
		}
	}
	if walkSet(needed) != walkSet(observed) {
		w.t.Fatalf("needed %v batches %v", needed, observed)
	}
	w.add("Collected", fmt.Sprintf(`, through |-> %d, scan |-> %d, needed |-> %s, cancelled |-> %s, phase |-> "send"`, w.rank(c.Through), end, walkSet(observed), walkSet(cancelled)))
}

func (w *walkTrace) push(ctx context.Context, s store.Snapshot) {
	w.t.Helper()
	cursor := w.rank(s.Cursors["push:r1"])
	through := w.rank(s.Seq)
	w.ack = nil
	w.add("Prepare", fmt.Sprintf(`, snapshotEpoch |-> %d, through |-> %d, scan |-> %d, phase |-> "scan"`, s.Epoch, through, cursor))
	p := NewPusher(walkStore{Store: w.base, w: w}, walkNotices{}, walkSender{w: w})
	p.observeCollect = w.collected
	if err := p.Push(ctx, "r1", s, time.Unix(2000+int64(s.Seq), 0)); err != nil {
		w.t.Fatal(err)
	}
	after := w.read()
	if w.pending != 0 || after.Cursors["push:r1"] != s.Seq {
		w.t.Fatalf("cursor or confirmation missing")
	}
	w.add("SaveCursor", fmt.Sprintf(`, cursor |-> %d, phase |-> "idle"`, w.rank(after.Cursors["push:r1"])))
}

func (w *walkTrace) actions(s store.Snapshot) []string {
	notices := 0
	for _, e := range w.events {
		if e.Kind == "walk-notice" {
			notices++
		}
	}
	var out []string
	if notices < 2 {
		out = append(out, "notice")
	}
	if s.Epoch == 1 {
		out = append(out, "clear")
	}
	if w.rank(s.Cursors["push:r1"]) < len(w.events) {
		out = append(out, "push")
	}
	return out
}
func (w *walkTrace) act(ctx context.Context, s store.Snapshot, action string) {
	w.t.Helper()
	if action == "push" {
		w.push(ctx, s)
		return
	}
	count := 0
	for _, e := range w.events {
		if e.Kind == "walk-notice" {
			count++
		}
	}
	o := op(fmt.Sprintf("walk-n%d", count+1))
	o.Epoch = s.Epoch
	o.Kind = "walk-notice"
	var rec store.Receipt
	var err error
	if action == "clear" {
		o.ID = "walk-clear"
		o.Kind = "clear"
		rec, err = w.base.Clear(ctx, o)
	} else {
		rec, err = w.base.Apply(ctx, o)
	}
	if err != nil || !rec.Accepted {
		w.t.Fatalf("%s: %+v %v", action, rec, err)
	}
	s = w.read()
	name := "AppendNotice"
	if action == "clear" {
		name = "Clear"
	}
	w.add(name, w.global(s))
}

func TestEmitClearActionWalk(t *testing.T) {
	ctx := context.Background()
	seed := int64(0)
	if raw := os.Getenv("NOVA_WALK_TRACE_SEED"); raw != "" {
		var err error
		seed, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
	}
	rng := rand.New(rand.NewSource(seed))
	w := &walkTrace{t: t, base: store.NewFake(), sent: map[int]bool{}}
	s := w.read()
	w.add("Init", fmt.Sprintf(`%s, cursor |-> %d, sent |-> {}, phase |-> "idle"`, w.global(s), w.rank(s.Cursors["push:r1"])))
	var choices []string
	for step := 0; step < 6; step++ {
		s = w.read()
		actions := w.actions(s)
		if len(actions) == 0 {
			break
		}
		action := actions[rng.Intn(len(actions))]
		choices = append(choices, action)
		w.act(ctx, s, action)
	}
	s = w.read()
	if actions := w.actions(s); len(actions) != 0 {
		t.Fatalf("walk bound exhausted: %v", actions)
	}
	path := os.Getenv("NOVA_WALK_TRACE_PATH")
	if path == "" {
		path = filepath.Join(t.TempDir(), "RunClearTraceLog.tla")
	}
	content := "---------------- MODULE RunClearTraceLog ----------------\nEXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(w.lines, ",\n") + "\n>>\n=========================================================\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("seed=%d actions=%v records=%d path=%s", seed, choices, len(w.lines), path)
}
