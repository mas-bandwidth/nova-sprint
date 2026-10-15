package friend

import (
	"time"
)

// Presence is whether the friend's SESSION is alive, never its daemon
// (docs/SPEC-FRIEND.md, presence). The finding of 2026-10-04: three friends
// read up with eight cards each while their harness apps were not running,
// because the daemon answered every ping itself. The proof is the session's:
// after SessionQuiet with no bus message from the session, the daemon
// delivers a session check carrying a fresh nonce through the adapter, the
// way a card goes in; only the session's own reply carrying that nonce
// counts; SessionBound with none and the friend is down, NoSessionAnswer; the
// next answer, or any bus message the session writes, brings it back up. The
// daemon's pong stays the daemon's and never counts.
const (
	SessionQuiet = 10 * time.Minute
	SessionBound = 5 * time.Minute
	// ProveEvery is how long after the last check went in the next goes in while the
	// session is up, whatever else it says on the bus, timed from the ask: the sprint
	// server counts only an answered check, for fifteen
	// minutes, so a session that answers within SessionBound is proved again before
	// the last proof lapses (ProveEvery + SessionBound < fifteen minutes).
	ProveEvery = 8 * time.Minute
	// ReaskAfter is how long an unanswered check waits before it is asked again when the
	// session has not read it: a queueing harness keeps every copy, so a check is asked
	// again early only once the session has read the last (Presence.Read).
	ReaskAfter = time.Hour

	NoSessionAnswer = "no session answer"
	NotYetAnswered  = "no session answer yet" // the daemon started; nothing has answered

	SessionCheckPrefix = "SESSION CHECK "
	PresenceFile       = "presence.json"

	LogBatch = 1000 // log entries read per step; the rest the next
)

// The presence, as the presence file says it.
const (
	PresenceUp   = "up"
	PresenceDown = "down"
)

// PresenceStatus is the presence file: what status reads.
type PresenceStatus struct {
	Friend    string    `json:"friend"`
	At        time.Time `json:"at"`
	Presence  string    `json:"presence"` // PresenceUp or PresenceDown
	Reason    string    `json:"reason,omitempty"`
	LastHeard time.Time `json:"last_heard"`
	Nonce     string    `json:"nonce,omitempty"`
	Asked     time.Time `json:"asked"`
	Checks    int       `json:"checks"`
	Answers   int       `json:"answers"`
	Answered  string    `json:"answered,omitempty"` // the nonce the session last answered
}

// StaleTurnLock is how long the turn lock may be held while a headless
// adapter's own turn record says no turn runs before the session check goes in
// by the record (docs/SPEC-FRIEND.md, presence: the headless case): one step's
// race with a turn on its way into the adapter is never read as a stale lock.
const StaleTurnLock = time.Minute

// TurnRecord is a headless adapter's own record of its turns: one that delivers
// by running a one-shot program into the session (dsh headless, gemini
// --resume), each turn a process that starts and ends. A turn runs while one has
// begun and not ended, since the latest's start.
type TurnRecord interface {
	TurnUnderWay() (running bool, since time.Time)
}

// ReadOnReturn is an adapter whose delivery returns exit 0 only once the session
// has taken the text: a headless turn that ran (dsh, gemini, opencode run), or a
// message the session marked read (antigravity's read.json). A check it delivered
// is read, and may be asked again on the check cadence.
type ReadOnReturn interface{ ReadOnReturn() }

func (*DSH) ReadOnReturn()         {}
func (*Gemini) ReadOnReturn()      {}
func (*OpenCode) ReadOnReturn()    {}
func (*Antigravity) ReadOnReturn() {}

// BeatWords are a beat's proof words (nova-sprint friend beat --run, --check,
// --pong): the daemon's run, the check it put into the session, and the check
// its session answered, each "" when there is none to say. The sprint server
// counts an answer only when it names a check this run asked (sprint.ProveBeat).
type BeatWords struct {
	Run, Check, Pong string
	// StopReturns is how many stop-returns the lanes owe (stop.go, OwedStopReturns): the
	// beat carries it (friend beat --stop-returns) while it is above zero.
	StopReturns int
}
