---------------------------- MODULE ServerLanes ----------------------------
(***************************************************************************)
(* The sprint server's line of control and its lanes (cmd/nova-sprint      *)
(* serve.go serveCtx, servelanes.go, seriallock.go; internal/sprint        *)
(* control_line.go; docs/SPEC-SPRINT.md section 14, The server, "The      *)
(* tick's turn"). One line (sprint.ControlLine): the run loop's tick and   *)
(* each batch of writes hold it in turn, so none runs during another. A    *)
(* batch takes the next ticket and is served in ticket order (first come,  *)
(* first served). The tick does not queue behind them: when it asks it     *)
(* takes the line at once when no batch waits, and once its turn is due it *)
(* takes the line next, after the holder in flight and before every batch  *)
(* still waiting, so it waits at most one turn and one batch in flight. A  *)
(* gone caller's ticket is served and handed back: its verbs are never     *)
(* run. A friend's beat runs on the beat lane and a read on the read lane  *)
(* (seriallock.go, the read lane's own line, one read at a time), neither  *)
(* on the line. A caller can go away at any time (its client's deadline).  *)
(*                                                                         *)
(* A read asked of a reader is held by a lease (the reader's read --begin  *)
(* starts it, its beat renews it, default 10 minutes); on start the server *)
(* keeps every read whose lease is live and only takes back reads whose     *)
(* lease lapsed.                                                           *)
(*                                                                         *)
(* The fault of 2026-10-04: every verb took the line, and a batch whose    *)
(* caller had gone was still run when its turn came, so beats waited       *)
(* behind ticks and writes and the line never drained.                     *)
(*                                                                         *)
(* The design is Broken = {}. Each other value is a reversed witness:      *)
(*   "beatonline"  a beat takes the line (the code before the lanes):      *)
(*                 BeatNeverWaitsForTheLine fails;                         *)
(*   "readonline"  a read takes the line: ReadWaitsOnlyForReads fails;     *)
(*   "rungone"     a write whose caller went is still run (a sync.Mutex    *)
(*                 that cannot be given up): GoneNeverRuns fails;          *)
(*   "tickqueues"  the tick takes a ticket like a batch and waits its      *)
(*                 turn in the queue: TickWaitsOneTurn fails.              *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Callers, KindOf, MaxTicks, Broken

Kinds  == {"beat", "read", "write"}
Faults == {"beatonline", "readonline", "rungone", "tickqueues"}
ASSUME Broken \subseteq Faults /\ "tick" \notin Callers /\ "none" \notin Callers
ASSUME \A r \in Callers : KindOf[r] \in Kinds

VARIABLES line,      \* the line's holder: "none", "tick" or a caller
          rline,     \* the read lane's holder: "none" or a caller
          pc,        \* each caller: idle, waiting, running, answered, dropped
          gone,      \* each caller: its client has gone away
          goneWait,  \* ghost: it went while it was still waiting to start
          ran,       \* each caller: its verb ran (changed the sprint)
          ticks,     \* ticks begun, bounded by MaxTicks
          lease,     \* each caller: "none", "live" or "lapsed"
          restarts,  \* server restarts, bounded by 1
          takenBack, \* ghost: each caller: taken back on restart
          wasLapsed, \* ghost: each caller: lapsed when restart happened
          next,      \* the next ticket to hand out
          serving,   \* the next ticket to serve
          tkt,       \* each caller: the ticket it took, or 0
          turnDue,   \* the tick's turn is due: it takes the line next
          dueFinishes, \* ghost: batches finished while the tick's turn was due
          tickQueued,\* broken "tickqueues": the tick has taken a ticket
          tickTkt    \* broken "tickqueues": the tick's ticket

vars == <<line, rline, pc, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
          next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

OnLine(r) == KindOf[r] = "write"
             \/ (KindOf[r] = "beat" /\ "beatonline" \in Broken)
             \/ (KindOf[r] = "read" /\ "readonline" \in Broken)

\* the tick wants the line: it is not holding it and has ticks left to run
TickWants == ticks < MaxTicks /\ line # "tick"

\* no batch holds a ticket not yet served
NoBatchWaits == serving = next

TypeOK ==
  /\ line \in {"none", "tick"} \cup Callers
  /\ rline \in {"none"} \cup Callers
  /\ pc \in [Callers -> {"idle", "waiting", "running", "answered", "dropped"}]
  /\ gone \in [Callers -> BOOLEAN]
  /\ goneWait \in [Callers -> BOOLEAN]
  /\ ran \in [Callers -> BOOLEAN]
  /\ ticks \in 0..MaxTicks
  /\ lease \in [Callers -> {"none", "live", "lapsed"}]
  /\ restarts \in 0..1
  /\ takenBack \in [Callers -> BOOLEAN]
  /\ wasLapsed \in [Callers -> BOOLEAN]
  /\ next \in 0..(Cardinality(Callers) + 1)
  /\ serving \in 0..(Cardinality(Callers) + 1)
  /\ tkt \in [Callers -> 0..(Cardinality(Callers) + 1)]
  /\ turnDue \in BOOLEAN
  /\ dueFinishes \in 0..(Cardinality(Callers) + 1)
  /\ tickQueued \in BOOLEAN
  /\ tickTkt \in 0..(Cardinality(Callers) + 1)

Init ==
  /\ line = "none" /\ rline = "none"
  /\ pc = [r \in Callers |-> "idle"]
  /\ gone = [r \in Callers |-> FALSE]
  /\ goneWait = [r \in Callers |-> FALSE]
  /\ ran = [r \in Callers |-> FALSE]
  /\ ticks = 0
  /\ lease = [r \in Callers |-> "none"]
  /\ restarts = 0
  /\ takenBack = [r \in Callers |-> FALSE]
  /\ wasLapsed = [r \in Callers |-> FALSE]
  /\ next = 0 /\ serving = 0
  /\ tkt = [r \in Callers |-> 0]
  /\ turnDue = TRUE
  /\ dueFinishes = 0
  /\ tickQueued = FALSE
  /\ tickTkt = 0

\* a caller sends its batch; a batch takes the next ticket
Send(r) == /\ pc[r] = "idle"
           /\ pc' = [pc EXCEPT ![r] = "waiting"]
           /\ tkt' = IF KindOf[r] = "write" THEN [tkt EXCEPT ![r] = next] ELSE tkt
           /\ next' = IF KindOf[r] = "write" THEN next + 1 ELSE next
           /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                          serving, turnDue, dueFinishes, tickQueued, tickTkt>>

\* the tick's turn becomes due (the batches' turn is over), while the tick still has
\* a turn left to take
Turn == /\ ~turnDue /\ ticks < MaxTicks
        /\ turnDue' = TRUE /\ dueFinishes' = 0
        /\ UNCHANGED <<line, rline, pc, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                       next, serving, tkt, tickQueued, tickTkt>>

\* broken "tickqueues": the tick takes a ticket like a batch
TickQueue == /\ "tickqueues" \in Broken
             /\ TickWants /\ ~tickQueued
             /\ tickQueued' = TRUE /\ tickTkt' = next /\ next' = next + 1
             /\ UNCHANGED <<line, rline, pc, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                            serving, tkt, turnDue, dueFinishes>>

\* the run loop takes the line for a tick, and gives it back; the design takes it at
\* its turn (when no batch waits, or the turn is due), never behind every batch waiting
TickBegin == /\ line = "none" /\ ticks < MaxTicks
             /\ IF "tickqueues" \in Broken
                THEN (tickQueued /\ serving = tickTkt)
                ELSE (NoBatchWaits \/ turnDue)
             /\ line' = "tick" /\ ticks' = ticks + 1
             /\ UNCHANGED <<rline, pc, gone, goneWait, ran, lease, restarts, takenBack, wasLapsed,
                            next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>
TickEnd == /\ line = "tick" /\ line' = "none"
           /\ turnDue' = FALSE /\ tickQueued' = FALSE
           /\ UNCHANGED <<rline, pc, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                          next, serving, tkt, dueFinishes, tickTkt>>

\* a caller's client gives up (its deadline): it is gone, whatever its verb is doing
GiveUp(r) == /\ pc[r] \in {"waiting", "running"} /\ ~gone[r]
             /\ gone' = [gone EXCEPT ![r] = TRUE]
             /\ goneWait' = [goneWait EXCEPT ![r] = (pc[r] = "waiting")]
             /\ UNCHANGED <<line, rline, pc, ran, ticks, lease, restarts, takenBack, wasLapsed,
                            next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a verb on the line takes it in ticket order (a batch is next when its ticket is
\* served); the design never for a caller gone, and not while the tick's turn is due
TakeLine(r) == /\ pc[r] = "waiting" /\ OnLine(r) /\ line = "none"
               /\ (~gone[r] \/ "rungone" \in Broken)
               /\ (KindOf[r] # "write" \/ (tkt[r] = serving /\ ("tickqueues" \in Broken \/ ~(turnDue /\ ticks < MaxTicks))))
               /\ line' = r /\ pc' = [pc EXCEPT ![r] = "running"]
               /\ serving' = IF KindOf[r] = "write" THEN serving + 1 ELSE serving
               /\ UNCHANGED <<rline, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                              next, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a batch gone while it waited is served and handed back: its ticket is served, its
\* verbs answered not run
ServeGone(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "write" /\ gone[r] /\ "rungone" \notin Broken
                /\ tkt[r] = serving /\ line = "none"
                /\ ("tickqueues" \in Broken \/ ~(turnDue /\ ticks < MaxTicks))
                /\ serving' = serving + 1
                /\ pc' = [pc EXCEPT ![r] = "dropped"]
                /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                               next, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a beat starts on the beat lane: no line, no other beat waited for
BeatStart(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "beat" /\ ~OnLine(r) /\ ~gone[r]
                /\ pc' = [pc EXCEPT ![r] = "running"]
                /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                               next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

BeatDrop(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "beat" /\ ~OnLine(r) /\ gone[r]
                /\ pc' = [pc EXCEPT ![r] = "dropped"]
                /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, restarts, takenBack, wasLapsed,
                               next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a read starts on the read lane: it waits for a read ahead of it alone, and is
\* dropped as a write is when its caller has gone first; its lease begins live
ReadStart(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "read" /\ ~OnLine(r) /\ rline = "none"
                /\ ~gone[r]
                /\ rline' = r /\ pc' = [pc EXCEPT ![r] = "running"]
                /\ lease' = [lease EXCEPT ![r] = "live"]
                /\ UNCHANGED <<line, gone, goneWait, ran, ticks, restarts, takenBack, wasLapsed,
                               next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>
ReadDrop(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "read" /\ ~OnLine(r) /\ gone[r]
               /\ pc' = [pc EXCEPT ![r] = "dropped"]
               /\ lease' = [lease EXCEPT ![r] = "none"]
               /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, restarts, takenBack, wasLapsed,
                              next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a read's lease lapses while it is running (10 minutes have passed without renewal)
LapseLease(r) ==
  /\ restarts = 0
  /\ line = "none"
  /\ pc[r] = "running" /\ KindOf[r] = "read" /\ lease[r] = "live"
  /\ lease' = [lease EXCEPT ![r] = "lapsed"]
  /\ UNCHANGED <<line, rline, pc, gone, goneWait, ran, ticks, restarts, takenBack, wasLapsed,
                 next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a server restart: transient locks are cleared, reads with a live lease are kept,
\* lapsed reads are taken back, and running writes/beats drop
Restart ==
  /\ restarts = 0
  /\ line = "none"
  /\ \E r \in Callers : KindOf[r] = "read" /\ pc[r] = "running"
  /\ restarts' = restarts + 1
  /\ line' = "none"
  /\ rline' = IF rline # "none" /\ KindOf[rline] = "read" /\ lease[rline] = "live"
              THEN rline ELSE "none"
  /\ pc' = [r \in Callers |->
              IF pc[r] = "running" THEN
                IF KindOf[r] = "read" THEN
                  IF lease[r] = "live" THEN "running" ELSE "dropped"
                ELSE "dropped"
              ELSE pc[r]]
  /\ takenBack' = [r \in Callers |->
                     takenBack[r] \/ (KindOf[r] = "read" /\ pc[r] = "running" /\ lease[r] = "lapsed")]
  /\ wasLapsed' = [r \in Callers |->
                     wasLapsed[r] \/ (KindOf[r] = "read" /\ pc[r] = "running" /\ lease[r] = "lapsed")]
  /\ lease' = [r \in Callers |->
                 IF pc[r] = "running" /\ KindOf[r] = "read" /\ lease[r] = "lapsed"
                 THEN "none" ELSE lease[r]]
  /\ UNCHANGED <<gone, goneWait, ran, ticks, next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* the verb runs and is answered; what it held is given back
Finish(r) == /\ pc[r] = "running"
             /\ ran' = [ran EXCEPT ![r] = TRUE]
             /\ pc' = [pc EXCEPT ![r] = "answered"]
             /\ line' = IF line = r THEN "none" ELSE line
             /\ rline' = IF rline = r THEN "none" ELSE rline
             /\ lease' = [lease EXCEPT ![r] = "none"]
             /\ dueFinishes' = IF KindOf[r] = "write" /\ turnDue /\ ticks < MaxTicks THEN dueFinishes + 1 ELSE dueFinishes
             /\ UNCHANGED <<gone, goneWait, ticks, restarts, takenBack, wasLapsed,
                            next, serving, tkt, turnDue, tickQueued, tickTkt>>

Next ==
  \/ TickBegin \/ TickEnd
  \/ Restart
  \/ Turn \/ TickQueue
  \/ \E r \in Callers : Send(r) \/ GiveUp(r) \/ TakeLine(r) \/ ServeGone(r)
                        \/ BeatStart(r) \/ BeatDrop(r) \/ ReadStart(r) \/ ReadDrop(r) \/ Finish(r)
                        \/ LapseLease(r)

Fairness ==
  /\ WF_vars(TickBegin) /\ WF_vars(TickEnd)
  /\ WF_vars(Turn)
  /\ WF_vars(TickQueue)
  /\ \A r \in Callers : /\ SF_vars(TakeLine(r)) /\ WF_vars(ServeGone(r))
                        /\ WF_vars(BeatStart(r)) /\ WF_vars(BeatDrop(r))
                        /\ SF_vars(ReadStart(r))
                        /\ WF_vars(ReadDrop(r)) /\ WF_vars(Finish(r))

Spec == Init /\ [][Next]_vars /\ Fairness

----------------------------------------------------------------------------
\* The line has one holder: a write runs only holding it, never during a tick.
WriteHoldsTheLine == \A r \in Callers :
  (pc[r] = "running" /\ KindOf[r] = "write") => line = r

\* A beat waits for nothing: whoever holds the line, a waiting beat whose caller is
\* there can start.
BeatNeverWaitsForTheLine == \A r \in Callers :
  (pc[r] = "waiting" /\ KindOf[r] = "beat" /\ ~gone[r]) => ENABLED BeatStart(r)

\* A read waits for another read alone: with the read lane free, a waiting read
\* whose caller is there can start whoever holds the line.
ReadWaitsOnlyForReads == \A r \in Callers :
  (pc[r] = "waiting" /\ KindOf[r] = "read" /\ rline = "none" /\ ~gone[r]) => ENABLED ReadStart(r)

\* A verb whose caller went before it started never runs.
GoneNeverRuns == \A r \in Callers : goneWait[r] => ~ran[r]

\* The lanes never hold the line.
LanesHoldNoLine == \A r \in Callers : KindOf[r] # "write" => line # r

\* Batches are served in ticket order: no batch runs while an earlier ticket still
\* waits.
FCFS == \A r, s \in Callers :
  (KindOf[r] = "write" /\ KindOf[s] = "write" /\ pc[r] = "running" /\ pc[s] = "waiting")
    => tkt[r] < tkt[s]

\* Once the tick's turn is due, at most the batch already holding the line finishes
\* before the tick begins: no second batch runs in the turn.
TickWaitsOneTurn == (turnDue /\ ticks < MaxTicks) => dueFinishes <= 1

\* A read with a live lease is never taken back.
LiveLeaseNeverTakenBack == \A r \in Callers :
  (KindOf[r] = "read" /\ lease[r] = "live") => ~takenBack[r]

\* Every lapsed read is taken back on restart.
EveryLapsedReadTakenBack == \A r \in Callers :
  (KindOf[r] = "read" /\ wasLapsed[r]) => takenBack[r]


\* Every batch sent is answered or dropped (its caller gone).
EveryBatchEnds == \A r \in Callers :
  (pc[r] = "waiting") ~> (pc[r] \in {"answered", "dropped"})
=============================================================================
