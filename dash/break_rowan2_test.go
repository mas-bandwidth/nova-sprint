package dash

// Break-it probes by Rowan's second breaker (rowan2), 2026-11-06, against
// v2 at f62551fe (dashboard round 8). Each TestBreak* fails at that head and
// names the exact sequence and the design line it holds the code to.
// TestHeldRowan2* records attacks that held. No probe waits: a slow store
// is simulated from the context's deadline (it answers only when the caller
// gives it at least its latency, and otherwise returns DeadlineExceeded at
// once).

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// slowSnapshotStore answers Snapshot after latency: when the caller's
// deadline leaves less than that, it fails as the deadline would. The first
// failFirst Snapshots are refused outright (a store restarting).
type slowSnapshotStore struct {
	store.Store
	mu        sync.Mutex
	latency   time.Duration
	failFirst int
	deadlines []time.Duration
}

func (s *slowSnapshotStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dl, ok := ctx.Deadline()
	left := time.Until(dl)
	s.deadlines = append(s.deadlines, left)
	if s.failFirst > 0 {
		s.failFirst--
		return store.Snapshot{}, errors.New("connection refused")
	}
	if ok && left < s.latency {
		return store.Snapshot{}, context.DeadlineExceeded
	}
	return s.Store.Snapshot(ctx)
}

// The startup Snapshot has a 30-second deadline, but when it fails once
// (the store restarting) every retry runs inside Advance under
// AdvanceDeadline, five seconds: a store whose Snapshot answers in 10 s,
// which the startup path accepts, is never recovered from.
// Sequence: NewRecovering on a store that refuses the first Snapshot and
// answers each later one after 10 s; Advance three times.
// Broken: dash/README.md: "If that Snapshot fails after the store has
// opened, the dashboard serves an unavailable cache and retries the
// Snapshot on each poll until it succeeds" and "store opening and the
// startup Snapshot each have a 30-second deadline".
func TestBreakDashRetriedStartupSnapshotNeverGetsItsThirtySeconds(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	s := &slowSnapshotStore{Store: f, latency: 10 * time.Second, failFirst: 1}
	c, err := NewRecovering(ctx, s)
	if err == nil {
		t.Fatal("the first Snapshot was to be refused")
	}
	var last error
	for range 3 {
		last = c.Advance(ctx, s)
	}
	if last != nil {
		t.Fatalf("after three polls the cache is still unavailable (%v); Snapshot deadlines given: %v", last, s.deadlines)
	}
}

// Held: the startup Snapshot gets its 30 seconds; a store that answers
// after 29 s starts the dashboard.
func TestHeldRowan2StartupSnapshotAt29Seconds(t *testing.T) {
	f := store.NewFake()
	seedSprint(t, f, 20)
	s := &slowSnapshotStore{Store: f, latency: 29 * time.Second}
	if _, err := New(context.Background(), s); err != nil {
		t.Fatalf("a 29 s Snapshot inside the 30 s deadline: %v", err)
	}
}

// Held: hostile paths, queries and headers against the HTTP server: every
// response carries no-store; /api/sprint answers JSON, gzip only when asked,
// whatever the query; nothing panics.
func TestHeldRowan2HostileRequests(t *testing.T) {
	f := store.NewFake()
	seedSprint(t, f, 20)
	c, err := New(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(c)
	for _, target := range []string{"/api/sprint?%zz=%%", "/api/sprint?" + strings.Repeat("a=b&", 5000), "//api/sprint", "/api/sprint/",
		"/%2e%2e/%2e%2e/etc/passwd", "/app.js/..", "/stream/%3Cscript%3E", "/index.html?<x>", "/public.html#x", "/nunito-800.woff2", "/../../"} {
		for _, enc := range []string{"", "gzip", "gzip;q=0, *", "GZIP;Q=0.5", "*;q=abc", "gzip;q=1;q=0", strings.Repeat("gzip,", 2000)} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.URL.RawQuery = ""
			req.RequestURI = target
			if u, err := req.URL.Parse(target); err == nil {
				req.URL = u
			}
			req.Header.Set("Accept-Encoding", enc)
			h.ServeHTTP(rec, req)
			if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
				t.Errorf("%s (%q): Cache-Control %q", target, enc, cc)
			}
			if req.URL.Path != "/api/sprint" || rec.Code != 200 {
				continue
			}
			var body io.Reader = rec.Body
			if rec.Header().Get("Content-Encoding") == "gzip" {
				z, err := gzip.NewReader(rec.Body)
				if err != nil {
					t.Fatalf("%s (%q): %v", target, enc, err)
				}
				body = z
			}
			var p map[string]any
			if err := json.NewDecoder(body).Decode(&p); err != nil || p["ok"] != true {
				t.Errorf("%s (%q): %v %v", target, enc, err, p["ok"])
			}
		}
	}
}

// flakyPages fails Events on the calls named in failAt (counted from 1):
// a store that refuses mid-page.
type flakyPages struct {
	store.Store
	calls  int
	failAt map[int]bool
}

func (s *flakyPages) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	s.calls++
	if s.failAt[s.calls] {
		return nil, errors.New("connection reset mid-page")
	}
	return s.Store.Events(ctx, after, max)
}

// Held: a random walk of writes (producers and consumers in every state,
// costs, rows up, down, held and narrowed, clears), batches past one page,
// and Events refusing mid-page every few calls; after every successful
// Advance the folded view equals the view of a cold Snapshot.
func TestHeldRowan2FoldEqualsColdUnderMidPageRefusals(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 40)
	s := &flakyPages{Store: f, failAt: map[int]bool{}}
	for i := 3; i < 400; i += 7 {
		s.failAt[i] = true
	}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	epoch := 1
	rnd := uint64(12345)
	next := func(n int) int {
		rnd = rnd*6364136223846793005 + 1442695040888963407
		return int((rnd >> 33) % uint64(n))
	}
	n, accepted := 0, 0
	op := func(w ...store.Write) {
		n++
		r, err := f.Apply(ctx, store.Op{ID: fmt.Sprintf("w%d", n), Epoch: epoch, Actor: "test", Kind: "test", At: time.Date(2026, 1, 1, 0, 0, n, 0, time.UTC), Writes: w})
		if err != nil {
			t.Fatal(err)
		}
		accepted += b2i(r.Accepted)
	}
	states := store.CardStates
	compared := 0
	for step := range 60 {
		batch := 1 + next(40)
		if step%10 == 9 {
			batch = 1500
		}
		for range batch {
			i := next(40)
			p, stream := fmt.Sprintf("p%05d", i), fmt.Sprintf("s%02d", i%50)
			if epoch > 1 {
				stream = "s00"
			}
			switch next(6) {
			case 0:
				op(store.SetCardState{Card: p, State: states[next(len(states))]})
			case 1:
				id := fmt.Sprintf("c%d", n)
				op(store.PutCard{ID: id, Kind: []string{"work", "read"}[next(2)], Of: p, Stream: stream, Holder: []string{"amd-1", "stella", "ghost"}[next(3)], Tier: "pro"})
			case 2:
				op(store.SetRowState{Row: []string{"amd-1", "stella"}[next(2)], State: []string{"up", "down"}[next(2)]})
			case 3:
				op(store.SetRowHeld{Row: "amd-1", Held: next(2) == 0, Reason: "walk"})
			case 4:
				op(store.SetCardPriority{Card: p, Priority: store.Priorities[next(len(store.Priorities))]})
			default:
				op(store.SetCardState{Card: p, State: "landed"})
			}
		}
		if step == 30 {
			n++
			if _, err := f.Clear(ctx, store.Op{ID: fmt.Sprintf("clear%d", n), Epoch: epoch, Actor: "test", Kind: "clear", At: time.Date(2026, 1, 2, 0, 0, n, 0, time.UTC)}); err != nil {
				t.Fatal(err)
			}
			epoch++
			seedSprintEpoch(t, f, epoch)
		}
		for range 3 {
			if c.Advance(ctx, s) == nil {
				break
			}
		}
		if c.err != nil {
			continue
		}
		snap, err := f.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Seq != c.state.Seq {
			continue // the cursor write's receipt is folded on the next poll
		}
		at := c.view.At
		got, _ := json.Marshal(c.view)
		want, _ := json.Marshal(mapSnapshot(snap, at))
		if string(got) != string(want) {
			t.Fatalf("step %d: folded view differs from cold view\n got %s\nwant %s", step, got, want)
		}
		compared++
	}
	t.Logf("%d ops, %d accepted, %d comparisons, %d Events calls", n, accepted, compared, s.calls)
	if compared < 20 {
		t.Fatalf("only %d comparisons", compared)
	}
}

// seedSprintEpoch re-seeds rows and streams after a clear, as seedSprint
// does, in epoch.
func seedSprintEpoch(t *testing.T, s store.Store, epoch int) {
	t.Helper()
	w := []store.Write{
		store.PutRow{Name: "amd-1", Kind: "worker", Width: 4000, Tiers: []string{"pro", "heavy"}},
		store.PutRow{Name: "stella", Kind: "kin", Width: 4000, Tiers: []string{"pro", "heavy"}},
		store.PutStream{Name: "s00"},
	}
	for i := range 40 {
		w = append(w, store.PutCard{ID: fmt.Sprintf("p%05d", i), Kind: "producer", Stream: "s00", Tier: "pro"})
	}
	if r := apply(t, s, fmt.Sprintf("reseed%d", epoch), epoch, w...); !r.Accepted {
		t.Fatalf("reseed refused: %s", r.Reason)
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// A producer in a state the page does not know is in no column and not in
// the total, so DONE lights while it is neither landed nor failed. The
// store holds any of store.CardStates on any kind; "done" is one (layer 2
// moves only consumers there, but the store, which the dashboard reads,
// accepts it on a producer: a hand repair or a bug elsewhere shows as DONE).
// Sequence: two producers in one stream; one landed; the other set to
// "done" by a store write; New; the envelope.
// Broken: dash/README.md: "Both count in the total; DONE means all
// producers are landed or failed"; DASHBOARD-LESSONS 2026-10-11: "The
// dashboard has to reflect the truth."
func TestBreakDashProducerInUnknownStateLightsDone(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	mustApply(t, f, "seed", 1, store.PutStream{Name: "s"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro"},
		store.PutCard{ID: "p2", Kind: "producer", Stream: "s", Tier: "pro"})
	mustApply(t, f, "move", 1, store.SetCardState{Card: "p1", State: "landed"}, store.SetCardState{Card: "p2", State: "done"})
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	d := c.pageSnapshot().Data.(envelope)
	if d.All != 2 || d.Done {
		t.Fatalf("two producers, one landed and one in state done: all=%d done=%v", d.All, d.Done)
	}
}
