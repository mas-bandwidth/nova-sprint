package verbs

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/usage"
)

// runShow is `show`: the record a refusal named, as JSON (a card's brief
// once: its prose is the brief's task part). A card, a stream,
// a row (with its worker bench's service, check_workers.go), `run` (who
// holds the chooser lock), nothing (the whole snapshot),
// or --stitch <stream> (every producer with its head, author, reads and
// costs, and the range to diff).
func runShow(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name := in.arg(0)
	var v any
	switch {
	case in.has("stitch"):
		s, ok := snap.Streams[name]
		if !ok {
			return "", misuse("show", "--stitch names a stream: nova-sprint show <stream> --stitch")
		}
		v = stitchOf(snap, s)
	case name == "":
		whole := wholeView{Snapshot: snap, Cards: map[string]shownCard{}}
		for id, c := range snap.Cards {
			whole.Cards[id] = briefOnce(c)
		}
		v = whole
	case name == "run":
		if e.Doctor == nil {
			return "", broken("the store is not configured", "nova-sprint check")
		}
		st, err := e.Doctor.Run(ctx)
		if err != nil {
			return "", err
		}
		v = st
	default:
		var ok bool
		if v, ok = record(snap, name); !ok {
			return "", refuse("no card, stream or row "+name, "nova-sprint show")
		}
		if row, isRow := v.(store.Row); isRow {
			v = e.showRow(ctx, row, snap)
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return done("nova-sprint next --as "+in.actor, string(b)), nil
}

// shownCard is a card as show prints it: the brief once. Prose is the
// brief's task part (store.Card), so it is left out when the brief holds it.
type shownCard struct {
	store.Card
	Prose string `json:"prose,omitempty"`
}

// wholeView is the snapshot with each card shown once.
type wholeView struct {
	store.Snapshot
	Cards map[string]shownCard
}

func briefOnce(c store.Card) shownCard {
	if strings.Contains(c.Brief, c.Prose) {
		return shownCard{Card: c}
	}
	return shownCard{Card: c, Prose: c.Prose}
}

func record(snap store.Snapshot, name string) (any, bool) {
	if c, ok := snap.Cards[name]; ok {
		return briefOnce(c), true
	}
	if s, ok := snap.Streams[name]; ok {
		return s, true
	}
	if r, ok := snap.Rows[name]; ok {
		return r, true
	}
	if j, ok := snap.Judgements[name]; ok {
		return j, true
	}
	return nil, false
}

type stitchView struct {
	Stream    string        `json:"stream"`
	DiffRange string        `json:"diff_range"`
	Cards     []stitchEntry `json:"cards"`
}

type stitchEntry struct {
	ID       string       `json:"id"`
	State    string       `json:"state"`
	Head     string       `json:"head"`
	Author   string       `json:"author"`
	Finding  string       `json:"finding"`
	Reads    []store.Card `json:"reads"`
	Work     []store.Card `json:"work"`
	CostUSD  string       `json:"cost_usd"`
	Unpriced bool         `json:"unpriced"`
}

// stitchOf is a stream's producers, each with its consumers, for the
// coordinator's stitch read.
func stitchOf(snap store.Snapshot, s store.Stream) stitchView {
	out := stitchView{Stream: s.Name, DiffRange: "origin/" + s.Base + "..origin/" + git.GroupBranch(s.Name), Cards: []stitchEntry{}}
	for _, p := range producers(snap, s.Name) {
		e := stitchEntry{ID: p.ID, State: p.State, Head: p.Head, Author: p.Author, Finding: p.Finding, Reads: []store.Card{}, Work: []store.Card{}}
		var cost int64
		for _, c := range consumers(snap, p.ID) {
			micro, priced := usage.Repriced(c.Usage, c.Cost, c.Priced) // as the board counts it
			cost += micro
			e.Unpriced = e.Unpriced || (c.Result != "" && !priced)
			if c.Kind == "read" {
				e.Reads = append(e.Reads, c)
			} else {
				e.Work = append(e.Work, c)
			}
		}
		e.CostUSD = fmt.Sprintf("%d.%06d", cost/1_000_000, cost%1_000_000)
		out.Cards = append(out.Cards, e)
	}
	return out
}

// producers are a stream's producers and sentinels ("" for every
// stream's) in deal order: stream position, priority, id.
func producers(snap store.Snapshot, stream string) []store.Card {
	var out []store.Card
	for _, c := range snap.Cards {
		if c.Of == "" && (stream == "" || c.Stream == stream) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b store.Card) int {
		return cmp.Or(cmp.Compare(snap.Streams[a.Stream].Pos, snap.Streams[b.Stream].Pos),
			-cmp.Compare(slices.Index(card.Levels, a.Priority), slices.Index(card.Levels, b.Priority)), strings.Compare(a.ID, b.ID))
	})
	return out
}

func consumers(snap store.Snapshot, producer string) []store.Card {
	var out []store.Card
	for _, c := range snap.Cards {
		if c.Of == producer {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b store.Card) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// runQueue is `queue`: every producer not landed or failed, one line each,
// in deal order.
func runQueue(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	stream := in.str("stream")
	if _, ok := snap.Streams[stream]; stream != "" && !ok {
		return "", refuse("no stream "+stream, "nova-sprint show")
	}
	var lines []string
	for _, p := range producers(snap, stream) {
		if p.State == card.LandedState || p.State == "fail" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s stream=%s state=%s priority=%s tier=%s needs=%s holders=%s",
			p.ID, p.Stream, p.State, orWord(p.Priority, "normal"), p.Tier, orWord(strings.Join(p.Needs, ","), "-"), orWord(holders(snap, p.ID), "-")))
	}
	if len(lines) == 0 {
		lines = []string{"the queue is empty"}
	}
	return done("nova-sprint show <card>", lines...), nil
}

func holders(snap store.Snapshot, producer string) string {
	var out []string
	for _, c := range consumers(snap, producer) {
		if live(c) {
			out = append(out, c.Holder)
		}
	}
	return strings.Join(out, ",")
}
