package steps

import (
	"fmt"
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

// baseMoved opens one base-moved judgement per watched repository and base
// (about = repo@base) when a foreigner moved it while cards wait in review or
// merging on its streams.
func (t *tick) baseMoved() {
	for _, about := range sortedKeys(t.in.Moves) {
		m := t.in.Moves[about]
		var streams []string
		waiting := 0
		for _, s := range m.Streams {
			if w := t.waitingToLand(s); w > 0 {
				streams = append(streams, s)
				waiting += w
			}
		}
		if waiting == 0 {
			continue
		}
		t.detect("base-moved", about)
		if t.pending("base-moved", about) {
			continue
		}
		n := t.episodes("base-moved", about)
		last := t.v.Judgements[fmt.Sprintf("base-moved.%s.%d", about, n)]
		same := strings.Contains(last.Question, " was moved to "+shortSHA(m.To)+" ")
		q := baseMovedQuestion(streams, m, waiting)
		switch {
		case last.State == "open" && same:
			t.held("base-moved", about)
			continue
		case last.State == "open":
			t.expires[last.ID] = fmt.Sprintf("the base was moved again (to %s): reopened as base-moved.%s.%d", shortSHA(m.To), about, n+1)
		case last.State == "answered" && same:
			if since, quiet := t.answeredQuiet("base-moved", about); quiet {
				continue
			} else if since > 0 {
				q = fmt.Sprintf("STILL HOLDS %s after your answer: %s", since.Truncate(time.Second), q)
			}
		}
		t.addJudge(judgement{kind: "base-moved", about: about, question: q})
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

func baseMovedQuestion(streams []string, m BaseMove, waiting int) string {
	cards := fmt.Sprintf("%d cards wait", waiting)
	if waiting == 1 {
		cards = "1 card waits"
	}
	who := "stream " + streams[0]
	if len(streams) > 1 {
		who = "streams " + firstIDs(streams)
	}
	return fmt.Sprintf("%s %s (%s) was moved to %s by something other than this sprint's landing (%s to land on it): "+
		"a stream lands into its own branch, which only its landings move; stop merging into %s; "+
		"put other work on its own branch and merge the stream into main once it is green", m.Repo, m.Base, who, shortSHA(m.To), cards, m.Base)
}

// clearedMoved is why an open base-moved judgement about repo@base no longer
// holds: no foreign move of that base within one CIWait, no card on its
// streams waits to land, or a legacy per-stream judgement was superseded.
func (t *tick) clearedMoved(about string) string {
	if !strings.Contains(about, "@") {
		return "cleared: per-stream base-moved judgements are now one per repo@base"
	}
	if m, ok := t.in.Moves[about]; ok {
		waiting := 0
		for _, s := range m.Streams {
			waiting += t.waitingToLand(s)
		}
		if waiting == 0 {
			return fmt.Sprintf("cleared: no card on %s %s waits to land", m.Repo, m.Base)
		}
		return ""
	}
	repo, base, _ := strings.Cut(about, "@")
	return fmt.Sprintf("cleared: %s %s has not been moved by anything but the sprint's landing for one CIWait", repo, base)
}
