#!/usr/bin/env python3
"""Reproduce two-phase setup configurations and reversed protocol witnesses."""
import json
from pathlib import Path
HERE = Path(__file__).resolve().parent
SOURCE = (HERE / "SetupHandoff.tla").read_text()
INVS = ["TypeOK", "DurableBeforeRecord", "ReleaseHasRecord", "EffectsOrdered", "ValidatedCurrentRecord",
        "RefusalHasNoEffects", "ParentProtocolOnly", "RecordSurvivesCrash"]
def cfg(crashes, invariants, live=False):
    return ("SPECIFICATION Spec\nCONSTANT MaxCrashes = " + str(crashes) +
            "\nCHECK_DEADLOCK FALSE\n" + "".join("INVARIANT " + i + "\n" for i in invariants) +
            ("PROPERTY EventuallyTerminal\n" if live else ""))
def change(source, action, old, new):
    start = source.index(action + " ==\n")
    end = source.find("\n", start)
    while end < len(source)-1:
        end = source.find("\n", end+1)
        if end == -1:
            end = len(source)
            break
        if source[end+1:end+2].isalpha():
            break
    body = source[start:end]
    assert body.count(old) == 1, (action, old, body.count(old))
    return source[:start] + body.replace(old,new,1) + source[end:]
rows = []
for name, crashes in [("SetupHandoff", 0), ("SetupHandoffCrash", 1)]:
    (HERE/(name+".cfg")).write_text(cfg(crashes, INVS, True))
    rows.append(dict(name=name,module="SetupHandoff.tla",cfg=name+".cfg",expect="pass"))
cases = [
 ("SetupHandoffEarlyRecord", "DurableBeforeRecord", [("SaveSchema", "schema' = TRUE", "schema' = FALSE")]),
 ("SetupHandoffNoLease", "EffectsOrdered", [("Acquire2", 'lease\' = "ours"', 'lease\' = "free"')]),
 ("SetupHandoffOldProbe", "EffectsOrdered", [("Verify2", "fresh' = TRUE", "fresh' = FALSE")]),
 ("SetupHandoffWrongSeat", "EffectsOrdered", [("Launch2", 'seat\' = "sprint"', 'seat\' = "admin"')]),
 ("SetupHandoffRefusedEffect", "RefusalHasNoEffects", [("Refuse", "validated, attemptEffect,", "validated,"), ("Refuse", "  /\\ UNCHANGED", "  /\\ attemptEffect' = (lease = \"other\")\n  /\\ UNCHANGED")]),
 ("SetupHandoffParentSecret", "ParentProtocolOnly", [("Release", '{"status", "id"}', '{"status", "id", "secret"}')]),
 ("SetupHandoffLostRecord", "RecordSurvivesCrash", [("Crash", "record' = record\n", 'record\' = "absent"\n')]),
 ("SetupHandoffStaleRecord", "ValidatedCurrentRecord", [("Validate", 'pc = "validate" /\\ ValidRecord', 'pc = "validate"')]),
 ("SetupHandoffWrongType", "ValidatedCurrentRecord", [("SaveRecord", 'record\' = "setup"', 'record\' = "other"'), ("Prevalidate", ' /\\ ValidRecord', ''), ("Validate", 'pc = "validate" /\\ ValidRecord', 'pc = "validate"')]),
 ("SetupHandoffReuseSkips", "EffectsOrdered", [("Reuse", 'pc\' = "prevalidate"', 'pc\' = "effects"')]),
]
for name,target,changes in cases:
    source=SOURCE
    for action,old,new in changes:
        source=change(source,action,old,new)
    source=source.replace("MODULE SetupHandoff ", "MODULE "+name+" ", 1)
    (HERE/(name+".tla")).write_text(source)
    (HERE/(name+".cfg")).write_text(cfg(1,[target]))
    rows.append(dict(name=name,module=name+".tla",cfg=name+".cfg",expect="invariant",target=target))
(HERE/"SetupHandoff-runs.json").write_text(json.dumps(rows,indent=2)+"\n")
