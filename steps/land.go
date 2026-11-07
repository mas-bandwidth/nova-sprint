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

// edgeItem is the land results step for one item (LAYER-3 "Land", from a
// result; the model's InboxFold). A landed result is one group of Landed for
// every head that landed and Red for the head the bisection blamed; a bench
// fault moves nothing (the request leaves flight and is reissued); a
// conflict and a red base are judgements. A stop ack, and a result for a
// request no longer in flight (a repeat), is consumed with no move.
func (t *tick) edgeItem(it InboxItem) {
	req, ok := t.request(it.Request)
	if it.Kind == "stop" || !ok {
		t.consume(it.ID)
		return
	}
	switch it.Outcome {
	case "landed", "red":
		t.landGroup(it, req)
	case "conflict":
		t.judge("conflict", req.Stream, fmt.Sprintf("stream %s cannot merge heads %s onto %s: a conflict across streams", req.Stream, strings.Join(req.Heads, ","), req.Base))
		t.consume(it.ID)
	case "base-red":
		t.baseRed(it, req)
	case "base-green":
		t.baseGreen(it)
	default:
		t.consume(it.ID)
	}
}

// landedMismatch is why a landed result is not the request's: no CI-passed
// tip, another base, or a head the request did not send (round 2: never a
// landing from a bare list).
func landedMismatch(it InboxItem, req EdgeRequest) string {
	if it.Outcome != "landed" {
		return ""
	}
	switch {
	case it.Tip == "":
		return "no CI-passed tip"
	case it.Base != req.Base:
		return "base " + it.Base + " is not the request's " + req.Base
	}
	for _, h := range it.Landed {
		if !slices.Contains(req.Heads, h) {
			return "head " + h + " is not one the request sent"
		}
	}
	return ""
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
	if why := landedMismatch(it, req); why != "" {
		t.engine(req.Stream, "land result "+it.ID+" for "+req.ID+" refused: "+why+"; nothing landed from it (layer 4 sends the CI-passed tip, the base and the heads of the request)")
		t.consume(it.ID)
		return
	}
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

// A red base (LANDING step 8): one judgement per base for the coordinator
// (base-red, open while the base is red); each meeting of a stream with it
// is recorded as an episode of base-red-met about <stream>@<base> (opened
// and closed in the one op: a count, never a push); the third meeting of one
// stream stops that stream's landing with a landing-stopped judgement about
// the stream, open while stopped. The landing stop is the state LAYER-3
// "Land" needs and the store lacks: a judgement, not a stream hold (a hold
// is a deal control and never touches landing, LANDING 9). Other streams
// meeting the same red are refused under the one base judgement and not
// stopped until their own third meeting. base-green lifts every landing stop
// for the base and expires its judgement.
func (t *tick) baseRed(it InboxItem, req EdgeRequest) {
	met := req.Stream + "@" + it.Base
	n := t.episodes("base-red-met", met) + 1
	t.record("base-red-met", met, fmt.Sprintf("stream %s met red base %s (meeting %d)", req.Stream, it.Base, n))
	t.judge("base-red", it.Base, fmt.Sprintf("base %s is red: a head that fixes it lands first; a stream's meeting %d of it stops its landing: nova-sprint judge base-red.%s.<n> retry when green", it.Base, t.lim.BaseRedMeetings, it.Base))
	if n >= t.lim.BaseRedMeetings {
		t.judgeGrouped("landing-stopped", req.Stream, "base:"+it.Base, fmt.Sprintf("stream %s met red base %s %d times; its landing is stopped until the base is green", req.Stream, it.Base, n))
	}
	t.consume(it.ID)
}

// baseGreen expires the base's judgement and every landing stop it caused.
func (t *tick) baseGreen(it InboxItem) {
	for _, id := range sortedKeys(t.v.Judgements) {
		j := t.v.Judgements[id]
		if j.State != "open" {
			continue
		}
		if (j.Kind == "base-red" && j.About == it.Base) || (j.Kind == "landing-stopped" && j.Group == "base:"+it.Base) {
			t.expires[id] = "base " + it.Base + " is green"
		}
	}
	t.consume(it.ID)
}

// landingStopped says whether stream s's landing is stopped (LANDING 8).
func (t *tick) landingStopped(s string) bool { return t.isOpen("landing-stopped", s) }

// landRequests is the land requests step (LAYER-3 "Land"; the model's
// LandPicks; AfterKept, AfterNeverHolds): per stream with no land request
// in flight and its landing not stopped, flowing, the first merging producer
// none of whose after targets is merging; backed up (merging at or past
// policy's landing number), every eligible one in score order as one group
// with one CI. An after orders only producers both queued to land and never
// holds on one that is not (LESSONS 2.15). A hold never touches landing
// (LANDING 9). Streams land concurrently; layer 4 serialises the base push
// and pos orders only ties (LANDING 2, round 2's ruling).
func (t *tick) landRequests() {
	for _, s := range sortedKeys(t.v.Streams) {
		if t.busy(s) || t.landingStopped(s) {
			continue
		}
		queued := t.queued(s)
		if len(queued) == 0 {
			continue
		}
		picks := t.eligible(queued)
		if len(picks) == 0 {
			continue
		}
		if t.o.landing(t.streamFacts(s)).BackedUp > len(queued) {
			picks = picks[:1]
		}
		t.landRequest(s, picks)
	}
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
// reissues the same request and a second landing of one head after a red
// has an id of its own (LPM R22; the model's finding 8).
func (t *tick) landRequest(s string, picks []store.Card) {
	base := t.in.Heads[s]
	h := sha256.New()
	fmt.Fprintf(h, "%d|%s|%s|", t.v.Epoch, s, base)
	req := EdgeRequest{Kind: "land", Stream: s, Base: base}
	for _, c := range picks {
		fmt.Fprintf(h, "%s@%s/%d|", c.ID, c.Head, c.Review)
		req.Cards = append(req.Cards, c.ID)
		req.Heads = append(req.Heads, c.Head)
	}
	req.ID = "land." + s + "." + hex.EncodeToString(h.Sum(nil))[:12]
	t.edges = append(t.edges, req)
}
