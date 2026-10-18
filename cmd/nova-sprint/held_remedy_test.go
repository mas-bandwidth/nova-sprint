package main

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
)

// 2026-10-18: a verb that cannot act on a card added held (add --held) -- accept, ask, return
// -- refused it with the remedy of a card that is waiting: change its task (brief) or drop it.
// A held card is not stuck on its task; it is held, and the verb that moves it is release
// (the coordinator\x27s, with a reason). The remedy names it first.
func TestAHeldCardsRefusalRemedyNamesRelease(t *testing.T) {
	t.Parallel()
	for _, col := range []string{sprint.Waiting, sprint.Ready} {
		held := &sprint.Card{ID: "a", Row: "s", Col: col, Fields: map[string]string{sprint.FieldHeld: "2026-10-18T15:46:49Z"}}
		got := cardRemedy(held)
		assert.Contains(t, got, "nova-sprint release a --reason", "a held card in %s is released: %s", col, got)
		assert.True(t, strings.HasPrefix(got, "nova-sprint release a --reason"), "release comes first: %s", got)

		plain := &sprint.Card{ID: "a", Row: "s", Col: col, Fields: map[string]string{}}
		assert.NotContains(t, cardRemedy(plain), "release", "a card that is not held is not released: %s", cardRemedy(plain))
		assert.Contains(t, cardRemedy(plain), "nova-sprint brief a --brief-file <path>", "its remedy stands")
	}
}
