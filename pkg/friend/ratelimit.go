package friend

import (
	"regexp"
	"strings"
	"time"
)

// A rate limit is not out of funds (docs/SPEC-FRIEND.md, one-shot lanes; the finding of
// 2026-10-05: a rate-limited friend was held down as if out of funds). A rate limit passes
// in a minute; out of funds does not. The daemon that acts on each is nova-tools'
// nova-friend run.

// RateLimited is a lane's answer when the provider rate-limited the turn (a
// 429, "rate limit reached", "too many requests", "input token limit
// exceeded"): the turn's card stays in hand, counted toward nothing, and the
// lanes back off.
type RateLimited struct{ Session, Reason string }

func (r RateLimited) Error() string {
	return "the provider rate-limited the turn in session " + r.Session + ": " + r.Reason
}

// OutOfFunds is a lane's answer when the provider refused for want of funds
// or credit (a 402, insufficient balance): the lanes hold until the daemon
// restarts, and the coordinator is told once.
type OutOfFunds struct{ Session, Reason string }

func (o OutOfFunds) Error() string {
	return "the provider is out of funds for session " + o.Session + ": " + o.Reason
}

// UsageLimited is a lane's answer when the harness account itself is at its
// usage limit (a Claude Code rate_limit_event rejected, adapter_claude.go):
// unlike a rate limit it names its reset, so the lanes stop taking until
// Until and no cap is lowered; the card stays in
// the lane's hand, counted toward nothing.
type UsageLimited struct {
	Session, Reason string
	Until           time.Time
}

func (u UsageLimited) Error() string {
	return "the harness is at its usage limit until " + u.Until.UTC().Format(time.RFC3339) + " in session " + u.Session + ": " + u.Reason
}

var (
	rateLimitLine = regexp.MustCompile(`(?i)(?:status|code|error|http)[^\n0-9]{0,24}429\b|\b429\b[^\n0-9]{0,8}(?:too many|rate)|rate[ _-]?limit[ _-]?(?:ed|error|reached|exceeded)\b|too many requests|input[ _-]tokens?[ _-](?:per[ _-]minute[ _-])?limit[ _-](?:exceeded|reached)|tokens per min`)
	fundsLine     = regexp.MustCompile(`(?i)(?:status|code|error|http)[^\n0-9]{0,24}402\b|payment required|insufficient (?:balance|funds|[a-z ]{0,20}credits)|out of (?:funds|credits?)\b|credit balance is too low`)
)

// ProviderLimit reads the tail of a lane turn's output (the last LimitTail
// bytes: a harness says it last) for a rate limit or out of funds, out of
// funds first: OutOfFunds, RateLimited, or nil. A line with its reset beside
// it ("Insufficient AI Credits ... will refresh 6:52 PM") is the harness's
// own limit (down until the reset, then woken), never out of funds
// here.
func ProviderLimit(session, out string) error {
	tail := out
	if len(tail) > LimitTail {
		tail = tail[len(tail)-LimitTail:]
	}
	var rate string
	for _, line := range strings.Split(tail, "\n") {
		line = stripANSI(line)
		if fundsLine.MatchString(line) {
			if lim, found := ReadLimit(line, time.Time{}); found && lim.Limited {
				continue // a reset beside it: the harness's limit
			}
			return OutOfFunds{Session: session, Reason: oneLine(line, 200)}
		}
		if rate == "" && rateLimitLine.MatchString(line) {
			rate = oneLine(line, 200)
		}
	}
	if rate != "" {
		return RateLimited{Session: session, Reason: rate}
	}
	return nil
}
