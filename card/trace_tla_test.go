package card

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func quoted(s string) string { return strconv.Quote(s) }

func tlaBool(b bool) string { return strings.ToUpper(strconv.FormatBool(b)) }

func tlaSet(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = strconv.Quote(s)
	}
	slices.Sort(q)
	return "{" + strings.Join(q, ", ") + "}"
}

func (s Slot) tla() string { return fmt.Sprintf("<<%q, %d>>", s.P, s.I) }

func (p ModelProd) tla() string {
	return fmt.Sprintf("[st |-> %q, att |-> %d, head |-> %d, author |-> %q, bounces |-> %d, score |-> %d, prio |-> %q, needs |-> %s, after |-> %s, review |-> %d, ncut |-> %d, finding |-> %q, red |-> %s]",
		p.St, p.Att, p.Head, p.Author, p.Bounces, p.Score, p.Prio, tlaSet(p.Needs), tlaSet(p.After), p.Review, p.Ncut, p.Finding, tlaBool(p.Red))
}

func (c ModelCons) tla() string {
	return fmt.Sprintf("[st |-> %q, kind |-> %q, row |-> %q, att |-> %d, head |-> %d, res |-> %q, author |-> %q, review |-> %d, prior |-> %d, finding |-> %q]",
		c.St, c.Kind, c.Row, c.Att, c.Head, c.Res, c.Author, c.Review, c.Prior, c.Finding)
}

func (r ModelRow) tla() string {
	return fmt.Sprintf("[st |-> %q, flaps |-> %d, width |-> %d, held |-> %s]", r.St, r.Flaps, r.Width, tlaBool(r.Held))
}

// tlaDiff is the entries of next that differ from prev: a TLA+ function
// (k :> v @@ ...), or <<>> when nothing changed. Nothing is ever removed
// from a walk's state (no clear), so a removed entry is not rendered.
func tlaDiff[K comparable, V any](prev, next map[K]V, tla func(V) string, key func(K) string) string {
	var parts []string
	for k, b := range next {
		if a, ok := prev[k]; ok && reflect.DeepEqual(a, b) {
			continue
		}
		parts = append(parts, key(k)+" :> "+tla(b))
	}
	if len(parts) == 0 {
		return "<<>>"
	}
	sort.Strings(parts)
	return "(" + strings.Join(parts, " @@ ") + ")"
}

// args is the record's action and arguments for the move the walk just
// landed (w.ev, nil for a raw stream hold).
func (tr *cardTrace) args(label string, plan Plan) ([]string, error) {
	subject := strings.Fields(label)[1]
	s := tr.w.snap()
	slot := func(id string) (string, error) {
		sl, ok := TraceSlot(s, id)
		if !ok {
			return "", fmt.Errorf("card: trace: %s has no slot", id)
		}
		return sl.tla(), nil
	}
	head, act := traceHead, traceAct
	p := quoted(subject)
	switch e := tr.w.ev.(type) {
	case nil:
		if held := plan.Writes[0].(store.SetStreamHeld).Held; held {
			return act("RawHold"), nil
		}
		return act("UnholdStream"), nil
	case Resolve, Park, Reached, ReadYes, ReadFix, WaiveRed, Cancel, Block, Unblock, Edit:
		return act(name(e), "p", p), nil
	case Start:
		return act("Start", "p", p, "r", quoted(e.Row)), nil
	case AskRead:
		return act("AskRead", "p", p, "r", quoted(e.Row)), nil
	case AskReads:
		if len(e.Rows) == 1 { // one row: the model's AskRead
			return act("AskRead", "p", p, "r", quoted(e.Rows[0])), nil
		}
		return act("AskReads", "p", p, "rows", tlaSet(e.Rows)), nil
	case Landed:
		return act("Landed", "p", p, "h", head(e.Head)), nil
	case Red:
		return act("Red", "p", p, "h", head(e.Head)), nil
	case Replace:
		return act("Replace", "p", p, "by", quoted(e.By)), nil
	case NeedFailed:
		return act("NeedFailed", "p", p, "n", quoted(e.Need)), inThisOp(e.InThisOp)
	case Waive:
		return act("Waive", "p", p, "n", quoted(e.Need)), inThisOp(e.InThisOp)
	case Relink:
		return act("Relink", "p", p, "n", quoted(e.Need), "to", quoted(e.To)), inThisOp(e.InThisOp)
	case Rescore:
		return act("Rescore", "p", p, "s", strconv.Itoa(e.Score)), nil
	case SetPriority:
		return act("SetPriority", "p", p, "lv", quoted(e.Level)), nil
	case Seen, Gone, Hold, Unhold:
		return act(name(e), "r", p), nil
	}
	return tr.consumerArgs(subject, slot, head, act)
}

// traceHead is a head's index in traceHeads; the base and "" are 0.
func traceHead(h string) string {
	if h == "" {
		return "0"
	}
	return strconv.Itoa(slices.Index(traceHeads, h))
}

// traceAct is a record's action and its arguments, given as key, value pairs.
func traceAct(a string, kv ...string) []string {
	out := []string{"action |-> " + quoted(a)}
	for i := 0; i < len(kv); i += 2 {
		out = append(out, kv[i]+" |-> "+kv[i+1])
	}
	return out
}

func inThisOp(b bool) error {
	if b {
		return fmt.Errorf("card: trace: an InThisOp move is one op with its need's drop; the walk merges none")
	}
	return nil
}

// consumerArgs is args for the producer events that name a consumer and the
// consumer events.
func (tr *cardTrace) consumerArgs(subject string, slot func(string) (string, error), head func(string) string, act func(string, ...string) []string) ([]string, error) {
	p := quoted(subject)
	var id string
	var rest []string
	switch e := tr.w.ev.(type) {
	case Finished:
		id, rest = e.Consumer, []string{"Finished", "p", p}
	case Broken:
		id, rest = e.Consumer, []string{"Broken", "p", p}
	case Faulted:
		id, rest = e.Consumer, []string{"Faulted", "p", p}
	case Released:
		id, rest = workID(subject, tr.w.snap().Cards[subject].Attempt), []string{"Released", "p", p}
	case Report:
		id, rest = subject, []string{"Report", "r", quoted(e.Row), "res", quoted(e.Result), "h", head(e.Head)}
	case Withdraw:
		id, rest = subject, []string{"Withdraw", "h", head(e.Head)}
	default:
		return nil, fmt.Errorf("card: trace: event %s is not in the trace", name(tr.w.ev))
	}
	c, err := slot(id)
	if err != nil {
		return nil, err
	}
	return act(rest[0], append(rest[1:], "c", c)...), nil
}

// write writes CardMachineTraceLog.tla and CardMachineTrace.cfg into dir.
func (tr *cardTrace) write(dir, expect string) error {
	if len(tr.errors) > 0 {
		return fmt.Errorf("moves outside the trace: %s", strings.Join(tr.errors, "; "))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var prods, sents, rows, needs, after []string
	for id, c := range tr.init.Cards {
		switch c.Kind {
		case "producer":
			prods = append(prods, id)
		case "sentinel":
			sents = append(sents, id)
		}
		needs = append(needs, fmt.Sprintf("%q :> %s", id, tlaSet(c.Needs)))
		after = append(after, fmt.Sprintf("%q :> %s", id, tlaSet(c.After)))
	}
	for n := range tr.init.Rows {
		rows = append(rows, n)
	}
	slices.Sort(rows)
	sort.Strings(needs)
	sort.Strings(after)
	idx := make([]string, len(rows))
	for i, r := range rows {
		idx[i] = fmt.Sprintf("%q :> %d", r, i+1)
	}
	var b strings.Builder
	b.WriteString("---- MODULE CardMachineTraceLog ----\n\\* Generated by card/trace_test.go; do not edit.\nEXTENDS Naturals, TLC\n\n")
	fmt.Fprintf(&b, "TAdded == {}\nTProds == %s\nTSentinels == %s\nTRows == %s\nTRowIdx == (%s)\n", tlaSet(prods), tlaSet(sents), tlaSet(rows), strings.Join(idx, " @@ "))
	fmt.Fprintf(&b, "TNeeds0 == (%s)\nTAfter0 == (%s)\n\n", strings.Join(needs, " @@ "), strings.Join(after, " @@ "))
	fmt.Fprintf(&b, "TraceLog == <<\n  %s\n>>\n====\n", strings.Join(tr.recs, ",\n  "))
	if err := os.WriteFile(filepath.Join(dir, "CardMachineTraceLog.tla"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	cfg := fmt.Sprintf("\\* Generated by card/trace_test.go; do not edit.\n\\* expect: %s\nCONSTANT Scenario = \"trace\"\n", expect)
	cfg += fmt.Sprintf("CONSTANT K = %d\nCONSTANT MaxHead = %d\nCONSTANT MaxAttempt = %d\nCONSTANT MaxBounce = %d\nCONSTANT MaxEnv = %d\n",
		tr.bounds["K"]+1, len(traceHeads)-1, tr.bounds["MaxAttempt"]+1, tr.bounds["MaxBounce"]+1, len(tr.recs))
	for _, o := range []string{"Prods", "Sentinels", "Added", "Rows", "RowIdx", "Needs0", "After0"} {
		cfg += fmt.Sprintf("CONSTANT %s <- T%s\n", o, o)
	}
	cfg += "SPECIFICATION TraceSpec\nPROPERTY TraceMatched\nCHECK_DEADLOCK FALSE\n"
	return os.WriteFile(filepath.Join(dir, "CardMachineTrace.cfg"), []byte(cfg), 0o644)
}
