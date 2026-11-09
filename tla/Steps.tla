------------------------------- MODULE Steps -------------------------------
(***************************************************************************)
(* Layer 3 of nova-sprint v2: the steps (docs/LAYER-3-STEPS.md), the tick  *)
(* as one atomic action.                                                   *)
(*                                                                         *)
(* The store w is the truth: producers, consumer slots, rows (up or down), *)
(* each stream's hold, and the deal order of working consumers (the        *)
(* youngest is the last dealt). Snap copies it to snap, the view; between  *)
(* Snap and Tick the rows and the coordinator move the store (Report, the *)
(* stream's hold, a row's Gone and Seen), so a tick plans on a view       *)
(* that may be stale. Tick plans every step from snap alone, takes the    *)
(* policy answers from an oracle (a), orders the groups, keeps the        *)
(* budget, packs groups into ops of up to Pack, and applies the ops in     *)
(* order: each op is accepted whole, fenced on every record its groups    *)
(* read (against the view with the tick's accepted ops applied, so a     *)
(* tick never races itself), or refused whole. A refused op's groups go   *)
(* into split, and the next tick packs any group touching their cards as  *)
(* an op of its own. Judgements are written by the tick directly (not    *)
(* fenced, not counted in the budget).                                    *)
(*                                                                         *)
(* The deal is the take (defect 52, intent rule 33): Start and AskRead    *)
(* cut their consumer working, in its row's hand, and a row is dealt only *)
(* while its working consumers are under its width (there is no ahead).  *)
(* The row's take is an acknowledgement (a cursor) that moves nothing the *)
(* steps read, so it is not an action here; the nudges it stops are       *)
(* layer 6's.                                                             *)
(*                                                                         *)
(* The undealt judgement (directive 65): rdy[p] counts the ticks in a row *)
(* that left producer p ready with room on an open serving row (RoomIn,   *)
(* in the view); at UndealtTicks the tick opens the judgement (ju), which *)
(* expires when p is not ready. DealtWithinKTicks: a ready producer with  *)
(* room is dealt within UndealtTicks ticks or its judgement is open. The  *)
(* code counts the same in seconds (UndealtAfter, one tick a second), so  *)
(* ju is not in the trace records (StepsTrace leaves it to the model).    *)
(*                                                                         *)
(* Inputs (LAYER-6's reading, which the build rules make win): reports    *)
(* are the rows' own store ops (Report), already in the                   *)
(* store when the tick folds; the inbox holds edge results only: a        *)
(* landing outcome and a runner's ack of a stop. A landing outcome is     *)
(* landed (the one automatic move) or red, which stands for every other   *)
(* outcome (red CI, no CI, a conflict, a refused merge, an unopenable PR, *)
(* a branch not created, a fault): one land judgement per card on the    *)
(* coordinator's row, nothing moved (intent rule 32). Answer is the      *)
(* coordinator's: land (merged by hand), fix (Red: to fix, review + 1)   *)
(* or retry (a new landing). The request picks one producer per stream  *)
(* (rung 1); rung 2's picking of every eligible one at backed_up is      *)
(* tested in Go (TestTickLadderRungs) and its landing modelled in        *)
(* LandEdge.tla; the trace walk answers backed_up above the scenario's   *)
(* producers, so its requests are rung 1's. EdgeResult delivers one, or the same twice (layer 4 may          *)
(* redeliver). Edge requests are records [id, stream, sent]: id is        *)
(* <<epoch, kind, card, sha>>; a stop request rides in its preemption     *)
(* group and is dispatched only if that group's op was accepted (its      *)
(* After); a land request has an empty After. A request stays in flight   *)
(* until a tick consumes its result.                                      *)
(*                                                                         *)
(* The per-card moves are layer 2's (CardMachine.tla): each guard (Legal) *)
(* and write (ApplyG) is the CardMachine action of the same name at the   *)
(* grain the steps read. CardMachine's own operators cannot be INSTANCEd  *)
(* here: its card sets are fixed by its Scenario constant (Prods is       *)
(* {p1} or {p1, p2}), and preemption of the youngest of two working       *)
(* consumers needs a third producer. Heads are attempt numbers (an ok     *)
(* report of attempt n pushes head n), so a head never needs choosing.    *)
(*                                                                         *)
(* The preemption's victim is the youngest working consumer on the row,  *)
(* the engine's fixed rule (LAYER-5's boundary, LESSONS 5.12); policy      *)
(* answers rows alone.                                                     *)
(* Scenarios (one module, each small enough to check whole):              *)
(* - "pre": b a blocker in stream s2, held at the start; p and q in s1    *)
(*   with disjoint paths; r1 down, r2 width 2. p and q are dealt to r2;   *)
(*   unholding s2 makes b ready with no free width: preemption; r2 going  *)
(*   down leaves b unserved.                                              *)
(* - "deal": p and q in s1, both free to start (only a NEEDS orders       *)
(*   starts); b (tier 2) in s2 needs p. Escalation by row rank, reads, a  *)
(*   fix, a landing and the need it releases. Only p bounces (fail,       *)
(*   fix). A store op may land p by hand between ticks (no land step, no  *)
(*   edge result): b resolves from the state all the same                 *)
(*   (ResolveFromState).                                                  *)
(* - "land": p (s1) and q (s2, after p) both merging: AfterKept,          *)
(*   AfterNeverHolds, landed and judged results, the coordinator's land,  *)
(*   fix and retry, the read after a fix, a hold on s1 that landing      *)
(*   ignores.                                                             *)
(* - "burst": land with budget 1, pack 1 and redelivered results: the    *)
(*   carry.                                                               *)
(* Landed means on main (LANDING.md, 2026-11-08): in land and burst s2's *)
(* base is a workstream branch, s1's is main (MainBase). A landed result *)
(* of a land request on s2 records the base merge (w.ob, the reason's     *)
(* "on base" record; the producer stays merging) and the tick asks a     *)
(* promote request for it (one per stream, the first on base, as rung 1; *)
(* the code asks one for all of a stream's, and the scenarios have one). *)
(* A landed promote result is its Landed. onmain is git's main: the       *)
(* producers whose merge is reachable from it, added when an edge merged *)
(* onto main (a land result on a main base, a promote result) or the     *)
(* coordinator did (a hand landing). LandedOnMain: a landed producer is  *)
(* on main.                                                               *)
(* A work fault report is a fail that spends no bounce, so no scenario   *)
(* needs it; red results come only in land and burst. A read may fault   *)
(* (p's, in deal): a faulted read is done but is not a read. It is owed   *)
(* again, and the verdict needs a read that said so: yes an ok read, fix  *)
(* a fix read; reads that all faulted are retry, which moves nothing, and *)
(* the deal cuts one more read.                                           *)
(* The policy gate (defect 53): w.pol is whether an active policy answers *)
(* (in pre it fails and is fixed as an environment move). While it is    *)
(* down the tick deals nothing (no preemption, no Start, no read) and     *)
(* one no-policy judgement on the coordinator's row says so; resolve,     *)
(* fix, the inbox and landing go on.                                      *)
(* Rows: r1 width 1 serves tier 1 (rank 1); r2 width 2 serves tiers 1 and *)
(* 2 (rank 2).                                                             *)
(*                                                                         *)
(* Presence (D1', 2026-11-07): bus is who is on nova-bus (layer 6's       *)
(* Present; the steps' Seen within PresentGrace), apart from the up the   *)
(* store keeps, which is the row's session record. An up row absent from  *)
(* the bus is open to nothing: the deal (Eligible), the preemption and    *)
(* the unserved and cannot-read judgements (ServingOpen) and the unserved *)
(* clearing (Expired) all require it present. In deal r2 may leave and    *)
(* return (an environment move). NeverDealtAbsent: no accepted Start,     *)
(* read or preemption lands on an absent row. The nudge and ready_take    *)
(* are not here: the model has no take (below), so they stay layer 6's    *)
(* and Go's.                                                              *)
(*                                                                         *)
(* Admission (2026-11-08): admitted is whether each producer passed add's  *)
(* admission (check's re-proof) when layer 6 last proved its stream at the *)
(* base it has now (Inputs.Unadmitted). A producer that failed is never    *)
(* dealt nor preempted for, never counted undealt, and its stream has an   *)
(* open admission judgement until none fails. In deal the base of s1 may   *)
(* move under p (an environment move). NeverDealtUnadmitted: no accepted   *)
(* Start or preemption is of a producer that failed; AdmissionShown: a     *)
(* waiting or ready one has its stream's judgement.                        *)
(*                                                                         *)
(* Lost cards (item 2, 2026-11-08): lapsed is the working consumers the  *)
(* code withdraws by structure, which the model folds into one             *)
(* environment fact: its row off the bus (or never seen since the run      *)
(* started) past row_silent_withdraw, its tier's deadline passed with no  *)
(* report, or not taken at ready_take with no other row to deal it to. In *)
(* pre and deal any working consumer may lapse (an environment move). The *)
(* tick releases each (CardMachine Withdraw: the consumer fails, its       *)
(* producer back to the queue, branch kept) unless an earlier step claimed *)
(* it. LapsedReleased: every such consumer is in a release group.          *)
(*                                                                         *)
(* Deadlines (item 3, 2026-11-08): Tick(a, due) with due a deadline      *)
(* passing for every open judgement that has one (all but land and        *)
(* no-policy) this tick. A due judgement whose cause the tick detects      *)
(* (jnew) stays open: the code expires it and opens its next episode in    *)
(* the same op, which is the same [kind, about] here; one no step detects  *)
(* closes (Lapsed). ExpiryNeverSilent: every cause the tick detects is     *)
(* open after it.                                                          *)
(*                                                                         *)
(* last is the step monitor of the last tick: bad is the set of checks    *)
(* the tick broke (each computed from the view, the store before and      *)
(* after, and the plan, independently of the planning operators where     *)
(* the check is about them), tags the few events a reach check needs.     *)
(* Every other action resets it, so it never splits states.               *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets, Sequences

CONSTANTS
  Scenario,   \* "pre", "deal", "land" or "burst"
  K,          \* consumer slots per producer
  MaxAtt,     \* model bound: Start is not enabled at this attempt
  MaxBounce,  \* model bound: a fail or fix report is not enabled at this count
  MaxEnv,     \* model bound: the stream's hold and the rows' Gone and Seen
  MaxArrive,  \* model bound: inbox items per behaviour
  Budget,     \* policy's budget: cards touched per tick
  Pack        \* policy's packing size: groups per op

Epoch     == 1
LandLike  == Scenario \in {"land", "burst"}
ProdSeq   == IF LandLike THEN <<"p", "q">> ELSE <<"b", "p", "q">>
Prods     == {ProdSeq[i] : i \in 1..Len(ProdSeq)}
RowSeq    == <<"r1", "r2">>
Rows      == {"r1", "r2"}
RowIdx(r) == IF r = "r1" THEN 1 ELSE 2
Streams   == {"s1", "s2"}
Width     == [r \in Rows |-> IF r = "r1" THEN 1 ELSE 2]
RowTiers  == [r \in Rows |-> IF r = "r1" THEN {1} ELSE {1, 2}]
Rank      == [r \in Rows |-> IF r = "r1" THEN 1 ELSE 2]
MaxRank   == 2
\* escalate_after is 2 by default (unmeasured); the model bounds bounces at
\* MaxBounce, so it escalates after 1.
EscalateAfter == 1
\* The undealt judgement's bound, in ticks: the code's UndealtAfter is 2 s
\* at one tick a second (a guess, not measured).
UndealtTicks == 2
TierName(t) == IF t = 1 THEN "t1" ELSE "t2"

Stream == [p \in Prods |-> CASE p = "p" -> "s1"
                             [] p = "q" -> IF LandLike THEN "s2" ELSE "s1"
                             [] OTHER   -> "s2"]
Tier   == [p \in Prods |-> IF p = "b" /\ Scenario = "deal" THEN 2 ELSE 1]
Needs  == [p \in Prods |-> IF p = "b" /\ Scenario = "deal" THEN {"p"} ELSE {}]
After  == [p \in Prods |-> IF p = "q" /\ LandLike THEN {"p"} ELSE {}]
Prio0  == [p \in Prods |-> IF p = "b" /\ Scenario = "pre" THEN "blocker" ELSE "normal"]
WorkResults == IF Scenario = "deal" THEN {"ok", "fail"} ELSE {"ok"}
ReadResults == IF Scenario = "deal" THEN {"ok", "fix", "fault"} ELSE {"ok"}
\* Only p bounces in deal (its fix, escalation and re-deal), and only p's
\* reads fault; a red result comes only in land and burst.
MayBounce(p) == Scenario = "deal" /\ p = "p"
MayFault(p)  == Scenario = "deal" /\ p = "p"
LandOutcomes == IF LandLike THEN {"landed", "red"} ELSE {"landed"}
\* Whether a stream's base is main: s2's is a workstream branch in land
\* and burst.
MainBase == [s \in Streams |-> ~(LandLike /\ s = "s2")]

NoR     == "-"
NoC     == <<"-", 0>>
PKey(p) == <<p, 0>>
CIds    == Prods \X (1..K)
ConsOf(p) == {<<p, i>> : i \in 1..K}
Started == {"working", "review", "merging"}
PStates == {"waiting", "ready", "working", "review", "merging", "landed"}

NoCons == [st |-> "none", kind |-> "-", row |-> NoR, att |-> 0, res |-> "-", review |-> 0]
P0(p) ==
  IF LandLike
  THEN [st |-> "merging", att |-> 1, head |-> 1, author |-> "r1", bounces |-> 0,
        prio |-> Prio0[p], review |-> 1, ncut |-> 0, red |-> FALSE]
  ELSE [st |-> "waiting", att |-> 0, head |-> 0, author |-> NoR, bounces |-> 0,
        prio |-> Prio0[p], review |-> 0, ncut |-> 0, red |-> FALSE]
W0 == [ps  |-> [p \in Prods |-> P0(p)],
       cs  |-> [c \in CIds |-> NoCons],
       up  |-> [r \in Rows |-> ~(Scenario = "pre" /\ r = "r1")],
       rg  |-> [r \in Rows |-> 0],
       sh  |-> [s \in Streams |-> Scenario = "pre" /\ s = "s2"],
       tko |-> <<>>,
       pol |-> TRUE,
       ob  |-> {}]

\* The oracle's answer: rows(candidates, rows). A policy that answers
\* prefers one row for every candidate (in domain for a candidate when that
\* row is eligible for it, else that candidate's answer is out of domain).
\* "failed" stands for every answer the engine cannot use: none in the
\* tick's policy time, an error, out of range, or naming a row not offered
\* (one path in the engine). The oracle branches only when a question has
\* subjects (Asked). The preemption's victim is not policy's: the youngest
\* working consumer on the row is the engine's fixed rule (LAYER-5, the
\* boundary; LESSONS 5.12).
Answers == {[rows |-> "r1"], [rows |-> "r2"], [rows |-> "failed"]}
Unasked == [rows |-> "failed"]

VARIABLES
  w,         \* the store: [ps, cs, up, rg, sh, tko, pol, ob]; rg is each row's generation, which every row write raises; pol is whether an active policy answers; ob the merging producers recorded on base
  snap,      \* the view the next tick plans on (W0 while idle)
  phase,     \* "idle" or "snapped"
  inbox,     \* edge results: Seq of [stamp, id, out]
  inflight,  \* edge requests in flight: set of [id, stream, sent]
  js,        \* open judgements: set of [kind, about]
  jsnap,     \* the open judgements as the snapshot read them ({} while idle)
  split,     \* the groups of the last tick's refused ops of two groups or more
  arrived,   \* inbox items so far (each item's stamp)
  processed, \* inbox items consumed so far
  env,       \* environment moves so far
  last,      \* the step monitor: [tags, bad]
  rdy,       \* producer -> ticks in a row it was left ready with room (0..UndealtTicks)
  ju,        \* producers with an open undealt judgement
  bus,       \* row -> on nova-bus this tick (Present; not the store's up)
  admitted,  \* producer -> passed admission at its stream's base (layer 6's re-proof)
  onmain,    \* git's main: the producers whose merge is reachable from it
  lapsed     \* consumers the code withdraws by structure: off the bus, past the deadline, untaken

vars == <<w, snap, phase, inbox, inflight, js, jsnap, split, arrived, processed, env, last, rdy, ju, bus, admitted, onmain, lapsed>>
aux  == <<rdy, ju>>
Clean == [tags |-> {}, bad |-> {}]

-----------------------------------------------------------------------------
(* Reading a world W (the store or the view)                               *)

Live(W, c)      == W.cs[c].st \in {"ready", "working"}
LiveOf(W, p)    == {c \in ConsOf(p) : Live(W, c)}
NoLive(W, p)    == LiveOf(W, p) = {}
Working(W, r)   == {c \in CIds : W.cs[c].st = "working" /\ W.cs[c].row = r}
Load(W, r)      == {c \in CIds : Live(W, c) /\ W.cs[c].row = r}
Serves(r, p)    == Tier[p] \in RowTiers[r]
ServingOpen(W, p) == {r \in Rows : W.up[r] /\ bus[r] /\ Serves(r, p)}   \* up and present (D1')
\* Room for p: under the model's bounds, an open serving row with working
\* consumers under its width.
RoomIn(W, p)    == /\ W.ps[p].att < MaxAtt
                   /\ W.ps[p].ncut < K
                   /\ \E r \in ServingOpen(W, p) : Cardinality(Load(W, r)) < Width[r]
BackOf(W, p)    == IF W.sh[Stream[p]] THEN "waiting" ELSE "ready"
NextSlot(W, p)  == <<p, W.ps[p].ncut + 1>>
NeedsLanded(W, p) == \A n \in Needs[p] : W.ps[n].st = "landed"
Same(V, W, k)   == IF k[2] = 0 THEN V.ps[k[1]] = W.ps[k[1]] ELSE V.cs[k] = W.cs[k]

Pos(s, x) == CHOOSE i \in 1..Len(s) : s[i] = x
Drop(s, x) == SelectSeq(s, LAMBDA y : y # x)
First(seq, S) == seq[CHOOSE i \in 1..Len(seq) : seq[i] \in S /\ \A j \in 1..(i - 1) : seq[j] \notin S]
Youngest(W, S) == CHOOSE c \in S : \A d \in S : Pos(W.tko, d) <= Pos(W.tko, c)

\* Start (CardMachine Start): p to working, attempt + 1, a work consumer
\* cut working on r, in the row's hand (the deal is the take).
StartOK(W, p, r) ==
  /\ W.ps[p].st = "ready"
  /\ W.up[r]
  /\ ~W.sh[Stream[p]]
  /\ Serves(r, p)
  /\ W.ps[p].att < MaxAtt
  /\ W.ps[p].ncut < K
StartedP(W, p) == [W.ps[p] EXCEPT !.st = "working", !.att = @ + 1, !.author = NoR,
                                  !.ncut = @ + 1, !.red = FALSE]
StartCut(W, p, r) == [st |-> "working", kind |-> "work", row |-> r, att |-> W.ps[p].att + 1,
                      res |-> "-", review |-> 0]

\* AskRead (CardMachine AskRead): a read of the current review on a row
\* that is not the author's and holds no live read of p.
ReadOK(W, p, r) ==
  /\ W.ps[p].st = "review"
  /\ ~W.sh[Stream[p]]
  /\ W.up[r]
  /\ r # W.ps[p].author
  /\ Serves(r, p)
  /\ W.ps[p].ncut < K
  /\ ~\E k \in LiveOf(W, p) : W.cs[k].kind = "read"
ReadCut(W, p, r) == [st |-> "working", kind |-> "read", row |-> r, att |-> W.ps[p].att,
                     res |-> "-", review |-> W.ps[p].review]
CurReads(W, p) == {c \in ConsOf(p) : W.cs[c].kind = "read" /\ W.cs[c].st # "none"
                                     /\ W.cs[c].review = W.ps[p].review}
\* A done read that faulted is not a read.
Faulted(W, c) == W.cs[c].st = "done" /\ W.cs[c].res = "fault"
\* Owed a read: in review with no live or done read of this review (a
\* withdrawn read and a faulted one are owed again).
ReadOwed(W, p) == W.ps[p].st = "review" /\ ~\E c \in CurReads(W, p) : W.cs[c].st # "fail" /\ ~Faulted(W, c)
\* ReadYes and ReadFix: every read of this review final, one done.
Settled(W, p) == W.ps[p].st = "review" /\ NoLive(W, p) /\ \E c \in CurReads(W, p) : W.cs[c].st = "done"
\* The verdict default over the done reads: fix on a fix read, else yes on
\* an ok read, else (every done read faulted) retry, which moves nothing.
DoneSaid(W, p, res) == \E c \in CurReads(W, p) : W.cs[c].st = "done" /\ W.cs[c].res = res
Verdict(W, p) == IF DoneSaid(W, p, "fix") THEN "fix" ELSE IF DoneSaid(W, p, "ok") THEN "yes" ELSE "retry"

\* Ready: needs landed and the stream not held. Only a NEEDS orders
\* starts: two producers that edit one file both start, and landing
\* stitches them. Room on a row is not a condition.
ResolveOK(W, p) ==
  /\ W.ps[p].st = "waiting"
  /\ NeedsLanded(W, p)
  /\ ~W.sh[Stream[p]]

-----------------------------------------------------------------------------
(* Groups. A group is one producer's move with the consumers it touches:  *)
(* cards (written), rk (card records read, fenced), rr and rsx (the rows  *)
(* and streams read, fenced; yes and landed read no stream, as            *)
(* CardMachine's ReadYes and Landed and modules/card/move.go do not). Card keys   *)
(* are <<p, 0>> for a producer and <<p, i>> for its i-th consumer slot.   *)
(* blk marks a blocker group (never cut by the budget); stamp the inbox   *)
(* item it came from (0: none); req the edge requests it carries (After = *)
(* its op).                                                               *)

StopReq(c) == [id |-> <<Epoch, "stop", c, 0>>, stream |-> Stream[c[1]], sent |-> FALSE]
LandReq(V, p) == [id |-> <<Epoch, "land", PKey(p), V.ps[p].head>>, stream |-> Stream[p], sent |-> FALSE]

Involved(kind, p, c) == CASE kind = "none" -> {} [] kind = "pre" -> {p, c[1]} [] OTHER -> {p}
CardsOf(kind, p, c, V) ==
  CASE kind = "none"                 -> {}
    [] kind = "pre"                  -> {PKey(p), NextSlot(V, p), c, PKey(c[1])}
    [] kind = "release"              -> {PKey(p), c}
    [] kind \in {"start", "read"}    -> {PKey(p), NextSlot(V, p)}
    [] OTHER                         -> {PKey(p)}
ReadKeys(kind, p, c, V) ==
  UNION {{PKey(x)} \cup ConsOf(x) : x \in Involved(kind, p, c)}
    \cup (IF kind = "resolve"
          THEN {PKey(q) : q \in {q \in Prods : Stream[q] = Stream[p]} \cup Needs[p]}
          ELSE {})

Grp(kind, p, c, r, stamp, sha, V) ==
  [kind  |-> kind, p |-> p, c |-> c, r |-> r, stamp |-> stamp, sha |-> sha,
   blk   |-> kind = "pre" \/ (kind = "start" /\ V.ps[p].prio = "blocker"),
   cards |-> CardsOf(kind, p, c, V),
   rk    |-> ReadKeys(kind, p, c, V),
   rr    |-> IF r = NoR THEN {} ELSE {r},
   rsx   |-> IF kind \in {"none", "yes", "landed", "onbase"} THEN {} ELSE {Stream[p]},
   req   |-> IF kind = "pre" THEN {StopReq(c)} ELSE {}]
NoneG(stamp, V) == Grp("none", "-", NoC, NoR, stamp, 0, V)

\* The fence: every record the group read is as the view had it. A row is
\* fenced by its generation, as the store does (RowGen): a row that went
\* down and came back is not the row the plan read.
Fence(V, W, g) ==
  /\ \A k \in g.rk : Same(V, W, k)
  /\ \A r \in g.rr : V.rg[r] = W.rg[r]
  /\ \A s \in g.rsx : V.sh[s] = W.sh[s]

\* The store's own rules for each move (the CardMachine guards).
Legal(W, g) ==
  CASE g.kind = "pre"     -> /\ StartOK(W, g.p, g.r)
                             /\ W.cs[g.c].st = "working" /\ W.cs[g.c].row = g.r
                             /\ g.c[1] # g.p
    [] g.kind = "start"   -> StartOK(W, g.p, g.r)
    [] g.kind = "read"    -> ReadOK(W, g.p, g.r)
    [] g.kind = "yes"     -> Settled(W, g.p) /\ Verdict(W, g.p) = "yes"
    [] g.kind = "fix"     -> Settled(W, g.p) /\ Verdict(W, g.p) = "fix"
    [] g.kind = "resolve" -> W.ps[g.p].st = "waiting" /\ NeedsLanded(W, g.p) /\ ~W.sh[Stream[g.p]]
    [] g.kind \in {"landed", "onbase"} -> W.ps[g.p].st = "merging" /\ W.ps[g.p].head = g.sha
    [] g.kind = "release" -> W.cs[g.c].st = "working"
    [] OTHER              -> TRUE

\* The writes. pre is CardMachine Preempt: Withdraw of the victim (which
\* releases its producer when it is the current work consumer of a
\* working producer) and the blocker's Start, in one plan.
Releases(W, c) == W.cs[c].kind = "work" /\ W.ps[c[1]].st = "working" /\ W.cs[c].att = W.ps[c[1]].att
PreWrites(W, g) ==
  LET v   == g.c[1]
      ps1 == IF Releases(W, g.c) THEN [W.ps EXCEPT ![v].st = BackOf(W, v)] ELSE W.ps
  IN [W EXCEPT !.ps  = [ps1 EXCEPT ![g.p] = StartedP(W, g.p)],
               !.cs  = [W.cs EXCEPT ![g.c].st = "fail", ![NextSlot(W, g.p)] = StartCut(W, g.p, g.r)],
               !.tko = Append(Drop(W.tko, g.c), NextSlot(W, g.p))]
ApplyG(W, g) ==
  CASE g.kind = "pre"     -> PreWrites(W, g)
    [] g.kind = "start"   -> [W EXCEPT !.ps[g.p] = StartedP(W, g.p),
                                       !.cs[NextSlot(W, g.p)] = StartCut(W, g.p, g.r),
                                       !.tko = Append(@, NextSlot(W, g.p))]
    [] g.kind = "read"    -> [W EXCEPT !.ps[g.p].ncut = @ + 1,
                                       !.cs[NextSlot(W, g.p)] = ReadCut(W, g.p, g.r),
                                       !.tko = Append(@, NextSlot(W, g.p))]
    [] g.kind = "yes"     -> [W EXCEPT !.ps[g.p].st = "merging"]
    [] g.kind = "fix"     -> [W EXCEPT !.ps[g.p].st = BackOf(W, g.p), !.ps[g.p].bounces = @ + 1]
    [] g.kind = "resolve" -> [W EXCEPT !.ps[g.p].st = "ready"]
    [] g.kind = "landed"  -> [W EXCEPT !.ps[g.p].st = "landed", !.ob = @ \ {g.p}]
    [] g.kind = "onbase"  -> [W EXCEPT !.ob = @ \cup {g.p}]
    [] g.kind = "release" -> [W EXCEPT !.ps = IF Releases(W, g.c) THEN [@ EXCEPT ![g.p].st = BackOf(W, g.p)] ELSE @,
                                       !.cs[g.c].st = "fail", !.tko = Drop(@, g.c)]
    [] OTHER              -> W

-----------------------------------------------------------------------------
(* The steps, each a function of the view V, the cards claimed by earlier *)
(* steps (cl) and the oracle's answers. A card claimed by an earlier step *)
(* is skipped by the later ones.                                          *)

Free(p, cl) == PKey(p) \notin cl /\ \A c \in ConsOf(p) : c \notin cl
Cards(gs) == UNION {gs[i].cards : i \in 1..Len(gs)}

\* Preemption. A blocker is due when it is ready (and startable) and no
\* open serving row has free width (working under width).
NoFreeWidth(W, b) == \A r \in ServingOpen(W, b) : Cardinality(Working(W, r)) >= Width[r]
BlockerDue(W, b) ==
  /\ admitted[b]   \* a producer that failed admission is preempted for by no one
  /\ W.ps[b].prio = "blocker"
  /\ W.ps[b].st = "ready"
  /\ ~W.sh[Stream[b]]
  /\ W.ps[b].att < MaxAtt
  /\ W.ps[b].ncut < K
  /\ NoFreeWidth(W, b)
Victims(W, b, r) == {c \in Working(W, r) : c[1] # b}
PreRows(W, b)    == {r \in ServingOpen(W, b) : Victims(W, b, r) # {}}
VictimFor(W, S) == Youngest(W, S)

\* acc: [gs, used (rows preempted this tick), uns (tiers unserved), miss]
RECURSIVE PreFold(_, _, _, _)
PreFold(V, a, i, acc) ==
  IF i > Len(ProdSeq) THEN acc
  ELSE
    LET b == ProdSeq[i]
        R == PreRows(V, b) \ acc.used
        r == IF a.rows \in R THEN a.rows ELSE First(RowSeq, R)
        c == VictimFor(V, Victims(V, b, r))
        g == Grp("pre", b, c, r, 0, 0, V)
    IN IF ~BlockerDue(V, b) THEN PreFold(V, a, i + 1, acc)
       ELSE IF ServingOpen(V, b) = {}
       THEN PreFold(V, a, i + 1, [acc EXCEPT !.uns = @ \cup {TierName(Tier[b])}])
       ELSE IF R = {} THEN PreFold(V, a, i + 1, acc)
       ELSE PreFold(V, a, i + 1,
                    [acc EXCEPT !.gs = Append(@, g), !.used = @ \cup {r},
                                !.miss = @ \cup (IF a.rows \notin R THEN {"rows"} ELSE {})])
PreStep(V, a) == PreFold(V, a, 1, [gs |-> <<>>, used |-> {}, uns |-> {}, miss |-> {}])

\* The gate's plan: nothing (the fields of PreStep's and DealStep's acc).
NoPlan == [gs |-> <<>>, t |-> [r \in Rows |-> 0], used |-> {}, uns |-> {}, miss |-> {}, cr |-> {}]

\* Land results, in arrival order. A landed result for a producer still
\* merging at the request's head is its Landed when the merge is on main
\* (a promote result, or a land result on a main base), else its OnBase
\* (the base merge recorded, still merging); anything else (a judged
\* outcome, an ack, a stale or repeated result) is consumed with no move.
LandKind(k, p) == IF k = "promote" \/ MainBase[Stream[p]] THEN "landed" ELSE "onbase"
ItemGroup(V, it, cl) ==
  LET p == it.id[3][1]
  IN IF /\ it.id[2] \in {"land", "promote"} /\ it.out = "landed"
        /\ V.ps[p].st = "merging" /\ V.ps[p].head = it.id[4]
        /\ PKey(p) \notin cl
     THEN Grp(LandKind(it.id[2], p), p, NoC, NoR, it.stamp, it.id[4], V)
     ELSE NoneG(it.stamp, V)

\* The land judgements a tick opens: every judged result in the inbox for a
\* request in flight whose producer is merging at its head in the view.
LandJ(p) == [kind |-> "land", about |-> p]
Judged(J, p) == LandJ(p) \in J
LandJudged(V, I, F) ==
  {LandJ(I[i].id[3][1]) : i \in {i \in 1..Len(I) :
      /\ I[i].id[2] \in {"land", "promote"} /\ I[i].out # "landed"
      /\ \E f \in F : f.id = I[i].id
      /\ V.ps[I[i].id[3][1]].st = "merging" /\ V.ps[I[i].id[3][1]].head = I[i].id[4]}}
RECURSIVE InboxFold(_, _, _, _)
InboxFold(V, I, i, acc) ==
  IF i > Len(I) THEN acc
  ELSE LET g == ItemGroup(V, I[i], acc.cl)
       IN InboxFold(V, I, i + 1, [acc EXCEPT !.es = Append(@, g), !.cl = @ \cup g.cards])

\* Needs: Resolve every producer the view makes ready.
ResolveSet(V, cl) == {p \in Prods : PKey(p) \notin cl /\ ResolveOK(V, p)}
GroupsOf(kind, S, V) ==
  LET s == SelectSeq(ProdSeq, LAMBDA x : x \in S)
  IN [i \in 1..Len(s) |-> Grp(kind, s[i], NoC, NoR, 0, 0, V)]

\* Release (item 2): each lapsed working consumer whose producer is free,
\* withdrawn back to the queue (the code's limits and unacked steps).
RECURSIVE RelFold(_, _, _, _)
RelFold(V, cl, i, acc) ==
  IF i > Len(ProdSeq) THEN acc
  ELSE LET p == ProdSeq[i]
           L == {c \in ConsOf(p) : c \in lapsed /\ V.cs[c].st = "working"}
           c == CHOOSE c \in ConsOf(p) : c \in L \/ L = {}
       IN IF L # {} /\ Free(p, cl)
          THEN RelFold(V, cl, i + 1, Append(acc, Grp("release", p, c, V.cs[c].row, 0, 0, V)))
          ELSE RelFold(V, cl, i + 1, acc)
RelStep(V, cl) == RelFold(V, cl, 1, <<>>)

\* Fix: ReadYes or ReadFix by the default verdict; retry plans nothing.
FixSet(V, cl, v) == {p \in Prods : Free(p, cl) /\ Settled(V, p) /\ Verdict(V, p) = v}

\* Deal and reads. Candidates down the ladder: blocker work, every read
\* owed, normal work; within a rung, id order (policy's order, default).
\* A row is dealt while its working consumers are under its width: what is
\* dealt is in the row's hand (defect 52), so there is no ahead.
\* Escalation is row choice: the default rank is the card's tier, one up
\* once its bounces reach escalate_after; a read's is the rank above its
\* author's row. A row below the card's tier is never eligible.
Desired(V, x) ==
  IF x.kind = "read"
  THEN IF Rank[V.ps[x.p].author] < MaxRank THEN Rank[V.ps[x.p].author] + 1 ELSE MaxRank
  ELSE IF V.ps[x.p].bounces >= EscalateAfter /\ Tier[x.p] < MaxRank THEN Tier[x.p] + 1 ELSE Tier[x.p]
Used(V, r, t) == Cardinality(Load(V, r)) + t[r]
Eligible(V, x, r, t) ==
  /\ V.up[r]
  /\ bus[r]   \* on the bus this tick: a configured row is not a present one (D1')
  /\ Serves(r, x.p)
  /\ Used(V, r, t) < Width[r]
  /\ (x.kind = "read" => r # V.ps[x.p].author)
\* The default: the desired rank exactly, then above it, then an idle
\* lane, then most room, then name.
Score(V, x, r, t) ==
  (CASE Rank[r] = Desired(V, x) -> 100 [] Rank[r] > Desired(V, x) -> 50 [] OTHER -> 0)
  + (IF Used(V, r, t) = 0 THEN 10 ELSE 0) + (Width[r] - Used(V, r, t))
DefaultRow(V, x, t) ==
  LET E == {r \in Rows : Eligible(V, x, r, t)}
  IN IF E = {} THEN NoR
     ELSE CHOOSE r \in E : \A s \in E : \/ Score(V, x, r, t) > Score(V, x, s, t)
                                        \/ Score(V, x, r, t) = Score(V, x, s, t) /\ RowIdx(r) <= RowIdx(s)
InDomain(V, x, ar, t) == ar \in Rows /\ Eligible(V, x, ar, t)
RowFor(V, x, t, ar) == IF InDomain(V, x, ar, t) THEN ar ELSE DefaultRow(V, x, t)
MayMove(V, x, r) == IF x.kind = "work" THEN StartOK(V, x.p, r) ELSE ReadOK(V, x.p, r)
\* A rows answer that names an offered row (up, on the bus, serving the
\* candidate, not a read's author) while no row has room is no miss: the
\* candidate waits for room, which is no failure of policy (the code's
\* pickRows, TestTickRowsMissNamesNoRoom). With room on another row it is.
Offered(V, x, r) == V.up[r] /\ bus[r] /\ Serves(r, x.p) /\ (x.kind = "read" => r # V.ps[x.p].author)
FullAnswer(V, x, ar, t) == ar \in Rows /\ Offered(V, x, ar) /\ DefaultRow(V, x, t) = NoR

\* acc: [gs, t (in-tick tally per row), miss, uns, cr]. useAns FALSE is the
\* no-policy reference the policy check compares with.
RECURSIVE DealFold(_, _, _, _, _, _)
DealFold(V, cands, i, acc, ar, useAns) ==
  IF i > Len(cands) THEN acc
  ELSE
    LET x  == cands[i]
        r  == IF useAns THEN RowFor(V, x, acc.t, ar) ELSE DefaultRow(V, x, acc.t)
        ok == r # NoR /\ MayMove(V, x, r)
        g  == Grp(IF x.kind = "work" THEN "start" ELSE "read", x.p, NoC, r, 0, 0, V)
        mi == IF InDomain(V, x, ar, acc.t) \/ FullAnswer(V, x, ar, acc.t) THEN {} ELSE {"rows"}
        un == IF x.kind = "work" /\ ServingOpen(V, x.p) = {} THEN {TierName(Tier[x.p])} ELSE {}
        cr == IF x.kind = "read" /\ ServingOpen(V, x.p) \subseteq {V.ps[x.p].author}
              THEN {x.p} ELSE {}
    IN DealFold(V, cands, i + 1,
                IF ok THEN [acc EXCEPT !.gs = Append(@, g), !.t = [@ EXCEPT ![r] = @ + 1],
                                       !.miss = @ \cup mi]
                      ELSE [acc EXCEPT !.miss = @ \cup mi, !.uns = @ \cup un, !.cr = @ \cup cr],
                ar, useAns)
Cands(V, cl) ==
  LET bl == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = "blocker" /\ V.ps[p].st = "ready" /\ Free(p, cl) /\ admitted[p])
      \* a held stream asks no read (the code's dealRead returns first; found
      \* by the trace check, steps-walk-land-1, 2026-11-07)
      rd == SelectSeq(ProdSeq, LAMBDA p : ReadOwed(V, p) /\ ~V.sh[Stream[p]] /\ Free(p, cl))
      nm == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = "normal" /\ V.ps[p].st = "ready" /\ Free(p, cl)
                                          /\ admitted[p])   \* a producer that failed admission is never dealt
  IN [i \in 1..Len(bl) |-> [p |-> bl[i], kind |-> "work"]]
       \o [i \in 1..Len(rd) |-> [p |-> rd[i], kind |-> "read"]]
       \o [i \in 1..Len(nm) |-> [p |-> nm[i], kind |-> "work"]]
DealStep(V, cl, ar, useAns) ==
  DealFold(V, Cands(V, cl), 1,
           [gs |-> <<>>, t |-> [r \in Rows |-> 0], miss |-> {}, uns |-> {}, cr |-> {}], ar, useAns)

\* Land requests: per stream with none in flight, flowing: the first
\* merging producer not on base, under no land judgement (J, open or
\* opened this tick), none of whose after targets is merging. A hold
\* never touches landing. Promote requests: per stream with none in
\* flight, the first producer on base under no land judgement.
Merging(V, s, cl) == {p \in Prods : Stream[p] = s /\ V.ps[p].st = "merging" /\ PKey(p) \notin cl /\ p \notin V.ob}
BusyK(F, s, k) == \E f \in F : f.stream = s /\ f.id[2] = k
Busy(F, s) == BusyK(F, s, "land")
LandElig(V, s, cl, J) == {p \in Merging(V, s, cl) : ~Judged(J, p) /\ \A t \in After[p] : V.ps[t].st # "merging"}
LandPicks(V, cl, F, J) ==
  {First(ProdSeq, LandElig(V, s, cl, J)) : s \in {s \in Streams : ~Busy(F, s) /\ LandElig(V, s, cl, J) # {}}}
PromElig(V, s, cl, J) == {p \in V.ob : Stream[p] = s /\ V.ps[p].st = "merging" /\ PKey(p) \notin cl /\ ~Judged(J, p)}
PromPicks(V, cl, F, J) ==
  {First(ProdSeq, PromElig(V, s, cl, J)) : s \in {s \in Streams : ~BusyK(F, s, "promote") /\ PromElig(V, s, cl, J) # {}}}
PromReq(V, p) == [id |-> <<Epoch, "promote", PKey(p), V.ps[p].head>>, stream |-> Stream[p], sent |-> FALSE]

-----------------------------------------------------------------------------
(* Order, budget, pack, apply                                             *)

Size(g) == Cardinality(g.cards)
\* Blocker groups always; then the rest in order while the cards touched
\* stay within the budget, the first of them always (so a group larger
\* than the budget is not starved), stopping at the first that does not
\* fit (the inbox stays a prefix).
RECURSIVE AdmitFold(_, _, _)
AdmitFold(E, i, acc) ==
  IF i > Len(E) THEN acc
  ELSE LET g == E[i]
       IN IF g.blk THEN AdmitFold(E, i + 1, [acc EXCEPT !.adm = Append(@, g), !.t = @ + Size(g)])
          ELSE IF ~acc.stop /\ (acc.nb = 0 \/ acc.t + Size(g) <= Budget)
          THEN AdmitFold(E, i + 1, [acc EXCEPT !.adm = Append(@, g), !.t = @ + Size(g), !.nb = @ + 1])
          ELSE AdmitFold(E, i + 1, [acc EXCEPT !.stop = TRUE])
Admit(E) == AdmitFold(E, 1, [adm |-> <<>>, t |-> 0, nb |-> 0, stop |-> FALSE]).adm

\* Groups into ops of up to Pack, in order; a group touching a card of a
\* group in split (from a refused op) is an op of its own.
Alone(g, sp) == \E h \in sp : h.cards \cap g.cards # {}
Close(ops, cur) == IF cur = <<>> THEN ops ELSE Append(ops, cur)
RECURSIVE PackFold(_, _, _, _, _)
PackFold(gs, i, cur, ops, sp) ==
  IF i > Len(gs) THEN Close(ops, cur)
  ELSE LET g == gs[i]
       IN IF Alone(g, sp) THEN PackFold(gs, i + 1, <<>>, Append(Close(ops, cur), <<g>>), sp)
          ELSE IF Len(cur) + 1 >= Pack THEN PackFold(gs, i + 1, <<>>, Append(ops, Append(cur, g)), sp)
          ELSE PackFold(gs, i + 1, Append(cur, g), ops, sp)
Real(gs) == SelectSeq(gs, LAMBDA g : g.kind # "none")
PackOps(gs, sp) == PackFold(Real(gs), 1, <<>>, <<>>, sp)

RECURSIVE ApplySeq(_, _, _)
ApplySeq(W, o, j) == IF j > Len(o) THEN W ELSE ApplySeq(ApplyG(W, o[j]), o, j + 1)
OpOK(P, W, o) == \A j \in 1..Len(o) : Fence(P, W, o[j]) /\ Legal(W, o[j])
\* acc: [w, pv, ok (accepted op indexes), ref (refused)]. Each op is fenced
\* against pv, the view with this tick's accepted ops applied: what the
\* plan expects every record to be when the op applies. Fencing on the
\* view itself would refuse an op whose group reads a card an earlier op
\* of the same tick wrote (a Resolve reads its stream's producers).
RECURSIVE ApplyFold(_, _, _, _)
ApplyFold(V, ops, i, acc) ==
  IF i > Len(ops) THEN acc
  ELSE IF OpOK(acc.pv, acc.w, ops[i])
       THEN ApplyFold(V, ops, i + 1, [acc EXCEPT !.w = ApplySeq(@, ops[i], 1),
                                                 !.pv = ApplySeq(@, ops[i], 1), !.ok = @ \cup {i}])
       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \cup {i}])

-----------------------------------------------------------------------------
(* The checks a tick must keep, each computed from the view, the store    *)
(* before (W) and after (W1) and the plan.                                *)

GroupsIn(o) == {o[j] : j \in 1..Len(o)}
Changed(W, W1, g)   == \E k \in g.cards : ~Same(W, W1, k)
AsApplied(W, W1, g) == \A k \in g.cards : Same(ApplyG(W, g), W1, k)
\* Guarantee 4's premise, stated apart from the planning operators: a
\* ready, startable blocker and every open serving row at width (working
\* consumers only; ready ones hold no width).
DueByDesign(V, b) ==
  /\ V.ps[b].prio = "blocker" /\ V.ps[b].st = "ready" /\ ~V.sh[Stream[b]] /\ admitted[b]
  /\ V.ps[b].att < MaxAtt /\ V.ps[b].ncut < K
  /\ \A r \in Rows : (V.up[r] /\ bus[r] /\ Serves(r, b)) =>
       Cardinality({c \in CIds : V.cs[c].st = "working" /\ V.cs[c].row = r}) >= Width[r]
RECURSIVE SumSize(_, _)
SumSize(s, k) == IF k = 0 THEN 0 ELSE Size(s[k]) + SumSize(s, k - 1)

\* The ready rule stated apart from ResolveOK: a producer resolvable from
\* the view's state alone, whatever landed its needs.
Resolvable(V, cl, p) ==
  /\ V.ps[p].st = "waiting" /\ PKey(p) \notin cl /\ ~V.sh[Stream[p]]
  /\ \A n \in Needs[p] : V.ps[n].st = "landed"

TickChecks(V, W, W1, a, E, adm, ops, ap, pre, dl, cl3, jsn, jv, picks, proms, cl4, res, clr, jnew) ==
  LET nb  == SelectSeq(adm, LAMBDA g : ~g.blk)
      bt  == SumSize(SelectSeq(adm, LAMBDA g : g.blk), Len(SelectSeq(adm, LAMBDA g : g.blk)))
      gs  == SelectSeq(E, LAMBDA g : g.kind # "none")
      pres == {adm[i] : i \in {i \in 1..Len(adm) : adm[i].kind = "pre"}}
      accd == UNION {GroupsIn(ops[i]) : i \in ap.ok}
  IN {n \in {"OneGroupPerCard", "GroupWhole", "NeverHalfApplied", "RefusalChangesNothing",
             "RefusedOnlyByRace", "BudgetKept", "BlockerPreemptsInOneTick", "PreemptYoungest",
             "PolicyFailsToDefault", "AfterKept", "AfterNeverHolds", "MergingCovered",
             "NoDealWithoutPolicy", "ResolveFromState",
             "VerdictHasItsRead", "NeverDealtAbsent", "NeverDealtUnadmitted", "AdmissionShown",
             "ExpiryNeverSilent", "LapsedReleased"} :
      ~CASE n = "OneGroupPerCard" ->
             \A i, j \in 1..Len(gs) : i # j => gs[i].cards \cap gs[j].cards = {}
        [] n = "GroupWhole" ->
             \A i \in 1..Len(ops) : \A g \in GroupsIn(ops[i]) :
               AsApplied(W, W1, g) \/ ~Changed(W, W1, g)
        [] n = "NeverHalfApplied" ->
             \A i \in 1..Len(ops) :
               \/ \A g \in GroupsIn(ops[i]) : AsApplied(W, W1, g)
               \/ \A g \in GroupsIn(ops[i]) : ~Changed(W, W1, g)
        [] n = "RefusalChangesNothing" ->
             \A i \in ap.ref : \A g \in GroupsIn(ops[i]) : ~Changed(W, W1, g)
        [] n = "RefusedOnlyByRace" ->
             \A i \in ap.ref : \E g \in GroupsIn(ops[i]) : ~Fence(V, W, g)
        [] n = "BudgetKept" ->
             \A k \in 2..Len(nb) : bt + SumSize(nb, k) <= Budget
        [] n = "BlockerPreemptsInOneTick" ->
             \A b \in Prods : DueByDesign(V, b) =>
               \/ \E g \in pres : g.p = b
               \/ ServingOpen(V, b) = {} /\ [kind |-> "unserved", about |-> TierName(Tier[b])] \in jsn
               \/ ~V.pol /\ [kind |-> "no-policy", about |-> "coord"] \in jsn
        [] n = "PreemptYoungest" ->
             \A g \in pres : g.c = Youngest(V, Victims(V, g.p, g.r))
        [] n = "PolicyFailsToDefault" ->
             /\ (V.pol /\ a.rows \notin Rows) => dl.gs = DealStep(V, cl3, a.rows, FALSE).gs
             /\ \A q \in pre.miss \cup dl.miss : [kind |-> "policy", about |-> q] \in jsn
        [] n = "AfterKept" ->
             \A p \in picks : \A t \in After[p] : V.ps[t].st # "merging"
        [] n = "AfterNeverHolds" ->
             \A s \in Streams :
               (~Busy(inflight, s) /\ \E p \in Prods :
                   Stream[p] = s /\ V.ps[p].st = "merging" /\ PKey(p) \notin Cards(gs) /\ p \notin V.ob
                   /\ ~Judged(jv, p) /\ \A t \in After[p] : V.ps[t].st # "merging")
                 => \E p \in picks : Stream[p] = s
        [] n = "MergingCovered" ->
             \A p \in Prods :
               (V.ps[p].st = "merging" /\ PKey(p) \notin cl4) =>
                 \/ p \notin V.ob /\ Busy(inflight, Stream[p])
                 \/ p \notin V.ob /\ \E q \in picks : Stream[q] = Stream[p]
                 \/ p \in V.ob /\ BusyK(inflight, Stream[p], "promote")
                 \/ p \in V.ob /\ \E q \in proms : Stream[q] = Stream[p]
                 \/ Judged(jv, p)
                 \/ p \notin V.ob /\ \E t \in After[p] : V.ps[t].st = "merging"
        [] n = "NoDealWithoutPolicy" ->
             ~V.pol => /\ \A g \in accd : g.kind \notin {"pre", "start", "read"}
                       /\ [kind |-> "no-policy", about |-> "coord"] \in jsn
        [] n = "ResolveFromState" ->
             \A p \in Prods : Resolvable(V, clr, p) => p \in res
        [] n = "VerdictHasItsRead" ->
             \A g \in {x \in GroupsIn(gs) : x.kind \in {"yes", "fix"}} :
               LET said(out) == \E c \in ConsOf(g.p) :
                                  /\ V.cs[c].kind = "read" /\ V.cs[c].review = V.ps[g.p].review
                                  /\ V.cs[c].st = "done" /\ V.cs[c].res = out
               IN IF g.kind = "yes" THEN said("ok") /\ ~said("fix") ELSE said("fix")
        [] n = "NeverDealtAbsent" ->
             \A g \in accd : g.kind \in {"pre", "start", "read"} => bus[g.r]
        [] n = "NeverDealtUnadmitted" ->
             \A g \in accd : g.kind \in {"pre", "start"} => admitted[g.p]
        [] n = "AdmissionShown" ->
             \A p \in Prods : (V.ps[p].st \in {"waiting", "ready"} /\ ~admitted[p])
                              => [kind |-> "admission", about |-> Stream[p]] \in jsn
        [] n = "ExpiryNeverSilent" ->
             jnew \subseteq jsn
        [] n = "LapsedReleased" ->
             \A c \in lapsed : (V.cs[c].st = "working" /\ Free(c[1], cl3))
                              => \E i \in 1..Len(E) : E[i].kind = "release" /\ E[i].c = c}

-----------------------------------------------------------------------------
(* Actions                                                                 *)

\* A question has subjects: a blocker due, a ready producer or a read owed.
Asked(V) == \E p \in Prods : BlockerDue(V, p) \/ V.ps[p].st = "ready" \/ ReadOwed(V, p)
\* While no active policy answers (the gate), nothing is asked.
AnswersFor(V) == IF Asked(V) /\ V.pol THEN Answers ELSE {Unasked}

Init ==
  /\ w = W0
  /\ snap = W0
  /\ phase = "idle"
  /\ inbox = <<>>
  /\ inflight = {}
  /\ js = {}
  /\ jsnap = {}
  /\ split = {}
  /\ arrived = 0
  /\ processed = 0
  /\ env = 0
  /\ last = Clean
  /\ rdy = [p \in Prods |-> 0]
  /\ ju = {}
  /\ bus = [r \in Rows |-> TRUE]
  /\ admitted = [p \in Prods |-> TRUE]
  /\ lapsed = {}
  /\ onmain = {}

Snap ==
  /\ phase = "idle"
  /\ phase' = "snapped"
  /\ snap' = w
  /\ jsnap' = js
  /\ last' = Clean
  /\ UNCHANGED <<w, inbox, inflight, js, split, arrived, processed, env>>
  /\ UNCHANGED <<aux, bus, admitted, onmain, lapsed>>

\* A stream's waiting and ready producers that failed admission.
Unadmitted(V, s) == {p \in Prods : Stream[p] = s /\ V.ps[p].st \in {"waiting", "ready"} /\ ~admitted[p]}

\* Judgements the tick opens (unserved per tier, cannot-read per producer,
\* policy per question, no-policy on the coordinator's row) and expires when the cause clears in the view.
Expired(V, J) ==
  {j \in J : \/ j.kind = "unserved" /\ \E r \in Rows : V.up[r] /\ bus[r] /\ \E t \in RowTiers[r] : TierName(t) = j.about
             \/ j.kind = "cannot-read" /\ \E p \in Prods :
                  p = j.about /\ ~(ReadOwed(V, p) /\ ServingOpen(V, p) \subseteq {V.ps[p].author})
             \/ j.kind = "no-policy" /\ V.pol
             \/ j.kind = "admission" /\ Unadmitted(V, j.about) = {}}

\* The judgements a deadline can close: all but land and no-policy, which
\* sit until answered or cleared (the code's held too).
Deadlined(J) == {j \in J : j.kind \notin {"land", "no-policy"}}

Tick(a, due) ==
  /\ phase = "snapped"
  /\ LET V    == snap
         W    == w
         pre  == IF V.pol THEN PreStep(V, a) ELSE NoPlan
         inb  == InboxFold(V, inbox, 1, [es |-> <<>>, cl |-> Cards(pre.gs)])
         res  == ResolveSet(V, inb.cl)
         rgs  == GroupsOf("resolve", res, V)
         cl2  == inb.cl \cup Cards(rgs)
         fyes == GroupsOf("yes", FixSet(V, cl2, "yes"), V)
         ffix == GroupsOf("fix", FixSet(V, cl2, "fix"), V)
         cl3  == cl2 \cup Cards(fyes) \cup Cards(ffix)
         rel  == RelStep(V, cl3)
         cl3r == cl3 \cup Cards(rel)
         dl   == IF V.pol THEN DealStep(V, cl3r, a.rows, TRUE) ELSE NoPlan
         dblk == SelectSeq(dl.gs, LAMBDA g : g.blk)
         drst == SelectSeq(dl.gs, LAMBDA g : ~g.blk)
         cl4  == cl3r \cup Cards(dl.gs)
         jl   == LandJudged(V, inbox, inflight)
         jv   == jsnap \cup jl   \* the judgements as the tick sees them: its snapshot's and its own
         picks == LandPicks(V, cl4, inflight, jv)
         proms == PromPicks(V, cl4, inflight, jv)
         E    == pre.gs \o dblk \o inb.es \o rgs \o fyes \o ffix \o rel \o drst
         adm  == Admit(E)
         ops  == PackOps(adm, split)
         ap   == ApplyFold(V, ops, 1, [w |-> W, pv |-> V, ok |-> {}, ref |-> {}])
         OpOf(g) == CHOOSE i \in 1..Len(ops) : g \in GroupsIn(ops[i])
         admIn == {adm[i] : i \in {i \in 1..Len(adm) : adm[i].stamp > 0}}
         \* An admitted item is consumed unless its group's op was refused
         \* (then it is put back, in place); the rest carry.
         used  == {g \in admIn : g.kind = "none" \/ OpOf(g) \in ap.ok}
         ustamps == {g.stamp : g \in used}
         uids  == {inbox[i].id : i \in {i \in 1..Len(inbox) : inbox[i].stamp \in ustamps}}
         stops == UNION {g.req : g \in UNION {GroupsIn(ops[i]) : i \in ap.ok}}
         lands == {LandReq(V, p) : p \in picks} \cup {PromReq(V, p) : p \in proms}
         jnew == {[kind |-> "unserved", about |-> t] : t \in pre.uns \cup dl.uns}
                   \cup {[kind |-> "cannot-read", about |-> p] : p \in dl.cr}
                   \cup {[kind |-> "policy", about |-> q] : q \in pre.miss \cup dl.miss}
                   \cup jl
                   \cup (IF V.pol THEN {} ELSE {[kind |-> "no-policy", about |-> "coord"]})
                   \cup {[kind |-> "admission", about |-> s] : s \in {s \in Streams : Unadmitted(V, s) # {}}}
         dueJ == IF due THEN Deadlined(js) ELSE {}
         \* At a deadline a cause the tick still detects is reopened, never
         \* closed; only one no step detects lapses.
         Lapsed == dueJ \ jnew
         jsn  == ((js \ Expired(V, js)) \cup jnew) \ Lapsed
         accd == UNION {GroupsIn(ops[i]) : i \in ap.ok}
         \* Left ready with room: a candidate of the view the tick did not deal.
         \* A run with no active policy deals nothing and says so (no-policy),
         \* so the gate counts nothing toward undealt.
         und  == {p \in Prods : V.pol /\ V.ps[p].st = "ready" /\ ~V.sh[Stream[p]] /\ RoomIn(V, p) /\ admitted[p]
                                /\ ap.w.ps[p].st = "ready"}
         tags == (IF \E g \in accd : g.kind = "pre" THEN {"pre"} ELSE {})
                 \cup (IF ap.ref # {} THEN {"refused"} ELSE {})
                 \cup (IF \E i \in 1..Len(Real(adm)) : Alone(Real(adm)[i], split) THEN {"split"} ELSE {})
                 \cup (IF \E i \in 1..Len(inbox) : inbox[i].stamp \notin ustamps THEN {"carry"} ELSE {})
                 \cup (IF picks # {} THEN {"landreq"} ELSE {})
                 \cup (IF proms # {} THEN {"promreq"} ELSE {})
                 \cup (IF \E s \in Streams : \E p \in Merging(V, s, cl4) \ LandElig(V, s, cl4, jv) :
                          ~Judged(jv, p)
                       THEN {"afterheld"} ELSE {})
                 \cup (IF jl \ js # {} THEN {"judged"} ELSE {})
                 \cup (IF dueJ \cap jnew # {} THEN {"reopened"} ELSE {})
                 \cup (IF \E g \in accd : g.kind = "release" THEN {"released"} ELSE {})
                 \cup (IF \E i \in 1..Len(inbox) : inbox[i].stamp \in ustamps /\ inbox[i].out = "ack"
                       THEN {"ack"} ELSE {})
                 \cup (IF \E r \in Rows : V.up[r] /\ ~bus[r] /\ \E p \in Prods : V.ps[p].st = "ready" /\ Serves(r, p)
                       THEN {"absent"} ELSE {})
                 \cup (IF \E g \in accd : g.kind = "start" /\ Rank[g.r] > Tier[g.p]
                                          /\ V.ps[g.p].bounces >= EscalateAfter
                       THEN {"escal"} ELSE {})
     IN /\ w' = ap.w
        /\ inbox' = SelectSeq(inbox, LAMBDA it : it.stamp \notin ustamps)
        /\ processed' = processed + Cardinality(ustamps)
        /\ inflight' = {f \in inflight : f.id \notin uids} \cup stops \cup lands
        /\ js' = jsn
        /\ split' = UNION {GroupsIn(ops[i]) : i \in {i \in ap.ref : Len(ops[i]) > 1}}
        /\ last' = [tags |-> tags,
                    bad  |-> TickChecks(V, W, ap.w, a, E, adm, ops, ap, pre, dl, cl3r, jsn, jv, picks, proms, cl4,
                                        res, inb.cl, jnew)]
        /\ rdy' = [p \in Prods |-> IF p \in und THEN IF rdy[p] < UndealtTicks THEN rdy[p] + 1 ELSE rdy[p]
                                                ELSE 0]
        /\ ju' = {p \in ju : V.ps[p].st = "ready"} \cup {p \in und : rdy[p] + 1 >= UndealtTicks}
  /\ phase' = "idle"
  /\ snap' = W0
  /\ jsnap' = {}
  /\ UNCHANGED <<arrived, env, bus, admitted, onmain, lapsed>>

\* There is no Take (defect 52): a consumer is dealt working, in its row's
\* hand; the row's acknowledgement moves nothing the steps read.

\* A row's report, applied by its verb as one op (LAYER-6): a work
\* consumer's report moves its producer in the same plan (Finished,
\* Broken, Faulted); a read's moves only the read.
ReportMove(W, p, c, res) ==
  CASE res = "ok"   -> [W.ps[p] EXCEPT !.st = "review", !.head = W.ps[p].att, !.author = W.cs[c].row,
                                       !.review = @ + 1, !.red = FALSE]
    [] res = "fail" -> [W.ps[p] EXCEPT !.st = BackOf(W, p), !.bounces = @ + 1]
    [] OTHER        -> [W.ps[p] EXCEPT !.st = BackOf(W, p)]
Report(c, res) ==
  /\ w.cs[c].st = "working"
  /\ IF w.cs[c].kind = "read"
     THEN /\ res \in ReadResults
          /\ res = "fix" => (MayBounce(c[1]) /\ w.ps[c[1]].bounces < MaxBounce)
          /\ res = "fault" => MayFault(c[1])
          /\ w' = [w EXCEPT !.cs[c].st = "done", !.cs[c].res = res, !.tko = Drop(@, c)]
     ELSE /\ res \in WorkResults
          /\ res = "fail" => (MayBounce(c[1]) /\ w.ps[c[1]].bounces < MaxBounce)
          /\ w' = [w EXCEPT !.cs[c].st = "done", !.cs[c].res = res, !.tko = Drop(@, c),
                            !.ps[c[1]] = ReportMove(w, c[1], c, res)]
  /\ last' = Clean
  /\ UNCHANGED <<snap, jsnap, phase, inbox, inflight, js, split, arrived, processed, env>>
  /\ UNCHANGED <<aux, bus, admitted, onmain, lapsed>>

\* The coordinator's hold (with Park of the stream's ready producers in the
\* same op, LESSONS 2.20) and its lifting; a row's Gone and Seen.
\* The environment each scenario needs: in pre, lifting s2's hold and r2
\* going and coming (unserved); in deal, r1 going and coming; in land, a
\* hold on s1 (which never touches landing).
HoldsOf  == CASE Scenario = "land" -> {"s1"} [] OTHER -> {}
UnholdsOf == CASE Scenario = "pre" -> {"s2"} [] Scenario = "land" -> {"s1"} [] OTHER -> {}
FlipsOf  == CASE Scenario = "pre" -> {"r2"} [] Scenario = "deal" -> {"r1"} [] OTHER -> {}
\* In deal r2's hook drops and comes back: it leaves the bus and returns,
\* the store's up untouched (D1').
BusFlipsOf == CASE Scenario = "deal" -> {"r2"} [] OTHER -> {}
\* In deal s1's base moves under p: layer 6's re-proof refuses it, and a
\* later move admits it again.
AdmFlipsOf == CASE Scenario = "deal" -> {"p"} [] OTHER -> {}
\* In pre the active policy fails (a question errors or no version is
\* active) and a fixed one is set; in deal a store op lands p by hand
\* (land <card>), between ticks and with no land request in
\* flight for it, so no land step and no edge result sees it.
PolFlips == Scenario = "pre"
HandLandsOf == IF Scenario = "deal" THEN {"p"} ELSE {}
\* In pre and deal a working consumer lapses (off the bus, past its
\* deadline, or untaken with no other row): the tick withdraws it.
LapsesOf == IF Scenario \in {"pre", "deal"} THEN CIds ELSE {}
\* A judged card's hand land answers its land judgement in the same op
\* (verbs handLand): that is Answer(p, "land"), so this one has none open.
HandLandOK(p) ==
  /\ phase = "idle"
  /\ w.ps[p].st = "merging"
  /\ ~\E f \in inflight : f.id[2] = "land" /\ f.id[3][1] = p
  /\ ~Judged(js, p)
EnvMove ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ \/ /\ bus' = bus /\ admitted' = admitted /\ lapsed' = lapsed
        /\ \/ \E s \in HoldsOf :
                /\ ~w.sh[s]
                /\ w' = [w EXCEPT !.sh[s] = TRUE,
                                  !.ps = [p \in Prods |-> IF Stream[p] = s /\ w.ps[p].st = "ready"
                                                         THEN [w.ps[p] EXCEPT !.st = "waiting"]
                                                         ELSE w.ps[p]]]
           \/ \E s \in UnholdsOf : w.sh[s] /\ w' = [w EXCEPT !.sh[s] = FALSE]
           \/ \E r \in FlipsOf : w' = [w EXCEPT !.up[r] = ~w.up[r], !.rg[r] = @ + 1]
           \/ PolFlips /\ w' = [w EXCEPT !.pol = ~w.pol]
           \/ \E p \in HandLandsOf : HandLandOK(p) /\ w' = [w EXCEPT !.ps[p].st = "landed"]
     \/ \E r \in BusFlipsOf : bus' = [bus EXCEPT ![r] = ~@] /\ w' = w /\ admitted' = admitted /\ lapsed' = lapsed
     \/ \E p \in AdmFlipsOf : admitted' = [admitted EXCEPT ![p] = ~@] /\ w' = w /\ bus' = bus /\ lapsed' = lapsed
     \/ \E c \in LapsesOf : /\ w.cs[c].st = "working" /\ c \notin lapsed
                             /\ lapsed' = lapsed \cup {c} /\ w' = w /\ bus' = bus /\ admitted' = admitted
  \* A hand landing in deal is onto s1's base, main.
  /\ onmain' = onmain \cup {p \in Prods : w.ps[p].st # "landed" /\ w'.ps[p].st = "landed"}
  \* The hand land is tagged for its reach check (StepsNeverHandLanded).
  /\ last' = IF \E p \in Prods : w.ps[p].st # "landed" /\ w'.ps[p].st = "landed"
             THEN [tags |-> {"handland"}, bad |-> {}] ELSE Clean
  /\ UNCHANGED <<snap, jsnap, phase, inbox, inflight, js, split, arrived, processed>>
  /\ UNCHANGED aux

\* An edge's result reaches the inbox, once or twice.
EdgeResult(f, out, n) ==
  /\ f \in inflight
  /\ ~f.sent
  /\ out \in (IF f.id[2] \in {"land", "promote"} THEN LandOutcomes ELSE {"ack"})
  /\ arrived + n <= MaxArrive
  /\ LET it(k) == [stamp |-> arrived + k, id |-> f.id, out |-> out]
     IN inbox' = IF n = 1 THEN Append(inbox, it(1)) ELSE Append(Append(inbox, it(1)), it(2))
  /\ arrived' = arrived + n
  /\ inflight' = (inflight \ {f}) \cup {[f EXCEPT !.sent = TRUE]}
  \* A landed promote, or a landed land onto a main base, merged onto main.
  /\ onmain' = IF out = "landed" /\ (f.id[2] = "promote" \/ (f.id[2] = "land" /\ MainBase[f.stream]))
               THEN onmain \cup {f.id[3][1]} ELSE onmain
  /\ last' = Clean
  /\ UNCHANGED <<w, snap, jsnap, phase, js, split, processed, env>>
  /\ UNCHANGED <<aux, bus, admitted, lapsed>>

\* The coordinator answers a land judgement (intent rule 32): land is the
\* hand merge (Landed at its head), fix the producer to fix with the
\* finding (Red: review + 1, red recorded), retry the answer alone (the
\* next tick asks a new landing). Each is one verb op, fenced on the
\* judgement open and the producer merging. It may come between Snap and
\* Tick: the tick then still sees the judgement open (jsnap, its view's),
\* asks nothing for the card, and the next tick does (the code's race).
\* A hand landing is landed only on main: the head merged onto a main
\* base, or a producer on base whose base the coordinator merged into
\* main (the verb refuses one that is not on main yet); a head merged
\* onto a workstream base is recorded on base, still merging.
HandOnMain(p) == p \in w.ob \/ MainBase[Stream[p]]
Answer(p, choice) ==
  /\ Judged(js, p)
  /\ choice \in {"land", "fix", "retry"}
  /\ choice # "retry" => w.ps[p].st = "merging"
  /\ w' = CASE choice = "land" /\ HandOnMain(p) -> [w EXCEPT !.ps[p].st = "landed", !.ob = @ \ {p}]
            [] choice = "land" -> [w EXCEPT !.ob = @ \cup {p}]
            [] choice = "fix"  -> [w EXCEPT !.ps[p].st = BackOf(w, p), !.ps[p].review = @ + 1,
                                            !.ps[p].red = TRUE, !.ob = @ \ {p}]   \* to fix (intent rule 30)
            [] OTHER           -> w
  /\ onmain' = IF choice = "land" /\ HandOnMain(p) THEN onmain \cup {p} ELSE onmain
  /\ js' = js \ {LandJ(p)}
  /\ last' = Clean
  /\ UNCHANGED <<snap, jsnap, phase, inbox, inflight, split, arrived, processed, env>>
  /\ UNCHANGED <<aux, bus, admitted, lapsed>>

Next ==
  \/ Snap
  \/ \E a \in AnswersFor(snap), due \in BOOLEAN : Tick(a, due)
  \/ \E c \in CIds, res \in WorkResults \cup ReadResults : Report(c, res)
  \/ EnvMove
  \/ \E f \in inflight, out \in {"landed", "red", "ack"}, n \in 1..2 : EdgeResult(f, out, n)
  \/ \E p \in Prods, choice \in {"land", "fix", "retry"} : Answer(p, choice)

Spec == Init /\ [][Next]_vars

\* Liveness: weak fairness on the tick (Snap, then Tick with any answer).
FairSpec == Spec /\ WF_vars(Snap) /\ WF_vars(\E a \in AnswersFor(snap), due \in BOOLEAN : Tick(a, due))

-----------------------------------------------------------------------------
(* Invariants, named as the design names its guarantees                   *)

ProdRec == [st : PStates, att : 0..MaxAtt, head : 0..MaxAtt, author : Rows \cup {NoR},
            bounces : 0..MaxBounce, prio : {"normal", "blocker"}, review : 0..(MaxAtt + MaxArrive + 1),
            ncut : 0..K, red : BOOLEAN]
ConsRec == [st : {"none", "ready", "working", "done", "fail"}, kind : {"-", "work", "read"},
            row : Rows \cup {NoR}, att : 0..MaxAtt, res : {"-", "ok", "fail", "fault", "fix"},
            review : 0..(MaxAtt + MaxArrive + 1)]
TypeOK ==
  /\ w.ps \in [Prods -> ProdRec]
  /\ w.cs \in [CIds -> ConsRec]
  /\ w.up \in [Rows -> BOOLEAN]
  /\ bus \in [Rows -> BOOLEAN]
  /\ admitted \in [Prods -> BOOLEAN]
  /\ lapsed \subseteq CIds
  /\ w.rg \in [Rows -> 0..MaxEnv]
  /\ w.sh \in [Streams -> BOOLEAN]
  /\ w.pol \in BOOLEAN
  /\ w.ob \subseteq {p \in Prods : w.ps[p].st = "merging"}
  /\ onmain \subseteq Prods
  /\ phase \in {"idle", "snapped"}
  /\ arrived \in 0..MaxArrive
  /\ processed \in 0..MaxArrive
  /\ env \in 0..MaxEnv
  /\ rdy \in [Prods -> 0..UndealtTicks]
  /\ ju \subseteq Prods
  \* the deal order holds every working consumer exactly once
  /\ \A c \in CIds : (w.cs[c].st = "working") <=> \E i \in 1..Len(w.tko) : w.tko[i] = c
  /\ \A i, j \in 1..Len(w.tko) : i # j => w.tko[i] # w.tko[j]

(* Guarantee 1: no producer is dealt twice. At most one live work         *)
(* consumer, of the current attempt, and only while the producer works.   *)
NeverDealtTwice ==
  \A p \in Prods :
    /\ Cardinality({c \in LiveOf(w, p) : w.cs[c].kind = "work"}) <= 1
    /\ \A c \in LiveOf(w, p) :
         w.cs[c].kind = "work" => (w.ps[p].st = "working" /\ w.cs[c].att = w.ps[p].att)

(* Guarantee 2: no row is over width; working consumers only.             *)
WidthKept == \A r \in Rows : Cardinality(Working(w, r)) <= Width[r]

(* The deal is the take (defect 52): no consumer is ever dealt ready.     *)
DealIsTake == \A c \in CIds : w.cs[c].st # "ready"

(* Nothing sits unworked (intent rule 33, directive 65): a producer the   *)
(* ticks left ready with room UndealtTicks times in a row has an open     *)
(* undealt judgement naming it.                                           *)
DealtWithinKTicks == \A p \in Prods : rdy[p] >= UndealtTicks => p \in ju

(* Guarantee 8: ready never depends on row load. The ready rule gives the *)
(* same answer for the store as it is and for the store with every        *)
(* consumer removed and every row down.                                   *)
Unloaded(W) == [W EXCEPT !.cs = [c \in CIds |-> NoCons], !.tko = <<>>, !.up = [r \in Rows |-> FALSE]]
ReadyNeverFlipsWithLoad == \A p \in Prods : ResolveOK(w, p) = ResolveOK(Unloaded(w), p)

(* Escalation is row choice, never a tier write: every live consumer is  *)
(* on a row that serves its card's tier.                                  *)
NeverBelowTier == \A c \in CIds : Live(w, c) => Serves(w.cs[c].row, c[1])

(* Guarantee 7, the carry: the inbox is in arrival order and nothing is  *)
(* lost (every item that arrived is consumed or still in the inbox).     *)
CarryFIFO ==
  /\ \A i, j \in 1..Len(inbox) : i < j => inbox[i].stamp < inbox[j].stamp
  /\ processed + Len(inbox) = arrived

(* An edge request is dispatched only after every op in its After was    *)
(* accepted: an in-flight stop names a withdrawn consumer; an in-flight  *)
(* land names a producer merging at that head, unless the coordinator    *)
(* answered its land judgement (land or fix) between the fold and the    *)
(* tick that planned it: a land request has no After, so a tick planned  *)
(* on a view from before the answer may ask one landing more. Its result *)
(* moves nothing (ItemGroup and LandJudged need the producer merging at  *)
(* that head); a green one can still merge a head the coordinator sent  *)
(* to fix, which the base keeps (LANDING.md, "Known gaps").              *)
EdgeAfterAccepted ==
  \A f \in inflight :
    /\ f.id[2] = "stop" => w.cs[f.id[3]].st = "fail"
    /\ f.id[2] \in {"land", "promote"} => \/ w.ps[f.id[3][1]].st = "merging" /\ w.ps[f.id[3][1]].head = f.id[4]
                          \/ w.ps[f.id[3][1]].st = "landed" \/ w.ps[f.id[3][1]].red
                          \/ w.ps[f.id[3][1]].att > f.id[4]

(* Landed means on main (LANDING.md, 2026-11-08): a producer is recorded *)
(* landed only when its merge is on main.                                 *)
LandedOnMain == \A p \in Prods : w.ps[p].st = "landed" => p \in onmain

(* The tick's checks (TickChecks), each kept by the last tick:           *)
(* OneGroupPerCard and GroupWhole (the group, list item 4);              *)
(* NeverHalfApplied (guarantee 5) and RefusalChangesNothing; RefusedOnlyByRace *)
(* (the steps pre-check every store rule, item 5); BudgetKept (item 6);  *)
(* BlockerPreemptsInOneTick (guarantee 4) and PreemptYoungest;           *)
(* PolicyFailsToDefault (guarantee 10); AfterKept and AfterNeverHolds    *)
(* (guarantee 3); MergingCovered (intent rule 32: after a tick, every    *)
(* merging producer of the view it planned on is in a land request in    *)
(* flight or planned for its stream, under a land judgement as the tick  *)
(* saw them (its snapshot's, jsnap, and its own), or held by a merging    *)
(* after target); NoDealWithoutPolicy (defect 53: no preemption, Start  *)
(* or read while no active policy answers, and the no-policy judgement); *)
(* ResolveFromState (section 64b: every producer the view's state makes   *)
(* ready is resolved, whatever landed its needs);                         *)
(* VerdictHasItsRead (a yes has a done ok read of the review and no fix  *)
(* read, a fix has a done fix read: a faulted read is neither).          *)
OneGroupPerCard          == "OneGroupPerCard" \notin last.bad
GroupWhole               == "GroupWhole" \notin last.bad
NeverHalfApplied         == "NeverHalfApplied" \notin last.bad
RefusalChangesNothing    == "RefusalChangesNothing" \notin last.bad
RefusedOnlyByRace        == "RefusedOnlyByRace" \notin last.bad
BudgetKept               == "BudgetKept" \notin last.bad
BlockerPreemptsInOneTick == "BlockerPreemptsInOneTick" \notin last.bad
PreemptYoungest          == "PreemptYoungest" \notin last.bad
PolicyFailsToDefault     == "PolicyFailsToDefault" \notin last.bad
AfterKept                == "AfterKept" \notin last.bad
AfterNeverHolds          == "AfterNeverHolds" \notin last.bad
MergingCovered           == "MergingCovered" \notin last.bad
NoDealWithoutPolicy      == "NoDealWithoutPolicy" \notin last.bad
ResolveFromState         == "ResolveFromState" \notin last.bad
VerdictHasItsRead        == "VerdictHasItsRead" \notin last.bad
NeverDealtAbsent         == "NeverDealtAbsent" \notin last.bad
NeverDealtUnadmitted     == "NeverDealtUnadmitted" \notin last.bad
AdmissionShown           == "AdmissionShown" \notin last.bad
ExpiryNeverSilent        == "ExpiryNeverSilent" \notin last.bad
LapsedReleased           == "LapsedReleased" \notin last.bad

(* Liveness, guarantee 8's second half and LAYER-2's nothing-stalls:    *)
(* a ready producer with room on an open serving row (under width) is     *)
(* eventually dealt, unless the room goes. The model's bounds (attempts,  *)
(* slots) are exempt, as in CardMachine, and so is a run with no active   *)
(* policy (the gate deals nothing).                                       *)
RoomFor(p) == w.pol /\ RoomIn(w, p) /\ admitted[p]
ReadyDealtWhenRoom ==
  \A p \in Prods : (w.ps[p].st = "ready" /\ RoomFor(p)) ~> (w.ps[p].st # "ready" \/ ~RoomFor(p))

(* A review whose reads all faulted is read again: settled with every     *)
(* done read of the review faulted and room for a read (an open serving  *)
(* row other than the author's, under width; the stream not held; a slot *)
(* left; an active policy), it eventually has a read that is not faulted, *)
(* or leaves review, unless the room goes.                                *)
OnlyFaulted(p) ==
  /\ w.ps[p].st = "review" /\ NoLive(w, p)
  /\ \E c \in CurReads(w, p) : Faulted(w, c)
  /\ \A c \in CurReads(w, p) : w.cs[c].st = "fail" \/ Faulted(w, c)
ReadRoomFor(p) ==
  /\ w.pol /\ ~w.sh[Stream[p]] /\ w.ps[p].ncut < K
  /\ \E r \in ServingOpen(w, p) : r # w.ps[p].author /\ Cardinality(Load(w, r)) < Width[r]
FaultedReadReadAgain ==
  \A p \in Prods : (OnlyFaulted(p) /\ ReadRoomFor(p)) ~> (~OnlyFaulted(p) \/ ~ReadRoomFor(p))

-----------------------------------------------------------------------------
(* Reachability: each is expected to be VIOLATED (reach/Steps*.cfg); each *)
(* violation shows a step or path is really taken.                        *)

StepsNeverPreempted   == "pre" \notin last.tags
StepsNeverRefused     == "refused" \notin last.tags
StepsNeverSplit       == "split" \notin last.tags
StepsNeverCarried     == "carry" \notin last.tags
StepsNeverLandRequested == "landreq" \notin last.tags
StepsNeverPromoteRequested == "promreq" \notin last.tags
StepsNeverOnBase      == w.ob = {}
StepsNeverLandedOffMainBase == \A p \in Prods : ~MainBase[Stream[p]] => w.ps[p].st # "landed"
StepsNeverAfterHeld   == "afterheld" \notin last.tags
StepsNeverStopAcked   == "ack" \notin last.tags
StepsNeverEscalated   == "escal" \notin last.tags
StepsNeverResolved    == \A p \in Prods : w.ps[p].st # "ready"
StepsNeverDealt       == \A c \in CIds : w.cs[c].kind # "work"
StepsNeverReadAsked   == \A c \in CIds : w.cs[c].kind # "read"
StepsNeverMerged      == \A p \in Prods : w.ps[p].st # "merging"
StepsNeverFixed       == \A p \in Prods : w.ps[p].st = "ready" => w.ps[p].bounces = 0
StepsNeverLanded      == \A p \in Prods : w.ps[p].st # "landed"
StepsNeverRed         == \A p \in Prods : ~w.ps[p].red
StepsNeverLandJudged  == "judged" \notin last.tags
StepsNeverNeedReleased == ~\E p \in Prods : Needs[p] # {} /\ w.ps[p].st # "waiting"
StepsNeverUnserved    == ~\E j \in js : j.kind = "unserved"
StepsNeverCannotRead  == ~\E j \in js : j.kind = "cannot-read"
StepsNeverPolicyJudged == ~\E j \in js : j.kind = "policy"
StepsNeverGated       == ~\E j \in js : j.kind = "no-policy"
StepsNeverBothStarted == ~(w.ps["p"].st \in Started /\ w.ps["q"].st \in Started)
StepsNeverHandLanded  == "handland" \notin last.tags
StepsNeverUndealt     == ju = {}
StepsNeverAbsent      == "absent" \notin last.tags
StepsNeverUnadmitted  == ~\E j \in js : j.kind = "admission"
StepsNeverReopened    == "reopened" \notin last.tags
StepsNeverReleased    == "released" \notin last.tags
StepsNeverReadAgain   == ~\E p \in Prods : \E c, d \in CurReads(w, p) :
                           Faulted(w, c) /\ d # c /\ w.cs[d].st \in {"working", "done"} /\ ~Faulted(w, d)

=============================================================================
