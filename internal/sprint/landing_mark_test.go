package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func landingMarkSoftWorld(t *testing.T) *world {
	t.Helper()
	return landingMarkWorldOf(t, [][2]string{{"a", "c"}, {"b", ""}, {"c", ""}})
}

// landingMarkWorldOf is a store with every card of cards (its id, its LAND-AFTER list)
// merging and queued in stream s1 at its head, read and accepted; the clock is
// time.Unix(1000, 0).
func landingMarkWorldOf(t *testing.T, cards [][2]string) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = time.Unix(1000, 0).UTC()
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	brief := func(id, after string) string {
		soft := ""
		if after != "" {
			soft = "LAND-AFTER: " + after + "\n"
		}
		return "RESULT: " + id + " sha=000000000000 tier: flash\nREPO: mas-bandwidth/nova-sprint\nBASE: main\n" + soft + "PATHS: internal/" + id + ".go\n\ntest"
	}
	var adds []CardAdd
	var ids, works []string
	for _, c := range cards {
		adds = append(adds, CardAdd{ID: c[0], Brief: brief(c[0], c[1])})
		ids, works = append(ids, c[0]), append(works, c[0]+".w1")
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: adds}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: len(cards)}}))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: works}, Gens: gensOf(w.s, works...)}))
	w.must(Ask(w.s, AskReq{}))
	for _, id := range ids {
		for _, rc := range readsAt(w.s, w.s.Work.Card(id), 1) {
			w.must(Read(w.s, ReadReq{Usage: "input=1 output=1", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
		}
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
	}
	return w
}

func TestMarkLandingStopsAtSoftBlockedPinAndMarksPrefix(t *testing.T) {
	t.Parallel()
	w := landingMarkSoftWorld(t)
	head := func(id string) string { return w.s.Work.Card(id).F("head") }
	p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head("b")}, {ID: "a", Head: head("a")}})
	require.Len(t, p.Refused, 1)
	assert.Equal(t, "a", p.Refused[0].Key)
	assert.Equal(t, "soft dependency on c not landed; a is skipped this batch", p.Refused[0].Why)
	assert.True(t, LandOrderSkipped(p.Refused[0]))
	w.do(p)
	assert.True(t, LandingMarked(w.s, "b"))
	assert.False(t, LandingMarked(w.s, "a"))
}

func TestMarkLandingAcceptsSoftTargetAheadInPins(t *testing.T) {
	t.Parallel()
	w := landingMarkSoftWorld(t)
	head := func(id string) string { return w.s.Work.Card(id).F("head") }
	p := MarkLanding(w.s, "s1", []PushedPin{{ID: "c", Head: head("c")}, {ID: "a", Head: head("a")}})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.True(t, LandingMarked(w.s, "c"))
	assert.True(t, LandingMarked(w.s, "a"))
}

// Every MarkLanding refusal but the LAND-AFTER skip writes nothing and is not the skip: the
// lander refuses the whole batch for it (cmd/nova-sprint markLanding).
func TestMarkLandingHardRefusalsWriteNothingAndAreNotTheSkip(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"not queued", "head moved", "re-pointed", "reads short"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			w := landingMarkSoftWorld(t)
			head := func(id string) string { return w.s.Work.Card(id).F("head") }
			bad := PushedPin{ID: "c", Head: head("c")}
			switch kind {
			case "not queued":
				bad.ID = "zz"
			case "head moved":
				bad.Head = "dddddddddddd"
			case "re-pointed":
				bad.Base = "other"
			case "reads short":
				w.s.Work.SetProp(PropReadsNeeded, "3")
			}
			p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head("b")}, bad})
			require.Len(t, p.Refused, 1)
			assert.False(t, LandOrderSkipped(p.Refused[0]), p.Refused[0].Why)
			assert.Contains(t, p.Refused[0].Why, "nothing pushed")
			assert.Empty(t, p.Units, "a %s refusal wrote a mark", kind)
		})
	}
}

// A batch of one card with three LAND-AFTER targets, all merging in its stream and none
// pinned ahead of it (dogfood run 3, "LAND FAILED stream=e3s cards=1 ... NOT reported:
// owner-pushes-route-to-coordinator: soft dependency on <3 targets> not landed"): the one
// pin is the skip, naming every unlanded target, and nothing is marked, so nothing is pushed
// (tla/SoftDependency.tla MarkPush, BaseRecorded). With two of the three pinned ahead of it,
// the skip names only the third, and the two ahead are marked.
func TestMarkLandingOneCardBatchWithThreeSoftTargets(t *testing.T) {
	t.Parallel()
	cards := [][2]string{{"a", "c1, c2, c3"}, {"c1", ""}, {"c2", ""}, {"c3", ""}}
	t.Run("none landed", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, cards)
		p := MarkLanding(w.s, "s1", []PushedPin{{ID: "a", Head: w.s.Work.Card("a").F("head")}})
		require.Len(t, p.Refused, 1)
		assert.Equal(t, "a", p.Refused[0].Key)
		assert.Equal(t, "soft dependency on c1, c2, c3 not landed; a is skipped this batch", p.Refused[0].Why)
		assert.True(t, LandOrderSkipped(p.Refused[0]))
		assert.Empty(t, p.Units, "a skipped one-card batch wrote a mark")
		w.do(p)
		assert.False(t, LandingMarked(w.s, "a"))
	})
	t.Run("one of three not ahead", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, cards)
		head := func(id string) string { return w.s.Work.Card(id).F("head") }
		p := MarkLanding(w.s, "s1", []PushedPin{{ID: "c1", Head: head("c1")}, {ID: "c3", Head: head("c3")}, {ID: "a", Head: head("a")}})
		require.Len(t, p.Refused, 1)
		assert.Equal(t, "soft dependency on c2 not landed; a is skipped this batch", p.Refused[0].Why)
		w.do(p)
		assert.True(t, LandingMarked(w.s, "c1"))
		assert.True(t, LandingMarked(w.s, "c3"))
		assert.False(t, LandingMarked(w.s, "a"))
	})
}
