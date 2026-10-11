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
(* Streams: s (held, stopped, resumed, reopened) and t (the card o the   *)
(* cross need names). Card a is s's work card (ready, working, merging,  *)
(* landed, or withdrawn by a hold); its merge column is none, queued or  *)
(* stuck (needing o).                                                    *)
(*                                                                       *)
(* Broken (the reversed witness):                                        *)
(*  "resumeearly" - Resume drops the needed-card check: a cross stop     *)
(*                  resumes while o has not landed.                      *)
(*                  CrossResumesAfterNeed fails.                         *)
(***************************************************************************)
EXTENDS Naturals

CONSTANT Broken

VARIABLES held, state, cause, aWork, aMerge, oLanded, did, sentinel

vars == <<held, state, cause, aWork, aMerge, oLanded, did, sentinel>>

States  == {"waiting", "merging", "stopped", "landed", "working"}
Causes  == {"", "cross", "red", "conflict", "rejected"}
AWork   == {"ready", "working", "merging", "landed", "withdrawn"}
AMerge  == {"none", "queued", "stuck"}

TypeOK ==
    /\ held \in BOOLEAN
    /\ state \in States
    /\ cause \in Causes
    /\ aWork \in AWork
    /\ aMerge \in AMerge
    /\ oLanded \in BOOLEAN
    /\ did \in BOOLEAN
    /\ sentinel \in BOOLEAN

Init ==
    /\ held = FALSE
    /\ state = "merging"
    /\ cause = ""
    /\ aWork = "ready"
    /\ aMerge = "none"
    /\ oLanded = FALSE
    /\ did = FALSE
    /\ sentinel = FALSE

\* the deal deals stream s's ready card a to a member; a held stream deals
\* nothing (steps_work.go dealPlan: StreamHeld refuses it)
Deal ==
    /\ held = FALSE
    /\ aWork = "ready"
    /\ aWork' = "working"
    /\ UNCHANGED <<held, state, cause, aMerge, oLanded, did, sentinel>>

\* a finishes its work and enters the merge queue
Promote ==
    /\ aWork = "working"
    /\ aWork' = "merging"
    /\ aMerge = "none"
    /\ aMerge' = "queued"
    /\ UNCHANGED <<held, state, cause, oLanded, did, sentinel>>

\* a withdrawn card is dealt again at a new generation (ready), so a hold
\* has something to keep refusing
Redeal ==
    /\ aWork = "withdrawn"
    /\ aWork' = "ready"
    /\ UNCHANGED <<held, state, cause, aMerge, oLanded, did, sentinel>>

\* holdStream: hold stream s, withdrawing its ready cards (never begun)
Hold ==
    /\ held = FALSE
    /\ held' = TRUE
    /\ IF aWork = "ready" THEN aWork' = "withdrawn" ELSE aWork' = aWork
    /\ UNCHANGED <<state, cause, aMerge, oLanded, did, sentinel>>

\* holdStream with --return also withdraws working cards
HoldReturn ==
    /\ held = FALSE
    /\ held' = TRUE
    /\ IF aWork \in {"ready", "working"} THEN aWork' = "withdrawn" ELSE aWork' = aWork
    /\ UNCHANGED <<state, cause, aMerge, oLanded, did, sentinel>>

Unhold ==
    /\ held = TRUE
    /\ held' = FALSE
    /\ UNCHANGED <<state, cause, aWork, aMerge, oLanded, did, sentinel>>

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
    /\ UNCHANGED <<held, aWork, oLanded, did, sentinel>>

\* a red stop (the branch is red)
StopRed ==
    /\ state \notin {"stopped", "landed"}
    /\ aMerge = "queued"
    /\ state' = "stopped"
    /\ cause' = "red"
    /\ UNCHANGED <<held, aWork, aMerge, oLanded, did, sentinel>>

\* a conflict stop (the lander's own failure; not a refusal of the card's head)
StopConflict ==
    /\ state \notin {"stopped", "landed"}
    /\ aMerge = "queued"
    /\ state' = "stopped"
    /\ cause' = "conflict"
    /\ aMerge' = "stuck"
    /\ UNCHANGED <<held, aWork, oLanded, did, sentinel>>

\* a rejected stop (the merge queue rejected the batch)
StopRejected ==
    /\ state \notin {"stopped", "landed"}
    /\ aMerge = "queued"
    /\ state' = "stopped"
    /\ cause' = "rejected"
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
    /\ UNCHANGED <<held, state, cause, oLanded, did, sentinel>>

\* card o (stream t) lands; the cross need waits on this
LandO ==
    /\ oLanded = FALSE
    /\ oLanded' = TRUE
    /\ UNCHANGED <<held, state, cause, aWork, aMerge, did, sentinel>>

\* card a lands: the stream s is done
LandA ==
    /\ state \notin {"stopped", "landed"}
    /\ aWork = "merging"
    /\ aMerge = "queued"
    /\ aWork' = "landed"
    /\ aMerge' = "none"
    /\ state' = "landed"
    /\ UNCHANGED <<held, cause, oLanded, did, sentinel>>

\* a stop sentinel of stream s lands (behind a closed stream)
LandSentinel ==
    /\ sentinel = FALSE
    /\ sentinel' = TRUE
    /\ UNCHANGED <<held, state, cause, aWork, aMerge, oLanded, did>>

\* the coordinator says what was done (a red stop needs it before resume)
SayDid ==
    /\ state = "stopped"
    /\ cause = "red"
    /\ did = FALSE
    /\ did' = TRUE
    /\ UNCHANGED <<held, state, cause, aWork, aMerge, oLanded, sentinel>>

\* Resume: a cross stop waits until the needed card has landed; a red stop
\* needs --did; the other causes are resolved by the coordinator. The stuck
\* cards go back to queued and the stream merges again.
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
    /\ UNCHANGED <<held, aWork, oLanded, sentinel>>

\* T7. TickResume: the tick resumes a cross stop on its own once the needed
\* card has landed (steps_tick.go)
TickResume ==
    /\ state = "stopped"
    /\ cause = "cross"
    /\ oLanded = TRUE
    /\ state' = "merging"
    /\ cause' = ""
    /\ aMerge' = IF aMerge = "stuck" THEN "queued" ELSE aMerge
    /\ UNCHANGED <<held, aWork, oLanded, did, sentinel>>

\* an add past a landed stop reopens the stream to working (steps_work.go,
\* stream_reopen.go reopenAdd)
ReopenAdd ==
    /\ state = "landed"
    /\ sentinel = TRUE
    /\ state' = "working"
    /\ aWork' = "ready"
    /\ aMerge' = "none"
    /\ UNCHANGED <<held, cause, oLanded, did, sentinel>>

Next ==
    \/ Deal \/ Promote \/ Redeal
    \/ Hold \/ HoldReturn \/ Unhold
    \/ StopCross \/ StopRed \/ StopConflict \/ StopRejected \/ Refusal
    \/ LandO \/ LandA \/ LandSentinel \/ SayDid
    \/ Resume \/ TickResume \/ ReopenAdd

Spec == Init /\ [][Next]_vars

(* A held stream deals nothing: its ready cards are never dealt to working. *)
HeldStreamDealsNothing ==
    [][(held /\ aWork = "ready") => aWork' # "working"]_vars

(* A cross stop waits until the needed card has landed: while the stream is  *)
(* stopped for a cross and o is not landed, the stream stays stopped.        *)
CrossResumesAfterNeed ==
    [][(state = "stopped" /\ cause = "cross" /\ ~oLanded) => state' = "stopped"]_vars

(* A refusal of the card's own head never stops the stream. *)
OwnRefusalNeverStops ==
    [][Refusal => state' # "stopped"]_vars
=============================================================================
