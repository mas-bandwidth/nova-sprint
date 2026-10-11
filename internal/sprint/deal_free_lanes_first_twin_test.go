package sprint_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/hostload"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// The deal fills free lanes first (the owner, 2026-10-11 ~19:05-19:20Z: "cards
// only may accrue per-fleet or friend, if there are not other machines/friends
// ready to do that work right now!"; tla/WhoPreference.tla, DealFreeLanesFirst):
// the deal, the redeal and the cards of a held or down member go to the members
// with a free lane (fewer work cards, ready and working, than their width)
// before any member that only has room (DealAhead times its width), round the
// fleet from the deal's cursor. On the twin store (store.NewTwin).

// dealRig is a running sprint on the twin: members brought up by name and width,
// primaries in s1, one tick dealing.
type dealRig struct {
	t       *testing.T
	m       *store.Mem
	st      *store.Store
	ctx     context.Context
	mu      sync.Mutex
	now     time.Time
	members []string // up, in the order brought up
	pokeN   int      // operation ids for poke
	cardN   int      // card ids for seedWork
}

func newDealRig(t *testing.T) *dealRig {
	t.Helper()
	m := store.NewMem()
	r := &dealRig{t: t, m: m, ctx: context.Background(), now: holdT0}
	n := 0
	r.st = &store.Store{B: m, Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	r.st.ShareTwin(store.NewTwin())
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, m.RowsAdd(r.ctx, "t-readers", []string{"reader-a"}))
	require.NoError(t, m.SetCoordinator(r.ctx, "coordinator"))
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	return r
}

func (r *dealRig) up(name string, width int) {
	r.t.Helper()
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: name, Width: width}))
	r.members = append(r.members, name)
}

func (r *dealRig) hold(name string) {
	r.t.Helper()
	res, err := r.st.Hold(r.ctx, sprint.HoldReq{Names: []string{name}, Reason: "a test", Who: "coordinator"})
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused, "hold %s refused", name)
}

func (r *dealRig) unhold(name string) {
	r.t.Helper()
	res, err := r.st.Hold(r.ctx, sprint.HoldReq{Names: []string{name}, Release: true, Reason: "a test", Who: "coordinator"})
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused, "unhold %s refused", name)
}

func (r *dealRig) add(count int) {
	r.t.Helper()
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Count: count}))
}

// take takes every ready card of the member, so they are working.
func (r *dealRig) take(name string) {
	r.t.Helper()
	r.must(store.TakeStep(sprint.TakeReq{As: name, Sel: sprint.Sel{Limit: 100}, Who: name}))
}

func (r *dealRig) beat() {
	r.t.Helper()
	require.NoError(r.t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	for _, m := range r.members {
		_, err := r.st.Beat(r.ctx, m, &zero, hostload.Source{})
		require.NoError(r.t, err)
	}
}

func (r *dealRig) tick() {
	r.t.Helper()
	r.mu.Lock()
	r.now = r.now.Add(time.Second)
	r.mu.Unlock()
	r.beat()
	_, err := r.st.Tick(r.ctx)
	require.NoError(r.t, err)
}

// deal runs the tick's deal part alone (T3, TickDeal), never the level, so the
// placement free-lanes-first is what the assertion reads.
func (r *dealRig) deal() {
	r.t.Helper()
	r.must(store.TickPartStep("deal", sprint.TickDeal, sprint.TickReq{}, nil, nil, nil))
}

func (r *dealRig) must(step store.Step) store.Result {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, step)
	require.NoError(r.t, err, step.Verb)
	require.Empty(r.t, res.Refused, "%s refused", step.Verb)
	return res
}

func (r *dealRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, store.All, nil)
	require.NoError(r.t, err)
	return s
}

// ready is the member's ready work cards.
func (r *dealRig) ready(name string) int { return r.snap().Fleet.Count(name, sprint.Ready) }

// heldBy is the member's work cards, ready and working, the load its width bounds.
func (r *dealRig) heldBy(name string) int {
	s := r.snap()
	return s.Fleet.Count(name, sprint.Ready) + s.Fleet.Count(name, sprint.Working)
}

// poke writes one record straight into the store's backend, as a write outside
// the steps would: a work card seeded in place, never dealt.
func (r *dealRig) poke(logical string, e ntable.BatchMemberEntry) {
	r.t.Helper()
	table := r.st.Names.Table(logical)
	r.pokeN++
	_, err := r.m.Apply(r.ctx, ntable.BatchManifest{Schema: 1, Table: table, Epoch: "0",
		ExpectedTableRevision: fmt.Sprint(r.m.Revision(table)), OperationID: fmt.Sprintf("poke-%d", r.pokeN), Actor: "test",
		Members: []ntable.BatchMemberEntry{e}})
	require.NoError(r.t, err)
}

// seedWork puts n work cards on the member in the column, as a store the member
// already holds would hold them (kind work, on stream s1).
func (r *dealRig) seedWork(member, col string, n int) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		r.cardN++
		primary := fmt.Sprintf("seed-%d", r.cardN)
		r.poke(sprint.Fleet, ntable.BatchMemberEntry{ID: primary + ".w1",
			Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: member, Col: col, Score: float64(r.cardN)},
			Set:    map[string]string{"kind": "work", "primary": primary, "stream": "s1", "attempt": "1", "gen": "1", "member": member, "redeals": "0"}})
	}
}

// TestTheDealFillsTheIdleMemberBeforeAFullOne pins the owner's observation 3 on
// the twin: hetzner2 (width 16) idle, b2 and hetzner1 (width 16) at 16 working
// and nothing ready, 16 ready primaries. The one tick deals all 16 to hetzner2,
// the only member with a free lane, never to a full one. The next tick, after
// six more primaries, has no free lane anywhere, so the six go round the three
// in turn, two each, none past DealAhead times its width.
func TestTheDealFillsTheIdleMemberBeforeAFullOne(t *testing.T) {
	t.Parallel()
	r := newDealRig(t)
	// seed b2 and hetzner1 with 16 working each: brought up alone, dealt and taken
	r.up("b2", 16)
	r.add(16)
	r.tick()
	require.Equal(t, 16, r.ready("b2"), "b2 got %d ready, want 16 (alone up)", r.ready("b2"))
	r.take("b2")
	r.hold("b2")
	r.up("hetzner1", 16)
	r.add(16)
	r.tick()
	r.take("hetzner1")
	r.hold("hetzner1")
	r.unhold("b2")
	r.unhold("hetzner1")
	r.up("hetzner2", 16)
	require.Equal(t, 0, r.ready("hetzner2"), "hetzner2 is idle")
	require.Equal(t, 16, r.heldBy("b2"), "b2 holds %d, want 16 working", r.heldBy("b2"))
	require.Equal(t, 16, r.heldBy("hetzner1"), "hetzner1 holds %d, want 16 working", r.heldBy("hetzner1"))

	r.add(16)
	r.deal()
	require.Equal(t, 16, r.ready("hetzner2"), "hetzner2 got %d ready, want all 16 (its free lanes)", r.ready("hetzner2"))
	require.Equal(t, 0, r.ready("b2"), "b2 got %d ready, want none: it is full", r.ready("b2"))
	require.Equal(t, 0, r.ready("hetzner1"), "hetzner1 got %d ready, want none: it is full", r.ready("hetzner1"))

	r.add(6)
	r.deal()
	require.Equal(t, 2, r.ready("b2"), "b2 got %d ready, want 2: six round the three in turn", r.ready("b2"))
	require.Equal(t, 2, r.ready("hetzner1"), "hetzner1 got %d ready, want 2: six round the three in turn", r.ready("hetzner1"))
	require.Equal(t, 18, r.ready("hetzner2"), "hetzner2 got %d ready, want 18 (its 16 and two more)", r.ready("hetzner2"))
	for _, m := range []string{"b2", "hetzner1", "hetzner2"} {
		require.LessOrEqual(t, r.heldBy(m), sprint.DealAhead*16, "%s holds %d, past DealAhead times its width", m, r.heldBy(m))
	}
}

// TestTheDealRollsOverFreeMembersFirst: four idle members of width 2 and two
// full ones (two working each), 4 ready primaries: one on each idle member,
// none on the full ones; the next tick 4 more: one on each idle member again,
// the cursor rolling round the free members, never all on the first by name.
func TestTheDealRollsOverFreeMembersFirst(t *testing.T) {
	t.Parallel()
	r := newDealRig(t)
	// seed m5 and m6 with two working each: brought up alone, dealt and taken
	r.up("m5", 2)
	r.up("m6", 2)
	r.add(4)
	r.tick()
	r.take("m5")
	r.take("m6")
	require.Equal(t, 2, r.heldBy("m5"), "m5 holds %d, want 2 (full lane)", r.heldBy("m5"))
	require.Equal(t, 2, r.heldBy("m6"), "m6 holds %d, want 2 (full lane)", r.heldBy("m6"))

	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		r.up(m, 2)
	}
	r.add(4)
	r.deal()
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		require.Equal(t, 1, r.ready(m), "%s got %d ready, want 1 (its free lane)", m, r.ready(m))
	}
	require.Equal(t, 0, r.ready("m5"), "m5 got %d ready, want none: it is full", r.ready("m5"))
	require.Equal(t, 0, r.ready("m6"), "m6 got %d ready, want none: it is full", r.ready("m6"))

	r.add(4)
	r.deal()
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		require.Equal(t, 2, r.ready(m), "%s got %d ready, want 2: the cursor rolled round the free members", m, r.ready(m))
	}
	require.Equal(t, 0, r.ready("m5"), "m5 got %d ready, want none", r.ready("m5"))
	require.Equal(t, 0, r.ready("m6"), "m6 got %d ready, want none", r.ready("m6"))
}

// TestAHeldMembersCardsGoToFreeLanesFirst: hold (no --return) of a member with
// six ready cards while one member is idle and two are full (at their width):
// all six land on the idle member, never on the full ones.
func TestAHeldMembersCardsGoToFreeLanesFirst(t *testing.T) {
	t.Parallel()
	r := newDealRig(t)
	// m2 wide enough for the six (width 8, idle); m3 and m4 full (width 4, four
	// working each), below their room so the old deal would take them
	r.up("m1", 8)
	r.up("m2", 8)
	r.up("m3", 4)
	r.up("m4", 4)
	r.seedWork("m3", sprint.Working, 4)
	r.seedWork("m4", sprint.Working, 4)
	r.seedWork("m1", sprint.Ready, 6)
	require.Equal(t, 6, r.ready("m1"), "m1 holds %d ready, want 6 (it is held next)", r.ready("m1"))
	require.Equal(t, 0, r.heldBy("m2"), "m2 holds %d, want none: it is idle", r.heldBy("m2"))

	r.hold("m1")
	require.Equal(t, 6, r.ready("m2"), "m2 got %d ready, want all 6 (the free lane)", r.ready("m2"))
	require.Equal(t, 0, r.ready("m3"), "m3 got %d ready, want none: it is full", r.ready("m3"))
	require.Equal(t, 0, r.ready("m4"), "m4 got %d ready, want none: it is full", r.ready("m4"))
	require.Equal(t, 0, r.ready("m1"), "m1 keeps %d ready, want none: its cards moved", r.ready("m1"))
}
