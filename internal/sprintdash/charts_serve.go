package sprintdash

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Serving the charts (history.go, streamview.go): /api/history and /api/stream/<name> are
// computed from the copy the server already holds (the last good read's rows and the page's
// copy), never by a read of their own, and at most once a minute: each answer is memoized for
// the minute, and the history is computed again only when the minute has turned and the copy
// changed (or the hour turned). A puller holds no rows (its upstream's page copy carries none), so
// it asks its upstream for the same route, at most once a minute per route, and serves what the
// upstream answered.

// maxStreamMemo bounds the stream views memoized in one minute.
const maxStreamMemo = 256

// chartMemo is the charts' memo: the history's and the stream views', each of a minute.
type chartMemo struct {
	histMinute, histHour time.Time
	histGen              uint64
	hist                 []byte
	minute               time.Time
	answers              map[string][]byte // a stream view, or a puller's upstream answer, by path
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

// History is /api/history's body now (above).
func (s *Server) History() []byte {
	if s.From != nil {
		return s.upstream("/api/history")
	}
	s.mu.Lock()
	now := s.Now()
	m, h := now.Truncate(time.Minute), now.UTC().Truncate(time.Hour)
	memo := &s.charts
	if memo.hist != nil && (m.Equal(memo.histMinute) || (memo.histGen == s.gen && h.Equal(memo.histHour))) {
		b := memo.hist
		s.mu.Unlock()
		return b
	}
	rows, gen, have := s.rows, s.gen, s.copy != nil
	s.mu.Unlock()
	if !have {
		return unavailable("no good read of the sprint yet")
	}
	b := mustJSON(History(HistCardsOf(rows), now))
	s.mu.Lock()
	memo.hist, memo.histMinute, memo.histHour, memo.histGen = b, m, h, gen
	s.mu.Unlock()
	return b
}

// Stream is /api/stream/<name>'s body now (above).
func (s *Server) Stream(name string) []byte {
	path := "/api/stream/" + url.PathEscape(name)
	if s.From != nil {
		return s.upstream(path)
	}
	s.mu.Lock()
	now := s.Now()
	if b, ok := s.memoGet(path, now); ok {
		s.mu.Unlock()
		return b
	}
	rows, page, have := s.rows, s.page.data, s.copy != nil
	s.mu.Unlock()
	if !have {
		return unavailable("no good read of the sprint yet")
	}
	b := mustJSON(StreamViewOf(HistCardsOf(rows), page, name, now))
	s.mu.Lock()
	s.memoPut(path, b)
	s.mu.Unlock()
	return b
}

// memoGet is the memo's answer for path this minute, under mu; a new minute empties the memo.
func (s *Server) memoGet(path string, now time.Time) ([]byte, bool) {
	m := now.Truncate(time.Minute)
	if !m.Equal(s.charts.minute) || s.charts.answers == nil {
		s.charts.minute, s.charts.answers = m, map[string][]byte{}
	}
	b, ok := s.charts.answers[path]
	return b, ok
}

// memoPut keeps an answer for the minute, under mu, while the memo holds fewer than maxStreamMemo.
func (s *Server) memoPut(path string, b []byte) {
	if s.charts.answers != nil && len(s.charts.answers) < maxStreamMemo {
		s.charts.answers[path] = b
	}
}

// upstream is a puller's answer for path: its upstream dashboard's, asked at most once a minute.
func (s *Server) upstream(path string) []byte {
	s.mu.Lock()
	if b, ok := s.memoGet(path, s.Now()); ok {
		s.mu.Unlock()
		return b
	}
	s.mu.Unlock()
	b := s.From.get(path)
	s.mu.Lock()
	s.memoPut(path, b)
	s.mu.Unlock()
	return b
}

// get is the upstream's answer for path (a chart route), or an unavailable answer that says why.
func (u *Upstream) get(path string) []byte {
	base := strings.TrimSuffix(strings.TrimSuffix(u.URL, "/"), "/api/sprint")
	c := u.Client
	if c == nil {
		c = &http.Client{Timeout: UpstreamTimeout}
	}
	resp, err := c.Get(base + path)
	if err != nil {
		return unavailable("the upstream dashboard did not answer")
	}
	defer func() { _ = resp.Body.Close() }() // ignored: the body is read to its end or abandoned
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case err != nil:
		return unavailable("the upstream dashboard's answer was cut off")
	case resp.StatusCode == http.StatusNotFound:
		return unavailable("the upstream dashboard serves no charts yet: it runs an older nova-sprint")
	case resp.StatusCode != http.StatusOK || !json.Valid(body):
		return unavailable(fmt.Sprintf("the upstream dashboard answered %d", resp.StatusCode))
	}
	return body
}

// serveCharts answers the chart routes and their files; false for any other path.
func (s *Server) serveCharts(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/api/history":
		s.Refresh()
		s.send(w, r, "application/json", s.History())
	case strings.HasPrefix(p, "/api/stream/"):
		name := strings.TrimPrefix(p, "/api/stream/")
		if !streamName(name) {
			http.Error(w, "no such stream name", http.StatusNotFound)
			return true
		}
		s.Refresh()
		s.send(w, r, "application/json", s.Stream(name))
	case strings.HasPrefix(p, "/stream/"):
		if !streamName(strings.TrimPrefix(p, "/stream/")) {
			http.Error(w, "no such stream name", http.StatusNotFound)
			return true
		}
		v := s.Build()
		html := strings.NewReplacer(`src="/charts.js"`, `src="/charts.js?v=`+v+`"`, `src="/stream.js"`, `src="/stream.js?v=`+v+`"`).Replace(string(file("stream.html")))
		s.send(w, r, "text/html; charset=utf-8", []byte(html))
	default:
		return false
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
