// Package sprintdash is the sprint dashboard's server (docs/SPEC-SPRINT-DASHBOARD.md):
// one page, embedded in the binary (the live page the owner watches, byte for byte in
// what it renders: live_test.go), /api/sprint, a cached copy of the sprint as
// `nova-sprint where --json` prints it, /events, each new copy pushed as it is read, the
// logo files (logos.go), and the pull routes (pull.go), a worker's own view of the same
// copy, read as `where --json --cards`. Every path the Python server.py served answers
// here with the same JSON. The terminal table stays the canonical view; this is a second
// view of the same JSON.
//
// The server is a function of its requests and its clock: Read is how it reads the
// sprint and Now is its clock, so a test drives it with no socket and no real time.
// The server reads the sprint at most once per Every, and only while a page or a puller
// asks or an event stream is open (Run, on a ticker the caller hands it). A read that
// fails holds the last good copy: the page changes nothing and says nothing, and the
// failure is a line on Log.
package sprintdash

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
)

// page is the page's files: index.html, app.js, the wordmark's face and its licence.
//
//go:embed page/index.html page/app.js page/nunito-800.woff2 page/OFL.txt
var page embed.FS

const (
	// Window is how far back the throughput's samples reach.
	Window = time.Hour
	// MinSpan is the samples throughput needs before it shows: until then it is null.
	MinSpan = 10 * time.Minute
	// LogEvery is the time between the read-time summary lines on Log.
	LogEvery = time.Minute
)

// Server serves the page and the sprint's cached copy. Read, Now and Every are
// required; the rest may be left zero.
type Server struct {
	// Read reads the sprint once: the bytes `where --json` prints. It is bounded by its
	// own transport (the sprint server's client, or the store's connection).
	Read func() ([]byte, error)
	// Now is the server's clock.
	Now func() time.Time
	// Every is the least time between two reads' starts.
	Every time.Duration
	// Logo is the image file served as the logo and the favicon; "" is none, and
	// the page's logo slot renders nothing.
	Logo string
	// LogoDir is a directory of logo files read as server.py read the files beside the
	// page (logos.go): logo.svg, else a tile with its 192 and 384 px copies, else a
	// photo keyed into an icon. "" is none. Logo, when given, wins.
	LogoDir string
	// Convert runs an image tool (sips or ffmpeg) to make a logo copy; nil runs it as
	// a process bounded by 30 s.
	Convert func(argv []string) error
	// FFmpeg is the ffmpeg a photo logo is keyed with; "" is the one on PATH.
	FFmpeg string
	// Version is the binary's version, part of the build number.
	Version string
	// Log takes a line per new read failure and a read-time summary a minute.
	Log io.Writer
	// Keepalive is the time between two keepalive comments on an idle /events stream;
	// zero is KeepaliveDefault.
	Keepalive time.Duration
	// keepaliveTick is a test's keepalive ticker in place of the clock's; nil is the clock.
	keepaliveTick func(time.Duration) (<-chan time.Time, func())

	mu      sync.Mutex
	reading bool
	began   time.Time // when the last read began; zero before the first
	snap    snapshot
	copy    *sprintCopy   // the last good read as the pull routes read it; nil before one
	gen     uint64        // the good reads so far: an /events client sends each new one
	changed chan struct{} // closed, and replaced, at each good read
	streams int           // the /events clients connected
	samples []sample
	stats   readStats
	full    json.RawMessage // the last good read whole, cards and judgments included: the pull routes' /api/sprint
	derive  sync.Mutex      // one logo copy made at a time
}

// snapshot is /api/sprint's body, server.py's keys in server.py's order: the page reads
// data, throughput, throughputMinutes and build; the rest says how the reads are going
// (ok and error the last attempt's, fetchedAt the last good read's end, attemptAt the last
// attempt's, readSeconds its wall time, minInterval the least seconds between two reads).
type snapshot struct {
	OK                bool            `json:"ok"`
	Data              json.RawMessage `json:"data"`
	FetchedAt         *isoTime        `json:"fetchedAt"`
	AttemptAt         *isoTime        `json:"attemptAt"`
	Error             *string         `json:"error"`
	ReadSeconds       *float64        `json:"readSeconds"`
	MinInterval       float64         `json:"minInterval"`
	Throughput        *float64        `json:"throughput"`
	ThroughputMinutes float64         `json:"throughputMinutes"`
	Build             string          `json:"build"`
}

// isoTime is a time as server.py wrote one (Python's isoformat in UTC):
// 2026-10-05T16:51:19.989410+00:00, the microseconds left out when they are zero.
type isoTime time.Time

func (t isoTime) MarshalJSON() ([]byte, error) {
	u := time.Time(t).UTC().Truncate(time.Microsecond)
	layout := "2006-01-02T15:04:05.000000-07:00"
	if u.Nanosecond() == 0 {
		layout = "2006-01-02T15:04:05-07:00"
	}
	return []byte(`"` + u.Format(layout) + `"`), nil
}

func (t *isoTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.Parse(time.RFC3339Nano, s)
	*t = isoTime(v)
	return err
}

// sample is one good read's landed count and when it began.
type sample struct {
	at     time.Time
	landed int64
}

// readStats is the reads since the last summary line.
type readStats struct {
	since     time.Time
	n, failed int
	sum, max  time.Duration
}

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
	case "/logo-tile-192.png", "/logo-tile-384.png", "/logo-icon.png", "/favicon.png", "/logo.webp", "/logo.png", "/favicon.svg":
		s.serveLogoFile(w, r.URL.Path)
	case "/logo":
		if body, err := s.logo(); err == nil {
			s.send(w, logoType(s.Logo, body), body)
		} else {
			http.Error(w, "no logo", http.StatusNotFound)
		}
	case "/api/sprint":
		s.Refresh()
		s.send(w, "application/json", s.Snapshot())
	case "/events":
		s.events(w, r, func(*sprintCopy) ([]byte, bool) { return s.Snapshot(), true })
	case "/healthz":
		s.send(w, "text/plain; charset=utf-8", []byte("ok\n"))
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) send(w http.ResponseWriter, ctype string, body []byte) {
	s.sendCode(w, http.StatusOK, ctype, body)
}

// sendCode answers with code, the body whole (a refusal's own words, as server.py's).
func (s *Server) sendCode(w http.ResponseWriter, code int, ctype string, body []byte) {
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(code)
	_, _ = w.Write(body) // ignored: a page gone away asks again in a second
}

// file is an embedded file of the page; every name asked for is embedded.
func file(name string) []byte {
	b, err := page.ReadFile("page/" + name)
	if err != nil {
		panic("dashboard: " + name + " is not embedded")
	}
	return b
}

// Refresh reads the sprint when Every has passed since the last read began and no
// read is running; otherwise the cached copy stands.
func (s *Server) Refresh() { s.refresh(s.Every) }

// refresh reads the sprint when gap has passed since the last read began and no read is
// running.
func (s *Server) refresh(gap time.Duration) {
	s.mu.Lock()
	start := s.Now()
	if s.reading || !s.began.IsZero() && start.Sub(s.began) < gap {
		s.mu.Unlock()
		return
	}
	s.reading, s.began = true, start
	s.mu.Unlock()

	body, err := s.Read()
	if err == nil {
		err = sprintJSON(body)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.reading = false
	s.record(start, s.Now(), body, err)
}

// sprintJSON is why body is not the sprint's JSON, nil when it is.
func sprintJSON(body []byte) error {
	var v struct {
		Tables json.RawMessage `json:"tables"`
	}
	if json.Unmarshal(body, &v) != nil {
		return errors.New("where printed no JSON")
	}
	if len(v.Tables) == 0 || string(v.Tables) == "null" {
		return errors.New("where JSON has no tables")
	}
	return nil
}

// record keeps a read's outcome, under mu: a good read replaces the copy and adds a
// throughput sample; a failed one keeps the copy, and a new failure is logged once.
func (s *Server) record(start, end time.Time, body []byte, err error) {
	took := end.Sub(start)
	secs := float64(took.Round(time.Millisecond)) / float64(time.Second)
	s.snap.AttemptAt, s.snap.ReadSeconds = (*isoTime)(&end), &secs
	if err != nil {
		why := oneline.Escape(err.Error())
		if s.snap.OK || s.snap.Error == nil || *s.snap.Error != why {
			s.logf(end, "read failed: %s; the page holds the last good copy", why)
		}
		s.snap.OK, s.snap.Error = false, &why
	} else {
		var v struct {
			Landed int64 `json:"landed"`
		}
		// ignored: sprintJSON has read body as JSON; a landed that is no number is 0
		_ = json.Unmarshal(body, &v)
		rate, minutes := s.sampleLanded(start, v.Landed)
		var c sprintCopy
		// ignored: sprintJSON has read body as JSON; a field of another shape is left zero
		_ = json.Unmarshal(body, &c)
		s.copy = &c
		s.gen++
		if s.changed != nil {
			close(s.changed)
		}
		s.changed = make(chan struct{})
		s.snap.OK, s.snap.Error = true, nil
		s.full = append(json.RawMessage(nil), bytes.TrimSpace(body)...)
		s.snap.Data = pageData(s.full)
		s.snap.FetchedAt, s.snap.Throughput, s.snap.ThroughputMinutes = (*isoTime)(&end), rate, minutes
	}
	s.summarize(end, took, err != nil)
}

// sampleLanded adds a sample and is the cards landed per hour over the samples of the
// last Window (one decimal), nil until they span MinSpan, with the minutes they span.
// A landed count lower than the last (a cleared sprint) starts the samples again.
func (s *Server) sampleLanded(at time.Time, landed int64) (*float64, float64) {
	if n := len(s.samples); n > 0 && landed < s.samples[n-1].landed {
		s.samples = s.samples[:0]
	}
	s.samples = append(s.samples, sample{at, landed})
	drop := 0
	for drop < len(s.samples) && at.Sub(s.samples[drop].at) > Window {
		drop++
	}
	s.samples = s.samples[drop:]
	span := at.Sub(s.samples[0].at)
	minutes := round1(span.Minutes())
	if span < MinSpan {
		return nil, minutes
	}
	rate := round1(float64(landed-s.samples[0].landed) * float64(time.Hour) / float64(span))
	return &rate, minutes
}

func round1(f float64) float64 {
	if f < 0 {
		return -round1(-f)
	}
	return float64(int64(f*10+0.5)) / 10
}

// summarize counts a read and writes the summary line once LogEvery has passed.
func (s *Server) summarize(now time.Time, took time.Duration, failed bool) {
	st := &s.stats
	if st.since.IsZero() {
		st.since = now
	}
	st.n++
	st.sum += took
	st.max = max(st.max, took)
	if failed {
		st.failed++
	}
	if now.Sub(st.since) < LogEvery {
		return
	}
	s.logf(now, "reads=%d failed=%d read_s mean=%.3f max=%.3f last=%.3f",
		st.n, st.failed, (st.sum / time.Duration(st.n)).Seconds(), st.max.Seconds(), took.Seconds())
	*st = readStats{since: now}
}

func (s *Server) logf(at time.Time, format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "DASHBOARD %s %s\n", at.Format("2006-01-02 3:04:05 PM"), fmt.Sprintf(format, args...))
	}
}

// pageData is a read as `where --json` prints it: the cards and judgments --cards adds
// are the pull routes' alone, so the page's /api/sprint is server.py's.
func pageData(full json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(full, &m) != nil {
		return full
	}
	_, cards := m["cards"]
	_, judgments := m["judgments"]
	if !cards && !judgments {
		return full
	}
	delete(m, "cards")
	delete(m, "judgments")
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(m) != nil {
		return full
	}
	return bytes.TrimSpace(b.Bytes())
}

// Snapshot is the page's /api/sprint body now, with the build number: data as
// `where --json` prints it.
func (s *Server) Snapshot() []byte { return s.snapshotJSON(false) }

// snapshotJSON is /api/sprint's body; full: data whole, as `where --json --cards` prints
// it (the pull routes').
func (s *Server) snapshotJSON(full bool) []byte {
	build := s.Build()
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.snap
	snap.Build = build
	snap.MinInterval = s.Every.Seconds()
	if full && s.full != nil {
		snap.Data = s.full
	}
	b, err := json.Marshal(snap)
	if err != nil {
		panic("dashboard: the snapshot does not marshal: " + err.Error())
	}
	return b
}

// pageSum is the build number's part from the embedded files: it changes with the binary.
var pageSum = func() uint32 {
	h := crc32.NewIEEE()
	for _, n := range []string{"index.html", "app.js", "nunito-800.woff2"} {
		h.Write(file(n))
	}
	return h.Sum32()
}()

// Build is the build number: it changes with the page's files, the version, and the
// logo file (its name, size and time), and an open page reloads itself when it does.
func (s *Server) Build() string {
	sig := fmt.Sprintf("%08x|%s", pageSum, s.Version)
	if s.Logo != "" {
		if fi, err := os.Stat(s.Logo); err == nil {
			sig += fmt.Sprintf("|%s:%d:%d", s.Logo, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	sig += s.logoDirSig()
	return fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(sig)))
}

func (s *Server) logo() ([]byte, error) {
	if s.Logo == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(s.Logo)
}

// logoType is the logo's media type: by its name's extension, else by its bytes.
func logoType(name string, body []byte) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); strings.HasPrefix(t, "image/") {
		return t
	}
	return http.DetectContentType(body)
}

// index is the page with its script versioned by the build and the logo slot and
// favicon filled when a logo is given and readable now: --logo's file, else the logo
// directory's (logos.go), else nothing.
func (s *Server) index() []byte {
	build := s.Build()
	html := strings.ReplaceAll(string(file("index.html")), `src="app.js"`, `src="app.js?v=`+build+`"`)
	slot, icon := "", ""
	if _, err := s.logo(); err == nil {
		slot = `<img id="logo" class="logo-tile" src="/logo?v=` + build + `" alt="">`
		icon = `<link rel="icon" href="/logo?v=` + build + `"><link rel="apple-touch-icon" href="/logo?v=` + build + `">`
	} else if s.LogoDir != "" {
		slot, icon = s.logoDirSlot(build)
	}
	html = strings.ReplaceAll(html, "<!--LOGO-->", slot)
	html = strings.ReplaceAll(html, "<!--FAVICON-->", icon)
	return []byte(html)
}
