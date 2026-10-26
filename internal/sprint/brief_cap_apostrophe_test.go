package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A ' with a letter or digit immediately before it and a letter immediately
// after it is a word's apostrophe, not an opening quote: collapsing whitespace
// after "don't" must not treat the rest of the line as quoted text and reset the
// attempt cap. A ' that opens a real literal still does, as every " and
// backtick does.
func TestAnApostropheInProseIsNoQuote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, next, decided string
	}{
		{
			name:    "ProseApostropheKeepsTheCount",
			old:     "tier: pro\nPATHS: internal/x.go\n\ndon't   touch",
			next:    "tier: pro\nPATHS: internal/x.go\n\ndon't touch",
			decided: "1",
		},
		{
			name:    "AQuotedLiteralStillResetsTheCount",
			old:     "tier: pro\nPATHS: internal/x.go\n\nthe task handles 'a  b'",
			next:    "tier: pro\nPATHS: internal/x.go\n\nthe task handles 'a b'",
			decided: "2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := boundWorld(t)
			w.s.Work.Placed("a-1").Fields["brief"] = tc.old
			w.s.Work.Placed("a-1").Fields[FieldDecidedAttempt] = "1"
			p := w.must(Brief(w.s, briefOf("a-1", tc.next, "")))
			assert.Equal(t, tc.decided, w.s.Work.Placed("a-1").F(FieldDecidedAttempt), "the cap window after the edit")
			if tc.decided == "1" {
				assert.Contains(t, strings.Join(p.Said, "\n"), "max attempts: unchanged (the edit decides nothing about the task)", "%v", p.Said)
			} else {
				assert.Contains(t, strings.Join(p.Said, "\n"), "max attempts: decided at attempt 2", "%v", p.Said)
			}
		})
	}
}
