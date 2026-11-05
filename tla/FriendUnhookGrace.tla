-------------------------- MODULE FriendUnhookGrace --------------------------
EXTENDS Naturals, FiniteSets
\* The unhook grace (2026-10-23; internal/sprint/unhook_grace.go FriendUnhookInGrace,
\* internal/sprint/live.go LiveReturns, internal/sprint/store/friends.go deriveFriendStatus).
\* One friend's hook record, as the tick reads it: up (proven), armed (hooked again and not
\* yet proven: down, and no unhook), or unhooked (nova-hook's heartbeat unhooked her after a
\* delivered challenge went unanswered for its down-after). Her cards: ready (unstarted) or
\* working (her children may be halfway through them), returned by the tick's reconcile,
\* moved off her row while she is not up (a ready card goes at once, as a down friend's
\* always has), or done. Two clocks, in tick windows, capped where nothing reads further:
\* since, the windows since the unhook (hook.Status.Unhooked), and quiet, the windows since
\* her session last answered a challenge (hook.Status.AnswerAt). The ghost alive is whether
\* a model is really behind her session: a dead one never answers, and her service may
\* re-arm her (armed) and be unhooked again, over and over, which restarts the unhook clock.
\*
\* InGrace is the code's predicate: unhooked, since < Grace and quiet < Grace + Proof (the
\* friend-idle setting, and the grace plus hook.ProofTTL). The tick is one step: the windows
\* pass and the reconcile returns every working card of a friend unhooked and not InGrace,
\* as LiveReturns does inside the same tick that reads the record.
\*
\* Found 2026-10-23: a busy friend (Johnny, Aurora) whose Monitor lapsed at its 30-minute
\* limit was unhooked after 5m, and the tick returned every working card and read on her row
\* at once ("eight reads left the row"); she re-armed a minute later to an empty row.
\*
\* Invariants: NoReturnInGrace (no working card is returned while she is inside the grace;
\* a friend who hooks in again inside it keeps them) and NoStrandedPastGrace (no working card
\* stays on a friend unhooked past the grace, by either clock, after a tick: the safety the
\* old return kept, now with a clock). Reversed witnesses (Broken):
\*   "nograce"         the v1.6.12 return: every unhook returns at once (NoReturnInGrace);
\*   "unhookclockonly" the grace by the unhook clock alone: a friend whose session re-arms
\*                     and is unhooked again and again without answering keeps her working
\*                     cards for ever (NoStrandedPastGrace).
\* Reach cases: MCFriendUnhookGraceReachReturn (NeverReturned fails: past the grace a card
\* is returned) and MCFriendUnhookGraceReachKept (NeverKeptAcrossUnhook fails: a friend
\* unhooked with a working card hooks in again and still has it).
\*
\* The down return (2026-11-05; internal/sprint/friend_down_return.go friendDownReturn): a card
\* she took and never started ("taken": a take recorded, no start, her lane gone and the card
\* back ready on her row) is hers only while she is up or inside DownGrace windows of her last
\* answer (quiet, her AnsweredAt); the tick past it returns every such card to the pool. Found
\* that morning: emma went down holding 16 cards taken and not started for 22 minutes, and
\* MoveReady (a card never taken) did not move them. The grace runs on both clocks, as the
\* code's (the later of her last answer and the card's take): quiet, and tq, the windows since
\* the card's take. Invariants
\* NoTakenReturnInDownGrace and NoTakenStrandedPastDownGrace; reversed witnesses:
\*   "keeptaken"   the code before: a down friend keeps them for ever (NoTakenStrandedPastDownGrace);
\*   "nodowngrace" returned the tick she is down (NoTakenReturnInDownGrace).
\* Not modelled: the beat's live set (LiveRuns: inside the grace her row is reconciled by
\* her own fresh beat, as any row), the deal, the hook heartbeat's own state machine
\* (nova-tools tla/HookBeat.tla), and an armed record that is never unhooked again.
CONSTANTS Cards, Grace, Proof, DownGrace, Broken
ASSUME DownGrace \in 1..(Grace + Proof)
VARIABLES hookSt, since, quiet, alive, card, returnedInGrace, kept, ticked, takenEarly, tq
vars == <<hookSt, since, quiet, alive, card, returnedInGrace, kept, ticked, takenEarly, tq>>

QuietCap == Grace + Proof
CardStates == {"ready", "taken", "working", "returned", "moved", "done"}

TypeOK == /\ hookSt \in {"up", "armed", "unhooked"}
          /\ since \in 0..Grace
          /\ quiet \in 0..QuietCap
          /\ alive \in BOOLEAN
          /\ card \in [Cards -> CardStates]
          /\ returnedInGrace \in BOOLEAN
          /\ kept \in BOOLEAN
          /\ ticked \in BOOLEAN
          /\ takenEarly \in BOOLEAN
          /\ tq \in [Cards -> 0..DownGrace]

Init == /\ hookSt = "up" /\ since = 0 /\ quiet = 0 /\ alive = TRUE
        /\ card \in [Cards -> {"ready", "taken", "working"}]
        /\ returnedInGrace = FALSE /\ kept = FALSE /\ ticked = FALSE /\ takenEarly = FALSE
        /\ tq \in [Cards -> 0..DownGrace]

\* the code's predicate (FriendUnhookInGrace)
InGrace == hookSt = "unhooked" /\ since < Grace /\ quiet < QuietCap

\* the predicate the reconcile runs on, broken or not
CodeInGrace == IF "nograce" \in Broken THEN FALSE
               ELSE IF "unhookclockonly" \in Broken THEN hookSt = "unhooked" /\ since < Grace
               ELSE InGrace

Working == {c \in Cards : card[c] = "working"}

\* the tick: a window passes on both clocks, then the reconcile reads the record and returns
\* every working card of a friend unhooked and not in grace (LiveReturns)
Tick == LET s2 == IF hookSt = "unhooked" THEN IF since < Grace THEN since + 1 ELSE Grace ELSE since
            q2 == IF quiet < QuietCap THEN quiet + 1 ELSE QuietCap
            inGrace2 == hookSt = "unhooked" /\ s2 < Grace /\ q2 < QuietCap
            code2 == IF "nograce" \in Broken THEN FALSE
                     ELSE IF "unhookclockonly" \in Broken THEN hookSt = "unhooked" /\ s2 < Grace
                     ELSE inGrace2
            ret == hookSt = "unhooked" /\ ~code2
            \* the down return of the cards she took and never started (friendDownReturn)
            t2 == [c \in Cards |-> IF tq[c] < DownGrace THEN tq[c] + 1 ELSE DownGrace]
            downRet(c) == hookSt # "up" /\ card[c] = "taken" /\ "keeptaken" \notin Broken
                          /\ ((q2 >= DownGrace /\ t2[c] >= DownGrace) \/ "nodowngrace" \in Broken)
            early == {c \in Cards : downRet(c) /\ ~(q2 >= DownGrace /\ t2[c] >= DownGrace)}
        IN /\ since' = s2 /\ quiet' = q2
           /\ card' = [c \in Cards |-> IF ret /\ card[c] = "working" THEN "returned"
                                       ELSE IF downRet(c) THEN "returned"
                                       ELSE card[c]]
           /\ tq' = t2
           /\ takenEarly' = (takenEarly \/ early # {})
           /\ returnedInGrace' = (returnedInGrace \/ (ret /\ inGrace2 /\ Working # {}))
           /\ ticked' = TRUE
           /\ UNCHANGED <<hookSt, alive, kept>>

\* nova-hook unhooks her: a delivered challenge unanswered for down-after, busy or dead;
\* an armed record never proven is unhooked again (the heartbeat's retry): the clock restarts.
\* The reconcile runs at the tick, which reads the record next.
Unhook == /\ hookSt \in {"up", "armed"}
          /\ hookSt' = "unhooked" /\ since' = 0
          /\ ticked' = FALSE
          /\ UNCHANGED <<quiet, alive, card, returnedInGrace, kept, takenEarly, tq>>

\* her service hooks her in again, a model behind it or not: down until she proves
Rearm == /\ hookSt = "unhooked"
         /\ hookSt' = "armed"
         /\ ticked' = FALSE
         /\ UNCHANGED <<since, quiet, alive, card, returnedInGrace, kept, takenEarly, tq>>

\* her session answers a challenge: up, and her last answer is now
Prove == /\ alive /\ hookSt \in {"up", "armed"}
         /\ hookSt' = "up" /\ quiet' = 0
         /\ kept' = (kept \/ (hookSt = "armed" /\ Working # {}))
         /\ ticked' = FALSE
         /\ UNCHANGED <<since, alive, card, returnedInGrace, takenEarly, tq>>

Die == /\ alive /\ alive' = FALSE
       /\ ticked' = FALSE
       /\ UNCHANGED <<hookSt, since, quiet, card, returnedInGrace, kept, takenEarly, tq>>

\* she starts a ready card or finishes a working one, only while up
Start(c) == /\ hookSt = "up" /\ card[c] \in {"ready", "taken"}
            /\ card' = [card EXCEPT ![c] = "working"]
            /\ ticked' = FALSE
            /\ UNCHANGED <<hookSt, since, quiet, alive, returnedInGrace, kept, takenEarly, tq>>
\* she takes a ready card and never starts it: her lane is gone, and the card is back ready on
\* her row with its take recorded (lane_hold.go laneGoneUnits keeps first_taken)
Take(c) == /\ hookSt = "up" /\ card[c] = "ready"
           /\ card' = [card EXCEPT ![c] = "taken"]
           /\ tq' = [tq EXCEPT ![c] = 0]
           /\ ticked' = FALSE
           /\ UNCHANGED <<hookSt, since, quiet, alive, returnedInGrace, kept, takenEarly>>
Finish(c) == /\ hookSt = "up" /\ alive /\ card[c] = "working"
             /\ card' = [card EXCEPT ![c] = "done"]
             /\ ticked' = FALSE
             /\ UNCHANGED <<hookSt, since, quiet, alive, returnedInGrace, kept, takenEarly, tq>>

\* a ready card of a friend not up moves at once (the deal places it elsewhere)
MoveReady(c) == /\ hookSt # "up" /\ card[c] = "ready"
                /\ card' = [card EXCEPT ![c] = "moved"]
                /\ ticked' = FALSE
                /\ UNCHANGED <<hookSt, since, quiet, alive, returnedInGrace, kept, takenEarly, tq>>

Next == \/ Tick \/ Unhook \/ Rearm \/ Prove \/ Die
        \/ \E c \in Cards : Start(c) \/ Take(c) \/ Finish(c) \/ MoveReady(c)

Spec == Init /\ [][Next]_vars

\* no working card is returned while its friend is inside the grace
NoReturnInGrace == ~returnedInGrace

\* after the tick's reconcile (ticked: the state a tick left; a step between two ticks, an
\* unhook the record took before the tick reads it, is the tick's to answer), no working card stays on a friend unhooked past the
\* grace by either clock: the tick that takes her past it returns them
NoStrandedPastGrace == (ticked /\ hookSt = "unhooked" /\ ~InGrace) => Working = {}

\* no card she took and never started is returned while she is inside the down grace
NoTakenReturnInDownGrace == ~takenEarly

\* after the tick, no card she took and never started stays on a friend down past the grace
NoTakenStrandedPastDownGrace == (ticked /\ hookSt # "up" /\ quiet >= DownGrace) =>
                                    \A c \in Cards : card[c] = "taken" => tq[c] < DownGrace

\* reach witnesses
NeverReturned == \A c \in Cards : card[c] # "returned"
NeverKeptAcrossUnhook == ~kept
=============================================================================
