package verbs

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runAdd is `add`: producers from brief files (each card's id its file's
// name without .md), or one sentinel over --needs, into a stream, in one op
// fenced at the snapshot's seq.
func runAdd(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	adds, err := readAdds(in, snap)
	if err != nil {
		return "", err
	}
	plan, err := planAdds(snap, adds)
	if err != nil {
		return "", err
	}
	if err := e.admitAdds(ctx, snap, adds); err != nil {
		return "", err
	}
	lines := make([]string, len(adds))
	for i, a := range adds {
		lines[i] = fmt.Sprintf("ADD %s kind=%s stream=%s tier=%s priority=%s needs=%s", a.ID, a.Kind, a.Stream, a.Tier, orWord(a.Priority, "normal"), orWord(strings.Join(a.Needs, ","), "-"))
	}
	lines = append(lines, e.chainAfterAdd(ctx, snap, adds))
	if in.has("dry-run") {
		return done("nova-sprint add (the same, without --dry-run)", append(lines, "(dry run)")...), nil
	}
	// The admission reads the stream's repository for seconds while a running
	// sprint's ticks move the store: the op is planned and fenced on a
	// snapshot read after it.
	if snap, err = e.snapshot(ctx); err != nil {
		return "", err
	}
	if plan, err = planAdds(snap, adds); err != nil {
		return "", err
	}
	op := e.newOp("add", in.actor, snap, adds)
	op.Expect, op.Writes = append(plan.Expect, store.AtSeq{Seq: snap.Seq}), plan.Writes
	r, err := e.commit(ctx, op, "nova-sprint queue --stream "+in.str("stream"))
	if err != nil {
		return "", err
	}
	return done(waitNext, append(lines, fmt.Sprintf("ADDED %d cards applied at seq %d", len(adds), r.Seq))...), nil
}

// readAdds is the cards the flags name.
func readAdds(in *input, snap store.Snapshot) ([]card.Add, error) {
	stream, sentinel, files := in.str("stream"), in.str("sentinel"), in.list("brief-file")
	needs, f := names("add", "needs", in.str("needs"))
	if f != nil {
		return nil, f
	}
	after, f := names("add", "after", in.str("after"))
	if f != nil {
		return nil, f
	}
	switch {
	case stream == "":
		return nil, misuse("add", "add names its stream: --stream <s>")
	case (sentinel == "") == (len(files) == 0):
		return nil, misuse("add", "add takes --brief-file <f>... or --sentinel <id> --needs <cards>, one of them")
	case sentinel != "" && len(needs) == 0:
		return nil, refuse("a sentinel needs the cards it waits for", "nova-sprint add --stream "+stream+" --sentinel "+sentinel+" --needs <a,b>")
	}
	if _, ok := snap.Streams[stream]; !ok {
		return nil, refuse("no stream "+stream, "nova-sprint stream add "+stream+" --repo <org/repo>")
	}
	if sentinel != "" {
		return []card.Add{{ID: sentinel, Kind: "sentinel", Stream: stream, Tier: "flash", Priority: in.str("priority"), Needs: needs, After: after}}, nil
	}
	adds := make([]card.Add, 0, len(files))
	for _, f := range files {
		a, err := readBrief(f)
		if err != nil {
			return nil, err
		}
		a.Stream, a.Needs, a.After = stream, append(a.Needs, needs...), append(a.After, after...)
		if p := in.str("priority"); p != "" {
			a.Priority = p
		}
		adds = append(adds, a)
	}
	return adds, nil
}

func orWord(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// readBrief is one brief file as a producer.
func readBrief(path string) (card.Add, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return card.Add{}, RefusalOr(err, broken("cannot read brief "+path+" ("+errText(err)+")", "pass a readable --brief-file"))
	}
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if id == "group" {
		return card.Add{}, refuse("card id group is the stream's group branch", "rename the brief file")
	}
	return parseBrief(id, string(data))
}

// planAdds is the one plan putting every card, each after the cards of
// this add it needs or comes after.
func planAdds(s store.Snapshot, adds []card.Add) (card.Plan, error) {
	known := map[string]card.Card{}
	for id, c := range s.Cards {
		known[id] = card.FromStore(c)
	}
	pending := map[string]card.Add{}
	for _, a := range adds {
		if _, ok := known[a.ID]; ok {
			return card.Plan{}, refuse("card "+a.ID+" exists", "nova-sprint show "+a.ID)
		}
		if _, ok := pending[a.ID]; ok {
			return card.Plan{}, misuse("add", "card "+a.ID+" is named twice")
		}
		pending[a.ID] = a
	}
	order, err := addOrder(pending)
	if err != nil {
		return card.Plan{}, err
	}
	plans := make([]card.Plan, 0, len(order))
	for _, id := range order {
		a := pending[id]
		// A clone: the plan's replay reads the cards it was given, and known
		// gains this card below; given known, the replay of the first add into
		// an empty sprint refused the card as existing.
		p, why := card.MoveAdd(a, maps.Clone(known))
		if why != nil {
			return card.Plan{}, refuse(why.Reason, "fix the brief or the flags and add again")
		}
		plans = append(plans, p)
		known[id] = card.Card{ID: a.ID, Kind: a.Kind, Stream: a.Stream, Needs: a.Needs, After: a.After}
	}
	plan, why := card.Merge(plans...)
	if why != nil {
		return card.Plan{}, refuse(why.Reason, "add the cards in smaller groups")
	}
	return plan, nil
}

// addOrder is the pending cards, each after those of them it needs or comes
// after, or the cycle.
func addOrder(pending map[string]card.Add) ([]string, error) {
	state := map[string]int{}
	var order, stack []string
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 2:
			return nil
		case 1:
			return refuse("the cards need each other: "+strings.Join(append(stack, id), " -> "), "break the cycle in NEEDS or AFTER")
		}
		state[id] = 1
		stack = append(stack, id)
		for _, dep := range slices.Concat(pending[id].Needs, pending[id].After) {
			if _, ok := pending[dep]; ok {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		stack, state[id] = stack[:len(stack)-1], 2
		order = append(order, id)
		return nil
	}
	for _, id := range slices.Sorted(maps.Keys(pending)) {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// admitAdds is add's admission of its producers (admitCards): the first
// refused card, by id, is the refusal. A sentinel has no brief and is
// never dealt; its stream was checked by readAdds.
func (e *Engine) admitAdds(ctx context.Context, snap store.Snapshot, adds []card.Add) error {
	var facts []cardFacts
	for _, a := range adds {
		if a.Kind == "producer" {
			facts = append(facts, factsOfAdd(a))
		}
	}
	if len(facts) == 0 {
		return nil
	}
	refused, err := e.admitCards(ctx, snap, facts)
	if err != nil {
		return err
	}
	if ids := slices.Sorted(maps.Keys(refused)); len(ids) > 0 {
		return refused[ids[0]]
	}
	return nil
}

// chainAfterAdd is the stream's longest NEEDS chain once adds are in, and
// what it takes at the stream's land rate: a warning (never a refusal) at
// chainWarn cards or more, so the coordinator flattens it before the deal.
func (e *Engine) chainAfterAdd(ctx context.Context, snap store.Snapshot, adds []card.Add) string {
	cards := maps.Clone(snap.Cards)
	for _, a := range adds {
		cards[a.ID] = store.Card{ID: a.ID, Kind: a.Kind, Stream: a.Stream, State: "waiting", Needs: a.Needs}
	}
	stream := adds[0].Stream
	chain := steps.LongestChain(cards, stream)
	if len(chain) < chainWarn {
		return fmt.Sprintf("CHAIN stream %s: its longest NEEDS chain is %d cards: %s", stream, len(chain), steps.ChainText(chain))
	}
	times, err := e.landTimes(ctx, snap)
	return "WARN CHAIN " + chainLine(stream, chain, times[stream], err) + ": " + flattenFix
}
