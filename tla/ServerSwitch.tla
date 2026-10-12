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
(* The hook gate (cmd/nova-sprint/hook.go hookedIn; land.go, coordinator.go *)
(* hookGateError): the machine's landing and the coordinator's are split by *)
(* identity. The server's own automatic landing of accepted cards (the      *)
(* loop's pass) is machine work and is exempt from the gate (land.go reopens*)
(* the store as run when the gate refuses, so the automatic pass runs       *)
(* whatever the hook); only a coordinator's hand land (nova-sprint land)    *)
(* still passes the gate. A restart drops the seat's hook (SwitchRestart);  *)
(* the seat hooks in again (SeatHooksIn). A hand land the gate refuses      *)
(* before it acts (LandHookRefused) is a wait, not a fault, so it never     *)
(* rolls the switch back and leaves the window as it was.                   *)
(*                                                                          *)
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
(*                 AtMostOneAutoRollbackPerSwitch fails;                    *)
(*   "hookfault"   counts a hand land the hook gate refused as a land fault *)
(*                 (the v1.2.12 switch rolled itself back 1.5 s after it    *)
(*                 began): NoRollbackOnAHookRefusal fails;                  *)
(*   "hookgate"    restores the old gate (the automatic pass waits while    *)
(*                 the seat is unhooked): LandingNeverWaitsOnTheHook fails. *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Binaries,     \* the binary names: the original and the two candidates
          Candidate,    \* [1..MaxSwitch -> Binaries]: the candidate of each switch
          Window,       \* the rollback window length
          MaxClock,     \* the clock's bound
          MaxSwitch,    \* the switch bound (at most two switches)
          Broken        \* the reversed witnesses: a subset of Faults

Faults == {"nodeadline", "keepstate", "hookfault", "hookgate"}
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
          rolledLate,     \* ghost: an auto-rollback past the deadline happened
          hooked,         \* the seat holds a live, proven hook (the hand land's gate)
          rolledOnRefusal, \* ghost: a rollback was made for a land the hook gate refused
          landTried        \* ghost: the automatic pass landed or refused (never the hook)

vars == <<target, prev, hasState, statePrev, stateAt, clock, switches,
          midSwitch, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>

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
  /\ hooked \in BOOLEAN
  /\ rolledOnRefusal \in BOOLEAN
  /\ landTried \in BOOLEAN

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
  /\ hooked \in BOOLEAN
  /\ rolledOnRefusal = FALSE
  /\ landTried = FALSE

\* the wall clock advances
Tick == /\ midSwitch = FALSE
        /\ clock < MaxClock
        /\ clock' = clock + 1
        /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, switches,
                       midSwitch, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>

\* ServerSwitch: back the target up to .prev (when there is one) and copy the
\* candidate over the target; the state write is the next step, so a crash
\* between the copy and the state write is midSwitch.
Switch == /\ midSwitch = FALSE
          /\ switches < MaxSwitch
          /\ target' = Candidate[switches + 1]
          /\ prev' = IF target = "none" THEN "none" ELSE target
          /\ switches' = switches + 1
          /\ midSwitch' = TRUE
          /\ UNCHANGED <<hasState, statePrev, stateAt, clock, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>

\* the state write: <target>.switch.json records Target, Previous, SwitchedAt.
SwitchWriteState == /\ midSwitch = TRUE
                    /\ hasState' = TRUE
                    /\ statePrev' = prev
                    /\ stateAt' = clock
                    /\ midSwitch' = FALSE
                    /\ UNCHANGED <<target, prev, clock, switches, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>

\* a crash between the copy and the state write: the target is switched but no
\* state is written, so no rollback window opens.
CrashSwitch == /\ midSwitch = TRUE
               /\ midSwitch' = FALSE
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>

\* ServerRollback: the previous binary back to the target, the state removed.
ManualRollback == /\ midSwitch = FALSE
                  /\ prev # "none"
                  /\ target' = prev
                  /\ hasState' = FALSE
                  /\ statePrev' = "none"
                  /\ stateAt' = 0
                  /\ UNCHANGED <<prev, clock, switches, midSwitch, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>

\* The automatic pass is enabled whatever the hook; only the reversed witness
\* "hookgate" restores the old gate, under which the pass waits while the seat is
\* unhooked.
AutoLandEnabled == "hookgate" \notin Broken \/ hooked

\* A land that is ok (exit 0) rolls nothing back. A refusal (exit 1) reaches
\* CheckRollbackOnLandFailure (landloop.go calls it on every nonzero exit) but
\* returns without rolling back (the le.Code != 2 filter), so only a pass fault
\* (exit 2) rolls back. The v1.2.11 card made a refusal (exit 1) stop rolling back.
\* The automatic pass runs whatever the hook (the reversed witness "hookgate"
\* restores the old gate, which waited while the seat was unhooked).
LandOk == /\ midSwitch = FALSE /\ AutoLandEnabled /\ landTried' = TRUE
          /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                         switches, midSwitch, autoRollbacks, rolledLate, hooked,
                         rolledOnRefusal>>
LandRefused == /\ midSwitch = FALSE /\ AutoLandEnabled /\ landTried' = TRUE
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, midSwitch, autoRollbacks, rolledLate, hooked,
                              rolledOnRefusal>>

\* The server restarts (a switch, a rollback, a crash): the seat's hook is gone with it
\* (the hook record says state=gone, why=the server restarted). SwitchRestart.
SwitchRestart == /\ midSwitch = FALSE
                 /\ hooked
                 /\ hooked' = FALSE
                 /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                                switches, midSwitch, autoRollbacks, rolledLate,
                                rolledOnRefusal, landTried>>

\* The seat runs nova-sprint hook and answers the challenge: the gate lets a hand
\* land run (the automatic pass never waits on it).
SeatHooksIn == /\ midSwitch = FALSE
               /\ ~hooked
               /\ hooked' = TRUE
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, midSwitch, autoRollbacks, rolledLate,
                              rolledOnRefusal, landTried>>

\* A hand land (a coordinator command, not the loop's pass) the hook gate refuses
\* before it acts on anything (the seat has not hooked in: cmd/nova-sprint/land.go,
\* the store's open): a wait, not a fault. The window, the binary and the state are
\* left as they were, and the hand land tries again. With "hookfault" the refusal is
\* counted as a land fault (LandHookFault below), the v1.2.12 defect.
LandHookRefused == /\ midSwitch = FALSE
                   /\ ~hooked
                   /\ "hookfault" \notin Broken
                   /\ UNCHANGED vars

\* The defect, reversed: the refused hand land reaches the rollback check as a fault,
\* and inside the window with a previous binary it rolls the switch back.
LandHookFault == /\ midSwitch = FALSE
                 /\ ~hooked
                 /\ "hookfault" \in Broken
                 /\ hasState
                 /\ statePrev # "none"
                 /\ clock <= stateAt + Window
                 /\ target' = statePrev
                 /\ hasState' = FALSE
                 /\ statePrev' = "none"
                 /\ stateAt' = 0
                 /\ autoRollbacks' = autoRollbacks + 1
                 /\ rolledOnRefusal' = TRUE
                 /\ UNCHANGED <<prev, clock, switches, midSwitch, rolledLate, hooked, landTried>>

\* a pass fault (exit 2) with no state: nothing rolls back.
LandFailedNoState == /\ midSwitch = FALSE
                     /\ AutoLandEnabled
                     /\ ~hasState
                     /\ landTried' = TRUE
                     /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                                    switches, midSwitch, autoRollbacks, rolledLate, hooked,
                                    rolledOnRefusal>>

\* a pass fault past the deadline: the state is removed, the switch permanent
\* (with "nodeadline" the deadline check is skipped, so this never fires).
LandFailedPastDeadline == /\ midSwitch = FALSE
                          /\ AutoLandEnabled
                          /\ hasState
                          /\ clock > stateAt + Window
                          /\ "nodeadline" \notin Broken
                          /\ hasState' = FALSE
                          /\ statePrev' = "none"
                          /\ stateAt' = 0
                          /\ landTried' = TRUE
                          /\ UNCHANGED <<target, prev, clock, switches, midSwitch,
                                         autoRollbacks, rolledLate, hooked, rolledOnRefusal>>

\* a pass fault in the window with no previous binary (the fresh install): the
\* rollback errors, the state stays, and every such failure errors.
LandFailedError == /\ midSwitch = FALSE
                   /\ AutoLandEnabled
                   /\ hasState
                   /\ statePrev = "none"
                   /\ \/ clock <= stateAt + Window
                      \/ "nodeadline" \in Broken
                   /\ landTried' = TRUE
                   /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                                  switches, midSwitch, autoRollbacks, rolledLate, hooked,
                                  rolledOnRefusal>>

\* a pass fault in the window with a previous binary: roll back (and, with
\* "nodeadline", roll back even past the deadline).
LandFailedRollback == /\ midSwitch = FALSE
                      /\ AutoLandEnabled
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
                      /\ landTried' = TRUE
                      /\ UNCHANGED <<prev, clock, switches, midSwitch, hooked, rolledOnRefusal>>

Next ==
  \/ Tick
  \/ Switch \/ SwitchWriteState \/ CrashSwitch
  \/ ManualRollback
  \/ LandOk \/ LandRefused
  \/ SwitchRestart \/ SeatHooksIn \/ LandHookRefused \/ LandHookFault
  \/ LandFailedNoState \/ LandFailedPastDeadline \/ LandFailedError
     \/ LandFailedRollback

\* The automatic land pass, whatever its outcome: a land, a refusal, or a failure
\* (which may roll back). The hand land's gate is LandHookRefused, not this.
AutoLand == LandOk \/ LandRefused \/ LandFailedNoState \/ LandFailedPastDeadline
            \/ LandFailedError \/ LandFailedRollback

Spec == Init /\ [][Next]_vars

\* The loop runs pass after pass, so a pass enabled (whatever the hook) eventually
\* runs; a switch left mid-write (midSwitch) still resolves, to the state write or
\* the crash. This is the fairness LandingNeverWaitsOnTheHook is read under.
FairSpec == Spec /\ WF_vars(AutoLand) /\ WF_vars(SwitchWriteState) /\ WF_vars(CrashSwitch)

---------------------------------------------------------------------------
\* The target is always a known binary (the original or a candidate) or absent
\* (the fresh install, no target yet); never any other value.
TargetIsAKnownBinary == target \in Binaries \/ target = "none"

\* A rollback happens only within the window (deadline = SwitchedAt + Window).
RollbackOnlyInWindow == ~rolledLate

\* Each switch rolls back at most once: an auto-rollback consumes its state, so
\* there can be no more auto-rollbacks than switches.
AtMostOneAutoRollbackPerSwitch == autoRollbacks <= switches

\* A land the hook gate refused never rolls a switch back (the v1.2.12 switch rolled
\* itself back 1.5 seconds after it began, on the new server's first pass, before the
\* seat hooked in again). The gate now refuses only a hand land, so the ghost is the
\* hand land's alone.
NoRollbackOnAHookRefusal == ~rolledOnRefusal

\* An accepted card with the seat unhooked and the server running (midSwitch = FALSE) is
\* landed or refused for a reason other than the hook: the automatic pass runs whatever
\* the hook, so it never waits for the seat to hook in (under weak fairness of the pass).
LandingNeverWaitsOnTheHook == (~hooked /\ midSwitch = FALSE) ~> landTried
=============================================================================
