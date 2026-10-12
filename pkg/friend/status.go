package friend

import (
	"fmt"
	"time"
)

// A friend's status, from evidence (docs/SPEC-FRIEND.md, "A friend's status,
// from evidence"). The owner, 2026-10-04: "are friends actually doing work?"
// The daemon's beat says only that the daemon's loop runs; the status is
// decided by FriendStatus from what the friend did, in order, and the
// evidence is shown beside it.

// AnswerBound is how old the session's last answer may be while the friend
// is up: two windows, a ping each window and a challenge open for less than
// one, so a session answering every ping is never older than this.
const AnswerBound = 2 * Window

// LimitFile is the friend's limit, in the state directory: the provider's
// limit the session is at and when it resets. The daemon is its one writer;
// no file is no limit.
const LimitFile = "limit.json"

// The harness evidence: whether the harness's process was seen in the
// process table. Advisory, shown and never deciding: a session run from its
// command line has no app to see, and an app that runs answers nothing.
// HarnessNotSeen is defined in alive.go ("not-seen").
const (
	HarnessUnknown = ""
	HarnessRunning = "running"
)

// Evidence is what a friend's status is decided from.
type Evidence struct {
	Harness     string    // HarnessRunning, HarnessNotSeen or HarnessUnknown; shown, never deciding
	DaemonUp    bool      // the daemon's status file is fresh; shown, never deciding up
	LastAnswer  time.Time // the session's last pong; zero is never
	Limit       string    // the limit's name, when the provider said one
	LimitUntil  time.Time // the limit's reset; zero, or past, is no limit
	Undelivered int       // messages waiting on the friend's stream; negative is not counted
	BusBlocked  string    // why the bus cannot deliver to the friend; empty when it can
	LastResult  time.Time // the end of the last turn; zero is none yet
	LastExit    int       // that turn's exit
}

// Verdict is a friend's status, the one reason that decided it, and every
// piece of evidence as a person reads it.
type Verdict struct {
	Status   string   `json:"status"` // up or down
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence"`
}

// Ago is a duration as a person reads it on the table: 40s, 12m, 3h5m, 2d.
func Ago(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// resultTail is how much of the end of the log LastResult reads.
const resultTail = 64 << 10
