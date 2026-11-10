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
    "ReaderNeverAuthor", "WidthKept", "DealIsTake", "NeedFailedTakesDependants",
    "SentinelShape", "NeedsAcyclic", "OrderAcyclic", "PreemptOnBlockerRow",
    "ResultsByKind", "OneLiveReadPerRow", "IdsDistinct", "PlainWithdrawUnguarded",
    "MergeParksResolved", "HeldNeverDealt",
    "FinalNeverMoves", "AttemptOnlyOnStart", "RedNeverAdvances", "FixKeepsScore",
    "HolderNeverChanges", "OldAttemptNeverMoves", "OnlyHolderActs", "ReadYesNeedsYes",
    "ReadFixNeedsFix", "WithdrawReleasesInOneStep", "WaiveRedNeedsPriorRead", "FixCarriesFailure",
    "ReplaceByLive", "HeadNeverRepeats",
    "FaultSpendsNothing", "RouteFaultWaits", "HoldNoLive", "UnholdFreshBudget", "EditNoLive", "SentinelLandsByReached", "WaiveRedNeedsRed", "FencesOnSnapshot",
    "ReviewOnEntry", "FixSaysWhat", "InThisOpOnFailedNeed",
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
    # life, sized for the one witness whose fault needs a third attempt
    # (CardWaiveRedWithoutRed); never a passing run.
    "redw":  {"Scenario": "life", "K": 6, "MaxHead": 2, "MaxAttempt": 3, "MaxBounce": 2, "MaxEnv": 0},
    # life with Merge: ops of up to three moves (MaxOp, read off the
    # scenario in CardMachine.tla), at small bounds.
    "merge": {"K": 2, "MaxHead": 1, "MaxAttempt": 1, "MaxBounce": 1, "MaxEnv": 1},
    # pre with Merge: a preempt Withdraw and a blocker Start in one op.
    "mergepre": {"K": 2, "MaxHead": 1, "MaxAttempt": 1, "MaxBounce": 1, "MaxEnv": 1},
    # deps with Merge: a need failed and its dependant moved InThisOp as two
    # moves of one op (round 7). No environment move: Add and Red are
    # checked in deps.
    "mergedeps": {"K": 2, "MaxHead": 1, "MaxAttempt": 1, "MaxBounce": 1, "MaxEnv": 0},
}

# The passing runs: cfg name -> scenario.
PASSING = {"CardMachine": "life", "CardDeps": "deps", "CardPreempt": "pre", "CardReads": "reads", "CardMerge": "merge", "CardMergePre": "mergepre",
           "CardMergeDeps": "mergedeps"}

# Reachability checks on CardMachine.tla: name -> (scenario, what).
REACH = {
    "NeverLanded": ("life", "a producer lands"),
    "NeverFixed": ("life", "a review ends in ReadFix"),
    "NeverRed": ("life", "a merging producer goes red"),
    "NeverHeld": ("life", "a producer waits on a hold"),
    "NeverUnheldBounced": ("life", "a producer held after a bounce is unheld with a fresh budget"),
    "NeverSentinelReached": ("life", "the sentinel lands by Reached"),
    "NeverFaultRedealt": ("life", "a faulted work consumer's producer is started again"),
    "NeverRouteHeld": ("life", "a fault naming a missing route leaves its producer waiting on it"),
    "NeverRedToFix": ("life", "a red sends a merging producer to fix with the finding"),
    "NeverNeedFailed": ("deps", "NeedFailed takes a dependant"),
    "NeverRelinked": ("deps", "a failed need is relinked"),
    "NeverAdded": ("deps", "Add puts a sentinel with a need and an after"),
    "NeverSentinelNeeded": ("deps", "a sentinel is a need"),
    "NeverAskReads": ("reads", "AskReads cuts two reads in one step"),
    "NeverInThisOp": ("deps", "a need is failed and its dependant moved in one op"),
    "NeverWaivedRed": ("life", "WaiveRed merges after a Red without a new read"),
    "NeverFindingCarried": ("life", "a fix attempt carries the reader's finding"),
    "NeverPreempted": ("pre", "a blocker preempts a live consumer"),
    "NeverInThisOpMerged": ("mergedeps", "a need fails and a later move of the op moves its dependant InThisOp"),
}

# Relink's lines, shared by the two cycle witnesses.
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
     [("PStep(\"Red\", p, NoC, [ps[p] EXCEPT !.st = Back, !.review = @ + 1,\n"
       "                                      !.red = TRUE, !.finding = \"fix\"])",
       "PStep(\"Red\", p, NoC, [ps[p] EXCEPT !.st = \"landed\"])   \\* FAULT: was Back", 1)]),
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
     [("  /\\ IsCur(p, c)\n  /\\ cs[c].st = \"done\"\n  /\\ cs[c].res \\in {\"fault\", \"route\"}\n",
       "  /\\ c \\in ConsOf(p) /\\ cs[c].kind = \"work\"   \\* FAULT: was IsCur(p, c)\n"
       "  /\\ cs[c].st = \"done\"\n  /\\ cs[c].res \\in {\"fault\", \"route\"}\n", 1)]),
    ("CardYesOnZero", "ReadYesNeedsYes", "life",
     "ReadYes does not require a done ok read of the current review: a yes\n"
     "fires on zero yes reads.",
     [("  /\\ NoLive(p)\n  /\\ YesRead(p)\n",
       "  /\\ NoLive(p)\n  /\\ TRUE   \\* FAULT: was YesRead(p)\n", 1)]),
    ("CardFaultBumpsAttempt", "FaultSpendsNothing", "life",
     "Faulted raises the producer's attempt.",
     [("PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = IF cs[c].res = \"route\" THEN \"waiting\" ELSE Back,\n"
       "                                          !.why = IF cs[c].res = \"route\" THEN \"route\" ELSE \"-\",\n"
       "                                          !.head = KeptHead(p, c)])",
       "PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = IF cs[c].res = \"route\" THEN \"waiting\" ELSE Back,\n"
       "                                          !.why = IF cs[c].res = \"route\" THEN \"route\" ELSE \"-\",\n"
       "                                          !.head = KeptHead(p, c),\n"
       "                                          !.att = @ + 1])   \\* FAULT: attempt+1", 1)]),
    ("CardFaultBounces", "FaultSpendsNothing", "life",
     "Faulted raises bounces, so a fault counts toward the bounce limit.",
     [("PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = IF cs[c].res = \"route\" THEN \"waiting\" ELSE Back,\n"
       "                                          !.why = IF cs[c].res = \"route\" THEN \"route\" ELSE \"-\",\n"
       "                                          !.head = KeptHead(p, c)])",
       "PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = IF cs[c].res = \"route\" THEN \"waiting\" ELSE Back,\n"
       "                                          !.why = IF cs[c].res = \"route\" THEN \"route\" ELSE \"-\",\n"
       "                                          !.head = KeptHead(p, c),\n"
       "                                          !.bounces = @ + 1])   \\* FAULT: bounces+1", 1)]),
    ("CardRouteRedealt", "RouteFaultWaits", "life",
     "A fault naming a missing route goes back to ready like any fault: the\n"
     "producer is dealt again to a row of the same kind (defect 48).",
     [("PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = IF cs[c].res = \"route\" THEN \"waiting\" ELSE Back,\n"
       "                                          !.why = IF cs[c].res = \"route\" THEN \"route\" ELSE \"-\",\n"
       "                                          !.head = KeptHead(p, c)])",
       "PStep(\"Faulted\", p, c, [ps[p] EXCEPT !.st = Back,   \\* FAULT: was waiting on a route\n"
       "                                          !.head = KeptHead(p, c)])", 1)]),
    ("CardHeldDealt", "HeldNeverDealt", "life",
     "Resolve does not refuse a held producer: the deal takes a card the\n"
     "coordinator holds, or one waiting on a missing route, as soon as its\n"
     "needs land (Glenn, 2026-11-07: a card with a problem waits; a hold\n"
     "is a reason on waiting that the deal refuses).",
     [("  /\\ ps[p].why = \"-\"   \\* the code: \"the deal never takes a held card\"\n",
       "  /\\ TRUE   \\* FAULT: was ps[p].why = \"-\"\n", 1)]),
    ("CardUnholdBudgetSpent", "UnholdFreshBudget", "life",
     "UnholdCard leaves the bounce base where it was: a producer held at\n"
     "the bounce limit is unheld with its budget still spent, so the limit\n"
     "holds it again on the next tick (live 2026-11-08: gen 33 to 36, back\n"
     "to \"held: bounced 12 times, the limit is 12\"; the only way out was\n"
     "drop and re-add).",
     [("[ps[p] EXCEPT !.why = \"-\", !.base = ps[p].bounces])",
       "[ps[p] EXCEPT !.why = \"-\"])   \\* FAULT: was also !.base = ps[p].bounces;", 1)]),
    ("CardEditUnderLive", "EditNoLive", "life",
     "Edit is accepted while the producer has a live consumer.",
     [("  /\\ NonFinal(p)\n  /\\ NoLive(p)\n  /\\ PStep(\"Edit\"",
       "  /\\ NonFinal(p)\n  /\\ TRUE   \\* FAULT: was NoLive(p)\n  /\\ PStep(\"Edit\"", 1)]),
    ("CardCutReady", "DealIsTake", "life",
     "Start cuts its work consumer ready, as before defect 52: the card\n"
     "waits on its row's session to take it.",
     [("StartCut(p, r) ==\n  [st |-> \"working\", kind |-> \"work\",",
       "StartCut(p, r) ==\n  [st |-> \"ready\", kind |-> \"work\",   \\* FAULT: was working", 1)]),
    ("CardDealOverWidth", "WidthKept", "pre",
     "Start does not check the row's width: with the deal the take, p2's\n"
     "Start on the row p1's work holds puts it over width 1.",
     [("  /\\ StartGuard(p, r)\n  /\\ RoomOn(r, {})\n",
       "  /\\ StartGuard(p, r)\n  /\\ TRUE   \\* FAULT: was RoomOn(r, {})\n", 1)]),
    ("CardNeedsCycle", "NeedsAcyclic", "deps",
     "Relink does not check for a cycle: p2's failed need p1 is relinked\n"
     "to the sentinel, which needs p2 (a sentinel target is the rule since\n"
     "round 4).",
     [(RELINK_ACYCLIC, "  /\\ TRUE   \\* FAULT: no cycle check\n", 1)]),
    ("CardRelinkAfterCycle", "OrderAcyclic", "deps",
     "Relink walks needs only, not after: p2's failed need p1 is relinked\n"
     "to a, which comes after s, which needs p2 (a sentinel target is the\n"
     "rule since round 4).",
     [(RELINK_ACYCLIC,
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
     "An ok work report may repeat the attempt's prior head when an\n"
     "earlier attempt finished at it: a fix that pushed nothing new\n"
     "reports ok.",
     [("ELSE IF res = \"ok\" THEN Heads \\ ({cs[c].prior} \\cap FinishedHeads(c[1])) ELSE",
       "ELSE IF res = \"ok\" THEN Heads   \\* FAULT: was Heads \\ finished prior\n       ELSE", 1)]),
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
     "the same head: a producer a Red sent to fix goes to merging on no read.",
     [("  /\\ PrevYes(p)\n  /\\ PStep(\"WaiveRed\"",
       "  /\\ TRUE   \\* FAULT: was PrevYes(p)\n  /\\ PStep(\"WaiveRed\"", 1)]),
    ("CardWaiveRedWithoutRed", "WaiveRedNeedsRed", "redw",
     "WaiveRed does not require the Red to be recorded: a producer back in\n"
     "ready by a second ReadFix at an already-read head goes to merging.",
     [("  /\\ ps[p].red   \\* the code: \"no red on review N\"\n",
       "  /\\ TRUE   \\* FAULT: was ps[p].red\n", 1)]),
    ("CardMergeCutUnseen", "HoldNoLive", "merge",
     "Merge: a consumer cut earlier in the op is invisible to the next\n"
     "move (overlayCons before round 6): AskRead then HoldCard in one op,\n"
     "and HoldCard's no-live check does not see the read just cut.",
     [("  /\\ NoLive(p)\n  /\\ PStep(\"HoldCard\"",
       "  /\\ \\A k \\in LiveOf(p) : op > 0 /\\ snap.cs[k].st = \"none\"   \\* FAULT: cut unseen\n"
       "  /\\ PStep(\"HoldCard\"", 1)]),
    ("CardMergeNestedHold", "ParkedUnderHold", "merge",
     "Merge: a release reads the stream from the snapshot, not from what a\n"
     "hold earlier in the op (nested or not) left: HoldStream then Withdraw\n"
     "in one op leaves the producer ready in a held stream.",
     [("THEN [ps EXCEPT ![c[1]].st = Back,",
       "THEN [ps EXCEPT ![c[1]].st = IF (IF op > 0 THEN snap.held ELSE held)   \\* FAULT\n"
       "                             THEN \"waiting\" ELSE \"ready\",", 1)]),
    ("CardMergeNoRerun", "FinalNeverMoves", "merge",
     "Merge: a merged plan merged again is kept as built on the snapshot,\n"
     "not run again on the earlier plans' writes (before round 6): Cancel\n"
     "then a merged Rescore in one op rescores the failed card.",
     [("  /\\ p \\in Prods\n  /\\ NonFinal(p)\n  /\\ s # ps[p].score\n",
       "  /\\ p \\in Prods\n"
       "  /\\ (IF op > 0 THEN snap.ps[p].st ELSE ps[p].st) \\notin PFinal \\cup {\"absent\"}   \\* FAULT\n"
       "  /\\ s # ps[p].score\n", 1)]),
    ("CardMergeOwnWrites", "FencesOnSnapshot", "merge",
     "Merge: a merged plan's read set includes its own writes: a later move\n"
     "keeps its condition on a state an earlier move of the op wrote, read\n"
     "after that write. Resolve then Edit fences p1 ready against a\n"
     "snapshot where it is waiting, and the store refuses the op.",
     [("PFence(p) == IF op > 0 /\\ ps[p].st # snap.ps[p].st THEN {} ELSE {ps[p].st}",
       "PFence(p) == {ps[p].st}   \\* FAULT: was keepExpects", 1)]),
    ("CardMergeNestedStart", "PreemptOnBlockerRow", "mergepre",
     "Merge: the preempt guard sees only the top-level plans, so a blocker\n"
     "Start nested in a merged plan skips it: a preempt Withdraw on one row\n"
     "and the blocker's Start on another row are one op.",
     [("     IN /\\ PreemptGuard(pw2, bs2)\n",
       "     IN /\\ TRUE   \\* FAULT: was PreemptGuard(pw2, bs2)\n", 1)]),
    # Round 7 (the rulings after the fifth cold read).
    ("CardDottedId", "NeverStranded", "deps",
     "Add takes an id with a dot: the sentinel p1.w1 is added before p1\n"
     "starts, and p1's first work consumer can never be cut under its id\n"
     "(fenced absent), so p1 is stranded in ready (ruling 1).",
     [("  /\\ p \\notin Dotted   \\* round 7, ruling 1: an id may not contain \".\"\n",
       "  /\\ TRUE   \\* FAULT: was p \\notin Dotted\n", 1)]),
    ("CardInThisOpAlone", "InThisOpOnFailedNeed", "mergedeps",
     "InThisOp is taken alone, with no earlier move of the op failing the\n"
     "need: the dependant moves on a need that is not fail (ruling 2).",
     [("  /\\ InThisOpFolded(n)\n",
       "  /\\ TRUE   \\* FAULT: was InThisOpFolded(n)\n", 1)]),
    ("CardPlainWithdrawGuarded", "PlainWithdrawUnguarded", "mergepre",
     "The preempt guard runs on every Withdraw (read off its reason), not\n"
     "only those flagged preempt: a plain Withdraw on another row cannot\n"
     "merge with a blocker's Start (rulings 3 and 8).",
     [("PlainRows == {}\n",
       "PlainRows == {cs[k].row : k \\in {x \\in CIds : Live(x) /\\ cs'[x].st = \"fail\"}}   \\* FAULT\n", 1)]),
    ("CardMergeSecondWrite", "MergeParksResolved", "merge",
     "Merge refuses a second write to a field an earlier move of the op\n"
     "wrote: Merge(Resolve, HoldStream) is refused instead of parking what\n"
     "the Resolve readied (ruling 4).",
     [("HoldStream ==\n  /\\ ~held\n",
       "HoldStream ==\n  /\\ ~held\n"
       "  /\\ \\A q \\in P : (op > 0 /\\ ps[q].st = \"ready\") => snap.ps[q].st = \"ready\"   \\* FAULT\n", 1)]),
    ("CardReadFixEmpty", "FixSaysWhat", "life",
     "ReadFix takes an empty finding and keeps the old one: the fix read\n"
     "does not say what to fix (ruling 6).",
     [("                                            !.finding = \"fix\"])",
       "                                            !.finding = @])   \\* FAULT: was \"fix\"", 1)]),
    ("CardFailRepeatsPrior", "HeadNeverRepeats", "life",
     "A fail report may name the attempt's prior head: the head given is\n"
     "checked for ok only, not for every result (ruling 6).",
     [("ELSE {0} \\cup (Heads \\ {cs[c].prior})",
       "ELSE {0} \\cup Heads   \\* FAULT", 1)]),
    ("CardMergeReviewTwice", "ReviewOnEntry", "merge",
     "Merge folds an earlier move's writes twice: Report then Finished in\n"
     "one op raises the review counter by two (ruling 7).",
     [("                                           !.author = cs[c].row, !.review = @ + 1,",
       "                                           !.author = cs[c].row,\n"
       "                                           !.review = @ + (IF op > 0 THEN 2 ELSE 1),   \\* FAULT", 1)]),
    ("CardHandPlanPartial", "AttemptOnlyOnStart", "merge",
     "A hand-built plan in a merge writes a partial record, its missing\n"
     "fields at their zero values, not folded onto the snapshot's record:\n"
     "Start then SetPriority in one op resets the attempt (ruling 9).",
     [("  /\\ PStep(\"SetPriority\", p, NoC, [ps[p] EXCEPT !.prio = l])",
       "  /\\ PStep(\"SetPriority\", p, NoC, [(IF op > 0 THEN NewProd(p) ELSE ps[p]) EXCEPT !.prio = l])   \\* FAULT", 1)]),
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
    out = "CONSTANT Scenario = \"%s\"\n" % s.get("Scenario", scenario)
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
