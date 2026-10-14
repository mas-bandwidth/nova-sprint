package main

// A seat verb says when a release window is open, and when the server is back
// (docs/SPEC-SPRINT.md, the release window; tla/ReleaseWindow.tla). The seat
// play writes one JSON file when the window opens and removes it when the
// window ends or is rolled back: path ${NOVA_RELEASE_WINDOW_FILE:-$HOME/.nova/
// release-window.json}; shape {"open":true,"reason":"adopt v1.2.24",
// "opened":"2026-10-14T03:00:00Z","until":"2026-10-14T03:30:00Z",
// "back_in_s":300}. `until` is the play's own upper bound. The file is advice
// for a human or an agent reading a refusal; no verb's behaviour, exit code or
// write depends on it.
//
// A verb that fails to reach the store (a refused dial, a timeout, "the store
// did not answer") reads the sentinel once, after the failure, never before
// and never on success. ReadReleaseWindow is the one read: it parses the file
// and returns the window and a state, one of none, open, stale or unreadable.
// The refusal then says, in place, whether the outage is the planned window
// (open), an expired sentinel that is no longer advice (stale), or nothing
// (none or unreadable, so an unreadable file never speaks for the play).

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// releaseWindowState is what ReadReleaseWindow found: none (no open window),
// open (the window is open and now is before until), stale (now is not before
// until), or unreadable (the file cannot be read or parsed as the shape).
type releaseWindowState string

const (
	releaseWindowNone       releaseWindowState = "none"
	releaseWindowOpen       releaseWindowState = "open"
	releaseWindowStale      releaseWindowState = "stale"
	releaseWindowUnreadable releaseWindowState = "unreadable"
)

// ReleaseWindow is the sentinel file's shape. until is the parsed Until, zero
// for a window that is none or unreadable; it is not written to JSON.
type ReleaseWindow struct {
	Open    bool   `json:"open"`
	Reason  string `json:"reason"`
	Opened  string `json:"opened"`
	Until   string `json:"until"`
	BackInS int64  `json:"back_in_s"`

	until time.Time
}

// releaseWindowEnv is the variable that names the sentinel's path.
const releaseWindowEnv = "NOVA_RELEASE_WINDOW_FILE"

// ReadReleaseWindow reads the sentinel at path and judges it at now: open when
// the file parses, open is true and now is before until; stale when now is not
// before until; none when the file is absent or open is false; unreadable when
// it cannot be read or parsed (a directory, malformed JSON, an unparsable
// until). The returned ReleaseWindow carries the parsed facts for the note.
func ReadReleaseWindow(path string, now time.Time) (ReleaseWindow, releaseWindowState) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ReleaseWindow{}, releaseWindowNone
		}
		return ReleaseWindow{}, releaseWindowUnreadable
	}
	var w ReleaseWindow
	if err := json.Unmarshal(b, &w); err != nil {
		return ReleaseWindow{}, releaseWindowUnreadable
	}
	if !w.Open {
		return w, releaseWindowNone
	}
	until, err := time.Parse(time.RFC3339, w.Until)
	if err != nil {
		return w, releaseWindowUnreadable
	}
	w.until = until
	if now.Before(until) {
		return w, releaseWindowOpen
	}
	return w, releaseWindowStale
}

// releaseWindowFile is the sentinel's path: NOVA_RELEASE_WINDOW_FILE, else
// $HOME/.nova/release-window.json.
func (a *app) releaseWindowFile() string {
	if a.getenv != nil {
		if v := a.getenv(releaseWindowEnv); v != "" {
			return v
		}
	}
	home := ""
	if a.getenv != nil {
		home = a.getenv("HOME")
	}
	if home == "" && a.home != nil {
		if h, err := a.home(); err == nil {
			home = h
		}
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	return filepath.Join(home, ".nova", "release-window.json")
}

// readReleaseWindow is the sentinel's read after a store-open failure, once.
// It is readReleaseWindowFn when a test sets one (to count the reads with no
// file and no clock), else ReadReleaseWindow of the path releaseWindowFile
// names. now is a.now, the app's clock, else the wall clock (a bare app).
func (a *app) readReleaseWindow() (ReleaseWindow, releaseWindowState) {
	read := a.readReleaseWindowFn
	if read == nil {
		read = ReadReleaseWindow
	}
	now := time.Now()
	if a.now != nil {
		now = a.now()
	}
	return read(a.releaseWindowFile(), now)
}

// releaseWindowBackIn is the whole seconds until the window's until, from now,
// at least one: the "about N s" the refusal says.
func releaseWindowBackIn(w ReleaseWindow, now time.Time) int {
	s := int(math.Ceil(w.until.Sub(now).Seconds()))
	if s < 1 {
		s = 1
	}
	return s
}

// releaseWindowUntil is until as the note says it: HH:MM:SSZ, the play's own
// upper bound.
func releaseWindowUntil(w ReleaseWindow) string {
	return w.until.UTC().Format("15:04:05Z")
}

// releaseWindowOpenNote is the open window's refusal prefix, one line ending
// in a space: refuse prints it in place of nothing, the original refusal
// unchanged after it (docs/SPEC-SPRINT.md, the release window).
func releaseWindowOpenNote(w ReleaseWindow, now time.Time) string {
	return "RELEASE WINDOW OPEN: " + w.Reason + "; the server is expected back in about " +
		strconv.Itoa(releaseWindowBackIn(w, now)) + " s (until " + releaseWindowUntil(w) +
		"); this is planned, not a failure; retry then. "
}

// releaseWindowStaleNote is the stale sentinel's one added note: an expired
// sentinel never says planned, so the outage may be a failure.
func releaseWindowStaleNote(w ReleaseWindow) string {
	return "release window file is stale (until " + releaseWindowUntil(w) +
		" passed): the outage may be a failure; run: nova-sprint where\n"
}

// releaseWindowJSON is the same facts as the open note, as the --json
// `release_window` object refuse prints beside the refusal.
func releaseWindowJSON(w ReleaseWindow, now time.Time) string {
	b, _ := json.Marshal(map[string]any{
		"release_window": map[string]any{
			"open":      true,
			"reason":    w.Reason,
			"until":     w.Until,
			"back_in_s": releaseWindowBackIn(w, now),
		},
	})
	return string(b) + "\n"
}

// releaseWindowError is the store-open failure a release window was read for:
// refuse splits its Error on the field separator and prints the note before
// the refusal (and, with --json, the object), the original error unchanged.
type releaseWindowError struct {
	err      error
	note     string // the open prefix (ends in a space) or the stale note (ends in \n)
	jsonLine string // the --json object line, "" when the verb asked none
}

func (e *releaseWindowError) Error() string {
	return e.note + releaseWindowSep + e.jsonLine + releaseWindowSep + e.err.Error()
}

func (e *releaseWindowError) Unwrap() error { return e.err }

// releaseWindowSep separates the note, the JSON line and the original error in
// a releaseWindowError's Error, split back apart by refuse.
const releaseWindowSep = "\x00"

// storeOpenErr reads the sentinel once, after the store-open failure, and
// returns the error for refuse to print: wrapped with the note for open or
// stale, unchanged for none or unreadable (docs/SPEC-SPRINT.md, the release
// window).
func (a *app) storeOpenErr(err error, jsonOut bool) error {
	w, st := a.readReleaseWindow()
	switch st {
	case releaseWindowOpen:
		now := time.Now()
		if a.now != nil {
			now = a.now()
		}
		var jsonLine string
		if jsonOut {
			jsonLine = releaseWindowJSON(w, now)
		}
		return &releaseWindowError{err: err, note: releaseWindowOpenNote(w, now), jsonLine: jsonLine}
	case releaseWindowStale:
		return &releaseWindowError{err: err, note: releaseWindowStaleNote(w)}
	default:
		return err
	}
}
