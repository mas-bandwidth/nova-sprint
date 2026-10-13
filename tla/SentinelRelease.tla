------------------------ MODULE SentinelRelease ------------------------
\* P8: automatic sentinel release and the consumer's next deal.
\* This bounded contract is separate from Holds' historical manual/cut
\* sentinel machine. It does not prove a refinement of Holds or Git+store
\* atomicity. See README-SentinelRelease.md for the concrete boundary.
EXTENDS Naturals, FiniteSets

CONSTANTS Producers, MaxHead, AllowEarly, Broken
ASSUME /\ IsFiniteSet(Producers)
       /\ MaxHead \in Nat \ {0}
       /\ AllowEarly \in BOOLEAN
       /\ Broken \in {"none", "unlanded", "nodev", "stalehead", "wrongstream",
                      "stalescope", "silent", "manualgate", "staleconsumer",
                      "failedmerge", "reusedmerge", "earlyauto", "worker", "dropwaivesall", "emptynotice"}

Heads == Producers \X (1..MaxHead)
Tokens == Heads \cup {"other"}
States == {"working", "landed", "dropped"}
EmptyProof == [scope |-> 0, head |-> 0, included |-> {}]

VARIABLES state, head, scope, remoteDev, observedDev, proof,
          released, releaseHeads, releaseDev, releaseDone, releaseFresh,
          releaseAuthorized, releaseDropped, notice, noticeKind, noticeDev,
          consumerBase, mergeOK, mergeSucceeded, mergeRound, deals,
          everyDealObserved, everyDealMerged, everyDealNewMerge

vars == <<state, head, scope, remoteDev, observedDev, proof,
          released, releaseHeads, releaseDev, releaseDone, releaseFresh,
          releaseAuthorized, releaseDropped, notice, noticeKind, noticeDev,
          consumerBase, mergeOK, mergeSucceeded, mergeRound, deals,
          everyDealObserved, everyDealMerged, everyDealNewMerge>>

TypeOK ==
  /\ state \in [Producers -> States]
  /\ head \in [Producers -> 1..MaxHead]
  /\ scope \in 1..2
  /\ remoteDev \subseteq Tokens
  /\ observedDev \subseteq remoteDev
  /\ proof \in [Producers -> [scope : 0..2, head : 0..MaxHead,
                              included : SUBSET Tokens]]
  /\ released \in {"none", "auto", "early"}
  /\ releaseHeads \subseteq Heads
  /\ releaseDev \subseteq Tokens
  /\ releaseDone \in BOOLEAN
  /\ releaseFresh \in BOOLEAN
  /\ releaseAuthorized \in BOOLEAN
  /\ releaseDropped \subseteq Producers
  /\ notice \in BOOLEAN
  /\ noticeKind \in {"none", "dev", "no-producers", "all-dropped", "early"}
  /\ noticeDev \subseteq Tokens
  /\ consumerBase \subseteq Tokens
  /\ mergeOK \in BOOLEAN
  /\ mergeSucceeded \in BOOLEAN
  /\ mergeRound \in 0..2
  /\ deals \in 0..2
  /\ everyDealObserved \in BOOLEAN
  /\ everyDealMerged \in BOOLEAN
  /\ everyDealNewMerge \in BOOLEAN

Init ==
  /\ state = [p \in Producers |-> "working"]
  /\ head = [p \in Producers |-> 1]
  /\ scope = 1
  /\ remoteDev = {}
  /\ observedDev = {}
  /\ proof = [p \in Producers |-> EmptyProof]
  /\ released = "none"
  /\ releaseHeads = {}
  /\ releaseDev = {}
  /\ releaseDone = FALSE
  /\ releaseFresh = FALSE
  /\ releaseAuthorized = FALSE
  /\ releaseDropped = {}
  /\ notice = FALSE
  /\ noticeKind = "none"
  /\ noticeDev = {}
  /\ consumerBase = {}
  /\ mergeOK = FALSE
  /\ mergeSucceeded = FALSE
  /\ mergeRound = 0
  /\ deals = 0
  /\ everyDealObserved = TRUE
  /\ everyDealMerged = TRUE
  /\ everyDealNewMerge = TRUE

releaseVars == <<released, releaseHeads, releaseDev, releaseDone,
                 releaseFresh, releaseAuthorized, releaseDropped, notice, noticeKind, noticeDev>>
consumerVars == <<consumerBase, mergeOK, mergeSucceeded, mergeRound, deals,
                  everyDealObserved, everyDealMerged, everyDealNewMerge>>

RequiredProducers == {p \in Producers : state[p] # "dropped"}
CurrentHeads == {<<p, head[p]>> : p \in RequiredProducers}
AllDone == \A p \in Producers : state[p] \in {"landed", "dropped"}
Eligible == AllDone /\ CurrentHeads \subseteq remoteDev

Land(p) ==
  /\ state[p] = "working"
  /\ state' = [state EXCEPT ![p] = "landed"]
  /\ UNCHANGED <<head, scope, remoteDev, observedDev, proof,
                  releaseVars, consumerVars>>

\* A dropped producer is complete by waiver and contributes no head. Other
\* producers still need their own landed state and ancestry evidence.
Drop(p) ==
  /\ released = "none"
  /\ state[p] = "working"
  /\ state' = [state EXCEPT ![p] = "dropped"]
  /\ UNCHANGED <<head, scope, remoteDev, observedDev, proof,
                  releaseVars, consumerVars>>

\* One bounded new producer revision can race with a completed Git probe.
\* Old evidence is retained to exercise the consuming fence, not erased to
\* make a stale-evidence counterexample impossible by construction.
NewHead(p) ==
  /\ released = "none"
  /\ state[p] = "landed"
  /\ head[p] < MaxHead
  /\ state' = [state EXCEPT ![p] = "working"]
  /\ head' = [head EXCEPT ![p] = @ + 1]
  /\ UNCHANGED <<scope, remoteDev, observedDev, proof,
                  releaseVars, consumerVars>>

\* A proof-scope change abstracts a new needs/row/epoch revision. This is
\* invalidation only, not a model of clear or dynamic graph construction.
ChangeScope ==
  /\ released = "none"
  /\ scope = 1
  /\ scope' = 2
  /\ UNCHANGED <<state, head, remoteDev, observedDev, proof,
                  releaseVars, consumerVars>>

MergeProducer(p) ==
  /\ state[p] = "landed"
  /\ <<p, head[p]>> \notin remoteDev
  /\ remoteDev' = remoteDev \cup {<<p, head[p]>>}
  /\ UNCHANGED <<state, head, scope, observedDev, proof,
                  releaseVars, consumerVars>>

\* One unrelated commit exposes a dev move between consumer merge and deal.
AdvanceDev ==
  /\ "other" \notin remoteDev
  /\ remoteDev' = remoteDev \cup {"other"}
  /\ UNCHANGED <<state, head, scope, observedDev, proof,
                  releaseVars, consumerVars>>

ObserveDev ==
  /\ observedDev # remoteDev
  /\ observedDev' = remoteDev
  /\ UNCHANGED <<state, head, scope, remoteDev, proof,
                  releaseVars, consumerVars>>

Probe(p) ==
  /\ state[p] = "landed"
  /\ <<p, head[p]>> \in observedDev
  /\ LET result == [scope |-> scope, head |-> head[p], included |-> observedDev]
     IN /\ proof[p] # result
        /\ proof' = [proof EXCEPT ![p] = result]
  /\ UNCHANGED <<state, head, scope, remoteDev, observedDev,
                  releaseVars, consumerVars>>

Fresh(p) == /\ proof[p].scope = scope
            /\ proof[p].head = head[p]
            /\ <<p, head[p]>> \in proof[p].included
ProofsFresh == \A p \in RequiredProducers : Fresh(p)

\* Each mutation removes one consuming guard. A previous head or another
\* stream's head is evidence about that head only, even if both have index 1.
AcceptedProofs ==
  CASE Broken = "stalehead" ->
         \A p \in RequiredProducers : /\ proof[p].scope = scope
                               /\ proof[p].head > 0
                               /\ <<p, proof[p].head>> \in proof[p].included
    [] Broken = "stalescope" ->
         \A p \in RequiredProducers : /\ proof[p].head = head[p]
                               /\ <<p, head[p]>> \in proof[p].included
    [] Broken = "wrongstream" ->
         \A p \in RequiredProducers : \E q \in RequiredProducers :
           /\ proof[q].scope = scope
           /\ proof[q].head = head[p]
           /\ <<q, head[p]>> \in proof[q].included
    [] Broken = "dropwaivesall" ->
         (RequiredProducers # Producers) \/ ProofsFresh
    [] OTHER -> ProofsFresh

NoticeKind(kind) ==
  IF kind # "auto" THEN "early"
  ELSE IF Producers = {} THEN "no-producers"
  ELSE IF RequiredProducers = {} THEN "all-dropped"
  ELSE "dev"

CommitRelease(kind, authorized) ==
  /\ released' = kind
  /\ releaseHeads' = CurrentHeads
  /\ releaseDev' = observedDev
  /\ releaseDone' = AllDone
  /\ releaseFresh' = ProofsFresh
  /\ releaseAuthorized' = authorized
  /\ releaseDropped' = Producers \ RequiredProducers
  /\ notice' = Broken # "silent"
  /\ noticeKind' = NoticeKind(kind)
  /\ noticeDev' = IF NoticeKind(kind) = "dev" \/ Broken = "emptynotice"
                    THEN observedDev ELSE {}
  /\ UNCHANGED <<state, head, scope, remoteDev, observedDev, proof, consumerVars>>

AutoRelease ==
  /\ released = "none"
  /\ AllDone \/ Broken = "unlanded"
  /\ AcceptedProofs \/ Broken \in {"nodev", "unlanded"}
  /\ Broken # "manualgate"
  /\ CommitRelease("auto", TRUE)

\* The explicit authorized override may cross working or dropped producers.
\* It is never silently relabeled as automatic, and has no fairness premise.
EarlyRelease(actor) ==
  /\ AllowEarly
  /\ released = "none"
  /\ actor = "coordinator" \/ Broken = "worker"
  /\ CommitRelease(IF Broken = "earlyauto" THEN "auto" ELSE "early",
                   actor = "coordinator")

MergeConsumer ==
  /\ released # "none"
  /\ deals < 2
  /\ ~mergeOK \/ consumerBase # observedDev \/ mergeRound # deals + 1
  /\ consumerBase' = observedDev
  /\ mergeOK' = TRUE
  /\ mergeSucceeded' = TRUE
  /\ mergeRound' = deals + 1
  /\ UNCHANGED <<state, head, scope, remoteDev, observedDev, proof, releaseVars,
                  deals, everyDealObserved, everyDealMerged, everyDealNewMerge>>

\* Failed/conflicting Git integration produces no success receipt. The
\* mutation writes a false success stamp without a successful merge.
MergeFailure ==
  /\ released # "none"
  /\ deals < 2
  /\ ~mergeOK \/ consumerBase # observedDev \/ mergeRound # deals + 1
  /\ mergeOK' = (Broken = "failedmerge")
  /\ consumerBase' = IF Broken = "failedmerge" THEN observedDev ELSE consumerBase
  /\ mergeSucceeded' = FALSE
  /\ mergeRound' = IF Broken = "failedmerge" THEN deals + 1 ELSE mergeRound
  /\ UNCHANGED <<state, head, scope, remoteDev, observedDev, proof, releaseVars,
                  deals, everyDealObserved, everyDealMerged, everyDealNewMerge>>

Deal ==
  /\ released # "none"
  /\ deals < 2
  /\ mergeOK
  /\ consumerBase = observedDev \/ Broken = "staleconsumer"
  /\ mergeRound = deals + 1 \/ Broken = "reusedmerge"
  /\ deals' = deals + 1
  /\ mergeOK' = (Broken = "reusedmerge")
  /\ everyDealObserved' = everyDealObserved /\ consumerBase = observedDev
  /\ everyDealMerged' = everyDealMerged /\ mergeSucceeded
  /\ everyDealNewMerge' = everyDealNewMerge /\ mergeRound = deals + 1
  /\ UNCHANGED <<state, head, scope, remoteDev, observedDev, proof, releaseVars,
                  consumerBase, mergeSucceeded, mergeRound>>

Next ==
  \/ \E p \in Producers : Land(p) \/ Drop(p) \/ NewHead(p) \/ MergeProducer(p) \/ Probe(p)
  \/ ChangeScope \/ AdvanceDev \/ ObserveDev \/ AutoRelease
  \/ \E actor \in {"coordinator", "worker"} : EarlyRelease(actor)
  \/ MergeConsumer \/ MergeFailure \/ Deal

AutoRequiresCompletedAndDev ==
  released = "auto" => (releaseDone /\ releaseHeads \subseteq releaseDev)
AutoUsesCurrentProof == released = "auto" => releaseFresh
EarlyIsAuthorized == released = "early" => releaseAuthorized
ReleaseHasNotice ==
  released # "none" =>
    /\ notice
    /\ noticeKind # "none"
    /\ (noticeKind = "dev" => noticeDev = releaseDev)
NoInventedMergeNotice ==
  noticeKind \in {"no-producers", "all-dropped"} => noticeDev = {}
DealsContainObservedDev == everyDealObserved
DealsFollowSuccessfulMerge == everyDealMerged
EveryDealHasNewMerge == everyDealNewMerge

\* Reachability witnesses deliberately negate permitted outcomes.
NeverAutoReleased == released # "auto"
NeverEarlyReleased == released # "early"
NeverAllDroppedAutoReleased == ~(released = "auto" /\ releaseDropped = Producers)
DroppedContributesNoHead ==
  \A p \in releaseDropped : \A h \in 1..MaxHead : <<p, h>> \notin releaseHeads

\* Git is append-only in this abstraction. Successful fetch/probe and
\* integration retries are eventually scheduled. No coordinator action is
\* fair: disabling automatic release cannot be rescued by an early release.
Fairness ==
  /\ \A p \in Producers : WF_vars(Land(p)) /\ WF_vars(MergeProducer(p)) /\ WF_vars(Probe(p))
  /\ WF_vars(ObserveDev)
  /\ WF_vars(AutoRelease)
  /\ WF_vars(MergeConsumer)
  /\ WF_vars(Deal)
Spec == Init /\ [][Next]_vars /\ Fairness
StableEligibilityReleases == (<>[]Eligible) => <>(released # "none")
ReleasedEventuallyDeals == (released # "none") ~> (deals = 2)
=============================================================================
