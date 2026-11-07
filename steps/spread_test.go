package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// holders is each card's holder, in the order given.
func (s *sim) holders(ids ...string) []string {
	var out []string
	for _, id := range ids {
		out = append(out, s.card(id).Holder)
	}
	return out
}

// TestTickDealSpreadsByLoad: the deal spreads by load (directive 62): among
// rows that serve the card and that policy cannot tell apart, the row with
// the fewest live cards goes first, this tick's deals counted, before one
// row is filled to its width; with no policy and with policy naming the
// same row first for every card.
func TestTickDealSpreadsByLoad(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("a", 4, "pro")
	s.row("b", 4, "pro")
	s.row("c", 4, "pro")
	s.producer("p1", "s1", "pro", "a/1")
	s.producer("p2", "s1", "pro", "a/2")
	s.producer("p3", "s1", "pro", "a/3")
	s.run(2)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, s.holders("p1.w1", "p2.w1", "p3.w1"), "the engine's ranking spreads")

	p := newSim(t)
	p.ask = scripted{Answers{Version: 1, Models: benchModels(), Rows: map[string][]string{"p1": {"a", "b", "c"}, "p2": {"a", "b", "c"}, "p3": {"a", "b", "c"}}}}
	p.stream("s1")
	p.row("a", 4, "pro")
	p.row("b", 4, "pro")
	p.row("c", 4, "pro")
	p.producer("p1", "s1", "pro", "a/1")
	p.producer("p2", "s1", "pro", "a/2")
	p.producer("p3", "s1", "pro", "a/3")
	p.run(2)
	assert.Equal(t, []string{"a", "b", "c"}, p.holders("p1.w1", "p2.w1", "p3.w1"), "policy's answer, spread in its order")
}

// TestTickDealSpreadKeepsRank: the spread is among rows of one rank and
// kind; a row of the desired rank is still taken before an idle one of
// another rank.
func TestTickDealSpreadKeepsRank(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("a", 4, "pro")
	s.row("h", 4, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/1")
	s.producer("p2", "s1", "pro", "a/2")
	s.run(2)
	assert.Equal(t, []string{"a", "a"}, s.holders("p1.w1", "p2.w1"), "the desired rank first, though h is idle")
}
