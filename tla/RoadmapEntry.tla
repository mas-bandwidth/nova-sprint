----------------------------- MODULE RoadmapEntry -----------------------------
\* nova-work roadmap (cmd/nova-work/roadmap.go, internal/roadmap): the life of
\* one entry of a repository's own work record, docs/roadmap.sexp (ROADMAP.md)
\* and docs/fixes.sexp (FIXES.md). An entry is a roadmap item under a group
\* (todo), a fix in a point release (open: planned or in-progress), or done: a
\* roadmap item moved to the roadmap's :done list, or a fix whose status is done
\* (or shipped, when its release is cut).
\*
\* The state: where[e], the places whose file holds a record of e (a group or
\* "donelist" in the roadmap file, a release in the fixes file); st[e], the
\* entry's state; live, the groups and releases the files declare; shipped, the
\* releases cut; added, the entries added and not removed; pend, a pull across
\* the two files whose destination is written and whose source is not yet
\* (<<>> when none); torn, the entries a crash left in both files. The outside:
\* a hand edit declares a group or release with its first entry, a release is
\* cut, and a verb can stop between its two file writes.
\*
\* The design, Broken = {}:
\*   Add      an absent id goes into a live group or an uncut release.
\*   Remove   a todo or open entry leaves; never the last of its group or
\*            release (every group and release holds an entry), never from a
\*            cut release.
\*   Pull     a todo or open entry moves to another live group or uncut
\*            release, never out of a cut one and never as the last of its
\*            place. Within one file it is one write. Across the files the
\*            destination is written first and the source second, so a stop
\*            between them leaves the entry twice (torn), never nowhere.
\*   Done     a todo item moves to the roadmap's :done list (never the last of
\*            its group); an open fix becomes done in place. Done is final.
\*   Repair   a torn entry is cut back to one copy; every verb refuses while
\*            an id stands twice (check names it), so nothing builds on a tear.
\*   Ship     a release is cut (its :status, a hand edit): nothing is added
\*            to it, removed from it or pulled into or out of it again. Its
\*            fixes' own statuses are a separate edit, so the release's guard
\*            stands on its own.
\* Reversed witnesses, each caught by one property below:
\*   "lastchild"   remove, pull and done skip the last-entry guard: ParentsHold
\*   "sourcefirst" a pull across the files writes the source first: NoLoss
\*   "movedone"    pull takes a done entry: DoneFinal
\*   "shippedmove" pull takes an entry out of a cut release: ShippedFrozen
EXTENDS Naturals, FiniteSets

CONSTANTS Entries, Groups, Releases, Broken

VARIABLES where, st, live, shipped, added, pend, torn
vars == <<where, st, live, shipped, added, pend, torn>>

Parents == Groups \cup Releases
Places == Parents \cup {"donelist"}
States == {"absent", "todo", "open", "done"}

\* the state an entry has at a place: an item under a group, a fix in a release
Kind(p) == IF p \in Groups THEN "todo" ELSE "open"
File(p) == IF p \in Releases THEN "fixes" ELSE "roadmap"
Holding(p) == {e \in Entries : p \in where[e]}
\* the one place an entry at rest is in (where[e] is a singleton then)
Home(e) == CHOOSE p \in where[e] : TRUE

TypeOK ==
    /\ where \in [Entries -> SUBSET Places]
    /\ st \in [Entries -> States]
    /\ live \subseteq Parents
    /\ shipped \subseteq live \cap Releases
    /\ added \subseteq Entries
    /\ torn \subseteq Entries
    /\ pend = <<>> \/ (pend[1] \in Entries /\ pend[2] \in Parents /\ pend[3] \in Parents)

Init ==
    /\ where = [e \in Entries |-> {}]
    /\ st = [e \in Entries |-> "absent"]
    /\ live = {}
    /\ shipped = {}
    /\ added = {}
    /\ pend = <<>>
    /\ torn = {}

\* every verb loads both files first, and the load refuses an id that stands
\* twice; no verb runs while another's two writes are half done
Quiet == pend = <<>> /\ torn = {}

\* the last-entry guard: an entry may leave p only when p holds another
Leaves(p) == "lastchild" \in Broken \/ Cardinality(Holding(p)) > 1

\* the outside: a hand edit declares a group or release with its first entry
Declare(p, e) ==
    /\ Quiet
    /\ p \notin live
    /\ st[e] = "absent"
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ live' = live \cup {p}
    /\ added' = added \cup {e}
    /\ UNCHANGED <<shipped, pend, torn>>

Add(e, p) ==
    /\ Quiet
    /\ st[e] = "absent"
    /\ p \in live
    /\ p \notin shipped
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ added' = added \cup {e}
    /\ UNCHANGED <<live, shipped, pend, torn>>

Remove(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ Home(e) \notin shipped
    /\ Leaves(Home(e))
    /\ where' = [where EXCEPT ![e] = {}]
    /\ st' = [st EXCEPT ![e] = "absent"]
    /\ added' = added \ {e}
    /\ UNCHANGED <<live, shipped, pend, torn>>

\* the guards every pull shares
CanPull(e, to) ==
    /\ Quiet
    /\ st[e] \in IF "movedone" \in Broken THEN {"todo", "open", "done"} ELSE {"todo", "open"}
    /\ where[e] # {}
    /\ Home(e) \in Parents
    /\ to \in live
    /\ to # Home(e)
    /\ to \notin shipped
    /\ "shippedmove" \in Broken \/ Home(e) \notin shipped
    /\ Leaves(Home(e))

\* within one file: one atomic write
PullWithin(e, to) ==
    /\ CanPull(e, to)
    /\ File(to) = File(Home(e))
    /\ where' = [where EXCEPT ![e] = {to}]
    /\ st' = [st EXCEPT ![e] = Kind(to)]
    /\ UNCHANGED <<live, shipped, added, pend, torn>>

\* across the files: the first of two writes
PullStart(e, to) ==
    /\ CanPull(e, to)
    /\ File(to) # File(Home(e))
    /\ where' = [where EXCEPT ![e] = IF "sourcefirst" \in Broken THEN {} ELSE {Home(e), to}]
    /\ pend' = <<e, Home(e), to>>
    /\ UNCHANGED <<st, live, shipped, added, torn>>

\* the second write
PullEnd ==
    /\ pend # <<>>
    /\ where' = [where EXCEPT ![pend[1]] = {pend[3]}]
    /\ st' = [st EXCEPT ![pend[1]] = Kind(pend[3])]
    /\ pend' = <<>>
    /\ UNCHANGED <<live, shipped, added, torn>>

\* the verb stops between its two writes: what is on disk stays
Crash ==
    /\ pend # <<>>
    /\ pend' = <<>>
    /\ torn' = IF Cardinality(where[pend[1]]) > 1 THEN torn \cup {pend[1]} ELSE torn
    /\ UNCHANGED <<where, st, live, shipped, added>>

\* check names the id that stands twice; one copy is cut (by hand)
Repair(e, p) ==
    /\ pend = <<>>
    /\ e \in torn
    /\ p \in where[e]
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ torn' = torn \ {e}
    /\ UNCHANGED <<live, shipped, added, pend>>

Done(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ Home(e) \notin shipped
    /\ \/ /\ Home(e) \in Groups
          /\ Leaves(Home(e))
          /\ where' = [where EXCEPT ![e] = {"donelist"}]
       \/ /\ Home(e) \in Releases
          /\ UNCHANGED where
    /\ st' = [st EXCEPT ![e] = "done"]
    /\ UNCHANGED <<live, shipped, added, pend, torn>>

\* the outside: a release is cut
Ship(r) ==
    /\ Quiet
    /\ r \in live \cap Releases
    /\ r \notin shipped
    /\ shipped' = shipped \cup {r}
    /\ UNCHANGED <<where, st, live, added, pend, torn>>

Next ==
    \/ \E p \in Parents, e \in Entries : Declare(p, e)
    \/ \E e \in Entries, p \in Parents : Add(e, p)
    \/ \E e \in Entries : Remove(e)
    \/ \E e \in Entries, p \in Parents : PullWithin(e, p) \/ PullStart(e, p)
    \/ PullEnd
    \/ Crash
    \/ \E e \in Entries, p \in Places : Repair(e, p)
    \/ \E e \in Entries : Done(e)
    \/ \E r \in Releases : Ship(r)

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* every group and release the files declare holds an entry (the decoder
\* refuses "group holds no item" and "release holds no fix")
ParentsHold == \A p \in live : Holding(p) # {}

\* every record sits under a group or release the files declare
ChildLinked == \A e \in Entries : \A p \in where[e] : p \in live \/ p = "donelist"

\* an entry added and not removed is in a file: no write order loses it
NoLoss == \A e \in added : where[e] # {}

\* at rest an entry is in one place; twice only mid-pull or after a stop that
\* check names
OneHome == \A e \in Entries :
    Cardinality(where[e]) <= 1 \/ (pend # <<>> /\ pend[1] = e) \/ e \in torn

\* done is final: a done entry never moves and never comes back
DoneFinal == [][\A e \in Entries : st[e] = "done" => (st'[e] = "done" /\ where'[e] = where[e])]_vars

\* a cut release's entries never change
ShippedFrozen == [][\A r \in shipped : {e \in Entries : r \in where'[e]} = Holding(r)]_vars

=============================================================================
