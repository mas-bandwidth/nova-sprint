---------------------------- MODULE StoreFence ----------------------------
\* The store's fence and commit core, layer 1 of the layered rewrite
\* (docs/layers/L1-STORE-CONTRACT.md, guarantees G3 to G7 and G14; the
\* COVERAGE.tsv row pending-fenced-op).
\*
\* The code it models (internal/sprint/store):
\*   Check       engine.go Store.callerOp, run after each fenced read: a result
\*               recorded at this epoch is replayed for the same arguments and
\*               refused for others (replay, G6); one recorded at an earlier
\*               epoch is refused (DoneBefore, G7). The fenced read and the check
\*               are one action: a fenced read finishes or waits out a pending
\*               operation first (fencedStep, G2), and any commit after the read
\*               needs an Acquire, which moves the generation.
\*   Acquire     redis.go Redis.Acquire: the operation goes in the fence and the
\*               generation advances only when the fence is empty, the generation
\*               is the one read and the sprint is still at the pinned epoch (G3).
\*               The operation id is OpFamily(caller op, epoch)-attempt
\*               (engine.go:817, schema.go OpFamily): two writers serving one
\*               caller op build the same ids, attempt by attempt.
\*   Send        engine.go Store.apply: the writer's manifests in order, with no
\*               check of the fence. A manifest whose id the table layer already
\*               holds (replay, or OPCONFLICT for other bytes) counts as applied
\*               (engine.go:1873). A refused first manifest: the writer asks
\*               whether its operation was committed by another writer
\*               (committed, engine.go:1175), else releases it uncommitted
\*               (Release(op, false)) and plans again, or is refused whole on a
\*               bound or rule.
\*   Commit      redis.go Redis.Release(op, true): when the fence holds an
\*               operation with this id (its record's id prefix, redis.go:727-735;
\*               mem.go compares the id), one MULTI/EXEC records this writer's
\*               result under the caller op and empties the fence (G4); else it
\*               writes nothing and returns success (G5), and the writer tells its
\*               caller ok.
\*   Repair      engine.go Store.finishOp: the repairer sends the pending
\*               operation's recorded manifests itself (:2217). One whose first
\*               manifest applies, or was applied, is finished and committed. One
\*               whose first manifest is refused is abandoned at once when the
\*               refusal is a bound or rule (not curable: MEMBEREXISTS, LIMIT, a
\*               malformed manifest; engine.go:1352, :2253), and on state only past
\*               the grace (:2253-2282). An abandonment records no caller result.
\*   Stray       a table write outside the fence (a guarded row write, G12). It
\*               can break a planned first manifest's expectation or make a
\*               refused one hold again (a member deleted, a property restored).
\*   Clear       the epoch advances only while the fence is empty (G10).
\*
\* exp[w] says writer w's planned first manifest would apply now. A plan is made
\* against the state read (TRUE). When another plan's manifest applies, whether it
\* still applies is left open: a revision refusal may be resent at a fresh
\* revision when every member still matches (resendFresh, G16), or not. Later
\* manifests apply when sent. appliedBy records whose plan each manifest id holds,
\* so a mixed operation shows.
\*
\* Timely says a writer holding the fence either finishes within the grace or is
\* dead, so the grace passes only over a dead writer's operation. Abandonment on a
\* bound or rule needs no grace, so it reaches a live writer either way.
\*
\* Not modelled: the lock path (lock.go takeLock, Relock; Relock keeps the
\* generation), a clear while the fence holds the clear's own operation and the
\* per-epoch fence key, a first manifest finished entry by entry with skips
\* (changedAny), a later manifest refused, and the resend of a lost reply (G15).
\*
\* Broken names a reversed witness (one config each):
\*   "replayignoresargs" replay without comparing arguments: breaks ReplayMatches
\*   "rerunoldepoch"     no DoneBefore check: breaks CallerCommitsOnce
\*   "releaseanyop"      release empties the fence whatever it holds: breaks
\*                       NoPartialOutsideFence
\*   "abandonapplied"    repair abandons an aged operation even when its first
\*                       manifest applied: breaks AbandonedAppliedNothing
\*   "repairnosend"      repair abandons without sending the first manifest:
\*                       breaks AbandonedAppliedNothing
\*   "acquirenogen"      Acquire without the generation check: breaks
\*                       CallerCommitsOnce

EXTENDS Naturals, FiniteSets

\* None is a model value: an empty fence, slot or answer, unequal to every record.
\* Nonce FALSE is the code: an operation id is OpFamily(caller op, epoch)-attempt.
\* Nonce TRUE is the proposed fix: the id also carries its writer's nonce, so two
\* writers serving one caller op never share an id.
\* Revive TRUE lets a write make a refused first manifest apply again (a member
\* deleted after a MEMBEREXISTS refusal); FALSE keeps a refused expectation refused
\* until its writer plans again.
CONSTANTS Writer, Caller, Arg, Manifests, MaxTries, MaxEpoch, MaxStray, Timely, Broken,
          Nonce, Revive, None

NonceSet == IF Nonce THEN Writer ELSE {None}
Ids == Caller \X (1..MaxEpoch) \X (1..MaxTries) \X NonceSet
Mids == Ids \X (0..(Manifests - 1))
Kinds == {"ok", "replay", "conflict", "refused", "lost"}

VARIABLES gen, epoch, fence, appliedBy, exp, done, commits, abandoned,
          pc, req, rgen, repoch, cur, next, tries, said, strays

vars == <<gen, epoch, fence, appliedBy, exp, done, commits, abandoned,
          pc, req, rgen, repoch, cur, next, tries, said, strays>>

Fences == [id : Ids, c : Caller, a : Arg, e : 1..MaxEpoch, plan : Writer, aged : BOOLEAN]
Receipts == [c : Caller, a : Arg, e : 1..MaxEpoch, id : Ids]
Commits == [c : Caller, id : Ids, plan : Writer]

TypeOK ==
  /\ gen \in Nat
  /\ epoch \in 1..MaxEpoch
  /\ fence \in Fences \cup {None}
  /\ appliedBy \in [Mids -> Writer \cup {None}]
  /\ exp \in [Writer -> BOOLEAN]
  /\ done \subseteq Receipts
  /\ commits \subseteq Commits
  /\ abandoned \subseteq (Ids \X Writer)
  /\ pc \in [Writer -> {"idle", "check", "read", "held", "out", "dead"}]
  /\ req \in [Writer -> [c : Caller, a : Arg] \cup {None}]
  /\ cur \in [Writer -> Ids \cup {None}]
  /\ next \in [Writer -> 0..Manifests]
  /\ tries \in [Writer -> 0..MaxTries]
  /\ said \in [Writer -> [kind : Kinds, id : Ids \cup {None}, a : Arg \cup {None}] \cup {None}]
  /\ strays \in 0..MaxStray

Init ==
  /\ gen = 0
  /\ epoch = 1
  /\ fence = None
  /\ appliedBy = [m \in Mids |-> None]
  /\ exp = [w \in Writer |-> TRUE]
  /\ done = {}
  /\ commits = {}
  /\ abandoned = {}
  /\ pc = [w \in Writer |-> "idle"]
  /\ req = [w \in Writer |-> None]
  /\ rgen = [w \in Writer |-> 0]
  /\ repoch = [w \in Writer |-> 1]
  /\ cur = [w \in Writer |-> None]
  /\ next = [w \in Writer |-> 0]
  /\ tries = [w \in Writer |-> 0]
  /\ said = [w \in Writer |-> None]
  /\ strays = 0

DoneNow(c) == {d \in done : d.c = c /\ d.e = epoch}
DoneBefore(c) == {d \in done : d.c = c /\ d.e < epoch}
\* Release's test: the fence holds an operation with this id, whoever planned it.
HoldsId(id) == fence # None /\ fence.id = id
Tell(w, k, id, a) == said' = [said EXCEPT ![w] = [kind |-> k, id |-> id, a |-> a]]
Receipt(f) == [c |-> f.c, a |-> f.a, e |-> f.e, id |-> f.id]
\* When plan p's manifest applies, whether every other plan's first manifest still
\* applies is left open; a refused one stays refused unless Revive.
OthersOpen(p) ==
  \E e \in [Writer -> BOOLEAN] :
    /\ e[p] = exp[p]
    /\ \A x \in Writer : (~Revive /\ ~exp[x]) => ~e[x]
    /\ exp' = e

\* A caller asks writer w to run operation c with arguments a.
Start(w, c, a) ==
  /\ pc[w] = "idle"
  /\ pc' = [pc EXCEPT ![w] = "check"]
  /\ req' = [req EXCEPT ![w] = [c |-> c, a |-> a]]
  /\ UNCHANGED <<gen, epoch, fence, appliedBy, exp, done, commits, abandoned,
                 rgen, repoch, cur, next, tries, said, strays>>

\* The fenced read and the caller-op check (Store.callerOp); a new plan.
Check(w) ==
  /\ pc[w] = "check"
  /\ fence = None
  /\ LET r == req[w] IN
     IF DoneNow(r.c) # {}
     THEN LET d == CHOOSE x \in DoneNow(r.c) : TRUE IN
          /\ IF d.a = r.a \/ Broken = "replayignoresargs"
             THEN Tell(w, "replay", d.id, d.a)
             ELSE Tell(w, "conflict", None, None)
          /\ pc' = [pc EXCEPT ![w] = "out"]
          /\ UNCHANGED <<rgen, repoch, exp, next>>
     ELSE IF DoneBefore(r.c) # {} /\ Broken # "rerunoldepoch"
     THEN /\ Tell(w, "refused", None, None)
          /\ pc' = [pc EXCEPT ![w] = "out"]
          /\ UNCHANGED <<rgen, repoch, exp, next>>
     ELSE IF tries[w] = MaxTries
     THEN /\ Tell(w, "lost", None, None)
          /\ pc' = [pc EXCEPT ![w] = "out"]
          /\ UNCHANGED <<rgen, repoch, exp, next>>
     ELSE /\ rgen' = [rgen EXCEPT ![w] = gen]
          /\ repoch' = [repoch EXCEPT ![w] = epoch]
          /\ exp' = [exp EXCEPT ![w] = TRUE]
          /\ next' = [next EXCEPT ![w] = 0]
          /\ pc' = [pc EXCEPT ![w] = "read"]
          /\ UNCHANGED said
  /\ UNCHANGED <<gen, epoch, fence, appliedBy, done, commits, abandoned,
                 req, cur, tries, strays>>

\* Redis.Acquire, the id OpFamily(caller op, epoch)-attempt.
Acquire(w) ==
  /\ pc[w] = "read"
  /\ tries' = [tries EXCEPT ![w] = @ + 1]
  /\ LET id == <<req[w].c, epoch, tries[w] + 1, IF Nonce THEN w ELSE None>> IN
     IF fence = None /\ (gen = rgen[w] \/ Broken = "acquirenogen") /\ epoch = repoch[w]
     THEN /\ fence' = [id |-> id, c |-> req[w].c, a |-> req[w].a, e |-> epoch,
                       plan |-> w, aged |-> FALSE]
          /\ gen' = gen + 1
          /\ cur' = [cur EXCEPT ![w] = id]
          /\ pc' = [pc EXCEPT ![w] = "held"]
     ELSE /\ pc' = [pc EXCEPT ![w] = "check"]
          /\ UNCHANGED <<fence, gen, cur>>
  /\ UNCHANGED <<epoch, appliedBy, exp, done, commits, abandoned, req, rgen, repoch,
                 next, said, strays>>

\* Store.apply: the writer's next manifest, with no check of the fence.
Send(w) ==
  /\ pc[w] = "held"
  /\ next[w] < Manifests
  /\ LET m == <<cur[w], next[w]>> IN
     IF appliedBy[m] # None
     THEN \* the table layer holds this manifest id: replay or OPCONFLICT, applied
          /\ next' = [next EXCEPT ![w] = @ + 1]
          /\ UNCHANGED <<fence, appliedBy, exp, pc, cur, said>>
     ELSE IF next[w] = 0 /\ ~exp[w]
     THEN \* the first manifest refused
          IF \E d \in done : d.c = req[w].c /\ d.e = epoch /\ d.id = cur[w]
          THEN \* committed: another writer committed this operation id
               /\ Tell(w, "replay", cur[w], req[w].a)
               /\ pc' = [pc EXCEPT ![w] = "out"]
               /\ UNCHANGED <<fence, appliedBy, exp, cur, next>>
          ELSE \* Release(op, false), then plan again or be refused whole
               /\ fence' = IF HoldsId(cur[w]) \/ Broken = "releaseanyop" THEN None ELSE fence
               /\ cur' = [cur EXCEPT ![w] = None]
               /\ \/ /\ pc' = [pc EXCEPT ![w] = "check"]
                     /\ UNCHANGED said
                  \/ /\ Tell(w, "refused", None, None)
                     /\ pc' = [pc EXCEPT ![w] = "out"]
               /\ UNCHANGED <<appliedBy, exp, next>>
     ELSE /\ appliedBy' = [appliedBy EXCEPT ![m] = w]
          /\ next' = [next EXCEPT ![w] = @ + 1]
          /\ OthersOpen(w)
          /\ UNCHANGED <<fence, pc, cur, said>>
  /\ UNCHANGED <<gen, epoch, done, commits, abandoned, req, rgen, repoch, tries, strays>>

\* Redis.Release(op, true) after the last manifest; the writer tells its caller ok.
Commit(w) ==
  /\ pc[w] = "held"
  /\ next[w] = Manifests
  /\ IF HoldsId(cur[w])
     THEN /\ fence' = None
          /\ done' = done \cup {[c |-> req[w].c, a |-> req[w].a, e |-> cur[w][2], id |-> cur[w]]}
          /\ commits' = commits \cup {[c |-> req[w].c, id |-> cur[w], plan |-> w]}
     ELSE \* no operation with this id in the fence: nothing written, success
          /\ fence' = IF Broken = "releaseanyop" THEN None ELSE fence
          /\ UNCHANGED <<done, commits>>
  /\ Tell(w, "ok", cur[w], req[w].a)
  /\ pc' = [pc EXCEPT ![w] = "out"]
  /\ UNCHANGED <<gen, epoch, appliedBy, exp, abandoned, req, rgen, repoch, cur, next,
                 tries, strays>>

\* A writer stops at any point; the fence keeps what it held.
Crash(w) ==
  /\ pc[w] \in {"check", "read", "held"}
  /\ pc' = [pc EXCEPT ![w] = "dead"]
  /\ UNCHANGED <<gen, epoch, fence, appliedBy, exp, done, commits, abandoned,
                 req, rgen, repoch, cur, next, tries, said, strays>>

\* The grace passes over the pending operation.
Age ==
  /\ fence # None
  /\ ~fence.aged
  /\ Timely => pc[fence.plan] = "dead"
  /\ fence' = [fence EXCEPT !.aged = TRUE]
  /\ UNCHANGED <<gen, epoch, appliedBy, exp, done, commits, abandoned,
                 pc, req, rgen, repoch, cur, next, tries, said, strays>>

\* A table write outside the fence: it breaks or restores one plan's first manifest.
Stray ==
  /\ strays < MaxStray
  /\ strays' = strays + 1
  /\ \E w \in Writer : (exp[w] \/ Revive) /\ exp' = [exp EXCEPT ![w] = ~@]
  /\ UNCHANGED <<gen, epoch, fence, appliedBy, done, commits, abandoned,
                 pc, req, rgen, repoch, cur, next, tries, said>>

\* Store.finishOp, run by repair or by the next fenced read, on the fence's record.
Repair ==
  /\ fence # None
  /\ LET f == fence
         firstApplies == appliedBy[<<f.id, 0>>] # None \/ (exp[f.plan] /\ Broken # "repairnosend")
     IN IF firstApplies /\ ~(f.aged /\ Broken = "abandonapplied")
        THEN \* finish: every manifest the table layer lacks, from the record's plan
             /\ appliedBy' = [m \in Mids |-> IF m[1] = f.id /\ appliedBy[m] = None
                                              THEN f.plan ELSE appliedBy[m]]
             /\ OthersOpen(f.plan)
             /\ fence' = None
             /\ done' = done \cup {Receipt(f)}
             /\ commits' = commits \cup {[c |-> f.c, id |-> f.id, plan |-> f.plan]}
             /\ UNCHANGED abandoned
        ELSE \* the first manifest refused: on a bound or rule at once, on state
             \* only past the grace
             /\ \E kind \in {"rule", "state"} : kind = "rule" \/ f.aged
             /\ fence' = None
             /\ abandoned' = abandoned \cup {<<f.id, f.plan>>}
             /\ UNCHANGED <<appliedBy, exp, done, commits>>
  /\ UNCHANGED <<gen, epoch, pc, req, rgen, repoch, cur, next, tries, said, strays>>

\* A clear advances the epoch, only while the fence is empty.
Clear ==
  /\ epoch < MaxEpoch
  /\ fence = None
  /\ epoch' = epoch + 1
  /\ UNCHANGED <<gen, fence, appliedBy, exp, done, commits, abandoned,
                 pc, req, rgen, repoch, cur, next, tries, said, strays>>

Next ==
  \/ \E w \in Writer, c \in Caller, a \in Arg : Start(w, c, a)
  \/ \E w \in Writer : Check(w) \/ Acquire(w) \/ Send(w) \/ Commit(w) \/ Crash(w)
  \/ Age
  \/ Stray
  \/ Repair
  \/ Clear

Spec == Init /\ [][Next]_vars

AppliedPlans(id) == {appliedBy[<<id, k>>] : k \in 0..(Manifests - 1)} \ {None}
AppliedCount(id) == Cardinality({k \in 0..(Manifests - 1) : appliedBy[<<id, k>>] # None})
CommittedIds == {x.id : x \in commits}

\* G4: an operation's manifests come from one plan.
NoMixedOp == \A id \in Ids : Cardinality(AppliedPlans(id)) <= 1

\* G4: a committed operation applied whole, and from the plan its record committed.
CommittedWhole ==
  \A x \in commits : AppliedCount(x.id) = Manifests /\ AppliedPlans(x.id) = {x.plan}

\* G4: an operation partly applied is the one the fence holds.
NoPartialOutsideFence ==
  \A id \in Ids : AppliedCount(id) \in 1..(Manifests - 1) => HoldsId(id)

\* G4: every manifest the tables hold belongs to a committed operation or the pending one.
NoApplyWithoutCommit ==
  \A id \in Ids : AppliedCount(id) > 0 => (id \in CommittedIds \/ HoldsId(id))

\* G14: an abandoned plan applied nothing.
AbandonedAppliedNothing == \A x \in abandoned : x[2] \notin AppliedPlans(x[1])

\* G5: a writer that tells its caller ok had its own plan committed.
OkMeansCommitted ==
  \A w \in Writer : (said[w] # None /\ said[w].kind = "ok") =>
    [c |-> req[w].c, id |-> said[w].id, plan |-> w] \in commits

\* G6: a replay is of a committed operation with the caller's own arguments.
ReplayMatches ==
  \A w \in Writer : (said[w] # None /\ said[w].kind = "replay") =>
    /\ said[w].id \in CommittedIds
    /\ said[w].a = req[w].a

\* G3, G6, G7: one caller operation id commits once, by one plan, across every epoch.
CallerCommitsOnce ==
  \A c \in Caller : Cardinality({x \in commits : x.c = c}) <= 1

=============================================================================
