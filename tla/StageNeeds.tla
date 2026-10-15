------------------------------ MODULE StageNeeds ------------------------------
\* The ordering behind sprint 21's same-second deal:
\* fixed3d-perf-harness lands, the five dependent spikes are dealt, but the
\* mirror still offers a tip read before that landing. Staging must compare the
\* packet's recorded landed heads with the actual tree it would hand the child.
\*
\* Heads abstract Git ancestry: a tree is the set of need heads it contains.
\* MoveTip observes a newer subset of landed heads, independently of LandNeed.
\* Deal captures the cached tip; a later MoveTip cannot repair an already cut
\* tree. Refuse returns the same attempt without spending it; the next Deal
\* can cut a fresh tree. The witness disables only the ancestry guard.
\*
\* recorded is fixed per run and enumerates the subset of needs with a stored
\* landed head. Needs without a recorded head are intentionally not checked.
\* Git failures, epoch/generation fences, rework merge conflict policy, network
\* fetches, and the live store's atomicity are outside this ordering model.
\* No liveness claim: fair tip refresh and retries would be extra assumptions.
\* spent counts child launches of the current attempt, not the attempt number
\* allocated at deal. A refusal starts no child and allows the same work to retry.
\* PacketCarriesLandedHeads independently checks A's wire obligation; the
\* ancestry invariant compares the tree with recorded heads, not merely the
\* packet, so accidentally dropping a packet head cannot make the claim vacuous.
EXTENDS Naturals, FiniteSets

CONSTANTS Children, Needs, Broken
ASSUME Children # {} /\ Needs # {} /\ Broken \in BOOLEAN

VARIABLES landed, recorded, tip, state, packetHeads, tree, spent
vars == <<landed, recorded, tip, state, packetHeads, tree, spent>>

Init ==
  /\ landed = {}
  /\ recorded \in SUBSET Needs
  /\ tip = {}
  /\ state = [c \in Children |-> "ready"]
  /\ packetHeads = [c \in Children |-> {}]
  /\ tree = [c \in Children |-> {}]
  /\ spent = [c \in Children |-> 0]

LandNeed(n) ==
  /\ n \in Needs \ landed
  /\ landed' = landed \cup {n}
  /\ UNCHANGED <<recorded, tip, state, packetHeads, tree, spent>>

MoveTip ==
  /\ tip # landed
  /\ \E newer \in SUBSET landed:
       /\ tip \subseteq newer /\ tip # newer
       /\ tip' = newer
  /\ UNCHANGED <<landed, recorded, state, packetHeads, tree, spent>>

Deal(c) ==
  /\ state[c] = "ready"
  /\ landed = Needs
  /\ state' = [state EXCEPT ![c] = "dealt"]
  /\ packetHeads' = [packetHeads EXCEPT ![c] = recorded]
  /\ tree' = [tree EXCEPT ![c] = tip]
  /\ UNCHANGED <<landed, recorded, tip, spent>>

ContainsNeeds(c) == packetHeads[c] \subseteq tree[c]

Stage(c) ==
  /\ state[c] = "dealt"
  /\ Broken \/ ContainsNeeds(c)
  /\ state' = [state EXCEPT ![c] = "running"]
  /\ spent' = [spent EXCEPT ![c] = 1]
  /\ UNCHANGED <<landed, recorded, tip, packetHeads, tree>>

Refuse(c) ==
  /\ state[c] = "dealt"
  /\ ~Broken /\ ~ContainsNeeds(c)
  /\ state' = [state EXCEPT ![c] = "ready"]
  /\ UNCHANGED <<landed, recorded, tip, packetHeads, tree, spent>>

Next ==
  \/ \E n \in Needs: LandNeed(n)
  \/ MoveTip
  \/ \E c \in Children: Deal(c) \/ Stage(c) \/ Refuse(c)
  \/ /\ \A c \in Children: state[c] = "running"
     /\ UNCHANGED vars

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ landed \subseteq Needs
  /\ recorded \subseteq Needs
  /\ tip \subseteq landed
  /\ state \in [Children -> {"ready", "dealt", "running"}]
  /\ packetHeads \in [Children -> SUBSET Needs]
  /\ tree \in [Children -> SUBSET Needs]
  /\ spent \in [Children -> 0..1]

NoChildWithoutLandedNeeds ==
  \A c \in Children: state[c] = "running" => recorded \subseteq tree[c]

PacketCarriesLandedHeads ==
  \A c \in Children: state[c] \in {"dealt", "running"} => packetHeads[c] = recorded

RefusalSpendsNoAttempt ==
  \A c \in Children: state[c] # "running" => spent[c] = 0

PacketNamesOnlyLandedHeads ==
  \A c \in Children: packetHeads[c] \subseteq landed
=============================================================================
