---------------------------- MODULE MCBenchLease ----------------------------
EXTENDS BenchLease
\* two benches, one of one slot and one of two (a 16-core and a 64-core member
\* at -p 8 would be 2 and 8; 1 and 2 keep the ceiling and the ratio in reach),
\* three workers, HoldFor 2, MaxNow 5, MaxBusy 2
MCMachines == {"m1", "m2"}
MCWorkers == {"w1", "w2", "w3"}
MCSlots == [m \in MCMachines |-> IF m = "m1" THEN 1 ELSE 2]
MCRank == [m \in MCMachines |-> IF m = "m1" THEN 1 ELSE 2]

\* Equal-sized benches make a same-ratio, same-free-slot name tie reachable.
MCTieMachines == {"m1", "m2"}
MCTieWorkers == {"w1", "w2"}
MCTieSlots == [m \in MCTieMachines |-> 2]
MCTieRank == [m \in MCTieMachines |-> IF m = "m1" THEN 1 ELSE 2]
\* liveness: two workers keep the run inside its budget
MCWorkersLive == {"w1", "w2"}
=============================================================================
