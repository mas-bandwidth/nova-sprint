------------------------------- MODULE Holds -------------------------------
(***************************************************************************)
(* A held card, a sentinel and its release (docs/SPEC-SPRINT.md section 16, *)
(* "Sentinel cards"; internal/sprint steps_sentinel.go, sentinel.go and     *)
(* held.go). Hold, sentinel and wave are one wait read by WaitOf            *)
(* (held.go:53-64); this module is the machine the code runs, not CardISA's *)
(* single Wait abstraction over that one wait.                              *)
(*                                                                          *)
(* The state: one stream whose line is [a, S, b] (S a plain or a cut        *)
(* sentinel) and one held card h; the actors are the coordinator and a      *)
(* worker. Each card has a column (waiting, ready, working, landed; review  *)
(* and merging are folded into "working, taken": a work card in flight past *)
(* the stop), a held flag (FieldHeld, steps_sentinel.go:22), the sentinel's *)
(* needs (isCut, sentinel.go:34: a plain sentinel names none, a cut names   *)
(* its needs) and waived waits, the reached judgment, and who released it.  *)
(*                                                                          *)
(* The design, Broken = {}:                                                 *)
(*   Resolve      the tick's resolve (steps_work.go:895-907, resolveAfter   *)
(*                :919-935): a waiting primary whose waits have all landed  *)
(*                moves to ready; a sentinel is marked reached instead, and *)
(*                a held card is skipped. A sentinel is reachable when it   *)
(*                has something before it or nothing is in flight           *)
(*                (Reachable, steps_sentinel.go:239); here S always has a   *)
(*                before it, so it is reachable throughout.                 *)
(*   Release      the coordinator's release (steps_sentinel.go:299-404):    *)
(*                coordinator only (:316-320); a sentinel's waits must each *)
(*                be under way (:344, notUnderWay:409-438) - landed, or in  *)
(*                flight with its work taken - and the unmet waits are      *)
(*                waived on it (:369-382); a held card has its hold cleared *)
(*                and is ready when it waits for nothing else               *)
(*                (releaseHeld:442-450).                                    *)
(*   CutRelease   the cut sentinel's self-release (sentinel.go:51-103): it  *)
(*                names needs, is reached, waits on nothing and its stream  *)
(*                is not held (:60); the machine releases it, and it never  *)
(*                waives.                                                   *)
(*   SentinelSet  the coordinator replaces a waiting sentinel's needs       *)
(*                (steps_sentinel.go:475-570): here plain -> cut (names a). *)
(*   Hold         the coordinator's stream hold (hold.go StreamHeld,        *)
(*                holdStream), which the cut release reads and respects.    *)
(*                                                                          *)
(* What must always hold (TLC):                                             *)
(*   HeldMovesOnlyByRelease          a held primary stays waiting until the *)
(*                                   coordinator's release clears its hold  *)
(*   OnlyCoordinatorReleases         no worker releases a sentinel or a held*)
(*                                   card                                   *)
(*   CutNeverWaives                  the machine's self-release waives      *)
(*                                   nothing                                *)
(*   SentinelLandsOnlyWhenWaitsUnderWay  a sentinel lands only when each of *)
(*                                   its waits is landed or in flight       *)
(*                                                                          *)
(* Reversed witnesses (Broken):                                             *)
(*   "resolveheld"      Resolve drops the held check (steps_work.go:922)   *)
(*                      and moves a held card to ready                     *)
(*                      (HeldMovesOnlyByRelease)                           *)
(*   "workerreleases"   Release drops the coordinator check                 *)
(*                      (steps_sentinel.go:318) and a worker releases       *)
(*                      (OnlyCoordinatorReleases)                          *)
(*                                                                          *)
(* Out of scope (the next card): stream holds, stops, resume and reopen.    *)
(* DirtyTick.tla's pump still auto-lands sentinels (DirtyTick.tla:445-461), *)
(* which is stale against Release here.                                     *)
(***************************************************************************)
EXTENDS FiniteSets

CONSTANTS
  Broken   \* a subset of {"resolveheld", "workerreleases"}

ASSUME Broken \subseteq {"resolveheld", "workerreleases"}

Cards     == {"a", "S", "b", "h"}
Sentinel  == "S"
HeldCard  == "h"
Actors    == {"coordinator", "worker"}

Cols      == {"waiting", "ready", "working", "landed"}
Releasers == {"none", "coordinator", "machine", "worker"}

\* the line's order: a before the sentinel S before b; h is off the line
Before(x, c) ==
  \/ x = "a" /\ c \in {"S", "b"}
  \/ x = "S" /\ c = "b"

VARIABLES
  col,        \* [Cards -> Cols]
  held,       \* [Cards -> BOOLEAN]       FieldHeld (steps_sentinel.go:22)
  needs,      \* [Cards -> SUBSET Cards]  the sentinel's needs (isCut)
  waived,     \* [Cards -> SUBSET Cards]  waits waived on a release
  reached,    \* [Cards -> BOOLEAN]       the sentinel's reached judgment
  taken,      \* [Cards -> BOOLEAN]       a working card's work card taken
  releasedBy, \* [Cards -> Releasers]
  streamHeld  \* BOOLEAN                  StreamHeld (hold.go:285)

vars == <<col, held, needs, waived, reached, taken, releasedBy, streamHeld>>

TypeOK ==
  /\ col \in [Cards -> Cols]
  /\ held \in [Cards -> BOOLEAN]
  /\ needs \in [Cards -> SUBSET Cards]
  /\ waived \in [Cards -> SUBSET Cards]
  /\ reached \in [Cards -> BOOLEAN]
  /\ taken \in [Cards -> BOOLEAN]
  /\ releasedBy \in [Cards -> Releasers]
  /\ streamHeld \in BOOLEAN

Init ==
  /\ col = [c \in Cards |-> "waiting"]
  /\ held = [c \in Cards |-> c = "h"]
  /\ needs \in { [c \in Cards |-> {}],
                 [c \in Cards |-> IF c = "S" THEN {"a"} ELSE {}] }
  /\ waived = [c \in Cards |-> {}]
  /\ reached = [c \in Cards |-> FALSE]
  /\ taken = [c \in Cards |-> FALSE]
  /\ releasedBy = [c \in Cards |-> "none"]
  /\ streamHeld = FALSE

----------------------------------------------------------------------------
(* The one wait read (WaitOf, held.go:53-64) *)

\* a sentinel names its needs: it is a cut the machine can read (sentinel.go:34)
IsCut == needs[Sentinel] /= {}

\* the needs a card names that have not landed and were not waived
\* (steps_work.go:721)
NamedWaits(c) == {n \in needs[c] : col[n] /= "landed" /\ n \notin waived[c]}

\* a card's place in line: the sentinel waits for the line before it, and
\* the card behind waits for the unlanded sentinel before it (position.go)
PositionWaits(c) ==
  IF c = "S" THEN {x \in Cards : Before(x, c) /\ col[x] /= "landed"}
  ELSE IF c = "b" THEN IF col["S"] /= "landed" THEN {"S"} ELSE {}
  ELSE {}

WaitsFor(c) == NamedWaits(c) \cup PositionWaits(c)

\* a wait is under way when it has landed, or is in flight with its work
\* taken (notUnderWay, steps_sentinel.go:409-438; review and merging fold
\* into "working, taken")
UnderWay(w) == col[w] = "landed" \/ (col[w] = "working" /\ taken[w])

AllUnderWay(c) == \A w \in WaitsFor(c) : UnderWay(w)

\* a sentinel is reachable when it has something before it or nothing is in
\* flight (Reachable, steps_sentinel.go:239). Here S always has a before it,
\* so it is reachable throughout; the predicate is the code's reading.
InFlight  == {c \in Cards : col[c] \in {"ready", "working"}}
HasBefore(c) == needs[c] /= {} \/ \E x \in Cards : Before(x, c)
Reachable == HasBefore("S") \/ InFlight = {}

----------------------------------------------------------------------------
(* The actions *)

\* Resolve (steps_work.go:895-907, resolveAfter:919-935): a waiting primary
\* whose waits have all landed moves to ready; a sentinel is marked reached
\* instead; a held card is skipped (the "resolveheld" witness drops this).
Resolve ==
  LET skip(c) == (held[c] /\ ~("resolveheld" \in Broken)) \/ (WaitsFor(c) /= {})
      moved == [c \in Cards |-> IF col[c] = "waiting" /\ ~skip(c) /\ c \in {"a", "b", "h"}
                                  THEN "ready" ELSE col[c]]
      marked == IF col["S"] = "waiting" /\ ~skip("S") /\ ~reached["S"] /\ Reachable
                THEN TRUE ELSE reached["S"]
  IN /\ col' = moved
     /\ reached' = [reached EXCEPT !["S"] = marked]
     /\ UNCHANGED <<held, needs, waived, taken, releasedBy, streamHeld>>

\* the worker deals a's work (ready -> working, not taken)
DealA ==
  /\ col["a"] = "ready"
  /\ col' = [col EXCEPT !["a"] = "working"]
  /\ taken' = [taken EXCEPT !["a"] = FALSE]
  /\ UNCHANGED <<held, needs, waived, reached, releasedBy, streamHeld>>

\* the worker takes a's work card (working, taken)
TakeA ==
  /\ col["a"] = "working" /\ ~taken["a"]
  /\ taken' = [taken EXCEPT !["a"] = TRUE]
  /\ UNCHANGED <<col, held, needs, waived, reached, releasedBy, streamHeld>>

\* the worker lands a's work (working, taken -> landed)
LandA ==
  /\ col["a"] = "working" /\ taken["a"]
  /\ col' = [col EXCEPT !["a"] = "landed"]
  /\ UNCHANGED <<held, needs, waived, reached, taken, releasedBy, streamHeld>>

\* the coordinator lands a sentinel whose waits are each under way, waiving
\* what is in flight (steps_sentinel.go:344-389); the wave behind it moves to
\* ready in the same step (resolveAfter)
ReleaseSentinel(actor) ==
  /\ col["S"] = "waiting"
  /\ AllUnderWay("S")
  /\ col' = [col EXCEPT !["S"] = "landed",
                       !["b"] = IF col["b"] = "waiting" THEN "ready" ELSE col["b"]]
  /\ waived' = [waived EXCEPT !["S"] = @ \cup WaitsFor("S")]
  /\ releasedBy' = [releasedBy EXCEPT !["S"] = actor]
  /\ UNCHANGED <<held, needs, reached, taken, streamHeld>>

\* the coordinator clears a held card's hold, and it is ready when it waits
\* for nothing else (releaseHeld, steps_sentinel.go:442-450)
ReleaseHeld(actor) ==
  /\ col["h"] = "waiting" /\ held["h"]
  /\ held' = [held EXCEPT !["h"] = FALSE]
  /\ col' = [col EXCEPT !["h"] = IF WaitsFor("h") = {} THEN "ready" ELSE "waiting"]
  /\ releasedBy' = [releasedBy EXCEPT !["h"] = actor]
  /\ UNCHANGED <<needs, waived, reached, taken, streamHeld>>

\* Release(actor) is the coordinator's release (steps_sentinel.go:299-404):
\* coordinator only, or a worker with the "workerreleases" witness.
Release(actor) ==
  /\ actor = "coordinator" \/ "workerreleases" \in Broken
  /\ \/ ReleaseSentinel(actor)
     \/ ReleaseHeld(actor)

\* the cut sentinel's self-release (sentinel.go:51-103): it names needs, is
\* reached, waits on nothing, and its stream is not held; it never waives.
CutRelease ==
  /\ IsCut
  /\ col["S"] = "waiting"
  /\ ~held["S"]
  /\ reached["S"]
  /\ WaitsFor("S") = {}
  /\ ~streamHeld
  /\ col' = [col EXCEPT !["S"] = "landed",
                       !["b"] = IF col["b"] = "waiting" THEN "ready" ELSE col["b"]]
  /\ releasedBy' = [releasedBy EXCEPT !["S"] = "machine"]
  /\ UNCHANGED <<held, needs, waived, reached, taken, streamHeld>>

\* the coordinator replaces a waiting sentinel's needs (steps_sentinel.go:
\* 475-570). Here the only move admitted is plain -> cut (naming a); emptying
\* is refused and setting the needs it has is refused, so one line is the cut.
SentinelSet ==
  /\ col["S"] = "waiting"
  /\ needs["S"] = {}
  /\ needs' = [needs EXCEPT !["S"] = {"a"}]
  /\ UNCHANGED <<col, held, waived, reached, taken, releasedBy, streamHeld>>

\* the coordinator holds or releases the stream's hold (hold.go holdStream);
\* the cut release respects it (sentinel.go:60)
Hold ==
  /\ \/ ~streamHeld /\ streamHeld' = TRUE
     \/ streamHeld /\ streamHeld' = FALSE
  /\ UNCHANGED <<col, held, needs, waived, reached, taken, releasedBy>>

Next ==
  \/ Resolve
  \/ \E actor \in Actors : Release(actor)
  \/ CutRelease
  \/ SentinelSet
  \/ Hold
  \/ DealA \/ TakeA \/ LandA

Spec == Init /\ [][Next]_vars

----------------------------------------------------------------------------
(* What must always hold *)

\* a held primary stays waiting until the coordinator's release clears its
\* hold (steps_sentinel.go:895, held.go:53-64)
HeldMovesOnlyByRelease == \A c \in Cards : held[c] => col[c] = "waiting"

\* no worker releases: the release verb is the coordinator's alone
\* (steps_sentinel.go:316-320); the machine's own cut release is "machine"
OnlyCoordinatorReleases == \A c \in Cards : releasedBy[c] /= "worker"

\* the machine's self-release waives nothing (sentinel.go:48, 90)
CutNeverWaives == \A c \in Cards : releasedBy[c] = "machine" => waived[c] = {}

\* a sentinel lands only when each of its waits is under way: landed, or in
\* flight and waived (steps_sentinel.go:344, notUnderWay:409-438). Here the
\* sentinel's waits are its needs and the card a before it.
SentinelWaits == needs["S"] \cup {"a"}
SentinelLandsOnlyWhenWaitsUnderWay ==
  col["S"] = "landed" =>
    \A w \in SentinelWaits : col[w] = "landed" \/ w \in waived["S"]

=============================================================================
