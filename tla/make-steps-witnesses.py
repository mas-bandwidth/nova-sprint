#!/usr/bin/env python3
"""Generate the step model's reversed witnesses and every cfg from Steps.tla.

Run from anywhere: python3 tla/make-steps-witnesses.py. The same shape as
make-witnesses.py and make-card-witnesses.py: each witness is Steps.tla with
its module renamed, a header naming the fault, and a few exact text
replacements; a replacement that does not match exactly the expected number
of times stops the script, so a change to Steps.tla that a fault no longer
fits fails here instead of producing a witness that tests nothing. Commit
the generated files; CI runs them as they are.

A witness's target is the first INVARIANT (or, for the liveness witness,
the first line, a PROPERTY) of its cfg, which ci-tlc.sh requires to be the
one TLC reports.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# Every invariant of the passing runs, in cfg order.
INVARIANTS = [
    "TypeOK", "NeverDealtTwice", "WidthKept", "ReadyNeverFlipsWithLoad", "NeverOverlapStarted",
    "NeverBelowTier", "CarryFIFO", "EdgeAfterAccepted",
    "OneGroupPerCard", "GroupWhole", "NeverHalfApplied", "RefusalChangesNothing",
    "RefusedOnlyByRace", "BudgetKept", "BlockerPreemptsInOneTick", "PreemptYoungest",
    "PolicyFailsToDefault", "AfterKept", "AfterNeverHolds",
]
PROPERTIES = ["ReadyDealtWhenRoom"]

CONST_ORDER = ["K", "MaxAtt", "MaxBounce", "MaxEnv", "MaxArrive", "Budget", "Pack"]

# The instance of each scenario (see the module header).
SCENARIOS = {
    "pre":   {"K": 2, "MaxAtt": 2, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 2, "Budget": 2, "Pack": 2},
    "deal":  {"K": 4, "MaxAtt": 2, "MaxBounce": 1, "MaxEnv": 1, "MaxArrive": 1, "Budget": 2, "Pack": 2},
    "land":  {"K": 2, "MaxAtt": 1, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 3, "Budget": 2, "Pack": 2},
    "burst": {"K": 1, "MaxAtt": 1, "MaxBounce": 0, "MaxEnv": 0, "MaxArrive": 4, "Budget": 1, "Pack": 1},
    # pre packed one group per op: a tick of several ops, so an op reads
    # cards an earlier op of the same tick wrote (the predicted fence).
    "pre1":  {"Scenario": "pre", "K": 2, "MaxAtt": 2, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 2,
              "Budget": 2, "Pack": 1},
}

# The passing runs: cfg name -> scenario. Each checks every invariant and,
# under FairSpec, the liveness property.
PASSING = {"StepsPre": "pre", "StepsPrePack1": "pre1", "StepsDeal": "deal", "StepsLand": "land",
           "StepsBurst": "burst"}

# Reachability checks on Steps.tla: name -> (scenario, what).
REACH = {
    "StepsNeverPreempted": ("pre", "a blocker preempts the youngest working consumer"),
    "StepsNeverUnserved": ("pre", "a ready blocker with no open serving row opens unserved"),
    "StepsNeverRefused": ("pre", "an op is refused by a race"),
    "StepsNeverSplit": ("pre", "a group of a refused op is packed as an op of its own"),
    "StepsNeverStopAcked": ("pre", "a runner's stop ack is consumed"),
    "StepsNeverPolicyJudged": ("pre", "a failed policy answer opens a policy judgement"),
    "StepsNeverResolved": ("deal", "Resolve makes a producer ready"),
    "StepsNeverDealt": ("deal", "the deal starts a producer"),
    "StepsNeverReadAsked": ("deal", "the deal cuts a read"),
    "StepsNeverMerged": ("deal", "the fix step's ReadYes"),
    "StepsNeverFixed": ("deal", "a producer back from a bounce (ReadFix or Broken)"),
    "StepsNeverEscalated": ("deal", "a bounced producer is dealt one rank up"),
    "StepsNeverNeedReleased": ("deal", "a need lands and its dependant resolves"),
    "StepsNeverCannotRead": ("deal", "a review no row may read opens cannot-read"),
    "StepsNeverLandRequested": ("land", "a land request is dispatched"),
    "StepsNeverLanded": ("land", "a landed result lands a producer"),
    "StepsNeverRed": ("land", "a red result sends a producer back to review"),
    "StepsNeverAfterHeld": ("land", "a landing waits on a queued after target"),
    "StepsNeverCarried": ("burst", "the budget carries inbox items to the next tick"),
}

# (name, target, scenario, fault, replacements). A target in PROPERTIES is a
# liveness witness: its cfg uses FairSpec and lists the property first.
WITNESSES = [
    ("StepsDealtTwice", "NeverDealtTwice", "pre",
     "The deal does not skip a blocker the preemption step claimed: one\n"
     "tick plans the preemption and a plain Start for the same blocker.",
     [("LET bl == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = \"blocker\" /\\ V.ps[p].st = \"ready\" /\\ Free(p, cl))",
       "LET bl == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = \"blocker\" /\\ V.ps[p].st = \"ready\" /\\ TRUE)   \\* FAULT: was Free(p, cl)", 1)]),
    ("StepsTwoGroups", "OneGroupPerCard", "land",
     "The land-result step ignores cards already claimed: a result\n"
     "delivered twice plans two groups on one producer in one tick.",
     [("        /\\ PKey(p) \\notin cl\n", "        /\\ TRUE   \\* FAULT: was PKey(p) \\notin cl\n", 1)]),
    ("StepsWidthOver", "WidthKept", "pre",
     "The store's take checks the deal's room (width plus ahead) instead\n"
     "of the width.",
     [("  /\\ Cardinality(Working(w, w.cs[c].row)) < Width[w.cs[c].row]\n",
       "  /\\ Cardinality(Working(w, w.cs[c].row)) < Width[w.cs[c].row] + Ahead[w.cs[c].row]   \\* FAULT\n", 1)]),
    ("StepsBlockerQueued", "BlockerPreemptsInOneTick", "pre",
     "The preemption step counts queue room (ready plus working under width\n"
     "plus ahead) as free width, so a ready blocker is queued behind full\n"
     "rows instead of preempting.",
     [("NoFreeWidth(W, b) == \\A r \\in ServingOpen(W, b) : Cardinality(Working(W, r)) >= Width[r]",
       "NoFreeWidth(W, b) == \\A r \\in ServingOpen(W, b) : Cardinality(Load(W, r)) >= Width[r] + Ahead[r]   \\* FAULT", 1)]),
    ("StepsHalfWrites", "GroupWhole", "pre",
     "A refused op is split by halves of writes: the preemption's Withdraw\n"
     "is applied without the blocker's Start.",
     [("       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i}])",
       "       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i},   \\* FAULT: the Withdraw half\n"
       "                                     !.w = IF ops[i][1].kind = \"pre\"\n"
       "                                           THEN [acc.w EXCEPT !.cs[ops[i][1].c].st = \"fail\",\n"
       "                                                              !.tko = Drop(acc.w.tko, ops[i][1].c)]\n"
       "                                           ELSE acc.w])", 1)]),
    ("StepsPartialOp", "NeverHalfApplied", "pre",
     "A refused op applies the groups whose fences pass and drops only the\n"
     "raced one (one op, half applied).",
     [("       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i}])",
       "       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i},   \\* FAULT\n"
       "                 !.w = ApplySeq(acc.w, SelectSeq(ops[i], LAMBDA g : Fence(acc.pv, acc.w, g) /\\ Legal(acc.w, g)), 1)])", 1)]),
    ("StepsRefusedApplied", "RefusalChangesNothing", "pre",
     "A refused op is applied anyway (its body resent without the fence).",
     [("       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i}])",
       "       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i},   \\* FAULT\n"
       "                                     !.w = ApplySeq(acc.w, ops[i], 1)])", 1)]),
    ("StepsSelfRefusal", "RefusedOnlyByRace", "deal",
     "The deal does not pre-check that a row is up: it plans a Start the\n"
     "store refuses on fresh state.",
     [("  /\\ V.up[r]\n  /\\ Serves(r, x.p)\n", "  /\\ TRUE   \\* FAULT: was V.up[r]\n  /\\ Serves(r, x.p)\n", 1),
      ("MayMove(V, x, r) == IF x.kind = \"work\" THEN StartOK(V, x.p, r) ELSE ReadOK(V, x.p, r)",
       "MayMove(V, x, r) == TRUE   \\* FAULT: was the store's own guard", 1)]),
    ("StepsFenceOnView", "RefusedOnlyByRace", "pre1",
     "Each op is fenced on the view as the tick began, not on the view\n"
     "with the tick's accepted ops applied: an op that reads a card an\n"
     "earlier op of the same tick wrote is refused with no race.",
     [("  ELSE IF OpOK(acc.pv, acc.w, ops[i])", "  ELSE IF OpOK(V, acc.w, ops[i])   \\* FAULT: was acc.pv", 1)]),
    ("StepsBudgetIgnored", "BudgetKept", "burst",
     "The budget is not applied: every group of the tick is admitted.",
     [("ELSE IF ~acc.stop /\\ (acc.nb = 0 \\/ acc.t + Size(g) <= Budget)",
       "ELSE IF TRUE   \\* FAULT: was ~acc.stop /\\ (acc.nb = 0 \\/ acc.t + Size(g) <= Budget)", 1)]),
    ("StepsCarryDropped", "CarryFIFO", "burst",
     "The carry drops what the budget left: only the items of refused ops\n"
     "stay in the inbox.",
     [("        /\\ inbox' = SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps)",
       "        /\\ inbox' = SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps   \\* FAULT\n"
       "                                                 /\\ it.stamp \\in {g.stamp : g \\in admIn})", 1)]),
    ("StepsCarryNewestFirst", "CarryFIFO", "burst",
     "The carry is put back newest first.",
     [("        /\\ inbox' = SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps)",
       "        /\\ inbox' = LET X == SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps)   \\* FAULT\n"
       "                    IN [i \\in 1..Len(X) |-> X[Len(X) + 1 - i]]", 1)]),
    ("StepsStopEarly", "EdgeAfterAccepted", "pre",
     "A stop request is dispatched from every op, refused ones included\n"
     "(After ignored): a runner is stopped for a consumer never withdrawn.",
     [("stops == UNION {g.req : g \\in UNION {GroupsIn(ops[i]) : i \\in ap.ok}}",
       "stops == UNION {g.req : g \\in UNION {GroupsIn(ops[i]) : i \\in 1..Len(ops)}}   \\* FAULT", 1)]),
    ("StepsReadyOnRoom", "ReadyNeverFlipsWithLoad", "deal",
     "Ready is conditioned on room: Resolve requires an open serving row\n"
     "with room under width plus ahead.",
     [("  /\\ ~\\E q \\in Prods : Overlap(p, q) /\\ (W.ps[q].st \\in Started \\cup {\"ready\"} \\/ q \\in extra)\n",
       "  /\\ ~\\E q \\in Prods : Overlap(p, q) /\\ (W.ps[q].st \\in Started \\cup {\"ready\"} \\/ q \\in extra)\n"
       "  /\\ \\E r \\in ServingOpen(W, p) : Cardinality(Load(W, r)) < Width[r] + Ahead[r]   \\* FAULT\n", 1)]),
    ("StepsNoOverlapCheck", "NeverOverlapStarted", "deal",
     "Resolve without the overlap check: two producers of one stream with\n"
     "overlapping paths are ready together.",
     [("  /\\ ~\\E q \\in Prods : Overlap(p, q) /\\ (W.ps[q].st \\in Started \\cup {\"ready\"} \\/ q \\in extra)\n",
       "  /\\ TRUE   \\* FAULT: was the overlap check\n", 1)]),
    ("StepsTierUnchecked", "NeverBelowTier", "deal",
     "The tier is checked nowhere: policy's rows answer is taken for any up\n"
     "row, and Start does not check that the row serves the card's tier.",
     [("InDomain(V, x, ar, t) == ar \\in Rows /\\ Eligible(V, x, ar, t)",
       "InDomain(V, x, ar, t) == ar \\in Rows /\\ V.up[ar]   \\* FAULT: was Eligible(V, x, ar, t)", 1),
      ("  /\\ ~W.sh[Stream[p]]\n  /\\ Serves(r, p)\n  /\\ W.ps[p].att < MaxAtt\n",
       "  /\\ ~W.sh[Stream[p]]\n  /\\ TRUE   \\* FAULT: was Serves(r, p)\n  /\\ W.ps[p].att < MaxAtt\n", 1)]),
    ("StepsAfterIgnored", "AfterKept", "land",
     "The land step ignores after: a producer is sent to land while its\n"
     "after target is queued to land.",
     [("LandElig(V, s, cl) == {p \\in Merging(V, s, cl) : \\A t \\in After[p] : V.ps[t].st # \"merging\"}",
       "LandElig(V, s, cl) == Merging(V, s, cl)   \\* FAULT: after ignored", 1)]),
    ("StepsAfterHolds", "AfterNeverHolds", "land",
     "The land step holds on any after target not yet landed, queued or\n"
     "not (v1's frozen LAND-AFTER).",
     [("LandElig(V, s, cl) == {p \\in Merging(V, s, cl) : \\A t \\in After[p] : V.ps[t].st # \"merging\"}",
       "LandElig(V, s, cl) == {p \\in Merging(V, s, cl) : \\A t \\in After[p] : V.ps[t].st = \"landed\"}   \\* FAULT", 1)]),
    ("StepsPolicyStall", "PolicyFailsToDefault", "deal",
     "A failed rows answer plans nothing for the candidate instead of the\n"
     "default row.",
     [("RowFor(V, x, t, ar) == IF InDomain(V, x, ar, t) THEN ar ELSE DefaultRow(V, x, t)",
       "RowFor(V, x, t, ar) == IF InDomain(V, x, ar, t) THEN ar\n"
       "                       ELSE IF ar = \"failed\" THEN NoR ELSE DefaultRow(V, x, t)   \\* FAULT", 1)]),
    ("StepsOldestVictim", "PreemptYoungest", "pre",
     "The victim default is the oldest working consumer, not the youngest.",
     [("VictimFor(W, S) == Youngest(W, S)",
       "VictimFor(W, S) == CHOOSE c \\in S : \\A d \\in S : Pos(W.tko, c) <= Pos(W.tko, d)   \\* FAULT: the oldest, not Youngest(W, S)", 1)]),
    ("StepsReadyCountedAgainstWidth", "ReadyDealtWhenRoom", "pre",
     "The deal counts ready consumers against width (a row is dealt only\n"
     "while ready plus working is under width): a ready producer with room\n"
     "under width plus ahead waits on takes that need not come.",
     [("  /\\ Used(V, r, t) < Width[r] + Ahead[r]\n",
       "  /\\ Used(V, r, t) < Width[r]   \\* FAULT: was Width[r] + Ahead[r]\n", 1)]),
]


def die(msg):
    sys.stderr.write("make-steps-witnesses: " + msg + "\n")
    sys.exit(1)


def header(name, fault, target):
    title = " MODULE " + name + " "
    left = (77 - len(title)) // 2
    line = "-" * left + title + "-" * (77 - len(title) - left)
    lines = ["REVERSED WITNESS. FAULT:"] + fault.split("\n")
    lines += ["TLC must report " + target + " violated. Everything else is",
              "Steps.tla unchanged (generated by make-steps-witnesses.py)."]
    for l in lines:
        if len(l) > 71:
            die(name + ": header line over 71 characters: " + l)
    box = ["(" + "*" * 75 + ")"]
    box += ["(* " + l.ljust(71) + " *)" for l in lines]
    box += ["(" + "*" * 75 + ")"]
    return line, "\n".join(box)


def constants(scenario, spec):
    s = SCENARIOS[scenario]
    out = "CONSTANT Scenario = \"%s\"\n" % s.get("Scenario", scenario)
    for k in CONST_ORDER:
        out += "CONSTANT %s = %d\n" % (k, s[k])
    return out + "SPECIFICATION %s\n" % spec


def invariant_lines(first=None):
    order = ([first] if first else []) + [i for i in INVARIANTS if i != first]
    return "".join("INVARIANT " + i + "\n" for i in order)


def witness_cfg(target, scenario):
    out = "\\* Reversed witness: TLC must report " + target + " violated.\n"
    if target in PROPERTIES:
        out += "\\* The property is listed first; no invariant may be violated.\n"
        return ("PROPERTY " + target + "\n" + out + constants(scenario, "FairSpec")
                + invariant_lines())
    out += "\\* The target invariant is listed first so it is the one TLC names.\n"
    return out + constants(scenario, "Spec") + invariant_lines(target)


def main():
    with open(os.path.join(HERE, "Steps.tla")) as f:
        base = f.read()
    first = base.split("\n", 1)
    if "MODULE Steps " not in first[0]:
        die("Steps.tla does not start with its module line")
    for i in INVARIANTS + PROPERTIES:
        if "\n" + i + " " not in base:
            die("invariant " + i + " is not defined in Steps.tla")
    for name, target, scenario, fault, reps in WITNESSES:
        if target not in INVARIANTS + PROPERTIES:
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
            f.write(witness_cfg(target, scenario))
    for name, scenario in PASSING.items():
        with open(os.path.join(HERE, name + ".cfg"), "w") as f:
            f.write(constants(scenario, "FairSpec") + invariant_lines()
                    + "".join("PROPERTY " + p + "\n" for p in PROPERTIES))
    for name, (scenario, what) in REACH.items():
        if "\n" + name + " " not in base:
            die("reach check " + name + " is not defined in Steps.tla")
        with open(os.path.join(HERE, "reach", name + ".cfg"), "w") as f:
            f.write("\\* Reachability on Steps: TLC must report %s violated (%s).\n" % (name, what)
                    + constants(scenario, "Spec") + "INVARIANT %s\n" % name)


main()
