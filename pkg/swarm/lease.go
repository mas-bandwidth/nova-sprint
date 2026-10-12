package swarm

// THE JOB LEASE: the launcher's own word that a card is alive, and the job directory's
// ownership.
//
// The bench's hygiene pass has to decide, hourly, which job directories are finished work
// and which are a card still doing its job. Deciding by SILENCE -- no new bytes in
// harness-output.log for fifteen minutes -- is wrong, because a card in one long model call
// or one long compile is silent and working; that heuristic once deleted <slot>/data and
// <slot>/tmp, a running card's HOME and TMPDIR, out from under two certify passes.
//
// A launcher knows what a heuristic can only guess, so it says so on disk: `nova-swarm
// native` writes <job>/.lease before the child starts, carrying the launcher's pid, and
// heartbeats the file's mtime while the child runs. The reaper reads exactly this: a job
// whose lease names a live pid, or whose heartbeat is younger than its stale window, is
// LIVE and is never touched. The file is removed when the run ends, so a finished job
// leaves nothing behind that pretends to be alive.
//
// IT IS ALSO THE ADMISSION TO THE JOB DIRECTORY. Two `native` runs were given one physical
// <slot>/jobs/<label>: the bench store gave each its own seat, but the job directory, the
// data home, the temp directory and the logs under it were one set of paths, and the first
// run to exit removed the other's lease -- which the other's heartbeat, a bare Chtimes,
// could not put back. SPEC-SWARM is not ambiguous about whether that is lawful: under
// **Slots**, a worker has "its own data home", "its own job directory", and "a slot is held
// by exactly one worker"; under **the races, taken out**, two workers on one data home is
// the `database is locked` failure, closed on purpose. So two live runs in one
// job directory is never a thing to make safe -- it is a thing to refuse.
//
// FOUR RULES HOLD IT, AND THE FIRST TWO ARE A FRIEND'S (her HOLD on the first repair, which
// still admitted two launchers in two demonstrated ways):
//
//  1. OWNERSHIP IS PUBLISHED WHOLE. The lease is written to a temp file in the same
//     directory, flushed to the platform, and LINKED into place -- so the name .lease never
//     exists holding a partial record. The first repair created the file with O_EXCL and
//     wrote afterwards, and a competitor that read the file in that window saw an empty
//     record, called it pid 0, called pid 0 dead, and took the path out from under a
//     creator whose own write then went to an unlinked inode.
//  2. AN INCOMPLETE RECORD IS NEVER EVIDENCE OF A DEAD OWNER. A lease that cannot be parsed
//     -- empty, truncated, half a line -- is HELD BY AN UNKNOWN OWNER, and stays held until
//     its heartbeat is older than JobLeaseStale. Only then is it reclaimed.
//  3. FAILING TO ESTABLISH OWNERSHIP IS A REFUSAL, NEVER A SILENT SUCCESS. This file does not
//     hand back a do-nothing release when it could not write, on the grounds that the
//     reaper's other rules still protected the job. That was defensible while the lease was
//     only advice to a reaper. It is not defensible now that the lease is what keeps two
//     launchers out of one directory: `.lease` as a directory, an unwritable job directory
//     or an unreadable record all ended with BOTH runs proceeding. Every one of them
//     is now an error, and `native` exits 2 on it with a remedy. See SPEC-SWARM, "One live
//     run per job directory".
//  4. THE RELEASE IS FENCED AND JOINED. A release removes the lease only while the file is
//     still the one this run published -- pid AND a per-run nonce -- and only after the
//     heartbeat has stopped, so a tick already selected cannot write the lease back after
//     the run that owned it has ended.
//
// The lease and its temp file are opened O_NOFOLLOW, like the capture beside it: the job
// directory is the card's own writable place, and a symlink planted there by an earlier run
// of the same card would carry this process's write out of the wall.

import (
	"os"

	"time"
)

// JobLeaseName is the lease file's name inside the job directory. The reaper knows it by
// this name and by nothing else.
const JobLeaseName = ".lease"

// providerBeatRenews decides one heartbeat tick (feature 87). Before the beat file has
// ever been seen (lastBeat zero, file absent) the tick renews: the card may still be
// connecting. Once the file exists, the tick renews only when its mtime has moved past
// the last one seen, and the new mtime is returned so the next tick compares against
// it. A beat file that vanishes after being seen renews nothing.

// touchProviderBeat moves the beat file's mtime to now, creating the file first when it
// is not there yet. Chtimes alone on an absent file fails silently, which left the
// heartbeat with no file to read when the first usage sample already showed turns.

// ignored: the file was only created to exist; Chtimes below reports any failure on it

// JobLease is what a lease file says. Beat is the file's mtime, which is the heartbeat, and
// Known says whether the record parsed at all: an incomplete record names no owner and is
// never read as a dead one.
type JobLease struct {
	PID     int
	Host    string
	Label   string
	Nonce   string
	Started string
	Beat    time.Time
	Known   bool
	Path    string
	// info is the file the record was read from. It is what os.SameFile answers on, and
	// it is how a reclamation proves that what it took off the path is the same FILE it
	// judged -- not a newer one another run published in between.
	info os.FileInfo
}

// String is the one line a refusal names a holder with.

// Live reports whether this lease still speaks for a running launcher.
//
// THE KERNEL IS ASKED WHEREVER IT CAN BE: a pid on this host that is gone is gone, and the
// next run of that card takes the directory over rather than waiting out a window for a
// launcher that crashed. Two records cannot be asked about -- one written on another host,
// and one that did not parse -- and for those the heartbeat is the only word there is, so
// they are HELD until they are older than JobLeaseStale. That is the reaper's second
// clause, and it is here for the same reason the reaper has it: a liveness file whose owner
// cannot be checked is not thereby a dead owner.

// HeldJobLease answers the holder a take was refused for, when that is why it was refused.
// It is here rather than at the caller so a launcher needs no errors.As of its own.

// ReadJobLease reads the lease in jobDir. A job directory with no lease answers an error
// satisfying errors.Is(err, os.ErrNotExist). A path that is THERE but is not a regular file
// -- a directory, a symlink, a socket -- is an error of its own: this process cannot tell
// whether that job directory is held, and rule 3 says it must not guess.

// parseJobLease reads the record. A pid line that is not a positive number leaves Known
// false, and an unknown record is never read as a dead owner (rule 2).

// StartJobLease takes the lease on jobDir for the running child and returns the release.
//
// It REFUSES rather than returning a release that protects nothing. A *JobLeaseHeldError
// says another run holds the directory -- the same-path exclusion -- and any
// other error says this run could not establish ownership at all, which under rule 3 is the
// same refusal with a different reason. The caller exits on either, before it has written
// anything the holder owns.
//
// The release is safe to call more than once. It stops the heartbeat, WAITS for it, and
// then removes the file only while the file is still this run's -- pid and nonce both.

// startJobLeaseTicking is the whole of it, with the heartbeat's clock handed in.

// THE PROVIDER-BYTE RULE (feature 87). The lease renews only when the
// supervisor has observed provider bytes, signalled by a .provider-beat
// file the supervisor touches on each usage sample that shows new output.
// A hung socket sends no bytes, the beat file never moves, and the lease
// expires rather than being kept alive by a blind timer.

// Renew the mtime only when provider bytes have arrived. Before
// the first provider-beat file appears, renew on every tick (the
// card may still be connecting). Once the beat file exists, renew
// only when its mtime advances -- a hung socket means no advance.
// THE REPAIR ALWAYS RUNS: if Chtimes fails (file missing), the
// lease is re-published regardless of the beat, because a live job
// that lost its file must be protected again.

// file is there, just not time to renew

// THE REPAIR. Chtimes on a path that is not there does nothing and says
// nothing, which is how a live job lost its protection. Publish
// the same lease again: if somebody else now holds the path, this is
// refused and their lease is left exactly alone.
// ignored: a heartbeat repair retried every tick; a refusal means another owner holds the path, which is left alone by design

// THE JOIN, before the remove. A tick already selected can be inside its own
// repair; without this wait it would publish the lease again AFTER the run
// that owned it had ended, and leave a finished job looking alive.

// publishJobLease puts a COMPLETE lease at path and answers nil only when this call now owns
// it. A *JobLeaseHeldError means somebody else does; any other error means this process
// could not establish ownership and the caller must refuse (rule 3).

// THE PUBLICATION IS THE LINK. It fails if the name exists, which makes it the
// atomic claim, and what it puts there is a record that was already whole and
// already on the platform -- so no reader ever sees a partial lease (rule 1).

// ignored: the temp name is removed whether the link landed or not; the link error below is the one returned

// it went away between the link and the read; claim it again

// The holder is gone. Clear EXACTLY the record this call judged -- see
// takeJobLeaseRecord for why that is a rename and never an unlink by path -- and
// claim the path again. A reclamation that took something else puts it back and
// says nothing was cleared, which sends this loop round to read the winner.
// A reclamation that took something else puts it back and clears nothing; going
// round the loop reads whoever won and reports THEM as the holder, by name.

// writeJobLeaseTemp writes the whole record to a file beside the lease and flushes it, so
// that what the link publishes is complete on disk and not only in this process.

// ignored: a close on the failure path; the write error is the one returned

// ignored: a best-effort cleanup of the temp file; the write error is the one returned

// ignored: a close on the failure path; the sync error is the one returned

// ignored: a best-effort cleanup of the temp file; the sync error is the one returned

// ignored: a best-effort cleanup of the temp file; the close error is the one returned

// reclaimable says whether this record's owner is gone, and by WHICH of the two rules --
// the two are kept apart on purpose. A PROVEN-DEAD owner is a pid this kernel was asked
// about and answered for: nothing about elapsed time enters it, and a live pid is never
// reclaimed however old its heartbeat. An UNKNOWN owner -- a record that did not parse, or
// one written on a host whose pids this kernel cannot be asked about -- is recovered only
// by age, and only after the reaper's own stale bound.

// takeJobLeaseRecord clears EXACTLY the record it was handed, and answers whether the path
// is now free of it.
//
// NEVER AN UNLINK BY PATH AFTER A READ. A read, a compare and then
// `os.Remove(path)` leaves a window that atomic publication does not close: two reclaimers
// can both judge one stale record, the first clears it and links its own live lease into
// place, and the second's remove -- aimed at a PATH, decided from a record that is no
// longer there -- unlinks the winner. So the removal is a RENAME to a tombstone nobody
// else's name collides with, which takes whatever is at the path in one step, and the
// record is judged AFTERWARDS, on the file in hand:
//
//   - it is the file that was judged (os.SameFile, or the same pid+nonce when a platform
//     gave no identity) -- clear it, and the path is free;
//   - it is anything else, or a reclamation finds it ALIVE -- put it straight back with a
//     link, which fails only if somebody has already published there, and answer that
//     nothing was cleared.
//
// The one residual is a restore that cannot land because another run published in the
// meantime: then the record this call lifted is genuinely superseded and dropping it is
// right. If the restore fails for any other reason the owner's own heartbeat publishes its
// lease again (rule 5), which is what that repair is for.

// Nothing is there: somebody else cleared it, and the path is free of the record
// this call was handed either way.

// AND IT MUST STILL BE ABANDONED. Elapsed age never steals a claim whose pid this
// kernel can see alive, so a record that came back to life between the judgement
// and the rename goes back where it was.

// ignored: the tomb is this take's own rename target; a leftover tomb holds no claim on the path

// PUT IT BACK. This is the case the rename exists for.

// The restore could not land and nobody has published: the owner's heartbeat
// republishes (rule 5). Nothing here may pretend the path is free.
// ignored: the tomb is this take's own rename target; a leftover tomb holds no claim on the path

// ignored: the tomb is this take's own rename target; a leftover tomb holds no claim on the path

// releaseOwnJobLease removes this run's own lease, and only while the file on disk is still
// the record this run published -- pid AND nonce. It answers whether the path is free of it.

// sameJobLeaseRecord says whether two reads are the same FILE. Identity first, because a
// newly published lease is always a different inode; the record's own pid and nonce are the
// fallback for a platform whose FileInfo cannot answer.

// readJobLeaseFile reads one lease record from an exact path, which is what a tombstone is.

// newLeaseNonce is what tells two runs apart when their pids cannot: the same process
// taking the lease twice, and a pid the operating system has re-issued.

// THE SLOT LEASE. The bench store's lease is a COUNT: it says an owner is
// holding n seats, and it does not say WHICH slot directory a seat is. So two `native`
// runs with the same --slot and DIFFERENT --label each took a seat, each took its own
// job lease -- whose refusal is about one job directory, and these are two -- and both
// started against one `<slot>/data`, which is one HOME, one cache and one opencode.db.
// SPEC-SWARM under **Slots** says a worker has "its own data home" and "a slot is held by
// exactly one worker"; under **the races, taken out**, two workers on one data home IS the
// `database is locked` failure, closed on purpose.
//
// The slot lease is the job lease pointed at the slot directory, and it is the same file
// format, the same four rules and the same pid-and-nonce fence -- published whole, an
// unparseable record held rather than assumed dead, a failure to establish ownership a
// refusal, and a release that only removes a record that is still this run's. There is no
// second spelling of ownership on this bench.
//
// It is taken AFTER the job lease so that two runs sharing one job directory keep saying
// exactly what the job lease says. The label it carries is this run's, so the refusal can
// name the card that is holding the slot.
