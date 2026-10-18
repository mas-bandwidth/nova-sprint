package sprintdash

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The last 24 hours (the owner, 2026-10-18: "I would like it if you could create a stats page
// showing me all the things you can do with https://pele.run for the sprint dashboard. You could
// put these things below the existing stuff we have at the bottom."; and "historical graphs and
// stuff showing what has happened over the last 24 hours for example. esp. throughput per-hour").
//
// History is a pure function of the cards the dashboard already holds (where --json --rows: every
// primary with its fields, its stamps and its consumer records) and a clock: it reads nothing of
// the sprint. It buckets the last 24 hours by UTC hour and writes each chart as Mermaid source,
// which the page hands to Pele (page/pele.js) to draw. Every chart is bounded (top-N lists, capped
// spans), so the answer stays small whatever the sprint's size.

// HistoryHours is how many hourly buckets the history spans, the current hour last.
const HistoryHours = 24

// HistCard is one primary as the history reads it: its place, its stamps and its consumers.
type HistCard struct {
	ID, Stream, Column, Tier, Kind string
	Needs                          []string
	Admitted, DealtAt, TakenAt     time.Time
	Landed                         time.Time
	Spans                          []Span // its consumer records, oldest taken first
}

// Span is one consumer record of a card (sprint.ConsumerRec): a work attempt, a read or a
// landing, who ran it on what route and model, when it was taken and when it ended (zero while
// it is out), and how it ended.
type Span struct {
	Key, Kind, Who, Route, Model, Outcome string
	Taken, Ended                          time.Time
}

// histRow is one row of where --json --rows.
type histRow struct {
	ID     string            `json:"id"`
	Stream string            `json:"stream"`
	Column string            `json:"column"`
	Tier   string            `json:"tier_shown"`
	Fields map[string]string `json:"fields"`
}

// consumerPrefix is the field prefix of a consumer record (sprint.FieldConsumerPrefix).
const consumerPrefix = "consumer:"

// HistCardsOf is where --json --rows's rows as the history reads them; none when rows is no
// list of rows.
func HistCardsOf(rows json.RawMessage) []HistCard {
	var in []histRow
	if len(rows) == 0 || json.Unmarshal(rows, &in) != nil {
		return nil
	}
	out := make([]HistCard, 0, len(in))
	for _, r := range in {
		f := r.Fields
		c := HistCard{ID: r.ID, Stream: r.Stream, Column: r.Column, Tier: r.Tier, Kind: f["kind"],
			Admitted: stampOf(cmp.Or(f["admitted"], f["ready_at"])), DealtAt: stampOf(f["dealt_at"]),
			TakenAt: stampOf(f["taken_at"]), Landed: stampOf(f["landed"])}
		for _, n := range strings.Split(f["needs"], ",") {
			if n = strings.TrimSpace(n); n != "" {
				c.Needs = append(c.Needs, n)
			}
		}
		for k, v := range f {
			if !strings.HasPrefix(k, consumerPrefix) || !strings.HasPrefix(v, "{") {
				continue
			}
			var rec struct {
				Key, Kind, Who, Route, Model, Outcome, Taken, Ended string
			}
			if json.Unmarshal([]byte(v), &rec) != nil {
				continue
			}
			c.Spans = append(c.Spans, Span{Key: rec.Key, Kind: rec.Kind, Who: rec.Who, Route: rec.Route, Model: rec.Model,
				Outcome: rec.Outcome, Taken: stampOf(rec.Taken), Ended: stampOf(rec.Ended)})
		}
		slices.SortFunc(c.Spans, func(a, b Span) int { return cmp.Or(a.Taken.Compare(b.Taken), cmp.Compare(a.Key, b.Key)) })
		out = append(out, c)
	}
	return out
}

// stampOf is a card's stamp; zero when it is none.
func stampOf(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// isWork is whether a card counts as work: a sentinel or a wait card is not.
func (c HistCard) isWork() bool { return c.Kind != "sentinel" && c.Kind != "wait" }

// The outcome classes of a work attempt and of a read.
const (
	outOK     = "ok"
	outFailed = "failed"
	outBroken = "broken"
	outOther  = "other"
)

// workClass is a work attempt's outcome class: ok, failed, or other (returned, redealt, ...).
func workClass(outcome string) string {
	switch strings.ToLower(outcome) {
	case "ok":
		return outOK
	case "failed", "fail", "failure", "error":
		return outFailed
	}
	return outOther
}

// readClass is a read's outcome class: ok, broken, or other.
func readClass(outcome string) string {
	switch strings.ToLower(outcome) {
	case "ok":
		return outOK
	case "broken":
		return outBroken
	}
	return outOther
}

// The page's dark tokens (index.html :root) the charts draw in.
const (
	colGreen  = "#199e70" // --s-landed
	colRed    = "#e05252" // --critical
	colGrey   = "#7d8490" // --s-ready
	colBlue   = "#3d9bff" // --s-working
	colOrange = "#fb8321" // --s-review, the reader orange
	colPurple = "#b48cff" // --series-friends
	colYellow = "#fab219" // --warning
	colPink   = "#d55181" // --s-merging
	colSlate  = "#4a4f58" // --s-waiting
)

// palette is the series colours of a pie or a top-N chart, in order.
var palette = []string{colBlue, colOrange, colGreen, colPurple, colYellow, colPink, colRed, colGrey}

// HistoryView is /api/history's body: the window (its first hour's start and the hours, in
// unix seconds; the page labels the hours in its own time zone), the totals over it, and the
// charts, top to bottom.
type HistoryView struct {
	Start  int64         `json:"start"`
	Hours  int           `json:"hours"`
	At     int64         `json:"at"`
	Totals HistoryTotals `json:"totals"`
	Charts []Chart       `json:"charts"`
}

// HistoryTotals is the window's headline counts.
type HistoryTotals struct {
	Landed   int `json:"landed"`
	Started  int `json:"started"`
	Finished int `json:"finished"`
	OK       int `json:"ok"`
	Reads    int `json:"reads"`
	Broken   int `json:"broken"`
	Workers  int `json:"workers"`
}

// Chart is one chart: its id, title and one sentence saying what it counts, its Mermaid source
// and legend, or, with nothing to draw, Empty (the line shown in its place). HourAxis marks an
// xychart whose x-axis is the window's hours, which the page relabels in local time.
type Chart struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Says     string       `json:"says"`
	Mermaid  string       `json:"mermaid,omitempty"`
	Legend   []LegendItem `json:"legend,omitempty"`
	Empty    string       `json:"empty,omitempty"`
	HourAxis bool         `json:"hourAxis,omitempty"`
	Wide     bool         `json:"wide,omitempty"`
}

// LegendItem is one series of a chart: its label and colour, in the order the series are drawn
// (the page sets them as the chart's --pele-series-N).
type LegendItem struct {
	Label string `json:"label"`
	Color string `json:"color"`
}

// nothing24h is an empty chart's line.
const nothing24h = "nothing in the last 24h"

// window is the history's hours: [start, start+HistoryHours h), the current hour last.
type window struct{ start, end, now time.Time }

func windowAt(now time.Time) window {
	now = now.UTC()
	end := now.Truncate(time.Hour).Add(time.Hour)
	return window{start: end.Add(-HistoryHours * time.Hour), end: end, now: now}
}

// bucket is t's hour in the window, -1 outside it (or zero, or after now).
func (w window) bucket(t time.Time) int {
	if t.IsZero() || t.Before(w.start) || !t.Before(w.end) || t.After(w.now) {
		return -1
	}
	return int(t.Sub(w.start) / time.Hour)
}

// in is whether t is in the window.
func (w window) in(t time.Time) bool { return w.bucket(t) >= 0 }

// labels is the window's hours as the UTC hour, two digits.
func (w window) labels() []string {
	out := make([]string, HistoryHours)
	for i := range out {
		out[i] = fmt.Sprintf("%02d", w.start.Add(time.Duration(i)*time.Hour).Hour())
	}
	return out
}

// History is the last 24 hours of cards at now (above).
func History(cards []HistCard, now time.Time) HistoryView {
	w := windowAt(now)
	v := HistoryView{Start: w.start.Unix(), Hours: HistoryHours, At: w.now.Unix()}
	labels := w.labels()

	landed := make([]float64, HistoryHours)
	rolling := make([]float64, HistoryHours)
	started := make([]float64, HistoryHours)
	finished := make([]float64, HistoryHours)
	work := map[string][]float64{outOK: make([]float64, HistoryHours), outFailed: make([]float64, HistoryHours), outOther: make([]float64, HistoryHours)}
	reads := map[string][]float64{outOK: make([]float64, HistoryHours), outBroken: make([]float64, HistoryHours), outOther: make([]float64, HistoryHours)}
	toLand := make([][]float64, HistoryHours)
	wait := make([][]float64, HistoryHours)
	byWho := map[string]*[2]int{}
	byRoute, byModel, byTier, byStream := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	workers := map[string]bool{}

	for _, c := range cards {
		if c.isWork() && !c.Landed.IsZero() {
			if b := w.bucket(c.Landed); b >= 0 {
				landed[b]++
				v.Totals.Landed++
				byStream[c.Stream]++
				if !c.Admitted.IsZero() && c.Landed.After(c.Admitted) {
					toLand[b] = append(toLand[b], c.Landed.Sub(c.Admitted).Hours())
				}
			}
			// the rolling mean: the landings of the 24 hours ending at each hour's end
			for i := range rolling {
				hourEnd := w.start.Add(time.Duration(i+1) * time.Hour)
				if c.Landed.Before(hourEnd) && !c.Landed.Before(hourEnd.Add(-24*time.Hour)) && !c.Landed.After(w.now) {
					rolling[i]++
				}
			}
		}
		if b := w.bucket(c.TakenAt); b >= 0 && !c.DealtAt.IsZero() && !c.TakenAt.Before(c.DealtAt) {
			wait[b] = append(wait[b], c.TakenAt.Sub(c.DealtAt).Minutes())
		}
		for _, sp := range c.Spans {
			switch sp.Kind {
			case "work":
				if b := w.bucket(sp.Taken); b >= 0 {
					started[b]++
					v.Totals.Started++
				}
				b := w.bucket(sp.Ended)
				if b < 0 {
					if sp.Ended.IsZero() && w.in(sp.Taken) && sp.Who != "" {
						workers[sp.Who] = true
					}
					continue
				}
				class := workClass(sp.Outcome)
				finished[b]++
				work[class][b]++
				v.Totals.Finished++
				who := cmp.Or(sp.Who, "unknown")
				workers[who] = true
				if byWho[who] == nil {
					byWho[who] = &[2]int{}
				}
				if class == outOK {
					v.Totals.OK++
					byWho[who][0]++
				} else {
					byWho[who][1]++
				}
				byRoute[cmp.Or(sp.Route, "none")]++
				byModel[cmp.Or(sp.Model, "none")]++
				byTier[cmp.Or(c.Tier, "none")]++
			case "read":
				if b := w.bucket(sp.Ended); b >= 0 {
					class := readClass(sp.Outcome)
					reads[class][b]++
					v.Totals.Reads++
					if class == outBroken {
						v.Totals.Broken++
					}
					if sp.Who != "" {
						workers[sp.Who] = true
					}
				}
			}
		}
	}
	for i := range rolling {
		rolling[i] = round1(rolling[i] / 24)
	}
	v.Totals.Workers = len(workers)

	var ch []Chart
	ch = append(ch, hourChart("landings", "Landings per hour",
		"Cards landed in each hour (bars), and the mean per hour over the 24 hours ending at that hour (line).",
		labels, "cards", v.Totals.Landed == 0,
		xySeries{"landed", colGreen, false, landed}, xySeries{"24h mean per hour", colYellow, true, rolling}))
	ch = append(ch, hourChart("started-finished", "Work started and finished per hour",
		"Work attempts taken by a worker in each hour, beside the attempts that ended in it, whatever their outcome.",
		labels, "attempts", v.Totals.Started+v.Totals.Finished == 0,
		xySeries{"started", colBlue, false, started}, xySeries{"finished", colGreen, false, finished}))
	ch = append(ch, hourChart("work-outcomes", "Work finishes by outcome",
		"Work attempts that ended in each hour: ok, failed, and other endings (returned, redealt, retired).",
		labels, "attempts", v.Totals.Finished == 0,
		xySeries{"ok", colGreen, false, work[outOK]}, xySeries{"failed", colRed, false, work[outFailed]}, xySeries{"other", colGrey, false, work[outOther]}))
	ch = append(ch, hourChart("reads", "Reads per hour",
		"Reads that ended in each hour: ok, broken, and other endings.",
		labels, "reads", v.Totals.Reads == 0,
		xySeries{"ok", colGreen, false, reads[outOK]}, xySeries{"broken", colRed, false, reads[outBroken]}, xySeries{"other", colGrey, false, reads[outOther]}))
	ch = append(ch, rateChart(labels, finished, work[outFailed], reads))
	med, p90 := make([]float64, HistoryHours), make([]float64, HistoryHours)
	for i, xs := range toLand {
		med[i], p90[i] = round1(quantile(xs, 0.5)), round1(quantile(xs, 0.9))
	}
	ch = append(ch, hourChart("time-to-land", "Time to land",
		"For the cards landed in each hour: the median and the p90 of the hours from admitted to landed.",
		labels, "hours", len(slices.Concat(toLand...)) == 0,
		xySeries{"median", colGreen, true, med}, xySeries{"p90", colOrange, true, p90}))
	wmed, wp90 := make([]float64, HistoryHours), make([]float64, HistoryHours)
	for i, xs := range wait {
		wmed[i], wp90[i] = round1(quantile(xs, 0.5)), round1(quantile(xs, 0.9))
	}
	ch = append(ch, hourChart("queue-wait", "Queue wait",
		"For the cards taken in each hour (their last attempt): the median and the p90 of the minutes from dealt to taken.",
		labels, "minutes", len(slices.Concat(wait...)) == 0,
		xySeries{"median", colBlue, true, wmed}, xySeries{"p90", colPurple, true, wp90}))
	ch = append(ch, whoChart(byWho))
	ch = append(ch, pieChart("routes", "Routes", "Work attempts that ended in the last 24 hours, by the route they ran on.", byRoute))
	ch = append(ch, pieChart("models", "Models", "Work attempts that ended in the last 24 hours, by the model that ran them.", byModel))
	ch = append(ch, pieChart("tiers", "Tiers", "Work attempts that ended in the last 24 hours, by their card's tier.", byTier))
	ch = append(ch, topBarChart("streams", "Landings by stream", "Cards landed in the last 24 hours, by work stream (the 15 most).", "cards", byStream, 15))
	ch = append(ch, sankeyChart("flow", "Card flow",
		"Every work attempt taken or ended in the last 24 hours, followed through: its outcome, the read of an ok finish, and the landing or the rework after it.",
		cards, func(sp Span) bool { return w.in(sp.Taken) || w.in(sp.Ended) }, nothing24h))
	ch = append(ch, ganttChart("busy", "Who was busy when",
		"Work (blue) and read (orange) spans in the last 24 hours, by worker: the 8 busiest, each with its 4 latest spans.",
		cards, w.start, w.now, 8, 4, nil))
	v.Charts = ch
	return v
}

// xySeries is one series of an xychart: a bar or a line.
type xySeries struct {
	label  string
	color  string
	line   bool
	values []float64
}

// hourChart is an xychart over the window's hours, Empty when empty.
func hourChart(id, title, says string, labels []string, yTitle string, empty bool, series ...xySeries) Chart {
	c := Chart{ID: id, Title: title, Says: says, HourAxis: true}
	if empty {
		c.Empty = nothing24h
		return c
	}
	c.Mermaid, c.Legend = xyChart(false, labels, yTitle, "", series)
	return c
}

// rateChart is the failure and broken rates per hour, percent.
func rateChart(labels []string, finished, failed []float64, reads map[string][]float64) Chart {
	fail, broken := make([]float64, HistoryHours), make([]float64, HistoryHours)
	any := false
	for i := range fail {
		if finished[i] > 0 {
			fail[i] = round1(100 * failed[i] / finished[i])
			any = true
		}
		if n := reads[outOK][i] + reads[outBroken][i] + reads[outOther][i]; n > 0 {
			broken[i] = round1(100 * reads[outBroken][i] / n)
			any = true
		}
	}
	c := Chart{ID: "rates", Title: "Failure and rework rate", HourAxis: true,
		Says: "Of the work attempts that ended in each hour, the percent that failed; of the reads, the percent broken (each sends its card back for rework)."}
	if !any {
		c.Empty = nothing24h
		return c
	}
	c.Mermaid, c.Legend = xyChart(false, labels, "percent", "0 --> 100", []xySeries{{"work failed %", colRed, true, fail}, {"reads broken %", colOrange, true, broken}})
	return c
}

// whoChart is the work attempts ended in the window per worker, ok and not, the 12 most.
func whoChart(byWho map[string]*[2]int) Chart {
	c := Chart{ID: "who", Title: "Who did the work", Says: "Work attempts that ended in the last 24 hours, by the member or friend that ran them (the 12 most): ok, and every other ending."}
	if len(byWho) == 0 {
		c.Empty = nothing24h
		return c
	}
	names := slices.Collect(maps.Keys(byWho))
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(byWho[b][0]+byWho[b][1], byWho[a][0]+byWho[a][1]), cmp.Compare(a, b))
	})
	names = names[:min(12, len(names))]
	ok, other := make([]float64, len(names)), make([]float64, len(names))
	for i, n := range names {
		ok[i], other[i] = float64(byWho[n][0]), float64(byWho[n][1])
	}
	c.Mermaid, c.Legend = xyChart(true, names, "attempts", "", []xySeries{{"ok", colGreen, false, ok}, {"not ok", colRed, false, other}})
	return c
}

// topBarChart is a horizontal bar of the n largest counts.
func topBarChart(id, title, says, yTitle string, counts map[string]int, n int) Chart {
	c := Chart{ID: id, Title: title, Says: says}
	keys := topKeys(counts, n)
	if len(keys) == 0 {
		c.Empty = nothing24h
		return c
	}
	vals := make([]float64, len(keys))
	for i, k := range keys {
		vals[i] = float64(counts[k])
	}
	c.Mermaid, c.Legend = xyChart(true, keys, yTitle, "", []xySeries{{yTitle, colGreen, false, vals}})
	return c
}

// topKeys is counts' keys, the largest first (then by name), at most n.
func topKeys(counts map[string]int, n int) []string {
	keys := slices.Collect(maps.Keys(counts))
	slices.SortFunc(keys, func(a, b string) int { return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b)) })
	return keys[:min(n, len(keys))]
}

// pieChart is a pie of counts: the 7 largest and the rest as "other".
func pieChart(id, title, says string, counts map[string]int) Chart {
	c := Chart{ID: id, Title: title, Says: says}
	if len(counts) == 0 {
		c.Empty = nothing24h
		return c
	}
	keys := topKeys(counts, len(counts))
	rest := 0
	if len(keys) > 7 {
		for _, k := range keys[7:] {
			rest += counts[k]
		}
		keys = keys[:7]
	}
	var b strings.Builder
	b.WriteString("pie\n")
	for i, k := range keys {
		fmt.Fprintf(&b, "  %s : %d\n", quoteLabel(k), counts[k])
		c.Legend = append(c.Legend, LegendItem{k, palette[i%len(palette)]})
	}
	if rest > 0 {
		fmt.Fprintf(&b, "  %s : %d\n", quoteLabel("other"), rest)
		c.Legend = append(c.Legend, LegendItem{"other", colGrey})
	}
	c.Mermaid = b.String()
	return c
}

// xyChart is an xychart's Mermaid: vertical (or horizontal) over labels, the y-axis titled
// (and ranged when yRange is set), one bar or line per series, drawn in the series' colours.
func xyChart(horizontal bool, labels []string, yTitle, yRange string, series []xySeries) (string, []LegendItem) {
	var b strings.Builder
	// the drawing's natural size: wide and low, a horizontal chart as tall as its rows need
	width, height := 900, 400
	if horizontal {
		height = 100 + 34*len(labels)
	}
	fmt.Fprintf(&b, "---\nconfig:\n  xyChart:\n    width: %d\n    height: %d\n---\n", width, height)
	b.WriteString("xychart")
	if horizontal {
		b.WriteString(" horizontal")
	}
	b.WriteString("\n  x-axis [")
	for i, l := range labels {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quoteLabel(l))
	}
	b.WriteString("]\n  y-axis ")
	b.WriteString(quoteLabel(yTitle))
	if yRange == "" {
		// a small count is drawn on whole steps: an axis of 0 to 5, never 0.2, 0.4, ...
		top := 0.0
		for _, s := range series {
			for _, x := range s.values {
				top = max(top, x)
			}
		}
		if top < 5 {
			yRange = "0 --> 5"
		}
	}
	if yRange != "" {
		b.WriteString(" " + yRange)
	}
	b.WriteString("\n")
	var legend []LegendItem
	for _, s := range series {
		kind := "bar"
		if s.line {
			kind = "line"
		}
		fmt.Fprintf(&b, "  %s [", kind)
		for i, x := range s.values {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(num(x))
		}
		b.WriteString("]\n")
		legend = append(legend, LegendItem{s.label, s.color})
	}
	return b.String(), legend
}

// sankeyChart is the flow of the work attempts include keeps (above, History): attempt ->
// outcome -> read -> landed or reworked; empty is the line shown when no attempt is kept.
func sankeyChart(id, title, says string, cards []HistCard, include func(Span) bool, empty string) Chart {
	c := Chart{ID: id, Title: title, Says: says, Wide: true}
	flows := map[[2]string]int{}
	add := func(from, to string) { flows[[2]string{from, to}]++ }
	const (
		attempts = "Work attempts"
		fOK      = "Finished ok"
		fFailed  = "Failed"
		fOther   = "Other ending"
		fOut     = "Still working"
		rOK      = "Read ok"
		rBroken  = "Read broken"
		rOut     = "Being read"
		rNone    = "Not read yet"
		landed   = "Landed"
		waiting  = "Not landed yet"
		reworked = "Reworked"
		notRe    = "Not reworked yet"
	)
	for _, card := range cards {
		for i, sp := range card.Spans {
			if sp.Kind != "work" || !include(sp) {
				continue
			}
			// later is whether the card has a later work attempt than the one that ended at t
			later := func(t time.Time) bool {
				for _, x := range card.Spans[i+1:] {
					if x.Kind == "work" && !x.Taken.Before(t) {
						return true
					}
				}
				return false
			}
			if sp.Ended.IsZero() {
				add(attempts, fOut)
				continue
			}
			switch workClass(sp.Outcome) {
			case outFailed:
				add(attempts, fFailed)
				add(fFailed, cond(later(sp.Ended), reworked, notRe))
				continue
			case outOther:
				add(attempts, fOther)
				continue
			}
			add(attempts, fOK)
			// its read: the first read taken after it ended and before the next attempt
			var read *Span
			next := nextWork(card.Spans[i+1:], sp.Ended)
			for j := range card.Spans {
				x := &card.Spans[j]
				if x.Kind == "read" && !x.Taken.Before(sp.Ended) && x.Taken.Before(next) {
					read = x
					break
				}
			}
			switch {
			case read == nil:
				if !card.Landed.IsZero() && !card.Landed.Before(sp.Ended) {
					add(fOK, landed) // landed with no read recorded
				} else {
					add(fOK, rNone)
				}
			case read.Ended.IsZero():
				add(fOK, rOut)
			case readClass(read.Outcome) == outOK:
				add(fOK, rOK)
				add(rOK, cond(!card.Landed.IsZero() && !card.Landed.Before(read.Ended), landed, waiting))
			case readClass(read.Outcome) == outBroken:
				add(fOK, rBroken)
				add(rBroken, cond(later(read.Ended), reworked, notRe))
			default:
				add(fOK, rNone)
			}
		}
	}
	if len(flows) == 0 {
		c.Empty = empty
		return c
	}
	order := []string{attempts, fOK, fFailed, fOther, fOut, rOK, rBroken, rOut, rNone, landed, waiting, reworked, notRe}
	rank := map[string]int{}
	for i, n := range order {
		rank[n] = i
	}
	keys := slices.Collect(maps.Keys(flows))
	slices.SortFunc(keys, func(a, b [2]string) int {
		return cmp.Or(cmp.Compare(rank[a[0]], rank[b[0]]), cmp.Compare(rank[a[1]], rank[b[1]]))
	})
	var b strings.Builder
	b.WriteString("---\nconfig:\n  sankey:\n    width: 1200\n    height: 460\n---\nsankey\n\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s,%s,%d\n", sankeyField(k[0]), sankeyField(k[1]), flows[k])
	}
	c.Mermaid = b.String()
	return c
}

// nextWork is the taken time of the first work span of rest taken at or after t; far in the
// future when none is.
func nextWork(rest []Span, t time.Time) time.Time {
	for _, x := range rest {
		if x.Kind == "work" && !x.Taken.Before(t) {
			return x.Taken
		}
	}
	return time.Unix(math.MaxInt32*4, 0)
}

func cond(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

// ganttChart is a gantt (times in unix milliseconds, Pele's dateFormat x) of the work and read spans that overlap [from, now], by worker: the
// maxWho busiest workers, each with its maxEach latest spans; keep, when set, limits the cards.
// A span still out ends at now.
func ganttChart(id, title, says string, cards []HistCard, from, now time.Time, maxWho, maxEach int, keep func(HistCard) bool) Chart {
	c := Chart{ID: id, Title: title, Says: says, Wide: true}
	type task struct {
		label      string
		read       bool
		start, end time.Time
	}
	byWho := map[string][]task{}
	for _, card := range cards {
		if keep != nil && !keep(card) {
			continue
		}
		for _, sp := range card.Spans {
			if (sp.Kind != "work" && sp.Kind != "read") || sp.Taken.IsZero() || sp.Taken.After(now) {
				continue
			}
			end := sp.Ended
			if end.IsZero() || end.After(now) {
				end = now
			}
			if end.Before(from) {
				continue
			}
			start := sp.Taken
			if start.Before(from) {
				start = from
			}
			if !end.After(start) {
				end = start.Add(time.Minute)
			}
			who := cmp.Or(sp.Who, "unknown")
			label := card.ID
			if sp.Kind == "read" {
				label += " read"
			} else if sp.Key != "" {
				label += " " + sp.Key
			}
			byWho[who] = append(byWho[who], task{label, sp.Kind == "read", start, end})
		}
	}
	if len(byWho) == 0 {
		c.Empty = nothing24h
		return c
	}
	counts := map[string]int{}
	for k, v := range byWho {
		counts[k] = len(v)
	}
	whos := topKeys(counts, maxWho)
	slices.Sort(whos)
	var b strings.Builder
	b.WriteString("gantt\n  dateFormat x\n  axisFormat %H:%M\n")
	n := 0
	for _, who := range whos {
		ts := byWho[who]
		slices.SortFunc(ts, func(a, b task) int { return cmp.Or(b.start.Compare(a.start), cmp.Compare(a.label, b.label)) })
		ts = ts[:min(maxEach, len(ts))]
		slices.SortFunc(ts, func(a, b task) int { return cmp.Or(a.start.Compare(b.start), cmp.Compare(a.label, b.label)) })
		fmt.Fprintf(&b, "  section %s\n", ganttText(who))
		for _, t := range ts {
			tag := ""
			if t.read {
				tag = "done, " // a read is drawn as a done task: the page colours those its read orange
			}
			fmt.Fprintf(&b, "  %s : %ss%d, %d, %d\n", ganttText(t.label), tag, n, t.start.UnixMilli(), t.end.UnixMilli())
			n++
		}
	}
	c.Mermaid = b.String()
	c.Legend = []LegendItem{{"work", colBlue}, {"read", colOrange}}
	return c
}

// quantile is the q-quantile of xs (nearest rank), 0 for none.
func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	i := int(math.Ceil(q*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}

// num is a chart value: one decimal at most, no trailing zero.
func num(f float64) string {
	return strconv.FormatFloat(round1(f), 'f', -1, 64)
}

// quoteLabel is a label as a Mermaid double-quoted string: a double quote becomes a single
// one, and line breaks and control characters become spaces.
func quoteLabel(s string) string {
	return `"` + strings.ReplaceAll(cleanText(s), `"`, "'") + `"`
}

// cleanText is s with every control character a space.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// sankeyField is a Mermaid sankey CSV field: quoted, its quotes doubled, when it holds a comma
// or a quote.
func sankeyField(s string) string {
	s = cleanText(s)
	if strings.ContainsAny(s, `,"`) {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// ganttText is a gantt task's or section's text: the characters a task line reads as syntax
// (colon, hash, semicolon) become spaces.
func ganttText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == ':' || r == '#' || r == ';' || r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s))
}
