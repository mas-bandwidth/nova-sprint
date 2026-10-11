----------------------------- MODULE RoadmapEntry -----------------------------
\* nova-work roadmap (cmd/nova-work/roadmap.go, internal/roadmap): the life of
\* one entry of a repository's own work record, docs/roadmap.sexp (ROADMAP.md)
\* and docs/fixes.sexp (FIXES.md). An entry is a roadmap item under a group
\* (todo), a fix in a point release (open: planned or in-progress), or done: a
\* roadmap item moved to the roadmap's :done list, or a fix whose status is done
\* (or shipped, when its release is cut). A scheduled note is a record and not
\* work: it sits in the roadmap file, names a release, and never moves here.
\*
\* The state: where[e], the places whose file holds a record of e (a group or
\* "donelist" in the roadmap file, a release in the fixes file); st[e], the
\* entry's state; live, the groups and releases the files declare; shipped, the
\* releases cut; added, the entries added and not removed; pend, a pull across
\* the two files whose writes are half done (<<>> when none); torn, the entries
\* a crash left in both files; page, the files whose rendered page got ahead of
\* their data (a defect, never in the code); title and text, the entry's own
\* words, which Note edits. The outside: a hand edit declares a group or release
\* with its first entry, a release is cut, and a verb can stop between two of
\* its file writes.
\*
\* The design, Broken = {}:
\*   Add      an absent id goes into a live group or an uncut release.
\*   Remove   a todo or open entry leaves; never the last of its group or
\*            release (every group and release holds an entry), never from a
\*            cut release.
\*   Pull     a todo or open entry moves to another live group or uncut
\*            release, never out of a cut one and never as the last of its
\*            place. Within one file it is one write. Across the files Save
\*            writes four files: the destination's data, then its page, then
\*            the source's data, then its page, so a stop between any two
\*            leaves the entry twice (torn), never nowhere, and a page stale,
\*            never ahead.
\*   Done     a todo item moves to the roadmap's :done list (never the last of
\*            its group); an open fix becomes done in place. Done is final.
\*   Note     a todo or open entry's title and text change; its place does not.
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
\*   "pagefirst"   Save writes each file's page before its data: PageNotAhead
EXTENDS Naturals, FiniteSets, Sequences

CONSTANTS Entries, Groups, Releases, Scheduled, Words, Broken

VARIABLES where, st, live, shipped, added, pend, torn, page, title, text
vars == <<where, st, live, shipped, added, pend, torn, page, title, text>>

Parents == Groups \cup Releases
Places == Parents \cup {"donelist"}
States == {"absent", "todo", "open", "done"}
Files == {"roadmap", "fixes"}

\* the state an entry has at a place: an item under a group, a fix in a release
Kind(p) == IF p \in Groups THEN "todo" ELSE "open"
File(p) == IF p \in Releases THEN "fixes" ELSE "roadmap"
Holding(p) == {e \in Entries : p \in where[e]}
\* the one place an entry at rest is in (where[e] is a singleton then)
Home(e) == CHOOSE p \in where[e] : TRUE

AnyWord == CHOOSE w \in Words : TRUE

TypeOK ==
    /\ where \in [Entries -> SUBSET Places]
    /\ st \in [Entries -> States]
    /\ live \subseteq Parents
    /\ shipped \subseteq live \cap Releases
    /\ added \subseteq Entries
    /\ torn \subseteq Entries
    /\ pend = <<>> \/ (/\ pend[1] \in Entries /\ pend[2] \in Parents
                      /\ pend[3] \in Parents /\ pend[4] \in 2..4)
    /\ page \subseteq Files
    /\ title \in [Entries -> Words]
    /\ text \in [Entries -> Words]
    /\ Scheduled \cap (Entries \cup Groups \cup Releases) = {}

Init ==
    /\ where = [e \in Entries |-> {}]
    /\ st = [e \in Entries |-> "absent"]
    /\ live = {}
    /\ shipped = {}
    /\ added = {}
    /\ pend = <<>>
    /\ torn = {}
    /\ page = {}
    /\ title = [e \in Entries |-> AnyWord]
    /\ text = [e \in Entries |-> AnyWord]

\* every verb loads both files first, and the load refuses an id that stands
\* twice; no verb runs while another's writes are half done
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
    /\ UNCHANGED <<shipped, pend, torn, page, title, text>>

Add(e, p) ==
    /\ Quiet
    /\ st[e] = "absent"
    /\ p \in live
    /\ p \notin shipped
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ added' = added \cup {e}
    /\ UNCHANGED <<live, shipped, pend, torn, page, title, text>>

Remove(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ Home(e) \notin shipped
    /\ Leaves(Home(e))
    /\ where' = [where EXCEPT ![e] = {}]
    /\ st' = [st EXCEPT ![e] = "absent"]
    /\ added' = added \ {e}
    /\ UNCHANGED <<live, shipped, pend, torn, page, title, text>>

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
    /\ UNCHANGED <<live, shipped, added, pend, torn, page, title, text>>

\* the four writes of one pull across the files, in the order Save takes them:
\* the destination file's data, then its page, then the source's data, then its
\* page (Save writes each file's data before its page). "pagefirst" writes each
\* file's page before its data; "sourcefirst" writes the source file before the
\* destination.
Writes(e, from, to) ==
    LET dest == IF "pagefirst" \in Broken THEN <<"dpage", "ddata">>
                ELSE <<"ddata", "dpage">>
        src  == IF "pagefirst" \in Broken THEN <<"spage", "sdata">>
                ELSE <<"sdata", "spage">>
    IN IF "sourcefirst" \in Broken THEN src \o dest ELSE dest \o src

\* one of the four writes: ddata puts the entry's record in the destination,
\* sdata cuts it from the source, dpage/spage write the rendered page. A page
\* written before its data stands ahead of it (page), which the code's order
\* never lets happen.
Write(e, from, to, w) ==
    /\ where' = IF w = "ddata" THEN [where EXCEPT ![e] = where[e] \cup {to}]
                ELSE IF w = "sdata" THEN [where EXCEPT ![e] = where[e] \ {from}]
                     ELSE where
    /\ st' = IF w = "ddata" THEN [st EXCEPT ![e] = Kind(to)] ELSE st
    /\ page' = IF w = "dpage" /\ "pagefirst" \in Broken THEN page \cup {File(to)}
               ELSE IF w = "spage" /\ "pagefirst" \in Broken THEN page \cup {File(from)}
                    ELSE IF w = "ddata" THEN page \ {File(to)}
                         ELSE IF w = "sdata" THEN page \ {File(from)}
                              ELSE page

\* the first of the four writes, and the pull is now half done
PullStart(e, to) ==
    /\ CanPull(e, to)
    /\ File(to) # File(Home(e))
    /\ Write(e, Home(e), to, Writes(e, Home(e), to)[1])
    /\ pend' = <<e, Home(e), to, 2>>
    /\ UNCHANGED <<live, shipped, added, torn, title, text>>

\* the next write, in Save's order; the last one ends the pull
Save ==
    /\ pend # <<>>
    /\ LET e == pend[1]  from == pend[2]  to == pend[3]  k == pend[4]
       IN  /\ Write(e, from, to, Writes(e, from, to)[k])
           /\ pend' = IF k = 4 THEN <<>> ELSE <<e, from, to, k + 1>>
    /\ UNCHANGED <<live, shipped, added, torn, title, text>>

\* the verb stops between two writes: what is on disk stays
Crash ==
    /\ pend # <<>>
    /\ pend' = <<>>
    /\ torn' = IF Cardinality(where[pend[1]]) > 1 THEN torn \cup {pend[1]} ELSE torn
    /\ UNCHANGED <<where, st, live, shipped, added, page, title, text>>

\* check names the id that stands twice; one copy is cut (by hand)
Repair(e, p) ==
    /\ pend = <<>>
    /\ e \in torn
    /\ p \in where[e]
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ torn' = torn \ {e}
    /\ UNCHANGED <<live, shipped, added, pend, page, title, text>>

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
    /\ UNCHANGED <<live, shipped, added, pend, torn, page, title, text>>

\* Note changes a todo or open entry's title and text; its place does not move.
\* (The code's Note holds no open() or release() guard, so it edits a done fix
\* or a shipped release too, against the package doc "a release whose status is
\* shipped is frozen"; that is a v1.2.14 candidate, not this module.)
Note(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ \E t \in Words, x \in Words :
         /\ title' = [title EXCEPT ![e] = t]
         /\ text' = [text EXCEPT ![e] = x]
    /\ UNCHANGED <<where, st, live, shipped, added, pend, torn, page>>

\* the outside: a release is cut
Ship(r) ==
    /\ Quiet
    /\ r \in live \cap Releases
    /\ r \notin shipped
    /\ shipped' = shipped \cup {r}
    /\ UNCHANGED <<where, st, live, added, pend, torn, page, title, text>>

Next ==
    \/ \E p \in Parents, e \in Entries : Declare(p, e)
    \/ \E e \in Entries, p \in Parents : Add(e, p)
    \/ \E e \in Entries : Remove(e)
    \/ \E e \in Entries, p \in Parents : PullWithin(e, p) \/ PullStart(e, p)
    \/ Save
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

\* every entry's place is the decoder's check: a roadmap item's :group names a
\* (group ...) record and a fix's :release names a (release ...) record
\* (internal/roadmapdoc). A scheduled note's :release the decoder does not
\* check (roadmapdoc.go:278 reads it, never against the releases), so Scheduled,
\* a constant set, is outside this and its release may name anything.
ChildLinked == \A e \in Entries : \A p \in where[e] : p \in live \/ p = "donelist"

\* an entry added and not removed is in a file: no write order loses it
NoLoss == \A e \in added : where[e] # {}

\* at rest an entry is in one place; twice only mid-pull or after a stop that
\* check names
OneHome == \A e \in Entries :
    Cardinality(where[e]) <= 1 \/ (pend # <<>> /\ pend[1] = e) \/ e \in torn

\* a page is never ahead of its data: the data is written before the page, so a
\* stop between them leaves the page stale, never ahead (Check names a stale
\* page; render mends it)
PageNotAhead == page = {}

\* done is final: a done entry never moves and never comes back
DoneFinal == [][\A e \in Entries : st[e] = "done" => (st'[e] = "done" /\ where'[e] = where[e])]_vars

\* a cut release's entries never change
ShippedFrozen == [][\A r \in shipped : {e \in Entries : r \in where'[e]} = Holding(r)]_vars

=============================================================================
