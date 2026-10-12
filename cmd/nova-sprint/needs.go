package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bench"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// The needs verb is a read (docs/SPEC-SPRINT.md section 11): it changes
// nothing and needs no actor, and it prints the dependency graph of the
// waiting cards. The class is kept here, beside the verb, since the verb
// table registers the verb and needs.go adds its class.
func init() { verbClasses["needs"] = classRead }

// needLine is one unmet need with its own column, distinct from the waiting
// card's column (docs/SPEC-SPRINT.md section 11). Off-table needs name their
// outcome, or absent when no record exists.
type needLine struct {
	ID     string `json:"id"`
	Column string `json:"column"`
}

// needsCard is one waiting card in the graph: its stream, its depth, whether
// it is a root, and its unmet needs.
type needsCard struct {
	ID     string     `json:"id"`
	Column string     `json:"column"`
	Stream string     `json:"stream"`
	Depth  int        `json:"depth"`
	Root   bool       `json:"root"`
	Needs  []needLine `json:"needs"`
	More   *needsMore `json:"more,omitempty"`
}

// needsWidth is the number of a stream's waiting cards at one depth.
type needsWidth struct {
	Depth int `json:"depth"`
	Width int `json:"width"`
}

// needsStream is one stream's graph: its waiting cards in chain order, the
// width at each depth, and the number of its cards that name a dropped or
// absent id. Total is every waiting card of the stream (the JSON view carries
// the same cards the text prints).
type needsStream struct {
	Stream  string       `json:"stream"`
	Cards   []needsCard  `json:"cards"`
	Widths  []needsWidth `json:"width"`
	Total   int          `json:"total"`
	Orphans int          `json:"dropped_or_absent"`
	// Cycle names the cards whose needs make a cycle in this stream, so the
	// graph prints the cycle instead of being followed around it.
	Cycle     []string   `json:"cycle,omitempty"`
	More      *needsMore `json:"more,omitempty"`
	WidthMore *needsMore `json:"width_more,omitempty"`
	CycleMore *needsMore `json:"cycle_more,omitempty"`
}

// needsView is the whole graph: a stream a section, and the sprint's totals.
type needsView struct {
	Streams []needsStream `json:"streams"`
	Cards   int           `json:"cards"`
	Orphans int           `json:"dropped_or_absent"`
	More    *needsMore    `json:"more,omitempty"`
}

// cmdNeeds prints the dependency graph of the waiting cards: for each stream
// (or the one --stream names) its waiting cards in chain order, a card after
// every card it needs, each with its unmet needs and their states, the roots
// marked, each card's depth, the width at each depth, and a line counting the
// cards whose needs name a dropped or absent id. It reads the work table once
// with the off-table needs of the waiting cards read too (sprint.ResolveExtras
// as Load's extras, the same read the tick's extras make), builds every need's
// state from the same sprint.NeedsOf the card view uses, writes nothing, and
// prints one object with --json (docs/SPEC-SPRINT.md section 11).
func (a *app) cmdNeeds(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("needs")
	stream := fs.String("stream", "", "the waiting cards of one stream (default: every stream)")
	roots := fs.Bool("roots", false, "print only the roots and the width lines")
	needs := fs.String("needs", "", "with set: the card's needs, comma separated, in place of the ones it has; each a primary on the table")
	clear := fs.Bool("clear", false, "with set: empty the card's needs")
	answers := fs.String("answers", "", "with set: the judgments it answers, comma separated")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, "needs", err.Error())
	}
	if len(pos) > 0 && pos[0] == "set" {
		return a.cmdNeedsSet(fs, c, pos[1:], *needs, *clear, *answers, stdout, stderr)
	}
	if len(pos) > 0 {
		return refuse(stderr, "needs", argErr("takes no words ", err, pos...))
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "needs", err.Error())
	}
	// One read of the work table: the placed waiting cards and, as extras, the
	// needs of the waiting cards that are off the table, so an absent need and a
	// dropped record are told apart (sprint.ResolveExtras; the tick reads them
	// the same way, store/tick.go tickExtras).
	s, err := st.Load(context.Background(), []string{sprint.Work}, func(s *sprint.Snapshot) map[string][]string {
		return map[string][]string{sprint.Work: sprint.ResolveExtras(s)}
	})
	if err != nil {
		return a.readFailed("needs", err, stderr)
	}
	v := limitNeeds(sprintNeeds(s, *stream, *roots), c.max, *roots, c.json)
	if c.json {
		b, _ := json.Marshal(v)
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	fmt.Fprint(stdout, needsText(v, *roots))
	return 0
}

// sprintNeeds builds the graph from one snapshot (pure over the read): only is
// a stream to keep ("" is every stream), rootsOnly keeps only the roots in
// each stream's cards. A card's depth is one more than its deepest waiting
// need; a card with no waiting need is a root at depth 0. A cycle through
// needs is never walked forever: its cards are marked and so are not roots
// (docs/SPEC-SPRINT.md section 11).
func sprintNeeds(s *sprint.Snapshot, only string, rootsOnly bool) needsView {
	waiting := s.Work.Column(sprint.Waiting)
	inWaiting := make(map[string]bool, len(waiting))
	for _, c := range waiting {
		inWaiting[c.ID] = true
	}
	// each waiting card's needs, resolved once, as the card view resolves them
	needs := make(map[string][]sprint.NeedState, len(waiting))
	for _, c := range waiting {
		ns, _ := sprint.NeedsOf(s, c.ID)
		needs[c.ID] = ns
	}
	depth := make(map[string]int, len(waiting))
	cycle := map[string]bool{}
	on := map[string]bool{}
	var walk func(id string) int
	walk = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		if on[id] {
			cycle[id] = true
			return 0
		}
		on[id] = true
		d := 0
		for _, n := range needs[id] {
			if n.Waived || n.State != sprint.Waiting || !inWaiting[n.ID] {
				continue
			}
			if nd := walk(n.ID) + 1; nd > d {
				d = nd
			}
		}
		delete(on, id)
		depth[id] = d
		return d
	}
	for _, c := range waiting {
		walk(c.ID)
	}
	var v needsView
	byStream := map[string]int{}
	for _, c := range waiting {
		if only != "" && c.Row != only {
			continue
		}
		i, ok := byStream[c.Row]
		if !ok {
			v.Streams = append(v.Streams, needsStream{Stream: c.Row})
			i = len(v.Streams) - 1
			byStream[c.Row] = i
		}
		st := &v.Streams[i]
		st.Total++
		card := needsCard{ID: c.ID, Column: c.Col, Stream: c.Row, Depth: depth[c.ID]}
		// orphan is whether this card's needs name a dropped or absent id:
		// the closing count counts cards, one card once however many of its
		// needs name one (docs/SPEC-SPRINT.md section 11).
		orphan := false
		for _, n := range needs[c.ID] {
			word := sprint.NeedWord(n.State)
			if n.Waived || word == sprint.Landed {
				continue
			}
			card.Needs = append(card.Needs, needLine{ID: n.ID, Column: word})
			if word == sprint.DroppedWord || word == sprint.AbsentWord {
				orphan = true
			}
		}
		if orphan {
			st.Orphans++
		}
		card.Root = !cycle[c.ID] && card.Depth == 0
		if !rootsOnly || card.Root {
			st.Cards = append(st.Cards, card)
		}
	}
	for i := range v.Streams {
		st := &v.Streams[i]
		// Chain order: a card after every card it needs. A card's depth is one
		// more than its deepest waiting need's, so every card sorts after the
		// cards it waits on; the work order is kept within a depth, where no
		// card waits on another (docs/SPEC-SPRINT.md section 11).
		sort.SliceStable(st.Cards, func(i, j int) bool {
			return st.Cards[i].Depth < st.Cards[j].Depth
		})
		st.Widths = needsWidths(waiting, depth, st.Stream)
		for _, c := range waiting {
			if c.Row == st.Stream && cycle[c.ID] {
				st.Cycle = append(st.Cycle, c.ID)
			}
		}
		if st.Cards == nil {
			st.Cards = []needsCard{}
		}
		v.Cards += st.Total
		v.Orphans += st.Orphans
	}
	return v
}

// needsMore names a cut list without changing its complete graph facts
// (docs/SPEC-SPRINT.md section 11). FirstHidden identifies its boundary.
type needsMore struct {
	Shown       int    `json:"shown"`
	Total       int    `json:"total"`
	Omitted     int    `json:"omitted"`
	FirstHidden string `json:"first_hidden"`
	Command     string `json:"command"`
}

// needsSlice spends one item-kind budget, keeping the first hidden identity
// and a command that reveals the whole list (SPEC-SPRINT section 11).
func needsSlice[T any](items []T, left *int, command string, id func(T) string) ([]T, *needsMore) {
	shown := min(len(items), *left)
	*left -= shown
	if shown == len(items) {
		return items, nil
	}
	return items[:shown], &needsMore{Shown: shown, Total: len(items), Omitted: len(items) - shown,
		FirstHidden: id(items[shown]), Command: command}
}

// limitNeeds bounds each item kind across one answer after the full graph is
// calculated, preserving totals, depths and dependency boundaries (SPEC-SPRINT
// section 11). Only displayed stream/card records are copied; the input is kept.
func limitNeeds(v needsView, max int, rootsOnly, jsonOut bool) needsView {
	if max <= 0 {
		return v
	}
	suffix := ""
	if rootsOnly {
		suffix += " --roots"
	}
	if jsonOut {
		suffix += " --json"
	}
	streams, cards, needs, widths, cycles := max, max, max, max, max
	v.Streams, v.More = needsSlice(v.Streams, &streams, "nova-sprint needs --max 0"+suffix, func(st needsStream) string {
		if len(st.Cards) > 0 {
			return st.Cards[0].ID
		}
		if len(st.Cycle) > 0 {
			return st.Cycle[0]
		}
		return st.Stream
	})
	v.Streams = slices.Clone(v.Streams)
	for i := range v.Streams {
		st := &v.Streams[i]
		command := "nova-sprint needs --stream " + bench.Quote(st.Stream) + " --max 0" + suffix
		st.Cards, st.More = needsSlice(st.Cards, &cards, command, func(c needsCard) string { return c.ID })
		st.Widths, st.WidthMore = needsSlice(st.Widths, &widths, command, func(w needsWidth) string { return fmt.Sprint(w.Depth) })
		st.Cycle, st.CycleMore = needsSlice(st.Cycle, &cycles, command, func(id string) string { return id })
		st.Cards = slices.Clone(st.Cards)
		for j := range st.Cards {
			c := &st.Cards[j]
			c.Needs, c.More = needsSlice(c.Needs, &needs, command, func(n needLine) string { return n.ID })
		}
	}
	return v
}

// needsWidths is the width at each depth of one stream's waiting cards, depth
// 0 first and a line for every depth between the shallowest and the deepest.
func needsWidths(waiting []*sprint.Card, depth map[string]int, stream string) []needsWidth {
	count := map[int]int{}
	maxDepth := -1
	for _, c := range waiting {
		if c.Row != stream {
			continue
		}
		d := depth[c.ID]
		count[d]++
		if d > maxDepth {
			maxDepth = d
		}
	}
	var out []needsWidth
	for d := 0; d <= maxDepth; d++ {
		if n := count[d]; n > 0 {
			out = append(out, needsWidth{Depth: d, Width: n})
		}
	}
	return out
}

// The words a need's place is printed with when it no longer sits in a work
// column (a kept record's outcome dropped, and no record at all) are
// sprint.DroppedWord and sprint.AbsentWord: sprint.NeedWord is the one function
// both the card and needs readers call (docs/SPEC-SPRINT.md section 11). The
// unexported names are kept here for this package's reader tests.
const (
	droppedWord = sprint.DroppedWord
	absentWord  = sprint.AbsentWord
)

// needsText is the graph in lines: one line a card (name, depth, ROOT, its
// unmet needs and their states), a width line a stream, then the closing
// counts. With rootsOnly it prints only the roots and the width lines.
func needsText(v needsView, rootsOnly bool) string {
	var b strings.Builder
	for _, st := range v.Streams {
		for _, c := range st.Cards {
			root := ""
			if c.Root {
				root = " ROOT"
			}
			fmt.Fprintf(&b, "NEEDS stream=%s depth=%d%s card %s column %s", st.Stream, c.Depth, root, c.ID, c.Column)
			if len(c.Needs) == 0 && c.More != nil {
				b.WriteString(" needs shown=0")
			} else if len(c.Needs) == 0 {
				b.WriteString(" needs none")
			} else {
				b.WriteString(" needs ")
				for i, n := range c.Needs {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "need %s column %s", n.ID, n.Column)
				}
			}
			b.WriteString("\n")
			needsMoreText(&b, "needs", " card="+oneline.Field(c.ID), c.More)
		}
		needsMoreText(&b, "cards", " stream="+oneline.Field(st.Stream), st.More)
		b.WriteString("WIDTH stream=" + st.Stream)
		for i, w := range st.Widths {
			if i == 0 {
				b.WriteString(" ")
			} else {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "depth %d: %d", w.Depth, w.Width)
		}
		b.WriteString("\n")
		needsMoreText(&b, "width", " stream="+oneline.Field(st.Stream), st.WidthMore)
		if len(st.Cycle) > 0 || st.CycleMore != nil {
			fmt.Fprintf(&b, "CYCLE stream=%s its needs make a cycle", st.Stream)
			if len(st.Cycle) > 0 {
				b.WriteString(" through " + strings.Join(st.Cycle, ", "))
			}
			b.WriteString("\n")
			needsMoreText(&b, "cycle", " stream="+oneline.Field(st.Stream), st.CycleMore)
		}
		if rootsOnly {
			continue
		}
		fmt.Fprintf(&b, "NEEDS stream=%s cards=%d dropped-or-absent=%d\n", st.Stream, st.Total, st.Orphans)
	}
	needsMoreText(&b, "streams", "", v.More)
	if !rootsOnly {
		fmt.Fprintf(&b, "NEEDS OK cards=%d dropped-or-absent=%d\n", v.Cards, v.Orphans)
	}
	return b.String()
}

// needsMoreText renders the JSON truncation facts on the text path too
// (docs/SPEC-SPRINT.md section 11): no cut list is silently complete.
func needsMoreText(b *strings.Builder, kind, scope string, more *needsMore) {
	if more != nil {
		fmt.Fprintf(b, "MORE kind=%s%s shown=%d total=%d omitted=%d first_hidden=%s run: %s\n",
			kind, scope, more.Shown, more.Total, more.Omitted, oneline.Field(more.FirstHidden), more.Command)
	}
}

// cmdNeedsSet is needs set <id> (--needs <a,b> | --clear) [--answers <note>]:
// the direct verb that re-points a primary's needs, in place of dropping the
// card and adding it again (the owner, 2026-10-11: "I hate twins."). It is the
// coordinator's alone; each need is a primary on the table, and a landed or
// off-table card is refused (docs/SPEC-SPRINT.md, needs).
func (a *app) cmdNeedsSet(fs flagSet, c *common, rest []string, needs string, clear bool, answersNote string, stdout, stderr io.Writer) int {
	given, cleared := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "needs":
			given = true
		case "clear":
			cleared = true
		}
	})
	switch {
	case len(rest) != 1:
		return refuse(stderr, "needs set", "wants <id> (--needs <a,b> | --clear) [--answers <note>]")
	case given && cleared:
		return refuse(stderr, "needs set", "wants one of --needs <a,b> or --clear, not both")
	case !given && !cleared:
		return refuse(stderr, "needs set", "wants one of --needs <a,b> or --clear")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "needs set", err.Error())
	}
	r := sprint.NeedsSetReq{ID: rest[0], Needs: sprint.Split(needs), Clear: clear, Answers: answers(answersNote), Who: c.actor}
	return a.runStep("needs set", *c, st, needsSetStep(r), stdout, stderr)
}

// needsSetStep reads the work table and the records of the needs named, placed
// or not, so a refusal says what became of a need off the table.
func needsSetStep(r sprint.NeedsSetReq) store.Step {
	return store.Step{Answers: r.Answers, Named: true, Args: store.ArgsOf(r), Verb: "needs set", Load: []string{sprint.Work},
		Extras: func(*sprint.Snapshot) map[string][]string {
			return map[string][]string{sprint.Work: append([]string{r.ID}, r.Needs...)}
		},
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.NeedsSet(s, r) }}
}
