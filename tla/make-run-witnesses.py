#!/usr/bin/env python3
"""Generate layer 6 cfgs and reversed witnesses with exact replacement counts.

No TLC runs here. Invoke TLC on hetzner2 under nova-reserve; see run-layer6.py.
RunLockCheckRace records the accepted gap between the last health check
and committing on a separate store session; it is not a code defect.
"""
from pathlib import Path

HERE = Path(__file__).resolve().parent
RUN_INV = ["TypeOK", "OneChooser", "NoApplyAfterObservedLoss", "AppliedOnce", "NoStaleMove", "BudgetKept",
           "PushAfterCommit", "CursorTrailsSent", "FIFO", "NoDroppedPlan", "EdgesAfterCommit", "NoTickAfterObservedStop", "NoAcceptedTickWhileStopped"]
SETUP_INV = ["TypeOK", "NeverAhead", "OnlyVerified", "AskedOnce", "KeyNeverPrinted", "RestartFromOne"]

def config(model, *, work=3, reports=1, failures=1, budget=2, processes=2, temporal=True):
    if model == "Run":
        constants = ("CONSTANTS\n  Processes = {%s}\n  Recipients = {recipient}\n"
                     "  WorkCount = %d\n  CardCount = 2\n  Budget = %d\n"
                     "  MaxReports = %d\n  MaxFailures = %d\n  AtomicChooser = FALSE\n  MaxStops = 0\n  InitiallyStopped = FALSE\n") % (
                         ", ".join("p%d" % n for n in range(1, processes+1)), work, budget, reports, failures)
        invariants, properties = RUN_INV, ["AllWork", "AllEventsSent"]
    else:
        constants = "CONSTANT MaxDisruptions = 1\n"
        invariants, properties = SETUP_INV, ["EventuallyDone"]
    return constants + "SPECIFICATION Spec\nCHECK_DEADLOCK FALSE\n" + "".join(
        "INVARIANT %s\n" % name for name in invariants) + (
        "".join("PROPERTY %s\n" % name for name in properties) if temporal else "")

# model, name, target, kind, fault, exact replacement pairs
WITNESSES = [
    ("Run", "RunLockCheckRace", "CommitUnderCurrentLock", "invariant",
     "Use an earlier successful health check instead of atomic commit fencing.",
     [("/\\ IF AtomicChooser THEN Active(p) ELSE TRUE", "/\\ TRUE \\* FAULT: prior check only")]),
    ("Run", "RunIgnoresObservedLoss", "NoApplyAfterObservedLoss", "invariant",
     "Retain pending work after observed lock loss and allow its next Apply.",
     [("NoticeLost(p) ==\n  /\\ local[p].phase # \"down\" /\\ ~Active(p)\n  /\\ (local[p].phase # \"hook\" \\/ lock # p)\n  /\\ local' = [local EXCEPT ![p] = Blank]",
       "NoticeLost(p) ==\n  /\\ local[p].phase # \"down\" /\\ ~Active(p)\n  /\\ (local[p].phase # \"hook\" \\/ lock # p)\n  /\\ local' = [local EXCEPT ![p].checked = FALSE]"),
      ("Apply(p) ==\n  /\\ local[p].phase = \"apply\" /\\ local[p].checked",
       "Apply(p) ==\n  /\\ local[p].phase = \"apply\"")]),
    ("Run", "RunFenceDropped", "NoStaleMove", "invariant",
     "Commit a batch without checking the planned card gens against current gens.",
     [("/\\ ~Stale(p,Head(local[p].batch))", "/\\ TRUE \\* FAULT: no gen fence")]),
    ("Run", "RunCursorBeforeSend", "CursorTrailsSent", "invariant",
     "Save the physical log cursor while the send is still in flight.",
     [("SaveCursor(p,r) ==\n  /\\ push[p][r].stage = \"sent\"",
       "SaveCursor(p,r) ==\n  /\\ push[p][r].stage \\in {\"sending\", \"sent\"}")]),
    ("Run", "RunBudgetIgnored", "BudgetKept", "invariant",
     "Select the entire FIFO, including moves beyond this tick's budget.",
     [("n == Min(Budget, Len(q))", "n == Len(q)")]),
    ("Run", "RunCarryDropped", "AllWork", "temporal",
     "Discard the over-budget suffix while remembering those plans as emitted.",
     [("![p].carry = SubSeq(q, n+1, Len(q))", "![p].carry = <<>>")]),
    ("Run", "RunRefusedLost", "AllWork", "temporal",
     "Forget the refused op instead of returning its work for replan.",
     [("![p].putback = Append(local[p].putback, Head(local[p].batch))", "![p].putback = local[p].putback")]),
    ("Run", "RunPushBeforeCommit", "PushAfterCommit", "invariant",
     "Include the next uncommitted event in the physical index range.",
     [("hi |-> Len(events)", "hi |-> Len(events)+1")]),
    ("Run", "RunEdgeBeforeCommit", "EdgesAfterCommit", "invariant",
     "Dispatch an edge without confirming its required operation committed.",
     [("DeliverEdge(j) ==\n  /\\ j \\in done", "DeliverEdge(j) ==\n  /\\ TRUE")]),
    ("Setup", "SetupUnverified", "OnlyVerified", "invariant",
     "Mark a step verified without probing its real postcondition.",
     [("IF world[pc] /\\ Need(pc) \\subseteq answers", "IF TRUE")]),
    ("Setup", "SetupAnswersLost", "AskedOnce", "invariant",
     "Crash forgets already durable answers, causing a second question.",
     [("/\\ UNCHANGED <<world, answers, asked, keySinks, orderOK, verifiedOnly, resumeOK>>",
       "/\\ answers' = {}\n  /\\ UNCHANGED <<world, asked, keySinks, orderOK, verifiedOnly, resumeOK>>")]),
    ("Setup", "SetupResumeSkips", "RestartFromOne", "invariant",
     "A new invocation resumes at step eight instead of rechecking from one.",
     [("/\\ pc' = 1 /\\ verified' = {} /\\ phase' = \"probe\"",
       "/\\ pc' = 8 /\\ verified' = {} /\\ phase' = \"probe\"")]),
    ("Setup", "SetupKeyInArgv", "KeyNeverPrinted", "invariant",
     "The hidden prompt forwards the secret through a command argument.",
     [("keySinks \\cup {\"secrets-stdin\"}", "keySinks \\cup {\"argv\"}")]),
]

def main():
    # Split safety/liveness instances keep the actual enumerations manageable.
    passing = {
        "Run": ("Run", config("Run", failures=0, reports=1, processes=1)),
        "RunFailover": ("Run", config("Run", work=2, failures=1, reports=0)),
        "RunReports": ("Run", config("Run", work=2, reports=3, failures=0, processes=1)),
        "Setup": ("Setup", config("Setup")),
    }
    manifest = []
    for name, (model, cfg) in passing.items():
        (HERE / (name + ".cfg")).write_text(cfg)
        manifest.append({"name": name, "module": model + ".tla", "cfg": name + ".cfg", "expect": "pass"})
    for model, name, target, kind, fault, replacements in WITNESSES:
        text = (HERE / (model + ".tla")).read_text()
        text = text.replace("MODULE " + model + " ", "MODULE " + name + " ", 1)
        first, rest = text.split("\n", 1)
        text = first + "\n\\* REVERSED WITNESS: " + fault + "\n\\* Must violate " + target + ".\n" + rest
        for old, new in replacements:
            assert text.count(old) == 1, (name, old, text.count(old))
            text = text.replace(old, new)
        cfg = config(model, failures=1 if name in {"RunLockCheckRace", "RunIgnoresObservedLoss"} else 0,
                     processes=2 if name == "RunLockCheckRace" else 1, temporal=False)
        cfg = "\n".join(line for line in cfg.splitlines() if not line.startswith("INVARIANT ")) + "\n"
        cfg += ("PROPERTY " if kind == "temporal" else "INVARIANT ") + target + "\n"
        path = HERE / "witnesses" / "layer6"
        path.mkdir(exist_ok=True)
        (path / (name + ".tla")).write_text(text)
        (path / (name + ".cfg")).write_text("\\* Expected " + kind + " violation: " + target + "\n" + cfg)
        manifest.append({"name": name, "module": "witnesses/layer6/" + name + ".tla",
                         "cfg": "witnesses/layer6/" + name + ".cfg", "expect": kind, "target": target})
    import json
    (HERE / "layer6-runs.json").write_text(json.dumps(manifest, indent=2) + "\n")

if __name__ == "__main__":
    main()
