package friend

import (
	"time"
)

// The coordinator's side of the connection (docs/SPEC-FRIEND.md, "The
// coordinator's ping"): the coordinator is the server, it pings every friend
// row each PingEvery, and a friend is down once DownAfter passes with no pong.
const (
	PingEvery = time.Second
	DownAfter = 10 * time.Second
	RowsEvery = time.Minute // how often the friend rows are read again
)

// What the coordinator knows of a friend's connection.
const (
	PeerUp   = "up"   // a pong to a ping of the last DownAfter
	PeerDown = "down" // none for DownAfter
)

// Change is one friend's connection changing state, the one line the
// keepalive says: never one per ping.
type Change struct {
	Friend   string
	State    string    // PeerUp or PeerDown
	LastPong time.Time // zero when the friend never answered
	Reason   string    // why down: no pong, or the ping could not be sent
}

// Keepalive is the coordinator's state: per friend, the nonces it sent and
// when, and the last pong to one of them. It is stepped with the clock the
// loop reads; the loop owns the transport (cmd/nova-friend serve), so every
// rule is tested with no socket and no real time.
type Keepalive struct {
	DownAfter time.Duration // DownAfter unless a test shortens it
	peers     map[string]*peer
}

type peer struct {
	state string               // "" until the first pong or DownAfter, then PeerUp or PeerDown
	since time.Time            // when the friend row was first read
	last  time.Time            // the last pong, zero before any
	sent  map[string]time.Time // the nonces of the last DownAfter, and when each went
	why   string               // the last ping's failure, "" once one is sent
}
