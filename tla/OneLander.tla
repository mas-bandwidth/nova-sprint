---------------------------- MODULE OneLander ----------------------------
(***************************************************************************)
(* At most one lander (v1.2.17, start-refuses-a-non-working-setting; cold  *)
(* read 1 of PR 188, F1; the coordinator's ruling (a) and (b)).            *)
(*                                                                         *)
(* Run loops (nova-sprint run) come up on any host. Each writes the        *)
(* store's one server record (store.ServerRecord: its process and whether  *)
(* it lands), which is fresh while its writer is up and expires after it   *)
(* stops. A loop lands when its --land says so; unset, the server          *)
(* (run --listen) lands and a plain run does not (cmd/nova-sprint/run.go   *)
(* landFlag.lands). A land loop starts only when no fresh record of        *)
(* another process says it lands (run.go landerClash), and it then says it *)
(* lands in the record.                                                    *)
(*                                                                         *)
(* StartLoop's check and its record write are one step here. In the code   *)
(* they are two (landerClash reads the record; the loop's next sayServer,  *)
(* within store.ServerEvery, writes it): two loops starting inside one     *)
(* ServerEvery of each other can both pass the check. That window is not   *)
(* modelled; units --check (pkg/units differs) refuses a second landing    *)
(* unit, so the fleet's own units never start two landers.                 *)
(*                                                                         *)
(* Broken selects a reversed witness, each red on its own invariant:       *)
(*   "nofence"       a land loop starts whatever the record says: two      *)
(*                   loops land (AtMostOneLander)                          *)
(*   "plaindefault"  a plain run lands with no --land, as when --land      *)
(*                   defaulted to on for every run (ServerIsTheLander)     *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Procs,      \* the run loops
    Server,     \* the loops run with --listen (the server)
    Broken      \* "none", or the reversed witness above

None == "none"
Flags == {"unset", "on", "off"}

VARIABLES
    up,         \* the loop runs
    flag,       \* its --land: unset, on (--land) or off (--land=false)
    landing,    \* its land loop runs
    record      \* the server record: [p |-> writer or None, land |-> it lands]

vars == <<up, flag, landing, record>>

TypeOK ==
    /\ up \in [Procs -> BOOLEAN]
    /\ flag \in [Procs -> Flags]
    /\ landing \in [Procs -> BOOLEAN]
    /\ record \in [p : Procs \cup {None}, land : BOOLEAN]

Init ==
    /\ up = [p \in Procs |-> FALSE]
    /\ flag = [p \in Procs |-> "unset"]
    /\ landing = [p \in Procs |-> FALSE]
    /\ record = [p |-> None, land |-> FALSE]

\* landFlag.lands: the flag when given, else whether the loop is the server
Wants(p) ==
    CASE flag[p] = "on"  -> TRUE
      [] flag[p] = "off" -> FALSE
      [] OTHER           -> p \in Server \/ Broken = "plaindefault"

\* landerClash: a fresh record of another process that lands
Clash(p) == record.p # None /\ record.p # p /\ record.land

\* a run loop comes up with its --land
Up(p, f) ==
    /\ ~up[p]
    /\ up' = [up EXCEPT ![p] = TRUE]
    /\ flag' = [flag EXCEPT ![p] = f]
    /\ UNCHANGED <<landing, record>>

\* its land loop starts, refused (the run goes on landing nothing) on a clash
StartLoop(p) ==
    /\ up[p]
    /\ ~landing[p]
    /\ Wants(p)
    /\ Broken = "nofence" \/ ~Clash(p)
    /\ landing' = [landing EXCEPT ![p] = TRUE]
    /\ record' = [p |-> p, land |-> TRUE]
    /\ UNCHANGED <<up, flag>>

\* the loop's heartbeat (sayServer): its record, whether it lands. A loop that does not land
\* never writes over another lander's fresh record (run.go sayServer): the record is one key,
\* and a plain run's heartbeat would otherwise erase the fence and let a third loop land.
Say(p) ==
    /\ up[p]
    /\ landing[p] \/ Broken = "nofence" \/ ~Clash(p)
    /\ record' = [p |-> p, land |-> landing[p]]
    /\ UNCHANGED <<up, flag, landing>>

\* the loop stops (its land loop with it, cmdRun's stopLand)
Down(p) ==
    /\ up[p]
    /\ up' = [up EXCEPT ![p] = FALSE]
    /\ landing' = [landing EXCEPT ![p] = FALSE]
    /\ UNCHANGED <<flag, record>>

\* a stopped writer's record expires (store.ServerTTL)
Expire ==
    /\ record.p # None
    /\ ~up[record.p]
    /\ record' = [p |-> None, land |-> FALSE]
    /\ UNCHANGED <<up, flag, landing>>

Next ==
    \/ \E p \in Procs, f \in Flags : Up(p, f)
    \/ \E p \in Procs : StartLoop(p) \/ Say(p) \/ Down(p)
    \/ Expire

Spec == Init /\ [][Next]_vars

---------------------------------------------------------------------------
\* Invariants.

\* at most one land loop runs
AtMostOneLander == Cardinality({p \in Procs : landing[p]}) <= 1

\* only the server lands by default; any other loop lands only on its explicit --land
ServerIsTheLander == \A p \in Procs : landing[p] => (p \in Server \/ flag[p] = "on")

=============================================================================
