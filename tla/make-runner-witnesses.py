#!/usr/bin/env python3
"""Generate the runner model's reversed witnesses from Runner.tla.

The same shape as make-witnesses.py: each witness is Runner.tla with its module
renamed and a few exact text replacements; a replacement that does not match
exactly once stops the script, so a change to Runner.tla that a fault no
longer fits fails here instead of producing a witness that tests nothing.
Run by gen-witnesses.sh; nothing it writes is committed.
"""
import os

HERE = os.path.dirname(os.path.abspath(__file__))

# Where the stale-push actions go: before the next holder's push.
HOLDER_PUSH = "\\* The card's next holder, on another row, pushes its branch.\nHolderPush(c) =="
NEXT_RETIRE = "       \\/ Retire(c)\n"
STALE_PUSH = ("StalePush(c) ==\n  /\\ ~env.crashed\n  /\\ pass = \"planning\"\n  /\\ storeState[c] = \"fail\"\n"
              "  /\\ jobDir[c].exists\n%s  /\\ branch[c].finalPushes < 2\n"
              "  /\\ branch' = [branch EXCEPT ![c].tip = \"job\", ![c].finalPushes = @ + 1]\n"
              "  /\\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass>>\n\n")


# The 2026-11-09 fault: a staged job that finds every slot held gives its card back.
NO_SLOT_GIVE_BACK = ("NoSlotGiveBack(c) ==\n  /\\ ~env.crashed\n  /\\ ~env.storeDown\n  /\\ storeState[c] = \"working\"\n"
                     "  /\\ jobState[c] = \"staged\"\n  /\\ harnessAlive[c] = 0\n  /\\ slotsInUse = MaxSlots\n"
                     "  /\\ storeState' = [storeState EXCEPT ![c] = \"fail\"]\n"
                     "  /\\ env' = [env EXCEPT !.returned = @ \\cup {c}]\n"
                     "  /\\ UNCHANGED <<jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, pass, branch>>\n\n")


def stale_push(lease, extra=""):
    body = STALE_PUSH % ("  /\\ branch[c].tip # \"holder\"\n" if lease else "")
    return [(HOLDER_PUSH, extra + body + HOLDER_PUSH), (NEXT_RETIRE, NEXT_RETIRE + "       \\/ StalePush(c)\n")]


# Invariants a fault breaks by construction, so TLC would name them first: left out of the cfg.
DROPPED = {
    "RunnerWithdrawnPushes": ["BranchOnlyByHolder"],
    "RunnerForcePushRewinds": ["NoPushAfterFinal"],
    "RunnerWithdrawnPushesRepeatedly": ["NoPushAfterFinal", "BranchOnlyByHolder"],
}
# The stale push the three above add is a push with no seal step: PushOnlySealed is not theirs to name;
# and it moves the tip after the next holder's take: neither is HolderStartsFromTip.
for _name in DROPPED:
    DROPPED[_name] = DROPPED[_name] + ["PushOnlySealed", "HolderStartsFromTip"]
# The kept hand is the fault: the restart's state already breaks RestartReturnsHand, before any report.
DROPPED["RunnerRestartResumes"] = ["RestartReturnsHand"]

# name, target invariant, fault, exact replacement pairs
WITNESSES = [
    ("RunnerReusedJobDir", "JobDirNeverReused", "A job directory is created twice.",
     [("![c].exists = TRUE, ![c].creates = @ + 1]\n", "![c].exists = TRUE, ![c].creates = 2]\n")]),
    ("RunnerSlotFreedEarly", "HarnessInsideSlot", "A harvest frees a slot it did not take.",
     [("slotsInUse' = slotsInUse - 1   \\* slot freed only when harness ends", "slotsInUse' = slotsInUse")]),
    ("RunnerDieClassifiedFail", "FaultSpendsNoAttemptOrBounce", "A harness death is a fail that bounces.",
     [("![c].outcome = \"fault\", ![c].bounced = FALSE,", "![c].outcome = \"fault\", ![c].bounced = TRUE,")]),
    ("RunnerFreshResultIDOnRetry", "ReportRecordedAtMostOnce", "A retried report takes a fresh result id.",
     [("![c].resId = id]   \\* keep same result id on retry", "![c].resId = id + 2]")]),
    ("RunnerCleanBeforeReport", "CleanOnlyAfterRecordedAndPushed", "Clean runs before the report is recorded.",
     [("/\\ jobState[c] = \"recorded\"   \\* clean only after report is recorded and head pushed\n  /\\ storeState[c] \\in {\"done\", \"fail\"}\n",
       "/\\ jobState[c] = \"reporting\"   \\* FAULT: clean before the report is recorded\n")]),
    ("RunnerRestartSpawnsSecondHarness", "OneLiveHarnessPerJobDir", "Restart starts a second harness for a working consumer.",
     [("/\\ harnessAlive' = harnessAlive   \\* a harness left running",
       "/\\ harnessAlive' = [c \\in Consumers |-> IF storeState[c] = \"working\" /\\ jobDir[c].exists THEN 2 ELSE harnessAlive[c]]   \\* a harness left running")]),
    ("RunnerPassWaitsOnJobs", "PassNeverWaitsOnJobs", "A pass ends only when its jobs have (the wake loop that waited).",
     [("PassEnd ==\n  /\\ ~env.crashed\n", "PassEnd ==\n  /\\ \\A c \\in Consumers : harnessAlive[c] = 0\n  /\\ ~env.crashed\n")]),
    ("RunnerStopIsAFault", "StopIsNeverAFault", "The worker's stop is reported as a fault.",
     [("[jobDir[c] EXCEPT !.pid = 0, !.stopped = TRUE,", "[jobDir[c] EXCEPT !.pid = 0, !.stopped = TRUE, !.outcome = \"fault\",")]),
    ("RunnerTreeOutlivesItsHarness", "NoProcessOutlivesItsJob", "A job's process outlives its harness (KillGroup reached nova-reserve's group only).",
     [("Ended(c) == FALSE", "Ended(c) == jobDir[c].tree")]),
    ("RunnerPushUnsealed", "PushOnlySealed", "A job's commits are pushed from a tree the seal step never ran on.",
     [("  /\\ jobDir[c].sealed   \\* pushed from the worker's clone of the sealed tree\n", ""),
      ("![c].outcome = \"ok\", ![c].sealed = TRUE, ![c].atPrior = np,", "![c].outcome = \"ok\", ![c].atPrior = np,")]),
    ("RunnerNoSlotReleases", "NoSlotNeverReleases", "A staged job that finds every slot held gives its card back.",
     [(HOLDER_PUSH, NO_SLOT_GIVE_BACK + HOLDER_PUSH), (NEXT_RETIRE, NEXT_RETIRE + "       \\/ NoSlotGiveBack(c)\n")]),
    ("RunnerRestartResumes", "ReturnedNeverReports",
     "A restart keeps the row's hand and resumes its jobs (the rule before 2026-11-10): a job reports for a card its row was to give up.",
     [("        /\\ storeState' = [c \\in Consumers |-> IF c \\in hand THEN \"fail\" ELSE storeState[c]]   \\* the hand, returned\n",
       "        /\\ storeState' = storeState   \\* the hand kept, its jobs resumed\n")]),
    ("RunnerRebaseDropsPrior", "PushKeepsPrior", "The stream step rebases the job's head, dropping the history it started from.",
     [("/\\ UNCHANGED <<storeState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass, branch>>   \\* the stream merged in: keepsPrior",
       "/\\ jobDir' = [jobDir EXCEPT ![c].keepsPrior = FALSE]\n  /\\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass, branch>>")]),
    ("RunnerWithdrawnPushes", "NoPushAfterFinal", "A withdrawn job's directory pushes its head by lease on every pass.",
     stale_push(True)),
    ("RunnerForcePushRewinds", "BranchOnlyByHolder", "A withdrawn job pushes its stale head with a plain force, rewinding the new holder.",
     stale_push(False)),
    ("RunnerWithdrawnPushesRepeatedly", "AtMostOneStalePush", "A withdrawn job's stale push happens on more than one pass.",
     stale_push(False, "AtMostOneStalePush ==\n  \\A c \\in Consumers : branch[c].finalPushes <= 1\n\n")),
    ("RunnerTakeFromStorePrior", "HolderStartsFromTip",
     "The next holder's take reads only the store's prior head, which a withdraw after a pushed harvest never learned.",
     [("TakePrior(c) == IF branch[c].tip # \"none\" THEN branch[c].tip ELSE branch[c].known",
       "TakePrior(c) == branch[c].known")]),
    ("RunnerPriorHeadOnlyFails", "NothingNewOnlyFinished",
     "An ok at the prior head is failed whatever the deal (the rule before 2026-11-11): an attempt that found its work pushed by one that died can only fail.",
     [("  /\\ ~(jobDir[c].atPrior /\\ jobDir[c].prior = \"finished\")   \\* nothing new over a finished head is NothingNew\n", "  /\\ ~jobDir[c].atPrior\n"),
      ("  /\\ jobDir[c].atPrior /\\ jobDir[c].prior = \"finished\"   \\* only at a finished head\n", "  /\\ jobDir[c].atPrior\n")]),
]


def main():
    with open(os.path.join(HERE, "Runner.tla")) as f:
        base = f.read()
    with open(os.path.join(HERE, "Runner.cfg")) as f:
        cfg = [l for l in f.read().splitlines() if not l.startswith("PROPERTY ")]
    for name, target, fault, replacements in WITNESSES:
        text = base.replace("MODULE Runner ", "MODULE " + name + " ", 1)
        for old, new in replacements:
            if text.count(old) != 1:
                raise SystemExit("make-runner-witnesses: %s: %r matches %d times" % (name, old, text.count(old)))
            text = text.replace(old, new)
        lines = ["\\* Reversed witness: TLC must report %s violated (%s)." % (target, fault), "\\* The target invariant is listed first so it is the one TLC names."]
        lines += [l for l in cfg if not l.startswith("INVARIANT") and l]
        lines += ["INVARIANT " + target] + [l for l in cfg if l.startswith("INVARIANT") and l != "INVARIANT " + target
                                         and l.split()[1] not in DROPPED.get(name, [])]
        with open(os.path.join(HERE, "witnesses", name + ".tla"), "w") as f:
            f.write(text)
        with open(os.path.join(HERE, "witnesses", name + ".cfg"), "w") as f:
            f.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
