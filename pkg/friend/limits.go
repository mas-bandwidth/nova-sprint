package friend

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What a harness says when it is at its usage limit or out of credits, one
// parser per harness (docs/SPEC-FRIEND.md, limits-mean-down-w-r.w1~15: a
// usage limit or an empty balance marks the friend down until the reset).
// The texts are in testdata/limits.tsv, one line each; a text not recognised
// is an ordinary failed turn.

var (
	resetEpochAt = regexp.MustCompile(`"resets_at"\s*:\s*(\d{10})\b`)
	resetSeconds = regexp.MustCompile(`"resets_in_seconds"\s*:\s*(\d+)`)
	resetStamp   = regexp.MustCompile(`(?i)(?:reset|resets|until|retry|regain access)\w*\s+(?:on\s+|at\s+)?(\d{4}-\d\d-\d\dT[0-9:.]+(?:Z|[+-]\d\d:\d\d))`)
	resetDateAt  = regexp.MustCompile(`(?i)regain access on (\d{4}-\d\d-\d\d) at (\d\d:\d\d) UTC`)
	// resetClock is a clock time with the date and the zone the provider names beside it:
	// "resets 8pm (America/New_York)", "resets Oct 10 at 5am (America/New_York)"
	resetClock    = regexp.MustCompile(`(?i)(?:resets?|refresh(?:es)?|try again|available again)\s+(?:on\s+|at\s+)?(?:(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*([ap])\.?m\b\.?(?:\s*\(([A-Za-z][A-Za-z0-9_+\-]*(?:/[A-Za-z0-9_+\-]+)*)\))?`)
	resetDuration = regexp.MustCompile(`(?i)(?:reset|resets|retry|try again|refresh|available again)\w*\s+(?:after|in)\s+((?:\d+\s*(?:days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)[\s,]*(?:and\s+)?)+)`)
	durationPart  = regexp.MustCompile(`(?i)(\d+)\s*(days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)`)
	durationUnits = map[byte]time.Duration{'d': 24 * time.Hour, 'h': time.Hour, 'm': time.Minute, 's': time.Second}
)

// resetOfText is the reset a line names, after now: an epoch or seconds in a
// 429 body, a stamp, a date and a time in UTC, a clock time with the month and
// day and the zone when it names them (clockReset), a duration ("after
// 3h12m5s", "in 2 days 3 hours"), or a bare clock time (resetOf).
func resetOfText(line string, now time.Time) (time.Time, bool) {
	var until time.Time
	switch {
	case resetEpochAt.MatchString(line):
		n, _ := strconv.ParseInt(resetEpochAt.FindStringSubmatch(line)[1], 10, 64) // ignored: ten digits always parse
		until = time.Unix(n, 0).In(now.Location())
	case resetSeconds.MatchString(line):
		n, _ := strconv.ParseInt(resetSeconds.FindStringSubmatch(line)[1], 10, 64) // ignored: digits always parse
		until = now.Add(time.Duration(n) * time.Second)
	case resetDateAt.MatchString(line):
		m := resetDateAt.FindStringSubmatch(line)
		until, _ = time.Parse("2006-01-02 15:04", m[1]+" "+m[2]) // ignored: the pattern is the layout; a zero time is no reset
	case resetStamp.MatchString(line):
		until, _ = time.Parse(time.RFC3339Nano, resetStamp.FindStringSubmatch(line)[1]) // ignored: a text that is no time is no reset
	case resetClock.MatchString(line):
		var ok bool
		if until, ok = clockReset(resetClock.FindStringSubmatch(line), now); !ok {
			return time.Time{}, false
		}
	case resetDuration.MatchString(line):
		var d time.Duration
		for _, p := range durationPart.FindAllStringSubmatch(resetDuration.FindStringSubmatch(line)[1], -1) {
			n, _ := strconv.Atoi(p[1]) // ignored: digits always parse
			d += time.Duration(n) * durationUnits[strings.ToLower(p[2])[0]]
		}
		until = now.Add(d)
	default:
		until, _ = resetOf(line, now)
	}
	return until, until.After(now)
}

var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

// clockReset is resetClock's match as the instant it names, after now: the
// clock time in the zone the text names (now's zone when it names none; a zone
// that does not load is no reset), on the month and day it names, else today;
// a time that has passed is tomorrow, a date that has passed next year.
func clockReset(m []string, now time.Time) (time.Time, bool) {
	loc := now.Location()
	if m[6] != "" {
		var err error
		if loc, err = time.LoadLocation(m[6]); err != nil {
			return time.Time{}, false
		}
	}
	h, _ := strconv.Atoi(m[3]) // ignored: digits always parse
	mins := 0
	if m[4] != "" {
		mins, _ = strconv.Atoi(m[4]) // ignored: digits always parse
	}
	if h < 1 || h > 12 || mins > 59 {
		return time.Time{}, false
	}
	h %= 12
	if strings.EqualFold(m[5], "p") {
		h += 12
	}
	local := now.In(loc)
	if m[1] == "" {
		t := time.Date(local.Year(), local.Month(), local.Day(), h, mins, 0, 0, loc)
		if !t.After(now) {
			t = time.Date(local.Year(), local.Month(), local.Day()+1, h, mins, 0, 0, loc)
		}
		return t, true
	}
	month := months[strings.ToLower(m[1])]
	day, _ := strconv.Atoi(m[2]) // ignored: digits always parse
	t := time.Date(local.Year(), month, day, h, mins, 0, 0, loc)
	if t.Month() != month || t.Day() != day {
		return time.Time{}, false // Feb 30 is no date
	}
	if !t.After(now) {
		t = time.Date(local.Year()+1, month, day, h, mins, 0, 0, loc)
	}
	return t, true
}
