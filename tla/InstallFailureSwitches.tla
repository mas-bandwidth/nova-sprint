----------------------------- MODULE InstallFailureSwitches -----------------------------
EXTENDS Naturals

(***************************************************************************
 Release install protocol, separate from the eight-step Setup walk.
 Layout: private stage -> complete published version directory -> private
 candidate symlink -> atomic rename over the active symlink. Verification
 runs against the published candidate before activation. Previous releases
 are never removed; failed checks leave the candidate available to inspect.

 Assumptions: one installer owns the OS locks, initial old target (when
 present) is complete, candidate bytes do not change behind the installer,
 WriteFile success means all bytes were written, and same-filesystem Rename
 is atomic for process-crash visibility. No fsync occurs in the code: this
 does NOT prove machine/power-loss durability. Download checksum/archive
 validation is abstracted as input to Begin, not cryptography modeled.

 Crash leaves disk state; no automatic retry is modeled. Fair progress only
 means external checks and filesystem calls eventually return. OS-lock
 release/reacquisition is an implementation obligation, not an invented
 recovery protocol. Three-tick adoption, deployment and automatic rollback
 are explicitly outside this model and the current install scope.
***************************************************************************)
CONSTANT HavePrevious
Old == "old"
Candidate == "candidate"
NoActive == "none"
Requested == "requested-version"
Other == "other-version"
InitialActive == IF HavePrevious THEN Old ELSE NoActive
VARIABLES phase, stageBytes, candidateBytes, checked, reported, stagedLink,
          active, previousPresent, failure, checkAtSwitch, versionAtSwitch
vars == <<phase, stageBytes, candidateBytes, checked, reported, stagedLink,
          active, previousPresent, failure, checkAtSwitch, versionAtSwitch>>
Init == /\ phase = "ready" /\ stageBytes = 0 /\ candidateBytes = 0
        /\ checked = FALSE /\ reported = "unread" /\ stagedLink = FALSE
        /\ active = InitialActive /\ previousPresent = HavePrevious
        /\ failure = "none" /\ checkAtSwitch = TRUE /\ versionAtSwitch = TRUE
Begin ==
  /\ phase = "ready"
  /\ phase' = "writing" /\ stageBytes' = 1
  /\ UNCHANGED <<candidateBytes, checked, reported, stagedLink, active,
       previousPresent, failure, checkAtSwitch, versionAtSwitch>>
FinishWrite ==
  /\ phase = "writing"
  /\ stageBytes' = 2 /\ phase' = "publishing"
  /\ UNCHANGED <<candidateBytes, checked, reported, stagedLink, active,
       previousPresent, failure, checkAtSwitch, versionAtSwitch>>
Publish ==
  /\ phase = "publishing"
  /\ candidateBytes' = stageBytes /\ stageBytes' = 0 /\ phase' = "check"
  /\ UNCHANGED <<checked, reported, stagedLink, active, previousPresent,
       failure, checkAtSwitch, versionAtSwitch>>
CheckPass ==
  /\ phase = "check"
  /\ checked' = TRUE /\ phase' = "version"
  /\ UNCHANGED <<stageBytes, candidateBytes, reported, stagedLink, active,
       previousPresent, failure, checkAtSwitch, versionAtSwitch>>
CheckFail ==
  /\ phase = "check"
  /\ phase' = "failed" /\ failure' = "check" /\ active' = Candidate
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, stagedLink,
       previousPresent, checkAtSwitch, versionAtSwitch>>
ReadVersion(v) ==
  /\ phase = "version" /\ v \in {Requested, Other}
  /\ reported' = v
  /\ phase' = IF v = Requested THEN "link" ELSE "failed"
  /\ failure' = IF v = Requested THEN "none" ELSE "version"
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, stagedLink, active,
       previousPresent, checkAtSwitch, versionAtSwitch>>
PrepareLink ==
  /\ phase = "link"
  /\ stagedLink' = TRUE /\ phase' = "switch"
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, active,
       previousPresent, failure, checkAtSwitch, versionAtSwitch>>
Activate ==
  /\ phase = "switch" /\ stagedLink
  /\ active' = Candidate /\ stagedLink' = FALSE /\ phase' = "done"
  /\ checkAtSwitch' = (checkAtSwitch /\ checked)
  /\ versionAtSwitch' = (versionAtSwitch /\ reported = Requested)
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported,
       previousPresent, failure>>
\* Includes cancellation, download/validation failures, filesystem errors,
\* and a non-symlink destination. None changes the active target.
Fail ==
  /\ phase \in {"ready", "writing", "publishing", "version", "link", "switch"}
  /\ phase' = "failed" /\ failure' = "external"
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, stagedLink,
       active, previousPresent, checkAtSwitch, versionAtSwitch>>
Crash ==
  /\ phase \notin {"failed", "crashed"}
  /\ phase' = "crashed"
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, stagedLink,
       active, previousPresent, failure, checkAtSwitch, versionAtSwitch>>
Progress == Begin \/ FinishWrite \/ Publish \/ CheckPass \/ CheckFail
            \/ (\E v \in {Requested, Other} : ReadVersion(v))
            \/ PrepareLink \/ Activate \/ Fail
Next == Progress \/ Crash
Spec == Init /\ [][Next]_vars /\ WF_vars(Progress)
TypeOK == /\ phase \in {"ready","writing","publishing","check","version",
                        "link","switch","done","failed","crashed"}
          /\ stageBytes \in 0..2 /\ candidateBytes \in 0..2
          /\ active \in {Old,Candidate,NoActive}
          /\ checked \in BOOLEAN /\ stagedLink \in BOOLEAN
          /\ previousPresent \in BOOLEAN
          /\ reported \in {"unread",Requested,Other}
CheckBeforeActivation == checkAtSwitch
ExactVersionBeforeActivation == versionAtSwitch
PublishedCandidateComplete == candidateBytes \in {0,2}
ActiveComplete == active = Candidate => candidateBytes = 2
NoActiveGap == HavePrevious => active \in {Old,Candidate}
FailurePreservesActive == phase = "failed" => active = InitialActive
FailedCandidateInspectable == (phase = "failed" /\ failure \in {"check","version"})
                             => candidateBytes = 2
PreviousRetained == HavePrevious => previousPresent
CrashOldOrCompleteNew == phase = "crashed" =>
                 (active = InitialActive \/ (active = Candidate /\ candidateBytes = 2))
EventuallyStopped == <>(phase \in {"done","failed","crashed"})
=============================================================================
