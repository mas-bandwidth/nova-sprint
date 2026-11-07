package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// outcome is a row's report as the three verbs give it.
type outcome struct {
	verb, id, result, head, reason string
	gen                            int
	cost                           int64
	priced, costGiven              bool
	usage                          string // the row's --usage words, priced by the engine (priceReport)
}

// runReport is `finish`, `read` and `report`: one op by the row itself, the
// consumer and its producer together (card.ReportPlan), fenced by gen, its
// id canonical (LAYER-6 section 2, "Reports are ordinary ops").
func runReport(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	o, err := readOutcome(in)
	if err != nil {
		return "", err
	}
	opID, err := machineID(in, snap, "report", o.id, o.gen)
	if err != nil {
		return "", err
	}
	r, found, err := e.Store.Replay(ctx, opID)
	if err != nil {
		return "", err
	}
	if !found {
		if r, err = e.report(ctx, in, snap, o, opID); err != nil {
			return "", err
		}
	}
	return reportOutput(in, snap, r, o.id)
}

// readOutcome parses the verb's form into one outcome.
func readOutcome(in *input) (outcome, error) {
	id, gen, f := cardRef(in.verb, in.arg(0))
	if f != nil {
		return outcome{}, f
	}
	o := outcome{verb: in.verb, id: id, gen: gen, head: in.str("head"), reason: in.str("reason"), usage: in.str("usage")}
	switch in.verb {
	case "finish":
		o.result = map[bool]string{true: "fail", false: "ok"}[in.has("failed")]
	case "read":
		o.result = map[string]string{"yes": "ok", "fix": "fix"}[in.arg(1)]
		if o.result == "" {
			return o, misuse("read", "a read answers yes or fix")
		}
	default:
		o.result = in.str("result")
	}
	if fd := in.str("finding"); fd != "" {
		o.reason = fd
	}
	if err := o.check(in); err != nil {
		return o, err
	}
	var err error
	o.cost, o.priced, err = readCost(in)
	o.costGiven = in.has("cost") || in.has("cost-microusd")
	return o, err
}

func (o outcome) check(in *input) error {
	switch {
	case o.result != "ok" && o.result != "fix" && o.result != "fail" && o.result != "fault":
		return misuse(in.verb, "--result is ok, fix, fail or fault")
	case (o.result == "fail" || o.result == "fault") && o.reason == "":
		return refuse("a "+o.result+" needs its reason", "add --reason \"<what failed, with the command and exit>\"")
	case o.result == "fix" && o.reason == "":
		return refuse("a fix needs a finding", "add --finding \"<path>:<line>: <the defect and the repair>\"")
	case in.verb == "finish" && o.result == "ok" && o.head == "":
		return refuse("a finish names its head", "add --head <the 40-hex sha git ls-remote origin <branch> prints>")
	}
	return nil
}

// readCost is the run's cost: --cost-microusd with --priced from a machine,
// or --cost in dollars from a mind; never both forms.
func readCost(in *input) (int64, bool, error) {
	machine := in.has("cost-microusd") || in.has("priced")
	if machine && in.has("cost") {
		return 0, false, refuse("--cost mixes with --cost-microusd and --priced", "give --cost <usd>, or --cost-microusd <n> --priced true|false")
	}
	if machine {
		n, err := strconv.ParseInt(in.str("cost-microusd"), 10, 64)
		p := in.str("priced")
		if err != nil || n < 0 {
			return 0, false, misuse(in.verb, "--cost-microusd <n> and --priced true|false go together (--cost-microusd: "+badNumber(in.str("cost-microusd"), err)+")")
		}
		if p != "true" && p != "false" {
			return 0, false, misuse(in.verb, fmt.Sprintf("--cost-microusd <n> and --priced true|false go together (--priced %q)", Redact(p)))
		}
		return n, p == "true", nil
	}
	if !in.has("cost") {
		return 0, false, nil
	}
	n, err := microUSD(in.str("cost"))
	if err != nil {
		return 0, false, RefusalOr(err, misuse(in.verb, "--cost is dollars, at most six decimals: 0.04 ("+err.Error()+")"))
	}
	return n, true, nil
}

// microUSD is a decimal dollar amount in micro-dollars, exactly.
func microUSD(s string) (int64, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		return 0, fmt.Errorf("bad amount: %s", edge.ParseRefused("no whole dollars", 0, []byte(s)))
	}
	if len(frac) > 6 {
		return 0, fmt.Errorf("bad amount: %s", edge.ParseRefused("more than six decimals", len(whole)+7, []byte(s)))
	}
	if i := strings.IndexFunc(whole+"."+frac, func(r rune) bool { return r != '.' && (r < '0' || r > '9') }); i >= 0 {
		return 0, fmt.Errorf("bad amount: %s", edge.ParseRefused("not a digit", i, []byte(s)))
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad amount: %s", edge.ParseFailure(err, []byte(s)))
	}
	if w > math.MaxInt64/1_000_000-1 {
		return 0, fmt.Errorf("bad amount: %s", edge.ParseRefused("too many dollars", 0, []byte(s)))
	}
	f, err := strconv.ParseInt((frac + "000000")[:6], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad amount: %s", edge.ParseFailure(err, []byte(s)))
	}
	return w*1_000_000 + f, nil
}

// report checks what the verb can see and applies the plan.
func (e *Engine) report(ctx context.Context, in *input, snap store.Snapshot, o outcome, opID string) (store.Receipt, error) {
	c, ok := snap.Cards[o.id]
	if !ok || c.Gen != o.gen || c.Holder != in.actor {
		return store.Receipt{}, refuse(o.id+" is not held by "+in.actor+" at generation "+strconv.Itoa(o.gen), "nova-sprint show "+o.id)
	}
	if err := kindFits(in.verb, c, o); err != nil {
		return store.Receipt{}, err
	}
	if c.Kind == "read" {
		if err := readable(in.actor, c, snap, &o); err != nil {
			return store.Receipt{}, err
		}
	} else if o.result == "ok" {
		if err := e.headReady(ctx, snap, c, o.head); err != nil {
			return store.Receipt{}, err
		}
	}
	p, err := e.priceReport(ctx, snap, in.actor, o)
	if err != nil {
		return store.Receipt{}, err
	}
	plan, why := reportPlan(snap, c, card.Report{Row: in.actor, Result: o.result, Head: o.head, Reason: o.reason, Cost: p.Cost, Priced: p.Priced, Usage: p.Record.String()})
	if why != nil {
		return store.Receipt{}, moved(why, o.id)
	}
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: in.actor, Kind: "report", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	return e.commit(ctx, op, "nova-sprint show "+o.id)
}

func kindFits(verb string, c store.Card, o outcome) error {
	switch {
	case verb == "finish" && c.Kind != "work":
		return refuse(c.ID+" is a "+c.Kind+" card", "nova-sprint read "+fmtCard(c.ID, c.Gen)+" yes|fix")
	case verb == "read" && c.Kind != "read":
		return refuse(c.ID+" is a "+c.Kind+" card", "nova-sprint finish "+fmtCard(c.ID, c.Gen)+" --head <sha>")
	case c.Kind == "work" && o.result == "fix":
		return misuse(verb, "fix is a read's answer; a work card reports ok, fail or fault")
	}
	return nil
}

// readable is a read by someone who did not write the head, of the head
// the producer is at.
func readable(row string, c store.Card, snap store.Snapshot, o *outcome) error {
	p := snap.Cards[c.Of]
	if row == p.Author || row == c.Author {
		return refuse(row+" wrote the head and cannot read it", waitNext)
	}
	if o.head == "" {
		o.head = c.Head
	}
	if o.head != c.Head {
		return refuse(fmt.Sprintf("producer %s is at head %s, the read was of %s", p.ID, c.Head, o.head), "nova-sprint show "+c.ID)
	}
	return nil
}

// reportPlan is the card machine's one plan for a report, consumer and
// producer together.
func reportPlan(s store.Snapshot, c store.Card, r card.Report) (card.Plan, *card.Refusal) {
	p := s.Cards[c.Of]
	rows := map[string]card.Row{}
	for n, row := range s.Rows {
		rows[n] = card.RowFromStore(row, 0, nil)
	}
	all := map[string]card.Card{}
	for id, x := range s.Cards {
		all[id] = card.FromStore(x)
	}
	return card.ReportPlan(card.FromStore(c), card.FromStore(p), consumersOf(s, p.ID), rows, card.StreamFromStore(s.Streams[p.Stream]), all, r)
}

func consumersOf(s store.Snapshot, producer string) []card.Card {
	var out []card.Card
	for _, c := range s.Cards {
		if c.Of == producer {
			out = append(out, card.FromStore(c))
		}
	}
	return out
}

// reportOutput is the receipt: JSON for a machine, one line for a mind.
func reportOutput(in *input, snap store.Snapshot, r store.Receipt, id string) (string, error) {
	if r.Actor != in.actor || r.Kind != "report" {
		return "", refuse(fmt.Sprintf("%s was reported by %s", id, r.Actor), "nova-sprint next --as "+in.actor)
	}
	if !r.Accepted {
		return "", refuse(r.Reason, "nova-sprint show "+id)
	}
	if in.has("json") {
		b, err := json.Marshal(r)
		return string(b) + "\n", err
	}
	c := r.After.Cards[id]
	label := map[string]string{"ok": "FINISHED", "fail": "FAILED", "fault": "FAULTED"}[c.Result]
	line := fmt.Sprintf("%s %s head=%s applied at seq %d", label, id, c.Head, r.Seq)
	if c.Kind == "read" {
		line = fmt.Sprintf("READ %s head=%s result=%s applied at seq %d", id, c.Head, c.Result, r.Seq)
	}
	next := waitNext
	if len(working(snap, in.actor)) > 1 {
		next = "nova-sprint next --as " + in.actor
	}
	return done(next, line), nil
}
