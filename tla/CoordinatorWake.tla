------------------------- MODULE CoordinatorWake -------------------------
\* nova-sprint watch --wake (cmd/nova-sprint/watchwake.go): the coordinator is
\* woken by what is new for it, by one line and an exit, and the session runs
\* the verb again. The verb keeps its cursors in a state file, so an event that
\* arrives between two runs is not lost and an event woken is not woken again.
\*
\* One run of the verb reads the bus up to busBatch = 100 entries past the cursor
\* (a look), and one wake takes every ask that look read: the asks are named
\* three and the rest counted, but every one is fired. A message the look read
\* but that does not ask (a ping, a dealt, a land: notAnAsk, watchwake.go:181)
\* is filtered: it moves the cursor and no line. The judgments, the stop and the
\* friends are level state, not cursors: a look reads them afresh, each can set
\* and clear between two looks, and each can clear before any run wakes on it,
\* so NoLapse holds for the bus events alone.
\*
\* The state: the bus events that have arrived, in order, numbered 1..busArrived,
\* each an ask (waking) or a filtered message; cursor, the state file's bus
\* cursor, the last event a run consumed; fired[e], how many wake lines named
\* bus event e; the level events (a judgment, a stop, a friend down) each set or
\* clear between two looks, held[e] the looks in a row each has held, seen[e]
\* the ones a run has woken; run, whether a run is blocked waiting (the session
\* starts one after each line). The outside: bus events and level events arrive
\* and clear at any time, including while no run is waiting.
\*
\* The design, Broken = {}:
\*   ArriveAsk    a bus event that asks is appended; nothing else changes.
\*   ArriveFilter a bus event that does not ask is appended; nothing else.
\*   RaiseLevel   a level event sets; ClearLevel clears it, between two looks.
\*   Rerun        the session starts the verb; it reads the cursor and blocks.
\*   Look         a blocked run reads; no ask and no due level event is found,
\*                so it moves the cursor past the filtered messages, no line.
\*   WakeBus      a blocked run reads; it fires every ask past the cursor, writes
\*                the cursor at the last ask (atomically, renamed into place) and
\*                only then prints the line and exits.
\*   WakeLevel    a blocked run reads no ask, and a level event that has held for
\*                at least Window looks is found: the run wakes on it (seen).
\* Reversed witnesses:
\*   "nocursor"   the run fires every ask but does not write its cursor back: the
\*                next run finds the same asks and fires them again (AtMostOnce).
\*   "skip"       the run fires only the first ask and writes the cursor at the
\*                newest arrival, past asks it has not fired (NoLapse).
\*   "batchdrops" the run fires only the first ask of the batch and writes the
\*                cursor past the rest, dropping the asks between (NoLapse).
EXTENDS Naturals

CONSTANTS MaxBus, MaxLevel, Window, Broken

VARIABLES busArrived, waking, cursor, fired, level, held, seen, run
vars == <<busArrived, waking, cursor, fired, level, held, seen, run>>

BusEvents == 1..MaxBus
LevelEvents == 1..MaxLevel

TypeOK ==
    /\ busArrived \in 0..MaxBus
    /\ waking \in [BusEvents -> BOOLEAN]
    /\ cursor \in 0..MaxBus
    /\ cursor <= busArrived
    /\ fired \in [BusEvents -> Nat]
    /\ level \subseteq LevelEvents
    /\ held \in [LevelEvents -> 0..Window]
    /\ seen \subseteq LevelEvents
    /\ run \in {"idle", "blocked"}

Init ==
    /\ busArrived = 0
    /\ waking = [e \in BusEvents |-> FALSE]
    /\ cursor = 0
    /\ fired = [e \in BusEvents |-> 0]
    /\ level = {}
    /\ held = [e \in LevelEvents |-> 0]
    /\ seen = {}
    /\ run = "idle"

\* a bus event that asks (waking), and one that does not (filtered)
ArriveAsk ==
    /\ busArrived < MaxBus
    /\ busArrived' = busArrived + 1
    /\ waking' = [waking EXCEPT ![busArrived + 1] = TRUE]
    /\ UNCHANGED <<cursor, fired, level, held, seen, run>>

ArriveFilter ==
    /\ busArrived < MaxBus
    /\ busArrived' = busArrived + 1
    /\ waking' = [waking EXCEPT ![busArrived + 1] = FALSE]
    /\ UNCHANGED <<cursor, fired, level, held, seen, run>>

\* a level event sets or clears between two looks; held moves only at a look
RaiseLevel(e) ==
    /\ e \notin level
    /\ level' = level \cup {e}
    /\ UNCHANGED <<busArrived, waking, cursor, fired, held, seen, run>>

ClearLevel(e) ==
    /\ e \in level
    /\ level' = level \ {e}
    /\ UNCHANGED <<busArrived, waking, cursor, fired, held, seen, run>>

Rerun ==
    /\ run = "idle"
    /\ run' = "blocked"
    /\ UNCHANGED <<busArrived, waking, cursor, fired, level, held, seen>>

\* the asks past the cursor; the last and the first of them
WakingAfter == { e \in BusEvents : e > cursor /\ e <= busArrived /\ waking[e] }
LastWaking == CHOOSE e \in WakingAfter : \A f \in WakingAfter : f <= e
FirstWaking == CHOOSE e \in WakingAfter : \A f \in WakingAfter : e <= f

\* a look's view of the level events that have held a window and not yet woken
DueLevel == { e \in LevelEvents : e \in level /\ held[e] >= Window /\ e \notin seen }

\* one look passes: a set level event is one look longer, a cleared one is none
Bump(x) == IF x + 1 > Window THEN Window ELSE x + 1
AdvanceHeld == [e \in LevelEvents |-> IF e \in level THEN Bump(held[e]) ELSE 0]

\* a look forgets the level events it sees cleared (watchwake.go prunes the
\* judgments, the stop and the friends when a look shows them gone), so a
\* clear-and-reappearance between looks need not be observed, and one that is
\* observed lets the event wake again
PruneSeen == seen \cap level

\* a look that wakes nothing: the bus holds no ask (the cursor passes what is
\* filtered, no line) and no level event is due; the run keeps looking.
Look ==
    /\ run = "blocked"
    /\ WakingAfter = {}
    /\ DueLevel = {}
    /\ run' = "blocked"
    /\ cursor' = busArrived
    /\ held' = AdvanceHeld
    /\ seen' = PruneSeen
    /\ UNCHANGED <<busArrived, waking, fired, level>>

\* a blocked run reads every ask past the cursor and fires them all, then writes
\* the cursor and exits.
WakeBus ==
    /\ run = "blocked"
    /\ WakingAfter # {}
    /\ run' = "idle"
    /\ held' = AdvanceHeld
    /\ seen' = PruneSeen
    /\ IF "batchdrops" \in Broken
         THEN /\ fired' = [e \in BusEvents |-> IF e = FirstWaking THEN fired[e] + 1 ELSE fired[e]]
              /\ cursor' = LastWaking
       ELSE IF "skip" \in Broken
         THEN /\ fired' = [e \in BusEvents |-> IF e = FirstWaking THEN fired[e] + 1 ELSE fired[e]]
              /\ cursor' = busArrived
       ELSE IF "nocursor" \in Broken
         THEN /\ fired' = [e \in BusEvents |-> IF e \in WakingAfter THEN fired[e] + 1 ELSE fired[e]]
              /\ cursor' = cursor
       ELSE /\ fired' = [e \in BusEvents |-> IF e \in WakingAfter THEN fired[e] + 1 ELSE fired[e]]
            /\ cursor' = LastWaking
    /\ UNCHANGED <<busArrived, waking, level>>

\* a blocked run reads no ask, and a level event has held a whole window: the run
\* wakes on every such event (seen).
WakeLevel ==
    /\ run = "blocked"
    /\ WakingAfter = {}
    /\ DueLevel # {}
    /\ run' = "idle"
    /\ seen' = PruneSeen \cup DueLevel
    /\ cursor' = busArrived
    /\ held' = AdvanceHeld
    /\ UNCHANGED <<busArrived, waking, fired, level>>

Next == ArriveAsk \/ ArriveFilter
     \/ (\E e \in LevelEvents : RaiseLevel(e))
     \/ (\E e \in LevelEvents : ClearLevel(e))
     \/ Rerun \/ Look \/ WakeBus \/ WakeLevel

Spec == Init /\ [][Next]_vars /\ WF_vars(Rerun) /\ WF_vars(WakeBus) /\ WF_vars(WakeLevel)

\* a bus event is woken at most once
AtMostOnce == \A e \in BusEvents : fired[e] <= 1

\* every bus event that asks and arrives is woken by some later run (the level
\* events are not here: a level event cleared before a wake never wakes)
NoLapse == \A e \in BusEvents : (busArrived >= e /\ waking[e]) ~> (fired[e] >= 1)

\* a level event that holds for a whole window of looks wakes a run, unless it is
\* cleared first: the clear is the level event's own lapse, which NoLapse does
\* not cover.
LevelSeenWhileItHolds ==
    \A e \in LevelEvents : (e \in level /\ held[e] >= Window) ~> (e \in seen \/ e \notin level)
=============================================================================
