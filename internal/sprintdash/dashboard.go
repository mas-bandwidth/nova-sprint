// Package sprintdash is the sprint dashboard's server (docs/SPEC-SPRINT-DASHBOARD.md):
// one page, embedded in the binary, /api/sprint, a cached copy of the sprint as
// `nova-sprint where --json --cards` prints it, /events, each new copy pushed as it is
// read, and the pull routes (pull.go), a worker's own view of the same copy. The terminal
// table stays the canonical view; this is a second view of the same JSON.
//
// The server is a function of its requests and its clock: Read is how it reads the
// sprint and Now is its clock, so a test drives it with no socket and no real time.
// The server reads the sprint once per Every while Run polls (Run, on a ticker the
// caller hands it: one poller, its reads back to back with Every as their floor; nova-sprint
// dashboard runs it only while someone is looking), and while Run polls a request only ever
// answers from the copy; while it does not, a request reads (Refresh, joining a read in
// flight) and /healthz checks the freshness itself. A read that fails holds the
// last good copy, marked ok false with a short reason (never the reader's own error
// text, which goes to Log only), and a read that runs past ReadTimeout is marked so.
// One freshness check (fresh.go) raises an alarm when the served data stays old; a
// puller (From) reads another dashboard's copy instead.
package sprintdash

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	htmlpkg "html"
	"io"
	"maps"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

const (
	// Window is how far back the throughput's samples reach.
	Window = time.Hour
	// MinSpan is the samples throughput needs before it shows: until then it is null. Only a
	// history that is truly empty (the dashboard's first ever start) waits for it: the samples
	// are kept across restarts (SaveSamples), so a release adoption does not. A gap in the
	// reads (the dashboard reads nothing while no one is looking, or was stopped) keeps the
	// newest sample before the Window as the base of /api/sprint's throughput (the pull
	// routes' and a puller's) until the Window's own samples span MinSpan (trimWindow): the
	// rate is then over the gap, and minutes says how long. The live page's own tile
	// (pageThroughput, pageview.go, under the page lock) is not changed: after a gap longer
	// than Window it is null again until its samples span MinSpan, ten minutes of looking.
	MinSpan = 10 * time.Minute
	// SaveEvery is the least time between two saves of the samples.
	SaveEvery = 10 * time.Second
	// LogEvery is the time between the read-time summary lines on Log.
	LogEvery = time.Minute
	// ReadTimeoutDefault is how long a read is waited on before it is marked failed.
	ReadTimeoutDefault = time.Minute
)

// ReadError is a failed read in two parts: Why is served in /api/sprint's error, and
// Detail (what the reader itself printed) goes to Log only, so nothing a read printed
// reaches the page.
type ReadError struct{ Why, Detail string }

func (e *ReadError) Error() string {
	if e.Detail == "" {
		return e.Why
	}
	return e.Why + ": " + e.Detail
}

// readFailureWhy is the part of a failed read's error the page is shown.
func readFailureWhy(err error) string {
	var re *ReadError
	if errors.As(err, &re) {
		return re.Why
	}
	return err.Error()
}

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
	// Logo is the image file served as the logo and the favicon; "" is none, and the
	// page's logo slot is the live page's robot tile, embedded (pageview.go).
	Logo string
	// Version is the binary's version, part of the build number.
	Version string
	// Command is what Read runs, logged on a slow read (>1s).
	Command string
	// Log takes a line per new read failure and a read-time summary a minute.
	Log io.Writer
	// Keepalive is the time between two keepalive comments on an idle /events stream;
	// zero is KeepaliveDefault.
	Keepalive time.Duration
	// From is the dashboard a puller reads its copy from in place of Read; nil reads the
	// sprint.
	From *Upstream
	// StaleAfter and StaleFor are the freshness check's: the served data older than
	// StaleAfter for StaleFor raises the alarm; zero is StaleAfterDefault, StaleForDefault.
	StaleAfter, StaleFor time.Duration
	// ReadTimeout is how long a read is waited on before it is marked failed; the next
	// read still waits for it to end. Zero is ReadTimeoutDefault.
	ReadTimeout time.Duration
	// keepaliveTick is a test's keepalive ticker in place of the clock's; nil is the clock.
	keepaliveTick func(time.Duration) (<-chan time.Time, func())
	// ReadTimer is a test's read timeout in place of the clock's; nil is the clock.
	ReadTimer func(time.Duration) (<-chan time.Time, func())
	// LoadSamples and SaveSamples, when both are set, keep the throughput's samples across a
	// restart: LoadSamples returns what SaveSamples last was given (nil, or an error, for none),
	// read once at the first good read; SaveSamples is given the samples at most every
	// SaveEvery, after a good read. A failure of either is logged and the samples start from
	// empty or go unsaved.
	LoadSamples func() ([]byte, error)
	SaveSamples func([]byte) error
	// OnRecord, when set, is called after record() completes under mu.
	OnRecord func()

	mu        sync.Mutex
	polling   bool // Run is the one reader: a request answers from the copy only
	reading   bool
	readOver  chan struct{} // closed once the read in flight is recorded (done or timed out)
	began     time.Time     // when the last read began; zero before the first
	pageTicks int
	snap      snapshot
	page      pageCopy       // the last good read as the page reads it (pageview.go)
	pageRate  pageThroughput // the page's throughput, counted as the live server counts it (pageview.go)
	copy      *sprintCopy    // the last good read as the pull routes read it; nil before one
	gen       uint64         // the good reads so far: an /events client sends each new one
	changed   chan struct{}  // closed, and replaced, at each good read
	samples   []sample
	restored  bool      // LoadSamples has been read
	savedAt   time.Time // when the samples were last saved
	saveBad   bool      // the last save failed (logged once)
	stats     readStats
	fresh     freshness
	rows      json.RawMessage // the last good read's rows (where --json --rows): the charts' cards; nil for a puller
	charts    chartMemo       // the charts' answers, a minute each (charts_serve.go)
}

// snapshot is /api/sprint's body: the page reads data, throughput,
// throughputMinutes, build and the release fields; the rest says how the reads are going:
// attemptAt and readSeconds are the last read's, good or not, and minInterval is Every.
// Data is the copy as the release shows it (release.go): release is the one shown,
// current the one shown when none is asked, releases every label a stream carries, and
// releaseStreams the streams shown (absent for all).
type snapshot struct {
	OK                bool            `json:"ok"`
	Data              json.RawMessage `json:"data"`
	Release           string          `json:"release,omitempty"`
	Current           string          `json:"current,omitempty"`
	Releases          []string        `json:"releases,omitempty"`
	ReleaseStreams    []string        `json:"releaseStreams,omitempty"`
	FetchedAt         *time.Time      `json:"fetchedAt"`
	AttemptAt         *time.Time      `json:"attemptAt"`
	ReadSeconds       *float64        `json:"readSeconds"`
	MinInterval       float64         `json:"minInterval"`
	Error             *string         `json:"error"`
	Throughput        *float64        `json:"throughput"`
	ThroughputMinutes float64         `json:"throughputMinutes"`
	Build             string          `json:"build"`
	Stale             bool            `json:"stale"`
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

func (s *Server) send(w http.ResponseWriter, r *http.Request, ctype string, body []byte) {
	w.Header().Set("Vary", "Accept-Encoding")
	if r != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", ctype)
		gw := gzip.NewWriter(w)
		_, _ = gw.Write(body)
		_ = gw.Close()
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	_, _ = w.Write(body) // ignored: a page gone away asks again in a second
}

// Refresh reads the sprint when Every has passed since the last read began, and waits
// for a read already running to be recorded (it joins it); otherwise the cached copy
// stands. While Run polls it reads nothing once a read has begun: the poller is the one
// reader, and a request answers from the copy (the verb makes the first read before it
// listens, so a page never reads). While Run does not (no one was looking:
// nova-sprint dashboard's viewers), the first request back reads the sprint, or joins the
// read the returning viewer started, and is answered from that read, never from the copy
// left from before (cold read of PR 490, 2026-10-18).
func (s *Server) Refresh() {
	s.mu.Lock()
	cacheOnly := s.polling && !s.began.IsZero()
	s.mu.Unlock()
	if !cacheOnly {
		s.refresh(s.Every, true)
	}
}

// readResult is one read's outcome.
type readResult struct {
	body []byte
	up   *snapshot
	err  error
}

// refresh reads the sprint when gap has passed since the last read began and no read is
// running. It decouples the page tick from slow background reads: at most one read runs
// at a time, in the background, logging when it exceeds 1s. With wait, a read already
// running is waited for until it is recorded (done, or timed out), and nothing more is
// read.
func (s *Server) refresh(gap time.Duration, wait bool) {
	s.mu.Lock()
	start := s.Now()
	if s.reading {
		over := s.readOver
		s.mu.Unlock()
		if wait && over != nil {
			<-over
		}
		return
	}
	if !s.began.IsZero() && start.Sub(s.began) < gap {
		s.mu.Unlock()
		return
	}
	over := make(chan struct{})
	recorded := sync.OnceFunc(func() { close(over) })
	s.reading, s.began, s.readOver = true, start, over
	s.mu.Unlock()

	readDone := make(chan readResult, 1)
	go func() {
		body, up, err := s.read()
		if err == nil {
			err = sprintJSON(body)
		}
		readDone <- readResult{body, up, err}
	}()

	finish := func(res readResult) {
		end := s.Now()
		took := end.Sub(start)
		if took > time.Second && s.Command != "" {
			s.logf(end, "slow read: took %.3fs: %s", took.Seconds(), s.Command)
		}
		s.mu.Lock()
		s.reading = false
		s.record(start, end, res.body, res.up, res.err)
		onRecord := s.OnRecord
		s.mu.Unlock()
		recorded()
		if onRecord != nil {
			onRecord()
		}
	}

	waitForRead := func(firstDeadline <-chan time.Time) {
		timeout := orDefault(s.ReadTimeout, ReadTimeoutDefault)
		expired, stop := s.timer(timeout)
		defer stop()
		select {
		case res := <-readDone:
			finish(res)
		case <-firstDeadline:
			go func() {
				select {
				case res := <-readDone:
					finish(res)
				case <-expired:
					s.mu.Lock()
					s.record(start, s.Now(), nil, nil, &ReadError{Why: fmt.Sprintf("the read timed out after %s", timeout)})
					s.mu.Unlock()
					recorded()
					res := <-readDone
					finish(res)
				}
			}()
		case <-expired:
			s.mu.Lock()
			s.record(start, s.Now(), nil, nil, &ReadError{Why: fmt.Sprintf("the read timed out after %s", timeout)})
			s.mu.Unlock()
			recorded()
			res := <-readDone
			finish(res)
		}
	}

	if wait {
		waitForRead(nil)
	} else {
		firstDeadline, stopFirst := s.timer(300 * time.Millisecond)
		defer stopFirst()
		waitForRead(firstDeadline)
	}
}

// timer is the read timeout's: the clock's, or a test's (ReadTimer).
func (s *Server) timer(d time.Duration) (<-chan time.Time, func()) {
	if s.ReadTimer != nil {
		return s.ReadTimer(d)
	}
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// read is one read: the sprint, or a puller's upstream with the snapshot it came in.
func (s *Server) read() ([]byte, *snapshot, error) {
	if s.From != nil {
		body, up, err := s.From.read()
		if err == nil && up != nil && (up.Release != "" || up.Current != "") {
			// a pull listener's /api/sprint, or a page from before the page was the live page:
			// its data carries the release view and the fix marks, which the page never draws
			return nil, nil, &ReadError{Why: "the upstream dashboard answered a view that is not the page's: --pull names a page's listener of a dashboard that serves the live page"}
		}
		return body, up, err
	}
	body, err := s.Read()
	return body, nil, err
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
// throughput sample (a puller's takes the upstream's time and throughput, up); a failed
// one keeps the copy, and a new failure is logged once.
func (s *Server) record(start, end time.Time, body []byte, up *snapshot, err error) {
	took := end.Sub(start)
	secs := float64(took.Milliseconds()) / 1000
	s.snap.AttemptAt, s.snap.ReadSeconds = &end, &secs
	if err != nil {
		why := oneline.Escape(readFailureWhy(err))
		if s.snap.OK || s.snap.Error == nil || *s.snap.Error != why {
			s.logf(end, "read failed: %s; the page holds the last good copy", oneline.Escape(err.Error()))
		}
		s.snap.OK, s.snap.Error = false, &why
	} else {
		s.restoreSamples(end)
		// the samples are the epoch's landed cards: the table's and the archived streams'
		// (stream archive), so an archive, which lands nothing, is not a fall that starts
		// them again; a sprint done (done) carries the epoch's in landed already, so it is
		// landed alone then, and the finish is no spike
		var v struct {
			Landed         int64 `json:"landed"`
			ArchivedLanded int64 `json:"archived_landed"`
			Done           bool  `json:"done"`
		}
		// ignored: sprintJSON has read body as JSON; a landed that is no number is 0
		_ = json.Unmarshal(body, &v)
		at, rate, minutes := end, (*float64)(nil), 0.0
		if up != nil {
			at, rate, minutes = *up.FetchedAt, up.Throughput, up.ThroughputMinutes
		} else {
			epoch := v.Landed
			if !v.Done {
				epoch += v.ArchivedLanded
			}
			rate, minutes = s.sampleLanded(start, epoch)
		}
		s.fresh.at = at
		var c sprintCopy
		// ignored: sprintJSON has read body as JSON; a field of another shape is left zero
		_ = json.Unmarshal(body, &c)
		s.copy = &c
		if up == nil {
			s.rows = rowsOf(body)
		}
		s.gen++
		if s.changed != nil {
			close(s.changed)
		}
		s.changed = make(chan struct{})
		s.snap.OK, s.snap.Error = true, nil
		s.snap.Data = placed(bytes.TrimSpace(body))
		s.pageRecord(bytes.TrimSpace(body), up, end)
		s.snap.FetchedAt, s.snap.Throughput, s.snap.ThroughputMinutes = &at, rate, minutes
		s.saveSamples(end)
	}
	s.summarize(end, took, err != nil)
}

// samplesFile is the saved samples: the pull routes' (epoch) and the page's, each oldest first,
// at in unix nanoseconds.
type samplesFile struct {
	Epoch []savedSample `json:"epoch"`
	Page  []savedSample `json:"page"`
}

type savedSample struct {
	At     int64 `json:"at"`
	Landed int64 `json:"landed"`
}

func savedOf(in []sample) []savedSample {
	out := make([]savedSample, 0, len(in))
	for _, x := range in {
		out = append(out, savedSample{x.at.UnixNano(), x.landed})
	}
	return out
}

// samplesOf is the saved samples still in the Window at now and not after it (a clock that went
// back drops them), oldest first, with the newest before the Window kept as the base while the
// Window's own span less than MinSpan (trimWindow).
func samplesOf(in []savedSample, now time.Time) []sample {
	var out []sample
	for _, x := range in {
		at := time.Unix(0, x.At)
		if at.After(now) || (len(out) > 0 && at.Before(out[len(out)-1].at)) {
			continue
		}
		out = append(out, sample{at, x.Landed})
	}
	return trimWindow(out, now)
}

// trimWindow drops the samples older than Window at at, but keeps the newest of them as the
// base while the samples inside the Window span less than MinSpan (or there are none): after a
// gap in the reads the rate is the landings over the gap at once, never a blank tile for
// MinSpan, and once the Window's own samples span MinSpan the base is dropped. On 2026-10-18
// the dashboard came to read nothing while no one looks, and a viewer back after an idle hour
// saw no throughput for ten minutes (cold read of PR 490).
func trimWindow(s []sample, at time.Time) []sample {
	drop := 0
	for drop < len(s) && at.Sub(s[drop].at) > Window {
		drop++
	}
	if drop > 0 && (drop == len(s) || at.Sub(s[drop].at) < MinSpan) {
		drop--
	}
	return s[drop:]
}

// restoreSamples loads the saved samples once, under mu, so the rolling hour carries across a
// restart; the reset rule (a landed below the last) is the add's, as ever.
func (s *Server) restoreSamples(now time.Time) {
	if s.restored || s.LoadSamples == nil || s.SaveSamples == nil {
		return
	}
	s.restored = true
	b, err := s.LoadSamples()
	if err != nil {
		s.logf(now, "samples not loaded: %s; the throughput starts from empty", oneline.Escape(err.Error()))
		return
	}
	var f samplesFile
	if len(b) == 0 || json.Unmarshal(b, &f) != nil {
		return
	}
	s.samples = samplesOf(f.Epoch, now)
	s.pageRate.samples = samplesOf(f.Page, now)
}

// saveSamples saves the samples, under mu, at most every SaveEvery.
func (s *Server) saveSamples(now time.Time) {
	if s.SaveSamples == nil || (!s.savedAt.IsZero() && now.Sub(s.savedAt) < SaveEvery) {
		return
	}
	s.savedAt = now
	b, err := json.Marshal(samplesFile{savedOf(s.samples), savedOf(s.pageRate.samples)})
	if err == nil {
		err = s.SaveSamples(b)
	}
	if err != nil && !s.saveBad {
		s.logf(now, "samples not saved: %s", oneline.Escape(err.Error()))
	}
	s.saveBad = err != nil
}

// sampleLanded adds a sample and is the cards landed per hour over the samples of the
// last Window (one decimal), nil until they span MinSpan, with the minutes they span.
// A landed count lower than the last (a cleared sprint) starts the samples again.
func (s *Server) sampleLanded(at time.Time, landed int64) (*float64, float64) {
	if n := len(s.samples); n > 0 && landed < s.samples[n-1].landed {
		s.samples = s.samples[:0]
	}
	s.samples = trimWindow(append(s.samples, sample{at, landed}), at)
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

// PageTicks is how many times the page has ticked from cache.
func (s *Server) PageTicks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pageTicks
}

// Snapshot is /api/sprint's body now, with the build number, as the default release shows it.
func (s *Server) Snapshot() []byte { return s.SnapshotOf("") }

// SnapshotOf is /api/sprint's body now as release shows it (release.go): "" is the
// default, all is every stream.
func (s *Server) SnapshotOf(release string) []byte {
	build := s.Build()
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.snap
	snap.Build, snap.Stale, snap.MinInterval = build, s.fresh.alarmed, s.Every.Seconds()
	if len(snap.Data) > 0 {
		v := viewOf(snap.Data, release)
		v.Data = fixView(v.Data)
		snap.Data, snap.Release, snap.Current, snap.Releases, snap.ReleaseStreams = v.Data, v.Release, v.Current, v.Releases, v.Streams
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
	for _, n := range []string{"index.html", "app.js", "nunito-800.woff2", "logo-robot-192.png", "logo-robot-384.png", "charts.js", "stream.html", "stream.js"} {
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
	return fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(sig)))
}

func (s *Server) logo() ([]byte, error) {
	if s.Logo == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(s.Logo)
}

// logoType is the logo's media type: a raster image's own magic bytes decide first (a
// webp named .png is image/webp), else its name's extension, else the bytes' own answer.
func logoType(name string, body []byte) string {
	if t := http.DetectContentType(body); strings.HasPrefix(t, "image/") {
		return t
	}
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); strings.HasPrefix(t, "image/") {
		return t
	}
	return http.DetectContentType(body)
}

// svgNS is the svg element's namespace, a name and never fetched.
const svgNS = "http://www.w3.org/2000/svg"

// faviconStyle colours an svg favicon for the browser's light or dark theme.
const faviconStyle = `<style>svg{color:#121417}@media (prefers-color-scheme:dark){svg{color:#eef0f3}}</style>`

var (
	svgRe     = regexp.MustCompile(`(?is)<svg\b([^>]*)>(.*)</svg>`)
	viewBoxRe = regexp.MustCompile(`viewBox\s*=\s*"([^"]+)"`)
)

func isSVG(name string) bool { return strings.EqualFold(filepath.Ext(name), ".svg") }

// svgLogo is an svg logo's drawing, read now: its viewBox (escaped) and its inner
// markup; false when the logo is no svg or has no <svg> element.
func (s *Server) svgLogo() (viewBox, inner string, ok bool) {
	if !isSVG(s.Logo) {
		return "", "", false
	}
	body, err := s.logo()
	if err != nil {
		return "", "", false
	}
	m := svgRe.FindSubmatch(body)
	if m == nil {
		return "", "", false
	}
	viewBox = "0 0 32 32"
	if v := viewBoxRe.FindSubmatch(m[1]); v != nil {
		viewBox = string(v[1])
	}
	return htmlpkg.EscapeString(viewBox), string(m[2]), true
}

// The fix state (docs/SPEC-SPRINT-DASHBOARD.md, "Fix"; the owner, 2026-10-07 5:58-6:01 PM ET:
// "I would like the cards that are awaiting rework to be purple", "between review and
// merging"): the view marks the cards awaiting rework on the copy it serves, so the page draws
// them purple. A dealt card is at fix when its level is fix (the fix level, card
// a-rework-is-priority-fix-bb), or, until where prints that level, when it is an attempt after
// the first (its attempt field, else its id's .w<n>) and not blocker or critical, which keep
// their red. The marks: fix true on the card; a fleet or friends row's fix, its fix cards
// working (where's fix_working when it prints one); a work row's fix, its primaries at fix,
// taken off its working, where a primary sent out again sits (lifecycle: review -> working on
// rework), unless where prints the row's fix itself; priorities' fix list, those primaries,
// off high and low; and the copy's fix, the work rows' summed. Every mark is absent when it
// is zero, so a copy with no card at fix is served exactly as where prints it.

// workAttempt reads a work card's attempt off its id: "ci-03.w2" is 2.
var workAttempt = regexp.MustCompile(`\.w(\d+)$`)

// urgentLevel is a level whose red a card keeps on any attempt: blocker, or a critical,
// set or by weight.
func urgentLevel(level string) bool {
	return level == "blocker" || strings.HasPrefix(level, "critical")
}

// fixCard is what the view reads of a dealt card.
type fixCard struct {
	ID       string `json:"id"`
	Primary  string `json:"primary"`
	Stream   string `json:"stream"`
	Member   string `json:"member"`
	State    string `json:"state"`
	Priority string `json:"priority"`
	Attempt  int    `json:"attempt"`
}

// atFix is whether the card c, whose primary is listed at listed (where's priorities), is at fix.
func (c fixCard) atFix(listed string) bool {
	level := cmp.Or(c.Priority, listed)
	// An explicit priority is authoritative once the server supports the ladder.
	// In particular, keep/high rework policies must not become fix by attempt.
	if c.Priority != "" {
		return c.Priority == "fix"
	}
	if level == "fix" {
		return true
	}
	attempt := c.Attempt
	if m := workAttempt.FindStringSubmatch(c.ID); attempt == 0 && m != nil {
		attempt, _ = strconv.Atoi(m[1]) // ignored: \d+ parses
	}
	return attempt >= 2 && !urgentLevel(level)
}

// cellCount is a table cell's number: the string where prints, or a number.
func cellCount(v any) int {
	switch n := v.(type) {
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n)) // ignored: not a number is none
		return i
	case float64:
		return int(n)
	}
	return 0
}

// cellOf is n written as the cell was: a number stays a number, else the string where prints.
func cellOf(was any, n int) any {
	if _, num := was.(float64); num {
		return n
	}
	return strconv.Itoa(n)
}

// fixView is the copy body with the cards awaiting rework marked (above); body itself when
// nothing is at fix or it is no JSON object.
func fixView(body json.RawMessage) json.RawMessage {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return body
	}
	var cards []map[string]json.RawMessage
	var read []fixCard
	// ignored: no cards, or cards of another shape, is none at fix; the two reads are the same list
	if json.Unmarshal(top["cards"], &cards) != nil || json.Unmarshal(top["cards"], &read) != nil {
		cards, read = nil, nil
	}
	var prio map[string][]string
	_ = json.Unmarshal(top["priorities"], &prio) // ignored: no priorities is every card normal
	listed := map[string]string{}
	for _, level := range slices.Sorted(maps.Keys(prio)) {
		for _, id := range prio[level] {
			if urgentLevel(level) || listed[id] == "" {
				listed[id] = level
			}
		}
	}
	var tables map[string]json.RawMessage
	_ = json.Unmarshal(top["tables"], &tables) // ignored: sprintJSON has read the tables
	rows := map[string]map[string]map[string]any{}
	for _, t := range []string{"work", "fleet", "friends"} {
		var r map[string]map[string]any
		if json.Unmarshal(tables[t], &r) == nil && r != nil {
			rows[t] = r
		}
	}

	working := map[[2]string]int{}            // the fix cards working, by table and row
	primaries := map[string]map[string]bool{} // the primaries at fix, by stream
	fixIDs := slices.Clone(prio["fix"])
	marked := false
	for i, c := range read {
		raw := cards[i]
		if !c.atFix(listed[cmp.Or(c.Primary, c.ID)]) {
			continue
		}
		marked = true
		raw["fix"] = json.RawMessage("true")
		if c.State == "working" {
			if f, ok := strings.CutPrefix(c.Member, "friend."); ok {
				working[[2]string{"friends", f}]++
			} else {
				working[[2]string{"fleet", c.Member}]++
			}
		}
		p := cmp.Or(c.Primary, c.ID)
		if primaries[c.Stream] == nil {
			primaries[c.Stream] = map[string]bool{}
		}
		primaries[c.Stream][p] = true
		if !slices.Contains(fixIDs, p) {
			fixIDs = append(fixIDs, p)
		}
	}
	for _, t := range []string{"fleet", "friends"} {
		for name, row := range rows[t] {
			if _, has := row["fix_working"]; has {
				working[[2]string{t, name}] = cellCount(row["fix_working"])
			}
		}
	}
	for k, n := range working {
		if n > 0 && rows[k[0]][k[1]] != nil {
			rows[k[0]][k[1]]["fix"] = strconv.Itoa(n)
			marked = true
		}
	}
	total := 0
	for stream, row := range rows["work"] {
		if v, has := row["fix"]; has { // where's own count: its columns already leave these out
			total += cellCount(v)
			continue
		}
		n := min(len(primaries[stream]), cellCount(row["working"]))
		if n > 0 {
			row["working"] = cellOf(row["working"], cellCount(row["working"])-n)
			row["fix"] = cellOf(row["working"], n)
			total += n
		}
	}
	if !marked && total == 0 {
		return body
	}
	if cards != nil {
		top["cards"] = mustJSON(cards)
	}
	for t, r := range rows {
		tables[t] = mustJSON(r)
	}
	top["tables"] = mustJSON(tables)
	if total > 0 {
		top["fix"] = mustJSON(total)
	}
	if len(fixIDs) > 0 {
		slices.Sort(fixIDs)
		if prio == nil {
			prio = map[string][]string{}
		}
		for level, ids := range prio {
			if urgentLevel(level) || level == "fix" {
				continue
			}
			prio[level] = slices.DeleteFunc(ids, func(id string) bool { return slices.Contains(fixIDs, id) })
			if len(prio[level]) == 0 {
				delete(prio, level)
			}
		}
		prio["fix"] = fixIDs
		top["priorities"] = mustJSON(prio)
	}
	return mustJSON(top)
}
