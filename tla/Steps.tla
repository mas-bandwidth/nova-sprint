------------------------------- MODULE Steps -------------------------------
(***************************************************************************)
(* Layer 3 of nova-sprint v3: the steps (docs/LAYER-3-STEPS.md), the tick  *)
(* as one atomic action, with multi-resource room (R50), AnyPaused         *)
(* NoDealBehindPause (R52), and multi-victim blocker preemption with       *)
(* non-sprint HeldOther headroom guard (R53).                              *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets, Sequences

CONSTANTS
  Scenario,   \* "pre", "deal", "land" or "burst"
  K,          \* consumer slots per producer
  MaxAtt,     \* model bound: Start is not enabled at this attempt
  MaxBounce,  \* model bound: a fail or fix report is not enabled at this count
  MaxEnv,     \* model bound: environment moves per behaviour
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
ZeroRes   == [cpu |-> 0, mem |-> 0]
UnitRes   == [cpu |-> 1, mem |-> 1]
TwoRes    == [cpu |-> 2, mem |-> 2]
ThreeRes  == [cpu |-> 3, mem |-> 3]
AddRes(a, b) == [cpu |-> a.cpu + b.cpu, mem |-> a.mem + b.mem]
FitsRes(cap, used, need) ==
  /\ cap.cpu - used.cpu >= need.cpu
  /\ cap.mem - used.mem >= need.mem
RowCap == [r \in Rows |-> IF r = "r1" THEN UnitRes
                          ELSE IF Scenario = "pre" THEN ThreeRes ELSE TwoRes]
RowTiers  == [r \in Rows |-> IF r = "r1" THEN {1} ELSE {1, 2}]
Rank      == [r \in Rows |-> IF r = "r1" THEN 1 ELSE 2]
MaxRank   == 2
EscalateAfter == 1
UndealtTicks  == 2
TierName(t)   == IF t = 1 THEN "t1" ELSE "t2"

Stream == [p \in Prods |-> CASE p = "p" -> "s1"
                             [] p = "q" -> IF LandLike THEN "s2" ELSE "s1"
                             [] OTHER   -> "s2"]
Tier    == [p \in Prods |-> IF p = "b" /\ Scenario = "deal" THEN 2 ELSE 1]
CardRes == [p \in Prods |-> IF p = "b" /\ Scenario = "pre" THEN TwoRes ELSE UnitRes]
ConsNeeds(kind, p) == IF kind = "read" THEN UnitRes ELSE CardRes[p]
UnservedAbout(p) ==
  IF CardRes[p] = UnitRes THEN TierName(Tier[p])
  ELSE TierName(Tier[p]) \o "+cpu:2+memory:2"

Needs  == [p \in Prods |-> IF p = "b" /\ Scenario = "deal" THEN {"p"} ELSE {}]
After  == [p \in Prods |-> IF p = "q" /\ LandLike THEN {"p"} ELSE {}]
Prio0  == [p \in Prods |-> IF p = "b" /\ Scenario = "pre" THEN "blocker" ELSE "normal"]
WorkResults == IF Scenario = "deal" THEN {"ok", "fail"} ELSE {"ok"}
ReadResults == IF Scenario = "deal" THEN {"ok", "fix", "fault"} ELSE {"ok"}
MayBounce(p) == Scenario = "deal" /\ p = "p"
MayFault(p)  == Scenario = "deal" /\ p = "p"
LandOutcomes == IF LandLike THEN {"landed", "red"} ELSE {"landed"}
MainBase == [s \in Streams |-> ~(LandLike /\ s = "s2")]

NoR       == "-"
PKeyMap   == [p \in Prods |-> <<p, 0>>]
PKey(p)   == PKeyMap[p]
CIds      == Prods \X (1..K)
ConsOfMap == [p \in Prods |-> {<<p, i>> : i \in 1..K}]
ConsOf(p) == ConsOfMap[p]
BaseReadKeys == [p \in Prods |-> {PKeyMap[p]} \cup ConsOfMap[p]]
Started   == {"working", "review", "merging"}
PStates   == {"waiting", "ready", "working", "review", "merging", "landed"}

NoCons == [st |-> "none", kind |-> "-", row |-> NoR, att |-> 0, res |-> "-", review |-> 0]
P0(p) ==
  IF LandLike
  THEN [st |-> "merging", att |-> 1, head |-> 1, author |-> "r1", bounces |-> 0,
        prio |-> Prio0[p], review |-> 1, ncut |-> 0, red |-> FALSE]
  ELSE [st |-> "waiting", att |-> 0, head |-> 0, author |-> NoR, bounces |-> 0,
        prio |-> Prio0[p], review |-> 0, ncut |-> 0, red |-> FALSE]
W0 == [ps     |-> [p \in Prods |-> P0(p)],
       cs     |-> [c \in CIds |-> NoCons],
       up     |-> [r \in Rows |-> ~(Scenario = "pre" /\ r = "r1")],
       rg     |-> [r \in Rows |-> 0],
       sh     |-> [s \in Streams |-> Scenario = "pre" /\ s = "s2"],
       tko    |-> <<>>,
       pol    |-> TRUE,
       ob     |-> {},
       paused |-> [r \in Rows |-> FALSE],
       other  |-> [r \in Rows |-> ZeroRes]]

Answers == {[rows |-> "r1"], [rows |-> "r2"], [rows |-> "failed"]}
Unasked == [rows |-> "failed"]
ZeroTally == [r \in Rows |-> ZeroRes]

VARIABLES
  w, snap, phase, inbox, inflight, js, jsnap, split,
  arrived, processed, env, last, rdy, ju, bus, admitted, onmain, lapsed

vars == <<w, snap, phase, inbox, inflight, js, jsnap, split, arrived, processed, env, last, rdy, ju, bus, admitted, onmain, lapsed>>
aux  == <<rdy, ju>>
Clean == [tags |-> {}, bad |-> {}]

-----------------------------------------------------------------------------
(* Reading a world W (the store or the view) and O(|tko|) room helpers     *)

Live(W, c)      == W.cs[c].st \in {"ready", "working"}
LiveOf(W, p)    == {c \in ConsOf(p) : Live(W, c)}
NoLive(W, p)    == LiveOf(W, p) = {}
Working(W, r)   == {c \in CIds : W.cs[c].st = "working" /\ W.cs[c].row = r}
Load(W, r)      == {c \in CIds : Live(W, c) /\ W.cs[c].row = r}

SlotNeeds(W, c) == ConsNeeds(W.cs[c].kind, c[1])

RECURSIVE SumTko(_, _, _, _)
SumTko(W, r, drop, i) ==
  IF i > Len(W.tko) THEN ZeroRes
  ELSE LET c    == W.tko[i]
           rest == SumTko(W, r, drop, i + 1)
       IN IF W.cs[c].row = r /\ c \notin drop
          THEN AddRes(rest, SlotNeeds(W, c))
          ELSE rest

SprintUsed(W, r)    == SumTko(W, r, {}, 1)
UsedRes(W, r, t)    == AddRes(AddRes(W.other[r], SprintUsed(W, r)), t[r])
FitsMaxRes(r, need) == RowCap[r].cpu >= need.cpu /\ RowCap[r].mem >= need.mem
Serves(r, kind, p)  == Tier[p] \in RowTiers[r] /\ FitsMaxRes(r, ConsNeeds(kind, p))
ServingOpen(W, kind, p) == {r \in Rows : W.up[r] /\ bus[r] /\ Serves(r, kind, p)}

HasRoom(W, r, need, t) ==
  /\ ~W.paused[r]
  /\ FitsMaxRes(r, need)
  /\ FitsRes(RowCap[r], UsedRes(W, r, t), need)

RoomIn(W, p) ==
  /\ W.ps[p].att < MaxAtt
  /\ W.ps[p].ncut < K
  /\ \E r \in ServingOpen(W, "work", p) : HasRoom(W, r, CardRes[p], ZeroTally)

BackOf(W, p)    == IF W.sh[Stream[p]] THEN "waiting" ELSE "ready"
NextSlot(W, p)  == <<p, W.ps[p].ncut + 1>>
NeedsLanded(W, p) == \A n \in Needs[p] : W.ps[n].st = "landed"
Same(V, W, k)   == IF k[2] = 0 THEN V.ps[k[1]] = W.ps[k[1]] ELSE V.cs[k] = W.cs[k]

Pos(s, x) == CHOOSE i \in 1..Len(s) : s[i] = x
Drop(s, x) == SelectSeq(s, LAMBDA y : y # x)
First(seq, S) == seq[CHOOSE i \in 1..Len(seq) : seq[i] \in S /\ \A j \in 1..(i - 1) : seq[j] \notin S]

StartOK(W, p, r) ==
  /\ W.ps[p].st = "ready"
  /\ W.up[r]
  /\ ~W.sh[Stream[p]]
  /\ Serves(r, "work", p)
  /\ W.ps[p].att < MaxAtt
  /\ W.ps[p].ncut < K
StartedP(W, p) == [W.ps[p] EXCEPT !.st = "working", !.att = @ + 1, !.author = NoR,
                                  !.ncut = @ + 1, !.red = FALSE]
StartCut(W, p, r) == [st |-> "working", kind |-> "work", row |-> r, att |-> W.ps[p].att + 1,
                      res |-> "-", review |-> 0]

ReadOK(W, p, r) ==
  /\ W.ps[p].st = "review"
  /\ ~W.sh[Stream[p]]
  /\ W.up[r]
  /\ r # W.ps[p].author
  /\ Serves(r, "read", p)
  /\ W.ps[p].ncut < K
  /\ ~\E k \in LiveOf(W, p) : W.cs[k].kind = "read"
ReadCut(W, p, r) == [st |-> "working", kind |-> "read", row |-> r, att |-> W.ps[p].att,
                     res |-> "-", review |-> W.ps[p].review]
CurReads(W, p) == {c \in ConsOf(p) : W.cs[c].kind = "read" /\ W.cs[c].st # "none"
                                     /\ W.cs[c].review = W.ps[p].review}
Faulted(W, c) == W.cs[c].st = "done" /\ W.cs[c].res = "fault"
ReadOwed(W, p) == W.ps[p].st = "review" /\ ~\E c \in CurReads(W, p) : W.cs[c].st # "fail" /\ ~Faulted(W, c)
Settled(W, p) == W.ps[p].st = "review" /\ NoLive(W, p) /\ \E c \in CurReads(W, p) : W.cs[c].st = "done"
DoneSaid(W, p, res) == \E c \in CurReads(W, p) : W.cs[c].st = "done" /\ W.cs[c].res = res
Verdict(W, p) == IF DoneSaid(W, p, "fix") THEN "fix" ELSE IF DoneSaid(W, p, "ok") THEN "yes" ELSE "retry"

ResolveOK(W, p) ==
  /\ W.ps[p].st = "waiting"
  /\ NeedsLanded(W, p)
  /\ ~W.sh[Stream[p]]

-----------------------------------------------------------------------------
(* Groups, supporting multi-victim preemption sets (vs \subseteq CIds).    *)

StopReq(c) == [id |-> <<Epoch, "stop", c, 0>>, stream |-> Stream[c[1]], sent |-> FALSE]
LandReq(V, p) == [id |-> <<Epoch, "land", PKey(p), V.ps[p].head>>, stream |-> Stream[p], sent |-> FALSE]

Involved(kind, p, vs) ==
  CASE kind = "none" -> {}
    [] kind = "pre"  -> {p} \cup {c[1] : c \in vs}
    [] OTHER         -> {p}
CardsOf(kind, p, vs, V) ==
  CASE kind = "none"              -> {}
    [] kind = "pre"               -> {PKey(p), NextSlot(V, p)} \cup vs \cup {PKey(c[1]) : c \in vs}
    [] kind = "release"           -> {PKey(p)} \cup vs
    [] kind \in {"start", "read"} -> {PKey(p), NextSlot(V, p)}
    [] OTHER                      -> {PKey(p)}
ReadKeys(kind, p, vs, V) ==
  (CASE kind = "none" -> {}
     [] kind = "pre"  -> BaseReadKeys[p] \cup UNION {BaseReadKeys[c[1]] : c \in vs}
     [] OTHER         -> BaseReadKeys[p])
    \cup (IF kind = "resolve"
          THEN {PKey(q) : q \in {q \in Prods : Stream[q] = Stream[p]} \cup Needs[p]}
          ELSE {})

Grp(kind, p, vs, r, stamp, sha, V) ==
  [kind  |-> kind, p |-> p, vs |-> vs, r |-> r, stamp |-> stamp, sha |-> sha,
   blk   |-> kind = "pre" \/ (kind = "start" /\ V.ps[p].prio = "blocker"),
   cards |-> CardsOf(kind, p, vs, V),
   rk    |-> ReadKeys(kind, p, vs, V),
   rr    |-> IF r = NoR THEN {} ELSE {r},
   rsx   |-> IF kind \in {"none", "yes", "landed", "onbase"} THEN {} ELSE {Stream[p]},
   req   |-> IF kind = "pre" THEN {StopReq(c) : c \in vs} ELSE {}]
NoneG(stamp, V) == Grp("none", "-", {}, NoR, stamp, 0, V)

Fence(V, W, g) ==
  /\ \A k \in g.rk : Same(V, W, k)
  /\ \A r \in g.rr : V.rg[r] = W.rg[r]
  /\ \A s \in g.rsx : V.sh[s] = W.sh[s]

Legal(W, g) ==
  CASE g.kind = "pre"     -> /\ StartOK(W, g.p, g.r)
                             /\ g.vs # {}
                             /\ \A c \in g.vs : W.cs[c].st = "working" /\ W.cs[c].row = g.r /\ c[1] # g.p
    [] g.kind = "start"   -> StartOK(W, g.p, g.r)
    [] g.kind = "read"    -> ReadOK(W, g.p, g.r)
    [] g.kind = "yes"     -> Settled(W, g.p) /\ Verdict(W, g.p) = "yes"
    [] g.kind = "fix"     -> Settled(W, g.p) /\ Verdict(W, g.p) = "fix"
    [] g.kind = "resolve" -> W.ps[g.p].st = "waiting" /\ NeedsLanded(W, g.p) /\ ~W.sh[Stream[g.p]]
    [] g.kind \in {"landed", "onbase"} -> W.ps[g.p].st = "merging" /\ W.ps[g.p].head = g.sha
    [] g.kind = "release" -> g.vs # {} /\ \A c \in g.vs : W.cs[c].st = "working"
    [] OTHER              -> TRUE

Releases(W, c) == W.cs[c].kind = "work" /\ W.ps[c[1]].st = "working" /\ W.cs[c].att = W.ps[c[1]].att
PreWrites(W, g) ==
  LET relProds == {c[1] : c \in {c \in g.vs : Releases(W, c)}}
      ps1 == [q \in Prods |-> IF q \in relProds THEN [W.ps[q] EXCEPT !.st = BackOf(W, q)] ELSE W.ps[q]]
      cs1 == [c \in CIds |-> IF c \in g.vs THEN [W.cs[c] EXCEPT !.st = "fail"] ELSE W.cs[c]]
      tko1 == SelectSeq(W.tko, LAMBDA c : c \notin g.vs)
  IN [W EXCEPT !.ps  = [ps1 EXCEPT ![g.p] = StartedP(W, g.p)],
               !.cs  = [cs1 EXCEPT ![NextSlot(W, g.p)] = StartCut(W, g.p, g.r)],
               !.tko = Append(tko1, NextSlot(W, g.p))]

RelWrites(W, g) ==
  LET relProds == {c[1] : c \in {c \in g.vs : Releases(W, c)}}
      ps1 == [q \in Prods |-> IF q \in relProds THEN [W.ps[q] EXCEPT !.st = BackOf(W, q)] ELSE W.ps[q]]
      cs1 == [c \in CIds |-> IF c \in g.vs THEN [W.cs[c] EXCEPT !.st = "fail"] ELSE W.cs[c]]
  IN [W EXCEPT !.ps = ps1, !.cs = cs1, !.tko = SelectSeq(W.tko, LAMBDA c : c \notin g.vs)]

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
    [] g.kind = "release" -> RelWrites(W, g)
    [] OTHER              -> W

ReturnHand(W, r) ==
  LET H == Load(W, r)
  IN [W EXCEPT !.ps  = [p \in Prods |-> IF \E c \in H : c[1] = p /\ Releases(W, c)
                                       THEN [W.ps[p] EXCEPT !.st = BackOf(W, p)] ELSE W.ps[p]],
               !.cs  = [c \in CIds |-> IF c \in H THEN [W.cs[c] EXCEPT !.st = "fail"] ELSE W.cs[c]],
               !.tko = SelectSeq(W.tko, LAMBDA c : c \notin H)]

-----------------------------------------------------------------------------
(* The steps: O(|tko|) ordered prefix preemption (R52/R53) & DealStep.     *)

Free(p, cl) == PKey(p) \notin cl /\ \A c \in ConsOf(p) : c \notin cl
Cards(gs) == UNION {gs[i].cards : i \in 1..Len(gs)}

NoFreeWidth(W, b) == \A r \in ServingOpen(W, "work", b) : ~HasRoom(W, r, CardRes[b], ZeroTally)
BlockerDue(W, b) ==
  /\ admitted[b]
  /\ W.ps[b].prio = "blocker"
  /\ W.ps[b].st = "ready"
  /\ ~W.sh[Stream[b]]
  /\ W.ps[b].att < MaxAtt
  /\ W.ps[b].ncut < K
  /\ NoFreeWidth(W, b)

ConsPrio(W, c) ==
  IF W.cs[c].kind = "read" THEN 3
  ELSE IF W.ps[c[1]].prio = "blocker" THEN 5
  ELSE 1

CanFitWithoutSprint(W, r, need) == FitsRes(RowCap[r], W.other[r], need)

FitsAfterFreeing(W, r, need, drop) ==
  FitsRes(RowCap[r], AddRes(W.other[r], SumTko(W, r, drop, 1)), need)

\* Collect working consumers on r for prio in youngest-first order directly from tko.
RECURSIVE CollectYoungest(_, _, _, _, _)
CollectYoungest(W, r, b, prio, i) ==
  IF i < 1 THEN <<>>
  ELSE LET c    == W.tko[i]
           rest == CollectYoungest(W, r, b, prio, i - 1)
       IN IF W.cs[c].row = r /\ c[1] # b /\ ConsPrio(W, c) = prio
          THEN <<c>> \o rest
          ELSE rest

EligSeq(W, r, b) ==
  CollectYoungest(W, r, b, 1, Len(W.tko)) \o CollectYoungest(W, r, b, 3, Len(W.tko))

WorkingOther(W, r, b) == {c \in Working(W, r) : c[1] # b /\ ConsPrio(W, c) < 5}

VictimSet(W, b, r) ==
  IF W.paused[r] \/ ~Serves(r, "work", b) \/ ~CanFitWithoutSprint(W, r, CardRes[b]) THEN {}
  ELSE LET es == EligSeq(W, r, b)
       IN IF Len(es) = 0 THEN {}
          ELSE IF FitsAfterFreeing(W, r, CardRes[b], {es[1]}) THEN {es[1]}
          ELSE IF Len(es) >= 2 /\ FitsAfterFreeing(W, r, CardRes[b], {es[1], es[2]})
          THEN IF FitsAfterFreeing(W, r, CardRes[b], {es[2]}) THEN {es[2]} ELSE {es[1], es[2]}
          ELSE {}

PreRows(W, b) == {r \in ServingOpen(W, "work", b) : VictimSet(W, b, r) # {}}

RECURSIVE PreFold(_, _, _, _)
PreFold(V, a, i, acc) ==
  IF i > Len(ProdSeq) THEN acc
  ELSE
    LET b == ProdSeq[i]
    IN IF ~BlockerDue(V, b) THEN PreFold(V, a, i + 1, acc)
       ELSE IF ServingOpen(V, "work", b) = {}
       THEN PreFold(V, a, i + 1, [acc EXCEPT !.uns = @ \cup {UnservedAbout(b)}])
       ELSE LET R == PreRows(V, b) \ acc.used
            IN IF R = {} THEN PreFold(V, a, i + 1, acc)
               ELSE LET r  == IF a.rows \in R THEN a.rows ELSE First(RowSeq, R)
                        vs == VictimSet(V, b, r)
                        g  == Grp("pre", b, vs, r, 0, 0, V)
                        nt == [acc.t EXCEPT ![r] = AddRes(@, CardRes[b])]
                    IN PreFold(V, a, i + 1,
                               [acc EXCEPT !.gs = Append(@, g), !.used = @ \cup {r}, !.t = nt,
                                           !.miss = @ \cup (IF a.rows \notin R THEN {"rows"} ELSE {})])
PreStep(V, a) == PreFold(V, a, 1, [gs |-> <<>>, t |-> ZeroTally, used |-> {}, uns |-> {}, miss |-> {}])

NoPlan == [gs |-> <<>>, t |-> ZeroTally, used |-> {}, uns |-> {}, miss |-> {}, cr |-> {}]

LandKind(k, p) == IF k = "promote" \/ MainBase[Stream[p]] THEN "landed" ELSE "onbase"
ItemGroup(V, it, cl) ==
  LET p == it.id[3][1]
  IN IF /\ it.id[2] \in {"land", "promote"} /\ it.out = "landed"
        /\ V.ps[p].st = "merging" /\ V.ps[p].head = it.id[4]
        /\ PKey(p) \notin cl
     THEN Grp(LandKind(it.id[2], p), p, {}, NoR, it.stamp, it.id[4], V)
     ELSE NoneG(it.stamp, V)

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

ResolveSet(V, cl) == {p \in Prods : PKey(p) \notin cl /\ ResolveOK(V, p)}
GroupsOf(kind, S, V) ==
  LET s == SelectSeq(ProdSeq, LAMBDA x : x \in S)
  IN [i \in 1..Len(s) |-> Grp(kind, s[i], {}, NoR, 0, 0, V)]

RECURSIVE RelFold(_, _, _, _)
RelFold(V, cl, i, acc) ==
  IF i > Len(ProdSeq) THEN acc
  ELSE LET p == ProdSeq[i]
           L == {c \in ConsOf(p) : c \in lapsed /\ V.cs[c].st = "working"}
           c == CHOOSE c \in ConsOf(p) : c \in L \/ L = {}
       IN IF L # {} /\ Free(p, cl)
          THEN RelFold(V, cl, i + 1, Append(acc, Grp("release", p, {c}, V.cs[c].row, 0, 0, V)))
          ELSE RelFold(V, cl, i + 1, acc)
RelStep(V, cl) == RelFold(V, cl, 1, <<>>)

FixSet(V, cl, v) == {p \in Prods : Free(p, cl) /\ Settled(V, p) /\ Verdict(V, p) = v}

Desired(V, x) ==
  IF x.kind = "read"
  THEN IF Rank[V.ps[x.p].author] < MaxRank THEN Rank[V.ps[x.p].author] + 1 ELSE MaxRank
  ELSE IF V.ps[x.p].bounces >= EscalateAfter /\ Tier[x.p] < MaxRank THEN Tier[x.p] + 1 ELSE Tier[x.p]

BottleneckPermille(V, r, u) ==
  LET fcpu == ((RowCap[r].cpu - u.cpu) * 1000) \div RowCap[r].cpu
      fmem == ((RowCap[r].mem - u.mem) * 1000) \div RowCap[r].mem
  IN IF fcpu <= fmem THEN fcpu ELSE fmem

Eligible(V, x, r, t) ==
  /\ V.up[r]
  /\ bus[r]   \* on the bus this tick: a configured row is not a present one (D1')
  /\ Serves(r, x.kind, x.p)
  /\ HasRoom(V, r, ConsNeeds(x.kind, x.p), t)
  /\ (x.kind = "read" => r # V.ps[x.p].author)

Score(V, x, r, t) ==
  LET u == UsedRes(V, r, t)
  IN (CASE Rank[r] = Desired(V, x) -> 10000 [] Rank[r] > Desired(V, x) -> 5000 [] OTHER -> 0)
     + (IF u = ZeroRes /\ Load(V, r) = {} THEN 2000 ELSE 0)
     + BottleneckPermille(V, r, u)

DefaultRow(V, x, t) ==
  LET E == {r \in Rows : Eligible(V, x, r, t)}
  IN IF E = {} THEN NoR
     ELSE IF E = {"r1"} THEN "r1"
     ELSE IF E = {"r2"} THEN "r2"
     ELSE IF Score(V, x, "r1", t) >= Score(V, x, "r2", t) THEN "r1" ELSE "r2"

InDomain(V, x, ar, t) == ar \in Rows /\ Eligible(V, x, ar, t)
RowFor(V, x, t, ar) == IF InDomain(V, x, ar, t) THEN ar ELSE DefaultRow(V, x, t)
MayMove(V, x, r) == IF x.kind = "work" THEN StartOK(V, x.p, r) ELSE ReadOK(V, x.p, r)

Offered(V, x, r) == V.up[r] /\ bus[r] /\ Serves(r, x.kind, x.p) /\ (x.kind = "read" => r # V.ps[x.p].author)
FullAnswer(V, x, ar, drow) == ar \in Rows /\ Offered(V, x, ar) /\ drow = NoR
PolicyOffered(V, x, r) == Offered(V, x, r)
RowsAsked(V) == {[p |-> p, kind |-> "work"] : p \in {p \in Prods : V.ps[p].st = "ready"}}
                  \cup {[p |-> p, kind |-> "read"] : p \in {p \in Prods : ReadOwed(V, p)}}
OfferedTo(V) == {<<y[1].p, y[1].kind, y[2]>> : y \in {y \in RowsAsked(V) \X Rows : PolicyOffered(V, y[1], y[2])}}

RECURSIVE DealFold(_, _, _, _, _, _)
DealFold(V, cands, i, acc, ar, useAns) ==
  IF i > Len(cands) THEN acc
  ELSE
    LET x    == cands[i]
        need == ConsNeeds(x.kind, x.p)
        drow == DefaultRow(V, x, acc.t)
        ind  == InDomain(V, x, ar, acc.t)
        r    == IF useAns THEN RowFor(V, x, acc.t, ar) ELSE drow
        ok   == r # NoR /\ MayMove(V, x, r)
        g    == Grp(IF x.kind = "work" THEN "start" ELSE "read", x.p, {}, r, 0, 0, V)
        mi   == IF ind \/ FullAnswer(V, x, ar, drow) THEN {} ELSE {"rows"}
        un   == IF x.kind = "work" /\ ServingOpen(V, "work", x.p) = {} THEN {UnservedAbout(x.p)} ELSE {}
        cr   == IF x.kind = "read" /\ ServingOpen(V, "read", x.p) \subseteq {V.ps[x.p].author}
                THEN {x.p} ELSE {}
        nt   == IF ok THEN [acc.t EXCEPT ![r] = AddRes(@, need)] ELSE acc.t
    IN DealFold(V, cands, i + 1,
                IF ok THEN [acc EXCEPT !.gs = Append(@, g), !.t = nt, !.miss = @ \cup mi]
                      ELSE [acc EXCEPT !.miss = @ \cup mi, !.uns = @ \cup un, !.cr = @ \cup cr],
                ar, useAns)
Cands(V, cl) ==
  LET bl == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = "blocker" /\ V.ps[p].st = "ready" /\ Free(p, cl) /\ admitted[p])
      rd == SelectSeq(ProdSeq, LAMBDA p : ReadOwed(V, p) /\ ~V.sh[Stream[p]] /\ Free(p, cl))
      nm == SelectSeq(ProdSeq, LAMBDA p : V.ps[p].prio = "normal" /\ V.ps[p].st = "ready" /\ Free(p, cl)
                                          /\ admitted[p])   \* a producer that failed admission is never dealt
  IN [i \in 1..Len(bl) |-> [p |-> bl[i], kind |-> "work"]]
       \o [i \in 1..Len(rd) |-> [p |-> rd[i], kind |-> "read"]]
       \o [i \in 1..Len(nm) |-> [p |-> nm[i], kind |-> "work"]]
DealStep(V, cl, t0, ar, useAns) ==
  DealFold(V, Cands(V, cl), 1,
           [gs |-> <<>>, t |-> t0, miss |-> {}, uns |-> {}, cr |-> {}], ar, useAns)

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
RECURSIVE AdmitFold(_, _, _)
AdmitFold(E, i, acc) ==
  IF i > Len(E) THEN acc
  ELSE LET g == E[i]
       IN IF g.blk THEN AdmitFold(E, i + 1, [acc EXCEPT !.adm = Append(@, g), !.t = @ + Size(g)])
          ELSE IF ~acc.stop /\ (acc.nb = 0 \/ acc.t + Size(g) <= Budget)
          THEN AdmitFold(E, i + 1, [acc EXCEPT !.adm = Append(@, g), !.t = @ + Size(g), !.nb = @ + 1])
          ELSE AdmitFold(E, i + 1, [acc EXCEPT !.stop = TRUE])
Admit(E) == AdmitFold(E, 1, [adm |-> <<>>, t |-> 0, nb |-> 0, stop |-> FALSE]).adm

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
RECURSIVE ApplyFold(_, _, _, _)
ApplyFold(V, ops, i, acc) ==
  IF i > Len(ops) THEN acc
  ELSE IF OpOK(acc.pv, acc.w, ops[i])
       THEN ApplyFold(V, ops, i + 1, [acc EXCEPT !.w = ApplySeq(@, ops[i], 1),
                                                 !.pv = ApplySeq(@, ops[i], 1), !.ok = @ \cup {i}])
       ELSE ApplyFold(V, ops, i + 1, [acc EXCEPT !.ref = @ \cup {i}])

-----------------------------------------------------------------------------
(* The checks a tick must keep                                            *)

GroupsIn(o) == {o[j] : j \in 1..Len(o)}
Changed(W, W1, g)   == \E k \in g.cards : ~Same(W, W1, k)
AsApplied(W, W1, g) == \A k \in g.cards : Same(ApplyG(W, g), W1, k)

DueByDesign(V, b) ==
  /\ V.ps[b].prio = "blocker" /\ V.ps[b].st = "ready" /\ ~V.sh[Stream[b]] /\ admitted[b]
  /\ V.ps[b].att < MaxAtt /\ V.ps[b].ncut < K
  /\ \A r \in ServingOpen(V, "work", b) : ~HasRoom(V, r, CardRes[b], ZeroTally)
  /\ (ServingOpen(V, "work", b) = {}
      \/ \E r \in ServingOpen(V, "work", b) :
           ~V.paused[r] /\ CanFitWithoutSprint(V, r, CardRes[b])
           /\ FitsAfterFreeing(V, r, CardRes[b], WorkingOther(V, r, b)))

RECURSIVE SumSize(_, _)
SumSize(s, k) == IF k = 0 THEN 0 ELSE Size(s[k]) + SumSize(s, k - 1)

Resolvable(V, cl, p) ==
  /\ V.ps[p].st = "waiting" /\ PKey(p) \notin cl /\ ~V.sh[Stream[p]]
  /\ \A n \in Needs[p] : V.ps[n].st = "landed"

BetterVictim(W, a, d) ==
  \/ ConsPrio(W, a) < ConsPrio(W, d)
  \/ (ConsPrio(W, a) = ConsPrio(W, d) /\ Pos(W.tko, a) > Pos(W.tko, d))

BestSingletonVictim(V, p, r) ==
  LET fits == {c \in WorkingOther(V, r, p) : FitsAfterFreeing(V, r, CardRes[p], {c})}
  IN CHOOSE c \in fits : \A d \in fits \ {c} : BetterVictim(V, c, d)

Chk(name, ok) == IF ok THEN {} ELSE {name}

TickChecks(V, W, W1, a, E, adm, ops, ap, pre, dl, cl3, jsn, jv, picks, proms, cl4, res, clr, jnew, pres, accd) ==
  LET nb == SelectSeq(adm, LAMBDA g : ~g.blk)
      bseq == SelectSeq(adm, LAMBDA g : g.blk)
      bt == SumSize(bseq, Len(bseq))
      gs == SelectSeq(E, LAMBDA g : g.kind # "none")
  IN Chk("OneGroupPerCard",
         \A i, j \in 1..Len(gs) : i < j => gs[i].cards \cap gs[j].cards = {})
     \cup Chk("GroupWhole",
              \A i \in 1..Len(ops) : \A g \in GroupsIn(ops[i]) :
                AsApplied(W, W1, g) \/ ~Changed(W, W1, g))
     \cup Chk("NeverHalfApplied",
              \A i \in 1..Len(ops) :
                \/ \A g \in GroupsIn(ops[i]) : AsApplied(W, W1, g)
                \/ \A g \in GroupsIn(ops[i]) : ~Changed(W, W1, g))
     \cup Chk("RefusalChangesNothing",
              \A i \in ap.ref : \A g \in GroupsIn(ops[i]) : ~Changed(W, W1, g))
     \cup Chk("RefusedOnlyByRace",
              \A i \in ap.ref : \E g \in GroupsIn(ops[i]) : ~Fence(V, W, g))
     \cup Chk("BudgetKept",
              \A k \in 2..Len(nb) : bt + SumSize(nb, k) <= Budget)
     \cup Chk("BlockerPreemptsInOneTick",
              \A b \in Prods : DueByDesign(V, b) =>
                \/ \E g \in pres : g.p = b
                \/ ServingOpen(V, "work", b) = {} /\ [kind |-> "unserved", about |-> UnservedAbout(b)] \in jsn
                \/ ~V.pol /\ [kind |-> "no-policy", about |-> "coord"] \in jsn)
     \cup Chk("PreemptYoungest",
              \A g \in pres :
                /\ g.vs # {}
                /\ \A c \in g.vs : g.vs \ {c} # {} => ~FitsAfterFreeing(V, g.r, CardRes[g.p], g.vs \ {c})
                /\ ((Cardinality(g.vs) = 1 /\ FitsAfterFreeing(V, g.r, CardRes[g.p], g.vs))
                      => g.vs = {BestSingletonVictim(V, g.p, g.r)}))
     \cup Chk("PolicyFailsToDefault",
              /\ (V.pol /\ a.rows \notin Rows) => dl.gs = DealStep(V, cl3, pre.t, a.rows, FALSE).gs
              /\ \A q \in pre.miss \cup dl.miss : [kind |-> "policy", about |-> q] \in jsn)
     \cup Chk("AfterKept",
              \A p \in picks : \A t \in After[p] : V.ps[t].st # "merging")
     \cup Chk("AfterNeverHolds",
              \A s \in Streams :
                (~Busy(inflight, s) /\ \E p \in Prods :
                    Stream[p] = s /\ V.ps[p].st = "merging" /\ PKey(p) \notin Cards(gs) /\ p \notin V.ob
                    /\ ~Judged(jv, p) /\ \A t \in After[p] : V.ps[t].st # "merging")
                  => \E p \in picks : Stream[p] = s)
     \cup Chk("MergingCovered",
              \A p \in Prods :
                (V.ps[p].st = "merging" /\ PKey(p) \notin cl4) =>
                  \/ p \notin V.ob /\ Busy(inflight, Stream[p])
                  \/ p \notin V.ob /\ \E q \in picks : Stream[q] = Stream[p]
                  \/ p \in V.ob /\ BusyK(inflight, Stream[p], "promote")
                  \/ p \in V.ob /\ \E q \in proms : Stream[q] = Stream[p]
                  \/ Judged(jv, p)
                  \/ p \notin V.ob /\ \E t \in After[p] : V.ps[t].st = "merging")
     \cup Chk("NoDealWithoutPolicy",
              ~V.pol => /\ \A g \in accd : g.kind \notin {"pre", "start", "read"}
                        /\ [kind |-> "no-policy", about |-> "coord"] \in jsn)
     \cup Chk("ResolveFromState",
              \A p \in Prods : Resolvable(V, clr, p) => p \in res)
     \cup Chk("VerdictHasItsRead",
              \A g \in {x \in GroupsIn(gs) : x.kind \in {"yes", "fix"}} :
                LET said(out) == \E c \in ConsOf(g.p) :
                                   /\ V.cs[c].kind = "read" /\ V.cs[c].review = V.ps[g.p].review
                                   /\ V.cs[c].st = "done" /\ V.cs[c].res = out
                IN IF g.kind = "yes" THEN said("ok") /\ ~said("fix") ELSE said("fix"))
     \cup Chk("NeverDealtAbsent",
              \A g \in accd : g.kind \in {"pre", "start", "read"} => bus[g.r])
     \cup Chk("NeverDealtUnadmitted",
              \A g \in accd : g.kind \in {"pre", "start"} => admitted[g.p])
     \cup Chk("AdmissionShown",
              \A p \in Prods : (V.ps[p].st \in {"waiting", "ready"} /\ ~admitted[p])
                               => [kind |-> "admission", about |-> Stream[p]] \in jsn)
     \cup Chk("ExpiryNeverSilent",
              jnew \subseteq jsn)
     \cup Chk("LapsedReleased",
              \A c \in lapsed : (V.cs[c].st = "working" /\ Free(c[1], cl3))
                               => \E i \in 1..Len(E) : E[i].kind = "release" /\ c \in E[i].vs)
     \cup Chk("OfferedIsAccepted",
              \A x \in RowsAsked(V) : \A r \in Rows : PolicyOffered(V, x, r) => Offered(V, x, r))
     \cup Chk("NoDealBehindPause",
              \A g \in accd : g.kind \in {"pre", "start", "read"} => ~V.paused[g.r])
     \cup Chk("NoFutilePreempt",
              \A g \in pres : CanFitWithoutSprint(V, g.r, CardRes[g.p]) /\ FitsAfterFreeing(V, g.r, CardRes[g.p], g.vs))
     \cup Chk("PreemptMultiStops",
              \A g \in accd : g.kind = "pre" => (g.vs # {} /\ g.req = {StopReq(c) : c \in g.vs}))

-----------------------------------------------------------------------------
(* Actions — \E bindings evaluate each stage strictly ONCE in TLC!        *)

Asked(V) == \E p \in Prods : BlockerDue(V, p) \/ V.ps[p].st = "ready" \/ ReadOwed(V, p)
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

Unadmitted(V, s) == {p \in Prods : Stream[p] = s /\ V.ps[p].st \in {"waiting", "ready"} /\ ~admitted[p]}

UnservedClearedBy(r, about) ==
  \/ about = "t1" /\ 1 \in RowTiers[r] /\ FitsMaxRes(r, UnitRes)
  \/ about = "t2" /\ 2 \in RowTiers[r] /\ FitsMaxRes(r, UnitRes)
  \/ about = "t1+cpu:2+memory:2" /\ 1 \in RowTiers[r] /\ FitsMaxRes(r, TwoRes)

Expired(V, J) ==
  {j \in J : \/ j.kind = "unserved" /\ \E r \in Rows : V.up[r] /\ bus[r] /\ UnservedClearedBy(r, j.about)
             \/ j.kind = "cannot-read" /\ \E p \in Prods :
                  p = j.about /\ ~(ReadOwed(V, p) /\ ServingOpen(V, "read", p) \subseteq {V.ps[p].author})
             \/ j.kind = "no-policy" /\ V.pol
             \/ j.kind = "admission" /\ Unadmitted(V, j.about) = {}}

Deadlined(J) == {j \in J : j.kind \notin {"land", "no-policy"}}

Tick(a, due) ==
  /\ phase = "snapped"
  /\ LET V == snap
         W == w
     IN \E pre \in {IF V.pol THEN PreStep(V, a) ELSE NoPlan} :
        \E inb \in {InboxFold(V, inbox, 1, [es |-> <<>>, cl |-> Cards(pre.gs)])} :
        \E res \in {ResolveSet(V, inb.cl)} :
        \E rgs \in {GroupsOf("resolve", res, V)} :
        \E cl2 \in {inb.cl \cup Cards(rgs)} :
        \E fyes \in {GroupsOf("yes", FixSet(V, cl2, "yes"), V)} :
        \E ffix \in {GroupsOf("fix", FixSet(V, cl2, "fix"), V)} :
        \E cl3 \in {cl2 \cup Cards(fyes) \cup Cards(ffix)} :
        \E rel \in {RelStep(V, cl3)} :
        \E cl3r \in {cl3 \cup Cards(rel)} :
        \E dl \in {IF V.pol THEN DealStep(V, cl3r, pre.t, a.rows, TRUE) ELSE NoPlan} :
        \E cl4 \in {cl3r \cup Cards(dl.gs)} :
        \E jl \in {LandJudged(V, inbox, inflight)} :
        \E jv \in {jsnap \cup jl} :
        \E picks \in {LandPicks(V, cl4, inflight, jv)} :
        \E proms \in {PromPicks(V, cl4, inflight, jv)} :
        \E E \in {pre.gs \o SelectSeq(dl.gs, LAMBDA g : g.blk) \o inb.es \o rgs \o fyes \o ffix \o rel \o SelectSeq(dl.gs, LAMBDA g : ~g.blk)} :
        \E adm \in {Admit(E)} :
        \E ops \in {PackOps(adm, split)} :
        \E ap \in {ApplyFold(V, ops, 1, [w |-> W, pv |-> V, ok |-> {}, ref |-> {}])} :
        \E accd \in {UNION {GroupsIn(ops[i]) : i \in ap.ok}} :
        \E pres \in {{adm[i] : i \in {i \in 1..Len(adm) : adm[i].kind = "pre"}}} :
          LET OpOf(g) == CHOOSE i \in 1..Len(ops) : g \in GroupsIn(ops[i])
              admIn == {adm[i] : i \in {i \in 1..Len(adm) : adm[i].stamp > 0}}
              used  == {g \in admIn : g.kind = "none" \/ OpOf(g) \in ap.ok}
              ustamps == {g.stamp : g \in used}
              uids  == {inbox[i].id : i \in {i \in 1..Len(inbox) : inbox[i].stamp \in ustamps}}
              stops == UNION {g.req : g \in accd}
              lands == {LandReq(V, p) : p \in picks} \cup {PromReq(V, p) : p \in proms}
              jnew == {[kind |-> "unserved", about |-> t] : t \in pre.uns \cup dl.uns}
                        \cup {[kind |-> "cannot-read", about |-> p] : p \in dl.cr}
                        \cup {[kind |-> "policy", about |-> q] : q \in pre.miss \cup dl.miss}
                        \cup jl
                        \cup (IF V.pol THEN {} ELSE {[kind |-> "no-policy", about |-> "coord"]})
                        \cup {[kind |-> "admission", about |-> s] : s \in {s \in Streams : Unadmitted(V, s) # {}}}
              dueJ == IF due THEN Deadlined(js) ELSE {}
              Lapsed == dueJ \ jnew
              jsn  == ((js \ Expired(V, js)) \cup jnew) \ Lapsed
              und  == {p \in Prods : V.pol /\ V.ps[p].st = "ready" /\ ~V.sh[Stream[p]] /\ RoomIn(V, p) /\ admitted[p]
                                     /\ ap.w.ps[p].st = "ready"}
              tags == (IF \E g \in accd : g.kind = "pre" THEN {"pre"} ELSE {})
                      \cup (IF \E g \in accd : g.kind = "pre" /\ Cardinality(g.vs) > 1 THEN {"multipre"} ELSE {})
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
                      \cup (IF \E r \in Rows : V.up[r] /\ ~bus[r] /\ \E p \in Prods : V.ps[p].st = "ready" /\ Serves(r, "work", p)
                            THEN {"absent"} ELSE {})
                      \cup (IF \E r \in Rows : V.up[r] /\ bus[r] /\ V.paused[r]
                                               /\ \E p \in Prods : V.ps[p].st = "ready" /\ ~V.sh[Stream[p]] /\ Serves(r, "work", p)
                                                                   /\ ap.w.ps[p].st = "ready"
                            THEN {"pausedskip"} ELSE {})
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
                                             res, inb.cl, jnew, pres, accd)]
             /\ rdy' = [p \in Prods |-> IF p \in und THEN IF rdy[p] < UndealtTicks THEN rdy[p] + 1 ELSE rdy[p]
                                                     ELSE 0]
             /\ ju' = {p \in ju : V.ps[p].st = "ready"} \cup {p \in und : rdy[p] + 1 >= UndealtTicks}
  /\ phase' = "idle"
  /\ snap' = W0
  /\ jsnap' = {}
  /\ UNCHANGED <<arrived, env, bus, admitted, onmain, lapsed>>

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

HoldsOf  == CASE Scenario = "land" -> {"s1"} [] OTHER -> {}
UnholdsOf == CASE Scenario = "pre" -> {"s2"} [] Scenario = "land" -> {"s1"} [] OTHER -> {}
FlipsOf  == CASE Scenario = "pre" -> {"r2"} [] Scenario = "deal" -> {"r1"} [] OTHER -> {}
BusFlipsOf == CASE Scenario = "deal" -> {"r2"} [] OTHER -> {}
AdmFlipsOf == CASE Scenario = "deal" -> {"p"} [] OTHER -> {}
PolFlips == Scenario = "pre"
PauseFlipsOf == IF Scenario = "pre" THEN {"r2"} ELSE {}
OtherRowsOf == IF Scenario = "pre" THEN {"r2"} ELSE {}
HandLandsOf == IF Scenario = "deal" THEN {"p"} ELSE {}
LapsesOf == IF Scenario \in {"pre", "deal"} THEN CIds ELSE {}

HandLandOK(p) ==
  /\ phase = "idle"
  /\ w.ps[p].st = "merging"
  /\ ~\E f \in inflight : f.id[2] = "land" /\ f.id[3][1] = p
  /\ ~Judged(js, p)

OtherMoveOK(r, lv) ==
  /\ phase = "idle"
  /\ lv # w.other[r].cpu
  /\ lv + SprintUsed(w, r).cpu <= RowCap[r].cpu
  /\ lv + SprintUsed(w, r).mem <= RowCap[r].mem

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
           \/ \E r \in FlipsOf : w' = IF w.up[r]
                                      THEN ReturnHand([w EXCEPT !.up[r] = FALSE, !.rg[r] = @ + 1], r)
                                      ELSE [w EXCEPT !.up[r] = TRUE, !.rg[r] = @ + 1]
           \/ PolFlips /\ w' = [w EXCEPT !.pol = ~w.pol]
           \/ \E r \in PauseFlipsOf : phase = "idle" /\ w' = [w EXCEPT !.paused[r] = ~@]
           \/ \E r \in OtherRowsOf, lv \in {0, 1, 2} :
                OtherMoveOK(r, lv) /\ w' = [w EXCEPT !.other[r] = [cpu |-> lv, mem |-> lv]]
           \/ \E p \in HandLandsOf : HandLandOK(p) /\ w' = [w EXCEPT !.ps[p].st = "landed"]
     \/ \E r \in BusFlipsOf : bus' = [bus EXCEPT ![r] = ~@] /\ w' = w /\ admitted' = admitted /\ lapsed' = lapsed
     \/ \E p \in AdmFlipsOf : admitted' = [admitted EXCEPT ![p] = ~@] /\ w' = w /\ bus' = bus /\ lapsed' = lapsed
     \/ \E c \in LapsesOf : /\ w.cs[c].st = "working" /\ c \notin lapsed
                             /\ lapsed' = lapsed \cup {c} /\ w' = w /\ bus' = bus /\ admitted' = admitted
  /\ onmain' = onmain \cup {p \in Prods : w.ps[p].st # "landed" /\ w'.ps[p].st = "landed"}
  /\ last' = IF \E p \in Prods : w.ps[p].st # "landed" /\ w'.ps[p].st = "landed"
             THEN [tags |-> {"handland"}, bad |-> {}] ELSE Clean
  /\ UNCHANGED <<snap, jsnap, phase, inbox, inflight, js, split, arrived, processed>>
  /\ UNCHANGED aux

EdgeResult(f, out, n) ==
  /\ f \in inflight
  /\ ~f.sent
  /\ out \in (IF f.id[2] \in {"land", "promote"} THEN LandOutcomes ELSE {"ack"})
  /\ arrived + n <= MaxArrive
  /\ LET it(k) == [stamp |-> arrived + k, id |-> f.id, out |-> out]
     IN inbox' = IF n = 1 THEN Append(inbox, it(1)) ELSE Append(Append(inbox, it(1)), it(2))
  /\ arrived' = arrived + n
  /\ inflight' = (inflight \ {f}) \cup {[f EXCEPT !.sent = TRUE]}
  /\ onmain' = IF out = "landed" /\ (f.id[2] = "promote" \/ (f.id[2] = "land" /\ MainBase[f.stream]))
               THEN onmain \cup {f.id[3][1]} ELSE onmain
  /\ last' = Clean
  /\ UNCHANGED <<w, snap, jsnap, phase, js, split, processed, env>>
  /\ UNCHANGED <<aux, bus, admitted, lapsed>>

HandOnMain(p) == p \in w.ob \/ MainBase[Stream[p]]
Answer(p, choice) ==
  /\ Judged(js, p)
  /\ choice \in {"land", "fix", "retry"}
  /\ choice # "retry" => w.ps[p].st = "merging"
  /\ w' = CASE choice = "land" /\ HandOnMain(p) -> [w EXCEPT !.ps[p].st = "landed", !.ob = @ \ {p}]
            [] choice = "land" -> [w EXCEPT !.ob = @ \cup {p}]
            [] choice = "fix"  -> [w EXCEPT !.ps[p].st = BackOf(w, p), !.ps[p].review = @ + 1,
                                            !.ps[p].red = TRUE, !.ob = @ \ {p}]
            [] OTHER           -> w
  /\ onmain' = IF choice = "land" /\ HandOnMain(p) THEN onmain \cup {p} ELSE onmain
  /\ js' = js \ {LandJ(p)}
  /\ last' = Clean
  /\ UNCHANGED <<snap, jsnap, phase, inbox, inflight, split, arrived, processed, env>>
  /\ UNCHANGED <<aux, bus, admitted, lapsed>>

AnyTick ==
  /\ phase = "snapped"
  /\ \E a \in AnswersFor(snap), due \in BOOLEAN : Tick(a, due)

Next ==
  \/ Snap
  \/ AnyTick
  \/ \E c \in CIds, res \in WorkResults \cup ReadResults : Report(c, res)
  \/ EnvMove
  \/ \E f \in inflight, out \in {"landed", "red", "ack"}, n \in 1..2 : EdgeResult(f, out, n)
  \/ \E p \in Prods, choice \in {"land", "fix", "retry"} : Answer(p, choice)

Spec == Init /\ [][Next]_vars

FairSpec == Spec /\ WF_vars(Snap) /\ WF_vars(AnyTick)

-----------------------------------------------------------------------------
(* Invariants — zero exponential function-space sets in TypeOK!           *)

TypeOK ==
  /\ \A p \in Prods :
       /\ w.ps[p].st \in PStates
       /\ w.ps[p].att \in 0..MaxAtt
       /\ w.ps[p].head \in 0..MaxAtt
       /\ w.ps[p].author \in Rows \cup {NoR}
       /\ w.ps[p].bounces \in 0..MaxBounce
       /\ w.ps[p].prio \in {"normal", "blocker"}
       /\ w.ps[p].review \in 0..(MaxAtt + MaxArrive + 1)
       /\ w.ps[p].ncut \in 0..K
       /\ w.ps[p].red \in BOOLEAN
  /\ \A c \in CIds :
       /\ w.cs[c].st \in {"none", "ready", "working", "done", "fail"}
       /\ w.cs[c].kind \in {"-", "work", "read"}
       /\ w.cs[c].row \in Rows \cup {NoR}
       /\ w.cs[c].att \in 0..MaxAtt
       /\ w.cs[c].res \in {"-", "ok", "fail", "fault", "fix"}
       /\ w.cs[c].review \in 0..(MaxAtt + MaxArrive + 1)
  /\ \A r \in Rows :
       /\ w.up[r] \in BOOLEAN
       /\ w.paused[r] \in BOOLEAN
       /\ w.other[r].cpu \in 0..3 /\ w.other[r].mem \in 0..3
       /\ bus[r] \in BOOLEAN
       /\ w.rg[r] \in 0..MaxEnv
  /\ \A p \in Prods : admitted[p] \in BOOLEAN /\ rdy[p] \in 0..UndealtTicks
  /\ lapsed \subseteq CIds
  /\ \A s \in Streams : w.sh[s] \in BOOLEAN
  /\ w.pol \in BOOLEAN
  /\ w.ob \subseteq {p \in Prods : w.ps[p].st = "merging"}
  /\ onmain \subseteq Prods
  /\ phase \in {"idle", "snapped"}
  /\ arrived \in 0..MaxArrive
  /\ processed \in 0..MaxArrive
  /\ env \in 0..MaxEnv
  /\ ju \subseteq Prods
  /\ \A c \in CIds : (w.cs[c].st = "working") <=> \E i \in 1..Len(w.tko) : w.tko[i] = c
  /\ \A i, j \in 1..Len(w.tko) : i # j => w.tko[i] # w.tko[j]

NeverDealtTwice ==
  \A p \in Prods :
    /\ Cardinality({c \in LiveOf(w, p) : w.cs[c].kind = "work"}) <= 1
    /\ \A c \in LiveOf(w, p) :
         w.cs[c].kind = "work" => (w.ps[p].st = "working" /\ w.cs[c].att = w.ps[p].att)

WidthKept == \A r \in Rows :
  LET su == SprintUsed(w, r)
  IN /\ w.other[r].cpu + su.cpu <= RowCap[r].cpu
     /\ w.other[r].mem + su.mem <= RowCap[r].mem

DealIsTake == \A c \in CIds : w.cs[c].st # "ready"

DealtWithinKTicks == \A p \in Prods : rdy[p] >= UndealtTicks => p \in ju

Unloaded(W) == [W EXCEPT !.cs = [c \in CIds |-> NoCons], !.tko = <<>>, !.up = [r \in Rows |-> FALSE],
                         !.paused = [r \in Rows |-> TRUE]]
ReadyNeverFlipsWithLoad == \A p \in Prods : ResolveOK(w, p) = ResolveOK(Unloaded(w), p)

HandEmptyWhenClosed == \A r \in Rows : ~w.up[r] => Load(w, r) = {}

NeverBelowTier == \A c \in CIds : Live(w, c) => Serves(w.cs[c].row, w.cs[c].kind, c[1])

CarryFIFO ==
  /\ \A i, j \in 1..Len(inbox) : i < j => inbox[i].stamp < inbox[j].stamp
  /\ processed + Len(inbox) = arrived

EdgeAfterAccepted ==
  \A f \in inflight :
    /\ f.id[2] = "stop" => w.cs[f.id[3]].st = "fail"
    /\ f.id[2] \in {"land", "promote"} => \/ w.ps[f.id[3][1]].st = "merging" /\ w.ps[f.id[3][1]].head = f.id[4]
                          \/ w.ps[f.id[3][1]].st = "landed" \/ w.ps[f.id[3][1]].red
                          \/ w.ps[f.id[3][1]].att > f.id[4]

LandedOnMain == \A p \in Prods : w.ps[p].st = "landed" => p \in onmain

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
OfferedIsAccepted        == "OfferedIsAccepted" \notin last.bad
NoDealBehindPause        == "NoDealBehindPause" \notin last.bad
NoFutilePreempt          == "NoFutilePreempt" \notin last.bad
PreemptMultiStops        == "PreemptMultiStops" \notin last.bad

RoomFor(p) == w.pol /\ RoomIn(w, p) /\ admitted[p]
ReadyDealtWhenRoom ==
  \A p \in Prods : (w.ps[p].st = "ready" /\ RoomFor(p)) ~> (w.ps[p].st # "ready" \/ ~RoomFor(p))

OnlyFaulted(p) ==
  /\ w.ps[p].st = "review" /\ NoLive(w, p)
  /\ \E c \in CurReads(w, p) : Faulted(w, c)
  /\ \A c \in CurReads(w, p) : w.cs[c].st = "fail" \/ Faulted(w, c)
ReadRoomFor(p) ==
  /\ w.pol /\ ~w.sh[Stream[p]] /\ w.ps[p].ncut < K
  /\ \E r \in ServingOpen(w, "read", p) : r # w.ps[p].author /\ HasRoom(w, r, UnitRes, ZeroTally)
FaultedReadReadAgain ==
  \A p \in Prods : (OnlyFaulted(p) /\ ReadRoomFor(p)) ~> (~OnlyFaulted(p) \/ ~ReadRoomFor(p))

-----------------------------------------------------------------------------
(* Reachability: each is expected to be VIOLATED (reach/Steps*.cfg)       *)

StepsNeverPreempted        == "pre" \notin last.tags
StepsNeverMultiPreempted   == "multipre" \notin last.tags
StepsNeverPausedSkipped    == "pausedskip" \notin last.tags
StepsNeverRefused          == "refused" \notin last.tags
StepsNeverSplit            == "split" \notin last.tags
StepsNeverCarried          == "carry" \notin last.tags
StepsNeverLandRequested    == "landreq" \notin last.tags
StepsNeverPromoteRequested == "promreq" \notin last.tags
StepsNeverOnBase           == w.ob = {}
StepsNeverLandedOffMainBase == \A p \in Prods : ~MainBase[Stream[p]] => w.ps[p].st # "landed"
StepsNeverAfterHeld        == "afterheld" \notin last.tags
StepsNeverStopAcked        == "ack" \notin last.tags
StepsNeverEscalated        == "escal" \notin last.tags
StepsNeverResolved         == \A p \in Prods : w.ps[p].st # "ready"
StepsNeverDealt            == \A c \in CIds : w.cs[c].kind # "work"
StepsNeverReadAsked        == \A c \in CIds : w.cs[c].kind # "read"
StepsNeverMerged           == \A p \in Prods : w.ps[p].st # "merging"
StepsNeverFixed            == \A p \in Prods : w.ps[p].st = "ready" => w.ps[p].bounces = 0
StepsNeverLanded           == \A p \in Prods : w.ps[p].st # "landed"
StepsNeverRed              == \A p \in Prods : ~w.ps[p].red
StepsNeverLandJudged       == "judged" \notin last.tags
StepsNeverNeedReleased     == ~\E p \in Prods : Needs[p] # {} /\ w.ps[p].st # "waiting"
StepsNeverUnserved         == ~\E j \in js : j.kind = "unserved"
StepsNeverCannotRead       == ~\E j \in js : j.kind = "cannot-read"
StepsNeverPolicyJudged     == ~\E j \in js : j.kind = "policy"
StepsNeverGated            == ~\E j \in js : j.kind = "no-policy"
StepsNeverBothStarted      == ~(w.ps["p"].st \in Started /\ w.ps["q"].st \in Started)
StepsNeverHandLanded       == "handland" \notin last.tags
StepsNeverUndealt          == ju = {}
StepsNeverAbsent           == "absent" \notin last.tags
StepsNeverUnadmitted       == ~\E j \in js : j.kind = "admission"
StepsNeverReopened         == "reopened" \notin last.tags
StepsNeverReleased         == "released" \notin last.tags
StepsNeverReadAgain        == ~\E p \in Prods : \E c, d \in CurReads(w, p) :
                                Faulted(w, c) /\ d # c /\ w.cs[d].st \in {"working", "done"} /\ ~Faulted(w, d)

=============================================================================
