package steps

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rate limiting (v1's, brought back): a consumer that faulted on a
// provider's rate limit rests its model for policy's route_rest_for, on
// every row; and a provider's concurrency budget caps the consumers working
// on its models across the fleet. Unit: store.Fake, the clock an argument.

// working is the work consumers working now.
func (s *sim) working() []string {
	var out []string
	for id, c := range s.snap().Cards {
		if c.Kind == "work" && c.State == "working" {
			out = append(out, id)
		}
	}
	return out
}

// TestTickRateLimitRestsTheModel: p1's attempt faults on a rate limit; p1
// goes back to ready, but no row is dealt the model until the rest ends.
func TestTickRateLimitRestsTheModel(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("b1", 1, "pro")
	s.row("b2", 1, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(3)
	require.Equal(t, "working", s.state("p1.w1"))
	s.take("p1.w1")
	s.report("p1.w1", "fault", "", "rate limited on test/pro: Rate limit reached: input token limit exceeded")
	s.run(3)
	assert.Equal(t, "ready", s.state("p1"), "the model rests: two rows with room, neither is dealt it")
	assert.Equal(t, 0, s.card("p1").Bounces, "a rate limit spends no attempt")
	s.now = s.now.Add(1800 * time.Second)
	s.run(3)
	assert.Equal(t, "working", s.state("p1"), "the rest ended: dealt again")
}

// TestTickProviderConcurrencyBudget: with the provider's budget at 1, two
// rows with room take one card between them; when it finishes the other is
// dealt. With no budget both are dealt at once.
func TestTickProviderConcurrencyBudget(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.ask = scripted{Answers{Models: benchModels(), Concurrency: map[string]int{"test": 1}}}
	s.row("b1", 1, "pro")
	s.row("b2", 1, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(3)
	first := s.working()
	require.Len(t, first, 1, "the provider's budget is 1")
	s.take(first[0])
	s.report(first[0], "fail", "", "tests fail")
	s.run(3)
	next := s.working()
	require.Len(t, next, 1, "one finished, one more dealt, still at the budget")
	assert.NotEqual(t, first[0], next[0])

	u := newSim(t)
	u.row("b1", 1, "pro")
	u.row("b2", 1, "pro")
	u.stream("s1")
	u.producer("p1", "s1", "pro", "a/x")
	u.producer("p2", "s1", "pro", "a/y")
	u.run(3)
	assert.Len(t, u.working(), 2, "no budget: unlimited, today's behaviour")
}
