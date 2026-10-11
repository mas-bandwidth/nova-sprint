----------------------------- MODULE SeatProof ------------------------------
\* The seat's push proof, its renewal and the handover gate
\* (docs/SPEC-SPRINT.md, "The push proof"; internal/sprint/pushproof.go PushPong,
\* PushLive, PushWhy; cmd/nova-sprint pushproof.go pushGate, renewSeatProof,
\* pushGateRenewing; cmd/nova-sprint coordinator.go seatPushed). The seat is held
\* only by a session the push loop reaches, and every coordinator verb is refused
\* while the acting seat's last pong is stale. The acting seat's own session
\* answers its outstanding check when it runs a verb, so a busy seat is never
\* locked out; a seat with no outstanding check to answer, or truly unreachable,
\* is still refused. A coordinator handover to a destination (coordinator <name>)
\* gates on the destination's proof alone and never answers the destination's
\* outstanding check from the acting session: renewal is confined to the acting
\* seat, so a handover destination whose session never answered is still refused.
\*
\* Broken = "none" is the design. "norenew" is the reversed witness: the acting
\* seat's verb never answers its outstanding check, so a busy seat stays locked
\* out and BusySeatNeverLockedOut fails. "renewdest" is the reversed witness of
\* this attempt: the handover answers the destination's outstanding check from
\* the acting session, so the seat is handed to a session the push loop never
\* reached, and CoordinatorNeverFabricates fails.

EXTENDS Naturals, FiniteSets

CONSTANTS Live, MaxTime, MaxVerbs, Broken

VARIABLES now, nonce, pong, proven, failed, verbs, dnonce, dpong, dproven, dfailed
vars == <<now, nonce, pong, proven, failed, verbs, dnonce, dpong, dproven, dfailed>>

Nonces == {"none", "a", "b", "c"}

TypeOK ==
  /\ now \in 0..MaxTime
  /\ nonce \in Nonces /\ pong \in Nonces /\ proven \in 0..MaxTime /\ failed \in BOOLEAN
  /\ dnonce \in Nonces /\ dpong \in Nonces /\ dproven \in 0..MaxTime /\ dfailed \in BOOLEAN
  /\ verbs \in 0..MaxVerbs

\* A seat is live (sprint.PushLive): a pong carrying the last check, no older than
\* Live, and the last delivery did not fail.
IsLive(n, p, pr, f) ==
  /\ f = FALSE
  /\ n # "none"
  /\ p = n
  /\ now - pr <= Live

\* A check the push loop wrote but nothing answered (sprint.PushPong accepts exactly
\* this): the folder holds a PROOF-<nonce> the session can answer.
Outstanding(n, p, f) == n # "none" /\ f = FALSE /\ p # n

Init ==
  /\ now = 0
  /\ nonce = "none" /\ pong = "none" /\ proven = 0 /\ failed = FALSE
  /\ dnonce = "none" /\ dpong = "none" /\ dproven = 0 /\ dfailed = FALSE
  /\ verbs = 0

\* Time passes: the proofs age.
Tick ==
  /\ now < MaxTime
  /\ now' = now + 1
  /\ UNCHANGED <<nonce, pong, proven, failed, verbs, dnonce, dpong, dproven, dfailed>>

\* The push loop delivers a new check to the acting seat (writes PROOF-<n>).
Deliver(n) ==
  /\ n \in Nonces \ {"none"}
  /\ nonce' = n /\ failed' = FALSE
  /\ UNCHANGED <<now, pong, proven, verbs, dnonce, dpong, dproven, dfailed>>

\* The push loop delivers a new check to the destination.
DeliverD(n) ==
  /\ n \in Nonces \ {"none"}
  /\ dnonce' = n /\ dfailed' = FALSE
  /\ UNCHANGED <<now, nonce, pong, proven, failed, verbs, dpong, dproven>>

\* The acting session answers its check (seat pong).
Pong ==
  /\ Outstanding(nonce, pong, failed)
  /\ pong' = nonce /\ proven' = now
  /\ UNCHANGED <<now, nonce, failed, verbs, dnonce, dpong, dproven, dfailed>>

\* The destination's session answers its check (seat pong).
PongD ==
  /\ Outstanding(dnonce, dpong, dfailed)
  /\ dpong' = dnonce /\ dproven' = now
  /\ UNCHANGED <<now, nonce, pong, proven, failed, verbs, dnonce, dfailed>>

\* The acting seat runs a coordinator verb. With the design the verb answers the
\* acting seat's own outstanding check before the gate is asked again
\* (renewSeatProof), so a busy seat proves itself; "norenew" never answers, and a
\* busy seat stays locked out.
Verb ==
  /\ verbs < MaxVerbs
  /\ verbs' = verbs + 1
  /\ IF Outstanding(nonce, pong, failed) /\ Broken # "norenew"
       THEN /\ pong' = nonce /\ proven' = now
       ELSE UNCHANGED <<pong, proven>>
  /\ UNCHANGED <<now, nonce, failed, dnonce, dpong, dproven, dfailed>>

\* The coordinator hands the seat to the destination (coordinator <name>). The
\* destination must already be live (plain pushGate on req.To): the handover never
\* answers the destination's outstanding check. Broken = "renewdest" answers it
\* from the acting session, the reversed witness of this attempt.
Coordinator ==
  /\ verbs < MaxVerbs
  /\ verbs' = verbs + 1
  /\ IF IsLive(dnonce, dpong, dproven, dfailed)
       THEN UNCHANGED <<dpong, dproven>>
       ELSE /\ Broken = "renewdest"
            /\ Outstanding(dnonce, dpong, dfailed)
            /\ dpong' = dnonce /\ dproven' = now
  /\ UNCHANGED <<now, nonce, pong, proven, failed, dnonce, dfailed>>

Next ==
  \/ Tick
  \/ \E n \in Nonces \ {"none"} : Deliver(n)
  \/ \E n \in Nonces \ {"none"} : DeliverD(n)
  \/ Pong
  \/ PongD
  \/ Verb
  \/ Coordinator

Spec == Init /\ [][Next]_vars

\* A busy acting seat (a check delivered but not answered) that runs a verb proves
\* itself: the verb answers the outstanding check, so the seat is live after it and
\* is never locked out while it works.
BusySeatNeverLockedOut ==
  [][(Verb /\ Outstanding(nonce, pong, failed)) => (pong' = nonce /\ proven' = now)]_vars

\* A verb never fabricates a proof: with nothing outstanding to answer it leaves the
\* acting seat's record alone, so a seat that is truly unreachable is still refused.
VerbNeverFabricates ==
  [][(Verb /\ ~Outstanding(nonce, pong, failed)) => UNCHANGED <<pong, proven>>]_vars

\* A handover never fabricates the destination's proof: the destination must already
\* be live, or the handover is refused; the acting session never answers the
\* destination's outstanding check (renewal is confined to the acting seat).
CoordinatorNeverFabricates ==
  [][(Coordinator /\ ~IsLive(dnonce, dpong, dproven, dfailed)) => UNCHANGED <<dpong, dproven>>]_vars

=============================================================================
