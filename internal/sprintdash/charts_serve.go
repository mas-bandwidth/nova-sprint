package sprintdash

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Serving the charts (history.go, streamview.go): /api/history and /api/stream/<name> are
// computed from the copy the server already holds (the last good read's rows and the page's
// copy), never by a read of their own, and their cost is bounded whoever asks:
//
//   - the rows are parsed once per copy (chartCards), never per request;
//   - a stream name that is no stream of the copy is answered 404 before anything is computed or
//     memoized, so a flood of made-up names costs a map lookup each;
//   - each answer is memoized for the minute (the history again only when the minute has turned
//     and the copy changed, or the hour turned), and the memo holds at most one answer per stream
//     of the copy;
//   - each computation is single-flight: N requests that miss at once compute once.
//
// Only the dashboard that reads the sprint (the reader, on loopback or the tailnet) serves them.
// A puller serves the public copy, which never carried the rows (pageview.go takes them off), and
// what the charts show (card ids of every stream, archived ones too, the needs between them, the
// workers and friends, routes, models and each attempt's times) is not the public copy's to show
// until the owner decides what it may: on a puller the routes answer 404 and the page carries
// neither the panel nor the script that draws it (index, pageview.go).

// chartsPanel and chartsScript are the page's charts, exactly as index.html carries them: a
// puller's page is index.html without them.
const (
	chartsPanel = `<section class="panel history" id="history-panel">
  <div class="panel-head"><h2>Last 24 hours</h2><span class="muted history-sub" id="history-sub"></span></div>
  <div class="charts" id="history"></div>
</section>

`
	chartsScript = "<script src=\"charts.js\"></script>\n"
)

// chartMemo is the charts' state, under mu: the copy's cards parsed once, the memoized answers,
// and the computations in flight.
type chartMemo struct {
	cardsGen uint64
	cards    []HistCard
	streams  map[string]bool // every stream of the copy: the rows' and the page copy's
	parsed   bool

	histMinute, histHour time.Time
	histGen              uint64
	hist                 []byte

	minute  time.Time
	answers map[string][]byte // a stream view, by name, this minute

	flight map[string]*flight

	parses, computes int // the rows parsed and the answers computed, counted (the tests read them)
}

// flight is one computation in flight; its waiters read body once done is closed.
type flight struct {
	done    chan struct{}
	body    []byte
	waiters int // the requests waiting on it
}

// rowsOf is the rows of a where --json --rows body; nil without them.
func rowsOf(body []byte) json.RawMessage {
	var v struct {
		Rows json.RawMessage `json:"rows"`
	}
	if json.Unmarshal(body, &v) != nil || len(v.Rows) == 0 || string(v.Rows) == "null" {
		return nil
	}
	return v.Rows
}

// unavailable is a chart route's answer when there is nothing to chart from.
func unavailable(why string) []byte { return mustJSON(map[string]string{"unavailable": why}) }

// once runs compute for key unless a run of it is in flight, whose answer it then waits for:
// single-flight.
func (s *Server) once(key string, compute func() []byte) []byte {
	s.mu.Lock()
	if s.charts.flight == nil {
		s.charts.flight = map[string]*flight{}
	}
	if f, ok := s.charts.flight[key]; ok {
		f.waiters++
		s.mu.Unlock()
		<-f.done
		return f.body
	}
	f := &flight{done: make(chan struct{})}
	s.charts.flight[key] = f
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.charts.flight, key)
		s.mu.Unlock()
		close(f.done)
	}()
	f.body = compute()
	return f.body
}

// chartCards is the copy's cards and streams, parsed once per copy (single-flight); have is
// false before a good read.
func (s *Server) chartCards() (cards []HistCard, streams map[string]bool, page json.RawMessage, have bool) {
	s.mu.Lock()
	if s.copy == nil {
		s.mu.Unlock()
		return nil, nil, nil, false
	}
	if s.charts.parsed && s.charts.cardsGen == s.gen {
		cards, streams, page = s.charts.cards, s.charts.streams, s.page.data
		s.mu.Unlock()
		return cards, streams, page, true
	}
	s.mu.Unlock()
	s.once("cards", func() []byte {
		s.mu.Lock()
		rows, gen, page := s.rows, s.gen, s.page.data
		done := s.charts.parsed && s.charts.cardsGen == gen
		s.mu.Unlock()
		if done {
			return nil
		}
		cards := HistCardsOf(rows)
		streams := pageStreams(page)
		for _, c := range cards {
			streams[c.Stream] = true
		}
		s.mu.Lock()
		s.charts.parses++
		s.charts.cards, s.charts.streams, s.charts.cardsGen, s.charts.parsed = cards, streams, gen, true
		s.mu.Unlock()
		return nil
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.charts.cards, s.charts.streams, s.page.data, true
}

// pageStreams is the streams the page's copy names: its work rows and its streams list.
func pageStreams(page json.RawMessage) map[string]bool {
	var p streamPage
	_ = json.Unmarshal(page, &p) // ignored: a copy of another shape names none
	out := map[string]bool{}
	for name := range p.Tables["work"] {
		out[name] = true
	}
	for _, x := range p.Streams {
		if x.Stream != "" {
			out[x.Stream] = true
		}
	}
	return out
}

// History is /api/history's body now (above).
func (s *Server) History() []byte {
	if b := s.histMemo(); b != nil {
		return b
	}
	return s.once("history", func() []byte {
		if b := s.histMemo(); b != nil {
			return b
		}
		cards, _, _, have := s.chartCards()
		if !have {
			return unavailable("no good read of the sprint yet")
		}
		s.mu.Lock()
		now, gen := s.Now(), s.charts.cardsGen
		s.mu.Unlock()
		b := mustJSON(History(cards, now))
		s.mu.Lock()
		s.charts.computes++
		s.charts.hist, s.charts.histMinute, s.charts.histHour, s.charts.histGen = b, now.Truncate(time.Minute), now.UTC().Truncate(time.Hour), gen
		s.mu.Unlock()
		return b
	})
}

// histMemo is the memoized history when it stands now: this minute's, or the copy unchanged in
// the same hour; nil otherwise.
func (s *Server) histMemo() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	m, h := now.Truncate(time.Minute), now.UTC().Truncate(time.Hour)
	if s.charts.hist != nil && (m.Equal(s.charts.histMinute) || (s.charts.histGen == s.gen && h.Equal(s.charts.histHour))) {
		return s.charts.hist
	}
	return nil
}

// Stream is /api/stream/<name>'s body now (above); false when name is no stream of the copy.
func (s *Server) Stream(name string) ([]byte, bool) {
	cards, streams, page, have := s.chartCards()
	if !have {
		return unavailable("no good read of the sprint yet"), true
	}
	if !streams[name] {
		return nil, false
	}
	if b, ok := s.streamMemo(name); ok {
		return b, true
	}
	return s.once("stream\x00"+name, func() []byte {
		if b, ok := s.streamMemo(name); ok {
			return b
		}
		s.mu.Lock()
		now := s.Now()
		s.mu.Unlock()
		b := mustJSON(StreamViewOf(cards, page, name, now))
		s.mu.Lock()
		s.charts.computes++
		s.charts.answers[name] = b
		s.mu.Unlock()
		return b
	}), true
}

// streamMemo is name's view memoized this minute; a new minute empties the memo.
func (s *Server) streamMemo(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.Now().Truncate(time.Minute)
	if !m.Equal(s.charts.minute) || s.charts.answers == nil {
		s.charts.minute, s.charts.answers = m, map[string][]byte{}
	}
	b, ok := s.charts.answers[name]
	return b, ok
}

// serveCharts answers the chart routes; false for any other path. A puller answers 404 (above).
func (s *Server) serveCharts(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	chart := p == "/api/history" || strings.HasPrefix(p, "/api/stream/") || strings.HasPrefix(p, "/stream/")
	if !chart {
		return false
	}
	if s.From != nil {
		http.Error(w, "not found: the charts are served by the dashboard that reads the sprint, never by a pulled copy", http.StatusNotFound)
		return true
	}
	switch {
	case p == "/api/history":
		s.Refresh()
		s.send(w, r, "application/json", s.History())
	case strings.HasPrefix(p, "/api/stream/"):
		name := strings.TrimPrefix(p, "/api/stream/")
		if !streamName(name) {
			http.Error(w, "no such stream", http.StatusNotFound)
			return true
		}
		s.Refresh()
		b, ok := s.Stream(name)
		if !ok {
			http.Error(w, "no such stream", http.StatusNotFound)
			return true
		}
		s.send(w, r, "application/json", b)
	default: // /stream/<name>
		name := strings.TrimPrefix(p, "/stream/")
		if !streamName(name) {
			http.Error(w, "no such stream", http.StatusNotFound)
			return true
		}
		if _, streams, _, have := s.chartCards(); have && !streams[name] {
			http.Error(w, "no such stream", http.StatusNotFound)
			return true
		}
		v := s.Build()
		html := strings.NewReplacer(`src="/charts.js"`, `src="/charts.js?v=`+v+`"`, `src="/stream.js"`, `src="/stream.js?v=`+v+`"`).Replace(string(file("stream.html")))
		s.send(w, r, "text/html; charset=utf-8", []byte(html))
	}
	return true
}

// streamName is whether name can be a stream's: one path segment, printable, bounded.
func streamName(name string) bool {
	if name == "" || len(name) > 200 || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
