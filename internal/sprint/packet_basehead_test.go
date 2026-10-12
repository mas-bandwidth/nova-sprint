package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	shaA = "0123456789abcdef0123456789abcdef01234567"
	shaB = "89abcdef0123456789abcdef0123456789abcdef"
)

// attemptCard is the work card of attempt n of p1 as its finish left it.
func attemptCard(n, ok, head string) *Card {
	return &Card{ID: "p1.w" + n, Fields: map[string]string{"kind": "work", "primary": "p1", "attempt": n, "ok": ok, "head": head, "branch": "sprint/p1.w" + n}}
}

// A later attempt starts from the last head any earlier attempt finished ok at, never from a
// branch name alone and whatever happened to the attempts after it: an attempt that failed, or
// finished at no commit, leaves no head, and when no attempt pushed one the attempt is staged
// from the card's base (docs/SPEC-CARD-CONTRACT.md layer 1; tla/CardContract.tla,
// RestagedAtLastPushedHead).
func TestALaterAttemptStartsFromTheLastPushedHeadOfAnyEarlierAttempt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		earlier []*Card
		attempt int // the attempt whose head is the base, 0 for the card's base
		head    string
	}{
		{"attempt 1 pushed, attempt 2 failed with no commit", []*Card{attemptCard("1", "yes", shaA), attemptCard("2", "no", "")}, 1, shaA},
		{"the same, the cards in the other order", []*Card{attemptCard("2", "no", ""), attemptCard("1", "yes", shaA)}, 1, shaA},
		{"attempts 1 and 2 both pushed", []*Card{attemptCard("1", "yes", shaA), attemptCard("2", "yes", shaB)}, 2, shaB},
		{"attempt 2 pushed, attempt 3 finished at no commit", []*Card{attemptCard("1", "no", ""), attemptCard("2", "yes", shaB), attemptCard("3", "yes", "p1.w3")}, 2, shaB},
		{"a failed latest attempt still retains its pushed head", []*Card{attemptCard("1", "yes", shaA), attemptCard("2", "no", shaB)}, 2, shaB},
		{"no attempt pushed", []*Card{attemptCard("1", "no", ""), attemptCard("2", "yes", "p1.w2")}, 0, ""},
		{"an attempt still in flight", []*Card{attemptCard("1", "", "")}, 0, ""},
		{"no earlier attempt", nil, 0, ""},
	} {
		want := Base{Attempt: tc.attempt, Head: tc.head}
		assert.Equal(t, want, BaseOf(tc.earlier), tc.name)
		work := &Card{ID: "p1.w9", Row: "m1", Fields: map[string]string{"kind": "work", "primary": "p1", "attempt": "9", "gen": "1"}}
		primary := &Card{ID: "p1", Fields: map[string]string{"attempt": "9", "brief": "b", "fix": "f.go:3 the bound"}}
		p := PacketOf("", 3, work, primary, tc.earlier, nil)
		assert.Equal(t, tc.head, p.BaseHead, tc.name)
		assert.Equal(t, tc.attempt, p.BaseAttempt, tc.name)
		assert.Equal(t, "sprint/p1.w9.g1.e3", p.Branch, tc.name)
		assert.Equal(t, "f.go:3 the bound", p.Fix, tc.name)
	}
}

// A finish-PR card carries the PR's head in its brief's CARRY: line: a first attempt (no
// earlier pushed head) is staged from it, so the worker builds on the PR's work rather than
// one commit off the base; the base is still merged in by the worker, never replaced. A
// pushed earlier attempt still wins over a stale carry in the brief.
func TestAFinishPRPacketStagesFromTheCarriedHead(t *testing.T) {
	t.Parallel()
	work := &Card{ID: "pr1.w1", Row: "m1", Fields: map[string]string{"kind": "work", "primary": "pr1", "attempt": "1", "gen": "1"}}
	primary := &Card{ID: "pr1", Fields: map[string]string{"attempt": "1",
		"brief": "finish pr, tier: pro\nCARRY: head=" + shaA + " branch=pr-1\n\nTHE TASK. Start from the PR's branch pr-1 at " + shaA + ".\n"}}
	p := PacketOf("", 0, work, primary, nil, nil)
	assert.Equal(t, shaA, p.BaseHead, "the slot stages from the PR head")
	assert.Equal(t, 0, p.BaseAttempt, "no attempt of this card's pushed it")

	q := PacketOf("", 0, work, primary, []*Card{attemptCard("1", "yes", shaB)}, nil)
	assert.Equal(t, shaB, q.BaseHead, "the last pushed head wins over a stale carry")
}

// A packet reads every earlier attempt of the card, and a read's reads none.
func TestPacketCardsNameEveryEarlierAttempt(t *testing.T) {
	t.Parallel()
	w := &Card{ID: "p1.w4", Fields: map[string]string{"kind": "work", "primary": "p1", "attempt": "4"}}
	p, earlier, work := PacketCards(w)
	assert.Equal(t, "p1", p)
	assert.Equal(t, []string{"p1.w1", "p1.w2", "p1.w3"}, earlier)
	assert.Empty(t, work)
	r := &Card{ID: "p1.r2.x", Fields: map[string]string{"kind": "read", "primary": "p1", "attempt": "2"}}
	_, earlier, work = PacketCards(r)
	assert.Empty(t, earlier)
	assert.Equal(t, "p1.w2", work)
}

// `card` says where the attempt after starts, from the same BaseOf the packet reads.
func TestNextLineSaysWhichHeadTheNextAttemptStartsFrom(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "NEXT starts from attempt 1 head="+shaA, NextLine([]*Card{attemptCard("1", "yes", shaA), attemptCard("2", "no", "")}))
	assert.Equal(t, "NEXT starts from the card's base: no attempt pushed a head", NextLine([]*Card{attemptCard("1", "no", "")}))
}
