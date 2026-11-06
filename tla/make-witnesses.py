#!/usr/bin/env python3
"""Generate the reversed witnesses and every cfg from Store.tla.

Run from anywhere: python3 tla/make-witnesses.py. Each witness is Store.tla
with its module renamed, a header naming the fault, and a few exact text
replacements; a replacement that does not match exactly the expected number
of times stops the script, so a change to Store.tla that a fault no longer
fits fails here instead of producing a witness that tests nothing. Commit the
generated files; CI runs them as they are.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# Every invariant of the passing runs, in cfg order.
INVARIANTS = [
    "TypeOK", "SingleStateSingleHolder", "ProducerNeverHeld", "ConsumersWellFormed",
    "WidthKept", "IdempotentById", "AppliedIsLog", "LogMatchesState", "OpsInTheirEpoch",
    "ConditionsAtomic", "ServesHeld", "NoChangeOnRefusal", "NoSpuriousRefusal", "GenPerOp",
    "ClearCarriesRows", "JudgementClosedOnce", "CursorMonotone", "CursorWithinLog",
    "LogAppendOnly", "ReceiptMatches",
]

# The passing runs: cfg name -> Scenario.
PASSING = {"Store": "deal", "StoreRecords": "records"}

# Reachability checks: each must be violated on Store.tla, in its scenario.
REACH = {
    "NeverTaken": ("deal", "an accepted deal (a1) is reachable"),
    "NeverRecovered": ("deal", "a caller recovers from a transport failure"),
    "NeverStaleEpochRefused": ("deal", "an op planned against a closed epoch is refused"),
    "NeverNeedsReady": ("deal", "a card whose needs landed becomes ready (s9)"),
    "NeverNewEpochOp": ("deal", "an op in the new epoch is accepted (k2)"),
    "NeverWidthFull": ("deal", "a row holds width working cards"),
    "NeverWidthLowered": ("deal", "a width is lowered (e1)"),
    "NeverWidthRefused": ("deal", "a width below the working holders is refused (e1)"),
    "NeverInOpWidthRefused": ("deal", "an op is refused on its own second work entry (s10)"),
    "NeverProducerRefused": ("deal", "a producer added with a holder is refused (s3)"),
    "NeverSentinelRefused": ("deal", "a sentinel added with a holder is refused (s4)"),
    "NeverHeldCarried": ("deal", "a row's hold survives the clear"),
    "NeverWorkEntryRefused": ("deal", "a held card going working on a full row is refused (b2)"),
    "NeverQueuedPastWidth": ("deal", "a row holds a ready consumer past its width (r1)"),
    "NeverHeldRefused": ("deal", "a deal is refused on RowHeld after a hold (a1)"),
    "NeverReadyServesRefused": ("deal", "a move into ready on a row that no longer serves the tier is refused (s15)"),
    "NeverAddServesRefused": ("deal", "put card of a consumer on a row that does not serve its tier is refused (s16)"),
    "NeverTiersRefused": ("deal", "dropping a tier a live consumer needs is refused (v1)"),
    "NeverRowFenced": ("deal", "a row write is refused on a stale RowGen (e1)"),
    "NeverSlotReused": ("deal", "a done consumer's slot is reused while it keeps its holder (b2)"),
    "NeverAnswered": ("records", "a judgement is answered (h1 or h2)"),
    "NeverJudgementFenced": ("records", "a second close of a judgement is refused on JudgementOpen"),
    "NeverCursorRefused": ("records", "a cursor write below the current value is refused (m2)"),
    "NeverCloseRefused": ("records", "an unfenced second close is refused by the write (h2)"),
    "NeverCursorPastRefused": ("records", "a cursor write past the log is refused (m3)"),
    "NeverCursorCarried": ("records", "the clear keeps the cursor"),
}

# The two-step transaction (Begin reads a snapshot, Apply commits on it),
# shared by the stale witnesses. The snapshot and the state at commit start
# ride in the state record as extra fields sn and p0 (the rebuild path never
# has them), and are stripped before the state is stored.
STALE_INFRA = [
    (r"  lost        \* op id -> transport failures so far (bounded by MaxLoss)",
     "  lost,       \\* op id -> transport failures so far (bounded by MaxLoss)\n"
     "  snap        \\* op id -> the state its transaction read at Begin (FAULT)", 1),
    ("vars == <<epoch, cards, rows, judgements, cursors, events, ledger, applied, phase, receipt, lost>>",
     "vars == <<epoch, cards, rows, judgements, cursors, events, ledger, applied, phase, receipt, lost, snap>>", 1),
    (r"  /\ lost       = [o \in OpIds |-> 0]",
     "  /\\ lost       = [o \\in OpIds |-> 0]\n  /\\ snap       = [o \\in OpIds |-> Empty]", 1),
    ("  /\\ UNCHANGED <<store, events, ledger, applied, lost>>",
     "  /\\ UNCHANGED <<store, events, ledger, applied, lost, snap>>", 1),
    ("  /\\ UNCHANGED <<store, events, ledger, applied, receipt>>",
     "  /\\ UNCHANGED <<store, events, ledger, applied, receipt, snap>>", 1),
    ("      out  == Outcome(pre, o, Len(events))\n      post == out.st\n",
     "      \\* FAULT: the op is judged with the snapshot its Begin read.\n"
     "      out  == Outcome([epoch |-> pre.epoch, cards |-> pre.cards, rows |-> pre.rows,\n"
     "                       judgements |-> pre.judgements, cursors |-> pre.cursors,\n"
     "                       sn |-> snap[o], p0 |-> pre], o, Len(events))\n"
     "      post == [epoch |-> out.st.epoch, cards |-> out.st.cards, rows |-> out.st.rows,\n"
     "               judgements |-> out.st.judgements, cursors |-> out.st.cursors]\n", 1),
    ("          /\\ lost'    = IF res = \"unknown\" THEN [lost EXCEPT ![o] = @ + 1] ELSE lost\n",
     "          /\\ lost'    = IF res = \"unknown\" THEN [lost EXCEPT ![o] = @ + 1] ELSE lost\n"
     "     /\\ UNCHANGED snap\n", 1),
    ("Apply(o) ==\n"
     "  /\\ phase[o] \\in {\"idle\", \"unknown\"}\n"
     "  /\\ Ready(o)\n"
     "  /\\ IF o \\in applied THEN ReplayEffect(o) ELSE Execute(o)\n",
     "(* FAULT: Apply is two steps. Begin reads the snapshot; Apply commits.    *)\n"
     "Begin(o) ==\n"
     "  /\\ phase[o] \\in {\"idle\", \"unknown\"}\n"
     "  /\\ Ready(o)\n"
     "  /\\ o \\notin applied\n"
     "  /\\ phase' = [phase EXCEPT ![o] = \"inflight\"]\n"
     "  /\\ snap'  = [snap EXCEPT ![o] = Current]\n"
     "  /\\ UNCHANGED <<store, events, ledger, applied, receipt, lost>>\n"
     "\n"
     "Apply(o) ==\n"
     "  \\/ phase[o] = \"inflight\" /\\ Execute(o)\n"
     "  \\/ phase[o] \\in {\"idle\", \"unknown\"} /\\ Ready(o) /\\ o \\in applied /\\ ReplayEffect(o)\n", 1),
    ("  \\/ \\E o \\in OpIds : Apply(o) \\/ LoseRequest(o) \\/ Replay(o)",
     "  \\/ \\E o \\in OpIds : Begin(o) \\/ Apply(o) \\/ LoseRequest(o) \\/ Replay(o)", 1),
    ("  /\\ phase \\in [OpIds -> {\"idle\", \"unknown\", \"done\"}]",
     "  /\\ phase \\in [OpIds -> {\"idle\", \"unknown\", \"inflight\", \"done\"}]", 1),
    ("Exists(st, c) == st.cards[c].state # Absent\n",
     "Exists(st, c) == st.cards[c].state # Absent\n"
     "\\* The snapshot a stale transaction read, or the state itself on the rebuild path.\n"
     "Snap(st) == IF \"sn\" \\in DOMAIN st THEN st.sn ELSE st\n", 1),
]

OUTCOME_HOLDS = ("  ELSE LET run == RunOp(st, Ops[o].writes)\n"
                 "       IN IF Holds(st, Ops[o].expect) /\\ run.ok /\\ CursorsFit(Ops[o].writes, last)")

# (name, target, scenario, fault, replacements)
WITNESSES = [
    ("StoreNoEvent", "LogMatchesState", "deal",
     "An accepted op's writes land but its event row is not appended (and so\n"
     "its id is not remembered).",
     [("     /\\ events'  = Append(events, ev)\n     /\\ ledger'  = Append(ledger, ev)\n"
       "     /\\ applied' = applied \\cup {o}\n",
       "     \\* FAULT: the accepted path forgets its event row.\n"
       "     /\\ events'  = IF out.acc THEN events ELSE Append(events, ev)\n"
       "     /\\ ledger'  = IF out.acc THEN ledger ELSE Append(ledger, ev)\n"
       "     /\\ applied' = IF out.acc THEN applied ELSE applied \\cup {o}\n", 1)]),
    ("StoreReplayApplies", "IdempotentById", "deal",
     "Replay runs the op again and logs it again instead of returning the\n"
     "stored receipt.",
     [("ReplayEffect(o) ==\n"
       "  /\\ phase'   = [phase EXCEPT ![o] = \"done\"]\n"
       "  /\\ receipt' = [receipt EXCEPT ![o] = StoredEvent(o)]\n"
       "  /\\ UNCHANGED <<store, events, ledger, applied, lost>>\n",
       "\\* FAULT: replay runs the op again and logs a new event.\n"
       "ReplayEffect(o) ==\n"
       "  LET pre == Current\n"
       "      out == Outcome(pre, o, Len(events))\n"
       "      ev  == [id |-> o, accepted |-> out.acc, epoch |-> pre.epoch, before |-> pre, after |-> out.st]\n"
       "  IN /\\ epoch'      = out.st.epoch\n"
       "     /\\ cards'      = out.st.cards\n"
       "     /\\ rows'       = out.st.rows\n"
       "     /\\ judgements' = out.st.judgements\n"
       "     /\\ cursors'    = out.st.cursors\n"
       "     /\\ events'  = Append(events, ev)\n"
       "     /\\ ledger'  = Append(ledger, ev)\n"
       "     /\\ phase'   = [phase EXCEPT ![o] = \"done\"]\n"
       "     /\\ receipt' = [receipt EXCEPT ![o] = ev]\n"
       "     /\\ UNCHANGED <<applied, lost>>\n", 1)]),
    ("StoreStaleCheck", "ConditionsAtomic", "deal",
     "Apply is two steps: Begin reads a snapshot, and the commit checks the\n"
     "op's conditions on that snapshot, not on the state its writes change\n"
     "(no lock): a1 reads r1 not held, g1 holds r1, a1 lands.",
     STALE_INFRA + [
         (OUTCOME_HOLDS,
          "  ELSE LET run == RunOp(st, Ops[o].writes)\n"
          "       \\* FAULT: the conditions are read on the snapshot.\n"
          "       IN IF Holds(Snap(st), Ops[o].expect) /\\ run.ok /\\ CursorsFit(Ops[o].writes, last)", 1)]),
    ("StoreEpochUnfenced", "OpsInTheirEpoch", "deal",
     "Ops other than clear are not checked against the store's epoch, so an\n"
     "op planned against epoch 1 arriving after the clear lands in epoch 2\n"
     "(v1 sets r1's tiers in epoch 2).",
     [("  IF Ops[o].epoch # st.epoch THEN [acc |-> FALSE, st |-> st]\n"
       "  ELSE IF Ops[o].kind = \"clear\"",
       "  \\* FAULT: only the clear is fenced by epoch.\n"
       "  IF Ops[o].kind = \"clear\" /\\ Ops[o].epoch # st.epoch THEN [acc |-> FALSE, st |-> st]\n"
       "  ELSE IF Ops[o].kind = \"clear\"", 1)]),
    ("StoreWidthStale", "WidthKept", "deal",
     "Apply is two steps, and the width check (count and width) is read\n"
     "on the snapshot Begin took, plus this op's own writes: a1 begins,\n"
     "e1 lowers r1's width to 0, and a1 lands u1 working on the stale\n"
     "width 1.",
     STALE_INFRA + [
         ("Room(st, r, c, L) == Cardinality(CountedIn(st, r, L) \\ {c}) < st.rows[r].width\n",
          "\\* FAULT: counted on the snapshot, plus the working holders this op added.\n"
          "Room(st, r, c, L) ==\n"
          "  IF \"sn\" \\in DOMAIN st\n"
          "  THEN Cardinality(CountedIn(st.sn, r, L) \\ {c})\n"
          "         + Cardinality((CountedIn(st, r, L) \\ CountedIn(st.p0, r, L)) \\ {c})\n"
          "       < st.sn.rows[r].width\n"
          "  ELSE Cardinality(CountedIn(st, r, L) \\ {c}) < st.rows[r].width\n", 1)]),
    ("StoreWidthUnchecked", "WidthKept", "deal",
     "Put row and set row width accept any width, whatever the row holds:\n"
     "a1 deals u1 working to r1, then e1 lowers r1's width to 0 and is\n"
     "accepted.",
     [("                              /\\ st.rows[w.r].state = Absent \\/ w.w >= Cardinality(CountedIn(st, w.r, L))\n"
       "                              /\\ st.rows[w.r].state = Absent \\/ TiersFit(st, w.r, w.n)\n"
       "    [] w.k = \"setRowWidth\" -> st.rows[w.r].state # Absent /\\ w.w >= Cardinality(CountedIn(st, w.r, L))\n",
       "                              /\\ st.rows[w.r].state = Absent \\/ TiersFit(st, w.r, w.n)\n"
       "    \\* FAULT: any width is accepted, whatever the row holds.\n"
       "    [] w.k = \"setRowWidth\" -> st.rows[w.r].state # Absent\n", 1)]),
    ("StoreWidthPerOp", "WidthKept", "deal",
     "The work-entry check counts the row's working holders as at the start\n"
     "of the op, not at the moment of the write: s10 makes two consumers\n"
     "working on the width-1 row r2 in one op and is accepted.",
     [("RECURSIVE SpecRun(_, _)\n",
       "\\* FAULT: the work-entry count is read in pre, the state before the op.\n"
       "PreLegal(pre, st, w) ==\n"
       "  IF w.k = \"setState\"\n"
       "  THEN LET card == st.cards[w.c]\n"
       "       IN /\\ Exists(st, w.c)\n"
       "          /\\ w.s \\in CardStates\n"
       "          /\\ w.s # card.state\n"
       "          /\\ ServesOK(st, w)\n"
       "          /\\ (w.s \\in Counted /\\ card.state \\notin Counted /\\ card.holder # NoHolder)\n"
       "               => Room(pre, card.holder, w.c, Counted)\n"
       "  ELSE RuleLegal(st, w, Counted)\n"
       "\n"
       "RECURSIVE RunPre(_, _, _)\n"
       "RunPre(pre, st, ws) ==\n"
       "  IF ws = <<>> THEN [ok |-> TRUE, st |-> st]\n"
       "  ELSE IF PreLegal(pre, st, Head(ws)) THEN RunPre(pre, Do(st, Head(ws)), Tail(ws))\n"
       "  ELSE [ok |-> FALSE, st |-> st]\n"
       "\n"
       "RECURSIVE SpecRun(_, _)\n", 1),
      ("  LET r == Run(st, ws)\n",
       "  LET r == RunPre(st, st, ws)   \\* FAULT\n", 1)]),
    ("StoreProducerHolds", "ProducerNeverHeld", "deal",
     "Add card does not refuse a holder on a producer (or a sentinel): s3\n"
     "adds producer p1 held by r1 and is accepted.",
     [("                                      /\\ w.r = NoHolder\n",
       "                                      \\* FAULT: no holder check\n", 1)]),
    ("StoreSentinelHolds", "ProducerNeverHeld", "deal",
     "Add card refuses a holder on a producer but not on a sentinel (a check\n"
     "on kind == producer): s4 adds sentinel t1 held by r1 and is accepted.",
     [("                                      /\\ w.r = NoHolder\n",
       "                                      /\\ (w.kd = \"producer\" => w.r = NoHolder)   \\* FAULT: sentinels pass\n", 1)]),
    ("StoreWidthCountsDone", "NoSpuriousRefusal", "deal",
     "The store's width count also counts done and fail holders (the design:\n"
     "they keep their holder and do not count): once u1 is done on r1, e1\n"
     "lowering r1's width to 0 is refused although r1 holds no working card.\n"
     "(b2 making u2 working on r1 is refused the same way.) The\n"
     "design's rule (SpecLegal) counts working only, so the refusal has no\n"
     "cause in the spec: an invariant, NoSpuriousRefusal, not a reach check.",
     [("Legal(st, w)     == RuleLegal(st, w, Counted)\n",
       "\\* FAULT: done and fail holders are counted too.\n"
       "Legal(st, w)     == RuleLegal(st, w, Counted \\cup {\"done\", \"fail\"})\n", 1)]),
    ("StoreWidthCountsReady", "NoSpuriousRefusal", "deal",
     "The store's width count also counts ready holders (the design: ready\n"
     "is queued, not counted): with u3 ready on r1, a1 making u1 working is\n"
     "refused although r1 has no working holder. An invariant,\n"
     "NoSpuriousRefusal, as in StoreWidthCountsDone.",
     [("Legal(st, w)     == RuleLegal(st, w, Counted)\n",
       "\\* FAULT: ready holders are counted too.\n"
       "Legal(st, w)     == RuleLegal(st, w, Counted \\cup {\"ready\"})\n", 1)]),
    ("StoreRowStale", "ConditionsAtomic", "deal",
     "The store does not check RowGen: g1 holds r1 (its gen rises to 1), and\n"
     "e1, fenced on RowGen(r1, 0), still lowers r1's width.",
     [(OUTCOME_HOLDS,
       "  ELSE LET run == RunOp(st, Ops[o].writes)\n"
       "       \\* FAULT: RowGen conditions are not checked.\n"
       "       IN IF (\\A x \\in Ops[o].expect : x.k = \"rowGen\" \\/ Cond(st, x)) /\\ run.ok\n"
       "             /\\ CursorsFit(Ops[o].writes, last)", 1)]),
    ("StoreWorkEntryUnchecked", "WidthKept", "deal",
     "No work-entry check: a state write that takes a held card into\n"
     "working is not width-checked: s10 makes u1 and u2 working on the\n"
     "width-1 row r2 in one op and is accepted (the shallowest of several\n"
     "over-fills; e1 then a1 on r1 is another).",
     [("Exists(st, w.c) /\\ w.s \\in CardStates /\\ WorkEntryOK(st, w, L)",
       "Exists(st, w.c) /\\ w.s \\in CardStates   \\* FAULT: no WorkEntryOK", 1)]),
    ("StoreJudgementTwice", "JudgementClosedOnce", "records",
     "Answer and expire refuse only an absent judgement, not a closed one:\n"
     "h1 answers j1, and h2, which carries no JudgementOpen fence, answers\n"
     "it again.",
     [("    [] w.k = \"closeJudge\"  -> st.judgements[w.c] = \"open\"",
       "    [] w.k = \"closeJudge\"  -> st.judgements[w.c] # Absent   \\* FAULT: a closed one too", 1)]),
    ("StoreCursorBack", "CursorMonotone", "records",
     "Set cursor accepts any value: m1 sets c1 to 2, then m2 sets it to 1.",
     [("    [] w.k = \"setCursor\"   -> w.w >= st.cursors[w.c]",
       "    [] w.k = \"setCursor\"   -> TRUE   \\* FAULT: a cursor may move back", 1)]),
    ("StoreCursorPastLog", "CursorWithinLog", "records",
     "Set cursor is not bounded by the log: m3 sets c1 past the last\n"
     "event's seq and is accepted.",
     [("  \\A k \\in DOMAIN ws : ws[k].k = \"setCursor\" => ws[k].w <= last",
       "  TRUE   \\* FAULT: no upper bound", 1)]),
    ("StoreServesUnchecked", "ServesHeld", "deal",
     "No serving check: put card and the move into ready or working do not\n"
     "read the holder row's tiers: s15 readies u4 on r2, which serves flash\n"
     "only, and is accepted.",
     [("Serves(st, r, t) == t \\in st.rows[r].tiers",
       "Serves(st, r, t) == TRUE   \\* FAULT: no serving check", 1)]),
    ("StoreTiersNarrowed", "ServesHeld", "deal",
     "Put row and set row tiers may drop a tier a live consumer needs: v1\n"
     "makes r1 serve flash only while the pro consumer u3 is ready there.",
     [("  \\A c \\in Cards : (st.cards[c].holder = r /\\ st.cards[c].state \\in Live) => st.cards[c].tier \\in ts",
       "  TRUE   \\* FAULT: any tiers are accepted", 1)]),
    ("StoreLogTampered", "LogAppendOnly", "deal",
     "The events table takes an UPDATE (the trigger is missing): an action\n"
     "flips a logged event's outcome.",
     [("Terminating == ",
       "(* FAULT: an UPDATE on the events table flips an event's outcome.        *)\n"
       "Tamper ==\n"
       "  /\\ \\E i \\in DOMAIN events : events' = [events EXCEPT ![i].accepted = ~@]\n"
       "  /\\ UNCHANGED <<store, ledger, applied, phase, receipt, lost>>\n"
       "\n"
       "Terminating == ", 1),
      ("  \\/ \\E o \\in OpIds : Apply(o) \\/ LoseRequest(o) \\/ Replay(o)\n",
       "  \\/ \\E o \\in OpIds : Apply(o) \\/ LoseRequest(o) \\/ Replay(o)\n  \\/ Tamper\n", 1)]),
]


def die(msg):
    sys.stderr.write("make-witnesses: " + msg + "\n")
    sys.exit(1)


def header(name, fault, target):
    title = " MODULE " + name + " "
    left = (77 - len(title)) // 2
    line = "-" * left + title + "-" * (77 - len(title) - left)
    lines = ["REVERSED WITNESS. FAULT:"] + fault.split("\n")
    lines += ["TLC must report " + target + " violated. Everything else is",
              "Store.tla unchanged (generated by make-witnesses.py)."]
    for l in lines:
        if len(l) > 71:
            die(name + ": header line over 71 characters: " + l)
    box = ["(" + "*" * 75 + ")"]
    box += ["(* " + l.ljust(71) + " *)" for l in lines]
    box += ["(" + "*" * 75 + ")"]
    return line, "\n".join(box)


# Invariants a witness leaves out of its cfg, with the reason, because its
# fault breaks them in a shallower state than its target and TLC stops at
# the first violation.
DROP = {
    "StoreWidthStale": (["NoSpuriousRefusal"],
                        "a stale count also refuses a deal the live state allows (NoSpuriousRefusal),\n"
                        "\\* found before the over-fill; left out so the over-fill is the one reported."),
}


def constants(scenario):
    return "CONSTANT MaxLoss = 1\nCONSTANT Scenario = \"%s\"\nSPECIFICATION Spec\n" % scenario


def cfg(name, target, scenario):
    out = ["\\* Reversed witness: TLC must report " + target + " violated.",
           "\\* The target invariant is listed first so it is the one TLC names."]
    drop, why = DROP.get(name, ([], ""))
    if drop:
        out.append("\\* Left out: " + why)
    text = "\n".join(out) + "\n" + constants(scenario)
    text += "".join("INVARIANT " + i + "\n"
                    for i in [target] + [i for i in INVARIANTS if i != target and i not in drop])
    return text


def main():
    with open(os.path.join(HERE, "Store.tla")) as f:
        base = f.read()
    first = base.split("\n", 1)
    if "MODULE Store " not in first[0]:
        die("Store.tla does not start with its module line")
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
            f.write(cfg(name, target, scenario))
    for name, scenario in PASSING.items():
        with open(os.path.join(HERE, name + ".cfg"), "w") as f:
            f.write(constants(scenario) + "".join("INVARIANT " + i + "\n" for i in INVARIANTS))
    for name, (scenario, what) in REACH.items():
        if "\n" + name + " " not in base and "\n" + name + "=" not in base:
            die("reach check " + name + " is not defined in Store.tla")
        with open(os.path.join(HERE, "reach", name + ".cfg"), "w") as f:
            f.write("\\* Reachability: TLC must report %s violated (%s).\n" % (name, what)
                    + constants(scenario) + "INVARIANT %s\n" % name)


main()
