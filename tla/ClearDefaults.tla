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
(* fenced against a concurrent start between them (internal/sprint/store/clear.go): its stop   *)
(* (ClearStop) and its epoch advance (ClearAdvance); a start may land      *)
(* between them; the advance re-stops it under the machine fence.        *)
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
(* owed step is the restore of the members and the readers, row by row    *)
(* (RestoreRow), closed by RestoreDone, which writes the restore's marker *)
(* (restored: the fleet table's members_restored property) in the same    *)
(* store write as the members' control cards; then SetDefaults           *)
(* (clear.go setDefaults, sprint.SetDefaults: each setting still ABSENT,  *)
(* never one the new epoch holds, so a Set that landed between the        *)
(* advance and the step survives; a Carry setting takes the value the     *)
(* epoch the advance left held (src), a plain one its default), then      *)
(* SettleDefaults clears the flag, only after both ran (clear.go          *)
(* owedStep). A crash may stop a clear at any point (Crash). A clear run  *)
(* again over an epoch whose flag names it finishes the step and advances *)
(* nothing (clear.go finishOwed); an epoch without the flag (an older     *)
(* binary's clear, OlderClear; a fresh init; epoch 0) is never owed. Read *)
(* is a reader of the settings while the machine runs, reading each       *)
(* stored value and an absent one by the code's rule: a cleared epoch's   *)
(* absent setting reads its default (defaults.go clearedDefault); the     *)
(* first epoch's absent setting reads its accessor's own, off for the     *)
(* alarms (OffWhenAbsent).                                                *)
(*                                                                         *)
(* Two clears (Procs) run at once, each with its own place in its step    *)
(* (phase, pending, clearing, from): two runs again over one owed epoch,  *)
(* or a run again beside the clear that advanced. Every row write of a    *)
(* restore checks, in the same atomic exchange, that the restore is still *)
(* pending (store.Backend GuardedRows: the epoch, its flag and its marker *)
(* read under WATCH, or under Mem's lock), so a clear that finds the      *)
(* marker written by the other writes nothing (RestoreSettled); every     *)
(* verb's edit or removal of a row checks, in the same exchange, that it  *)
(* is not (EditRow, DeleteRow, Start under the machine lock).             *)
(*                                                                         *)
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
(*   "restorerepeat" the re-run ignores the marker and writes the tiers   *)
(*                   unconditionally, as 02af0680 did (RerunKeepsEpoch-   *)
(*                   Writes)                                              *)
(*   "noguard"       edits and removals land while the restore is pending *)
(*                   (RerunKeepsEpochWrites)                              *)
(*   "nonatomicrestore" the marker is checked once, before the restore,   *)
(*                   and the row writes do not check it again, as at      *)
(*                   90b5ae7d: two clears both find it absent, one        *)
(*                   restores and marks, a row is removed, the other      *)
(*                   restores it again (RerunKeepsEpochWrites)            *)
(***************************************************************************)
EXTENDS Integers

CONSTANTS
    MaxEpoch,      \* clears the model takes
    Settings,      \* the work table's properties (sprint.Defaults, HomeProp and HomeCarry)
    Carry,         \* the HomeCarry ones, carried across a clear
    OffWhenAbsent, \* the ones whose own accessor reads absent as off (the alarms)
    OneSetting,    \* the setting the "leaveone" witness leaves absent
    Rows,          \* the members' and the readers' rows (their control cards and tiers with them)
    Procs,         \* the clears that may run at once
    Broken         \* "none", or the reversed witness above

States == {"READY", "RUNNING", "STOPPED"}
Acts == {"init", "clearstop", "clearadvance", "advancelost", "restorerow", "restoredone", "restoresettled",
         "setdefaults", "settle", "crash", "olderclear", "editrow", "deleterow",
         "set", "read", "start", "startpin", "startrefused", "stop", "refused", "add", "tick"}
Values == {"absent", "default", "away"}
Phases == {"none", "restore", "defaults", "settle"}
\* a row in the current epoch: not yet restored, as restored from the old epoch, edited (a
\* reader's tiers set, a member's hold or width) or removed in this epoch
RowStates == {"none", "old", "edited", "deleted"}

VARIABLES
    run,          \* the record says RUNNING
    stamp,        \* the last epoch the machine ran in, as recorded; -1 is none
    epoch,        \* the sprint's epoch
    clearing,     \* each clear: it has stopped the machine and not yet advanced the epoch
    from,         \* each clear: the epoch it read at its stop (its advance's compare)
    ran,          \* the truth: the machine has been RUNNING in the current epoch
    due,          \* moves are due on the tables
    judged,       \* the read-time machine:stopped judgment, as the last tick read it
    prev,         \* the derived state before the last action
    last,         \* the last action
    lastFinish,   \* the last clearstop went on to an owed step rather than an advance
    settings,     \* the current epoch's settings: absent, default, or away (set)
    setIn,        \* the settings a Set wrote in the current epoch
    lastRead,     \* what the last Read saw: "none", "working", "offfirst" or "off"
    src,          \* the carry values the step carries: the epoch the last advance left
    choice,       \* ghost: the coordinator's carried settings as last chosen (a Set of a Carry
                  \* setting, in any epoch); an older binary's clear carries none and loses them
    flag,         \* the epoch the epoch record's set-defaults owed flag names; -1 is none
    phase,        \* each clear: where it is in its owed step; "none" when in none
    rows,         \* each row of the members and the readers in the current epoch
    written,      \* ghost: each row's last write in the current epoch by a verb ("none": none)
    restored,     \* the restore's marker in the current epoch (members_restored)
    pending,      \* each clear: the rows its running restore has still to write
    older,        \* the current epoch was made by an older binary's clear
    markedCur,    \* ghost: SetDefaults ran in the current epoch (where 14caa8b3 wrote its
                  \* marker, defaults_set)
    startStage,    \* idle or pinned: the concurrent start owns the old-epoch machine fence
    startEpoch,    \* epoch pinned while taking the machine fence
    startPhase,    \* owed flag and restore marker pinned by the start guard
    markedBefore  \* ghost: an earlier epoch holds it

vars == <<run, stamp, epoch, clearing, from, ran, due, judged, prev, last, lastFinish, settings, setIn, lastRead,
          src, choice, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

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

\* the restore is pending: the flag names the epoch and its marker is absent
RestorePending == Owed /\ ~restored

\* no clear is anywhere in a clear
Idle == \A p \in Procs : ~clearing[p] /\ phase[p] = "none"

TypeOK ==
    /\ run \in BOOLEAN
    /\ stamp \in -1..MaxEpoch
    /\ epoch \in 0..MaxEpoch
    /\ clearing \in [Procs -> BOOLEAN]
    /\ from \in [Procs -> 0..MaxEpoch]
    /\ ran \in BOOLEAN
    /\ due \in BOOLEAN
    /\ judged \in BOOLEAN
    /\ prev \in States
    /\ last \in Acts
    /\ lastFinish \in BOOLEAN
    /\ settings \in [Settings -> Values]
    /\ setIn \subseteq Settings
    /\ lastRead \in {"none", "working", "offfirst", "off"}
    /\ src \in [Carry -> Values]
    /\ choice \in [Carry -> Values]
    /\ flag \in -1..MaxEpoch
    /\ phase \in [Procs -> Phases]
    /\ rows \in [Rows -> RowStates]
    /\ written \in [Rows -> {"none", "edited", "deleted"}]
    /\ restored \in BOOLEAN
    /\ pending \in [Procs -> SUBSET Rows]
    /\ older \in BOOLEAN
    /\ markedCur \in BOOLEAN
    /\ markedBefore \in BOOLEAN
    /\ startStage \in {"idle", "pinned"}
    /\ startEpoch \in 0..MaxEpoch
    /\ startPhase \in (-1..MaxEpoch) \X BOOLEAN

Init ==
    /\ run = FALSE
    /\ stamp = -1
    /\ epoch = 0
    /\ clearing = [p \in Procs |-> FALSE]
    /\ from = [p \in Procs |-> 0]
    /\ ran = FALSE
    /\ due = FALSE
    /\ judged = FALSE
    /\ prev = "READY"
    /\ last = "init"
    /\ lastFinish = FALSE
    /\ settings = [s \in Settings |-> "absent"]
    /\ setIn = {}
    /\ lastRead = "none"
    /\ src = [c \in Carry |-> "absent"]
    /\ choice = [c \in Carry |-> "absent"]
    /\ flag = -1
    /\ phase = [p \in Procs |-> "none"]
    /\ rows = [r \in Rows |-> "old"]
    /\ written = [r \in Rows |-> "none"]
    /\ restored = FALSE
    /\ pending = [p \in Procs |-> {}]
    /\ older = FALSE
    /\ markedCur = FALSE
    /\ markedBefore = FALSE
    /\ startStage = "idle"
    /\ startEpoch = 0
    /\ startPhase = <<-1, FALSE>>

\* the stop's stamp (machine_transition.go): a stop of a RUNNING machine records the
\* epoch it ran in; the reversed witness leaves the stamp as it was
StopStamp == IF Broken = "nostopstamp" THEN stamp ELSE epoch

\* a clear's first step (clear.go SetMachine false): stops a RUNNING machine. Over an epoch
\* it reads as owed the clear goes on to the owed step there (finishOwed) and advances
\* nothing; its read of the marker (owedStep's membersRestored) only picks the fast path, the
\* restore's writes check it again. Else it goes on to the advance. "rerunadvance" always
\* advances; "restoreskip" finishes the step without restoreMembers; "restorerepeat"
\* restores whatever the marker says
ClearStop(p) ==
    /\ startStage = "idle" \/ Broken = "startadvance"
    /\ ~clearing[p]
    /\ phase[p] = "none"
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE stamp
    /\ prev' = Shown
    /\ last' = "clearstop"
    /\ IF OwedRead /\ Broken # "rerunadvance"
       THEN /\ clearing' = clearing
            /\ from' = from
            /\ phase' = [phase EXCEPT ![p] = IF Broken = "restoreskip" THEN "defaults"
                                             ELSE IF restored /\ Broken # "restorerepeat" THEN "defaults"
                                             ELSE "restore"]
            /\ pending' = [pending EXCEPT ![p] = Rows]
            /\ lastFinish' = TRUE
       ELSE /\ epoch < MaxEpoch
            /\ clearing' = [clearing EXCEPT ![p] = TRUE]
            /\ from' = [from EXCEPT ![p] = epoch]
            /\ phase' = phase
            /\ pending' = pending
            /\ lastFinish' = FALSE
    /\ UNCHANGED <<epoch, ran, due, judged, settings, setIn, lastRead, src, choice, flag, rows, written, restored, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* a clear's second step (clear.go AdvanceEpoch, a compare on the epoch it read): the new
\* epoch has no work, no setting (settings being epoch keyed) and no members until restored;
\* the same write flags it owed its step (flag); the step will carry from the epoch left
\* (src); "carryadvance" carries every old setting; the fenced advance re-stops a start
\* that landed after ClearStop. The other clear's step, pinned to the epoch left, is refused as stale from now on
ClearAdvance(p) ==
    /\ IF Broken = "startadvance" THEN startStage = "pinned" /\ ~run
       ELSE startStage = "idle"
    /\ clearing[p]
    /\ epoch = from[p]
    /\ clearing' = [clearing EXCEPT ![p] = FALSE]
    /\ epoch' = epoch + 1
    /\ stamp' = IF Broken = "clearstopped" THEN epoch + 1 ELSE stamp
    /\ run' = IF Broken = "startadvance" THEN run ELSE FALSE
    /\ ran' = IF Broken = "startadvance" THEN run ELSE FALSE
    /\ due' = FALSE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "clearadvance"
    /\ settings' = IF Broken = "carryadvance" THEN settings ELSE [s \in Settings |-> "absent"]
    /\ src' = CarryOf(settings)
    /\ setIn' = {}
    /\ flag' = epoch + 1
    /\ phase' = [q \in Procs |-> IF q = p THEN "restore" ELSE "none"]
    /\ rows' = [r \in Rows |-> "none"]
    /\ written' = [r \in Rows |-> "none"]
    /\ restored' = FALSE
    /\ pending' = [pending EXCEPT ![p] = Rows]
    /\ older' = FALSE
    /\ markedBefore' = (markedBefore \/ markedCur)
    /\ markedCur' = FALSE
    /\ UNCHANGED <<from, lastFinish, lastRead, choice, startStage, startEpoch, startPhase>>

\* the other clear advanced first: this one's compare fails and it changes nothing
AdvanceLost(p) ==
    /\ clearing[p]
    /\ epoch # from[p]
    /\ clearing' = [clearing EXCEPT ![p] = FALSE]
    /\ prev' = Shown
    /\ last' = "advancelost"
    /\ UNCHANGED <<run, stamp, epoch, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* a row as the restore leaves it (clear.go restoreMembers): RowsAdd adds a row the epoch
\* lacks, so a removed row comes back, and the old epoch's tiers are written. Only the
\* guard keeps it off a row the epoch wrote: no verb writes a row while the restore is pending
RestoredRow(v) == "old"

\* the restore's row writes (store.Backend GuardedRows with Pending): written only while the
\* restore is pending, checked in the same exchange; "nonatomicrestore" and "restorerepeat"
\* write without the check
RestoreRow(p) ==
    /\ phase[p] = "restore"
    /\ ~restored \/ Broken \in {"nonatomicrestore", "restorerepeat"}
    /\ \E r \in pending[p] :
        /\ rows' = [rows EXCEPT ![r] = RestoredRow(rows[r])]
        /\ pending' = [pending EXCEPT ![p] = pending[p] \ {r}]
    /\ prev' = Shown
    /\ last' = "restorerow"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, written, restored, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* the guarded write finds the marker the other clear wrote: this one writes nothing and goes
\* on to the defaults (GuardSettled)
RestoreSettled(p) ==
    /\ phase[p] = "restore"
    /\ restored
    /\ Broken \notin {"nonatomicrestore", "restorerepeat"}
    /\ phase' = [phase EXCEPT ![p] = "defaults"]
    /\ pending' = [pending EXCEPT ![p] = {}]
    /\ prev' = Shown
    /\ last' = "restoresettled"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, rows, written, restored, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* the restore's last write (the RestoreShape step): the members' control cards and the
\* marker in one store write; a step that finds the marker already written writes nothing
RestoreDone(p) ==
    /\ phase[p] = "restore"
    /\ pending[p] = {}
    /\ restored' = TRUE
    /\ phase' = [phase EXCEPT ![p] = "defaults"]
    /\ prev' = Shown
    /\ last' = "restoredone"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, rows, written, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* a verb edits a row of the epoch (set --tiers, an intentional empty tiers included, a hold,
\* a width): its guard and its write are one exchange (GuardedRows), refused while the
\* restore is pending; "noguard" lets it land
EditRow ==
    /\ ~RestorePending \/ Broken = "noguard"
    /\ \E r \in Rows :
        /\ rows[r] \in {"old", "edited"}
        /\ rows' = [rows EXCEPT ![r] = "edited"]
        /\ written' = [written EXCEPT ![r] = "edited"]
    /\ prev' = Shown
    /\ last' = "editrow"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* a verb removes a row of the epoch (reader remove, the sync's and the tick's drop of a
\* member, a step taking a member's control card off), guarded as EditRow
DeleteRow ==
    /\ ~RestorePending \/ Broken = "noguard"
    /\ \E r \in Rows :
        /\ rows[r] \in {"old", "edited"}
        /\ rows' = [rows EXCEPT ![r] = "deleted"]
        /\ written' = [written EXCEPT ![r] = "deleted"]
    /\ prev' = Shown
    /\ last' = "deleterow"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* the owed step's defaults (clear.go setDefaults, sprint.SetDefaults): each setting still
\* absent gets a carry value from src or its default; one the epoch holds is kept.
\* "overwrite" writes every one; "leaveone" leaves OneSetting as it is. The marker is the
\* 14caa8b3 one, a ghost for "markerinfer"
SetDefaults(p) ==
    /\ phase[p] = "defaults"
    /\ settings' = [s \in Settings |->
                     IF Broken = "leaveone" /\ s = OneSetting THEN settings[s]
                     ELSE IF settings[s] # "absent" /\ Broken # "overwrite" THEN settings[s]
                     ELSE IF s \in Carry THEN Work(src[s])
                     ELSE "default"]
    /\ phase' = [phase EXCEPT ![p] = "settle"]
    /\ markedCur' = TRUE
    /\ prev' = Shown
    /\ last' = "setdefaults"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, setIn, lastRead, src, choice, flag, rows, written, restored, pending, older, markedBefore, startStage, startEpoch, startPhase>>

\* the owed step's last write (clear.go owedStep, SettleDefaults): the flag is cleared, only
\* after both parts ran, and only while it names this epoch
SettleDefaults(p) ==
    /\ phase[p] = "settle"
    /\ flag' = IF flag = epoch THEN -1 ELSE flag
    /\ phase' = [phase EXCEPT ![p] = "none"]
    /\ prev' = Shown
    /\ last' = "settle"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* a clear stops anywhere: between its stop and its advance, or anywhere in its owed step
Crash(p) ==
    /\ clearing[p] \/ phase[p] # "none"
    /\ clearing' = [clearing EXCEPT ![p] = FALSE]
    /\ phase' = [phase EXCEPT ![p] = "none"]
    /\ prev' = Shown
    /\ last' = "crash"
    /\ UNCHANGED <<run, stamp, epoch, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* an older binary's clear (v1.2.16): it stops the machine, advances and restores the
\* members, writing no flag and no defaults; a flag left naming the epoch it leaves stays
\* behind, naming an epoch that is not the current one
OlderClear ==
    /\ startStage = "idle"
    /\ Idle
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
    /\ rows' = [r \in Rows |-> "old"]
    /\ written' = [r \in Rows |-> "none"]
    /\ restored' = FALSE
    /\ older' = TRUE
    /\ markedBefore' = (markedBefore \/ markedCur)
    /\ markedCur' = FALSE
    /\ choice' = [c \in Carry |-> "absent"]
    /\ UNCHANGED <<clearing, from, lastFinish, lastRead, src, flag, phase, pending, startStage, startEpoch, startPhase>>

\* the coordinator's set (sprint.Set): one setting away from its default, at any time,
\* between a clear's advance and its step included
Set ==
    /\ \E s \in Settings :
        /\ settings' = [settings EXCEPT ![s] = "away"]
        /\ setIn' = setIn \cup {s}
        /\ choice' = IF s \in Carry THEN [choice EXCEPT ![s] = "away"] ELSE choice
    /\ prev' = Shown
    /\ last' = "set"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, lastRead, src, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

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
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, src, choice, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* Start is a concurrent process: the guard and the eventual record write are
\* separate actions while the pinned old-epoch operation fence is owned. Clear's
\* advance acquires that same fence, re-stops any intervening committed start,
\* advances atomically, and releases the OLD epoch fence before restoring rows.
\* Completed transactions forget their pins (canonical idle state); this bounds
\* the graph without removing any live interleaving.
\* The fixed write compares epoch, restore phase and ownership; a stale pin
\* refuses rather than restamping the current epoch (GuardedMachine).
\* "startadvance" reverses BOTH protections of the original sequence: advance
\* ignores a held start fence and the start writes without its pinned guard.
StartPin ==
    /\ startStage = "idle"
    /\ ~run
    /\ ~RestorePending
    /\ startStage' = "pinned"
    /\ startEpoch' = epoch
    /\ startPhase' = <<flag, restored>>
    /\ prev' = Shown
    /\ last' = "startpin"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish,
                    settings, setIn, lastRead, src, choice, flag, phase, rows,
                    written, restored, pending, older, markedCur, markedBefore>>

StartGuard == startEpoch = epoch /\ startPhase = <<flag, restored>> /\ ~RestorePending

StartWrite ==
    /\ startStage = "pinned"
    /\ StartGuard \/ Broken = "startadvance"
    /\ startStage' = "idle"
    /\ startEpoch' = 0
    /\ startPhase' = <<-1, FALSE>>
    /\ run' = TRUE
    /\ stamp' = epoch
    /\ ran' = TRUE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "start"
    /\ UNCHANGED <<epoch, clearing, from, due, lastFinish, settings, setIn, lastRead,
                    src, choice, flag, phase, rows, written, restored, pending,
                    older, markedCur, markedBefore>>

StartRefuse ==
    /\ startStage = "pinned"
    /\ ~StartGuard
    /\ Broken # "startadvance"
    /\ startStage' = "idle"
    /\ startEpoch' = 0
    /\ startPhase' = <<-1, FALSE>>
    /\ prev' = Shown
    /\ last' = "startrefused"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish,
                    settings, setIn, lastRead, src, choice, flag, phase, rows,
                    written, restored, pending, older, markedCur, markedBefore>>

StartComplete == StartWrite \/ StartRefuse

\* stop by hand (store.StopUntil), and the tick's done and funds stops
\* (stopWithCause): RUNNING to STOPPED
Stop ==
    /\ startStage = "idle"
    /\ \/ run
       \/ Broken = "stopready" /\ Shown = "READY"
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE epoch
    /\ prev' = Shown
    /\ last' = "stop"
    /\ UNCHANGED <<epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* a stop of a READY machine is refused and changes nothing
\* (machine_transition.go machineRecord, store.ErrNothingToStop)
StopRefused ==
    /\ Broken # "stopready"
    /\ Shown = "READY"
    /\ prev' = Shown
    /\ last' = "refused"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, due, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* work added (add): moves are due
AddWork ==
    /\ due' = TRUE
    /\ prev' = Shown
    /\ last' = "add"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, judged, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

\* a tick: a RUNNING machine moves what is due; every tick's read computes the
\* machine judgment (machineGroups)
Tick ==
    /\ due' = (due /\ ~run)
    /\ judged' = (due' /\ (Shown = "STOPPED" \/ (Broken = "readyjudges" /\ Shown = "READY")))
    /\ prev' = Shown
    /\ last' = "tick"
    /\ UNCHANGED <<run, stamp, epoch, clearing, from, ran, lastFinish, settings, setIn, lastRead, src, choice, flag, phase, rows, written, restored, pending, older, markedCur, markedBefore, startStage, startEpoch, startPhase>>

Next == \/ \E p \in Procs : ClearStop(p) \/ ClearAdvance(p) \/ AdvanceLost(p) \/ RestoreRow(p) \/ RestoreSettled(p)
                            \/ RestoreDone(p) \/ SetDefaults(p) \/ SettleDefaults(p) \/ Crash(p)
        \/ OlderClear \/ EditRow \/ DeleteRow \/ Set \/ Read \/ StartPin \/ StartComplete \/ Stop \/ StopRefused \/ AddWork \/ Tick

Spec == Init /\ [][Next]_vars

\* Only the finite start transaction is weakly fair: once it holds the fence,
\* its write or refusal cannot be postponed forever. Crashes remain arbitrary;
\* no unearned liveness assumption forces a crashed clear to finish restoring.
FairSpec == Spec /\ WF_vars(StartComplete)
StartEventuallyReleases == startStage = "pinned" ~> startStage = "idle"
NeverRunningWhileRestorePending == ~(run /\ RestorePending)

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

\* after a clear's defaults, every plain (not carried) setting holds its default, but one the
\* new epoch set
DefaultsAfterClear == last = "setdefaults" => \A s \in Settings \ Carry : settings[s] = "default" \/ s \in setIn

\* after a clear's defaults, every carried setting holds the coordinator's choice (its working
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
FinishOnlyWhatIsOwed == (last = "clearstop" /\ lastFinish) => Owed

\* the flag is cleared only once the members and the readers are restored: an epoch a clear
\* made and no longer owes holds every row (as restored, edited or removed since)
MembersRestored == ~Owed => \A r \in Rows : rows[r] # "none"

\* no clear, the second of two at once included, ever changes a row the epoch wrote: a
\* reader's tiers set (an empty one included), a row removed, stay as the epoch left them
RerunKeepsEpochWrites == \A r \in Rows : written[r] # "none" => rows[r] = written[r]

\* epoch 0 is never owed
EpochZeroNeverOwed == epoch = 0 => ~Owed

=============================================================================
