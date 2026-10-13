---------------------- MODULE MCClearDefaultsRetiredStop ----------------------
EXTENDS ClearDefaults

\* Narrow mirror of stopWithCauseReason / GuardedMachine: a funded STOP
\* reads RUNNING under an operation owner, pauses, loses that owner through
\* expired-empty-lock retirement, then attempts its machine write. Clear
\* may stop/advance/restore after retirement, before the old write returns.
\* STOP checks pinned epoch and current owner identity, not restore phase:
\* unlike START, a STOP is permitted during a pending restore.
\* One deferred writer and one distinct token are enough to expose this
\* seam; no token recycling, funding decision or lock timing is assumed.
VARIABLES stopStage, stopEpoch, stopOwner, machineOwner, staleStopWrote
stopVars == <<stopStage, stopEpoch, stopOwner, machineOwner, staleStopWrote>>
allVars == <<vars, stopVars>>

StopInit == Init /\ stopStage = "idle" /\ stopEpoch = 0
                /\ stopOwner = 0 /\ machineOwner = 0 /\ staleStopWrote = FALSE
StopTypeOK == TypeOK /\ stopStage \in {"idle", "held", "retired"}
             /\ stopEpoch \in 0..MaxEpoch /\ stopOwner \in {0, 1}
             /\ machineOwner \in {0, 1} /\ staleStopWrote \in BOOLEAN

DeferredStopPin ==
    /\ stopStage = "idle" /\ machineOwner = 0 /\ startStage = "idle" /\ run
    /\ stopStage' = "held" /\ stopEpoch' = epoch
    /\ stopOwner' = 1 /\ machineOwner' = 1
    /\ UNCHANGED <<vars, staleStopWrote>>

\* finishOp repairs an expired empty lock by recording Abandoned and
\* releasing its fence. The old writer still exists; no write is implied.
RetireStopOwner ==
    /\ stopStage = "held" /\ machineOwner = stopOwner
    /\ stopStage' = "retired" /\ machineOwner' = 0
    /\ UNCHANGED <<vars, stopEpoch, stopOwner, staleStopWrote>>

EpochAndOwner == stopEpoch = epoch /\ stopOwner # 0 /\ machineOwner = stopOwner
\* The retiredstop witness waits for clear to change epoch, reproducing
\* the observed retire -> clear -> old commit trace. stopowner reverses
\* only ownership, and exposes retirement in the SAME epoch separately.
MayCommit == EpochAndOwner
             \/ (Broken = "retiredstop" /\ stopStage = "retired" /\ stopEpoch # epoch /\ RestorePending)
             \/ (Broken = "stopowner" /\ stopStage = "retired" /\ stopEpoch = epoch)
DeferredStopWrite ==
    /\ stopStage # "idle" /\ MayCommit
    /\ run' = FALSE
    /\ stamp' = IF Broken = "retiredstop" THEN epoch ELSE stopEpoch
    /\ prev' = Shown /\ last' = "stop"
    /\ staleStopWrote' = (staleStopWrote \/ ~EpochAndOwner)
    /\ stopStage' = "idle" /\ stopEpoch' = 0
    /\ stopOwner' = 0 /\ machineOwner' = 0
    /\ UNCHANGED <<epoch, clearing, from, ran, due, judged, lastFinish, settings,
                    setIn, lastRead, src, choice, flag, phase, rows, written,
                    restored, pending, older, markedCur, markedBefore,
                    startStage, startEpoch, startPhase>>

DeferredStopRefuse ==
    /\ stopStage # "idle" /\ ~MayCommit
    /\ stopStage' = "idle" /\ stopEpoch' = 0
    /\ stopOwner' = 0 /\ machineOwner' = 0
    /\ UNCHANGED <<vars, staleStopWrote>>
StopComplete == DeferredStopWrite \/ DeferredStopRefuse

\* A held deferred STOP excludes other machine operations. After its
\* owner retires, the ordinary clear/start model can run; its atomic
\* advance includes the reacquired clear fence and re-STOP. Ordinary
\* actions preserve the deferred writer's observation, not erase it.
StopNext == (machineOwner = 0 /\ Next /\ UNCHANGED stopVars)
            \/ DeferredStopPin \/ RetireStopOwner \/ StopComplete
StopSpec == StopInit /\ [][StopNext]_allVars /\ WF_allVars(StopComplete)
NoRetiredStopWrites == ~staleStopWrote
DeferredStopEventuallyReleases == stopStage # "idle" ~> stopStage = "idle"
=============================================================================
