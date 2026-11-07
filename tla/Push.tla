-------------------------------- MODULE Push --------------------------------
(***************************************************************************)
(* Layer 4 bus push cursor model (docs/LAYER-4-EDGES.md, Guarantee 2 & 3). *)
(* Models multiple recipients, an event window, and the 4 KB message cap.  *)
(* Each recipient cursor advances past irrelevant event windows and moves  *)
(* past relevant events only after nova-bus send confirms delivery; one    *)
(* failing recipient never starves another.                                *)
(***************************************************************************)
EXTENDS Integers, FiniteSets

CONSTANTS MaxSeq, Window, Cap, MaxCrashes

Recipients == {"r1", "r2"}

VARIABLES events, cursor, pending, delivered, confirmed, crashes

vars == <<events, cursor, pending, delivered, confirmed, crashes>>

Target(e) == IF e = 2 THEN "r2" ELSE "r1"

WinEnd(r) == IF cursor[r] + Window < events THEN cursor[r] + Window ELSE events

Relevant(r, a, b) == IF a <= b THEN {e \in a..b : Target(e) = r} ELSE {}

EmptyPending == [active |-> FALSE, from |-> 0, to |-> 0, rel |-> {}, size |-> 0]

Init ==
  /\ events = 0
  /\ cursor = [r \in Recipients |-> 0]
  /\ pending = [r \in Recipients |-> EmptyPending]
  /\ delivered = [r \in Recipients |-> {}]
  /\ confirmed = [r \in Recipients |-> {}]
  /\ crashes = 0

CommitEvent ==
  /\ events < MaxSeq
  /\ events' = events + 1
  /\ UNCHANGED <<cursor, pending, delivered, confirmed, crashes>>

SkipIrrelevant(r) ==
  /\ ~pending[r].active
  /\ cursor[r] < events
  /\ Relevant(r, cursor[r] + 1, WinEnd(r)) = {}
  /\ cursor' = [cursor EXCEPT ![r] = WinEnd(r)]
  /\ UNCHANGED <<events, pending, delivered, confirmed, crashes>>

StartSend(r) ==
  /\ ~pending[r].active
  /\ cursor[r] < events
  /\ LET to == WinEnd(r)
         rel == Relevant(r, cursor[r] + 1, to)
         sz == IF Cardinality(rel) <= Cap THEN Cardinality(rel) ELSE Cap
     IN /\ rel # {}
        /\ pending' = [pending EXCEPT ![r] = [active |-> TRUE, from |-> cursor[r] + 1, to |-> to, rel |-> rel, size |-> sz]]
        /\ cursor' = cursor   \* cursor waits for send confirmation
        /\ UNCHANGED <<events, delivered, confirmed, crashes>>

SendConfirmed(r) ==
  /\ pending[r].active
  /\ delivered' = [delivered EXCEPT ![r] = @ \cup pending[r].rel]
  /\ confirmed' = [confirmed EXCEPT ![r] = @ \cup pending[r].rel]
  /\ cursor' = [cursor EXCEPT ![r] = pending[r].to]
  /\ pending' = [pending EXCEPT ![r] = EmptyPending]
  /\ UNCHANGED <<events, crashes>>

SendFailed(r) ==
  /\ pending[r].active
  /\ crashes < MaxCrashes
  /\ crashes' = crashes + 1
  /\ pending' = [pending EXCEPT ![r] = EmptyPending]
  /\ cursor' = cursor   \* failed send on r leaves r unmoved and never starves other recipients
  /\ UNCHANGED <<events, delivered, confirmed>>

ReplyLost(r) ==
  /\ pending[r].active
  /\ crashes < MaxCrashes
  /\ crashes' = crashes + 1
  /\ delivered' = [delivered EXCEPT ![r] = @ \cup pending[r].rel]
  /\ pending' = [pending EXCEPT ![r] = EmptyPending]
  /\ cursor' = cursor
  /\ UNCHANGED <<events, confirmed>>

Crash ==
  /\ crashes < MaxCrashes
  /\ crashes' = crashes + 1
  /\ pending' = [r \in Recipients |-> EmptyPending]
  /\ UNCHANGED <<events, cursor, delivered, confirmed>>

Idle ==
  /\ events = MaxSeq
  /\ \A r \in Recipients : cursor[r] = events /\ ~pending[r].active
  /\ UNCHANGED vars

Next ==
  \/ CommitEvent
  \/ \E r \in Recipients :
       \/ SkipIrrelevant(r)
       \/ StartSend(r)
       \/ SendConfirmed(r)
       \/ SendFailed(r)
       \/ ReplyLost(r)
  \/ Crash
  \/ Idle

FairRecipient(r) ==
  /\ WF_vars(SkipIrrelevant(r))
  /\ WF_vars(StartSend(r))
  /\ WF_vars(SendConfirmed(r))

Spec == Init /\ [][Next]_vars /\ WF_vars(CommitEvent) /\ FairRecipient("r1") /\ FairRecipient("r2")

TypeOK ==
  /\ events \in 0..MaxSeq
  /\ cursor \in [Recipients -> 0..MaxSeq]
  /\ \A r \in Recipients :
       /\ pending[r].active \in BOOLEAN
       /\ pending[r].from \in 0..(MaxSeq + 1)
       /\ pending[r].to \in 0..MaxSeq
       /\ pending[r].rel \subseteq (1..MaxSeq)
       /\ pending[r].size \in 0..Cap
       /\ delivered[r] \subseteq (1..MaxSeq)
       /\ confirmed[r] \subseteq (1..MaxSeq)
  /\ crashes \in 0..MaxCrashes

CursorBounded ==
  \A r \in Recipients : cursor[r] <= events

MessageWithinCap ==
  \A r \in Recipients : pending[r].size <= Cap

EveryBelowCursorConfirmed ==
  \A r \in Recipients : \A e \in 1..cursor[r] : Target(e) = r => e \in confirmed[r]

(* Guarantee 2: every event for a recipient below cursor[r] is either      *)
(* carried in a push or summarised in one (up to Cap items, with           *)
(* "and N more: nova-sprint next --as <r>" retrieving the rest), and       *)
(* cursor[r] advances past both only after nova-bus send confirms it.      *)
NoEventSkipped ==
  \A r \in Recipients : \A e \in 1..cursor[r] : Target(e) = r => e \in delivered[r]

Liveness ==
  <>[](\A r \in Recipients : cursor[r] = MaxSeq /\ \A e \in 1..MaxSeq : Target(e) = r => e \in confirmed[r])
=============================================================================
