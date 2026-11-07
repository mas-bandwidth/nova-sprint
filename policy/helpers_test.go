package policy

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// The world every test builds on store.Fake (or PG in the functional tier):
// no clock, At is t0 plus the op's number.

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

type world struct {
	t  testing.TB
	st store.Store
	ro roster.Roster // the tick's roster, beside the store (round 5)
	n  int
}

// rc is one row's configuration in a test roster, built by hand: a worker
// serves the tiers the test names (roster.Load gives a worker every tier),
// kin has a model.
type rc struct {
	name, kind string
	width      int
	tiers      []string
	model      string
}

// handRoster is the roster of rcs.
func handRoster(rcs ...rc) roster.Roster {
	ro := roster.Roster{Workers: map[string]roster.Worker{}, Kin: map[string]roster.Kin{}}
	for _, c := range rcs {
		if c.kind == roster.KindKin {
			ro.Kin[c.name] = roster.Kin{Name: c.name, Model: c.model, Harness: "claude", Width: c.width, Tiers: c.tiers}
		} else {
			ro.Workers[c.name] = roster.Worker{Name: c.name, Width: c.width, Tiers: c.tiers, Harness: roster.WorkerHarness}
		}
	}
	return ro
}

// fakeClock is the unit tests' clock: Now never moves from t0, and After
// fires only once expire is called (every wait at once, then at once from
// then on). A test that never expires it never reaches a deadline.
type fakeClock struct {
	once sync.Once
	fire chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{fire: make(chan time.Time)} }

func (c *fakeClock) Now() time.Time { return t0 }

func (c *fakeClock) After(time.Duration) <-chan time.Time { return c.fire }

// expire makes every After fire: the tick's deadline (and the early
// fallback) has passed.
func (c *fakeClock) expire() { c.once.Do(func() { close(c.fire) }) }

// testEngine is an in-process engine on a fake clock that never expires.
func testEngine() *Engine { return newLocal(newFakeClock()) }

// configure adds c to the world's roster, as nova-config would.
func (w *world) configure(c rc) {
	one := handRoster(c)
	maps.Copy(w.ro.Workers, one.Workers)
	maps.Copy(w.ro.Kin, one.Kin)
}

// seedRoster is the seeded rows' roster (seed).
func seedRoster() roster.Roster {
	return handRoster(rc{"r1", "worker", 2, []string{"flash", "pro", "heavy"}, ""}, rc{"r2", "worker", 1, []string{"flash"}, ""},
		rc{"r3", "kin", 1, []string{"frontier"}, "m-frontier"}, rc{"r4", "worker", 1, []string{"pro"}, ""})
}

func fakeWorld(t testing.TB) *world {
	w := &world{t: t, st: store.NewFake(), ro: seedRoster()}
	w.seed()
	return w
}

// seed: streams s1 and s2; rows r1 (flash pro heavy, width 2), r2 (flash,
// width 1), r3 (frontier kin, width 1), r4 (pro, width 1, held), all up,
// their configuration in w.ro (seedRoster);
// producers p1 (s1 pro, ready), p2 (s1 heavy, ready, 2 bounces, a prior
// head), p3 (s2 flash, review 1, author r2, one ok read by r1 done), p4 (s2
// pro, waiting), p5 (s1 pro, review, author r1, no read yet).
func (w *world) seed() {
	writes := []store.Write{
		store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1},
		store.PutStream{Name: "s2", Repo: "git@example.com:r.git", Base: "main", Pos: 2},
		store.PutRow{Name: "r1"}, store.PutRow{Name: "r2"}, store.PutRow{Name: "r3"}, store.PutRow{Name: "r4"},
	}
	for _, p := range [][2]string{{"p1", "pro"}, {"p2", "heavy"}, {"p3", "flash"}, {"p4", "pro"}, {"p5", "pro"}} {
		stream := map[bool]string{true: "s2", false: "s1"}[p[0] == "p3" || p[0] == "p4"]
		writes = append(writes, store.PutCard{ID: p[0], Kind: "producer", Stream: stream, Tier: p[1], Score: 10, Brief: "do " + p[0], Prose: "do " + p[0]})
	}
	w.apply(writes...)
	w.apply(store.SetRowState{Row: "r1", State: "up"}, store.SetRowState{Row: "r2", State: "up"}, store.SetRowState{Row: "r3", State: "up"},
		store.SetRowState{Row: "r4", State: "up"}, store.SetRowHeld{Row: "r4", Held: true, Reason: "test"},
		store.SetCardState{Card: "p1", State: "ready"}, store.SetCardState{Card: "p2", State: "ready"},
		store.SetCardBounces{Card: "p2", Bounces: 2}, store.SetCardHead{Card: "p2", Head: "aaaa"},
		store.SetCardState{Card: "p3", State: "review"}, store.SetCardReview{Card: "p3"}, store.SetCardAuthor{Card: "p3", Author: "r2"},
		store.SetCardState{Card: "p5", State: "review"}, store.SetCardAuthor{Card: "p5", Author: "r1"})
	w.apply(store.PutCard{ID: "p3-r1", Kind: "read", Of: "p3", Stream: "s2", Tier: "flash", Holder: "r1", Review: 1})
	w.apply(store.SetCardState{Card: "p3-r1", State: "done"}, store.SetCardResult{Card: "p3-r1", Result: "ok"})
}

func (w *world) ctx() context.Context { return context.Background() }

func (w *world) next() (string, time.Time) {
	w.n++
	return fmt.Sprintf("op-%d", w.n), t0.Add(time.Duration(w.n) * time.Second)
}

// apply applies writes as one op and requires it accepted.
func (w *world) apply(writes ...store.Write) store.Receipt {
	w.t.Helper()
	id, at := w.next()
	r, err := w.st.Apply(w.ctx(), store.Op{ID: id, Epoch: w.snap().Epoch, Actor: "test", Kind: "test", At: at, Writes: writes})
	require.NoError(w.t, err)
	require.True(w.t, r.Accepted, r.Reason)
	return r
}

func (w *world) snap() store.Snapshot {
	w.t.Helper()
	s, err := w.st.Snapshot(w.ctx())
	require.NoError(w.t, err)
	return s
}

// caller is the coordinator friendF with the next op id and time.
func (w *world) caller() Caller {
	id, at := w.next()
	return Caller{Actor: "friendF", Coordinator: true, OpID: id, At: at}
}

// up runs Up and requires it.
func (w *world) up() {
	w.t.Helper()
	_, err := Up(w.ctx(), w.st, w.caller())
	require.NoError(w.t, err)
}

// file writes body to a file of the test's own and returns its path.
func (w *world) file(name, body string) string {
	w.t.Helper()
	return writeFile(w.t, name, body)
}

// set stores body as the next version and requires it; it returns the version.
func (w *world) set(name, body string) int {
	w.t.Helper()
	r, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), w.file(name, body), "test")
	require.NoError(w.t, err)
	return r.Version
}

// override is the default body with question q's def replaced by def; it
// returns the body and the line of the new def.
func override(q, def string) (string, int) {
	lines := strings.Split(defaultBody, "\n")
	start := 0
	for start < len(lines) && !strings.HasPrefix(lines[start], "def "+q+"(") {
		start++
	}
	end := start + 1
	for end < len(lines) && (lines[end] == "" || strings.HasPrefix(lines[end], " ")) {
		end++
	}
	out := append(append(append([]string{}, lines[:start]...), strings.Split(def, "\n")...), "", "")
	return strings.Join(append(out, lines[end:]...), "\n"), start + 1
}

// pv is an unstored version v with body, for Answer.
func pv(v int, body string) *store.PolicyVersion {
	return &store.PolicyVersion{Version: v, Kind: "script", Name: fmt.Sprintf("v%d.star", v), Body: body}
}

func writeFile(t testing.TB, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}
