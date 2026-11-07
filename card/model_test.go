package card

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The deal's model: Start and AskReads carry the model the deal
// names, and the consumer is cut with it, working in its row's hand, so the take
// receipt reads it from the consumer and never invents it. Unit: store.Fake.
func TestModelCarriedByTheDeal(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b-p1", Base: base, Model: "deepseek/deepseek-flash"})
	assert.Equal(t, "deepseek/deepseek-flash", w.snap().Cards["p1.w1"].Model, "the work consumer carries its deal's model")
	assert.Equal(t, "", w.snap().Cards["p1"].Model, "the producer carries none")
	w.consume("p1.w1", Report{Row: "r1", Result: "ok", Head: h1})
	w.move("p1", Finished{Consumer: "p1.w1"})
	w.refused("p1", AskReads{Rows: []string{"r2", "r3"}, Models: []string{"inception/mercury-3"}},
		"AskReads of producer p1 names 2 rows and 1 models; name one model per row")
	w.move("p1", AskReads{Rows: []string{"r2", "r3"}, Models: []string{"inception/mercury-3", "m-friend"}})
	got := map[string]string{}
	for _, c := range w.snap().Cards {
		if c.Kind == "read" {
			got[c.Holder] = c.Model
		}
	}
	assert.Equal(t, map[string]string{"r2": "inception/mercury-3", "r3": "m-friend"}, got, "each read carries its row's model")
}
