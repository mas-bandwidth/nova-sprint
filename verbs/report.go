package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// HeadVerifier checks origin branch, PR and other external finish prerequisites.
// A nonempty problem and fix is a refusal; errors must not carry secrets.
type HeadVerifier interface {
	VerifyHead(context.Context, store.Card, string) (problem, fix string, err error)
}

// FaultVerifier determines whether a fault is a harness, bench, or route failure.
type FaultVerifier interface {
	VerifyFault(context.Context, store.Card, string) (bool, error)
}

type reportInput struct {
	id, actor, result, head, reason, finding string
	gen, epoch                               int
	cost                                     int64
	priced                                   bool
	jsonOutput, pricedSet, microSet, usdSet  bool
	opID                                     string
}

func parseReport(verb string, args []string, actor string) (reportInput, bool) {
	in := reportInput{actor: actor}
	if len(args) < 1 {
		return in, false
	}
	var ok bool
	in.id, in.gen, ok = cardRef(args[0])
	if !ok {
		return in, false
	}
	i := 1
	if verb == "read" {
		if len(args) < 2 {
			return in, false
		}
		switch args[1] {
		case "yes":
			in.result = "ok"
		case "fix":
			in.result = "fix"
		default:
			return in, false
		}
		i++
	}
	if !parseReportFlags(verb, args[i:], &in) {
		return in, false
	}
	if verb == "read" && in.finding != "" {
		in.reason = in.finding
	}
	if verb == "finish" && in.result == "" {
		in.result = "ok"
	}
	if ((verb == "report" && !in.jsonOutput) || in.result == "fail" || in.result == "fault") && in.reason == "" {
		return in, false
	}
	if in.jsonOutput {
		if in.epoch == 0 || in.opID == "" || !in.microSet || !in.pricedSet || in.usdSet {
			return in, false
		}
	} else if in.opID != "" || in.microSet || in.pricedSet {
		return in, false
	}
	return in, in.actor != "" && in.result != ""
}

func parseReportFlags(verb string, args []string, in *reportInput) bool {
	i := 0
	seen := map[string]bool{}
	for ; i < len(args); i++ {
		if seen[args[i]] {
			return false
		}
		seen[args[i]] = true
		if args[i] == "--json" {
			in.jsonOutput = true
			continue
		}
		if args[i] == "--failed" && verb == "finish" {
			in.result = "fail"
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" {
			return false
		}
		if args[i] == "--result" && verb != "report" {
			return false
		}
		if args[i] == "--finding" && verb != "read" && verb != "report" {
			return false
		}
		if !reportFlag(in, args[i], args[i+1]) {
			return false
		}
		i++
	}
	return true
}

func reportFlag(in *reportInput, key, value string) bool {
	switch key {
	case "--as":
		in.actor = value
	case "--epoch":
		var err error
		in.epoch, err = strconv.Atoi(value)
		if err != nil || in.epoch < 1 {
			return false
		}
	case "--head":
		in.head = value
	case "--reason":
		in.reason = value
	case "--finding":
		in.finding = value
	case "--result":
		in.result = value
	case "--id":
		in.opID = value
	case "--cost-microusd":
		if !decimalDigits(value) {
			return false
		}
		var err error
		in.cost, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false
		}
		in.microSet = true
	case "--priced":
		if value != "true" && value != "false" {
			return false
		}
		in.priced = value == "true"
		in.pricedSet = true
	case "--cost":
		in.usdSet = true
		var err error
		in.cost, err = parseCost(value)
		if err != nil {
			return false
		}
		in.priced = true
	default:
		return false
	}
	return true
}

func (e *Engine) report(ctx context.Context, verb string, args []string) Result {
	in, ok := parseReport(verb, args, e.Actor)
	if !ok {
		return refused(verb, "invalid arguments", "nova-sprint help "+verb)
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Now == nil || e.Env == nil {
		return unavailable(verb)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return unavailable(verb)
	}
	if in.epoch != 0 && in.epoch != snap.Epoch {
		return epochRefusal(verb, in.epoch, snap.Epoch)
	}
	opID := consumerOpID(verb, snap.Epoch, in.id, in.gen)
	if in.jsonOutput && in.opID != opID {
		return refused(verb, "op id must be "+opID, "use the derived operation id")
	}
	r, found, err := st.Replay(ctx, opID)
	if err != nil {
		return operationalFailure(verb, err)
	}
	if found {
		return formatReportReceipt(r, in, verb, opID)
	}
	if e.Reports == nil {
		return unavailable(verb)
	}
	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return unavailable(verb)
	}
	if !hooked {
		return refused(verb, in.actor+" is not hooked in", recipe)
	}
	c, ok := snap.Cards[in.id]
	if !ok || c.Gen != in.gen || c.Holder != in.actor {
		return refused(verb, in.id+" is not held by "+in.actor, "nova-sprint show "+in.id)
	}
	if result, stop := e.checkReport(ctx, verb, snap, c, &in); stop {
		return result
	}
	return e.applyReport(ctx, st, snap, c, in, opID, verb)
}

func (e *Engine) checkReport(ctx context.Context, verb string, snap store.Snapshot, c store.Card, in *reportInput) (Result, bool) {
	if in.result == "fault" {
		if e.Faults == nil {
			return unavailable(verb), true
		}
		allowed, err := e.Faults.VerifyFault(ctx, c, in.reason)
		if err != nil {
			return unavailable(verb), true
		}
		if !allowed {
			return refused(verb, "fault needs a harness, bench or route failure", "nova-sprint help report"), true
		}
	}
	if c.Kind == "read" && in.result == "ok" && in.head == "" {
		in.head = c.Head
	}
	if verb == "finish" && c.Kind != "work" {
		return refused(verb, in.id+" is not a work card", "nova-sprint show "+in.id), true
	}
	if verb == "read" && c.Kind != "read" {
		return refused(verb, in.id+" is not a read", "nova-sprint show "+in.id), true
	}
	if c.Kind == "read" {
		p, found := snap.Cards[c.Of]
		if !found {
			return refused(verb, "producer "+c.Of+" is missing", "nova-sprint show "+in.id), true
		}
		if in.actor == p.Author || in.actor == c.Author {
			return refused(verb, in.actor+" wrote the head and cannot read it", "nova-sprint next"), true
		}
	}
	if c.Kind == "read" && in.result == "fix" && in.finding != "" {
		in.reason = in.finding
	}
	if in.result == "fix" && in.reason == "" {
		return refused(verb, "a fix needs a finding", "nova-sprint read "+in.id+"@"+strconv.Itoa(in.gen)+" fix --finding <text>"), true
	}
	if c.Kind == "work" && in.result == "ok" {
		if e.Heads == nil {
			return unavailable(verb), true
		}
		problem, fix, err := e.Heads.VerifyHead(ctx, c, in.head)
		if err != nil {
			return unavailable(verb), true
		}
		if problem != "" {
			return refused(verb, problem, fix), true
		}
	}
	return Result{}, false
}

func (e *Engine) applyReport(ctx context.Context, st ApplyStore, snap store.Snapshot, c store.Card, in reportInput, id, verb string) Result {
	plan, why := e.Reports.PlanReport(snap, c, card.Report{Row: in.actor, Result: in.result, Head: in.head, Reason: in.reason, Cost: in.cost, Priced: in.priced})
	if why != nil {
		return refused(verb, why.Reason, "nova-sprint show "+in.id)
	}
	if c.Kind == "read" {
		plan.Expect = append(plan.Expect, store.AtSeq{Seq: snap.Seq})
	}
	next := "nothing; wait for the bus"
	if len(obligations(snap, in.actor)) > 1 {
		next = "nova-sprint next"
	}
	op := store.Op{ID: id, Epoch: snap.Epoch, Actor: in.actor, Kind: "report", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes, Note: next}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return unavailable(verb)
	}
	return formatReportReceipt(rec, in, verb, id)
}

func formatReportReceipt(r store.Receipt, in reportInput, verb, opID string) Result {
	if !in.jsonOutput {
		return reportReceipt(r, in.actor, verb, in.id)
	}
	if r.Op != opID || r.Actor != in.actor || r.Kind != "report" {
		return refused(verb, "operation cannot be performed", "nova-sprint next --as "+in.actor)
	}
	if !r.Accepted {
		return refused(verb, r.Reason, "nova-sprint show "+in.id)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return unavailable(verb)
	}
	return Result{string(data) + "\n", 0}
}

func parseCost(s string) (int64, error) {
	parts := strings.Split(s, ".")
	if len(parts) > 2 || !decimalDigits(parts[0]) {
		return 0, fmt.Errorf("invalid cost")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > math.MaxInt64/1000000 {
		return 0, fmt.Errorf("invalid cost")
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
		if !decimalDigits(frac) || len(frac) > 6 {
			return 0, fmt.Errorf("invalid cost")
		}
	}
	frac += strings.Repeat("0", 6-len(frac))
	fraction, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || whole*1000000 > math.MaxInt64-fraction {
		return 0, fmt.Errorf("invalid cost")
	}
	return whole*1000000 + fraction, nil
}

func reportReceipt(r store.Receipt, actor, verb, id string) Result {
	if r.Actor != actor || r.Kind != "report" {
		return refused(verb, "operation cannot be performed", "nova-sprint next --as "+actor)
	}
	if !r.Accepted {
		return refused(verb, r.Reason, "nova-sprint show "+id)
	}
	if r.After == nil {
		return unavailable(verb)
	}
	c, ok := r.After.Cards[id]
	if !ok {
		return unavailable(verb)
	}
	label := "REPORTED"
	if c.Kind == "read" {
		label = "READ"
	} else {
		switch c.Result {
		case "ok":
			label = "FINISHED"
		case "fail":
			label = "FAILED"
		case "fault":
			label = "FAULTED"
		}
	}
	head := ""
	if c.Head != "" {
		head = " head=" + c.Head
	}
	result := ""
	if c.Kind == "read" {
		result = " result=" + c.Result
	}

	next := r.Note
	if next == "" {
		next = "nothing; wait for the bus"
	}
	return Result{fmt.Sprintf("%s %s%s%s applied at seq %d\nnext: %s\n", label, id, head, result, r.Seq, next), 0}
}

func decimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
