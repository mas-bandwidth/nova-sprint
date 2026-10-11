-------------------------------- MODULE NoRoom --------------------------------
\* A member's no-room word (the disk floor, the usage source's rest), its drain, and
\* what the deal and the read ask do with them (v1.2.6; nova-tools#5569, #5578 and the
\* drain follow-up; v1.2.10: the drain told to the server, and a held machine's reader).
\* v1.2.12: a member is known (accepted into the fleet) only once its own probe passed
\* (the probe gate, steps_work.go:2594-2621), and the deal and the reader's ask both
\* read it: no deal to a member the sprint does not know (NeverDealtUnknown), and a
\* reader whose machine is not a fleet member belongs to no machine and is asked like
\* any reader (drain.go ReaderOwner, readerOff). sweepReads takes a read back only when
\* another reader can take it (readers.go:669-683, 694), so ReadReturn is guarded on a
\* taker and ReadsReturn is restated under that guard.
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
\* (readerOff, readerRooms, sweepReads).
\*
\* The state, per member: disk, the machine's real state (ok or low: under its
\* floor), an outside event; word, the member's word from its last Room (TRUE: no
\* room); ticked, a tick has run since disk last changed; draining; stored, the
\* word of the member's last beat, as the store holds it; sdrain, the drain of the
\* member's last beat, as the store holds it; held, the coordinator's hold on the
\* machine; known, the member has been accepted into the fleet; probe, the
\* caller's own word for the member's probe (the --probe fleet up is given), which
\* the model trusts as the code does (TRUE: passed). Per card: at, the member it is
\* dealt to, or "none"; taken, its member took it (a child runs it). Per read: rat,
\* the machine whose reader it is asked of (the reader of m stands for every reader
\* row of m), or "none"; begun, that reader began it. last records the last deal:
\* its kind (tick, fleet) and whether the store's word for that member said no room
\* then, whether the store held it draining, and whether it was known; lastAsk the
\* last ask and whether its machine took no work then.
\*
\* The design, Broken = {}:
\*   Disk        the machine's disk falls under its floor or is freed.
\*   Tick        the member asks Room every tick, draining or not: its word is
\*               the disk as it is now.
\*   Drain       the member begins draining (its binary was replaced).
\*   Restart     a draining member that runs nothing stops; its supervisor starts
\*               the new binary, which does not drain.
\*   Beat        the member's beat writes its word and its drain to the store.
\*   Hold        the coordinator holds the machine, or releases it.
\*   Probe       the caller runs the member's own probe: its word is "passed" or
\*               not (the model trusts it as the code does).
\*   Accept      fleet up accepts a new member only on a passed probe: known.
\*   TickDeal    the tick deals a card to a member whose stored word is room and
\*               that the store does not hold draining, nor held; known.
\*   FleetDeal   a verb outside the tick's deal (a level, a down's or a hold's
\*               redeal, a rework's or a redo's deal, the rebalance) moves a card
\*               not taken to such a member: each plans with notQuiet on the
\*               words its step read (Step.ReadNoRoom, readerStatesInto).
\*   Take        a member that does not drain takes a card dealt to it.
\*   Finish      a card taken ends and leaves its member.
\*   DrainReturn the tick's level returns a card dealt and not taken from a member
\*               the store holds draining.
\*   Ask         the tick asks a read of the reader of a machine neither held nor
\*               held draining; a reader of an unknown machine is asked like any.
\*   Begin, End  the reader begins the read, and ends it.
\*   ReadReturn  the tick takes back a read asked and not begun of the reader of a
\*               machine held or held draining, but only when another reader can
\*               take it (a taker); otherwise it stays until the machine takes
\*               work again.
\* Invariants: WordIsThisTicks (a member that has ticked since the disk last
\* changed says the disk as it is), NeverDealtUnderFloor (no deal, by the tick
\* or by a fleet verb, goes to a member whose stored word says no room),
\* NeverDealtToDraining (none goes to a member the store holds draining),
\* NeverDealtUnknown (none goes to a member the sprint does not know),
\* NeverAskedOfHeldOrDraining (no read is asked of a held or draining machine's reader).
\* Liveness: DealtReturns (a card dealt to a draining member and not taken leaves it,
\* or the drain ends), ReadsReturn (a read asked of a held or draining machine's reader
\* and not begun leaves it for a taker, or the machine takes work again, or no other
\* reader can take it; and a read is never taken back while no other reader can take it).
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
\*   "probeblind"  the deal does not read the probe gate: a card goes to a member
\*                 the sprint does not know: NeverDealtUnknown
\*   "notaker"     a read asked of a held or draining machine's reader is taken
\*                 back even when no other reader can take it:
\*                 NeverReturnedWithoutATaker
EXTENDS Naturals, FiniteSets

\* Disky: the disk falls and is freed (the no-room word's instance); Holds: the coordinator
\* holds and releases machines (the drain's and the reads' instance). The two instances
\* are checked apart, so each stays small.
CONSTANTS Members, Cards, Reads, Disky, Holds, Broken

ASSUME Disky \in BOOLEAN /\ Holds \in BOOLEAN

ASSUME Broken \subseteq {"drainskip", "fleetblind", "tickblind", "silentdrain", "drainblind", "noreturn", "readblind", "noreadreturn", "probeblind", "notaker"}

VARIABLES disk, word, ticked, draining, stored, sdrain, held, known, probe, at, taken, rat, begun, last, lastAsk, lastRet

vars == <<disk, word, ticked, draining, stored, sdrain, held, known, probe, at, taken, rat, begun, last, lastAsk, lastRet>>

TypeOK ==
  /\ disk \in [Members -> {"ok", "low"}]
  /\ word \in [Members -> BOOLEAN]
  /\ ticked \in [Members -> BOOLEAN]
  /\ draining \in [Members -> BOOLEAN]
  /\ stored \in [Members -> BOOLEAN]
  /\ sdrain \in [Members -> BOOLEAN]
  /\ held \in [Members -> BOOLEAN]
  /\ known \in [Members -> BOOLEAN]
  /\ probe \in [Members -> BOOLEAN]
  /\ at \in [Cards -> Members \cup {"none"}]
  /\ taken \in [Cards -> BOOLEAN]
  /\ rat \in [Reads -> Members \cup {"none"}]
  /\ begun \in [Reads -> BOOLEAN]
  /\ last \in [kind : {"none", "tick", "fleet"}, under : BOOLEAN, drain : BOOLEAN, known : BOOLEAN]
  /\ lastAsk \in [kind : {"none", "ask"}, off : BOOLEAN]
  /\ lastRet \in [kind : {"none", "ret"}, taker : BOOLEAN]

Init ==
  /\ disk = [m \in Members |-> "ok"]
  /\ word = [m \in Members |-> FALSE]
  /\ ticked = [m \in Members |-> TRUE]
  /\ draining = [m \in Members |-> FALSE]
  /\ stored = [m \in Members |-> FALSE]
  /\ sdrain = [m \in Members |-> FALSE]
  /\ held = [m \in Members |-> FALSE]
  /\ known = [m \in Members |-> FALSE]
  /\ probe = [m \in Members |-> FALSE]
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
  /\ UNCHANGED <<word, draining, stored, sdrain, held, known, probe, cards, reads, last, lastRet>>

\* member.go Tick: room(now) before the drain return (drainskip: after it)
Tick(m) ==
  /\ word' = IF draining[m] /\ "drainskip" \in Broken
                THEN word
                ELSE [word EXCEPT ![m] = (disk[m] = "low")]
  /\ ticked' = [ticked EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, draining, stored, sdrain, held, known, probe, cards, reads, last, lastRet>>

Drain(m) ==
  /\ ~draining[m]
  /\ draining' = [draining EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, stored, sdrain, held, known, probe, cards, reads, last, lastRet>>

\* nova-swarm memberLoop: the last child reported, it stops; the new binary does not drain
Restart(m) ==
  /\ draining[m]
  /\ \A c \in Cards : at[c] = m => ~taken[c]
  /\ draining' = [draining EXCEPT ![m] = FALSE]
  /\ UNCHANGED <<disk, word, ticked, stored, sdrain, held, known, probe, cards, reads, last, lastRet>>

\* Store.BeatDraining: the word and the drain of this beat (silentdrain: no drain said)
Beat(m) ==
  /\ stored' = [stored EXCEPT ![m] = word[m]]
  /\ sdrain' = [sdrain EXCEPT ![m] = draining[m] /\ "silentdrain" \notin Broken]
  /\ UNCHANGED <<disk, word, ticked, draining, held, known, probe, cards, reads, last, lastRet>>

Hold(m) ==
  /\ Holds
  /\ held' = [held EXCEPT ![m] = ~@]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, known, probe, cards, reads, last, lastRet>>

\* steps_work.go fleetStepPlan: a member the sprint does not know runs its own probe
\* before its width is accepted (the caller's word --probe, trusted as the code does).
Probe(m) ==
  /\ ~known[m]
  /\ probe' = [probe EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, cards, reads, last, lastRet>>

\* fleet up accepts a new member only on a passed probe (probe[m]); the model trusts
\* the caller's word: a member never probed, or a failed one, stays unknown.
Accept(m) ==
  /\ ~known[m]
  /\ probe[m]
  /\ known' = [known EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, probe, cards, reads, last, lastRet>>

\* the deal's up is notQuiet: no word of no room, no drain the store holds; a held member is down
\* (probeblind: the deal reads no probe gate, so a member the sprint does not know is dealt)
Dealable(m, blind) ==
  /\ ~held[m]
  /\ blind \/ ~stored[m]
  /\ "drainblind" \in Broken \/ ~sdrain[m]
  /\ "probeblind" \in Broken \/ known[m]

TickDeal(c, m) ==
  /\ at[c] = "none"
  /\ Dealable(m, "tickblind" \in Broken)
  /\ at' = [at EXCEPT ![c] = m]
  /\ last' = [kind |-> "tick", under |-> stored[m], drain |-> sdrain[m], known |-> known[m]]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, taken, reads, lastRet>>

\* a fleet verb moves a dealt card not taken from one member to another (a level, a down's redeal)
FleetDeal(c, m) ==
  /\ at[c] \notin {"none", m}
  /\ ~taken[c]
  /\ Dealable(m, "fleetblind" \in Broken)
  /\ at' = [at EXCEPT ![c] = m]
  /\ last' = [kind |-> "fleet", under |-> stored[m], drain |-> sdrain[m], known |-> known[m]]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, taken, reads, lastRet>>

\* member.go Tick: a draining member starts nothing
Take(c) ==
  /\ at[c] # "none"
  /\ ~taken[c]
  /\ ~draining[at[c]]
  /\ taken' = [taken EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, at, reads, last, lastRet>>

Finish(c) ==
  /\ taken[c]
  /\ at' = [at EXCEPT ![c] = "none"]
  /\ taken' = [taken EXCEPT ![c] = FALSE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, reads, last, lastRet>>

\* drain.go drainReturns: the level withdraws a card dealt and not taken off a member held draining
DrainReturn(c) ==
  /\ "noreturn" \notin Broken
  /\ at[c] # "none"
  /\ ~taken[c]
  /\ sdrain[at[c]]
  /\ at' = [at EXCEPT ![c] = "none"]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, taken, reads, last, lastRet>>

\* readerOff: the reader's machine takes no work (known: a reader of a machine the
\* sprint does not know belongs to no machine and is asked like any reader)
Off(m) == known[m] /\ (held[m] \/ sdrain[m])

\* a taker is another machine whose reader can take the read: known, not held, not draining
Taker(m) == \E other \in Members : other # m /\ known[other] /\ ~held[other] /\ ~sdrain[other]

Ask(r, m) ==
  /\ rat[r] = "none"
  /\ "readblind" \in Broken \/ ~Off(m)
  /\ rat' = [rat EXCEPT ![r] = m]
  /\ lastAsk' = [kind |-> "ask", off |-> Off(m)]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, cards, begun, last, lastRet>>

\* the reader loop knows nothing of its machine's hold: it may begin what it was asked
Begin(r) ==
  /\ rat[r] # "none"
  /\ ~begun[r]
  /\ ~draining[rat[r]]
  /\ begun' = [begun EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, cards, rat, lastAsk, last, lastRet>>

End(r) ==
  /\ begun[r]
  /\ rat' = [rat EXCEPT ![r] = "none"]
  /\ begun' = [begun EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, cards, lastAsk, last, lastRet>>

\* readers.go sweepReads: a read asked and not begun of a reader off goes back only
\* when another reader can take it (a taker); "notaker" takes it back with no taker
ReadReturn(r) ==
  /\ "noreadreturn" \notin Broken
  /\ rat[r] # "none"
  /\ ~begun[r]
  /\ Off(rat[r])
  /\ "notaker" \in Broken \/ Taker(rat[r])
  /\ rat' = [rat EXCEPT ![r] = "none"]
  /\ lastRet' = [kind |-> "ret", taker |-> Taker(rat[r])]
  /\ UNCHANGED <<disk, word, ticked, draining, stored, sdrain, held, known, probe, cards, begun, lastAsk, last>>

Next ==
  \/ \E m \in Members : Disk(m) \/ Tick(m) \/ Drain(m) \/ Restart(m) \/ Beat(m) \/ Hold(m) \/ Probe(m) \/ Accept(m)
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

\* a read asked of a held or draining machine's reader and not begun leaves it for a
\* taker, or the machine takes work again, or no other reader can take it (sweepReads'
\* taker: with no other reader the read stays)
ReadsReturn ==
  \A r \in Reads, m \in Members :
    (rat[r] = m /\ ~begun[r] /\ Off(m)) ~> (rat[r] # m \/ begun[r] \/ ~Off(m) \/ ~Taker(m))

\* the taker guard's safety: a read asked and not begun of a held or draining machine's
\* reader is never taken back while no other reader can take it (sweepReads' taker)
NeverReturnedWithoutATaker == lastRet.kind # "none" => lastRet.taker

=============================================================================
