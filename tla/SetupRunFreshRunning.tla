------------------------ MODULE SetupRunFreshRunning ------------------------
\* REVERSED WITNESS (made from SetupRun.tla by one replacement): a fresh store
\* begins running, the defect of 2026-11-07. Must violate NothingMovesUnstarted.
EXTENDS Setup

(***************************************************************************
 up's walk (Setup.tla, unchanged) with the sprint's stop and the run's
 ticks beside it (Glenn, 2026-11-07: the run ticked, dealt and pushed while
 up was at step 7 of 8, and stop refused until step 8; "seems bad").

 Step 2's action makes the store, once. A fresh store is stopped, its
 reason "never started" (FreshSprint; store.FreshStop, written by Migrate).
 Step 4's action starts the run service, which ticks from then on whatever
 step the walk is at, the walk unfinished included: a run can begin before
 setup completes. A tick moves something only while the sprint runs
 (run.Ticker.Tick returns before steps.Tick when its copy is stopped;
 Run.tla NoTickWhileStopped). The coordinator's start and stop need only a
 store with an epoch, at any step of the walk (verbs upNeed, LAYER-6
 section 3); they are not fair: a sprint never started is allowed.

 NothingMovesUnstarted: nothing moves before the coordinator's first
 start. SetupRunFreshRunning makes the fresh store running and violates
 it. The reach checks (each must be violated) show the run up on a fresh
 store mid-walk, a stop mid-walk, and work moving mid-walk after a start.
***************************************************************************)
VARIABLES sprint, started, moved
upVars == <<sprint, started, moved>>
allVars == <<vars, sprint, started, moved>>

SprintStates == {"none", "never started", "running", "stopped"}
FreshSprint == "running"   \* FAULT: a fresh store runs

UInit == Init /\ sprint = "none" /\ started = FALSE /\ moved = FALSE

\* Step 2's action makes the store once; a later repair of step 2 keeps it.
MakeStore == IF pc = 2 /\ sprint = "none" THEN sprint' = FreshSprint ELSE UNCHANGED sprint
UDo == Do /\ MakeStore /\ UNCHANGED <<started, moved>>
UProgress == UDo \/ ((Restart \/ Probe \/ (\E q \in Questions : Ask(q))) /\ UNCHANGED upVars)
Disrupt == (Crash \/ (\E k \in Steps : Regress(k))) /\ UNCHANGED upVars

\* The run service (up from step 4's action) ticks whatever the walk's step.
Tick ==
  /\ world[4] /\ sprint # "none"
  /\ moved' = (moved \/ sprint = "running")
  /\ UNCHANGED <<vars, sprint, started>>
\* The coordinator's verbs: any store with an epoch, any step of the walk.
Start ==
  /\ sprint \in {"never started", "stopped"}
  /\ sprint' = "running" /\ started' = TRUE
  /\ UNCHANGED <<vars, moved>>
Stop ==
  /\ sprint = "running" /\ sprint' = "stopped"
  /\ UNCHANGED <<vars, started, moved>>

UNext == UProgress \/ Disrupt \/ Tick \/ Start \/ Stop
USpec == UInit /\ [][UNext]_allVars /\ WF_allVars(UProgress)

UTypeOK == TypeOK /\ sprint \in SprintStates /\ started \in BOOLEAN /\ moved \in BOOLEAN
\* The store exists exactly once step 2's action has run.
StoreFromStepTwo == (sprint = "none") => ~world[2]
NothingMovesUnstarted == moved => started

\* Reachability: each is expected to be VIOLATED.
NeverRunUpFreshMidWalk == ~(world[4] /\ sprint = FreshSprint /\ phase # "done")
NeverStoppedMidWalk    == ~(sprint = "stopped" /\ phase # "done")
NeverMovedMidWalk      == ~(moved /\ phase # "done")
=============================================================================
