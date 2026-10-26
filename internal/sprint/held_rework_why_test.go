package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 2026-10-18: rework of a card added held (add --held) said "no attempt has run, so there is
// nothing to rework: change its task instead: nova-sprint brief". The task is not what is in
// the way; the hold is, and the verb that clears it is release.
func TestReworkWhyOfAHeldCardNamesRelease(t *testing.T) {
	t.Parallel()
	for _, col := range []string{Waiting, Ready} {
		held := &Card{ID: "a", Row: "s", Col: col, Fields: map[string]string{FieldHeld: "2026-10-18T15:46:49Z"}}
		got := reworkWhy(held)
		assert.Contains(t, got, "nova-sprint release a --reason", "a held card in %s: %s", col, got)

		plain := &Card{ID: "a", Row: "s", Col: col, Fields: map[string]string{}}
		assert.Contains(t, reworkWhy(plain), "nova-sprint brief a --brief-file <path>")
		assert.NotContains(t, reworkWhy(plain), "release")
	}
}
