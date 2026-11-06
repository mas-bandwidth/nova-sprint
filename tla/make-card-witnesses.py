#!/usr/bin/env python3
"""Generate the card machine's reversed witnesses and every cfg from CardMachine.tla.

Run from anywhere: python3 tla/make-card-witnesses.py. The same shape as
make-witnesses.py (the store's): each witness is CardMachine.tla with its
module renamed, a header naming the fault, and a few exact text
replacements; a replacement that does not match exactly the expected number
of times stops the script, so a change to CardMachine.tla that a fault no
longer fits fails here instead of producing a witness that tests nothing.
Commit the generated files; CI runs them as they are.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# Every invariant of the passing runs, in cfg order.
INVARIANTS = [
    "TypeOK", "ProducerNeverHeld", "SlotsDense", "WorkingOneWorkConsumer",
    "WorkingLeavesInOneStep", "ParkedUnderHold", "NeverStranded", "ReadOnlyInReview",
    "ReaderNeverAuthor", "WidthKept", "TakeBelowWidth", "NeedFailedTakesDependants",
    "SentinelShape", "NeedsAcyclic", "OrderAcyclic", "AfterNeverHolds",
    "ResultsByKind", "OneLiveReadPerRow",
    "FinalNeverMoves", "AttemptOnlyOnStart", "RedNeverAdvances", "FixKeepsScore",
    "HolderNeverChanges", "OldAttemptNeverMoves", "OnlyHolderActs", "ReadYesNeedsYes",
    "ReadFixNeedsFix", "WithdrawReleasesInOneStep", "WaiveRedNeedsPriorRead", "FixCarriesFailure",
    "ReplaceByLive", "HeadNeverRepeats",
    "FaultSpendsNothing", "BlockNoLive", "EditNoLive", "SentinelLandsByReached", "AfterKept",
]

# The instance of each scenario (see the module header). "life": one
# producer, room for a fix, a Red and a re-deal. "deps": p2 needs p1, s needs
# p2, and the sentinel a put by Add. "pre": p1 and p2 independent, p2 after
# p1; two environment moves, so a blocker priority and a closed row fit in
# one behaviour. "reads": one producer and three rows, for AskReads (with a
# third row in deps it passed 24M states).
SCENARIOS = {
    "life":  {"K": 3, "MaxHead": 2, "MaxAttempt": 2, "MaxBounce": 1, "MaxEnv": 2},
    "deps":  {"K": 2, "MaxHead": 1, "MaxAttempt": 1, "MaxBounce": 1, "MaxEnv": 1},
    "pre":   {"K": 2, "MaxHead": 1, "MaxAttempt": 1, "MaxBounce": 1, "MaxEnv": 2},
    "reads": {"K": 3, "MaxHead": 1, "MaxAttempt": 1, "MaxBounce": 1, "MaxEnv": 1},
}

# The passing runs: cfg name -> scenario.
PASSING = {"CardMachine": "life", "CardDeps": "deps", "CardPreempt": "pre", "CardReads": "reads"}

# Reachability checks on CardMachine.tla: name -> (scenario, what).
REACH = {
    "NeverLanded": ("life", "a producer lands"),
    "NeverFixed": ("life", "a review ends in ReadFix"),
    "NeverRed": ("life", "a merging producer goes red"),
    "NeverBlocked": ("life", "a producer is blocked"),
    "NeverSentinelReached": ("life", "the sentinel lands by Reached"),
    "NeverFaultRedealt": ("life", "a faulted work consumer's producer is started again"),
    "NeverYesAfterRed": ("life", "a yes after a Red, on a new read"),
    "NeverNeedFailed": ("deps", "NeedFailed takes a dependant"),
    "NeverRelinked": ("deps", "a failed need is relinked"),
    "NeverAdded": ("deps", "Add puts a sentinel with a need and an after"),
    "NeverSentinelNeeded": ("deps", "a sentinel is a need"),
    "NeverAskReads": ("reads", "AskReads cuts two reads in one step"),
    "NeverInThisOp": ("deps", "a need is failed and its dependant moved in one op"),
    "NeverWaivedRed": ("life", "WaiveRed merges after a Red without a new read"),
    "NeverFindingCarried": ("life", "a fix attempt carries the failure it fixes"),
    "NeverPreempted": ("pre", "a blocker preempts a live consumer"),
}

# Relink's lines, shared by the two cycle witnesses.
RELINK_TO = "  /\\ to \\in Prods \\ {p}   \\* the code: a need is relinked only to a producer\n"
RELINK_TO_ANY = "  /\\ to \\in P \\ {p}   \\* FAULT: a sentinel target\n"
RELINK_ACYCLIC = ("  /\\ Acyclic([OrderGraph EXCEPT ![p] = (@ \\ {n}) \\cup {to}])"
                  "   \\* needs and after (the code)\n")

# (name, target, scenario, fault, replacements)
WITNESSES = [
    ("CardLandedMoves", "FinalNeverMoves", "life",
     "Rescore has no final check: a landed or fail producer is rescored.",
     [("Rescore(p, s) ==\n  /\\ p \\in Prods\n  /\\ NonFinal(p)\n",
       "Rescore(p, s) ==\n  /\\ p \\in Prods\n  /\\ TRUE   \\* FAULT: was NonFinal(p)\n", 1)]),
    ("CardAuthorReads", "ReaderNeverAuthor", "life",
     "AskRead does not refuse the author: the row that produced the head\n"
     "is asked to read it.",
     [("  /\\ r # ps[p].author\n",
       "  /\\ TRUE   \\* FAULT: was r # ps[p].author\n", 1)]),
    ("CardRedAdvances", "RedNeverAdvances", "life",
     "A red CI result moves merging to landed.",
     [("PStep(\"Red\", p, NoC, [ps[p] EXCEPT !.st = \"review\", !.review = @ + 1])",
       "PStep(\"Red\", p, NoC, [ps[p] EXCEPT !.st = \"landed\"])   \\* FAULT: was review", 1)]),
    ("CardNeedFailedIgnored", "NeedFailedTakesDependants", "deps",
     "NeedFailed reads the dependant's state where it means the need's, so\n"
     "it is never enabled for a live dependant whose need failed.",
     [("  /\\ ps[n].st = \"fail\"\n  /\\ NoLive(p)\n  /\\ PStep(\"NeedFailed\"",
       "  /\\ ps[p].st = \"fail\"   \\* FAULT: was ps[n].st\n  /\\ NoLive(p)\n  /\\ PStep(\"NeedFailed\"", 1)]),
    ("CardNonHolderReports", "OnlyHolderActs", "life",
     "Report does not check the holder: a row that does not hold the\n"
     "consumer reports it.",
     [("  /\\ cs[c].st = \"working\"\n  /\\ cs[c].row = r\n  /\\ res \\in ReportResults(c)\n",
       "  /\\ cs[c].st = \"working\"\n  /\\ TRUE   \\* FAULT: was cs[c].row = r\n"
       "  /\\ res \\in ReportResults(c)\n", 1)]),
    ("CardTwoWorkConsumers", "WorkingOneWorkConsumer", "life",
     "Start is accepted from working too, so a second work consumer is cut\n"
     "while the first is live.",
     [("StartGuard(p, r) ==\n  /\\ p \\in Prods\n  /\\ ps[p].st = \"ready\"\n",
       "StartGuard(p, r) ==\n  /\\ p \\in Prods\n"
       "  /\\ ps[p].st \\in {\"ready\", \"working\"}   \\* FAULT: was ready only\n", 1)]),
    ("CardStaleReport", "OldAttemptNeverMoves", "life",
     "Faulted accepts any work consumer of the producer, not only the one of\n"
     "its current attempt: the first attempt's fault report moves the\n"
     "producer while the second attempt is working.",
     [("  /\\ IsCur(p, c)\n  /\\ cs[c].st = \"done\"\n  /\\ cs[c].res = \"fault\"\n",
       "  /\\ c \\in ConsOf(p) /\\ cs[c].kind = \"work\"   \\* FAULT: was IsCur(p, c)\n"
       "  /\\ cs[c].st = \"done\"\n  /\\ cs[c].res = \"fault\"\n", 1)]),
    ("CardYesOnZero", "ReadYesNeedsYes", "life",
     "ReadYes does not require a done ok read of the current review: a yes\n"
     "fires on zero yes reads.",
     [("  /\\ NoLive(p)\n  /\\ YesRead(p)\n",
       "  /\\ NoLive(p)\n  /\\ TRUE   \\* FAULT: was YesRead(p)\n", 1)]),
    ("CardFaultBumpsAttempt", "FaultSpendsNothing", "life",
     "Faulted raises the producer's attempt.",
     [("PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = Back, !.head = KeptHead(p, c),\n"
       "                                          !.finding = \"fault\"])",
       "PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = Back, !.head = KeptHead(p, c),\n"
       "                                          !.finding = \"fault\", !.att = @ + 1])   \\* FAULT: attempt+1", 1)]),
    ("CardFaultBounces", "FaultSpendsNothing", "life",
     "Faulted raises bounces, so a fault counts toward Block.",
     [("PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = Back, !.head = KeptHead(p, c),\n"
       "                                          !.finding = \"fault\"])",
       "PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = Back, !.head = KeptHead(p, c),\n"
       "                                          !.finding = \"fault\", !.bounces = @ + 1])   \\* FAULT: bounces+1", 1)]),
    ("CardEditUnderLive", "EditNoLive", "life",
     "Edit is accepted while the producer has a live consumer.",
     [("  /\\ NonFinal(p)\n  /\\ NoLive(p)\n  /\\ PStep(\"Edit\"",
       "  /\\ NonFinal(p)\n  /\\ TRUE   \\* FAULT: was NoLive(p)\n  /\\ PStep(\"Edit\"", 1)]),
    ("CardReadyCounted", "TakeBelowWidth", "life",
     "Take counts the row's ready consumers against its width as well as\n"
     "its working ones, so a row below width refuses the take.",
     [("  /\\ Cardinality(WorkingOn(r)) < rs[r].width\n  /\\ CStep(\"Take\"",
       "  \\* FAULT: ready consumers are counted too.\n"
       "  /\\ Cardinality({k \\in CIds : cs[k].st \\in LiveSt /\\ cs[k].row = r}) < rs[r].width\n"
       "  /\\ CStep(\"Take\"", 1)]),
    ("CardNeedsCycle", "NeedsAcyclic", "deps",
     "Relink does not check for a cycle, and takes a sentinel as its target\n"
     "(the producer-only rule alone keeps this instance acyclic): p2's\n"
     "failed need p1 is relinked to the sentinel, which needs p2.",
     [(RELINK_TO, RELINK_TO_ANY, 1),
      (RELINK_ACYCLIC, "  /\\ TRUE   \\* FAULT: no cycle check\n", 1)]),
    ("CardRelinkAfterCycle", "OrderAcyclic", "deps",
     "Relink walks needs only, not after, and takes a sentinel target: p2's\n"
     "failed need p1 is relinked to a, which comes after s, which needs p2.",
     [(RELINK_TO, RELINK_TO_ANY, 1),
      (RELINK_ACYCLIC,
       "  /\\ Acyclic([NeedsGraph EXCEPT ![p] = (@ \\ {n}) \\cup {to}])   \\* FAULT: needs only\n", 1)]),
    ("CardReadReportsFail", "ResultsByKind", "life",
     "A read consumer may report fail, a work consumer's result.",
     [("IF cs[c].kind = \"read\" THEN {\"ok\", \"fault\"} \\cup",
       "IF cs[c].kind = \"read\" THEN {\"ok\", \"fault\", \"fail\"} \\cup   \\* FAULT: fail\n     ", 1)]),
    ("CardReplaceByFinal", "ReplaceByLive", "life",
     "Replace does not require by to be non-final.",
     [("  /\\ by \\in P \\ {p}\n  /\\ NonFinal(by)\n",
       "  /\\ by \\in P \\ {p}\n  /\\ TRUE   \\* FAULT: was NonFinal(by)\n", 1)]),
    ("CardTwoReadsOneRow", "OneLiveReadPerRow", "life",
     "AskRead does not refuse a row that already holds a live read of this\n"
     "head.",
     [("  /\\ ~\\E k \\in LiveOf(p) : cs[k].kind = \"read\" /\\ cs[k].row = r /\\ cs[k].head = ps[p].head\n",
       "  /\\ TRUE   \\* FAULT: a second live read on one row\n", 1)]),
    ("CardPriorRepeated", "HeadNeverRepeats", "life",
     "An ok work report may repeat the attempt's prior head: a fix that\n"
     "pushed nothing new reports ok.",
     [("ELSE IF res = \"ok\" THEN Heads \\ {cs[c].prior} ELSE",
       "ELSE IF res = \"ok\" THEN Heads   \\* FAULT: was Heads \\ {prior}\n       ELSE", 1)]),
    ("CardWithdrawLeavesWorking", "WithdrawReleasesInOneStep", "life",
     "Withdraw of the working producer's work consumer does not release\n"
     "the producer in the same plan: it stays working with no live\n"
     "consumer until a separate Released.",
     [("  /\\ ps'   = ReleasedBy(c, h)\n",
       "  /\\ ps'   = ps   \\* FAULT: was ReleasedBy(c, h)\n", 1)]),
    ("CardReadFixOnZero", "ReadFixNeedsFix", "life",
     "ReadFix does not require a done fix read of the current review: a\n"
     "fix fires on zero fix reads.",
     [("  /\\ NoLive(p)\n  /\\ FixRead(p)\n",
       "  /\\ NoLive(p)\n  /\\ TRUE   \\* FAULT: was FixRead(p)\n", 1)]),
    ("CardHeldStreamReady", "ParkedUnderHold", "life",
     "Broken returns the producer to ready even when its stream is held,\n"
     "instead of writing it parked in the same plan.",
     [("PStep(\"Broken\", p, c, [ps[p] EXCEPT !.st = Back,",
       "PStep(\"Broken\", p, c, [ps[p] EXCEPT !.st = \"ready\",   \\* FAULT: was Back\n"
       "                                        ", 1)]),
    ("CardWaiveRedWithoutRead", "WaiveRedNeedsPriorRead", "life",
     "WaiveRed does not require a done ok read of the previous review at\n"
     "the same head: a producer in review goes to merging on no read.",
     [("  /\\ NoLive(p)\n  /\\ PrevYes(p)\n",
       "  /\\ NoLive(p)\n  /\\ TRUE   \\* FAULT: was PrevYes(p)\n", 1)]),
]


def die(msg):
    sys.stderr.write("make-card-witnesses: " + msg + "\n")
    sys.exit(1)


def header(name, fault, target):
    title = " MODULE " + name + " "
    left = (77 - len(title)) // 2
    line = "-" * left + title + "-" * (77 - len(title) - left)
    lines = ["REVERSED WITNESS. FAULT:"] + fault.split("\n")
    lines += ["TLC must report " + target + " violated. Everything else is",
              "CardMachine.tla unchanged (generated by make-card-witnesses.py)."]
    for l in lines:
        if len(l) > 71:
            die(name + ": header line over 71 characters: " + l)
    box = ["(" + "*" * 75 + ")"]
    box += ["(* " + l.ljust(71) + " *)" for l in lines]
    box += ["(" + "*" * 75 + ")"]
    return line, "\n".join(box)


def constants(scenario):
    s = SCENARIOS[scenario]
    out = "CONSTANT Scenario = \"%s\"\n" % scenario
    for k in ["K", "MaxHead", "MaxAttempt", "MaxBounce", "MaxEnv"]:
        out += "CONSTANT %s = %d\n" % (k, s[k])
    return out + "SPECIFICATION Spec\n"


def cfg(target, scenario):
    out = ["\\* Reversed witness: TLC must report " + target + " violated.",
           "\\* The target invariant is listed first so it is the one TLC names."]
    text = "\n".join(out) + "\n" + constants(scenario)
    text += "".join("INVARIANT " + i + "\n" for i in [target] + [i for i in INVARIANTS if i != target])
    return text


def main():
    with open(os.path.join(HERE, "CardMachine.tla")) as f:
        base = f.read()
    first = base.split("\n", 1)
    if "MODULE CardMachine " not in first[0]:
        die("CardMachine.tla does not start with its module line")
    for i in INVARIANTS:
        if "\n" + i + " " not in base:
            die("invariant " + i + " is not defined in CardMachine.tla")
    for name, target, scenario, fault, reps in WITNESSES:
        if target not in INVARIANTS:
            die(name + ": unknown target " + target)
        text = first[1]
        for old, new, count in reps:
            n = text.count(old)
            if n != count:
                die("%s: expected %d match(es), found %d, for:\n%s" % (name, count, n, old))
            text = text.replace(old, new)
        line, box = header(name, fault, target)
        with open(os.path.join(HERE, "witnesses", name + ".tla"), "w") as f:
            f.write(line + "\n" + box + "\n" + text)
        with open(os.path.join(HERE, "witnesses", name + ".cfg"), "w") as f:
            f.write(cfg(target, scenario))
    for name, scenario in PASSING.items():
        with open(os.path.join(HERE, name + ".cfg"), "w") as f:
            f.write(constants(scenario) + "".join("INVARIANT " + i + "\n" for i in INVARIANTS))
    for name, (scenario, what) in REACH.items():
        if "\n" + name + " " not in base:
            die("reach check " + name + " is not defined in CardMachine.tla")
        with open(os.path.join(HERE, "reach", "Card" + name + ".cfg"), "w") as f:
            f.write("\\* Reachability on CardMachine: TLC must report %s violated (%s).\n" % (name, what)
                    + constants(scenario) + "INVARIANT %s\n" % name)


main()
