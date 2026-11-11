------------------------- MODULE HookReplaceUnhooks -------------------------
\* REVERSED WITNESS of Hook.tla: a hook that replaces a stale one returns
\* the row's hand, as unhook then hook would. HandKept must be violated.
(***************************************************************************)
(* The sprint hook, bound to the bus hook it was made on                   *)
(* (modules/verbs/hooked.go hooked and busSession, hook.go hookAs and      *)
(* unhook, next.go look).                                                  *)
(*                                                                         *)
(* nova-bus who shows, for each name whose bus hook is running, a session  *)
(* id: new each time the hook is started, kept across its reconnects.      *)
(* nova-sprint hook records that id on the name's row with the role, and   *)
(* every verb that acts as a name is accepted only while the row's id is   *)
(* the one who shows now. So a name whose bus hook was started since its   *)
(* sprint hook (a new session after a wrap-around, a restarted service, a  *)
(* restarted worker) is refused "hook in first" until it hooks in, and the *)
(* hook, which prints the role's playbook line, is never skipped by a      *)
(* session that forgot.                                                    *)
(*                                                                         *)
(* A hook by a name whose recorded id is the live one is refused (already  *)
(* hooked in). One whose recorded id is stale replaces it in place: the    *)
(* row keeps its hand, and a coordinator its seat, where unhook then hook  *)
(* would return the hand to the queue and free the seat. One coordinator:  *)
(* a hook as coordinator while another name holds the seat is refused.     *)
(* Unhook is the name's own, on the bus; it returns the hand.              *)
(*                                                                         *)
(* The id compared is who's word for the name, nothing about the process   *)
(* that runs the verb: the bus hook may be a service beside the session or *)
(* a worker's child. A bus hook that reconnects keeps its id (no step      *)
(* here); one started again is BusStart.                                   *)
(*                                                                         *)
(* Not here: the deal (the run's; hand is whether the row holds live       *)
(* cards, any value at the start), up --take-over and a coordinator's      *)
(* unhook of another row (the seat's own model is owed, docs/ROADMAP.md),  *)
(* and the store's side of the replacement (Store.tla, HookKept).          *)
(*                                                                         *)
(* Reversed witnesses beside it (run by ci-tlc.sh): HookAnySession, the    *)
(* gate before 2026-11-11 (any recorded hook passes: the incident), and    *)
(* HookReplaceUnhooks (the replacement returns the hand). Trace            *)
(* validation: HookTrace.tla on the walk in modules/verbs/hook_trace_test.go. *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Names,      \* the names that hook in
  MaxStarts   \* model bound on bus hook starts per name

Roles == {"worker", "coordinator"}
None  == "none"

VARIABLES
  live,    \* name -> the session id who shows for its bus hook now; 0 off the bus
  starts,  \* name -> its bus hook's starts so far (the last id given)
  rec,     \* name -> the bus session id its sprint hook recorded; 0 not hooked
  role,    \* name -> its sprint hook's role; None not hooked
  hand,    \* name -> its row holds live cards
  act,     \* the last accepted acting verb: who, and the two ids then (ghost)
  repl     \* the names whose stale hook was replaced with a hand held, until they unhook (ghost)

vars == <<live, starts, rec, role, hand, act, repl>>

NoAct == [name |-> None, rec |-> 0, live |-> 0]

Init ==
  /\ live = [n \in Names |-> 0]
  /\ starts = [n \in Names |-> 0]
  /\ rec = [n \in Names |-> 0]
  /\ role = [n \in Names |-> None]
  /\ hand \in [Names -> BOOLEAN]
  /\ act = NoAct
  /\ repl = {}

\* The bus hook is started (again): who shows a new id for the name.
BusStart(n) ==
  /\ starts[n] < MaxStarts
  /\ starts' = [starts EXCEPT ![n] = @ + 1]
  /\ live' = [live EXCEPT ![n] = starts[n] + 1]
  /\ UNCHANGED <<rec, role, hand, act, repl>>

\* The bus hook ends: the session is gone, or its service stopped.
BusStop(n) ==
  /\ live[n] # 0
  /\ live' = [live EXCEPT ![n] = 0]
  /\ UNCHANGED <<starts, rec, role, hand, act, repl>>

Seat == {m \in Names : role[m] = "coordinator"}

\* nova-sprint hook --as n --role r is accepted: n is on the bus, its
\* recorded id is not the live one (never hooked, or stale), and the seat,
\* when asked for, is free or n's own. A stale hook is replaced in place.
CanHook(n, r) ==
  /\ live[n] # 0
  /\ rec[n] # live[n]
  /\ r = "coordinator" => Seat \subseteq {n}

Hook(n, r, ok) ==
  /\ ok = CanHook(n, r)
  /\ IF ok
     THEN /\ rec' = [rec EXCEPT ![n] = live[n]]
          /\ role' = [role EXCEPT ![n] = r]
          /\ repl' = IF rec[n] # 0 /\ hand[n] THEN repl \cup {n} ELSE repl
     ELSE UNCHANGED <<rec, role, repl>>
  /\ hand' = IF ok /\ rec[n] # 0 THEN [hand EXCEPT ![n] = FALSE] ELSE hand   \* FAULT: the replacement returns the hand
  /\ UNCHANGED <<live, starts, act>>

\* nova-sprint unhook --as n: on the bus, the hook cleared and the hand
\* returned to the queue (nothing to unhook is accepted too, changing
\* nothing); off the bus it is refused.
Unhook(n, ok) ==
  /\ ok = (live[n] # 0)
  /\ IF ok /\ rec[n] # 0
     THEN /\ rec' = [rec EXCEPT ![n] = 0]
          /\ role' = [role EXCEPT ![n] = None]
          /\ hand' = [hand EXCEPT ![n] = FALSE]
          /\ repl' = repl \ {n}
     ELSE UNCHANGED <<rec, role, hand, repl>>
  /\ UNCHANGED <<live, starts, act>>

\* The hooked check: on the bus, the recorded id the live one, and the
\* role the verb needs.
Hooked(n, need) ==
  /\ live[n] # 0
  /\ rec[n] = live[n]
  /\ need = "coordinator" => role[n] = "coordinator"

\* A verb that acts as n, needing role need: accepted or refused.
Verb(n, need, ok) ==
  /\ ok = Hooked(n, need)
  /\ act' = IF ok THEN [name |-> n, rec |-> rec[n], live |-> live[n]] ELSE act
  /\ UNCHANGED <<live, starts, rec, role, hand, repl>>

Next ==
  \E n \in Names :
    \/ BusStart(n) \/ BusStop(n)
    \/ \E ok \in BOOLEAN :
         \/ Unhook(n, ok)
         \/ \E r \in Roles : Hook(n, r, ok) \/ Verb(n, r, ok)

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
TypeOK ==
  /\ live \in [Names -> 0..MaxStarts] /\ starts \in [Names -> 0..MaxStarts]
  /\ rec \in [Names -> 0..MaxStarts]
  /\ role \in [Names -> Roles \cup {None}]
  /\ hand \in [Names -> BOOLEAN]
  /\ act \in [name : Names \cup {None}, rec : 0..MaxStarts, live : 0..MaxStarts]
  /\ repl \subseteq Names

\* No acting verb is accepted from a name whose recorded bus session is not
\* the live one (the incident of 2026-11-11: a new session started only the
\* bus hook, and every coordinator verb passed on the old session's hook).
NoStaleVerb == act.name # None => act.live # 0 /\ act.rec = act.live

\* A row is hooked with a role, or neither; a recorded id was once live.
HookWellFormed == \A n \in Names : (rec[n] = 0) = (role[n] = None) /\ rec[n] <= starts[n]

\* One coordinator.
OneCoordinator == Cardinality(Seat) <= 1

\* The replacement of a stale hook is in place: a name that held cards
\* when its hook was replaced holds them still, hooked, until it unhooks.
HandKept == \A n \in repl : hand[n] /\ rec[n] # 0

\* Reach (expected violated in HookNever*.cfg): a stale hook is replaced
\* with a hand held, and a name acts again after its bus hook was started
\* again.
HookNeverReplaced == repl = {}
HookNeverActsAgain == ~(act.name # None /\ act.live > 1)
=============================================================================
