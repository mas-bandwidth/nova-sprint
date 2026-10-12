package swarm

// Bench slot leases: a bench-wide lease store with shares, reserve, expiry and
// live-pid fencing (docs/SPEC-SWARM.md, "Bench slot leases").
//
// The store is <store>/slots with one directory per lease. A lease directory
// is published by renaming a fully-written staging directory into place, so a
// directory in the store always arrives WITH its `lease` file already inside:
// a concurrent take scanning the store can never meet a half-written take and
// reap it (nova-tools#1868). A lease is never inferred from a count but from
// the directories on disk. Each lease directory holds a file `lease`
// with lines `owner=`, `pid=`, `label=`, `until=<RFC3339>` and optional
// `weight=` / `kind=`. Shares live in
// <store>/shares.tsv with rows `capacity\t<n>`, `reserve\t<n>` and
// `<owner>\t<n>`.
//
// Expiry is fenced by liveness: before granting, take reaps every lease whose
// until= is past AND whose pid is not alive (Alive, signal 0). A lease past
// until= with a live pid is DRIFT: it stays and counts as held, because the
// process it names is still running and its slot is not free. A lease whose
// until= is still ahead and whose pid is gone is stranded: it stays, still
// counts as held, and list prints stranded=1 with the label so a manager can
// re-queue it (nova-tools#2033).

// Stranded reports a live-until lease whose holder is gone: the seat is still
// held, and list prints stranded=1 with the label so a manager can re-queue it.

// State reports live, expired or DRIFT at now: past until= with a live pid is
// DRIFT, past until= with a dead pid is expired, anything else is live.
// Stranded leases stay `live` on this field so a listing fill already knows
// (live, DRIFT, expired) still counts them as held.

// Line is the `slots list` row for this lease.

// loadSlotShares reads <store>/shares.tsv: rows `capacity\t<n>`,
// `reserve\t<n>` and `<owner>\t<n>`. Capacity is required; reserve defaults
// to 0; an owner with no row holds share 0.
// ListSlotLeases reads every lease in the store, in id order. A directory
// with no parseable lease file holds no lease and is skipped: it is a
// half-written take (Mkdir landed, the file never did), and take reaps it.

// publishSlotLease stages a complete lease directory beside the slot store
// and renames it into place, so a directory in slots/ always arrives WITH
// its lease file already inside. The staging directory lives beside slots/,
// never inside it, so no take scanning the store ever sees it, and the rename
// is one atomic step on the store's filesystem.
//
// An id that is already held is reported as an existence error for the
// caller's retry loop: the look-then-rename cannot claim the atomicity a bare
// Mkdir had, but the id carries the owner's nanosecond stamp plus 32 random
// bits, so a clash is a retry, never a silent replace.
// MakeSlotLease writes one lease directory by publish (atomic) for tests and
// for fixtures: the pid and until are the caller's, not the taker's.

// slotHolders renders owner:count pairs in name order for the refusal line.
// TakeSlotLeases grants k unweighted (weight 1) leases. See takeSlotLeases.

// TakeSlotLeasesWaiting requests k weighted leases for owner, waiting up to wait
// duration when capacity is occupied. When wait is 0, it makes a single attempt.
// It checks ctx for cancellation during the wait (docs/SPEC-SWARM.md, "Bench slot leases").

// takeSlotLeases grants k leases to owner when both caps hold after reaping:
// the owner's held+demand stays within its share, and the total held+demand
// stays within capacity-reserve, where demand is k times weight (nova-tools#2033).
// Expired leases with a dead pid are reaped first; expired leases with a live
// pid are DRIFT and stay held. New leases carry the caller's pid and until=now+dur.
//
// IT RETURNS THE IDS IT GRANTED, not a count, and that is deliberate: the
// count was what let a holder release by owner and label instead of by
// identity, and give away a seat that was never its own (nova-tools#1546,
// a friend's hold on PR #1562). len(ids) is the count for anyone who only
// wanted that; taking a lease without learning which one is now impossible.
// Hand the ids back to ReleaseSlotLeasesByID.
// ReleaseSlotLeases removes owner's leases: all of them with all=true, or only the
// ones carrying label otherwise. It reports the removed count and the owner's leases
// still held. It never frees a seat whose holder is still running; see
// ReleaseSlotLeasesForcing.

// ReleaseSlotLeasesForcing is the body, with the live-seat fence spelled out.
//
// A LEASE IS NOT A TICKET SOMEBODY ELSE MAY TEAR UP (issue #1902). Deleting a lease
// does not stop the process holding it: the holder keeps running, keeps spending, and
// the seat it is sitting in is handed to the next taker. A friend freed a live `native`'s
// only seat from outside and watched a second `native` take it -- two cards on a
// capacity-1 bench, both printing NATIVE OK -- and a card given --no-wall did the same
// to a bystander from inside its own shell. `--owner` is an unauthenticated string and
// every owner on a shared bench is the same unix user, so who CALLED release is not a
// fence either. The only fence that means anything is the holder: a lease whose pid is
// ALIVE and is not this process is KEPT and counted in live.
//
// Releasing your OWN lease is always allowed: `run`'s dispatcher and `native`'s cleanup
// give back the seat they are sitting in while their own pid is alive, and that is the
// ordinary end of a run rather than a steal. force is the person's loud override, the
// way --no-wall is the loud way to ask for no containment.

// Under the store lock (issue #1900), so that a release cannot interleave with a
// take's count-then-mkdir and leave the count the grant was made against wrong.

// ReleaseSlotLeasesByID removes EXACTLY the leases named, and only while they are still
// the caller's. It exists because releasing by owner and label is not releasing by
// identity: an owner is a bench and a label is a card's name, and two runs that share both
// -- two slots, two benches, a retry -- would each give away the other's seat
// (nova-tools#1546, a friend's hold on PR #1562). A holder that took leases learns their ids
// from TakeSlotLeases and hands exactly those back here.
//
// The pid is a fence, not bookkeeping. An id whose lease has since been reaped and remade
// by somebody else must not be removed by a stale list, so each lease is RE-READ and left
// alone unless its pid is the one given. An id that is simply gone is not an error: a
// release is allowed to be late, and the caller's job is to stop holding, not to prove
// nobody tidied up first.
//
// `slots release --owner ... --label ...` stays as it is, by owner and label, because a
// PERSON at a prompt wants exactly that: free whatever this owner is holding for that card.
// A person can see the store; a deferred cleanup cannot.

// Under the store lock (issue #1900): the same reason as ReleaseSlotLeases. A store
// this process cannot lock -- one that is already gone, say -- is not a reason to
// refuse to stop holding, so the release goes on unlocked rather than erroring.

// Already gone, or never readable: nothing of ours is held under it.

// Not ours any more. Someone reaped it and took the id, or the list is
// stale. Either way this is not a seat we may give away.

// takeSlotStoreLock serialises the whole grant (issue #1900). os.Mkdir is atomic PER
// ID, which is what the comment at the top of this file says; it is not atomic per
// CAPACITY. Two takers who both read total=0 against a capacity-1 store both counted,
// both passed the share test, and both made a directory with a different name: three
// of four 50-way trials over-granted. The count and the mkdir have to be one
// transaction, and the store is a directory on one bench, so an flock on a file inside
// it is the transaction -- the same primitive, and the same dies-with-its-holder
// property, as the pool's slots.lock.
//
// The lock file is only created inside a store that already exists: a take against a
// store that was never `slots init`ed must still say `shares.tsv: ...` rather than
// quietly making a directory.
// returnCardForLease puts the card an expired lease names back in queue/. The
// taken file is <store>/taken/<owner>-<label>; the rename is what makes the card
// queueable again. A lease with no label names no card, and a card already back
// in queue/ is left alone. It returns the card name when it moved one.
