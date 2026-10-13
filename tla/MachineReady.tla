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
(* sprint.SetDefaults), writes each setting still ABSENT, never one the   *)
(* new epoch holds: a Set that landed between the advance and the step    *)
(* survives. A Carry setting (the coordinator's threshold alarms, the     *)
(* base) takes the value the epoch the clear left held (src), a plain one *)
(* its default. The step marks its epoch; a clear that crashed between    *)
(* its advance and its step leaves the epoch unmarked (defaulting), and a *)
(* clear run again over it finishes the step and advances nothing         *)
(* (clear.go defaultsOwed): ClearStop is not enabled while defaulting,    *)
(* the re-run is SetDefaults itself. Read is a reader of the settings     *)
(* while the machine runs, reading each stored value and an absent one by *)
(* the code's rule: a cleared epoch's absent setting reads its default    *)
(* (defaults.go clearedDefault); the first epoch's absent setting reads   *)
(* its accessor's own, off for the alarms (OffWhenAbsent).                *)
(* Five more witnesses:                                                   *)
(*   "leaveone"      SetDefaults leaves OneSetting absent  (NeverAbsent)   *)
(*   "carryadvance"  the advance carries the old epoch's settings        *)
(*                   (DefaultsAfterClear)                                 *)
(*   "overwrite"     SetDefaults overwrites a Set of the new epoch        *)
(*                   (SetSurvives)                                        *)
(*   "nofallback"    a cleared epoch's absent setting reads as the        *)
(*                   accessor's own, off for an alarm (ReadersSeeWorking) *)
(*   "rerunadvance"  a clear run again over a crashed one advances again, *)
(*                   carrying from the empty epoch (CarrySurvives)        *)
(***************************************************************************)
EXTENDS Integers

CONSTANTS
    MaxEpoch,      \* clears the model takes
    Settings,      \* the work table's properties (sprint.Defaults, HomeProp and HomeCarry)
    Carry,         \* the HomeCarry ones, carried across a clear
    OffWhenAbsent, \* the ones whose own accessor reads absent as off (the alarms)
    OneSetting,    \* the setting the "leaveone" witness leaves absent
    Broken         \* "none", or the reversed witness above

States == {"READY", "RUNNING", "STOPPED"}
Acts == {"init", "clearstop", "clearadvance", "setdefaults", "set", "read", "start", "stop", "refused", "add", "tick"}
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
    defaulting, \* the current epoch is owed its set-defaults step (unmarked)
    setIn,      \* the settings a Set wrote in the current epoch
    lastRead,   \* what the last Read saw: "none", "working", "offfirst" or "off"
    src,        \* the carry values the step carries: the epoch the last advance left
    marked      \* ghost: the carry values the newest epoch that completed its step held at
                \* the advance that left it (never moved by a clear run again)

vars == <<run, stamp, epoch, clearing, ran, due, judged, prev, last, settings, defaulting, setIn, lastRead, src, marked>>

\* the derived state (Machine.Ready, MachineLine)
ShownOf(r, s, e) == IF r THEN "RUNNING" ELSE IF s # e THEN "READY" ELSE "STOPPED"
Shown == ShownOf(run, stamp, epoch)

\* a carried value: the old epoch's, else its working value
Work(v) == IF v = "absent" THEN "default" ELSE v
CarryOf(m) == [c \in Carry |-> m[c]]

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
    /\ setIn \subseteq Settings
    /\ lastRead \in {"none", "working", "offfirst", "off"}
    /\ src \in [Carry -> Values]
    /\ marked \in [Carry -> Values]

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
    /\ setIn = {}
    /\ lastRead = "none"
    /\ src = [c \in Carry |-> "absent"]
    /\ marked = [c \in Carry |-> "absent"]

\* the stop's stamp (machine_transition.go): a stop of a RUNNING machine records the
\* epoch it ran in; the reversed witness leaves the stamp as it was
StopStamp == IF Broken = "nostopstamp" THEN stamp ELSE epoch

\* clear's first step (clear.go SetMachine false): stops a RUNNING machine. Over an epoch
\* owed its step (a crashed clear) the clear finishes the step instead (SetDefaults) and
\* advances nothing; "rerunadvance" advances again
ClearStop ==
    /\ ~clearing
    /\ ~defaulting \/ Broken = "rerunadvance"
    /\ epoch < MaxEpoch
    /\ clearing' = TRUE
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE stamp
    /\ prev' = Shown
    /\ last' = "clearstop"
    /\ UNCHANGED <<epoch, ran, due, judged, settings, defaulting, setIn, lastRead, src, marked>>

\* clear's second step (clear.go AdvanceEpoch): the new epoch has no work and, settings
\* being epoch keyed, no setting; the step will carry from the epoch left (src);
\* "carryadvance" carries every old setting; a start that landed after ClearStop runs on
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
    /\ settings' = IF Broken = "carryadvance" THEN settings ELSE [s \in Settings |-> "absent"]
    /\ src' = CarryOf(settings)
    /\ marked' = IF defaulting THEN marked ELSE CarryOf(settings)
    /\ setIn' = {}
    /\ defaulting' = TRUE
    /\ UNCHANGED <<run, lastRead>>

\* clear's third step (clear.go setDefaults, sprint.SetDefaults), and a clear run again over
\* an epoch owed it: each setting still absent gets a carry value from src or its default;
\* one the epoch holds is kept; the epoch is marked. "overwrite" writes every one;
\* "leaveone" leaves OneSetting as it is
SetDefaults ==
    /\ defaulting
    /\ defaulting' = FALSE
    /\ settings' = [s \in Settings |->
                     IF Broken = "leaveone" /\ s = OneSetting THEN settings[s]
                     ELSE IF settings[s] # "absent" /\ Broken # "overwrite" THEN settings[s]
                     ELSE IF s \in Carry THEN Work(src[s])
                     ELSE "default"]
    /\ prev' = Shown
    /\ last' = "setdefaults"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, setIn, lastRead, src, marked>>

\* the coordinator's set (sprint.Set): one setting away from its default, at any time,
\* between a clear's advance and its step included
Set ==
    /\ \E s \in Settings :
        /\ settings' = [settings EXCEPT ![s] = "away"]
        /\ setIn' = setIn \cup {s}
    /\ prev' = Shown
    /\ last' = "set"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, defaulting, lastRead, src, marked>>

\* the value a reader reads: the stored one, else by the code's rule (clearedDefault): a
\* cleared epoch's default; the first epoch's accessor's own (off for an alarm).
\* "nofallback" reads every absent setting by its accessor's own
Eff(s) == IF settings[s] # "absent" THEN settings[s]
          ELSE IF epoch > 0 /\ Broken # "nofallback" THEN "default"
          ELSE IF s \in OffWhenAbsent THEN "off"
          ELSE "default"

\* the tick's readers of the settings while the machine runs
Read ==
    /\ run
    /\ lastRead' = IF \E s \in Settings : Eff(s) = "off"
                   THEN IF epoch = 0 THEN "offfirst" ELSE "off"
                   ELSE "working"
    /\ prev' = Shown
    /\ last' = "read"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, defaulting, setIn, src, marked>>

\* start (store.SetMachine true): READY or STOPPED to RUNNING, recording the epoch
\* it reads; it may land between ClearStop and ClearAdvance, or after a crashed clear
Start ==
    /\ ~run
    /\ run' = TRUE
    /\ stamp' = epoch
    /\ ran' = TRUE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "start"
    /\ UNCHANGED <<epoch, clearing, due, settings, defaulting, setIn, lastRead, src, marked>>

\* stop by hand (store.StopUntil), and the tick's done and funds stops
\* (stopWithCause): RUNNING to STOPPED
Stop ==
    /\ \/ run
       \/ Broken = "stopready" /\ Shown = "READY"
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE epoch
    /\ prev' = Shown
    /\ last' = "stop"
    /\ UNCHANGED <<epoch, clearing, ran, due, judged, settings, defaulting, setIn, lastRead, src, marked>>

\* a stop of a READY machine is refused and changes nothing
\* (machine_transition.go machineRecord, store.ErrNothingToStop)
StopRefused ==
    /\ Broken # "stopready"
    /\ Shown = "READY"
    /\ prev' = Shown
    /\ last' = "refused"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, defaulting, setIn, lastRead, src, marked>>

\* work added (add): moves are due
AddWork ==
    /\ due' = TRUE
    /\ prev' = Shown
    /\ last' = "add"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, judged, settings, defaulting, setIn, lastRead, src, marked>>

\* a tick: a RUNNING machine moves what is due; every tick's read computes the
\* machine judgment (machineGroups)
Tick ==
    /\ due' = (due /\ ~run)
    /\ judged' = (due' /\ (Shown = "STOPPED" \/ (Broken = "readyjudges" /\ Shown = "READY")))
    /\ prev' = Shown
    /\ last' = "tick"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, settings, defaulting, setIn, lastRead, src, marked>>

Next == ClearStop \/ ClearAdvance \/ SetDefaults \/ Set \/ Read \/ Start \/ Stop \/ StopRefused \/ AddWork \/ Tick

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

\* after a clear's step, every plain (not carried) setting holds its default, but one the
\* new epoch set
DefaultsAfterClear == last = "setdefaults" => \A s \in Settings \ Carry : settings[s] = "default" \/ s \in setIn

\* after a clear's step, every carried setting holds the value of the newest epoch that
\* completed its step (its working value when that held none), but one the new epoch set:
\* a crashed clear run again never loses the coordinator's choice
CarrySurvives == last = "setdefaults" => \A c \in Carry : settings[c] = Work(marked[c]) \/ c \in setIn

\* a Set of the current epoch is never undone by the clear's step
SetSurvives == \A s \in setIn : settings[s] = "away"

\* once a clear has completed, no setting of the epoch is ever absent
NeverAbsent == (epoch > 0 /\ ~defaulting) => \A s \in Settings : settings[s] # "absent"

\* no reader of a cleared epoch reads a setting as off for being absent (before the step:
\* a start inside the clear, a crashed clear); the first epoch's alarms read off until set
ReadersSeeWorking == lastRead # "off"

=============================================================================
