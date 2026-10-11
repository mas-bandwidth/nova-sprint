--------------------------- MODULE Holds -----------------------------
(***************************************************************************)
(* nova-sprint's stream hold and stop, resume and reopen                  *)
(* (internal/sprint/hold.go holdStream, steps_merge.go mergeStep/Resume, *)
(* steps_tick.go TickResume, steps_work.go dealPlan/reopen,              *)
(* stream_reopen.go reopenAdd; docs/SPEC-SPRINT.md section 11, hold).    *)
(*                                                                       *)
(* A stream's control card has held and held_reason, and a state among   *)
(* waiting, merging, stopped, landed and working; a stop carries a cause *)
(* cross, conflict, red or rejected. A cross stop is a card queued in    *)
(* the merge table that needs another stream's card (o, here) landed     *)
(* first; the stuck card keeps need_card o. holdStream holds the stream  *)
(* (withdrawing its ready cards, and its working ones with --return) and *)
(* the deal refuses a held stream. A stopped stream still deals its      *)
(* ready cards. Resume moves a stopped stream again: a cross stop waits  *)
(* until o has landed, a red stop needs --did; the tick's TickResume     *)
(* does the cross resume on its own. An add past a landed stop reopens   *)
(* the stream to working (stream_reopen.go). A conflict fact that is a   *)
(* refusal of the card's own head stops nothing (Refusal); the lander's  *)
(* own failure stops the stream (StopConflict).                          *)
(*                                                                       *)
(* The no-stall rule (internal/sprint/held.go Unheld): a held card or a  *)
(* held stream is the coordinator's hold, holder (c) (held.go judgment); *)
(* a stopped stream with no open judgment is a stall (Unheld). Here a    *)
(* stop raises an open judgment (open) on the stream, which the          *)
(* coordinator's answer (SayDid, then Resume) or the tick's cross resume *)
(* closes; a stopped stream the coordinator holds (held) is not a stall  *)
(* even with no judgment. NoSilentStall says every stopped stream has an *)
(* open judgment or is the coordinator's hold. Under fairness of the     *)
(* coordinator's answers and the machine's steps, Progress says a card   *)
(* the coordinator does not hold eventually lands, is held, or is        *)
(* stopped with an open judgment (never a silent stall).                 *)
(*                                                                       *)
(* Streams: s (held, stopped, resumed, reopened) and t (the card o the   *)
(* cross need names). Card a is s's work card (ready, working, merging,  *)
(* landed, or withdrawn by a hold); its merge column is none, queued or  *)
(* stuck (needing o).                                                    *)
(*                                                                       *)
(* Broken (the reversed witness):                                        *)
(*  "resumeearly" - Resume drops the needed-card check: a cross stop     *)
(*                  resumes while o has not landed.                      *)
(*                  CrossResumesAfterNeed fails.                         *)
(*  "silentstop"  - a stop raises no judgment: a stopped stream has no   *)
(*                  open judgment and is not held. NoSilentStall fails.  *)
(*                                                                       *)
(* The held card, the sentinel and its release (docs/SPEC-SPRINT.md      *)
(* section 16, "Sentinel cards"; internal/sprint steps_sentinel.go,      *)
(* sentinel.go and held.go): a second machine in the same module, whose  *)
(* card variables carry the c prefix. One stream whose line is [a, S, b] *)
(* (S a plain or a cut sentinel) and one held card h; the actors are the *)
(* coordinator and a worker. Each card has a column (waiting, ready,     *)
(* working, landed; review and merging fold into "working, taken"), a    *)
(* held flag (FieldHeld, steps_sentinel.go:22), the sentinel's needs     *)
(* (isCut, sentinel.go:34), waived waits, the reached judgment, and who  *)
(* released it. Resolve (steps_work.go:895-907, resolveAfter:919-935)    *)
(* skips a held card and marks a sentinel reached instead of ready; a    *)
(* sentinel is reachable when it has something before it or nothing is   *)
(* in flight (Reachable, steps_sentinel.go:239). Release is the          *)
(* coordinator's (steps_sentinel.go:299-404): a sentinel's waits must    *)
(* each be under way (notUnderWay:409-438), the unmet waits are waived   *)
(* (:369-382), and a held card has its hold cleared (releaseHeld:        *)
(* 442-450). CutRelease is the cut sentinel's self-release               *)
(* (sentinel.go:51-103): it names needs, is reached, waits on nothing    *)
(* and its stream is not held; it never waives. SentinelSet replaces a   *)
(* waiting sentinel's needs (steps_sentinel.go:475-570): here plain ->   *)
(* cut (names a). The coordinator's stream hold (Hold) is read and       *)
(* respected by the cut release (sentinel.go:60).                        *)
(*                                                                       *)
(* The stream and the card machine run side by side: their variables are *)
(* disjoint, so a step of one leaves the other's state unchanged, and    *)
(* the module's Next is their disjunction. CardBroken (the reversed      *)
(* witness):                                                            *)
(*  "resolveheld"    - Resolve drops the held check (steps_work.go:922) *)
(*                     and moves a held card to ready.                   *)
(*                     HeldMovesOnlyByRelease fails.                     *)
(*  "workerreleases" - Release drops the coordinator check               *)
(*                     (steps_sentinel.go:318) and a worker releases.    *)
(*                     OnlyCoordinatorReleases fails.                    *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Broken, CardBroken

ASSUME CardBroken \subseteq {"resolveheld", "workerreleases"}

VARIABLES held, state, cause, aWork, aMerge, oLanded, did, sentinel, open,
          cardCol, cardHeld, cardNeeds, cardWaived, cardReached, cardTaken,
          cardReleasedBy, cardStreamHeld

streamVars == <<held, state, cause, aWork, aMerge, oLanded, did, sentinel, open>>

States  == {"waiting", "merging", "stopped", "landed", "working"}
Causes  == {"", "cross", "red", "conflict", "rejected"}
AWork   == {"ready", "working", "merging", "landed", "withdrawn"}
AMerge  == {"none", "queued", "stuck"}

StreamTypeOK ==
    /\ held \in BOOLEAN
    /\ state \in States
    /\ cause \in Causes
    /\ aWork \in AWork
    /\ aMerge \in AMerge
    /\ oLanded \in BOOLEAN
    /\ did \in BOOLEAN
    /\ sentinel \in BOOLEAN
    /\ open \in BOOLEAN

StreamInit ==
    /\ held = FALSE
    /\ state = "merging"
    /\ cause = ""
    /\ aWork = "ready"
    /\ aMerge = "none"
    /\ oLanded = FALSE
    /\ did = FALSE
    /\ sentinel = FALSE
    /\ open = FALSE

\* the deal deals stream s's ready card a to a member; a held stream deals
\* nothing (steps_work.go dealPlan: StreamHeld refuses it)
Deal ==
    /\ held = FALSE
    /\ aWork = "ready"
    /\ aWork' = "working"
    /\ UNCHANGED <<held, state, cause, aMerge, oLanded, did, sentinel, open>>

\* a finishes its work and enters the merge queue
Promote ==
    /\ aWork = "working"
    /\ aWork' = "merging"
    /\ aMerge = "none"
    /\ aMerge' = "queued"
    /\ UNCHANGED <<held, state, cause, oLanded, did, sentinel, open>>

\* a withdrawn card is dealt again at a new generation (ready), so a hold
\* has something to keep refusing
Redeal ==
    /\ aWork = "withdrawn"
    /\ aWork' = "ready"
    /\ UNCHANGED <<held, state, cause, aMerge, oLanded, did, sentinel, open>>

\* holdStream: hold stream s, withdrawing its ready cards (never begun)
Hold ==
    /\ held = FALSE
    /\ held' = TRUE
    /\ IF aWork = "ready" THEN aWork' = "withdrawn" ELSE aWork' = aWork
    /\ UNCHANGED <<state, cause, aMerge, oLanded, did, sentinel, open>>

\* holdStream with --return also withdraws working cards
HoldReturn ==
    /\ held = FALSE
    /\ held' = TRUE
    /\ IF aWork \in {"ready", "working"} THEN aWork' = "withdrawn" ELSE aWork' = aWork
    /\ UNCHANGED <<state, cause, aMerge, oLanded, did, sentinel, open>>

Unhold ==
    /\ held = TRUE
    /\ held' = FALSE
    /\ UNCHANGED <<state, cause, aWork, aMerge, oLanded, did, sentinel, open>>

\* a stop raises an open judgment on the stream (the coordinator is asked to
\* resume); the reversed witness "silentstop" stops with no judgment.
StopRaises == (Broken # "silentstop")

\* a cross stop: card a queued needs card o (stream t) landed first; the
\* guard is crossRefusal (o placed, in another stream, not landed)
StopCross ==
    /\ state \notin {"stopped", "landed"}
    /\ aWork = "merging"
    /\ aMerge = "queued"
    /\ oLanded = FALSE
    /\ state' = "stopped"
    /\ cause' = "cross"
    /\ aMerge' = "stuck"
    /\ open' = StopRaises
    /\ UNCHANGED <<held, aWork, oLanded, did, sentinel>>

\* a red stop (the branch is red)
StopRed ==
    /\ state \notin {"stopped", "landed"}
    /\ aMerge = "queued"
    /\ state' = "stopped"
    /\ cause' = "red"
    /\ open' = StopRaises
    /\ UNCHANGED <<held, aWork, aMerge, oLanded, did, sentinel>>

\* a conflict stop (the lander's own failure; not a refusal of the card's head)
StopConflict ==
    /\ state \notin {"stopped", "landed"}
    /\ aMerge = "queued"
    /\ state' = "stopped"
    /\ cause' = "conflict"
    /\ aMerge' = "stuck"
    /\ open' = StopRaises
    /\ UNCHANGED <<held, aWork, oLanded, did, sentinel>>

\* a rejected stop (the merge queue rejected the batch)
StopRejected ==
    /\ state \notin {"stopped", "landed"}
    /\ aMerge = "queued"
    /\ state' = "stopped"
    /\ cause' = "rejected"
    /\ open' = StopRaises
    /\ UNCHANGED <<held, aWork, aMerge, oLanded, did, sentinel>>

\* a conflict fact that is a refusal of the card's own head stops nothing:
\* the card is reworked at the tip (or returned for the widen rule) and the
\* stream goes on (steps_merge.go RefusalWay, landRefused)
Refusal ==
    /\ state \notin {"stopped", "landed"}
    /\ aWork = "merging"
    /\ aMerge = "queued"
    /\ aWork' = "ready"
    /\ aMerge' = "none"
    /\ UNCHANGED <<held, state, cause, oLanded, did, sentinel, open>>

\* card o (stream t) lands; the cross need waits on this
LandO ==
    /\ oLanded = FALSE
    /\ oLanded' = TRUE
    /\ UNCHANGED <<held, state, cause, aWork, aMerge, did, sentinel, open>>

\* card a lands: the stream s is done
LandA ==
    /\ state \notin {"stopped", "landed"}
    /\ aWork = "merging"
    /\ aMerge = "queued"
    /\ aWork' = "landed"
    /\ aMerge' = "none"
    /\ state' = "landed"
    /\ UNCHANGED <<held, cause, oLanded, did, sentinel, open>>

\* a stop sentinel of stream s lands (behind a closed stream)
LandSentinel ==
    /\ sentinel = FALSE
    /\ sentinel' = TRUE
    /\ UNCHANGED <<held, state, cause, aWork, aMerge, oLanded, did, open>>

\* the coordinator says what was done (a red stop needs it before resume)
SayDid ==
    /\ state = "stopped"
    /\ cause = "red"
    /\ did = FALSE
    /\ did' = TRUE
    /\ UNCHANGED <<held, state, cause, aWork, aMerge, oLanded, sentinel, open>>

\* Resume: a cross stop waits until the needed card has landed; a red stop
\* needs --did; the other causes are resolved by the coordinator. The stuck
\* cards go back to queued and the stream merges again. The answer closes the
\* open judgment.
ResumeOk ==
    \/ (cause = "cross" /\ oLanded)
    \/ (cause = "red" /\ did)
    \/ (cause \in {"conflict", "rejected"})

Resume ==
    /\ state = "stopped"
    /\ (ResumeOk \/ (cause = "cross" /\ Broken = "resumeearly"))
    /\ state' = "merging"
    /\ cause' = ""
    /\ aMerge' = IF aMerge = "stuck" THEN "queued" ELSE aMerge
    /\ did' = FALSE
    /\ open' = FALSE
    /\ UNCHANGED <<held, aWork, oLanded, sentinel>>

\* T7. TickResume: the tick resumes a cross stop on its own once the needed
\* card has landed (steps_tick.go); it closes the open judgment.
TickResume ==
    /\ state = "stopped"
    /\ cause = "cross"
    /\ oLanded = TRUE
    /\ state' = "merging"
    /\ cause' = ""
    /\ aMerge' = IF aMerge = "stuck" THEN "queued" ELSE aMerge
    /\ open' = FALSE
    /\ UNCHANGED <<held, aWork, oLanded, did, sentinel>>

\* an add past a landed stop reopens the stream to working (steps_work.go,
\* stream_reopen.go reopenAdd)
ReopenAdd ==
    /\ state = "landed"
    /\ sentinel = TRUE
    /\ state' = "working"
    /\ aWork' = "ready"
    /\ aMerge' = "none"
    /\ UNCHANGED <<held, cause, oLanded, did, sentinel, open>>

StreamNext ==
    \/ Deal \/ Promote \/ Redeal
    \/ Hold \/ HoldReturn \/ Unhold
    \/ StopCross \/ StopRed \/ StopConflict \/ StopRejected \/ Refusal
    \/ LandO \/ LandA \/ LandSentinel \/ SayDid
    \/ Resume \/ TickResume \/ ReopenAdd

(* A held stream deals nothing: its ready cards are never dealt to working. *)
HeldStreamDealsNothing ==
    [][(held /\ aWork = "ready") => aWork' # "working"]_streamVars

(* A cross stop waits until the needed card has landed: while the stream is  *)
(* stopped for a cross and o is not landed, the stream stays stopped.        *)
CrossResumesAfterNeed ==
    [][(state = "stopped" /\ cause = "cross" /\ ~oLanded) => state' = "stopped"]_streamVars

(* A refusal of the card's own head never stops the stream. *)
OwnRefusalNeverStops ==
    [][Refusal => state' # "stopped"]_streamVars

(* NoSilentStall: a stopped stream with no open judgment is a stall, so every *)
(* stopped stream has an open judgment or is the coordinator's hold (held.go  *)
(* Unheld, the stopped-stream check; holder (c)).                            *)
NoSilentStall ==
    (state = "stopped") => (open \/ held)

(* Progress: a card the coordinator does not hold eventually lands, is held,  *)
(* or is stopped with an open judgment (which NoSilentStall says the          *)
(* coordinator's hold or an open judgment covers): under fairness of the      *)
(* coordinator's answers and the machine's steps a card cannot stay ready,    *)
(* working, merging or withdrawn without a hold.                              *)
Progress ==
    (~held) ~> (aWork = "landed" \/ held \/ state = "stopped")

----------------------------------------------------------------------------
(* The held card, the sentinel and its release (the c-prefixed card machine) *)

Cards == {"a", "S", "b", "h"}
SentinelCard == "S"
HeldCard == "h"
Actors == {"coordinator", "worker"}

Cols == {"waiting", "ready", "working", "landed"}
Releasers == {"none", "coordinator", "machine", "worker"}

\* the line's order: a before the sentinel S before b; h is off the line
Before(x, c) ==
  \/ x = "a" /\ c \in {"S", "b"}
  \/ x = "S" /\ c = "b"

cardVars == <<cardCol, cardHeld, cardNeeds, cardWaived, cardReached, cardTaken,
              cardReleasedBy, cardStreamHeld>>

CardTypeOK ==
  /\ cardCol \in [Cards -> Cols]
  /\ cardHeld \in [Cards -> BOOLEAN]
  /\ cardNeeds \in [Cards -> SUBSET Cards]
  /\ cardWaived \in [Cards -> SUBSET Cards]
  /\ cardReached \in [Cards -> BOOLEAN]
  /\ cardTaken \in [Cards -> BOOLEAN]
  /\ cardReleasedBy \in [Cards -> Releasers]
  /\ cardStreamHeld \in BOOLEAN

CardInit ==
  /\ cardCol = [c \in Cards |-> "waiting"]
  /\ cardHeld = [c \in Cards |-> c = "h"]
  /\ cardNeeds \in { [c \in Cards |-> {}],
                     [c \in Cards |-> IF c = "S" THEN {"a"} ELSE {}] }
  /\ cardWaived = [c \in Cards |-> {}]
  /\ cardReached = [c \in Cards |-> FALSE]
  /\ cardTaken = [c \in Cards |-> FALSE]
  /\ cardReleasedBy = [c \in Cards |-> "none"]
  /\ cardStreamHeld = FALSE

\* a sentinel names its needs: it is a cut the machine can read (sentinel.go:34)
IsCut == cardNeeds[SentinelCard] /= {}

\* the needs a card names that have not landed and were not waived
\* (steps_work.go:721)
NamedWaits(c) == {n \in cardNeeds[c] : cardCol[n] /= "landed" /\ n \notin cardWaived[c]}

\* a card's place in line: the sentinel waits for the line before it, and
\* the card behind waits for the unlanded sentinel before it (position.go)
PositionWaits(c) ==
  IF c = "S" THEN {x \in Cards : Before(x, c) /\ cardCol[x] /= "landed"}
  ELSE IF c = "b" THEN IF cardCol["S"] /= "landed" THEN {"S"} ELSE {}
  ELSE {}

WaitsFor(c) == NamedWaits(c) \cup PositionWaits(c)

\* a wait is under way when it has landed, or is in flight with its work
\* taken (notUnderWay, steps_sentinel.go:409-438; review and merging fold
\* into "working, taken")
UnderWay(w) == cardCol[w] = "landed" \/ (cardCol[w] = "working" /\ cardTaken[w])

AllUnderWay(c) == \A w \in WaitsFor(c) : UnderWay(w)

\* a sentinel is reachable when it has something before it or nothing is in
\* flight (Reachable, steps_sentinel.go:239). Here S always has a before it,
\* so it is reachable throughout; the predicate is the code's reading.
InFlight  == {c \in Cards : cardCol[c] \in {"ready", "working"}}
HasBefore(c) == cardNeeds[c] /= {} \/ \E x \in Cards : Before(x, c)
Reachable == HasBefore("S") \/ InFlight = {}

\* Resolve (steps_work.go:895-907, resolveAfter:919-935): a waiting primary
\* whose waits have all landed moves to ready; a sentinel is marked reached
\* instead; a held card is skipped (the "resolveheld" witness drops this).
Resolve ==
  LET skip(c) == (cardHeld[c] /\ ~("resolveheld" \in CardBroken)) \/ (WaitsFor(c) /= {})
      moved == [c \in Cards |-> IF cardCol[c] = "waiting" /\ ~skip(c) /\ c \in {"a", "b", "h"}
                                  THEN "ready" ELSE cardCol[c]]
      marked == IF cardCol["S"] = "waiting" /\ ~skip("S") /\ ~cardReached["S"] /\ Reachable
                THEN TRUE ELSE cardReached["S"]
  IN /\ cardCol' = moved
     /\ cardReached' = [cardReached EXCEPT !["S"] = marked]
     /\ UNCHANGED <<cardHeld, cardNeeds, cardWaived, cardTaken, cardReleasedBy, cardStreamHeld>>

\* the worker deals a's work (ready -> working, not taken)
DealA ==
  /\ cardCol["a"] = "ready"
  /\ cardCol' = [cardCol EXCEPT !["a"] = "working"]
  /\ cardTaken' = [cardTaken EXCEPT !["a"] = FALSE]
  /\ UNCHANGED <<cardHeld, cardNeeds, cardWaived, cardReached, cardReleasedBy, cardStreamHeld>>

\* the worker takes a's work card (working, taken)
TakeA ==
  /\ cardCol["a"] = "working" /\ ~cardTaken["a"]
  /\ cardTaken' = [cardTaken EXCEPT !["a"] = TRUE]
  /\ UNCHANGED <<cardCol, cardHeld, cardNeeds, cardWaived, cardReached, cardReleasedBy, cardStreamHeld>>

\* the worker lands a's work (working, taken -> landed)
LandCardA ==
  /\ cardCol["a"] = "working" /\ cardTaken["a"]
  /\ cardCol' = [cardCol EXCEPT !["a"] = "landed"]
  /\ UNCHANGED <<cardHeld, cardNeeds, cardWaived, cardReached, cardTaken, cardReleasedBy, cardStreamHeld>>

\* the coordinator lands a sentinel whose waits are each under way, waiving
\* what is in flight (steps_sentinel.go:344-389); the wave behind it moves to
\* ready in the same step (resolveAfter)
ReleaseSentinel(actor) ==
  /\ cardCol["S"] = "waiting"
  /\ AllUnderWay("S")
  /\ cardCol' = [cardCol EXCEPT !["S"] = "landed",
                           !["b"] = IF cardCol["b"] = "waiting" THEN "ready" ELSE cardCol["b"]]
  /\ cardWaived' = [cardWaived EXCEPT !["S"] = @ \cup WaitsFor("S")]
  /\ cardReleasedBy' = [cardReleasedBy EXCEPT !["S"] = actor]
  /\ UNCHANGED <<cardHeld, cardNeeds, cardReached, cardTaken, cardStreamHeld>>

\* the coordinator clears a held card's hold, and it is ready when it waits
\* for nothing else (releaseHeld, steps_sentinel.go:442-450)
ReleaseHeld(actor) ==
  /\ cardCol["h"] = "waiting" /\ cardHeld["h"]
  /\ cardHeld' = [cardHeld EXCEPT !["h"] = FALSE]
  /\ cardCol' = [cardCol EXCEPT !["h"] = IF WaitsFor("h") = {} THEN "ready" ELSE "waiting"]
  /\ cardReleasedBy' = [cardReleasedBy EXCEPT !["h"] = actor]
  /\ UNCHANGED <<cardNeeds, cardWaived, cardReached, cardTaken, cardStreamHeld>>

\* Release(actor) is the coordinator's release (steps_sentinel.go:299-404):
\* coordinator only, or a worker with the "workerreleases" witness.
Release(actor) ==
  /\ actor = "coordinator" \/ "workerreleases" \in CardBroken
  /\ \/ ReleaseSentinel(actor)
     \/ ReleaseHeld(actor)

\* the cut sentinel's self-release (sentinel.go:51-103): it names needs, is
\* reached, waits on nothing, and its stream is not held; it never waives.
CutRelease ==
  /\ IsCut
  /\ cardCol["S"] = "waiting"
  /\ ~cardHeld["S"]
  /\ cardReached["S"]
  /\ WaitsFor("S") = {}
  /\ ~cardStreamHeld
  /\ cardCol' = [cardCol EXCEPT !["S"] = "landed",
                           !["b"] = IF cardCol["b"] = "waiting" THEN "ready" ELSE cardCol["b"]]
  /\ cardReleasedBy' = [cardReleasedBy EXCEPT !["S"] = "machine"]
  /\ UNCHANGED <<cardHeld, cardNeeds, cardWaived, cardReached, cardTaken, cardStreamHeld>>

\* the coordinator replaces a waiting sentinel's needs (steps_sentinel.go:
\* 475-570). Here the only move admitted is plain -> cut (naming a); emptying
\* is refused and setting the needs it has is refused, so one line is the cut.
SentinelSet ==
  /\ cardCol["S"] = "waiting"
  /\ cardNeeds["S"] = {}
  /\ cardNeeds' = [cardNeeds EXCEPT !["S"] = {"a"}]
  /\ UNCHANGED <<cardCol, cardHeld, cardWaived, cardReached, cardTaken, cardReleasedBy, cardStreamHeld>>

\* the coordinator holds or releases the stream's hold (hold.go holdStream);
\* the cut release respects it (sentinel.go:60)
StreamHold ==
  /\ \/ ~cardStreamHeld /\ cardStreamHeld' = TRUE
     \/ cardStreamHeld /\ cardStreamHeld' = FALSE
  /\ UNCHANGED <<cardCol, cardHeld, cardNeeds, cardWaived, cardReached, cardTaken, cardReleasedBy>>

CardNext ==
  \/ Resolve
  \/ \E actor \in Actors : Release(actor)
  \/ CutRelease
  \/ SentinelSet
  \/ StreamHold
  \/ DealA \/ TakeA \/ LandCardA

\* a held primary stays waiting until the coordinator's release clears its
\* hold (steps_work.go:895, held.go:53-64)
HeldMovesOnlyByRelease == \A c \in Cards : cardHeld[c] => cardCol[c] = "waiting"

\* no worker releases: the release verb is the coordinator's alone
\* (steps_sentinel.go:316-320); the machine's own cut release is "machine"
OnlyCoordinatorReleases == \A c \in Cards : cardReleasedBy[c] /= "worker"

\* the machine's self-release waives nothing (sentinel.go:48, 90)
CutNeverWaives == \A c \in Cards : cardReleasedBy[c] = "machine" => cardWaived[c] = {}

\* a sentinel lands only when each of its waits is under way: landed, or in
\* flight and waived (steps_sentinel.go:344, notUnderWay:409-438). Here the
\* sentinel's waits are its needs and the card a before it.
SentinelWaits == cardNeeds["S"] \cup {"a"}
SentinelLandsOnlyWhenWaitsUnderWay ==
  cardCol["S"] = "landed" =>
    \A w \in SentinelWaits : cardCol[w] = "landed" \/ w \in cardWaived["S"]

----------------------------------------------------------------------------
(* The combined machine: the stream and the card machine run side by side.   *)

vars == <<held, state, cause, aWork, aMerge, oLanded, did, sentinel, open,
          cardCol, cardHeld, cardNeeds, cardWaived, cardReached, cardTaken,
          cardReleasedBy, cardStreamHeld>>

TypeOK == StreamTypeOK /\ CardTypeOK

Init == StreamInit /\ CardInit

Next ==
  \/ (StreamNext /\ UNCHANGED cardVars)
  \/ (CardNext /\ UNCHANGED streamVars)

Spec == Init /\ [][Next]_vars

\* Fairness of the coordinator's answers and the machine's steps: the deal,
\* promote and redeal are the tick's steps (weak fairness; a held stream may
\* keep them waiting), the landing commits whenever it can (strong fairness:
\* a stop can always preempt it), card o lands (resolving a cross stop), and
\* the coordinator answers a stop (says what was done, resumes; the tick's
\* cross resume). Under this a card the coordinator does not hold eventually
\* lands, is held, or is stopped with an open judgment.
FairSpec ==
    /\ Spec
    /\ WF_streamVars(Deal) /\ WF_streamVars(Promote) /\ WF_streamVars(Redeal)
    /\ SF_streamVars(LandA)
    /\ WF_streamVars(LandO) /\ WF_streamVars(SayDid) /\ WF_streamVars(Resume) /\ WF_streamVars(TickResume)
=============================================================================
