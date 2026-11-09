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
    "IdempotentById", "AppliedIsLog", "LogMatchesState", "OpsInTheirEpoch",
    "ConditionsAtomic", "NoChangeOnRefusal", "NoSpuriousRefusal", "GenPerOp",
    "ClearCarriesRows", "JudgementClosedOnce", "CursorMonotone", "CursorWithinLog",
    "LogAppendOnly", "PolicyImmutable", "ReceiptMatches", "RedOnlyOnProducers", "HookKept",
    "FreshMovesNothing",
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
    "NeverTwoWorking": ("deal", "the store takes two working consumers on one row (a1, b2): width is the roster's"),
    "NeverProducerRefused": ("deal", "a producer added with a holder is refused (s3)"),
    "NeverSentinelRefused": ("deal", "a sentinel added with a holder is refused (s4)"),
    "NeverHoldReleased": ("deal", "a clear releases a row's hold (g1, then k1)"),
    "NeverHeldRefused": ("deal", "a deal is refused on RowHeld after a hold (a1)"),
    "NeverRowDown": ("deal", "a fenced row state write is accepted (e1)"),
    "NeverTwoPolicies": ("deal", "a second policy version is put beside the first (s13)"),
    "NeverRed": ("deal", "a producer is set red (s14)"),
    "NeverRedKindRefused": ("deal", "a red write on a consumer (s15) and on a sentinel (s16) is refused"),
    "NeverSameRedRefused": ("deal", "a red write equal to the producer's flag is refused (s17)"),
    "NeverRowFenced": ("deal", "a row write is refused on a stale RowGen (e1)"),
    "NeverHookRefused": ("deal", "a hook by another session while one holds the row is refused (s18)"),
    "NeverRehooked": ("deal", "an unhooked row is hooked by another session as coordinator (s19, s20)"),
    "NeverDealtStopped": ("deal", "a deal lands while the sprint is stopped (s21, a1)"),
    "NeverSameStopRefused": ("deal", "a stop write equal to the sprint's stop is refused (s22)"),
    "NeverStoppedRefused": ("deal", "an op fenced on Running (s23) is refused once the sprint is stopped"),
    "NeverFreshRefused": ("deal", "a tick op on the fresh store (n1) is refused: the sprint was never started"),
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
    ("vars == <<epoch, cards, rows, judgements, cursors, policies, stopped, events, ledger, applied, phase, receipt, lost>>",
     "vars == <<epoch, cards, rows, judgements, cursors, policies, stopped, events, ledger, applied, phase, receipt, lost, snap>>", 1),
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
     "                       policies |-> pre.policies, stopped |-> pre.stopped,\n"
     "                       sn |-> snap[o], p0 |-> pre], o, Len(events))\n"
     "      post == [epoch |-> out.st.epoch, cards |-> out.st.cards, rows |-> out.st.rows,\n"
     "               judgements |-> out.st.judgements, cursors |-> out.st.cursors,\n"
     "               policies |-> out.st.policies, stopped |-> out.st.stopped]\n", 1),
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
       "     /\\ policies'   = out.st.policies\n"
       "     /\\ stopped'    = out.st.stopped\n"
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
    ("StoreEpochUnfenced", "OpsInTheirEpoch", "records",
     "Ops other than clear are not checked against the store's epoch, so an\n"
     "op planned against epoch 1 arriving after the clear lands in epoch 2\n"
     "(m1, unfenced, sets c1 to 2 in epoch 2). Records, not deal: every\n"
     "epoch-1 deal op is refused in epoch 2 by a fence or a write rule (the\n"
     "clear raises r1's gen and sets it down, and leaves p2 behind).",
     [("  IF Ops[o].epoch # st.epoch THEN [acc |-> FALSE, st |-> st]\n"
       "  ELSE IF Ops[o].kind = \"clear\"",
       "  \\* FAULT: only the clear is fenced by epoch.\n"
       "  IF Ops[o].kind = \"clear\" /\\ Ops[o].epoch # st.epoch THEN [acc |-> FALSE, st |-> st]\n"
       "  ELSE IF Ops[o].kind = \"clear\"", 1)]),
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
    ("StoreRowStale", "ConditionsAtomic", "deal",
     "The store does not check RowGen: g1 holds r1 (its gen rises to 1), and\n"
     "e1, fenced on RowGen(r1, 0), still sets r1 down.",
     [(OUTCOME_HOLDS,
       "  ELSE LET run == RunOp(st, Ops[o].writes)\n"
       "       \\* FAULT: RowGen conditions are not checked.\n"
       "       IN IF (\\A x \\in Ops[o].expect : x.k = \"rowGen\" \\/ Cond(st, x)) /\\ run.ok\n"
       "             /\\ CursorsFit(Ops[o].writes, last)", 1)]),
    ("StoreWidthInStore", "NoSpuriousRefusal", "deal",
     "The store still caps a row at one working consumer (the width check\n"
     "round 22 moved to the roster): with a1's u1 working on r1, b2 making\n"
     "u2 working on r1 is refused. The design's rule (SpecLegal) has no\n"
     "width, so the refusal has no cause in the spec.",
     [("Legal(st, w)     == RuleLegal(st, w)\n",
       "\\* FAULT: a width of one per row, enforced by the store.\n"
       "Legal(st, w)     ==\n"
       "  /\\ RuleLegal(st, w)\n"
       "  /\\ (w.k = \"setState\" /\\ w.s = \"working\" /\\ st.cards[w.c].holder # NoHolder)\n"
       "       => ~\\E x \\in Cards \\ {w.c} : /\\ st.cards[x].holder = st.cards[w.c].holder\n"
       "                                   /\\ st.cards[x].state = \"working\"\n", 1)]),
    ("StoreHookStolen", "HookKept", "deal",
     "Put row takes any session, whatever session holds the row: s18\n"
     "hooks r2 by y while x holds it and is accepted.",
     [("    [] w.k = \"putRow\"      -> ~(/\\ st.rows[w.r].state # Absent\n"
       "                                /\\ st.rows[w.r].sess # None\n"
       "                                /\\ w.s # None\n"
       "                                /\\ w.s # st.rows[w.r].sess)\n",
       "    [] w.k = \"putRow\"      -> TRUE   \\* FAULT: no session check\n", 1)]),
    ("StoreHoldCarried", "ClearCarriesRows", "deal",
     "A clear carries a row's hold into the next epoch (the defect of\n"
     "2026-11-08: emma, johnny and stella came up in epoch 2 still held\n"
     "with epoch 1's reasons): after g1 holds r1, k1 opens epoch 2 with r1\n"
     "held.",
     [("ELSE [st.rows[r] EXCEPT !.state = \"down\", !.gen = @ + 1, !.held = FALSE]],",
       "ELSE [st.rows[r] EXCEPT !.state = \"down\", !.gen = @ + 1]],   \\* FAULT: the hold is carried", 1)]),
    ("StoreStopDropped", "ClearCarriesRows", "deal",
     "A clear does not keep the sprint's stop: after s21 stops it, k1\n"
     "opens epoch 2 running.",
     [("   policies |-> st.policies,\n   stopped |-> st.stopped]",
       "   policies |-> st.policies,\n   stopped |-> FALSE]   \\* FAULT: the stop is not carried", 1)]),
    ("StoreFreshRunning", "FreshMovesNothing", "deal",
     "A fresh store begins running (the defect of 2026-11-07: the run\n"
     "ticked while up was at step 7): n1, a tick op fenced on Running,\n"
     "lands before any start.",
     [("policies |-> [v \\in Versions |-> Absent], stopped |-> TRUE]   \\* a fresh store: never started",
       "policies |-> [v \\in Versions |-> Absent], stopped |-> FALSE]   \\* FAULT: a fresh store runs", 1)]),
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
    ("StorePolicyRewritten", "PolicyImmutable", "deal",
     "Put policy writes the latest version instead of adding the next (or\n"
     "the immutability trigger is missing): s13 overwrites version 1's body.",
     [("NextVersion(st) == Cardinality({v \\in Versions : st.policies[v] # Absent}) + 1",
       "\\* FAULT: the latest version, not the next.\n"
       "NextVersion(st) == LET n == Cardinality({v \\in Versions : st.policies[v] # Absent})\n"
       "                   IN IF n = 0 THEN 1 ELSE n", 1)]),
    ("StoreRedOnConsumer", "RedOnlyOnProducers", "deal",
     "The red write checks only that the card exists, not its kind: s15\n"
     "sets the consumer u3 red and is accepted.",
     [("    [] w.k = \"setRed\"      -> Exists(st, w.c) /\\ st.cards[w.c].kind = \"producer\"",
       "    [] w.k = \"setRed\"      -> Exists(st, w.c)   \\* FAULT: no kind check", 1)]),
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
DROP = {}


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
