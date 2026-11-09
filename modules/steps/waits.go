package steps

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Why the fleet is idle (intent rule 33: nothing sits unexplained; Glenn,
// 2026-11-07: "keep everybody working at width"). A waiting producer waits
// for one of two things: a need not landed, or a file a ready or started
// producer of its stream names (the needs step's heldBehind). Waits groups
// the waiting producers of each stream by the card whose landing would free
// them (the front of the NEEDS chain each waits on) and by the producer and
// file each is held behind, and finds the stream's longest NEEDS chain.
// check, the board and the idle judgement all read these, so the three say
// the same thing.

// StreamWaits is one stream's waiting producers, explained.
type StreamWaits struct {
	Stream   string
	Blockers []Blocker // the fronts of the chains its waiting producers wait on, most waiting first
	Holders  []Holder  // the producers and files its waiting producers are held behind, most held first
	Chain    []string  // its longest NEEDS chain of cards not landed, the first to land first
}

// Blocker is a card not landed that Count waiting producers wait on, each
// through a chain of NEEDS whose front it is: landing it is what moves them.
// State is its state, "missing" for a need that names no card.
type Blocker struct {
	Card, State string
	Reason      string // the card's reason: a waiting card's hold (card.HeldReason) is answered by unhold
	Count       int
}

// Holder is a producer, ready or started, that names File, which Count
// waiting producers (needs landed) also name.
type Holder struct {
	Card, File string
	Count      int
}

// waitScan memoizes the fronts and chain depths over one card map.
type waitScan struct {
	cards map[string]store.Card
	front map[string]string // card -> the front of the chain it waits on ("" none)
	depth map[string]int    // card -> its longest chain's length, in producers
	prev  map[string]string // card -> the need its longest chain runs through
	paths map[string]map[string]set
}

// Waits is every stream with a producer not landed or failed, in name
// order, its waiting producers explained (cards is a snapshot's or a view's
// cards: consumers are skipped).
func Waits(cards map[string]store.Card) []StreamWaits {
	w := newWaitScan(cards)
	by, deepest := map[string]*StreamWaits{}, map[string]string{}
	fronts, held := map[string]map[string]int{}, map[string]map[[2]string]int{}
	for _, id := range sortedKeys(cards) {
		c := cards[id]
		if isConsumer(c) || isFinal(c) {
			continue
		}
		s := by[c.Stream]
		if s == nil {
			s = &StreamWaits{Stream: c.Stream}
			by[c.Stream], fronts[c.Stream], held[c.Stream] = s, map[string]int{}, map[[2]string]int{}
		}
		if d, at := w.depthOf(id, set{}), deepest[c.Stream]; at == "" || d > w.depth[at] {
			deepest[c.Stream] = id
		}
		if c.Kind != "producer" || c.State != "waiting" {
			continue
		}
		if f := w.frontOf(id, set{}); f != "" {
			fronts[c.Stream][f]++
		} else if hb, on := leastHolder(c, func(p string) []set { return []set{w.paths[c.Stream][p]} }); hb != "" {
			held[c.Stream][[2]string{hb, on}]++
		}
	}
	out := make([]StreamWaits, 0, len(by))
	for _, name := range sortedKeys(by) {
		s := by[name]
		s.Blockers, s.Holders, s.Chain = w.blockers(fronts[name]), holders(held[name]), w.chain(deepest[name])
		out = append(out, *s)
	}
	return out
}

// LongestChain is stream's longest NEEDS chain over cards, the first to
// land first (empty when the stream has no card not landed).
func LongestChain(cards map[string]store.Card, stream string) []string {
	for _, s := range Waits(cards) {
		if s.Stream == stream {
			return s.Chain
		}
	}
	return nil
}

// ChainThrough is the longest NEEDS chain over cards that runs through one
// of ids, of cards not landed or failed, the first to land first: what add
// holds to the policy's chain_limit, so an add beside a long chain is not
// refused for it.
func ChainThrough(cards map[string]store.Card, ids []string) []string {
	w := newWaitScan(cards)
	deps := map[string][]string{}
	for _, id := range sortedKeys(cards) {
		if c := cards[id]; !isConsumer(c) && !isFinal(c) {
			for _, n := range c.Needs {
				deps[n] = append(deps[n], id)
			}
		}
	}
	down := map[string][]string{} // card -> its longest chain of dependants after it
	var after func(string, set) []string
	after = func(id string, seen set) []string {
		if d, ok := down[id]; ok {
			return d
		}
		seen[id] = struct{}{}
		var best []string
		for _, n := range deps[id] {
			if _, loop := seen[n]; loop {
				continue
			}
			d := after(n, seen)
			if w.cards[n].Kind != "sentinel" {
				d = append([]string{n}, d...)
			}
			if len(d) > len(best) {
				best = d
			}
		}
		delete(seen, id)
		down[id] = best
		return best
	}
	var best []string
	for _, id := range ids {
		w.depthOf(id, set{})
		if c := append(w.chain(id), after(id, set{})...); len(c) > len(best) {
			best = c
		}
	}
	return best
}

func newWaitScan(cards map[string]store.Card) *waitScan {
	w := &waitScan{cards: cards, front: map[string]string{}, depth: map[string]int{}, prev: map[string]string{}, paths: map[string]map[string]set{}}
	for id, c := range cards {
		if isConsumer(c) || !slices.Contains(started, c.State) {
			continue
		}
		for _, p := range c.Paths {
			if Concrete(p) {
				if w.paths[c.Stream] == nil {
					w.paths[c.Stream] = map[string]set{}
				}
				add(w.paths[c.Stream], p, id)
			}
		}
	}
	return w
}

// isFinal is a card that will not move again: landed or failed.
func isFinal(c store.Card) bool { return c.State == "landed" || c.State == "fail" }

// firstUnlanded is the first of c's needs not landed, "" when all are.
func (w *waitScan) firstUnlanded(c store.Card) string {
	for _, n := range c.Needs {
		if x, ok := w.cards[n]; !ok || x.State != "landed" {
			return n
		}
	}
	return ""
}

// frontOf is the front of the chain id waits on: its first need not
// landed, followed while that need is itself waiting on a need not landed;
// "" when every need of id has landed. seen guards a cycle (add refuses
// one; a store op could still write one).
func (w *waitScan) frontOf(id string, seen set) string {
	if f, ok := w.front[id]; ok {
		return f
	}
	f := w.firstUnlanded(w.cards[id])
	if x, ok := w.cards[f]; ok && x.State == "waiting" && w.firstUnlanded(x) != "" {
		if _, loop := seen[f]; !loop {
			seen[id] = struct{}{}
			f = w.frontOf(f, seen)
		}
	}
	w.front[id] = f
	return f
}

// chain is id's longest NEEDS chain of cards not landed or failed, the
// first to land first (depthOf has run for id); a sentinel adds no length
// and is not listed.
func (w *waitScan) chain(id string) []string {
	var out []string
	for at := id; at != ""; at = w.prev[at] {
		if w.cards[at].Kind != "sentinel" {
			out = append(out, at)
		}
	}
	slices.Reverse(out)
	return out
}

// depthOf is the longest chain's length ending at id, memoized with the
// need it runs through (ties to the least id).
func (w *waitScan) depthOf(id string, seen set) int {
	if d, ok := w.depth[id]; ok {
		return d
	}
	seen[id] = struct{}{}
	best, through := 0, ""
	for _, n := range sortedNeeds(w.cards[id]) {
		x, ok := w.cards[n]
		if _, loop := seen[n]; !ok || loop || isFinal(x) {
			continue
		}
		if d := w.depthOf(n, seen); d > best {
			best, through = d, n
		}
	}
	delete(seen, id)
	if w.cards[id].Kind != "sentinel" {
		best++
	}
	w.depth[id], w.prev[id] = best, through
	return best
}

func sortedNeeds(c store.Card) []string {
	ns := slices.Clone(c.Needs)
	slices.Sort(ns)
	return ns
}

// blockers is the fronts by count, most first, then by id.
func (w *waitScan) blockers(m map[string]int) []Blocker {
	out := make([]Blocker, 0, len(m))
	for id, n := range m {
		st, why := "missing", ""
		if c, ok := w.cards[id]; ok {
			st, why = c.State, c.Reason
		}
		out = append(out, Blocker{Card: id, State: st, Reason: why, Count: n})
	}
	slices.SortFunc(out, func(a, b Blocker) int { return cmpCount(a.Count, b.Count, a.Card, b.Card) })
	return out
}

// holders is the holders by count, most first, then by card and file.
func holders(m map[[2]string]int) []Holder {
	out := make([]Holder, 0, len(m))
	for k, n := range m {
		out = append(out, Holder{Card: k[0], File: k[1], Count: n})
	}
	slices.SortFunc(out, func(a, b Holder) int { return cmpCount(a.Count, b.Count, a.Card+"\x00"+a.File, b.Card+"\x00"+b.File) })
	return out
}

func cmpCount(a, b int, ka, kb string) int {
	if a != b {
		return b - a
	}
	return strings.Compare(ka, kb)
}

// Waiting is how many waiting producers the stream's blockers and holders
// explain.
func (s StreamWaits) Waiting() (onNeeds, onFiles int) {
	for _, b := range s.Blockers {
		onNeeds += b.Count
	}
	for _, h := range s.Holders {
		onFiles += h.Count
	}
	return onNeeds, onFiles
}

// shown is how many of a stream's blockers or holders a line names.
const shown = 3

// Line is the stream's why on one line, "" when nothing of it waits:
// "53 wait on a (working), 2 on b (waiting); 3 held behind src/x.go of c".
func (s StreamWaits) Line() string {
	var parts []string
	if len(s.Blockers) > 0 {
		var bs []string
		for i, b := range s.Blockers[:min(len(s.Blockers), shown)] {
			verb := " on "
			if i == 0 {
				verb = " wait on "
			}
			bs = append(bs, fmt.Sprintf("%d%s%s (%s)", b.Count, verb, b.Card, b.State))
		}
		parts = append(parts, strings.Join(bs, ", ")+more(len(s.Blockers), "card"))
	}
	if len(s.Holders) > 0 {
		var hs []string
		for i, h := range s.Holders[:min(len(s.Holders), shown)] {
			verb := " behind "
			if i == 0 {
				verb = " held behind "
			}
			hs = append(hs, fmt.Sprintf("%d%s%s of %s", h.Count, verb, h.File, h.Card))
		}
		parts = append(parts, strings.Join(hs, ", ")+more(len(s.Holders), "file"))
	}
	return strings.Join(parts, "; ")
}

func more(n int, what string) string {
	if n <= shown {
		return ""
	}
	return fmt.Sprintf(" and %d more %ss", n-shown, what)
}

// ChainText is a chain as "a -> b -> ... -> z", at most six names.
func ChainText(chain []string) string {
	if len(chain) <= 6 {
		return strings.Join(chain, " -> ")
	}
	return strings.Join(chain[:4], " -> ") + " -> ... -> " + chain[len(chain)-1]
}

// Fix is the command for a blocker: a held one (waiting on a hold or a
// missing route) unheld, a missing or failed one relinked away from, one in
// flight looked at (its landing frees the chain). Never a priority: levels
// are set by request only (intent rule 14).
func (b Blocker) Fix() string {
	switch {
	case b.State == card.Waiting && card.HeldReason(b.Reason):
		return "nova-sprint unhold " + b.Card + " --reason \"<why>\" (it waits: " + b.Reason + ")"
	}
	switch b.State {
	case "missing", "fail":
		return "nova-sprint relink <card> --need " + b.Card + " --to <card> --reason \"<why>\""
	}
	return "nova-sprint show " + b.Card + " (it is " + b.State + "; its landing frees the chain)"
}

// unchain is how a chain is made shorter during a sprint: a NEEDS that is
// not a consumed output goes with a corrected brief.
const unchain = "a NEEDS that is not an output the card consumes: nova-sprint replace <card> --by <its brief without it> --reason \"<why>\""

// IdleWhy is why nothing is dealable, over every stream: the chains (how
// many fronts, the longest, the front most wait on) and the overlaps (how
// many producers are held behind a file), and the command that helps most;
// "" when nothing waits.
func IdleWhy(ws []StreamWaits) (what, fix string) {
	fronts, longest, held := 0, 0, 0
	var top Blocker
	var hold Holder
	for _, s := range ws {
		fronts += len(s.Blockers)
		longest = max(longest, len(s.Chain))
		for _, b := range s.Blockers {
			if b.Count > top.Count || (b.Count == top.Count && b.Card < top.Card) {
				top = b
			}
		}
		for _, h := range s.Holders {
			held += h.Count
			if h.Count > hold.Count {
				hold = h
			}
		}
	}
	if fronts == 0 && held == 0 {
		return "", ""
	}
	what = fmt.Sprintf("%d chain%s (longest %d", fronts, plural(fronts), longest)
	if top.Card != "" {
		what += fmt.Sprintf(", top: %s blocks %d", top.Card, top.Count)
	}
	what += fmt.Sprintf("), %d overlap%s", held, plural(held))
	if hold.Card != "" {
		what += fmt.Sprintf(" (top: %s holds %d on %s)", hold.Card, hold.Count, hold.File)
	}
	fix = "each stream's waits and chains: nova-sprint check"
	if top.Card != "" {
		fix += "; the top: " + top.Fix() + "; " + unchain
	}
	return what, fix
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
