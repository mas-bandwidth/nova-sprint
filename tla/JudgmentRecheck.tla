------------------------- MODULE JudgmentRecheck -------------------------
EXTENDS Naturals, FiniteSets

(* P4: closure-only recheck after a successful guarded START or EVERY server startup.
 * Startup includes first rollout, rollback and same-version restart: there is
 * no version marker. The adopt CLI is store-free and absent from this model.
 * Machine authorization and the later closure transaction are separate.
 * START's funding/debt/restore checks are represented by its success/refusal;
 * ClearDefaults and StopReturn retain their detailed transition models.
 * Evidence is the existing pure predicate's answer, not a clock:
 * late=withdrawn/dead, route=current funded route, orphan=off-table/absent.
 * Unknown and unrecognized judgments must remain open.
 * Route truth is the configured RouteSet OBSERVED FOR THIS PASS: the engine
 * loads it once before retrying fenced tables. Once ReadPlan captures it,
 * retries retain that observation. External configuration is not a jointly
 * atomic participant and is deliberately not represented as global truth.
 * Only table evidence changes advance revision; observing new configuration
 * between passes never pretends to advance the sprint table generation.
 * The closure Step is pinned to the authorization epoch. Its snapshot is
 * acquired only at the read generation; commit owns the operation fence.
 * Clear/change may invalidate a read, causing abort/replan rather than closure.
 * A new epoch has distinct judgment identities even when their names match.
 * Decided and notice counts stand for the existing exactly-once release path.
 * No action in the recheck creates judgments, deals, asks, or changes work.
 *)

CONSTANTS Judgments, Kinds, MaxEpoch, MaxRevision, Broken
VARIABLES epoch, revision, machine, truth, open, decided, notices,
          proof, authorized, authKind, authEpoch, phase, readEpoch, readRevision,
          readTruth, planned, lastClosed, readHadClosures, idempotentDone, routeFrozen, checkedRouteTruth, owner, checkedEpoch, checkedRevision

vars == <<epoch, revision, machine, truth, open, decided, notices,
          proof, authorized, authKind, authEpoch, phase, readEpoch, readRevision,
          readTruth, planned, lastClosed, readHadClosures, idempotentDone, routeFrozen, checkedRouteTruth, owner, checkedEpoch, checkedRevision>>

TruthValues == {"live", "obsolete", "unknown"}
RouteJudgments == {j \in Judgments : Kinds[j] = "route"}
Recognized(j) == Kinds[j] \in {"late", "route", "orphan"}
Obsolete(evidence, js) == {j \in js : Recognized(j) /\ evidence[j] = "obsolete"}
EmptyCounts == [e \in 1..MaxEpoch |-> [j \in Judgments |-> 0]]
EmptyProof == [e \in 1..MaxEpoch |-> [j \in Judgments |-> "none"]]

TypeOK ==
  /\ epoch \in 1..MaxEpoch
  /\ revision \in 0..MaxRevision
  /\ machine \in {"ready", "running", "stopped"}
  /\ truth \in [Judgments -> TruthValues]
  /\ open \subseteq Judgments
  /\ decided \in [1..MaxEpoch -> [Judgments -> 0..2]]
  /\ notices \in [1..MaxEpoch -> [Judgments -> 0..2]]
  /\ proof \in [1..MaxEpoch -> [Judgments -> {"none", "authorized", "unauthorized", "live", "unknown", "unrecognized"}]]
  /\ authorized \in BOOLEAN
  /\ authKind \in {"none", "start", "already-running", "startup", "refused"}
  /\ authEpoch \in 1..MaxEpoch
  /\ phase \in {"idle", "eligible", "read", "locked"}
  /\ readEpoch \in 1..MaxEpoch
  /\ readRevision \in 0..MaxRevision
  /\ readTruth \in [Judgments -> TruthValues]
  /\ planned \subseteq Judgments
  /\ lastClosed \subseteq Judgments
  /\ readHadClosures \in BOOLEAN /\ idempotentDone \in BOOLEAN
  /\ routeFrozen \in BOOLEAN
  /\ checkedRouteTruth \in [RouteJudgments -> TruthValues]
  /\ owner \in {"none", "other", "recheck"}
  /\ checkedEpoch \in 0..MaxEpoch
  /\ checkedRevision \in 0..MaxRevision

Init ==
  /\ epoch = 1 /\ revision = 0 /\ machine = "ready"
  /\ truth = [j \in Judgments |-> "live"]
  /\ open = Judgments /\ decided = EmptyCounts /\ notices = EmptyCounts
  /\ proof = EmptyProof /\ authorized = FALSE /\ authKind = "none" /\ authEpoch = 1
  /\ phase = "idle" /\ readEpoch = 1 /\ readRevision = 0
  /\ readTruth = truth /\ planned = {} /\ lastClosed = {} /\ readHadClosures = FALSE /\ idempotentDone = FALSE /\ routeFrozen = FALSE /\ owner = "none"
  /\ checkedEpoch = 0 /\ checkedRevision = 0
  /\ checkedRouteTruth = [j \in RouteJudgments |-> "live"]

StartSuccess ==
  /\ routeFrozen' = FALSE
  /\ lastClosed' = {}
  /\ phase = "idle" /\ owner = "none"
  /\ machine' = "running"  \* includes already RUNNING success
  /\ authKind' = IF machine = "running" THEN "already-running" ELSE "start"
  /\ authorized' = TRUE /\ authEpoch' = epoch /\ phase' = "eligible"
  /\ UNCHANGED <<epoch, revision, truth, open, decided, notices, proof,
       readEpoch, readRevision, readTruth, planned, readHadClosures, idempotentDone, owner, checkedEpoch, checkedRevision, checkedRouteTruth>>

\* Invocation is allowed in EVERY state. If another operation is pending,
\* this invocation waits/refuses without effects (stutter); it never bypasses
\* the store fence. No product queue or installed-version state is invented.
ServerStartup ==
  IF phase = "idle" /\ owner = "none"
  THEN /\ routeFrozen' = FALSE
       /\ lastClosed' = {}
       /\ authKind' = "startup"
       /\ authorized' = TRUE /\ authEpoch' = epoch /\ phase' = "eligible"
       /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices,
            proof, readEpoch, readRevision, readTruth, planned, owner,
            checkedEpoch, checkedRevision, readHadClosures, idempotentDone, checkedRouteTruth>>
  ELSE UNCHANGED vars

RefusedOrDry ==
  /\ routeFrozen' = FALSE
  /\ lastClosed' = {}
  /\ authKind' = "refused"
  /\ phase = "idle" /\ owner = "none"
  /\ authorized' = FALSE /\ authEpoch' = epoch
  /\ phase' = IF Broken = "failed" THEN "eligible" ELSE "idle"
  /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices, proof,
       readEpoch, readRevision, readTruth, planned, readHadClosures, idempotentDone, owner, checkedEpoch, checkedRevision, checkedRouteTruth>>

ReadPlan ==
  /\ routeFrozen' = TRUE
  /\ readHadClosures' = \E j \in Judgments : decided[epoch][j] > 0
  /\ phase = "eligible" /\ authEpoch = epoch /\ owner = "none"
  /\ readEpoch' = epoch /\ readRevision' = revision /\ readTruth' = truth
  /\ planned' = CASE Broken = "keep" -> {}
                      [] Broken = "closeLive" -> open
                      [] Broken = "closeUnknown" -> Obsolete(truth, open) \cup
                           {j \in open : truth[j] = "unknown" \/ ~Recognized(j)}
                      [] OTHER -> Obsolete(truth, open)
  /\ phase' = "read"
  /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices, proof,
       authorized, authKind, authEpoch, owner, checkedEpoch, checkedRevision, lastClosed, idempotentDone, checkedRouteTruth>>

AcquirePlan ==
  /\ phase = "read" /\ owner = "none"
  /\ IF Broken = "stale" THEN TRUE
       ELSE IF Broken = "epoch" THEN readRevision = revision
       ELSE /\ readEpoch = epoch /\ authEpoch = epoch /\ readRevision = revision
  /\ owner' = "recheck" /\ phase' = "locked"
  /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices, proof,
       authorized, authKind, authEpoch, readEpoch, readRevision, readTruth, planned,
       checkedEpoch, checkedRevision, lastClosed, readHadClosures, idempotentDone, routeFrozen, checkedRouteTruth>>

CommitPlan ==
  /\ checkedRouteTruth' = [j \in RouteJudgments |-> truth[j]]
  /\ routeFrozen' = FALSE
  /\ idempotentDone' = idempotentDone \/ (readHadClosures /\ planned = {})
  /\ lastClosed' = planned
  /\ IF Broken = "duplicate" THEN (phase = "locked" \/ (phase = "idle" /\ checkedEpoch # 0)) ELSE phase = "locked"
  /\ IF Broken \in {"owner", "duplicate"} THEN TRUE ELSE owner = "recheck"
  /\ IF Broken = "stale" THEN TRUE
       ELSE IF Broken = "epoch" THEN readRevision = revision
       ELSE /\ readEpoch = epoch /\ authEpoch = epoch /\ readRevision = revision
  /\ open' = open \ planned
  /\ decided' = [decided EXCEPT ![epoch] =
       [j \in Judgments |-> @ [j] + IF j \in planned THEN 1 ELSE 0]]
  /\ notices' = [notices EXCEPT ![epoch] =
       [j \in Judgments |-> @ [j] + IF j \in planned THEN 1 ELSE 0]]
  /\ proof' = [proof EXCEPT ![epoch] = [j \in Judgments |->
       IF j \notin planned THEN @ [j]
       ELSE IF ~authorized THEN "unauthorized"
       ELSE IF ~Recognized(j) THEN "unrecognized"
       ELSE IF truth[j] # "obsolete" THEN truth[j]
       ELSE IF authEpoch # epoch \/ owner # "recheck" THEN "unauthorized"
       ELSE "authorized"]]
  /\ checkedEpoch' = epoch /\ checkedRevision' = revision
  /\ phase' = "idle" /\ owner' = "none" /\ authorized' = FALSE
  /\ UNCHANGED <<epoch, revision, machine, truth, authEpoch,
       readEpoch, readRevision, readTruth, planned, authKind, readHadClosures>>

AbortOrRetry ==
  /\ routeFrozen' = IF authEpoch = epoch THEN routeFrozen ELSE FALSE
  /\ phase \in {"eligible", "read", "locked"}
  /\ phase' = IF authEpoch = epoch THEN "eligible" ELSE "idle"
  /\ owner' = IF owner = "recheck" THEN "none" ELSE owner
  /\ planned' = {}
  /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices, proof,
       authorized, authKind, authEpoch, readEpoch, readRevision, readTruth,
       checkedEpoch, checkedRevision, lastClosed, readHadClosures, idempotentDone, checkedRouteTruth>>

TableEvidenceChanged(evidence) ==
  \E j \in Judgments : Kinds[j] # "route" /\ evidence[j] # truth[j]

ChangeEvidence(evidence) ==
  /\ owner # "recheck" /\ evidence # truth
  /\ ~TableEvidenceChanged(evidence) \/ revision < MaxRevision
  /\ routeFrozen => \A j \in Judgments : Kinds[j] = "route" => evidence[j] = truth[j]
  /\ truth' = evidence
  /\ revision' = IF TableEvidenceChanged(evidence) THEN revision + 1 ELSE revision
  /\ UNCHANGED <<epoch, machine, open, decided, notices, proof,
       authorized, authKind, authEpoch, phase, readEpoch, readRevision, readTruth,
       planned, lastClosed, readHadClosures, idempotentDone, routeFrozen, owner,
       checkedEpoch, checkedRevision, checkedRouteTruth>>

OtherAcquire ==
  /\ owner = "none" /\ owner' = "other"
  /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices, proof,
       authorized, authKind, authEpoch, phase, readEpoch, readRevision, readTruth,
       planned, checkedEpoch, checkedRevision, lastClosed, readHadClosures, idempotentDone, routeFrozen, checkedRouteTruth>>

OtherRelease ==
  /\ owner = "other" /\ owner' = "none"
  /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices, proof,
       authorized, authKind, authEpoch, phase, readEpoch, readRevision, readTruth,
       planned, checkedEpoch, checkedRevision, lastClosed, readHadClosures, idempotentDone, routeFrozen, checkedRouteTruth>>

\* A caller can resume after its abandoned operation has a different owner.
\* The new owner changes no table yet; commit must still prove ownership.
LoseOwner ==
  /\ phase = "locked" /\ owner = "recheck"
  /\ owner' = "other"
  /\ UNCHANGED <<epoch, revision, machine, truth, open, decided, notices, proof,
       authorized, authKind, authEpoch, phase, readEpoch, readRevision, readTruth,
       planned, checkedEpoch, checkedRevision, lastClosed, readHadClosures, idempotentDone, routeFrozen, checkedRouteTruth>>

Clear ==
  /\ idempotentDone' = FALSE
  /\ owner # "recheck" /\ epoch < MaxEpoch
  /\ epoch' = epoch + 1 /\ revision' = 0 /\ machine' = "ready"
  /\ truth' = [j \in Judgments |-> IF Kinds[j] = "route" THEN truth[j] ELSE "live"]
  /\ open' = Judgments
  /\ owner' = "none"
  /\ UNCHANGED <<decided, notices, proof, authorized, authEpoch, phase,
       readEpoch, readRevision, readTruth, planned, checkedEpoch, checkedRevision, lastClosed, authKind, readHadClosures, routeFrozen, checkedRouteTruth>>

Next == StartSuccess \/ ServerStartup \/ RefusedOrDry \/ ReadPlan \/ AcquirePlan
     \/ CommitPlan \/ AbortOrRetry \/ LoseOwner \/ OtherAcquire \/ OtherRelease \/ Clear
     \/ \E e \in [Judgments -> TruthValues] : ChangeEvidence(e)

Spec == Init /\ [][Next]_vars
\* Progress is conditional on a persistent caller and eventual operation-owner
\* release. Strong fairness prevents an adversarial abort from starving every
\* read/acquire/commit opportunity. It is not a success guarantee for a refused
\* start, an unavailable store, or a caller that stops retrying.
FairSpec == Spec /\ SF_vars(ReadPlan) /\ SF_vars(AcquirePlan)
                 /\ SF_vars(CommitPlan) /\ WF_vars(OtherRelease)
                 /\ WF_vars(AbortOrRetry)
RecheckCompletes == (phase # "idle") ~> (phase = "idle")
ClosedOnlyProven == \A e \in 1..MaxEpoch, j \in Judgments :
  decided[e][j] > 0 => proof[e][j] = "authorized"
ExactlyOnce == \A e \in 1..MaxEpoch, j \in Judgments :
  /\ decided[e][j] <= 1 /\ notices[e][j] = decided[e][j]
OpenMatchesHistory == \A j \in Judgments : (j \in open) <=> decided[epoch][j] = 0
NoObsoleteAfterRecheck ==
  (checkedEpoch = epoch /\ checkedRevision = revision
   /\ checkedRouteTruth = [j \in RouteJudgments |-> truth[j]]) => Obsolete(truth, open) = {}
\* Preserve judgments present at this closure event, not newly recurring causes
\* on an old already-closed ID (raising a new episode is outside this pass).
PreservesUnknownAndUnrecognized ==
  (checkedEpoch = epoch /\ checkedRevision = revision
   /\ checkedRouteTruth = [j \in RouteJudgments |-> truth[j]]) =>
    \A j \in lastClosed : truth[j] # "unknown" /\ Recognized(j)
StartupAlwaysEnabled == ENABLED ServerStartup
NoIdempotentRecheckWitness ==
  ~(idempotentDone /\ checkedEpoch = epoch /\ checkedRevision = revision /\ phase = "idle")
NoClosureYet == \A e \in 1..MaxEpoch, j \in Judgments : decided[e][j] = 0
NoMixedClosureWitness ==
  ~(checkedEpoch = epoch /\ checkedRevision = revision
    /\ checkedRouteTruth = [j \in RouteJudgments |-> truth[j]]
    /\ lastClosed # {}
    /\ \E j \in open : truth[j] = "unknown")
NoAlreadyRunningWitness ==
  ~(authKind = "already-running" /\ checkedEpoch = epoch
    /\ checkedRevision = revision /\ phase = "idle" /\ lastClosed # {})
NoStartupWitness ==
  ~(authKind = "startup" /\ checkedEpoch = epoch
    /\ checkedRevision = revision /\ phase = "idle" /\ lastClosed # {})
=============================================================================
