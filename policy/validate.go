package policy

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"go.starlark.net/starlark"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// The validator: each function checks one answer against its question's
// domain and returns the typed answer, or the refusal ("" when in domain).
// The refusal is the text after the subject in a Failure.

const maxSeconds = 86400

func refuse(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// kind is a value's type with its article: "a list", "an int".
func kind(v starlark.Value) string {
	if v == nil {
		return "nothing"
	}
	if strings.ContainsRune("aeiou", rune(v.Type()[0])) {
		return "an " + v.Type()
	}
	return "a " + v.Type()
}

// checkOrder: a tuple of up to 4 ints (each fitting int64) or strings.
func checkOrder(v starlark.Value) ([]any, string) {
	t, ok := v.(starlark.Tuple)
	if !ok {
		return nil, refuse("the answer is %s, not a tuple of up to 4 ints or strings", kind(v))
	}
	if len(t) > 4 {
		return nil, refuse("the answer has %d elements, over 4", len(t))
	}
	out := make([]any, len(t))
	for i, x := range t {
		switch y := x.(type) {
		case starlark.String:
			out[i] = string(y)
		case starlark.Int:
			n, ok := y.Int64()
			if !ok {
				return nil, refuse("element %d does not fit a 64-bit int", i+1)
			}
			out[i] = n
		default:
			return nil, refuse("element %d is %s, not an int or a string", i+1, kind(x))
		}
	}
	return out, ""
}

// checkRows: the candidates to use, in preference order, never a row below
// the card's tier, never empty while a candidate exists, no name twice.
func checkRows(v starlark.Value, c store.Card, cands []store.Row, rows map[string]store.Row) ([]string, string) {
	names, msg := stringList(v)
	switch {
	case msg != "":
		return nil, msg
	case len(names) == 0 && len(cands) > 0:
		return nil, refuse("the answer is empty while %d candidate(s) exist", len(cands))
	}
	for i, n := range names {
		r, known := rows[n]
		switch {
		case known && rung(Rank(r)) < rung(c.Tier):
			return nil, refuse("row %s has rank %s, below the card's tier %s", n, cmp.Or(Rank(r), "none"), c.Tier)
		case !slices.ContainsFunc(cands, func(r store.Row) bool { return r.Name == n }):
			return nil, refuse("row %s is not a candidate (the candidates: %s)", n, cmp.Or(rowNames(cands), "none"))
		case slices.Contains(names[:i], n):
			return nil, refuse("row %s is named twice", n)
		}
	}
	return names, ""
}

func rowNames(rs []store.Row) string {
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name
	}
	return strings.Join(names, " ")
}

func stringList(v starlark.Value) ([]string, string) {
	var items []starlark.Value
	switch x := v.(type) {
	case *starlark.List:
		for i := range x.Len() {
			items = append(items, x.Index(i))
		}
	case starlark.Tuple:
		items = x
	default:
		return nil, refuse("the answer is %s, not a list of row names", kind(v))
	}
	out := make([]string, len(items))
	for i, x := range items {
		s, ok := x.(starlark.String)
		if !ok {
			return nil, refuse("element %d is %s, not a row name", i+1, kind(x))
		}
		out[i] = string(s)
	}
	return out, ""
}

// checkReads: the count of reads, 1..5.
func checkReads(v starlark.Value) (int, string) { return intIn("the answer", v, 1, 5) }

// checkVerdict: yes or fix, never yes with no ok read nor fix with no fix read.
func checkVerdict(v starlark.Value, results []string) (string, string) {
	s, ok := v.(starlark.String)
	switch {
	case !ok || (s != "yes" && s != "fix"):
		return "", refuse("the answer is %s, not \"yes\" or \"fix\"", show(v))
	case s == "yes" && !slices.Contains(results, "ok"):
		return "", refuse("\"yes\" with no ok read")
	case s == "fix" && !slices.Contains(results, "fix"):
		return "", refuse("\"fix\" with no fix read")
	}
	return string(s), ""
}

func show(v starlark.Value) string {
	if s, ok := v.(starlark.String); ok {
		return s.String()
	}
	return kind(v)
}

// checkWidth: {"work": w, "ahead": a}, 0 <= w <= width, 0 <= a <= 4 x width.
func checkWidth(v starlark.Value, width int) (Width, string) {
	d, msg := dictOf(v, "work", "ahead")
	if msg != "" {
		return Width{}, msg
	}
	w, msg := intIn("work", d["work"], 0, width)
	if msg != "" {
		return Width{}, msg + " (the row's width)"
	}
	a, msg := intIn("ahead", d["ahead"], 0, 4*width)
	if msg != "" {
		return Width{}, msg + " (4 x the row's width)"
	}
	return Width{Work: w, Ahead: a}, ""
}

// checkLimits: every key present, each in its range.
func checkLimits(v starlark.Value) (Limits, string) {
	d, msg := dictOf(v, "bounces", "faults", "same_finding", "reports_per_tick", "route_rest_window", "route_rest_after")
	if msg != "" {
		return Limits{}, msg
	}
	var l Limits
	for _, f := range []struct {
		key    string
		lo, hi int
		dst    *int
	}{{"bounces", 1, 100, &l.Bounces}, {"faults", 1, 100, &l.Faults}, {"same_finding", 1, 10, &l.SameFinding},
		{"reports_per_tick", 1, 10000, &l.ReportsPerTick}, {"route_rest_window", 1, 100, &l.RouteRestWindow}} {
		if *f.dst, msg = intIn(f.key, d[f.key], f.lo, f.hi); msg != "" {
			return Limits{}, msg
		}
	}
	if l.RouteRestAfter, msg = intIn("route_rest_after", d["route_rest_after"], 1, l.RouteRestWindow); msg != "" {
		return Limits{}, msg + " (route_rest_window)"
	}
	return l, ""
}

// checkLanding: {"backed_up": b, "flowing": f}, 0 <= f < b <= 1000.
func checkLanding(v starlark.Value) (Landing, string) {
	d, msg := dictOf(v, "backed_up", "flowing")
	if msg != "" {
		return Landing{}, msg
	}
	b, msg := intIn("backed_up", d["backed_up"], 1, 1000)
	if msg != "" {
		return Landing{}, msg
	}
	f, msg := intIn("flowing", d["flowing"], 0, b-1)
	if msg != "" {
		return Landing{}, msg + " (below backed_up)"
	}
	return Landing{BackedUp: b, Flowing: f}, ""
}

// checkTimeouts: seconds, each 1..86400, base_regate a rising list, the
// withdraw silence at or above the gone one: no wait is ever unbounded.
func checkTimeouts(v starlark.Value) (Timeouts, string) {
	d, msg := dictOf(v, "row_silent_gone", "row_silent_withdraw", "landing_stall", "base_regate", "judgement", "route_rest_for")
	if msg != "" {
		return Timeouts{}, msg
	}
	var t Timeouts
	for _, f := range []struct {
		key string
		dst *int
	}{{"row_silent_gone", &t.RowSilentGone}, {"landing_stall", &t.LandingStall}, {"judgement", &t.Judgement}, {"route_rest_for", &t.RouteRestFor}} {
		if *f.dst, msg = intIn(f.key, d[f.key], 1, maxSeconds); msg != "" {
			return Timeouts{}, msg
		}
	}
	if t.RowSilentWithdraw, msg = intIn("row_silent_withdraw", d["row_silent_withdraw"], t.RowSilentGone, maxSeconds); msg != "" {
		return Timeouts{}, msg + " (at or above row_silent_gone)"
	}
	t.BaseRegate, msg = rising(d["base_regate"])
	return t, msg
}

func rising(v starlark.Value) ([]int, string) {
	l, ok := v.(*starlark.List)
	if !ok || l.Len() == 0 {
		return nil, refuse("base_regate is %s, not a rising list of seconds", cmp.Or(map[bool]string{true: "an empty list"}[ok], kind(v)))
	}
	out := make([]int, l.Len())
	for i := range out {
		lo := 1
		if i > 0 {
			lo = out[i-1] + 1
		}
		var msg string
		if out[i], msg = intIn(fmt.Sprintf("base_regate element %d", i+1), l.Index(i), lo, maxSeconds); msg != "" {
			return nil, msg + " (a rising list)"
		}
	}
	return out, ""
}

// checkStitch: True or False.
func checkStitch(v starlark.Value) (bool, string) {
	b, ok := v.(starlark.Bool)
	if !ok {
		return false, refuse("the answer is %s, not True or False", kind(v))
	}
	return bool(b), ""
}

// checkPrices: model name to {"in": n, "out": n}, ints at or above 0.
func checkPrices(v starlark.Value) (map[string]Price, string) {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return nil, refuse("the answer is %s, not a dict", kind(v))
	}
	out := map[string]Price{}
	for _, kv := range d.Items() {
		model, ok := kv[0].(starlark.String)
		if !ok {
			return nil, refuse("a key is %s, not a model name", kind(kv[0]))
		}
		p, msg := dictOf(kv[1], "in", "out")
		if msg != "" {
			return nil, string(model) + ": " + msg
		}
		var price Price
		if price.In, msg = int64In(string(model)+" in", p["in"]); msg == "" {
			price.Out, msg = int64In(string(model)+" out", p["out"])
		}
		if msg != "" {
			return nil, msg
		}
		out[string(model)] = price
	}
	return out, ""
}

// dictOf is v as a dict of exactly keys, by name.
func dictOf(v starlark.Value, keys ...string) (map[string]starlark.Value, string) {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return nil, refuse("the answer is %s, not a dict of %s", kind(v), strings.Join(keys, " "))
	}
	out := map[string]starlark.Value{}
	for _, kv := range d.Items() {
		k, ok := kv[0].(starlark.String)
		if !ok || !slices.Contains(keys, string(k)) {
			return nil, refuse("key %s is not one of %s", kv[0].String(), strings.Join(keys, " "))
		}
		out[string(k)] = kv[1]
	}
	for _, k := range keys {
		if out[k] == nil {
			return nil, refuse("key %s is missing", k)
		}
	}
	return out, ""
}

// intIn is v as an int in lo..hi.
func intIn(what string, v starlark.Value, lo, hi int) (int, string) {
	i, ok := v.(starlark.Int)
	if !ok {
		return 0, refuse("%s is %s, not an int", what, kind(v))
	}
	n, ok := i.Int64()
	if !ok || n < int64(lo) || n > int64(hi) {
		return 0, refuse("%s %s is outside %d..%d", what, i.String(), lo, hi)
	}
	return int(n), ""
}

// int64In is v as an int in 0..MaxInt64.
func int64In(what string, v starlark.Value) (int64, string) {
	i, ok := v.(starlark.Int)
	if !ok {
		return 0, refuse("%s is %s, not an int", what, kind(v))
	}
	n, ok := i.Int64()
	if !ok || n < 0 {
		return 0, refuse("%s %s is outside 0..%d", what, i.String(), int64(math.MaxInt64))
	}
	return n, ""
}
