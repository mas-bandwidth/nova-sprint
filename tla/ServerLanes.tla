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
(* A read asked of a reader is held by a lease on a reader card in the     *)
(* Readers Reading column (internal/sprint/readers.go): read --begin, a    *)
(* line verb, begins it (begun + DefaultReadLease); the reader's beat      *)
(* renews it on a served queue (queue --as <reader>, setting lease to now  *)
(* + DefaultReadLease) once three quarters of it or less is left        *)
(* (RenewAt, Mark; readers.go ReadLeaseRenewAt, 7.5 of 10 minutes: on     *)
(* 2026-10-17 every poll rewrote every lease, each a log line waking the  *)
(* tick and every long poller), under the held line, errors ignored (a    *)
(* renew may fail); a lease not renewed expires. A served queue leaves    *)
(* every live lease more than three quarters of its bound. Age (the       *)
(* lease's decrement) is not tied to how often a reader polls: that a     *)
(* reader polls at least once each quarter of its lease, and so keeps it  *)
(* live, is an assumption of the prose, not a step of the model. On start *)
(* the server keeps every read whose lease is live and only takes back    *)
(* reads whose lease lapsed.                                               *)
(* Read-lane verbs (where, card, log, ...) hold no lease.                  *)
(*                                                                         *)
(* A member's beat (fleet beat <member> --load ..., the fleet's beat, not a  *)
(* friend's) writes one record outside every table too, but its words are  *)
(* the member's word the tick reconciles the row against: its live set     *)
(* (LiveReturns), its no-room and draining (the deal), its facts and its   *)
(* stop-returns. A beat that changes that word, or is the member's first,  *)
(* takes the line, so the tick sees the word whole and in order with the   *)
(* takes and finishes before it; a beat that changes nothing but its clock *)
(* and its load (nine idle benches, every 3 s: ~180 a minute on the line   *)
(* on 2026-10-17) runs on the beat lane as a friend's does (servelanes.go  *)
(* fleetBeatChanges). Changes[r] says a fleet beat carries a change.       *)
(*                                                                         *)
(* The dashboard's read (`where --json --dashboard`, the public page's once  *)
(* a second) runs on a lane of its own, the dashboard lane (servelanes.go    *)
(* dashWhere): one compute at a time, beside the line and the read lane,     *)
(* answered from a copy callers within a second of its start share. A dash   *)
(* caller takes only the dashboard lane's holder, never the line nor the     *)
(* read lane's.                                                              *)
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
(*                 turn in the queue: TickWaitsOneTurn fails;              *)
(*   "dashonreadlane" the dashboard's read takes the read lane as a read     *)
(*                 does and waits behind the reads: DashWaitsOnlyForDash   *)
(*                 fails;                                                  *)
(*   "norenew"     the served queue never renews a lease: a lease begun    *)
(*                 and not renewed expires and is taken back, so           *)
(*                 LiveLeaseNeverTakenBack fails;                          *)
(*   "renewlast"   the served queue renews a lease only on its last unit   *)
(*                 (the margin gone): a queue leaves a live lease with     *)
(*                 three quarters of its bound or less, so                 *)
(*                 LiveLeaseNeverTakenBack                                 *)
(*                 fails (MCServerLanesBrokenRenewLast, Bound 4);          *)
(*   "fbeatlane"   every fleet beat runs on the beat lane, a change of the *)
(*                 member's word too, beside the tick that reconciles the  *)
(*                 row against it: ChangeHoldsTheLine fails. "beatonline"  *)
(*                 covers the fleet beat too: a changeless one on the line *)
(*                 fails BeatNeverWaitsForTheLine.                         *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Callers, KindOf, Changes, MaxTicks, Broken, Cards, Bound

Kinds  == {"beat", "fbeat", "read", "dash", "write", "begin", "queue"}
Faults == {"beatonline", "readonline", "rungone", "tickqueues", "norenew", "renewlast", "dashonreadlane", "fbeatlane"}
ASSUME Broken \subseteq Faults /\ "tick" \notin Callers /\ "none" \notin Callers
ASSUME \A r \in Callers : KindOf[r] \in Kinds
ASSUME \A r \in Callers : Changes[r] \in BOOLEAN
ASSUME Bound > 0

\* three quarters of the lease's bound: a served queue renews a lease with this much left or less
Mark == (3 * Bound) \div 4
\* the lease left at which the served queue renews it: Mark (broken "renewlast": the last unit)
RenewAt == IF "renewlast" \in Broken THEN 1 ELSE Mark

VARIABLES line,      \* the line's holder: "none", "tick" or a caller
          rline,     \* the read lane's holder: "none" or a caller
          dline,     \* the dashboard lane's holder: "none" or a caller
          pc,        \* each caller: idle, waiting, running, answered, dropped
          gone,      \* each caller: its client has gone away
          goneWait,  \* ghost: it went while it was still waiting to start
          ran,       \* each caller: its verb ran (changed the sprint)
          ticks,     \* ticks begun, bounded by MaxTicks
          lease,     \* each card: 0 (none or lapsed), or Bound..1 left live
          begun,     \* each card: its read is in flight (read --begin ran)
          restarts,  \* server restarts, bounded by 1
          takenBack, \* ghost: each card: taken back on restart
          wasLapsed, \* ghost: each card: lapsed when restart happened
          next,      \* the next ticket to hand out
          serving,   \* the next ticket to serve
          tkt,       \* each caller: the ticket it took, or 0
          turnDue,   \* the tick's turn is due: it takes the line next
          dueFinishes, \* ghost: batches finished while the tick's turn was due
          tickQueued,\* broken "tickqueues": the tick has taken a ticket
          tickTkt    \* broken "tickqueues": the tick's ticket

vars == <<line, rline, dline, pc, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
          next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a line verb: it runs holding the line (a write, read --begin, a reader's
\* queue, which is its beat, or a member's beat that changes the member's word
\* or is its first; broken "fbeatlane": no fleet beat is one)
LineVerb(r) == KindOf[r] \in {"write", "begin", "queue"}
                \/ (KindOf[r] = "fbeat" /\ Changes[r] /\ "fbeatlane" \notin Broken)

\* a beat-lane verb: a friend's beat, or a member's beat that is no line verb
BeatLane(r) == KindOf[r] \in {"beat", "fbeat"} /\ ~LineVerb(r)

OnLine(r) == LineVerb(r)
             \/ (BeatLane(r) /\ "beatonline" \in Broken)
             \/ (KindOf[r] = "read" /\ "readonline" \in Broken)

\* the tick wants the line: it is not holding it and has ticks left to run
TickWants == ticks < MaxTicks /\ line # "tick"

\* no batch holds a ticket not yet served
NoBatchWaits == serving = next

TypeOK ==
  /\ line \in {"none", "tick"} \cup Callers
  /\ rline \in {"none"} \cup Callers
  /\ dline \in {"none"} \cup Callers
  /\ pc \in [Callers -> {"idle", "waiting", "running", "answered", "dropped"}]
  /\ gone \in [Callers -> BOOLEAN]
  /\ goneWait \in [Callers -> BOOLEAN]
  /\ ran \in [Callers -> BOOLEAN]
  /\ ticks \in 0..MaxTicks
  /\ lease \in [Cards -> 0..Bound]
  /\ begun \in [Cards -> BOOLEAN]
  /\ restarts \in 0..1
  /\ takenBack \in [Cards -> BOOLEAN]
  /\ wasLapsed \in [Cards -> BOOLEAN]
  /\ next \in 0..(Cardinality(Callers) + 1)
  /\ serving \in 0..(Cardinality(Callers) + 1)
  /\ tkt \in [Callers -> 0..(Cardinality(Callers) + 1)]
  /\ turnDue \in BOOLEAN
  /\ dueFinishes \in 0..(Cardinality(Callers) + 1)
  /\ tickQueued \in BOOLEAN
  /\ tickTkt \in 0..(Cardinality(Callers) + 1)

Init ==
  /\ line = "none" /\ rline = "none" /\ dline = "none"
  /\ pc = [r \in Callers |-> "idle"]
  /\ gone = [r \in Callers |-> FALSE]
  /\ goneWait = [r \in Callers |-> FALSE]
  /\ ran = [r \in Callers |-> FALSE]
  /\ ticks = 0
  /\ lease = [c \in Cards |-> 0]
  /\ begun = [c \in Cards |-> FALSE]
  /\ restarts = 0
  /\ takenBack = [c \in Cards |-> FALSE]
  /\ wasLapsed = [c \in Cards |-> FALSE]
  /\ next = 0 /\ serving = 0
  /\ tkt = [r \in Callers |-> 0]
  /\ turnDue = TRUE
  /\ dueFinishes = 0
  /\ tickQueued = FALSE
  /\ tickTkt = 0

\* a caller sends its batch; a batch takes the next ticket
Send(r) == /\ pc[r] = "idle"
           /\ pc' = [pc EXCEPT ![r] = "waiting"]
           /\ tkt' = IF LineVerb(r) THEN [tkt EXCEPT ![r] = next] ELSE tkt
           /\ next' = IF LineVerb(r) THEN next + 1 ELSE next
           /\ UNCHANGED <<line, rline, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                          serving, turnDue, dueFinishes, tickQueued, tickTkt>>

\* the tick's turn becomes due (the batches' turn is over), while the tick still has
\* a turn left to take
Turn == /\ ~turnDue /\ ticks < MaxTicks
        /\ turnDue' = TRUE /\ dueFinishes' = 0
        /\ UNCHANGED <<line, rline, dline, pc, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                       next, serving, tkt, tickQueued, tickTkt>>

\* broken "tickqueues": the tick takes a ticket like a batch
TickQueue == /\ "tickqueues" \in Broken
             /\ TickWants /\ ~tickQueued
             /\ tickQueued' = TRUE /\ tickTkt' = next /\ next' = next + 1
             /\ UNCHANGED <<line, rline, dline, pc, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                            serving, tkt, turnDue, dueFinishes>>

\* the run loop takes the line for a tick, and gives it back; the design takes it at
\* its turn (when no batch waits, or the turn is due), never behind every batch waiting
TickBegin == /\ line = "none" /\ ticks < MaxTicks
             /\ IF "tickqueues" \in Broken
                THEN (tickQueued /\ serving = tickTkt)
                ELSE (NoBatchWaits \/ turnDue)
             /\ line' = "tick" /\ ticks' = ticks + 1
             /\ UNCHANGED <<rline, dline, pc, gone, goneWait, ran, lease, begun, restarts, takenBack, wasLapsed,
                            next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>
TickEnd == /\ line = "tick" /\ line' = "none"
           /\ turnDue' = FALSE /\ tickQueued' = FALSE
           /\ UNCHANGED <<rline, dline, pc, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                          next, serving, tkt, dueFinishes, tickTkt>>

\* a caller's client gives up (its deadline): it is gone, whatever its verb is doing
GiveUp(r) == /\ pc[r] \in {"waiting", "running"} /\ ~gone[r]
             /\ gone' = [gone EXCEPT ![r] = TRUE]
             /\ goneWait' = [goneWait EXCEPT ![r] = (pc[r] = "waiting")]
             /\ UNCHANGED <<line, rline, dline, pc, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                            next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a verb on the line takes it in ticket order (a batch is next when its ticket is
\* served); the design never for a caller gone, and not while the tick's turn is due
TakeLine(r) == /\ pc[r] = "waiting" /\ OnLine(r) /\ line = "none"
               /\ (~gone[r] \/ "rungone" \in Broken)
               /\ (~LineVerb(r) \/ (tkt[r] = serving /\ ("tickqueues" \in Broken \/ ~(turnDue /\ ticks < MaxTicks))))
               /\ line' = r /\ pc' = [pc EXCEPT ![r] = "running"]
               /\ serving' = IF LineVerb(r) THEN serving + 1 ELSE serving
               /\ UNCHANGED <<rline, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                              next, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a batch gone while it waited is served and handed back: its ticket is served, its
\* verbs answered not run
ServeGone(r) == /\ pc[r] = "waiting" /\ LineVerb(r) /\ gone[r] /\ "rungone" \notin Broken
                /\ tkt[r] = serving /\ line = "none"
                /\ ("tickqueues" \in Broken \/ ~(turnDue /\ ticks < MaxTicks))
                /\ serving' = serving + 1
                /\ pc' = [pc EXCEPT ![r] = "dropped"]
                /\ UNCHANGED <<line, rline, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                               next, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a beat starts on the beat lane: no line, no other beat waited for
BeatStart(r) == /\ pc[r] = "waiting" /\ BeatLane(r) /\ ~OnLine(r) /\ ~gone[r]
                /\ pc' = [pc EXCEPT ![r] = "running"]
                /\ UNCHANGED <<line, rline, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                               next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

BeatDrop(r) == /\ pc[r] = "waiting" /\ BeatLane(r) /\ ~OnLine(r) /\ gone[r]
                /\ pc' = [pc EXCEPT ![r] = "dropped"]
                /\ UNCHANGED <<line, rline, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                               next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a read-lane verb starts on the read lane: it waits for a read ahead of it alone,
\* and is dropped as a write is when its caller has gone first; it holds no lease
ReadStart(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "read" /\ ~OnLine(r) /\ rline = "none"
                /\ ~gone[r]
                /\ rline' = r /\ pc' = [pc EXCEPT ![r] = "running"]
                /\ UNCHANGED <<line, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                               next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>
ReadDrop(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "read" /\ ~OnLine(r) /\ gone[r]
               /\ pc' = [pc EXCEPT ![r] = "dropped"]
               /\ UNCHANGED <<line, rline, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                              next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a dashboard read (where --json --dashboard) starts on the dashboard lane
\* (servelanes.go dashWhere): it waits for another dashboard read alone, never for the
\* line nor the read lane, and is dropped as a read is when its caller has gone first.
\* Broken "dashonreadlane": it takes the read lane's holder as a read does.
DashStart(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "dash" /\ ~OnLine(r) /\ ~gone[r]
                /\ IF "dashonreadlane" \in Broken
                   THEN /\ rline = "none" /\ rline' = r /\ dline' = dline
                   ELSE /\ dline = "none" /\ dline' = r /\ rline' = rline
                /\ pc' = [pc EXCEPT ![r] = "running"]
                /\ UNCHANGED <<line, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                               next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>
DashDrop(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "dash" /\ ~OnLine(r) /\ gone[r]
               /\ pc' = [pc EXCEPT ![r] = "dropped"]
               /\ UNCHANGED <<line, rline, dline, gone, goneWait, ran, ticks, lease, begun, restarts, takenBack, wasLapsed,
                              next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a lease lapses (ages): it has not been renewed, so its bound runs out
Age(c) == /\ lease[c] > 0
          /\ lease' = [lease EXCEPT ![c] = @ - 1]
          /\ UNCHANGED <<line, rline, dline, pc, gone, goneWait, ran, ticks, begun, restarts, takenBack, wasLapsed,
                         next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* a server restart: reads with a live lease are kept, lapsed reads are taken back
Restart ==
  /\ restarts = 0
  /\ \E c \in Cards : begun[c]
  /\ restarts' = restarts + 1
  /\ takenBack' = [c \in Cards |-> takenBack[c] \/ (begun[c] /\ lease[c] = 0)]
  /\ wasLapsed' = [c \in Cards |-> wasLapsed[c] \/ (begun[c] /\ lease[c] = 0)]
  /\ UNCHANGED <<line, rline, dline, pc, gone, goneWait, ran, ticks, lease, begun,
                 next, serving, tkt, turnDue, dueFinishes, tickQueued, tickTkt>>

\* the verb runs and is answered; what it held is given back. A begin verb begins a
\* reader card's lease (read --begin).
Finish(r) == /\ pc[r] = "running" /\ KindOf[r] # "queue"
             /\ ran' = [ran EXCEPT ![r] = TRUE]
             /\ pc' = [pc EXCEPT ![r] = "answered"]
             /\ line' = IF line = r THEN "none" ELSE line
             /\ rline' = IF rline = r THEN "none" ELSE rline
             /\ dline' = IF dline = r THEN "none" ELSE dline
             /\ IF KindOf[r] = "begin"
                   THEN /\ \E c \in Cards : ~begun[c]
                                          /\ begun' = [begun EXCEPT ![c] = TRUE]
                                          /\ lease' = [lease EXCEPT ![c] = Bound]
                   ELSE /\ UNCHANGED <<lease, begun>>
             /\ dueFinishes' = IF LineVerb(r) /\ turnDue /\ ticks < MaxTicks THEN dueFinishes + 1 ELSE dueFinishes
             /\ UNCHANGED <<gone, goneWait, ticks, restarts, takenBack, wasLapsed,
                            next, serving, tkt, turnDue, tickQueued, tickTkt>>

\* the served queue's renew (RenewReaderLeases): the reader's beat (queue --as
\* <reader>) runs and is answered, and it renews every live lease with RenewAt or
\* less left to its bound (now + DefaultReadLease); one with more left is not
\* written. Best effort: the renew may fail and its error is ignored (the queue is
\* still served), and under "norenew" it never renews, so the lease is left to lapse.
Renew(r) == /\ pc[r] = "running" /\ KindOf[r] = "queue"
            /\ ran' = [ran EXCEPT ![r] = TRUE]
            /\ pc' = [pc EXCEPT ![r] = "answered"]
            /\ line' = IF line = r THEN "none" ELSE line
            /\ rline' = IF rline = r THEN "none" ELSE rline
            /\ dline' = IF dline = r THEN "none" ELSE dline
            /\ begun' = begun
            /\ lease' = IF "norenew" \in Broken
                        THEN lease
                        ELSE [c \in Cards |-> IF begun[c] /\ lease[c] > 0 /\ lease[c] <= RenewAt THEN Bound ELSE lease[c]]
            /\ dueFinishes' = IF turnDue /\ ticks < MaxTicks THEN dueFinishes + 1 ELSE dueFinishes
            /\ UNCHANGED <<gone, goneWait, ticks, restarts, takenBack, wasLapsed,
                           next, serving, tkt, turnDue, tickQueued, tickTkt>>

Next ==
  \/ TickBegin \/ TickEnd
  \/ Restart
  \/ Turn \/ TickQueue
  \/ \E c \in Cards : Age(c)
  \/ \E r \in Callers : Send(r) \/ GiveUp(r) \/ TakeLine(r) \/ ServeGone(r)
                        \/ BeatStart(r) \/ BeatDrop(r) \/ ReadStart(r) \/ ReadDrop(r)
                        \/ DashStart(r) \/ DashDrop(r)
                        \/ Finish(r) \/ Renew(r)

Fairness ==
  /\ WF_vars(TickBegin) /\ WF_vars(TickEnd)
  /\ WF_vars(Turn)
  /\ WF_vars(TickQueue)
  /\ \A r \in Callers : /\ SF_vars(TakeLine(r)) /\ WF_vars(ServeGone(r))
                        /\ WF_vars(BeatStart(r)) /\ WF_vars(BeatDrop(r))
                        /\ SF_vars(ReadStart(r))
                        /\ SF_vars(DashStart(r)) /\ WF_vars(DashDrop(r))
                        /\ WF_vars(ReadDrop(r)) /\ WF_vars(Finish(r)) /\ WF_vars(Renew(r))
  /\ \A c \in Cards : WF_vars(Age(c))

Spec == Init /\ [][Next]_vars /\ Fairness

----------------------------------------------------------------------------
\* The line has one holder: a line verb runs only holding it, never during a tick.
WriteHoldsTheLine == \A r \in Callers :
  (pc[r] = "running" /\ LineVerb(r)) => line = r

\* A beat waits for nothing: whoever holds the line, a waiting beat whose caller is
\* there can start (a friend's, and a member's that changes nothing but its clock
\* and its load).
BeatNeverWaitsForTheLine == \A r \in Callers :
  (pc[r] = "waiting" /\ BeatLane(r) /\ ~gone[r]) => ENABLED BeatStart(r)

\* A member's beat that changes the member's word (its live set, no-room, draining,
\* facts or stop-returns) runs holding the line, never beside the tick that
\* reconciles the row against it.
ChangeHoldsTheLine == \A r \in Callers :
  (pc[r] = "running" /\ KindOf[r] = "fbeat" /\ Changes[r]) => line = r

\* A read waits for another read alone: with the read lane free, a waiting read
\* whose caller is there can start whoever holds the line.
ReadWaitsOnlyForReads == \A r \in Callers :
  (pc[r] = "waiting" /\ KindOf[r] = "read" /\ rline = "none" /\ ~gone[r]) => ENABLED ReadStart(r)

\* A verb whose caller went before it started never runs.
GoneNeverRuns == \A r \in Callers : goneWait[r] => ~ran[r]

\* The lanes never hold the line.
LanesHoldNoLine == \A r \in Callers : (BeatLane(r) \/ KindOf[r] \in {"read", "dash"}) => line # r

\* The dashboard's read waits for another dashboard read alone: with the dashboard lane
\* free, a waiting dash caller whose caller is there can start, whoever holds the line
\* and whoever holds the read lane (a dashboard that waited behind the reads is the fault
\* of 2026-10-15: its read took 2.5 to 11.4 s through the server, 0.3 s on the store).
DashWaitsOnlyForDash == \A r \in Callers :
  (pc[r] = "waiting" /\ KindOf[r] = "dash" /\ dline = "none" /\ ~gone[r]) => ENABLED DashStart(r)

\* Batches are served in ticket order: no batch runs while an earlier ticket still
\* waits.
FCFS == \A r, s \in Callers :
  (LineVerb(r) /\ LineVerb(s) /\ pc[r] = "running" /\ pc[s] = "waiting")
    => tkt[r] < tkt[s]

\* Once the tick's turn is due, at most the batch already holding the line finishes
\* before the tick begins: no second batch runs in the turn.
TickWaitsOneTurn == (turnDue /\ ticks < MaxTicks) => dueFinishes <= 1

\* A lease renewed inside its bound is never taken back: whenever a served queue
\* (the reader's beat) runs, it leaves every live lease with more than Mark (three
\* quarters) of its bound, renewing to the bound each one with Mark or less left, so
\* a lease a live reader keeps renewing (a beat within each quarter, the prose's
\* assumption: Age is not tied to the polls) never lapses and is never taken back on
\* restart. Under "norenew" the served queue never renews, and under "renewlast" it
\* renews too late (a lease left with Mark or less): the property is stated over the
\* Renew that succeeds.
QueueFinishes == \E r \in Callers :
  KindOf[r] = "queue" /\ pc[r] = "running" /\ pc'[r] = "answered"
LiveLeaseNeverTakenBack ==
  [][\A c \in Cards :
      (QueueFinishes /\ begun[c] /\ lease[c] > 0) =>
        (lease'[c] > Mark /\ (lease[c] <= Mark => lease'[c] = Bound))]_vars

\* Every lapsed read is taken back on restart.
EveryLapsedReadTakenBack == \A c \in Cards :
  wasLapsed[c] => takenBack[c]


\* Every batch sent is answered or dropped (its caller gone).
EveryBatchEnds == \A r \in Callers :
  (pc[r] = "waiting") ~> (pc[r] \in {"answered", "dropped"})
=============================================================================
