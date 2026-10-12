------------------------------- MODULE LandPass -------------------------------
\* STREAM FLOW (2026-10-12, revised after Rowan's cold read). StreamFlowSpec
\* models per-card base and the land_protected mark, stream batches, trunk moves,
\* inflight certification, and empty branches following sibling trunk advances.
\* SetBase compares rewrite-all with the existing preserve-cut-head selector;
\* an independent BriefEdit mutation tests postmigration routing. StreamLand and
\* StreamMerge separate branch landing from serialized certification admission;
\* CertDone and an explicitly successful Fix support weakly fair completion.
\* Reversed witnesses cover each rule and adversarial red/retry cycles. The old
\* batch-set base is batchBase. This is a separate abstraction, not a composition
\* or refinement of the legacy gate machine; each spec freezes the other's state.
\*
\* nova-sprint land, the pass in two phases (cmd/nova-sprint/landpass.go; the owner,
\* 2026-10-07: "We can do merges across work streams in parallel. The only thing that
\* needs to be serial is the merge after."). Land.tla holds one batch's push and report
\* and the fences between them; this module holds the pass over several streams: the
\* merges in parallel, the landings one at a time, the pushed tip's record, and the
\* refusals.
\*
\* THE WORLD. Each stream has one batch (its cards land together, as one tip: a card is the
\* batch here). The base is a tree, modelled as the set of batches landed on it. Every
\* batch touches a set of files (Touches). Whether a tree is green under the tree gate is
\* the oracle green, chosen at Init over every tree and then fixed: a gate of a tree
\* answers what the tree is, not what the lander hopes, so a tree gated twice answers
\* twice the same, and a batch green alone may be red combined with another (two cards
\* each declaring one name in two files).
\*
\* THE STATE.
\*   phase[s]  queued (its cards in merging, nothing built; waiting for a slot), merging
\*             (phase 1 in its worktree), green (gated at cut[s], waiting to land),
\*             landed, stopped (the card's own fact: a red gate blames the head that turned
\*             the tree red, LandBisect.tla), abandoned (LandDeadline: out of this pass,
\*             queued again for the next)
\*   stuck     the streams whose gate never answers (a bench that never returns), fixed at
\*             Init: every subset is an instance
\*   cut[s]    the base tree the batch was cut from
\*   base      the base tree: the batches landed
\*   green     the oracle: each tree green or red
\*   running   the streams in phase 1 at once (the worker bound, --land-parallel)
\*   pushes    a ghost: every push, with its tree, the batches landed since the cut, and
\*             how it was justified: own (the base did not move: the batch's own gate),
\*             disjoint (the base moved; the batch's files and the files landed since are
\*             disjoint: a clean merge, no new gate), combined (the base moved and the
\*             files met: one gate of the combined tree)
\*   collided  a ghost: the streams a red combined gate refused in this pass, cleared as
\*             each starts merging again
\*   cached    the trees recorded as gated (baseGateCache): a tip whose whole tree passed
\*             a gate with the tree tests (the batch's own, or the combined gate), never a
\*             clean merge of disjoint files, which no gate saw as a tree; empty at first,
\*             so the first pass gates the base it finds
\* Shrinks says the outside may make a re-merge onto a moved base merge fewer heads than
\* the batch merged alone (a head conflicting with what landed): on, in every instance
\* but the liveness ones, where a stream stopped on such a conflict is not a batch that
\* fails to land.
\*
\* THE ACTIONS. Phase 1, any order, up to Width at once: StartMerge(s) cuts the batch
\* from the base as it is; the base's own gate runs first unless the base is in the cache
\* (BaseRed(s): a red base refuses the batch for the pass, no head merged, as the base-gate
\* rule does before its third refusal; its cure is below this module's grain); Gate(s)
\* gates the batch's tree once (green: the batch waits to land; red: the bisection blames
\* the head that turned it red, the batch is cut to the prefix before it and that prefix
\* goes on to land while the blamed head is reworked with the stream going on; a refusal
\* that blames no head -- a bench that did not run the gate, a cancelled gate, a bisect
\* environment refusal -- returns the batch to queued; a batch whose tree tests read none
\* of its files lands whole, nothing blamed). Abandon(s) is
\* LandDeadline on the stream's own clock, started when it took its slot: a stuck gate
\* cancelled and its slot freed, the batch out of this pass with nothing cached, no stop
\* and no blame. A stream waiting for a slot, the same-repository/base chain or the
\* per-commit gate has no clock of its own: the holder's bound frees it (2026-10-10; before,
\* the pass ran the clocks in priority order and cancelled waiters, so a stream behind k
\* slow ones waited k x LandDeadline). Phase 2, as each batch is built, one landing at a
\* time (each is one atomic step: the merge after stays serial), whatever stream is still
\* merging: Push(s) for a batch cut from the tip the base still has; PushDisjoint(s) and
\* PushCombined(s) for a batch whose base moved, merged again onto it: pushed with no new
\* gate when its files and the files landed since are disjoint and every head merged
\* again, else gated once combined and pushed when green; Refuse(s) when that gate is red:
\* the batch stays queued for the next pass, the base unchanged, no stream stopped.
\* ShrinkConflict(s) is a re-merge where the first head no longer merges: nothing is
\* pushed and the stream stops on the conflict; ShrinkPrefix(s) is one where fewer heads
\* merge: the prefix is gated combined whatever the files (a tree not the one gated),
\* pushed when green, refused when red, and the stream stops on the conflict after its
\* push. Requeue(s) is the next pass, an abandoned batch queued for it.
\*
\* THE RULES.
\*   BoundHeld: never more than Width streams merge at once.
\*   BaseAdvancesGated: every push is of a tree that passed a gate (own or combined), or a
\*     clean merge of disjoint files onto a tip that did.
\*   LandsOnce: a batch (its cards) is pushed at most once.
\*   BaseKeepsLandings: a landed batch is in the base: the merge after never loses one.
\*   RefusedStaysMerging: a batch refused for a collision is still queued (merging in the
\*     store; abandoned at a deadline is queued too), not landed and not stopped.
\*   AbandonTouchesNothingElse (an action property): an abandonment changes the base, the
\*     cache, the pushes and no other stream's phase.
\*   RefusalTouchesNoOtherStream (an action property): a refusal changes the base and no
\*     other stream's phase or cut.
\*   UnblamedRefusalQueues (an action property): a gate that refuses the batch with no head
\*     blamed leaves it queued, never stopped.
\*   NoHeadPastItsBlame: no head at or after the blamed one lands in the pass that blames it.
\*   CachedIsGreen: every tree recorded as gated is green: a disjoint merge is never
\*     recorded, so the next pass's base gate runs on it and a base red from two heads green
\*     alone is found there, blaming no head.
\*   Lands (liveness, under fairness, Shrinks off): when every tree is green and no gate is
\*     stuck, every batch lands.
\*   Leaves (liveness, under fairness): when every tree is green, whatever gates never
\*     answer, every batch in phase 1 leaves it, built, stopped or abandoned: a stuck gate
\*     holds neither the pass nor a stream waiting behind it for ever.
\*   NoHeadOfLine (liveness, SlowSpec: no deadline counted on, a slot for every stream):
\*     when every tree is green, every stream whose gate answers lands, however long
\*     another stream's gate runs.
\*
\* Broken: "none" is the design.
\*   "inorder"     a built batch lands only once every stream before it in Order has left
\*                 phase 1: the priority-order landing of before 2026-10-10 (NoHeadOfLine
\*                 fails: s1's gate runs on, s2 is built and never lands; fault item 4,
\*                 v1-4's 99-card batch held every later stream's landing).
\*   "nogate"      pushes a moved batch with no combined gate whatever the files
\*                 (BaseAdvancesGated fails).
\*   "nocut"       pushes the batch's own tip over a moved base, as if nothing had landed
\*                 (BaseKeepsLandings fails: the earlier landing is gone from the base).
\*   "stopcollide" records a red combined gate as the conflict fact, stopping the stream
\*                 (RefusedStaysMerging fails).
\*   "revert"      a red combined gate resets the base to the batch's cut
\*                 (RefusalTouchesNoOtherStream fails).
\*   "cachedisjoint" records a disjoint merge's tip as gated (CachedIsGreen fails: the
\*                 next pass gates no base and the red tree is trusted).
\*   "pushempty"   a re-merge that merges no head is reported landed all the same, the
\*                 base's own tip pushed and a batch of none reported (BaseKeepsLandings
\*                 fails: landed, and not in the base).
\*   "landpastblame" a blamed red gate pushes the whole batch, heads at and after the blamed
\*                 one included (NoHeadPastItsBlame fails).
\*   "unblamedstops" a refusal that blames no head stops the stream (UnblamedRefusalQueues
\*                 fails: the batch is not queued).
\* The "plainwait" witness of 2026-10-09 (a waiter ignoring the pass's cancellation) is
\* retired with the pass's cancellation of waiters: no waiter is cancelled now.
\*
\* WHAT IS NOT MODELLED. The report (Land.tla), the base's own gate and its cure (the base
\* is green here), --check, pushes from outside the pass (a rejected push is met once more
\* and reported, as before), the heads inside a batch beyond the one blamed and the prefix
\* before it (LandBisect.tla holds the search), the batch's size
\* (landBatchMax), the store, and landGrace (a built batch waits up to 30 s for a
\* higher-priority stream still merging: timing, so every order of landings is a behaviour
\* here, and NoHeadOfLine holds whatever the grace).
\*
\* TLC, 2026-10-11, on a Linux bench, the same jar: the controls and the seven earlier
\* reversed witnesses pass or fail as before, with Heads and blame in every configuration;
\* the two new reversed witnesses each fail the property their configuration names
\* (MCLandPassBrokenLandPastBlame fails NoHeadPastItsBlame, MCLandPassBrokenUnblamedStops
\* fails UnblamedRefusalQueues). The records are tla/RUNS.tsv.
\*
\* TLC, 2026-10-10, hetzner2, the same jar: MCLandPass, MCLandPassSerial, MCLandPassLive
\* (Lands, Leaves) and MCLandPassSlow (NoHeadOfLine) pass; MCLandPassBrokenInOrder fails
\* NoHeadOfLine; the six other reversed witnesses each fail the property their
\* configuration names. The records are tla/RUNS.tsv.
\*
\* TLC, 2026-10-09, the same bench and jar: the three control configurations and the seven
\* reversed witnesses, plus MCLandPassLive checking Leaves and the retired
\* MCLandPassBrokenPlainWait failing it.
\*
\* TLC, 2026-10-07, on a Linux bench, tla2tools.jar as tla/tla2tools.sha256 pins it:
\* MCLandPass (three streams in order, two files, width 2, every oracle) passes every
\* invariant and RefusalTouchesNoOtherStream; MCLandPassSerial (width 1) passes the same;
\* MCLandPassLive (Shrinks off) passes Lands under fairness; the six reversed witnesses each fail the
\* property their configuration names. The records are tla/RUNS.tsv.
EXTENDS Integers, Sequences, FiniteSets, TLC

CONSTANTS Streams, Order, Files, Touches, Width, Shrinks, Broken, Heads, blame

\* blame[s] is the head the bisection blames when the stream's tree is red, or one of these
\* two outcomes instead: the gate refused the batch with no head blamed (Unblamed), or the
\* tree tests read none of the batch's files and the batch lands whole (Whole).
Unblamed == 0
Whole == -1

ASSUME Width \in Nat \ {0}
ASSUME Shrinks \in BOOLEAN
ASSUME Len(Order) = Cardinality(Streams) /\ {Order[i] : i \in 1..Len(Order)} = Streams
ASSUME Touches \in [Streams -> SUBSET Files]
ASSUME Heads \subseteq Nat \ {0} /\ Heads # {}
ASSUME blame \in [Streams -> (Heads \cup {Unblamed, Whole})]

VARIABLES phase, cut, batchBase, green, running, pushes, collided, cached, stuck

legacyVars == <<phase, cut, batchBase, green, running, pushes, collided, cached, stuck>>

VARIABLES sbranch, trunk, inflight, cert, base, column, streamBase, integrated, landedAt, moveFree, landProtected, edited, branchTrunk, firstLandCut, firstLandTrunk, markedPushes
flowVars == <<sbranch, trunk, inflight, cert, base, column, streamBase, integrated, landedAt, moveFree, landProtected, edited, branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>
vars == <<legacyVars, flowVars>>

Trees == SUBSET Streams

Phases == {"queued", "merging", "green", "landed", "stopped", "abandoned", "reworked"}

\* The files the batches in a tree touch.
Touched(T) == UNION {Touches[t] : t \in T}

\* The batches landed on the base since the stream's batch was cut.
Since(s) == batchBase \ cut[s]

\* The batch's files and the files landed since it was cut do not meet.
Disjoint(s) == Touches[s] \cap Touched(Since(s)) = {}

\* The heads of the stream's batch that are landed: all of them when the batch landed, the
\* prefix before the blamed head when a red gate reworked it, none while it is still on its
\* way (a head is <<card, attempt>> in the code; the reworked head is a new attempt, so this
\* module does not re-land it).
Prefix(s) == {h \in Heads : h < blame[s]}
Landed(s) ==
  IF phase[s] = "landed" THEN Heads
  ELSE IF phase[s] = "reworked" THEN Prefix(s)
  ELSE {}

TypeOK ==
  /\ phase \in [Streams -> Phases]
  /\ cut \in [Streams -> Trees]
  /\ batchBase \in Trees
  /\ green \in [Trees -> BOOLEAN]
  /\ running \in 0..Width
  /\ pushes \in Seq([s : Streams, tree : Trees, since : Trees, how : {"own", "disjoint", "combined", "prefix", "whole"}])
  /\ collided \subseteq Streams
  /\ cached \subseteq Trees
  /\ stuck \subseteq Streams

LegacyInit ==
  /\ phase = [s \in Streams |-> "queued"]
  /\ cut = [s \in Streams |-> {}]
  /\ batchBase = {}
  /\ green \in [Trees -> BOOLEAN]
  /\ running = 0
  /\ pushes = <<>>
  /\ collided = {}
  /\ cached = {}
  /\ stuck \in SUBSET Streams

\* A stream is in phase 1 (its batch is the pass's to merge, or waiting for a slot to).
InPass(s) == phase[s] \in {"queued", "merging"}

\* One push, recorded with the stream, the tree pushed, what landed since its cut and how.
Pushed(s, tree, how) == pushes' = Append(pushes, [s |-> s, tree |-> tree, since |-> Since(s), how |-> how])

\* ---- phase 1: the merges, in parallel (landpass.go, prepare and launch) ----

\* The stream's batch is cut from the base's tip as it is now, in its own worktree.
StartMerge(s) ==
  /\ phase[s] = "queued" /\ running < Width
  /\ phase' = [phase EXCEPT ![s] = "merging"]
  /\ cut' = [cut EXCEPT ![s] = batchBase]
  /\ running' = running + 1
  /\ collided' = collided \ {s}
  /\ UNCHANGED <<batchBase, green, pushes, cached, stuck>>

\* The base's own gate, before any head is merged, unless the base is recorded as gated:
\* a red base refuses the batch for the pass and blames no head (the base-gate rule; the
\* cure is below this grain). A stuck gate answers nothing.
BaseRed(s) ==
  /\ phase[s] = "merging" /\ s \notin stuck /\ cut[s] \notin cached /\ ~green[cut[s]]
  /\ phase' = [phase EXCEPT ![s] = "queued"]
  /\ running' = running - 1
  /\ UNCHANGED <<cut, batchBase, green, pushes, collided, cached, stuck>>

\* The base green (gated now and recorded, or recorded before), the batch's tree is gated
\* once: green, it waits to land; red, the head that turned it red is found by bisection
\* and the batch is cut to the prefix before it (which goes on to land) while the blamed
\* head is reworked, the stream going on; a refusal that blames no head returns the batch
\* to queued; and a batch whose tree tests read none of its files lands whole (LandBisect.tla
\* holds the search). A stuck gate answers nothing.
GateGuard(s) ==
  phase[s] = "merging" /\ s \notin stuck /\ (cut[s] \in cached \/ green[cut[s]])

GateGreen(s) ==
  /\ GateGuard(s) /\ green[cut[s] \cup {s}]
  /\ phase' = [phase EXCEPT ![s] = "green"]
  /\ running' = running - 1
  /\ cached' = cached \cup {cut[s]}
  /\ UNCHANGED <<cut, batchBase, green, pushes, collided, stuck>>

\* Red, blamed: the bisection blames one head (blame[s]); the prefix before it lands and the
\* head is reworked, the stream going on (landpastblame: the whole batch lands, heads at and
\* after the blamed one included, which NoHeadPastItsBlame catches).
GateBlame(s) ==
  /\ GateGuard(s) /\ ~green[cut[s] \cup {s}] /\ blame[s] \in Heads
  /\ phase' = [phase EXCEPT ![s] = IF Broken = "landpastblame" THEN "landed" ELSE "reworked"]
  /\ batchBase' = batchBase \cup {s}
  /\ running' = running - 1
  /\ cached' = cached \cup {cut[s]}
  /\ Pushed(s, batchBase \cup {s}, "prefix")
  /\ UNCHANGED <<cut, green, collided, stuck>>

\* Red, refused with no head blamed: a bench that did not run the gate, a cancelled gate, or
\* a bisect environment refusal leaves the batch queued, nothing landed (unblamedstops: the
\* stream stops on it, which UnblamedRefusalQueues catches).
GateUnblamed(s) ==
  /\ GateGuard(s) /\ ~green[cut[s] \cup {s}] /\ blame[s] = Unblamed
  /\ phase' = [phase EXCEPT ![s] = IF Broken = "unblamedstops" THEN "stopped" ELSE "queued"]
  /\ running' = running - 1
  /\ cached' = cached \cup {cut[s]}
  /\ UNCHANGED <<cut, batchBase, green, pushes, collided, stuck>>

\* Red, but the tree tests read none of the batch's files: the batch lands whole, nothing
\* blamed, and its tip is not recorded as gated (the tree tests never saw it green).
GateWhole(s) ==
  /\ GateGuard(s) /\ ~green[cut[s] \cup {s}] /\ blame[s] = Whole
  /\ phase' = [phase EXCEPT ![s] = "landed"]
  /\ batchBase' = batchBase \cup {s}
  /\ running' = running - 1
  /\ cached' = cached \cup {cut[s]}
  /\ Pushed(s, batchBase \cup {s}, "whole")
  /\ UNCHANGED <<cut, green, collided, stuck>>

Gate(s) == GateGreen(s) \/ GateBlame(s) \/ GateUnblamed(s) \/ GateWhole(s)

\* LandDeadline, on the stream's own clock (started when it took its slot): a stuck gate
\* is cancelled and its slot freed, the batch out of this pass with nothing cached, no
\* stop and no blame, its cards still queued in the store. A stream waiting for a slot,
\* the chain or the commit's gate has no clock running: the holder's own bound frees it.
Abandon(s) ==
  /\ phase[s] = "merging" /\ s \in stuck
  /\ phase' = [phase EXCEPT ![s] = "abandoned"]
  /\ running' = running - 1
  /\ UNCHANGED <<cut, batchBase, green, pushes, collided, cached, stuck>>

\* ---- phase 2: the landings, one at a time, as each batch is built (landpass.go, land) ----

\* A built batch lands when it is built, whatever stream is still merging: every landing
\* is one atomic step here, so landing onto the base stays serial (inorder: it waits until
\* every stream before it in Order has left phase 1, the head-of-line wait of before).
Landing(s) ==
  /\ phase[s] = "green"
  /\ Broken = "inorder" => \A i \in 1..Len(Order) : Order[i] = s => \A k \in 1..(i - 1) : ~InPass(Order[k])

\* The base still has the tip the batch was cut from: its gated tip is pushed, no new gate,
\* and recorded as gated.
Push(s) ==
  /\ Landing(s) /\ cut[s] = batchBase
  /\ batchBase' = batchBase \cup {s}
  /\ phase' = [phase EXCEPT ![s] = "landed"]
  /\ Pushed(s, batchBase \cup {s}, "own")
  /\ cached' = cached \cup {batchBase \cup {s}}
  /\ UNCHANGED <<cut, green, running, collided, stuck>>

\* The base moved: the batch is merged again onto its tip, every head merging; the files
\* disjoint, it is pushed with no new gate and is not recorded as gated (nogate: whatever
\* the files; nocut: the batch's own tip is pushed over the moved base, and what landed
\* since is gone from it; cachedisjoint: recorded as gated all the same).
PushDisjoint(s) ==
  /\ Landing(s) /\ cut[s] # batchBase
  /\ Broken = "nogate" \/ Disjoint(s)
  /\ batchBase' = IF Broken = "nocut" THEN cut[s] \cup {s} ELSE batchBase \cup {s}
  /\ phase' = [phase EXCEPT ![s] = "landed"]
  /\ Pushed(s, batchBase', "disjoint")
  /\ cached' = IF Broken = "cachedisjoint" THEN cached \cup {batchBase'} ELSE cached
  /\ UNCHANGED <<cut, green, running, collided, stuck>>

\* The base moved and the files met, or fewer heads merged (ShrinkPrefix): the combined
\* tree is gated once, and green, pushed and recorded as gated.
Combined(s) ==
  /\ green[batchBase \cup {s}]
  /\ batchBase' = batchBase \cup {s}
  /\ Pushed(s, batchBase \cup {s}, "combined")
  /\ cached' = cached \cup {batchBase \cup {s}}
  /\ UNCHANGED <<cut, green, running, collided, stuck>>

PushCombined(s) ==
  /\ Landing(s) /\ cut[s] # batchBase
  /\ Broken # "nogate" /\ ~Disjoint(s)
  /\ Combined(s)
  /\ phase' = [phase EXCEPT ![s] = "landed"]

\* The re-merge onto the moved base merges fewer heads than the batch alone did: the heads
\* before the conflict are gated combined whatever the files (the tree is not the one gated)
\* and pushed when green, and the stream stops on the conflict after its push; a red gate
\* refuses the batch as Refuse does.
ShrinkPrefix(s) ==
  /\ Shrinks /\ Landing(s) /\ cut[s] # batchBase
  /\ Broken # "nogate"
  /\ \/ /\ Combined(s)
        /\ phase' = [phase EXCEPT ![s] = "stopped"]
     \/ /\ ~green[batchBase \cup {s}]
        /\ phase' = [phase EXCEPT ![s] = "queued"]
        /\ collided' = collided \cup {s}
        /\ UNCHANGED <<cut, batchBase, green, running, pushes, cached, stuck>>

\* The re-merge onto the moved base merges no head: the first met a conflict there; nothing
\* is pushed and nothing reported as a batch, the stream stops on the conflict (pushempty:
\* the batch is reported landed all the same, the base unchanged).
ShrinkConflict(s) ==
  /\ Shrinks /\ Landing(s) /\ cut[s] # batchBase
  /\ phase' = [phase EXCEPT ![s] = IF Broken = "pushempty" THEN "landed" ELSE "stopped"]
  /\ UNCHANGED <<cut, batchBase, green, running, pushes, collided, cached, stuck>>

\* The combined tree is red: the batch is refused for this pass, its cards still queued,
\* nothing pushed, no fact, no stream stopped (stopcollide: the stream stops; revert: the
\* base is put back to the batch's cut).
Refuse(s) ==
  /\ Landing(s) /\ cut[s] # batchBase
  /\ Broken # "nogate" /\ ~Disjoint(s)
  /\ ~green[batchBase \cup {s}]
  /\ phase' = [phase EXCEPT ![s] = IF Broken = "stopcollide" THEN "stopped" ELSE "queued"]
  /\ batchBase' = IF Broken = "revert" THEN cut[s] ELSE batchBase
  /\ collided' = collided \cup {s}
  /\ UNCHANGED <<cut, green, running, pushes, cached, stuck>>

\* The pass ends when nothing is merging, waiting or built; the next pass begins, and a
\* batch abandoned in this one is queued for it (its cards never left the store's queue).
Requeue(s) ==
  /\ phase[s] = "abandoned"
  /\ \A t \in Streams : ~InPass(t) /\ phase[t] # "green"
  /\ phase' = [phase EXCEPT ![s] = "queued"]
  /\ UNCHANGED <<cut, batchBase, green, running, pushes, collided, cached, stuck>>

LegacyNext ==
  \E s \in Streams : StartMerge(s) \/ BaseRed(s) \/ Gate(s) \/ Abandon(s)
                     \/ Push(s) \/ PushDisjoint(s) \/ PushCombined(s)
                     \/ ShrinkPrefix(s) \/ ShrinkConflict(s) \/ Refuse(s) \/ Requeue(s)


\* ---- stream branches and trunk certification (separate abstraction) ----
\* Fault/policy selectors reuse Broken so historical configurations need no edits.
BugCardOntoDev == Broken = "cardontodev"
BugMergeBeforeCert == Broken = "mergebeforecert"
BugBaseSkipsReview == Broken = "baseskipsreview"
BugBriefBase == Broken = "briefbase"
BugEmptyStale == Broken = "emptystale"
BugRedRetry == Broken = "redretry"
BugNoRepair == Broken = "norepair"
BugUnmarkedLanding == Broken = "unmarkedlanding"
\* The current Go selector preserves already-cut working/review heads. Rewrite-all
\* is the requested stronger policy, not a claim that changing an active head's
\* base is already the agreed Go implementation. Merging/queued is repointed.
BaseRewritePolicy == IF BugBaseSkipsReview THEN "preserve-cut-head" ELSE "all-unlanded"
None == "none"
TrunkBranch == "trunk"
\* With two Heads this is three cards: two in the first stream, one in the other.
Cards == {c \in Streams \X Heads : c[1] = Order[1] \/ c[2] = CHOOSE h \in Heads : \A k \in Heads : h <= k}
Stream(c) == c[1]
Columns == {"absent", "waiting", "ready", "working", "review", "merging", "landed"}
Unlanded == {c \in Cards : column[c] \notin {"absent", "landed"}}

FlowStart(protected) ==
  /\ sbranch = [s \in Streams |-> {}]
  /\ trunk = <<>>
  /\ inflight = None
  /\ cert = None
  /\ base = [c \in Cards |-> None]
  /\ column = [c \in Cards |-> "absent"]
  /\ streamBase = [s \in Streams |-> TrunkBranch]
  /\ integrated = [s \in Streams |-> {}]
  /\ landedAt = [c \in Cards |-> None]
  /\ moveFree = <<>>
  /\ landProtected = protected
  /\ edited = {}
  /\ branchTrunk = [s \in Streams |-> 0]
  /\ firstLandCut = [s \in Streams |-> 0]
  /\ firstLandTrunk = [s \in Streams |-> 0]
  /\ markedPushes = <<>>
FlowIdle == FlowStart({})

AddCard(c) ==
  /\ column[c] = "absent"
  /\ base' = [base EXCEPT ![c] = streamBase[Stream(c)]]
  /\ column' = [column EXCEPT ![c] = "waiting"]
  /\ UNCHANGED <<sbranch, trunk, inflight, cert, streamBase, integrated, landedAt,
                 moveFree, landProtected, edited, branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>

AdvanceCard(c) ==
  /\ column[c] \in {"waiting", "ready", "working", "review"}
  /\ column' = [column EXCEPT ![c] = CASE @ = "waiting" -> "ready"
                                         [] @ = "ready" -> "working"
                                         [] @ = "working" -> "review"
                                         [] OTHER -> "merging"]
  /\ UNCHANGED <<sbranch, trunk, inflight, cert, base, streamBase, integrated, landedAt,
                 moveFree, landProtected, edited, branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>

SetBase(s, b) ==
  /\ b = s /\ streamBase[s] = TrunkBranch
  /\ streamBase' = [streamBase EXCEPT ![s] = b]
  /\ base' = [c \in Cards |->
       IF Stream(c) = s /\ c \in Unlanded
          /\ ~(BaseRewritePolicy = "preserve-cut-head" /\ column[c] \in {"working", "review"})
       THEN b ELSE base[c]]
  /\ UNCHANGED <<sbranch, trunk, inflight, cert, column, integrated, landedAt,
                 moveFree, landProtected, edited, branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>

\* One attempted brief edit per card. Its payload is independent input, not a
\* pre-checked invariant; the fault writes its proposed BASE into the real state.
BriefEdit(c, proposedBase) ==
  /\ c \in Unlanded \ edited /\ proposedBase \in {Stream(c), TrunkBranch}
  /\ edited' = edited \cup {c}
  /\ base' = IF BugBriefBase THEN [base EXCEPT ![c] = proposedBase] ELSE base
  /\ UNCHANGED <<sbranch, trunk, inflight, cert, column, streamBase, integrated, landedAt,
                 moveFree, landProtected, branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>

\* Singleton batches, oldest first; the first stream can land a second batch
\* while a frozen snapshot of its first is being certified on the trunk.
ReadyBatch(s) == {c \in Cards : Stream(c) = s /\ column[c] = "merging"
                  /\ \A d \in Cards : Stream(d) = s /\ d[2] < c[2] => column[d] = "landed"}
Batch(s) == ReadyBatch(s)
Eligible(s) == Batch(s) # {}
MoveAllowed == inflight = None /\ (cert = "green" \/ trunk = <<>>)

\* A sibling can advance the trunk while this stream is empty. Fast-forward is
\* separate from that move, but must finish before the stream's first landing.
FollowTrunk(s) ==
  /\ sbranch[s] = {} /\ branchTrunk[s] < Len(trunk)
  /\ branchTrunk' = [branchTrunk EXCEPT ![s] = Len(trunk)]
  /\ UNCHANGED <<sbranch, trunk, inflight, cert, base, column, streamBase, integrated,
                 landedAt, moveFree, landProtected, edited, firstLandCut, firstLandTrunk, markedPushes>>

StreamLand(s) ==
  /\ Eligible(s) /\ streamBase[s] = s
  /\ \A c \in Batch(s) : base[c] = s
  /\ sbranch[s] # {} \/ branchTrunk[s] = Len(trunk) \/ BugEmptyStale
  /\ sbranch' = [sbranch EXCEPT ![s] = @ \cup Batch(s)]
  /\ column' = [c \in Cards |-> IF c \in Batch(s) THEN "landed" ELSE column[c]]
  /\ landedAt' = [c \in Cards |-> IF c \in Batch(s) THEN s ELSE landedAt[c]]
  /\ firstLandCut' = IF sbranch[s] = {} THEN [firstLandCut EXCEPT ![s] = branchTrunk[s]] ELSE firstLandCut
  /\ firstLandTrunk' = IF sbranch[s] = {} THEN [firstLandTrunk EXCEPT ![s] = Len(trunk)] ELSE firstLandTrunk
  /\ UNCHANGED <<trunk, inflight, cert, base, streamBase, integrated, moveFree,
                 landProtected, edited, branchTrunk, markedPushes>>

\* The actual land_protected mark is independent of the skip-base fault. An
\* unmarked stream is refused before git (land.go placeWhy); only the explicit
\* unmarkedlanding mutation removes that guard. The normal design forbids even
\* marked direct pushes; cardontodev restores the historical marked exception.
CardOntoTrunk(s) ==
  /\ Eligible(s) /\ (BugCardOntoDev \/ BugUnmarkedLanding)
  /\ s \in landProtected \/ BugUnmarkedLanding
  /\ \A c \in Batch(s) : base[c] = TrunkBranch
  /\ trunk' = Append(trunk, <<"card", Batch(s)>>)
  /\ moveFree' = Append(moveFree, MoveAllowed)
  /\ markedPushes' = Append(markedPushes, s \in landProtected)
  /\ cert' = IF cert = "running" THEN "cancelled" ELSE "running"
  /\ column' = [c \in Cards |-> IF c \in Batch(s) THEN "landed" ELSE column[c]]
  /\ landedAt' = [c \in Cards |-> IF c \in Batch(s) THEN TrunkBranch ELSE landedAt[c]]
  /\ UNCHANGED <<sbranch, inflight, base, streamBase, integrated, landProtected,
                 edited, branchTrunk, firstLandCut, firstLandTrunk>>

StreamMerge(s) ==
  /\ sbranch[s] \ integrated[s] # {}
  /\ MoveAllowed \/ (BugMergeBeforeCert /\ cert = "running")
  /\ trunk' = Append(trunk, <<"merge", s>>)
  /\ moveFree' = Append(moveFree, MoveAllowed)
  /\ integrated' = [integrated EXCEPT ![s] = sbranch[s]]
  /\ inflight' = s
  /\ cert' = "running"
  /\ UNCHANGED <<sbranch, base, column, streamBase, landedAt, landProtected, edited,
                 branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>

CertDone(r) ==
  /\ cert = "running" /\ r \in {"green", "red"}
  /\ cert' = r
  /\ inflight' = None
  /\ UNCHANGED <<sbranch, trunk, base, column, streamBase, integrated, landedAt, moveFree,
                 landProtected, edited, branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>
CertFinish == \E r \in {"green", "red"} : CertDone(r)

\* Fix is a SUCCESSFUL repair-and-recertification of the same trunk move, not a
\* new merge. Weak fairness of this action is the explicit recovery assumption.
\* redretry weakens it to a mere retry: fair red/retry cycles then defeat liveness.
Fix ==
  /\ cert = "red" /\ ~BugNoRepair
  /\ cert' = IF BugRedRetry THEN "running" ELSE "green"
  /\ UNCHANGED <<sbranch, trunk, inflight, base, column, streamBase, integrated, landedAt,
                 moveFree, landProtected, edited, branchTrunk, firstLandCut, firstLandTrunk, markedPushes>>

FlowComplete ==
  /\ \A c \in Cards : column[c] = "landed" /\ c \in integrated[Stream(c)]
  /\ inflight = None /\ cert = "green"
\* The only explicit stutter is completed work. Incomplete wedges are deadlocks,
\* not "ignore-terminal" passes. Brief edits are finite, non-stuttering events.
Quiesce == FlowComplete /\ UNCHANGED flowVars

FlowNext ==
  \/ \E c \in Cards : AddCard(c) \/ AdvanceCard(c)
                        \/ \E b \in Streams \cup {TrunkBranch} : BriefEdit(c, b)
  \/ \E s \in Streams : SetBase(s, s) \/ FollowTrunk(s) \/ StreamLand(s)
                         \/ CardOntoTrunk(s) \/ StreamMerge(s)
  \/ CertFinish \/ Fix \/ Quiesce

StreamFlowInit ==
  /\ LegacyInit /\ stuck = {} /\ \A T \in Trees : green[T]
  /\ \E protected \in SUBSET Streams : FlowStart(protected)
StreamFlowNext == FlowNext /\ UNCHANGED legacyVars
StreamFlowSpec == StreamFlowInit /\ [][StreamFlowNext]_vars
StreamFlowFairSpec ==
  /\ StreamFlowSpec
  /\ \A c \in Cards : WF_flowVars(AddCard(c)) /\ WF_flowVars(AdvanceCard(c))
  /\ \A s \in Streams : WF_flowVars(SetBase(s, s)) /\ WF_flowVars(FollowTrunk(s))
                         /\ WF_flowVars(StreamLand(s)) /\ WF_flowVars(StreamMerge(s))
  /\ WF_flowVars(CertFinish) /\ WF_flowVars(Fix)

FlowTypeOK ==
  /\ sbranch \in [Streams -> SUBSET Cards]
  /\ integrated \in [Streams -> SUBSET Cards]
  /\ trunk \in Seq(({"merge"} \X Streams) \cup ({"card"} \X SUBSET Cards))
  /\ inflight \in Streams \cup {None}
  /\ cert \in {None, "running", "green", "red", "cancelled"}
  /\ base \in [Cards -> Streams \cup {TrunkBranch, None}]
  /\ column \in [Cards -> Columns]
  /\ streamBase \in [Streams -> Streams \cup {TrunkBranch}]
  /\ landedAt \in [Cards -> Streams \cup {TrunkBranch, None}]
  /\ moveFree \in Seq(BOOLEAN) /\ Len(moveFree) = Len(trunk)
  /\ landProtected \subseteq Streams /\ edited \subseteq Cards
  /\ branchTrunk \in [Streams -> 0..Len(trunk)]
  /\ firstLandCut \in [Streams -> 0..Len(trunk)]
  /\ firstLandTrunk \in [Streams -> 0..Len(trunk)]
  /\ markedPushes \in Seq(BOOLEAN)

TrunkMovesOnlyByStreamMerge == \A i \in 1..Len(trunk) : trunk[i][1] = "merge"
DevMovesOnlyByStreamMerge == TrunkMovesOnlyByStreamMerge
\* PRE-state guard history survives CertDone and detects overlapping certification
\* admission. The forge's merge queue already serializes actual Git trunk writes.
OneStreamMergeInflightPerTrunk == cert # "cancelled" /\ \A i \in 1..Len(moveFree) : moveFree[i]
StreamMergesSerial == OneStreamMergeInflightPerTrunk
ProtectedLandingRequiresMark == \A i \in 1..Len(markedPushes) : markedPushes[i]
CardLandsOnItsStream ==
  \A c \in Cards : column[c] = "landed" =>
    landedAt[c] = Stream(c) /\ c \in sbranch[Stream(c)]
NoUnlandedCardBasesOnTrunk ==
  \A c \in Unlanded : streamBase[Stream(c)] = Stream(c) => base[c] = Stream(c)
EmptyStreamFollowsTrunk ==
  \A s \in Streams : sbranch[s] # {} => firstLandCut[s] = firstLandTrunk[s]
SetBaseReachesEveryUnlanded ==
  [][\A s \in Streams : SetBase(s, s) =>
       \A c \in Unlanded : Stream(c) = s => base'[c] = s]_vars
BriefEditKeepsBase ==
  [][\A c \in Cards : \A b \in Streams \cup {TrunkBranch} :
       BriefEdit(c, b) => base' = base]_vars
LandedEventuallyIntegrated ==
  \A c \in Cards : [](column[c] = "landed" => <>(c \in integrated[Stream(c)]))
AllFlowWorkCompletes == <>FlowComplete
\* This structural invariant catches a partial-completion terminal even in a
\* witness run: no broad stutter is used to conceal a missing repair action.
NoPartialCompletionWedge == FlowComplete \/ ENABLED StreamFlowNext

Init == LegacyInit /\ FlowIdle
Next == LegacyNext /\ UNCHANGED flowVars
Spec == Init /\ [][Next]_vars

\* Every action weakly fair but the deadline: a slow gate may hold its stream as long as
\* it likes within its bound, and no other stream's landing waits for it.
SlowSpec ==
  /\ Spec
  /\ \A s \in Streams : WF_legacyVars(StartMerge(s)) /\ WF_legacyVars(BaseRed(s)) /\ WF_legacyVars(Gate(s)) /\ WF_legacyVars(Push(s))
                        /\ WF_legacyVars(PushDisjoint(s)) /\ WF_legacyVars(PushCombined(s)) /\ WF_legacyVars(Refuse(s))
                        /\ WF_legacyVars(Requeue(s))

\* Every action weakly fair: the loop runs pass after pass, and every deadline fires.
FairSpec == SlowSpec /\ \A s \in Streams : WF_legacyVars(Abandon(s))

\* ---- the rules ----

BoundHeld == running <= Width

\* Every push is of a tree that passed a gate, or a clean merge of disjoint files onto a
\* tip that did: an own push saw no landing since its cut; a combined push gated the tree
\* it pushed; a disjoint push's files and the files landed since its cut do not meet.
BaseAdvancesGated ==
  \A i \in 1..Len(pushes) :
    LET p == pushes[i] IN
      /\ p.how = "own" => p.since = {} /\ green[p.tree]
      /\ p.how = "combined" => green[p.tree]
      /\ p.how = "disjoint" => Touches[p.s] \cap Touched(p.since) = {}
      /\ p.how = "prefix" => blame[p.s] \in Heads
      /\ p.how = "whole" => blame[p.s] = Whole

LandsOnce == \A s \in Streams : Cardinality({i \in 1..Len(pushes) : pushes[i].s = s}) <= 1

BaseKeepsLandings == \A s \in Streams : phase[s] \in {"landed", "reworked"} => s \in batchBase

\* A refused batch is still queued in the store (and so is one abandoned at a deadline).
RefusedStaysMerging == \A s \in collided : phase[s] \in {"queued", "abandoned"}

CachedIsGreen == \A T \in cached : green[T]

\* No head at or after the blamed one lands in the pass that blames it: a blamed red gate
\* (a "prefix" push) lands only the prefix before the blamed head, the head reworked and
\* nothing after it landed.
NoHeadPastItsBlame ==
  \A i \in 1..Len(pushes) :
    LET p == pushes[i] IN
      p.how = "prefix" => Landed(p.s) \cap {h \in Heads : h >= blame[p.s]} = {}

RefusalTouchesNoOtherStream ==
  [][\A s \in Streams : Refuse(s) => batchBase' = batchBase /\ \A t \in Streams \ {s} : phase'[t] = phase[t] /\ cut'[t] = cut[t]]_vars

\* An abandonment changes the base, the cache, the pushes and no other stream's phase.
AbandonTouchesNothingElse ==
  [][\A s \in Streams : Abandon(s) => batchBase' = batchBase /\ cached' = cached /\ pushes' = pushes /\ \A t \in Streams \ {s} : phase'[t] = phase[t]]_vars

\* A gate that refuses the batch with no head blamed leaves it queued, never stopped.
UnblamedRefusalQueues ==
  [][\A s \in Streams : GateUnblamed(s) => phase'[s] = "queued"]_vars

\* Every tree green, no gate stuck and no re-merge dropping a head (Shrinks off), every batch
\* lands, under fairness.
Lands == (stuck = {} /\ \A T \in Trees : green[T]) => \A s \in Streams : <>(phase[s] = "landed")

\* Every tree green, whatever gates never answer: a batch in phase 1 leaves it (built,
\* stopped or abandoned), so no stuck gate holds the pass, and no stream behind it, for ever.
Leaves == (\A T \in Trees : green[T]) => \A s \in Streams : [](InPass(s) => <>(~InPass(s)))

\* Every tree green and a slot for every stream: a stream whose gate answers lands however
\* long another stream's gate runs (SlowSpec: no deadline counted on). Before 2026-10-10 the
\* landings went in priority order, each waiting for the streams before it to leave phase 1,
\* so one slow batch (v1-4's 99 cards, 708 s) held every later stream's landing.
NoHeadOfLine == (\A T \in Trees : green[T]) => \A s \in Streams : s \notin stuck => <>(phase[s] = "landed")

=============================================================================
