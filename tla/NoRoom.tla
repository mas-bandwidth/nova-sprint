-------------------------------- MODULE NoRoom --------------------------------
\* A member's no-room word (the disk floor, the usage source's rest), its drain, its
\* admission, and what the deal and the read ask do with them (v1.2.6; nova-tools#5569,
\* #5578 and the drain follow-up; v1.2.10: the drain told to the server, and a held
\* machine's reader; v1.2.12: admission by a passed probe, and the read return's taker).
\*
\* The code: each tick a member asks Room (pkg/member/member.go, Member.room) and
\* keeps the answer as its word (noRoomWhy); every beat carries it (noRoomArgs:
\* fleet beat --no-room <why>, a reader's queue the same), and the store keeps the
\* last beat's word (Store.BeatOwing, BeatReaders). The tick's deal and read ask
\* plan with the words of fresh beats (Snapshot.NoRoom, filled by
\* readerStatesInto), and so do the coordinator's fleet verbs outside the tick
\* (fleet up, down, hold, level: Step.ReadNoRoom, memberNoRoomInto). A member that
\* drains (its binary replaced) takes no new card, but it still ticks and beats
\* until it stops. Before v1.2.10 the store did not know it drains, and the cards
\* dealt to it sat ready behind its longest running card (2026-10-11 ~03:50Z: b3
\* held 2 dealt cards with 15 slots idle). Now its beat says so (drainArgs: fleet beat
\* --draining, a reader's queue --draining), the store keeps it (Beat.Draining), and
\* its no-room word is a drain's (NoRoomNow, DrainWord): the deal and the fleet verbs
\* give it nothing (notQuiet), and the tick's level returns what it was dealt and has
\* not taken (drainReturns, internal/sprint/drain.go). A reader row belongs to its
\* machine (reader-<m>, Snapshot.ReaderOwner): while the machine is held or drains
\* the ask asks that reader nothing and its reads asked and not begun go back
\* (readerOff, readerRooms, sweepReads). Since v1.2.12 the sprint knows a member only
\* after its own probe passes: fleet up refuses a member the sprint does not know
\* until --probe passed (steps_work.go FleetUp), so a member is dealt to, and its
\* reader is held/drained off, only once it is known (known, Accept); the reader of a
\* machine refused at the probe gate belongs to no machine and is asked like any
\* reader. And sweepReads takes a read back only when another reader can take it
\* (readers.go taker), so the model's ReadReturn is guarded on a taker.
\*
\* The state, per member: disk, the machine's real state (ok or low: under its
\* floor), an outside event; word, the member's word from its last Room (TRUE: no
\* room); ticked, a tick has run since disk last changed; draining; stored, the
\* word of the member's last beat, as the store holds it; sdrain, the drain of the
\* member's last beat, as the store holds it; held, the coordinator's hold on the
\* machine; known, the sprint knows the member (its own probe passed). Per card: at,
\* the member it is dealt to, or "none"; taken, its member took it (a child runs it).
\* Per read: rat, the machine whose reader it is asked of (the reader of m stands
\* for every reader row of m), or "none"; begun, that reader began it. last records
\* the last deal: its kind (tick, fleet) and whether the store's word for that member
\* said no room then, and whether the store held it draining, and whether the sprint
\* knew it; lastAsk the last ask and whether its machine took no work then; lastRet
\* the last read return and whether a taker existed then.
\*
\* The design, Broken = {}:
\*   Disk        the machine's disk falls under its floor or is freed.
\*   Tick        the member asks Room every tick, draining or not: its word is
\*               the disk as it is now.
\*   Drain       a known member begins draining (its binary was replaced).
\*   Restart     a draining member that runs nothing stops; its supervisor starts
\*               the new binary, which does not drain.
\*   Beat        a known member's beat writes its word and its drain to the store.
\*   Hold        the coordinator holds a known machine, or releases it.
\*   Accept      a member the sprint does not know passes its own probe: the sprint
\*               knows it (fleet up --probe passed; the probe is an outside event).
\*   TickDeal    the tick deals a card to a member the sprint knows whose stored word
\*               is room and that the store does not hold draining, nor held.
\*   FleetDeal   a verb outside the tick's deal (a level, a down's or a hold's
\*               redeal, a rework's or a redo's deal, the rebalance) moves a card
\*               not taken to such a member: each plans with notQuiet on the
\*               words its step read (Step.ReadNoRoom, readerStatesInto).
\*   Take        a member that does not drain takes a card dealt to it.
\*   Finish      a card taken ends and leaves its member.
\*   DrainReturn the tick's level returns a card dealt and not taken from a member
\*               the store holds draining.
\*   Ask         the tick asks a read of the reader of a machine neither held nor
\*               held draining (an unknown machine's reader is asked like any reader:
\*               it belongs to no machine).
\*   Begin, End  the reader begins the read, and ends it.
\*   ReadReturn  the tick takes back a read asked and not begun of the reader of a
\*               machine held or held draining, only when another reader can take it.
\* Invariants: WordIsThisTicks (a member that has ticked since the disk last
\* changed says the disk as it is), NeverDealtUnderFloor (no deal, by the tick
\* or by a fleet verb, goes to a member whose stored word says no room),
\* NeverDealtToDraining (none goes to a member the store holds draining),
\* NeverDealtUnknown (none goes to a member the sprint does not know),
\* NeverAskedOfHeldOrDraining (no read is asked of a held or draining machine's reader).
\* Liveness: DealtReturns (a card dealt to a draining member and not taken leaves it,
\* or the drain ends), ReadsReturn (a read asked of a held or draining machine's reader
\* and not begun leaves it, or the machine takes work again, or no other reader can
\* take it; and a read is never taken back when no other reader can take it).
\* Reversed witnesses:
\*   "drainskip"   Room asked only past the drain return (the code before the
\*                 v1.2.6 fix): a draining member keeps its last word:
\*                 WordIsThisTicks
\*   "fleetblind"  the fleet verbs plan with no NoRoom (before #5578):
\*                 NeverDealtUnderFloor
\*   "tickblind"   the tick's deal reads no word (before #5569):
\*                 NeverDealtUnderFloor
\*   "silentdrain" the beat does not say the member drains (before v1.2.10): a card
\*                 dealt to it sits ready for the whole drain: DealtReturns
\*   "drainblind"  the deal reads no drain: NeverDealtToDraining
\*   "noreturn"    the level returns nothing: DealtReturns
\*   "readblind"   the ask reads neither the hold nor the drain of the reader's
\*                 machine (2026-10-11: mini-m5 held, reader-mini-m5 asked):
\*                 NeverAskedOfHeldOrDraining
\*   "noreadreturn" no read asked is taken back: ReadsReturn
\*   "probeblind"  the deal reads no admission, so it deals to a member the sprint
\*                 does not know (the probe gate and the drain landing apart):
\*                 NeverDealtUnknown
\*   "notaker"     the sweep takes a read back with no other reader to take it:
\*                 ReadsReturn
EXTENDS Naturals, FiniteSets

\* Disky: the disk falls and is freed (the no-room word's instance); Holds: the coordinator
\* holds and releases machines (the drain's and the reads' instance). The two instances
\* are checked apart, so each stays small. A probe (Accept) is an outside event on its own.
CONSTANTS Members, Cards, Reads, Disky, Holds, Broken

ASSUME Disky \in BOOLEAN /\ Holds \in BOOLEAN

ASSUME Broken \subseteq {"drainskip", "fleetblind", "tickblind", "silentdrain", "drainblind", "noreturn", "readblind", "noreadreturn", "probeblind", "notaker"}

VARIABLES disk, word, ticked, draining, stored, sdrain, held, known, at, taken, rat, begun, last, lastAsk, lastRet

vars == <<disk, word, ticked, draining, stored, sdrain, held, known, at, taken, rat, begun, last, lastAsk, lastRet>>

TypeOK ==
  /\ disk \in [Members -> {"ok", "low"}]
  /\ word \in [Members -> BOOLEAN]
  /\ ticked \in [Members -> BOOLEAN]
  /\ draining \in [Members -> BOOLEAN]
  /\ stored \in [Members -> BOOLEAN]
  /\ sdrain \in [Members -> BOOLEAN]
  /\ held \in [Members -> BOOLEAN]
  /\ known \in [Members -> BOOLEAN]
  /\ at \in [Cards -> Members \cup {"none"}]
  /\ taken \in [Cards -> BOOLEAN]
  /\ rat \in [Reads -> Members \cup {"none"}]
  /\ begun \in [Reads -> BOOLEAN]
  /\ last \in [kind : {"none", "tick", "fleet"}, under : BOOLEAN, drain : BOOLEAN, known : BOOLEAN]
  /\ lastAsk \in [kind : {"none", "ask"}, off : BOOLEAN]
  /\ lastRet \in [kind : {"none", "return"}, taker : BOOLEAN]

Init ==
  /\ disk = [m \in Members |-> "ok"]
  /\ word = [m \in Members |-> FALSE]
  /\ ticked = [m \in Members |-> TRUE]
  /\ draining = [m \in Members |-> FALSE]
  /\ stored = [m \in Members |-> FALSE]
  /\ sdrain = [m \in Members |-> FALSE]
  /\ held = [m \in Members |-> FALSE]
  /\ known = [m \in Members |-> FALSE]
  /\ at = [c \in Cards |-> "none"]
  /\ taken = [c \in Cards |-> FALSE]
  /\ rat = [r \in Reads |-> "none"]
  /\ begun = [r \in Reads |-> FALSE]
  /\ last = [kind |-> "none", under |-> FALSE, drain |-> FALSE, known |-> FALSE]
  /\ lastAsk = [kind |-> "none", off |-> FALSE]
  /\ lastRet = [kind |-> "none", taker |-> FALSE]

cards == <<at, taken>>
reads == <<rat, begun, lastAsk>>

Disk(m) ==
  /\ Disky
  /\ disk' = [disk EXCEPT ![m] = IF @ = "ok" THEN "low" ELSE "ok"]
  /\ ticked' = [ticked EXCEPT ![m] = FALSE]
  /\ UNCHANGED <<word, draining, stored, sdrain, held, known, cards, reads, last, lastRet>>

\* member.go Tick: room(now) before the drain return (drainskip: after it)
Tick(m) ==
  /\ word' = IF draining[m] /\ "drainskip" \in Broken
                THEN word
                ELSE [word EXCEPT ![m] = (disk[m] = "low")]
  /\ ticked' = [ticked EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, draining, stored, sdrain, held, known, cards, reads, last, lastRet>>

\* a member that drains is a known fleet member (its binary was replaced under it)
Drain(m) ==
  /\ known[m]
  /\ ~draining[m]
  /\ draining' = [draining EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, stored, sdrain, held, known, cards, reads, last, lastRet>>

\* nova-swarm memberLoop: the last child reported, it stops; the new binary does not drain
Restart(m) ==
  /\ draining[m]
  /\ \A c \in Cards : at[c] = m => ~taken[c]
  /\ draining' = [draining EXCEPT ![m] = FALSE]
  /\ UNCHANGED <<disk, word, ticked, stored, sdrain, held, known, cards, reads, last, lastRet>>

\* Store.BeatDraining: the word and the drain of this beat (silentdrain: no drain said);
\* the store keeps a known member's beat
Beat(m) ==
  /\ known[m]
  /\ stored' = [stored EXCEPT ![m] = word[m]]
  /\ sdrain' = [sdrain EXCEPT ![m] = draining[m] /\ "silentdrain" \notin Broken]
  /\ UNCHANGED <<disk, word, ticked, draining, held, known, cards, reads, last, lastRet>>

\* the coordinator holds a known machine, or releases it
Hold(m) ==
  /\ Holds
  /\ known[m]
  /\ held' = [held EXCEPT ![m] = ~@]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, known, cards, reads, last, lastRet>>

\* steps_work.go FleetUp: a member the sprint does not know runs its own probe (the
\* push-credential probe, the harness check); a passed probe admits it (known).
Accept(m) ==
  /\ ~known[m]
  /\ known' = [known EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, cards, reads, last, lastRet>>

\* the deal's up is notQuiet: no word of no room, no drain the store holds, a member the
\* sprint knows; a held member is down (probeblind: no admission)
Dealable(m, blind) ==
  /\ ~held[m]
  /\ "probeblind" \in Broken \/ known[m]
  /\ blind \/ ~stored[m]
  /\ "drainblind" \in Broken \/ ~sdrain[m]

TickDeal(c, m) ==
  /\ at[c] = "none"
  /\ Dealable(m, "tickblind" \in Broken)
  /\ at' = [at EXCEPT ![c] = m]
  /\ last' = [kind |-> "tick", under |-> stored[m], drain |-> sdrain[m], known |-> known[m]]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, taken, reads, lastRet>>

\* a fleet verb moves a dealt card not taken from one member to another (a level, a down's redeal)
FleetDeal(c, m) ==
  /\ at[c] \notin {"none", m}
  /\ ~taken[c]
  /\ Dealable(m, "fleetblind" \in Broken)
  /\ at' = [at EXCEPT ![c] = m]
  /\ last' = [kind |-> "fleet", under |-> stored[m], drain |-> sdrain[m], known |-> known[m]]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, taken, reads, lastRet>>

\* member.go Tick: a draining member starts nothing
Take(c) ==
  /\ at[c] # "none"
  /\ ~taken[c]
  /\ ~draining[at[c]]
  /\ taken' = [taken EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, at, reads, last, lastRet>>

Finish(c) ==
  /\ taken[c]
  /\ at' = [at EXCEPT ![c] = "none"]
  /\ taken' = [taken EXCEPT ![c] = FALSE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, reads, last, lastRet>>

\* drain.go drainReturns: the level withdraws a card dealt and not taken off a member held draining
DrainReturn(c) ==
  /\ "noreturn" \notin Broken
  /\ at[c] # "none"
  /\ ~taken[c]
  /\ sdrain[at[c]]
  /\ at' = [at EXCEPT ![c] = "none"]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, taken, reads, last, lastRet>>

\* readerOff: the reader of a known machine that takes no work; the reader of an unknown
\* machine belongs to no machine and is up (asked like any reader)
Off(m) == known[m] /\ (held[m] \/ sdrain[m])

\* another reader that can take a read off m's reader: any other member whose reader is not off
Taker(m) == \E m2 \in Members \ {m} : ~Off(m2)

Ask(r, m) ==
  /\ rat[r] = "none"
  /\ "readblind" \in Broken \/ ~Off(m)
  /\ rat' = [rat EXCEPT ![r] = m]
  /\ lastAsk' = [kind |-> "ask", off |-> Off(m)]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, cards, begun, last, lastRet>>

\* the reader loop knows nothing of its machine's hold: it may begin what it was asked
Begin(r) ==
  /\ rat[r] # "none"
  /\ ~begun[r]
  /\ ~draining[rat[r]]
  /\ begun' = [begun EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, cards, rat, lastAsk, last, lastRet>>

End(r) ==
  /\ begun[r]
  /\ rat' = [rat EXCEPT ![r] = "none"]
  /\ begun' = [begun EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, cards, lastAsk, last, lastRet>>

\* readers.go sweepReads: a read asked and not begun of a reader off goes back, but only
\* when another reader can take it (notaker: taken back with no other reader)
ReadReturn(r) ==
  /\ "noreadreturn" \notin Broken
  /\ rat[r] # "none"
  /\ ~begun[r]
  /\ Off(rat[r])
  /\ "notaker" \in Broken \/ Taker(rat[r])
  /\ rat' = [rat EXCEPT ![r] = "none"]
  /\ lastRet' = [kind |-> "return", taker |-> Taker(rat[r])]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, cards, begun, lastAsk, last>>

Next ==
  \/ \E m \in Members : Disk(m) \/ Tick(m) \/ Drain(m) \/ Restart(m) \/ Beat(m) \/ Hold(m) \/ Accept(m)
  \/ \E c \in Cards, m \in Members : TickDeal(c, m) \/ FleetDeal(c, m)
  \/ \E c \in Cards : Take(c) \/ Finish(c) \/ DrainReturn(c)
  \/ \E r \in Reads, m \in Members : Ask(r, m)
  \/ \E r \in Reads : Begin(r) \/ End(r) \/ ReadReturn(r)

\* the beat and the tick's returns run on their own clocks; the drain and the hold end
\* only when they end (no fairness: a drain lasts as long as its longest card)
Spec == Init /\ [][Next]_vars

FairSpec ==
  /\ Spec
  /\ \A m \in Members : WF_vars(Beat(m))
  /\ \A c \in Cards : WF_vars(DrainReturn(c))
  /\ \A r \in Reads : WF_vars(ReadReturn(r))

WordIsThisTicks == \A m \in Members : ticked[m] => (word[m] = (disk[m] = "low"))

NeverDealtUnderFloor == last.kind # "none" => ~last.under

NeverDealtToDraining == last.kind # "none" => ~last.drain

NeverDealtUnknown == last.kind # "none" => last.known

NeverAskedOfHeldOrDraining == lastAsk.kind # "none" => ~lastAsk.off

\* a card dealt to a draining member and not taken leaves it, or the drain ends
DealtReturns ==
  \A c \in Cards, m \in Members :
    (at[c] = m /\ ~taken[c] /\ draining[m]) ~> (at[c] # m \/ ~draining[m])

\* a read asked of a held machine's reader and not begun leaves it, or the hold ends; and
\* the same of a draining machine's reader and its drain (two leads-to: a hold and a drain
\* that take turns on one machine, neither lasting, owe no return); each only while another
\* reader can take it, and a read is never taken back when no other reader can take it.
ReadsReturn ==
  /\ \A r \in Reads, m \in Members :
       /\ (rat[r] = m /\ ~begun[r] /\ held[m] /\ Taker(m)) ~> (rat[r] # m \/ begun[r] \/ ~held[m] \/ ~Taker(m))
       /\ (rat[r] = m /\ ~begun[r] /\ draining[m] /\ Taker(m)) ~> (rat[r] # m \/ begun[r] \/ ~draining[m] \/ ~Taker(m))
  /\ (lastRet.kind = "return" /\ ~lastRet.taker) ~> FALSE

=============================================================================
