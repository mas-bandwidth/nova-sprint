package verbs

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// replacing is the sprint of the brief correction (playbook section 4): p4
// ready with p3 waiting on it, and p9, its corrected brief just added, ready
// with no needs, so the run deals it within a tick.
func replacing(s *store.Snapshot) {
	dependant(s)
	s.Cards["p9"] = store.Card{ID: "p9", Kind: "producer", Stream: "s", State: "ready", Gen: 1, Attempt: 1, Tier: "pro", Priority: "normal"}
}

// TestReplaceByACardDealtBetween is the live refusal of 2026-11-10: `add`
// then `replace <old> --by <new>`, and the run dealt the new card between
// replace's read and its commit, so the store refused the op ("card p9 is
// at gen 2, expected gen 1") and the old card's dependants could not be
// moved. The new card's gen is fenced so that it is still not final when
// its dependants are relinked to it; a deal leaves it live, so replace reads
// again and commits, its dependants following the new card.
func TestReplaceByACardDealtBetween(t *testing.T) {
	t.Parallel()
	s := sprint()
	replacing(&s)
	w := worldFrom(t, s)
	w.e.Store = between(w, "replace", store.SetCardState{Card: "p9", State: "working"})
	want(t, w.do("carol", "replace", "p4", "--by", "p9", "--reason", "corrected brief"), 0, "p4->fail", "p3 needs=p9")
	after := w.snap(t)
	if c := after.Cards["p4"]; c.State != "fail" {
		t.Fatalf("old card %+v", c)
	}
	if c := after.Cards["p3"]; len(c.Needs) != 1 || c.Needs[0] != "p9" {
		t.Fatalf("its dependant %+v", c)
	}
	if c := after.Cards["p9"]; c.State != "working" {
		t.Fatalf("the new card %+v: the deal did not land between", c)
	}
}

// TestReplaceByACardAlreadyDealt: a new card dealt before replace read the
// store is a target like any live card.
func TestReplaceByACardAlreadyDealt(t *testing.T) {
	t.Parallel()
	s := sprint()
	replacing(&s)
	setCard("p9", func(c *store.Card) { c.State, c.Gen = "working", 2 })(&s)
	w := worldFrom(t, s)
	want(t, w.do("carol", "replace", "p4", "--by", "p9", "--reason", "corrected brief"), 0, "p4->fail", "p3 needs=p9")
}

// TestReplaceStillRefuses: what replace refuses on a quiet store it still
// refuses when it changes between the read and the commit, naming it: a
// new card that landed or failed, or was held, and an old card that
// changed under it. Nothing moves.
func TestReplaceStillRefuses(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		snap    func(*store.Snapshot)
		between []store.Write
		why     string
	}{
		{"a held new card", setCard("p9", func(c *store.Card) { c.State, c.Reason = "waiting", card.HeldPrefix + "wrong base" }), nil,
			"producer p9 is held (held: wrong base)"},
		{"the new card landed between", nil, []store.Write{store.SetCardState{Card: "p9", State: "landed"}},
			"producer p9 is landed"},
		{"the new card failed between", nil, []store.Write{store.SetCardState{Card: "p9", State: "fail"}},
			"producer p9 is fail"},
		{"the new card held between", nil, []store.Write{store.SetCardState{Card: "p9", State: "waiting"}, store.SetCardReason{Card: "p9", Reason: card.HeldPrefix + "wrong base"}},
			"producer p9 is held (held: wrong base)"},
		{"the old card changed between", nil, []store.Write{store.SetCardPriority{Card: "p4", Priority: "high"}},
			"card p4 changed since this verb read it (gen 1 ready, now gen 2 ready)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := sprint()
			replacing(&s)
			if c.snap != nil {
				c.snap(&s)
			}
			w := worldFrom(t, s)
			if c.between != nil {
				w.e.Store = between(w, "replace", c.between...)
			}
			want(t, w.do("carol", "replace", "p4", "--by", "p9", "--reason", "corrected brief"), 1, c.why)
			after := w.snap(t)
			if p4 := after.Cards["p4"]; p4.State == "fail" {
				t.Fatalf("the old card failed: %+v", p4)
			}
			if p3 := after.Cards["p3"]; len(p3.Needs) != 1 || p3.Needs[0] != "p4" {
				t.Fatalf("its dependant moved: %+v", p3)
			}
		})
	}
}
