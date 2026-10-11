---------------------------- MODULE ServerLanes ------------------------------
(***************************************************************************)
(* The sprint server's line of control and its lanes (cmd/nova-sprint      *)
(* serve.go serveCtx, servelanes.go, seriallock.go; docs/SPEC-SPRINT.md    *)
(* section 14, The server). One line (app.serial): the run loop's tick and *)
(* each batch of writes hold it in turn. A friend's beat runs on the beat  *)
(* lane and a read on the read lane, neither on the line; the read lane is *)
(* its own line, one read at a time. A caller can go away at any time      *)
(* (its client's deadline): a write that has not taken the line by then is *)
(* dropped, never run.                                                     *)
(*                                                                         *)
(* A read asked of a reader is held by a lease on a reader card in the     *)
(* Readers Reading column (internal/sprint/readers.go): read --begin, a    *)
(* line verb, begins it (begun + DefaultReadLease); the reader's beat      *)
(* renews it on a served queue (queue --as <reader>, setting lease to now  *)
(* + DefaultReadLease), under the held line, errors ignored (a renew may   *)
(* fail); a lease not renewed expires. On start the server keeps every     *)
(* read whose lease is live and only takes back reads whose lease lapsed.  *)
(* Read-lane verbs (where, card, log, ...) hold no lease.                  *)
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
(*   "norenew"     the served queue never renews a lease: a lease begun    *)
(*                 and not renewed expires and is taken back, so           *)
(*                 LiveLeaseNeverTakenBack fails.                          *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Callers, KindOf, MaxTicks, Broken, Cards, Bound

Kinds  == {"beat", "read", "write", "begin", "queue"}
Faults == {"beatonline", "readonline", "rungone", "norenew"}
ASSUME Broken \subseteq Faults /\ "tick" \notin Callers /\ "none" \notin Callers
ASSUME \A r \in Callers : KindOf[r] \in Kinds
ASSUME Bound > 0

VARIABLES line,      \* the line's holder: "none", "tick" or a caller
          rline,     \* the read lane's holder: "none" or a caller
          pc,        \* each caller: idle, waiting, running, answered, dropped
          gone,      \* each caller: its client has gone away
          goneWait,  \* ghost: it went while it was still waiting to start
          ran,       \* each caller: its verb ran (changed the sprint)
          ticks,     \* ticks begun, bounded by MaxTicks
          lease,     \* each card: 0 (none or lapsed), or Bound..1 left live
          begun,     \* each card: its read is in flight (read --begin ran)
          renewed,   \* ghost: each card: a renew succeeded inside its bound
          restarts,  \* server restarts, bounded by 1
          takenBack, \* ghost: each card: taken back on restart
          wasLapsed  \* ghost: each card: lapsed when restart happened

vars == <<line, rline, pc, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

OnLine(r) == KindOf[r] \in {"write", "begin", "queue"}
             \/ (KindOf[r] = "beat" /\ "beatonline" \in Broken)
             \/ (KindOf[r] = "read" /\ "readonline" \in Broken)

TypeOK ==
  /\ line \in {"none", "tick"} \cup Callers
  /\ rline \in {"none"} \cup Callers
  /\ pc \in [Callers -> {"idle", "waiting", "running", "answered", "dropped"}]
  /\ gone \in [Callers -> BOOLEAN]
  /\ goneWait \in [Callers -> BOOLEAN]
  /\ ran \in [Callers -> BOOLEAN]
  /\ ticks \in 0..MaxTicks
  /\ lease \in [Cards -> 0..Bound]
  /\ begun \in [Cards -> BOOLEAN]
  /\ renewed \in [Cards -> BOOLEAN]
  /\ restarts \in 0..1
  /\ takenBack \in [Cards -> BOOLEAN]
  /\ wasLapsed \in [Cards -> BOOLEAN]

Init ==
  /\ line = "none" /\ rline = "none"
  /\ pc = [r \in Callers |-> "idle"]
  /\ gone = [r \in Callers |-> FALSE]
  /\ goneWait = [r \in Callers |-> FALSE]
  /\ ran = [r \in Callers |-> FALSE]
  /\ ticks = 0
  /\ lease = [c \in Cards |-> 0]
  /\ begun = [c \in Cards |-> FALSE]
  /\ renewed = [c \in Cards |-> FALSE]
  /\ restarts = 0
  /\ takenBack = [c \in Cards |-> FALSE]
  /\ wasLapsed = [c \in Cards |-> FALSE]

\* a caller sends its batch
Send(r) == /\ pc[r] = "idle"
           /\ pc' = [pc EXCEPT ![r] = "waiting"]
           /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

\* the run loop takes the line for a tick, and gives it back
TickBegin == /\ line = "none" /\ ticks < MaxTicks
             /\ line' = "tick" /\ ticks' = ticks + 1
             /\ UNCHANGED <<rline, pc, gone, goneWait, ran, lease, begun, renewed, restarts, takenBack, wasLapsed>>
TickEnd == /\ line = "tick" /\ line' = "none"
           /\ UNCHANGED <<rline, pc, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

\* a caller's client gives up (its deadline): it is gone, whatever its verb is doing
GiveUp(r) == /\ pc[r] \in {"waiting", "running"} /\ ~gone[r]
             /\ gone' = [gone EXCEPT ![r] = TRUE]
             /\ goneWait' = [goneWait EXCEPT ![r] = (pc[r] = "waiting")]
             /\ UNCHANGED <<line, rline, pc, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

\* a verb on the line takes it (serialLock.LockCtx); the design never for a caller gone
TakeLine(r) == /\ pc[r] = "waiting" /\ OnLine(r) /\ line = "none"
               /\ (~gone[r] \/ "rungone" \in Broken)
               /\ line' = r /\ pc' = [pc EXCEPT ![r] = "running"]
               /\ UNCHANGED <<rline, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

\* a caller gone while it waited for the line is dropped: its verbs answered not run
Drop(r) == /\ pc[r] = "waiting" /\ OnLine(r) /\ gone[r] /\ "rungone" \notin Broken
           /\ pc' = [pc EXCEPT ![r] = "dropped"]
           /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

\* a beat starts on the beat lane: no line, no other beat waited for
BeatStart(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "beat" /\ ~OnLine(r) /\ ~gone[r]
                /\ pc' = [pc EXCEPT ![r] = "running"]
                /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

BeatDrop(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "beat" /\ ~OnLine(r) /\ gone[r]
                /\ pc' = [pc EXCEPT ![r] = "dropped"]
                /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

\* a read-lane verb starts on the read lane: it waits for a read ahead of it alone,
\* and is dropped as a write is when its caller has gone first; it holds no lease
ReadStart(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "read" /\ ~OnLine(r) /\ rline = "none"
                /\ ~gone[r]
                /\ rline' = r /\ pc' = [pc EXCEPT ![r] = "running"]
                /\ UNCHANGED <<line, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>
ReadDrop(r) == /\ pc[r] = "waiting" /\ KindOf[r] = "read" /\ ~OnLine(r) /\ gone[r]
               /\ pc' = [pc EXCEPT ![r] = "dropped"]
               /\ UNCHANGED <<line, rline, gone, goneWait, ran, ticks, lease, begun, renewed, restarts, takenBack, wasLapsed>>

\* a lease lapses (ages): it has not been renewed, so its bound runs out
Age(c) == /\ lease[c] > 0
          /\ lease' = [lease EXCEPT ![c] = @ - 1]
          /\ UNCHANGED <<line, rline, pc, gone, goneWait, ran, ticks, begun, renewed, restarts, takenBack, wasLapsed>>

\* a server restart: reads with a live lease are kept, lapsed reads are taken back
Restart ==
  /\ restarts = 0
  /\ \E c \in Cards : begun[c]
  /\ restarts' = restarts + 1
  /\ takenBack' = [c \in Cards |-> takenBack[c] \/ (begun[c] /\ lease[c] = 0)]
  /\ wasLapsed' = [c \in Cards |-> wasLapsed[c] \/ (begun[c] /\ lease[c] = 0)]
  /\ UNCHANGED <<line, rline, pc, gone, goneWait, ran, ticks, lease, begun, renewed>>

\* the verb runs and is answered; what it held is given back. A begin verb begins a
\* reader card's lease (read --begin); a served queue renews every live lease, best
\* effort: under "norenew" the renew never happens and the lease is left to lapse
Finish(r) == /\ pc[r] = "running"
             /\ ran' = [ran EXCEPT ![r] = TRUE]
             /\ pc' = [pc EXCEPT ![r] = "answered"]
             /\ line' = IF line = r THEN "none" ELSE line
             /\ rline' = IF rline = r THEN "none" ELSE rline
             /\ IF KindOf[r] = "begin"
                   THEN /\ \E c \in Cards : ~begun[c]
                                          /\ begun' = [begun EXCEPT ![c] = TRUE]
                                          /\ lease' = [lease EXCEPT ![c] = Bound]
                        /\ UNCHANGED <<renewed>>
                   ELSE IF KindOf[r] = "queue" /\ "norenew" \notin Broken
                        THEN /\ lease' = [c \in Cards |-> IF begun[c] /\ lease[c] > 0 THEN Bound ELSE lease[c]]
                             /\ renewed' = [c \in Cards |-> renewed[c] \/ (begun[c] /\ lease[c] > 0)]
                             /\ begun' = begun
                        ELSE /\ UNCHANGED <<lease, begun, renewed>>
             /\ UNCHANGED <<gone, goneWait, ticks, restarts, takenBack, wasLapsed>>

Next ==
  \/ TickBegin \/ TickEnd
  \/ Restart
  \/ \E c \in Cards : Age(c)
  \/ \E r \in Callers : Send(r) \/ GiveUp(r) \/ TakeLine(r) \/ Drop(r)
                        \/ BeatStart(r) \/ BeatDrop(r) \/ ReadStart(r) \/ ReadDrop(r) \/ Finish(r)

Fairness ==
  /\ WF_vars(TickEnd)
  /\ \A r \in Callers : /\ SF_vars(TakeLine(r)) /\ WF_vars(Drop(r))
                        /\ WF_vars(BeatStart(r)) /\ WF_vars(BeatDrop(r))
                        /\ SF_vars(ReadStart(r))
                        /\ WF_vars(ReadDrop(r)) /\ WF_vars(Finish(r))
  /\ \A c \in Cards : WF_vars(Age(c))

Spec == Init /\ [][Next]_vars /\ Fairness

----------------------------------------------------------------------------
\* The line has one holder: a line verb runs only holding it, never during a tick.
WriteHoldsTheLine == \A r \in Callers :
  (pc[r] = "running" /\ KindOf[r] \in {"write", "begin", "queue"}) => line = r

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
LanesHoldNoLine == \A r \in Callers : KindOf[r] \in {"beat", "read"} => line # r

\* A lease renewed inside its bound is never taken back: whenever a served queue
\* (the reader's beat) sees a live lease it renews it, so a lease a live reader
\* keeps renewing never lapses and is never taken back on restart. Under
\* "norenew" the served queue never renews, the lease lapses, and the read is
\* taken back: the property is stated over the renew that succeeds.
QueueFinishes == \E r \in Callers :
  KindOf[r] = "queue" /\ pc[r] = "running" /\ pc'[r] = "answered"
LiveLeaseNeverTakenBack ==
  [][\A c \in Cards :
      (QueueFinishes /\ begun[c] /\ lease[c] > 0) => (lease'[c] = Bound)]_vars

\* Every lapsed read is taken back on restart.
EveryLapsedReadTakenBack == \A c \in Cards :
  wasLapsed[c] => takenBack[c]


\* Every batch sent is answered or dropped (its caller gone).
EveryBatchEnds == \A r \in Callers :
  (pc[r] = "waiting") ~> (pc[r] \in {"answered", "dropped"})
=============================================================================
