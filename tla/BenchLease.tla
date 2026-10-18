----------------------------- MODULE BenchLease -----------------------------
(***************************************************************************)
(* The bench lease (docs/SPEC-SPRINT.md section 18, "The bench lease"):    *)
(* the part of `the-server-schedules-bench-work-not-a-lock` that hands out *)
(* a bench and a slot. `nova-sprint lane take go --machine auto --as <who>`*)
(* asks the sprint server for a bench; the server answers from one record  *)
(* (`lanes:go`), one step at a time, with the allowed bench of the lowest  *)
(* committed / slots that has a slot free, or WAIT when none has. A lease  *)
(* is a placement, never a lock: it never queues and never waits behind    *)
(* another holder. The named-machine lanes stay disabled and unchanged.    *)
(*                                                                         *)
(* The code each action maps to:                                           *)
(*   Grant, Wait (Pick)  internal/sprint/lane.go Lanes.Pick (to be written *)
(*                       by v17-bench-lease-pick), run as one step on the  *)
(*                       record by internal/sprint/store/lane.go's pick    *)
(*                       step (v17-bench-lease-take-auto);                 *)
(*   Renew               the same take line by a worker that holds a lease *)
(*                       (Lanes.Pick's renewal, Seen = now);               *)
(*   Give                `lane give go --machine auto`, Lanes.Give;        *)
(*   Expire              Lane.expire (internal/sprint/lane.go): applied at *)
(*                       the start of every step that reads the record and *)
(*                       by Lanes.Expire, a lease not seen for LaneHoldFor *)
(*                       (20m) by the store's clock;                       *)
(*   Die                 a worker killed or lost: it never gives back;     *)
(*   Tick                the store's clock;                                *)
(*   Load, Allow,        the fleet table: a member's beat (Load, Cores),   *)
(*   Disallow            fleet sync from nova-config's machine rows, holds, *)
(*                       quiet windows, NoRoom, width, bench.CheckHost.    *)
(*                                                                         *)
(* What it abstracts. Cores and the busy percent are folded into slots:    *)
(* Slots[m] is max(1, Cores / 8) and busy[m] is ceil(Load / 100 * Cores /  *)
(* 8), both in slots. Every reason a member is not a bench (status not up, *)
(* held, quiet, NoRoom, width 0, the server's own machine, CheckHost, a    *)
(* stale beat, Cores 0) is folded into `allowed`. Time is the store's      *)
(* clock in whole steps; seen is the last renewal; HoldFor is LaneHoldFor. *)
(* The time abstraction: now runs 0..MaxNow, and a Tick at MaxNow shifts   *)
(* every seen down by one instead, so each age (now - seen) is exact up to *)
(* MaxNow and saturates there; MaxNow > HoldFor, so a saturated age is an  *)
(* expired one and the state space stays finite. The retry time of a WAIT *)
(* (the earliest live deadline, at most LaneWaitFor away) is not modelled.  *)
(* The D4 name tie-break is an explicit rank and is checked by              *)
(* GrantTieBreak on an equal-slot instance, where the tie is reachable.     *)
(*                                                                         *)
(* lease[w] is the SET of benches w holds, not one bench or none: the code *)
(* keeps holders per machine (Lanes is machine -> holders), so nothing in  *)
(* the record stops a worker appearing under two machines, and OneLeaseEach *)
(* is a property the pick must keep, not a fact of a type.                 *)
(*                                                                         *)
(* waiting is a ghost: the workers whose last answer was WAIT and that     *)
(* still want a bench. It is not in the record (a wait writes nothing).    *)
(*                                                                         *)
(* The design is Broken = "none". Each other value is a reversed witness:  *)
(*   "noexpiry"   Expire never fires and no step drops an expired lease:   *)
(*                the lease is the client's to give, with no server        *)
(*                deadline (the stuck lock Glenn banned; 2026-10-18: "if   *)
(*                it is client/server" it is allowed): DeadLeaseEnds fails *)
(*                (MCBenchLeaseBrokenNoExpiry.cfg);                        *)
(*   "loadonly"   committed is busy alone, as an uptime-only pick is, so   *)
(*                leases not yet showing as load are not counted:          *)
(*                NeverOverSlots fails (MCBenchLeaseBrokenLoadOnly.cfg);   *)
(*   "noceiling"  a full fleet still grants the least committed bench:     *)
(*                NeverOverSlots fails (MCBenchLeaseBrokenNoCeiling.cfg);  *)
(*   "queue"      a wait records a place (the time it asked) in the        *)
(*                record: WaitWritesNothing fails                          *)
(*                (MCBenchLeaseBrokenQueue.cfg);                           *)
(*   "anybench"   the pick considers every machine, allowed or not:        *)
(*                GrantOnlyAllowed fails;                                  *)
(*   "anyroom"    the pick takes any bench with room, not the least        *)
(*                committed: GrantIsLeastCommitted fails;                  *)
(*   "second"     a worker that holds a lease and asks again is granted a  *)
(*                second one instead of a renewal: OneLeaseEach fails;     *)
(*   "waitwhenroom" a take may answer WAIT while a bench has room:         *)
(*                WaitOnlyWhenFull fails, and with it WaiterGetsLease;     *)
(*   "renewexpired" a renewal neither drops expired leases nor refuses its *)
(*                own expired one: NoLeaseOutlivesDeadline fails;           *)
(*   "anytie"     chooses freely among least-committed ties: GrantTieBreak  *)
(*                fails (MCBenchLeaseBrokenAnyTie.cfg).                    *)
(* Each reversed witness has a committed configuration and CASES.tsv row.  *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets, TLC

CONSTANTS Machines, Workers, Slots, NameRank, HoldFor, MaxNow, MaxBusy, Broken

Faults == {"none", "noexpiry", "loadonly", "noceiling", "queue", "anybench", "anyroom", "second",
           "waitwhenroom", "renewexpired", "anytie"}
NoMachine == "none"
NoWorker  == "none"
ASSUME Broken \in Faults
ASSUME Slots \in [Machines -> 1..2]
ASSUME NameRank \in [Machines -> 1..Cardinality(Machines)]
ASSUME \A m1, m2 \in Machines : m1 # m2 => NameRank[m1] # NameRank[m2]
ASSUME MaxNow > HoldFor /\ HoldFor > 0
ASSUME NoMachine \notin Machines /\ NoWorker \notin Workers

VARIABLES now,     \* the store's clock
          lease,   \* each worker: the benches it holds (one under the design)
          seen,    \* each worker: when it last took or renewed (0 with no lease)
          busy,    \* each machine: its outside load, in slots (the beat)
          allowed, \* the machines the fleet table makes benches now
          alive,   \* the workers still running
          waiting, \* ghost: workers answered WAIT that still want a bench
          last     \* the last answer: [w, kind, m]

vars == <<now, lease, seen, busy, allowed, alive, waiting, last>>

Kinds == {"granted", "renewed", "wait", "none"}
NoAnswer == [w |-> NoWorker, kind |-> "none", m |-> NoMachine]

Max(a, b) == IF a >= b THEN a ELSE b

\* a lease not renewed within HoldFor is past its deadline
Expired(v) == lease[v] # {} /\ now - seen[v] > HoldFor

\* the record as every step reads it: expired leases dropped first
Dropped == [v \in Workers |->
              IF Broken # "noexpiry" /\ Expired(v) THEN {} ELSE lease[v]]

\* seen after a write: a worker with no lease keeps no time
SeenAfter(L) == [v \in Workers |-> IF L[v] = {} THEN 0 ELSE seen[v]]

\* the leases held on m in the record L
HeldIn(L, m) == Cardinality({v \in Workers : m \in L[v]})

\* D3: committed slots are max(held, busy), so a lease counts before its load
\* shows and outside load counts too
DesignCommitted(L, m) == Max(HeldIn(L, m), busy[m])
Committed(L, m) == IF Broken = "loadonly" THEN busy[m] ELSE DesignCommitted(L, m)

RoomIn(L, m) == Committed(L, m) + 1 <= Slots[m]
DesignRoomIn(L, m) == DesignCommitted(L, m) + 1 <= Slots[m]

\* D2, D4: the benches a pick may grant
Cands(L) == IF Broken = "anybench"  THEN {m \in Machines : RoomIn(L, m)}
            ELSE IF Broken = "noceiling" THEN allowed
            ELSE {m \in allowed : RoomIn(L, m)}

\* D4: the lowest committed / slots, compared without division
Least(L, C) == IF Broken = "anyroom" THEN C
               ELSE {m \in C : \A m2 \in C :
                       Committed(L, m) * Slots[m2] <= Committed(L, m2) * Slots[m]}

\* D4: a tie goes to the most free slots, then the lower machine name
Best(L, C) == LET Lc == Least(L, C) IN
              IF Broken = "anyroom" \/ Broken = "anytie" THEN Lc
              ELSE {m \in Lc : \A m2 \in Lc :
                      /\ Slots[m] + Committed(L, m2) >= Slots[m2] + Committed(L, m)
                      /\ (Slots[m] + Committed(L, m2) > Slots[m2] + Committed(L, m)
                          \/ NameRank[m] <= NameRank[m2])}

TypeOK ==
  /\ now \in 0..MaxNow
  /\ lease \in [Workers -> SUBSET Machines]
  /\ seen \in [Workers -> 0..MaxNow]
  /\ busy \in [Machines -> 0..MaxBusy]
  /\ allowed \in SUBSET Machines
  /\ alive \in SUBSET Workers
  /\ waiting \in SUBSET Workers
  /\ last \in [w : Workers \cup {NoWorker}, kind : Kinds, m : Machines \cup {NoMachine}]

Init ==
  /\ now = 0
  /\ lease = [w \in Workers |-> {}]
  /\ seen = [w \in Workers |-> 0]
  /\ busy = [m \in Machines |-> 0]
  /\ allowed = Machines
  /\ alive = Workers
  /\ waiting = {}
  /\ last = NoAnswer

\* D4, D7: a worker with no live lease asks and a bench has room: one step
\* drops the expired leases, picks and writes the lease
Grant(w) ==
  /\ w \in alive
  /\ Dropped[w] = {} \/ Broken = "second"
  /\ LET L == Dropped
         C == Cands(L)
     IN /\ C # {}
        /\ \E m \in Best(L, C) :
             /\ lease' = [L EXCEPT ![w] = @ \cup {m}]
             /\ seen' = [SeenAfter(L) EXCEPT ![w] = now]
             /\ last' = [w |-> w, kind |-> "granted", m |-> m]
  /\ waiting' = waiting \ {w}
  /\ UNCHANGED <<now, busy, allowed, alive>>

\* D6: no allowed bench has room: WAIT, and nothing is written (no queue, no
\* place; the expired leases the answer counted as gone stay for the next write)
Wait(w) ==
  /\ w \in alive
  /\ Dropped[w] = {}
  /\ Cands(Dropped) = {} \/ Broken = "waitwhenroom"
  /\ lease' = lease
  /\ seen' = IF Broken = "queue" THEN [seen EXCEPT ![w] = now] ELSE seen
  /\ last' = [w |-> w, kind |-> "wait", m |-> NoMachine]
  /\ waiting' = waiting \cup {w}
  /\ UNCHANGED <<now, busy, allowed, alive>>

\* the take line, one step: a grant or a wait
Pick(w) == Grant(w) \/ Wait(w)

\* D4: a worker that holds a live lease asks again: its own bench, renewed
Renew(w) ==
  /\ w \in alive
  /\ IF Broken = "renewexpired"
       THEN /\ lease[w] # {}
            /\ lease' = lease
            /\ seen' = [seen EXCEPT ![w] = now]
       ELSE /\ Dropped[w] # {}
            /\ lease' = Dropped
            /\ seen' = [SeenAfter(Dropped) EXCEPT ![w] = now]
  /\ last' = [w |-> w, kind |-> "renewed", m |-> CHOOSE m \in lease[w] : TRUE]
  /\ UNCHANGED <<now, busy, allowed, alive, waiting>>

\* D5: a give releases the lease at once
Give(w) ==
  /\ w \in alive
  /\ lease[w] # {}
  /\ lease' = [Dropped EXCEPT ![w] = {}]
  /\ seen' = SeenAfter([Dropped EXCEPT ![w] = {}])
  /\ last' = NoAnswer
  /\ UNCHANGED <<now, busy, allowed, alive, waiting>>

\* a worker is killed or lost; its lease stays in the record
Die(w) ==
  /\ w \in alive
  /\ alive' = alive \ {w}
  /\ waiting' = waiting \ {w}
  /\ last' = NoAnswer
  /\ UNCHANGED <<now, lease, seen, busy, allowed>>

\* D5: a lease past its deadline is dropped (in the code, at every step)
Expire ==
  /\ Broken # "noexpiry"
  /\ \E v \in Workers : Expired(v)
  /\ lease' = Dropped
  /\ seen' = SeenAfter(Dropped)
  /\ last' = NoAnswer
  /\ UNCHANGED <<now, busy, allowed, alive, waiting>>

\* the store's clock, with the time abstraction at MaxNow
Tick ==
  /\ IF now < MaxNow
       THEN /\ now' = now + 1
            /\ seen' = seen
       ELSE /\ now' = now
            /\ seen' = [v \in Workers |-> IF seen[v] > 0 THEN seen[v] - 1 ELSE 0]
  /\ last' = NoAnswer
  /\ UNCHANGED <<lease, busy, allowed, alive, waiting>>

\* a beat changes a machine's outside load
Load(m, b) ==
  /\ busy[m] # b
  /\ busy' = [busy EXCEPT ![m] = b]
  /\ last' = NoAnswer
  /\ UNCHANGED <<now, lease, seen, allowed, alive, waiting>>

\* D2, D5: the fleet table lets a machine in, or keeps it out (a hold, down,
\* quiet, NoRoom, width 0); a lease on a machine kept out stays until it is
\* given or expires
Allow(m) ==
  /\ m \notin allowed
  /\ allowed' = allowed \cup {m}
  /\ last' = NoAnswer
  /\ UNCHANGED <<now, lease, seen, busy, alive, waiting>>

Disallow(m) ==
  /\ m \in allowed
  /\ allowed' = allowed \ {m}
  /\ last' = NoAnswer
  /\ UNCHANGED <<now, lease, seen, busy, alive, waiting>>

Next ==
  \/ \E w \in Workers : Pick(w) \/ Renew(w) \/ Give(w) \/ Die(w)
  \/ Expire
  \/ Tick
  \/ \E m \in Machines, b \in 0..MaxBusy : Load(m, b)
  \/ \E m \in Machines : Allow(m) \/ Disallow(m)

\* a waiting worker keeps asking (lane take again at its retry time); the answer
\* is the server's, a grant or another wait
Ask(w) == w \in waiting /\ Pick(w)

Fairness ==
  /\ WF_vars(Tick)
  /\ WF_vars(Expire)
  /\ \A w \in Workers : WF_vars(Ask(w))

Spec == Init /\ [][Next]_vars /\ Fairness

---------------------------------------------------------------------------
\* Safety.

\* D3, D4, D7: no bench ever holds more leases than its slots
NeverOverSlots == \A m \in Machines : HeldIn(lease, m) <= Slots[m]

\* D4: a worker holds at most one lease; asking again renews it
OneLeaseEach == \A w \in Workers : Cardinality(lease[w]) <= 1

\* D2: a grant is only ever of an allowed bench
GrantOnlyAllowed ==
  [][last'.kind = "granted" => last'.m \in allowed]_vars

\* D3, D4: a grant is of the allowed bench with the lowest committed / slots
\* among those with room, by the design's committed, on the record as read
GrantIsLeastCommitted ==
  [][last'.kind = "granted" =>
       \A m2 \in allowed :
         DesignRoomIn(Dropped, m2) =>
           DesignCommitted(Dropped, last'.m) * Slots[m2]
             <= DesignCommitted(Dropped, m2) * Slots[last'.m]]_vars

\* D4: among equal-ratio benches with room, grant the most free slots, then
\* the lowest machine name
GrantTieBreak ==
  [][last'.kind = "granted" =>
       \A m2 \in allowed :
         (DesignRoomIn(Dropped, m2) /\
          DesignCommitted(Dropped, last'.m) * Slots[m2] =
            DesignCommitted(Dropped, m2) * Slots[last'.m]) =>
           /\ Slots[last'.m] + DesignCommitted(Dropped, m2)
                >= Slots[m2] + DesignCommitted(Dropped, last'.m)
           /\ (Slots[last'.m] + DesignCommitted(Dropped, m2) =
                  Slots[m2] + DesignCommitted(Dropped, last'.m) =>
                  NameRank[last'.m] <= NameRank[m2])]_vars

\* D6: a wait is answered only when no allowed bench has room
WaitOnlyWhenFull ==
  [][last'.kind = "wait" => \A m \in allowed : ~DesignRoomIn(Dropped, m)]_vars

\* D5: no step that writes the record leaves a lease past its deadline in it,
\* and a renewal is never of a lease already past its deadline
NoLeaseOutlivesDeadline ==
  [][last'.kind \in {"granted", "renewed"} =>
       /\ \A v \in Workers : lease'[v] # {} => now' - seen'[v] <= HoldFor
       /\ (last'.kind = "renewed" => ~Expired(last'.w))]_vars

\* D6: a wait writes nothing: no queue, no place
WaitWritesNothing ==
  [][last'.kind = "wait" => UNCHANGED <<lease, seen>>]_vars

---------------------------------------------------------------------------
\* Liveness.

\* D5: a dead worker never holds a slot past its deadline: its lease ends
DeadLeaseEnds ==
  \A w \in Workers : (w \notin alive /\ lease[w] # {}) ~> (lease[w] = {})

\* some allowed bench has a slot free, on the record as read
RoomNow == \E m \in allowed : DesignRoomIn(Dropped, m)

\* D6: when capacity frees and stays free, a waiting worker that keeps asking
\* is granted a lease. The fairness is only that it keeps asking (WF on Ask:
\* the take answers, grant or wait); the grant comes from the pick never
\* answering WAIT while a bench has room. Capacity that frees only now and
\* then is not enough: with no queue, other askers can take it first.
WaiterGetsLease ==
  \A w \in Workers : (<>[]RoomNow) => ((w \in waiting) ~> (w \notin waiting))

=============================================================================
