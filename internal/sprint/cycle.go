package sprint

import (
	"fmt"
	"sort"
	"strings"
)

// The graph of what waits on what (docs/SPEC-SPRINT.md, sentinel cards). A
// waiting primary waits for the needs it names, and a card behind a sentinel
// waits for that sentinel; a sentinel waits for every primary of its stream
// before it. The last is never materialised as edges: a sentinel's need is
// the rank interval of its line from the sentinel before it (inclusive) up
// to itself, and the sentinel before it already needs everything earlier, so
// every card of a line is in exactly one sentinel's interval and one walk
// over the whole table is linear in its cards.
//
// Only a waiting card waits by its place in line: a card ready or in flight is
// past every stop. Every open card's named needs are edges, whatever its state
// (a card in flight that gained a need not landed goes back to waiting at the
// tick); a landed card, a card off the table or a name with no card needs
// nothing. A waived need counts as landed.

// needGraph is the graph over a snapshot and the cards an add would place.
//
// The graph is the union of a card's named needs (waived ones out), its
// LAND-AFTER edges and its place in line, over open cards only: a landed card,
// a dropped card, a card off the table and a name with no card are no node and
// need nothing. tla/WaitFor.tla WaitEdges and NoWaitCycle; docs/SPEC-SPRINT.md
// section 4 (Order).
type needGraph struct {
	s *Snapshot
	// named replaces the stored needs of a card (the add's cards and the
	// cards in line whose needs it changes).
	named map[string][]string
	// soft replaces the stored LAND-AFTER of a card (the edits that rewrite it).
	soft map[string][]string
	// adds is the add's cards, not yet on the table, by id; addsOf by stream.
	adds   map[string]*Card
	addsOf map[string][]*Card
	// waits marks cards in line the add moves back to waiting (ready cards
	// behind a sentinel inserted in front of them).
	waits map[string]bool
	lines map[string]*gateLine
	// keepLanded is the tests' seam of the reversed witness "landedpoisons"
	// (tla/WaitFor.tla WouldCycleLandedPoisons): a landed card's edges are
	// walked when true, as the code did before this rule. No verb sets it.
	keepLanded bool
	// steps counts every edge the walks look at: the bound is linear.
	steps int
}

// gateLine is a stream's open line (with the add's cards in it) in score
// order, and for each place the place of the last sentinel before it.
type gateLine struct {
	ids  []string
	at   map[string]int
	prev []int // -1: no sentinel before it
}

func newNeedGraph(s *Snapshot, named map[string][]string, adds []*Card, waits map[string]bool) *needGraph {
	return newNeedGraphSoft(s, named, nil, adds, waits)
}

// newNeedGraphSoft is newNeedGraph with the LAND-AFTER edges an edit writes
// (soft, by card id) over the stored ones.
func newNeedGraphSoft(s *Snapshot, named, soft map[string][]string, adds []*Card, waits map[string]bool) *needGraph {
	if soft == nil {
		soft = map[string][]string{}
	}
	g := &needGraph{s: s, named: named, soft: soft, adds: map[string]*Card{}, addsOf: map[string][]*Card{}, waits: waits, lines: map[string]*gateLine{}}
	for _, c := range adds {
		g.adds[c.ID] = c
		g.addsOf[c.Row] = append(g.addsOf[c.Row], c)
	}
	return g
}

func (g *needGraph) card(id string) *Card {
	if c := g.adds[id]; c != nil {
		return c
	}
	if g.s.Work == nil {
		return nil
	}
	return g.s.Work.Card(id)
}

// waiting says the card waits for anything at all.
func (g *needGraph) waiting(c *Card) bool {
	if c == nil || !c.Placed() {
		return false
	}
	return c.Col == Waiting || g.waits[c.ID]
}

func (g *needGraph) line(stream string) *gateLine {
	if l := g.lines[stream]; l != nil {
		return l
	}
	cards := append([]*Card(nil), g.s.Work.openLine(stream)...)
	cards = append(cards, g.addsOf[stream]...)
	SortCards(cards)
	l := &gateLine{ids: make([]string, len(cards)), at: make(map[string]int, len(cards)), prev: make([]int, len(cards))}
	last := -1
	for i, c := range cards {
		l.ids[i], l.at[c.ID], l.prev[i] = c.ID, i, last
		if IsSentinel(c) {
			last = i
		}
	}
	g.lines[stream] = l
	return l
}

// open says the card is a node of the graph: placed and open, or a landed one
// only under the tests' reversed-witness seam.
func (g *needGraph) open(c *Card) bool {
	return c != nil && c.Placed() && (IsOpen(c.Col) || g.keepLanded)
}

// hardNeeds is the named needs of a card (waived ones out), or the needs an
// edit proposes for it.
func (g *needGraph) hardNeeds(id string) []string {
	c := g.card(id)
	if n, ok := g.named[id]; ok {
		return n
	}
	if !g.open(c) {
		return nil
	}
	waived := Split(c.F("waived"))
	var named []string
	for _, n := range Split(c.F("needs")) {
		if !contains(waived, n) {
			named = append(named, n)
		}
	}
	return named
}

// softNeeds is the LAND-AFTER edges of a card, or the ones an edit proposes for
// it; only an open card needs anything (a landed card's order is history).
func (g *needGraph) softNeeds(id string) []string {
	if s, ok := g.soft[id]; ok {
		return s
	}
	c := g.card(id)
	if !g.open(c) {
		return nil
	}
	return SoftDepsOf(g.s, c)
}

// needs is what a card waits for: the needs it names, its LAND-AFTER edges and
// its place in line (a sentinel: its rank interval; a card behind a sentinel:
// that sentinel).
func (g *needGraph) needs(id string) (named, place []string) {
	c := g.card(id)
	named = append(g.hardNeeds(id), g.softNeeds(id)...)
	if !g.waiting(c) {
		return named, nil
	}
	l := g.line(c.Row)
	i, ok := l.at[id]
	if !ok {
		return named, nil
	}
	if IsSentinel(c) {
		from := max(l.prev[i], 0)
		return named, l.ids[from:i]
	}
	if p := l.prev[i]; p >= 0 {
		return named, l.ids[p : p+1]
	}
	return named, nil
}

// sccs is Tarjan's strongly connected components over what the roots reach,
// iterative: comp gives each node's component, size its size, and self the
// nodes that need themselves. Each node and each edge is looked at once.
type sccs struct {
	comp map[string]int
	size []int
	self map[string]bool
	// order is the nodes in the order the walk met them.
	order []string
}

func (s *sccs) cyclic(id string) bool {
	c, ok := s.comp[id]
	return ok && (s.size[c] > 1 || s.self[id])
}

func (g *needGraph) components(roots []string) *sccs {
	out := &sccs{comp: map[string]int{}, self: map[string]bool{}}
	index, low := map[string]int{}, map[string]int{}
	on := map[string]bool{}
	var stack []string
	type frame struct {
		id           string
		named, place []string
		k            int
	}
	next := func(f *frame) (string, bool) {
		if f.k < len(f.named) {
			f.k++
			return f.named[f.k-1], true
		}
		if j := f.k - len(f.named); j < len(f.place) {
			f.k++
			return f.place[j], true
		}
		return "", false
	}
	for _, root := range roots {
		if _, seen := index[root]; seen {
			continue
		}
		var frames []*frame
		push := func(id string) {
			index[id], low[id] = len(index), len(index)
			stack = append(stack, id)
			on[id] = true
			out.order = append(out.order, id)
			n, p := g.needs(id)
			frames = append(frames, &frame{id: id, named: n, place: p})
		}
		push(root)
		for len(frames) > 0 {
			f := frames[len(frames)-1]
			if w, ok := next(f); ok {
				g.steps++
				if w == f.id {
					out.self[w] = true
				}
				if _, seen := index[w]; !seen {
					push(w)
				} else if on[w] && index[w] < low[f.id] {
					low[f.id] = index[w]
				}
				continue
			}
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				if up := frames[len(frames)-1]; low[f.id] < low[up.id] {
					low[up.id] = low[f.id]
				}
			}
			if low[f.id] == index[f.id] {
				c := len(out.size)
				out.size = append(out.size, 0)
				for {
					x := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					on[x] = false
					out.comp[x] = c
					out.size[c]++
					if x == f.id {
						break
					}
				}
			}
		}
	}
	return out
}

// loop is the shortest way from id back to itself inside its component, as
// the path from id to id, with each run of sentinels of one stream that ends
// on a card before them told as the first sentinel needing that card.
func (g *needGraph) loop(c *sccs, id string) []string {
	comp := c.comp[id]
	if c.self[id] {
		return []string{id, id}
	}
	parent := map[string]string{id: ""}
	queue := []string{id}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		named, place := g.needs(x)
		for _, w := range append(append([]string(nil), named...), place...) {
			g.steps++
			if w == id {
				var path []string
				for y := x; y != ""; y = parent[y] {
					path = append(path, y)
				}
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return g.shorten(append(path, id))
			}
			if _, seen := parent[w]; seen {
				continue
			}
			if cw, ok := c.comp[w]; !ok || cw != comp {
				continue
			}
			parent[w] = x
			queue = append(queue, w)
		}
	}
	return nil
}

// shorten tells a sentinel that reaches a card before it through the
// sentinels between them as needing the card itself: it does, by its place.
func (g *needGraph) shorten(path []string) []string {
	before := func(a, b string) bool { // a is a card of b's stream before b
		ca, cb := g.card(a), g.card(b)
		return ca != nil && cb != nil && ca.Row == cb.Row && ca.Score < cb.Score
	}
	for changed := true; changed; {
		changed = false
		for i := 0; i+2 < len(path); i++ {
			if IsSentinel(g.card(path[i])) && IsSentinel(g.card(path[i+1])) && before(path[i+1], path[i]) && before(path[i+2], path[i]) {
				path = append(path[:i+1], path[i+2:]...)
				changed = true
			}
		}
	}
	return path
}

// sentinelsIn is the sentinels a loop passes through.
func (g *needGraph) sentinelsIn(loop []string) []string {
	var out []string
	for _, id := range loop[:len(loop)-1] {
		if IsSentinel(g.card(id)) && !contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// tellLoop is a loop as the refusal and check print it: each edge is
// marked -needs-> when the card names the next as a need, -land-after-> when
// it names it as a LAND-AFTER, or -line-> when the edge is its place in line
// (behind a sentinel, or a sentinel waiting on the cards before it), so the
// seat sees which edge is which.
func (g *needGraph) tellLoop(loop []string) string {
	var b strings.Builder
	b.WriteString(loop[0])
	for i := 0; i+1 < len(loop); i++ {
		switch {
		case contains(g.hardNeeds(loop[i]), loop[i+1]):
			b.WriteString(" -needs-> ")
		case contains(g.softNeeds(loop[i]), loop[i+1]):
			b.WriteString(" -land-after-> ")
		default:
			b.WriteString(" -line-> ")
		}
		b.WriteString(loop[i+1])
	}
	out := b.String()
	if gates := g.sentinelsIn(loop); len(gates) > 0 {
		out += " (through sentinel " + strings.Join(gates, ", ") + ": a card behind a sentinel needs it, and a sentinel needs every card of its stream before it)"
	}
	return out
}

// closes is the loop through the first of ids that is on a cycle, nil when
// none is: the ids are the cards a step places or changes, so a cycle through
// none of them was there before the step.
func (g *needGraph) closes(ids []string) []string {
	c := g.components(ids)
	for _, id := range ids {
		if c.cyclic(id) {
			return g.loop(c, id)
		}
	}
	return nil
}

// Cycles is every cycle on the table (a store written before this rule may
// hold one), each as its loop from its first card and the count of cards that
// can never be reached: the cards of the cycle and every card that needs one
// of them, through any chain. It walks the same union graph as the refuser --
// needs, LAND-AFTER and sentinel lines over open cards -- so a loop written
// before the rule is one judgment naming it, never a refusal of an edit that
// does not pass through it. tla/WaitFor.tla NoWaitCycle and
// EditRefusedOnlyForItsOwnCycle; docs/SPEC-SPRINT.md section 4 (Order).
func Cycles(s *Snapshot) []CycleFound {
	if s.Work == nil {
		return nil
	}
	var roots []string
	for _, c := range s.Work.Cards() {
		if c.Placed() && IsOpen(c.Col) {
			roots = append(roots, c.ID)
		}
	}
	sort.Strings(roots)
	g := newNeedGraph(s, nil, nil, nil)
	c := g.components(roots)
	firsts := map[int]string{}
	for _, id := range roots {
		if c.cyclic(id) {
			if _, ok := firsts[c.comp[id]]; !ok {
				firsts[c.comp[id]] = id
			}
		}
	}
	if len(firsts) == 0 {
		return nil
	}
	back := map[string][]string{}
	for _, x := range c.order {
		named, place := g.needs(x)
		for _, w := range named {
			back[w] = append(back[w], x)
		}
		for _, w := range place {
			back[w] = append(back[w], x)
		}
	}
	var out []CycleFound
	for _, id := range roots {
		comp, ok := c.comp[id]
		if !ok || firsts[comp] != id {
			continue
		}
		stuck := map[string]bool{id: true}
		queue := []string{id}
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			for _, y := range back[x] {
				if !stuck[y] {
					stuck[y] = true
					queue = append(queue, y)
				}
			}
		}
		loop := g.loop(c, id)
		out = append(out, CycleFound{Loop: loop, Sentinels: g.sentinelsIn(loop), Stuck: len(stuck), told: g.tellLoop(loop)})
	}
	return out
}

// CycleFound is one cycle of needs on the table.
type CycleFound struct {
	Loop      []string
	Sentinels []string
	Stuck     int // cards that can never be reached
	told      string
}

func (f CycleFound) String() string {
	return fmt.Sprintf("a cycle through %s: %d cards can never be reached", f.told, f.Stuck)
}
