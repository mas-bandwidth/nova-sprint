// Package friend is the part of a friend's machinery that nova-sprint's own
// commands use: the harness adapters (Deliverer, NewDeliverer) the seat push
// delivers through, the friend settings and pronouns, the limit and fault
// words a harness's output is read by, and the types of what a friend holds
// (HeldCard, Card, Status) as the sprint server answers and reads them.
//
// The friend daemon is not here. It is nova-tools' `nova-friend run`
// (nova-tools pkg/friend; docs/SPEC-FRIEND.md there): its loop, lanes, inbox,
// outbox, notifications, staging, stop returns, reads, pacing, rate limits
// and the ping/pong machine. The diverged copy of it that lived here was
// unreachable from every nova-sprint command and was deleted in v1.3.6;
// internal/ci/testdata/retired-presence.txt refuses its names.
package friend

import (
	"time"
)

// Window is how long either side waits before it decides the other is gone:
// the coordinator for a session pong, the daemon for a ping. One number on
// both sides (the contract of 2026-10-04: "3 minutes to start").
const Window = 3 * time.Minute
