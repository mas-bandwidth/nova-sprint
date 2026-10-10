-------------------------------- MODULE NoRoom --------------------------------
\* A member's no-room word (the disk floor, the usage source's rest) and what the
\* deal does with it (v1.2.6; nova-tools#5569, #5578 and the drain follow-up).
\*
\* The code: each tick a member asks Room (pkg/member/member.go, Member.room) and
\* keeps the answer as its word (noRoomWhy); every beat carries it (noRoomArgs:
\* fleet beat --no-room <why>, a reader's queue the same), and the store keeps the
\* last beat's word (Store.BeatOwing, BeatReaders). The tick's deal and read ask
\* plan with the words of fresh beats (Snapshot.NoRoom, filled by
\* readerStatesInto), and so do the coordinator's fleet verbs outside the tick
\* (fleet up, down, hold, level: Step.ReadNoRoom, memberNoRoomInto). A member that
\* drains (its binary replaced) takes no new card, but it still ticks and beats
\* until it stops; the store does not know it drains.
\*
\* The state, per member: disk, the machine's real state (ok or low: under its
\* floor), an outside event; word, the member's word from its last Room (TRUE: no
\* room); ticked, a tick has run since disk last changed; draining; stored, the
\* word of the member's last beat, as the store holds it. Per card: at, the
\* member it is dealt to, or "none". last records the last deal: its kind (tick,
\* fleet) and whether the store's word for that member said no room then.
\*
\* The design, Broken = {}:
\*   Disk       the machine's disk falls under its floor or is freed.
\*   Tick       the member asks Room every tick, draining or not: its word is
\*              the disk as it is now.
\*   Drain      the member begins draining.
\*   Beat       the member's beat writes its word to the store.
\*   TickDeal   the tick deals a card to a member whose stored word is room.
\*   FleetDeal  a fleet verb (a level, a down's redeal) moves a card to a member
\*              whose stored word is room.
\*   Finish     a dealt card ends and leaves its member.
\* Invariants: WordIsThisTicks (a member that has ticked since the disk last
\* changed says the disk as it is), NeverDealtUnderFloor (no deal, by the tick
\* or by a fleet verb, goes to a member whose stored word says no room).
\* Reversed witnesses:
\*   "drainskip"  Room asked only past the drain return (the code before the
\*                v1.2.6 fix): a draining member keeps its last word:
\*                WordIsThisTicks
\*   "fleetblind" the fleet verbs plan with no NoRoom (before #5578):
\*                NeverDealtUnderFloor
\*   "tickblind"  the tick's deal reads no word (before #5569):
\*                NeverDealtUnderFloor
EXTENDS Naturals, FiniteSets

CONSTANTS Members, Cards, Broken

ASSUME Broken \subseteq {"drainskip", "fleetblind", "tickblind"}

VARIABLES disk, word, ticked, draining, stored, at, last

vars == <<disk, word, ticked, draining, stored, at, last>>

TypeOK ==
  /\ disk \in [Members -> {"ok", "low"}]
  /\ word \in [Members -> BOOLEAN]
  /\ ticked \in [Members -> BOOLEAN]
  /\ draining \in [Members -> BOOLEAN]
  /\ stored \in [Members -> BOOLEAN]
  /\ at \in [Cards -> Members \cup {"none"}]
  /\ last \in [kind : {"none", "tick", "fleet"}, under : BOOLEAN]

Init ==
  /\ disk = [m \in Members |-> "ok"]
  /\ word = [m \in Members |-> FALSE]
  /\ ticked = [m \in Members |-> TRUE]
  /\ draining = [m \in Members |-> FALSE]
  /\ stored = [m \in Members |-> FALSE]
  /\ at = [c \in Cards |-> "none"]
  /\ last = [kind |-> "none", under |-> FALSE]

Disk(m) ==
  /\ disk' = [disk EXCEPT ![m] = IF @ = "ok" THEN "low" ELSE "ok"]
  /\ ticked' = [ticked EXCEPT ![m] = FALSE]
  /\ UNCHANGED <<word, draining, stored, at, last>>

\* member.go Tick: room(now) before the drain return (drainskip: after it)
Tick(m) ==
  /\ word' = IF draining[m] /\ "drainskip" \in Broken
               THEN word
               ELSE [word EXCEPT ![m] = (disk[m] = "low")]
  /\ ticked' = [ticked EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, draining, stored, at, last>>

Drain(m) ==
  /\ ~draining[m]
  /\ draining' = [draining EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, stored, at, last>>

Beat(m) ==
  /\ stored' = [stored EXCEPT ![m] = word[m]]
  /\ UNCHANGED <<disk, word, ticked, draining, at, last>>

\* the tick deals a ready card (the store does not know a member drains: that is the
\* member's own, until its beat stops)
TickDeal(c, m) ==
  /\ at[c] = "none"
  /\ "tickblind" \in Broken \/ ~stored[m]
  /\ at' = [at EXCEPT ![c] = m]
  /\ last' = [kind |-> "tick", under |-> stored[m]]
  /\ UNCHANGED <<disk, word, ticked, draining, stored>>

\* a fleet verb moves a dealt card from one member to another (a level, a down's redeal)
FleetDeal(c, m) ==
  /\ at[c] \notin {"none", m}
  /\ "fleetblind" \in Broken \/ ~stored[m]
  /\ at' = [at EXCEPT ![c] = m]
  /\ last' = [kind |-> "fleet", under |-> stored[m]]
  /\ UNCHANGED <<disk, word, ticked, draining, stored>>

Finish(c) ==
  /\ at[c] # "none"
  /\ at' = [at EXCEPT ![c] = "none"]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, last>>

Next ==
  \/ \E m \in Members : Disk(m) \/ Tick(m) \/ Drain(m) \/ Beat(m)
  \/ \E c \in Cards, m \in Members : TickDeal(c, m) \/ FleetDeal(c, m)
  \/ \E c \in Cards : Finish(c)

Spec == Init /\ [][Next]_vars

WordIsThisTicks == \A m \in Members : ticked[m] => (word[m] = (disk[m] = "low"))

NeverDealtUnderFloor == last.kind # "none" => ~last.under

=============================================================================
