---------------------------- MODULE RoadmapEntry -----------------------------
\* nova-work roadmap (cmd/nova-work/roadmap.go, internal/roadmap): the life of
\* one entry of a repository's own work record, docs/roadmap.sexp (ROADMAP.md)
\* and docs/fixes.sexp (FIXES.md). An entry is a roadmap item under a group
\* (todo), a fix in a point release (open: planned or in-progress), or done: a
\* roadmap item moved to the roadmap's :done list, or a fix whose status is done
\* (or shipped, when its release is cut). A scheduled note is a record, not
\* work: it stands in the roadmap file with a :release that decode() never
\* checks against the fixes file's releases (internal/roadmapdoc/roadmap.go).
\*
\* The state: where[e], the places whose file holds a record of e (a group or
\* "donelist" in the roadmap file, a release in the fixes file); st[e], the
\* entry's state; page[e], the places the rendered page still shows e (it lags
\* where[e] between a file's data write and its page write); live, the groups
\* and releases the files declare; shipped, the releases cut; added, the entries
\* added and not removed; pend, a pull across the two files mid-Save; torn, the
\* entries a crash left in both files. The outside: a hand edit declares a group
\* or release with its first entry, a release is cut, and a verb can stop
\* between writes.
\*
\* The design, Broken = {}:
\*   Add      an absent id goes into a live group or an uncut release.
\*   Remove   a todo or open entry leaves; never the last of its group or
\*            release (every group and release holds an entry), never from a
\*            cut release.
\*   Pull     a todo or open entry moves to another live group or uncut
\*            release, never out of a cut one and never as the last of its
\*            place. Within one file it is one write. Across the files Save
\*            writes the destination's file first, each file's data before its
\*            page, four writes, so a stop between any two leaves the entry
\*            twice (torn), never nowhere.
\*   Note     a todo item or an open fix is retitled and its text appended in
\*            place: its place and state do not change.
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
\*   "pagefirst"   a pull writes each file's page before its data: DataBeforePage
EXTENDS Naturals, FiniteSets

CONSTANTS Entries, Groups, Releases, Scheduled, Broken

VARIABLES where, st, page, live, shipped, added, pend, torn, title, text
vars == <<where, st, page, live, shipped, added, pend, torn, title, text>>

Parents == Groups \cup Releases
Places == Parents \cup {"donelist"}
States == {"absent", "todo", "open", "done", "scheduled"}

\* the state an entry has at a place: an item under a group, a fix in a release
Kind(p) == IF p \in Groups THEN "todo" ELSE "open"
File(p) == IF p \in Releases THEN "fixes" ELSE "roadmap"
Holding(p) == {e \in Entries : p \in where[e]}
\* the one place an entry at rest is in (where[e] is a singleton then)
Home(e) == CHOOSE p \in where[e] : TRUE

TypeOK ==
    /\ where \in [Entries -> SUBSET Places]
    /\ st \in [Entries -> States]
    /\ page \in [Entries -> SUBSET Places]
    /\ title \in [Entries -> {0, 1}]
    /\ text \in [Entries -> {0, 1}]
    /\ live \subseteq Parents
    /\ shipped \subseteq live \cap Releases
    /\ added \subseteq Entries
    /\ torn \subseteq Entries
    /\ Scheduled \subseteq Entries
    /\ pend = <<>> \/ (pend[1] \in Entries /\ pend[2] \in Parents /\ pend[3] \in Parents /\ pend[4] \in {1, 2, 3})

Init ==
    /\ where = [e \in Entries |-> {}]
    /\ st = [e \in Entries |-> IF e \in Scheduled THEN "scheduled" ELSE "absent"]
    /\ page = [e \in Entries |-> {}]
    /\ title = [e \in Entries |-> 0]
    /\ text = [e \in Entries |-> 0]
    /\ live = {}
    /\ shipped = {}
    /\ added = {}
    /\ pend = <<>>
    /\ torn = {}

\* every verb loads both files first, and the load refuses an id that stands
\* twice; no verb runs while another's Save is half done
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
    /\ page' = [page EXCEPT ![e] = {p}]
    /\ live' = live \cup {p}
    /\ added' = added \cup {e}
    /\ UNCHANGED <<shipped, pend, torn, title, text>>

Add(e, p) ==
    /\ Quiet
    /\ st[e] = "absent"
    /\ p \in live
    /\ p \notin shipped
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ page' = [page EXCEPT ![e] = {p}]
    /\ added' = added \cup {e}
    /\ UNCHANGED <<live, shipped, pend, torn, title, text>>

Remove(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ Home(e) \notin shipped
    /\ Leaves(Home(e))
    /\ where' = [where EXCEPT ![e] = {}]
    /\ st' = [st EXCEPT ![e] = "absent"]
    /\ page' = [page EXCEPT ![e] = {}]
    /\ added' = added \ {e}
    /\ UNCHANGED <<live, shipped, pend, torn, title, text>>

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
    /\ page' = [page EXCEPT ![e] = {to}]
    /\ UNCHANGED <<live, shipped, added, pend, torn, title, text>>

\* Save's four writes across the files: add to the destination's data, add to
\* its page, remove from the source's data, remove from its page. The code's
\* order is the destination's file first, each file's data before its page.
\* "sourcefirst" writes the source's file before the destination's (NoLoss
\* breaks); "pagefirst" writes each file's page before its data (DataBeforePage
\* breaks).
Writes ==
    IF "sourcefirst" \in Broken
    THEN IF "pagefirst" \in Broken THEN <<4, 3, 2, 1>> ELSE <<3, 4, 1, 2>>
    ELSE IF "pagefirst" \in Broken THEN <<2, 1, 4, 3>> ELSE <<1, 2, 3, 4>>

\* write n of the four, by its number
Write(n, e, from, to) ==
    CASE n = 1 -> /\ where' = [where EXCEPT ![e] = where[e] \cup {to}]
                  /\ UNCHANGED page
      [] n = 2 -> /\ page' = [page EXCEPT ![e] = page[e] \cup {to}]
                  /\ UNCHANGED where
      [] n = 3 -> /\ where' = [where EXCEPT ![e] = where[e] \ {from}]
                  /\ UNCHANGED page
      [] n = 4 -> /\ page' = [page EXCEPT ![e] = page[e] \ {from}]
                  /\ UNCHANGED where

\* the first of the four writes
PullStart(e, to) ==
    /\ CanPull(e, to)
    /\ File(to) # File(Home(e))
    /\ LET from == Home(e)
       IN /\ Write(Writes[1], e, from, to)
          /\ pend' = <<e, from, to, 1>>
    /\ UNCHANGED <<st, live, shipped, added, torn, title, text>>

\* the second, third and fourth writes
PullWrite ==
    \/ \E e \in Entries, from \in Parents, to \in Parents :
        /\ pend = <<e, from, to, 1>>
        /\ Write(Writes[2], e, from, to)
        /\ pend' = <<e, from, to, 2>>
        /\ UNCHANGED <<st, live, shipped, added, torn, title, text>>
    \/ \E e \in Entries, from \in Parents, to \in Parents :
        /\ pend = <<e, from, to, 2>>
        /\ Write(Writes[3], e, from, to)
        /\ pend' = <<e, from, to, 3>>
        /\ UNCHANGED <<st, live, shipped, added, torn, title, text>>
    \/ \E e \in Entries, from \in Parents, to \in Parents :
        /\ pend = <<e, from, to, 3>>
        /\ Write(Writes[4], e, from, to)
        /\ pend' = <<>>
        /\ UNCHANGED <<st, live, shipped, added, torn, title, text>>

\* the verb stops between writes: what is on disk stays
Crash ==
    /\ pend # <<>>
    /\ pend' = <<>>
    /\ torn' = IF Cardinality(where[pend[1]]) > 1 THEN torn \cup {pend[1]} ELSE torn
    /\ UNCHANGED <<where, st, page, live, shipped, added, title, text>>

\* check names the id that stands twice; one copy is cut (by hand)
Repair(e, p) ==
    /\ pend = <<>>
    /\ e \in torn
    /\ p \in where[e]
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ page' = [page EXCEPT ![e] = {p}]
    /\ torn' = torn \ {e}
    /\ UNCHANGED <<live, shipped, added, pend, title, text>>

Done(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ Home(e) \notin shipped
    /\ \/ /\ Home(e) \in Groups
          /\ Leaves(Home(e))
          /\ where' = [where EXCEPT ![e] = {"donelist"}]
          /\ page' = [page EXCEPT ![e] = {"donelist"}]
       \/ /\ Home(e) \in Releases
          /\ UNCHANGED <<where, page>>
    /\ st' = [st EXCEPT ![e] = "done"]
    /\ UNCHANGED <<live, shipped, added, pend, torn, title, text>>

\* note retitles a todo item or an open fix and appends to its text; its place
\* and state stay (the code's Note, internal/roadmap/roadmap.go, calls entry()
\* with no open() or release() guard; this card models the safe case: an entry
\* that is still work, in a release not cut)
Note(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ Home(e) \notin shipped
    /\ title' = [title EXCEPT ![e] = 1]
    /\ text' = [text EXCEPT ![e] = 1]
    /\ UNCHANGED <<where, st, page, live, shipped, added, pend, torn>>

\* the outside: a release is cut
Ship(r) ==
    /\ Quiet
    /\ r \in live \cap Releases
    /\ r \notin shipped
    /\ shipped' = shipped \cup {r}
    /\ UNCHANGED <<where, st, page, live, added, pend, torn, title, text>>

Next ==
    \/ \E p \in Parents, e \in Entries : Declare(p, e)
    \/ \E e \in Entries, p \in Parents : Add(e, p)
    \/ \E e \in Entries : Remove(e)
    \/ \E e \in Entries, p \in Parents : PullWithin(e, p) \/ PullStart(e, p)
    \/ PullWrite
    \/ Crash
    \/ \E e \in Entries, p \in Places : Repair(e, p)
    \/ \E e \in Entries : Done(e)
    \/ \E e \in Entries : Note(e)
    \/ \E r \in Releases : Ship(r)

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* every group and release the files declare holds an entry (the decoder
\* refuses "group holds no item" and "release holds no fix")
ParentsHold == \A p \in live : Holding(p) # {}

\* every work record sits under a group or release the files declare; a
\* scheduled note's :release is not checked (decode() checks an item's :group
\* and a fix's :release only, internal/roadmapdoc/roadmap.go, fixes.go)
ChildLinked == \A e \in Entries \ Scheduled : \A p \in where[e] : p \in live \/ p = "donelist"

\* an entry added and not removed is in a file: no write order loses it
NoLoss == \A e \in added : where[e] # {}

\* within a file Save writes the data before the page: a file's page never
\* shows the pull done that its data has not (dest) or its data not done that
\* its page has (source)
DataBeforePage ==
    \A e \in Entries :
        (pend # <<>> /\ pend[1] = e) =>
            /\ (pend[3] \in page[e] => pend[3] \in where[e])
            /\ (pend[2] \notin page[e] => pend[2] \notin where[e])

\* at rest an entry is in one place; twice only mid-pull or after a stop that
\* check names
OneHome == \A e \in Entries :
    Cardinality(where[e]) <= 1 \/ (pend # <<>> /\ pend[1] = e) \/ e \in torn

\* done is final: a done entry never moves and never comes back
DoneFinal == [][\A e \in Entries : st[e] = "done" => (st'[e] = "done" /\ where'[e] = where[e])]_vars

\* a cut release's entries never change
ShippedFrozen == [][\A r \in shipped : {e \in Entries : r \in where'[e]} = Holding(r)]_vars

=============================================================================
