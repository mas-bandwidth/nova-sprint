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
		case "no-ci":
			// Nothing merged into base; the request leaves flight and is asked
			// again next tick, under this one open judgement (defect 40).
			t.judge("no-ci", req.Stream, fmt.Sprintf("stream %s: %s on %s: no CI runs on that branch, so nothing can land: make the repository's workflow run on push to sprint/** (LANDING, \"CI on the group branch\"), then re-run CI on that sha or answer retry", req.Stream, cmp.Or(it.Reason, "no check was reported"), groupBranch(req.Stream)))
			t.consume(it.ID)
		case "forge":
			t.judge("forge", req.Stream, fmt.Sprintf("stream %s cannot land: %s", req.Stream, cmp.Or(it.Reason, "the forge refused")))
			t.consume(it.ID)
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
// one, for producers still merging at those heads and unclaimed. Red sends
// the producer to fix with the CI finding (intent rule 30).
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
			moves = append(moves, t.producerMove(p, card.Red{Head: c.Head, Reason: redFinding(c.Head, req.Base, it.Reason)}))
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

// groupBranch is a stream's integration branch (layer 4's git.StreamBranch;
// layer 3 does not import the edges).
func groupBranch(stream string) string { return "sprint/" + stream + "/group" }

// redFinding is a red landing's finding, the fix attempt's brief addition
// (intent rule 30): the head, the base, and the failing checks with their
// URLs as the edge read them.
func redFinding(head, base, ci string) string {
	f := "CI red at " + head + " on " + base
	if ci != "" {
		f += ": " + ci
	}
	return f
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
// LandPicks; AfterKept, AfterNeverHolds): per stream with no land request
// in flight, flowing, the first merging producer none of whose after
// targets is merging; backed up (merging at or past policy's landing
// number), every eligible one in score order as one group with one CI. An
// after orders only producers both queued to land and never holds on one
// that is not (LESSONS 2.15). A hold never touches landing (LANDING 9).
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
