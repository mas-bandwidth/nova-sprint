package sprintdash

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The stream view (the owner, 2026-10-18: "Maybe we can click on a work stream by name, and this
// takes you to a page showing all details for that work stream, including all its cards, sankey
// for all cards if applicable, dependencies between cards etc etc etc."): a pure function of the
// cards the dashboard holds, the page's copy (the stream's row, cost and state) and a clock. It
// is computed for one stream when it is asked for (memoized a minute, serve.go's chartMemo),
// never for every stream at each read.

// StreamView is /api/stream/<name>'s body.
type StreamView struct {
	Stream  string       `json:"stream"`
	Found   bool         `json:"found"`
	Display string       `json:"display,omitempty"`
	State   string       `json:"state,omitempty"`
	Since   string       `json:"since,omitempty"`
	At      int64        `json:"at"`
	Start   int64        `json:"start"`
	Hours   int          `json:"hours"`
	Counts  []Fact       `json:"counts"`
	Facts   []Fact       `json:"facts"`
	Cards   []StreamCard `json:"cards"`
	// CardsTotal is the stream's cards; Cards holds at most maxCardRows of them, in flow order.
	CardsTotal int     `json:"cardsTotal"`
	Charts     []Chart `json:"charts"`
}

// Fact is one labelled value of the stream view.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// StreamCard is one card of the stream's table: its place, tier, attempts (work spans), reworks
// (attempts after the first), reads, who ran its latest attempt, how its latest consumer ended,
// its needs, and its admitted and landed times (unix seconds, 0 for none).
type StreamCard struct {
	ID       string   `json:"id"`
	Column   string   `json:"column"`
	Tier     string   `json:"tier,omitempty"`
	Attempts int      `json:"attempts"`
	Reworks  int      `json:"reworks"`
	Reads    int      `json:"reads"`
	Who      string   `json:"who,omitempty"`
	Last     string   `json:"last,omitempty"`
	Needs    []string `json:"needs,omitempty"`
	Admitted int64    `json:"admitted,omitempty"`
	Landed   int64    `json:"landed,omitempty"`
}

// flowColumns is the work table's columns in flow order (sprint.States, with fix).
var flowColumns = []string{"waiting", "ready", "working", "review", "fix", "merging", "landed"}

// columnColor is each column's colour on the page (index.html, --s-*).
var columnColor = map[string]string{"waiting": colSlate, "ready": colGrey, "working": colBlue, "review": colOrange, "fix": "#8b5cf6", "merging": colPink, "landed": colGreen}

// maxDepNodes caps the dependency chart's boxes (the stream's cards and the cards they need),
// maxDepEdges its arrows, and maxCardRows the stream page's card table.
const (
	maxDepNodes = 150
	maxDepEdges = 400
	maxCardRows = 500
)

// streamPage is what the stream view reads of the page's copy.
type streamPage struct {
	Tables      map[string]map[string]map[string]any `json:"tables"`
	StreamCosts map[string]map[string]any            `json:"stream_costs"`
	Streams     []struct {
		Stream, Display, State, Since string
	} `json:"streams"`
}

// StreamViewOf is the view of stream at now over cards (every stream's: a need may name another
// stream's card) and the page's copy page.
func StreamViewOf(cards []HistCard, page json.RawMessage, stream string, now time.Time) StreamView {
	w := windowAt(now)
	v := StreamView{Stream: stream, At: w.now.Unix(), Start: w.start.Unix(), Hours: HistoryHours, Counts: []Fact{}, Facts: []Fact{}, Cards: []StreamCard{}}
	var p streamPage
	_ = json.Unmarshal(page, &p) // ignored: a copy of another shape has no facts
	for _, s := range p.Streams {
		if s.Stream == stream {
			v.Found, v.Display, v.State, v.Since = true, s.Display, s.State, s.Since
		}
	}
	// the page's copy has each stream_costs record folded into its work row already: a cost cell
	// the work row shows with the same value is not shown again
	shown := map[Fact]bool{}
	if row, ok := p.Tables["work"][stream]; ok {
		v.Found = true
		for _, f := range scalarFacts("", row) {
			shown[f] = true
			v.Facts = append(v.Facts, f)
		}
	}
	if row, ok := p.Tables["merge"][stream]; ok {
		v.Facts = append(v.Facts, scalarFacts("merge ", row)...)
	}
	if row, ok := p.StreamCosts[stream]; ok {
		for _, f := range scalarFacts("", row) {
			if !shown[f] {
				v.Facts = append(v.Facts, Fact{"cost " + f.Label, f.Value})
			}
		}
	}

	var mine []HistCard
	byID := map[string]HistCard{}
	for _, c := range cards {
		byID[c.ID] = c
		if c.Stream == stream {
			mine = append(mine, c)
		}
	}
	if len(mine) > 0 {
		v.Found = true
	}
	counts := map[string]int{}
	for _, c := range mine {
		counts[c.Column]++
		sc := StreamCard{ID: c.ID, Column: c.Column, Tier: c.Tier, Needs: c.Needs, Admitted: unixOr0(c.Admitted), Landed: unixOr0(c.Landed)}
		for _, sp := range c.Spans {
			switch sp.Kind {
			case "work":
				sc.Attempts++
				sc.Who = sp.Who
			case "read":
				sc.Reads++
			}
			if sp.Kind == "work" || sp.Kind == "read" || sp.Kind == "land" {
				sc.Last = strings.TrimSpace(sp.Kind + " " + cmp.Or(sp.Outcome, cond(sp.Ended.IsZero(), "out", "")))
			}
		}
		sc.Reworks = max(0, sc.Attempts-1)
		v.Cards = append(v.Cards, sc)
	}
	order := map[string]int{}
	for i, col := range flowColumns {
		order[col] = i
	}
	slices.SortStableFunc(v.Cards, func(a, b StreamCard) int {
		return cmp.Or(cmp.Compare(rankOf(order, a.Column), rankOf(order, b.Column)), cmp.Compare(a.ID, b.ID))
	})
	v.CardsTotal = len(v.Cards)
	v.Cards = v.Cards[:min(len(v.Cards), maxCardRows)]
	cols := slices.Collect(maps.Keys(counts))
	slices.SortFunc(cols, func(a, b string) int {
		return cmp.Or(cmp.Compare(rankOf(order, a), rankOf(order, b)), cmp.Compare(a, b))
	})
	for _, col := range cols {
		v.Counts = append(v.Counts, Fact{col, strconv.Itoa(counts[col])})
	}
	v.Counts = append(v.Counts, Fact{"cards", strconv.Itoa(len(mine))})

	none := "no cards in this stream"
	landed := make([]float64, HistoryHours)
	n := 0
	for _, c := range mine {
		if b := w.bucket(c.Landed); b >= 0 && c.isWork() {
			landed[b]++
			n++
		}
	}
	v.Charts = append(v.Charts, depsChart(mine, byID))
	v.Charts = append(v.Charts, sankeyChart("flow", "Card flow",
		"Every work attempt of the stream's cards, followed through: its outcome, the read of an ok finish, and the landing or the rework after it.",
		mine, func(Span) bool { return true }, "no work attempt recorded on this stream's cards"))
	v.Charts = append(v.Charts, hourChart("landings", "Landings per hour", "The stream's cards landed in each hour of the last 24.",
		w.labels(), "cards", n == 0, xySeries{"landed", colGreen, false, landed}))
	g := ganttChart("busy", "Work and read spans",
		"The stream's work (blue) and read (orange) spans in the last 24 hours, by worker: the 12 busiest, each with its 8 latest spans.",
		mine, w.start, w.now, 12, 8, nil)
	v.Charts = append(v.Charts, g)
	if len(mine) == 0 {
		for i := range v.Charts {
			v.Charts[i].Mermaid, v.Charts[i].Legend, v.Charts[i].Empty = "", nil, none
		}
	}
	return v
}

// rankOf is a column's place in the flow, after every flow column when it is none of them.
func rankOf(order map[string]int, col string) int {
	if i, ok := order[col]; ok {
		return i
	}
	return len(order)
}

func unixOr0(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// scalarFacts is a row's scalar cells as facts, by key, each label prefixed.
func scalarFacts(prefix string, row map[string]any) []Fact {
	var out []Fact
	for _, k := range slices.Sorted(maps.Keys(row)) {
		switch x := row[k].(type) {
		case string:
			if x != "" {
				out = append(out, Fact{prefix + k, x})
			}
		case float64:
			out = append(out, Fact{prefix + k, strconv.FormatFloat(x, 'f', -1, 64)})
		case bool:
			out = append(out, Fact{prefix + k, strconv.FormatBool(x)})
		}
	}
	return out
}

// depsChart is the flowchart of the stream's cards and their needs, left to right, each card
// classed by its column; a need on a card of another stream (or one not on the table) is drawn
// dashed. Past maxDepNodes cards, the landed cards no open card needs are left out.
func depsChart(mine []HistCard, byID map[string]HistCard) Chart {
	c := Chart{ID: "deps", Title: "Dependencies", Wide: true,
		Says: "Each card of the stream, coloured by its column, with an arrow from each card it needs; a need on another stream's card is dashed."}
	edges := 0
	for _, x := range mine {
		edges += len(x.Needs)
	}
	if len(mine) == 0 {
		c.Empty = "no cards in this stream"
		return c
	}
	if edges == 0 {
		c.Empty = "no card of this stream needs another"
		return c
	}
	keep := mine
	if len(keep) > maxDepNodes {
		needed := map[string]bool{}
		for _, x := range mine {
			if x.Column != "landed" {
				for _, n := range x.Needs {
					needed[n] = true
				}
			}
		}
		keep = slices.DeleteFunc(slices.Clone(mine), func(x HistCard) bool { return x.Column == "landed" && !needed[x.ID] })
		if len(keep) > maxDepNodes {
			keep = keep[:maxDepNodes]
		}
		c.Says += fmt.Sprintf(" The stream has %d cards: %d are drawn, the landed cards no open card needs left out.", len(mine), len(keep))
	}
	// a card that needs none and that none needs is counted, not drawn: alone, it says nothing
	// about the order, and a stream of hundreds would be a column of boxes
	needed := map[string]bool{}
	for _, x := range keep {
		for _, n := range x.Needs {
			needed[n] = true
		}
	}
	alone := 0
	keep = slices.DeleteFunc(slices.Clone(keep), func(x HistCard) bool {
		if len(x.Needs) == 0 && !needed[x.ID] {
			alone++
			return true
		}
		return false
	})
	if alone > 0 {
		c.Says += fmt.Sprintf(" %d %s with no need either way %s not drawn.", alone, cond(alone == 1, "card", "cards"), cond(alone == 1, "is", "are"))
	}
	ids := map[string]string{}
	node := func(id string) string {
		if n, ok := ids[id]; ok {
			return n
		}
		n := "n" + strconv.Itoa(len(ids))
		ids[id] = n
		return n
	}
	in := map[string]bool{}
	for _, x := range keep {
		in[x.ID] = true
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	used := map[string]bool{}
	for _, x := range keep {
		col := cmp.Or(x.Column, "none")
		if columnColor[col] == "" {
			col = "other"
		}
		used[col] = true
		fmt.Fprintf(&b, "  %s%s:::%s\n", node(x.ID), flowLabel(x.ID), col)
	}
	// every node (the stream's cards and the other streams' cards they need) and every edge is
	// capped: a need whose node would pass maxDepNodes, or an edge past maxDepEdges, is left out
	// and counted
	edgesDrawn, left := 0, 0
	for _, x := range keep {
		for _, n := range x.Needs {
			if edgesDrawn >= maxDepEdges {
				left++
				continue
			}
			if in[n] {
				fmt.Fprintf(&b, "  %s --> %s\n", node(n), node(x.ID))
				edgesDrawn++
				continue
			}
			if _, drawn := ids[n]; !drawn {
				if len(ids) >= maxDepNodes {
					left++
					continue
				}
				label := n
				if o, ok := byID[n]; ok && o.Stream != "" {
					label = n + " (" + o.Stream + ")"
				}
				fmt.Fprintf(&b, "  %s%s:::ext\n", node(n), flowLabel(label))
			}
			fmt.Fprintf(&b, "  %s -.-> %s\n", node(n), node(x.ID))
			edgesDrawn++
		}
	}
	if left > 0 {
		c.Says += fmt.Sprintf(" %d %s past the chart's bounds (%d boxes, %d arrows) %s not drawn.", left, cond(left == 1, "need", "needs"), maxDepNodes, maxDepEdges, cond(left == 1, "is", "are"))
	}
	for _, col := range flowColumns {
		if used[col] {
			text := "#0d0f12"
			if col == "waiting" {
				text = "#eef0f3"
			}
			fmt.Fprintf(&b, "  classDef %s fill:%s,stroke:%s,color:%s\n", col, columnColor[col], columnColor[col], text)
			c.Legend = append(c.Legend, LegendItem{col, columnColor[col]})
		}
	}
	if used["other"] || used["none"] {
		fmt.Fprintf(&b, "  classDef other fill:%s,stroke:%s,color:#eef0f3\n  classDef none fill:%s,stroke:%s,color:#eef0f3\n", colSlate, colSlate, colSlate, colSlate)
	}
	b.WriteString("  classDef ext fill:none,stroke:#6b727d,stroke-dasharray:3 3,color:#a4aab4\n")
	c.Mermaid = b.String()
	return c
}

// flowLabel is a flowchart node's label: a double-quoted string, its quotes and angle brackets
// written as Mermaid's entity codes.
func flowLabel(s string) string {
	s = cleanText(s)
	s = strings.NewReplacer(`"`, "#quot;", "<", "#lt;", ">", "#gt;").Replace(s)
	return `["` + s + `"]`
}
