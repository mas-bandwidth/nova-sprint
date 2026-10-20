------------------------ MODULE WaitForResources ------------------------
(***************************************************************************)
(* Every resource wait in the sprint is bounded (docs/SPEC-SPRINT.md       *)
(* section 9, "Every resource wait is bounded"; tla/WAITFOR-RESOURCES.tsv).*)
(*                                                                         *)
(* The card-side wait-for graph (needs, LAND-AFTER, judgments, holds, the   *)
(* landing) is WaitFor.tla's; this module is the resource side it rests on. *)
(* Each wait is a wait-for edge on a member, a reader's room, a friend, a   *)
(* route or a provider, a bench lease, the lander or a hook. Every one ends *)
(* by progress under fairness or at a server-computed deadline that raises  *)
(* a judgment, and no two resource holders wait on each other.             *)
(*                                                                         *)
(* The code each action stands for:                                        *)
(*   DealWork, TakeWork, FinishWork, WorkDeadline                           *)
(*        internal/sprint/steps_tick.go:WorkDeadline (a work card's         *)
(*        deadline by state; DealtMax from settings.go, 2h unfinished from  *)
(*        steps_tick.go:DeadlineUnfinished) and steps_work.go's deal/take/  *)
(*        finish; the drain return is internal/sprint/drain.go drainReturns *)
(*        via TickPresence (presence.go:presenceMoves);                    *)
(*   StartPrimary, AskRoom, DeadlinePrimary, ReadBegin, ReadFinish,        *)
(*   ReadReturn, ReadDeadline                                              *)
(*        internal/sprint/readers.go:askFinders/askPicks/sweepReads and     *)
(*        read_cards.go:readCardsTakeBack (ReadCardDeadline 60m, a ready    *)
(*        read's DealtMax), readers.go:ReadLeaseExpires/RenewReaderLeases; *)
(*   FriendDeal, FriendTake, FriendDeadline                                *)
(*        internal/sprint/friend_deal.go:friendDealPass and                 *)
(*        friend_deadline.go:unfinishedLimit; a hook ask to a friend runs   *)
(*        in nova-tools (outside this repository) and is modelled here as   *)
(*        an outside action whose server deadline is an assumption;        *)
(*   RouteStart, RouteServes, RouteRest, RouteBackoff, RouteUnfund,         *)
(*   RouteEnds, RouteDeadline                                              *)
(*        internal/sprint/route.go:drawRoute/routeServed and                *)
(*        route_rest.go:RestProvider/ExpireProvider, provider_funds.go:     *)
(*        providerRestsDue/AllOutOfCredit (tla/RouteRest.tla,              *)
(*        tla/RateBackoff.tla, tla/ProviderFunds.tla);                     *)
(*   BenchStart, BenchGrant, BenchGive, BenchExpire, BenchDeadline          *)
(*        internal/sprint/lane.go:Lanes.Pick/Lanes.Give/Lane.expire        *)
(*        (LaneHoldFor 20m; tla/BenchLease.tla);                            *)
(*   MergeStart, GateStart, LandStart, LandFinish, MergeDeadline,           *)
(*   GateDeadline                                                          *)
(*        cmd/nova-sprint/landloop.go:landOnce/landCycle/noGateBenchReason  *)
(*        and landgo.go's gate (a land gate bench may be held:              *)
(*        v17-land-gate-benches-include-held-members); tla/OneLander.tla,   *)
(*        tla/LandPass.tla;                                                 *)
(*   PushStart, PushAck, PushDeadline                                      *)
(*        tla/CoordinatorPush.tla (the coordinator push over the hook       *)
(*        record) and steps_tick.go:TickOverdue;                           *)
(*   HookAsk, HookAnswer, HookDeadline                                      *)
(*        the friend's hook ask in nova-tools (outside this repository), a *)
(*        server deadline assumption named below;                          *)
(*   Hold, Release, Drain, Down, Restart, ReaderOff, AllReadersOff,        *)
(*   ReaderUp, FriendUp, FriendDown, Tick                                  *)
(*        the outside events: internal/sprint/presence.go:TickPresence,    *)
(*        presenceMoves/DrainingFrom, fleet_quiet.go, readers.go's reader  *)
(*        rows, and the store's clock.                                     *)
(*                                                                         *)
(* The assumption. A hook ask to a friend runs in nova-tools, outside this *)
(* repository. It is an outside action here, and its server deadline is an *)
(* assumption of the model: Broken = "hooknodeadline" drops it and fails   *)
(* EveryResourceWaitHasADeadline.                                          *)
(*                                                                         *)
(* The design is Broken = "none". Each other value is a reversed witness:  *)
(*   "heldgate"     a land gate needs an unheld bench and every fleet       *)
(*                  member is held: ResourceWaitEnds fails (the live       *)
(*                  sprint's fleet hold that stopped every landing; mended *)
(*                  by v17-land-gate-benches-include-held-members);        *)
(*   "drainall"     every reader drains at once and the reads dealt to     *)
(*                  them are neither returned nor retired:                 *)
(*                  ResourceWaitEnds fails (the code returns them,         *)
(*                  internal/sprint/readers.go:sweepReads);                *)
(*   "noreaddeadline" a read card waiting on an absent reader has no       *)
(*                  deadline: EveryResourceWaitHasADeadline fails;         *)
(*   "leasenoexpiry" a bench lease never expires: ResourceWaitEnds fails   *)
(*                  (tla/BenchLease.tla's "noexpiry");                     *)
(*   "hooknodeadline" a hook ask has no server deadline:                   *)
(*                  EveryResourceWaitHasADeadline fails.                   *)
(*                                                                         *)
(* Time is the store's clock in whole steps 0..MaxNow and a deadline is    *)
(* min(now + its bound, MaxNow), so every bound is reached by MaxNow. The   *)
(* waits are placed in Init (the Start* definitions name the Go function    *)
(* that creates each one) and each ends once; the Live instance holds the   *)
(* outside state Init placed and checks that the waits themselves end,      *)
(* while the safety instance also moves the outside events.                *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets, TLC

CONSTANTS Members, Readers, Routes, MaxNow, Broken

Faults == {"none", "heldgate", "drainall", "noreaddeadline", "leasenoexpiry", "hooknodeadline"}
ASSUME Broken \in Faults
ASSUME MaxNow > 0
ASSUME Members # {}
ASSUME Readers # {}
ASSUME Routes # {}
ASSUME "none" \notin Members
ASSUME "none" \notin Readers
ASSUME "none" \notin Routes

\* the bounds of the code, folded into whole steps of the model's clock
HoldFor == 0    \* lane.go:LaneHoldFor
RestFor == 1    \* route_rest.go:RouteRestFor / RateBackoff
MaxRoom == 1    \* readers.go:readerRoom.share
DealtFor == 2   \* settings.go:DealtMaxDefault
WorkFor  == 2   \* steps_tick.go:DeadlineUnfinished
ReadFor  == 2   \* read_cards.go:ReadCardDeadline
ReviewFor == 2  \* steps_tick.go:TickOverdue (NReviewNoReader)
FriendFor == 2 \* friend_deadline.go:unfinishedLimit
RouteFor == 2  \* route_rest.go:RouteRestFor
BenchFor == 2   \* lane.go:LaneHoldFor (a waiter's judgment)
MergeFor == 2   \* steps_tick.go:DeadlineMergeIdle
PushFor  == 2   \* steps_tick.go:DeadlineJudgment
AskFor   == 2   \* the hook ask's server deadline (an assumption)

VARIABLES now, mstate, readerState, room,
          routeState,
          owner,
          w, wMember,
          p,
          rc, rcReader, rcDeadline,
          f, friend,
          rw, rwRoute,
          bw, bwDeadline,
          mg, mgDeadline, lander,
          hook, hookDeadline

vars == <<now, mstate, readerState, room, routeState,
          owner, w, wMember, p,
          rc, rcReader, rcDeadline, f, friend,
          rw, rwRoute, bw, bwDeadline,
          mg, mgDeadline, lander, hook, hookDeadline>>

NoDeadline == MaxNow + 1

Add(d) == IF now + d > MaxNow THEN MaxNow ELSE now + d

HasDeadline(x) == x # NoDeadline

DeadlineOK(x) == x \in 0..NoDeadline

WaitW    == w \in {"ready", "working"}
WaitP    == p = "waiting"
WaitRc   == rc \in {"ready", "working"}
WaitF    == f = "waiting"
WaitRw   == rw = "waiting"
WaitBw   == bw = "waiting"
WaitMg   == mg \in {"merging", "landing"}
WaitGate == mg = "gating"
WaitPush == hook = "push"
WaitAsk  == hook = "ask"

\* a bench a land gate may use: every fleet member, held or not (the code
\* after v17-land-gate-benches-include-held-members); the heldgate witness
\* wrongly requires one that is not held.
BenchOK == IF Broken = "heldgate"
           THEN \E m \in Members : mstate[m] = "up"
           ELSE \E m \in Members : mstate[m] \in {"up", "held"}

CanW == w = "working"
        \/ (w = "ready" /\ \E m \in Members : mstate[m] = "up" /\ wMember = m)
        \/ (w # "none" /\ \E m \in Members : mstate[m] = "draining" /\ wMember = m)
CanP == p = "waiting" /\ \E r \in Readers : readerState[r] = "up" /\ room[r] > 0
CanRc == (rc = "ready" /\ readerState[rcReader] = "up")
         \/ (rc = "working" /\ readerState[rcReader] = "up")
         \/ (rc = "ready" /\ readerState[rcReader] = "off" /\ Broken # "drainall")
CanF == f = "waiting" /\ friend = "up"
CanRw == rw = "waiting" /\ routeState[rwRoute] = "ok"
CanBw == bw = "waiting" /\ owner = "none"
CanMg == (mg = "merging")
         \/ (mg = "landing" /\ lander = "busy")
CanGate == mg = "gating" /\ lander = "free" /\ BenchOK
CanPush == hook = "push"
CanAsk == hook = "ask" /\ friend = "up"

TypeOK ==
  /\ now \in 0..MaxNow
  /\ mstate \in [Members -> {"up", "held", "draining", "down"}]
  /\ readerState \in [Readers -> {"up", "off"}]
  /\ room \in [Readers -> 0..MaxRoom]
  /\ routeState \in [Routes -> {"ok", "cooling"}]
  /\ owner \in Members \cup {"none"}
  /\ w \in {"none", "ready", "working"}
  /\ wMember \in Members \cup {"none"}
  /\ p \in {"none", "waiting"}
  /\ rc \in {"none", "ready", "working"}
  /\ rcReader \in Readers \cup {"none"}
  /\ DeadlineOK(rcDeadline)
  /\ f \in {"none", "waiting"}
  /\ friend \in {"up", "down"}
  /\ rw \in {"none", "waiting"}
  /\ rwRoute \in Routes \cup {"none"}
  /\ bw \in {"none", "waiting"}
  /\ DeadlineOK(bwDeadline)
  /\ mg \in {"none", "merging", "gating", "landing"}
  /\ DeadlineOK(mgDeadline)
  /\ lander \in {"free", "busy"}
  /\ hook \in {"none", "push", "ask"}
  /\ DeadlineOK(hookDeadline)

Init ==
  /\ now = 0
  /\ mstate = [m \in Members |->
        IF Broken = "heldgate" THEN "held" ELSE "up"]
  /\ readerState = [r \in Readers |->
        IF Broken \in {"drainall", "noreaddeadline"} THEN "off" ELSE "up"]
  /\ room = [r \in Readers |-> IF Broken = "drainall" THEN 0 ELSE MaxRoom]
  /\ routeState = [t \in Routes |-> "ok"]
  /\ owner = IF Broken = "leasenoexpiry" THEN CHOOSE m \in Members : TRUE ELSE "none"
  /\ w = "ready" /\ wMember = (CHOOSE m \in Members : TRUE)
  /\ p = "waiting"
  /\ rc = "working" /\ rcReader = (CHOOSE r \in Readers : TRUE)
  /\ rcDeadline = IF Broken = "noreaddeadline" THEN NoDeadline ELSE Add(ReadFor)
  /\ f = "waiting" /\ friend = "down"
  /\ rw = "waiting" /\ rwRoute = (CHOOSE t \in Routes : TRUE)
  /\ bw = "waiting"
  /\ bwDeadline = IF Broken = "leasenoexpiry" THEN NoDeadline ELSE Add(BenchFor)
  /\ mg = IF Broken = "heldgate" THEN "gating" ELSE "merging"
  /\ mgDeadline = IF Broken = "heldgate" THEN NoDeadline ELSE Add(MergeFor)
  /\ lander = "free"
  /\ hook = IF Broken = "hooknodeadline" THEN "ask" ELSE "push"
  /\ hookDeadline = IF Broken = "hooknodeadline" THEN NoDeadline ELSE Add(PushFor)


\* steps_work.go: the deal cuts a work card to a member
DealWork ==
  /\ w = "none"
  /\ \E m \in Members : mstate[m] = "up"
  /\ w' = "ready"
  /\ wMember' = CHOOSE m \in Members : mstate[m] = "up"
  
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 p, rc, rcReader, rcDeadline, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* steps_work.go: a member takes the card dealt to it
TakeWork ==
  /\ w = "ready"
  /\ wMember # "none" /\ mstate[wMember] = "up"
  /\ w' = "working"
  
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 wMember, p, rc, rcReader, rcDeadline, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* steps_work.go: a working card finishes and leaves its member
FinishWork ==
  /\ w = "working"
  /\ mstate[wMember] = "up"
  /\ w' = "none" /\ wMember' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 p, rc, rcReader, rcDeadline, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* internal/sprint/drain.go:drainReturns, the tick's level returns a dealt card
\* of a draining member
DrainReturn ==
  /\ w # "none" /\ wMember # "none" /\ mstate[wMember] = "draining"
  /\ w' = "none" /\ wMember' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 p, rc, rcReader, rcDeadline, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* steps_tick.go:WorkDeadline / TickOverdue raises the late judgment
WorkDeadline ==
  /\ WaitW
  /\ now >= 1
  /\ w' = "none" /\ wMember' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 p, rc, rcReader, rcDeadline, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* readers.go: a primary wants reads and waits for a reader's room
StartPrimary ==
  /\ p = "none"
  /\ p' = "waiting"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, rc, rcReader, rcDeadline, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* readers.go:askFinders/askPicks: a read card is dealt to a reader with room
AskRoom ==
  /\ p = "waiting"
  /\ \E r \in Readers : readerState[r] = "up" /\ room[r] > 0
  /\ LET r == CHOOSE r \in Readers : readerState[r] = "up" /\ room[r] > 0 IN
       /\ p' = "none"
       /\ rc' = "ready" /\ rcReader' = r
       /\ rcDeadline' = IF Broken = "noreaddeadline" THEN NoDeadline ELSE Add(ReadFor)
       /\ room' = [room EXCEPT ![r] = room[r] - 1]
  /\ UNCHANGED <<now, mstate, readerState, routeState, owner,
                 w, wMember, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* steps_tick.go:TickOverdue raises NReviewNoReader at the primary's deadline
PrimaryDeadline ==
  /\ WaitP
  /\ now >= 1
  /\ p' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, rc, rcReader, rcDeadline, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* readers.go: the reader begins the read
ReadBegin ==
  /\ rc = "ready" /\ readerState[rcReader] = "up"
  /\ rc' = "working"
  /\ rcDeadline' = IF Broken = "noreaddeadline" THEN NoDeadline ELSE Add(ReadFor)
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rcReader, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* readers.go: the read finishes and gives its room back
ReadFinish ==
  /\ rc = "working" /\ readerState[rcReader] = "up"
  /\ room[rcReader] < MaxRoom
  /\ rc' = "none" /\ rcReader' = "none" /\ rcDeadline' = NoDeadline
  /\ room' = [room EXCEPT ![rcReader] = room[rcReader] + 1]
  /\ UNCHANGED <<now, mstate, readerState, routeState, owner,
                 w, wMember, p, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* readers.go:sweepReads: a read asked of an absent reader comes back
ReadReturn ==
  /\ rc = "ready" /\ readerState[rcReader] = "off"
  /\ Broken # "drainall"
  /\ room[rcReader] < MaxRoom
  /\ rc' = "none" /\ rcReader' = "none" /\ rcDeadline' = NoDeadline
  /\ room' = [room EXCEPT ![rcReader] = room[rcReader] + 1]
  /\ UNCHANGED <<now, mstate, readerState, routeState, owner,
                 w, wMember, p, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* read_cards.go:readCardsTakeBack: a read past its deadline is retired
ReadDeadline ==
  /\ WaitRc
  /\ HasDeadline(rcDeadline) /\ now >= rcDeadline
  /\ Broken # "drainall"
  /\ rc' = "none" /\ rcReader' = "none" /\ rcDeadline' = NoDeadline
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, f, friend,
                 rw, rwRoute, bw, bwDeadline, mg, mgDeadline, lander,
                 hook, hookDeadline>>

\* friend_deal.go:friendDealPass: a card dealt to a friend (a hook ask)
FriendDeal ==
  /\ f = "none"
  /\ f' = "waiting"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 friend, rw, rwRoute, bw, bwDeadline, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* friend_deal.go: the friend takes the card
FriendTake ==
  /\ f = "waiting" /\ friend = "up"
  /\ f' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 friend, rw, rwRoute, bw, bwDeadline, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* friend_deadline.go:unfinishedLimit: the friend's card is late
FriendDeadline ==
  /\ WaitF
  /\ now >= 1
  /\ f' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 friend, rw, rwRoute, bw, bwDeadline, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* route.go:drawRoute: a card wants a route
RouteStart ==
  /\ rw = "none"
  /\ rw' = "waiting"
  /\ rwRoute' = CHOOSE t \in Routes : TRUE
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, bw, bwDeadline, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* route.go:routeServed: the route serves the card
RouteServes ==
  /\ rw = "waiting" /\ routeState[rwRoute] = "ok"
  /\ rw' = "none" /\ rwRoute' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, bw, bwDeadline, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* route_rest.go:RestProvider: a route rests for a provider failure
RouteRest ==
  /\ \E t \in Routes : routeState[t] = "ok"
  /\ routeState' = [t \in Routes |-> "cooling"]
  /\ UNCHANGED <<now, mstate, readerState, room, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* route_rest.go: a rate backoff rests the route
RouteBackoff ==
  /\ \E t \in Routes : routeState[t] = "ok"
  /\ routeState' = [t \in Routes |-> "cooling"]
  /\ UNCHANGED <<now, mstate, readerState, room, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* provider_funds.go:AllOutOfCredit: a provider is unfunded
RouteUnfund ==
  /\ \E t \in Routes : routeState[t] = "ok"
  /\ routeState' = [t \in Routes |-> "cooling"]
  /\ UNCHANGED <<now, mstate, readerState, room, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* route_rest.go:ExpireProvider: a timed rest ends
RouteEnds ==
  /\ \E t \in Routes : routeState[t] = "cooling"
  /\ routeState' = [t \in Routes |-> "ok"]
  /\ UNCHANGED <<now, mstate, readerState, room, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* steps_tick.go:TickOverdue raises NNoRoute at the card's deadline
RouteDeadline ==
  /\ WaitRw
  /\ now >= 1
  /\ rw' = "none" /\ rwRoute' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, bw, bwDeadline, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* lane.go:Lanes.Pick: a member asks for a bench and waits
BenchStart ==
  /\ bw = "none"
  /\ bw' = "waiting"
  /\ bwDeadline' = IF Broken = "leasenoexpiry" THEN NoDeadline ELSE Add(BenchFor)
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* lane.go:Lanes.Pick: the server grants a free bench
BenchGrant ==
  /\ bw = "waiting" /\ owner = "none"
  /\ bw' = "none" /\ bwDeadline' = NoDeadline
  /\ owner' = CHOOSE m \in Members : TRUE
  /\ UNCHANGED <<now, mstate, readerState, room, routeState,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* lane.go:Lanes.Give: a holder gives its bench back
BenchGive ==
  /\ Broken # "leasenoexpiry"
  /\ owner # "none"
  /\ owner' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* lane.go:Lane.expire: a lease not seen for LaneHoldFor is released
BenchExpire ==
  /\ Broken # "leasenoexpiry"
  /\ owner # "none"
  /\ owner' = "none"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* steps_tick.go:TickOverdue raises NOverloaded at the bench waiter's deadline
BenchDeadline ==
  /\ WaitBw
  /\ HasDeadline(bwDeadline) /\ now >= bwDeadline
  /\ bw' = "none" /\ bwDeadline' = NoDeadline
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, mg, mgDeadline,
                 lander, hook, hookDeadline>>

\* steps_merge.go:mergeStep: a merging card asks the lander
MergeStart ==
  /\ mg = "none"
  /\ mg' = "merging"
  /\ mgDeadline' = IF Broken = "heldgate" THEN NoDeadline ELSE Add(MergeFor)
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 lander, hook, hookDeadline>>

\* landloop.go:landOnce: the land gate waits for a bench
GateStart ==
  /\ mg = "merging"
  /\ mg' = "gating"
  /\ mgDeadline' = IF Broken = "heldgate" THEN NoDeadline ELSE Add(MergeFor)
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 lander, hook, hookDeadline>>

\* landloop.go:landOnce: the gate takes a bench (held or not) and the lander runs
LandStart ==
  /\ mg = "gating" /\ lander = "free" /\ BenchOK
  /\ mg' = "landing" /\ mgDeadline' = IF Broken = "heldgate" THEN NoDeadline ELSE Add(MergeFor)
  /\ lander' = "busy"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 hook, hookDeadline>>

\* landloop.go:landOnce: the lander finishes the batch and frees its slot
LandFinish ==
  /\ mg = "landing" /\ lander = "busy"
  /\ mg' = "none" /\ mgDeadline' = NoDeadline
  /\ lander' = "free"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 hook, hookDeadline>>

\* steps_tick.go:DeadlineMergeIdle raises the merge's late judgment
MergeDeadline ==
  /\ WaitMg
  /\ HasDeadline(mgDeadline) /\ now >= mgDeadline
  /\ mg' = "none" /\ mgDeadline' = NoDeadline
  /\ lander' = "free"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 hook, hookDeadline>>

\* steps_tick.go:DeadlineJudgment raises NLandingStall for a gate that never opens
GateDeadline ==
  /\ WaitGate
  /\ HasDeadline(mgDeadline) /\ now >= mgDeadline
  /\ mg' = "none" /\ mgDeadline' = NoDeadline
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 lander, hook, hookDeadline>>

\* tla/CoordinatorPush.tla: the coordinator's push over the hook record
PushStart ==
  /\ hook = "none"
  /\ hook' = "push" /\ hookDeadline' = Add(PushFor)
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander>>

\* tla/CoordinatorPush.tla: the seat acks the push
PushAck ==
  /\ hook = "push"
  /\ hook' = "none" /\ hookDeadline' = NoDeadline
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander>>

\* steps_tick.go:TickOverdue: the server's deadline raises NStatus
PushDeadline ==
  /\ WaitPush
  /\ HasDeadline(hookDeadline) /\ now >= hookDeadline
  /\ hook' = "none" /\ hookDeadline' = NoDeadline
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander>>

\* the hook ask (nova-tools, outside this repository): the server deadline is
\* an assumption of this model
HookAsk ==
  /\ hook = "none"
  /\ hook' = "ask"
  /\ hookDeadline' = IF Broken = "hooknodeadline" THEN NoDeadline ELSE Add(AskFor)
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander>>

\* the friend's hook answers
HookAnswer ==
  /\ hook = "ask" /\ friend = "up"
  /\ hook' = "none" /\ hookDeadline' = NoDeadline
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander>>

\* steps_tick.go:TickOverdue raises NStreamStale when the hook's deadline passes
HookDeadline ==
  /\ WaitAsk
  /\ HasDeadline(hookDeadline) /\ now >= hookDeadline
  /\ hook' = "none" /\ hookDeadline' = NoDeadline
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander>>


\* readers.go: a reader's row goes away (its machine is held or drains)
ReaderOff ==
  /\ \E r \in Readers : readerState[r] = "up"
  /\ readerState' = [r \in Readers |-> "off"]
  /\ UNCHANGED <<now, mstate, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* the drainall witness: every reader drains at once
AllReadersOff ==
  /\ Broken = "drainall"
  /\ \E r \in Readers : readerState[r] = "up"
  /\ readerState' = [r \in Readers |-> "off"]
  /\ UNCHANGED <<now, mstate, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* readers.go: a reader's row comes back
ReaderUp ==
  /\ Broken # "drainall"
  /\ \E r \in Readers : readerState[r] = "off"
  /\ readerState' = [r \in Readers |-> "up"]
  /\ UNCHANGED <<now, mstate, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* the friend's presence: the outside friend comes up or goes down
FriendUp ==
  /\ friend = "down"
  /\ friend' = "up"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

FriendDown ==
  /\ friend = "up"
  /\ friend' = "down"
  /\ UNCHANGED <<now, mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* the store's clock, with the age abstraction at MaxNow (tla/BenchLease.tla Tick)
Tick ==
  /\ (IF now < MaxNow THEN now' = now + 1 ELSE now' = now)
  /\ UNCHANGED <<mstate, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

\* the outside events are parameterized so fairness can name each member
Hold(m) ==
  /\ mstate[m] = "up"
  /\ mstate' = [x \in Members |-> "held"]
  /\ UNCHANGED <<now, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>
Release(m) ==
  /\ mstate[m] = "held"
  /\ mstate' = [x \in Members |-> "up"]
  /\ UNCHANGED <<now, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>
Drain(m) ==
  /\ mstate[m] = "up"
  /\ mstate' = [x \in Members |-> "draining"]
  /\ UNCHANGED <<now, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>
Down(m) ==
  /\ mstate[m] = "up"
  /\ mstate' = [x \in Members |-> "down"]
  /\ UNCHANGED <<now, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>
Restart(m) ==
  /\ mstate[m] \in {"draining", "down"}
  /\ mstate' = [x \in Members |-> "up"]
  /\ UNCHANGED <<now, readerState, room, routeState, owner,
                 w, wMember, p, rc, rcReader, rcDeadline,
                 f, friend, rw, rwRoute, bw, bwDeadline,
                 mg, mgDeadline, lander, hook, hookDeadline>>

Next ==
  \/ TakeWork \/ FinishWork \/ DrainReturn \/ WorkDeadline
  \/ AskRoom \/ PrimaryDeadline
  \/ ReadBegin \/ ReadFinish \/ ReadReturn \/ ReadDeadline
  \/ FriendDeal \/ FriendTake \/ FriendDeadline
  \/ RouteServes \/ RouteRest \/ RouteBackoff \/ RouteUnfund
  \/ RouteEnds \/ RouteDeadline
  \/ BenchGrant \/ BenchGive \/ BenchExpire \/ BenchDeadline
  \/ GateStart \/ LandStart \/ LandFinish \/ MergeDeadline
  \/ GateDeadline
  \/ PushAck \/ PushDeadline
  \/ HookAnswer \/ HookDeadline
  \/ (\E m \in Members : Hold(m)) \/ (\E m \in Members : Release(m))
  \/ (\E m \in Members : Drain(m)) \/ (\E m \in Members : Down(m))
  \/ (\E m \in Members : Restart(m))
  \/ ReaderOff \/ AllReadersOff \/ ReaderUp
  \/ FriendUp \/ FriendDown
  \/ Tick

Fairness ==
  /\ WF_vars(Tick)
  /\ WF_vars(TakeWork) /\ WF_vars(FinishWork)
  /\ WF_vars(DrainReturn) /\ WF_vars(WorkDeadline)
  /\ WF_vars(AskRoom) /\ WF_vars(PrimaryDeadline)
  /\ WF_vars(ReadBegin) /\ WF_vars(ReadFinish) /\ WF_vars(ReadReturn) /\ WF_vars(ReadDeadline)
  /\ WF_vars(FriendDeal) /\ WF_vars(FriendTake) /\ WF_vars(FriendDeadline)
  /\ WF_vars(RouteServes) /\ WF_vars(RouteEnds) /\ WF_vars(RouteDeadline)
  /\ WF_vars(BenchGrant) /\ WF_vars(BenchGive)
  /\ WF_vars(BenchExpire) /\ WF_vars(BenchDeadline)
  /\ WF_vars(GateStart) /\ WF_vars(LandStart)
  /\ WF_vars(LandFinish) /\ WF_vars(MergeDeadline) /\ WF_vars(GateDeadline)
  /\ WF_vars(PushAck) /\ WF_vars(PushDeadline)
  /\ WF_vars(HookAnswer) /\ WF_vars(HookDeadline)
  /\ (\A m \in Members : WF_vars(Hold(m)) /\ WF_vars(Drain(m))
                       /\ WF_vars(Down(m)) /\ WF_vars(Restart(m)))
  /\ IF Broken = "heldgate"
       THEN TRUE
       ELSE (\A m \in Members : WF_vars(Release(m)))
  /\ WF_vars(ReaderOff) /\ WF_vars(AllReadersOff) /\ WF_vars(ReaderUp)
  /\ WF_vars(FriendUp) /\ WF_vars(FriendDown)

\* LiveNext: the liveness instance holds the outside state as Init placed it
\* (a broken witness sets the outside it needs in Init) and asks only that the
\* waits themselves end by progress or at their deadline; the outside events
\* above are what a quiet bench does not do while a wait is measured.
LiveNext ==
  \/ TakeWork \/ FinishWork \/ DrainReturn \/ WorkDeadline
  \/ AskRoom \/ PrimaryDeadline
  \/ ReadBegin \/ ReadFinish \/ ReadReturn \/ ReadDeadline
  \/ FriendDeal \/ FriendTake \/ FriendDeadline
  \/ RouteServes \/ RouteDeadline
  \/ BenchGrant \/ BenchGive \/ BenchExpire \/ BenchDeadline
  \/ GateStart \/ LandStart \/ LandFinish \/ MergeDeadline
  \/ GateDeadline
  \/ PushAck \/ PushDeadline
  \/ HookAnswer \/ HookDeadline
  \/ Tick

LiveFairness ==
  /\ WF_vars(Tick)
  /\ WF_vars(TakeWork) /\ WF_vars(FinishWork) /\ WF_vars(DrainReturn) /\ WF_vars(WorkDeadline)
  /\ WF_vars(AskRoom) /\ WF_vars(PrimaryDeadline)
  /\ WF_vars(ReadBegin) /\ WF_vars(ReadFinish) /\ WF_vars(ReadReturn) /\ WF_vars(ReadDeadline)
  /\ WF_vars(FriendDeal) /\ WF_vars(FriendTake) /\ WF_vars(FriendDeadline)
  /\ WF_vars(RouteServes) /\ WF_vars(RouteDeadline)
  /\ WF_vars(BenchGrant) /\ WF_vars(BenchGive) /\ WF_vars(BenchExpire) /\ WF_vars(BenchDeadline)
  /\ WF_vars(GateStart) /\ WF_vars(LandStart) /\ WF_vars(LandFinish)
  /\ WF_vars(MergeDeadline) /\ WF_vars(GateDeadline)
  /\ WF_vars(PushAck) /\ WF_vars(PushDeadline)
  /\ WF_vars(HookAnswer) /\ WF_vars(HookDeadline)

LiveSpec == Init /\ [][LiveNext]_vars /\ LiveFairness

Spec == Init /\ [][Next]_vars /\ Fairness

---------------------------------------------------------------------------
\* Safety.

\* Every wait's finite server deadline, or an action that would end it now.
EveryResourceWaitHasADeadline ==
  /\ (WaitW    => TRUE)
  /\ (WaitP    => TRUE)
  /\ (WaitRc   => HasDeadline(rcDeadline) \/ CanRc)
  /\ (WaitF    => TRUE)
  /\ (WaitRw   => TRUE)
  /\ (WaitBw   => HasDeadline(bwDeadline) \/ CanBw)
  /\ (WaitMg   => HasDeadline(mgDeadline) \/ CanMg)
  /\ (WaitGate => HasDeadline(mgDeadline) \/ CanGate)
  /\ (WaitPush => HasDeadline(hookDeadline) \/ CanPush)
  /\ (WaitAsk  => HasDeadline(hookDeadline) \/ CanAsk)

\* The derived wait-for relation over resource holders. A member waiting for a
\* bench waits on the lease's holder; a lander waiting on the heldgate witness
\* waits on the member whose held bench it refuses to use. No two holders wait
\* on each other.
Holders == Members \cup Readers \cup Routes \cup {"lander"}

WaitsOn(h) ==
  CASE h = "lander" ->
         IF Broken = "heldgate" /\ mg = "gating" /\ owner # "none"
         THEN {owner} ELSE {}
    [] h \in Members ->
         IF bw = "waiting" /\ owner # "none" /\ h # owner
         THEN {owner} ELSE {}
    [] OTHER -> {}

NoHoldWaitCycle ==
  \A h1, h2 \in Holders :
    h1 # h2 => ~(h2 \in WaitsOn(h1) /\ h1 \in WaitsOn(h2))

---------------------------------------------------------------------------
\* Liveness.

WaitAll ==
  \/ WaitW \/ WaitP \/ WaitRc \/ WaitF \/ WaitRw
  \/ WaitBw \/ WaitMg \/ WaitGate \/ WaitPush \/ WaitAsk

\* Every resource wait ends: by progress or at its deadline with a judgment.
\* The model starts each wait in Init (the Start* definitions name the Go
\* function that creates it), so the run has one episode of each wait.
ResourceWaitEnds == WaitAll ~> ~WaitAll

===========================================================================
