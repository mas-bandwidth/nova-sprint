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
(* (only on its own PR's green checks: HandOK), fix sends the card to   *)
(* fix, retry                                                              *)
(* asks a new landing. Switch is stream set --base: the stream lands    *)
(* into another branch from now on, its tip a tree no gate was built on *)
(* (the same cards: the branch was cut from base); a gate in flight is  *)
(* let go at once and the next pass builds on the new tip.              *)
(*                                                                         *)
(* CI takes time (CIStep): a gate's checks run, may fail or be stopped   *)
(* while others still run (partial), and complete green, red or stopped  *)
(* (every failing check killed, cancelled or timed out: no verdict about *)
(* the code, and any tree can be stopped). A verdict is taken only from  *)
(* completed checks: a partial gate is asked again until the request's  *)
(* wait passes (Late), then it is no verdict, never red; a stopped gate  *)
(* is no verdict at once (the sprint never runs CI again: LESSONS W19).  *)
(* A gate is the tree pushed for the request on base's tip as it was     *)
(* (gbase), at its rung. It is kept to its verdict: a pass on a base     *)
(* that moved meanwhile reads the same gate (never rebuilt, never pushed *)
(* again, its CI never restarted). A green gate on a base that moved is  *)
(* let go (ci none) and the next pass builds on the new tip; GitHub may  *)
(* say a kept gate conflicts with the moved base, and then it is built   *)
(* again and does not merge.                                             *)
(*                                                                         *)
(* A conflict names the one card whose merge failed (odd: the first in  *)
(* landing order that does not merge onto the tip with the cards before *)
(* it); the tick sends that card back to fix (SharedRed: policy's       *)
(* land_on_shared_red, on since 2026-11-15), or judges it alone (off),  *)
(* and the group's others are asked again without it. jc is the cards a *)
(* conflict was laid on (judged, or sent to fix).                        *)
(*                                                                         *)
(* Checks have names (2026-11-15): "test" runs on base's own pushes and  *)
(* on PRs, "lint" only on PRs. A tree is red on Reds: test when it holds *)
(* c1 and c3, lint when it holds c2, and whatever an outside commit      *)
(* broke (bad). With SharedRed a red gate whose every red is Known red  *)
(* on the base it was gated on lands as a green one does, with no human *)
(* (modules/edge/git/land.go sharedRed_internal): test is known red on   *)
(* a base tip whose own run is red on it; lint, never run on a base tip, *)
(* only by proof, the reds of the PR that merged that tip when its head *)
(* is the tip's very tree (an automatic landing's gate; a hand landing  *)
(* of a card onto the first base commit, its own PR's head). Anything   *)
(* else red is the card's own: a judgement, never a landing.             *)
(*                                                                         *)
(* Flaky (FALSE only for the liveness of SharedRedLands, LandEdgeShared  *)
(* Live.cfg) allows the faults: CI absent or stopped, a refused merge,  *)
(* a rested forge, the wait passing, base moving under a green gate.    *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS K,         \* outside commits to base at most
          BackedUp,  \* policy's landing(stream).backed_up
          MaxPass,   \* passes that ask again at most (with retries: they bound the model)
          SharedRed, \* policy's landing(stream).land_on_shared_red
          Flaky      \* CI and the forge may fault (absent, stopped, refused, rested, late)

Cards   == {"c1", "c2", "c3"}
Streams == {"s1", "s2"}
S(c)    == IF c = "c3" THEN "s2" ELSE "s1"
Order   == <<"c1", "c2", "c3">>
First(X) == Order[CHOOSE i \in 1..3 : Order[i] \in X /\ \A j \in 1..(i - 1) : Order[j] \notin X]
Idx(c)   == CHOOSE i \in 1..3 : Order[i] = c

Checks  == {"test", "lint"}
PROnly  == {"lint"}   \* run only on PRs, never on base's own pushes
\* What CI is red on for a tree: test with c1 and c3 (a semantic conflict
\* across streams), lint with c2, and what outside commits broke.
Reds(t)      == (IF {"c1", "c3"} \subseteq t.cards THEN {"test"} ELSE {})
                \cup (IF "c2" \in t.cards THEN {"lint"} ELSE {}) \cup t.bad
Conflict(cs) == {"c2", "c3"} \subseteq cs
\* The cards that conflict with some other card at all.
Conflicting  == {c \in Cards : \E d \in Cards : d # c /\ Conflict({c, d})}

NoTree == [cards |-> {}, ext |-> 0, bad |-> {}]
\* A request: its cards, state and outcome, the rung, and its gate: the
\* tree pushed (gate), the length of base when it was (gbase), its CI
\* (ci: none, running, partial, done; res: the completed result), whether
\* the wait passed on a partial CI (late), and the card a conflict names
\* (odd).
NoReq  == [cards |-> {}, st |-> "none", out |-> "none", rung |-> 0,
           gate |-> NoTree, gbase |-> 0, ci |-> "none", res |-> "none", late |-> FALSE,
           odd |-> {}]

VARIABLES
  base,    \* Seq of [tree, how ("init", "auto", "hand", "ext", "switch"), gated,
           \*   proof: the checks the PR that merged it read red on its very tree]
  cs,      \* each card: "merging", "landed" or "fix"
  req,     \* each stream's land request in flight, or NoReq
  js,      \* the cards under an open land judgement
  jc,      \* of those, the cards judged for a conflict
  held,    \* the streams with a request in flight when the last tick began
  ticked,  \* the last step was a tick
  passes   \* passes so far

vars == <<base, cs, req, js, jc, held, ticked, passes>>

Tip == base[Len(base)].tree
Init0 == base[1].tree

Init ==
  /\ base = << [tree |-> NoTree, how |-> "init", gated |-> NoTree, proof |-> {}] >>
  /\ cs = [c \in Cards |-> "merging"]
  /\ req = [s \in Streams |-> NoReq]
  /\ js = {}
  /\ jc = {}
  /\ held = {}
  /\ ticked = FALSE
  /\ passes = 0

-----------------------------------------------------------------------------
(* The tick (layer 3)                                                      *)

Consumed(s) == req[s].st = "done"
Moving(s)   == {c \in req[s].cards : cs[c] = "merging"}
\* The cards a finished request's judgement is for: a conflict that names
\* its card, that card alone; anything else, every card still merging.
JudgedOf(s) == IF req[s].out = "conflict" /\ req[s].odd # {} THEN Moving(s) \cap req[s].odd ELSE Moving(s)

\* A conflict that names its card sends that card to fix (SharedRed).
FixedOf(s) == IF SharedRed /\ req[s].out = "conflict" /\ req[s].odd # {} THEN Moving(s) \cap req[s].odd ELSE {}

Tick ==
  LET busy    == {s \in Streams : req[s].st # "none"}
      landedC == UNION {Moving(s) : s \in {s \in Streams : Consumed(s) /\ req[s].out = "landed"}}
      fixedC  == UNION {FixedOf(s) : s \in {s \in Streams : Consumed(s)}}
      judgedC == UNION {JudgedOf(s) : s \in {s \in Streams : Consumed(s) /\ req[s].out # "landed"}} \ fixedC
      clashC  == UNION {JudgedOf(s) : s \in {s \in Streams : Consumed(s) /\ req[s].out = "conflict"}}
      cs1     == [c \in Cards |-> IF c \in landedC THEN "landed" ELSE IF c \in fixedC THEN "fix" ELSE cs[c]]
      js1     == js \cup judgedC
      E(s)    == {c \in Cards : S(c) = s /\ cs1[c] = "merging" /\ c \notin js1}
      ask(s)  == IF Cardinality(E(s)) >= BackedUp THEN E(s) ELSE {First(E(s))}
  IN /\ cs' = cs1
     /\ js' = js1
     /\ jc' = jc \cup clashC
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
Gated(s) == [Tip EXCEPT !.cards = @ \cup req[s].cards]
RungOf(s) == IF Cardinality(req[s].cards) = 1 /\ Tip = Init0 THEN 1 ELSE 2
\* What a gate's checks can complete as: truthful CI (red only on a red
\* tree), and any tree can have every failing check stopped.
CIOf(t) == (IF Reds(t) # {} THEN {"red"} ELSE {"green"}) \cup (IF Flaky THEN {"stopped"} ELSE {})

\* The reds the landing knows of base[i] (land.go baseRed_internal and
\* prPathRed_internal): its own run's, on the checks that run on base, and
\* the proof of the PR that merged it, on the checks that run only on PRs.
Known(i) == (Reds(base[i].tree) \ PROnly) \cup (base[i].proof \cap PROnly)
\* Every red of r's gate is red on the base it was gated on.
SharedReds(r) == Reds(r.gate) \subseteq Known(r.gbase)
\* What a pass merges: a green gate, or with SharedRed a red one whose
\* reds are all the base's own.
Mergeable(r) == r.res = "green" \/ (r.res = "red" /\ SharedRed /\ SharedReds(r))

\* r finished with out, at its gate's rung.
Done(s, r, out) == req' = [req EXCEPT ![s] = [r EXCEPT !.st = "done", !.out = out]]
\* Every head already on base: landed at once, rung by the heads' count.
OnBase(s) == req' = [req EXCEPT ![s].st = "done", ![s].out = "landed",
                                ![s].rung = IF Cardinality(req[s].cards) = 1 THEN 1 ELSE 2]

\* The heads do not merge onto the tip (a tip that already holds both
\* sides of a conflict was merged by hand, and merges).
NoMerge(s) == Conflict(Gated(s).cards) /\ ~Conflict(Tip.cards)
\* The card a conflict names: the first in landing order that does not
\* merge onto the tip with the request's cards before it.
Odd(s) == LET cs1 == req[s].cards
              bad == {c \in cs1 : Conflict(Tip.cards \cup {d \in cs1 : Idx(d) < Idx(c)} \cup {c})}
          IN IF bad = {} THEN {} ELSE {First(bad)}

\* The gate a pass reads: the one the request keeps while it has one (its
\* CI not let go), on the base it was built on; else built on base's tip
\* and pushed, at the rung the pass finds, its CI started.
Kept(s) == req[s].ci # "none"
\* A kept gate on a base that moved whose CI no runner has taken yet is
\* let go and rebuilt on the tip in the same pass (Glenn 2026-11-15:
\* keeps_internal, queuedOnly_internal); once its CI started it is read to
\* its verdict (GateKept; live 2026-11-10).
MovedQueued(s) == req[s].ci = "queued" /\ req[s].gbase # Len(base)
KeepsGate(s) == Kept(s) /\ ~MovedQueued(s)
Gate1(s) == IF KeepsGate(s) THEN req[s]
            ELSE [req[s] EXCEPT !.gate = Gated(s), !.gbase = Len(base), !.rung = RungOf(s),
                                !.ci = "queued", !.res = "none", !.late = FALSE]
\* A gate let go: the next pass builds on the tip.
LetGo(r) == [r EXCEPT !.ci = "none"]

\* passes bounds only the passes that ask again with nothing changed (a
\* gate's CI still running, a rested forge) and the coordinator's retries:
\* a pass that ends its request, builds a new gate or lets one go (base
\* grew under it) is always enabled, so a landing CI has passed is never
\* starved by the bound (SharedRedLands).
Pass(s) ==
  /\ req[s].st = "sent"
  /\ passes' = passes + 1
  /\ \/ /\ req[s].cards \subseteq Tip.cards
        /\ OnBase(s)
        /\ UNCHANGED base
     \/ /\ ~(req[s].cards \subseteq Tip.cards)   \* built, or a kept gate GitHub says conflicts, built again
        /\ NoMerge(s)
        /\ Done(s, [req[s] EXCEPT !.rung = RungOf(s), !.odd = Odd(s)], "conflict")
        /\ UNCHANGED base
     \* GitHub rate-limited the forge (forge_rested): no CI read, asked again,
     \* nothing changed. (A rest after a new gate was pushed keeps that gate,
     \* LandResult.Kept: that is the no-verdict-yet branch below, with no read.)
     \/ /\ Flaky /\ passes < MaxPass
        /\ ~(req[s].cards \subseteq Tip.cards)
        /\ ~NoMerge(s) \/ Kept(s)
        /\ UNCHANGED <<base, req>>
     \/ /\ ~(req[s].cards \subseteq Tip.cards)
        /\ ~NoMerge(s) \/ KeepsGate(s)          \* a kept gate whose CI started is read whatever base did since
        /\ LET r == Gate1(s) IN
           \/ /\ Flaky
              /\ Done(s, r, "absent")                  \* no CI in time, a fault, a PR not opened
              /\ UNCHANGED base
           \/ /\ r.ci \in {"queued", "running"} \/ (r.ci = "partial" /\ ~r.late)   \* no verdict yet: asked again
              /\ KeepsGate(s) => passes < MaxPass
              /\ req' = [req EXCEPT ![s] = r]
              /\ UNCHANGED base
           \/ /\ r.ci = "partial" /\ r.late            \* the wait passed: no verdict
              /\ Done(s, r, "noverdict")
              /\ UNCHANGED base
           \/ /\ r.ci = "done" /\ r.res = "red" /\ ~Mergeable(r)   \* the card's own red: a judgement
              /\ Done(s, r, "red")
              /\ UNCHANGED base
           \/ /\ r.ci = "done" /\ r.res = "stopped"   \* every failing check stopped: no verdict
              /\ Done(s, r, "noverdict")
              /\ UNCHANGED base
           \/ /\ r.ci = "done" /\ Mergeable(r) /\ r.gbase = Len(base)
              /\ \/ /\ base' = Append(base, [tree |-> r.gate, how |-> "auto", gated |-> r.gate, proof |-> Reds(r.gate)])
                    /\ Done(s, r, "landed")
                 \/ /\ Flaky
                    /\ Done(s, r, "merge_refused")
                    /\ UNCHANGED base
                 \/ /\ Flaky /\ Tip.ext < K           \* base moved under the gate: let go, asked again
                    /\ base' = Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1], how |-> "ext", gated |-> NoTree, proof |-> {}])
                    /\ req' = [req EXCEPT ![s] = LetGo(r)]
           \/ /\ r.ci = "done" /\ Mergeable(r) /\ r.gbase # Len(base)   \* base moved since: let go, asked again
              /\ req' = [req EXCEPT ![s] = LetGo(r)]
              /\ UNCHANGED base
  /\ ticked' = FALSE
  /\ UNCHANGED <<cs, js, jc, held>>

\* A runner takes the gate's CI.
CIStarts(s) ==
  /\ req[s].st = "sent"
  /\ req[s].ci = "queued"
  /\ req' = [req EXCEPT ![s].ci = "running"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<base, cs, js, jc, held, passes>>

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
  /\ UNCHANGED <<base, cs, js, jc, held, passes>>

\* The request's wait passes on a partial CI.
Late(s) ==
  /\ Flaky
  /\ req[s].st = "sent"
  /\ req[s].ci = "partial"
  /\ ~req[s].late
  /\ req' = [req EXCEPT ![s].late = TRUE]
  /\ ticked' = FALSE
  /\ UNCHANGED <<base, cs, js, jc, held, passes>>

\* An outside commit to base; it may break checks (bad), of either kind.
BaseMoves(b) ==
  /\ Tip.ext < K
  /\ base' = Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1, !.bad = @ \cup b], how |-> "ext", gated |-> NoTree, proof |-> {}])
  /\ ticked' = FALSE
  /\ UNCHANGED <<cs, req, js, jc, held, passes>>

\* stream set --base: s lands into another branch from now on. Its gate in
\* flight, built on the old base, is let go (edges.go land reads the
\* stream's base before each pass); nothing built on the old base lands.
Switch(s) ==
  /\ Tip.ext < K
  /\ base' = Append(base, [tree |-> [Tip EXCEPT !.ext = @ + 1], how |-> "switch", gated |-> NoTree, proof |-> {}])
  /\ req' = [req EXCEPT ![s] = IF @.st = "sent" /\ Kept(s) THEN LetGo(@) ELSE @]   \* only a request in flight holds a gate
  /\ ticked' = FALSE
  /\ UNCHANGED <<cs, js, jc, held, passes>>

-----------------------------------------------------------------------------
(* The coordinator (rung 3)                                                *)

\* The tree of card c's own branch, its PR's head: the first base commit
\* and the card. A hand landing merges that PR (verbs handLand), so the PR
\* is the base tip's proof when the tip is that very tree.
Branch(c) == [NoTree EXCEPT !.cards = {c}]

\* A hand landing merges only when the checks of that PR's head, read once,
\* are green (git.HandVerdict; truthful CI: green on a tree with no reds).
\* Pending, red, stopped, all skipped and none are refused, nothing merged
\* (a stutter here: the coordinator answers again, or fix). A head already
\* on base records at once, with no read.
HandOK(c) == c \in Tip.cards \/ Reds(Branch(c)) = {}

Answer(c, choice) ==
  /\ c \in js
  /\ cs[c] = "merging"
  /\ choice = "retry" => passes < MaxPass
  /\ choice = "land" => HandOK(c)
  /\ js' = js \ {c} /\ jc' = jc \ {c}
  /\ CASE choice = "land" ->
            /\ cs' = [cs EXCEPT ![c] = "landed"]
            /\ base' = IF c \in Tip.cards THEN base
                       ELSE LET t == [Tip EXCEPT !.cards = @ \cup {c}]
                            IN Append(base, [tree |-> t, how |-> "hand", gated |-> NoTree,
                                             proof |-> IF t = Branch(c) THEN Reds(t) ELSE {}])
       [] choice = "fix" -> cs' = [cs EXCEPT ![c] = "fix"] /\ UNCHANGED base
       [] choice = "retry" -> UNCHANGED <<cs, base>>
  /\ ticked' = FALSE
  /\ UNCHANGED <<req, held, passes>>

Next ==
  \/ Tick
  \/ \E s \in Streams : Pass(s) \/ CIStarts(s) \/ CIStep(s) \/ Late(s)
  \/ \E b \in SUBSET Checks : BaseMoves(b)
  \/ \E s \in Streams : Switch(s)
  \/ \E c \in Cards, choice \in {"land", "fix", "retry"} : Answer(c, choice)

Spec == Init /\ [][Next]_vars

\* CI that runs completes (LandEdgeLive.cfg).
LiveSpec == Spec /\ \A s \in Streams : WF_vars(CIStarts(s)) /\ WF_vars(CIStep(s))

\* And the run keeps passing and ticking (LandEdgeSharedLive.cfg, Flaky
\* off): no human step is assumed, though the coordinator may act.
SharedLiveSpec == Spec /\ WF_vars(Tick) /\ \A s \in Streams : WF_vars(CIStarts(s)) /\ WF_vars(CIStep(s)) /\ WF_vars(Pass(s))

\* CI may never start (queued behind other runs); the run keeps passing
\* and ticking (LandEdgeQueuedLive.cfg, Flaky off).
QueuedLiveSpec == Spec /\ WF_vars(Tick) /\ \A s \in Streams : WF_vars(Pass(s))

-----------------------------------------------------------------------------
(* Invariants                                                              *)

TypeOK ==
  /\ cs \in [Cards -> {"merging", "landed", "fix"}]
  /\ js \subseteq Cards
  /\ jc \subseteq js \cup {c \in Cards : cs[c] = "fix"}
  /\ \A i \in 1..Len(base) : base[i].tree.bad \subseteq Checks /\ base[i].proof \subseteq Checks
  /\ \A s \in Streams : req[s].odd \subseteq req[s].cards
  /\ held \subseteq Streams
  /\ ticked \in BOOLEAN
  /\ \A s \in Streams : req[s].st \in {"none", "sent", "done"}
  /\ \A s \in Streams : req[s].ci \in {"none", "queued", "running", "partial", "done"}

  /\ \A s \in Streams : req[s].res \in {"none", "green", "red", "stopped"}
  /\ \A s \in Streams : req[s].late \in BOOLEAN

\* With land_on_shared_red off, base is never red from a landing: every
\* tree the landing put on base is one CI passed green.
BaseNeverRed ==
  ~SharedRed => \A i \in 1..Len(base) : base[i].how = "auto" => Reds(base[i].tree) = {}

\* A card lands without the coordinator only when its reds are all the
\* base's own and its merge is clean: every tree the landing put on base
\* is the exact tree CI gated (NothingLandsUnpassed), and red on nothing
\* the base it landed on was not already red on.
SharedRedOnly ==
  \A i \in 2..Len(base) : base[i].how = "auto" =>
       /\ base[i].tree = base[i].gated
       /\ Reds(base[i].tree) \subseteq Reds(base[i - 1].tree)

\* The coordinator is never asked about reds that are all the base's own:
\* with land_on_shared_red on, a red judgement is always a red of the
\* card's own.
SharedRedNeverJudged ==
  SharedRed => \A s \in Streams : req[s].out = "red" => ~SharedReds(req[s])

\* Nothing lands that CI did not pass except by land: a card enters base
\* only in the exact tree CI gated (auto) or by the coordinator's hand;
\* an outside commit brings none; and a landed card is on base.
NothingLandsUnpassed ==
  /\ \A i \in 2..Len(base) :
       /\ base[i].how = "auto" => base[i].tree = base[i].gated
       /\ base[i].how \in {"ext", "switch"} => base[i].tree.cards = base[i - 1].tree.cards
  /\ \A c \in Cards : cs[c] = "landed" => c \in Tip.cards

\* A hand landing never merges a card whose own PR's checks are not green:
\* every card a hand merge brings onto base is green on its own branch
\* (on 11-16 and 11-17, 41 PRs were merged by hand with every check skipped).
HandLandsGreen ==
  \A i \in 2..Len(base) : base[i].how = "hand" =>
       \A c \in base[i].tree.cards \ base[i - 1].tree.cards : Reds(Branch(c)) = {}

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
  \A s \in Streams : req[s].out = "red" => Reds(req[s].gate) # {}


\* A conflict is laid (judged, or sent to fix) on the card that does not
\* merge, never on a card of its group that conflicts with nothing.
ConflictJudgesItsCard == jc \subseteq Conflicting

\* With land_on_shared_red on, a conflict that names its card never waits
\* for the coordinator: no card is judged for a conflict.
ConflictNeverJudged == SharedRed => jc \cap js = {}

\* One group in flight per stream, kept to its verdict: a gate whose CI
\* still runs is never replaced (rebuilt, pushed again) while its request
\* is in flight, whatever the base does; a base switch lets it go first.
\* A gate whose CI is only queued is kept too while the base has not moved
\* (MovedQueued lets it go once the base moved).
GateKept ==
  [][\A s \in Streams :
       req[s].st = "sent" /\ (req[s].ci \in {"running", "partial"} \/ (req[s].ci = "queued" /\ req[s].gbase = Len(base)))
         => req'[s].gate = req[s].gate /\ req'[s].gbase = req[s].gbase]_vars


\* A gate in flight reaches a verdict (its checks complete) or its request
\* an outcome, the same gate (LiveSpec: CI that runs completes), unless the
\* stream's base was switched under it (stream set --base lets it go).
GateReachesVerdict ==
  \A s \in Streams, n \in 1..(K + 4) :
    (req[s].st = "sent" /\ req[s].ci \in {"running", "partial"} /\ req[s].gbase = n)
      ~> \/ req[s].gbase = n /\ (req[s].ci = "done" \/ req[s].st = "done")
         \/ \E i \in (n + 1)..Len(base) : base[i].how = "switch"

\* LiveBound bounds total passes in LandEdgeLive.cfg so the Flaky liveness
\* tableau stays tight (build pass + kept-gate pass on a moved base) while
\* Spec/LandEdge.cfg still explores MaxPass = 6 without constraint.
LiveBound == passes <= 2

\* A gate in flight on a base that moved, whose CI no runner has taken, is
\* let go and rebuilt on the tip (Glenn 2026-11-15: a base move lets the
\* in-flight group go), even if its CI never starts (QueuedLiveSpec: no
\* fairness for CIStarts): it never waits on CI of a base that is gone.
MovedQueuedLetsGo ==
  \A s \in Streams, n \in 1..8 :
    (req[s].st = "sent" /\ req[s].ci = "queued" /\ req[s].gbase = n /\ Len(base) > n)
      ~> \/ req[s].gbase # n
         \/ req[s].st # "sent"
         \/ req[s].ci # "queued"


\* A merging card in a request whose gate completed red on the base's own
\* reds lands with no human step (SharedLiveSpec, Flaky off), unless base
\* moved under it first (then the next gate is weighed again).
SharedRedLands ==
  \A c \in Cards, n \in 1..8 :
    LET r == req[S(c)] IN
    (/\ cs[c] = "merging" /\ c \in r.cards /\ r.st = "sent"
     /\ r.ci = "done" /\ r.res = "red" /\ SharedReds(r)
     /\ r.gbase = n /\ Len(base) = n)
      ~> \/ cs[c] = "landed"
         \/ Len(base) > n /\ c \notin base[n + 1].tree.cards

-----------------------------------------------------------------------------
(* Reachability: each expected VIOLATED (a run that shows the path).       *)

NeverRungTwo   == \A s \in Streams : req[s].rung # 2
NeverJudged    == js = {}
NeverHandLand  == \A i \in 1..Len(base) : base[i].how # "hand"
NeverRedGated  == \A s \in Streams : req[s].out # "red"
NeverNoVerdict == \A s \in Streams : req[s].out # "noverdict"
NeverLate      == \A s \in Streams : ~(req[s].out = "noverdict" /\ req[s].ci = "partial")
NeverKeptMoved == \A s \in Streams : ~(req[s].st = "sent" /\ req[s].ci = "running" /\ req[s].gbase # Len(base))
NeverSwitchInFlight == \A i \in 1..Len(base) : base[i].how # "switch" \/ \A s \in Streams : req[s].gbase # i - 1
NeverOddAlone  == \A s \in Streams : ~(req[s].out = "conflict" /\ req[s].odd # {} /\ req[s].odd # req[s].cards)
NeverSharedLand == \A i \in 1..Len(base) : base[i].how = "auto" => Reds(base[i].tree) = {}
NeverPROnlyShared == \A i \in 1..Len(base) : base[i].how = "auto" => Reds(base[i].tree) \cap PROnly = {}
NeverConflictFix == \A c \in Cards : ~(c \in jc /\ cs[c] = "fix")
=============================================================================
