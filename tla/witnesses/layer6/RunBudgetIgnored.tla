------------------------------ MODULE RunBudgetIgnored ------------------------------
\* REVERSED WITNESS: Select the entire FIFO, including moves beyond this tick's budget.
\* Must violate BudgetKept.
EXTENDS Naturals, Sequences, FiniteSets, TLC

(***************************************************************************
 Layer 6's chooser, FIFO tick and pusher; see LAYER-6-RUN sections 2, 8, 9.
 Ops are numbered by first FIFO admission, not by card id. This bounded
 instance uses one move per packed atomic op (store owns multi-write atomicity).
 A report changes a card gen between fold and apply. A refused op goes back
 through the next L3 fold/replan; run continues the independently packed
 ops in order. Run never splits members of a packed atomic op.

 AtomicChooser=FALSE implements the coordinator ruling: check session health
 at tick start, stop after observing loss, but permit the check/use window.
 At most one owns the PG advisory lock. The old process may still Apply or
 push before observing loss; gen/condition fences are the store contract.
 RunLockCheckRace violates the deliberately STRONGER CommitUnderCurrentLock
 property, recording the accepted limitation rather than an implementation
 bug. AtomicChooser=TRUE is only a comparison contract, not implemented.

 Fold/Select/Refuse/EndTick compose the L3 scheduling interface: L3 owns
 ordering, budgets, packing and carry. Run applies returned ops in order,
 passes Plan.Carry back in the next inbox, and invokes edges after receipts.

 Restart starts with an empty inbox and replans unfinished store-derived
 work. This small instance enumerates that work in a fixed snapshot order;
 it makes NO claim to recover pre-crash volatile FIFO history. External L4
 facts must be requeryable by stable (kind, card, sha): steps reemit queries
 for awaiting cards. L4 tests that assumption; this module does not prove it.

 Init is the store the run comes up on, not a set-up sprint: up starts the
 run service at step 4 of its 8, so a run may take the lock and tick before
 the walk is done. InitiallyStopped=TRUE is a fresh store (stopped, reason
 "never started", store.FreshStop), as every store begins; started records
 the coordinator's first Start, and nothing moves before it
 (NeverStartedMovesNothing). InitiallyStopped=FALSE is a store whose sprint
 was started before this run came up (a restart, a failover).

 MaxFailures bounds crashes/lock losses/hook failures; MaxReports bounds
 reports. Liveness assumes eventual stability, fair scheduling, successful
 sends and a nonzero budget. It does not prove the one-second/two-tick bar.
 SendOK means exit 0 AND a confirmed message id for the intended recipient.
 It is not proof of remote human processing. Failed/unconfirmed sends may
 stutter indefinitely; liveness assumes eventual confirmation.
***************************************************************************)
CONSTANTS Processes, Recipients, WorkCount, CardCount, Budget,
          MaxReports, MaxFailures, AtomicChooser, MaxStops, InitiallyStopped
NoOwner == "none"
Jobs == 1..WorkCount
Cards == 1..CardCount
AllJobs == [i \in Jobs |-> i]
Card(j) == 1 + ((j - 1) % CardCount)
Elems(s) == {s[i] : i \in 1..Len(s)}
Min(a,b) == IF a < b THEN a ELSE b
Max(a,b) == IF a > b THEN a ELSE b
Pending(s, d) == SelectSeq(s, LAMBDA j : j \notin d)
Increasing(s) == \A i,j \in 1..Len(s) : i < j => s[i] < s[j]
Blank == [phase |-> "down", seen |-> {}, copy |-> [c \in Cards |-> 0],
          fold |-> 0, carry |-> <<>>, batch |-> <<>>, putback |-> <<>>,
          tickApplied |-> 0, checked |-> FALSE, control |-> "unknown"]
EmptyPush == [stage |-> "idle", lo |-> 0, hi |-> 0]

VARIABLES sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, sequenceFresh, observedFresh, fenceFrozen,
          lock, hook, local, gens, done, counts, events, reports, failures,
          cursor, sent, push, commitOwned, commitFresh, pushCommitted,
          fifoKept, observedStop, edgeDone
vars == <<sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, sequenceFresh, observedFresh, fenceFrozen,
          lock, hook, local, gens, done, counts, events, reports, failures,
          cursor, sent, push, commitOwned, commitFresh, pushCommitted,
          fifoKept, observedStop, edgeDone>>
Active(p) == lock = p /\ hook = p
Init ==
  /\ sprintStopped = InitiallyStopped /\ stopCount = 0
  /\ started = ~InitiallyStopped
  /\ tickCallSafe = TRUE /\ tickStopSafe = TRUE
  /\ sequenceFresh = [p \in Processes |-> FALSE]
  /\ observedFresh = [p \in Processes |-> FALSE]
  /\ fenceFrozen = [p \in Processes |-> FALSE]
  /\ lock = NoOwner /\ hook = NoOwner
  /\ local = [p \in Processes |-> Blank]
  /\ gens = [c \in Cards |-> 0]
  /\ done = {} /\ counts = [j \in Jobs |-> 0] /\ events = <<>>
  /\ reports = 0 /\ failures = 0
  /\ cursor = [r \in Recipients |-> 0]
  /\ sent = [r \in Recipients |-> {}]
  /\ push = [p \in Processes |-> [r \in Recipients |-> EmptyPush]]
  /\ commitOwned = TRUE /\ commitFresh = TRUE /\ pushCommitted = TRUE
  /\ fifoKept = TRUE /\ observedStop = TRUE /\ edgeDone = {}

Take(p) ==
  /\ lock = NoOwner /\ local[p].phase = "down"
  /\ lock' = p /\ local' = [local EXCEPT ![p].phase = "hook"]
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, hook, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>
Hook(p) ==
  /\ lock = p /\ local[p].phase = "hook" /\ hook = NoOwner
  /\ hook' = p
  /\ local' = [local EXCEPT ![p] = [Blank EXCEPT !.phase = "ready"]]
  /\ fenceFrozen' = [fenceFrozen EXCEPT ![p] = FALSE]
  /\ UNCHANGED <<sequenceFresh, observedFresh, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>
HookFailure(p) ==
  /\ lock = p /\ local[p].phase = "hook" /\ failures < MaxFailures
  /\ lock' = NoOwner /\ local' = [local EXCEPT ![p] = Blank]
  /\ failures' = failures + 1
  /\ fenceFrozen' = [fenceFrozen EXCEPT ![p] = FALSE]
  /\ UNCHANGED <<sequenceFresh, observedFresh, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, hook, gens, done, counts, events, reports, cursor, sent,
       push, commitOwned, commitFresh, pushCommitted, fifoKept, observedStop, edgeDone>>

\* The PG connection dies before this process notices. The bus hook can
\* disappear too; no oracle clears its previously successful health check.
LoseLock(p) ==
  /\ lock = p /\ failures < MaxFailures
  /\ lock' = NoOwner /\ hook' = IF hook = p THEN NoOwner ELSE hook
  /\ failures' = failures + 1
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, local, gens, done, counts, events, reports, cursor, sent,
       push, commitOwned, commitFresh, pushCommitted, fifoKept, observedStop, edgeDone>>
LoseHook(p) ==
  /\ hook = p /\ failures < MaxFailures
  /\ hook' = NoOwner /\ failures' = failures + 1
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, local, gens, done, counts, events, reports, cursor, sent,
       push, commitOwned, commitFresh, pushCommitted, fifoKept, observedStop, edgeDone>>
Crash(p) ==
  /\ local[p].phase # "down" /\ failures < MaxFailures
  /\ lock' = IF lock = p THEN NoOwner ELSE lock
  /\ hook' = IF hook = p THEN NoOwner ELSE hook
  /\ local' = [local EXCEPT ![p] = Blank]
  /\ push' = [push EXCEPT ![p] = [r \in Recipients |-> EmptyPush]]
  /\ failures' = failures + 1
  /\ fenceFrozen' = [fenceFrozen EXCEPT ![p] = FALSE]
  /\ UNCHANGED <<sequenceFresh, observedFresh, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, gens, done, counts, events, reports, cursor, sent,
       commitOwned, commitFresh, pushCommitted, fifoKept, observedStop, edgeDone>>
NoticeLost(p) ==
  /\ local[p].phase # "down" /\ ~Active(p)
  /\ (local[p].phase # "hook" \/ lock # p)
  /\ local' = [local EXCEPT ![p] = Blank]
  /\ lock' = IF lock = p THEN NoOwner ELSE lock
  /\ hook' = IF hook = p THEN NoOwner ELSE hook
  /\ fenceFrozen' = [fenceFrozen EXCEPT ![p] = FALSE]
  /\ UNCHANGED <<sequenceFresh, observedFresh, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>


\* Stop contract: store atomically checks the captured AtSeq.
\* sequenceFresh abstracts equality to that physical seq, not card-gen equality.
\* observedFresh is the current folded observation; fenceFrozen protects an
\* already captured op across later folds. An uncommitted unknown attempt may
\* stutter with that fence intact. Replay of an already committed unknown
\* outcome is outside this abstraction; it must not be emitted as a new Apply.
\* Source trace binding is separate from this conditional contract model.
\* Stop does not erase historical tick events. The monitor covers newly
\* accepted tick appends while stopped; refusal/cursor rows are not events.
Stop ==
  /\ sequenceFresh' = [q \in Processes |-> FALSE]
  /\ observedFresh' = [q \in Processes |-> FALSE]
  /\ ~sprintStopped /\ stopCount < MaxStops
  /\ sprintStopped' = TRUE /\ stopCount' = stopCount + 1
  /\ UNCHANGED <<started, fenceFrozen, lock, hook, local, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone, tickCallSafe, tickStopSafe>>
Start ==
  /\ sequenceFresh' = [q \in Processes |-> FALSE]
  /\ observedFresh' = [q \in Processes |-> FALSE]
  /\ sprintStopped /\ sprintStopped' = FALSE
  /\ started' = TRUE
  /\ UNCHANGED <<fenceFrozen, lock, hook, local, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone, stopCount, tickCallSafe, tickStopSafe>>
ReadControl(p) ==
  /\ Active(p) /\ local[p].phase \in {"ready", "apply"}
  /\ local' = [local EXCEPT ![p].control = IF sprintStopped THEN "stopped" ELSE "running"]
  /\ observedFresh' = [observedFresh EXCEPT ![p] = TRUE]
  /\ UNCHANGED <<sequenceFresh, fenceFrozen, lock, hook, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe>>

\* Capture the exact observed seq ONCE for this op, before its first Apply.
\* A later fold after an unknown outcome must not replace the captured fence.
CaptureFence(p) ==
  /\ local[p].phase = "apply" /\ local[p].batch # <<>>
  /\ local[p].control = "running" /\ ~fenceFrozen[p]
  /\ sequenceFresh' = [sequenceFresh EXCEPT ![p] = observedFresh[p]]
  /\ fenceFrozen' = [fenceFrozen EXCEPT ![p] = TRUE]
  /\ UNCHANGED <<observedFresh, lock, hook, local, gens, done, counts,
       events, reports, failures, cursor, sent, push, commitOwned, commitFresh,
       pushCommitted, fifoKept, observedStop, edgeDone, sprintStopped,
       stopCount, started, tickCallSafe, tickStopSafe>>

Fold(p) ==
  /\ Active(p) /\ local[p].phase = "ready"
  /\ local[p].control = "running"
  /\ tickCallSafe' = (tickCallSafe /\ local[p].control # "stopped")
  /\ LET q == Pending(local[p].carry, done) \o
                 SelectSeq(AllJobs, LAMBDA j : j \notin local[p].seen /\ j \notin done)
     IN local' = [local EXCEPT ![p].phase = "select", ![p].copy = gens,
          ![p].fold = Len(events), ![p].carry = q, ![p].seen = Jobs,
          ![p].checked = TRUE, ![p].tickApplied = 0]
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickStopSafe, lock, hook, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>
Select(p) ==
  /\ local[p].phase = "select" /\ local[p].checked
  /\ LET q == local[p].carry
         n == Len(q)
     IN /\ local' = [local EXCEPT ![p].phase = "apply",
              ![p].batch = SubSeq(q, 1, n),
              ![p].carry = SubSeq(q, n+1, Len(q))]
        /\ fifoKept' = (fifoKept /\ SubSeq(q, 1, n) \o SubSeq(q,n+1,Len(q)) = q)
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted, observedStop, edgeDone>>

Report(c) ==
  /\ sequenceFresh' = [q \in Processes |-> FALSE]
  /\ observedFresh' = [q \in Processes |-> FALSE]
  /\ reports < MaxReports
  /\ gens' = [gens EXCEPT ![c] = @ + 1]
  /\ reports' = reports + 1
  /\ events' = Append(events, [kind |-> "report", id |-> WorkCount + reports + 1])
  /\ UNCHANGED <<fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, local, done, counts, failures, cursor, sent,
       push, commitOwned, commitFresh, pushCommitted, fifoKept, observedStop, edgeDone>>
Stale(p,j) == local[p].copy[Card(j)] # gens[Card(j)] \/ j \in done
\* L3 receives the whole refused op; run never decomposes its writes.
Refuse(p) ==
  /\ sequenceFresh' = [q \in Processes |-> FALSE]
  /\ observedFresh' = [q \in Processes |-> FALSE]
  /\ local[p].phase = "apply" /\ local[p].checked
  /\ local[p].control = "running" /\ fenceFrozen[p]
  /\ local[p].batch # <<>> /\ (Stale(p,Head(local[p].batch)) \/ ~sequenceFresh[p])
  /\ local' = [local EXCEPT
       ![p].putback = Append(local[p].putback, Head(local[p].batch)),
       ![p].batch = Tail(local[p].batch)]
  /\ fenceFrozen' = [fenceFrozen EXCEPT ![p] = FALSE]
  /\ UNCHANGED <<sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>
\* Accepted append: running observation plus atomic physical-sequence fence.
Apply(p) ==
  /\ local[p].phase = "apply" /\ local[p].checked
  /\ local[p].control = "running" /\ fenceFrozen[p]
  /\ sequenceFresh[p] \* Atomic store AtSeq condition
  /\ tickStopSafe' = (tickStopSafe /\ ~sprintStopped)
  /\ IF AtomicChooser THEN Active(p) ELSE TRUE
  /\ local[p].batch # <<>>
  /\ ~Stale(p,Head(local[p].batch))
  /\ LET j == Head(local[p].batch)
     IN /\ gens' = [gens EXCEPT ![Card(j)] = @ + 1]
        /\ done' = done \cup {j}
        /\ counts' = [counts EXCEPT ![j] = @ + 1]
        /\ events' = Append(events, [kind |-> "tick", id |-> j])
        /\ local' = [local EXCEPT ![p].batch = Tail(@),
                     ![p].tickApplied = @ + 1,
                     ![p].copy[Card(j)] = gens[Card(j)] + 1]
        /\ commitFresh' = (commitFresh /\ ~Stale(p,j))
  /\ commitOwned' = (commitOwned /\ Active(p))
  /\ observedStop' = (observedStop /\ local[p].checked)
  /\ sequenceFresh' = [q \in Processes |-> FALSE]
  /\ observedFresh' = [q \in Processes |-> FALSE]
  /\ fenceFrozen' = [fenceFrozen EXCEPT ![p] = FALSE]
  /\ UNCHANGED <<sprintStopped, stopCount, started, tickCallSafe, lock, hook, reports, failures, cursor, sent, push,
       pushCommitted, fifoKept, edgeDone>>
EndTick(p) ==
  /\ local[p].phase = "apply" /\ local[p].batch = <<>>
  /\ local' = [local EXCEPT ![p].phase = "ready",
       ![p].carry = local[p].putback \o local[p].carry,
       ![p].putback = <<>>, ![p].checked = FALSE, ![p].control = "unknown"]
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>

\* Cursor is a physical log index, including events irrelevant to a name.
\* In this small instance every event needs every configured recipient.
PreparePush(p,r) ==
  /\ Active(p) /\ push[p][r].stage = "idle" /\ cursor[r] < Len(events)
  /\ push' = [push EXCEPT ![p][r] =
                  [stage |-> "sending", lo |-> cursor[r]+1, hi |-> Len(events)]]
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, local, gens, done, counts, events, reports,
       failures, cursor, sent, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>
SendOK(p,r) ==
  /\ push[p][r].stage = "sending"
  /\ sent' = [sent EXCEPT ![r] = @ \cup (push[p][r].lo..push[p][r].hi)]
  /\ pushCommitted' = (pushCommitted /\ push[p][r].hi <= Len(events))
  /\ push' = [push EXCEPT ![p][r].stage = "sent"]
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, local, gens, done, counts, events, reports,
       failures, cursor, commitOwned, commitFresh, fifoKept, observedStop, edgeDone>>
SaveCursor(p,r) ==
  /\ push[p][r].stage = "sent"
  /\ cursor' = [cursor EXCEPT ![r] = Max(@, push[p][r].hi)]
  /\ push' = [push EXCEPT ![p][r] = EmptyPush]
  /\ sequenceFresh' = [q \in Processes |-> FALSE]
  /\ observedFresh' = [q \in Processes |-> FALSE]
  /\ UNCHANGED <<fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, local, gens, done, counts, events, reports,
       failures, sent, commitOwned, commitFresh, pushCommitted,
       fifoKept, observedStop, edgeDone>>

\* An edge request names the accepted operation ids on which it depends.
\* This instance has one dependency per edge; empty After is independent.
DeliverEdge(j) ==
  /\ j \in done
  /\ edgeDone' = edgeDone \cup {j}
  /\ UNCHANGED <<sequenceFresh, observedFresh, fenceFrozen, sprintStopped, stopCount, started, tickCallSafe, tickStopSafe, lock, hook, local, gens, done, counts, events, reports, failures,
       cursor, sent, push, commitOwned, commitFresh, pushCommitted, fifoKept, observedStop>>

Progress(p) == Take(p) \/ Hook(p) \/ NoticeLost(p) \/ ReadControl(p) \/ CaptureFence(p) \/ Fold(p) \/ Select(p)
                 \/ Refuse(p) \/ Apply(p) \/ EndTick(p)
Next == Stop \/ Start \/ (\E p \in Processes : Progress(p) \/ HookFailure(p) \/ LoseLock(p) \/ LoseHook(p) \/ Crash(p))
     \/ (\E c \in Cards : Report(c))
     \/ (\E j \in Jobs : DeliverEdge(j))
     \/ (\E p \in Processes, r \in Recipients :
                    PreparePush(p,r) \/ SendOK(p,r) \/ SaveCursor(p,r))
Spec == Init /\ [][Next]_vars
        /\ (\A p \in Processes : WF_vars(Progress(p)))
        /\ (\A p \in Processes, r \in Recipients :
             WF_vars(PreparePush(p,r)) /\ WF_vars(SendOK(p,r)) /\ WF_vars(SaveCursor(p,r)))
TypeOK == /\ sequenceFresh \in [Processes -> BOOLEAN]
          /\ observedFresh \in [Processes -> BOOLEAN]
          /\ fenceFrozen \in [Processes -> BOOLEAN]
          /\ sprintStopped \in BOOLEAN /\ stopCount \in 0..MaxStops /\ started \in BOOLEAN
          /\ lock \in Processes \cup {NoOwner} /\ hook \in Processes \cup {NoOwner}
          /\ done \subseteq Jobs /\ reports \in 0..MaxReports
          /\ failures \in 0..MaxFailures
          /\ gens \in [Cards -> 0..(MaxReports+WorkCount)]
          /\ counts \in [Jobs -> 0..2]
          /\ \A p \in Processes :
               /\ local[p].phase \in {"down","hook","ready","select","apply"}
               /\ local[p].control \in {"unknown","running","stopped"}
               /\ local[p].fold <= Len(events)
               /\ Elems(local[p].carry \o local[p].batch \o local[p].putback) \subseteq Jobs
          /\ \A r \in Recipients : cursor[r] \in 0..Len(events)
OneChooser == lock \in Processes \cup {NoOwner}
NoApplyAfterObservedLoss == observedStop
CommitUnderCurrentLock == commitOwned
AppliedOnce == \A j \in Jobs : counts[j] <= 1
NoStaleMove == commitFresh
BudgetKept == \A p \in Processes : local[p].tickApplied <= Budget
PushAfterCommit == pushCommitted
CursorTrailsSent == \A r \in Recipients : (1..cursor[r]) \subseteq sent[r]
FIFO == fifoKept /\ \A p \in Processes :
           Increasing(local[p].carry) /\ Increasing(local[p].batch) /\ Increasing(local[p].putback)
NoDroppedPlan == \A p \in Processes : local[p].phase # "down" =>
  (local[p].seen \ done) \subseteq Elems(local[p].carry \o local[p].batch \o local[p].putback)
EdgesAfterCommit == edgeDone \subseteq done
NoTickAfterObservedStop == tickCallSafe
NoAcceptedTickWhileStopped == tickStopSafe
\* The brief's name (BRIEF-V2-STITCH step 3): nothing ticks while Stopped,
\* neither a tick begun after the stop was seen nor one accepted under it.
\* run.Ticker.Tick returns before steps.Tick when its folded copy is
\* stopped (run/tick.go; TestNoTickWhileStopped).
NoTickWhileStopped == NoTickAfterObservedStop /\ NoAcceptedTickWhileStopped
\* A fresh store is stopped, its reason "never started" (store.FreshStop),
\* and a run may come up on it before up's walk is done (up starts the run
\* service at step 4 of 8): until the coordinator's first Start it moves
\* nothing, no tick applied, no edge dispatched (Glenn, 2026-11-07).
\* RunFreshRunning starts the fresh store running and violates it.
NeverStartedMovesNothing ==
  ~started => /\ done = {} /\ edgeDone = {}
              /\ \A j \in Jobs : counts[j] = 0
              /\ \A p \in Processes : local[p].tickApplied = 0
\* Intentionally stopped service is not promised eventual work completion.
WorkWhenRunning == (<>[](~sprintStopped)) => <>(done = Jobs)
AllWork == <>(done = Jobs)
AllEventsSent == \A r \in Recipients : \A n \in 1..(WorkCount+MaxReports) :
                                      (n <= Len(events)) ~> (n \in sent[r])
=============================================================================
