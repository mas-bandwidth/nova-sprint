package sprintdash

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specPath is the page's specification, read by the drift check below.
const specPath = "../../docs/SPEC-SPRINT-DASHBOARD.md"

// node is one element of the page: its name, attributes, children and own text.
type node struct {
	name     string
	attr     map[string]string
	children []*node
	text     string // the text directly inside it, in order
	parts    []any  // children and text runs in document order, for textOf
}

// parsePage reads the page's markup into a tree with the standard library's XML reader in
// its forgiving mode: HTML's void elements close themselves and its entities are known.
func parsePage(t *testing.T, html []byte) *node {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(html))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	root := &node{name: "#root"}
	stack := []*node{root}
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err, "the page's markup does not parse")
		top := stack[len(stack)-1]
		switch tk := tok.(type) {
		case xml.StartElement:
			n := &node{name: strings.ToLower(tk.Name.Local), attr: map[string]string{}}
			for _, a := range tk.Attr {
				n.attr[strings.ToLower(a.Name.Local)] = a.Value
			}
			top.children = append(top.children, n)
			top.parts = append(top.parts, n)
			stack = append(stack, n)
		case xml.EndElement:
			name := strings.ToLower(tk.Name.Local)
			for i := len(stack) - 1; i > 0; i-- {
				if stack[i].name == name {
					stack = stack[:i]
					break
				}
			}
		case xml.CharData:
			top.text += string(tk)
			top.parts = append(top.parts, string(tk))
		}
	}
	return root
}

func (n *node) classes() []string { return strings.Fields(n.attr["class"]) }

func (n *node) has(class string) bool { return slices.Contains(n.classes(), class) }

// find is every element under n (n excepted) that ok accepts, in document order.
func (n *node) find(ok func(*node) bool) []*node {
	var out []*node
	for _, c := range n.children {
		if ok(c) {
			out = append(out, c)
		}
		out = append(out, c.find(ok)...)
	}
	return out
}

func (n *node) one(t *testing.T, what string, ok func(*node) bool) *node {
	t.Helper()
	all := n.find(ok)
	require.Len(t, all, 1, "the page has %d of %s, wants one", len(all), what)
	return all[0]
}

func byID(id string) func(*node) bool { return func(n *node) bool { return n.attr["id"] == id } }

func byClass(c string) func(*node) bool { return func(n *node) bool { return n.has(c) } }

// textOf is the text n shows, whitespace folded; an element of class alt (shown only in
// the narrow layout) is left out.
func textOf(n *node) string {
	var b strings.Builder
	var walk func(*node)
	walk = func(n *node) {
		for _, p := range n.parts {
			switch v := p.(type) {
			case string:
				b.WriteString(v)
			case *node:
				if !v.has("alt") {
					walk(v)
				}
			}
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// spec is the specification's sections by name (a heading's words before " (" or ":"),
// in order, and the body of each, its heading line included.
type spec struct {
	order []string
	body  map[string]string
}

func readSpec(t *testing.T) spec {
	t.Helper()
	b, err := os.ReadFile(specPath)
	require.NoError(t, err)
	s := spec{body: map[string]string{}}
	cur := ""
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			cur = rest
			if i := strings.IndexAny(cur, "(:"); i >= 0 {
				cur = cur[:i]
			}
			cur = strings.TrimSpace(cur)
			s.order = append(s.order, cur)
		}
		if cur != "" {
			s.body[cur] += line + "\n"
		}
	}
	return s
}

func (s spec) section(t *testing.T, name string) string {
	t.Helper()
	b, ok := s.body[name]
	require.True(t, ok, "%s has no section %q", specPath, name)
	return b
}

// match is the first submatch of re in text, which must match.
func match(t *testing.T, re, text, what string) string {
	t.Helper()
	m := regexp.MustCompile(re).FindStringSubmatch(text)
	require.NotNil(t, m, "%s: the spec does not say %s (%s)", specPath, what, re)
	return m[1]
}

func splitList(s, sep string) []string {
	var out []string
	for _, w := range strings.Split(s, sep) {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// counted is the numeric rule of Work, Fleet and Friends (docs/SPEC-SPRINT-DASHBOARD.md,
// Page: "All numbers: monospace (ui-monospace, Menlo), right-aligned"): the row's name, a
// status pill and a fraction column ("n / total", "n / width") are no number, and every
// other column is one.
func counted(cols []string, fractions map[string]bool) func(string) bool {
	return func(name string) bool { return name != cols[0] && name != "status" && !fractions[name] }
}

// ruleSections are the spec's sections that are rules over the page or notes on serving
// it; every other section is a part of the page, in order from the top. A new section is
// one of the two, or the test below is red until it is classified here.
var ruleSections = []string{"Serving and publishing", "The specification", "Page", "Responsive", "LOCKED", "Column alignment across the three tables", "LOCK 2", "Live deployment contract", "Freshness"}

// TestDashboardPageIsTheSpec is the drift check (docs/SPEC-SPRINT-DASHBOARD.md): the
// page's markup says what the specification says, string for string. A change to the
// page without the spec, or to the spec without the page, is red here.
func TestDashboardPageIsTheSpec(t *testing.T) {
	t.Parallel()
	sp := readSpec(t)
	doc := parsePage(t, file("index.html"))
	body := doc.one(t, "a body", func(n *node) bool { return n.name == "body" })

	// The panels, top to bottom: the spec's view sections in order are the page's.
	var want []string
	for _, name := range sp.order {
		if !slices.Contains(ruleSections, name) {
			want = append(want, name)
		}
	}
	var got []string
	for _, c := range body.children {
		_, hidden := c.attr["hidden"]
		switch {
		case hidden || c.name == "script":
		case c.name == "header":
			got = append(got, "Header")
		case c.has("hero"):
			got = append(got, "Hero row")
		case c.has("progress-card"):
			got = append(got, "Progress bar")
		case c.has("spend"):
			spendTitle := textOf(c.one(t, "a spend panel title", func(n *node) bool { return n.name == "h2" }))
			assert.Equal(t, "Cost breakdown", spendTitle, "spend panel title must be 'Cost breakdown'")
		case c.has("panel"):
			got = append(got, textOf(c.one(t, "a panel title", func(n *node) bool { return n.name == "h2" })))
		case c.name == "footer":
			got = append(got, "Footer")
		default:
			got = append(got, "<"+c.name+" class="+c.attr["class"]+">")
		}
	}
	assert.Equal(t, want, got, "the page's parts, top to bottom, are not the spec's sections in order")

	// Each panel's title is exactly the one the spec gives.
	for _, name := range []string{"Work", "Fleet", "Friends"} {
		title := match(t, `^## [^\n]*?title (?:exactly )?"([^"]+)"`, sp.section(t, name), "the "+name+" panel's title")
		assert.Contains(t, got, title, "the %s panel's title %q is no panel's on the page", name, title)
	}

	// The readers and merge panel is there only for ?all=1: hidden by default.
	assert.Contains(t, sp.section(t, "Page"), "No readers or merge panel (available at ?all=1 only)")
	_, hidden := body.one(t, "a readers panel", byID("readers-panel")).attr["hidden"]
	assert.True(t, hidden, "the readers and merge panel shows by default; the spec has it at ?all=1 only")

	work := splitList(match(t, `(?m)^- Columns: ([^(\n]+)\(headers exactly so`, sp.section(t, "Work"), "Work's columns"), "|")
	fleet := splitList(match(t, `(?m)^- Columns: ([^(\n]+)\(headers exactly so`, sp.section(t, "Fleet"), "Fleet's columns"), "|")
	// The owner's live page keeps the same columns as Fleet, with a friend name.
	friendsSec := sp.section(t, "Friends")
	assert.Contains(t, friendsSec, "same eight-column shape as the live Fleet table, including load")
	friends := append([]string{"friend"}, slices.Clone(fleet[1:])...)
	// A fraction column ("n / total", "n / width") and the status pill are not numbers;
	// the first column is the row's name; every other column is a number, right-aligned.
	fractionsOf := func(section string) map[string]bool {
		out := map[string]bool{}
		for _, m := range regexp.MustCompile(`(?m)^- (\w+)(?: as "n / total"|: a cell track[^"]*"n / width")`).FindAllStringSubmatch(sp.section(t, section), -1) {
			out[strings.ToLower(m[1])] = true
		}
		return out
	}
	workFractions, fleetFractions := fractionsOf("Work"), fractionsOf("Fleet")
	assert.Equal(t, map[string]bool{"landed": true}, workFractions, "Work's fraction columns in the spec")
	assert.Equal(t, map[string]bool{"working": true}, fleetFractions, "Fleet's fraction columns in the spec")
	for _, tc := range []struct {
		id      string
		cols    []string
		numeric func(name string) bool
	}{
		{"streams", work, counted(work, workFractions)},
		{"fleet", fleet, counted(fleet, fleetFractions)},
		{"friends", friends, counted(friends, fleetFractions)},
	} {
		head := body.one(t, "#"+tc.id, byID(tc.id)).one(t, "a header row in #"+tc.id, byClass("head"))
		var names []string
		for _, cell := range head.children {
			name := textOf(cell)
			if name == "" {
				continue // the fleet's figure column: its header is the narrow layout's alone
			}
			names = append(names, name)
			assert.Equal(t, tc.numeric(name), cell.has("num"), "#%s column %q: numeric (right-aligned) is %v in the spec", tc.id, name, tc.numeric(name))
			if name == "landed" {
				assert.True(t, cell.has("frac"), "#%s column landed is the n / total figure, class frac", tc.id)
			}
		}
		assert.Equal(t, tc.cols, names, "#%s's column headers, in order, are not the spec's", tc.id)
	}

	// The hero row: its tiles and their labels, in order, and the one fixed sub-line.
	hero := sp.section(t, "Hero row")
	var labels []string
	for _, m := range regexp.MustCompile(`(?m)^\d+\. ([A-Z][A-Z ]*):`).FindAllStringSubmatch(hero, -1) {
		labels = append(labels, m[1])
	}
	assert.Contains(t, hero, "## Hero row: five tiles")
	assert.Len(t, labels, 5, "the spec's hero tiles")
	tiles := body.one(t, "a hero row", byClass("hero")).find(byClass("tile"))
	var pageLabels []string
	for _, tile := range tiles {
		pageLabels = append(pageLabels, strings.ToUpper(textOf(tile.one(t, "a tile label", byClass("label")))))
	}
	assert.Equal(t, labels, pageLabels, "the hero tiles' labels (shown in capitals), in order")
	tput := match(t, `(?m)^\d+\. THROUGHPUT:[^\n]*sub-line "([^"]+)"`, hero, "the throughput's sub-line")
	if assert.Len(t, tiles, 5) {
		assert.Equal(t, tput, textOf(tiles[4].one(t, "the throughput's sub-line", byClass("sub"))))
	}

	// The progress bar: its label, and the legend's states in order (app.js draws it).
	bar := sp.section(t, "Progress bar")
	label := match(t, `^## Progress bar: "([^"]+)"`, bar, "the progress bar's label")
	assert.Equal(t, label, strings.ToUpper(textOf(body.one(t, "a progress card", byClass("progress-card")).one(t, "its label", byClass("label")))))
	legend := splitList(match(t, `with the legend \(([^)]+)\)`, bar, "the legend"), ",")
	states := match(t, `(?m)^var STATES = \[([^\]]+)\];`, string(file("app.js")), "app.js's STATES")
	assert.Equal(t, legend, splitList(strings.ReplaceAll(states, `"`, ""), ","), "the legend's states in app.js")

	// The header: the wordmark and the pills, in order.
	header := sp.section(t, "Header")
	word := match(t, `The word "([^"]+)"`, header, "the wordmark")
	assert.Equal(t, word, textOf(body.one(t, "a wordmark", byClass("wordmark"))))
	var pills []string
	for _, p := range splitList(match(t, `(?m)^- Pills: ([^;]+);`, header, "the pills"), ",") {
		pills = append(pills, strings.Fields(p)[0])
	}
	var chips []string
	for _, c := range body.find(byClass("chip")) {
		chips = append(chips, strings.Fields(textOf(c))[0])
	}
	assert.Equal(t, pills, chips, "the header's pills, in order")

	// The live page's single repository link is also the complete footer label.
	foot := sp.section(t, "Footer")
	url := match(t, `^## Footer: one link, "([^"]+)"`, foot, "the footer's link")
	footer := body.one(t, "a footer", func(n *node) bool { return n.name == "footer" })
	link := footer.one(t, "the footer's link", func(n *node) bool { return n.name == "a" })
	assert.Equal(t, url, textOf(footer))
	assert.Equal(t, url, link.attr["href"])

}

// The page loads nothing from anywhere else: its face is the embedded one.
func TestDashboardPageLoadsNothingFromElsewhere(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"index.html", "app.js"} {
		assert.NotRegexp(t, `https?://(?:[^"\s]*)\.(?:css|js|woff2?|png|webp|svg)|fonts\.googleapis|fonts\.gstatic`, string(file(name)), "%s loads an asset from elsewhere", name)
	}
	assert.Contains(t, string(file("index.html")), `src: url("nunito-800.woff2")`)
	assert.Equal(t, []byte("wOF2"), file("nunito-800.woff2")[:4], "the face is a woff2")
	assert.Contains(t, string(file("OFL.txt")), "SIL Open Font License", "the face's licence is embedded beside it")
}

// emptyStateDriver draws each snapshot in turn with app.js on the scroll test's DOM shim, through
// accept (the poll's own path: render, then the Updated dot), and reads back the three places the
// owner saw wrong on the empty epoch: the cost breakdown, the Landed and ETA tiles, the dot.
const emptyStateDriver = `
const txt = id => doc.getElementById(id).textContent;
const out = input.snaps.map(d => {
  context.accept({ build: 'b1', data: d });
  const top = doc.getElementById('top-streams');
  return {
    topText: top.textContent,
    rows: top.children.map(r => ({ cls: r.className, cells: r.children.map(c => c.textContent) })),
    pct: txt('pct'), pctSfx: txt('pct-sfx'), eta: txt('eta'), etaAt: txt('eta-at'),
    live: doc.getElementById('live').className, machine: txt('machine'),
  };
});
process.stdout.write(JSON.stringify(out));
`

type emptyStateDraw struct {
	TopText string `json:"topText"`
	Rows    []struct {
		Cls   string   `json:"cls"`
		Cells []string `json:"cells"`
	} `json:"rows"`
	Pct, PctSfx, Eta, EtaAt, Live, Machine string
}

// TestEmptyAndStoppedStatesOfTheDashboard pins the owner's three sightings on the empty epoch
// (the seat ledger v1.2.4-held-2026-10-10 items 8, 9 and 10), by running the shipped app.js and
// reading the page it draws: with nothing spent the Cost breakdown keeps its stream and total
// header and shows blank rows where a line of prose stood; with nothing to land the Landed tile
// reads "nothing complete", never "- complete"; and the Updated dot is red while the machine is
// STOPPED, from the first paint and again after a run, green only while it runs.
func TestEmptyAndStoppedStatesOfTheDashboard(t *testing.T) {
	t.Parallel()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NOVA_CI") == "1" {
			require.NoError(t, err, "node is required for the dashboard JS behavioural test")
		}
		t.Skip("node is not installed on this machine; the dashboard JS behavioural test needs it")
	}
	var base map[string]any
	require.NoError(t, json.Unmarshal(fixture(t), &base))
	snap := func(sec int, machine string, empty bool) map[string]any {
		b, err := json.Marshal(base)
		require.NoError(t, err)
		var d map[string]any
		require.NoError(t, json.Unmarshal(b, &d))
		d["at"] = "2026-10-10T12:00:0" + string(rune('0'+sec)) + "Z"
		d["machine"] = machine
		if empty {
			d["landed"], d["all"], d["summary"] = 0, 0, "0/0 0.0% done"
			delete(d, "cost_by_tier")
			delete(d, "stream_costs")
			d["tables"].(map[string]any)["work"] = map[string]any{}
		}
		return d
	}
	in, err := json.Marshal(map[string]any{"appJS": string(file("app.js")), "snaps": []any{
		snap(1, "machine: STOPPED (by hand)", true), // the first paint of a stopped machine
		snap(2, "machine: running", true),
		snap(3, "machine: STOPPED (by hand)", true), // stopped again after a run
		snap(4, "machine: running", false),          // the filled layout, for comparison
	}})
	require.NoError(t, err)
	shim, _, ok := strings.Cut(scrollShim, "// the viewer:")
	require.True(t, ok, "the scroll test's shim has its viewer")
	cmd := exec.Command(nodePath, "-e", shim+emptyStateDriver)
	cmd.Stdin = bytes.NewReader(in)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	require.NoError(t, cmd.Run(), "node runner failed: %s", errBuf.String())
	require.Empty(t, errBuf.String(), "app.js threw while drawing")
	var draws []emptyStateDraw
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &draws), outBuf.String())
	require.Len(t, draws, 4)

	for i, d := range draws[:3] { // the empty epoch, stopped or running
		require.NotEmpty(t, d.Rows, "draw %d: the cost breakdown keeps its table", i)
		assert.NotContains(t, d.TopText, "no stream has spent", "draw %d: no line of prose stands where the table is", i)
		head := d.Rows[0]
		assert.Equal(t, "row head", head.Cls, "draw %d: the header row stays", i)
		require.GreaterOrEqual(t, len(head.Cells), 2)
		assert.Equal(t, "stream", head.Cells[0], "draw %d: the stream header", i)
		assert.Equal(t, "total", head.Cells[len(head.Cells)-1], "draw %d: the total header", i)
		require.Greater(t, len(d.Rows), 1, "draw %d: blank rows stand where the streams would", i)
		for _, r := range d.Rows[1:] {
			assert.Len(t, r.Cells, len(head.Cells), "draw %d: a blank row has the header's cells", i)
			for _, c := range r.Cells {
				assert.Equal(t, "-", c, "draw %d: a blank row's cell", i)
			}
		}
		assert.Equal(t, "nothing complete", d.Pct+d.PctSfx, "draw %d: the Landed tile at 0 of 0", i)
		assert.Equal(t, "none", d.Eta, "draw %d: the ETA tile with nothing to land", i)
		assert.Equal(t, "nothing to land", d.EtaAt, "draw %d", i)
	}
	// the dot: red from the first paint of a stopped machine, and again after a run
	assert.Equal(t, "STOPPED", draws[0].Machine)
	assert.Contains(t, strings.Fields(draws[0].Live), "stopped", "the first paint of a stopped machine is red")
	assert.NotContains(t, strings.Fields(draws[0].Live), "ok", "a stopped machine's dot is never green")
	assert.Equal(t, "live ok", draws[1].Live, "a running machine's dot is green")
	assert.Contains(t, strings.Fields(draws[2].Live), "stopped", "stopped again after a run: red")
	assert.NotContains(t, strings.Fields(draws[2].Live), "ok")
	assert.Equal(t, "live ok", draws[3].Live)

	// the filled layout is unchanged: the same header, the stream's row, its percentage and " complete"
	f := draws[3]
	require.Greater(t, len(f.Rows), 1)
	assert.Equal(t, "row head", f.Rows[0].Cls)
	assert.Equal(t, "stream", f.Rows[0].Cells[0])
	assert.True(t, strings.HasSuffix(f.Pct, "%"), "the Landed tile reads a percentage: %q", f.Pct)
	assert.Equal(t, " complete", f.PctSfx)
	assert.NotContains(t, f.TopText, "no stream has spent")

	// the CSS the dot needs, and the suffix the tile's script owns
	html := string(file("index.html"))
	assert.Regexp(t, `\.live\.stopped \.dot \{ background: var\(--critical\)`, html, "the stopped dot is the critical color")
	assert.Contains(t, html, `id="pct-sfx"`)
}
