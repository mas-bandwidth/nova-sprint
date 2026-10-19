package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFleetHelpDoesNotSayReadsAreOneAtATime: a pro card's two reads are asked
// together (internal/sprint/read_cards.go readCardsAskFull), each by a
// different reader, each on a route of the card's tier; the readers help must
// not say the old sequential rule.
func TestFleetHelpDoesNotSayReadsAreOneAtATime(t *testing.T) {
	t.Parallel()
	help := banner()
	a := newApp(func(string) string { return "" })
	var out, errb bytes.Buffer
	code := runInto(a, []string{"help", "reader"}, &out, &errb)
	require.Equal(t, 0, code, errb.String())
	help += out.String()
	require.Contains(t, help, "The readers:", "the readers help is captured")
	for _, stale := range []string{"one read at a time", "second asked once the first comes back ok"} {
		assert.NotContains(t, strings.ToLower(help), stale, "the readers help still says the old sequential rule: %q", stale)
	}
}
