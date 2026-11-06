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

# Every invariant of the passing run, in cfg order.
INVARIANTS = [
    "TypeOK", "SingleStateSingleHolder", "ProducerNeverHeld", "ConsumersWellFormed",
    "WidthKept", "IdempotentById", "AppliedIsLog", "LogMatchesState", "OpsInTheirEpoch",
    "ConditionsAtomic", "NoChangeOnRefusal", "NoSpuriousRefusal", "GenPerOp", "ClearCarriesRows",
    "ReceiptMatches",
]

# Reachability checks: each must be violated on Store.tla.
REACH = {
    "NeverTaken": "an accepted deal (a1) is reachable",
    "NeverRecovered": "a caller recovers from a transport failure",
    "NeverStaleEpochRefused": "an op planned against a closed epoch is refused",
    "NeverNeedsReady": "a card whose needs landed becomes ready (s9)",
    "NeverNewEpochOp": "an op in the new epoch is accepted (k2)",
    "NeverWidthFull": "a row holds width live cards",
    "NeverWidthLowered": "a width is lowered (e1)",
    "NeverWidthRefused": "a width below the live holders is refused (e1)",
    "NeverInOpWidthRefused": "an op is refused on its own second holder write (s10)",
    "NeverProducerRefused": "a holder write on a producer is refused (s5)",
    "NeverSentinelRefused": "a holder write on a sentinel is refused (s6)",
    "NeverHeldCarried": "a row's hold survives the clear",
    "NeverWorkEntryRefused": "a held card going working on a full row is refused (b2)",
    "NeverQueuedPastWidth": "a row holds a ready consumer past its width (r1)",
    "NeverHeldRefused": "a deal is refused on RowHeld after a hold (a1)",
    "NeverRowFenced": "a row write is refused on a stale RowGen (e1)",
    "NeverSlotReused": "a done consumer's slot is reused while it keeps its holder (b2)",
}

# The two-step transaction (Begin reads a snapshot, Apply commits on it),
# shared by the stale witnesses. The snapshot and the state at commit start
# ride in the state record as extra fields sn and p0 (the rebuild path never
# has them), and are stripped before the state is stored.
STALE_INFRA = [
    ("  lost      \\* op id -> transport failures so far (bounded by MaxLoss)",
     "  lost,     \\* op id -> transport failures so far (bounded by MaxLoss)\n"
     "  snap      \\* op id -> the state its transaction read at Begin (FAULT)", 1),
    ("vars == <<epoch, cards, rows, events, applied, phase, receipt, lost>>",
     "vars == <<epoch, cards, rows, events, applied, phase, receipt, lost, snap>>", 1),
    ("  /\\ lost    = [o \\in OpIds |-> 0]",
     "  /\\ lost    = [o \\in OpIds |-> 0]\n  /\\ snap    = [o \\in OpIds |-> Empty]", 1),
    ("  /\\ UNCHANGED <<epoch, cards, rows, events, applied, lost>>",
     "  /\\ UNCHANGED <<epoch, cards, rows, events, applied, lost, snap>>", 1),
    ("  /\\ UNCHANGED <<epoch, cards, rows, events, applied, receipt>>",
     "  /\\ UNCHANGED <<epoch, cards, rows, events, applied, receipt, snap>>", 1),
    ("      out  == Outcome(pre, o)\n      post == out.st\n",
     "      \\* FAULT: the op is judged with the snapshot its Begin read.\n"
     "      out  == Outcome([epoch |-> pre.epoch, cards |-> pre.cards, rows |-> pre.rows,\n"
     "                       sn |-> snap[o], p0 |-> pre], o)\n"
     "      post == [epoch |-> out.st.epoch, cards |-> out.st.cards, rows |-> out.st.rows]\n", 1),
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
     "  /\\ UNCHANGED <<epoch, cards, rows, events, applied, receipt, lost>>\n"
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

WITNESSES = [
    ("StoreNoEvent", "LogMatchesState",
     "An accepted op's writes land but its event row is not appended (and so\n"
     "its id is not remembered).",
     [("     /\\ events'  = Append(events, ev)\n     /\\ applied' = applied \\cup {o}\n",
       "     \\* FAULT: the accepted path forgets its event row.\n"
       "     /\\ events'  = IF out.acc THEN events ELSE Append(events, ev)\n"
       "     /\\ applied' = IF out.acc THEN applied ELSE applied \\cup {o}\n", 1)]),
    ("StoreReplayApplies", "IdempotentById",
     "Replay runs the op again and logs it again instead of returning the\n"
     "stored receipt.",
     [("ReplayEffect(o) ==\n"
       "  /\\ phase'   = [phase EXCEPT ![o] = \"done\"]\n"
       "  /\\ receipt' = [receipt EXCEPT ![o] = StoredEvent(o)]\n"
       "  /\\ UNCHANGED <<epoch, cards, rows, events, applied, lost>>\n",
       "\\* FAULT: replay runs the op again and logs a new event.\n"
       "ReplayEffect(o) ==\n"
       "  LET pre == Current\n"
       "      out == Outcome(pre, o)\n"
       "      ev  == [id |-> o, accepted |-> out.acc, epoch |-> pre.epoch, before |-> pre, after |-> out.st]\n"
       "  IN /\\ epoch'   = out.st.epoch\n"
       "     /\\ cards'   = out.st.cards\n"
       "     /\\ rows'    = out.st.rows\n"
       "     /\\ events'  = Append(events, ev)\n"
       "     /\\ phase'   = [phase EXCEPT ![o] = \"done\"]\n"
       "     /\\ receipt' = [receipt EXCEPT ![o] = ev]\n"
       "     /\\ UNCHANGED <<applied, lost>>\n", 1)]),
    ("StoreStaleCheck", "ConditionsAtomic",
     "Apply is two steps: Begin reads a snapshot, and the commit checks the\n"
     "op's conditions on that snapshot, not on the state its writes change\n"
     "(no lock): a1 reads r1 not held, g1 holds r1, a1 lands.",
     STALE_INFRA + [
         ("  ELSE LET run == RunOp(st, Ops[o].writes)\n"
          "       IN IF Holds(st, Ops[o].expect) /\\ run.ok",
          "  ELSE LET run == RunOp(st, Ops[o].writes)\n"
          "       \\* FAULT: the conditions are read on the snapshot.\n"
          "       IN IF Holds(Snap(st), Ops[o].expect) /\\ run.ok", 1)]),
    ("StoreTwoHolders", "SingleStateSingleHolder",
     "The holder is modelled as a set of rows (as a join table would allow)\n"
     "so the fault is expressible: set holder adds the new row without\n"
     "clearing the old one, and s12 leaves u3 held by both r2 and r1.",
     [("NoCard == [state |-> Absent, gen |-> 0, holder |-> NoHolder,",
       "NoCard == [state |-> Absent, gen |-> 0, holder |-> {},", 1),
      ("Exists(st, c) == st.cards[c].state # Absent\n",
       "Exists(st, c) == st.cards[c].state # Absent\n"
       "HolderSet(r) == IF r = NoHolder THEN {} ELSE {r}\n", 1),
      ("CountedIn(st, r, L) == {c \\in Cards : st.cards[c].holder = r /\\",
       "CountedIn(st, r, L) == {c \\in Cards : r \\in st.cards[c].holder /\\", 1),
      ("Exists(st, x.c) /\\ st.cards[x.c].holder = x.r",
       "Exists(st, x.c) /\\ st.cards[x.c].holder = HolderSet(x.r)", 1),
      ("card.holder # NoHolder)\n       => Room(st, card.holder, w.c, L)",
       "card.holder # {})\n       => \\A h \\in card.holder : Room(st, h, w.c, L)", 1),
      ("[state |-> \"waiting\", gen |-> 0, holder |-> NoHolder,",
       "[state |-> \"waiting\", gen |-> 0, holder |-> {},", 1),
      ("    [] w.k = \"setHolder\"   -> [st EXCEPT !.cards[w.c].holder = w.r]",
       "    \\* FAULT: adds to the holders instead of replacing the one holder.\n"
       "    [] w.k = \"setHolder\"   -> [st EXCEPT !.cards[w.c].holder =\n"
       "                                 IF w.r = NoHolder THEN {} ELSE @ \\cup {w.r}]", 1),
      ("holder : Rows \\cup {NoHolder}", "holder : SUBSET Rows", 1),
      ("    /\\ cards[c].holder \\in Rows \\cup {NoHolder}\n"
       "    /\\ cards[c].holder # NoHolder => rows[cards[c].holder].state # Absent\n",
       "    /\\ Cardinality(cards[c].holder) <= 1\n"
       "    /\\ \\A r \\in cards[c].holder : rows[r].state # Absent\n", 1),
      ("cards[c].kind \\in Unheld => cards[c].holder = NoHolder",
       "cards[c].kind \\in Unheld => cards[c].holder = {}", 1)]),
    ("StoreEpochUnfenced", "OpsInTheirEpoch",
     "Ops other than clear are not checked against the store's epoch, so an\n"
     "op planned against epoch 1 arriving after the clear lands in epoch 2\n"
     "(x1 sets r2 up in epoch 2).",
     [("  IF Ops[o].epoch # st.epoch THEN [acc |-> FALSE, st |-> st]\n"
       "  ELSE IF Ops[o].kind = \"clear\"",
       "  \\* FAULT: only the clear is fenced by epoch.\n"
       "  IF Ops[o].kind = \"clear\" /\\ Ops[o].epoch # st.epoch THEN [acc |-> FALSE, st |-> st]\n"
       "  ELSE IF Ops[o].kind = \"clear\"", 1)]),
    ("StoreWidthStale", "WidthKept",
     "Apply is two steps, and the width check (count and width) is read\n"
     "on the snapshot Begin took, plus this op's own writes: a1 begins,\n"
     "e1 lowers r1's width to 0, and a1 lands u1 working on the stale\n"
     "width 1.",
     STALE_INFRA + [
         ("Room(st, r, c, L) == Cardinality(CountedIn(st, r, L) \\ {c}) < st.rows[r].width\n",
          "\\* FAULT: counted on the snapshot, plus the live holders this op added.\n"
          "Room(st, r, c, L) ==\n"
          "  IF \"sn\" \\in DOMAIN st\n"
          "  THEN Cardinality(CountedIn(st.sn, r, L) \\ {c})\n"
          "         + Cardinality((CountedIn(st, r, L) \\ CountedIn(st.p0, r, L)) \\ {c})\n"
          "       < st.sn.rows[r].width\n"
          "  ELSE Cardinality(CountedIn(st, r, L) \\ {c}) < st.rows[r].width\n", 1)]),
    ("StoreWidthUnchecked", "WidthKept",
     "Put row and set row width accept any width, whatever the row holds:\n"
     "a1 deals u1 to r1, then e1 lowers r1's width to 0 and is accepted.",
     [("    [] w.k = \"putRow\"      -> st.rows[w.r].state = Absent \\/ w.w >= Cardinality(CountedIn(st, w.r, L))\n"
       "    [] w.k = \"setRowWidth\" -> st.rows[w.r].state # Absent /\\ w.w >= Cardinality(CountedIn(st, w.r, L))\n",
       "    \\* FAULT: any width is accepted, whatever the row holds.\n"
       "    [] w.k = \"putRow\"      -> TRUE\n"
       "    [] w.k = \"setRowWidth\" -> st.rows[w.r].state # Absent\n", 1)]),
    ("StoreWidthPerOp", "WidthKept",
     "The holder write counts the row's live holders as at the start of the\n"
     "op, not at the moment of the write: s10 puts two ready consumers on\n"
     "the width-1 row r2 in one op and is accepted.",
     [("RECURSIVE SpecRun(_, _)\n",
       "\\* FAULT: the holder write's count is read in pre, the state before the op.\n"
       "PreLegal(pre, st, w) ==\n"
       "  IF w.k = \"setHolder\" /\\ w.r # NoHolder\n"
       "  THEN Exists(st, w.c) /\\ st.cards[w.c].kind \\notin Unheld /\\ st.rows[w.r].state # Absent\n"
       "       /\\ (st.cards[w.c].state \\in Counted => Room(pre, w.r, w.c, Counted))\n"
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
    ("StoreProducerHolds", "ProducerNeverHeld",
     "The holder write does not refuse a producer (or a sentinel): s5 sets\n"
     "producer p1's holder to r1 and is accepted.",
     [("                                  \\/ (st.cards[w.c].kind \\notin Unheld\n"
       "                                      /\\ st.rows[w.r].state # Absent\n",
       "                                  \\* FAULT: no producer or sentinel check.\n"
       "                                  \\/ (st.rows[w.r].state # Absent\n", 1)]),
    ("StoreSentinelHolds", "ProducerNeverHeld",
     "The holder write refuses a producer but not a sentinel (Go's check,\n"
     "kind == producer): s6 sets sentinel t1's holder to r1 and is accepted.",
     [("st.cards[w.c].kind \\notin Unheld\n",
       "st.cards[w.c].kind # \"producer\"   \\* FAULT: sentinels pass\n", 1)]),
    ("StoreWidthCountsDone", "NoSpuriousRefusal",
     "The store's width count also counts done and fail holders (the design:\n"
     "they keep their holder and do not count): once u1 is done on r1, e1\n"
     "lowering r1's width to 0 is refused although r1 holds no working card.\n"
     "(b2 making u2 working on r1 is refused the same way.) The\n"
     "design's rule (SpecLegal) counts working only, so the refusal has no\n"
     "cause in the spec: an invariant, NoSpuriousRefusal, not a reach check.",
     [("Legal(st, w)     == RuleLegal(st, w, Counted)\n",
       "\\* FAULT: done and fail holders are counted too.\n"
       "Legal(st, w)     == RuleLegal(st, w, Counted \\cup {\"done\", \"fail\"})\n", 1)]),
    ("StoreWidthCountsReady", "NoSpuriousRefusal",
     "The store's width count also counts ready holders (live, as Go counts;\n"
     "the design: ready is queued, not counted): with u3 ready on r1, a1\n"
     "dealing u1 working is refused although r1 has no working holder. An\n"
     "invariant, NoSpuriousRefusal, as in StoreWidthCountsDone.",
     [("Legal(st, w)     == RuleLegal(st, w, Counted)\n",
       "\\* FAULT: ready holders are counted too.\n"
       "Legal(st, w)     == RuleLegal(st, w, Counted \\cup {\"ready\"})\n", 1)]),
    ("StoreRowStale", "ConditionsAtomic",
     "The store does not check RowGen: g1 holds r1 (its gen rises to 1), and\n"
     "e1, fenced on RowGen(r1, 0), still lowers r1's width.",
     [("  ELSE LET run == RunOp(st, Ops[o].writes)\n"
       "       IN IF Holds(st, Ops[o].expect) /\\ run.ok",
       "  ELSE LET run == RunOp(st, Ops[o].writes)\n"
       "       \\* FAULT: RowGen conditions are not checked.\n"
       "       IN IF (\\A x \\in Ops[o].expect : x.k = \"rowGen\" \\/ Cond(st, x)) /\\ run.ok", 1)]),
    ("StoreWorkEntryUnchecked", "WidthKept",
     "No work-entry check: only the holder write on a working card is\n"
     "width-checked, and a state write that takes a held card into working\n"
     "is not: e1 lowers r1's width to 0, then a1 holds u1 on r1 while it\n"
     "waits and makes it working: one working holder on a width-0 row.",
     [("Exists(st, w.c) /\\ w.s \\in CardStates /\\ WorkEntryOK(st, w, L)",
       "Exists(st, w.c) /\\ w.s \\in CardStates   \\* FAULT: no WorkEntryOK", 1)]),
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


def cfg(name, target):
    out = ["\\* Reversed witness: TLC must report " + target + " violated.",
           "\\* The target invariant is listed first so it is the one TLC names."]
    drop, why = DROP.get(name, ([], ""))
    if drop:
        out.append("\\* Left out: " + why)
    out += ["CONSTANT MaxLoss = 1", "SPECIFICATION Spec"]
    out += ["INVARIANT " + i for i in [target] + [i for i in INVARIANTS if i != target and i not in drop]]
    return "\n".join(out) + "\n"


def main():
    with open(os.path.join(HERE, "Store.tla")) as f:
        base = f.read()
    first = base.split("\n", 1)
    if "MODULE Store " not in first[0]:
        die("Store.tla does not start with its module line")
    for name, target, fault, reps in WITNESSES:
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
            f.write(cfg(name, target))
    with open(os.path.join(HERE, "Store.cfg"), "w") as f:
        f.write("CONSTANT MaxLoss = 1\nSPECIFICATION Spec\n" +
                "".join("INVARIANT " + i + "\n" for i in INVARIANTS))
    for name, what in REACH.items():
        if "\n" + name + " " not in base and "\n" + name + "=" not in base:
            die("reach check " + name + " is not defined in Store.tla")
        with open(os.path.join(HERE, "reach", name + ".cfg"), "w") as f:
            f.write("\\* Reachability: TLC must report %s violated (%s).\n"
                    "CONSTANT MaxLoss = 1\nSPECIFICATION Spec\nINVARIANT %s\n" % (name, what, name))


main()
