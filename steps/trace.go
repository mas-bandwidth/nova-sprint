package steps

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// The refinement mapping from this package's state to the variables of
// tla/Steps.tla, for trace validation (tla/StepsTrace.tla): the Go walk of
// steps_trace_test.go drives one of the model's scenarios on store.Fake and
// writes one record per action, the action's name and arguments plus the
// model's variables projected from the code's state by the functions here.
// TLC then checks the trace is a behaviour of the model
// (tla/check-trace.sh).
//
// The mapping. The model's store w is [ps, cs, up, sh, tko, pol]:
//   - ps[p]: the producer p of the view: st its state (blocked is not in the
//     model and never arises in a scenario), att its attempt, head the
//     attempt number of its head (heads are attempt numbers in the model: the
//     walk pushes head Sha(n) for attempt n), author its author row ("-" for
//     none), bounces, prio normal or blocker, review, ncut the consumers cut
//     so far (its consumers in the view, done and failed included), red.
//   - cs[<<p, i>>]: p's i-th consumer in cut order (View.Consumers): st its
//     state, kind work or read, row its holder, att its attempt, res its
//     result ("-" for none), review a read's review (0 for work). A slot with
//     no consumer is the model's "none" record, logged by absence.
//   - up[r]: the row is up. rg[r]: the row's gen less its gen at Init (every
//     row write raises it; the fence reads it). sh[s]: the stream is held.
//   - tko: the working consumers in take order (View.TakenAt).
//   - pol: an active policy answers. It is not logged (left to the model):
//     the walk's asker always answers, so the gate never closes in a walk.
// inbox is the items in arrival order as [stamp, id, out], stamp the arrival
// count the walk keeps, id the request's model id (ReqID), out the outcome
// (ack for a stop). inflight is the requests in flight as [id, stream, sent].
// js is the open judgements of the model's kinds (unserved, cannot-read,
// policy, overlap, no-policy, read-faults) as [kind, about], about a tier as its model
// name ("t1", "t2"), the coordinator's row as "coord", a card or a question
// as is. The Tick's answer is [rows |-> r]: the row the
// walk's asker prefers, or "failed" (no answer, or no question asked).

// TraceScenario names the model's world in the Go world: the tiers the
// model numbers 1 and 2, in order, and each row's generation at Init (the
// model's rg counts from 0).
type TraceScenario struct {
	Tiers []string       // Tiers[i] is model tier i + 1
	Gen0  map[string]int // row -> its gen at Init
}

// TierName is the model's name of a tier: "t1", "t2"; a tier outside the
// scenario is its own name.
func (sc TraceScenario) TierName(tier string) string {
	if i := slices.Index(sc.Tiers, tier); i >= 0 {
		return fmt.Sprintf("t%d", i+1)
	}
	return tier
}

// HeadN is the attempt number a head stands for: the walk pushes
// Sha(n) for attempt n; "" is 0.
func HeadN(head string) int {
	if head == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimLeft(head, "0"), 16, 64)
	if err != nil {
		return -1
	}
	return int(n)
}

// Sha is the head the walk pushes for attempt n: a full 40-hex sha.
func Sha(n int) string { return fmt.Sprintf("%040x", n) }

// StoreTLA is the model's w projected from the view, as the fields of a
// trace record (ps, cs, up, sh, tko), joined with ", ".
func StoreTLA(v *View, sc TraceScenario) string {
	var ps, cs []string
	for _, p := range sortedKeys(v.Cards) {
		c := v.Cards[p]
		if isConsumer(c) {
			continue
		}
		ps = append(ps, p+" |-> "+producerTLA(v, c))
		for i, k := range v.Consumers(p) {
			cs = append(cs, fmt.Sprintf("<<%q, %d>> :> %s", p, i+1, consumerTLA(k)))
		}
	}
	csTLA := "<<>>"
	if len(cs) > 0 {
		csTLA = "(" + strings.Join(cs, " @@ ") + ")"
	}
	var up, rg, sh []string
	for _, r := range sortedKeys(v.Rows) {
		up = append(up, fmt.Sprintf("%s |-> %s", r, tlaBool(v.Rows[r].State == "up")))
		rg = append(rg, fmt.Sprintf("%s |-> %d", r, v.Rows[r].Gen-sc.Gen0[r]))
	}
	for _, s := range sortedKeys(v.Streams) {
		sh = append(sh, fmt.Sprintf("%s |-> %s", s, tlaBool(v.Streams[s].Held)))
	}
	return fmt.Sprintf("ps |-> [%s], cs |-> %s, up |-> [%s], rg |-> [%s], sh |-> [%s], tko |-> <<%s>>",
		strings.Join(ps, ", "), csTLA, strings.Join(up, ", "), strings.Join(rg, ", "), strings.Join(sh, ", "), strings.Join(takeOrder(v), ", "))
}

func producerTLA(v *View, c store.Card) string {
	prio := "normal"
	if c.Priority == "blocker" {
		prio = "blocker"
	}
	author := c.Author
	if author == "" {
		author = "-"
	}
	return fmt.Sprintf("[st |-> %q, att |-> %d, head |-> %d, author |-> %q, bounces |-> %d, prio |-> %q, review |-> %d, ncut |-> %d, red |-> %s]",
		c.State, c.Attempt, HeadN(c.Head), author, c.Bounces, prio, c.Review, len(v.consOf[c.ID]), tlaBool(c.Red))
}

func consumerTLA(c store.Card) string {
	res := c.Result
	if res == "" {
		res = "-"
	}
	review := 0
	if c.Kind == "read" {
		review = c.Review
	}
	return fmt.Sprintf("[st |-> %q, kind |-> %q, row |-> %q, att |-> %d, res |-> %q, review |-> %d]", c.State, c.Kind, c.Holder, c.Attempt, res, review)
}

// takeOrder is the working consumers as model slots, oldest take first.
func takeOrder(v *View) []string {
	var ids []string
	for id, c := range v.Cards {
		if isConsumer(c) && c.State == "working" {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		if !v.TakenAt[a].Equal(v.TakenAt[b]) {
			return v.TakenAt[a].Compare(v.TakenAt[b])
		}
		return strings.Compare(a, b)
	})
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = SlotTLA(v, id)
	}
	return out
}

// SlotTLA is a consumer as a model slot <<p, i>>, i its place in cut order.
func SlotTLA(v *View, id string) string {
	c := v.Cards[id]
	for i, k := range v.Consumers(c.Of) {
		if k.ID == id {
			return fmt.Sprintf("<<%q, %d>>", c.Of, i+1)
		}
	}
	return fmt.Sprintf("<<%q, 0>>", c.Of)
}

// ReqID is an edge request's model id <<epoch, kind, card, head>>: for a
// land of one producer <<e, "land", <<p, 0>>, n>>; for a stop <<e, "stop",
// slot, 0>>.
func ReqID(v *View, r EdgeRequest) string {
	if r.Kind == "stop" {
		return fmt.Sprintf("<<%d, \"stop\", %s, 0>>", v.Epoch, SlotTLA(v, r.Consumer))
	}
	n := 0
	if len(r.Heads) > 0 {
		n = HeadN(r.Heads[0])
	}
	return fmt.Sprintf("<<%d, \"land\", <<%q, 0>>, %d>>", v.Epoch, firstOr(r.Cards, "-"), n)
}

// InboxTLA is the inbox as [stamp, id, out] in arrival order; stamps and
// ids are the walk's.
func InboxTLA(items []InboxItem, stamp map[string]int, id map[string]string) string {
	var out []string
	for _, it := range items {
		o := it.Outcome
		if it.Kind == "stop" {
			o = "ack"
		}
		out = append(out, fmt.Sprintf("[stamp |-> %d, id |-> %s, out |-> %q]", stamp[it.ID], id[it.Request], o))
	}
	return "<<" + strings.Join(out, ", ") + ">>"
}

// InflightTLA is the requests in flight as the set of [id, stream, sent].
func InflightTLA(reqs []EdgeRequest, id map[string]string, sent set) string {
	var out []string
	for _, r := range reqs {
		_, s := sent[r.ID]
		out = append(out, fmt.Sprintf("[id |-> %s, stream |-> %q, sent |-> %s]", id[r.ID], r.Stream, tlaBool(s)))
	}
	return "{" + strings.Join(out, ", ") + "}"
}

// JudgementsTLA is the open judgements of the model's kinds as the set of
// [kind, about].
func JudgementsTLA(v *View, sc TraceScenario) string {
	var out []string
	for _, id := range sortedKeys(v.Judgements) {
		j := v.Judgements[id]
		if j.State != "open" || !slices.Contains([]string{"unserved", "cannot-read", "policy", "land", "overlap", "no-policy", "read-faults"}, j.Kind) {
			continue
		}
		about := j.About
		switch j.Kind {
		case "unserved":
			about = sc.TierName(about)
		case "no-policy":
			about = "coord"
		}
		out = append(out, fmt.Sprintf("[kind |-> %q, about |-> %q]", j.Kind, about))
	}
	return "{" + strings.Join(out, ", ") + "}"
}

func tlaBool(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}
