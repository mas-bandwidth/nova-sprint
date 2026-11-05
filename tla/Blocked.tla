------------------------------ MODULE Blocked ------------------------------
(***************************************************************************)
(* The blocked state of a primary (internal/sprint/blocked.go: FlapsOf,   *)
(* recordFlaps, TickBlock, Unblock; lifecycle.go Moves). The owner,       *)
(* 2026-11-05: "I want there to be something that automatically catches  *)
(* flaps, and moves them into 'blocked' card state"; "if a card flaps     *)
(* around more than n times, it goes into blocked primary card state";    *)
(* "blocked -> waiting -> ready -> etc."; "When a new card goes into      *)
(* blocked, it sits there until you review it and decide what to do."    *)
(*                                                                       *)
(* state[c] is the primary's column, or "dropped" once it is off the     *)
(* table. out[c] is what is dealt for it and not back: "work" (its work   *)
(* card on a member, ready or working) and "read" (a read card asked).    *)
(* flaps[c] counts its returns without progress in the window,           *)
(* saturating at Limit + 1: a dealt card that ends with no verdict, a     *)
(* merge refused, a rework. A finish, a verdict, an accept and a landing  *)
(* never count. head[c] is the work it holds: once a finish wrote it, no  *)
(* action takes it away. judged[c] is the blocked judgment open on it and *)
(* pushed[c] the pushes sent for it: one each per entry.                  *)
(*                                                                       *)
(* Abstracted: the window. A count in the code ages out of its window     *)
(* (default 30m); here it does not, so a card that bounces slower than    *)
(* Limit a window is outside the model (it is progressing by the owner's  *)
(* measure). unblocks bounds the coordinator's unblocks, so the state     *)
(* space is finite; a remind re-tells the judgment and changes no state,  *)
(* and a brief edit on a blocked card changes no state either, so neither *)
(* is an action here.                                                     *)
(*                                                                       *)
(* Broken: "none"; "noentry" (the tick never blocks: the reversed        *)
(* witness of NoInfiniteBounce); "nowithdraw" (entry leaves the work and  *)
(* read cards out: the witness of BlockedHasNothingOut).                  *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Limit, MaxUnblocks, Broken

Open == {"waiting", "ready", "working", "review", "merging"}
States == Open \cup {"blocked", "landed", "dropped"}
Blockable == {"ready", "working", "review"}

VARIABLES state, out, flaps, head, judged, pushed, unblocks

vars == <<state, out, flaps, head, judged, pushed, unblocks>>

TypeOK ==
    /\ state \in [Cards -> States]
    /\ out \in [Cards -> SUBSET {"work", "read"}]
    /\ flaps \in [Cards -> 0..(Limit + 1)]
    /\ head \in [Cards -> BOOLEAN]
    /\ judged \in [Cards -> 0..2]
    /\ pushed \in [Cards -> 0..2]
    /\ unblocks \in 0..MaxUnblocks

Init ==
    /\ state = [c \in Cards |-> "ready"]
    /\ out = [c \in Cards |-> {}]
    /\ flaps = [c \in Cards |-> 0]
    /\ head = [c \in Cards |-> FALSE]
    /\ judged = [c \in Cards |-> 0]
    /\ pushed = [c \in Cards |-> 0]
    /\ unblocks = 0

Flap(c) == flaps' = [flaps EXCEPT ![c] = IF @ > Limit THEN @ ELSE @ + 1]

\* the deal: a ready primary's work card goes to a member (TickDeal); never a blocked one
Deal(c) ==
    /\ state[c] = "ready"
    /\ state' = [state EXCEPT ![c] = "working"]
    /\ out' = [out EXCEPT ![c] = {"work"}]
    /\ UNCHANGED <<flaps, head, judged, pushed, unblocks>>

\* the work card's finish: forward, never a flap
Finish(c) ==
    /\ state[c] = "working" /\ "work" \in out[c]
    /\ state' = [state EXCEPT ![c] = "review"]
    /\ out' = [out EXCEPT ![c] = {}]
    /\ head' = [head EXCEPT ![c] = TRUE]
    /\ UNCHANGED <<flaps, judged, pushed, unblocks>>

\* the ask: a read card for a primary in review
Ask(c) ==
    /\ state[c] = "review" /\ "read" \notin out[c]
    /\ out' = [out EXCEPT ![c] = @ \cup {"read"}]
    /\ UNCHANGED <<state, flaps, head, judged, pushed, unblocks>>

\* a read with a verdict, and the accept: forward
Accept(c) ==
    /\ state[c] = "review" /\ "read" \in out[c]
    /\ state' = [state EXCEPT ![c] = "merging"]
    /\ out' = [out EXCEPT ![c] = {}]
    /\ UNCHANGED <<flaps, head, judged, pushed, unblocks>>

Land(c) ==
    /\ state[c] = "merging"
    /\ state' = [state EXCEPT ![c] = "landed"]
    /\ UNCHANGED <<out, flaps, head, judged, pushed, unblocks>>

\* the returns without progress, each one flap:
\* a work card back with no verdict (not taken, an environment exit, taken off)
WorkBack(c) ==
    /\ state[c] = "working" /\ "work" \in out[c]
    /\ state' = [state EXCEPT ![c] = "ready"]
    /\ out' = [out EXCEPT ![c] = {}]
    /\ Flap(c)
    /\ UNCHANGED <<head, judged, pushed, unblocks>>

\* a read card back with no verdict
ReadBack(c) ==
    /\ state[c] = "review" /\ "read" \in out[c]
    /\ out' = [out EXCEPT ![c] = @ \ {"read"}]
    /\ Flap(c)
    /\ UNCHANGED <<state, head, judged, pushed, unblocks>>

\* a rework or recut: review back to ready, its reads with it
Rework(c) ==
    /\ state[c] = "review"
    /\ state' = [state EXCEPT ![c] = "ready"]
    /\ out' = [out EXCEPT ![c] = {}]
    /\ Flap(c)
    /\ UNCHANGED <<head, judged, pushed, unblocks>>

\* a merge refused: merging back to review
MergeRefused(c) ==
    /\ state[c] = "merging"
    /\ state' = [state EXCEPT ![c] = "review"]
    /\ Flap(c)
    /\ UNCHANGED <<out, head, judged, pushed, unblocks>>

Bounce(c) == WorkBack(c) \/ ReadBack(c) \/ Rework(c) \/ MergeRefused(c)

\* the tick's entry (TickBlock): more than Limit flaps in the window, from ready,
\* working or review (a merging card is blocked once it is back in review); in the
\* same step every deal and read withdrawn, one judgment and one push
Block(c) ==
    /\ Broken # "noentry"
    /\ state[c] \in Blockable
    /\ flaps[c] > Limit
    /\ state' = [state EXCEPT ![c] = "blocked"]
    /\ out' = IF Broken = "nowithdraw" THEN out ELSE [out EXCEPT ![c] = {}]
    /\ judged' = [judged EXCEPT ![c] = @ + 1]
    /\ pushed' = [pushed EXCEPT ![c] = @ + 1]
    /\ UNCHANGED <<flaps, head, unblocks>>

\* the coordinator's unblock: to waiting, the counts cleared, the judgment closed
Unblock(c) ==
    /\ state[c] = "blocked"
    /\ unblocks < MaxUnblocks
    /\ state' = [state EXCEPT ![c] = "waiting"]
    /\ flaps' = [flaps EXCEPT ![c] = 0]
    /\ judged' = [judged EXCEPT ![c] = 0]
    /\ pushed' = [pushed EXCEPT ![c] = 0]
    /\ unblocks' = unblocks + 1
    /\ UNCHANGED <<out, head>>

\* the tick's resolve: waiting -> ready (no needs in the model)
Resolve(c) ==
    /\ state[c] = "waiting"
    /\ state' = [state EXCEPT ![c] = "ready"]
    /\ UNCHANGED <<out, flaps, head, judged, pushed, unblocks>>

\* the coordinator's drop, from any open state and from blocked
Drop(c) ==
    /\ state[c] \in Open \cup {"blocked"}
    /\ state' = [state EXCEPT ![c] = "dropped"]
    /\ out' = [out EXCEPT ![c] = {}]
    /\ judged' = [judged EXCEPT ![c] = 0]
    /\ UNCHANGED <<flaps, head, pushed, unblocks>>

Progress(c) == Finish(c) \/ Accept(c) \/ Land(c)

Next == \E c \in Cards :
    Deal(c) \/ Finish(c) \/ Ask(c) \/ Accept(c) \/ Land(c) \/ Bounce(c)
    \/ Block(c) \/ Unblock(c) \/ Resolve(c) \/ Drop(c)

Spec == Init /\ [][Next]_vars /\ \A c \in Cards : WF_vars(Block(c))

\* A blocked card has no deal, read or work out.
BlockedHasNothingOut == \A c \in Cards : state[c] = "blocked" => out[c] = {}

\* One judgment and one push per entry.
OneJudgmentPerEntry == \A c \in Cards : state[c] = "blocked" => judged[c] = 1 /\ pushed[c] = 1

\* Only unblock (to waiting) or drop leaves blocked: a blocked card is never
\* dealt, merged or landed.
OnlyUnblockOrDropLeaves ==
    [][\A c \in Cards : state[c] = "blocked" => state'[c] \in {"blocked", "waiting", "dropped"}]_vars

\* Blocking (and unblocking) loses no head.
NoHeadLost == [][\A c \in Cards : head[c] => head'[c]]_vars

\* Under weak fairness of the tick, no card bounces forever: it stops bouncing
\* (it progresses to landed, or ends blocked or dropped), or it keeps progressing.
NoInfiniteBounce ==
    \A c \in Cards : <>[][~Bounce(c)]_vars \/ []<><<Progress(c)>>_vars
=============================================================================
