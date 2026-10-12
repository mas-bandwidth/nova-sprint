package friend

import (
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
)

// PushRenewEvery is how often the daemon renews its friend's push proof on
// the bus while the session stays up: well inside bus.PushFresh, so a live
// daemon's proof never reads stale, and a dead one's does within PushFresh.
// PushWriteBudget bounds one write of it.
const (
	PushRenewEvery  = time.Minute
	PushWriteBudget = 5 * time.Second
)

// PushProver records the friend's inbox push proof on the bus (bus.PushKey;
// docs/SPEC-BUS.md, bus-requires-inbox-push-proof) from the daemon's
// presence, as the SessionCheck saves it: nova-bus names shows it, and send
// and recv say it as a NOTE beside a message to or from a name without one,
// never a refusal. The proof is the SESSION CHECK round trip: the check
// carried into the session by the deliver adapter and the session's pong
// carrying its nonce (SessionCheck, presence.go). While the presence is up
// the proof is up and renewed every PushRenewEvery; when the presence is
// down (a check unanswered within its bound, or a daemon that has not yet
// been answered) the proof is written down at once, so a sender is told
// the moment the daemon knows. A passive harness (no deliver command: the
// check only goes on the stream) pushes nothing into the session and is
// written down whatever its pongs say.
type PushProver struct {
	Friend, Harness string
	Store           bus.Store // the store itself, never the daemon's (DaemonStore)
	Deliver         Deliverer // the adapter the check goes in by
	Now             func() time.Time
	Record          func(line string) // nil records nothing

	mu      sync.Mutex
	asked   string    // the nonce the latest check carried, until it is answered
	nonce   string    // the nonce the session last answered
	proven  time.Time // when the daemon saw that answer
	answers int
	wrote   bool // a proof has been written by this daemon
	up      bool
	reason  string
	wroteAt time.Time
}

// passiveReason is the down proof of a harness with no deliver command.
const passiveReason = "the harness has no deliver adapter, so nothing pushes into the session"
