package sprint

import (
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// heldRepoStore is the studio store's shape on 2026-10-18 at a small scale: four members
// up, `ready` ready primaries whose briefs name their repository on a REPO: line (and
// carry no repo field, as the live primaries did not), `done` work cards each member
// finished ok on the fleet, and one staging take a member refused for a credential. The
// held rule (newHeld) asks the deal and the deadlines of every ready primary, each ask
// offers the primary to every member up (routeServed, stagingRepoWhy), and each such ask
// once walked every fleet card's repository, reading each primary's brief header again.
func heldRepoStore(t *testing.T, ready, done int) *Snapshot {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true, Deadline: 600}}
	members := []string{"m1", "m2", "m3", "m4"}
	for _, m := range members {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 4}))
	}
	cards := make([]CardAdd, ready)
	for i := range cards {
		cards[i] = CardAdd{ID: fmt.Sprintf("c%04d", i), Brief: fmt.Sprintf("c: the work tier: flash\nREPO: mas-bandwidth/nova-sprint\nBASE: sprint/ns\n\nThe task of card %d.\n", i)}
	}
	w.must(Add(w.s, AddReq{Stream: "ns", Cards: cards}))
	for _, c := range w.s.Work.Cards() {
		if _, ok := c.Fields[FieldRepo]; ok {
			x := *c
			x.Fields = maps.Clone(c.Fields)
			delete(x.Fields, FieldRepo)
			w.s.Work.Put(&x)
		}
	}
	for i := 0; i < done; i++ {
		m := members[i%len(members)]
		w.s.Fleet.Put(&Card{ID: fmt.Sprintf("c%04d.w9", i%ready), Row: m, Col: DoneOK, Fields: map[string]string{
			"ok": "yes", "member": m, "finished": t0.Add(-time.Duration(i+1) * time.Minute).Format(time.RFC3339)}})
	}
	w.s.Fleet.Put(&Card{ID: "c0000.w8", Row: "m1", Col: Withdrawn, Fields: map[string]string{
		"gen":                  "1",
		FieldStagingTake + "1": ProviderTake{Member: "m2", Finished: t0.Add(-time.Minute).Format(time.RFC3339), Error: "staging refused: remote: Write access to repository not granted."}.String(),
	}})
	return w.s
}

// The held rule, which card (Holder) and check (CheckHeld) run on every call, derives the
// staging refusals once and reads each brief's header once, however many ready primaries
// it offers to however many members. On the studio's store, 2026-10-18 00:30 EDT (about
// 385 work cards, the sprint stopped), card took 8.3 s and check 10 s, almost all of it
// here: stagingRepoWhy walked the whole fleet for every member a ready card was offered
// to, and cardRepo split every primary's whole brief again on every walk. Counted, with
// no clock: on the unfixed code check's held rule on this store walks the fleet 465 times
// and parses briefs 19,352 times; fixed, it walks three times (the refusals with
// credentials and the mirror refusals alone, memoized, and the mirror-need judgment's
// own walk) and parses each of the 40 briefs once. Not parallel: the brief memo and the
// counters are the process's (as modelOf's are, route.go).
func TestHeldWalksTheStagingRefusalsOnce(t *testing.T) {
	const ready, done = 40, 120
	s := heldRepoStore(t, ready, done)
	t.Cleanup(resetCardBaseMemo)

	for _, run := range []struct {
		verb string
		held func()
	}{
		{"check", func() { CheckHeld(HeldState{Snap: s}, t0) }},
		{"card", func() { Holder(HeldState{Snap: s}, t0, "c0001") }},
	} {
		resetCardBaseMemo()
		walks := refusalWalks.Load()
		run.held()
		walked, parsed := refusalWalks.Load()-walks, cardBaseReads.Load()
		t.Logf("HELD-COST verb=%s ready=%d fleet=%d walks=%d briefs_parsed=%d", run.verb, ready, done, walked, parsed)
		require.LessOrEqual(t, walked, int64(3), "%s: the held rule derives the staging refusals once per form, never once per member a card is offered to", run.verb)
		require.EqualValues(t, ready, parsed, "%s: each primary's brief header is read once", run.verb)
	}
}

// The memo answers only for the tables and the clock it read: a staging refusal put on
// the same fleet table after the first ask, a card dropped, a frozen view, or a later clock
// is read by the next ask, as the unmemoized derivation read it on every ask.
func TestTheStagingRefusalMemoSeesEveryChange(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, mirrorErr, t0.Add(-time.Minute))
	s, _ = s.withRests()
	require.NotNil(t, s.refusals, "a dealing step settles the memo")
	ns := nsPrimary("ns-4")
	require.Contains(t, s.stagingRepoWhy("studio", ns), "studio refused mas-bandwidth/nova-sprint at staging")
	require.Empty(t, s.stagingRepoWhy("b1", ns), "b1 refused nothing")

	// a refusal put on the same table after the first ask
	s.Fleet.Put(&Card{ID: "ns-2.w1", Row: "studio", Col: Withdrawn, Fields: map[string]string{
		"gen":                  "1",
		FieldStagingTake + "1": ProviderTake{Member: "b1", Finished: t0.Add(-time.Minute).Format(time.RFC3339), Error: mirrorErr}.String(),
	}})
	require.Contains(t, s.stagingRepoWhy("b1", ns), "b1 refused mas-bandwidth/nova-sprint at staging", "a put after the first ask is read")

	// the card dropped again
	s.Fleet.Drop("ns-2.w1")
	require.Empty(t, s.stagingRepoWhy("b1", ns), "a drop after the first ask is read")

	// a view with a frozen fleet where studio finished a nova-sprint card ok
	v := *s
	v.Fleet = s.Fleet.Frozen()
	v.Fleet.Put(&Card{ID: "ns-6.w1", Row: "studio", Col: DoneOK, Fields: map[string]string{
		"ok": "yes", "member": "studio", "finished": t0.Format(time.RFC3339),
		FieldRepo: "https://github.com/mas-bandwidth/nova-sprint"}})
	require.Empty(t, v.stagingRepoWhy("studio", ns), "the view's own fleet is read, not the memo of the parent's")
	require.NotEmpty(t, s.stagingRepoWhy("studio", ns), "the parent still reads its own fleet")

	// a later clock, past the window
	s.Now = t0.Add(2 * StagingMirrorWindow)
	require.Empty(t, s.stagingRepoWhy("studio", ns), "a later clock is read: the refusal lapsed")
}

// The brief memo is ReadCardBase's answer, and a caller's copy of the Stage list never
// writes into it.
func TestReadCardBaseOnceIsReadCardBase(t *testing.T) {
	t.Parallel()
	brief := "c: tier: flash\nREPO: mas-bandwidth/nova-sprint\nBASE: sprint/ns@0123456789012345678901234567890123456789\nStage: a.sh b.sh\n\nbody\n"
	want := swarmReadCardBase(brief)
	got := readCardBaseOnce(brief)
	require.Equal(t, want, got)
	got.Stage[0] = "changed"
	require.Equal(t, want, readCardBaseOnce(brief), "the memo keeps its own Stage")
}

func swarmReadCardBase(brief string) swarm.CardBase { return swarm.ReadCardBase([]byte(brief)) }
