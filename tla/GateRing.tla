------------------------------ MODULE GateRing ------------------------------
\* nova-sprint land, the gate bench's hash ring (cmd/nova-sprint/landgo.go
\* ringGate, gateOn, takeGateLane, gateBenches; landring.go benchRing, ringSlot;
\* pkg/bench/stage_mirror.go StageSkips; docs/SPEC-SPRINT.md section 7, the tree
\* gate). The up benches, in the fleet's order, are a ring; a batch's gate starts
\* at the slot its stream hashes to (FNV-1a 64-bit of the stream name mod the
\* ring's size) and steps to the next slot while a lane is held.
\*
\* BEFORE. Every gate asked the benches in the fleet's order, so every gate ran on
\* the first up member (the weakest). A bench whose stage was refused was asked
\* again on the next gate, so a broken bench held the ring and every gate it
\* refused still blamed a head or stalled the stream. The owner: "you should be
\* distributing according to our regular trick, where we do the uint64 and it is
\* modulo % n", "even when we have parallel land".
\*
\* THE WORLD. Two up benches, h1 and h2, in a ring of two; the batch's key hashes
\* to h1 (the start slot), so the ring asks h1 then h2. A land pass runs three
\* gates, two passes. Each stage's outcome on a host is the oracle outcome, chosen
\* at Init for every host and gate and then fixed: green (the tree passed), red
\* (the tree failed), refused (the stage was refused, the tree never reached the
\* bench), fault (the bench could not run the gate at all: its disk full, no go on
\* its PATH -- read as a refusal, gateOn's benchFault), or noanswer (the bench did
\* not answer; the gate returns at once). remote says another fleet member is
\* configured, even when it is down.
\*
\* THE STATE.
\*   pass, gate  the current gate of the current pass
\*   fails       the pass's skip record: fails[h] is how many stages of h failed
\*               this pass; at SkipAfter the host is passed over for the rest of
\*               the pass (StageSkips), and a new pass starts it fresh at 0
\*   tried       the hosts already asked by this gate (ringGate's tried)
\*   held        the benches' Go lanes this lander holds (always empty: gateOn's
\*               deferred give gives the lane back however the gate ended)
\*   ranWhere    where the current gate ran: "none" while it asks, then a host, or
\*               "here" (the clone) or "refused" (the remote bench unavailable)
\*   gaveUp      why the ring was left with no bench having run the gate: "none",
\*               "noanswer" (a bench did not answer), or "exhausted" (every host
\*               skipped or already tried)
\*
\* THE RULES.
\*   AskedAtMostSkipAfter: a host is asked at most SkipAfter times in a pass after
\*     refusing; the second refusal passes it over for the rest of the pass.
\*   NoLocalGoWhenRemote: the gate never runs in the clone while another member is
\*     configured; a gate no bench ran is refused as a remote-bench unavailability
\*     (benchGateUnavailableWhy) and blames no head.
\*   LanesGivenBack: every lane taken is given back, whatever the gate did.
\*   RefusedStepsToNextSlot: a refused stage steps the gate to the next slot (the
\*     test's rule); a refusal never ends the gate -- the gate falls back only on
\*     a no answer or because every host is skipped or already tried.
\*
\* Broken: "none" is the design.
\*   "noskip"          the ring ignores the pass's skip record (Skipped is always
\*                     FALSE): a host is asked past SkipAfter (AskedAtMostSkipAfter
\*                     fails).
\*   "localwhenremote" the fallback runs the gate in the clone even when remote is
\*                     set (the caller's `if remote` guard dropped, landgo.go:389):
\*                     a gate no bench ran is run here instead of refused
\*                     (NoLocalGoWhenRemote fails).
\*
\* WHAT IS NOT MODELLED. The gate key's hash and the slot it lands on (the ring is
\* fixed at the start slot; the hashed start slot is the code's gateSlot, and the
\* record names that, not the slot that ran -- see landgo.go gateSlot); the lane
\* grant itself (takeGateLane asks hosts in ring order and, when none grants, keeps
\* its place on hosts[0] and waits: the wait is the land loop's clock, so it adds
\* no step here); the push of the temporary gate ref and the mirror the bench
\* stages from; the runs themselves and the red finding they carry; and which gates
\* run the tree tests. A bench fault is the refusal it reads as, as in the code.
EXTENDS Integers, FiniteSets, Sequences

CONSTANTS Hosts, Ring, SkipAfter, Gates, Passes, Remote, Broken

ASSUME Hosts # {}
ASSUME Len(Ring) = Cardinality(Hosts)
ASSUME {Ring[i] : i \in 1..Len(Ring)} = Hosts
ASSUME SkipAfter \in Nat
ASSUME Gates \in Nat \ {0}
ASSUME Passes \in Nat \ {0}
ASSUME Remote \in BOOLEAN

Outcomes == {"green", "red", "refused", "fault", "noanswer"}

VARIABLES pass, gate, fails, tried, held, ranWhere, gaveUp, outcome

vars == <<pass, gate, fails, tried, held, ranWhere, gaveUp, outcome>>

\* A host passed over this pass: its stage failed at least SkipAfter times. The
\* "noskip" witness drops this guard.
Skipped(h) == IF Broken = "noskip" THEN FALSE ELSE fails[h] >= SkipAfter

\* The hosts the ring still offers this gate, in ring order: the skipped and the
\* already-tried are dropped (ringGate: skips.Ring, then the untried).
live == SelectSeq(Ring, LAMBDA h : ~Skipped(h) /\ h \notin tried)

TypeOK ==
  /\ pass \in 1..Passes
  /\ gate \in 1..Gates
  /\ fails \in [Hosts -> Nat]
  /\ tried \in SUBSET Hosts
  /\ held \in SUBSET Hosts
  /\ ranWhere \in {"none", "here", "refused"} \cup Hosts
  /\ gaveUp \in {"none", "noanswer", "exhausted"}
  /\ outcome \in [Hosts \X (1..Gates) -> Outcomes]

Init ==
  /\ pass = 1 /\ gate = 1
  /\ fails = [h \in Hosts |-> 0]
  /\ tried = {} /\ held = {}
  /\ ranWhere = "none" /\ gaveUp = "none"
  /\ outcome \in [Hosts \X (1..Gates) -> Outcomes]

\* The next gate and pass after the current gate is done, and what each starts
\* fresh: tried per gate, fails per pass (a new pass asks every bench again).
Advance ==
  IF gate < Gates
    THEN /\ gate' = gate + 1 /\ pass' = pass /\ fails' = fails
    ELSE IF pass < Passes
      THEN /\ gate' = 1 /\ pass' = pass + 1 /\ fails' = [h \in Hosts |-> 0]
      ELSE /\ gate' = gate /\ pass' = pass /\ fails' = fails

\* The last gate of the last pass: once it is done the machine is done.
LastGate == gate = Gates /\ pass = Passes

\* One attempt on the first live host of the ring. The lander holds the host's Go
\* lane and gives it back in the same step (gateOn's deferred give): held stays
\* empty.

\* green or red: the gate ran on the bench, whatever the tree.
AskRun ==
  /\ ranWhere = "none" /\ gaveUp = "none" /\ live # <<>>
  /\ outcome[Head(live), gate] \in {"green", "red"}
  /\ ranWhere' = Head(live)
  /\ UNCHANGED <<pass, gate, fails, tried, held, gaveUp, outcome>>

\* refused or fault: the fail is recorded (a fault is read as a refusal) and the
\* gate steps to the next slot, still asking.
AskRefused ==
  /\ ranWhere = "none" /\ gaveUp = "none" /\ live # <<>>
  /\ LET h == Head(live)
     IN  /\ outcome[h, gate] \in {"refused", "fault"}
         /\ tried' = tried \cup {h}
         /\ fails' = [fails EXCEPT ![h] = fails[h] + 1]
  /\ UNCHANGED <<pass, gate, ranWhere, gaveUp, held, outcome>>

\* no answer: the gate returns at once, no bench having run it.
AskNoAnswer ==
  /\ ranWhere = "none" /\ gaveUp = "none" /\ live # <<>>
  /\ outcome[Head(live), gate] = "noanswer"
  /\ tried' = tried \cup {Head(live)}
  /\ gaveUp' = "noanswer"
  /\ UNCHANGED <<pass, gate, fails, ranWhere, held, outcome>>

\* every host is skipped or already tried: the gate returns, no bench having run it.
Exhaust ==
  /\ ranWhere = "none" /\ gaveUp = "none" /\ live = <<>>
  /\ gaveUp' = "exhausted"
  /\ UNCHANGED <<pass, gate, fails, tried, ranWhere, held, outcome>>

\* No bench ran the gate: the caller refuses it as a remote-bench unavailability
\* when another member is configured, else runs it here; both blame no head.
\* The "localwhenremote" witness drops the remote guard and runs it here anyway.
Fallback ==
  /\ ranWhere = "none" /\ gaveUp # "none"
  /\ IF Remote /\ Broken # "localwhenremote"
       THEN ranWhere' = "refused"
       ELSE ranWhere' = "here"
  /\ UNCHANGED <<pass, gate, fails, tried, held, gaveUp, outcome>>

\* The gate is done: the next gate begins (after the last the machine is done).
NextGate ==
  /\ ranWhere # "none" /\ ~LastGate
  /\ ranWhere' = "none"
  /\ gaveUp' = "none"
  /\ tried' = {}
  /\ held' = {}
  /\ Advance
  /\ UNCHANGED outcome

Next == AskRun \/ AskRefused \/ AskNoAnswer \/ Exhaust \/ Fallback \/ NextGate

Spec == Init /\ [][Next]_vars

\* ---- the rules ----

\* A host is asked at most SkipAfter times in a pass after refusing: the SkipAfter-th
\* refusal passes it over for the rest of the pass, so fails never exceeds SkipAfter.
AskedAtMostSkipAfter == \A h \in Hosts : fails[h] <= SkipAfter

\* The gate never runs in the clone while another member is configured: a gate no
\* bench ran is refused (benchGateUnavailableWhy), not run here.
NoLocalGoWhenRemote == Remote => ranWhere # "here"

\* Every lane taken is given back, however the gate ended (gateOn's deferred give).
LanesGivenBack == held = {}

\* A refused stage steps the gate to the next slot (the test's rule): a refusal never
\* ends the gate -- the gate falls back only on a no answer, or because every host is
\* skipped or already tried, so there is no next slot to step to.
RefusedStepsToNextSlot ==
  gaveUp = "exhausted" => \A h \in Hosts : Skipped(h) \/ h \in tried

=============================================================================
