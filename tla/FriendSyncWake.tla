--------------------------- MODULE FriendSyncWake ---------------------------
\* nova-sprint friend sync --every (cmd/nova-sprint/friend_loop.go,
\* cmd/nova-sprint/friendcards.go, cmd/nova-sprint/friends.go): a card dealt to
\* a friend row (the coordinator seat's own row included) is committed on the
\* sprint log by the tick (friendDealPass, internal/sprint/friend_deal.go).
\* friendSyncLoop blocks on Store.WaitLog from the last log entry its pass saw,
\* bounded by --every, so a deal commit wakes the loop within one log event
\* rather than waiting for the next --every timer poll. The --every wait stays
\* as the quiet-log backstop sweep. Both batch and one-shot tells carry
\* kind=status so the seat's own row is told as a machine status line.
\*
\* Variables:
\*   log     the set of <<friend, card>> deals committed to the sprint log
\*   cursor  the log length the friend sync loop has woken on
\*   inbox   each friend's delivered inbox briefs (inbox/<job>/BRIEF.md)
\*   told    each friend's cards told on the bus
\*   clock   the loop's wake/pass/tell state and --every backstop clock
\*
\* Reversed witnesses:
\*   "clockonly"  removes Wake so the loop runs only on the --every Sweep,
\*                violating PushedWithinOneEvent.
\*   "telllost"   drops a tell with no backstop Sweep recovery, violating
\*                EveryDealIsPushed.
EXTENDS Naturals, FiniteSets

CONSTANTS Friends, Cards, Broken

VARIABLES log, cursor, inbox, told, clock
vars == <<log, cursor, inbox, told, clock>>

DealtCards == { c \in Cards : \E f \in Friends : <<f, c>> \in log }
LogLen == Cardinality(log)
Undelivered == { <<f, c>> \in log : c \notin inbox[f] }
Untold == { <<f, c>> \in log : c \notin told[f] }

ClockStates == {"waiting", "woke", "passed", "lost", "reswept", "repassed", "swept"}

TypeOK ==
    /\ log \subseteq (Friends \X Cards)
    /\ cursor \in 0..Cardinality(Cards)
    /\ cursor <= LogLen
    /\ inbox \in [Friends -> SUBSET Cards]
    /\ told \in [Friends -> SUBSET Cards]
    /\ clock \in ClockStates

Init ==
    /\ log = {}
    /\ cursor = 0
    /\ inbox = [f \in Friends |-> {}]
    /\ told = [f \in Friends |-> {}]
    /\ clock = "waiting"

\* A tick commits a deal of card c to friend row f on the sprint log.
Deal(f, c) ==
    /\ c \notin DealtCards
    /\ log' = log \cup {<<f, c>>}
    /\ UNCHANGED <<cursor, inbox, told, clock>>

\* WaitLog wakes the loop when the sprint log has advanced past cursor.
WakeEnabled ==
    /\ "clockonly" \notin Broken
    /\ clock \in {"waiting", "lost"}
    /\ LogLen > cursor

Wake ==
    /\ WakeEnabled
    /\ cursor' = LogLen
    /\ clock' = IF clock = "lost" THEN "reswept" ELSE "woke"
    /\ UNCHANGED <<log, inbox, told>>

\* The --every backstop fires when no log wake is pending (or in clock-only
\* broken mode when Wake is removed).
Sweep ==
    /\ "telllost" \notin Broken
    /\ \/ (clock = "lost" /\ LogLen = cursor)
       \/ (clock = "waiting" /\ LogLen = cursor /\ Untold # {})
       \/ ("clockonly" \in Broken /\ clock = "waiting" /\ LogLen > cursor)
    /\ cursor' = LogLen
    /\ clock' = IF Undelivered # {} THEN "swept"
                ELSE IF clock = "lost" THEN "reswept"
                ELSE "woke"
    /\ UNCHANGED <<log, inbox, told>>

\* friendSyncPass writes each dealt card's BRIEF.md into the friend's inbox.
Pass ==
    /\ clock \in {"woke", "reswept", "swept"}
    /\ inbox' = [f \in Friends |-> { c \in Cards : <<f, c>> \in log }]
    /\ clock' = IF clock = "reswept" THEN "repassed" ELSE "passed"
    /\ UNCHANGED <<log, cursor, told>>

\* wakeFriend / wakeFriendPass tells the friend on the bus with kind=status.
Tell ==
    /\ clock \in {"passed", "repassed"}
    /\ told' = [f \in Friends |-> told[f] \cup inbox[f]]
    /\ clock' = "waiting"
    /\ UNCHANGED <<log, cursor, inbox>>

\* A transient bus send failure drops the tell; the backstop Sweep catches it.
TellLost ==
    /\ clock = "passed"
    /\ \E f \in Friends : inbox[f] \ told[f] # {}
    /\ clock' = "lost"
    /\ UNCHANGED <<log, cursor, inbox, told>>

Next ==
    \/ (\E f \in Friends, c \in Cards : Deal(f, c))
    \/ Wake
    \/ Sweep
    \/ Pass
    \/ Tell
    \/ TellLost

Spec ==
    /\ Init
    /\ [][Next]_vars
    /\ WF_vars(Wake)
    /\ WF_vars(Sweep)
    /\ WF_vars(Pass)
    /\ WF_vars(Tell)

\* Only dealt cards are delivered, and only delivered cards are told.
DeliveredWasDealt ==
    \A f \in Friends : \A c \in inbox[f] : <<f, c>> \in log

ToldWasDelivered ==
    \A f \in Friends : told[f] \subseteq inbox[f]

\* No --every Sweep fires while a dealt card sits undelivered: log wake
\* delivers every deal before the backstop timer is needed.
PushedWithinOneEvent ==
    clock # "swept"

\* Every dealt card is eventually delivered to its friend's inbox and told on
\* the bus.
EveryDealIsPushed ==
    \A f \in Friends, c \in Cards :
        (<<f, c>> \in log) ~> (c \in inbox[f] /\ c \in told[f])
=============================================================================
