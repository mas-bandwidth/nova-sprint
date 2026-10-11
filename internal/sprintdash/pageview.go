package sprintdash

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The page's copy (the owner, 2026-10-11, of the page the Python server serves at :7390:
// "this is what it is."): /api/sprint on the page's listeners is the envelope that server
// serves (dashboard/server.py: ok, data, fetchedAt, dataAgeSeconds, attemptAt, error,
// readSeconds, minInterval, throughput, throughputMinutes, build), and its data is where
// --json as that server's feed prints it (dashboard/bin/ns.sh): every stream, no release
// switch and no view of its own, with the feed's two folds and nothing else:
//
//   - each stream's stream_costs record is merged into its row of tables.work, the record's
//     fields over the row's (the feed's jq: .tables.work |= with_entries(.value += $sc[.key]));
//   - each fleet row's reads_working and working gain the reading count of its machine's
//     reader rows, reader-<m> and reader-<m>-<n> (the owner, 2026-10-11: "you forgot to
//     display the orange read cards"; reads run on reader rows, so a fleet row's own
//     reads_working reads 0 while its machine reads), so the page's orange read cells are lit
//     and the lit cells number the row's working figure, as app.js draws them.
//
// The dashboard reads where with --cards --rows --archived for its pull routes; what those
// flags add (cards, merging, judgments, holds, lanes, rows, merge_row's oldest merging age, the
// archived streams' rows of the work and merge tables) is taken off the page's copy, so the page is shown what the feed's plain
// where --json shows it. A puller's copy is its upstream's page copy already, kept as it came.

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

// pageOnly is what where --json --cards --rows adds to where --json: taken off the page's copy.
var pageOnly = []string{"cards", "merging", "judgments", "holds", "lanes", "rows"}

// readerRow reads a reader row's machine off its name: reader-<m> or reader-<m>-<n>.
var readerRow = regexp.MustCompile(`^reader-(.+?)(-[0-9]+)?$`)

// pageCopyOf is the page's copy of a good read's body, read at end; pulled is a puller's,
// its upstream's page copy, kept as it came.
func pageCopyOf(body []byte, pulled bool, end time.Time) pageCopy {
	data := append(json.RawMessage(nil), body...)
	if !pulled {
		data = pageData(data)
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
// copy (pageData).
func PageData(body []byte) []byte { return pageData(body) }

// pageData is where --json --cards --rows --archived's body as the feed's where --json with
// its folds (above); body itself when it is no JSON object.
func pageData(body json.RawMessage) json.RawMessage {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil || top == nil {
		return body
	}
	for _, k := range pageOnly {
		delete(top, k)
	}
	// merge_row's oldest merging card's age is read with --cards or --rows alone: plain where
	// --json carries null there
	var mergeRow map[string]json.RawMessage
	if json.Unmarshal(top["merge_row"], &mergeRow) == nil && mergeRow != nil {
		if _, has := mergeRow["oldest_merging_min"]; has {
			mergeRow["oldest_merging_min"] = json.RawMessage("null")
			top["merge_row"] = mustJSON(mergeRow)
		}
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

// PageSnapshot is /api/sprint's body on the page's listeners now (pageSnapshot).
func (s *Server) PageSnapshot() []byte {
	build := s.Build()
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := pageSnapshot{
		OK: s.snap.OK, AttemptAt: s.snap.AttemptAt, Error: s.snap.Error, ReadSeconds: s.snap.ReadSeconds,
		MinInterval: s.Every.Seconds(), Throughput: s.snap.Throughput, ThroughputMinutes: s.snap.ThroughputMinutes, Build: build,
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

// index is the page with its script versioned by the build and the logo slot and favicon
// filled as the live page's server fills them (its index_page): with no --logo, the robot
// tile at 192 px and 384 px, the markup that server writes, character for character, the
// build in place of its version; with --logo, an svg logo is drawn inline in the page's
// text colour and is the favicon at /favicon.svg, and any other image is /logo.
func (s *Server) index() []byte {
	build := s.Build()
	html := strings.ReplaceAll(string(file("index.html")), `src="app.js"`, `src="app.js?v=`+build+`"`)
	slot, icon := "", ""
	if s.Logo == "" {
		v := build
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
