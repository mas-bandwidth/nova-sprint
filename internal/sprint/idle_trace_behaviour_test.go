package sprint

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The idle alarm's words and episode, driven through the exported TraceIdle and
// TickIdle (docs/SPEC-SPRINT.md section 14; tla/SprintRules.tla AlarmOncePerEpisode,
// ClearFollowsAlarm). The state a bound needs is put on the table directly, the
// way the store tests seed it.

// idleWorld is a world (two members up) with the given work cards put on the
// table directly, for driving TraceIdle/TickIdle on states the verbs refuse to
// make.
func idleWorld(t *testing.T, cards ...*Card) *world {
	t.Helper()
	w := setup(t, 0)
	w.s.Work.SetRows(append(w.s.Work.Rows(), "s2"))
	for _, c := range cards {
		w.s.Work.Put(c)
	}
	w.s.Work.cells, w.s.Work.byPrimary = nil, nil
	return w
}

// waitCard is a waiting primary of s2 that names need.
func waitCard(id, need string) *Card {
	return &Card{ID: id, Row: "s2", Col: Waiting, Score: 2, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s2", "brief": proBrief, "needs": need}}
}

// TraceIdle counts the cards behind a dropped need, and names the oldest open
// NBlocked judgment's age, absent when there is none (idle.go:146-150,217-222).
func TestTheIdleTraceCountsCardsBehindADroppedNeed(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"b"}, Needs: []string{"s1-1"}}))
	w.seedDroppedNeed("s1-1")
	// no judgment open on b yet: the group is named without an oldest
	got := TraceIdle(w.s, TickReq{})
	require.Contains(t, got, "1 behind 1 drop-blocked judgments")
	assert.NotContains(t, got, "oldest")
	// the resolve opens the NBlocked judgment on b: the age is named
	w.must(Resolve(w.s, ResolveReq{}))
	require.Len(t, openOf(w, NBlocked, "b"), 1)
	assert.Contains(t, TraceIdle(w.s, TickReq{}), "1 behind 1 drop-blocked judgments (oldest 0s)")
}

// A need absent from the table counts under cards with missing needs, and two
// cards waiting on each other count there too without hanging (idle.go:126,143-145).
func TestTheIdleTraceCountsAMissingNeedAndATwoCardCycle(t *testing.T) {
	t.Parallel()
	// a need not on the table
	w := idleWorld(t, waitCard("x", "ghost"))
	assert.Contains(t, TraceIdle(w.s, TickReq{}), "1 behind 1 cards with missing needs")

	// x needs y, y needs x: both wait, neither hangs
	w2 := idleWorld(t,
		waitCard("x", "y"),
		&Card{ID: "y", Row: "s2", Col: Waiting, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s2", "needs": "x"}},
	)
	assert.Contains(t, TraceIdle(w2.s, TickReq{}), "2 behind 1 cards with missing needs")
}

// A held need, a held sentinel, a reached unreleased sentinel and an unheld
// waiting sentinel each give their own words (idle.go:131-137,153-154).
func TestTheIdleTraceNamesHeldAndSentinelRoots(t *testing.T) {
	t.Parallel()
	held := func(id string, sentinel bool) *Card {
		kind := "primary"
		if sentinel {
			kind = "sentinel"
		}
		return &Card{ID: id, Row: "s2", Col: Waiting, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": kind, "attempt": "0", "stream": "s2", FieldHeld: "1"}}
	}
	cases := []struct {
		name string
		card *Card
		want string
	}{
		{"held need", held("n", false), "2 behind n (held)"},
		{"held sentinel", held("n", true), "1 behind sentinel n (held)"},
		{"reached sentinel", &Card{ID: "n", Row: "s2", Col: Waiting, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "sentinel", "reached": "1"}}, "1 behind sentinel n (reached, not released)"},
		{"waiting sentinel", &Card{ID: "n", Row: "s2", Col: Waiting, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "sentinel"}}, "1 behind sentinel n (waiting for the cards before it)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := idleWorld(t, waitCard("w", "n"), tc.card)
			assert.Contains(t, TraceIdle(w.s, TickReq{}), tc.want)
		})
	}
}

// A ready need on a tier no route serves gives "tier <t>: no route serves it"
// (idle.go:111-117).
func TestTheIdleTraceNamesATierNoRouteServes(t *testing.T) {
	t.Parallel()
	w := idleWorld(t, waitCard("w", "n"),
		&Card{ID: "n", Row: "s2", Col: Ready, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s2", "brief": proBrief, FieldTierNow: "pro"}})
	// a route store with no route of the tier: the tier is not served
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Enabled: true}}
	assert.Contains(t, TraceIdle(w.s, TickReq{}), "1 behind tier pro: no route serves it")
}

// A need left merging on a stopped stream is a stopped root: the trace names the
// stream and the cause (idle.go:106-108).
func TestTheIdleTraceNamesANeedOnAStoppedStream(t *testing.T) {
	t.Parallel()
	w := idleWorld(t, waitCard("w", "n"),
		&Card{ID: "n", Row: "s2", Col: Merging, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "primary", "stream": "s2"}})
	w.s.Merge.Put(&Card{ID: CtlID("s2"), Row: "s2", Col: Ctl,
		Fields: map[string]string{"state": StreamStopped, "cause": "conflict"}})
	assert.Contains(t, TraceIdle(w.s, TickReq{}), "1 behind stream s2 stopped (conflict)")
}

// A need in flight with an open judgment is a judgment root: the trace names the
// need, the judgment's type and its age (idle.go:103-104,225-226).
func TestTheIdleTraceNamesANeedWithAnOpenJudgment(t *testing.T) {
	t.Parallel()
	w := idleWorld(t, waitCard("w", "n"),
		&Card{ID: "n", Row: "s2", Col: Merging, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "primary", "stream": "s2"}})
	w.note(judgment(NBlocked, "s2", w.s.Now, 0, "n"))
	assert.Contains(t, TraceIdle(w.s, TickReq{}), "1 behind n ("+NBlocked+", 0s)")
}

// The trace names at most IdleRoots roots, the most cards behind first, and says
// how many are left (idle.go:205-208; IdleRoots is 8).
func TestTheIdleTraceNamesEightRootsAndCountsTheRest(t *testing.T) {
	t.Parallel()
	var cards []*Card
	for i := 1; i <= IdleRoots+1; i++ {
		cards = append(cards, waitCard(fmt.Sprintf("w%d", i), fmt.Sprintf("n%d", i)),
			&Card{ID: fmt.Sprintf("n%d", i), Row: "s2", Col: Merging, Score: 1, Rev: 1,
				Fields: map[string]string{"kind": "primary", "stream": "s2"}})
	}
	w := idleWorld(t, cards...)
	for i := 1; i <= IdleRoots+1; i++ {
		w.note(judgment(NBlocked, "s2", w.s.Now, 0, fmt.Sprintf("n%d", i)))
	}
	got := TraceIdle(w.s, TickReq{})
	assert.Contains(t, got, "and 1 roots more")
	assert.Equal(t, IdleRoots, strings.Count(got, " behind "), got)
}

// TickIdle: the first idle tick writes idle_since; under IdleWindow nothing is
// said; past it one NIdle note and idle_said; no second note while it stays
// idle; when the fleet works at half its width again one NIdleCleared note and
// both properties are cleared (idle.go:248-293).
func TestTickIdleAlarmsOnceAnEpisodeAndClears(t *testing.T) {
	t.Parallel()
	w := idleWorld(t, waitCard("w", "ghost"))
	r := TickReq{IdleAlarm: true}

	p, _ := TickIdle(w.s, r)
	w.must(p)
	require.Len(t, p.Props, 1)
	assert.Equal(t, PropIdleSince, p.Props[0].Name)

	// under the window: nothing said
	w.tick(time.Minute)
	p, _ = TickIdle(w.s, r)
	w.do(p)
	assert.Empty(t, w.notesOf(NIdle))

	// past the window: one note to the coordinator, and idle_said written
	w.tick(IdleWindow)
	p, _ = TickIdle(w.s, r)
	w.do(p)
	require.Len(t, w.notesOf(NIdle), 1)
	assert.Contains(t, w.notesOf(NIdle)[0].What, "fleet 0/")
	said, ok := w.s.Fleet.Prop(PropIdleSaid)
	assert.True(t, ok, "idle_said: %q", said)

	// still idle: no second note
	w.tick(time.Minute)
	p, _ = TickIdle(w.s, r)
	w.do(p)
	assert.Len(t, w.notesOf(NIdle), 1, "once an episode")

	// the fleet works at its width again: one clear note, both properties gone
	for _, m := range []string{"m1", "m2"} {
		w.setWidth(m, 1)
		w.s.Fleet.Put(&Card{ID: "busy-" + m, Row: m, Col: Working, Rev: 1, Fields: map[string]string{"kind": "work"}})
	}
	p, _ = TickIdle(w.s, r)
	w.do(p)
	require.Len(t, w.notesOf(NIdleCleared), 1)
	assert.Contains(t, w.notesOf(NIdleCleared)[0].What, "working again")
	since, _ := w.s.Fleet.Prop(PropIdleSince)
	assert.Empty(t, since, "the episode's idle_since is cleared")
	said, _ = w.s.Fleet.Prop(PropIdleSaid)
	assert.Empty(t, said, "the episode's idle_said is cleared")
}

// An episode that ends before IdleWindow, so its note was never pushed, ends
// with no note: no NIdleCleared, and both properties are cleared (idle.go:277-281,
// the !idle && since != "" arm with said == "").
func TestTickIdleEndsAnEpisodeThatNeverSaidItsNote(t *testing.T) {
	t.Parallel()
	w := idleWorld(t, waitCard("w", "ghost"))
	r := TickReq{IdleAlarm: true}

	// the episode begins: idle_since written, no note yet
	p, _ := TickIdle(w.s, r)
	w.must(p)
	since, ok := w.s.Fleet.Prop(PropIdleSince)
	require.True(t, ok, "idle_since: %q", since)
	assert.Empty(t, w.notesOf(NIdle))

	// under the window the fleet works at its width again: the episode ends
	// without ever having said its note
	w.tick(time.Minute)
	for _, m := range []string{"m1", "m2"} {
		w.setWidth(m, 1)
		w.s.Fleet.Put(&Card{ID: "busy-" + m, Row: m, Col: Working, Rev: 1, Fields: map[string]string{"kind": "work"}})
	}
	p, _ = TickIdle(w.s, r)
	w.do(p)
	assert.Empty(t, w.notesOf(NIdleCleared), "a note never said is not cleared")
	assert.Empty(t, w.notesOf(NIdle))
	since, _ = w.s.Fleet.Prop(PropIdleSince)
	assert.Empty(t, since, "the episode's idle_since is cleared")
	said, _ := w.s.Fleet.Prop(PropIdleSaid)
	assert.Empty(t, said, "the episode's idle_said stayed unset")
}

func TestIdleBlockersAndIdleLine(t *testing.T) {
	t.Parallel()
	assert.Empty(t, IdleBlockers(nil, 3))
	assert.Empty(t, IdleBlockers(&Snapshot{}, 3))
	assert.Equal(t, "", IdleLine(nil))
	assert.Equal(t, "", IdleLine([]string{}))

	w := idleWorld(t,
		waitCard("x", "ghost"),
		waitCard("y", "ghost"),
	)
	blockers := IdleBlockers(w.s, 3)
	require.Len(t, blockers, 1)
	assert.Equal(t, "2 behind 2 cards with missing needs", blockers[0])
	assert.Equal(t, "idle: 2 behind 2 cards with missing needs", IdleLine(blockers))

	// at most n blockers
	wMany := setup(t, 0)
	wMany.s.Work.SetRows(append(wMany.s.Work.Rows(), "s2"))
	for i := 1; i <= 5; i++ {
		sid := fmt.Sprintf("sent-%d", i)
		wid := fmt.Sprintf("wait-%d", i)
		wMany.s.Work.Put(&Card{ID: sid, Row: "s2", Col: Waiting, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "sentinel", FieldHeld: "1"}})
		wMany.s.Work.Put(waitCard(wid, sid))
	}
	top3 := IdleBlockers(wMany.s, 3)
	require.Len(t, top3, 3, "at most 3 blockers")
	line := IdleLine(top3)
	assert.True(t, strings.HasPrefix(line, "idle: "))
	assert.Equal(t, 2, strings.Count(line, "; "), "3 blockers separated by 2 semicolons")
}
