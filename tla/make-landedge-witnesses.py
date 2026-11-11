#!/usr/bin/env python3
"""Generate the landing model's reversed witnesses and reach cfgs from
LandEdge.tla: python3 tla/make-landedge-witnesses.py.

The same shape as make-steps-witnesses.py: each witness is LandEdge.tla with
its module renamed, a header naming the fault, and exact text replacements;
a replacement that does not match exactly once stops the script. The
witness's target is the first INVARIANT of its cfg (ci-tlc.sh). Commit the
generated files; CI runs them as they are.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
INVARIANTS = ["TypeOK", "BaseNeverRed", "NothingLandsUnpassed", "MergingCovered",
              "VerdictOnlyCompleted", "RedIsReal", "StoppedRunAgainOnce"]
CONSTS = "CONSTANT K = 1\nCONSTANT BackedUp = 2\nCONSTANT MaxPass = 6\nSPECIFICATION Spec\n"

REACH = {
    "LandEdgeNeverRungTwo": "a pass gates the group branch (rung 2)",
    "LandEdgeNeverJudged": "a result other than landed opens a land judgement (rung 3)",
    "LandEdgeNeverHandLand": "the coordinator lands a card by hand",
    "LandEdgeNeverRedGated": "CI is red on a gated tree and nothing lands",
    "LandEdgeNeverNoVerdict": "a landing ends with no verdict (stopped twice, or the wait passed)",
    "LandEdgeNeverRerun": "a gate whose failing checks were all stopped is run again",
    "LandEdgeNeverLate": "the wait passes on a failure while checks still run: no verdict",
}

# The rerun of a gate whose every failing check was stopped (the text the
# stopped-check witnesses replace).
RERUN = ("           \\/ /\\ r.ci = \"done\" /\\ r.res = \"stopped\" /\\ ~r.reran   \\* run again once: asked again\n"
         "              /\\ req' = [req EXCEPT ![s] = [r EXCEPT !.ci = \"running\", !.res = \"none\", !.reran = TRUE, !.late = FALSE]]\n")

WITNESSES = [
    ("LandEdgeRedLands", "BaseNeverRed",
     "The merge does not read the gate: a red tree is merged as green.",
     [('CIOf(t) == IF Red(t.cards) THEN {"red", "stopped"} ELSE {"green", "stopped"}',
       'CIOf(t) == {"green", "red", "stopped"}   \\* FAULT: was the truthful CI')]),
    ("LandEdgeStaleGate", "NothingLandsUnpassed",
     "Base moves under the gate and the pass merges anyway: what lands is\n"
     "the heads on the new tip, not the tree CI passed.",
     [("                 \\/ /\\ Tip.ext < K                    \\* base moved under the gate: asked again\n"
       "                    /\\ base' = Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1], how |-> \"ext\", gated |-> NoTree])\n"
       "                    /\\ req' = [req EXCEPT ![s] = r]\n",
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
     [(RERUN, "           \\/ /\\ r.ci = \"done\" /\\ r.res = \"stopped\"   \\* FAULT: stopped reads as red\n"
              "              /\\ Done(s, r, \"red\")\n")]),
    ("LandEdgeStoppedJudgedAtOnce", "StoppedRunAgainOnce",
     "A gate whose failing checks were all stopped is the coordinator's at\n"
     "once: its stopped jobs are never run again.",
     [(RERUN, "           \\/ /\\ r.ci = \"done\" /\\ r.res = \"stopped\"   \\* FAULT: judged at once\n"
              "              /\\ Done(s, r, \"noverdict\")\n")]),
    ("LandEdgeJudgementHoldsStream", "MergingCovered",
     "A land judgement holds its whole stream: while one card is judged,\n"
     "no landing is asked for the stream's other merging cards.",
     [("      E(s)    == {c \\in Cards : S(c) = s /\\ cs1[c] = \"merging\" /\\ c \\notin js1}",
       "      E(s)    == IF \\E j \\in js1 : S(j) = s THEN {}   \\* FAULT: a judgement holds the stream\n"
       "                 ELSE {c \\in Cards : S(c) = s /\\ cs1[c] = \"merging\" /\\ c \\notin js1}")]),
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
    for name, target, fault, reps in WITNESSES:
        text = first[1]
        for old, new in reps:
            if text.count(old) != 1:
                die("%s: expected one match, found %d, for:\n%s" % (name, text.count(old), old))
            text = text.replace(old, new)
        line, box = header(name, fault, target)
        with open(os.path.join(HERE, "witnesses", name + ".tla"), "w") as f:
            f.write(line + "\n" + box + "\n" + text)
        order = [target] + [i for i in INVARIANTS if i != target]
        with open(os.path.join(HERE, "witnesses", name + ".cfg"), "w") as f:
            f.write("\\* Reversed witness: TLC must report " + target + " violated.\n" + CONSTS
                    + "".join("INVARIANT " + i + "\n" for i in order))
    for name, what in REACH.items():
        inv = name.replace("LandEdge", "", 1)
        if "\n" + inv + " " not in base:
            die("reach check " + inv + " is not defined in LandEdge.tla")
        with open(os.path.join(HERE, "reach", name + ".cfg"), "w") as f:
            f.write("\\* Reachability on LandEdge: TLC must report %s violated (%s).\n" % (inv, what)
                    + CONSTS + "INVARIANT %s\n" % inv)


main()
