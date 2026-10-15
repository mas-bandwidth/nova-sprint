package friend

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A harness at its usage limit or out of credits is down until its reset,
// and woken after it (docs/SPEC-FRIEND.md, a harness at its limit; the
// friend's side in docs/SPEC-SPRINT.md, friend down). The finding of 2026-10-04: a friend's harness
// stopped on "Insufficient AI Credits ... will refresh 6:52 PM" while her row
// read up with six working cards, and four Claude accounts ran out of their
// weekly usage unseen.

// LimitTail is how much of the end of a command's output is read for a
// limit line: a harness says it last, and the body of a reply that only
// talks about limits is far from the tail.
const LimitTail = 2048

// DefaultLimitWait is how long a harness that refused at its limit and
// named no reset is down before a wake is tried.
const DefaultLimitWait = time.Hour

// Usage is a harness's measured utilization of its five-hour and weekly
// windows, each a fraction (1 is the window spent), with when each resets
// and when it was measured (zero: never).
type Usage struct {
	FiveHour, SevenDay             float64
	FiveHourResets, SevenDayResets time.Time
	At                             time.Time
}

// Limit is what a command's output says of the harness's limit: Limited,
// until when and why; Usage when the output measured it; Overage when the
// harness is spending paid overage (a limit only if the owner does not allow
// it).
type Limit struct {
	Limited bool
	Until   time.Time
	Reason  string
	Usage   Usage
	Overage bool
}

var (
	// limitWords is a line that says the harness is at its limit or out of
	// credits; a line counts only with its reset beside it (limitReset).
	limitWords = regexp.MustCompile(`(?i)insufficient [a-z ]{0,20}credits|out of (?:ai )?credits|credits? (?:exhausted|ran out)|usage limit|limit reached|hit your (?:[a-z-]+ )?limit|quota (?:exceeded|exhausted)`)
	limitEpoch = regexp.MustCompile(`(?i)limit reached\|(\d{10})\b`)
	limitAt    = regexp.MustCompile(`(?i)(?:refresh(?:es)?|resets?|try again|available again)\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*([ap])\.?m\b`)
	limitAfter = regexp.MustCompile(`(?i)(?:refresh(?:es)?|resets?|try again|available again)\s+in\s+(\d{1,3})\s*(hours?|hrs?|h|minutes?|mins?|m)\b`)
)

// rateLimitEvent is Claude Code's stream-json rate_limit_event, printed per
// run with --output-format stream-json --verbose: the status (allowed,
// allowed_warning, rejected), paid overage, and each window's utilization
// (0 to 1) and reset (epoch seconds); an older shape names one window.
type rateLimitEvent struct {
	Type string `json:"type"`
	Info struct {
		Status         string   `json:"status"`
		ResetsAt       int64    `json:"resetsAt"`
		RateLimitType  string   `json:"rateLimitType"`
		Utilization    *float64 `json:"utilization"`
		IsUsingOverage bool     `json:"isUsingOverage"`
		Windows        map[string]struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    int64   `json:"resetsAt"`
		} `json:"unifiedWindows"`
	} `json:"rate_limit_info"`
}

// ReadLimit reads a command's output at now for the harness's limit: the
// last rate_limit_event anywhere in it (Claude Code), else a limit line in
// its tail with the reset beside it (a clock time, today or else tomorrow
// in now's zone or the zone it names, on the date it names; "in N hours";
// an epoch after "limit reached|"; resetOfText). found is
// whether the output said anything of the limit at all. A provider's
// transient rate_limit_error is no limit (ProviderRefusal passes it).
func ReadLimit(out string, now time.Time) (lim Limit, found bool) {
	if strings.Contains(out, `"rate_limit_event"`) {
		for _, line := range strings.Split(out, "\n") {
			var ev rateLimitEvent
			if !strings.Contains(line, `"rate_limit_event"`) || json.Unmarshal([]byte(line), &ev) != nil || ev.Type != "rate_limit_event" {
				continue
			}
			lim, found = claudeLimit(ev, now), true
		}
		if found {
			return lim, true
		}
	}
	tail := out
	if len(tail) > LimitTail {
		tail = tail[len(tail)-LimitTail:]
	}
	for _, line := range strings.Split(tail, "\n") {
		if !limitWords.MatchString(line) {
			continue
		}
		if until, ok := resetOfText(stripANSI(line), now); ok {
			lim = Limit{Limited: true, Until: until, Reason: oneLine(line, 200)}
			found = true
		}
	}
	return lim, found
}

// resetOf is the reset a limit line names.
func resetOf(line string, now time.Time) (time.Time, bool) {
	if m := limitEpoch.FindStringSubmatch(line); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64) // ignored: ten digits always parse
		return time.Unix(n, 0).In(now.Location()), true
	}
	if m := limitAt.FindStringSubmatch(line); m != nil {
		h, _ := strconv.Atoi(m[1]) // ignored: digits always parse
		mins := 0
		if m[2] != "" {
			mins, _ = strconv.Atoi(m[2]) // ignored: digits always parse
		}
		if h < 1 || h > 12 || mins > 59 {
			return time.Time{}, false
		}
		h %= 12
		if strings.EqualFold(m[3], "p") {
			h += 12
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), h, mins, 0, 0, now.Location())
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t, true
	}
	if m := limitAfter.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1]) // ignored: digits always parse
		unit := time.Minute
		if strings.HasPrefix(strings.ToLower(m[2]), "h") {
			unit = time.Hour
		}
		return now.Add(time.Duration(n) * unit), true
	}
	return time.Time{}, false
}

// claudeLimit is a rate_limit_event as a Limit: its windows the usage; a
// rejection, or overage, down until the spent window resets (the latest of
// the windows at 1, else the five-hour window's, else the event's own reset,
// else DefaultLimitWait).
func claudeLimit(ev rateLimitEvent, now time.Time) Limit {
	in := ev.Info
	u := Usage{At: now}
	if in.Utilization != nil && in.RateLimitType != "" && len(in.Windows) == 0 {
		in.Windows = map[string]struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    int64   `json:"resetsAt"`
		}{in.RateLimitType: {Utilization: *in.Utilization, ResetsAt: in.ResetsAt}}
	}
	var spent []string
	var until time.Time
	for name, w := range in.Windows {
		reset := time.Time{}
		if w.ResetsAt > 0 {
			reset = time.Unix(w.ResetsAt, 0)
		}
		switch name {
		case "five_hour":
			u.FiveHour, u.FiveHourResets = w.Utilization, reset
		case "seven_day":
			u.SevenDay, u.SevenDayResets = w.Utilization, reset
		}
		if w.Utilization >= 1 {
			spent = append(spent, name)
			if reset.After(until) {
				until = reset
			}
		}
	}
	if until.IsZero() {
		until = u.FiveHourResets
	}
	if until.IsZero() && in.ResetsAt > 0 {
		until = time.Unix(in.ResetsAt, 0)
	}
	if !until.After(now) {
		until = now.Add(DefaultLimitWait)
	}
	lim := Limit{Usage: u, Overage: in.IsUsingOverage, Until: until}
	why := "usage limit"
	if len(spent) > 0 {
		slices.Sort(spent)
		why += " (" + strings.Join(spent, ", ") + " spent)"
	}
	switch {
	case in.Status == "rejected":
		lim.Limited, lim.Reason = true, "claude rate_limit_event rejected: "+why
	case in.IsUsingOverage:
		lim.Reason = "claude is spending paid overage: " + why
	}
	return lim
}
