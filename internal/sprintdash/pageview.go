package sprintdash

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The page's copy (the owner, 2026-10-11, of the page the Python server serves at :7390:
// "this is what it is."): /api/sprint on the page's listeners is the envelope that server
// serves (dashboard/server.py: ok, data, fetchedAt, dataAgeSeconds, attemptAt, error,
// readSeconds, minInterval, throughput, throughputMinutes, build), and its data is where
// --json --archived --rows as that server's feed prints it (dashboard/bin/ns.sh): every
// stream, no release switch and no view of its own, with the feed's three folds and nothing
// else:
//
//   - each stream's stream_costs record is merged into its row of tables.work, the record's
//     fields over the row's (the feed's jq: .tables.work |= with_entries(.value += $sc[.key]));
//   - each fleet row's reads_working and working gain the reading count of its machine's
//     reader rows, reader-<m> and reader-<m>-<n> (the owner, 2026-10-11: "you forgot to
//     display the orange read cards"; reads run on reader rows, so a fleet row's own
//     reads_working reads 0 while its machine reads), so the page's orange read cells are lit
//     and the lit cells number the row's working figure, as app.js draws them;
//   - landed, all and the summary's x/y and ETA are counted over the work cards alone, never
//     the sentinels (foldSentinels, the feed's second jq), and the rows and the archived
//     streams' rows of the work and merge tables are taken out again.
//
// The dashboard reads where with --cards --rows --archived for its pull routes; what --cards
// adds (cards, merging, judgments, holds, lanes) is taken off the page's copy, so the page is
// shown what the feed's where shows it. A puller's copy is its upstream's page copy already,
// kept as it came, with its upstream's throughput; an upstream that answers any other view (a
// pull listener's, with its release switch and fix marks) is a failed read (dashboard.go, read).
//
// The page's throughput is counted as the live server counts it (pageThroughput): the page
// copy's landed alone, so an archive, which lowers it, starts the samples again, as on the live
// page; the pull routes keep the epoch's count (dashboard.go, record).
//
// This file is the page: its files are embedded here, its handler (ServeHTTP) and its copy and
// envelope are here, and the page lock (pagelock.go) holds this file with page/.

// page is the page's files: the live page's own (the owner, 2026-10-11, of the page the
// Python server serves at :7390: "this is what it is."), byte for byte: index.html, app.js,
// the wordmark's face and its licence, and the robot tile logo at 192 and 384 px. The page
// lock (pagelock.go) holds them: a change to any of them is red without the owner's ask.
//
// The charts (the owner, 2026-10-18: "I would like it if you could create a stats page showing me
// all the things you can do with https://pele.run for the sprint dashboard. You could put these
// things below the existing stuff we have at the bottom."): charts.js draws them, with Pele
// (pele.js, MIT, its licence and pin in PELE.txt), loaded only when the charts are in view.
//
//go:embed page/index.html page/app.js page/nunito-800.woff2 page/OFL.txt page/logo-robot-192.png page/logo-robot-384.png
//go:embed page/charts.js page/pele.js page/PELE.txt
var page embed.FS

// ServeHTTP answers every path the page uses; every answer is no-store.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store, max-age=0")
	h.Set("Pragma", "no-cache")
	h.Set("Expires", "0")
	switch r.URL.Path {
	case "/", "/index.html":
		s.send(w, r, "text/html; charset=utf-8", s.index())
	case "/app.js":
		s.send(w, r, "text/javascript; charset=utf-8", file("app.js"))
	case "/nunito-800.woff2":
		s.send(w, r, "font/woff2", file("nunito-800.woff2"))
	case "/OFL.txt":
		s.send(w, r, "text/plain; charset=utf-8", file("OFL.txt"))
	case "/logo":
		if body, err := s.logo(); err == nil {
			s.send(w, r, logoType(s.Logo, body), body)
		} else {
			http.Error(w, "no logo", http.StatusNotFound)
		}
	case "/favicon.svg":
		if vb, inner, ok := s.svgLogo(); ok {
			s.send(w, r, "image/svg+xml", []byte(`<svg xmlns="`+svgNS+`" viewBox="`+vb+`" fill="currentColor">`+faviconStyle+inner+`</svg>`))
		} else {
			http.Error(w, "no svg logo", http.StatusNotFound)
		}
	case "/logo-tile-192.png", "/logo-tile-384.png":
		// the live page's tile logo, embedded, when no --logo is given; else the --logo file
		if s.Logo == "" {
			s.send(w, r, "image/png", file(tileFile[r.URL.Path]))
		} else if body, err := s.logo(); err == nil && !isSVG(s.Logo) {
			s.send(w, r, logoType(s.Logo, body), body)
		} else {
			http.Error(w, "no raster logo", http.StatusNotFound)
		}
	case "/favicon.png", "/logo-icon.png", "/logo.webp", "/logo.png":
		// the raster logo's routes a page or a bookmark may still name: each is the --logo file
		if body, err := s.logo(); err == nil && !isSVG(s.Logo) {
			s.send(w, r, logoType(s.Logo, body), body)
		} else {
			http.Error(w, "no raster logo", http.StatusNotFound)
		}
	case "/api/sprint":
		// the page's envelope over the page's copy (pageview.go): every stream, no release
		// switch, the keys the live page's server serves; a query is read by nothing
		s.Refresh()
		s.send(w, r, "application/json", s.PageSnapshot())
	case "/events":
		s.events(w, r, func(*sprintCopy) ([]byte, bool) { return s.PageSnapshot(), true })
	case "/healthz":
		s.healthz(w, r)
	case "/charts.js":
		s.send(w, r, "text/javascript; charset=utf-8", file("charts.js"))
	case "/pele.js":
		s.send(w, r, "text/javascript; charset=utf-8", file("pele.js"))
	case "/PELE.txt":
		s.send(w, r, "text/plain; charset=utf-8", file("PELE.txt"))
	default:
		if !s.serveCharts(w, r) {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

// file is an embedded file of the page; every name asked for is embedded.
func file(name string) []byte {
	b, err := page.ReadFile("page/" + name)
	if err != nil {
		panic("dashboard: " + name + " is not embedded")
	}
	return b
}

// pageSnapshot is /api/sprint's body on the page's listeners, its keys the live server's.
type pageSnapshot struct {
	OK                bool            `json:"ok"`
	Data              json.RawMessage `json:"data"`
	FetchedAt         *time.Time      `json:"fetchedAt"`
	DataAgeSeconds    *float64        `json:"dataAgeSeconds"`
	AttemptAt         *time.Time      `json:"attemptAt"`
	Error             *string         `json:"error"`
	ReadSeconds       *float64        `json:"readSeconds"`
	MinInterval       float64         `json:"minInterval"`
	Throughput        *float64        `json:"throughput"`
	ThroughputMinutes float64         `json:"throughputMinutes"`
	Build             string          `json:"build"`
}

// pageCopy is the last good read as the page reads it: its data, the data's own time (where's
// at; the read's end when at does not parse) and its age when the read returned.
type pageCopy struct {
	data json.RawMessage
	at   *time.Time
	age  *float64
}

// pageOnly is what --cards adds to the feed's where --json --archived --rows, and the rows the
// feed's jq drops: taken off the page's copy.
var pageOnly = []string{"cards", "merging", "judgments", "holds", "lanes", "rows"}

// readerRow reads a reader row's machine off its name: reader-<m> or reader-<m>-<n>.
var readerRow = regexp.MustCompile(`^reader-(.+?)(-[0-9]+)?$`)

// pageCopyOf is the page's copy of a good read's body, read at end; pulled is a puller's,
// its upstream's page copy, kept as it came.
func pageCopyOf(body []byte, pulled bool, end time.Time) pageCopy {
	data := append(json.RawMessage(nil), body...)
	if !pulled {
		data = pageData(data, end)
	}
	c := pageCopy{data: data}
	var v struct {
		At time.Time `json:"at"`
	}
	if json.Unmarshal(data, &v) == nil && !v.At.IsZero() {
		at := v.At
		age := math.Round(end.Sub(at).Seconds()*1000) / 1000
		c.at, c.age = &at, &age
	} else {
		at := end
		c.at = &at
	}
	return c
}

// PageData is a where --json body (with --cards, --rows, --archived or none) as the page's
// copy at now (pageData).
func PageData(body []byte, now time.Time) []byte { return pageData(body, now) }

// pageData is where --json --cards --rows --archived's body, read at now, as the feed's where
// --json --archived --rows with its folds (above); body itself when it is no JSON object.
func pageData(body json.RawMessage, now time.Time) json.RawMessage {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil || top == nil {
		return body
	}
	foldSentinels(top, now)
	for _, k := range pageOnly {
		delete(top, k)
	}
	var tables map[string]map[string]map[string]json.RawMessage
	if json.Unmarshal(top["tables"], &tables) != nil || tables == nil {
		return mustJSON(top)
	}
	var archived struct {
		Streams []string `json:"streams"`
	}
	_ = json.Unmarshal(top["archived"], &archived) // ignored: no archived streams is none to take off
	for _, s := range archived.Streams {
		delete(tables["work"], s)
		delete(tables["merge"], s)
	}
	foldStreamCosts(tables, top["stream_costs"])
	foldReaders(tables)
	top["tables"] = mustJSON(tables)
	return mustJSON(top)
}

// foldStreamCosts merges each stream's stream_costs record into its work row, the record's
// fields over the row's; nothing without stream_costs or a work table.
func foldStreamCosts(tables map[string]map[string]map[string]json.RawMessage, raw json.RawMessage) {
	var costs map[string]map[string]json.RawMessage
	if json.Unmarshal(raw, &costs) != nil || costs == nil || tables["work"] == nil {
		return
	}
	for stream, row := range tables["work"] {
		if row == nil {
			row = map[string]json.RawMessage{}
			tables["work"][stream] = row
		}
		for k, v := range costs[stream] {
			row[k] = v
		}
	}
}

// foldReaders adds to each fleet row's reads_working and working the reading count of its
// machine's reader rows (a friend's reader rows name no fleet row and add nothing); both are
// written as strings, as where writes them. Nothing without a readers and a fleet table.
func foldReaders(tables map[string]map[string]map[string]json.RawMessage) {
	readers, fleet := tables["readers"], tables["fleet"]
	if readers == nil || fleet == nil {
		return
	}
	reading := map[string]float64{}
	for name, row := range readers {
		if m := readerRow.FindStringSubmatch(name); m != nil {
			reading[m[1]] += cellNumber(row["reading"])
		}
	}
	for machine, row := range fleet {
		if row == nil {
			row = map[string]json.RawMessage{}
			fleet[machine] = row
		}
		r := reading[machine]
		row["reads_working"] = mustJSON(numberText(cellNumber(row["reads_working"]) + r))
		row["working"] = mustJSON(numberText(cellNumber(row["working"]) + r))
	}
}

// cellNumber is a cell's number: a JSON number, or a string that reads as one; else 0 (the
// feed's tonumber? // 0).
func cellNumber(raw json.RawMessage) float64 {
	raw = bytes.TrimSpace(raw)
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = []byte(s)
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

// numberText is a number as the feed's tostring writes it: a whole number without a point.
func numberText(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// pageThroughput is the page's throughput, as the live server counts it (server.py
// sample_landed): one sample of the page copy's landed (where's own, the archived streams' not
// added) at each good read's end, kept for Window; a landed lower than the last starts the
// samples again; the rate, cards an hour over the samples' span, is null until they span
// MinSpan; both figures are rounded to one decimal as Python's round(x, 1) rounds them.
type pageThroughput struct {
	samples []sample
	rate    *float64
	minutes float64
}

// add samples landed at at.
func (p *pageThroughput) add(at time.Time, landed int64) {
	if n := len(p.samples); n > 0 && landed < p.samples[n-1].landed {
		p.samples = p.samples[:0]
	}
	p.samples = append(p.samples, sample{at, landed})
	drop := 0
	for drop < len(p.samples) && at.Sub(p.samples[drop].at) > Window {
		drop++
	}
	p.samples = p.samples[drop:]
	span := at.Sub(p.samples[0].at)
	p.minutes = pyRound1(span.Seconds() / 60)
	p.rate = nil
	if span >= MinSpan {
		r := pyRound1(float64(landed-p.samples[0].landed) * 3600 / span.Seconds())
		p.rate = &r
	}
}

// pyRound1 is Python's round(f, 1): the nearest one-decimal value to f's exact binary value,
// a tie to even.
func pyRound1(f float64) float64 {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', 1, 64), 64) // ignored: FormatFloat writes a number
	return r
}

// pageRecord keeps a good read as the page reads it (pageCopyOf) and samples its landed for the
// page's throughput, under mu; a puller's is its upstream's page's (up), so the two pages agree.
func (s *Server) pageRecord(body []byte, up *snapshot, end time.Time) {
	s.page = pageCopyOf(body, up != nil, end)
	if up != nil {
		s.pageRate.rate, s.pageRate.minutes = up.Throughput, up.ThroughputMinutes
		return
	}
	var v struct {
		Landed json.RawMessage `json:"landed"`
	}
	_ = json.Unmarshal(s.page.data, &v) // ignored: a copy with no landed samples 0, as int(data.get("landed", 0))
	s.pageRate.add(end, int64(cellNumber(v.Landed)))
}

// withServerVersion is data with its server_version set to version when data has none, as the
// live page's server sets it (server.py: data.setdefault("server_version", ...)); app.js reads the
// version chip off data.server_version (the owner 2026-10-12 ~01:20Z). data itself when version is
// empty, data already carries one, or data is no JSON object.
func withServerVersion(data json.RawMessage, version string) json.RawMessage {
	if version == "" {
		return data
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil || top == nil {
		return data
	}
	var have string
	if json.Unmarshal(top["server_version"], &have) == nil && have != "" {
		return data
	}
	top["server_version"] = mustJSON(version)
	return mustJSON(top)
}

// PageSnapshot is /api/sprint's body on the page's listeners now (pageSnapshot).
func (s *Server) PageSnapshot() []byte {
	build := s.Build()
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := pageSnapshot{
		OK: s.snap.OK, AttemptAt: s.snap.AttemptAt, Error: s.snap.Error, ReadSeconds: s.snap.ReadSeconds,
		MinInterval: s.Every.Seconds(), Throughput: s.pageRate.rate, ThroughputMinutes: s.pageRate.minutes, Build: build,
	}
	if len(s.page.data) > 0 {
		var age *float64
		if s.page.at != nil {
			a := math.Round(s.Now().Sub(*s.page.at).Seconds()*1000) / 1000
			age = &a
		} else {
			age = s.page.age
		}
		snap.Data, snap.FetchedAt, snap.DataAgeSeconds = withServerVersion(s.page.data, s.Version), s.page.at, age
	}
	b, err := json.Marshal(snap)
	if err != nil {
		panic("dashboard: the page snapshot does not marshal: " + err.Error())
	}
	return b
}

// tileFile is the embedded file each tile logo route serves.
var tileFile = map[string]string{"/logo-tile-192.png": "logo-robot-192.png", "/logo-tile-384.png": "logo-robot-384.png"}

// index is the page with its script versioned by the build and the logo slot and favicon
// filled as the live page's server fills them (its index_page): with no --logo, its tile
// branch (the tile img in the slot, the tile favicon links), the build in
// place of its version; with --logo, an svg logo is drawn inline in the page's
// text colour and is the favicon at /favicon.svg, and any other image is /logo.
func (s *Server) index() []byte {
	build := s.Build()
	html := strings.ReplaceAll(string(file("index.html")), `src="app.js"`, `src="app.js?v=`+build+`"`)
	html = strings.ReplaceAll(html, `src="charts.js"`, `src="charts.js?v=`+build+`"`)
	slot, icon := "", ""
	if s.Logo == "" {
		v := build
		// server.py index_page, its tile branch: the tile img in the <!--LOGO--> slot (which
		// sits inside a comment in the live page, beside its inline data-PNG logo)
		slot = `<img id="logo" class="logo-tile" src="/logo-tile-192.png?v=` + v + `" srcset="/logo-tile-192.png?v=` + v + ` 1x, /logo-tile-384.png?v=` + v + ` 2x" alt="">`
		icon = `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=` + v + `">` +
			`<link rel="apple-touch-icon" href="/logo-tile-192.png?v=` + v + `">` +
			`<link rel="preload" as="image" href="/logo-tile-192.png?v=` + v + `" imagesrcset="/logo-tile-192.png?v=` + v + ` 1x, /logo-tile-384.png?v=` + v + ` 2x">`
	} else if vb, inner, ok := s.svgLogo(); ok {
		slot = `<svg id="logo" viewBox="` + vb + `" width="32" height="32" fill="currentColor" aria-hidden="true">` + inner + `</svg>`
		icon = `<link rel="icon" type="image/svg+xml" href="/favicon.svg?v=` + build + `">`
	} else if _, err := s.logo(); err == nil {
		slot = `<img id="logo" class="logo-tile" src="/logo?v=` + build + `" alt="">`
		icon = `<link rel="icon" href="/logo?v=` + build + `"><link rel="apple-touch-icon" href="/logo?v=` + build + `">`
	}
	html = strings.ReplaceAll(html, "<!--LOGO-->", slot)
	html = strings.ReplaceAll(html, "<!--FAVICON-->", icon)
	return []byte(html)
}

// Sentinels are not work (the owner, 2026-10-11: "sentinel cards should not be counted when we
// are estimating time remaining, or cards per-hour"; "Right now they are inflating the ETA"):
// the live feed (bin/ns.sh, its second jq) counts the page's x/y and ETA over the work cards
// alone, and foldSentinels is that jq, line for line. Over every primary of where --json
// --archived --rows's rows whose kind is not sentinel: all is their count and landed those in
// the landed column; the rate is the landings of the last hour (from the epoch's clear when
// that is later) when five or more, else landed over the time since the clear; the ETA is the
// cards left at that rate, rounded up, "-" when the rate is none, fewer than five have landed
// or none is left; the summary's x/y, percent and ETA are rewritten when the summary has
// where's shape, its held= kept; landed and all are set; and the archived streams' rows of
// the work and merge tables go. Times are read as the feed's jq reads them (jqTime). Nothing
// changes without rows. landed is what the page's throughput samples, so it counts work
// landings only.

// summaryShape is where's summary line: x/y, percent, held=, ETA.
var summaryShape = regexp.MustCompile(`^[0-9]+/[0-9]+ [0-9.]+%(( held=[0-9]+)?) -> ETA [^ ]+$`)

// jqFraction is the fraction of seconds the feed's jq cuts before it reads a time.
var jqFraction = regexp.MustCompile(`\.[0-9]+`)

// jqTime is a time as the feed's jq reads it (sub("\\.[0-9]+"; "") | fromdateiso8601): the first
// run of a point and digits cut, then exactly YYYY-MM-DDTHH:MM:SSZ, in seconds; false when it does
// not read so (an offset other than Z, or no time), and false before 1900, which the Studio's
// jq (jq-1.7.1-apple) does not read either: where's zero time, 0001-01-01T00:00:00Z, a sprint never
// cleared, reads as none there and so here. (That jq also refuses the last seconds of 1969, a
// clock no sprint carries; that one quirk is not followed.)
func jqTime(s string) (float64, bool) {
	cut := jqFraction.FindStringIndex(s)
	if cut != nil {
		s = s[:cut[0]] + s[cut[1]:]
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", s)
	if err != nil || t.Year() < 1900 {
		return 0, false
	}
	return float64(t.Unix()), true
}

// foldSentinels is the feed's second jq over top at now (above).
func foldSentinels(top map[string]json.RawMessage, now time.Time) {
	var rows []struct {
		Column string         `json:"column"`
		Fields map[string]any `json:"fields"`
	}
	if raw, ok := top["rows"]; !ok || json.Unmarshal(raw, &rows) != nil || rows == nil {
		return // (.rows | type) != "array": as where printed it
	}
	nowS := float64(now.UnixNano()) / 1e9
	all, landed := 0, 0
	var landedAt []string
	for _, r := range rows {
		if kind, _ := r.Fields["kind"].(string); kind == "sentinel" {
			continue
		}
		all++
		if r.Column == "landed" {
			landed++
			at, _ := r.Fields["landed"].(string) // a field of another shape reads as none
			landedAt = append(landedAt, at)
		}
	}
	var cleared string
	_ = json.Unmarshal(top["cleared"], &cleared) // ignored: no clear reads as now, as the jq's // $now
	first, ok := jqTime(cleared)
	if !ok {
		first = nowS
	}
	from := math.Max(nowS-3600, first)
	n := 0
	for _, at := range landedAt {
		if t, ok := jqTime(at); ok && t > from && t <= nowS {
			n++
		}
	}
	rate := 0.0
	switch {
	case n >= 5 && nowS > from:
		rate = float64(n) * 3600 / (nowS - from)
	case landed > 0 && nowS > first:
		rate = float64(landed) * 3600 / (nowS - first)
	}
	left := all - landed
	eta := 0.0
	if !(rate <= 0 || landed < 5 || left <= 0) {
		eta = math.Ceil(float64(left) * 60 / rate)
	}
	etaw := "-"
	switch {
	case eta >= 1440:
		h := math.Floor((eta + 59) / 60)
		etaw = fmt.Sprintf("%dd%dh", int64(math.Floor(h/24)), int64(h)%24)
	case eta >= 60:
		etaw = fmt.Sprintf("%dh%dm", int64(math.Floor(eta/60)), int64(eta)%60)
	case eta > 0:
		etaw = fmt.Sprintf("%dm", int64(eta))
	}
	pct := "0.0%"
	if all > 0 {
		r := int64(math.Floor(1000*float64(landed)/float64(all) + 0.5))
		pct = fmt.Sprintf("%d.%d%%", r/10, r%10)
	}
	var summary string
	if json.Unmarshal(top["summary"], &summary) == nil {
		if m := summaryShape.FindStringSubmatch(summary); m != nil {
			top["summary"] = mustJSON(fmt.Sprintf("%d/%d %s%s -> ETA %s", landed, all, pct, m[1], etaw))
		}
	}
	top["landed"], top["all"] = mustJSON(landed), mustJSON(all)
}
