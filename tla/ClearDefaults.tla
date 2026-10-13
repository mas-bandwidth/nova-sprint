---------------------------- MODULE ClearDefaults -----------------------------
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
(* epoch keyed, so ClearAdvance leaves the new epoch with none (absent),  *)
(* and the fleet's members and readers unrestored. The advance writes, in *)
(* the same write, the set-defaults owed flag naming the new epoch (flag: *)
(* store.EpochState.DefaultsOwed, the epoch hash's defaults field). The   *)
(* owed step is RestoreMembers (clear.go restoreMembers), then SetDefaults *)
(* (clear.go setDefaults, sprint.SetDefaults: each setting still ABSENT,  *)
(* never one the new epoch holds, so a Set that landed between the        *)
(* advance and the step survives; a Carry setting takes the value the     *)
(* epoch the advance left held (src), a plain one its default), then      *)
(* SettleDefaults clears the flag, only after both ran (clear.go          *)
(* owedStep). A crash may stop the clear at any point (Crash). A clear    *)
(* run again over an epoch whose flag names it finishes the step and      *)
(* advances nothing (clear.go finishOwed); an epoch without the flag (an  *)
(* older binary's clear, OlderClear; a fresh init; epoch 0) is never      *)
(* owed. Read is a reader of the settings while the machine runs, reading *)
(* each stored value and an absent one by the code's rule: a cleared      *)
(* epoch's absent setting reads its default (defaults.go clearedDefault); *)
(* the first epoch's absent setting reads its accessor's own, off for the *)
(* alarms (OffWhenAbsent).                                                *)
(* More witnesses, each red on its own invariant:                         *)
(*   "leaveone"      SetDefaults leaves OneSetting absent  (NeverAbsent)   *)
(*   "carryadvance"  the advance carries the old epoch's settings        *)
(*                   (DefaultsAfterClear)                                 *)
(*   "overwrite"     SetDefaults overwrites a Set of the new epoch        *)
(*                   (SetSurvives)                                        *)
(*   "nofallback"    a cleared epoch's absent setting reads as the        *)
(*                   accessor's own, off for an alarm (ReadersSeeWorking) *)
(*   "rerunadvance"  a clear run again over a crashed first clear         *)
(*                   advances again, carrying from the empty epoch        *)
(*                   (CarrySurvives)                                      *)
(*   "markerinfer"   owed is inferred, as at 14caa8b3, from a missing     *)
(*                   per-epoch marker with a marked epoch before it: an   *)
(*                   older binary's clear between two of this binary's    *)
(*                   is taken for a crash, and the clear advances nothing *)
(*                   (FinishOnlyWhatIsOwed)                               *)
(*   "restoreskip"   the clear run again sets the defaults and clears the *)
(*                   flag without restoreMembers (MembersRestored)        *)
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
Acts == {"init", "clearstop", "clearadvance", "restoremembers", "setdefaults", "settle", "crash", "olderclear",
         "set", "read", "start", "stop", "refused", "add", "tick"}
Values == {"absent", "default", "away"}
Phases == {"none", "restore", "defaults", "settle"}

VARIABLES
    run,          \* the record says RUNNING
    stamp,        \* the last epoch the machine ran in, as recorded; -1 is none
    epoch,        \* the sprint's epoch
    clearing,     \* a clear has stopped the machine and not yet advanced the epoch
    ran,          \* the truth: the machine has been RUNNING in the current epoch
    due,          \* moves are due on the tables
    judged,       \* the read-time machine:stopped judgment, as the last tick read it
    prev,         \* the derived state before the last action
    last,         \* the last action
    settings,     \* the current epoch's settings: absent, default, or away (set)
    setIn,        \* the settings a Set wrote in the current epoch
    lastRead,     \* what the last Read saw: "none", "working", "offfirst" or "off"
    src,          \* the carry values the step carries: the epoch the last advance left
    choice,       \* ghost: the coordinator's carried settings as last chosen (a Set of a Carry
                  \* setting, in any epoch); an older binary's clear carries none and loses them
    flag,         \* the epoch the epoch record's set-defaults owed flag names; -1 is none
    phase,        \* where a clear is in its owed step; "none" when no clear is in one
    members,      \* the current epoch holds the fleet's members and the readers
    older,        \* the current epoch was made by an older binary's clear
    markedCur,    \* ghost: SetDefaults ran in the current epoch (where 14caa8b3 wrote its
                  \* marker, defaults_set)
    markedBefore  \* ghost: an earlier epoch holds it

vars == <<run, stamp, epoch, clearing, ran, due, judged, prev, last, settings, setIn, lastRead, src, choice,
          flag, phase, members, older, markedCur, markedBefore>>

\* the derived state (Machine.Ready, MachineLine)
ShownOf(r, s, e) == IF r THEN "RUNNING" ELSE IF s # e THEN "READY" ELSE "STOPPED"
Shown == ShownOf(run, stamp, epoch)

\* a carried value: the old epoch's, else its working value
Work(v) == IF v = "absent" THEN "default" ELSE v
CarryOf(m) == [c \in Carry |-> m[c]]

\* the epoch record says the current epoch is owed its step (EpochState.DefaultsOwed): the
\* flag names this epoch, and epoch 0 never is (Init writes none)
Owed == flag = epoch

\* what the clear reads as owed: the flag; "markerinfer" infers it from the markers
OwedRead == IF Broken = "markerinfer" THEN epoch > 0 /\ ~markedCur /\ markedBefore ELSE Owed

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
    /\ setIn \subseteq Settings
    /\ lastRead \in {"none", "working", "offfirst", "off"}
    /\ src \in [Carry -> Values]
    /\ choice \in [Carry -> Values]
    /\ flag \in -1..MaxEpoch
    /\ phase \in Phases
    /\ members \in BOOLEAN
    /\ older \in BOOLEAN
    /\ markedCur \in BOOLEAN
    /\ markedBefore \in BOOLEAN

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
    /\ setIn = {}
    /\ lastRead = "none"
    /\ src = [c \in Carry |-> "absent"]
    /\ choice = [c \in Carry |-> "absent"]
    /\ flag = -1
    /\ phase = "none"
    /\ members = TRUE
    /\ older = FALSE
    /\ markedCur = FALSE
    /\ markedBefore = FALSE

\* the stop's stamp (machine_transition.go): a stop of a RUNNING machine records the
\* epoch it ran in; the reversed witness leaves the stamp as it was
StopStamp == IF Broken = "nostopstamp" THEN stamp ELSE epoch

\* clear's first step (clear.go SetMachine false): stops a RUNNING machine. Over an epoch
\* it reads as owed the clear goes on to the owed step there (finishOwed) and advances
\* nothing; else it goes on to the advance. "rerunadvance" always advances; "restoreskip"
\* finishes the step without restoreMembers
ClearStop ==
    /\ ~clearing
    /\ phase = "none"
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE stamp
    /\ prev' = Shown
    /\ last' = "clearstop"
    /\ IF OwedRead /\ Broken # "rerunadvance"
       THEN /\ clearing' = FALSE
            /\ phase' = IF Broken = "restoreskip" THEN "defaults" ELSE "restore"
       ELSE /\ epoch < MaxEpoch
            /\ clearing' = TRUE
            /\ phase' = phase
    /\ UNCHANGED <<epoch, ran, due, judged, settings, setIn, lastRead, src, choice, flag, members, older, markedCur, markedBefore>>

\* clear's second step (clear.go AdvanceEpoch): the new epoch has no work, no setting
\* (settings being epoch keyed) and no members until restored; the same write flags it
\* owed its step (flag); the step will carry from the epoch left (src); "carryadvance"
\* carries every old setting; a start that landed after ClearStop runs on
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
    /\ setIn' = {}
    /\ flag' = epoch + 1
    /\ phase' = "restore"
    /\ members' = FALSE
    /\ older' = FALSE
    /\ markedBefore' = (markedBefore \/ markedCur)
    /\ markedCur' = FALSE
    /\ UNCHANGED <<run, lastRead, choice>>

\* the owed step's first part (clear.go restoreMembers): the members and the readers of the
\* epoch the advance left, written where absent (idempotent)
RestoreMembers ==
    /\ phase = "restore"
    /\ members' = TRUE
    /\ phase' = "defaults"
    /\ prev' = Shown
    /\ last' = "restoremembers"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, setIn, lastRead, src, choice, flag, older, markedCur, markedBefore>>

\* the owed step's second part (clear.go setDefaults, sprint.SetDefaults): each setting still
\* absent gets a carry value from src or its default; one the epoch holds is kept.
\* "overwrite" writes every one; "leaveone" leaves OneSetting as it is. The marker is the
\* 14caa8b3 one, a ghost for "markerinfer"
SetDefaults ==
    /\ phase = "defaults"
    /\ settings' = [s \in Settings |->
                     IF Broken = "leaveone" /\ s = OneSetting THEN settings[s]
                     ELSE IF settings[s] # "absent" /\ Broken # "overwrite" THEN settings[s]
                     ELSE IF s \in Carry THEN Work(src[s])
                     ELSE "default"]
    /\ phase' = "settle"
    /\ markedCur' = TRUE
    /\ prev' = Shown
    /\ last' = "setdefaults"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, setIn, lastRead, src, choice, flag, members, older, markedBefore>>

\* the owed step's last write (clear.go owedStep, SettleDefaults): the flag is cleared, only
\* after both parts ran, and only while it names this epoch
SettleDefaults ==
    /\ phase = "settle"
    /\ flag' = IF flag = epoch THEN -1 ELSE flag
    /\ phase' = "none"
    /\ prev' = Shown
    /\ last' = "settle"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, setIn, lastRead, src, choice, members, older, markedCur, markedBefore>>

\* the clear stops anywhere: between its stop and its advance, or anywhere in its owed step
Crash ==
    /\ clearing \/ phase # "none"
    /\ clearing' = FALSE
    /\ phase' = "none"
    /\ prev' = Shown
    /\ last' = "crash"
    /\ UNCHANGED <<run, stamp, epoch, ran, due, judged, settings, setIn, lastRead, src, choice, flag, members, older, markedCur, markedBefore>>

\* an older binary's clear (v1.2.16): it stops the machine, advances and restores the
\* members, writing no flag and no defaults; a flag left naming the epoch it leaves stays
\* behind, naming an epoch that is not the current one
OlderClear ==
    /\ ~clearing
    /\ phase = "none"
    /\ epoch < MaxEpoch
    /\ epoch' = epoch + 1
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE stamp
    /\ ran' = FALSE
    /\ due' = FALSE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "olderclear"
    /\ settings' = [s \in Settings |-> "absent"]
    /\ setIn' = {}
    /\ members' = TRUE
    /\ older' = TRUE
    /\ markedBefore' = (markedBefore \/ markedCur)
    /\ markedCur' = FALSE
    /\ choice' = [c \in Carry |-> "absent"]
    /\ UNCHANGED <<clearing, lastRead, src, flag, phase>>

\* the coordinator's set (sprint.Set): one setting away from its default, at any time,
\* between a clear's advance and its step included
Set ==
    /\ \E s \in Settings :
        /\ settings' = [settings EXCEPT ![s] = "away"]
        /\ setIn' = setIn \cup {s}
        /\ choice' = IF s \in Carry THEN [choice EXCEPT ![s] = "away"] ELSE choice
    /\ prev' = Shown
    /\ last' = "set"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, lastRead, src, flag, phase, members, older, markedCur, markedBefore>>

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
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, setIn, src, choice, flag, phase, members, older, markedCur, markedBefore>>

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
    /\ UNCHANGED <<epoch, clearing, due, settings, setIn, lastRead, src, choice, flag, phase, members, older, markedCur, markedBefore>>

\* stop by hand (store.StopUntil), and the tick's done and funds stops
\* (stopWithCause): RUNNING to STOPPED
Stop ==
    /\ \/ run
       \/ Broken = "stopready" /\ Shown = "READY"
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE epoch
    /\ prev' = Shown
    /\ last' = "stop"
    /\ UNCHANGED <<epoch, clearing, ran, due, judged, settings, setIn, lastRead, src, choice, flag, phase, members, older, markedCur, markedBefore>>

\* a stop of a READY machine is refused and changes nothing
\* (machine_transition.go machineRecord, store.ErrNothingToStop)
StopRefused ==
    /\ Broken # "stopready"
    /\ Shown = "READY"
    /\ prev' = Shown
    /\ last' = "refused"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, setIn, lastRead, src, choice, flag, phase, members, older, markedCur, markedBefore>>

\* work added (add): moves are due
AddWork ==
    /\ due' = TRUE
    /\ prev' = Shown
    /\ last' = "add"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, judged, settings, setIn, lastRead, src, choice, flag, phase, members, older, markedCur, markedBefore>>

\* a tick: a RUNNING machine moves what is due; every tick's read computes the
\* machine judgment (machineGroups)
Tick ==
    /\ due' = (due /\ ~run)
    /\ judged' = (due' /\ (Shown = "STOPPED" \/ (Broken = "readyjudges" /\ Shown = "READY")))
    /\ prev' = Shown
    /\ last' = "tick"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, settings, setIn, lastRead, src, choice, flag, phase, members, older, markedCur, markedBefore>>

Next == ClearStop \/ ClearAdvance \/ RestoreMembers \/ SetDefaults \/ SettleDefaults \/ Crash \/ OlderClear
        \/ Set \/ Read \/ Start \/ Stop \/ StopRefused \/ AddWork \/ Tick

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

\* after a clear's step, every carried setting holds the coordinator's choice (its working
\* value when none was made, or an older binary's clear lost it), but one the new epoch set:
\* a crashed clear run again never loses the coordinator's choice
CarrySurvives == last = "setdefaults" => \A c \in Carry : settings[c] = Work(choice[c]) \/ c \in setIn

\* a Set of the current epoch is never undone by the clear's step
SetSurvives == \A s \in setIn : settings[s] = "away"

\* once this binary's clear has written its defaults (markedCur: SetDefaults ran in the
\* epoch) or completed its step, no setting of the epoch is ever absent
NeverAbsent == (markedCur \/ (epoch > 0 /\ ~Owed /\ ~older)) => \A s \in Settings : settings[s] # "absent"

\* no reader of a cleared epoch reads a setting as off for being absent (before the step:
\* a start inside the clear, a crashed clear); the first epoch's alarms read off until set
ReadersSeeWorking == lastRead # "off"

\* a clear finishes an owed step, advancing nothing, only over an epoch the flag names: never
\* over an older binary's epoch, a fresh init or epoch 0 (never a success with nothing done)
FinishOnlyWhatIsOwed == (last = "clearstop" /\ phase # "none") => Owed

\* the flag is cleared only once the members and the readers are restored: an epoch a clear
\* made and no longer owes holds them
MembersRestored == ~Owed => members

\* epoch 0 is never owed
EpochZeroNeverOwed == epoch = 0 => ~Owed

=============================================================================
