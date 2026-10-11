-------------------------------- MODULE Adopt ---------------------------------
\* nova-sprint adopt (docs/SPEC-SPRINT.md, "Adopting a build"; the finding
\* adopt-carries-a-lua-change): the seat adopts a build through the tools play,
\* as a window. The build carries a new function library; the store holds the
\* old one until the play's fn load puts the new one there.
\*
\* The state: lib, the store's library ("old" or "new"); window, whether the
\* window has opened; the play's five steps, each a flag that the step has run:
\* stopped, migrated, loaded (the swap, fn load), reinstalled, restarted;
\* refused, whether a check refused the adopt; ever, a ghost that the migration
\* has ever run (the migration is one-way, so a refusal never undoes it).
\*
\* The design, Broken = {}:
\*   PreCheck  the pre-window check (the candidate's shadow tick, its own
\*             `nova-sprint server switch --dry-run`): it reads the store and
\*             tolerates a library the window will load, so it changes nothing;
\*             a store that holds no library, or one the check cannot read,
\*             refuses the adopt before the window opens, still changing
\*             nothing (libraryShadow, cmd/nova-sprint/main.go).
\*   Open      the window opens while it is closed (a tool to replace, or a
\*             library difference).
\*   Stop      inside the window the play boots out the old server and member
\*             (seen gone in ps).
\*   Migrate   the play migrates the configuration store as its owning role
\*             (nova-config migrate --window); the migration is one-way: no
\*             refusal undoes it.
\*   Swap      the play loads the build's library (fn load); a load may fail and
\*             leave the old library, which is exactly what the post-load check
\*             is there to catch.
\*   Reinstall the play installs the tools on fresh inodes.
\*   Restart   the play bootstraps every stopped or stale agent on the installed
\*             binaries.
\*   PostCheck the check after fn load: a store whose library is still not the
\*             build's refuses the adopt, and the old library stays loaded
\*             (libraryMatches).
\*   Rescue    a refusal for any other check that does not hold (the shadow
\*             tick's other mismatch, a friend reinstall, a server that will
\*             not start) restores the old library and tools and restarts the
\*             old agents before it is said; the migration is not undone.
\* Reversed witnesses:
\*   "keep-new"      the rescue leaves the new library loaded after a refusal
\*                   (fails RefusedLeavesOld).
\*   "undomigration" the rescue rolls the migration back (fails
\*                   MigrationIsOneWay): the code's doc says the migration is
\*                   never undone, so a model where it is would be wrong.
EXTENDS Naturals

CONSTANTS Broken

VARIABLES lib, window, stopped, migrated, loaded, reinstalled, restarted, refused, ever
vars == <<lib, window, stopped, migrated, loaded, reinstalled, restarted, refused, ever>>

Lib == {"old", "new"}

TypeOK ==
    /\ lib \in Lib
    /\ window \in BOOLEAN
    /\ stopped \in BOOLEAN
    /\ migrated \in BOOLEAN
    /\ loaded \in BOOLEAN
    /\ reinstalled \in BOOLEAN
    /\ restarted \in BOOLEAN
    /\ refused \in BOOLEAN
    /\ ever \in BOOLEAN

Init ==
    /\ lib = "old"
    /\ window = FALSE
    /\ stopped = FALSE
    /\ migrated = FALSE
    /\ loaded = FALSE
    /\ reinstalled = FALSE
    /\ restarted = FALSE
    /\ refused = FALSE
    /\ ever = FALSE

\* the pre-window check: the shadow tick reads the store and tolerates a
\* library the window will load, changing nothing; a store that holds no
\* library, or one the check cannot read, refuses the adopt before the window
\* opens, still changing nothing
PreCheck ==
    /\ ~window
    /\ ~refused
    /\ \/ UNCHANGED vars
       \/ /\ refused' = TRUE
          /\ UNCHANGED <<lib, window, stopped, migrated, loaded, reinstalled, restarted, ever>>

\* the window opens while it is closed
Open ==
    /\ ~window
    /\ ~refused
    /\ window' = TRUE
    /\ UNCHANGED <<lib, stopped, migrated, loaded, reinstalled, restarted, refused, ever>>

\* inside the window the play boots out the old server and member
Stop ==
    /\ window
    /\ ~stopped
    /\ ~refused
    /\ stopped' = TRUE
    /\ UNCHANGED <<lib, window, migrated, loaded, reinstalled, restarted, refused, ever>>

\* the play migrates the configuration store; the migration is one-way
Migrate ==
    /\ window
    /\ stopped
    /\ ~migrated
    /\ ~refused
    /\ migrated' = TRUE
    /\ ever' = TRUE
    /\ UNCHANGED <<lib, window, stopped, loaded, reinstalled, restarted, refused>>

\* the play loads the build's library (fn load), or fails and leaves the old
\* one: a load is not guaranteed to take
Swap ==
    /\ window
    /\ migrated
    /\ ~loaded
    /\ ~refused
    /\ loaded' = TRUE
    /\ lib' \in Lib
    /\ UNCHANGED <<window, stopped, migrated, reinstalled, restarted, refused, ever>>

\* the play installs the tools on fresh inodes
Reinstall ==
    /\ window
    /\ loaded
    /\ ~reinstalled
    /\ ~refused
    /\ reinstalled' = TRUE
    /\ UNCHANGED <<lib, window, stopped, migrated, loaded, restarted, refused, ever>>

\* the play bootstraps every stopped or stale agent
Restart ==
    /\ window
    /\ reinstalled
    /\ ~restarted
    /\ ~refused
    /\ restarted' = TRUE
    /\ UNCHANGED <<lib, window, stopped, migrated, loaded, reinstalled, refused, ever>>

\* the post-load check: after fn load the store's library must be the build's;
\* a library that is still the old one refuses the adopt, and the old library
\* stays loaded; the build's library passes
PostCheck ==
    /\ loaded
    /\ ~refused
    /\ IF lib = "new"
       THEN UNCHANGED vars
       ELSE /\ window' = FALSE
            /\ refused' = TRUE
            /\ UNCHANGED <<lib, stopped, migrated, loaded, reinstalled, restarted, ever>>

\* the rescue: a refusal for any other check restores the old library and tools
\* and restarts the old agents before the refusal is said, so a refused adopt
\* never leaves the new library loaded; the migration is not undone
Rescue ==
    /\ loaded
    /\ ~refused
    /\ window
    /\ lib = "new"
    /\ window' = FALSE
    /\ refused' = TRUE
    /\ loaded' = FALSE
    /\ reinstalled' = FALSE
    /\ restarted' = FALSE
    /\ migrated' = IF "undomigration" \in Broken THEN FALSE ELSE migrated
    /\ lib' = IF "keep-new" \in Broken THEN "new" ELSE "old"
    /\ UNCHANGED <<stopped, ever>>

Next ==
    \/ PreCheck
    \/ Open
    \/ Stop
    \/ Migrate
    \/ Swap
    \/ Reinstall
    \/ Restart
    \/ PostCheck
    \/ Rescue

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* a refused adopt leaves the old library loaded: the refusal and the rescue
\* that restores the old library are one step, so a store is never left with
\* the new library after the adopt is refused
RefusedLeavesOld == refused => lib = "old"

\* the migration is one-way: once it has run it is never rolled back by a
\* refusal, so a store that was migrated is still migrated
MigrationIsOneWay == ever => migrated

=============================================================================
