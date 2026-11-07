package bus

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type pushPendingState_internal struct {
	active   bool
	from, to int
	rel      []int
	size     int
}

type pushTraceStep_internal struct {
	action    string
	arg       string
	events    int
	cursor    map[string]int
	pending   map[string]pushPendingState_internal
	delivered map[string][]int
	confirmed map[string][]int
	crashes   int
}

type pushWalkHarness_internal struct {
	st         *store.Fake
	fb         *Fake
	edge       *Edge
	t0         time.Time
	stepN      int
	domainSeqs []int64
	pending    map[string]pushPendingState_internal
	confirmed  map[string]map[int]bool
	crashes    int
}

func newPushWalkHarness_internal(t *testing.T) *pushWalkHarness_internal {
	t.Helper()
	st, t0 := crSeed_internal(t, "r1", "r2")
	fb := NewFake()
	e := New(Config{Transport: fb, Coordinator: "rowan", Window: 2})
	return &pushWalkHarness_internal{
		st:        st,
		fb:        fb,
		edge:      e,
		t0:        t0,
		pending:   map[string]pushPendingState_internal{"r1": {}, "r2": {}},
		confirmed: map[string]map[int]bool{"r1": {}, "r2": {}},
	}
}

func pushTargetFor_internal(evIdx int) string {
	if evIdx == 2 {
		return "r2"
	}
	return "r1"
}

// refinementPushState_internal is the refinement mapping from edge/bus + store.Fake + bus.Fake
// onto Push.tla variables <<events, cursor, pending, delivered, confirmed, crashes>>.
func refinementPushState_internal(t *testing.T, h *pushWalkHarness_internal, action, arg string) pushTraceStep_internal {
	t.Helper()
	snap, err := h.st.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	curMap := make(map[string]int, 2)
	delivMap := make(map[string][]int, 2)
	confMap := make(map[string][]int, 2)
	pendMap := make(map[string]pushPendingState_internal, 2)
	for _, r := range []string{"r1", "r2"} {
		rawCur := snap.Cursors[CursorName(r)]
		cIdx := 0
		for i, seq := range h.domainSeqs {
			if seq <= rawCur {
				cIdx = i + 1
			}
		}
		curMap[r] = cIdx
		pendMap[r] = h.pending[r]
		delivMap[r] = extractDeliveredIndices_internal(h.fb.WireMessages(), r)
		confMap[r] = extractDeliveredIndices_internal(h.fb.Sent(), r)
	}
	return pushTraceStep_internal{
		action:    action,
		arg:       arg,
		events:    len(h.domainSeqs),
		cursor:    curMap,
		pending:   pendMap,
		delivered: delivMap,
		confirmed: confMap,
		crashes:   h.crashes,
	}
}

func extractDeliveredIndices_internal(msgs []SentMessage, r string) []int {
	seen := make(map[int]bool)
	for _, m := range msgs {
		if m.To != r {
			continue
		}
		for idx := 1; idx <= 3; idx++ {
			tag := fmt.Sprintf("ev-%d.w1", idx)
			if strings.Contains(m.Body, tag) {
				seen[idx] = true
			}
		}
	}
	return sortedSetKeys_internal(seen)
}

func sortedSetKeys_internal(m map[int]bool) []int {
	var out []int
	for k, ok := range m {
		if ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func (h *pushWalkHarness_internal) winEnd_internal(cIdx int) int {
	if cIdx+2 < len(h.domainSeqs) {
		return cIdx + 2
	}
	return len(h.domainSeqs)
}

func (h *pushWalkHarness_internal) commitEvent_internal(t *testing.T) {
	t.Helper()
	h.stepN++
	idx := len(h.domainSeqs) + 1
	target := pushTargetFor_internal(idx)
	prod := fmt.Sprintf("ev-%d", idx)
	work := prod + ".w1"
	at := h.t0.Add(time.Duration(h.stepN) * time.Second)
	rec, err := h.st.Apply(context.Background(), store.Op{
		ID:    fmt.Sprintf("op-ev-%d", idx),
		Epoch: 1,
		Actor: "sprint",
		Kind:  "deal",
		At:    at,
		Writes: []store.Write{
			store.PutCard{ID: prod, Kind: "producer", Stream: "s1", Tier: "pro"},
			store.PutCard{ID: work, Kind: "work", Of: prod, Stream: "s1", Holder: target, Tier: "pro", Attempt: 1},
			store.SetCardState{Card: work, State: "ready"},
		},
	})
	if err != nil || !rec.Accepted {
		t.Fatalf("commitEvent %d: err=%v rec=%+v", idx, err, rec)
	}
	h.domainSeqs = append(h.domainSeqs, rec.Seq)
}

func (h *pushWalkHarness_internal) windowEvents_internal(t *testing.T, r string) (store.Snapshot, []store.Event, int, int) {
	t.Helper()
	ctx := context.Background()
	snap, err := h.st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rawCur := snap.Cursors[CursorName(r)]
	cIdx := 0
	for i, seq := range h.domainSeqs {
		if seq <= rawCur {
			cIdx = i + 1
		}
	}
	toIdx := h.winEnd_internal(cIdx)
	if toIdx <= cIdx {
		return snap, nil, cIdx, toIdx
	}
	endSeq := h.domainSeqs[toIdx-1]
	allEvs, err := h.st.Events(ctx, rawCur, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	var winEvs []store.Event
	for _, ev := range allEvs {
		if ev.Seq <= endSeq && ev.Kind != "cursor" {
			winEvs = append(winEvs, ev)
		}
	}
	return snap, winEvs, cIdx, toIdx
}

func (h *pushWalkHarness_internal) startSendOrSkip_internal(t *testing.T, r string) string {
	t.Helper()
	snap, winEvs, cIdx, toIdx := h.windowEvents_internal(t, r)
	msgs := h.edge.BuildRecipientMessages(snap, winEvs, r)
	if len(msgs) == 0 {
		h.stepN++
		endSeq := h.domainSeqs[toIdx-1]
		if err := advanceCursor_internal(context.Background(), h.st, snap.Epoch, r, endSeq, fmt.Sprintf("skip-%d", h.stepN), h.t0.Add(time.Duration(h.stepN)*time.Second)); err != nil {
			t.Fatal(err)
		}
		return "SkipIrrelevant"
	}
	var rel []int
	for idx := cIdx + 1; idx <= toIdx; idx++ {
		if pushTargetFor_internal(idx) == r {
			rel = append(rel, idx)
		}
	}
	sz := len(rel)
	if sz > 2 {
		sz = 2
	}
	for _, m := range msgs {
		if len(m.Body) > MaxMessageBytes {
			t.Fatalf("message exceeds cap: %d", len(m.Body))
		}
	}
	h.pending[r] = pushPendingState_internal{active: true, from: cIdx + 1, to: toIdx, rel: rel, size: sz}
	return "StartSend"
}

func (h *pushWalkHarness_internal) finishSend_internal(t *testing.T, r, mode string) string {
	t.Helper()
	h.stepN++
	pend := h.pending[r]
	snap, allWinEvs, _, _ := h.windowEvents_internal(t, r)
	endSeq := h.domainSeqs[pend.to-1]
	var winEvs []store.Event
	for _, ev := range allWinEvs {
		if ev.Seq <= endSeq {
			winEvs = append(winEvs, ev)
		}
	}
	curSeq := snap.Cursors[CursorName(r)]
	msgs := h.edge.BuildRecipientMessages(snap, winEvs, r)
	switch mode {
	case "SendFailed":
		h.fb.SetRecipientError(r, errors.New("injected send failure"))
		_, nextSeq, err := h.edge.sendWindow_internal(context.Background(), msgs, curSeq, endSeq, true)
		h.fb.SetRecipientError(r, nil)
		if err == nil || nextSeq != curSeq {
			t.Fatalf("expected failed send to leave cursor at %d, got %d err=%v", curSeq, nextSeq, err)
		}
		h.crashes++
	case "ReplyLost":
		h.fb.DropNextReply(1)
		_, nextSeq, err := h.edge.sendWindow_internal(context.Background(), msgs, curSeq, endSeq, true)
		if err == nil || nextSeq != curSeq {
			t.Fatalf("expected lost reply to leave cursor at %d, got %d err=%v", curSeq, nextSeq, err)
		}
		h.crashes++
	default:
		deliv, nextSeq, err := h.edge.sendWindow_internal(context.Background(), msgs, curSeq, endSeq, true)
		if err != nil || nextSeq != endSeq {
			t.Fatalf("sendWindow confirmed: nextSeq=%d want=%d err=%v", nextSeq, endSeq, err)
		}
		if err := advanceCursor_internal(context.Background(), h.st, snap.Epoch, r, nextSeq, fmt.Sprintf("conf-%d", h.stepN), h.t0.Add(time.Duration(h.stepN)*time.Second)); err != nil {
			t.Fatal(err)
		}
		_ = deliv
		_ = pend
	}
	h.pending[r] = pushPendingState_internal{}
	return mode
}

func walkPushTrace_internal(t *testing.T, seed int64) []pushTraceStep_internal {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	h := newPushWalkHarness_internal(t)
	steps := []pushTraceStep_internal{refinementPushState_internal(t, h, "Init", "none")}
	for step := 0; step < 24; step++ {
		st := steps[len(steps)-1]
		if st.events == 3 && st.cursor["r1"] == 3 && st.cursor["r2"] == 3 && !st.pending["r1"].active && !st.pending["r2"].active {
			steps = append(steps, refinementPushState_internal(t, h, "Idle", "none"))
			break
		}
		action, arg := pickPushMove_internal(t, rng, h, st)
		steps = append(steps, refinementPushState_internal(t, h, action, arg))
	}
	return steps
}

func pickPushMove_internal(t *testing.T, rng *rand.Rand, h *pushWalkHarness_internal, st pushTraceStep_internal) (string, string) {
	t.Helper()
	type move struct{ act, arg string }
	var moves []move
	if st.events < 3 {
		moves = append(moves, move{"CommitEvent", "none"})
	}
	for _, r := range []string{"r1", "r2"} {
		if st.pending[r].active {
			moves = append(moves, move{"SendConfirmed", r})
			if st.crashes < 1 {
				moves = append(moves, move{"SendFailed", r}, move{"ReplyLost", r})
			}
		} else if st.cursor[r] < st.events {
			moves = append(moves, move{"StepWindow", r})
		}
	}
	m := moves[rng.Intn(len(moves))]
	switch m.act {
	case "CommitEvent":
		h.commitEvent_internal(t)
		return "CommitEvent", "none"
	case "StepWindow":
		return h.startSendOrSkip_internal(t, m.arg), m.arg
	default:
		return h.finishSend_internal(t, m.arg, m.act), m.arg
	}
}

func formatPushTraceTLA_internal(steps []pushTraceStep_internal) string {
	var b strings.Builder
	b.WriteString("---------------------------- MODULE PushTraceLog ----------------------------\n")
	b.WriteString("EXTENDS Naturals, Sequences\nTraceLog == <<\n")
	for i, s := range steps {
		comma := ","
		if i == len(steps)-1 {
			comma = ""
		}
		b.WriteString(fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, events |-> %d, cursor |-> [r1 |-> %d, r2 |-> %d], pending |-> [r1 |-> %s, r2 |-> %s], delivered |-> [r1 |-> %s, r2 |-> %s], confirmed |-> [r1 |-> %s, r2 |-> %s], crashes |-> %d]%s\n",
			i+1, s.action, s.arg, s.events,
			s.cursor["r1"], s.cursor["r2"],
			formatPendingTLA_internal(s.pending["r1"]), formatPendingTLA_internal(s.pending["r2"]),
			formatIntSetTLA_internal(s.delivered["r1"]), formatIntSetTLA_internal(s.delivered["r2"]),
			formatIntSetTLA_internal(s.confirmed["r1"]), formatIntSetTLA_internal(s.confirmed["r2"]),
			s.crashes, comma))
	}
	b.WriteString(">>\n")
	b.WriteString("=============================================================================\n")
	return b.String()
}

func formatPendingTLA_internal(p pushPendingState_internal) string {
	act := "FALSE"
	if p.active {
		act = "TRUE"
	}
	return fmt.Sprintf("[active |-> %s, from |-> %d, to |-> %d, rel |-> %s, size |-> %d]",
		act, p.from, p.to, formatIntSetTLA_internal(p.rel), p.size)
}

func formatIntSetTLA_internal(vals []int) string {
	if len(vals) == 0 {
		return "{}"
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func TestPushTraceRefinementWalk(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 8; seed++ {
		steps := walkPushTrace_internal(t, seed)
		if len(steps) < 4 {
			t.Fatalf("seed %d: trace too short (%d steps)", seed, len(steps))
		}
		last := steps[len(steps)-1]
		if last.events != 3 || last.cursor["r1"] != 3 || last.cursor["r2"] != 3 {
			t.Fatalf("seed %d: walk did not reach terminal state: %+v", seed, last)
		}
		tlaText := formatPushTraceTLA_internal(steps)
		if traceDir := os.Getenv("NOVA_TRACE_DIR"); traceDir != "" && seed <= 3 {
			sub := filepath.Join(traceDir, fmt.Sprintf("push-walk-%d", seed))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "PushTraceLog.tla"), []byte(tlaText), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		wantSeed := int64(1)
		if raw := os.Getenv("NOVA_TRACE_SEED"); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				wantSeed = parsed
			}
		}
		if seed == wantSeed {
			out := os.Getenv("NOVA_PUSH_TRACE_OUT")
			if out == "" {
				out = os.Getenv("NOVA_TRACE_OUT")
			}
			if out != "" {
				if err := os.WriteFile(out, []byte(tlaText), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
