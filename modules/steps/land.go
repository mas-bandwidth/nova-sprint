package steps

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

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
		case it.Outcome == "conflict" && req.Kind == "land" && req.SharedRed && t.conflictFix(it, req):
		case (it.Outcome == "ci_red" || it.Outcome == "conflict" || it.Outcome == "ci_no_verdict") && req.Kind == "land" && req.SharedRed && len(req.Cards) > 1 && t.peel(it, req):
		case (it.Outcome == "ci_red" || it.Outcome == "ci_no_verdict") && it.OwnRed && req.Kind == "land" && req.SharedRed && len(req.Cards) == 1 && t.redFix(it, req):
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
		var e card.Event = card.Landed{Head: c.Head, Merge: it.Merge, Reason: it.Reason}
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
// and the cards that were in the landing with it. A conflict that names
// the head whose merge into the group failed (it.Head) is that card's
// alone: the group's other cards are not judged, so the next tick asks
// them again without it (LANDING.md, "One card's conflict").
func (t *tick) landJudge(it InboxItem, req EdgeRequest) {
	odd := -1
	if it.Outcome == "conflict" && it.Head != "" {
		odd = slices.Index(req.Heads, it.Head)
	}
	for i, p := range req.Cards {
		c, ok := t.v.Cards[p]
		if !ok || c.State != "merging" || c.Head != req.Heads[i] || odd >= 0 && i != odd {
			continue
		}
		with := ""
		switch {
		case odd >= 0 && len(req.Cards) > 1:
			with = "; its head is the one in the group that does not merge (onto the base tip, after the group's cards before it); landing again without it: " + strings.Join(slices.Delete(slices.Clone(req.Cards), i, i+1), ", ")
		case len(req.Cards) > 1:
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

// conflictFix sends the card whose head does not merge (it.Head, or the
// request's one card) back to fix with the conflict as its finding, in a
// group that consumes the item (land_on_shared_red; LANDING.md, "One
// card's conflict"; tla/LandEdge.tla Tick): no judgement, and the group's
// other cards are asked again without it. False when no card is named in
// a group of several (GitHub said the group's PR conflicts), or the card
// is no longer merging at the head: the judgement as before.
func (t *tick) conflictFix(it InboxItem, req EdgeRequest) bool {
	i := slices.Index(req.Heads, it.Head)
	if i < 0 && len(req.Cards) == 1 {
		i = 0
	}
	if i < 0 {
		return false
	}
	p := req.Cards[i]
	c, ok := t.v.Cards[p]
	if !ok || c.State != "merging" || c.Head != req.Heads[i] || !t.free(p) {
		return false
	}
	reason := fmt.Sprintf("conflict: head %s of branch %s does not merge onto %s at %s: %s; merge %s into the branch, resolve, and finish again",
		c.Head, req.Branches[i], t.v.Streams[req.Stream].Base, req.Base, cmp.Or(it.Reason, "no facts given"), t.v.Streams[req.Stream].Base)
	if g := t.group("land", p, t.producerMove(p, card.Red{Head: c.Head, Reason: reason})); g != nil {
		g.key.Item = it.ID
	}
	return true
}

// peel answers a group's own red (one the base does not share), or a
// conflict that names no card, with no judgement (land_on_shared_red;
// LANDING.md "One card's red never holds the others"; Glenn 2026-11-15):
// the item is consumed and, in the same tick, the group's first card still
// merging at its head is asked alone. Its own verdict lands it or sends it
// to fix, and the rest regroup after it, so a card that breaks its group is
// found in at most group_max solo passes, and never holds the others in a
// judgement. False when no card of the group is still merging and free.
func (t *tick) peel(it InboxItem, req EdgeRequest) bool {
	if t.lanePlanned(t.laneOf(req.Stream, "land")) {
		t.consume(it.ID)
		return true
	}
	for i, p := range req.Cards {
		c, ok := t.v.Cards[p]
		if !ok || c.State != "merging" || c.Head != req.Heads[i] || !t.free(p) || t.judged(p) || t.afterHeld(c) {
			continue
		}
		t.consume(it.ID)
		l := t.o.landing(t.streamFacts(req.Stream))
		t.landRequest("land", req.Stream, []store.Card{c}, l.CIWait)
		t.edges[len(t.edges)-1].SharedRed = !l.HoldSharedRed
		t.edges[len(t.edges)-1].Quiet = t.quiet(req.Stream, []string{p})
		t.edges[len(t.edges)-1].Fresh = true
		return true
	}
	return false
}

// redFix sends a card whose own landing came back red on a check proven
// the card's own (InboxItem.OwnRed: it completed on the base tip and was
// not red there, or timed out persistently across bounded reruns) back to
// fix, with the failing checks as its finding, in a group that consumes the
// item (land_on_shared_red; LANDING.md "One card's red never holds the
// others"): no judgement. Persistent timeouts are bounded by limits.bounces.
func (t *tick) redFix(it InboxItem, req EdgeRequest) bool {
	p := req.Cards[0]
	c, ok := t.v.Cards[p]
	if !ok || c.State != "merging" || c.Head != req.Heads[0] || !t.free(p) {
		return false
	}
	if it.Outcome == "ci_no_verdict" && t.redBounces(c) >= t.lim.Bounces {
		return false
	}
	reason := fmt.Sprintf("red: head %s of branch %s is red onto %s at %s on checks the base does not share: %s; fix them and finish again",
		c.Head, req.Branches[0], t.v.Streams[req.Stream].Base, req.Base, cmp.Or(it.Reason, "no facts given"))
	if it.Outcome == "ci_no_verdict" {
		reason = fmt.Sprintf("no verdict: head %s of branch %s timed out or stopped repeatedly onto %s at %s: %s; fix the hang or timeout and finish again",
			c.Head, req.Branches[0], t.v.Streams[req.Stream].Base, req.Base, cmp.Or(it.Reason, "no facts given"))
	}
	if g := t.group("land", p, t.producerMove(p, card.Red{Head: c.Head, Reason: reason})); g != nil {
		g.key.Item = it.ID
	}
	return true
}

func (t *tick) redBounces(c store.Card) int {
	cons := t.v.Consumers(c.ID)
	if len(cons) == 0 {
		return max(0, c.Review-max(c.Attempt, 1))
	}
	ok := 0
	for _, w := range cons {
		if w.Kind == "work" && w.State == "done" && w.Result == "ok" {
			ok++
		}
	}
	return max(0, c.Review-ok)
}

// quiet is the branches of merging producers across all streams on stream s's
// repository except those named (or gated on rung 1): no CI on them gates a
// landing, so the landing cancels their runs (EdgeRequest.Quiet; LANDING.md
// "The landing's CI comes first").
func (t *tick) quiet(s string, except []string) []string {
	repo := repoKey(t.v.Streams[s].Repo)
	var out []string
	for _, p := range t.v.Producers("merging") {
		c := t.v.Cards[p]
		if repoKey(t.v.Streams[c.Stream].Repo) != repo || slices.Contains(except, p) || t.rungOneActive(p) {
			continue
		}
		out = append(out, branch(c))
	}
	return out
}

func (t *tick) rungOneActive(p string) bool {
	for _, r := range slices.Concat(t.in.Inflight, t.edges) {
		if r.Kind == "land" && !r.Fresh && slices.Contains(r.Cards, p) {
			return true
		}
	}
	return false
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
type laneCandidate struct {
	kind, stream, lane string
	cards              []store.Card
	landing            LandingAnswer
	since              time.Time
}

func (t *tick) landRequests() {
	byLane := map[string][]laneCandidate{}
	for _, s := range sortedKeys(t.v.Streams) {
		queued := slices.DeleteFunc(t.queued(s), func(c store.Card) bool { return t.judged(c.ID) })
		if based := slices.DeleteFunc(slices.Clone(queued), func(c store.Card) bool { return !onBase(c) }); len(based) > 0 {
			if lane := t.laneOf(s, "promote"); !t.laneBusy(lane) && !t.lanePlanned(lane) {
				l := t.o.landing(t.streamFacts(s))
				byLane[lane] = append(byLane[lane], laneCandidate{kind: "promote", stream: s, lane: lane, cards: based, landing: l, since: t.earliestMerging(based)})
			}
		}
		picks := t.eligible(slices.DeleteFunc(queued, onBase))
		if len(picks) == 0 {
			continue
		}
		lane := t.laneOf(s, "land")
		if t.laneBusy(lane) || t.lanePlanned(lane) {
			continue
		}
		l := t.o.landing(t.streamFacts(s))
		if l.BackedUp > len(picks) {
			picks = picks[:1]
		}
		if max := cmp.Or(l.GroupMax, Engine.Landing.GroupMax); len(picks) > max {
			picks = picks[:max] // a group never grows past group_max: it lands within one CI run
		}
		byLane[lane] = append(byLane[lane], laneCandidate{kind: "land", stream: s, lane: lane, cards: picks, landing: l, since: t.earliestMerging(picks)})
	}
	for _, lane := range sortedKeys(byLane) {
		cands := byLane[lane]
		slices.SortFunc(cands, func(a, b laneCandidate) int {
			if c := a.since.Compare(b.since); c != 0 {
				return c
			}
			if a.kind != b.kind {
				if a.kind == "promote" {
					return -1
				}
				return 1
			}
			return cmp.Compare(a.stream, b.stream)
		})
		win := cands[0]
		t.landRequest(win.kind, win.stream, win.cards, win.landing.CIWait)
		if win.kind == "land" {
			t.edges[len(t.edges)-1].SharedRed = !win.landing.HoldSharedRed
			t.edges[len(t.edges)-1].Quiet = t.quiet(win.stream, t.edges[len(t.edges)-1].Cards)
			t.edges[len(t.edges)-1].Fresh = true
		}
	}
	t.unlanded()
}

func (t *tick) earliestMerging(cards []store.Card) time.Time {
	var minAt time.Time
	for _, c := range cards {
		at := t.v.MergingAt[c.ID]
		if at.IsZero() {
			at = t.v.Since
		}
		if minAt.IsZero() || at.Before(minAt) {
			minAt = at
		}
	}
	return minAt
}

// unlanded reports, as an engine judgement, a merging producer the tick
// leaves with no land request (a promote request for one on base) in
// flight or planned on its (repo, base) lane, no land judgement, and no
// after target holding it: a card that would sit in merging unexplained
// (intent rule 32). By construction landRequests leaves none; this is the
// check that it did.
func (t *tick) unlanded() {
	for _, p := range t.v.Producers("merging") {
		c, kind := t.v.Cards[p], "land"
		if onBase(c) {
			kind = "promote"
		}
		lane := t.laneOf(c.Stream, kind)
		if !t.free(p) || t.judged(p) || t.laneBusy(lane) || t.lanePlanned(lane) || kind == "land" && t.afterHeld(c) {
			continue
		}
		t.engine(p, "card "+p+" is merging with no "+kind+" request and no land judgement: answer retry to ask the landing again")
	}
}

// laneOf is the canonical <repo>@<base> lane a land or promote request on
// stream s merges onto.
func (t *tick) laneOf(s, kind string) string {
	st, ok := t.v.Streams[s]
	if !ok {
		return s + "@" + kind
	}
	target := st.Base
	if kind == "promote" {
		target = "main"
	}
	return BaseAbout(repoKey(st.Repo), target)
}

func (t *tick) lanePlanned(lane string) bool {
	return slices.ContainsFunc(t.edges, func(r EdgeRequest) bool {
		return (r.Kind == "land" || r.Kind == "promote") && t.laneOf(r.Stream, r.Kind) == lane
	})
}

// afterHeld is whether c waits for an after target that is merging.
func (t *tick) afterHeld(c store.Card) bool {
	return slices.ContainsFunc(c.After, func(a string) bool { x, ok := t.v.Cards[a]; return ok && x.State == "merging" })
}

func (t *tick) laneBusy(lane string) bool {
	return slices.ContainsFunc(t.in.Inflight, func(r EdgeRequest) bool {
		return (r.Kind == "land" || r.Kind == "promote") && t.laneOf(r.Stream, r.Kind) == lane
	})
}

func repoKey(repo string) string {
	repo = strings.TrimSpace(repo)
	host, path := "", ""
	if _, rest, ok := strings.Cut(repo, "://"); ok {
		host, path, _ = strings.Cut(rest, "/")
	} else if at, colon := strings.Index(repo, "@"), strings.Index(repo, ":"); at >= 0 && colon > at {
		host, path = repo[:colon], repo[colon+1:]
	} else {
		return repo
	}
	if _, h, ok := strings.Cut(host, "@"); ok {
		host = h
	}
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	if !strings.EqualFold(host, "github.com") {
		return repo
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if owner, name, ok := strings.Cut(path, "/"); ok && owner != "" && name != "" && !strings.Contains(name, "/") {
		return path
	}
	return repo
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
