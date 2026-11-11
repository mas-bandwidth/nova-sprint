------------------------------ MODULE LandEdge ------------------------------
(***************************************************************************)
(* The landing (docs/LANDING.md; intent rules 16 and 32): the ladder of   *)
(* three rungs, from a tick's land request to the base.                    *)
(*                                                                         *)
(* Cards c1, c2 are stream s1's, c3 is s2's; both streams land on one     *)
(* base. Every card's branch was cut from the first base commit. A tree    *)
(* is the set of cards it holds and the count of outside commits (ext).   *)
(* CI is truthful: a tree holding both c1 and c3 is red (a semantic       *)
(* conflict across streams: each is green alone); a tree holding c2 and  *)
(* c3 does not merge (a textual conflict). CI may also report nothing    *)
(* (absent stands for every outcome that is neither green nor red: no CI *)
(* in time, a fault, a PR not opened, a branch not created).             *)
(*                                                                         *)
(* Tick is layer 3's landing, one atomic step: each finished request is  *)
(* consumed (landed moves its cards to landed; anything else opens one   *)
(* land judgement per card still merging, nothing moved, nothing        *)
(* blamed), then each stream with no request in flight at the tick's    *)
(* start asks one: its first eligible card (rung 1's pick) while fewer   *)
(* than BackedUp wait, every one of them when backed up (rung 2).        *)
(* Pass is one LandPass of layer 4 (modules/edge/git/land.go): all the request's *)
(* heads already on base is landed at once; one head whose branch holds   *)
(* base's tip gates its own PR (rung 1); otherwise the heads are merged  *)
(* onto base's tip into the group branch and its PR gates (rung 2). A    *)
(* conflict stops the pass. A green gate merges the gated tree exactly,  *)
(* only while base is still the tip it was gated on; base moving under   *)
(* the gate (an outside commit) asks the pass again; GitHub may refuse   *)
(* the merge. Answer is the coordinator's (rung 3): land merges by hand  *)
(* (no gate: the coordinator looked), fix sends the card to fix, retry   *)
(* asks a new landing.                                                    *)
(*                                                                         *)
(* CI takes time (CIStep): a gate's checks run, may fail or be stopped   *)
(* while others still run (partial), and complete green, red or stopped  *)
(* (every failing check killed, cancelled or timed out: no verdict about *)
(* the code, and any tree can be stopped). A verdict is taken only from  *)
(* completed checks: a partial gate is asked again until the request's  *)
(* wait passes (Late), then it is no verdict, never red; a stopped gate  *)
(* is run again once (reran), then no verdict. A gate is the tree pushed *)
(* for the request on base's tip as it was (gbase); a pass on a base     *)
(* that moved since rebuilds it and its CI starts again.                 *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS K,         \* outside commits to base at most
          BackedUp,  \* policy's landing(stream).backed_up
          MaxPass    \* passes at most (retries and base moves bound the model)

Cards   == {"c1", "c2", "c3"}
Streams == {"s1", "s2"}
S(c)    == IF c = "c3" THEN "s2" ELSE "s1"
Order   == <<"c1", "c2", "c3">>
First(X) == Order[CHOOSE i \in 1..3 : Order[i] \in X /\ \A j \in 1..(i - 1) : Order[j] \notin X]

Red(cs)      == {"c1", "c3"} \subseteq cs
Conflict(cs) == {"c2", "c3"} \subseteq cs

NoTree == [cards |-> {}, ext |-> 0]
\* A request: its cards, state and outcome, the rung, and its gate: the
\* tree pushed (gate), the length of base when it was (gbase), its CI
\* (ci: none, running, partial, done; res: the completed result), whether
\* its stopped jobs were run again (reran) and whether the wait passed on a
\* partial CI (late).
NoReq  == [cards |-> {}, st |-> "none", out |-> "none", rung |-> 0,
           gate |-> NoTree, gbase |-> 0, ci |-> "none", res |-> "none", reran |-> FALSE, late |-> FALSE]

VARIABLES
  base,    \* Seq of [tree, how ("init", "auto", "hand", "ext"), gated]
  cs,      \* each card: "merging", "landed" or "fix"
  req,     \* each stream's land request in flight, or NoReq
  js,      \* the cards under an open land judgement
  held,    \* the streams with a request in flight when the last tick began
  ticked,  \* the last step was a tick
  passes   \* passes so far

vars == <<base, cs, req, js, held, ticked, passes>>

Tip == base[Len(base)].tree
Init0 == base[1].tree

Init ==
  /\ base = << [tree |-> NoTree, how |-> "init", gated |-> NoTree] >>
  /\ cs = [c \in Cards |-> "merging"]
  /\ req = [s \in Streams |-> NoReq]
  /\ js = {}
  /\ held = {}
  /\ ticked = FALSE
  /\ passes = 0

-----------------------------------------------------------------------------
(* The tick (layer 3)                                                      *)

Consumed(s) == req[s].st = "done"
Moving(s)   == {c \in req[s].cards : cs[c] = "merging"}

Tick ==
  LET busy    == {s \in Streams : req[s].st # "none"}
      landedC == UNION {Moving(s) : s \in {s \in Streams : Consumed(s) /\ req[s].out = "landed"}}
      judgedC == UNION {Moving(s) : s \in {s \in Streams : Consumed(s) /\ req[s].out # "landed"}}
      cs1     == [c \in Cards |-> IF c \in landedC THEN "landed" ELSE cs[c]]
      js1     == js \cup judgedC
      E(s)    == {c \in Cards : S(c) = s /\ cs1[c] = "merging" /\ c \notin js1}
      ask(s)  == IF Cardinality(E(s)) >= BackedUp THEN E(s) ELSE {First(E(s))}
  IN /\ cs' = cs1
     /\ js' = js1
     /\ req' = [s \in Streams |->
                  IF s \notin busy /\ E(s) # {} THEN [NoReq EXCEPT !.cards = ask(s), !.st = "sent"]
                  ELSE IF Consumed(s) THEN NoReq ELSE req[s]]
     /\ held' = busy
     /\ ticked' = TRUE
     /\ UNCHANGED <<base, passes>>

-----------------------------------------------------------------------------
(* One pass of the landing edge (layer 4)                                  *)

\* The tree CI gates: rung 1 (one head whose branch holds base's tip, the
\* first commit) gates the card's branch, which is that tip plus the card;
\* rung 2 gates the heads merged onto the tip. Both are Tip plus the cards.
Gated(s) == [cards |-> Tip.cards \cup req[s].cards, ext |-> Tip.ext]
RungOf(s) == IF Cardinality(req[s].cards) = 1 /\ Tip = Init0 THEN 1 ELSE 2
\* What a gate's checks can complete as: truthful CI (red only on a red
\* tree), and any tree can have every failing check stopped.
CIOf(t) == IF Red(t.cards) THEN {"red", "stopped"} ELSE {"green", "stopped"}

\* r finished with out, at the rung the pass found.
Done(s, r, out) == req' = [req EXCEPT ![s] = [r EXCEPT !.st = "done", !.out = out, !.rung = RungOf(s)]]
Finish(s, out) == Done(s, req[s], out)
\* Every head already on base: landed at once, rung by the heads' count.
OnBase(s) == req' = [req EXCEPT ![s].st = "done", ![s].out = "landed",
                                ![s].rung = IF Cardinality(req[s].cards) = 1 THEN 1 ELSE 2]

\* The gate a pass reads: the one pushed while base is still the tip it was
\* built on; else rebuilt on base's tip and pushed, its CI started again.
Current(s) == req[s].gbase = Len(base) /\ req[s].ci # "none"
Gate1(s) == IF Current(s) THEN req[s]
            ELSE [req[s] EXCEPT !.gate = Gated(s), !.gbase = Len(base), !.ci = "running",
                                !.res = "none", !.late = FALSE]

Pass(s) ==
  /\ req[s].st = "sent"
  /\ passes < MaxPass
  /\ passes' = passes + 1
  /\ \/ /\ req[s].cards \subseteq Tip.cards
        /\ OnBase(s)
        /\ UNCHANGED base
     \/ /\ ~(req[s].cards \subseteq Tip.cards)
        /\ Conflict(Gated(s).cards)
        /\ Finish(s, "conflict")
        /\ UNCHANGED base
     \/ /\ ~(req[s].cards \subseteq Tip.cards)
        /\ ~Conflict(Gated(s).cards)
        /\ LET r == Gate1(s) IN
           \/ /\ Done(s, r, "absent")                  \* no CI in time, a fault, a PR not opened
              /\ UNCHANGED base
           \/ /\ r.ci = "running" \/ (r.ci = "partial" /\ ~r.late)   \* no verdict yet: asked again
              /\ req' = [req EXCEPT ![s] = r]
              /\ UNCHANGED base
           \/ /\ r.ci = "partial" /\ r.late            \* the wait passed: no verdict
              /\ Done(s, r, "noverdict")
              /\ UNCHANGED base
           \/ /\ r.ci = "done" /\ r.res = "red"
              /\ Done(s, r, "red")
              /\ UNCHANGED base
           \/ /\ r.ci = "done" /\ r.res = "stopped" /\ ~r.reran   \* run again once: asked again
              /\ req' = [req EXCEPT ![s] = [r EXCEPT !.ci = "running", !.res = "none", !.reran = TRUE, !.late = FALSE]]
              /\ UNCHANGED base
           \/ /\ r.ci = "done" /\ r.res = "stopped" /\ r.reran
              /\ Done(s, r, "noverdict")
              /\ UNCHANGED base
           \/ /\ r.ci = "done" /\ r.res = "green"
              /\ \/ /\ base' = Append(base, [tree |-> r.gate, how |-> "auto", gated |-> r.gate])
                    /\ Done(s, r, "landed")
                 \/ /\ Done(s, r, "merge_refused")
                    /\ UNCHANGED base
                 \/ /\ Tip.ext < K                    \* base moved under the gate: asked again
                    /\ base' = Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1], how |-> "ext", gated |-> NoTree])
                    /\ req' = [req EXCEPT ![s] = r]
  /\ ticked' = FALSE
  /\ UNCHANGED <<cs, js, held>>

\* The gate's checks move on: a check fails or is stopped while others
\* still run (partial), or they all complete (CIOf; never green once one
\* failed or was stopped).
CIStep(s) ==
  /\ req[s].st = "sent"
  /\ \/ /\ req[s].ci = "running"
        /\ \/ req' = [req EXCEPT ![s].ci = "partial"]
           \/ \E v \in CIOf(req[s].gate) : req' = [req EXCEPT ![s].ci = "done", ![s].res = v]
     \/ /\ req[s].ci = "partial"
        /\ \E v \in CIOf(req[s].gate) \ {"green"} : req' = [req EXCEPT ![s].ci = "done", ![s].res = v]
  /\ ticked' = FALSE
  /\ UNCHANGED <<base, cs, js, held, passes>>

\* The request's wait passes on a partial CI.
Late(s) ==
  /\ req[s].st = "sent"
  /\ req[s].ci = "partial"
  /\ ~req[s].late
  /\ req' = [req EXCEPT ![s].late = TRUE]
  /\ ticked' = FALSE
  /\ UNCHANGED <<base, cs, js, held, passes>>

\* An outside commit to base.
BaseMoves ==
  /\ Tip.ext < K
  /\ base' = Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1], how |-> "ext", gated |-> NoTree])
  /\ ticked' = FALSE
  /\ UNCHANGED <<cs, req, js, held, passes>>

-----------------------------------------------------------------------------
(* The coordinator (rung 3)                                                *)

Answer(c, choice) ==
  /\ c \in js
  /\ cs[c] = "merging"
  /\ js' = js \ {c}
  /\ CASE choice = "land" ->
            /\ cs' = [cs EXCEPT ![c] = "landed"]
            /\ base' = IF c \in Tip.cards THEN base
                       ELSE Append(base, [tree |-> [Tip EXCEPT !.cards = @ \cup {c}], how |-> "hand", gated |-> NoTree])
       [] choice = "fix" -> cs' = [cs EXCEPT ![c] = "fix"] /\ UNCHANGED base
       [] choice = "retry" -> UNCHANGED <<cs, base>>
  /\ ticked' = FALSE
  /\ UNCHANGED <<req, held, passes>>

Next ==
  \/ Tick
  \/ \E s \in Streams : Pass(s) \/ CIStep(s) \/ Late(s)
  \/ BaseMoves
  \/ \E c \in Cards, choice \in {"land", "fix", "retry"} : Answer(c, choice)

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Invariants                                                              *)

TypeOK ==
  /\ cs \in [Cards -> {"merging", "landed", "fix"}]
  /\ js \subseteq Cards
  /\ held \subseteq Streams
  /\ ticked \in BOOLEAN
  /\ \A s \in Streams : req[s].st \in {"none", "sent", "done"}
  /\ \A s \in Streams : req[s].ci \in {"none", "running", "partial", "done"}
  /\ \A s \in Streams : req[s].res \in {"none", "green", "red", "stopped"}
  /\ \A s \in Streams : req[s].reran \in BOOLEAN /\ req[s].late \in BOOLEAN

\* Base is never red from a landing: every tree the landing put on base
\* is one CI passed green.
BaseNeverRed ==
  \A i \in 1..Len(base) : base[i].how = "auto" => ~Red(base[i].tree.cards)

\* Nothing lands that CI did not pass except by land: a card enters base
\* only in the exact tree CI gated (auto) or by the coordinator's hand;
\* an outside commit brings none; and a landed card is on base.
NothingLandsUnpassed ==
  /\ \A i \in 2..Len(base) :
       /\ base[i].how = "auto" => base[i].tree = base[i].gated
       /\ base[i].how = "ext" => base[i].tree.cards = base[i - 1].tree.cards
  /\ \A c \in Cards : cs[c] = "landed" => c \in Tip.cards

\* No merging card without a request or a judgement after a tick: each is
\* under a land judgement, or its stream has a request in flight (its own,
\* or one ahead of it in landing order), or had one when the tick began
\* (consumed by that tick; the next tick asks).
MergingCovered ==
  ticked => \A c \in Cards :
    cs[c] = "merging" => c \in js \/ req[S(c)].st # "none" \/ S(c) \in held

\* A verdict only from completed checks: red is taken only once the gate's
\* checks all completed, never while some still run.
VerdictOnlyCompleted ==
  \A s \in Streams : req[s].out = "red" => req[s].ci = "done"

\* Red says something about the code: a red judgement only on a tree CI is
\* truthfully red on, never on checks that were all stopped.
RedIsReal ==
  \A s \in Streams : req[s].out = "red" => Red(req[s].gate.cards)

\* Stopped checks are run again once before they are the coordinator's.
StoppedRunAgainOnce ==
  \A s \in Streams : req[s].out = "noverdict" /\ req[s].ci = "done" => req[s].reran

-----------------------------------------------------------------------------
(* Reachability: each expected VIOLATED (a run that shows the path).       *)

NeverRungTwo   == \A s \in Streams : req[s].rung # 2
NeverJudged    == js = {}
NeverHandLand  == \A i \in 1..Len(base) : base[i].how # "hand"
NeverRedGated  == \A s \in Streams : req[s].out # "red"
NeverNoVerdict == \A s \in Streams : req[s].out # "noverdict"
NeverRerun     == \A s \in Streams : ~req[s].reran
NeverLate      == \A s \in Streams : ~(req[s].out = "noverdict" /\ req[s].ci = "partial")
=============================================================================
