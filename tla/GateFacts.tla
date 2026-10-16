----------------------------- MODULE GateFacts -----------------------------
\* A member's gate facts, from its own measure of its machine to the deal
\* (docs/SPEC-SPRINT.md, the gate-host rule). The member measures its facts and
\* carries them on every beat (fleet beat --facts; nova-tools pkg/member Beat,
\* Config.Facts); the tick's presence writes them onto the member's control card
\* whenever they differ from the card's (internal/sprint/deal.go factsPlan, run by
\* presence in presence.go); the deal gives a GATE-NEEDS card only to a member
\* whose card's facts carry every need (GateHostPool). A control card made again
\* (fleet sync, a rejoin) starts with no facts.
\*
\* Broken = "none" is the code. Broken = "nowriter" is v1.4.2: nothing writes the card's facts, so a card some
\* member could take is never dealt (every member read "facts: "). Broken =
\* "onchange" writes the card only when the beat's facts change from the beat
\* before, so a card made again keeps no facts. Both fail CardDealt.

EXTENDS FiniteSets

CONSTANTS Members, Facts, Need, Broken

\* each member's machine, as it truly is: lin carries the need, mac does not
TrueFacts == [m \in Members |-> IF m = "lin" THEN Facts ELSE Facts \ Need]

VARIABLES beat, ctl, card, rejoined
vars == <<beat, ctl, card, rejoined>>

Init ==
  /\ beat = [m \in Members |-> {}]
  /\ ctl = [m \in Members |-> {}]
  /\ card = "ready"
  /\ rejoined = FALSE

\* a member's beat carries its facts (fleet beat --facts)
Beat(m) ==
  /\ beat[m] # TrueFacts[m]
  /\ beat' = [beat EXCEPT ![m] = TrueFacts[m]]
  /\ IF Broken = "onchange"
       THEN ctl' = [ctl EXCEPT ![m] = TrueFacts[m]]
       ELSE UNCHANGED ctl
  /\ UNCHANGED <<card, rejoined>>

\* the tick's presence writes each beat's facts that differ from the card's
Presence ==
  /\ Broken \notin {"nowriter", "onchange"}
  /\ \E m \in Members : beat[m] # {} /\ beat[m] # ctl[m]
  /\ ctl' = [m \in Members |-> IF beat[m] # {} THEN beat[m] ELSE ctl[m]]
  /\ UNCHANGED <<beat, card, rejoined>>

\* fleet sync makes a member's control card again, with no facts (once)
Rejoin(m) ==
  /\ ~rejoined
  /\ ctl' = [ctl EXCEPT ![m] = {}]
  /\ rejoined' = TRUE
  /\ UNCHANGED <<beat, card>>

\* the deal: a ready card goes only to a member whose card carries every need
Deal(m) ==
  /\ card = "ready"
  /\ Need \subseteq ctl[m]
  /\ card' = m
  /\ UNCHANGED <<beat, ctl, rejoined>>

Next ==
  \/ \E m \in Members : Beat(m) \/ Rejoin(m) \/ Deal(m)
  \/ Presence

Spec == Init /\ [][Next]_vars /\ WF_vars(Presence) /\ \A m \in Members : WF_vars(Beat(m)) /\ WF_vars(Deal(m))

TypeOK ==
  /\ beat \in [Members -> SUBSET Facts]
  /\ ctl \in [Members -> SUBSET Facts]
  /\ card \in Members \cup {"ready"}
  /\ rejoined \in BOOLEAN

\* a card's facts are the member's own measure or none: never another's
CardFactsAreTheMembers == \A m \in Members : ctl[m] \in {{}, TrueFacts[m]}

\* the gate-host rule: a card is dealt only to a member whose machine carries the need
DealtOnlyWhereTheNeedIs == card # "ready" => Need \subseteq TrueFacts[card]

\* a card a member can take is dealt: never a judgment naming empty facts forever
CardDealt == <>(card # "ready")

=============================================================================
