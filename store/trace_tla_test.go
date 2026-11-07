package store

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// storeTrace is a recording in tla/StoreTrace.tla's shape: one record per
// step (the first is Init), each with the step, its op and the projected
// state's changes since the record before; and the ops it names, in the
// model's terms.
type storeTrace struct {
	recs   []string
	ops    map[string]string
	prev   ModelState
	puts   int // put policy writes among the ops: the versions the model needs
	errors []string
}

func newStoreTrace(init ModelState) *storeTrace {
	tr := &storeTrace{ops: map[string]string{}, prev: TraceState(emptySnapshot(1), nil)}
	tr.add(`"Init"`, nil, init)
	return tr
}

// step records one step: action is Apply, LoseRequest or Replay; rc is the
// receipt, nil when the caller did not learn the outcome (or there is none).
// A refusal's reason rides along for the reader; the model does not read it.
func (tr *storeTrace) step(action string, op Op, clear bool, rc *Receipt, st ModelState) {
	if _, ok := tr.ops[op.ID]; !ok {
		m, err := TraceOp(op, clear)
		if err != nil {
			tr.errors = append(tr.errors, err.Error())
			return
		}
		tr.ops[op.ID] = tlaOp(m)
		for _, x := range m.Writes {
			if x.K == "putPolicy" {
				tr.puts++
			}
		}
	}
	f := []string{"action |-> " + strconv.Quote(action), "op |-> " + strconv.Quote(op.ID)}
	if action == "Apply" {
		f = append(f, "known |-> "+tlaBool(rc != nil))
	}
	if rc != nil {
		f = append(f, "accepted |-> "+tlaBool(rc.Accepted))
	}
	if rc != nil && !rc.Accepted {
		f = append(f, "reason |-> "+strconv.Quote(rc.Reason))
	}
	tr.add("", f, st)
}

func (tr *storeTrace) add(init string, f []string, st ModelState) {
	head := []string{"i |-> " + strconv.Itoa(len(tr.recs)+1)}
	if init != "" {
		head = append(head, "action |-> "+init)
	}
	f = append(append(head, f...), "epoch |-> "+strconv.Itoa(st.Epoch),
		"cards |-> "+tlaDiff(tr.prev.Cards, st.Cards, func(c ModelCard) string { return c.tla() }, ModelCard{}.absent()),
		"rows |-> "+tlaDiff(tr.prev.Rows, st.Rows, func(r ModelRow) string { return r.tla() }, ModelRow{}.absent()),
		"judgements |-> "+tlaDiff(tr.prev.Judgements, st.Judgements, strconv.Quote, `"absent"`),
		"cursors |-> "+tlaDiff(tr.prev.Cursors, st.Cursors, func(n int64) string { return strconv.FormatInt(n, 10) }, "0"),
		"policies |-> "+tlaDiff(tr.prev.Policies, st.Policies, strconv.Quote, `"absent"`),
		"stopped |-> "+tlaBool(st.Stopped))
	tr.recs = append(tr.recs, "["+strings.Join(f, ", ")+"]")
	tr.prev = st
}

// tlaDiff is the entries of next that differ from prev, absent ones as
// absent: a TLA+ function (k :> v @@ ...), or <<>> when nothing changed.
func tlaDiff[K int | string, V any](prev, next map[K]V, tla func(V) string, absent string) string {
	keys := map[K]bool{}
	for k := range prev {
		keys[k] = true
	}
	for k := range next {
		keys[k] = true
	}
	var parts []string
	for k := range keys {
		a, inA := prev[k]
		b, inB := next[k]
		if inA == inB && reflect.DeepEqual(a, b) {
			continue
		}
		v := absent
		if inB {
			v = tla(b)
		}
		parts = append(parts, tlaKey(k)+" :> "+v)
	}
	if len(parts) == 0 {
		return "<<>>"
	}
	sort.Strings(parts)
	return "(" + strings.Join(parts, " @@ ") + ")"
}

func tlaKey[K int | string](k K) string {
	if s, ok := any(k).(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(k)
}

func tlaBool(b bool) string { return strings.ToUpper(strconv.FormatBool(b)) }

func tlaSet(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = strconv.Quote(s)
	}
	return "{" + strings.Join(q, ", ") + "}"
}

func (c ModelCard) tla() string {
	return fmt.Sprintf("[state |-> %q, gen |-> %d, holder |-> %q, needs |-> %s, kind |-> %q, prod |-> %q, tier |-> %q, red |-> %s]",
		c.State, c.Gen, c.Holder, tlaSet(c.Needs), c.Kind, c.Prod, c.Tier, tlaBool(c.Red))
}

// absent is Store.tla's NoCard.
func (ModelCard) absent() string {
	return ModelCard{State: "absent", Holder: "none", Kind: "-", Prod: "-", Tier: "-"}.tla()
}

func (r ModelRow) tla() string {
	return fmt.Sprintf("[state |-> %q, gen |-> %d, held |-> %s, sess |-> %q, role |-> %q]", r.State, r.Gen, tlaBool(r.Held), r.Sess, r.Role)
}

// absent is Store.tla's NoRow.
func (ModelRow) absent() string { return ModelRow{State: "absent", Sess: "-", Role: "-"}.tla() }

func tlaOp(m ModelOp) string {
	kind := "op"
	if m.Clear {
		kind = "clear"
	}
	var xs, ws []string
	for _, c := range m.Expect {
		xs = append(xs, fmt.Sprintf("[k |-> %q, c |-> %q, r |-> %q, s |-> %q, g |-> %d]", c.K, c.C, c.R, c.S, c.G))
	}
	for _, x := range m.Writes {
		ws = append(ws, fmt.Sprintf("[k |-> %q, c |-> %q, r |-> %q, s |-> %q, n |-> %s, w |-> %d, kd |-> %q, p |-> %q]", x.K, x.C, x.R, x.S, tlaSet(x.N), x.W, x.Kd, x.P))
	}
	return fmt.Sprintf("[kind |-> %q, epoch |-> %d, expect |-> {%s}, writes |-> <<%s>>]", kind, m.Epoch, strings.Join(xs, ", "), strings.Join(ws, ", "))
}

// write writes StoreTraceLog.tla and StoreTrace.cfg into dir; expect is
// "matched" or "forbidden at K" (the cfg's expect line, for check-trace.sh).
func (tr *storeTrace) write(dir, expect string) error {
	if len(tr.errors) > 0 {
		return fmt.Errorf("ops outside the model: %s", strings.Join(tr.errors, "; "))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ids := make([]string, 0, len(tr.ops))
	for id := range tr.ops {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int { return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b)) })
	var ops []string
	for _, id := range ids {
		ops = append(ops, "  "+id+" |-> "+tr.ops[id])
	}
	var b strings.Builder
	b.WriteString("---- MODULE StoreTraceLog ----\n\\* Generated by store/trace_test.go; do not edit.\nEXTENDS Naturals, TLC\n\n")
	fmt.Fprintf(&b, "TCards == %s\nTRows == %s\nTJudgements == %s\nTCursors == %s\nTBodies == %s\nTVersions == 1..%d\n\n",
		tlaSet(twCards), tlaSet(twRows), tlaSet(twJudgements), tlaSet(twCursors), tlaSet(twBodies), tr.puts+1)
	fmt.Fprintf(&b, "TOps == [\n%s\n]\n\nTraceLog == <<\n  %s\n>>\n====\n", strings.Join(ops, ",\n"), strings.Join(tr.recs, ",\n  "))
	if err := os.WriteFile(filepath.Join(dir, "StoreTraceLog.tla"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	cfg := fmt.Sprintf("\\* Generated by store/trace_test.go; do not edit.\n\\* expect: %s\nCONSTANT MaxLoss = %d\nCONSTANT Scenario = \"trace\"\n", expect, traceMaxLoss)
	for _, o := range []string{"Cards", "Rows", "Judgements", "Cursors", "Bodies", "Versions", "Ops", "Active", "Lossy", "Pred"} {
		cfg += fmt.Sprintf("CONSTANT %s <- T%s\n", o, o)
	}
	cfg += "SPECIFICATION TraceSpec\nPROPERTY TraceMatched\nCHECK_DEADLOCK FALSE\n"
	return os.WriteFile(filepath.Join(dir, "StoreTrace.cfg"), []byte(cfg), 0o644)
}
