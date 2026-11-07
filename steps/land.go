package steps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// landResults is the land results step (LAYER-3 "Land", from a result; the
// model's InboxFold): inbox items of kind land and stop in arrival order. A
// landed result is one group of Landed for every head that landed and Red
// for the head the bisection blamed; a bench fault moves nothing (the
// request leaves flight and is reissued); a conflict and a red base are
// judgements. A stop ack, and a result for a request no longer in flight (a
// repeat), is consumed with no move.
func (t *tick) landResults() {
	for _, it := range t.in.Inbox {
		if it.Kind != "land" && it.Kind != "stop" {
			continue
		}
		req, ok := t.request(it.Request)
		if it.Kind == "stop" || !ok {
			t.consume(it.ID)
			continue
		}
		switch it.Outcome {
		case "landed", "red":
			t.landGroup(it, req)
		case "conflict":
			t.judge("conflict", req.Stream, fmt.Sprintf("stream %s cannot merge heads %s onto %s: a conflict across streams", req.Stream, strings.Join(req.Heads, ","), req.Base))
			t.consume(it.ID)
		case "escalate":
			t.escalate(it, req)
		case "base-red":
			t.baseRed(it, req)
		case "base-green":
			t.baseGreen(it)
		default:
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

// landGroup plans Landed for each head that landed and Red for the blamed
// one, for producers still merging at those heads and unclaimed.
func (t *tick) landGroup(it InboxItem, req EdgeRequest) {
	var moves []move
	about := ""
	for _, p := range req.Cards {
		c, ok := t.v.Cards[p]
		if !ok || c.State != "merging" || !t.free(p) {
			continue
		}
		switch {
		case slices.Contains(it.Landed, c.Head):
			moves = append(moves, t.producerMove(p, card.Landed{Head: c.Head}))
		case it.Blamed != "" && it.Blamed == c.Head:
			moves = append(moves, t.producerMove(p, card.Red{Head: c.Head, Reason: "CI red at " + c.Head + " on " + req.Base}))
		default:
			continue
		}
		if about == "" {
			about = p
		}
	}
	if len(moves) == 0 {
		t.consume(it.ID)
		return
	}
	if g := t.group("land", about, moves...); g != nil {
		g.key.Item = it.ID
	}
}

// escalate is a landing that did not land easily (Glenn 2026-11-07: "doesn't
// land easily, it must escalate to you"): CI red on the group, no CI run
// within the bound, a merge or fast-forward refused, a PR that cannot be
// opened. One land judgement on the coordinator, carrying the edge's facts:
// about the card for rung 1, about the stream for rung 2 (no bisection: the
// coordinator decides which head to pull). The stream lands nothing more
// while it is open (queued). The answers are land (the coordinator's hand
// merge: nova-sprint land), fix (LandFix, back to the worker with the
// finding) and retry (the answer alone: the next tick requests again).
func (t *tick) escalate(it InboxItem, req EdgeRequest) {
	facts := it.Facts
	if facts == "" {
		facts = "the landing did not land and said nothing more"
	}
	var at []string
	for i, p := range req.Cards {
		if c, ok := t.v.Cards[p]; ok && c.State == "merging" && i < len(req.Heads) && c.Head == req.Heads[i] {
			at = append(at, p+"@"+c.Head)
		}
	}
	switch {
	case len(at) == 0: // nothing of the request still merging at its head: the result is stale
	case len(req.Cards) == 1:
		t.judge("land", req.Cards[0], fmt.Sprintf("%s did not land on %s of stream %s: %s", at[0], req.Base, req.Stream, facts))
	default:
		t.judge("land", req.Stream, fmt.Sprintf("stream %s's group of %s did not land on %s: %s; answer fix --card <id> to send one back, land after resolving by hand, or retry", req.Stream, strings.Join(at, ","), req.Base, facts))
	}
	t.consume(it.ID)
}

// baseRed opens one judgement per base meeting (LANDING step 8): the third
// stops the stream that met it, by holding it, and the judgement says so.
func (t *tick) baseRed(it InboxItem, req EdgeRequest) {
	n := t.episodes("base-red", it.Base)
	why := fmt.Sprintf("base %s is red (meeting %d); a head that fixes it lands first", it.Base, n+1)
	if n+1 >= 3 && !t.v.Streams[req.Stream].Held {
		why = fmt.Sprintf("base %s is red for the third time; stream %s is stopped until the base is green", it.Base, req.Stream)
		t.group("hold", req.Stream, move{label: "HoldStream " + req.Stream, plan: func() (card.Plan, *card.Refusal) {
			return card.MoveStream(t.streamOf(req.Stream), card.HoldStream{Reason: why, Ready: t.readyIn(req.Stream), Seq: t.v.Seq})
		}})
	}
	t.judgeAgain("base-red", it.Base, why)
	t.consume(it.ID)
}

// baseGreen expires the base's red judgements and resumes every stream
// stopped only on that red.
func (t *tick) baseGreen(it InboxItem) {
	for _, id := range sortedKeys(t.v.Judgements) {
		j := t.v.Judgements[id]
		if j.Kind == "base-red" && j.About == it.Base && j.State == "open" {
			t.expires[id] = "base " + it.Base + " is green"
		}
	}
	for _, s := range sortedKeys(t.v.Streams) {
		if st := t.v.Streams[s]; st.Held && t.stoppedOn(s, it.Base) && t.unclaimed(s) {
			t.group("unhold", s, move{label: "UnholdStream " + s, plan: func() (card.Plan, *card.Refusal) {
				return card.MoveStream(t.streamOf(s), card.UnholdStream{Reason: "base " + it.Base + " is green"})
			}})
		}
	}
	t.consume(it.ID)
}

// stoppedOn says whether stream s was held by the tick for base's red: the
// judgement that says so is open.
func (t *tick) stoppedOn(s, base string) bool {
	for _, j := range t.v.Judgements {
		if j.Kind == "base-red" && j.About == base && j.State == "open" && strings.Contains(j.Question, "stream "+s+" is stopped") {
			return true
		}
	}
	return false
}

func (t *tick) readyIn(stream string) []card.Card {
	var out []card.Card
	for _, p := range t.v.Producers("ready") {
		if t.v.Cards[p].Stream == stream {
			out = append(out, t.cardOf(p))
		}
	}
	return out
}

// landRequests is the land requests step (LAYER-3 "Land"; the model's
// LandPicks; AfterKept, AfterNeverHolds), the ladder of INTENT rule 16: per
// stream with no land request in flight, flowing (one eligible merging
// producer) it alone, rung 1: the landing opens its PR from the card's
// branch into the base and merges it when the PR's checks are green; backed
// up (more than one), every eligible one in after order as one request,
// rung 2: merged into the stream's group branch, one PR into the base, one
// CI. Whatever does not land easily is a land judgement (escalate), rung 3,
// and the stream lands nothing more until it is answered. Each tick re-derives the requests
// from state, so a restart re-requests every merging producer. A stream
// whose merging producers get no request and no judgement is a defect,
// judged (an engine judgement). An after orders only producers both queued
// to land and never holds on one that is not (LESSONS 2.15). A hold never
// touches landing (LANDING 9).
func (t *tick) landRequests() {
	for _, s := range sortedKeys(t.v.Streams) {
		if t.busy(s) {
			continue
		}
		queued := t.queued(s)
		if len(queued) == 0 {
			continue
		}
		picks := t.eligible(queued)
		if len(picks) == 0 {
			t.engine(queued[0].ID, fmt.Sprintf("%s is merging in stream %s with no land request and no land judgement: every merging producer waits behind an after target that is not landing (%s)", queued[0].ID, s, strings.Join(queued[0].After, ",")))
			continue
		}
		t.landRequest(s, picks) // one pick: rung 1 (the card's PR); more: rung 2 (the group's PR)
	}
}

func (t *tick) busy(s string) bool {
	return slices.ContainsFunc(t.in.Inflight, func(r EdgeRequest) bool { return r.Kind == "land" && r.Stream == s })
}

// escalated says a land judgement about a card or a stream is open or
// opening this tick: the landing does not touch it until it is answered.
func (t *tick) escalated(about string) bool {
	return t.isOpen("land", about) || t.pending("land", about)
}

// queued is the unclaimed merging producers of s in score order, then id,
// without those whose landing escalated and waits for the coordinator.
func (t *tick) queued(s string) []store.Card {
	var out []store.Card
	for _, p := range t.v.Producers("merging") {
		if c := t.v.Cards[p]; c.Stream == s && t.free(p) && !t.escalated(p) && !t.escalated(s) {
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
// reissues the same request and a second landing of one head after a red
// has an id of its own (LPM R22; the model's finding 8).
func (t *tick) landRequest(s string, picks []store.Card) {
	base := t.in.Heads[s]
	h := sha256.New()
	fmt.Fprintf(h, "%d|%s|%s|", t.v.Epoch, s, base)
	req := EdgeRequest{Kind: "land", Stream: s, Base: base, Branch: t.v.Cards[workID(picks[0])].Branch}
	for _, c := range picks {
		fmt.Fprintf(h, "%s@%s/%d|", c.ID, c.Head, c.Review)
		req.Cards = append(req.Cards, c.ID)
		req.Heads = append(req.Heads, c.Head)
	}
	req.ID = "land." + s + "." + hex.EncodeToString(h.Sum(nil))[:12]
	t.edges = append(t.edges, req)
}
