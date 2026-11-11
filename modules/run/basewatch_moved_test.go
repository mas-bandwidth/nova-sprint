package run

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestBaseWatchSaysAForeignMove (2026-11-11): a watched base whose tip
// moves to a commit that is not one of the sprint's own landings on its
// streams (a landed or on-base producer's recorded merge, or its head) is
// moved by a foreigner. The first tip the watch sees is no move; a move is
// judged one LandPoll after it is seen, so a landing's result has folded
// first; a foreign move stands for one CIWait, then is gone. No forge read
// is made for it: the watch already reads the base tip.
func TestBaseWatchSaysAForeignMove(t *testing.T) {
	t.Parallel()
	a, b, c, d := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	f := &checksForge{rollups: map[string]forge.CheckRollup{}}
	w := &BaseWatch{Forge: f}
	snap, now := watchSnap(), time.Unix(1000, 0).UTC()
	watchStep(w, snap, a, &now, 0)
	watchStep(w, snap, a, &now, LandPoll)
	if m := w.Moves(); len(m) != 0 {
		t.Fatalf("the first tip seen is a move: %+v", m)
	}

	snap.Cards["p8"] = store.Card{ID: "p8", Kind: "producer", Stream: "s1", State: "landed", Reason: store.LandedReason + " at " + b}
	watchStep(w, snap, b, &now, LandPoll)
	watchStep(w, snap, b, &now, LandPoll)
	if m := w.Moves(); len(m) != 0 {
		t.Fatalf("the sprint's own landing is a foreign move: %+v", m)
	}

	watchStep(w, snap, c, &now, LandPoll)
	if m := w.Moves(); len(m) != 0 {
		t.Fatalf("a move judged before a landing's result could fold: %+v", m)
	}
	watchStep(w, snap, c, &now, LandPoll)
	m, ok := w.Moves()["o/r@main"]
	if !ok || m.To != c || m.Repo != "o/r" || m.Base != "main" || strings.Join(m.Streams, ",") != "s1,s2,s3" {
		t.Fatalf("a foreign move is no move: %+v (ok %v)", m, ok)
	}

	watchStep(w, snap, d, &now, LandPoll/2) // a landing merged d; its result folds within the grace
	snap.Cards["p9"] = store.Card{ID: "p9", Kind: "producer", Stream: "s2", State: "merging", Reason: store.OnBaseReason("main", d)}
	watchStep(w, snap, d, &now, LandPoll)
	if got := w.Moves()["o/r@main"]; got.To != c {
		t.Fatalf("an own landing after a foreign move replaced it: %+v", got)
	}
	watchStep(w, snap, d, &now, CIWait)
	if m := w.Moves(); len(m) != 0 {
		t.Fatalf("a foreign move stands past one CIWait: %+v", m)
	}
}
