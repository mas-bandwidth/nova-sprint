#!/usr/bin/env python3
"""Generate the step model's reversed witnesses and every cfg from Steps.tla.

Run from anywhere: python3 tla/make-steps-witnesses.py.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# Every invariant of the passing runs, in cfg order.
INVARIANTS = [
    "TypeOK", "NeverDealtTwice", "WidthKept", "ReadyNeverFlipsWithLoad",
    "NeverBelowTier", "CarryFIFO", "EdgeAfterAccepted",
    "OneGroupPerCard", "GroupWhole", "NeverHalfApplied", "RefusalChangesNothing",
    "RefusedOnlyByRace", "BudgetKept", "BlockerPreemptsInOneTick", "PreemptYoungest",
    "PolicyFailsToDefault", "AfterKept", "AfterNeverHolds", "MergingCovered", "NoDealWithoutPolicy",
    "ResolveFromState", "DealIsTake", "DealtWithinKTicks", "VerdictHasItsRead",
    "NeverDealtAbsent", "NeverDealtUnadmitted", "AdmissionShown", "LandedOnMain", "ExpiryNeverSilent",
    "LapsedReleased", "HandEmptyWhenClosed", "OfferedIsAccepted",
    "NoDealBehindPause", "NoDealOverClaim", "NoFutilePreempt", "PreemptMultiStops",
]
PROPERTIES = ["ReadyDealtWhenRoom", "FaultedReadReadAgain"]

CONST_ORDER = ["K", "MaxAtt", "MaxBounce", "MaxEnv", "MaxArrive", "Budget", "Pack"]

# The instance of each scenario (see the module header).
SCENARIOS = {
    "pre":   {"K": 2, "MaxAtt": 2, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 2, "Budget": 2, "Pack": 2},
    "deal":  {"K": 4, "MaxAtt": 2, "MaxBounce": 1, "MaxEnv": 1, "MaxArrive": 1, "Budget": 2, "Pack": 2},
    "land":  {"K": 2, "MaxAtt": 1, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 3, "Budget": 2, "Pack": 2},
    "burst": {"K": 2, "MaxAtt": 2, "MaxBounce": 0, "MaxEnv": 0, "MaxArrive": 4, "Budget": 1, "Pack": 1},
    "pre1":  {"Scenario": "pre", "K": 2, "MaxAtt": 2, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 2,
              "Budget": 2, "Pack": 1},
}

PASSING = {"StepsPre": "pre", "StepsPrePack1": "pre1", "StepsDeal": "deal", "StepsLand": "land",
           "StepsBurst": "burst"}

# Reachability checks on Steps.tla: name -> (scenario, what).
REACH = {
    "StepsNeverPreempted": ("pre", "a blocker preempts a working consumer"),
    "StepsNeverMultiPreempted": ("pre", "a blocker preempts multiple victims in one atomic group"),
    "StepsNeverPausedSkipped": ("pre", "a ready producer waits behind an AnyPaused bench"),
    "StepsNeverUnserved": ("pre", "a ready blocker with no open serving row opens unserved"),
    "StepsNeverRefused": ("pre", "an op is refused by a race"),
    "StepsNeverSplit": ("pre", "a group of a refused op is packed as an op of its own"),
    "StepsNeverStopAcked": ("pre", "a runner's stop ack is consumed"),
    "StepsNeverPolicyJudged": ("pre", "a failed policy answer opens a policy judgement"),
    "StepsNeverGated": ("pre", "the policy gate opens a no-policy judgement"),
    "StepsNeverResolved": ("deal", "Resolve makes a producer ready"),
    "StepsNeverDealt": ("deal", "the deal starts a producer"),
    "StepsNeverReadAsked": ("deal", "the deal cuts a read"),
    "StepsNeverMerged": ("deal", "the fix step's ReadYes"),
    "StepsNeverFixed": ("deal", "a producer back from a bounce (ReadFix or Broken)"),
    "StepsNeverEscalated": ("deal", "a bounced producer is dealt one rank up"),
    "StepsNeverNeedReleased": ("deal", "a need lands and its dependant resolves"),
    "StepsNeverCannotRead": ("deal", "a review no row may read opens cannot-read"),
    "StepsNeverBothStarted": ("deal", "p and q of one stream, naming one file, are started together"),
    "StepsNeverHandLanded": ("deal", "a store op lands a producer by hand"),
    "StepsNeverReadAgain": ("deal", "a review whose read faulted is read again"),
    "StepsNeverAbsent": ("deal", "a tick runs with an up row absent from the bus serving a ready card"),
    "StepsNeverUnadmitted": ("deal", "a producer failing admission opens its stream's admission judgement"),
    "StepsNeverReopened": ("deal", "a judgement whose cause still holds at its deadline is reopened"),
    "StepsNeverReleased": ("pre", "a lapsed working consumer is withdrawn back to the queue"),
    "StepsNeverLandRequested": ("land", "a land request is dispatched"),
    "StepsNeverLanded": ("land", "a landed result lands a producer"),
    "StepsNeverRed": ("land", "the coordinator's fix sends a judged producer to fix"),
    "StepsNeverLandJudged": ("land", "a result other than landed opens a land judgement"),
    "StepsNeverAfterHeld": ("land", "a landing waits on a queued after target"),
    "StepsNeverOnBase": ("land", "a landed result on a workstream base records the base merge"),
    "StepsNeverPromoteRequested": ("land", "a promote request is dispatched for a producer on base"),
    "StepsNeverLandedOffMainBase": ("land", "a producer of a workstream base lands (on main)"),
    "StepsNeverCarried": ("burst", "the budget carries inbox items to the next tick"),
    "StepsNeverUndealt": ("burst", "a producer left ready with room two ticks opens undealt"),
}

# (name, target, scenario, fault, replacements).
WITNESSES = [
    ("StepsDealtTwice", "NeverDealtTwice", "pre",
     "The deal does not skip a blocker the preemption step claimed: one\n"
     "tick plans the preemption and a plain Start for the same blocker.\n"
     "The second fault makes preemption fire with free room so the deal\n"
     "can place it too when the Free guard is removed.",
     [("LET bl == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = \"blocker\" /\\ V.ps[p].st = \"ready\" /\\ Free(p, cl) /\\ admitted[p])",
       "LET bl == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = \"blocker\" /\\ V.ps[p].st = \"ready\" /\\ TRUE /\\ admitted[p])   \\* FAULT", 1),
      ("  /\\ NoFreeWidth(W, b)\n", "  /\\ TRUE   \\* FAULT 2: was NoFreeWidth(W, b)\n", 1),
      ("DealStep(V, cl3r, pre.t, a.rows, TRUE)", "DealStep(V, cl3r, ZeroTally, a.rows, TRUE)", 1)]),
    ("StepsTwoGroups", "OneGroupPerCard", "land",
     "The land-result step ignores cards already claimed: a result\n"
     "delivered twice plans two groups on one producer in one tick.",
     [("        /\\ PKey(p) \\notin cl\n", "        /\\ TRUE   \\* FAULT: was PKey(p) \\notin cl\n", 1)]),
    ("StepsWidthOver", "WidthKept", "burst",
     "The deal ignores multi-resource room (R50): a second producer is\n"
     "dealt onto a bench whose resources are already full.",
     [("  /\\ HasRoom(V, r, ConsNeeds(x.kind, x.p), t)\n",
       "  /\\ ~V.paused[r]   \\* FAULT: was HasRoom(V, r, ConsNeeds(x.kind, x.p), t)\n", 1)]),
    ("StepsCutReady", "DealIsTake", "pre",
     "Start cuts its work consumer ready, as before defect 52.",
     [("StartCut(W, p, r) == [st |-> \"working\", kind |-> \"work\",",
       "StartCut(W, p, r) == [st |-> \"ready\", kind |-> \"work\",   \\* FAULT: was working\n                     ", 1)]),
    ("StepsUndealtSilent", "DealtWithinKTicks", "burst",
     "The tick never opens the undealt judgement: a producer the budget\n"
     "leaves ready with room tick after tick sits unworked unseen.",
     [("             /\\ ju' = {p \\in ju : V.ps[p].st = \"ready\"} \\cup {p \\in und : rdy[p] + 1 >= UndealtTicks}",
       "             /\\ ju' = {p \\in ju : V.ps[p].st = \"ready\"}   \\* FAULT: no new undealt judgement", 1)]),
    ("StepsBlockerQueued", "BlockerPreemptsInOneTick", "pre",
     "The preemption step demands more load than capacity before firing,\n"
     "so a ready blocker is queued behind full rows instead of preempting.",
     [("NoFreeWidth(W, b) == \\A r \\in ServingOpen(W, \"work\", b) : ~HasRoom(W, r, CardRes[b], ZeroTally)",
       "NoFreeWidth(W, b) == \\A r \\in ServingOpen(W, \"work\", b) : SprintUsed(W, r).cpu > RowCap[r].cpu   \\* FAULT", 1)]),
    ("StepsHalfWrites", "GroupWhole", "pre",
     "A refused op is split by halves of writes: the preemption's Withdraw\n"
     "is applied without the blocker's Start.",
     [("       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i}])",
       "       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \\cup {i},   \\* FAULT: the Withdraw half\n"
       "                                     !.w = IF ops[i][1].kind = \"pre\"\n"
       "                                           THEN [acc.w EXCEPT !.cs = [c \\in CIds |-> IF c \\in ops[i][1].vs THEN [acc.w.cs[c] EXCEPT !.st = \"fail\"] ELSE acc.w.cs[c]],\n"
       "                                                              !.tko = SelectSeq(acc.w.tko, LAMBDA c : c \\notin ops[i][1].vs)]\n"
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
     [("  /\\ V.up[r]\n  /\\ bus[r]", "  /\\ TRUE   \\* FAULT: was V.up[r]\n  /\\ bus[r]", 1),
      ("MayMove(V, x, r) == IF x.kind = \"work\" THEN StartOK(V, x.p, r) ELSE ReadOK(V, x.p, r)",
       "MayMove(V, x, r) == TRUE   \\* FAULT: was the store's own guard", 1)]),
    ("StepsFenceOnView", "RefusedOnlyByRace", "pre1",
     "Each op is fenced on the view as the tick began, not on the view\n"
     "with the tick's accepted ops applied: an op that reads a card an\n"
     "earlier op of the same tick wrote is refused with no race.",
     [("  ELSE IF OpOK(acc.pv, acc.w, ops[i])", "  ELSE IF OpOK(V, acc.w, ops[i])   \\* FAULT: was acc.pv", 1)]),
    ("StepsBudgetIgnored", "BudgetKept", "burst",
     "The budget is not applied: every group of the tick is admitted.",
     [("          ELSE IF ~acc.stop /\\ (acc.nb = 0 \\/ acc.t + Size(g) <= Budget)",
       "          ELSE IF TRUE   \\* FAULT: was ~acc.stop /\\ (acc.nb = 0 \\/ acc.t + Size(g) <= Budget)", 1)]),
    ("StepsCarryDropped", "CarryFIFO", "burst",
     "The carry drops what the budget left: only the items of refused ops\n"
     "stay in the inbox.",
     [("             /\\ inbox' = SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps)",
       "             /\\ inbox' = SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps   \\* FAULT\n"
       "                                                      /\\ it.stamp \\in {g.stamp : g \\in admIn})", 1)]),
    ("StepsCarryNewestFirst", "CarryFIFO", "burst",
     "The carry is put back newest first.",
     [("             /\\ inbox' = SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps)",
       "             /\\ inbox' = LET X == SelectSeq(inbox, LAMBDA it : it.stamp \\notin ustamps)   \\* FAULT\n"
       "                         IN [i \\in 1..Len(X) |-> X[Len(X) + 1 - i]]", 1)]),
    ("StepsStopEarly", "EdgeAfterAccepted", "pre",
     "A stop request is dispatched from every op, refused ones included\n"
     "(After ignored): a runner is stopped for a consumer never withdrawn.",
     [("              stops == UNION {g.req : g \\in accd}",
       "              stops == UNION {g.req : g \\in UNION {GroupsIn(ops[i]) : i \\in 1..Len(ops)}}   \\* FAULT", 1)]),
    ("StepsReadyOnRoom", "ReadyNeverFlipsWithLoad", "deal",
     "Ready is conditioned on room: Resolve requires an open serving row\n"
     "with room.",
     [("  /\\ NeedsLanded(W, p)\n  /\\ ~W.sh[Stream[p]]\n",
       "  /\\ NeedsLanded(W, p)\n  /\\ ~W.sh[Stream[p]]\n"
       "  /\\ \\E r \\in ServingOpen(W, \"work\", p) : HasRoom(W, r, CardRes[p], ZeroTally)   \\* FAULT\n", 1)]),
    ("StepsTierUnchecked", "NeverBelowTier", "deal",
     "The tier is checked nowhere: policy's rows answer is taken for any up\n"
     "row, and Start does not check that the row serves the card's tier.",
     [("InDomain(V, x, ar, t) == ar \\in Rows /\\ Eligible(V, x, ar, t)",
       "InDomain(V, x, ar, t) == ar \\in Rows /\\ V.up[ar] /\\ bus[ar] /\\ HasRoom(V, ar, ConsNeeds(x.kind, x.p), t)\n"
       "                          /\\ (x.kind = \"read\" => ar # V.ps[x.p].author)   \\* FAULT: Eligible without Serves", 1),
      ("  /\\ ~W.sh[Stream[p]]\n  /\\ Serves(r, \"work\", p)\n  /\\ W.ps[p].att < MaxAtt\n",
       "  /\\ ~W.sh[Stream[p]]\n  /\\ TRUE   \\* FAULT: was Serves(r, \"work\", p)\n  /\\ W.ps[p].att < MaxAtt\n", 1)]),
    ("StepsAfterIgnored", "AfterKept", "land",
     "The land step ignores after: a producer is sent to land while its\n"
     "after target is queued to land.",
     [("LandElig(V, s, cl, J) == {p \\in Merging(V, s, cl) : ~Judged(J, p) /\\ \\A t \\in After[p] : V.ps[t].st # \"merging\"}",
       "LandElig(V, s, cl, J) == {p \\in Merging(V, s, cl) : ~Judged(J, p)}   \\* FAULT: after ignored", 1)]),
    ("StepsAfterHolds", "AfterNeverHolds", "land",
     "The land step holds on any after target not yet landed, queued or\n"
     "not (v1's frozen LAND-AFTER).",
     [("LandElig(V, s, cl, J) == {p \\in Merging(V, s, cl) : ~Judged(J, p) /\\ \\A t \\in After[p] : V.ps[t].st # \"merging\"}",
       "LandElig(V, s, cl, J) == {p \\in Merging(V, s, cl) : ~Judged(J, p) /\\ \\A t \\in After[p] : V.ps[t].st = \"landed\"}   \\* FAULT", 1)]),
    ("StepsLandAfterHoldsUntilLanded", "MergingCovered", "land",
     "The after hold lasts until the earlier card lands, not while it\n"
     "merges: a card sent back to fix leaves the card after it merging\n"
     "with no request and no judgement.",
     [("LandElig(V, s, cl, J) == {p \\in Merging(V, s, cl) : ~Judged(J, p) /\\ \\A t \\in After[p] : V.ps[t].st # \"merging\"}",
       "LandElig(V, s, cl, J) == {p \\in Merging(V, s, cl) : ~Judged(J, p) /\\ \\A t \\in After[p] : V.ps[t].st = \"landed\"}   \\* FAULT: was # \"merging\"", 1)]),
    ("StepsLandedBeforeMain", "LandedOnMain", "land",
     "A landed result is a Landed whatever the stream's base: a merge\n"
     "onto a workstream base records landed before it is on main.",
     [("LandKind(k, p) == IF k = \"promote\" \\/ MainBase[Stream[p]] THEN \"landed\" ELSE \"onbase\"",
       "LandKind(k, p) == \"landed\"   \\* FAULT: was onbase off a main base", 1)]),
    ("StepsPolicyStall", "PolicyFailsToDefault", "deal",
     "A failed rows answer plans nothing for the candidate instead of the\n"
     "default row.",
     [("RowFor(V, x, t, ar) == IF InDomain(V, x, ar, t) THEN ar ELSE DefaultRow(V, x, t)",
       "RowFor(V, x, t, ar) == IF InDomain(V, x, ar, t) THEN ar\n"
       "                       ELSE IF ar = \"failed\" THEN NoR ELSE DefaultRow(V, x, t)   \\* FAULT", 1)]),
    ("StepsOldestVictim", "PreemptYoungest", "pre",
     "Within one priority rung, preemption picks the oldest working\n"
     "consumer instead of the youngest.",
     [("          THEN <<c>> \\o rest",
       "          THEN rest \\o <<c>>   \\* FAULT: oldest first", 1)]),
    ("StepsDealsWithoutPolicy", "NoDealWithoutPolicy", "pre",
     "The policy gate is ignored: the tick preempts and deals on the\n"
     "default rows while no active policy answers.",
     [("{IF V.pol THEN PreStep(V, a) ELSE NoPlan} :",
       "{PreStep(V, a)} :   \\* FAULT: was gated on V.pol", 1),
      ("{IF V.pol THEN DealStep(V, cl3r, pre.t, a.rows, TRUE) ELSE NoPlan} :",
       "{DealStep(V, cl3r, pre.t, a.rows, TRUE)} :   \\* FAULT: was gated on V.pol", 1)]),
    ("StepsResolveOnEvent", "ResolveFromState", "deal",
     "Resolve is driven by the land step's events, not the state: a\n"
     "producer with needs resolves only in a tick that consumes its need's\n"
     "landed result, so a need landed by a store op never releases it.",
     [("\\E res \\in {ResolveSet(V, inb.cl)} :",
       "\\E res \\in {{p \\in ResolveSet(V, inb.cl) :   \\* FAULT: on the event only\n"
       "             \\A n \\in Needs[p] : \\E i \\in 1..Len(inbox) :\n"
       "               inbox[i].out = \"landed\" /\\ inbox[i].id[3][1] = n}} :", 1)]),
    ("StepsYesOnFault", "VerdictHasItsRead", "deal",
     "The verdict default is yes unless a read said fix: a review whose\n"
     "only read faulted is merged with no read that said ok.",
     [("Verdict(W, p) == IF DoneSaid(W, p, \"fix\") THEN \"fix\" ELSE IF DoneSaid(W, p, \"ok\") THEN \"yes\" ELSE \"retry\"",
       "Verdict(W, p) == IF DoneSaid(W, p, \"fix\") THEN \"fix\" ELSE \"yes\"   \\* FAULT: was yes only on an ok read, else retry", 1)]),
    ("StepsFaultCounted", "FaultedReadReadAgain", "deal",
     "A faulted read counts as a read: the review is owed none, the\n"
     "verdict is retry, and the producer sits in review for good.",
     [("ReadOwed(W, p) == W.ps[p].st = \"review\" /\\ ~\\E c \\in CurReads(W, p) : W.cs[c].st # \"fail\" /\\ ~Faulted(W, c)",
       "ReadOwed(W, p) == W.ps[p].st = \"review\" /\\ ~\\E c \\in CurReads(W, p) : W.cs[c].st # \"fail\"   \\* FAULT: a faulted read counted", 1)]),
    ("StepsDealtAbsent", "NeverDealtAbsent", "deal",
     "The deal reads the store's up as presence (D1', 2026-11-07): a row\n"
     "hooked and up but gone from the bus is dealt a card it never takes.",
     [("  /\\ bus[r]   \\* on the bus this tick: a configured row is not a present one (D1')\n",
       "  /\\ TRUE   \\* FAULT: was bus[r]\n", 1)]),
    ("StepsOfferedAbsent", "OfferedIsAccepted", "deal",
     "Layer 5 builds a rows question's candidates itself, from the store's\n"
     "up with no bus: a row up in the store and off the bus is offered,\n"
     "and the deal refuses the answer for a row it never accepted.",
     [("PolicyOffered(V, x, r) == Offered(V, x, r)",
       "PolicyOffered(V, x, r) == V.up[r] /\\ Serves(r, x.kind, x.p) /\\ (x.kind = \"read\" => r # V.ps[x.p].author)   \\* FAULT", 1)]),
    ("StepsDealtUnadmitted", "NeverDealtUnadmitted", "deal",
     "The deal reads no admission (item 4, 2026-11-08): a producer whose\n"
     "PATHS left its stream's base is dealt, and fails on its row.",
     [("/\\ admitted[p])   \\* a producer that failed admission is never dealt",
       "/\\ TRUE)   \\* FAULT: was admitted[p]", 1)]),
    ("StepsAdmissionUnshown", "AdmissionShown", "deal",
     "A failed admission opens no judgement: the card waits undealt and\n"
     "only check run by hand says why (item 4, 2026-11-08).",
     [("                        \\cup {[kind |-> \"admission\", about |-> s] : s \\in {s \\in Streams : Unadmitted(V, s) # {}}}\n",
       "", 1)]),
    ("StepsExpiresSilently", "ExpiryNeverSilent", "deal",
     "A deadline closes a judgement whatever holds (item 3, 2026-11-08): the\n"
     "cause is still there and nothing says so until a later tick reopens it.",
     [("              Lapsed == dueJ \\ jnew\n",
       "              Lapsed == dueJ   \\* FAULT: was dueJ \\ jnew\n", 1)]),
    ("StepsLapsedKept", "LapsedReleased", "pre",
     "A card whose row is off the bus, past its deadline or that was never\n"
     "taken stays in the row's hand until the coordinator releases it.",
     [("       IN IF L # {} /\\ Free(p, cl)\n",
       "       IN IF FALSE   \\* FAULT: was L # {} /\\ Free(p, cl)\n", 1)]),
    ("StepsHandKept", "HandEmptyWhenClosed", "deal",
     "A row's Gone leaves its hand in place, as before 2026-11-10: a\n"
     "closed row holds cards nobody works.",
     [("THEN ReturnHand([w EXCEPT !.up[r] = FALSE, !.rg[r] = @ + 1], r)",
       "THEN [w EXCEPT !.up[r] = FALSE, !.rg[r] = @ + 1]   \\* FAULT: was ReturnHand", 1)]),
    ("StepsDealOneShort", "ReadyDealtWhenRoom", "pre",
     "The deal leaves two resource units of each row empty: a ready\n"
     "producer with room waits for capacity the deal never fills.",
     [("  /\\ HasRoom(V, r, ConsNeeds(x.kind, x.p), t)\n",
       "  /\\ HasRoom(V, r, [cpu |-> ConsNeeds(x.kind, x.p).cpu + 2, mem |-> ConsNeeds(x.kind, x.p).mem + 2], t)   \\* FAULT\n", 1)]),
    ("StepsDealsBehindPause", "NoDealBehindPause", "pre",
     "R52 AnyPaused guard omitted: the dealer deals work onto a bench\n"
     "that already has a paused holder queued behind CI.",
     [("  /\\ ~W.paused[r]\n",
       "  /\\ TRUE   \\* FAULT: ignored W.paused[r] (R52)\n", 1)]),
    ("StepsDealsOverClaim", "NoDealOverClaim", "pre",
     "R50 soft person claim headroom omitted: NonSprintUsed ignores\n"
     "W.claim[r] and deals new sprint work onto claimed bench room.",
     [("NonSprintUsed(W, r) == AddRes(W.other[r], W.claim[r])\n",
       "NonSprintUsed(W, r) == W.other[r]   \\* FAULT: ignored W.claim[r]\n", 1)]),
    ("StepsFutilePreempt", "NoFutilePreempt", "pre",
     "R53 HeldOther headroom guard omitted: preemption kills a sprint\n"
     "victim on a bench held by CI where the blocker still cannot fit.",
     [("  IF W.paused[r] \\/ ~Serves(r, \"work\", b) \\/ ~CanFitWithoutSprint(W, r, CardRes[b]) THEN {}\n"
       "  ELSE LET es == EligSeq(W, r, b)\n"
       "       IN IF Len(es) = 0 THEN {}\n"
       "          ELSE IF FitsAfterFreeing(W, r, CardRes[b], {es[1]}) THEN {es[1]}",
       "  IF W.paused[r] \\/ ~Serves(r, \"work\", b) THEN {}\n"
       "  ELSE LET es == EligSeq(W, r, b)\n"
       "       IN IF Len(es) = 0 THEN {}\n"
       "          ELSE IF TRUE THEN {es[1]}   \\* FAULT: ignored HeldOther headroom (R53)", 1)]),
    ("StepsMissingMultiStop", "PreemptMultiStops", "pre",
     "Multi-victim preemption group emits a stop request for only one\n"
     "victim instead of every preempted victim in g.vs.",
     [("   req   |-> IF kind = \"pre\" THEN {StopReq(c) : c \\in vs} ELSE {}]",
       "   req   |-> IF kind = \"pre\" /\\ vs # {} THEN {StopReq(CHOOSE c \\in vs : TRUE)} ELSE {}]   \\* FAULT", 1)]),
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
