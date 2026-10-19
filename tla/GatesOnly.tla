----------------------------- MODULE GatesOnly -----------------------------
\* A gates-only member (Glenn 2026-10-19: native Windows members, captain and titan,
\* that take ONLY the cards whose GATE-NEEDS names windows). nova-config's machine row
\* carries gates_only; nova-sprint fleet sync writes it onto the member's control card
\* (internal/sprint/fleet_sync.go FleetDrift, DriftGates; fleetSyncPlan); every place
\* that picks a member for a card narrows the pool by it first
\* (internal/sprint/gates_only.go AdmitGatesOnly, gatesOnlyAdmits; deal.go GateHostPool;
\* route.go dealPool; steps_review.go's next attempt; steps_work.go's redeal of a held
\* member's cards) and a read never goes to one (read_cards.go readUnitsOf). Then the
\* gate-host rule holds the card's needs against the member's facts as before.
\*
\* Members: "win" is gates-only on {"windows"} and carries the windows fact; "lin" is a
\* plain bench. Cards: "gen" names no GATE-NEEDS, "wcard" names windows, "both" names
\* windows and linux together (no member satisfies it: it is never dealt).
\*
\* Broken = "none" is the code. Broken = "nofilter" is the deal before this change: a
\* gates-only member is dealt like any other, so the generic card reaches "win"; it fails
\* GenericNeverToGatesOnly. Broken = "readsto" deals the cards right but reads go to every
\* member up; it fails ReadsNeverToGatesOnly. Broken = "nogates" drops the gates on a
\* rejoin (the control card made again without gates_only); it fails GenericNeverToGatesOnly.
\*
\* Move (v1.6.8): every place that moves an attempt already on a member to another (the
\* next attempt's deal, steps_review.go, and the conflict redo, redo.go; the redeal of a member gone down or held and the
\* level, steps_work.go; the rebalance, rebalance.go) holds the one rule the deal holds,
\* gates and facts together (gates_only.go gateEligible, AdmitGate). Broken = "movegates" is
\* the code before v1.6.8 for the next attempt and the redeal: the gates alone, so the
\* windows card leaves "win" for the plain bench "lin" that lacks the fact; it fails
\* DealtOnlyWhereTheNeedIs (in the fleet: a GATE-NEEDS darwin, arm64 card moved off the one
\* Mac to a Linux bench, every attempt there HOLD, the bound marking a sound brief
\* defective: a card dealt to a member that can never satisfy it, a livelock). Broken =
\* "movefree" is the level and the rebalance before v1.6.8: no check at all; it fails
\* DealtOnlyWhereTheNeedIs too. "lin2" is a second plain bench so a sound move exists
\* (NeverMoved is violated in the pass instance: Move is not vacuous).

EXTENDS FiniteSets

CONSTANTS Members, Cards, Broken

GatesOf == [m \in Members |-> IF m = "win" THEN {"windows"} ELSE {}]
Facts == [m \in Members |-> IF m = "win" THEN {"windows"} ELSE {"linux"}]
Needs == [c \in Cards |-> CASE c = "gen" -> {}
                            [] c = "wcard" -> {"windows"}
                            [] OTHER -> {"windows", "linux"}]

VARIABLES ctlGates, placed, read, rejoined, moved
vars == <<ctlGates, placed, read, rejoined, moved>>

\* a member's control card is made by the sync's add with its gates in the same write
\* (fleetSyncPlan, DriftAdd), and a card the sync held comes back with its release and its
\* gates in one unit (DriftRelease with DriftGates): no member is ever up without them
Init ==
  /\ ctlGates = GatesOf
  /\ placed = [c \in Cards |-> "ready"]
  /\ read = [c \in Cards |-> "none"]
  /\ rejoined = FALSE
  /\ moved = FALSE

\* fleet sync writes each member's gates from its machine row onto its control card
Sync ==
  /\ \E m \in Members : ctlGates[m] # GatesOf[m]
  /\ ctlGates' = GatesOf
  /\ UNCHANGED <<placed, read, rejoined, moved>>

\* a control card made again (a rejoin): the sync's add writes gates_only with the width
Rejoin(m) ==
  /\ ~rejoined
  /\ ctlGates' = [ctlGates EXCEPT ![m] = IF Broken = "nogates" THEN {} ELSE GatesOf[m]]
  /\ rejoined' = TRUE
  /\ UNCHANGED <<placed, read, moved>>

\* gatesOnlyAdmits: a member with no gates admits every card; one with gates admits a
\* card only when its GATE-NEEDS names one of them
Admits(m, c) == ctlGates[m] = {} \/ ctlGates[m] \cap Needs[c] # {}

\* the deal (GateHostPool): admitted by the gates, then every need among the facts
Deal(m, c) ==
  /\ placed[c] = "ready"
  /\ Broken = "nofilter" \/ Admits(m, c)
  /\ Needs[c] \subseteq Facts[m]
  /\ placed' = [placed EXCEPT ![c] = m]
  /\ UNCHANGED <<ctlGates, read, rejoined, moved>>

\* a read of a dealt card (readUnitsOf): never by a gates-only member, never its worker
Read(m, c) ==
  /\ placed[c] \in Members
  /\ read[c] = "none"
  /\ m # placed[c]
  /\ Broken = "readsto" \/ ctlGates[m] = {}
  /\ read' = [read EXCEPT ![c] = m]
  /\ UNCHANGED <<ctlGates, placed, rejoined, moved>>

\* gateEligible: the member's gates admit the card and its facts carry every need
Eligible(m, c) == Admits(m, c) /\ Needs[c] \subseteq Facts[m]

\* a move of an attempt already on a member (rework's next attempt, a redeal, the level,
\* the rebalance): to another member the one rule admits
Move(m, c) ==
  /\ placed[c] \in Members
  /\ m # placed[c]
  /\ CASE Broken = "movefree" -> TRUE
       [] Broken = "movegates" -> Admits(m, c)
       [] OTHER -> Eligible(m, c)
  /\ placed' = [placed EXCEPT ![c] = m]
  /\ moved' = TRUE
  /\ UNCHANGED <<ctlGates, read, rejoined>>

Next ==
  \/ Sync
  \/ \E m \in Members : Rejoin(m)
  \/ \E m \in Members, c \in Cards : Deal(m, c) \/ Read(m, c) \/ Move(m, c)

Spec == Init /\ [][Next]_vars /\ WF_vars(Sync)
          /\ \A m \in Members, c \in Cards : WF_vars(Deal(m, c))

TypeOK ==
  /\ ctlGates \in [Members -> SUBSET {"windows"}]
  /\ placed \in [Cards -> Members \cup {"ready"}]
  /\ read \in [Cards -> Members \cup {"none"}]
  /\ rejoined \in BOOLEAN
  /\ moved \in BOOLEAN

\* the rule: a card that names none of a gates-only member's gates never reaches it.
\* GatesOf is the machine row: the rule is held against the row, the truth the deal must
\* never break, not against the control card's copy the deal reads.
GenericNeverToGatesOnly ==
  \A c \in Cards : placed[c] \in Members /\ GatesOf[placed[c]] # {}
                     => GatesOf[placed[c]] \cap Needs[c] # {}

\* a gates-only member reads nothing
ReadsNeverToGatesOnly == \A c \in Cards : read[c] \in Members => GatesOf[read[c]] = {}

\* the gate-host rule still holds: a card goes only where its needs are
DealtOnlyWhereTheNeedIs == \A c \in Cards : placed[c] \in Members => Needs[c] \subseteq Facts[placed[c]]

\* the windows card and the generic card are both dealt: gates-only narrows, never starves
WindowsCardDealt == <>(placed["wcard"] = "win")
GenericCardDealt == <>(placed["gen"] \in {"lin", "lin2"})

\* the cover: a sound move is reachable (expected violated in MCGatesOnlyMoveCover.cfg)
NeverMoved == ~moved

=============================================================================
