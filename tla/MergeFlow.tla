------------------------------ MODULE MergeFlow ------------------------------
\* Cards land as they arrive (docs/LANDING.md "Cards land as they arrive";
\* Glenn 2026-11-15). Merging cards, the stream's landing groups, and the
\* repository's CI queue: runners take runs first in, first out.
\*
\* modules/steps/land.go:
\*   - landRequests: a group is at most GroupMax cards, first in landing
\*     order.
\*   - peel: a group's own red asks its first card alone.
\*   - redFix: a card alone, red on its own, goes to fix.
\* modules/edge/git/land.go:
\*   - quietRuns_internal: a pushed gate cancels the runs of every merging
\*     card and of a replaced gate.
\*   - Fresh: the gate is always a new run on the group branch.
\*
\* Arrivals have no fairness: cards arrive, or do not, as they will,
\* including cards outside the model ("o", at most OtherMax runs of them
\* at once) whose landing is not modelled.
\*
\* MergingDrains: every merging card leaves merging (it lands, or goes to
\* fix), with no human step.
\* GateFirst: the gate's run never waits behind a run of a card that was
\* merging when it was pushed.
\*
\* Reversed witnesses, each a cfg on this module:
\*   - MergeFlowUncapped: every arrival joins and rebuilds the gate (the
\*     100-card group of 2026-11-15). It starves, so MergingDrains is
\*     violated.
\*   - MergeFlowNoPeel: a group's own red is a judgement that holds every
\*     card in it, so MergingDrains is violated.
\*   - MergeFlowNoCancel: card runs are never cancelled, so GateFirst is
\*     violated.
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS GroupMax, Runners, OtherMax,
          Capped,  \* FALSE: the uncapped group every arrival joins
          Peel,    \* FALSE: a group's own red holds the group for a judgement
          Cancel   \* FALSE: no run is cancelled when a gate is pushed

Cards == {"c1", "c2", "c3"}
Red == {"c2"}      \* its head is red on its own checks

VARIABLES st,      \* card -> idle | merging | fix
          line,    \* the merging cards in landing order
          queue,   \* the CI queue: card ids, "g" the gate, "o" an outside card
          running, \* the card and gate runs on runners
          orun,    \* how many outside cards' runs are on runners
          group,   \* the cards the gate gates ({} none)
          gate,    \* none | queued | running | green | red
          quietAt, \* the merging cards' runs when the gate was pushed
          jd       \* a judgement holds the stream (only without Peel)

vars == <<st, line, queue, running, orun, group, gate, quietAt, jd>>

Range(s) == {s[i] : i \in DOMAIN s}
Min(a, b) == IF a < b THEN a ELSE b
Drop(q, S) == SelectSeq(q, LAMBDA x : x \notin S)
Take(n) == {line[i] : i \in 1..Min(n, Len(line))}
FirstIn(S) == line[CHOOSE i \in 1..Len(line) : line[i] \in S /\ \A j \in 1..(i - 1) : line[j] \notin S]
Free == Runners - Cardinality(running) - orun
Others == Len(SelectSeq(queue, LAMBDA x : x = "o")) + orun
InFlight == gate \in {"queued", "running"}

\* Push the gate of g onto q, merging the cards m: a new run at the tail;
\* with Cancel every merging card's run and the outside cards' (merging
\* too) are cancelled; a replaced gate's run always goes.
PushOn(q, g, m) ==
  LET drop == IF Cancel THEN m \cup {"o", "g"} ELSE {"g"}
  IN /\ group' = g
     /\ gate' = "queued"
     /\ queue' = Append(Drop(q, drop), "g")
     /\ running' = running \ drop
     /\ orun' = IF Cancel THEN 0 ELSE orun
     /\ quietAt' = m \cup {"o"}

Init ==
  /\ st = [c \in Cards |-> "idle"]
  /\ line = <<>>
  /\ queue = <<>>
  /\ running = {}
  /\ orun = 0
  /\ group = {}
  /\ gate = "none"
  /\ quietAt = {}
  /\ jd = FALSE

\* A card goes merging (finished and read ok, or back from fix): its PR's
\* run is queued unless one still is.
Arrive(c) ==
  /\ st[c] # "merging"
  /\ st' = [st EXCEPT ![c] = "merging"]
  /\ line' = Append(line, c)
  /\ LET q1 == IF c \in Range(queue) \cup running THEN queue ELSE Append(queue, c)
     IN IF ~Capped /\ group # {} /\ InFlight
        THEN PushOn(q1, group \cup {c}, Range(line) \cup {c})
        ELSE /\ queue' = q1
             /\ UNCHANGED <<running, orun, group, gate, quietAt>>
  /\ UNCHANGED jd

\* A card outside the model goes merging: its run is queued; uncapped, it
\* joins the gate, which is rebuilt.
ArriveOther ==
  /\ Others < OtherMax
  /\ IF ~Capped /\ group # {} /\ InFlight
     THEN PushOn(Append(queue, "o"), group, Range(line))
     ELSE /\ queue' = Append(queue, "o")
          /\ UNCHANGED <<running, orun, group, gate, quietAt>>
  /\ UNCHANGED <<st, line, jd>>

\* The tick asks a landing of the first cards in landing order.
Form ==
  /\ group = {} /\ gate = "none" /\ ~jd /\ line # <<>>
  /\ PushOn(queue, IF Capped THEN Take(GroupMax) ELSE Range(line), Range(line))
  /\ UNCHANGED <<st, line, jd>>

\* A free runner takes the run at the head of the queue.
Start ==
  /\ Free > 0 /\ queue # <<>>
  /\ queue' = Tail(queue)
  /\ IF Head(queue) = "o"
     THEN orun' = orun + 1 /\ UNCHANGED running
     ELSE running' = running \cup {Head(queue)} /\ UNCHANGED orun
  /\ gate' = IF Head(queue) = "g" THEN "running" ELSE gate
  /\ UNCHANGED <<st, line, group, quietAt, jd>>

FinishCard(c) ==
  /\ c \in running
  /\ running' = running \ {c}
  /\ UNCHANGED <<st, line, queue, orun, group, gate, quietAt, jd>>

FinishOther ==
  /\ orun > 0
  /\ orun' = orun - 1
  /\ UNCHANGED <<st, line, queue, running, group, gate, quietAt, jd>>

FinishGate ==
  /\ "g" \in running
  /\ running' = running \ {"g"}
  /\ gate' = IF group \cap Red = {} THEN "green" ELSE "red"
  /\ UNCHANGED <<st, line, queue, orun, group, quietAt, jd>>

Leave(S, to) ==
  /\ st' = [c \in Cards |-> IF c \in S THEN to ELSE st[c]]
  /\ line' = SelectSeq(line, LAMBDA x : x \notin S)
  /\ group' = {}
  /\ gate' = "none"
  /\ UNCHANGED <<queue, running, orun, quietAt, jd>>

\* The tick reads the gate's verdict.
Verdict ==
  \/ gate = "green" /\ Leave(group, "idle")
  \/ gate = "red" /\ Cardinality(group) = 1 /\ Leave(group, "fix")
  \/ /\ gate = "red" /\ Cardinality(group) > 1 /\ Peel
     /\ PushOn(queue, {FirstIn(group)}, Range(line))
     /\ UNCHANGED <<st, line, jd>>
  \/ /\ gate = "red" /\ Cardinality(group) > 1 /\ ~Peel /\ ~jd
     /\ jd' = TRUE
     /\ UNCHANGED <<st, line, queue, running, orun, group, gate, quietAt>>

Next ==
  \/ Form \/ Start \/ FinishGate \/ FinishOther \/ Verdict \/ ArriveOther
  \/ \E c \in Cards : Arrive(c) \/ FinishCard(c)

Spec == Init /\ [][Next]_vars

\* The tick, the runners and CI are fair; arrivals are not.
LiveSpec == Spec /\ WF_vars(Form) /\ WF_vars(Start) /\ WF_vars(FinishGate)
               /\ WF_vars(Verdict) /\ WF_vars(FinishOther)
               /\ \A c \in Cards : WF_vars(FinishCard(c))

TypeOK ==
  /\ st \in [Cards -> {"idle", "merging", "fix"}]
  /\ Range(line) = {c \in Cards : st[c] = "merging"}
  /\ Len(line) = Cardinality(Range(line))
  /\ running \subseteq Cards \cup {"g"}
  /\ orun \in 0..Runners
  /\ Free >= 0
  /\ group \subseteq Range(line)
  /\ gate \in {"none", "queued", "running", "green", "red"}
  /\ (gate = "none") = (group = {})
  /\ (gate = "queued") = ("g" \in Range(queue))
  /\ (gate = "running") = ("g" \in running)

GroupNeverPastMax == Cardinality(group) <= GroupMax

Pos(x) == CHOOSE i \in 1..Len(queue) : queue[i] = x

GateFirst == gate = "queued" => \A i \in 1..(Pos("g") - 1) : queue[i] \notin quietAt

MergingDrains == \A c \in Cards : st[c] = "merging" ~> st[c] # "merging"
=============================================================================
