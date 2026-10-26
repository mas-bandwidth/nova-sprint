---- MODULE WaitFor ----
(***************************************************************************)
(* WaitFor models the card-side wait-for graph, open judgments, landing    *)
(* conflict reworks, gate-needs fact routing, and deadline/auto-resume     *)
(* exits in internal/sprint (cycle.go, soft_deps.go, steps_edit.go,        *)
(* steps_tick.go, wait_refused.go, seat_answers.go, redo.go,               *)
(* brief_bound.go, steps_merge.go, deal.go, steps_work.go,                 *)
(* steps_review.go).                                                       *)
(*                                                                         *)
(* Broken = "none" is the target v1.7 design. Each other value of Broken   *)
(* is a reversed witness reproducing TODAY's code:                         *)
(*   "gatefix"       - class A (steps_work.go:2279-2297): gate card's      *)
(*                     HOLD naming a fix card that needs the gate spends   *)
(*                     attempts to NBound with no Split step.              *)
(*   "hardonlycycle" - class B (cycle.go:97-112, steps_edit.go:382):       *)
(*                     needsCycleWith walks hard needs only, missing       *)
(*                     cycles closed through LAND-AFTER.                   *)
(*   "landedpoisons" - class B (soft_deps.go:357-407): softCycleWith walks *)
(*                     Placed (including landed cards) and returns any     *)
(*                     reachable cycle, refusing an unrelated edit.        *)
(*   "conflictbound" - class C (redo.go:108-135, brief_bound.go:356-369):  *)
(*                     landRefused counts a merge conflict toward          *)
(*                     AtBriefBound and raises NBriefWrong.                *)
(*   "hotlivelock"   - class C (steps_merge.go:447, redo.go:173):          *)
(*                     cards conflicting on one shared file rework without *)
(*                     hot-file ordering, starving on repeated conflicts.  *)
(*   "reportrefuses" - class D (steps_merge.go:484-501): mergeStep filters *)
(*                     merge --landed through MergeSoftOrder and unlanded  *)
(*                     needs after Push, refusing Report.                  *)
(*   "cannotaskloop" - class E (seat_answers.go:371-372,482-497):          *)
(*                     TickDecisionDefaults answers NCannotAsk with        *)
(*                     "wait 30m and re-ask", restarting the due clock.    *)
(*   "envheld"       - class F (steps_work.go:2279, seat_answers.go:445):  *)
(*                     missing-tool failure parks card in Waiting with     *)
(*                     server-default hold instead of waiting on a fact.   *)
(*   "factblind"     - class H (steps_review.go:1825, redo.go:294):        *)
(*                     Rework/Redo deal a card with GATE-NEEDS without     *)
(*                     GateHostPool and count the failed attempt.          *)
(*   "hiddencause"   - class I (steps_work.go:2279-2297): HOLD naming an   *)
(*                     open blocking card reworks as a failed attempt      *)
(*                     instead of turning the blocker into a need.         *)
(***************************************************************************)

EXTENDS Naturals, FiniteSets, Sequences, TLC

CONSTANTS
    Cards,          \* set of card ids, e.g. {1, 2, 3}
    Files,          \* set of shared files, e.g. {1}
    MaxAttempt,     \* attempt cap before NBound / NBriefWrong, e.g. 3
    MaxNow,         \* bounded clock horizon for finite state space
    Broken          \* "none" or one of the 10 broken witness names

Faults == {
    "none", "gatefix", "hardonlycycle", "landedpoisons", "conflictbound",
    "hotlivelock", "reportrefuses", "cannotaskloop", "envheld",
    "factblind", "hiddencause"
}
ASSUME Broken \in Faults

Columns == {"waiting", "ready", "working", "review", "merging", "landed", "dropped"}
HoldCauses == {"none", "coordinator", "server-default", "env"}
JudgmentKinds == {
    "none", "NBound", "NBriefWrong", "NCannotAsk", "NConflict",
    "NGateNeeds", "NHoldNoCause", "NLanderRefuses", "NWorkFailed"
}

VARIABLES
    col,                \* [Cards -> Columns]
    needs,              \* [Cards -> SUBSET Cards] hard dependency edges
    landAfter,          \* [Cards -> SUBSET Cards] soft LAND-AFTER edges
    gateArt,            \* [Cards -> SUBSET Cards] gate-artifact wait edges (class A)
    held,               \* [Cards -> HoldCauses]
    jKind,              \* [Cards -> JudgmentKinds]
    jDue,               \* [Cards -> 0..MaxNow]
    attempt,            \* [Cards -> 0..MaxAttempt]
    decidedAttempt,     \* [Cards -> 0..MaxAttempt]
    conflictCount,      \* [Cards -> [Files -> 0..MaxAttempt]]
    facts,              \* BOOLEAN: whether required gate/env fact is present
    cutBatch,           \* SUBSET Cards: cards cut for landing batch
    pushed,             \* SUBSET Cards: batch pushed to origin awaiting Report
    readerFree,         \* BOOLEAN: whether a qualified reader is free
    baseMoved,          \* BOOLEAN: whether base tip has settled after conflict
    editRefused,        \* BOOLEAN: whether an acyclic open-card edit was refused
    lastConflictSpent,  \* BOOLEAN: whether a conflict refusal spent an attempt
    now                 \* 0..MaxNow discrete store clock

vars == <<col, needs, landAfter, gateArt, held, jKind, jDue, attempt,
          decidedAttempt, conflictCount, facts, cutBatch, pushed,
          readerFree, baseMoved, editRefused, lastConflictSpent, now>>

TypeOK ==
    /\ col \in [Cards -> Columns]
    /\ needs \in [Cards -> SUBSET Cards]
    /\ landAfter \in [Cards -> SUBSET Cards]
    /\ gateArt \in [Cards -> SUBSET Cards]
    /\ held \in [Cards -> HoldCauses]
    /\ jKind \in [Cards -> JudgmentKinds]
    /\ jDue \in [Cards -> 0..MaxNow]
    /\ attempt \in [Cards -> 0..MaxAttempt]
    /\ decidedAttempt \in [Cards -> 0..MaxAttempt]
    /\ conflictCount \in [Cards -> [Files -> 0..MaxAttempt]]
    /\ facts \in BOOLEAN
    /\ cutBatch \subseteq Cards
    /\ pushed \subseteq Cards
    /\ readerFree \in BOOLEAN
    /\ baseMoved \in BOOLEAN
    /\ editRefused \in BOOLEAN
    /\ lastConflictSpent \in BOOLEAN
    /\ now \in 0..MaxNow

OpenCards == {c \in Cards : col[c] \notin {"landed", "dropped"}}

\* Wait-for edges over a card subset (needs, LAND-AFTER, and explicit gate-artifact edges).
WaitEdges(nMap, lMap, gMap, activeSet) ==
    {<<u, v>> \in activeSet \X activeSet :
        v \in nMap[u] \/ v \in lMap[u] \/ v \in gMap[u]}

Reachable(u, v, R) ==
    LET RECURSIVE Reach(_, _)
        Reach(x, visited) ==
            IF x = v THEN TRUE
            ELSE \E y \in Cards :
                /\ <<x, y>> \in R
                /\ y \notin visited
                /\ Reach(y, visited \cup {y})
    IN \E z \in Cards : <<u, z>> \in R /\ Reach(z, {z})

HasCycle(R) == \E c \in Cards : Reachable(c, c, R)

\* Control refuser: walks needs, LAND-AFTER, and gate-artifact edges over OpenCards only.
WouldCycleControl(nMap, lMap, gMap) ==
    HasCycle(WaitEdges(nMap, lMap, gMap, OpenCards))

\* Broken "hardonlycycle" (internal/sprint/cycle.go:97-112): walks hard needs only.
WouldCycleHardOnly(nMap) ==
    HasCycle({<<u, v>> \in OpenCards \X OpenCards : v \in nMap[u]})

\* Broken "landedpoisons" (internal/sprint/soft_deps.go:357-407): walks all Placed
\* cards (including landed cards) and returns any reachable cycle.
WouldCycleLandedPoisons(nMap, lMap, gMap) ==
    HasCycle(WaitEdges(nMap, lMap, gMap, Cards))

UnmetNeeds(c) == {d \in needs[c] : col[d] # "landed"}
UnmetSoft(c) == {d \in landAfter[c] : col[d] \notin {"landed", "dropped"}}
NextDue == IF now < MaxNow THEN now + 1 ELSE MaxNow

Init ==
    /\ col = [c \in Cards |->
        IF Broken = "landedpoisons" /\ c \in {1, 2} THEN "landed"
        ELSE IF Broken = "landedpoisons" /\ c = 3 THEN "ready"
        ELSE IF c = 1 THEN "ready" ELSE "waiting"]
    /\ needs = [c \in Cards |->
        IF Broken = "landedpoisons" /\ c = 1 THEN {2}
        ELSE IF Broken = "gatefix" /\ c = 2 THEN {1}
        ELSE {}]
    /\ landAfter = [c \in Cards |->
        IF Broken = "landedpoisons" /\ c = 2 THEN {1}
        ELSE {}]
    /\ gateArt = [c \in Cards |-> {}]
    /\ held = [c \in Cards |-> "none"]
    /\ jKind = [c \in Cards |-> "none"]
    /\ jDue = [c \in Cards |-> 0]
    /\ attempt = [c \in Cards |-> 0]
    /\ decidedAttempt = [c \in Cards |-> 0]
    /\ conflictCount = [c \in Cards |-> [f \in Files |-> 0]]
    /\ facts = (Broken \notin {"envheld", "factblind", "none"})
    /\ cutBatch = {}
    /\ pushed = {}
    /\ readerFree = (Broken \notin {"cannotaskloop", "none"})
    /\ baseMoved = TRUE
    /\ editRefused = FALSE
    /\ lastConflictSpent = FALSE
    /\ now = 1

\* Deal — internal/sprint/deal.go: DealWork, gateNeedsNow, factsPlan
Deal(c) ==
    /\ col[c] = "ready"
    /\ held[c] = "none"
    /\ jKind[c] = "none"
    /\ UnmetNeeds(c) = {}
    /\ baseMoved
    /\ col' = [col EXCEPT ![c] = "working"]
    /\ UNCHANGED <<needs, landAfter, gateArt, held, jKind, jDue, attempt,
                   decidedAttempt, conflictCount, facts, cutBatch, pushed,
                   readerFree, baseMoved, editRefused, lastConflictSpent, now>>

\* Finish (ok) — internal/sprint/steps_work.go: finishPlan
FinishOk(c) ==
    /\ col[c] = "working"
    /\ jKind[c] = "none"
    /\ facts
    /\ ~(Broken = "gatefix" /\ c = 1 /\ col[2] # "landed")
    /\ ~(Broken = "hiddencause" /\ c = 1 /\ col[2] # "landed")
    /\ col' = [col EXCEPT ![c] = "review"]
    /\ UNCHANGED <<needs, landAfter, gateArt, held, jKind, jDue, attempt,
                   decidedAttempt, conflictCount, facts, cutBatch, pushed,
                   readerFree, baseMoved, editRefused, lastConflictSpent, now>>

\* Finish (HOLD naming cards that need c, class A) — internal/sprint/steps_work.go:2279-2297 finishPlan
FinishHoldGateFix(c, fix) ==
    /\ Broken = "gatefix"
    /\ c = 1 /\ fix = 2
    /\ col[c] = "working"
    /\ jKind[c] = "none"
    /\ c \in needs[fix]
    /\ col[fix] # "landed"
    /\ LET nextAtt == IF attempt[c] < MaxAttempt THEN attempt[c] + 1 ELSE MaxAttempt IN
       /\ attempt' = [attempt EXCEPT ![c] = nextAtt]
       /\ IF nextAtt >= MaxAttempt
          THEN /\ jKind' = [jKind EXCEPT ![c] = "NBound"]
               /\ jDue' = [jDue EXCEPT ![c] = NextDue]
               /\ UNCHANGED col
          ELSE /\ col' = [col EXCEPT ![c] = "ready"]
               /\ UNCHANGED <<jKind, jDue>>
       /\ UNCHANGED <<needs, landAfter, gateArt, held, decidedAttempt,
                      conflictCount, facts, cutBatch, pushed, readerFree,
                      baseMoved, editRefused, lastConflictSpent, now>>

\* Split — internal/sprint/steps_work.go: Split (v17-hold-naming-cards-becomes-a-need-or-a-split)
Split(c, fix) ==
    /\ Broken # "gatefix"
    /\ c = 1 /\ fix = 2
    /\ col[c] = "working"
    /\ jKind[c] = "none"
    /\ c \in needs[fix]
    /\ col[fix] # "landed"
    /\ LET n2 == [needs EXCEPT ![fix] = @ \ {c}, ![c] = @ \cup {fix}]
           g2 == [gateArt EXCEPT ![c] = @ \ {fix}]
       IN
       /\ ~WouldCycleControl(n2, landAfter, g2)
       /\ needs' = n2
       /\ gateArt' = g2
       /\ col' = [col EXCEPT ![c] = "waiting",
                             ![fix] = IF col[fix] = "waiting" THEN "ready" ELSE col[fix]]
       /\ UNCHANGED <<landAfter, held, jKind, jDue, attempt, decidedAttempt,
                      conflictCount, facts, cutBatch, pushed, readerFree,
                      baseMoved, editRefused, lastConflictSpent, now>>

\* Finish (HOLD naming cards that do not need c, class I) — internal/sprint/steps_work.go:2279-2297 finishPlan
FinishHoldHiddenCause(c, blocker) ==
    /\ c = 1 /\ blocker = 2
    /\ col[c] = "working"
    /\ jKind[c] = "none"
    /\ col[blocker] # "landed"
    /\ c \notin needs[blocker]
    /\ blocker \notin needs[c]
    /\ IF Broken = "hiddencause"
       THEN LET nextAtt == IF attempt[c] < MaxAttempt THEN attempt[c] + 1 ELSE MaxAttempt IN
            /\ attempt' = [attempt EXCEPT ![c] = nextAtt]
            /\ IF nextAtt >= MaxAttempt
               THEN /\ jKind' = [jKind EXCEPT ![c] = "NBound"]
                    /\ jDue' = [jDue EXCEPT ![c] = NextDue]
                    /\ UNCHANGED col
               ELSE /\ col' = [col EXCEPT ![c] = "ready"]
                    /\ UNCHANGED <<jKind, jDue>>
            /\ UNCHANGED <<needs, landAfter, gateArt, held, decidedAttempt,
                           conflictCount, facts, cutBatch, pushed, readerFree,
                           baseMoved, editRefused, lastConflictSpent, now>>
       ELSE /\ Broken = "none"
            /\ attempt[c] = 0
            /\ LET n2 == [needs EXCEPT ![c] = @ \cup {blocker}] IN
               /\ ~WouldCycleControl(n2, landAfter, gateArt)
               /\ needs' = n2
               /\ col' = [col EXCEPT ![c] = "waiting"]
               /\ UNCHANGED <<landAfter, gateArt, held, jKind, jDue, attempt,
                              decidedAttempt, conflictCount, facts, cutBatch,
                              pushed, readerFree, baseMoved, editRefused,
                              lastConflictSpent, now>>

\* Finish (failed) — internal/sprint/steps_work.go:2279 finishPlan
FinishFailed(c) ==
    /\ Broken = "none"
    /\ c = 1
    /\ col[c] = "working"
    /\ jKind[c] = "none"
    /\ facts
    /\ attempt[c] = 0
    /\ attempt' = [attempt EXCEPT ![c] = 1]
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ UNCHANGED <<needs, landAfter, gateArt, held, jKind, jDue, decidedAttempt,
                   conflictCount, facts, cutBatch, pushed, readerFree,
                   baseMoved, editRefused, lastConflictSpent, now>>

\* Finish (failed for a missing tool, class F) — internal/sprint/steps_work.go:2279 finishPlan
FinishEnvMissing(c) ==
    /\ c = 1
    /\ col[c] = "working"
    /\ jKind[c] = "none"
    /\ ~facts
    /\ Broken \in {"none", "envheld"}
    /\ IF Broken = "envheld"
       THEN /\ attempt' = [attempt EXCEPT ![c] = MaxAttempt]
            /\ jKind' = [jKind EXCEPT ![c] = "NBound"]
            /\ jDue' = [jDue EXCEPT ![c] = NextDue]
            /\ UNCHANGED <<col, needs, landAfter, gateArt, held, decidedAttempt,
                           conflictCount, facts, cutBatch, pushed, readerFree,
                           baseMoved, editRefused, lastConflictSpent, now>>
       ELSE /\ col' = [col EXCEPT ![c] = "waiting"]
            /\ held' = [held EXCEPT ![c] = "env"]
            /\ UNCHANGED <<needs, landAfter, gateArt, jKind, jDue, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           pushed, readerFree, baseMoved, editRefused,
                           lastConflictSpent, now>>

\* Finish (failed on a member without its facts, class H) — internal/sprint/steps_review.go:1825 Rework, redo.go:294 Redo
FinishFactBlind(c) ==
    /\ c = 1
    /\ col[c] = "working"
    /\ jKind[c] = "none"
    /\ ~facts
    /\ Broken \in {"none", "factblind"}
    /\ IF Broken = "factblind"
       THEN LET nextAtt == IF attempt[c] < MaxAttempt THEN attempt[c] + 1 ELSE MaxAttempt IN
            /\ attempt' = [attempt EXCEPT ![c] = nextAtt]
            /\ IF nextAtt >= MaxAttempt
               THEN /\ jKind' = [jKind EXCEPT ![c] = "NBound"]
                    /\ jDue' = [jDue EXCEPT ![c] = NextDue]
                    /\ UNCHANGED col
               ELSE /\ col' = [col EXCEPT ![c] = "ready"]
                    /\ UNCHANGED <<jKind, jDue>>
            /\ UNCHANGED <<needs, landAfter, gateArt, held, decidedAttempt,
                           conflictCount, facts, cutBatch, pushed, readerFree,
                           baseMoved, editRefused, lastConflictSpent, now>>
       ELSE /\ col' = [col EXCEPT ![c] = "ready"]
            /\ jKind' = [jKind EXCEPT ![c] = "NGateNeeds"]
            /\ jDue' = [jDue EXCEPT ![c] = NextDue]
            /\ UNCHANGED <<needs, landAfter, gateArt, held, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           pushed, readerFree, baseMoved, editRefused,
                           lastConflictSpent, now>>

\* Read — internal/sprint/deal.go: DealReads, steps_review.go: finishReadPlan
Read(c) ==
    /\ col[c] = "review"
    /\ jKind[c] = "none"
    /\ IF readerFree
       THEN /\ col' = [col EXCEPT ![c] = "merging"]
            /\ UNCHANGED <<jKind, jDue>>
       ELSE /\ jKind' = [jKind EXCEPT ![c] = "NCannotAsk"]
            /\ jDue' = [jDue EXCEPT ![c] = NextDue]
            /\ UNCHANGED col
    /\ UNCHANGED <<needs, landAfter, gateArt, held, attempt, decidedAttempt,
                   conflictCount, facts, cutBatch, pushed, readerFree,
                   baseMoved, editRefused, lastConflictSpent, now>>

\* Accept — internal/sprint/steps_review.go: Accept
Accept(c) ==
    /\ col[c] = "review"
    /\ jKind[c] = "NCannotAsk"
    /\ Broken # "cannotaskloop"
    /\ col' = [col EXCEPT ![c] = "merging"]
    /\ jKind' = [jKind EXCEPT ![c] = "none"]
    /\ jDue' = [jDue EXCEPT ![c] = 0]
    /\ UNCHANGED <<needs, landAfter, gateArt, held, attempt, decidedAttempt,
                   conflictCount, facts, cutBatch, pushed, readerFree,
                   baseMoved, editRefused, lastConflictSpent, now>>

\* Cut — internal/sprint/steps_merge.go:447 mergeStep (cut landing batch)
Cut(c) ==
    /\ col[c] = "merging"
    /\ jKind[c] = "none"
    /\ c \notin cutBatch
    /\ c \notin pushed
    /\ UnmetNeeds(c) = {}
    /\ UnmetSoft(c) = {}
    /\ ~(Broken = "hotlivelock" /\ c = 1 /\ col[2] # "landed")
    /\ cutBatch' = cutBatch \cup {c}
    /\ UNCHANGED <<col, needs, landAfter, gateArt, held, jKind, jDue, attempt,
                   decidedAttempt, conflictCount, facts, pushed, readerFree,
                   baseMoved, editRefused, lastConflictSpent, now>>

\* Push — internal/sprint/steps_merge.go: mergeStep (git push batch to origin)
Push(c) ==
    /\ c \in cutBatch
    /\ c \notin pushed
    /\ cutBatch' = cutBatch \ {c}
    /\ pushed' = pushed \cup {c}
    /\ UNCHANGED <<col, needs, landAfter, gateArt, held, jKind, jDue, attempt,
                   decidedAttempt, conflictCount, facts, readerFree, baseMoved,
                   editRefused, lastConflictSpent, now>>

\* Report — internal/sprint/steps_merge.go:484-501 mergeStep (merge --landed report)
Report(c) ==
    /\ c \in pushed
    /\ IF Broken = "reportrefuses" /\ \E d \in Cards \ {c} : col[d] # "landed"
       THEN /\ pushed' = pushed
            /\ jKind' = [jKind EXCEPT ![c] = "NLanderRefuses"]
            /\ jDue' = [jDue EXCEPT ![c] = NextDue]
            /\ UNCHANGED <<col, needs, landAfter, gateArt, held, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           readerFree, baseMoved, editRefused, lastConflictSpent, now>>
       ELSE /\ pushed' = pushed \ {c}
            /\ col' = [col EXCEPT ![c] = "landed"]
            /\ jKind' = [jKind EXCEPT ![c] = "none"]
            /\ jDue' = [jDue EXCEPT ![c] = 0]
            /\ UNCHANGED <<needs, landAfter, gateArt, held, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           readerFree, baseMoved, editRefused, lastConflictSpent, now>>

\* LandRefused — internal/sprint/redo.go:108-173 landRefused, brief_bound.go:356-369
LandRefused(c, f) ==
    /\ c = 1
    /\ col[c] = "merging"
    /\ c \notin cutBatch
    /\ c \notin pushed
    /\ jKind[c] = "none"
    /\ f \in Files
    /\ conflictCount[c][f] < 1
    /\ Broken \in {"none", "conflictbound", "hotlivelock"}
    /\ IF Broken = "conflictbound"
       THEN /\ attempt' = [attempt EXCEPT ![c] = MaxAttempt]
            /\ conflictCount' = [conflictCount EXCEPT ![c][f] = @ + 1]
            /\ jKind' = [jKind EXCEPT ![c] = "NBriefWrong"]
            /\ jDue' = [jDue EXCEPT ![c] = NextDue]
            /\ lastConflictSpent' = TRUE
            /\ UNCHANGED <<col, needs, landAfter, gateArt, held, decidedAttempt,
                           facts, cutBatch, pushed, readerFree, baseMoved,
                           editRefused, now>>
       ELSE IF Broken = "hotlivelock"
       THEN /\ col' = [col EXCEPT ![c] = "ready"]
            /\ UNCHANGED <<needs, landAfter, gateArt, held, jKind, jDue, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           pushed, readerFree, baseMoved, editRefused,
                           lastConflictSpent, now>>
       ELSE /\ col' = [col EXCEPT ![c] = "ready"]
            /\ conflictCount' = [conflictCount EXCEPT ![c][f] = @ + 1]
            /\ baseMoved' = FALSE
            /\ lastConflictSpent' = FALSE
            /\ UNCHANGED <<needs, landAfter, gateArt, held, jKind, jDue, attempt,
                           decidedAttempt, facts, cutBatch, pushed, readerFree,
                           editRefused, now>>

\* Rework — internal/sprint/steps_review.go:1825 Rework
Rework(c) ==
    /\ col[c] \in {"working", "review", "merging"}
    /\ jKind[c] \in {"NConflict", "NHoldNoCause", "NWorkFailed"}
    /\ c \notin pushed
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ jKind' = [jKind EXCEPT ![c] = "none"]
    /\ jDue' = [jDue EXCEPT ![c] = 0]
    /\ UNCHANGED <<needs, landAfter, gateArt, held, attempt, decidedAttempt,
                   conflictCount, facts, cutBatch, pushed, readerFree,
                   baseMoved, editRefused, lastConflictSpent, now>>

\* AutoResume — internal/sprint/steps_tick.go: TickNeeds (resumes waiting card when cause clears)
AutoResumeCard(c) ==
    /\ col[c] = "waiting"
    /\ jKind[c] = "none"
    /\ UnmetNeeds(c) = {}
    /\ gateArt[c] = {}
    /\ \/ held[c] = "none"
       \/ (held[c] = "env" /\ facts)
    /\ ~(Broken \in {"gatefix", "hiddencause", "hotlivelock"} /\ c = 2 /\ col[1] # "landed")
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ held' = [held EXCEPT ![c] = "none"]
    /\ UNCHANGED <<needs, landAfter, gateArt, jKind, jDue, attempt,
                   decidedAttempt, conflictCount, facts, cutBatch, pushed,
                   readerFree, baseMoved, editRefused, lastConflictSpent, now>>

\* Overdue — internal/sprint/steps_tick.go: TickOverdue (store clock with MaxNow shift abstraction)
Overdue ==
    /\ \E c \in OpenCards : jKind[c] # "none"
    /\ IF now < MaxNow
       THEN /\ now' = now + 1
            /\ jDue' = jDue
       ELSE /\ now' = now
            /\ jDue' = [c \in Cards |-> IF jDue[c] > 1 THEN jDue[c] - 1 ELSE jDue[c]]
    /\ UNCHANGED <<col, needs, landAfter, gateArt, held, jKind, attempt,
                   decidedAttempt, conflictCount, facts, cutBatch, pushed,
                   readerFree, baseMoved, editRefused, lastConflictSpent>>

\* ServerDefault — internal/sprint/seat_answers.go:369-374,445-497 TickDecisionDefaults, SafeDecisionDefault
ServerDefault(c) ==
    /\ jKind[c] # "none"
    /\ now >= jDue[c]
    /\ IF jKind[c] = "NCannotAsk" /\ Broken = "cannotaskloop"
       THEN /\ jKind' = [jKind EXCEPT ![c] = "NCannotAsk"]
            /\ jDue' = [jDue EXCEPT ![c] = MaxNow]
            /\ now' = IF now > 1 THEN now - 1 ELSE 1
            /\ UNCHANGED <<col, needs, landAfter, gateArt, held, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           pushed, readerFree, baseMoved, editRefused,
                           lastConflictSpent>>
       ELSE IF jKind[c] \in {"NBound", "NBriefWrong"} /\ Broken \in {"envheld", "gatefix", "factblind", "hiddencause", "conflictbound"}
       THEN /\ col' = [col EXCEPT ![c] = "waiting"]
            /\ held' = [held EXCEPT ![c] = "server-default"]
            /\ jKind' = [jKind EXCEPT ![c] = "none"]
            /\ jDue' = [jDue EXCEPT ![c] = 0]
            /\ decidedAttempt' = [decidedAttempt EXCEPT ![c] = attempt[c]]
            /\ UNCHANGED <<needs, landAfter, gateArt, attempt, conflictCount,
                           facts, cutBatch, pushed, readerFree, baseMoved,
                           editRefused, lastConflictSpent, now>>
       ELSE /\ jKind' = [jKind EXCEPT ![c] = "none"]
            /\ jDue' = [jDue EXCEPT ![c] = 0]
            /\ col' = [col EXCEPT ![c] = IF col[c] = "review" THEN "merging" ELSE "ready"]
            /\ held' = [held EXCEPT ![c] = "none"]
            /\ UNCHANGED <<needs, landAfter, gateArt, attempt, decidedAttempt,
                           conflictCount, facts, cutBatch, pushed, readerFree,
                           baseMoved, editRefused, lastConflictSpent, now>>

\* OwnerDeliver — internal/sprint/seat_answers.go: OwnerDeliver
OwnerDeliver(c) ==
    /\ jKind[c] = "NCannotAsk"
    /\ Broken # "cannotaskloop"
    /\ now >= jDue[c]
    /\ jKind' = [jKind EXCEPT ![c] = "none"]
    /\ jDue' = [jDue EXCEPT ![c] = 0]
    /\ col' = [col EXCEPT ![c] = "merging"]
    /\ UNCHANGED <<needs, landAfter, gateArt, held, attempt, decidedAttempt,
                   conflictCount, facts, cutBatch, pushed, readerFree,
                   baseMoved, editRefused, lastConflictSpent, now>>

\* Answer(c, d) — internal/sprint/seat_answers.go: Answer for offered decision d
Answer(c, d) ==
    /\ jKind[c] # "none"
    /\ d \in {"rework", "accept", "release"}
    /\ Broken = "none"
    /\ jKind' = [jKind EXCEPT ![c] = "none"]
    /\ jDue' = [jDue EXCEPT ![c] = 0]
    /\ held' = [held EXCEPT ![c] = "none"]
    /\ decidedAttempt' = [decidedAttempt EXCEPT ![c] = attempt[c]]
    /\ col' = [col EXCEPT ![c] =
        IF col[c] = "review" THEN "merging"
        ELSE IF col[c] = "merging" THEN "merging"
        ELSE "ready"]
    /\ UNCHANGED <<needs, landAfter, gateArt, attempt, conflictCount, facts,
                   cutBatch, pushed, readerFree, baseMoved, editRefused,
                   lastConflictSpent, now>>

\* NeedsSet — internal/sprint/steps_edit.go:382 needsCycleWith
NeedsSet(c, target) ==
    /\ Broken \in {"none", "hardonlycycle"}
    /\ c = 2 /\ target = 1
    /\ col[c] = "waiting"
    /\ col[target] = "ready"
    /\ target \notin needs[c]
    /\ LET n2 == [needs EXCEPT ![c] = @ \cup {target}] IN
       IF Broken = "hardonlycycle"
       THEN /\ ~WouldCycleHardOnly(n2)
            /\ needs' = n2
            /\ UNCHANGED <<col, landAfter, gateArt, held, jKind, jDue, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           pushed, readerFree, baseMoved, editRefused,
                           lastConflictSpent, now>>
       ELSE /\ ~WouldCycleControl(n2, landAfter, gateArt)
            /\ needs' = n2
            /\ UNCHANGED <<col, landAfter, gateArt, held, jKind, jDue, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           pushed, readerFree, baseMoved, editRefused,
                           lastConflictSpent, now>>

\* LandAfterSet — internal/sprint/soft_deps.go:357-407 softCycleWith
LandAfterSet(c, target) ==
    /\ Broken \in {"none", "hardonlycycle", "landedpoisons"}
    /\ \/ (Broken \in {"none", "hardonlycycle"} /\ c = 1 /\ target = 2 /\ col[1] = "ready" /\ col[2] = "waiting")
       \/ (Broken = "landedpoisons" /\ c = 3 /\ target = 1 /\ col[3] = "ready")
    /\ target \notin landAfter[c]
    /\ LET l2 == [landAfter EXCEPT ![c] = @ \cup {target}] IN
       IF Broken = "landedpoisons"
       THEN IF WouldCycleLandedPoisons(needs, l2, gateArt)
            THEN /\ editRefused' = (~WouldCycleControl(needs, l2, gateArt))
                 /\ UNCHANGED <<col, needs, landAfter, gateArt, held, jKind, jDue,
                                attempt, decidedAttempt, conflictCount, facts,
                                cutBatch, pushed, readerFree, baseMoved,
                                lastConflictSpent, now>>
            ELSE /\ landAfter' = l2
                 /\ UNCHANGED <<col, needs, gateArt, held, jKind, jDue, attempt,
                                decidedAttempt, conflictCount, facts, cutBatch,
                                pushed, readerFree, baseMoved, editRefused,
                                lastConflictSpent, now>>
       ELSE /\ ~WouldCycleControl(needs, l2, gateArt)
            /\ landAfter' = l2
            /\ UNCHANGED <<col, needs, gateArt, held, jKind, jDue, attempt,
                           decidedAttempt, conflictCount, facts, cutBatch,
                           pushed, readerFree, baseMoved, editRefused,
                           lastConflictSpent, now>>

\* Add — internal/sprint/steps_add.go: addPlan (behind the unified cycle refuser)
Add(c, target) ==
    /\ Broken = "none"
    /\ c = 3 /\ target = 2
    /\ col[c] = "waiting"
    /\ col[target] = "waiting"
    /\ target \notin needs[c]
    /\ LET n2 == [needs EXCEPT ![c] = @ \cup {target}] IN
       /\ ~WouldCycleControl(n2, landAfter, gateArt)
       /\ needs' = n2
       /\ UNCHANGED <<col, landAfter, gateArt, held, jKind, jDue, attempt,
                      decidedAttempt, conflictCount, facts, cutBatch, pushed,
                      readerFree, baseMoved, editRefused, lastConflictSpent, now>>

\* BaseMoves — outside event settling base tip for a conflict-refused card
BaseMoves ==
    /\ ~baseMoved
    /\ baseMoved' = TRUE
    /\ UNCHANGED <<col, needs, landAfter, gateArt, held, jKind, jDue, attempt,
                   decidedAttempt, conflictCount, facts, cutBatch, pushed,
                   readerFree, editRefused, lastConflictSpent, now>>

\* FactAppears — outside event supplying the required gate/environment fact
FactAppears ==
    /\ ~facts
    /\ Broken # "factblind"
    /\ facts' = TRUE
    /\ jKind' = [c \in Cards |-> IF jKind[c] = "NGateNeeds" THEN "none" ELSE jKind[c]]
    /\ jDue' = [c \in Cards |-> IF jKind[c] = "NGateNeeds" THEN 0 ELSE jDue[c]]
    /\ UNCHANGED <<col, needs, landAfter, gateArt, held, attempt, decidedAttempt,
                   conflictCount, cutBatch, pushed, readerFree, baseMoved,
                   editRefused, lastConflictSpent, now>>

\* ReaderFrees — outside event freeing a reader for review
ReaderFrees ==
    /\ ~readerFree
    /\ Broken # "cannotaskloop"
    /\ readerFree' = TRUE
    /\ UNCHANGED <<col, needs, landAfter, gateArt, held, jKind, jDue, attempt,
                   decidedAttempt, conflictCount, facts, cutBatch, pushed,
                   baseMoved, editRefused, lastConflictSpent, now>>

Next ==
    \/ \E c \in Cards :
        \/ Deal(c)
        \/ FinishOk(c)
        \/ FinishFailed(c)
        \/ FinishEnvMissing(c)
        \/ FinishFactBlind(c)
        \/ Read(c)
        \/ Accept(c)
        \/ Cut(c)
        \/ Push(c)
        \/ Report(c)
        \/ Rework(c)
        \/ AutoResumeCard(c)
        \/ ServerDefault(c)
        \/ OwnerDeliver(c)
        \/ \E d \in {"rework", "accept", "release"} : Answer(c, d)
        \/ \E f \in Files : LandRefused(c, f)
        \/ \E other \in Cards :
            \/ FinishHoldGateFix(c, other)
            \/ Split(c, other)
            \/ FinishHoldHiddenCause(c, other)
            \/ NeedsSet(c, other)
            \/ LandAfterSet(c, other)
            \/ Add(c, other)
    \/ Overdue
    \/ BaseMoves
    \/ FactAppears
    \/ ReaderFrees

-----------------------------------------------------------------------------
\* Safety & Action Properties.

\* NoWaitCycle: needs, LAND-AFTER, and gate-artifact edges over open cards are acyclic.
NoWaitCycle ==
    ~WouldCycleControl(needs, landAfter, gateArt)

\* EveryJudgmentHasADue: every open judgment on a card carries a positive finite due.
EveryJudgmentHasADue ==
    \A c \in Cards : jKind[c] # "none" => (jDue[c] >= 1 /\ jDue[c] <= MaxNow)

\* ConflictSpendsNoAttempt: a merge conflict refusal never spends an attempt or raises NBriefWrong.
ConflictSpendsNoAttempt ==
    ~lastConflictSpent

\* EditRefusedOnlyForItsOwnCycle: an acyclic edit on an open card is never refused
\* because of a cycle between already-landed cards.
EditRefusedOnlyForItsOwnCycle ==
    ~editRefused

\* PushedIsRecorded (action property): a pushed card's Report step records it as landed
\* and never refuses it with NLanderRefuses.
PushedIsRecorded ==
    [][\A c \in Cards :
        (c \in pushed /\ jKind[c] = "none" /\ jKind'[c] = "NLanderRefuses") => (c \notin pushed')]_vars

-----------------------------------------------------------------------------
\* Liveness Properties & Fairness.

\* NeverSilentlyStuck: every open card has an enabled machine/wait path or a finite
\* judgment due, and every open judgment eventually clears.
NeverSilentlyStuck ==
    \A c \in Cards : (jKind[c] # "none") ~> (jKind[c] = "none")

\* AutoResume: a card waiting whose cause clears (needs landed, fact present)
\* leaves waiting without requiring an Answer step.
AutoResume ==
    \A c \in Cards :
        (col[c] = "waiting" /\ UnmetNeeds(c) = {} /\ gateArt[c] = {} /\ facts) ~> (col[c] # "waiting")

\* EveryFeasibleLands: every card eventually lands.
EveryFeasibleLands ==
    \A c \in Cards : <>(col[c] = "landed")

MachineFairness ==
    /\ WF_vars(Overdue)
    /\ WF_vars(BaseMoves)
    /\ WF_vars(FactAppears)
    /\ WF_vars(ReaderFrees)
    /\ \A c \in Cards :
        /\ WF_vars(Deal(c))
        /\ WF_vars(FinishOk(c))
        /\ WF_vars(FinishEnvMissing(c))
        /\ WF_vars(FinishFactBlind(c))
        /\ WF_vars(Read(c))
        /\ WF_vars(Accept(c))
        /\ WF_vars(Cut(c))
        /\ WF_vars(Push(c))
        /\ WF_vars(Report(c))
        /\ WF_vars(Rework(c))
        /\ WF_vars(AutoResumeCard(c))
        /\ WF_vars(ServerDefault(c))
        /\ WF_vars(OwnerDeliver(c))
        /\ \A f \in Files :
            /\ IF Broken = "hotlivelock" THEN WF_vars(LandRefused(c, f)) ELSE TRUE
        /\ \A other \in Cards :
            /\ WF_vars(Split(c, other))
            /\ WF_vars(FinishHoldGateFix(c, other))
            /\ WF_vars(FinishHoldHiddenCause(c, other))

AnswerFairness ==
    \A c \in Cards : \A d \in {"rework", "accept", "release"} : WF_vars(Answer(c, d))

Spec == Init /\ [][Next]_vars
LiveSpec == Init /\ [][Next]_vars /\ MachineFairness /\ AnswerFairness

====
