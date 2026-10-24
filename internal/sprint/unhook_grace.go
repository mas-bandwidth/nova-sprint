package sprint

import (
	"time"

	"github.com/mas-bandwidth/nova-tools/hook"
)

// The unhook grace (tla/FriendUnhookGrace.tla; 2026-10-23). nova-hook unhooks a friend
// whose session left a delivered challenge unanswered for its down-after (5m): her Monitor
// lapsed at its 30-minute limit, or one challenge came while she was deep in a turn. Until
// v1.6.12 the tick's reconcile (LiveReturns) returned every working card and read of an
// unhooked friend at once, and a friend running reads and work in parallel children came
// back a minute later, re-armed, to an empty row: the children's work was thrown away and
// dealt again (Johnny, 2026-10-23: "eight reads left the row"), the main reason the reads
// backed up. The safety is unchanged in kind: a friend who stays unhooked has her working
// cards and reads returned, never stranded; only its clock moves. Her working cards and
// reads stay while she is inside the grace and are returned once she is past it; a
// friend who hooks in again inside it keeps them. Nothing else changes: an unhooked
// friend is down at once, so she is dealt nothing and her ready cards go as a down
// friend's always have, and while she is inside the grace her row is reconciled as any
// row is, by her own fresh beat's live set (LiveGrace), so a card her children no longer
// run still goes back.
//
// Two clocks, both the hook record's own, so a tick that restarts loses nothing:
//   - the unhook: inside the grace while now - UnhookedAt < the grace (the friend-idle
//     setting, FriendIdleAfter, default 20m: the sprint's one bound on a friend holding
//     cards and showing nothing);
//   - her last answer: and while now - AnsweredAt < the grace + hook.ProofTTL. A busy
//     friend's bus sends hold her challenges off, so she proves about once a ProofTTL and
//     her last answer may be half an hour old at a real unhook; a friend whose session
//     re-arms and is unhooked again and again without ever answering would restart the
//     unhook clock each time, and this bound ends that: her cards are returned no later
//     than the grace + ProofTTL after her last answer, however often she flaps.
//
// A record that names no unhook time or no answer is past the grace: its cards are returned
// at once, as before (a record from an older nova-hook, a friend that never proved).
func FriendUnhookInGrace(s *Snapshot, f FriendSeat) bool {
	if !f.Unhooked || f.UnhookedAt.IsZero() || f.AnsweredAt.IsZero() {
		return false
	}
	grace := s.FriendIdleAfter()
	return s.Now.Sub(f.UnhookedAt) < grace && s.Now.Sub(f.AnsweredAt) < UnhookAnswerBound(grace)
}

// UnhookAnswerBound is the second clock's bound for a grace: the grace plus a proof's life.
func UnhookAnswerBound(grace time.Duration) time.Duration { return grace + hook.ProofTTL }
