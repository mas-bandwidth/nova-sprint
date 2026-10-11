---------------------------- MODULE DirtyTickRead ----------------------------
\* The tick's read (internal/sprint/store/twin.go, pipeline_load.go,
\* engine.go's Run): how a part of the tick reads the sprint from its twin
\* instead of reading the four tables whole, and why what it plans on is the
\* store's state at one generation of the fence. Written for PR 4906 (the
\* owner's requirement of 2026-09-30, "the whole intent is sub-second ticks").
\*
\* THE STORE.
\*   gen, pend    the sprint's fence: its generation, and the operation in
\*                flight ("none", or "w": another writer's). Every write of a
\*                record takes the fence at the generation its plan read
\*                (Acquire: gen' = gen + 1, only if gen is the one read).
\*   rec          each table's records (a value per key; 0 is none)
\*   rev          each table's revision: every write of the table moves it,
\*                a record's or a display cell's (outside the fence)
\*   log          each table's change stream: one event per write, the
\*                revision it left and the records it named (a display write
\*                names none)
\*   running      the machine's state, read with the fence
\*   runseq       the machine's run: bumped by Start (the fence taken), the
\*                generation a part records when it begins and must still hold
\*                to commit (engine.go's StaleRun refusal)
\*   stopRevoked  a run's STOP has committed (engine.go:595-605): a StartsWork
\*                or ReportsWork part is refused, and a RequiresStopped part
\*                is refused while it is not; the tick part (kind "plain")
\*                halts on its first read of a STOPPED machine instead
\*                (BeganRunning). A Stop sets it, a Start clears it; the
\*                epoch's initial stopped setup state would leave it FALSE,
\*                which this model does not reach.
\*   epoch        the sprint's epoch; an outside Clear moves it and refuses
\*                every writer still pinned to the old one (engine.go:566).
\*   queued       the work table's queue length: a step other than the pump
\*                queues its work-table changes while the machine runs; a
\*                STOPPED machine's queue is drained before any step
\*                (engine.go:607-619, Drain).
\*
\* THE TICK'S TWIN.
\*   tv           the twin holds the tables (a read cut short, or a dropped
\*                twin, holds none: its next read reads them whole)
\*   tg           the generation its last closed read or commit was at
\*   trec, trev   the records it holds and the revision of each table it is at
\*
\* A PART OF THE TICK (pc).
\*   pkind        the part's kind: "plain" (the tick's read: it halts on a
\*                first read that finds the machine STOPPED, and it holds no
\*                pinned epoch, so a clear while it planned repins it and it
\*                reads again), "start" (StartsWork: refused once StopRevoked),
\*                "report" (ReportsWork: refused once StopRevoked), or
\*                "require" (RequiresStopped: refused while StopRevoked is
\*                FALSE). A worker part (start, report, require) pins the
\*                epoch it began under and is refused when it moved.
\*   vepoch       the epoch the part's read began at (pinned at Begin): a
\*                worker part is refused, writing nothing, when it moved
\*                (OldEpochPartWritesNothing); a plain part repins it and
\*                reads again (engine.go:555-562).
\*   "idle"  -> Begin: the view's exchange (the fence first: a pending
\*              operation waits; the shapes: each table's revision, and the
\*              part's kind and pinned epoch). A part's FIRST read that finds
\*              the machine STOPPED halts the part (it begins nothing); a part
\*              that begins records the runseq it began under; a later read of
\*              a part in flight goes on.
\*   "view"  -> CatchUp(t): a table whose revision moved since the twin's is
\*              brought to the shape's revision: the records its change stream
\*              names between the two, read again (refused, and read again
\*              from Begin, when the table moved after the shape: movedError).
\*              ReadWhole: a twin that holds no tables reads them whole, at
\*              the shapes' revisions, all or none (the two pipelined stages
\*              and the trailing fence of pipeline_load.go, one step here).
\*              Close: the fence again, last: the same generation and nothing
\*              pending, or the read is taken again.
\*   "plan"  -> Commit: the part's write, refused first (writing nothing) when
\*              its pinned epoch moved (a worker part) or a fence on its kind
\*              holds or the run it began under moved (StaleRun), repinned
\*              first (a plain part, which holds no epoch: it reads again), or
\*              else at the generation its view read (the Acquire), its
\*              receipt applied to the twin (the record, and the table's
\*              revision the store left); or Lost: the generation moved, and
\*              the part reads again.
\*   PassOver: a part with nothing to do on the twin is passed over, reading
\*              no store (it writes nothing: nothing to check).
\*   Drop: any path the engine does not know the end of drops the twin.
\*   Lock: a part that lost a try takes the fence before its next read
\*              (lock.go): nothing else writes until its write, handed the
\*              fence, commits, or NoWrite releases it.
\*
\* THE OTHER WRITERS. W: another process's step (Acquire, its write, its
\* Release; its write names its record in the change stream). D: a display
\* cell, outside the fence (the table's revision moves; no record changes;
\* no generation). Stop / Start: the machine's state, each read and written
\* under the fence (the generation steps), Stop revoking and Start bumping
\* runseq. Clear: the sprint's epoch, advanced by an outside clear.
\*
\* WHAT IS CHECKED.
\*   ViewIsSnapshot   a part that plans, at the generation the store is at
\*                    with nothing pending, plans on the store's records
\*   TwinIsTheStore   a twin at the store's generation, nothing pending,
\*                    holds the store's records
\*   BeganRunning     no tick part (kind "plain") writes that found the
\*                    machine STOPPED at its first read
\*   TwinNotAhead     the twin is never at a revision the table has not reached
\*   LockedApplyNotLost  a part that plans under its lock still holds the fence
\*                    at the generation it read: its write cannot be lost
\*   NoStaleRunCommit no part commits under a runseq other than the one current
\*                    at its commit (a part that began before STOP then START
\*                    reads the new generation but is refused StaleRun)
\*   OldEpochPartWritesNothing  a worker part whose pinned epoch moved writes
\*                    nothing (a plain part repins and reads again)
\*   StartsWorkNotAfterStop     no StartsWork part commits once StopRevoked

EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Tables, Keys, Vals, MaxW, MaxD, MaxParts, MaxStarts, MaxEpoch, MaxQ,
          Broken \* "none"; a reversed witness breaks one rule: "lock" (the
                 \* lock marks the part locked without taking the fence),
                 \* "acquire" (the
                 \* write takes the fence whatever generation its plan read,
                 \* the engine's one guard), "halt" (no halt on a first
                 \* read of a STOPPED machine), "catchup" (the catch-up misses
                 \* the last write its change stream names), "runseq" (the
                 \* commit drops the runseq check: a part whose run moved under
                 \* it commits anyway), "epoch" (the plan drops the epoch
                 \* check: a part commits at an epoch it did not pin),
                 \* "startsafterstop" (a StartsWork part commits after a stop
                 \* revoked it)

ASSUME 0 \in Vals

VARIABLES gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued,
          locked, lost, stale,
          tv, tg, trec, trev,
          pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts,
          wroteOldEpoch, wroteAfterStop,
          wn, dn, wt, wk, wv

vars == <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued, locked, lost, stale, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wroteOldEpoch, wroteAfterStop, wn, dn, wt, wk, wv>>

Zero == [t \in Tables |-> [k \in Keys |-> 0]]

Init ==
  /\ gen = 0 /\ pend = "none" /\ rec = Zero /\ rev = [t \in Tables |-> 0]
  /\ log = [t \in Tables |-> <<>>] /\ running = TRUE /\ runseq = 0
  /\ stopRevoked = FALSE /\ epoch = 0 /\ queued = 0
  /\ tv = FALSE /\ tg = 0 /\ trec = Zero /\ trev = [t \in Tables |-> 0]
  /\ pc = "idle" /\ pkind = "plain" /\ vepoch = 0 /\ vgen = 0 /\ vrunseq = 0 /\ vrev = [t \in Tables |-> 0]
  /\ first = TRUE /\ began = TRUE /\ parts = 0
  /\ locked = FALSE /\ lost = FALSE /\ stale = FALSE
  /\ wroteOldEpoch = FALSE /\ wroteAfterStop = FALSE
  /\ wn = 0 /\ dn = 0 /\ wv = 0
  /\ wt = (CHOOSE t \in Tables : TRUE)
  /\ wk = (CHOOSE k \in Keys : TRUE)

\* the records a table's change stream names between two revisions
Named(t, from, to) ==
  UNION {log[t][i][2] : i \in {j \in 1..Len(log[t]) : log[t][j][1] > from /\ log[t][j][1] <= to}}

--------------------------------------------------------------------------------
\* THE TICK'S PART

\* the fence is free for the part: nothing in flight, or the part's own lock
Free == pend = "none" \/ (locked /\ pend = "t")

Begin ==
  /\ pc = "idle" /\ parts < MaxParts /\ Free
  /\ IF first
       THEN \* a new part pins the epoch and run it begins under, and chooses
            \* its kind
            /\ pkind' \in {"plain", "start", "report", "require"}
            /\ vepoch' = epoch /\ vrunseq' = runseq /\ began' = running
            /\ IF ~running /\ pkind' = "plain" /\ Broken # "halt"
                 THEN \* a tick part's first read finds the machine STOPPED: it
                      \* begins nothing (a worker part does not halt: its fence
                      \* refuses it)
                      /\ parts' = parts + 1
                      /\ UNCHANGED <<pc, vgen, vrev, first>>
                 ELSE /\ pc' = "view" /\ vgen' = gen /\ vrev' = rev
                      /\ first' = FALSE
                      /\ UNCHANGED parts
       ELSE \* a part in flight reads again: its kind, pinned epoch and run are
            \* fixed (pinned at its first read)
            /\ pc' = "view" /\ vgen' = gen /\ vrev' = rev
            /\ UNCHANGED <<pkind, vepoch, vrunseq, began, parts, first>>
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tv, tg, trec, trev, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

CatchUp(t) ==
  /\ pc = "view" /\ tv /\ trev[t] # vrev[t]
  /\ IF rev[t] = vrev[t]
        THEN \* the records named since the twin's revision, read at the shape's
             /\ trec' = [trec EXCEPT ![t] = [k \in Keys |-> IF k \in Named(t, trev[t], IF Broken = "catchup" THEN vrev[t] - 1 ELSE vrev[t]) THEN rec[t][k] ELSE trec[t][k]]]
             /\ trev' = [trev EXCEPT ![t] = vrev[t]]
             /\ UNCHANGED pc
        ELSE \* the table moved after its shape: read again
             /\ pc' = "idle" /\ UNCHANGED <<trec, trev>>
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tv, tg, vgen, vrunseq, vrev, first, began, parts, pkind, vepoch, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

ReadWhole ==
  /\ pc = "view" /\ ~tv
  /\ IF \A t \in Tables : rev[t] = vrev[t]
        THEN /\ tv' = TRUE /\ trec' = rec /\ trev' = vrev /\ UNCHANGED pc
        ELSE /\ pc' = "idle" /\ UNCHANGED <<tv, trec, trev>>
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tg, vgen, vrunseq, vrev, first, began, parts, pkind, vepoch, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

Close ==
  /\ pc = "view" /\ tv /\ \A t \in Tables : trev[t] = vrev[t]
  /\ IF gen = vgen /\ Free
        THEN pc' = "plan" /\ tg' = vgen
        ELSE pc' = "idle" /\ UNCHANGED tg
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tv, trec, trev, vgen, vrunseq, vrev, first, began, parts, pkind, vepoch, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

\* The part's write. First the epoch: a clear while the part planned repins a
\* plain part, which holds no epoch, and reads again (engine.go:555-562); a
\* worker part pinned to the moved epoch is refused, writing nothing
\* (engine.go:566-575, ClearedError). Then a fence on its kind (engine.go:595-605)
\* and the run it began under (engine.go:582-588, StaleRun) refuse it, else it
\* commits at the generation its view read, else it is lost. "epoch" drops the
\* epoch check; "startsafterstop" drops the StartsWork fence; "runseq" drops the
\* run check.
Commit(t, k, v) ==
  /\ pc = "plan"
  /\ IF vepoch # epoch /\ Broken # "epoch"
        THEN /\ IF pkind = "plain"
                  THEN \* repin: a plain part adopts the epoch the sprint is at
                       \* and reads again
                       /\ vepoch' = epoch /\ pc' = "idle"
                       /\ UNCHANGED <<gen, pend, rec, rev, log, tg, trec, trev, parts, first, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>
                  ELSE \* a worker part pinned to the moved epoch: refused,
                       \* writing nothing, and the part is done
                       /\ pc' = "idle" /\ parts' = parts + 1 /\ first' = TRUE
                       /\ locked' = FALSE /\ lost' = FALSE
                       /\ pend' = IF locked THEN "none" ELSE pend
                       /\ UNCHANGED <<vepoch, gen, rec, rev, log, tg, trec, trev, stale, wroteOldEpoch, wroteAfterStop>>
        ELSE IF \/ (pkind = "start" /\ stopRevoked /\ Broken # "startsafterstop")
                 \/ (pkind = "report" /\ stopRevoked)
                 \/ (pkind = "require" /\ ~stopRevoked)
             THEN \* refused (a fence on its kind holds): writing nothing, the
                  \* part is done
                  /\ pc' = "idle" /\ parts' = parts + 1 /\ first' = TRUE
                  /\ locked' = FALSE /\ lost' = FALSE
                  /\ pend' = IF locked THEN "none" ELSE pend
                  /\ UNCHANGED <<vepoch, gen, rec, rev, log, tg, trec, trev, stale, wroteOldEpoch, wroteAfterStop>>
             ELSE IF (gen = vgen /\ Free) \/ Broken = "acquire"
                  THEN /\ IF (runseq = vrunseq) \/ Broken = "runseq"
                           THEN \* the Acquire at the generation read (or, locked,
                                \* the lock handed to the operation: the
                                \* generation is the one the lock set); the run it
                                \* began under still current; the write; the
                                \* receipt on the twin; the fence released
                                /\ gen' = IF locked THEN gen ELSE gen + 1
                                /\ rec' = [rec EXCEPT ![t][k] = v]
                                /\ rev' = [rev EXCEPT ![t] = rev[t] + 1]
                                /\ log' = [log EXCEPT ![t] = Append(log[t], <<rev[t] + 1, {k}>>)]
                                /\ trec' = [trec EXCEPT ![t][k] = v]
                                /\ trev' = [trev EXCEPT ![t] = rev[t] + 1]
                                /\ tg' = IF locked THEN gen ELSE gen + 1
                                /\ pend' = IF locked THEN "none" ELSE pend
                                /\ locked' = FALSE /\ lost' = FALSE
                                /\ pc' = "idle" /\ parts' = parts + 1 /\ first' = TRUE
                                /\ stale' = (stale \/ (runseq # vrunseq))
                                /\ wroteOldEpoch' = (wroteOldEpoch \/ (vepoch # epoch))
                                /\ wroteAfterStop' = (wroteAfterStop \/ (pkind = "start" /\ stopRevoked))
                                /\ vepoch' = vepoch
                           ELSE \* the run moved under the part: its commit is
                                \* refused (StaleRun), it writes nothing, and the
                                \* part is done
                                /\ pc' = "idle" /\ parts' = parts + 1 /\ first' = TRUE
                                /\ pend' = IF locked THEN "none" ELSE pend
                                /\ locked' = FALSE /\ lost' = FALSE
                                /\ UNCHANGED <<vepoch, gen, rec, rev, log, tg, trec, trev, stale, wroteOldEpoch, wroteAfterStop>>
                  ELSE \* lost to another writer: the part reads again (it began:
                       \* no halt), and takes the fence before that read (Lock)
                       /\ pc' = "idle" /\ lost' = TRUE
                       /\ UNCHANGED <<vepoch, gen, pend, rec, rev, log, tg, trec, trev, parts, first, locked, stale, wroteOldEpoch, wroteAfterStop>>
  /\ UNCHANGED <<running, runseq, stopRevoked, epoch, queued, tv, vgen, vrunseq, vrev, began, pkind, wn, dn, wt, wk, wv>>

\* A part that lost a try takes the fence before its next read: once, and
\* only while nothing else holds it (lock.go). The broken witness marks the
\* part locked without taking the fence.
Lock ==
  /\ pc = "idle" /\ lost /\ ~locked /\ pend = "none"
  /\ locked' = TRUE
  /\ IF Broken = "lock"
        THEN UNCHANGED <<gen, pend>>
        ELSE pend' = "t" /\ gen' = gen + 1
  /\ UNCHANGED <<rec, rev, log, running, runseq, stopRevoked, epoch, queued, lost, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, dn, wt, wk, wv, stale, wroteOldEpoch, wroteAfterStop>>

\* A part with nothing to write ends; a lock it holds is released unwritten.
NoWrite ==
  /\ pc = "plan"
  /\ pc' = "idle" /\ parts' = parts + 1 /\ first' = TRUE
  /\ locked' = FALSE /\ lost' = FALSE
  /\ pend' = IF locked THEN "none" ELSE pend
  /\ UNCHANGED <<gen, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tv, tg, trec, trev, vgen, vrunseq, vrev, began, pkind, vepoch, wn, dn, wt, wk, wv, stale, wroteOldEpoch, wroteAfterStop>>

PassOver ==
  /\ pc = "idle" /\ first /\ ~locked /\ parts < MaxParts
  /\ parts' = parts + 1
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

Drop ==
  /\ pc = "idle" /\ tv
  /\ tv' = FALSE
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

--------------------------------------------------------------------------------
\* THE OTHER WRITERS

WAcquire(t, k, v) ==
  /\ pend = "none" /\ wn < MaxW
  /\ pend' = "w" /\ gen' = gen + 1 /\ wt' = t /\ wk' = k /\ wv' = v
  /\ UNCHANGED <<rec, rev, log, running, runseq, stopRevoked, epoch, queued, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, dn, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

WApply ==
  /\ pend = "w"
  /\ rec' = [rec EXCEPT ![wt][wk] = wv]
  /\ rev' = [rev EXCEPT ![wt] = rev[wt] + 1]
  /\ log' = [log EXCEPT ![wt] = Append(log[wt], <<rev[wt] + 1, {wk}>>)]
  /\ pend' = "applied"
  /\ UNCHANGED <<gen, running, runseq, stopRevoked, epoch, queued, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

WRelease ==
  /\ pend = "applied"
  /\ pend' = "none" /\ wn' = wn + 1
  /\ UNCHANGED <<gen, rec, rev, log, running, runseq, stopRevoked, epoch, queued, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

Display(t) ==
  /\ dn < MaxD
  /\ rev' = [rev EXCEPT ![t] = rev[t] + 1]
  /\ log' = [log EXCEPT ![t] = Append(log[t], <<rev[t] + 1, {}>>)]
  /\ dn' = dn + 1
  /\ UNCHANGED <<gen, pend, rec, running, runseq, stopRevoked, epoch, queued, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

Stop ==
  /\ running /\ pend = "none"
  /\ running' = FALSE /\ stopRevoked' = TRUE /\ gen' = gen + 1
  /\ UNCHANGED <<pend, rec, rev, log, runseq, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, epoch, queued, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

Start ==
  /\ ~running /\ pend = "none" /\ runseq < MaxStarts
  /\ running' = TRUE /\ stopRevoked' = FALSE /\ gen' = gen + 1 /\ runseq' = runseq + 1
  /\ UNCHANGED <<pend, rec, rev, log, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, epoch, queued, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

\* The epoch the outside clear advances (engine.go:566): every writer still
\* pinned to the old epoch is refused by the tables.
Clear ==
  /\ epoch < MaxEpoch
  /\ epoch' = epoch + 1
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, queued, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

\* A step other than the pump queues its work-table change while the machine
\* runs (engine.go:607: fence.Queued). Queued is the length of that queue.
Queue ==
  /\ running /\ queued < MaxQ
  /\ queued' = queued + 1
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

\* A STOPPED machine has no next tick: the queue it left is drained before any
\* step (engine.go:607-619).
Drain ==
  /\ ~running /\ queued > 0
  /\ queued' = 0
  /\ UNCHANGED <<gen, pend, rec, rev, log, running, runseq, stopRevoked, epoch, tv, tg, trec, trev, pc, pkind, vepoch, vgen, vrunseq, vrev, first, began, parts, wn, dn, wt, wk, wv, locked, lost, stale, wroteOldEpoch, wroteAfterStop>>

Next ==
  \/ Begin \/ ReadWhole \/ Close \/ PassOver \/ Drop \/ Lock \/ NoWrite
  \/ \E t \in Tables : CatchUp(t)
  \/ \E t \in Tables, k \in Keys, v \in Vals : Commit(t, k, v)
  \/ \E t \in Tables, k \in Keys, v \in Vals : WAcquire(t, k, v)
  \/ WApply \/ WRelease
  \/ \E t \in Tables : Display(t)
  \/ Stop \/ Start \/ Clear \/ Queue \/ Drain

Spec == Init /\ [][Next]_vars

--------------------------------------------------------------------------------
\* INVARIANTS

TypeOK ==
  /\ pc \in {"idle", "view", "plan"}
  /\ pend \in {"none", "w", "applied", "t"}
  /\ locked \in BOOLEAN /\ lost \in BOOLEAN /\ stale \in BOOLEAN
  /\ tv \in BOOLEAN /\ running \in BOOLEAN /\ stopRevoked \in BOOLEAN
  /\ runseq \in 0..MaxStarts /\ vrunseq \in 0..MaxStarts
  /\ pkind \in {"plain", "start", "report", "require"}
  /\ epoch \in 0..MaxEpoch /\ vepoch \in 0..MaxEpoch
  /\ queued \in 0..MaxQ
  /\ wroteOldEpoch \in BOOLEAN /\ wroteAfterStop \in BOOLEAN

\* A part that plans, at the generation the store is at with nothing pending,
\* plans on the store's records.
ViewIsSnapshot ==
  (pc = "plan" /\ gen = vgen /\ Free) => trec = rec

\* A part that plans under its lock plans at the generation it read with the
\* fence still its own: its write cannot be lost to another writer.
LockedApplyNotLost ==
  (pc = "plan" /\ locked) => (gen = vgen /\ pend = "t")

\* A twin at the store's generation, nothing pending, holds the store's
\* records (after its part closed its read, or committed).
TwinIsTheStore ==
  (tv /\ pc = "idle" /\ tg = gen /\ pend = "none") => trec = rec

\* No tick part (kind "plain") writes that found the machine STOPPED at its
\* first read.
BeganRunning == (pc = "plan" /\ pkind = "plain") => began

\* The twin is never at a revision its table has not reached.
TwinNotAhead == \A t \in Tables : trev[t] <= rev[t]

\* No part commits under a runseq other than the one current at its commit:
\* a part that began before STOP then START reads the new generation but is
\* refused (StaleRun), never committed.
NoStaleRunCommit == ~stale

\* A worker part whose pinned epoch moved is refused and writes nothing: no
\* commit records a write the part planned under an epoch the store has left.
OldEpochPartWritesNothing == ~wroteOldEpoch

\* No StartsWork part commits once a stop revoked it: the fence refuses it.
StartsWorkNotAfterStop == ~wroteAfterStop

=============================================================================
