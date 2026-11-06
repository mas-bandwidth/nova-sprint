---------------------------- MODULE CardMachine ----------------------------
(***************************************************************************)
(* Layer 2 of nova-sprint v2: the card machine                             *)
(* (docs/LAYER-2-CARD-MACHINE.md), producer and consumer shape.            *)
(*                                                                         *)
(* Each action is one accepted event of the producer, consumer and row     *)
(* tables. Its guard is the design's from-state plus its conditions; an    *)
(* event that does not match is not enabled (the Go code refuses it with a *)
(* value, which changes nothing). The model is the reachable state space   *)
(* of accepted moves.                                                      *)
(*                                                                         *)
(* Producers p1 and p2 and the sentinel s. Scenario "deps": p2 needs p1, s *)
(* needs p2. Scenario "pre": p1 and p2 are independent, p2 names p1 in     *)
(* after, s needs both; it exists because a blocker can only preempt the   *)
(* consumer of another producer that is live at the same time, which      *)
(* needs a pair that can both be in flight. A sentinel has no consumers.   *)
(*                                                                         *)
(* Consumers are slots: producer p's i-th cut consumer is <<p, i>>, for i  *)
(* in 1..K; an uncut slot is in state "none". Start and AskRead cut the    *)
(* next slot. Two rows, r1 and r2, with state, flaps, width, held and a    *)
(* constant tier set; one stream with a held flag.                         *)
(*                                                                         *)
(* Heads are 1..MaxHead; 0 is "no head" and also the base commit, so an ok *)
(* work report's head (in 1..MaxHead) always differs from base_sha.        *)
(*                                                                         *)
(* Bounds that are the model's and not the design's: K consumer slots per  *)
(* producer; attempt stops at MaxAttempt and bounces at MaxBounce (Start,  *)
(* Broken and ReadFix are not enabled past them); flaps stop at 1; scores  *)
(* are 0..1; widths 1..2; priority is normal or blocker (the other levels  *)
(* only order within policy). The environment and coordinator moves       *)
(* (Rescore, SetPriority, Edit, the row table, the stream's hold) share a *)
(* budget of MaxEnv per behaviour; every card and consumer move is        *)
(* unbounded but for the slots. Rows start up, width 1, not held.         *)
(*                                                                         *)
(* Left out, because no guard reads them: branch, base_sha (always 0),     *)
(* prior_head, reason, brief, cost, priced, the by of Replace. Edit writes *)
(* only the brief, so in the model it changes nothing but the history.    *)
(*                                                                         *)
(* last is a history variable: the event, the producer, consumer and row   *)
(* it names, and that producer and consumer as they were before the step. *)
(* The step invariants below read it.                                      *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Scenario,    \* "deps" or "pre"
  K,           \* consumer slots per producer
  MaxHead,     \* heads are 1..MaxHead
  MaxAttempt,  \* model bound: Start is not enabled at this attempt
  MaxBounce,   \* model bound: Broken and ReadFix are not enabled at this count
  MaxEnv       \* model bound: environment and coordinator moves per behaviour

Prods     == {"p1", "p2"}            \* producers with work
Sentinels == {"s"}
P         == Prods \cup Sentinels
Rows      == {"r1", "r2"}
CIds      == Prods \X (1..K)
ConsOf(p) == IF p \in Prods THEN {<<p, i>> : i \in 1..K} ELSE {}

Needs0 == IF Scenario = "deps"
          THEN [p \in P |-> CASE p = "p2" -> {"p1"} [] p = "s" -> {"p2"} [] OTHER -> {}]
          ELSE [p \in P |-> IF p = "s" THEN {"p1", "p2"} ELSE {}]
After  == IF Scenario = "deps"
          THEN [p \in P |-> {}]
          ELSE [p \in P |-> IF p = "p2" THEN {"p1"} ELSE {}]

Tier     == [p \in P |-> "pro"]
RowTiers == [r \in Rows |-> {"pro"}]
Serves(r, p) == Tier[p] \in RowTiers[r]

PStates == {"waiting", "ready", "working", "review", "merging", "landed", "fail", "blocked"}
PFinal  == {"landed", "fail"}
CStates == {"none", "ready", "working", "done", "fail"}
CFinal  == {"done", "fail"}
LiveSt  == {"ready", "working"}
Results == {"-", "ok", "fix", "fail", "fault"}
Prios   == {"normal", "blocker"}
None    == "none"
NoP     == "-"
NoR     == "-"
NoC     == <<"-", 0>>
Heads   == 1..MaxHead

NoCons == [st |-> "none", kind |-> "-", row |-> NoR, att |-> 0, head |-> 0,
           res |-> "-", author |-> None, rseq |-> 0]
NewProd(p) == [st |-> "waiting", att |-> 0, head |-> 0, author |-> None, bounces |-> 0,
               score |-> 0, prio |-> "normal", needs |-> Needs0[p], rseq |-> 0, ncut |-> 0]
NoProd == [NewProd("p1") EXCEPT !.needs = {}]

VARIABLES
  ps,    \* producer -> [st, att, head, author, bounces, score, prio, needs, rseq, ncut]
  cs,    \* consumer slot -> [st, kind, row, att, head, res, author, rseq]
  rs,    \* row -> [st, flaps, width, held]
  held,  \* the stream's held flag
  last,  \* history: [ev, p, c, r, pp, pc]
  env    \* environment and coordinator moves so far (bounded by MaxEnv)

vars == <<ps, cs, rs, held, last, env>>

Live(c)      == cs[c].st \in LiveSt
LiveOf(p)    == {c \in ConsOf(p) : Live(c)}
NoLive(p)    == LiveOf(p) = {}
WorkingOn(r) == {c \in CIds : cs[c].st = "working" /\ cs[c].row = r}
RowOpen(r)   == rs[r].st = "up" /\ ~rs[r].held
NeedsLanded(p) == \A n \in ps[p].needs : ps[n].st = "landed"
NonFinal(p)  == ps[p].st \notin PFinal
\* c is p's work consumer of its current attempt.
IsCur(p, c)  == c \in ConsOf(p) /\ cs[c].kind = "work" /\ cs[c].att = ps[p].att /\ cs[c].st # "none"
NextSlot(p)  == <<p, ps[p].ncut + 1>>

\* The needs graph with p's needs replaced by ns has no cycle.
RECURSIVE Reach(_, _, _)
Reach(g, frontier, seen) ==
  IF frontier = {} THEN seen
  ELSE LET nxt == UNION {g[x] : x \in frontier} \ seen
       IN Reach(g, nxt, seen \cup nxt)
Acyclic(g) == \A p \in P : p \notin Reach(g, g[p], g[p])
NeedsGraph == [p \in P |-> ps[p].needs]

Init ==
  /\ ps   = [p \in P |-> NewProd(p)]
  /\ cs   = [c \in CIds |-> NoCons]
  /\ rs   = [r \in Rows |-> [st |-> "up", flaps |-> 0, width |-> 1, held |-> FALSE]]
  /\ held = FALSE
  /\ last = [ev |-> "Init", p |-> NoP, c |-> NoC, r |-> NoR, pp |-> NoProd, pc |-> NoCons]
  /\ env  = 0

L(ev, p, c, r) == [ev |-> ev, p |-> p, c |-> c, r |-> r,
                   pp |-> IF p = NoP THEN NoProd ELSE ps[p],
                   pc |-> IF c = NoC THEN NoCons ELSE cs[c]]

\* A producer event that writes producer p only.
PStep(ev, p, c, rec) ==
  /\ ps'   = [ps EXCEPT ![p] = rec]
  /\ last' = L(ev, p, c, NoR)
  /\ UNCHANGED <<cs, rs, held>>

\* A consumer event that writes consumer c only.
CStep(ev, c, r, rec) ==
  /\ cs'   = [cs EXCEPT ![c] = rec]
  /\ last' = L(ev, NoP, c, r)
  /\ UNCHANGED <<ps, rs, held>>

RStep(ev, r, rec) ==
  /\ rs'   = [rs EXCEPT ![r] = rec]
  /\ last' = L(ev, NoP, NoC, r)
  /\ UNCHANGED <<ps, cs, held>>

-----------------------------------------------------------------------------
(* Producer events                                                         *)

Resolve(p) ==
  /\ p \in Prods
  /\ ps[p].st = "waiting"
  /\ NeedsLanded(p)
  /\ ~held
  /\ PStep("Resolve", p, NoC, [ps[p] EXCEPT !.st = "ready"])

Park(p) ==
  /\ ps[p].st = "ready"
  /\ held
  /\ PStep("Park", p, NoC, [ps[p] EXCEPT !.st = "waiting"])

Reached(p) ==
  /\ p \in Sentinels
  /\ ps[p].st = "waiting"
  /\ NeedsLanded(p)
  /\ PStep("Reached", p, NoC, [ps[p] EXCEPT !.st = "landed"])

\* Start: p to working, attempt+1, author cleared, and a work consumer cut
\* on row r in state ready.
StartCut(p, r) ==
  [st |-> "ready", kind |-> "work", row |-> r, att |-> ps[p].att + 1, head |-> 0,
   res |-> "-", author |-> None, rseq |-> 0]
StartGuard(p, r) ==
  /\ p \in Prods
  /\ ps[p].st = "ready"
  /\ RowOpen(r)
  /\ ~held
  /\ Serves(r, p)
  /\ ps[p].att < MaxAttempt
  /\ ps[p].ncut < K

Start(p, r) ==
  /\ StartGuard(p, r)
  /\ ps'   = [ps EXCEPT ![p].st = "working", ![p].att = @ + 1, ![p].author = None,
                        ![p].ncut = @ + 1]
  /\ cs'   = [cs EXCEPT ![NextSlot(p)] = StartCut(p, r)]
  /\ last' = L("Start", p, NextSlot(p), r)
  /\ UNCHANGED <<rs, held>>

Finished(p, c) ==
  /\ ps[p].st = "working"
  /\ IsCur(p, c)
  /\ cs[c].st = "done"
  /\ cs[c].res = "ok"
  /\ PStep("Finished", p, c, [ps[p] EXCEPT !.st = "review", !.head = cs[c].head,
                                           !.author = cs[c].row, !.rseq = @ + 1])

\* A pushed head is kept on the producer; a report with no head keeps p's.
KeptHead(p, c) == IF cs[c].head # 0 THEN cs[c].head ELSE ps[p].head

Broken(p, c) ==
  /\ ps[p].st = "working"
  /\ IsCur(p, c)
  /\ cs[c].st = "done"
  /\ cs[c].res = "fail"
  /\ ps[p].bounces < MaxBounce
  /\ PStep("Broken", p, c, [ps[p] EXCEPT !.st = "ready", !.bounces = @ + 1,
                                         !.head = KeptHead(p, c)])

Faulted(p, c) ==
  /\ ps[p].st = "working"
  /\ IsCur(p, c)
  /\ cs[c].st = "done"
  /\ cs[c].res = "fault"
  /\ PStep("Faulted", p, c, [ps[p] EXCEPT !.st = "ready", !.head = KeptHead(p, c)])

AskRead(p, r) ==
  /\ p \in Prods
  /\ ps[p].st = "review"
  /\ RowOpen(r)
  /\ ~held
  /\ r # ps[p].author
  /\ Serves(r, p)
  /\ ps[p].ncut < K
  /\ ps'   = [ps EXCEPT ![p].ncut = @ + 1]
  /\ cs'   = [cs EXCEPT ![NextSlot(p)] =
                [st |-> "ready", kind |-> "read", row |-> r, att |-> ps[p].att,
                 head |-> ps[p].head, res |-> "-", author |-> ps[p].author,
                 rseq |-> ps[p].rseq]]
  /\ last' = L("AskRead", p, NextSlot(p), r)
  /\ UNCHANGED <<rs, held>>

\* Every read consumer of p at p.head is done or fail.
ReadsSettled(p) ==
  \A c \in ConsOf(p) :
    (cs[c].kind = "read" /\ cs[c].st # "none" /\ cs[c].head = ps[p].head)
      => cs[c].st \in CFinal

\* A read of the current head, done ok, cut after p last entered review
\* (Rules added from the promise reviews: no yes on zero yes).
YesRead(p) ==
  \E c \in ConsOf(p) :
    /\ cs[c].kind = "read"
    /\ cs[c].st = "done"
    /\ cs[c].res = "ok"
    /\ cs[c].head = ps[p].head
    /\ cs[c].rseq = ps[p].rseq

ReadYes(p) ==
  /\ ps[p].st = "review"
  /\ ReadsSettled(p)
  /\ NoLive(p)
  /\ YesRead(p)
  /\ PStep("ReadYes", p, NoC, [ps[p] EXCEPT !.st = "merging"])

ReadFix(p) ==
  /\ ps[p].st = "review"
  /\ ReadsSettled(p)
  /\ NoLive(p)
  /\ ps[p].bounces < MaxBounce
  /\ PStep("ReadFix", p, NoC, [ps[p] EXCEPT !.st = "ready", !.bounces = @ + 1])

\* Soft order: p lands only when no producer it names in after is merging.
AfterClear(p) == \A a \in After[p] : ps[a].st # "merging"

Landed(p, h) ==
  /\ ps[p].st = "merging"
  /\ h = ps[p].head
  /\ AfterClear(p)
  /\ PStep("Landed", p, NoC, [ps[p] EXCEPT !.st = "landed"])

Red(p, h) ==
  /\ ps[p].st = "merging"
  /\ h = ps[p].head
  /\ PStep("Red", p, NoC, [ps[p] EXCEPT !.st = "review", !.rseq = @ + 1])

Released(p, c) ==
  /\ ps[p].st = "working"
  /\ IsCur(p, c)
  /\ cs[c].st = "fail"
  /\ PStep("Released", p, c, [ps[p] EXCEPT !.st = "ready"])

Cancel(p) ==
  /\ NonFinal(p)
  /\ NoLive(p)
  /\ PStep("Cancel", p, NoC, [ps[p] EXCEPT !.st = "fail"])

Replace(p, by) ==
  /\ NonFinal(p)
  /\ NoLive(p)
  /\ PStep("Replace", p, NoC, [ps[p] EXCEPT !.st = "fail"])

NeedFailed(p, n) ==
  /\ NonFinal(p)
  /\ n \in ps[p].needs
  /\ ps[n].st = "fail"
  /\ NoLive(p)
  /\ PStep("NeedFailed", p, NoC, [ps[p] EXCEPT !.st = "fail"])

Waive(p, n) ==
  /\ ps[p].st = "waiting"
  /\ n \in ps[p].needs
  /\ ps[n].st = "fail"
  /\ PStep("Waive", p, NoC, [ps[p] EXCEPT !.needs = @ \ {n}])

Relink(p, n, to) ==
  /\ ps[p].st = "waiting"
  /\ n \in ps[p].needs
  /\ ps[n].st = "fail"
  /\ to \in P \ {p}
  /\ NonFinal(to)
  /\ Acyclic([NeedsGraph EXCEPT ![p] = (@ \ {n}) \cup {to}])
  /\ PStep("Relink", p, NoC, [ps[p] EXCEPT !.needs = (@ \ {n}) \cup {to}])

\* Edit writes the brief, which the model leaves out: only the history moves.
Edit(p) ==
  /\ NonFinal(p)
  /\ NoLive(p)
  /\ PStep("Edit", p, NoC, ps[p])

Rescore(p, s) ==
  /\ NonFinal(p)
  /\ s # ps[p].score
  /\ PStep("Rescore", p, NoC, [ps[p] EXCEPT !.score = s])

SetPriority(p, l) ==
  /\ p \in Prods
  /\ NonFinal(p)
  /\ l # ps[p].prio
  /\ PStep("SetPriority", p, NoC, [ps[p] EXCEPT !.prio = l])

Block(p) ==
  /\ \/ ps[p].st \in {"ready", "review"}
     \/ ps[p].st = "working" /\ \E c \in ConsOf(p) : IsCur(p, c) /\ cs[c].st = "fail"
  /\ NoLive(p)
  /\ PStep("Block", p, NoC, [ps[p] EXCEPT !.st = "blocked"])

Unblock(p) ==
  /\ ps[p].st = "blocked"
  /\ PStep("Unblock", p, NoC, [ps[p] EXCEPT !.st = "waiting"])

-----------------------------------------------------------------------------
(* Consumer events                                                         *)

\* The store's state write refuses a move into working when the row already
\* has as many working consumers as its width; ready ones are not counted.
Take(c, r) ==
  /\ cs[c].st = "ready"
  /\ cs[c].row = r
  /\ Cardinality(WorkingOn(r)) < rs[r].width
  /\ CStep("Take", c, r, [cs[c] EXCEPT !.st = "working"])

\* A work report: ok carries a pushed head (never the base, 0); fail and
\* fault may carry one or none. A read report carries the producer's head.
ReportHeads(c, res) ==
  IF cs[c].kind = "read" THEN {ps[c[1]].head}
  ELSE IF res = "ok" THEN Heads ELSE 0..MaxHead
ReportResults(c) ==
  IF cs[c].kind = "read" THEN {"ok", "fix", "fail", "fault"} ELSE {"ok", "fail", "fault"}

Report(c, r, res, h) ==
  /\ cs[c].st = "working"
  /\ cs[c].row = r
  /\ res \in ReportResults(c)
  /\ h \in ReportHeads(c, res)
  /\ CStep("Report", c, r, [cs[c] EXCEPT !.st = "done", !.res = res, !.head = h])

Withdraw(c) ==
  /\ Live(c)
  /\ CStep("Withdraw", c, NoR, [cs[c] EXCEPT !.st = "fail"])

\* Blocker preemption (Rules the tables encode): with no serving open row
\* below its width, policy withdraws a live consumer of a serving row and,
\* in the same plan, Start deals the blocker to that row. Which consumer
\* (the youngest) is policy's; the model lets it be any.
Preempt(b, c, r) ==
  /\ StartGuard(b, r)
  /\ ps[b].prio = "blocker"
  /\ \A r2 \in Rows : (Serves(r2, b) /\ RowOpen(r2)) => Cardinality(WorkingOn(r2)) >= rs[r2].width
  /\ Live(c)
  /\ cs[c].row = r
  /\ c \notin ConsOf(b)
  /\ ps'   = [ps EXCEPT ![b].st = "working", ![b].att = @ + 1, ![b].author = None,
                        ![b].ncut = @ + 1]
  /\ cs'   = [cs EXCEPT ![c].st = "fail", ![NextSlot(b)] = StartCut(b, r)]
  /\ last' = L("Preempt", b, c, r)
  /\ UNCHANGED <<rs, held>>

-----------------------------------------------------------------------------
(* Row events and the stream's hold                                        *)

Seen(r) ==
  /\ rs[r].st = "down"
  /\ RStep("Seen", r, [rs[r] EXCEPT !.st = "up"])

Gone(r) ==
  /\ rs[r].st = "up"
  /\ rs[r].flaps < 1
  /\ RStep("Gone", r, [rs[r] EXCEPT !.st = "down", !.flaps = @ + 1])

\* Layer 1 refuses a width below the row's working count.
Width(r, n) ==
  /\ n # rs[r].width
  /\ n >= Cardinality(WorkingOn(r))
  /\ RStep("Width", r, [rs[r] EXCEPT !.width = n])

Hold(r) ==
  /\ ~rs[r].held
  /\ RStep("Hold", r, [rs[r] EXCEPT !.held = TRUE])

Unhold(r) ==
  /\ rs[r].held
  /\ RStep("Unhold", r, [rs[r] EXCEPT !.held = FALSE])

\* The stream's hold, with Park raised in the same op for every ready
\* producer (LESSONS 2.20).
HoldStream ==
  /\ ~held
  /\ held' = TRUE
  /\ ps'   = [p \in P |-> IF ps[p].st = "ready" THEN [ps[p] EXCEPT !.st = "waiting"] ELSE ps[p]]
  /\ last' = L("HoldStream", NoP, NoC, NoR)
  /\ UNCHANGED <<cs, rs>>

UnholdStream ==
  /\ held
  /\ held' = FALSE
  /\ last' = L("UnholdStream", NoP, NoC, NoR)
  /\ UNCHANGED <<ps, cs, rs>>

\* The card and consumer moves.
CardMove ==
  \/ \E p \in P :
       \/ Resolve(p) \/ Park(p) \/ Reached(p) \/ ReadYes(p) \/ ReadFix(p)
       \/ Cancel(p) \/ Block(p) \/ Unblock(p)
       \/ \E h \in Heads : Landed(p, h) \/ Red(p, h)
       \/ \E r \in Rows : Start(p, r) \/ AskRead(p, r)
       \/ \E c \in CIds : Finished(p, c) \/ Broken(p, c) \/ Faulted(p, c) \/ Released(p, c)
       \/ \E q \in P \ {p} : Replace(p, q)
       \/ \E n \in P : NeedFailed(p, n) \/ Waive(p, n)
       \/ \E n, to \in P : Relink(p, n, to)
       \/ \E c \in CIds, r \in Rows : Preempt(p, c, r)
  \/ \E c \in CIds :
       \/ Withdraw(c)
       \/ \E r \in Rows :
            \/ Take(c, r)
            \/ \E res \in Results, h \in 0..MaxHead : Report(c, r, res, h)

\* The environment and coordinator moves, on the MaxEnv budget.
EnvMove ==
  \/ \E p \in P :
       \/ Edit(p)
       \/ \E s \in 0..1 : Rescore(p, s)
       \/ \E l \in Prios : SetPriority(p, l)
  \/ \E r \in Rows :
       \/ Seen(r) \/ Gone(r) \/ Hold(r) \/ Unhold(r)
       \/ \E n \in 1..2 : Width(r, n)
  \/ HoldStream \/ UnholdStream

\* Every producer final: the run is over (any other deadlock is reported).
Terminating == (\A p \in P : ps[p].st \in PFinal) /\ UNCHANGED vars

Next ==
  \/ CardMove /\ UNCHANGED env
  \/ env < MaxEnv /\ EnvMove /\ env' = env + 1
  \/ Terminating

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Invariants                                                              *)

ProdRec == [st : PStates, att : 0..MaxAttempt, head : 0..MaxHead, author : Rows \cup {None},
            bounces : 0..MaxBounce, score : 0..1, prio : Prios, needs : SUBSET P,
            rseq : 0..(K + MaxAttempt), ncut : 0..K]
ConsRec == [st : CStates, kind : {"-", "work", "read"}, row : Rows \cup {NoR},
            att : 0..MaxAttempt, head : 0..MaxHead, res : Results,
            author : Rows \cup {None}, rseq : 0..(K + MaxAttempt)]

TypeOK ==
  /\ ps \in [P -> ProdRec]
  /\ cs \in [CIds -> ConsRec]
  /\ rs \in [Rows -> [st : {"up", "down"}, flaps : 0..1, width : 1..2, held : BOOLEAN]]
  /\ held \in BOOLEAN
  /\ env \in 0..MaxEnv
  /\ last.p \in P \cup {NoP}
  /\ last.r \in Rows \cup {NoR}
  /\ last.pp \in ProdRec
  /\ last.pc \in ConsRec

(* A producer never has a holder: the holder lives on the consumer only,  *)
(* so every cut consumer names a row and belongs to a producer with work. *)
(* Structural (producers have no holder column); stated anyway.           *)
ProducerNeverHeld ==
  \A c \in CIds : cs[c].st # "none" => (cs[c].row \in Rows /\ c[1] \in Prods)

(* The cut slots of p are exactly 1..ncut.                                *)
SlotsDense == \A p \in Prods : \A i \in 1..K : (cs[<<p, i>>].st # "none") <=> (i <= ps[p].ncut)

(* working <=> one work consumer. The design's "working exactly when it   *)
(* has one live work consumer" cannot hold as written: after Report and   *)
(* before Finished, Broken or Faulted, and after Withdraw and before      *)
(* Released or Block, the producer is working with no live consumer. The  *)
(* form the tables keep: at most one live work consumer; a live work      *)
(* consumer is the current attempt's and its producer is working; and a   *)
(* working producer has its current attempt's work consumer.              *)
WorkingOneWorkConsumer ==
  \A p \in Prods :
    /\ Cardinality({c \in LiveOf(p) : cs[c].kind = "work"}) <= 1
    /\ \A c \in LiveOf(p) : cs[c].kind = "work" => (ps[p].st = "working" /\ IsCur(p, c))
    /\ ps[p].st = "working" => \E c \in ConsOf(p) : IsCur(p, c)

(* A live read consumer only in review, of the producer's current head.   *)
ReadOnlyInReview ==
  \A p \in Prods : \A c \in LiveOf(p) :
    cs[c].kind = "read" => (ps[p].st = "review" /\ cs[c].head = ps[p].head)

ReaderNeverAuthor ==
  \A p \in Prods : \A c \in LiveOf(p) : cs[c].kind = "read" => cs[c].row # ps[p].author

WidthKept == \A r \in Rows : Cardinality(WorkingOn(r)) <= rs[r].width

(* A ready consumer is never refused Take while its row's working count  *)
(* is below width: ready ones are not counted (LESSONS 3.3).              *)
TakeBelowWidth ==
  \A c \in CIds :
    (cs[c].st = "ready" /\ Cardinality(WorkingOn(cs[c].row)) < rs[cs[c].row].width)
      => ENABLED Take(c, cs[c].row)

(* Final states never move: the producer and consumer a step names, if   *)
(* final before it, are unchanged after it.                               *)
FinalNeverMoves ==
  /\ (last.p # NoP /\ last.pp.st \in PFinal) => ps[last.p] = last.pp
  /\ (last.c # NoC /\ last.pc.st \in CFinal) => cs[last.c] = last.pc

(* attempt rises by one on Start (and the Start inside Preempt) of that   *)
(* producer, and on nothing else.                                         *)
AttemptOnlyOnStart ==
  last.p # NoP =>
    ps[last.p].att = last.pp.att + (IF last.ev \in {"Start", "Preempt"} THEN 1 ELSE 0)

RedNeverAdvances == last.ev = "Red" => ps[last.p].st \notin {"merging", "landed"}

FixKeepsScore == last.ev = "ReadFix" => ps[last.p].score = last.pp.score

(* A consumer's holder never changes once it is cut.                      *)
HolderNeverChanges == (last.c # NoC /\ last.pc.st # "none") => cs[last.c].row = last.pc.row

(* A producer event moved by a consumer accepts only the work consumer of *)
(* its current attempt.                                                   *)
OldAttemptNeverMoves ==
  last.ev \in {"Finished", "Broken", "Faulted", "Released"} =>
    (last.pc.kind = "work" /\ last.pc.att = last.pp.att)

(* Take and Report are accepted only from the consumer's holder.          *)
OnlyHolderActs == last.ev \in {"Take", "Report"} => last.r = last.pc.row

(* ReadYes fired with a done ok read of the current head, cut after the   *)
(* producer last entered review. ReadYes changes no consumer and keeps    *)
(* head and rseq, so the check reads the state after it.                  *)
ReadYesNeedsYes == last.ev = "ReadYes" => YesRead(last.p)

(* A fault spends no attempt and raises no bounce.                        *)
FaultSpendsNothing ==
  last.ev = "Faulted" => (ps[last.p].att = last.pp.att /\ ps[last.p].bounces = last.pp.bounces)

(* Block and Edit only with no live consumer (neither changes a consumer). *)
BlockNoLive == last.ev = "Block" => NoLive(last.p)
EditNoLive  == last.ev = "Edit" => NoLive(last.p)

NeedFailedTakesDependants ==
  \A p \in P : \A n \in ps[p].needs :
    (ps[n].st = "fail" /\ NonFinal(p)) => ENABLED NeedFailed(p, n)

(* A sentinel never has a consumer (structural: it has no slots), is      *)
(* never in a state that has one, and lands only by Reached, with every   *)
(* need landed.                                                           *)
SentinelShape ==
  /\ \A s \in Sentinels : ps[s].st \in {"waiting", "landed", "fail"}
  /\ (last.p \in Sentinels /\ last.pp.st # "landed" /\ ps[last.p].st = "landed")
       => (last.ev = "Reached" /\ NeedsLanded(last.p))

NeedsAcyclic == Acyclic(NeedsGraph)

(* Soft order: a landing never passes a merging after-target, and a       *)
(* merging producer whose after-targets are not merging can land.         *)
AfterKept ==
  /\ last.ev = "Landed" => AfterClear(last.p)
  /\ \A p \in P : (ps[p].st = "merging" /\ AfterClear(p)) => ENABLED Landed(p, ps[p].head)

-----------------------------------------------------------------------------
(* Reachability: these are expected to be VIOLATED (reach/*.cfg). Each    *)
(* violation shows that the passing run is not vacuous.                   *)

NeverLanded       == \A p \in Prods : ps[p].st # "landed"
NeverFixed        == last.ev # "ReadFix"
NeverRed          == last.ev # "Red"
NeverNeedFailed   == last.ev # "NeedFailed"
NeverBlocked      == \A p \in P : ps[p].st # "blocked"
NeverPreempted    == last.ev # "Preempt"
NeverSentinelReached == last.ev # "Reached"
NeverRelinked     == last.ev # "Relink"
(* A work consumer that reported fault, then a new Start of its producer. *)
NeverFaultRedealt ==
  ~(last.ev = "Start"
    /\ \E c \in ConsOf(last.p) : cs[c].kind = "work" /\ cs[c].res = "fault")

=============================================================================
