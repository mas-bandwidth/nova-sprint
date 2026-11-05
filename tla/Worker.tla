-------------------------------- MODULE Worker --------------------------------
\* nova-sprint worker (internal/worker/worker.go, internal/worker/lane.go): one
\* fleet row's loop. The server deals a card to the row at a generation; the
\* worker takes it into a lane, the lane runs the harness and ends (ok, failed,
\* or expired at its deadline), and the worker reports the end. The server
\* accepts a report only while the claim it names is still the card's current
\* claim; a claim the server moved (a live-grace return, a redeal, a stop) is
\* reaped by the worker with no report.
\*
\* The state, per card c:
\*   gen[c]      the card's generation on the server (0 = never dealt)
\*   claim[c]    the generation this row holds on the server, 0 for none
\*   lane[c]     the worker's lane: "none", "running" or "ended"
\*   laneGen[c]  the generation the lane was taken at
\*   outcome[c]  how the lane ended: "none", "ok", "failed", "expired"
\* and the row's own flags: draining (SIGTERM: take nothing more) and stopped
\* (the machine is STOPPED: stop-return every running lane). Ghosts:
\*   accepted    the reports the server accepted, <<card, gen, verdict>>
\*   expiredAt   the <<card, gen>> lanes ended at their deadline
\*   stale       a report was sent for a claim that had moved
\*
\* The design, Broken = {}:
\*   Deal       the server deals a card to the row at its next generation.
\*   Move       the server moves the row's claim away (live-grace return,
\*              coordinator return, redeal elsewhere).
\*   Take       the worker takes a dealt card into a lane at its generation,
\*              unless it is draining or stopped; a launch that cannot start
\*              (no route, staging refused) ends at once, failed.
\*   RunOk, HarnessFail, Expire
\*              a running lane ends: the harness did its work, the harness
\*              failed, or the lane passed its deadline and was killed.
\*   Report     the worker reports an ended lane whose claim is still its own
\*              (the reap check runs first in every pass); the server accepts
\*              it while the claim stands and clears the claim. The answer may
\*              be lost: the lane stays and the report is sent again next pass.
\*   Reap       a lane whose claim moved is dropped, killed if running, with
\*              no report.
\*   Stop, StopReturn
\*              the machine is stopped; each running lane is killed and handed
\*              back with stop-return.
\*   Drain      the row drains: nothing more is taken.
\*
\* Reversed witnesses:
\*   "report-stale" Report skips the claim check: a report is sent for a claim
\*                  that moved (NoStaleReport fails).
\*   "deadline-ok"  an expired lane is judged ok (ExpiredNeverOk fails).
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, MaxGen, Broken

ASSUME Broken \subseteq {"report-stale", "deadline-ok"}

VARIABLES gen, claim, lane, laneGen, outcome, draining, stopped,
          accepted, expiredAt, stale

vars == <<gen, claim, lane, laneGen, outcome, draining, stopped,
          accepted, expiredAt, stale>>

Gens == 0..MaxGen
Lanes == {"none", "running", "ended"}
Outcomes == {"none", "ok", "failed", "expired"}

TypeOK ==
  /\ gen \in [Cards -> Gens]
  /\ claim \in [Cards -> Gens]
  /\ lane \in [Cards -> Lanes]
  /\ laneGen \in [Cards -> Gens]
  /\ outcome \in [Cards -> Outcomes]
  /\ draining \in BOOLEAN
  /\ stopped \in BOOLEAN
  /\ accepted \subseteq (Cards \X (1..MaxGen) \X {"ok", "failed"})
  /\ expiredAt \subseteq (Cards \X (1..MaxGen))
  /\ stale \in BOOLEAN

Init ==
  /\ gen = [c \in Cards |-> 0]
  /\ claim = [c \in Cards |-> 0]
  /\ lane = [c \in Cards |-> "none"]
  /\ laneGen = [c \in Cards |-> 0]
  /\ outcome = [c \in Cards |-> "none"]
  /\ draining = FALSE
  /\ stopped = FALSE
  /\ accepted = {}
  /\ expiredAt = {}
  /\ stale = FALSE

\* member.Judge: only a lane whose harness did its work is reported ok.
Verdict(o) ==
  IF o = "ok" \/ (o = "expired" /\ "deadline-ok" \in Broken) THEN "ok" ELSE "failed"

Deal(c) ==
  /\ claim[c] = 0
  /\ gen[c] < MaxGen
  /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
  /\ claim' = [claim EXCEPT ![c] = gen[c] + 1]
  /\ UNCHANGED <<lane, laneGen, outcome, draining, stopped, accepted, expiredAt, stale>>

Move(c) ==
  /\ claim[c] # 0
  /\ claim' = [claim EXCEPT ![c] = 0]
  /\ UNCHANGED <<gen, lane, laneGen, outcome, draining, stopped, accepted, expiredAt, stale>>

Take(c) ==
  /\ claim[c] # 0
  /\ lane[c] = "none"
  /\ ~draining /\ ~stopped
  /\ \E started \in BOOLEAN :
       /\ lane' = [lane EXCEPT ![c] = IF started THEN "running" ELSE "ended"]
       /\ outcome' = [outcome EXCEPT ![c] = IF started THEN "none" ELSE "failed"]
  /\ laneGen' = [laneGen EXCEPT ![c] = claim[c]]
  /\ UNCHANGED <<gen, claim, draining, stopped, accepted, expiredAt, stale>>

End(c, o) ==
  /\ lane[c] = "running"
  /\ lane' = [lane EXCEPT ![c] = "ended"]
  /\ outcome' = [outcome EXCEPT ![c] = o]
  /\ UNCHANGED <<gen, claim, laneGen, draining, stopped, accepted, stale>>

RunOk(c) == End(c, "ok") /\ UNCHANGED expiredAt
HarnessFail(c) == End(c, "failed") /\ UNCHANGED expiredAt
Expire(c) ==
  /\ End(c, "expired")
  /\ expiredAt' = expiredAt \cup {<<c, laneGen[c]>>}

Report(c) ==
  /\ lane[c] = "ended"
  /\ ("report-stale" \in Broken \/ claim[c] = laneGen[c])
  /\ stale' = (stale \/ claim[c] # laneGen[c])
  /\ IF claim[c] = laneGen[c]
       THEN /\ claim' = [claim EXCEPT ![c] = 0]
            /\ accepted' = accepted \cup {<<c, laneGen[c], Verdict(outcome[c])>>}
       ELSE UNCHANGED <<claim, accepted>>
  /\ \E answered \in BOOLEAN :
       lane' = [lane EXCEPT ![c] = IF answered THEN "none" ELSE "ended"]
  /\ UNCHANGED <<gen, laneGen, outcome, draining, stopped, expiredAt>>

Reap(c) ==
  /\ lane[c] # "none"
  /\ claim[c] # laneGen[c]
  /\ lane' = [lane EXCEPT ![c] = "none"]
  /\ UNCHANGED <<gen, claim, laneGen, outcome, draining, stopped, accepted, expiredAt, stale>>

Stop ==
  /\ ~stopped
  /\ stopped' = TRUE
  /\ UNCHANGED <<gen, claim, lane, laneGen, outcome, draining, accepted, expiredAt, stale>>

StopReturn(c) ==
  /\ stopped
  /\ lane[c] = "running"
  /\ claim' = [claim EXCEPT ![c] = IF claim[c] = laneGen[c] THEN 0 ELSE claim[c]]
  /\ lane' = [lane EXCEPT ![c] = "none"]
  /\ UNCHANGED <<gen, laneGen, outcome, draining, stopped, accepted, expiredAt, stale>>

Drain ==
  /\ ~draining
  /\ draining' = TRUE
  /\ UNCHANGED <<gen, claim, lane, laneGen, outcome, stopped, accepted, expiredAt, stale>>

Next ==
  \/ Stop \/ Drain
  \/ \E c \in Cards :
       \/ Deal(c) \/ Move(c) \/ Take(c)
       \/ RunOk(c) \/ HarnessFail(c) \/ Expire(c)
       \/ Report(c) \/ Reap(c) \/ StopReturn(c)

\* The deadline always comes, and the worker's pass always runs.
Spec == Init /\ [][Next]_vars
  /\ \A c \in Cards : /\ WF_vars(Expire(c)) /\ WF_vars(Reap(c)) /\ WF_vars(StopReturn(c))
                      /\ SF_vars(Report(c) /\ lane'[c] = "none")

\* The worker never sends a report for a claim the server moved.
NoStaleReport == ~stale

\* A lane killed at its deadline is never reported ok.
ExpiredNeverOk == \A r \in accepted : <<r[1], r[2]>> \in expiredAt => r[3] = "failed"

\* At most one report is accepted for a card's generation.
OneReportPerGen ==
  \A c \in Cards, g \in 1..MaxGen :
    Cardinality({r \in accepted : r[1] = c /\ r[2] = g}) <= 1

\* A lane runs only at a generation the server dealt.
LaneAtADealtGen == \A c \in Cards : lane[c] # "none" => laneGen[c] \in 1..gen[c]

\* Every lane leaves: by an accepted report, a reap or a stop-return.
LaneLeaves == \A c \in Cards : lane[c] # "none" ~> lane[c] = "none"
=============================================================================
