---------------------------- MODULE MachineReady ----------------------------
(***************************************************************************)
(* The machine's READY state (docs/SPEC-SPRINT.md section 14, "READY"; the *)
(* owner, 2026-10-13: "I would like a new state right after the sprint is  *)
(* cleared 'READY'", "it is different to stop").                           *)
(*                                                                         *)
(* The record holds RUNNING or STOPPED (run) and the last epoch the        *)
(* machine ran in (stamp: StartedIn, written by start and by every stop    *)
(* of a RUNNING machine). The state people read is derived (Shown): READY  *)
(* when not running and the stamp is not the current epoch                 *)
(* (internal/sprint/store/tick.go Machine.Ready). A clear is two steps     *)
(* with no fence between them (internal/sprint/store/clear.go): its stop   *)
(* (ClearStop) and its epoch advance (ClearAdvance); a start may land      *)
(* between them, record the old epoch and run on into the new one.        *)
(* ran is the truth the derived state must agree with: the machine has     *)
(* been RUNNING at some point in the current epoch.                        *)
(*                                                                         *)
(* start moves READY and STOPPED to RUNNING; stop moves RUNNING to STOPPED *)
(* and is refused on a READY machine ("nothing to stop"); the tick's done  *)
(* and funds stops are stops of a RUNNING machine. The read-time judgment  *)
(* machine:stopped (internal/sprint/store/ops.go machineGroups) is raised  *)
(* at a tick's read of a STOPPED machine with moves due, never of a READY  *)
(* one. A record from before StartedIn (the live store at epoch 20) is     *)
(* read off its spans; that reading is not modelled here.                  *)
(*                                                                         *)
(* Broken selects a reversed witness, each red on its own invariant:       *)
(*   "clearstopped"  a clear leaves STOPPED     (ClearLeavesReady)         *)
(*   "readyjudges"   a READY tick judges         (ReadyNeverJudges)        *)
(*   "stopready"     stop accepted in READY      (StoppedOnlyFromRunning)  *)
(*   "nostopstamp"   a stop of a RUNNING machine leaves the stamp as it    *)
(*                   was: a start inside a clear, stopped later, reads     *)
(*                   READY though it ran       (RanIsNotReady)             *)
(*                                                                         *)
(* The working defaults (the owner, 2026-10-13: "you advance epoch, and   *)
(* then set defaults in the new epoch that need to be set"): settings are *)
(* epoch keyed, so ClearAdvance leaves the new epoch with none (absent);  *)
(* the clear's third step, SetDefaults (clear.go setDefaults,             *)
(* sprint.SetDefaults), writes every setting's default into it. A Set by  *)
(* the coordinator moves one away. Two more witnesses:                    *)
(*   "leaveone"      SetDefaults leaves OneSetting absent  (NeverAbsent)   *)
(*   "carryoff"      the advance carries the old epoch's settings and     *)
(*                   SetDefaults writes only the absent ones              *)
(*                   (DefaultsAfterClear)                                 *)
(***************************************************************************)
EXTENDS Integers

CONSTANTS
    MaxEpoch,   \* clears the model takes
    Settings,   \* the work table's properties (sprint.Defaults, HomeProp)
    OneSetting, \* the setting the "leaveone" witness leaves absent
    Broken      \* "none", or the reversed witness above

States == {"READY", "RUNNING", "STOPPED"}
Acts == {"init", "clearstop", "clearadvance", "setdefaults", "set", "start", "stop", "refused", "add", "tick"}
Values == {"absent", "default", "away"}

VARIABLES
    run,        \* the record says RUNNING
    stamp,      \* the last epoch the machine ran in, as recorded; -1 is none
    epoch,      \* the sprint's epoch
    clearing,   \* a clear has stopped the machine and not yet advanced the epoch
    ran,        \* the truth: the machine has been RUNNING in the current epoch
    due,        \* moves are due on the tables
    judged,     \* the read-time machine:stopped judgment, as the last tick read it
    prev,       \* the derived state before the last action
    last,       \* the last action
    settings,   \* the current epoch's settings: absent, default, or away (set)
    defaulting  \* a clear has advanced the epoch and not yet set its defaults

vars == <<run, stamp, epoch, clearing, ran, due, judged, prev, last, settings, defaulting>>

\* the derived state (Machine.Ready, MachineLine)
ShownOf(r, s, e) == IF r THEN "RUNNING" ELSE IF s # e THEN "READY" ELSE "STOPPED"
Shown == ShownOf(run, stamp, epoch)

TypeOK ==
    /\ run \in BOOLEAN
    /\ stamp \in -1..MaxEpoch
    /\ epoch \in 0..MaxEpoch
    /\ clearing \in BOOLEAN
    /\ ran \in BOOLEAN
    /\ due \in BOOLEAN
    /\ judged \in BOOLEAN
    /\ prev \in States
    /\ last \in Acts
    /\ settings \in [Settings -> Values]
    /\ defaulting \in BOOLEAN

Init ==
    /\ run = FALSE
    /\ stamp = -1
    /\ epoch = 0
    /\ clearing = FALSE
    /\ ran = FALSE
    /\ due = FALSE
    /\ judged = FALSE
    /\ prev = "READY"
    /\ last = "init"
    /\ settings = [s \in Settings |-> "absent"]
    /\ defaulting = FALSE

\* the stop's stamp (machine_transition.go): a stop of a RUNNING machine records the
\* epoch it ran in; the reversed witness leaves the stamp as it was
StopStamp == IF Broken = "nostopstamp" THEN stamp ELSE epoch

\* clear's first step (clear.go SetMachine false): stops a RUNNING machine
ClearStop ==
    /\ ~clearing
    /\ ~defaulting
    /\ epoch < MaxEpoch
    /\ clearing' = TRUE
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE stamp
    /\ prev' = Shown
    /\ last' = "clearstop"
    /\ UNCHANGED <<epoch, ran, due, judged, settings, defaulting>>

\* clear's second step (clear.go AdvanceEpoch): the new epoch has no work; a start
\* that landed after ClearStop runs on into it
ClearAdvance ==
    /\ clearing
    /\ clearing' = FALSE
    /\ epoch' = epoch + 1
    /\ stamp' = IF Broken = "clearstopped" THEN epoch + 1 ELSE stamp
    /\ ran' = run
    /\ due' = FALSE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "clearadvance"
    \* settings are epoch keyed: the new epoch holds none; "carryoff" carries them
    /\ settings' = IF Broken = "carryoff" THEN settings ELSE [s \in Settings |-> "absent"]
    /\ defaulting' = TRUE
    /\ UNCHANGED <<run>>

\* start (store.SetMachine true): READY or STOPPED to RUNNING, recording the epoch
\* it reads; it may land between ClearStop and ClearAdvance
Start ==
    /\ ~run
    /\ run' = TRUE
    /\ stamp' = epoch
    /\ ran' = TRUE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "start"
    /\ UNCHANGED <<epoch, clearing, due, settings, defaulting>>

\* stop by hand (store.StopUntil), and the tick's done and funds stops
\* (stopWithCause): RUNNING to STOPPED
Stop ==
    /\ \/ run
       \/ Broken = "stopready" /\ Shown = "READY"
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE epoch
    /\ prev' = Shown
    /\ last' = "stop"
    /\ UNCHANGED <<epoch, clearing, ran, due, judged, settings, defaulting>>

\* a stop of a READY machine is refused and changes nothing
\* (machine_transition.go machineRecord, store.ErrNothingToStop)
StopRefused ==
    /\ Broken # "stopready"
    /\ Shown = "READY"
    /\ prev' = Shown
    /\ last' = "refused"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, defaulting>>

\* work added (add): moves are due
AddWork ==
    /\ due' = TRUE
    /\ prev' = Shown
    /\ last' = "add"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, judged, settings, defaulting>>

\* a tick: a RUNNING machine moves what is due; every tick's read computes the
\* machine judgment (machineGroups)
Tick ==
    /\ due' = (due /\ ~run)
    /\ judged' = (due' /\ (Shown = "STOPPED" \/ (Broken = "readyjudges" /\ Shown = "READY")))
    /\ prev' = Shown
    /\ last' = "tick"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, settings, defaulting>>

\* clear's third step (clear.go setDefaults, sprint.SetDefaults): every setting's
\* default written into the new epoch; "carryoff" writes only the absent ones,
\* "leaveone" leaves OneSetting as it is
SetDefaults ==
    /\ defaulting
    /\ defaulting' = FALSE
    /\ settings' = [s \in Settings |->
                     IF \/ Broken = "leaveone" /\ s = OneSetting
                        \/ Broken = "carryoff" /\ settings[s] # "absent"
                     THEN settings[s] ELSE "default"]
    /\ prev' = Shown
    /\ last' = "setdefaults"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged>>

\* the coordinator's set (sprint.Set): one setting away from its default
Set ==
    /\ \E s \in Settings : settings' = [settings EXCEPT ![s] = "away"]
    /\ prev' = Shown
    /\ last' = "set"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, defaulting>>

Next == ClearStop \/ ClearAdvance \/ SetDefaults \/ Set \/ Start \/ Stop \/ StopRefused \/ AddWork \/ Tick

Spec == Init /\ [][Next]_vars

---------------------------------------------------------------------------
\* Invariants.

\* a clear always leaves READY, unless a start landed inside it (then RUNNING)
ClearLeavesReady == (last = "clearadvance" /\ ~run) => Shown = "READY"

\* READY never raises the stopped judgment
ReadyNeverJudges == Shown = "READY" => ~judged

\* STOPPED is reachable only from RUNNING (a stop of READY is refused)
StoppedOnlyFromRunning == (Shown = "STOPPED" /\ prev # "STOPPED") => prev = "RUNNING"

\* STOPPED keeps its judgment as before
StoppedWithDueJudges == (last = "tick" /\ Shown = "STOPPED" /\ due) => judged

\* a machine that ran in this epoch and is not running is STOPPED, never READY
RanIsNotReady == (ran /\ ~run) => Shown = "STOPPED"

\* after a clear completes, every setting holds its default (until a Set)
DefaultsAfterClear == last = "setdefaults" => \A s \in Settings : settings[s] = "default"

\* once a clear has completed, no setting of the epoch is ever absent
NeverAbsent == (epoch > 0 /\ ~defaulting) => \A s \in Settings : settings[s] # "absent"

=============================================================================
