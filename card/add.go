package card

import (
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// MoveAdd is the put-card plan of a new producer or sentinel: e is the card,
// cards every card its needs and after reach (through needs and after). It
// refuses a kind other than producer or sentinel, an id that exists, a
// sentinel with a brief or a score, a need that is not a producer, an after
// that is not a producer or sentinel, a name given twice, and a cycle through
// needs or after (the first cold read of layer 2). The new card is fenced
// absent. No other card is fenced: layer 1 refuses a need or after naming a
// card that does not exist, so no card of the store can name the new id
// before it lands, and a loop can close only through its own lists, which the
// walk reads from cards as passed.
func MoveAdd(e Add, cards map[string]Card) (Plan, *Refusal) {
	if r := addShape(e, cards); r != nil {
		return Plan{}, r
	}
	if r := addLists(e, cards); r != nil {
		return Plan{}, r
	}
	if loop, r := addCycle(e, cards); r != nil || loop != nil {
		if r == nil {
			r = no("%s", strings.Join(loop, " needs "))
		}
		return Plan{}, r
	}
	put := store.PutCard{ID: e.ID, Kind: e.Kind, Stream: e.Stream, Tier: e.Tier, Priority: e.Priority, Score: e.Score,
		Needs: slices.Clone(e.Needs), After: slices.Clone(e.After), Brief: e.Brief, Prose: e.Prose,
		Paths: slices.Clone(e.Paths), DoneWhen: slices.Clone(e.DoneWhen)}
	return checked(Plan{Expect: []store.Expect{store.CardAbsent{Card: e.ID}}, Writes: []store.Write{put}})
}

func addShape(e Add, cards map[string]Card) *Refusal {
	_, exists := cards[e.ID]
	level := e.Priority
	if level == "" {
		level = "normal"
	}
	switch {
	case e.Kind != "producer" && e.Kind != "sentinel":
		return no("Add makes a producer or sentinel, not a %s; consumers are cut by Start and AskRead", e.Kind)
	case exists:
		return no("card %s exists", e.ID)
	case e.Kind == "sentinel" && e.Brief != "":
		return no("sentinel %s has no brief", e.ID)
	case e.Kind == "sentinel" && e.Score != 0:
		return no("sentinel %s has no score of its own", e.ID)
	case !slices.Contains(Levels, level):
		return no("priority %s is not one of %s", level, orList(Levels))
	}
	return nil
}

// addLists refuses a need that is not a producer, an after that is not a
// producer or sentinel, and a name given twice in one list.
func addLists(e Add, cards map[string]Card) *Refusal {
	for i, n := range e.Needs {
		c, ok := cards[n]
		switch {
		case slices.Contains(e.Needs[:i], n):
			return no("need %s of %s %s is named twice", n, e.Kind, e.ID)
		case n == e.ID:
			return no("%s needs %s", e.ID, e.ID)
		case !ok:
			return no("need %s of %s %s is not a card", n, e.Kind, e.ID)
		case c.Kind != "producer":
			return no("need %s of %s %s is a %s; a need is a producer", n, e.Kind, e.ID, c.Kind)
		}
	}
	for i, a := range e.After {
		c, ok := cards[a]
		switch {
		case slices.Contains(e.After[:i], a):
			return no("after %s of %s %s is named twice", a, e.Kind, e.ID)
		case a == e.ID:
			return no("%s comes after %s", e.ID, e.ID)
		case !ok:
			return no("after %s of %s %s is not a card", a, e.Kind, e.ID)
		case consumer(c.Kind):
			return no("after %s of %s %s is a %s consumer; a card comes after a producer or sentinel", a, e.Kind, e.ID, c.Kind)
		}
	}
	return nil
}

// addCycle walks needs and after from the new card's lists through cards,
// and is the loop back to it, if any; a card the walk reaches that was not
// passed is refused, since the proof would be incomplete.
func addCycle(e Add, cards map[string]Card) ([]string, *Refusal) {
	seen := map[string]bool{}
	var refusal *Refusal
	var walk func(id, by string) []string
	walk = func(id, by string) []string {
		if id == e.ID {
			return []string{id}
		}
		if seen[id] || refusal != nil {
			return nil
		}
		c, ok := cards[id]
		if !ok {
			refusal = no("card %s, reached from %s, was not passed; Add needs every card reachable through needs and after", id, by)
			return nil
		}
		seen[id] = true
		for _, n := range append(slices.Clone(c.Needs), c.After...) {
			if p := walk(n, id); p != nil {
				return append([]string{id}, p...)
			}
		}
		return nil
	}
	for _, n := range append(slices.Clone(e.Needs), e.After...) {
		if p := walk(n, e.ID); p != nil {
			return append([]string{e.ID}, p...), nil
		}
	}
	return nil, refusal
}
