#!/usr/bin/env python3
"""Generate the landing model's reversed witnesses and reach cfgs from
LandEdge.tla: python3 tla/make-landedge-witnesses.py.

The same shape as make-steps-witnesses.py: each witness is LandEdge.tla with
its module renamed, a header naming the fault, and exact text replacements;
a replacement that does not match exactly once stops the script. The
witness's target is the first INVARIANT of its cfg, or the first PROPERTY of
a liveness witness's (LIVE, on LiveSpec) (ci-tlc.sh). Commit the generated
files; CI runs them as they are.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
INVARIANTS = ["TypeOK", "BaseNeverRed", "NothingLandsUnpassed", "MergingCovered",
              "VerdictOnlyCompleted", "RedIsReal", "ConflictJudgesItsCard"]
CONSTS = "CONSTANT K = 1\nCONSTANT BackedUp = 2\nCONSTANT MaxPass = 6\nSPECIFICATION Spec\n"
LIVE_CONSTS = "CONSTANT K = 1\nCONSTANT BackedUp = 2\nCONSTANT MaxPass = 6\nSPECIFICATION LiveSpec\n"

REACH = {
    "LandEdgeNeverRungTwo": "a pass gates the group branch (rung 2)",
    "LandEdgeNeverJudged": "a result other than landed opens a land judgement (rung 3)",
    "LandEdgeNeverHandLand": "the coordinator lands a card by hand",
    "LandEdgeNeverRedGated": "CI is red on a gated tree and nothing lands",
    "LandEdgeNeverNoVerdict": "a landing ends with no verdict (every failing check stopped, or the wait passed)",
    "LandEdgeNeverLate": "the wait passes on a failure while checks still run: no verdict",
    "LandEdgeNeverKeptMoved": "a gate whose CI runs is kept while base moves under it",
    "LandEdgeNeverOddAlone": "a group's conflict names one card of the group",
    "LandEdgeNeverSwitchInFlight": "stream set --base switches the base under a gate in flight",
}

# A gate whose every failing check was stopped (the text the stopped-check
# witness replaces).
STOPPED = ("           \\/ /\\ r.ci = \"done\" /\\ r.res = \"stopped\"   \\* every failing check stopped: no verdict\n"
           "              /\\ Done(s, r, \"noverdict\")\n")

WITNESSES = [
    ("LandEdgeRedLands", "BaseNeverRed",
     "The merge does not read the gate: a red tree is merged as green.",
     [('CIOf(t) == IF Red(t.cards) THEN {"red", "stopped"} ELSE {"green", "stopped"}',
       'CIOf(t) == {"green", "red", "stopped"}   \\* FAULT: was the truthful CI')]),
    ("LandEdgeStaleGate", "NothingLandsUnpassed",
     "Base moves under the gate and the pass merges anyway: what lands is\n"
     "the heads on the new tip, not the tree CI passed.",
     [("                 \\/ /\\ Tip.ext < K                    \\* base moved under the gate: let go, asked again\n"
       "                    /\\ base' = Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1], how |-> \"ext\", gated |-> NoTree])\n"
       "                    /\\ req' = [req EXCEPT ![s] = LetGo(r)]\n",
       "                 \\/ /\\ Tip.ext < K   \\* FAULT: base moved under the gate, merged anyway\n"
       "                    /\\ base' = Append(Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1], how |-> \"ext\", gated |-> NoTree]),\n"
       "                                      [tree |-> [r.gate EXCEPT !.ext = @ + 1], how |-> \"auto\", gated |-> r.gate])\n"
       "                    /\\ Done(s, r, \"landed\")\n")]),
    ("LandEdgeRedWhileRunning", "VerdictOnlyCompleted",
     "The old rule (v2.0.36): a check that failed while others still run\n"
     "is judged red at once, before the gate's checks completed.",
     [("           \\/ /\\ r.ci = \"partial\" /\\ r.late            \\* the wait passed: no verdict\n"
       "              /\\ Done(s, r, \"noverdict\")\n",
       "           \\/ /\\ r.ci = \"partial\"   \\* FAULT: judged while checks still run\n"
       "              /\\ Done(s, r, \"red\")\n")]),
    ("LandEdgeStoppedIsRed", "RedIsReal",
     "The old rule (v2.0.36): a gate whose failing checks were all stopped\n"
     "(killed at a time limit, cancelled) is judged red.",
     [(STOPPED, "           \\/ /\\ r.ci = \"done\" /\\ r.res = \"stopped\"   \\* FAULT: stopped reads as red\n"
              "              /\\ Done(s, r, \"red\")\n")]),
    ("LandEdgeJudgementHoldsStream", "MergingCovered",
     "A land judgement holds its whole stream: while one card is judged,\n"
     "no landing is asked for the stream's other merging cards.",
     [("      E(s)    == {c \\in Cards : S(c) = s /\\ cs1[c] = \"merging\" /\\ c \\notin js1}",
       "      E(s)    == IF \\E j \\in js1 : S(j) = s THEN {}   \\* FAULT: a judgement holds the stream\n"
       "                 ELSE {c \\in Cards : S(c) = s /\\ cs1[c] = \"merging\" /\\ c \\notin js1}")]),
    ("LandEdgeConflictJudgesGroup", "ConflictJudgesItsCard",
     "The old rule (v2.0.37): a group's conflict on one card's head is a\n"
     "judgement for every card of the group.",
     [("JudgedOf(s) == IF req[s].out = \"conflict\" /\\ req[s].odd # {} THEN Moving(s) \\cap req[s].odd ELSE Moving(s)",
       "JudgedOf(s) == Moving(s)   \\* FAULT: every card of the group is judged")]),
]

# Liveness witnesses, on LiveSpec: the target is the cfg's first PROPERTY.
LIVE = [
    ("LandEdgeRebuildsInFlight", "GateReachesVerdict",
     "The old rule (v2.0.36): a pass on a base that moved rebuilds the\n"
     "gate and pushes it again while its CI still runs, so that CI never\n"
     "reaches a verdict.",
     [("Kept(s) == req[s].ci # \"none\"",
       "Kept(s) == req[s].ci # \"none\" /\\ req[s].gbase = Len(base)   \\* FAULT: rebuilt when base moved")]),
]


def die(msg):
    sys.stderr.write("make-landedge-witnesses: " + msg + "\n")
    sys.exit(1)


def header(name, fault, target):
    title = " MODULE " + name + " "
    left = (77 - len(title)) // 2
    line = "-" * left + title + "-" * (77 - len(title) - left)
    lines = ["REVERSED WITNESS. FAULT:"] + fault.split("\n")
    lines += ["TLC must report " + target + " violated. Everything else is",
              "LandEdge.tla unchanged (make-landedge-witnesses.py)."]
    for l in lines:
        if len(l) > 71:
            die(name + ": header line over 71 characters: " + l)
    box = ["(" + "*" * 75 + ")"] + ["(* " + l.ljust(71) + " *)" for l in lines] + ["(" + "*" * 75 + ")"]
    return line, "\n".join(box)


def main():
    with open(os.path.join(HERE, "LandEdge.tla")) as f:
        base = f.read()
    first = base.split("\n", 1)
    if "MODULE LandEdge " not in first[0]:
        die("LandEdge.tla does not start with its module line")
    for name, target, fault, reps in WITNESSES + LIVE:
        text = first[1]
        for old, new in reps:
            if text.count(old) != 1:
                die("%s: expected one match, found %d, for:\n%s" % (name, text.count(old), old))
            text = text.replace(old, new)
        line, box = header(name, fault, target)
        with open(os.path.join(HERE, "witnesses", name + ".tla"), "w") as f:
            f.write(line + "\n" + box + "\n" + text)
        live = (name, target, fault, reps) in LIVE
        order = [i for i in INVARIANTS if i != target]
        head = ("PROPERTY " if live else "INVARIANT ") + target + "\n"
        with open(os.path.join(HERE, "witnesses", name + ".cfg"), "w") as f:
            f.write("\\* Reversed witness: TLC must report " + target + " violated.\n"
                    + (LIVE_CONSTS if live else CONSTS) + head
                    + "".join("INVARIANT " + i + "\n" for i in order))
    for name, what in REACH.items():
        inv = name.replace("LandEdge", "", 1)
        if "\n" + inv + " " not in base:
            die("reach check " + inv + " is not defined in LandEdge.tla")
        with open(os.path.join(HERE, "reach", name + ".cfg"), "w") as f:
            f.write("\\* Reachability on LandEdge: TLC must report %s violated (%s).\n" % (inv, what)
                    + CONSTS + "INVARIANT %s\n" % inv)


main()
