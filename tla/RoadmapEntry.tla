----------------------------- MODULE RoadmapEntry -----------------------------
\* nova-work roadmap (cmd/nova-work/roadmap.go, internal/roadmap): the life of
\* one entry of a repository's own work record, docs/roadmap.sexp (ROADMAP.md)
\* and docs/fixes.sexp (FIXES.md). An entry is a roadmap item under a group
\* (todo), a fix in a point release (open: planned or in-progress), a scheduled
\* note that names a release, or done: a roadmap item moved to the roadmap's
\* :done list, or a fix whose status is done (or shipped, when its release is
\* cut).
\*
\* The state: where[e], the places whose file holds a record of e (a group or
\* "donelist" in the roadmap file, a release in either file); st[e], the
\* entry's state; live, the groups and releases the files declare; shipped, the
\* releases cut; added, the entries added and not removed; pend, a pull across
\* the two files in flight (<<>> when none); torn, the entries a crash left in
\* both files; title[e] and text[e], the words a note has written; dataed[f]
\* and paged[f], whether Save has written a file's data and its page; stage,
\* how many writes of a pull across the files are done. The outside: a hand
\* edit declares a group or release with its first entry and records a
\* scheduled note, a release is cut, and a verb can stop between two writes.
\*
\* The design, Broken = {}:
\*   Add      an absent id goes into a live group or an uncut release.
\*   Remove   a todo or open entry leaves; never the last of its group or
\*            release (every group and release holds an entry), never from a
\*            cut release.
\*   Pull     a todo or open entry moves to another live group or uncut
\*            release, never out of a cut one and never as the last of its
\*            place. Within one file the data and the page are written. Across
\*            the files Save writes the destination file's data, then its
\*            page, then the source file's data, then its page, and a stop is
\*            possible between any two: the entry is torn across the files
\*            until the source data is written, never nowhere.
\*   Done     a todo item moves to the roadmap's :done list (never the last of
\*            its group); an open fix becomes done in place. Done is final.
\*   Repair   a torn entry is cut back to one copy; every verb refuses while
\*            an id stands twice (check names it), so nothing builds on a tear.
\*   Note     an open entry's title and text change; its place does not. The
\*            code has no open()/release() guard here, so it also edits a done
\*            fix or a shipped release; that unguarded case is outside this
\*            module (a v1.2.14 candidate).
\*   Schedule a hand edit records a scheduled note in a release's list without
\*            declaring the release: decode checks that a scheduled note has a
\*            :release field, never that a (release ...) record names it.
\*   Ship     a release is cut (its :status, a hand edit): nothing is added
\*            to it, removed from it or pulled into or out of it again. Its
\*            fixes' own statuses are a separate edit, so the release's guard
\*            stands on its own.
\* Reversed witnesses, each caught by one property below:
\*   "lastchild"   remove, pull and done skip the last-entry guard: ParentsHold
\*   "sourcefirst" a pull across the files writes the source data first: NoLoss
\*   "movedone"    pull takes a done entry: DoneFinal
\*   "shippedmove" pull takes an entry out of a cut release: ShippedFrozen
\*   "pagefirst"   Save writes a file's page before its data: PageAfterData
EXTENDS Naturals, FiniteSets

CONSTANTS Entries, Groups, Releases, Scheduled, Broken

VARIABLES where, st, live, shipped, added, pend, torn, title, text, dataed, paged, stage
vars == <<where, st, live, shipped, added, pend, torn, title, text, dataed, paged, stage>>

Parents == Groups \cup Releases
Places == Parents \cup {"donelist"}
States == {"absent", "todo", "open", "done", "scheduled"}
Files == {"roadmap", "fixes"}
Titles == {"", "n"}
Words == {"x"}

\* the state an entry has at a place: an item under a group, a fix in a release
Kind(p) == IF p \in Groups THEN "todo" ELSE "open"
File(p) == IF p \in Releases THEN "fixes" ELSE "roadmap"
Holding(p) == {e \in Entries : p \in where[e]}
\* the one place an entry at rest is in (where[e] is a singleton then)
Home(e) == CHOOSE p \in where[e] : TRUE

\* Save writes each changed file's data then its page; "pagefirst" writes the
\* page before the data it renders
FirstWrite == IF "pagefirst" \in Broken THEN "page" ELSE "data"
SecondWrite == IF "pagefirst" \in Broken THEN "data" ELSE "page"
\* Save writes the file the entry moved into first; "sourcefirst" writes the
\* file it left first, so the entry is cut from the source before it is added
\* to the destination and a stop between the two loses it
FirstFile == IF "sourcefirst" \in Broken THEN "source" ELSE "dest"
SecondFile == IF FirstFile = "dest" THEN "source" ELSE "dest"

TypeOK ==
    /\ where \in [Entries -> SUBSET Places]
    /\ st \in [Entries -> States]
    /\ live \subseteq Parents
    /\ shipped \subseteq live \cap Releases
    /\ added \subseteq Entries
    /\ torn \subseteq Entries
    /\ Scheduled \subseteq Entries
    /\ title \in [Entries -> Titles]
    /\ text \in [Entries -> SUBSET Words]
    /\ dataed \in [Files -> BOOLEAN]
    /\ paged \in [Files -> BOOLEAN]
    /\ stage \in 0..4
    /\ pend = <<>> \/ (pend[1] \in Entries /\ pend[2] \in Parents /\ pend[3] \in Parents)

Init ==
    /\ where = [e \in Entries |-> {}]
    /\ st = [e \in Entries |-> "absent"]
    /\ live = {}
    /\ shipped = {}
    /\ added = {}
    /\ pend = <<>>
    /\ torn = {}
    /\ title = [e \in Entries |-> ""]
    /\ text = [e \in Entries |-> {}]
    /\ dataed = [f \in Files |-> FALSE]
    /\ paged = [f \in Files |-> FALSE]
    /\ stage = 0

\* every verb loads both files first, and the load refuses an id that stands
\* twice; no verb runs while another's writes are half done
Quiet == pend = <<>> /\ torn = {}

\* the last-entry guard: an entry may leave p only when p holds another entry
Leaves(p) == "lastchild" \in Broken \/ Cardinality(Holding(p)) > 1

\* the outside: a hand edit declares a group or release with its first entry
Declare(p, e) ==
    /\ Quiet
    /\ p \notin live
    /\ st[e] = "absent"
    /\ e \notin Scheduled
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ live' = live \cup {p}
    /\ added' = added \cup {e}
    /\ UNCHANGED <<shipped, pend, torn, title, text, dataed, paged, stage>>

Add(e, p) ==
    /\ Quiet
    /\ st[e] = "absent"
    /\ e \notin Scheduled
    /\ p \in live
    /\ p \notin shipped
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ added' = added \cup {e}
    /\ UNCHANGED <<live, shipped, pend, torn, title, text, dataed, paged, stage>>

Remove(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ Home(e) \notin shipped
    /\ Leaves(Home(e))
    /\ where' = [where EXCEPT ![e] = {}]
    /\ st' = [st EXCEPT ![e] = "absent"]
    /\ added' = added \ {e}
    /\ UNCHANGED <<live, shipped, pend, torn, title, text, dataed, paged, stage>>

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

\* within one file: the data and the page are written in one step
PullWithin(e, to) ==
    /\ CanPull(e, to)
    /\ File(to) = File(Home(e))
    /\ where' = [where EXCEPT ![e] = {to}]
    /\ st' = [st EXCEPT ![e] = Kind(to)]
    /\ dataed' = [dataed EXCEPT ![File(to)] = TRUE]
    /\ paged' = [paged EXCEPT ![File(to)] = TRUE]
    /\ UNCHANGED <<live, shipped, added, pend, torn, title, text, stage>>

\* across the files: Save's first write, the file it writes first's first write
\* (its data, or, when "pagefirst", its page)
PullStart(e, to) ==
    /\ CanPull(e, to)
    /\ File(to) # File(Home(e))
    /\ pend = <<>>
    /\ stage = 0
    /\ LET from == Home(e)
           isData == FirstWrite = "data"
           file == IF FirstFile = "dest" THEN File(to) ELSE File(from)
       IN
       /\ where' = IF ~isData THEN where
                   ELSE IF "sourcefirst" \in Broken THEN {}
                        ELSE {from, to}
       /\ st' = st
       /\ dataed' = IF isData THEN [dataed EXCEPT ![file] = TRUE] ELSE dataed
       /\ paged' = IF isData THEN paged ELSE [paged EXCEPT ![file] = TRUE]
       /\ live' = live
       /\ shipped' = shipped
       /\ added' = added
       /\ torn' = torn
       /\ title' = title
       /\ text' = text
       /\ pend' = <<e, from, to>>
       /\ stage' = 1

\* Save's remaining writes: the first file's second write, then the second
\* file's two writes; a data write for the first file leaves the entry in both
\* files (or, for "sourcefirst", in neither), and a data write for the second
\* file settles it in the destination
SaveStep ==
    /\ pend # <<>>
    /\ stage \in 1..3
    /\ LET e == pend[1]
           from == pend[2]
           to == pend[3]
           firstFile == stage <= 1
           kind == IF stage = 1 \/ stage = 3 THEN SecondWrite ELSE FirstWrite
           isData == kind = "data"
           file == IF firstFile
                    THEN (IF FirstFile = "dest" THEN File(to) ELSE File(from))
                    ELSE (IF SecondFile = "dest" THEN File(to) ELSE File(from))
       IN
       /\ where' = IF ~isData THEN where
                   ELSE IF ~firstFile THEN [where EXCEPT ![e] = {to}]
                        ELSE IF "sourcefirst" \in Broken THEN [where EXCEPT ![e] = {}]
                             ELSE [where EXCEPT ![e] = where[e] \cup {to}]
       /\ st' = IF isData /\ ~firstFile THEN [st EXCEPT ![e] = Kind(to)] ELSE st
       /\ dataed' = IF isData THEN [dataed EXCEPT ![file] = TRUE] ELSE dataed
       /\ paged' = IF isData THEN paged ELSE [paged EXCEPT ![file] = TRUE]
       /\ pend' = IF stage = 3 THEN <<>> ELSE pend
       /\ stage' = IF stage = 3 THEN 0 ELSE stage + 1
       /\ UNCHANGED <<live, shipped, added, torn, title, text>>

\* the verb stops between two writes: what is on disk stays
Crash ==
    /\ pend # <<>>
    /\ pend' = <<>>
    /\ stage' = 0
    /\ torn' = IF Cardinality(where[pend[1]]) > 1 THEN torn \cup {pend[1]} ELSE torn
    /\ UNCHANGED <<where, st, live, shipped, added, title, text, dataed, paged>>

\* check names the id that stands twice; one copy is cut (by hand)
Repair(e, p) ==
    /\ pend = <<>>
    /\ e \in torn
    /\ p \in where[e]
    /\ where' = [where EXCEPT ![e] = {p}]
    /\ st' = [st EXCEPT ![e] = Kind(p)]
    /\ torn' = torn \ {e}
    /\ UNCHANGED <<live, shipped, added, pend, title, text, dataed, paged, stage>>

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
    /\ UNCHANGED <<live, shipped, added, pend, torn, title, text, dataed, paged, stage>>

\* the outside: a hand edit records a scheduled note in a release's list; the
\* release itself need not be declared (decode checks the :release field only)
Schedule(e, r) ==
    /\ Quiet
    /\ e \in Scheduled
    /\ st[e] = "absent"
    /\ r \in Releases
    /\ where' = [where EXCEPT ![e] = {r}]
    /\ st' = [st EXCEPT ![e] = "scheduled"]
    /\ added' = added \cup {e}
    /\ UNCHANGED <<live, shipped, pend, torn, title, text, dataed, paged, stage>>

\* Note retitles and appends the words of an open entry; the place does not move
Note(e) ==
    /\ Quiet
    /\ st[e] \in {"todo", "open"}
    /\ title' = [title EXCEPT ![e] = "n"]
    /\ text' = [text EXCEPT ![e] = text[e] \cup {"x"}]
    /\ UNCHANGED <<where, st, live, shipped, added, pend, torn, dataed, paged, stage>>

\* the outside: a release is cut
Ship(r) ==
    /\ Quiet
    /\ r \in live \cap Releases
    /\ r \notin shipped
    /\ shipped' = shipped \cup {r}
    /\ UNCHANGED <<where, st, live, added, pend, torn, title, text, dataed, paged, stage>>

Next ==
    \/ \E p \in Parents, e \in Entries : Declare(p, e)
    \/ \E e \in Entries, p \in Parents : Add(e, p)
    \/ \E e \in Entries : Remove(e)
    \/ \E e \in Entries, p \in Parents : PullWithin(e, p) \/ PullStart(e, p)
    \/ SaveStep
    \/ Crash
    \/ \E e \in Entries, p \in Places : Repair(e, p)
    \/ \E e \in Entries : Done(e)
    \/ \E e \in Entries, r \in Releases : Schedule(e, r)
    \/ \E e \in Entries : Note(e)
    \/ \E r \in Releases : Ship(r)

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* every group and release the files declare holds an entry (the decoder
\* refuses "group holds no item" and "release holds no fix")
ParentsHold == \A p \in live : Holding(p) # {}

\* every item and fix sits under a group or release the files declare; a
\* scheduled note's :release is not checked by decode, so it is outside this
ChildLinked == \A e \in Entries \ Scheduled : \A p \in where[e] : p \in live \/ p = "donelist"

\* an entry added and not removed is in a file: no write order loses it
NoLoss == \A e \in added : where[e] # {}

\* at rest an entry is in one place; twice only mid-pull or after a stop that
\* check names
OneHome == \A e \in Entries :
    Cardinality(where[e]) <= 1 \/ (pend # <<>> /\ pend[1] = e) \/ e \in torn

\* a file's page is written only after that file's data
PageAfterData == \A f \in Files : paged[f] => dataed[f]

\* done is final: a done entry never moves and never comes back
DoneFinal == [][\A e \in Entries : st[e] = "done" => (st'[e] = "done" /\ where'[e] = where[e])]_vars

\* a cut release's entries never change
ShippedFrozen == [][\A r \in shipped : {e \in Entries : r \in where'[e]} = Holding(r)]_vars

\* a note changes the words and never the place
NoteLeavesPlace == [][\A e \in Entries : (title'[e] # title[e] \/ text'[e] # text[e]) => where'[e] = where[e]]_vars

=============================================================================
