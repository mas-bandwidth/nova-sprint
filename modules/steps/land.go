package steps

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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

// landGroup plans, for each card of the request still merging at the head
// it was requested at and unclaimed, as one group, Landed with the merge
// sha when the merge is on main (a promotion's, or a landing onto a main
// base), else OnBase: the merge onto a base that is not main is recorded
// and the card stays merging until a promotion finds it on main
// (LANDING.md, "Landed means on main").
func (t *tick) landGroup(it InboxItem, req EdgeRequest) {
	var moves []move
	about := ""
	for i, p := range req.Cards {
		c, ok := t.v.Cards[p]
		got := req.Heads[i]
		if req.Kind == "promote" {
			got = req.Merges[i]
		}
		if !ok || c.State != "merging" || !t.free(p) || c.Head != req.Heads[i] || !slices.Contains(it.Landed, got) {
			continue
		}
		var e card.Event = card.Landed{Head: c.Head, Merge: it.Merge}
		if req.Kind == "land" && !it.Main {
			e = card.OnBase{Head: c.Head, Merge: it.Merge, Base: t.v.Streams[req.Stream].Base}
		}
		moves = append(moves, t.producerMove(p, e))
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
		onto := t.v.Streams[req.Stream].Base
		if req.Kind == "promote" {
			onto = "main from its base " + onto
		}
		what := fmt.Sprintf("did not land: %s: %s", it.Outcome, cmp.Or(it.Reason, "no facts given"))
		t.judge("land", p, fmt.Sprintf("card %s (stream %s onto %s at %s, branch %s, head %s) %s%s. land: you looked, merge it by hand; fix: back to its producer with your finding as the reason; retry: a new landing", p, req.Stream, onto, req.Base, req.Branches[i], c.Head, what, with))
	}
}

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

// onBase is whether c is merging with its base merge recorded: its head is
// on its stream's base, which is not main (LANDING.md, "Landed means on
// main").
func onBase(c store.Card) bool {
	_, ok := store.BaseMerge(c.Reason)
	return ok && c.State == "merging"
}

// landRequests is the land requests step (LAYER-3 "Land"; LANDING.md "The
// ladder"; the model's LandPicks; AfterKept, AfterNeverHolds): per stream
// with no land request in flight, the merging producers not on base and not
// under a land judgement, without those held by an after target merging elsewhere: one
// (the first in score order) while fewer than policy's backed_up wait
// (rung 1), every one of them as one request while backed up (rung 2). An
// after orders only producers both queued to land and never holds on one
// that is not (LESSONS 2.15). A hold never touches landing (LANDING 9). The
// requests are rebuilt from the store's state every tick, so a restart
// loses none (defect 51). Per stream with no promotion in flight, its
// producers on base not under a land judgement are one promote request
// (the model's PromPicks).
func (t *tick) landRequests() {
	for _, s := range sortedKeys(t.v.Streams) {
		queued := slices.DeleteFunc(t.queued(s), func(c store.Card) bool { return t.judged(c.ID) })
		if based := slices.DeleteFunc(slices.Clone(queued), func(c store.Card) bool { return !onBase(c) }); len(based) > 0 && !t.busy(s, "promote") {
			t.landRequest("promote", s, based, t.o.landing(t.streamFacts(s)).CIWait)
		}
		if t.busy(s, "land") {
			continue
		}
		picks := t.eligible(slices.DeleteFunc(queued, onBase))
		if len(picks) == 0 {
			continue
		}
		l := t.o.landing(t.streamFacts(s))
		if l.BackedUp > len(picks) {
			picks = picks[:1]
		}
		t.landRequest("land", s, picks, l.CIWait)
	}
	t.unlanded()
}

// unlanded reports, as an engine judgement, a merging producer the tick
// leaves with no land request (a promote request for one on base) in
// flight or planned for its stream, no land judgement, and no after target
// holding it: a card that would sit in merging unexplained (intent rule
// 32). By construction landRequests leaves none; this is the check that it
// did.
func (t *tick) unlanded() {
	for _, p := range t.v.Producers("merging") {
		c, kind := t.v.Cards[p], "land"
		if onBase(c) {
			kind = "promote"
		}
		if !t.free(p) || t.judged(p) || t.busy(c.Stream, kind) || t.plannedIn(c.Stream, kind) || kind == "land" && t.afterHeld(c) {
			continue
		}
		t.engine(p, "card "+p+" is merging with no "+kind+" request and no land judgement: answer retry to ask the landing again")
	}
}

// plannedIn is whether this tick planned a request of kind for stream s.
func (t *tick) plannedIn(s, kind string) bool {
	return slices.ContainsFunc(t.edges, func(r EdgeRequest) bool { return r.Kind == kind && r.Stream == s })
}

// afterHeld is whether c waits for an after target that is merging.
func (t *tick) afterHeld(c store.Card) bool {
	return slices.ContainsFunc(c.After, func(a string) bool { x, ok := t.v.Cards[a]; return ok && x.State == "merging" })
}

// busy is whether a request of kind for stream s is in flight.
func (t *tick) busy(s, kind string) bool {
	return slices.ContainsFunc(t.in.Inflight, func(r EdgeRequest) bool { return r.Kind == kind && r.Stream == s })
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

// landRequest is one edge request of kind (land or promote) for the heads
// of picks in order: its id hashes the epoch, stream, cards, heads,
// reviews, base merges and base, so a restart reissues the same request and
// a second landing of one head after a retry has an id of its own when
// anything changed (LPM R22; the model's finding 8). A promote request
// carries each card's base merge, its head when none is recorded.
func (t *tick) landRequest(kind, s string, picks []store.Card, wait int) {
	base := t.in.Heads[s]
	h := sha256.New()
	fmt.Fprintf(h, "%d|%s|%s|", t.v.Epoch, s, base)
	req := EdgeRequest{Kind: kind, Stream: s, Base: base, Wait: wait}
	for _, c := range picks {
		fmt.Fprintf(h, "%s@%s/%d|", c.ID, c.Head, c.Review)
		req.Cards = append(req.Cards, c.ID)
		req.Heads = append(req.Heads, c.Head)
		req.Branches = append(req.Branches, branch(c))
		if kind == "promote" {
			merge, _ := store.BaseMerge(c.Reason)
			fmt.Fprintf(h, "%s|", merge)
			req.Merges = append(req.Merges, cmp.Or(merge, c.Head))
		}
	}
	req.ID = kind + "." + s + "." + hex.EncodeToString(h.Sum(nil))[:12]
	t.edges = append(t.edges, req)
}
