-------------------------- MODULE LandEdgeWrongSHA --------------------------
(***************************************************************************)
(* Layer 4 landing edge model (docs/LAYER-4-EDGES.md, Guarantees 4 & 5,    *)
(* docs/LANDING.md). Models 2 streams, 3 heads in score order, K = 2 base  *)
(* moves, and CI outcomes green, red, and fault.                           *)
(***************************************************************************)
EXTENDS Integers, Sequences, FiniteSets

CONSTANTS K

Streams == {"s1", "s2"}
Heads == {"h1", "h2", "h3"}
Steps == {"idle", "fetched", "merged", "pushed", "gating", "green", "red", "fault", "landed", "refused"}

VARIABLES base, stream, group, step, gatedAt, landed

vars == <<base, stream, group, step, gatedAt, landed>>

BaseTip == base[Len(base)].sha

SeqSet(sq) == {sq[i] : i \in 1..Len(sq)}

LandedSet == SeqSet(landed)

CandidateSHA(s, tip) ==
  IF s = "s1" THEN 100 + tip ELSE 200 + tip

ExternalMoves ==
  Cardinality({i \in 2..Len(base) : base[i].stream = "ext"})

Init ==
  /\ base = << [sha |-> 1, parent |-> 0, stream |-> "init", heads |-> {}, gate |-> "green", gatedTip |-> 0, gatedSHA |-> 1] >>
  /\ stream = [s \in Streams |-> 1]
  /\ group = [s \in Streams |-> IF s = "s1" THEN << "h1", "h2" >> ELSE << "h3" >>]
  /\ step = [s \in Streams |-> "idle"]
  /\ gatedAt = [s \in Streams |-> [baseTip |-> 0, sha |-> 0, gate |-> "none", blamed |-> {}, chased |-> FALSE, faults |-> 0]]
  /\ landed = << >>

Fetch(s) ==
  /\ step[s] = "idle"
  /\ step' = [step EXCEPT ![s] = "fetched"]
  /\ gatedAt' = [gatedAt EXCEPT ![s].baseTip = BaseTip]
  /\ UNCHANGED <<base, stream, group, landed>>

Merge(s) ==
  /\ step[s] = "fetched"
  /\ LET sha == CandidateSHA(s, gatedAt[s].baseTip) IN
     /\ gatedAt' = [gatedAt EXCEPT ![s].sha = sha, ![s].gate = "none", ![s].blamed = {}]
     /\ step' = [step EXCEPT ![s] = "merged"]
  /\ UNCHANGED <<base, stream, group, landed>>

MergeConflict(s) ==
  /\ step[s] = "fetched"
  /\ step' = [step EXCEPT ![s] = "refused"]
  /\ UNCHANGED <<base, stream, group, gatedAt, landed>>

PushStream(s) ==
  /\ step[s] = "merged"
  /\ stream' = [stream EXCEPT ![s] = gatedAt[s].sha]
  /\ step' = [step EXCEPT ![s] = "pushed"]
  /\ UNCHANGED <<base, group, gatedAt, landed>>

Gate(s) ==
  /\ step[s] \in {"pushed", "fault"}
  /\ step' = [step EXCEPT ![s] = "gating"]
  /\ gatedAt' = [gatedAt EXCEPT ![s].gate = "none", ![s].blamed = {}]
  /\ UNCHANGED <<base, stream, group, landed>>

CIGreen(s) ==
  /\ step[s] = "gating"
  /\ gatedAt' = [gatedAt EXCEPT ![s].gate = "green", ![s].blamed = {}]
  /\ step' = [step EXCEPT ![s] = "green"]
  /\ UNCHANGED <<base, stream, group, landed>>

CIRed(s) ==
  /\ step[s] = "gating"
  /\ gatedAt' = [gatedAt EXCEPT ![s].gate = "red", ![s].blamed = {group[s][1]}]
  /\ step' = [step EXCEPT ![s] = "red"]
  /\ UNCHANGED <<base, stream, group, landed>>

CIFault(s) ==
  /\ step[s] = "gating"
  /\ gatedAt[s].faults < 1
  /\ gatedAt' = [gatedAt EXCEPT ![s].gate = "fault", ![s].blamed = {}, ![s].faults = @ + 1]   \* fault blames no head
  /\ step' = [step EXCEPT ![s] = "fault"]
  /\ UNCHANGED <<base, stream, group, landed>>

BaseMoves ==
  /\ ExternalMoves < K
  /\ \E s \in Streams : step[s] # "landed"
  /\ LET nextSHA == BaseTip + 10 IN
     base' = Append(base, [sha |-> nextSHA, parent |-> BaseTip, stream |-> "ext", heads |-> {}, gate |-> "green", gatedTip |-> BaseTip, gatedSHA |-> nextSHA])
  /\ UNCHANGED <<stream, group, step, gatedAt, landed>>

Land(s) ==
  /\ step[s] = "green"
  /\ gatedAt[s].gate = "green"
  /\ gatedAt[s].baseTip = BaseTip   \* up-to-date check: base has not moved since gate
  /\ \A h \in SeqSet(group[s]) : h \notin LandedSet
  /\ LET landedSHA == gatedAt[s].sha + 99 IN   \* FAULT: Land at another sha than gated
     /\ base' = Append(base, [sha |-> landedSHA, parent |-> BaseTip, stream |-> s, heads |-> SeqSet(group[s]), gate |-> gatedAt[s].gate, gatedTip |-> gatedAt[s].baseTip, gatedSHA |-> gatedAt[s].sha])
     /\ stream' = [stream EXCEPT ![s] = landedSHA]
     /\ landed' = landed \o group[s]   \* group lands whole
     /\ step' = [step EXCEPT ![s] = "landed"]
  /\ UNCHANGED <<group, gatedAt>>

Remerge(s) ==
  /\ step[s] \in {"fetched", "merged", "pushed", "gating", "green"}
  /\ gatedAt[s].baseTip # BaseTip
  /\ LET sha == CandidateSHA(s, BaseTip) IN
     /\ gatedAt' = [gatedAt EXCEPT ![s].baseTip = BaseTip, ![s].sha = sha, ![s].gate = "none", ![s].blamed = {}, ![s].chased = FALSE]   \* re-merge onto new base tip
     /\ stream' = [stream EXCEPT ![s] = sha]
     /\ step' = [step EXCEPT ![s] = "pushed"]
  /\ UNCHANGED <<base, group, landed>>

Refuse(s) ==
  /\ step[s] = "red"
  /\ step' = [step EXCEPT ![s] = "refused"]
  /\ UNCHANGED <<base, stream, group, gatedAt, landed>>

Done ==
  /\ \A s \in Streams : step[s] \in {"landed", "refused"}
  /\ UNCHANGED vars

Next ==
  \/ \E s \in Streams :
       \/ Fetch(s)
       \/ Merge(s)
       \/ MergeConflict(s)
       \/ PushStream(s)
       \/ Gate(s)
       \/ CIGreen(s)
       \/ CIRed(s)
       \/ CIFault(s)
       \/ Land(s)
       \/ Remerge(s)
       \/ Refuse(s)
  \/ BaseMoves
  \/ Done

FairLander(s) ==
  /\ WF_vars(Fetch(s))
  /\ WF_vars(Merge(s))
  /\ WF_vars(PushStream(s))
  /\ WF_vars(Gate(s))
  /\ WF_vars(CIGreen(s))
  /\ WF_vars(Land(s))
  /\ WF_vars(Remerge(s))
  /\ WF_vars(Refuse(s))

Spec == Init /\ [][Next]_vars /\ FairLander("s1") /\ FairLander("s2")

TypeOK ==
  /\ Len(base) >= 1
  /\ step \in [Streams -> Steps]
  /\ SeqSet(landed) \subseteq Heads

BaseGatedGreenFromParent ==
  \A i \in 2..Len(base) :
    /\ base[i].gate = "green"
    /\ base[i].parent = base[i - 1].sha
    /\ base[i].gatedTip = base[i].parent
    /\ base[i].gatedSHA = base[i].sha

HeadLandsAtMostOnce ==
  \A i, j \in 1..Len(landed) : i # j => landed[i] # landed[j]

GroupAllOrNone ==
  \A s \in Streams :
    \/ \A h \in SeqSet(group[s]) : h \in LandedSet
    \/ \A h \in SeqSet(group[s]) : h \notin LandedSet

FaultBlamesNoHead ==
  \A s \in Streams :
    (step[s] = "fault" \/ gatedAt[s].gate = "fault") => gatedAt[s].blamed = {}

StreamEqualsBaseAfterLand ==
  \A i \in 2..Len(base) :
    (i = Len(base) /\ base[i].stream \in Streams) => stream[base[i].stream] = BaseTip

NoChaseBack ==
  \A s \in Streams : ~gatedAt[s].chased /\ Len(group[s]) >= 1

Liveness ==
  \A s \in Streams : <>[](step[s] \in {"landed", "refused"})
=============================================================================
