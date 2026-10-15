--------------------------- MODULE FriendSyncWake ---------------------------
\* friend_loop captures its cursor before the pass. friendcards prepares durable
\* intent before BRIEF, freezes one token/payload, and records Sent only after
\* receipt. A lost response may already have committed one message. Retry never
\* changes its token. At the original retry deadline pending intent becomes one
\* receipt-unknown judgment, never Sent. Legacy BRIEFs without intent are outside
\* this new-delivery model. Epoch changes name new jobs; old intents never send
\* into the new epoch. Restart preserves prepared, accepted, told and unknown.
\* CloseWindow abstracts the fixed24h deadline; Expire is its serviced sweep. Weak
\* fairness assumes the store and loop eventually run; this is NOT a wall-clock
\* guarantee while the process/store is unavailable. A send may succeed or fail
\* within its window; expiry is the terminal alternative to successful receipt.
EXTENDS Naturals, FiniteSets
CONSTANTS Friends, Cards, Broken
VARIABLES log, cursor, inbox, told, clock, prepared, accepted, unknown, sends, expired, restarts
vars == <<log, cursor, inbox, told, clock, prepared, accepted, unknown, sends, expired, restarts>>
Pairs == Friends \X Cards
DealtCards == { c \in Cards : \E f \in Friends : <<f,c>> \in log }
LogLen == Cardinality(log)
Delivered == {<<f,c>> \in Pairs : c \in inbox[f]}
Receipted == {<<f,c>> \in Pairs : c \in told[f]}
Pending == prepared \ (Receipted \cup unknown)
Retry == Pending \ expired
Undelivered == log \ Delivered
ClockStates == {"waiting","woke","prepared","passed","lost","reswept","reprepared","repassed","swept"}
TypeOK == /\ log \subseteq Pairs
          /\ cursor \in 0..Cardinality(Cards)
          /\ cursor <= LogLen
          /\ inbox \in [Friends -> SUBSET Cards]
          /\ told \in [Friends -> SUBSET Cards]
          /\ clock \in ClockStates
          /\ prepared \subseteq log
          /\ accepted \subseteq prepared
          /\ unknown \subseteq prepared
          /\ expired \subseteq prepared
          /\ sends \in [Pairs -> 0..2]
          /\ restarts \in 0..1
Init == /\ log = {} /\ cursor = 0
        /\ inbox = [f \in Friends |-> {}]
        /\ told = [f \in Friends |-> {}]
        /\ clock = "waiting"
        /\ prepared = {} /\ accepted = {} /\ unknown = {} /\ expired = {}
        /\ sends = [p \in Pairs |-> 0]
        /\ restarts = 0
Deal(f,c) == /\ c \notin DealtCards
             /\ log' = log \cup {<<f,c>>}
             /\ UNCHANGED <<cursor,inbox,told,clock,prepared,accepted,unknown,sends,expired,restarts>>
WakeEnabled == /\ "clockonly" \notin Broken
               /\ clock \in {"waiting","lost"}
               /\ LogLen > cursor
Wake == /\ WakeEnabled /\ cursor' = LogLen
        /\ clock' = IF clock="lost" THEN "reswept" ELSE "woke"
        /\ UNCHANGED <<log,inbox,told,prepared,accepted,unknown,sends,expired,restarts>>
Sweep == /\ "telllost" \notin Broken
         /\ \/ (clock="lost" /\ LogLen=cursor)
            \/ (clock="waiting" /\ LogLen=cursor /\ Pending # {})
            \/ ("clockonly" \in Broken /\ clock="waiting" /\ LogLen>cursor)
         /\ cursor'=LogLen
         /\ clock'=IF Undelivered # {} THEN "swept"
                    ELSE IF clock="lost" THEN "reswept" ELSE "woke"
         /\ UNCHANGED <<log,inbox,told,prepared,accepted,unknown,sends,expired,restarts>>
Prepare == /\ clock \in {"woke","reswept","swept"}
           /\ prepared'=prepared \cup log
           /\ clock'=IF clock="reswept" THEN "reprepared" ELSE "prepared"
           /\ UNCHANGED <<log,cursor,inbox,told,accepted,unknown,sends,expired,restarts>>
Pass == /\ clock \in {"prepared","reprepared"}
        /\ inbox'=[f \in Friends |-> inbox[f] \cup {c \in Cards : <<f,c>> \in prepared}]
        /\ clock'=IF clock="reprepared" THEN "repassed" ELSE "passed"
        /\ UNCHANGED <<log,cursor,told,prepared,accepted,unknown,sends,expired,restarts>>
\* A stable token replays an accepted send without appending a second message.
NextCounts == [p \in Pairs |-> IF p \in Retry /\ (p \notin accepted \/ "freshtoken" \in Broken)
                            THEN sends[p]+1 ELSE sends[p]]
Tell == /\ clock \in {"passed","repassed"}
        /\ "ackfirst" \notin Broken
        /\ accepted'=accepted \cup Retry
        /\ sends'=NextCounts
        /\ told'=[f \in Friends |-> told[f] \cup {c \in Cards : <<f,c>> \in Retry}]
        /\ clock'="waiting"
        /\ UNCHANGED <<log,cursor,inbox,prepared,unknown,expired,restarts>>
TellLost == /\ clock \in {"passed","repassed"} /\ Retry # {}
            /\ \E committed \in BOOLEAN :
                 /\ accepted'=IF committed THEN accepted \cup Retry ELSE accepted
                 /\ sends'=IF committed THEN NextCounts ELSE sends
            /\ clock'="lost"
            /\ UNCHANGED <<log,cursor,inbox,told,prepared,unknown,expired,restarts>>
\* Never reset the first prepared deadline. Unknown is durable and terminal.
CloseWindow == /\ "telllost" \notin Broken /\ Retry # {}
               /\ expired'=expired \cup Pending
               /\ UNCHANGED <<log,cursor,inbox,told,clock,prepared,accepted,unknown,sends,restarts>>
Expire == /\ "telllost" \notin Broken /\ Pending \cap expired # {}
          /\ unknown'=unknown \cup (Pending \cap expired)
          /\ UNCHANGED <<log,cursor,inbox,told,clock,prepared,accepted,sends,expired,restarts>>
AckBeforeSend == /\ "ackfirst" \in Broken /\ clock \in {"passed","repassed"} /\ Retry # {}
                 /\ told'=[f \in Friends |-> told[f] \cup {c \in Cards : <<f,c>> \in Retry}]
                 /\ clock'="waiting"
                 /\ UNCHANGED <<log,cursor,inbox,prepared,accepted,unknown,sends,expired,restarts>>
\* One arbitrary process restart exercises durable intent/receipt retention.
\* An infinite stream of crashes is not assumed to provide fair service.
Restart == /\ restarts=0 /\ restarts'=1
           /\ cursor'=0 /\ clock'="waiting"
           /\ UNCHANGED <<log,inbox,told,prepared,accepted,unknown,sends,expired>>
Next == \/ (\E f \in Friends,c \in Cards : Deal(f,c))
        \/ Wake \/ Sweep \/ Prepare \/ Pass \/ Tell \/ TellLost \/ CloseWindow \/ Expire \/ AckBeforeSend \/ Restart
Spec == /\ Init /\ [][Next]_vars
        /\ WF_vars(Wake) /\ WF_vars(Sweep) /\ WF_vars(Prepare)
        /\ WF_vars(Pass) /\ WF_vars(Tell) /\ WF_vars(CloseWindow) /\ WF_vars(Expire)
DeliveredWasDealt == Delivered \subseteq log
IntentBeforeBrief == Delivered \subseteq prepared
ToldWasDelivered == Receipted \subseteq Delivered
ReceiptAfterSend == Receipted \subseteq accepted
NoDuplicateTell == \A p \in Pairs : sends[p] <= 1
PushedWithinOneEvent == clock # "swept"
EveryDealPushedOrJudged == \A p \in Pairs : (p \in log) ~> (p \in Receipted \cup unknown)
=============================================================================
