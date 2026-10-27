---------------------------- MODULE StoreFence ----------------------------
\* The store's fence and commit core, layer 1 of the layered rewrite
\* (docs/layers/L1-STORE-CONTRACT.md, guarantees G3 to G7 and G14; the
\* COVERAGE.tsv row pending-fenced-op).
\*
\* The code it models (internal/sprint/store):
\*   Acquire     redis.go Redis.Acquire: WATCH fence, generation and epoch; the
\*               operation goes in the fence and the generation advances only
\*               when the fence is empty, the generation is the one read and the
\*               sprint is still at the pinned epoch (G3).
\*   Send        engine.go Store.apply: the operation's manifests, in order. A
\*               first manifest refused on state applies nothing, and the writer
\*               releases the operation uncommitted and plans again (G17). A
\*               manifest carries no check of the fence.
\*   Commit      redis.go Redis.Release(op, true): WATCH the fence; when it still
\*               holds this operation, one MULTI/EXEC records the caller's result
\*               under its operation id and empties the fence (G4). When it holds
\*               another operation or none, it writes nothing and returns
\*               success (G5).
\*   Check       engine.go Store.callerOp, run after each fenced read: a result
\*               recorded at this epoch is replayed for the same arguments and
\*               refused for others (replay, G6); one recorded at an earlier
\*               epoch is refused (DoneBefore, G7). The fenced read and the check
\*               are one action here: a fenced read finishes or waits out a
\*               pending operation first (engine.go fencedStep, G2), so it reads
\*               only an empty fence.
\*   Repair      engine.go Store.finishOp: an operation whose first manifest
\*               applied is finished and committed; one whose first manifest has
\*               not applied stays open within the grace and is abandoned past it
\*               (abandonment: a release that records no caller result, G14).
\*   Clear       the epoch advances only while the fence is empty (G10).
\*
\* Manifests are counted, not written out: applied[id] is how many of the
\* operation's manifests the tables hold, and rev is the table revision a first
\* manifest is checked against. Each writer serves one request (a caller's
\* operation id and its arguments) over at most MaxTries attempts, each attempt a
\* fresh operation id.
\*
\* Timely is the timing assumption the code relies on: a writer holding the fence
\* either finishes within the grace or is dead, so an operation is aged only when
\* its writer is dead. With Timely FALSE a live writer may act after its operation
\* was abandoned (MCStoreFenceLateWriter.cfg).
\*
\* Broken names a reversed witness (one config each):
\*   "replayignoresargs" replay without comparing arguments: breaks ReplayMatches
\*   "rerunoldepoch"     no DoneBefore check: breaks CallerCommitsOnce
\*   "releaseanyop"      release empties the fence whoever holds it: breaks
\*                       NoPartialOutsideFence
\*   "abandonapplied"    repair abandons an aged operation even when its first
\*                       manifest applied: breaks AbandonedAppliedNothing

EXTENDS Naturals, FiniteSets

CONSTANTS Writer, Caller, Arg, Manifests, MaxTries, MaxEpoch, Timely, Broken

None == "none"
Ids == Writer \X (1..MaxTries)
Kinds == {"ok", "replay", "conflict", "refused", "lost"}

VARIABLES gen, epoch, fence, applied, rev, done, committed, abandoned,
          pc, req, rgen, repoch, rrev, cur, tries, said

vars == <<gen, epoch, fence, applied, rev, done, committed, abandoned,
          pc, req, rgen, repoch, rrev, cur, tries, said>>

Fences == [id : Ids, c : Caller, a : Arg, e : 1..MaxEpoch, aged : BOOLEAN]
Receipts == [c : Caller, a : Arg, e : 1..MaxEpoch, id : Ids]

TypeOK ==
  /\ gen \in Nat
  /\ epoch \in 1..MaxEpoch
  /\ fence \in Fences \cup {None}
  /\ applied \in [Ids -> 0..Manifests]
  /\ rev \in Nat
  /\ done \subseteq Receipts
  /\ committed \subseteq Ids
  /\ abandoned \subseteq Ids
  /\ pc \in [Writer -> {"idle", "check", "read", "held", "out", "dead"}]
  /\ req \in [Writer -> [c : Caller, a : Arg] \cup {None}]
  /\ cur \in [Writer -> Ids \cup {None}]
  /\ tries \in [Writer -> 0..MaxTries]
  /\ said \in [Writer -> [kind : Kinds, id : Ids \cup {None}, a : Arg \cup {None}] \cup {None}]

Init ==
  /\ gen = 0
  /\ epoch = 1
  /\ fence = None
  /\ applied = [id \in Ids |-> 0]
  /\ rev = 0
  /\ done = {}
  /\ committed = {}
  /\ abandoned = {}
  /\ pc = [w \in Writer |-> "idle"]
  /\ req = [w \in Writer |-> None]
  /\ rgen = [w \in Writer |-> 0]
  /\ repoch = [w \in Writer |-> 1]
  /\ rrev = [w \in Writer |-> 0]
  /\ cur = [w \in Writer |-> None]
  /\ tries = [w \in Writer |-> 0]
  /\ said = [w \in Writer |-> None]

DoneNow(c) == {d \in done : d.c = c /\ d.e = epoch}
DoneBefore(c) == {d \in done : d.c = c /\ d.e < epoch}
Holds(w) == fence # None /\ fence.id = cur[w]
Tell(w, k, id, a) == said' = [said EXCEPT ![w] = [kind |-> k, id |-> id, a |-> a]]

\* A caller asks writer w to run operation c with arguments a.
Start(w, c, a) ==
  /\ pc[w] = "idle"
  /\ pc' = [pc EXCEPT ![w] = "check"]
  /\ req' = [req EXCEPT ![w] = [c |-> c, a |-> a]]
  /\ UNCHANGED <<gen, epoch, fence, applied, rev, done, committed, abandoned,
                 rgen, repoch, rrev, cur, tries, said>>

\* The fenced read and the caller-op check (Store.callerOp).
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
          /\ UNCHANGED <<rgen, repoch, rrev>>
     ELSE IF DoneBefore(r.c) # {} /\ Broken # "rerunoldepoch"
     THEN /\ Tell(w, "refused", None, None)
          /\ pc' = [pc EXCEPT ![w] = "out"]
          /\ UNCHANGED <<rgen, repoch, rrev>>
     ELSE IF tries[w] = MaxTries
     THEN /\ Tell(w, "lost", None, None)
          /\ pc' = [pc EXCEPT ![w] = "out"]
          /\ UNCHANGED <<rgen, repoch, rrev>>
     ELSE /\ rgen' = [rgen EXCEPT ![w] = gen]
          /\ repoch' = [repoch EXCEPT ![w] = epoch]
          /\ rrev' = [rrev EXCEPT ![w] = rev]
          /\ pc' = [pc EXCEPT ![w] = "read"]
          /\ UNCHANGED said
  /\ UNCHANGED <<gen, epoch, fence, applied, rev, done, committed, abandoned,
                 req, cur, tries>>

\* Redis.Acquire: a fresh operation id each attempt.
Acquire(w) ==
  /\ pc[w] = "read"
  /\ tries' = [tries EXCEPT ![w] = @ + 1]
  /\ LET id == <<w, tries[w] + 1>> IN
     IF fence = None /\ gen = rgen[w] /\ epoch = repoch[w]
     THEN /\ fence' = [id |-> id, c |-> req[w].c, a |-> req[w].a, e |-> epoch, aged |-> FALSE]
          /\ gen' = gen + 1
          /\ cur' = [cur EXCEPT ![w] = id]
          /\ pc' = [pc EXCEPT ![w] = "held"]
     ELSE /\ pc' = [pc EXCEPT ![w] = "check"]
          /\ UNCHANGED <<fence, gen, cur>>
  /\ UNCHANGED <<epoch, applied, rev, done, committed, abandoned, req, rgen, repoch,
                 rrev, said>>

\* Store.apply: the next manifest. Under Timely a writer acts only while the
\* fence holds its operation.
Send(w) ==
  /\ pc[w] = "held"
  /\ applied[cur[w]] < Manifests
  /\ Timely => Holds(w)
  /\ IF applied[cur[w]] = 0 /\ rev # rrev[w]
     THEN \* the first manifest refused on state: release uncommitted, plan again
          /\ fence' = IF Holds(w) \/ Broken = "releaseanyop" THEN None ELSE fence
          /\ pc' = [pc EXCEPT ![w] = "check"]
          /\ cur' = [cur EXCEPT ![w] = None]
          /\ UNCHANGED <<applied, rev>>
     ELSE /\ applied' = [applied EXCEPT ![cur[w]] = @ + 1]
          /\ rev' = rev + 1
          /\ UNCHANGED <<fence, pc, cur>>
  /\ UNCHANGED <<gen, epoch, done, committed, abandoned, req, rgen, repoch, rrev,
                 tries, said>>

\* Redis.Release(op, true) after the last manifest; the writer tells its caller ok.
Commit(w) ==
  /\ pc[w] = "held"
  /\ applied[cur[w]] = Manifests
  /\ IF Holds(w)
     THEN /\ fence' = None
          /\ done' = done \cup {[c |-> fence.c, a |-> fence.a, e |-> fence.e, id |-> fence.id]}
          /\ committed' = committed \cup {cur[w]}
     ELSE \* the fence moved: nothing is written, and the release is success
          /\ fence' = IF Broken = "releaseanyop" THEN None ELSE fence
          /\ UNCHANGED <<done, committed>>
  /\ Tell(w, "ok", cur[w], req[w].a)
  /\ pc' = [pc EXCEPT ![w] = "out"]
  /\ UNCHANGED <<gen, epoch, applied, rev, abandoned, req, rgen, repoch, rrev, cur,
                 tries>>

\* A writer stops at any point; the fence keeps what it held.
Crash(w) ==
  /\ pc[w] \in {"check", "read", "held"}
  /\ pc' = [pc EXCEPT ![w] = "dead"]
  /\ UNCHANGED <<gen, epoch, fence, applied, rev, done, committed, abandoned,
                 req, rgen, repoch, rrev, cur, tries, said>>

\* The grace passes over the pending operation.
Age ==
  /\ fence # None
  /\ ~fence.aged
  /\ Timely => pc[fence.id[1]] = "dead"
  /\ fence' = [fence EXCEPT !.aged = TRUE]
  /\ UNCHANGED <<gen, epoch, applied, rev, done, committed, abandoned,
                 pc, req, rgen, repoch, rrev, cur, tries, said>>

\* Store.finishOp, run by repair or by the next fenced read.
Repair ==
  /\ fence # None
  /\ LET f == fence IN
     IF applied[f.id] > 0 /\ ~(f.aged /\ Broken = "abandonapplied")
     THEN /\ applied' = [applied EXCEPT ![f.id] = Manifests]
          /\ rev' = rev + (Manifests - applied[f.id])
          /\ fence' = None
          /\ done' = done \cup {[c |-> f.c, a |-> f.a, e |-> f.e, id |-> f.id]}
          /\ committed' = committed \cup {f.id}
          /\ UNCHANGED abandoned
     ELSE /\ f.aged
          /\ fence' = None
          /\ abandoned' = abandoned \cup {f.id}
          /\ UNCHANGED <<applied, rev, done, committed>>
  /\ UNCHANGED <<gen, epoch, pc, req, rgen, repoch, rrev, cur, tries, said>>

\* A clear advances the epoch, only while the fence is empty.
Clear ==
  /\ epoch < MaxEpoch
  /\ fence = None
  /\ epoch' = epoch + 1
  /\ UNCHANGED <<gen, fence, applied, rev, done, committed, abandoned,
                 pc, req, rgen, repoch, rrev, cur, tries, said>>

Next ==
  \/ \E w \in Writer, c \in Caller, a \in Arg : Start(w, c, a)
  \/ \E w \in Writer : Check(w) \/ Acquire(w) \/ Send(w) \/ Commit(w) \/ Crash(w)
  \/ Age
  \/ Repair
  \/ Clear

Spec == Init /\ [][Next]_vars

\* G4: a committed operation applied whole.
CommittedWhole == \A id \in committed : applied[id] = Manifests

\* G4: an operation partly applied is the one the fence holds.
NoPartialOutsideFence ==
  \A id \in Ids : applied[id] \in 1..(Manifests - 1) => (fence # None /\ fence.id = id)

\* G4: every manifest the tables hold belongs to a committed operation or the pending one.
NoApplyWithoutCommit ==
  \A id \in Ids : applied[id] > 0 => (id \in committed \/ (fence # None /\ fence.id = id))

\* G14: an abandoned operation applied nothing.
AbandonedAppliedNothing == \A id \in abandoned : applied[id] = 0

\* G5: a writer that tells its caller ok has its operation committed.
OkMeansCommitted ==
  \A w \in Writer : (said[w] # None /\ said[w].kind = "ok") => said[w].id \in committed

\* G6: a replay is of a committed operation with the caller's own arguments.
ReplayMatches ==
  \A w \in Writer : (said[w] # None /\ said[w].kind = "replay") =>
    /\ said[w].id \in committed
    /\ said[w].a = req[w].a

\* G6, G7: one caller operation id commits at most once, across every epoch.
CallerCommitsOnce ==
  \A c \in Caller : Cardinality({id \in committed : req[id[1]].c = c}) <= 1

\* G3: the generation counts the acquires; no two operations hold one generation.
FenceOneHolder ==
  fence # None => Cardinality({w \in Writer : pc[w] = "held" /\ cur[w] = fence.id}) <= 1

=============================================================================
