---------------------------- MODULE AttemptsEpochs ----------------------------
\* The sprint's epoch and clear (docs/SPEC-SPRINT.md section 13, "Epochs and
\* clear"; internal/sprint/store/epoch.go, clear.go, backend.go, redis.go,
\* mem.go, engine.go): four tables bound to one epoch hash; a clear advances
\* the epoch once, blanks the new epoch's streams and readers, and carries the
\* fleet's members over; the old epoch stays where it is and readable, and
\* every writer still holding it is refused as stale.
\*
\* The state: epoch, the sprint's epoch n; owed, the restore marker (the epoch
\* hash's restore field: true when a clear has advanced and not yet settled);
\* streams, readers and members, the per-epoch tables; pinned, the epoch the
\* one writer holds (its step pinned when it read); clears and cfrom, two
\* clears in flight, each with the epoch it read to advance from (the CAS);
\* ever, the epochs whose streams the writer ever wrote its card into (a ghost
\* for OldEpochReadable).
\*
\* The design, Broken = "none":
\*   PinStep   a step pins the store to the sprint's current epoch (every
\*             command builds its store pinned; a step whose epoch moved
\*             re-pins, engine.go:555-562).
\*   WriteStep the writer's step writes its card into the streams table at the
\*             epoch it pinned, and is refused as stale when the sprint has
\*             left that epoch (engine.go:566-575): it writes nothing. Broken
\*             "noepochcheck" drops that epoch check, so a late writer of the
\*             old epoch lands its card in the new one (OldEpochWritesNothing
\*             fails).
\*   Clear     two clears race on the CAS (AdvanceEpoch, clear.go:96): each
\*             reads the epoch it will advance from (ClearRead), the winner
\*             advances by one and settles the restore in the same step
\*             (ClearAdvance: streams and readers blank, members carried,
\*             clear.go:114-121), and a clear whose epoch another advanced
\*             past writes nothing (ClearRefuse, clear.go:100-110).
\*   Restore   the pin's restore (epoch.go:54, clear.go:184-222): when the
\*             epoch still owes its restore, the next pin copies the old
\*             epoch's streams, readers and members into the new one. A clear
\*             settles the restore it sets in its own step, so no crash cut
\*             between AdvanceEpoch and SettleEpoch is modelled here: owed is
\*             never observed, and Restore never fires. That cut (a crash
\*             leaving restore set) is a v1.2.14 candidate; its fix card adds
\*             the crash to this module.
\* Reversed witnesses:
\*   "noepochcheck"  the write drops the epoch check: OldEpochWritesNothing
\*                   fails.
EXTENDS Naturals, FiniteSets

CONSTANTS Card,      \* the writer's card
          Reader,    \* a reader row of the readers table
          Member,    \* a member row of the fleet
          MaxEpoch,  \* the epoch bound
          Broken     \* "none" or "noepochcheck"

VARIABLES epoch, owed, streams, readers, members, pinned, clears, cfrom, ever

vars == <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever>>

Epochs == 0..MaxEpoch
Clears == {"A", "B"}

TypeOK ==
    /\ epoch \in Epochs
    /\ owed \in BOOLEAN
    /\ streams \in [Epochs -> SUBSET {Card}]
    /\ readers \in [Epochs -> SUBSET {Reader}]
    /\ members \in [Epochs -> SUBSET {Member}]
    /\ pinned \in Epochs
    /\ clears \subseteq Clears
    /\ cfrom \in [Clears -> Epochs]
    /\ ever \in [Epochs -> BOOLEAN]

Init ==
    /\ epoch = 0
    /\ owed = FALSE
    /\ streams = [e \in Epochs |-> {}]
    /\ readers = [e \in Epochs |-> IF e = 0 THEN {Reader} ELSE {}]
    /\ members = [e \in Epochs |-> IF e = 0 THEN {Member} ELSE {}]
    /\ pinned = 0
    /\ clears = {}
    /\ cfrom = [k \in Clears |-> 0]
    /\ ever = [e \in Epochs |-> FALSE]

\* a step pins the store to the sprint's current epoch (a re-pin of a moved
\* step, engine.go:555-562; every command pins before it acts)
PinStep ==
    /\ pinned # epoch
    /\ pinned' = epoch
    /\ UNCHANGED <<epoch, owed, streams, readers, members, clears, cfrom, ever>>

\* the writer's step writes its card at the epoch it pinned; a step whose
\* epoch moved is refused as stale and writes nothing (engine.go:566-575)
WriteStep ==
    /\ Broken = "noepochcheck" \/ pinned = epoch
    /\ Card \notin streams[epoch]
    /\ streams' = [streams EXCEPT ![epoch] = streams[epoch] \cup {Card}]
    /\ ever' = [ever EXCEPT ![epoch] = TRUE]
    /\ UNCHANGED <<epoch, owed, readers, members, pinned, clears, cfrom>>

\* the pin's restore: when the epoch still owes its restore, copy the old
\* epoch's shape into it (epoch.go:54, clear.go:184-222). A clear settles the
\* restore it sets in its own step, so owed is never observed and this action
\* never fires; the crash cut that would is a v1.2.14 candidate.
Restore ==
    /\ owed
    /\ epoch > 0
    /\ streams' = [streams EXCEPT ![epoch] = streams[epoch-1]]
    /\ readers' = [readers EXCEPT ![epoch] = readers[epoch-1]]
    /\ members' = [members EXCEPT ![epoch] = members[epoch-1]]
    /\ owed' = FALSE
    /\ UNCHANGED <<epoch, pinned, clears, cfrom, ever>>

\* a clear reads the epoch it will advance from (clear.go:36-68: the epoch
\* read, the machine stopped and left stopped, then re-pinned)
ClearRead(k) ==
    /\ k \notin clears
    /\ epoch < MaxEpoch
    /\ clears' = clears \cup {k}
    /\ cfrom' = [cfrom EXCEPT ![k] = epoch]
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, ever>>

\* the CAS wins (AdvanceEpoch, clear.go:96): advance by one, settle the restore
\* it sets, blank the new streams and readers, carry the members (clear.go:
\* 112-121). The new epoch's own STOPPED note and the finish-or-abandon of the
\* old pending operation are out of scope here.
ClearAdvance(k) ==
    /\ k \in clears
    /\ cfrom[k] = epoch
    /\ epoch < MaxEpoch
    /\ epoch' = epoch + 1
    /\ streams' = [streams EXCEPT ![epoch + 1] = {}]
    /\ readers' = [readers EXCEPT ![epoch + 1] = {}]
    /\ members' = [members EXCEPT ![epoch + 1] = members[epoch]]
    /\ clears' = clears \ {k}
    /\ UNCHANGED <<owed, pinned, cfrom, ever>>

\* the CAS lost: another clear advanced the epoch first, so this one writes
\* nothing (clear.go:100-110)
ClearRefuse(k) ==
    /\ k \in clears
    /\ cfrom[k] # epoch
    /\ clears' = clears \ {k}
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, cfrom, ever>>

Next ==
    \/ PinStep
    \/ WriteStep
    \/ Restore
    \/ \E k \in Clears : ClearRead(k) \/ ClearAdvance(k) \/ ClearRefuse(k)

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* a writer holding an old epoch writes nothing into the new one (section 13:
\* "every writer still holding it is refused as stale: nothing of that epoch
\* lands in the new one")
OldEpochWritesNothing == pinned # epoch => Card \notin streams[epoch]

\* the new epoch is blank except the members: streams and readers are not
\* carried across a clear, and members are carried whole
NewEpochBlank ==
    /\ \A e \in 1..epoch : members[e] = members[e-1]
    /\ \A e \in 1..epoch : readers[e] = {}
    /\ \A e \in 1..epoch : (streams[e] # {} => ever[e])

\* the old epoch stays readable: a card written to an epoch stays there
OldEpochReadable == \A e \in Epochs : ever[e] => Card \in streams[e]

\* the epoch never goes back
EpochMonotone == [][epoch' >= epoch]_vars

=============================================================================
