package steps

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// A base moved by anything but the sprint's landing wakes the coordinator
// (2026-11-11: other work merged into main while the sprint's landing
// group built against main, and nothing landed for over an hour). A stream
// lands in batches into its own branch, which only its landings move; that
// branch is made green; then the stream is merged into main once. Layer 6
// sees each watched base's tip (run.BaseWatch, no forge read of its own)
// and passes, for one CIWait after it, a move to a commit that is none of
// the sprint's own landings on that base's streams. Each stream on it with
// cards in review or merging has one open base-moved judgement; a further
// foreign move replaces it with the next episode, pushed again; it clears
// when no foreign move has been seen for one CIWait. Answered, the same
// move stays quiet (answeredQuiet). tla/BaseMoved.tla is the model.

// BaseMove is a watched repository and base whose tip was moved to To, at
// At, by something other than the sprint's landing; Streams land on it.
type BaseMove struct {
	Repo, Base, To string
	At             time.Time
	Streams        []string
}

// baseMoved opens the base-moved judgement of each stream with cards in
// review or merging on a base a foreigner moved.
func (t *tick) baseMoved() {
	for _, about := range sortedKeys(t.in.Moves) {
		m := t.in.Moves[about]
		for _, s := range m.Streams {
			waiting := t.waitingToLand(s)
			if waiting == 0 {
				continue
			}
			t.detect("base-moved", s)
			if t.pending("base-moved", s) {
				continue
			}
			n := t.episodes("base-moved", s)
			last := t.v.Judgements[fmt.Sprintf("base-moved.%s.%d", s, n)]
			same := strings.Contains(last.Question, " was moved to "+shortSHA(m.To)+" ")
			q := baseMovedQuestion(s, m, waiting)
			switch {
			case last.State == "open" && same:
				t.held("base-moved", s)
				continue
			case last.State == "open":
				t.expires[last.ID] = fmt.Sprintf("the base was moved again (to %s): reopened as base-moved.%s.%d", shortSHA(m.To), s, n+1)
			case last.State == "answered" && same:
				if since, quiet := t.answeredQuiet("base-moved", s); quiet {
					continue
				} else if since > 0 {
					q = fmt.Sprintf("STILL HOLDS %s after your answer: %s", since.Truncate(time.Second), q)
				}
			}
			t.addJudge(judgement{kind: "base-moved", about: s, question: q})
		}
	}
}

// waitingToLand is stream s's producers in review or merging.
func (t *tick) waitingToLand(s string) int {
	n := 0
	for _, state := range []string{"review", "merging"} {
		for id := range t.v.byState[state] {
			if c := t.v.Cards[id]; c.Kind == "producer" && c.Stream == s {
				n++
			}
		}
	}
	return n
}

func baseMovedQuestion(s string, m BaseMove, waiting int) string {
	cards := fmt.Sprintf("%d cards wait", waiting)
	if waiting == 1 {
		cards = "1 card waits"
	}
	return fmt.Sprintf("stream %s's base %s was moved to %s by something other than this sprint's landing (%s to land on it): "+
		"a stream lands into its own branch, which only its landings move; stop merging into %s; "+
		"put other work on its own branch and merge the stream into main once it is green", s, m.Base, shortSHA(m.To), cards, m.Base)
}

// clearedMoved is why an open base-moved judgement about stream s no longer
// holds: no foreign move of its base within one CIWait, or no card of it
// waits to land.
func (t *tick) clearedMoved(s string) string {
	for _, about := range sortedKeys(t.in.Moves) {
		if m := t.in.Moves[about]; slices.Contains(m.Streams, s) {
			if t.waitingToLand(s) == 0 {
				return "cleared: no card of stream " + s + " waits to land"
			}
			return ""
		}
	}
	st := t.v.Streams[s]
	return fmt.Sprintf("cleared: %s %s has not been moved by anything but the sprint's landing for one CIWait", st.Repo, st.Base)
}
