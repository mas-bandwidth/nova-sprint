#!/usr/bin/env python3
"""Generate small install configurations and deliberately reversed witnesses."""
import json
from pathlib import Path
HERE = Path(__file__).resolve().parent
source = (HERE / "Install.tla").read_text()
invariants = ["TypeOK", "CheckBeforeActivation", "ExactVersionBeforeActivation",
    "PublishedCandidateComplete", "ActiveComplete", "NoActiveGap",
    "FailurePreservesActive", "FailedCandidateInspectable", "PreviousRetained",
    "CrashOldOrCompleteNew"]
def config(previous, names, live=False):
    return ("SPECIFICATION Spec\nCONSTANT HavePrevious = " + str(previous).upper() +
            "\nCHECK_DEADLOCK FALSE\n" + "".join("INVARIANT " + n + "\n" for n in names) +
            ("PROPERTY EventuallyStopped\n" if live else ""))
rows = []
for name, previous in [("Install", True), ("InstallFresh", False)]:
    (HERE / (name + ".cfg")).write_text(config(previous, invariants, True))
    rows.append(dict(name=name, module="Install.tla", cfg=name+".cfg", expect="pass"))
cases = [
    ("InstallCheckSkipped", "CheckBeforeActivation",
     "checked' = TRUE /\\ phase' = \"version\"", "checked' = FALSE /\\ phase' = \"version\""),
    ("InstallVersionIgnored", "ExactVersionBeforeActivation",
     "phase' = IF v = Requested THEN \"link\" ELSE \"failed\"", "phase' = \"link\""),
    ("InstallPartialPublished", "PublishedCandidateComplete",
     "stageBytes' = 2 /\\ phase' = \"publishing\"", "stageBytes' = 1 /\\ phase' = \"publishing\""),
    ("InstallFailureSwitches", "FailurePreservesActive",
     r"""CheckFail ==
  /\ phase = "check"
  /\ phase' = "failed" /\ failure' = "check"
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, stagedLink,
       active, previousPresent, checkAtSwitch, versionAtSwitch>>""",
     r"""CheckFail ==
  /\ phase = "check"
  /\ phase' = "failed" /\ failure' = "check" /\ active' = Candidate
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, stagedLink,
       previousPresent, checkAtSwitch, versionAtSwitch>>"""),
    ("InstallUnlinksFirst", "NoActiveGap",
     r"""  /\ stagedLink' = TRUE /\ phase' = "switch"
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, active,
       previousPresent, failure, checkAtSwitch, versionAtSwitch>>""",
     r"""  /\ stagedLink' = TRUE /\ phase' = "switch" /\ active' = NoActive
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported,
       previousPresent, failure, checkAtSwitch, versionAtSwitch>>"""),
    ("InstallDeletesPrevious", "PreviousRetained",
     r"""  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported,
       previousPresent, failure>>""",
     r"""  /\ previousPresent' = FALSE
  /\ UNCHANGED <<stageBytes, candidateBytes, checked, reported, failure>>"""),
]
for name, target, old, new in cases:
    assert source.count(old) == 1, (name, source.count(old))
    module = source.replace("MODULE Install ", "MODULE " + name + " ", 1).replace(old, new, 1)
    (HERE / (name + ".tla")).write_text(module)
    (HERE / (name + ".cfg")).write_text(config(True, [target]))
    rows.append(dict(name=name, module=name+".tla", cfg=name+".cfg", expect="invariant", target=target))
(HERE / "Install-runs.json").write_text(json.dumps(rows, indent=2) + "\n")
