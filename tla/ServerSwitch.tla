---------------------------- MODULE ServerSwitch ----------------------------
(***************************************************************************)
(* The server switch and its rollback window (internal/sprint/             *)
(* server_switch.go ServerSwitch, ServerRollback, CheckRollbackOnLandFailure; *)
(* cmd/nova-sprint/landloop.go landOnce; cmd/nova-sprint/run.go runLoop;    *)
(* docs/SPEC-SPRINT.md section 14, "server switch <binary> [--rollback]").  *)
(* ServerSwitch backs the target up to <target>.prev (only when a target    *)
(* exists), copies the candidate over the target, then writes                *)
(* <target>.switch.json when rollback is on; a crash between the copy and   *)
(* the state write leaves the switch without a rollback window. A land pass *)
(* fault (exit 2) inside the window rolls the previous binary back to the   *)
(* target and removes the state; past the deadline the state is removed and *)
(* the switch is permanent; a refusal (exit 1) never rolls back.            *)
(* ServerRollback restores the previous binary by hand and removes the      *)
(* state.                                                                   *)
(*                                                                          *)
(* Two facts of the code are modelled and worth naming:                     *)
(*   a second switch inside the window overwrites <target>.prev with the    *)
(*   first candidate (the backup copies the current target, which after the *)
(*   first switch is the first candidate), so the original binary is lost;  *)
(*   and a switch with no prior target (the fresh install) writes a state   *)
(*   whose Previous file is missing, so every failure inside the window     *)
(*   errors instead of rolling back.                                        *)
(*                                                                          *)
(* The design is Broken = {}. Each other value is a reversed witness:        *)
(*   "nodeadline"  skips the deadline check (a failure past the window      *)
(*                 rolls back): RollbackOnlyInWindow fails;                 *)
(*   "keepstate"   keeps the state after a rollback (a second failure in    *)
(*                 the window rolls back again):                            *)
(*                 AtMostOneAutoRollbackPerSwitch fails.                    *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Binaries,     \* the binary names: the original and the two candidates
          Candidate,    \* [1..MaxSwitch -> Binaries]: the candidate of each switch
          Window,       \* the rollback window length
          MaxClock,     \* the clock's bound
          MaxSwitch,    \* the switch bound (at most two switches)
          Broken        \* the reversed witnesses: a subset of Faults

Faults == {"nodeadline", "keepstate"}
ASSUME Broken \subseteq Faults
ASSUME Candidate \in [1..MaxSwitch -> Binaries]
ASSUME Window > 0 /\ MaxClock >= 0 /\ MaxSwitch >= 1
ASSUME "none" \notin Binaries

VARIABLES target,       \* the binary now at the target: Binaries, or "none" (no target yet)
          prev,         \* the binary at <target>.prev: Binaries, or "none" (no backup)
          hasState,     \* is there a <target>.switch.json
          statePrev,    \* the state's Previous (meaningful when hasState)
          stateAt,      \* the state's SwitchedAt clock (meaningful when hasState)
          clock,        \* the wall clock, 0..MaxClock
          switches,     \* switches made, bounded by MaxSwitch
          midSwitch,    \* TRUE between the copy and the state write (the crash point)
          autoRollbacks,  \* ghost: auto-rollbacks made (AtMostOneAutoRollbackPerSwitch)
          rolledLate      \* ghost: an auto-rollback past the deadline happened

vars == <<target, prev, hasState, statePrev, stateAt, clock, switches,
          midSwitch, autoRollbacks, rolledLate>>

TypeOK ==
  /\ target \in Binaries \cup {"none"}
  /\ prev \in Binaries \cup {"none"}
  /\ hasState \in BOOLEAN
  /\ statePrev \in Binaries \cup {"none"}
  /\ stateAt \in 0..MaxClock
  /\ clock \in 0..MaxClock
  /\ switches \in 0..MaxSwitch
  /\ midSwitch \in BOOLEAN
  /\ autoRollbacks \in 0..MaxSwitch
  /\ rolledLate \in BOOLEAN

Init ==
  /\ target \in Binaries \cup {"none"}
  /\ prev = "none"
  /\ hasState = FALSE
  /\ statePrev = "none"
  /\ stateAt = 0
  /\ clock = 0
  /\ switches = 0
  /\ midSwitch = FALSE
  /\ autoRollbacks = 0
  /\ rolledLate = FALSE

\* the wall clock advances
Tick == /\ midSwitch = FALSE
        /\ clock < MaxClock
        /\ clock' = clock + 1
        /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, switches,
                       midSwitch, autoRollbacks, rolledLate>>

\* ServerSwitch: back the target up to .prev (when there is one) and copy the
\* candidate over the target; the state write is the next step, so a crash
\* between the copy and the state write is midSwitch.
Switch == /\ midSwitch = FALSE
          /\ switches < MaxSwitch
          /\ target' = Candidate[switches + 1]
          /\ prev' = IF target = "none" THEN "none" ELSE target
          /\ switches' = switches + 1
          /\ midSwitch' = TRUE
          /\ UNCHANGED <<hasState, statePrev, stateAt, clock, autoRollbacks, rolledLate>>

\* the state write: <target>.switch.json records Target, Previous, SwitchedAt.
SwitchWriteState == /\ midSwitch = TRUE
                    /\ hasState' = TRUE
                    /\ statePrev' = prev
                    /\ stateAt' = clock
                    /\ midSwitch' = FALSE
                    /\ UNCHANGED <<target, prev, clock, switches, autoRollbacks, rolledLate>>

\* a crash between the copy and the state write: the target is switched but no
\* state is written, so no rollback window opens.
CrashSwitch == /\ midSwitch = TRUE
               /\ midSwitch' = FALSE
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, autoRollbacks, rolledLate>>

\* ServerRollback: the previous binary back to the target, the state removed.
ManualRollback == /\ midSwitch = FALSE
                  /\ prev # "none"
                  /\ target' = prev
                  /\ hasState' = FALSE
                  /\ statePrev' = "none"
                  /\ stateAt' = 0
                  /\ UNCHANGED <<prev, clock, switches, midSwitch, autoRollbacks, rolledLate>>

\* A land that is ok (exit 0) rolls nothing back. A refusal (exit 1) reaches
\* CheckRollbackOnLandFailure (landloop.go calls it on every nonzero exit) but
\* returns without rolling back (the le.Code != 2 filter), so only a pass fault
\* (exit 2) rolls back. The v1.2.11 card made a refusal (exit 1) stop rolling back.
LandOk == /\ midSwitch = FALSE /\ UNCHANGED vars
LandRefused == /\ midSwitch = FALSE /\ UNCHANGED vars

\* a pass fault (exit 2) with no state: nothing rolls back.
LandFailedNoState == /\ midSwitch = FALSE
                     /\ ~hasState
                     /\ UNCHANGED vars

\* a pass fault past the deadline: the state is removed, the switch permanent
\* (with "nodeadline" the deadline check is skipped, so this never fires).
LandFailedPastDeadline == /\ midSwitch = FALSE
                          /\ hasState
                          /\ clock > stateAt + Window
                          /\ "nodeadline" \notin Broken
                          /\ hasState' = FALSE
                          /\ statePrev' = "none"
                          /\ stateAt' = 0
                          /\ UNCHANGED <<target, prev, clock, switches, midSwitch,
                                         autoRollbacks, rolledLate>>

\* a pass fault in the window with no previous binary (the fresh install): the
\* rollback errors, the state stays, and every such failure errors.
LandFailedError == /\ midSwitch = FALSE
                   /\ hasState
                   /\ statePrev = "none"
                   /\ \/ clock <= stateAt + Window
                      \/ "nodeadline" \in Broken
                   /\ UNCHANGED vars

\* a pass fault in the window with a previous binary: roll back (and, with
\* "nodeadline", roll back even past the deadline).
LandFailedRollback == /\ midSwitch = FALSE
                      /\ hasState
                      /\ statePrev # "none"
                      /\ \/ clock <= stateAt + Window
                         \/ "nodeadline" \in Broken
                      /\ target' = statePrev
                      /\ hasState' = ("keepstate" \in Broken)
                      /\ statePrev' = IF "keepstate" \in Broken THEN statePrev ELSE "none"
                      /\ stateAt' = IF "keepstate" \in Broken THEN stateAt ELSE 0
                      /\ autoRollbacks' = autoRollbacks + 1
                      /\ rolledLate' = (rolledLate \/ (clock > stateAt + Window))
                      /\ UNCHANGED <<prev, clock, switches, midSwitch>>

Next ==
  \/ Tick
  \/ Switch \/ SwitchWriteState \/ CrashSwitch
  \/ ManualRollback
  \/ LandOk \/ LandRefused
  \/ LandFailedNoState \/ LandFailedPastDeadline \/ LandFailedError
     \/ LandFailedRollback

Spec == Init /\ [][Next]_vars

---------------------------------------------------------------------------
\* The target is always a known binary (the original or a candidate) or absent
\* (the fresh install, no target yet); never any other value.
TargetIsAKnownBinary == target \in Binaries \/ target = "none"

\* A rollback happens only within the window (deadline = SwitchedAt + Window).
RollbackOnlyInWindow == ~rolledLate

\* Each switch rolls back at most once: an auto-rollback consumes its state, so
\* there can be no more auto-rollbacks than switches.
AtMostOneAutoRollbackPerSwitch == autoRollbacks <= switches
=============================================================================
