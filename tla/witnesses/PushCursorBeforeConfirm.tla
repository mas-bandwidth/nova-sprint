---------------------- MODULE PushCursorBeforeConfirm -----------------------
(***************************************************************************)
(* Layer 4 bus push cursor model (docs/LAYER-4-EDGES.md, Guarantee 2).     *)
(* Events are committed in sequence 1..MaxSeq; a recipient cursor advances *)
(* only after nova-bus send exits 0 and prints the recipient and message   *)
(* id. A failed send or a lost reply leaves the cursor unmoved so the next *)
(* tick resends from the cursor (at-least-once, never skipped).            *)
(* Size: 3 events, 1 crash.                                                *)
(***************************************************************************)
EXTENDS Integers, FiniteSets

CONSTANTS MaxSeq, MaxCrashes

VARIABLES events, cursor, pending, delivered, confirmed, crashes

vars == <<events, cursor, pending, delivered, confirmed, crashes>>

BatchRange(b) == IF b.from <= b.to THEN b.from .. b.to ELSE {}

Init ==
  /\ events = 0
  /\ cursor = 0
  /\ pending = [active |-> FALSE, from |-> 0, to |-> 0]
  /\ delivered = {}
  /\ confirmed = {}
  /\ crashes = 0

CommitEvent ==
  /\ events < MaxSeq
  /\ events' = events + 1
  /\ UNCHANGED <<cursor, pending, delivered, confirmed, crashes>>

StartSend ==
  /\ ~pending.active
  /\ cursor < events
  /\ pending' = [active |-> TRUE, from |-> cursor + 1, to |-> events]
  /\ cursor' = events   \* FAULT: cursor set before send id came back
  /\ UNCHANGED <<events, delivered, confirmed, crashes>>

SendConfirmed ==
  /\ pending.active
  /\ delivered' = delivered \cup BatchRange(pending)
  /\ confirmed' = confirmed \cup BatchRange(pending)
  /\ cursor' = pending.to
  /\ pending' = [active |-> FALSE, from |-> 0, to |-> 0]
  /\ UNCHANGED <<events, crashes>>

SendFailed ==
  /\ pending.active
  /\ crashes < MaxCrashes
  /\ crashes' = crashes + 1
  /\ pending' = [active |-> FALSE, from |-> 0, to |-> 0]
  /\ cursor' = cursor   \* failed send never advances cursor
  /\ UNCHANGED <<events, delivered, confirmed>>

ReplyLost ==
  /\ pending.active
  /\ crashes < MaxCrashes
  /\ crashes' = crashes + 1
  /\ delivered' = delivered \cup BatchRange(pending)
  /\ pending' = [active |-> FALSE, from |-> 0, to |-> 0]
  /\ cursor' = cursor   \* lost reply never advances cursor
  /\ UNCHANGED <<events, confirmed>>

Crash ==
  /\ crashes < MaxCrashes
  /\ crashes' = crashes + 1
  /\ pending' = [active |-> FALSE, from |-> 0, to |-> 0]
  /\ UNCHANGED <<events, cursor, delivered, confirmed>>

Idle ==
  /\ events = MaxSeq
  /\ cursor = events
  /\ ~pending.active
  /\ UNCHANGED vars

Next ==
  \/ CommitEvent
  \/ StartSend
  \/ SendConfirmed
  \/ SendFailed
  \/ ReplyLost
  \/ Crash
  \/ Idle

Spec == Init /\ [][Next]_vars /\ WF_vars(CommitEvent) /\ WF_vars(StartSend) /\ WF_vars(SendConfirmed)

TypeOK ==
  /\ events \in 0..MaxSeq
  /\ cursor \in 0..MaxSeq
  /\ pending.active \in BOOLEAN
  /\ pending.from \in 0..MaxSeq
  /\ pending.to \in 0..MaxSeq
  /\ delivered \subseteq (1..MaxSeq)
  /\ confirmed \subseteq (1..MaxSeq)
  /\ crashes \in 0..MaxCrashes

CursorBounded == cursor <= events

EveryBelowCursorConfirmed ==
  \A e \in 1..cursor : e \in confirmed

NoEventSkipped ==
  \A e \in 1..cursor : e \in delivered

Liveness == <>[](cursor = MaxSeq /\ \A e \in 1..MaxSeq : e \in confirmed)
=============================================================================
