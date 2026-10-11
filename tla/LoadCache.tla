------------------------------ MODULE LoadCache ------------------------------
\* The load cache (internal/sprint/store/loadcache.go, load.go placedRecords,
\* pipeline_load.go): a process keeps each table's placed records as its last
\* whole read found them, at the revision read, with the mark of the change event
\* that left that revision (its stream id), and a later load of the table brings
\* them to the shape's revision from the table's change stream instead of reading
\* the table whole (2026-10-10: the server read the fleet table whole about 56
\* times a second, most of it finished cards). The catch-up is the tick's twin's
\* (tla/DirtyTickRead.tla, CatchUp); what is new is that the cache is shared by
\* loads running at once (one lock per table, held across a table's catch-up),
\* never writes, is read without the fence (a fenced read reads the fence after,
\* as DirtyTickRead's Close does), and outlives a store that loses its last writes
\* (a Redis restart from an AOF synced every second, an RDB or a backup restore:
\* the same epoch, a lower revision, and a new history written from there, whose
\* events are new events with new marks).
\*
\* THE STORE.
\*   rec, rev, log  each table's records, its revision, its change stream (one
\*                  event per write: the revision it left, the keys it named, its
\*                  mark; a display write names none)
\*   hist           each table's records at every revision of its present
\*                  history: hist[t][r+1] is the table at revision r (what a whole
\*                  read of revision r gives)
\*   mk             the marks given so far this epoch (every event gets a new one;
\*                  a clear starts a new epoch, whose stream marks begin again)
\*   ep             the store's epoch, advanced by the outside Clear
\*
\* THE CACHE (one per process; its tables each behind their own lock, keyed by
\* <<table, epoch>>).
\*   cv, crec, crev, cmark  the cache holds the table, its records, their
\*                  revision, and the mark of the event that left that revision
\*                  (0: revision 0, the epoch's empty table, left by no event)
\*   cep           the epoch the entry was read at: an entry of another epoch is
\*                  not held (a load of the new epoch reads whole and keeps anew)
\*
\* A LOAD (l in Loaders, running at once).
\*   "idle" -> Begin: the shapes, one exchange: the epoch (vep) and each table's
\*              revision (vrev).
\*   "view" -> a table at a time:
\*     held (the cache holds it at the load's epoch): Take(t), under the lock,
\*              all or nothing, the catch-up atomic: the store behind the cache
\*              (crev > the shape's revision) the entry dropped; the cache at or
\*              behind the shape's revision, the stream read back from the
\*              shape's revision to the cache's, its event at the cache's
\*              revision must have the cache's mark, else the entry dropped; the
\*              keys written between are read at the shape's revision (refused
\*              when the table moved after the shape: the load begins again);
\*     not held: the trips, each its own step, outside the lock:
\*       "mark"  the mark at the shape's revision read (markOf),
\*       "ids"   the cell ids read (CellIDs),
\*       "records" the table read whole at its revision (readInto; refused when
\*              it moved: the load begins again),
\*       "keep"  the entry kept, the lock taken only to store, guarded: a cache
\*              that already holds the table at a later revision of the same
\*              epoch keeps the later entry (keepLoaded).
\*             Drop(t): the cache forgets a table (its counts did not agree).
\*             End: every table taken: the load returns.
\*   A clear between trips leaves the load's epoch stale (vep < ep): the load
\*   begins again.
\*
\* WHAT IS CHECKED.
\*   LoadIsSnapshot  every table a load took was, when it took it, the store's
\*                   records at the revision its shape read
\*   LoadIsOfItsEpoch every table a load took is the records of the epoch it read
\*   CacheIsHistory  a table the cache holds at the current epoch whose mark is
\*                   still the store's event at its revision is the store's
\*                   records at it
\* Reversed witnesses (Broken): "catchup" (the catch-up misses the last write its
\* stream names), "ahead" (a cache later than the shape used as it is),
\* "nomark" (the mark is not checked), "noepochkey" (entries keyed by table alone:
\* a load of the old epoch is served after a clear). The card's section (2) names
\* a "norevguard" witness (the keep's revision check dropped) as a LoadIsSnapshot
\* failure, but that expectation is unsatisfiable as modelled: the revision guard
\* only keeps the newest read (an efficiency), while the serve-time mark check
\* (which "nomark" drops) is the correctness guard, so a cache that regresses
\* still serves the right records. That witness is not added, and no new
\* invariant is substituted in; the card ends not-done on that point.

EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Tables, Keys, Vals, Loaders, MaxW, MaxLoads, MaxRollbacks, MaxClear, Broken

ASSUME 0 \in Vals

VARIABLES rec, rev, log, hist, wn, mk, rb, ep,
          cv, crec, crev, cmark, cep,
          pc, vrev, vep, cur, lmark,
          out, done, good, oep, loads

vars == <<rec, rev, log, hist, wn, mk, rb, ep, cv, crec, crev, cmark, cep,
          pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>
storeVars == <<rec, rev, log, hist, wn, mk, rb, ep>>
cacheVars == <<cv, crec, crev, cmark, cep>>

ZeroT == [k \in Keys |-> 0]

Init ==
  /\ rec = [t \in Tables |-> ZeroT] /\ rev = [t \in Tables |-> 0]
  /\ log = [t \in Tables |-> <<>>] /\ hist = [t \in Tables |-> <<ZeroT>>]
  /\ wn = 0 /\ mk = 0 /\ rb = 0 /\ ep = 0
  /\ cv = [t \in Tables |-> FALSE] /\ crec = [t \in Tables |-> ZeroT]
  /\ crev = [t \in Tables |-> 0] /\ cmark = [t \in Tables |-> 0]
  /\ cep = [t \in Tables |-> 0]
  /\ pc = [l \in Loaders |-> "idle"] /\ vrev = [l \in Loaders |-> [t \in Tables |-> 0]]
  /\ vep = [l \in Loaders |-> 0]
  /\ cur = [l \in Loaders |-> CHOOSE t \in Tables : TRUE]
  /\ lmark = [l \in Loaders |-> 0]
  /\ out = [l \in Loaders |-> [t \in Tables |-> ZeroT]]
  /\ done = [l \in Loaders |-> [t \in Tables |-> FALSE]]
  /\ good = [l \in Loaders |-> [t \in Tables |-> TRUE]]
  /\ oep = [l \in Loaders |-> [t \in Tables |-> 0]]
  /\ loads = 0

\* the keys a table's change stream names between two revisions
Named(t, from, to) ==
  UNION {log[t][i][2] : i \in {j \in 1..Len(log[t]) : log[t][j][1] > from /\ log[t][j][1] <= to}}

\* no event: a mark no event has (events are marked 1..MaxW)
NoEvent == MaxW + 1

\* the mark of the event that left revision r in the present stream: 0 for
\* revision 0, NoEvent when no event left it (the store is behind r)
MarkAt(t, r) ==
  IF r = 0 THEN 0
  ELSE IF \E i \in 1..Len(log[t]) : log[t][i][1] = r
         THEN log[t][CHOOSE i \in 1..Len(log[t]) : log[t][i][1] = r][3]
         ELSE NoEvent

\* what a whole read of revision r gives, in the present history (ZeroT past it)
At(t, r) == IF r < Len(hist[t]) THEN hist[t][r + 1] ELSE ZeroT
HasAt(t, r) == r < Len(hist[t])

\* the cache holds t at the load's epoch
Held(l, t) == cv[t] /\ (cep[t] = vep[l] \/ Broken = "noepochkey")

-------------------------------------------------------------------------------
\* A LOAD

Begin(l) ==
  /\ pc[l] = "idle" /\ loads < MaxLoads
  /\ pc' = [pc EXCEPT ![l] = "view"] /\ vrev' = [vrev EXCEPT ![l] = rev]
  /\ vep' = [vep EXCEPT ![l] = ep]
  /\ done' = [done EXCEPT ![l] = [t \in Tables |-> FALSE]]
  /\ good' = [good EXCEPT ![l] = [t \in Tables |-> TRUE]]
  /\ oep' = [oep EXCEPT ![l] = [t \in Tables |-> 0]]
  /\ loads' = loads + 1
  /\ UNCHANGED <<storeVars, cacheVars, cur, lmark, out>>

\* the load begins again: a clear left its epoch behind (a table that moved is
\* abandoned by the trip that read it)
Again(l) ==
  /\ pc[l] # "idle" /\ vep[l] < ep
  /\ pc' = [pc EXCEPT ![l] = "idle"]
  /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, cur, lmark, out, done, good, oep, loads>>

\* table t of load l taken with records r at epoch e: good when they are the
\* store's at the shape's revision, in the history the store has as it is taken
Took(l, t, r, e) ==
  /\ out' = [out EXCEPT ![l][t] = r]
  /\ done' = [done EXCEPT ![l][t] = TRUE]
  /\ good' = [good EXCEPT ![l][t] = HasAt(t, vrev[l][t]) /\ r = At(t, vrev[l][t])]
  /\ oep' = [oep EXCEPT ![l][t] = e]

DropEntry(t) ==
  /\ cv' = [cv EXCEPT ![t] = FALSE]
  /\ UNCHANGED <<crec, crev, cmark, cep, pc, out, done, good, oep>>

\* the catch-up: held, atomic under the lock (loadcache.go placedFromCache and
\* catchUpCached)
Take(l, t) ==
  /\ pc[l] = "view" /\ ~done[l][t] /\ vep[l] = ep /\ Held(l, t)
  /\ LET v == vrev[l][t] IN
     IF crev[t] > v /\ Broken # "ahead"
       THEN DropEntry(t) /\ UNCHANGED <<pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>
     ELSE IF MarkAt(t, crev[t]) # cmark[t] /\ Broken # "nomark"
       THEN DropEntry(t) /\ UNCHANGED <<pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>
     ELSE IF crev[t] >= v
       THEN /\ Took(l, t, crec[t], cep[t])
            /\ UNCHANGED <<pc, cacheVars, vrev, vep, cur, lmark, loads>>
      ELSE IF rev[t] = v
       THEN LET to == IF Broken = "catchup" THEN v - 1 ELSE v
                r  == [k \in Keys |-> IF k \in Named(t, crev[t], to) THEN rec[t][k] ELSE crec[t][k]]
            IN /\ crec' = [crec EXCEPT ![t] = r] /\ crev' = [crev EXCEPT ![t] = v]
               /\ cmark' = [cmark EXCEPT ![t] = MarkAt(t, v)]
               /\ cep' = [cep EXCEPT ![t] = vep[l]]
               /\ Took(l, t, r, vep[l])
               /\ UNCHANGED <<pc, cv, vrev, vep, cur, lmark, loads>>
     ELSE /\ pc' = [pc EXCEPT ![l] = "idle"]
          /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, cur, lmark, out, done, good, oep, loads>>
  /\ UNCHANGED <<storeVars>>

\* not held: begin the trips
StartRead(l, t) ==
  /\ pc[l] = "view" /\ ~done[l][t] /\ ~Held(l, t) /\ vep[l] = ep
  /\ cur' = [cur EXCEPT ![l] = t] /\ pc' = [pc EXCEPT ![l] = "mark"]
  /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, lmark, out, done, good, oep, loads>>

\* the mark at the shape's revision, read before the records (markOf)
Mark(l) ==
  /\ pc[l] = "mark" /\ vep[l] = ep
  /\ lmark' = [lmark EXCEPT ![l] = MarkAt(cur[l], vrev[l][cur[l]])]
  /\ pc' = [pc EXCEPT ![l] = "ids"]
  /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, cur, out, done, good, oep, loads>>

\* the cell ids (CellIDs), one exchange; the model reads the table whole, so
\* nothing is carried, but the step is its own
Ids(l) ==
  /\ pc[l] = "ids" /\ vep[l] = ep
  /\ pc' = [pc EXCEPT ![l] = "records"]
  /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, cur, lmark, out, done, good, oep, loads>>

\* the records read whole at their revision (readInto): refused when the table
\* moved after the shape (the load begins again)
Records(l) ==
  /\ pc[l] = "records"
  /\ LET t == cur[l]
         v == vrev[l][t]
     IN IF vep[l] = ep /\ rev[t] = v
         THEN /\ Took(l, t, rec[t], vep[l])
              /\ pc' = [pc EXCEPT ![l] = "keep"]
              /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, cur, lmark, loads>>
         ELSE /\ pc' = [pc EXCEPT ![l] = "idle"]
              /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, cur, lmark, out, done, good, oep, loads>>

\* the entry kept, the lock taken only to store, with the revision guard: a
\* cache that holds the table at a later revision of the same epoch keeps the
\* later entry
Keep(l) ==
  /\ pc[l] = "keep" /\ vep[l] = ep
  /\ LET t == cur[l]
         v == vrev[l][t]
     IN /\ IF cv[t] /\ cep[t] = vep[l] /\ crev[t] > v
            THEN UNCHANGED <<cv, crec, crev, cmark, cep>>
            ELSE /\ cv' = [cv EXCEPT ![t] = TRUE]
                 /\ crec' = [crec EXCEPT ![t] = out[l][t]]
                 /\ crev' = [crev EXCEPT ![t] = v]
                 /\ cmark' = [cmark EXCEPT ![t] = lmark[l]]
                 /\ cep' = [cep EXCEPT ![t] = vep[l]]
       /\ pc' = [pc EXCEPT ![l] = "view"]
       /\ UNCHANGED <<storeVars, vrev, vep, cur, lmark, out, done, good, oep, loads>>

Drop(t) ==
  /\ cv[t] /\ cv' = [cv EXCEPT ![t] = FALSE]
  /\ UNCHANGED <<storeVars, crec, crev, cmark, cep, pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>

End(l) ==
  /\ pc[l] = "view" /\ \A t \in Tables : done[l][t]
  /\ pc' = [pc EXCEPT ![l] = "idle"]
  /\ UNCHANGED <<storeVars, cacheVars, vrev, vep, cur, lmark, out, done, good, oep, loads>>

-------------------------------------------------------------------------------
\* THE STORE: a record's write (a step's operation, any process), a display cell's
\* (its revision moves, no record changes), and a restart that loses the last
\* writes of a table (the same epoch, a lower revision: the stream and the records
\* as they were at it; the writes after it are new events with new marks)

Write(t, k, v) ==
  /\ wn < MaxW
  /\ rec' = [rec EXCEPT ![t][k] = v]
  /\ rev' = [rev EXCEPT ![t] = rev[t] + 1]
  /\ log' = [log EXCEPT ![t] = Append(log[t], <<rev[t] + 1, {k}, mk + 1>>)]
  /\ hist' = [hist EXCEPT ![t] = Append(hist[t], [rec[t] EXCEPT ![k] = v])]
  /\ wn' = wn + 1 /\ mk' = mk + 1
  /\ UNCHANGED <<rb, ep, cacheVars, pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>

Display(t) ==
  /\ wn < MaxW
  /\ rev' = [rev EXCEPT ![t] = rev[t] + 1]
  /\ log' = [log EXCEPT ![t] = Append(log[t], <<rev[t] + 1, {}, mk + 1>>)]
  /\ hist' = [hist EXCEPT ![t] = Append(hist[t], rec[t])]
  /\ wn' = wn + 1 /\ mk' = mk + 1
  /\ UNCHANGED <<rec, rb, ep, cacheVars, pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>

Rollback(t, r) ==
  /\ rb < MaxRollbacks /\ r < rev[t]
  /\ rev' = [rev EXCEPT ![t] = r]
  /\ rec' = [rec EXCEPT ![t] = hist[t][r + 1]]
  /\ hist' = [hist EXCEPT ![t] = SubSeq(hist[t], 1, r + 1)]
  /\ log' = [log EXCEPT ![t] = SelectSeq(log[t], LAMBDA e : e[1] <= r)]
  /\ rb' = rb + 1
  /\ UNCHANGED <<wn, mk, ep, cacheVars, pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>

\* the outside clear: the epoch advances once and the new epoch starts empty
Clear ==
  /\ ep < MaxClear
  /\ rec' = [t \in Tables |-> ZeroT] /\ rev' = [t \in Tables |-> 0]
  /\ log' = [t \in Tables |-> <<>>] /\ hist' = [t \in Tables |-> <<ZeroT>>]
  /\ wn' = 0 /\ mk' = 0 /\ ep' = ep + 1
  /\ UNCHANGED <<rb, cv, crec, crev, cmark, cep, pc, vrev, vep, cur, lmark, out, done, good, oep, loads>>

Next ==
  \/ \E l \in Loaders : Begin(l) \/ End(l) \/ Again(l) \/ Mark(l) \/ Ids(l)
                        \/ Records(l) \/ Keep(l) \/ \E t \in Tables : Take(l, t) \/ StartRead(l, t)
  \/ \E t \in Tables : Drop(t) \/ Display(t)
  \/ \E t \in Tables, k \in Keys, v \in Vals : Write(t, k, v)
  \/ \E t \in Tables, r \in 0..MaxW : Rollback(t, r)
  \/ Clear

Spec == Init /\ [][Next]_vars

-------------------------------------------------------------------------------
\* INVARIANTS

TypeOK ==
  /\ \A l \in Loaders : pc[l] \in {"idle", "view", "mark", "ids", "records", "keep"}
  /\ \A t \in Tables : Len(hist[t]) = rev[t] + 1

\* every table a load took was, when it took it, the store's records at the
\* revision its shape read
LoadIsSnapshot ==
  \A l \in Loaders, t \in Tables : done[l][t] => good[l][t]

\* every table a load took is the records of the epoch it read
LoadIsOfItsEpoch ==
  \A l \in Loaders, t \in Tables : done[l][t] => oep[l][t] = vep[l]

\* a table the cache holds at the current epoch whose mark is still the store's
\* event at its revision is the store's records at it
CacheIsHistory ==
  \A t \in Tables : (cv[t] /\ cep[t] = ep /\ MarkAt(t, crev[t]) = cmark[t]) => crec[t] = At(t, crev[t])

=============================================================================
