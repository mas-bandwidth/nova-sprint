---- MODULE HoldUntil ----
\* HoldUntil models a fleet member's timed and indefinite holds, --extend, and
\* the tick's automatic release at or after held_until (internal/sprint/hold_until.go).
\*
\*   HoldFor(m, d)       -> internal/sprint/hold.go:HoldNames + steps_work.go:downPlan (sets held_until = now + d)
\*   HoldIndefinite(m)   -> internal/sprint/hold.go:HoldNames + steps_work.go:downPlan (unsets held_until)
\*   Extend(m, d)        -> internal/sprint/hold_until.go:holdExtend (sets held_until = held_until + d)
\*   Unhold(m)           -> internal/sprint/hold.go:HoldNames (Release: true)
\*   Tick                -> internal/sprint/hold_until.go:holdUntilClear (releases held[m] with held_until > 0 and now >= held_until)
\*   AdvanceClock        -> discrete clock step between ticks

EXTENDS Naturals

CONSTANTS
    Members,
    MaxTime,
    MaxDur,
    BrokenOutlives,
    BrokenIndefiniteReleased,
    BrokenExtendShortens

VARIABLES
    now,
    held,
    heldUntil,
    beatAlive,
    status,
    ticked,
    lastOp,
    lastMember

vars == <<now, held, heldUntil, beatAlive, status, ticked, lastOp, lastMember>>

MaxUntil == MaxTime + 2 * MaxDur

StatusSet == {"up", "down", "held"}
OpSet == {"init", "hold_for", "hold_indefinite", "extend", "unhold", "beat", "tick", "clock"}

StatusFromBeat(alive) == IF alive THEN "up" ELSE "down"

Init ==
    /\ now = 0
    /\ held = [m \in Members |-> FALSE]
    /\ heldUntil = [m \in Members |-> 0]
    /\ beatAlive \in [Members -> BOOLEAN]
    /\ status = [m \in Members |-> StatusFromBeat(beatAlive[m])]
    /\ ticked = FALSE
    /\ lastOp = "init"
    /\ lastMember = CHOOSE m \in Members : TRUE

\* HoldFor(m, d): hold member m for duration d > 0; sets held_until = now + d.
HoldFor(m, d) ==
    /\ d \in 1..MaxDur
    /\ now + d <= MaxUntil
    /\ held' = [held EXCEPT ![m] = TRUE]
    /\ heldUntil' = [heldUntil EXCEPT ![m] = now + d]
    /\ status' = [status EXCEPT ![m] = "held"]
    /\ ticked' = FALSE
    /\ lastOp' = "hold_for"
    /\ lastMember' = m
    /\ UNCHANGED <<now, beatAlive>>

\* HoldIndefinite(m): hold member m indefinitely (held_until = 0, i.e., absent).
HoldIndefinite(m) ==
    /\ held' = [held EXCEPT ![m] = TRUE]
    /\ heldUntil' = [heldUntil EXCEPT ![m] = 0]
    /\ status' = [status EXCEPT ![m] = "held"]
    /\ ticked' = FALSE
    /\ lastOp' = "hold_indefinite"
    /\ lastMember' = m
    /\ UNCHANGED <<now, beatAlive>>

\* Extend(m, d): extend an active timed hold on m by d > 0.
\* BrokenExtendShortens models setting heldUntil'[m] = now + d instead of heldUntil[m] + d.
Extend(m, d) ==
    /\ d \in 1..MaxDur
    /\ held[m]
    /\ heldUntil[m] > 0
    /\ heldUntil[m] + d <= MaxUntil
    /\ LET nextUntil ==
            IF BrokenExtendShortens /\ now + d < heldUntil[m]
            THEN now + d
            ELSE heldUntil[m] + d
       IN heldUntil' = [heldUntil EXCEPT ![m] = nextUntil]
    /\ ticked' = FALSE
    /\ lastOp' = "extend"
    /\ lastMember' = m
    /\ UNCHANGED <<now, held, beatAlive, status>>

\* Unhold(m): manual unhold clears held and held_until, restoring status from beatAlive.
Unhold(m) ==
    /\ held[m]
    /\ held' = [held EXCEPT ![m] = FALSE]
    /\ heldUntil' = [heldUntil EXCEPT ![m] = 0]
    /\ status' = [status EXCEPT ![m] = StatusFromBeat(beatAlive[m])]
    /\ ticked' = FALSE
    /\ lastOp' = "unhold"
    /\ lastMember' = m
    /\ UNCHANGED <<now, beatAlive>>

\* BeatToggle(m): member m's heartbeat becomes alive or expires; while held, status stays "held".
BeatToggle(m) ==
    /\ beatAlive' = [beatAlive EXCEPT ![m] = ~beatAlive[m]]
    /\ status' = [status EXCEPT ![m] = IF held[m] THEN "held" ELSE StatusFromBeat(~beatAlive[m])]
    /\ ticked' = FALSE
    /\ lastOp' = "beat"
    /\ lastMember' = m
    /\ UNCHANGED <<now, held, heldUntil>>

\* ShouldRelease(m): whether Tick at `now` releases held member m.
\* - Normal: held[m] /\ heldUntil[m] > 0 /\ now >= heldUntil[m]
\* - BrokenOutlives: Tick skips the heldUntil release check
\* - BrokenIndefiniteReleased: Tick treats heldUntil[m] = 0 as 0 <= now and releases
ShouldRelease(m) ==
    /\ held[m]
    /\ ~BrokenOutlives
    /\ IF BrokenIndefiniteReleased
       THEN now >= heldUntil[m]
       ELSE heldUntil[m] > 0 /\ now >= heldUntil[m]

\* Tick evaluates at `now` and releases every held member whose timed hold has reached or passed `now`.
Tick ==
    /\ held' = [m \in Members |-> IF ShouldRelease(m) THEN FALSE ELSE held[m]]
    /\ heldUntil' = [m \in Members |-> IF ShouldRelease(m) THEN 0 ELSE heldUntil[m]]
    /\ status' = [m \in Members |->
        IF ShouldRelease(m)
        THEN StatusFromBeat(beatAlive[m])
        ELSE status[m]]
    /\ ticked' = TRUE
    /\ lastOp' = "tick"
    /\ UNCHANGED <<now, beatAlive, lastMember>>

\* AdvanceClock advances `now` by 1 up to MaxTime; ticked becomes FALSE until the next Tick runs.
AdvanceClock ==
    /\ now < MaxTime
    /\ now' = now + 1
    /\ ticked' = FALSE
    /\ lastOp' = "clock"
    /\ UNCHANGED <<held, heldUntil, beatAlive, status, lastMember>>

Next ==
    \/ \E m \in Members, d \in 1..MaxDur : HoldFor(m, d)
    \/ \E m \in Members : HoldIndefinite(m)
    \/ \E m \in Members, d \in 1..MaxDur : Extend(m, d)
    \/ \E m \in Members : Unhold(m)
    \/ \E m \in Members : BeatToggle(m)
    \/ Tick
    \/ AdvanceClock

Spec == Init /\ [][Next]_vars

\* ---- Safety Invariants & Action Properties ----

TypeOK ==
    /\ now \in 0..MaxTime
    /\ held \in [Members -> BOOLEAN]
    /\ heldUntil \in [Members -> 0..MaxUntil]
    /\ beatAlive \in [Members -> BOOLEAN]
    /\ status \in [Members -> StatusSet]
    /\ ticked \in BOOLEAN
    /\ lastOp \in OpSet
    /\ lastMember \in Members

\* StatusMatchesHold: a member's status is "held" iff held[m] is TRUE.
StatusMatchesHold ==
    \A m \in Members : held[m] <=> (status[m] = "held")

\* NoTimedHoldOutlivesItsTick: immediately after a Tick at `now`, no member with a timed hold
\* (heldUntil[m] > 0) remains held if `now >= heldUntil[m]`.
NoTimedHoldOutlivesItsTick ==
    ticked => \A m \in Members : (held[m] /\ heldUntil[m] > 0) => now < heldUntil[m]

TickOccurs == lastOp' = "tick" /\ now' = now

ExtendOccurs(m) == lastOp' = "extend" /\ lastMember' = m /\ heldUntil'[m] /= heldUntil[m]

\* IndefiniteHoldNeverReleasedByTick: a Tick step never releases an indefinite hold (heldUntil[m] = 0).
IndefiniteHoldNeverReleasedByTick ==
    [][(\A m \in Members : (held[m] /\ heldUntil[m] = 0 /\ TickOccurs) => (held'[m] /\ heldUntil'[m] = 0))]_vars

\* ExtendNeverShortens: an Extend step strictly increases heldUntil[m].
ExtendNeverShortens ==
    [][(\A m \in Members : (held[m] /\ heldUntil[m] > 0 /\ lastOp' = "extend" /\ lastMember' = m /\ vars' /= vars) => heldUntil'[m] > heldUntil[m])]_vars

====
