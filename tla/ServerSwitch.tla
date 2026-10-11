------------------------------- MODULE ServerSwitch -------------------------------
\* The server switch and its rollback window (internal/sprint/server_switch.go
\* ServerSwitch, ServerRollback, CheckRollbackOnLandFailure; cmd/nova-sprint/
\* server_switch.go the guards and SwitchFromBase; cmd/nova-sprint/landloop.go
\* the exit that rolls back; docs/SPEC-SPRINT.md section 14). A switch backs up
\* the target to <target>.prev, copies the candidate to the target and, with
\* rollback, writes the switch state (the target, the previous binary, the
\* switched-at time, the window and the rollback flag). A failed land (exit 2)
\* inside the window rolls the target back to the previous binary and removes
\* the state; past the window the state is removed and the switch is permanent;
\* a refusal (exit 1) never rolls back. A manual rollback copies .prev to the
\* target and removes the state. A crash between the copy and the state write
\* switches the binary and writes no state.
\*
\* The binaries are b0 (the target's original) and b1, b2 (the candidates); the
\* target and .prev each hold one of them or none (a fresh disk, no prior
\* target). At most two switches, a window of 2, a clock 0..MaxClock, and a
\* land outcome of ok, refused or failed.
\*
\* The design is Broken = {}. Each other value is a reversed witness:
\*   "nodeadline"  the deadline check is skipped, so a failed land rolls back
\*                 past the window: RollbackOnlyInWindow fails.
\*   "keepstate"   the state is kept after a rollback, so one switch can roll
\*                 back twice: AtMostOneAutoRollbackPerSwitch fails.
EXTENDS Naturals

CONSTANTS Bins, Candidates, Window, MaxClock, MaxSwitches, MaxRolls, Broken

ASSUME Candidates \subseteq Bins
ASSUME "b0" \in Bins /\ "b0" \notin Candidates
ASSUME Window > 0 /\ MaxClock > Window

\* the switch state file: absent, or present with its switched-at clock
NoState == [present |-> FALSE, at |-> 0]
State(t) == [present |-> TRUE, at |-> t]

VARIABLES target,      \* the binary at the target: a bin or "none"
          prev,        \* the binary at <target>.prev: a bin or "none"
          state,       \* the switch state: NoState or State(t)
          clock,       \* the clock: 0..MaxClock
          switches,    \* switches done: 0..MaxSwitches
          rolls,       \* automatic rollbacks done: 0..MaxRolls
          outOfWindow, \* ghost: a rollback happened outside its window
          land         \* the land outcome being checked: ok, refused, failed

vars == <<target, prev, state, clock, switches, rolls, outOfWindow, land>>

TypeOK ==
    /\ target \in Bins \cup {"none"}
    /\ prev \in Bins \cup {"none"}
    /\ state \in [present: BOOLEAN, at: 0..MaxClock]
    /\ clock \in 0..MaxClock
    /\ switches \in 0..MaxSwitches
    /\ rolls \in 0..MaxRolls
    /\ outOfWindow \in BOOLEAN
    /\ land \in {"ok", "refused", "failed"}

Init ==
    /\ target \in {"b0", "none"}
    /\ prev = "none"
    /\ state = NoState
    /\ clock = 0
    /\ switches = 0
    /\ rolls = 0
    /\ outOfWindow = FALSE
    /\ land = "ok"

\* the clock moves one unit; it never goes back
Tick ==
    /\ clock < MaxClock
    /\ clock' = clock + 1
    /\ UNCHANGED <<target, prev, state, switches, rolls, outOfWindow, land>>

\* a land ends with an outcome; ok and refused change nothing by themselves,
\* a failed (exit 2) is the one that reaches Rollback
Land(o) ==
    /\ o \in {"ok", "refused", "failed"}
    /\ land' = o
    /\ UNCHANGED <<target, prev, state, clock, switches, rolls, outOfWindow>>

\* ServerSwitch with rollback: the target is backed up to .prev when it exists,
\* the candidate is copied to the target, and the state is written.
Switch(cand) ==
    /\ switches < MaxSwitches
    /\ cand \in Candidates
    /\ prev' = IF target = "none" THEN "none" ELSE target
    /\ target' = cand
    /\ state' = State(clock)
    /\ switches' = switches + 1
    /\ UNCHANGED <<clock, rolls, outOfWindow, land>>

\* a crash between the copy and the state write: the binary is switched and no
\* state is written (an older state stays).
SwitchCrash(cand) ==
    /\ switches < MaxSwitches
    /\ cand \in Candidates
    /\ prev' = IF target = "none" THEN "none" ELSE target
    /\ target' = cand
    /\ state' = state
    /\ switches' = switches + 1
    /\ UNCHANGED <<clock, rolls, outOfWindow, land>>

\* a failed or refused land past the window removes the state: the switch is
\* permanent, never a rollback.
Expire ==
    /\ land \in {"refused", "failed"}
    /\ state.present
    /\ clock > state.at + Window
    /\ "nodeadline" \notin Broken
    /\ state' = NoState
    /\ land' = "ok"
    /\ UNCHANGED <<target, prev, clock, switches, rolls, outOfWindow>>

\* a failed land (exit 2) inside the window rolls the target back to .prev and
\* removes the state; a refusal (exit 1) never reaches this action. The
\* previous binary missing (a switch with no prior target) errors, so no
\* rollback: prev = "none" disables it.
Rollback ==
    /\ land = "failed"
    /\ state.present
    /\ prev # "none"
    /\ rolls < MaxRolls
    /\ \/ "nodeadline" \in Broken
       \/ clock <= state.at + Window
    /\ target' = prev
    /\ state' = IF "keepstate" \in Broken THEN state ELSE NoState
    /\ rolls' = rolls + 1
    /\ outOfWindow' = (outOfWindow \/ (clock > state.at + Window))
    /\ land' = "ok"
    /\ UNCHANGED <<prev, clock, switches>>

\* a manual rollback (server switch --rollback with no binary): .prev to the
\* target, the state removed.
RollbackManual ==
    /\ prev # "none"
    /\ target' = prev
    /\ state' = NoState
    /\ UNCHANGED <<prev, clock, switches, rolls, outOfWindow, land>>

Next ==
    \/ Tick
    \/ \E o \in {"ok", "refused", "failed"} : Land(o)
    \/ \E cand \in Candidates : Switch(cand) \/ SwitchCrash(cand)
    \/ Expire
    \/ Rollback
    \/ RollbackManual

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------------ the rules

\* a rollback happens only inside its window: the deadline check holds
RollbackOnlyInWindow == ~outOfWindow

\* each switch is rolled back at most once
AtMostOneAutoRollbackPerSwitch == rolls <= switches

\* the target is always the candidate or the previous binary: a known binary,
\* or no binary before the first switch
TargetIsAKnownBinary == target \in Bins \/ (target = "none" /\ switches = 0)

=============================================================================
