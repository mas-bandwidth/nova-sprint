package dash

import (
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// waitLine is one row of the board's Why waiting panel: a stream and why
// its producers wait, as check says it (steps.Waits).
type waitLine struct {
	Stream string `json:"stream"`
	Why    string `json:"why"`
}

// waitLines is the Why waiting panel: one line per stream whose producers
// wait or whose longest NEEDS chain is steps.ChainWarn cards or more, and
// the summary over every stream (steps.IdleWhy: the chains and overlaps,
// and the command that moves the most).
func waitLines(cards map[string]store.Card) ([]waitLine, string) {
	ws := steps.Waits(cards)
	var out []waitLine
	for _, s := range ws {
		why := s.Line()
		if len(s.Chain) >= steps.ChainWarn {
			chain := fmt.Sprintf("longest NEEDS chain %d: %s", len(s.Chain), steps.ChainText(s.Chain))
			if why != "" {
				why += "; "
			}
			why += chain
		}
		if why != "" {
			out = append(out, waitLine{Stream: s.Stream, Why: why})
		}
	}
	what, fix := steps.IdleWhy(ws)
	if what == "" {
		return out, ""
	}
	return out, what + "; " + fix
}
