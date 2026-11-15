------------------------------ MODULE MergeNag ------------------------------
(***************************************************************************)
(* The merge-queue nag (layer 3, modules/steps/nag.go; Glenn 2026-11-15:  *)
(* "You should be getting pested every 1s when cards are in the merge     *)
(* queue longer than 5 minutes"). Each card is not merging, merging       *)
(* young (up to timeouts merging_nag) or merging old (past it). Each tick *)
(* (one a second: merging_nag_every rides it) plans the nag: one line for *)
(* the whole queue to the coordinator while any card is old and the       *)
(* coordinator is hooked on the bus, none otherwise. pushed is the lines  *)
(* the last tick planned.                                                  *)
(*                                                                         *)
(* Bound to the code by modules/steps/nagtrace_test.go                     *)
(* (TestMergeNagTraceWalk: mergingNag on a View the walk moves, its clock *)
(* an argument), checked by tla/check-trace.sh MergeNag.                   *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANT Cards

VARIABLES
  st,      \* each card: "none" (not merging), "young" or "old" (merging past the threshold)
  coord,   \* a coordinator is hooked and on the bus
  pushed,  \* the nag lines the last tick planned
  ticked   \* the last step was a tick

vars == <<st, coord, pushed, ticked>>

Old == {c \in Cards : st[c] = "old"}
Past == coord /\ Old # {}

Init ==
  /\ st = [c \in Cards |-> "none"]
  /\ coord \in BOOLEAN
  /\ pushed = 0
  /\ ticked = FALSE

\* A card enters merging (its producer passed its reads).
Enter(c) ==
  /\ st[c] = "none"
  /\ st' = [st EXCEPT ![c] = "young"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<coord, pushed>>

\* Time passes: some young cards pass the threshold together.
Ages(S) ==
  /\ S # {}
  /\ \A c \in S : st[c] = "young"
  /\ st' = [c \in Cards |-> IF c \in S THEN "old" ELSE st[c]]
  /\ ticked' = FALSE
  /\ UNCHANGED <<coord, pushed>>

\* A card leaves merging: landed, sent to fix, or withdrawn.
Leave(c) ==
  /\ st[c] # "none"
  /\ st' = [st EXCEPT ![c] = "none"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<coord, pushed>>

\* The coordinator hooks on or drops off the bus.
Hook ==
  /\ coord' = ~coord
  /\ ticked' = FALSE
  /\ UNCHANGED <<st, pushed>>

\* The tick plans the nag: one line for the whole queue while any card is
\* past the threshold and the coordinator is there to read it.
Tick ==
  /\ pushed' = IF Past THEN 1 ELSE 0
  /\ ticked' = TRUE
  /\ UNCHANGED <<st, coord>>

Next ==
  \/ \E c \in Cards : Enter(c) \/ Leave(c)
  \/ \E S \in SUBSET Cards : Ages(S)
  \/ Hook
  \/ Tick

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

-----------------------------------------------------------------------------
TypeOK ==
  /\ st \in [Cards -> {"none", "young", "old"}]
  /\ coord \in BOOLEAN
  /\ pushed \in Nat
  /\ ticked \in BOOLEAN

\* One line for the whole queue, never one per card.
OneLinePerTick == pushed <= 1

\* Past the threshold, every tick pushes the coordinator.
PushedWhilePast == ticked /\ Past => pushed = 1

\* The nag stops the tick the queue clears.
StopsWhenClear == ticked /\ Old = {} => pushed = 0

\* Nothing is queued on the bus for a coordinator who is not on it.
NoneOffBus == ticked /\ ~coord => pushed = 0

\* Past the threshold, the coordinator is pushed until the queue clears
\* (or the coordinator leaves the bus).
PushedUntilClear == Past ~> (~Past \/ (ticked /\ pushed = 1))

\* Reachability: expected VIOLATED.
NeverNags == pushed = 0
=============================================================================
