package steps

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// landResults is the land results step (LAYER-3 "Land", from a result;
// LANDING.md; the model's InboxFold): inbox items of kind land and stop in
// arrival order. landed is the one automatic move: Landed, with the merge
// sha, for every card of the request still merging at its head, as one
// group. Anything else (red, no CI, a conflict, a refused merge, a PR that
// cannot be opened, a branch that cannot be pushed, a stream that lands by
// hand, a fault) is one land judgement per card on the coordinator's row,
// this tick, with the facts and the choices land, fix and retry; nothing
// moves and nothing is blamed. A stop ack, and a result for a request no
// longer in flight (a repeat), is consumed with no move.
func (t *tick) landResults() {
	for _, it := range t.in.Inbox {
		if it.Kind != "land" && it.Kind != "stop" {
			continue
		}
		req, ok := t.request(it.Request)
		switch {
		case it.Kind == "stop" || !ok:
			t.consume(it.ID)
		case it.Outcome == "landed":
			t.landGroup(it, req)
		default:
			t.landJudge(it, req)
			t.consume(it.ID)
		}
	}
}

func (t *tick) request(id string) (EdgeRequest, bool) {
	i := slices.IndexFunc(t.in.Inflight, func(r EdgeRequest) bool { return r.ID == id })
	if i < 0 {
		return EdgeRequest{}, false
	}
	return t.in.Inflight[i], true
}

// landGroup plans Landed, with the merge sha, for each card of the request
// still merging at the head it was requested at and unclaimed, as one
// group: the request landed whole.
func (t *tick) landGroup(it InboxItem, req EdgeRequest) {
	var moves []move
	about := ""
	for i, p := range req.Cards {
		c, ok := t.v.Cards[p]
		if !ok || c.State != "merging" || !t.free(p) || c.Head != req.Heads[i] || !slices.Contains(it.Landed, c.Head) {
			continue
		}
		moves = append(moves, t.producerMove(p, card.Landed{Head: c.Head, Merge: it.Merge}))
		about = cmp.Or(about, p)
	}
	if len(moves) == 0 {
		t.consume(it.ID)
		return
	}
	if g := t.group("land", about, moves...); g != nil {
		g.key.Item = it.ID
	}
}

// landJudge opens a land judgement for each card of the request still
// merging at its head: what kept it from landing, the facts the edge read,
// and the cards that were in the landing with it.
func (t *tick) landJudge(it InboxItem, req EdgeRequest) {
	for i, p := range req.Cards {
		c, ok := t.v.Cards[p]
		if !ok || c.State != "merging" || c.Head != req.Heads[i] {
			continue
		}
		with := ""
		if len(req.Cards) > 1 {
			with = "; landing together: " + strings.Join(req.Cards, ", ")
		}
		what := fmt.Sprintf("did not land: %s: %s", it.Outcome, cmp.Or(it.Reason, "no facts given"))
		if it.Outcome == InheritedRed {
			what = fmt.Sprintf("has only inherited red (its run ended; every failing workflow is red on the base too, so not this card's): land it with nova-sprint land %s --reason <why>. Facts: %s", p, cmp.Or(it.Reason, "no facts given"))
		}
		t.judge("land", p, fmt.Sprintf("card %s (stream %s onto %s at %s, branch %s, head %s) %s%s. land: you looked, merge it by hand; fix: back to its producer with your finding as the reason; retry: a new landing", p, req.Stream, t.v.Streams[req.Stream].Base, req.Base, req.Branches[i], c.Head, what, with))
	}
}

// InheritedRed is the land outcome of red that is all red on the base too,
// the run ended (layer 4's ci_red_inherited, directive 59): still a
// judgement, never a landing, worded so the coordinator can answer it at a
// glance.
const InheritedRed = "ci_red_inherited"

// judged is whether card p has a land judgement open, or opened this tick.
func (t *tick) judged(p string) bool { return t.isOpen("land", p) || t.pending("land", p) }

func (t *tick) readyIn(stream string) []card.Card {
	var out []card.Card
	for _, p := range t.v.Producers("ready") {
		if t.v.Cards[p].Stream == stream {
			out = append(out, t.cardOf(p))
		}
	}
	return out
}

// landRequests is the land requests step (LAYER-3 "Land"; LANDING.md "The
// ladder"; the model's LandPicks; AfterKept, AfterNeverHolds): per stream
// with no land request in flight, the merging producers not under a land
// judgement, without those held by an after target merging elsewhere: one
// (the first in score order) while fewer than policy's backed_up wait
// (rung 1), every one of them as one request while backed up (rung 2). An
// after orders only producers both queued to land and never holds on one
// that is not (LESSONS 2.15). A hold never touches landing (LANDING 9). The
// requests are rebuilt from the store's state every tick, so a restart
// loses none (defect 51).
func (t *tick) landRequests() {
	for _, s := range sortedKeys(t.v.Streams) {
		if t.busy(s) {
			continue
		}
		queued := slices.DeleteFunc(t.queued(s), func(c store.Card) bool { return t.judged(c.ID) })
		picks := t.eligible(queued)
		if len(picks) == 0 {
			continue
		}
		l := t.o.landing(t.streamFacts(s))
		if l.BackedUp > len(picks) {
			picks = picks[:1]
		}
		t.landRequest(s, picks, l.CIWait)
	}
	t.unlanded()
}

// unlanded reports, as an engine judgement, a merging producer the tick
// leaves with no land request in flight or planned for its stream, no land
// judgement, and no after target holding it: a card that would sit in
// merging unexplained (intent rule 32). By construction landRequests leaves
// none; this is the check that it did.
func (t *tick) unlanded() {
	for _, p := range t.v.Producers("merging") {
		c := t.v.Cards[p]
		if !t.free(p) || t.judged(p) || t.busy(c.Stream) || t.plannedLand(c.Stream) || t.afterHeld(c) {
			continue
		}
		t.engine(p, "card "+p+" is merging with no land request and no land judgement: answer retry to ask the landing again")
	}
}

// plannedLand is whether this tick planned a land request for stream s.
func (t *tick) plannedLand(s string) bool {
	return slices.ContainsFunc(t.edges, func(r EdgeRequest) bool { return r.Kind == "land" && r.Stream == s })
}

// afterHeld is whether c waits for an after target that is merging.
func (t *tick) afterHeld(c store.Card) bool {
	return slices.ContainsFunc(c.After, func(a string) bool { x, ok := t.v.Cards[a]; return ok && x.State == "merging" })
}

func (t *tick) busy(s string) bool {
	return slices.ContainsFunc(t.in.Inflight, func(r EdgeRequest) bool { return r.Kind == "land" && r.Stream == s })
}

// queued is the unclaimed merging producers of s in score order, then id.
func (t *tick) queued(s string) []store.Card {
	var out []store.Card
	for _, p := range t.v.Producers("merging") {
		if c := t.v.Cards[p]; c.Stream == s && t.free(p) {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b store.Card) int { return a.Score - b.Score })
	return out
}

// eligible is queued without those held by an after target merging outside
// the list, with every target inside the list moved ahead of its dependant.
func (t *tick) eligible(queued []store.Card) []store.Card {
	in := map[string]bool{}
	for _, c := range queued {
		in[c.ID] = true
	}
	var out []store.Card
	for _, c := range queued {
		held := false
		for _, a := range c.After {
			if x, ok := t.v.Cards[a]; ok && x.State == "merging" && !in[a] {
				held = true
			}
		}
		if !held {
			out = append(out, c)
		}
	}
	for changed := true; changed; {
		changed = false
		for i, c := range out {
			for j := i + 1; j < len(out); j++ {
				if slices.Contains(c.After, out[j].ID) {
					out[i], out[j] = out[j], out[i]
					changed = true
				}
			}
		}
	}
	return out
}

// landRequest is one edge request for the heads of picks in order: its id
// hashes the epoch, stream, cards, heads, reviews and base, so a restart
// reissues the same request and a second landing of one head after a
// retry has an id of its own when anything changed (LPM R22; the model's
// finding 8).
func (t *tick) landRequest(s string, picks []store.Card, wait int) {
	base := t.in.Heads[s]
	h := sha256.New()
	fmt.Fprintf(h, "%d|%s|%s|", t.v.Epoch, s, base)
	req := EdgeRequest{Kind: "land", Stream: s, Base: base, Wait: wait}
	for _, c := range picks {
		fmt.Fprintf(h, "%s@%s/%d|", c.ID, c.Head, c.Review)
		req.Cards = append(req.Cards, c.ID)
		req.Heads = append(req.Heads, c.Head)
		req.Branches = append(req.Branches, branch(c))
	}
	req.ID = "land." + s + "." + hex.EncodeToString(h.Sum(nil))[:12]
	t.edges = append(t.edges, req)
}
