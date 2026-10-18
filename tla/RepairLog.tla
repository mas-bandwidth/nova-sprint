------------------------------- MODULE RepairLog -------------------------------
(*
 * The repair of a cut operation's later manifest and the log lines of its
 * entries (internal/sprint/store/engine.go finishOp, applyEntries, sentBefore,
 * heldAsResult, withSkips; repair_skipped.go ReconcileSkipped). It sits beside
 * SprintTables, whose Crash and Repair hold the deferred work-table write as
 * one step and model no log, and DirtyTick, whose drain models no cut: neither
 * holds the table layer's record of an operation id, nor the entry-by-entry
 * path, nor a move line per entry, and adding them to either touches every
 * action of a model of more than a thousand lines; this module holds exactly
 * the three, for one manifest of Cards entries.
 *
 * The bug (2026-10-17, v1.5.3): a tick drain's work manifest applied whole
 * under its own operation id and its release was lost. The repair sent it
 * again, the table layer refused it (the store answered no more from the id's
 * record), and the manifest went entry by entry under new ids (<id>.eN). Each
 * entry found its member one revision past what it expected, at its target
 * (MEMBERREVISION), and was judged skipped: its move line was dropped
 * (withSkips) though the table holds the change. 159 cards, 106 rule 13
 * violations; the machine could not start.
 *
 * The fix (v1.5.4): an entry refused whose member the store holds as its
 * result (heldAsResult: one revision past, at the target, its fields set)
 * counts as applied and keeps its line (ApplyEntry); a later manifest refused
 * when sent again is probed first, as the first one is (sentBefore): the
 * record of its own id says its writer applied it whole, and none of its
 * entries is sent again (RepairProbe). The reconcile (repair --skipped)
 * restores the line of each entry an old repair skipped that the store holds
 * as applied, and leaves the others (Reconcile).
 *
 * tbl[c]  the member of entry c: "pre" as the entry expects it, "post" as the
 *         entry leaves it (one revision past), "outside" changed by a writer
 *         outside the fence before the manifest applied (never the entry's)
 * rec     the table layer answers from the record of the manifest's own id
 * bound   a bound the table layer checks ahead of the record refuses a whole
 *         resend (table.lua T.apply: manifest_bytes before the lookup)
 * log[c]  the log holds entry c's move line
 * Broken  the witnesses: "skipapplied" (an applied entry refused is skipped:
 *         the bug), "noprobe" (a later manifest gets no probe),
 *         "reconcileblind" (the reconcile restores nothing)
 *)
EXTENDS FiniteSets

CONSTANTS Cards, Broken

VARIABLES tbl, rec, bound, pending, log, skipped, resent, done, recAt, reconciled

vars == <<tbl, rec, bound, pending, log, skipped, resent, done, recAt, reconciled>>

TypeOK ==
  /\ tbl \in [Cards -> {"pre", "post", "outside"}]
  /\ rec \in BOOLEAN /\ bound \in BOOLEAN /\ pending \in BOOLEAN
  /\ log \in [Cards -> BOOLEAN]
  /\ skipped \subseteq Cards
  /\ resent \in BOOLEAN /\ done \in BOOLEAN /\ recAt \in BOOLEAN /\ reconciled \in BOOLEAN

\* The operation is cut and pending: its earlier manifests applied, this one
\* not yet.
Init ==
  /\ tbl = [c \in Cards |-> "pre"]
  /\ rec = FALSE /\ bound = FALSE /\ pending = TRUE
  /\ log = [c \in Cards |-> FALSE]
  /\ skipped = {} /\ resent = FALSE /\ done = FALSE /\ recAt = FALSE /\ reconciled = FALSE

Outer == <<pending, log, skipped, resent, done, recAt, reconciled>>

\* The writer's send of the manifest applies it whole under its own id, and its
\* release is lost: the operation stays pending.
WriterApplies ==
  /\ pending /\ \A c \in Cards : tbl[c] = "pre"
  /\ tbl' = [c \in Cards |-> "post"] /\ rec' = TRUE
  /\ UNCHANGED <<bound, Outer>>

\* The store answers no more from the id's record.
Forget ==
  /\ pending /\ rec /\ rec' = FALSE
  /\ UNCHANGED <<tbl, bound, Outer>>

\* A bound ahead of the record's lookup comes to refuse a whole resend.
Bound ==
  /\ pending /\ ~bound /\ bound' = TRUE
  /\ UNCHANGED <<tbl, rec, Outer>>

\* A writer outside the fence changes a member before the manifest applied.
Outside(c) ==
  /\ pending /\ tbl[c] = "pre"
  /\ tbl' = [tbl EXCEPT ![c] = "outside"]
  /\ UNCHANGED <<rec, bound, Outer>>

\* ApplyEntry: one entry sent under its own id (<id>.eN). It applies where its
\* expectation holds; refused, it counts as applied when the store holds its
\* member as its result (heldAsResult), else it is skipped.
Applied(c) == tbl[c] = "pre" \/ (tbl[c] = "post" /\ "skipapplied" \notin Broken)

\* RepairProbe: the record of the manifest's own id answers a probe at a
\* revision no table reaches (sentBefore).
Probed == "noprobe" \notin Broken /\ rec

\* The repair: the manifest sent again whole replays from its record or applies
\* now; refused, it is probed; else it goes entry by entry. Its release writes
\* the move line of every entry counted applied, and skips the rest.
Repair ==
  /\ pending
  /\ LET whole == ~bound /\ (rec \/ \A c \in Cards : tbl[c] = "pre") IN
       IF whole \/ Probed
       THEN /\ tbl' = IF whole /\ ~rec THEN [c \in Cards |-> "post"] ELSE tbl
            /\ log' = [c \in Cards |-> TRUE]
            /\ skipped' = {}
            /\ resent' = FALSE
       ELSE /\ tbl' = [c \in Cards |-> IF tbl[c] = "pre" THEN "post" ELSE tbl[c]]
            /\ log' = [c \in Cards |-> Applied(c)]
            /\ skipped' = {c \in Cards : ~Applied(c)}
            /\ resent' = TRUE
  /\ pending' = FALSE /\ done' = TRUE /\ recAt' = rec
  /\ UNCHANGED <<rec, bound, reconciled>>

\* Reconcile (repair --skipped): the line of each skipped entry the store holds
\* as applied is restored; any other stays skipped (refused by name).
Reconcile ==
  /\ done /\ ~reconciled
  /\ LET restore == IF "reconcileblind" \in Broken THEN {} ELSE {c \in skipped : tbl[c] = "post"} IN
       /\ log' = [c \in Cards |-> log[c] \/ c \in restore]
       /\ skipped' = skipped \ restore
  /\ reconciled' = TRUE
  /\ UNCHANGED <<tbl, rec, bound, pending, resent, done, recAt>>

Next ==
  \/ WriterApplies \/ Forget \/ Bound \/ \E c \in Cards : Outside(c)
  \/ Repair \/ Reconcile

Spec == Init /\ [][Next]_vars

\* An entry the table holds as applied has its move line once the repair is
\* done (check rule 13: the log replays to the table).
AppliedHasLine == done => \A c \in Cards : tbl[c] = "post" => log[c]

\* No line is written for an entry that did not apply.
LineOnlyIfApplied == done => \A c \in Cards : log[c] => tbl[c] = "post"

\* A manifest whose own id's record answers is never sent again entry by entry.
ProbeSparesEntries == (done /\ recAt) => ~resent

\* After the reconcile, every entry the table holds as applied has its line.
ReconciledHasLine == reconciled => \A c \in Cards : tbl[c] = "post" => log[c]

=============================================================================
