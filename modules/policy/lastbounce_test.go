package policy

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestRowsSeesLastBounce: rows()'s card carries last_bounce, "" for a card
// never sent back, "fail" after a work fail, "fix" after a read said fix
// (card.LastBounce), and never the reason or finding text.
func TestRowsSeesLastBounce(t *testing.T) {
	t.Parallel()
	body, _ := override("rows", "def rows(card, kind, candidates):\n    if card.id == \"p1\":\n        fail(\"last_bounce=\" + card.last_bounce + \";\" + str(dir(card)))\n    return [c.name for c in candidates]")
	w := fakeWorld(t)
	work := func(result, reason string) store.Card {
		return store.Card{ID: "p1.w1", Kind: "work", Of: "p1", Stream: "s1", Tier: "pro", Holder: "r1", State: "done", Attempt: 1, Result: result, Reason: reason}
	}
	fixRead := store.Card{ID: "p1.r1.1", Kind: "read", Of: "p1", Stream: "s1", Tier: "pro", Holder: "r2", State: "done", Attempt: 1, Review: 1, Result: "fix", Reason: "the finding text"}
	for _, tc := range []struct {
		want   string
		review int
		cons   []store.Card
	}{
		{"", 0, nil},
		{"fail", 0, []store.Card{work("fail", "merge conflict in x.go")}},
		{"fix", 1, []store.Card{work("ok", ""), fixRead}},
	} {
		snap := w.snap()
		snap.Cards = maps.Clone(snap.Cards)
		p := snap.Cards["p1"]
		p.Attempt, p.Review = min(1, len(tc.cons)), tc.review
		snap.Cards["p1"] = p
		for _, c := range tc.cons {
			snap.Cards[c.ID] = c
		}
		_, fs := testEngine().Answer(Versions{Active: pv(3, body)}, snap, w.ro, FullQuestions(snap, nil))
		require.NotEmpty(t, fs, tc.want)
		msg := fs[0].Error()
		assert.Contains(t, msg, "last_bounce="+tc.want+";", tc.want)
		for _, prose := range []string{"merge conflict", "the finding text"} {
			assert.False(t, strings.Contains(msg, prose), "%q: no reason or finding text in the card", tc.want)
		}
	}
}
