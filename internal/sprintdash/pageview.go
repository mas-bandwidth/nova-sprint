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
//go:embed page/index.html page/app.js page/nunito-800.woff2 page/OFL.txt page/logo-robot-192.png page/logo-robot-384.png
var page embed.FS

// ServeHTTP answers every path the page uses; every answer is no-store.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store, max-age=0")
	h.Set("Pragma", "no-cache")
	h.Set("Expires", "0")
	switch r.URL.Path {
	case "/", "/index.html":
		s.send(w, "text/html; charset=utf-8", s.index())
	case "/app.js":
		s.send(w, "text/javascript; charset=utf-8", file("app.js"))
	case "/nunito-800.woff2":
		s.send(w, "font/woff2", file("nunito-800.woff2"))
	case "/OFL.txt":
		s.send(w, "text/plain; charset=utf-8", file("OFL.txt"))
	case "/logo":
		if body, err := s.logo(); err == nil {
			s.send(w, logoType(s.Logo, body), body)
		} else {
			http.Error(w, "no logo", http.StatusNotFound)
		}
	case "/favicon.svg":
		if vb, inner, ok := s.svgLogo(); ok {
			s.send(w, "image/svg+xml", []byte(`<svg xmlns="`+svgNS+`" viewBox="`+vb+`" fill="currentColor">`+faviconStyle+inner+`</svg>`))
		} else {
			http.Error(w, "no svg logo", http.StatusNotFound)
		}
	case "/logo-tile-192.png", "/logo-tile-384.png":
		// the live page's tile logo, embedded, when no --logo is given; else the --logo file
		if s.Logo == "" {
			s.send(w, "image/png", file(tileFile[r.URL.Path]))
		} else if body, err := s.logo(); err == nil && !isSVG(s.Logo) {
			s.send(w, logoType(s.Logo, body), body)
		} else {
			http.Error(w, "no raster logo", http.StatusNotFound)
		}
	case "/favicon.png", "/logo-icon.png", "/logo.webp", "/logo.png":
		// the raster logo's routes a page or a bookmark may still name: each is the --logo file
		if body, err := s.logo(); err == nil && !isSVG(s.Logo) {
			s.send(w, logoType(s.Logo, body), body)
		} else {
			http.Error(w, "no raster logo", http.StatusNotFound)
		}
	case "/api/sprint":
		// the page's envelope over the page's copy (pageview.go): every stream, no release
		// switch, the keys the live page's server serves; a query is read by nothing
		s.Refresh()
		s.send(w, "application/json", s.PageSnapshot())
	case "/events":
		s.events(w, r, func(*sprintCopy) ([]byte, bool) { return s.PageSnapshot(), true })
	case "/healthz":
		s.healthz(w)
	default:
		http.Error(w, "not found", http.StatusNotFound)
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
	ServerVersion     string          `json:"server_version"`
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

// PageSnapshot is /api/sprint's body on the page's listeners now (pageSnapshot).
func (s *Server) PageSnapshot() []byte {
	build := s.Build()
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := pageSnapshot{
		OK: s.snap.OK, AttemptAt: s.snap.AttemptAt, Error: s.snap.Error, ReadSeconds: s.snap.ReadSeconds,
		MinInterval: s.Every.Seconds(), Throughput: s.pageRate.rate, ThroughputMinutes: s.pageRate.minutes, Build: build,
		ServerVersion: s.Version,
	}
	if len(s.page.data) > 0 {
		snap.Data, snap.FetchedAt, snap.DataAgeSeconds = s.page.data, s.page.at, s.page.age
	}
	b, err := json.Marshal(snap)
	if err != nil {
		panic("dashboard: the page snapshot does not marshal: " + err.Error())
	}
	return b
}

// tileFile is the embedded file each tile logo route serves.
var tileFile = map[string]string{"/logo-tile-192.png": "logo-robot-192.png", "/logo-tile-384.png": "logo-robot-384.png"}

// novaLogoData is the nova-sprint image (brand/fleet/nova-sprint-square.png, resized to
// 192 px), inline as a data PNG, the owner 2026-10-12 ~00:28Z: "The nova sprint logo goes
// top left, and replaces the robot logo".
const novaLogoData = "data:image/png;base64," + `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`

// index is the page with its script versioned by the build and the logo slot and favicon
// filled as the live page's server fills them (its index_page): with no --logo, the
// nova-sprint image inline as a data PNG (the owner, 2026-10-12 ~00:28Z), the build in
// place of its version; with --logo, an svg logo is drawn inline in the page's
// text colour and is the favicon at /favicon.svg, and any other image is /logo.
func (s *Server) index() []byte {
	build := s.Build()
	html := strings.ReplaceAll(string(file("index.html")), `src="app.js"`, `src="app.js?v=`+build+`"`)
	slot, icon := "", ""
	if s.Logo == "" {
		v := build
		slot = `<img id="logo" class="logo-tile" src="` + novaLogoData + `" alt="">`
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
