package policy

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// refusedBy runs a tick with body as v2 on w and returns its one failure.
func refusedBy(t *testing.T, w *world, body string) Failure {
	t.Helper()
	a, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, w.snap(), FullQuestions(w.snap(), nil))
	require.Len(t, fs, 1)
	assert.Equal(t, 0, a.Version, "the default answered the tick")
	return fs[0]
}

// TestRefusals: every domain refusal, one row each, with its exact text.
func TestRefusals(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	const limits = `"bounces": 6, "faults": 3, "same_finding": 2, "reports_per_tick": 1000, "route_rest_window": 10`
	const times = `"row_silent_gone": 600, "landing_stall": 60, "judgement": 3600, "route_rest_for": 1800`
	for _, tc := range []struct{ q, ret, want string }{
		{"order", "[1]", "order(p1): the answer is a list, not a tuple of up to 4 ints or strings"},
		{"order", "(1, 2, 3, 4, 5)", "order(p1): the answer has 5 elements, over 4"},
		{"order", "(1.5,)", "order(p1): element 1 is a float, not an int or a string"},
		{"order", "(1 << 70,)", "order(p1): element 1 does not fit a 64-bit int"},
		{"rows", `["r2"]`, "rows(p1 work): row r2 has rank flash, below the card's tier pro"},
		{"rows", `["r9"]`, "rows(p1 work): row r9 is not a candidate (the candidates: r1)"},
		{"rows", `["r4"]`, "rows(p1 work): row r4 is not a candidate (the candidates: r1)"},
		{"rows", "[]", "rows(p1 work): the answer is empty while 1 candidate(s) exist"},
		{"rows", `["r1", "r1"]`, "rows(p1 work): row r1 is named twice"},
		{"rows", `"r1"`, "rows(p1 work): the answer is a string, not a list of row names"},
		{"rows", "[1]", "rows(p1 work): element 1 is an int, not a row name"},
		{"reads", "0", "reads(p3): the answer 0 is outside 1..5"},
		{"reads", "6", "reads(p3): the answer 6 is outside 1..5"},
		{"reads", `"1"`, "reads(p3): the answer is a string, not an int"},
		{"verdict", `"maybe"`, `verdict(p3): the answer is "maybe", not "yes", "fix" or "retry"`},
		{"verdict", "True", `verdict(p3): the answer is a bool, not "yes", "fix" or "retry"`},
		{"verdict", `"fix"`, `verdict(p3): "fix" with no fix read`},
		{"width", `{"work": 3, "ahead": 2}`, "width(r1): work 3 is outside 0..2 (the row's width)"},
		{"width", `{"work": -1, "ahead": 2}`, "width(r1): work -1 is outside 0..2 (the row's width)"},
		{"width", `{"work": 1, "ahead": 9}`, "width(r1): ahead 9 is outside 0..8 (4 x the row's width)"},
		{"width", `{"work": 1}`, "width(r1): key ahead is missing"},
		{"width", `{"work": 1, "ahead": 1, "x": 1}`, `width(r1): key "x" is not one of work ahead`},
		{"width", "[1]", "width(r1): the answer is a list, not a dict of work ahead"},
		{"limits", `{"bounces": 0, "faults": 3, "same_finding": 2, "reports_per_tick": 1000, "route_rest_window": 10, "route_rest_after": 3}`, "limits(): bounces 0 is outside 1..100"},
		{"limits", "{" + limits + `, "route_rest_after": 11}`, "limits(): route_rest_after 11 is outside 1..10 (route_rest_window)"},
		{"limits", "{" + limits + "}", "limits(): key route_rest_after is missing"},
		{"limits", "{" + strings.Replace(limits, `"same_finding": 2`, `"same_finding": 11`, 1) + `, "route_rest_after": 3}`, "limits(): same_finding 11 is outside 1..10"},
		{"timeouts", "{" + times + `, "row_silent_withdraw": 300, "base_regate": [120, 300]}`, "timeouts(): row_silent_withdraw 300 is outside 600..86400 (at or above row_silent_gone)"},
		{"timeouts", "{" + strings.Replace(times, "600", "0", 1) + `, "row_silent_withdraw": 1800, "base_regate": [120]}`, "timeouts(): row_silent_gone 0 is outside 1..86400"},
		{"timeouts", "{" + strings.Replace(times, "3600", "86401", 1) + `, "row_silent_withdraw": 1800, "base_regate": [120]}`, "timeouts(): judgement 86401 is outside 1..86400"},
		{"timeouts", "{" + times + `, "row_silent_withdraw": 1800, "base_regate": [300, 120]}`, "timeouts(): base_regate element 2 120 is outside 301..86400 (a rising list)"},
		{"timeouts", "{" + times + `, "row_silent_withdraw": 1800, "base_regate": []}`, "timeouts(): base_regate is an empty list, not a rising list of seconds"},
		{"timeouts", "{" + times + `, "row_silent_withdraw": 1800, "base_regate": 120}`, "timeouts(): base_regate is an int, not a rising list of seconds"},
		{"timeouts", "{" + times + `, "base_regate": [120]}`, "timeouts(): key row_silent_withdraw is missing"},
		{"landing", `{"backed_up": 4, "flowing": 4}`, "landing(s1): flowing 4 is outside 0..3 (below backed_up)"},
		{"landing", `{"backed_up": 1001, "flowing": 1}`, "landing(s1): backed_up 1001 is outside 1..1000"},
		{"stitch", "1", "stitch(s1): the answer is an int, not True or False"},
		{"prices", "[]", "prices(): the answer is a list, not a dict"},
		{"prices", `{"m": {"in": -1, "out": 1}}`, "prices(): m in -1 is outside 0..9223372036854775807"},
		{"prices", `{"m": {"in": 1, "out": 1.0}}`, "prices(): m out is a float, not an int"},
		{"prices", `{"m": {"in": 1}}`, "prices(): m: key out is missing"},
		{"prices", `{1: {"in": 1, "out": 1}}`, "prices(): a key is an int, not a model name"},
		{"width", `{"work": 0, "ahead": 0}`, "rows(p1 work): every row named has work 0 in width, while row r1 could take it"},
	} {
		body, line := override(tc.q, fmt.Sprintf("def %s(%s):\n    return %s", tc.q, params(tc.q), tc.ret))
		if tc.q == "width" && strings.HasPrefix(tc.want, "rows") {
			line = defLine(body, "rows")
		}
		f := refusedBy(t, w, body)
		assert.Equal(t, fmt.Sprintf("POLICY REFUSED v2 line %d %s", line, tc.want), f.Error(), "%s returning %s", tc.q, tc.ret)
	}
}

func defLine(body, q string) int {
	for i, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "def "+q+"(") {
			return i + 1
		}
	}
	return 0
}

// TestRefuseYesWithNoOk: "yes" over reads that are all fix.
func TestRefuseYesWithNoOk(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.apply(store.SetCardResult{Card: "p3-r1", Result: "fix"})
	body, line := override("verdict", "def verdict(card, reads):\n    return \"yes\"")
	f := refusedBy(t, w, body)
	assert.Equal(t, fmt.Sprintf(`POLICY REFUSED v2 line %d verdict(p3): "yes" with no ok read`, line), f.Error())
	a, fs := NewEngine().Answer(Versions{Active: pv(2, defaultBody)}, w.snap(), FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, "fix", a.Verdict["p3"], "the default: any fix read means fix")
}

// fuzzLit is a Starlark literal of any type, size and nesting, from b.
type fuzzLit struct {
	b []byte
	i int
}

var fuzzWords = []string{"yes", "fix", "r1", "r2", "r3", "r4", "r9", "work", "ahead", "backed_up", "flowing", "bounces", "faults",
	"same_finding", "reports_per_tick", "route_rest_window", "route_rest_after", "row_silent_gone", "row_silent_withdraw",
	"landing_stall", "base_regate", "judgement", "route_rest_for", "in", "out", "m"}

func (g *fuzzLit) next() int {
	if g.i >= len(g.b) {
		return 0
	}
	g.i++
	return int(g.b[g.i-1])
}

func (g *fuzzLit) lit(depth int) string {
	c := g.next()
	if depth > 3 {
		c = 11
	}
	switch c % 12 {
	case 0:
		return strconv.Itoa(int(int8(g.next())) * 50)
	case 1:
		return "1 << 70"
	case 2, 3:
		return strconv.Quote(fuzzWords[g.next()%len(fuzzWords)])
	case 4:
		return []string{"True", "False", "None", "1.5"}[g.next()%4]
	case 5, 6, 7:
		items := make([]string, g.next()%6)
		for i := range items {
			items[i] = g.lit(depth + 1)
		}
		switch inner := strings.Join(items, ", "); {
		case c%12 != 7:
			return "[" + inner + "]"
		case inner == "":
			return "()"
		default:
			return "(" + inner + ",)"
		}
	case 8, 9, 10:
		items := make([]string, g.next()%8)
		for i := range items {
			items[i] = strconv.Quote(fuzzWords[g.next()%len(fuzzWords)]) + ": " + g.lit(depth+1)
		}
		return "{" + strings.Join(items, ", ") + "}"
	}
	return strconv.Itoa(g.next() % 10)
}

// FuzzAnswers: a policy whose every question returns a fuzzed value; no
// panic, and either every answer is used in domain or the version is
// refused by name and the default answers the tick.
func FuzzAnswers(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{9, 2, 8, 1, 3, 2})
	f.Add([]byte("width and rows and everything in between, nested deep"))
	w := fakeWorld(f)
	snap := w.snap()
	f.Fuzz(func(t *testing.T, b []byte) {
		g := &fuzzLit{b: b}
		var body strings.Builder
		for _, q := range questionNames() {
			fmt.Fprintf(&body, "def %s(%s):\n    return %s\n\n", q, params(q), g.lit(0))
		}
		a, fs := NewEngine().Answer(Versions{Active: pv(2, body.String())}, snap, FullQuestions(snap, nil))
		if a.Version == 2 {
			require.Empty(t, fs)
			inDomain(t, snap, a)
			return
		}
		require.Len(t, fs, 1, body.String())
		assert.Equal(t, "v2", fs[0].Label)
		assert.Equal(t, 0, a.Version, "the default answered")
		assert.True(t, strings.HasPrefix(fs[0].Error(), "POLICY REFUSED v2"))
	})
}

// inDomain: widths at or below each row's, rows only candidates at or above
// the card's tier.
func inDomain(t *testing.T, snap store.Snapshot, a Answers) {
	for name, wd := range a.Width {
		r := snap.Rows[name]
		assert.True(t, wd.Work >= 0 && wd.Work <= r.Width && wd.Ahead >= 0 && wd.Ahead <= 4*r.Width, name)
	}
	for q, names := range a.Rows {
		c := snap.Cards[q.Card]
		for _, n := range names {
			r := snap.Rows[n]
			assert.True(t, slices.Contains(r.Tiers, c.Tier) && r.State == "up" && !r.Held, "%v %s", q, n)
		}
	}
}
