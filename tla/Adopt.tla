-------------------------------- MODULE Adopt ---------------------------------
\* nova-sprint adopt (docs/SPEC-SPRINT.md, "Adopting a build"; the finding
\* adopt-carries-a-lua-change): the seat adopts a build through the tools play,
\* as a window. The build carries a new function library; the store holds the
\* old one until the play's fn load puts the new one there. Four steps, in
\* order: the pre-window check, fn load, the post-load check, and the rescue.
\*
\* The state: lib, the store's library ("old" or "new"); window, whether the
\* window has opened; loaded, whether fn load has run; refused, whether a check
\* refused the adopt. The outside holds nothing else: the play's own steps are
\* the model.
\*
\* The design, Broken = {}:
\*   PreCheck  the pre-window check (the candidate's shadow tick, its own
\*             `nova-sprint server switch --dry-run`): it reads the store and
\*             tolerates a library the window will load, so it changes nothing
\*             and refuses nothing (libraryMatches's libraryShadow).
\*   Open      the window opens while it is closed (a tool to replace, or a
\*             library difference).
\*   Load      inside the window the play loads the build's library (fn load);
\*             a load may fail and leave the old library, which is exactly what
\*             the post-load check is there to catch.
\*   PostCheck the check after fn load: a store whose library is still not the
\*             build's refuses the adopt, and the old library stays loaded
\*             (libraryMatches).
\*   Rescue    a refusal for any other check that does not hold (the shadow
\*             tick's other mismatch, a friend reinstall, a server that will
\*             not start) restores the old library before it is said.
\* Reversed witness, caught by RefusedLeavesOld:
\*   "keep-new"  the rescue leaves the new library loaded after a refusal.
EXTENDS Naturals

CONSTANTS Broken

VARIABLES lib, window, loaded, refused
vars == <<lib, window, loaded, refused>>

Lib == {"old", "new"}

TypeOK ==
    /\ lib \in Lib
    /\ window \in BOOLEAN
    /\ loaded \in BOOLEAN
    /\ refused \in BOOLEAN

Init ==
    /\ lib = "old"
    /\ window = FALSE
    /\ loaded = FALSE
    /\ refused = FALSE

\* the pre-window check: the shadow tick reads the store and tolerates a
\* library the window will load; it changes nothing
PreCheck ==
    /\ ~window
    /\ ~refused
    /\ UNCHANGED vars

\* the window opens while it is closed
Open ==
    /\ ~window
    /\ ~refused
    /\ window' = TRUE
    /\ UNCHANGED <<lib, loaded, refused>>

\* inside the window, fn load puts the build's library on the store, or fails
\* and leaves the old one: a load is not guaranteed to take
Load ==
    /\ window
    /\ ~loaded
    /\ ~refused
    /\ loaded' = TRUE
    /\ lib' \in Lib
    /\ UNCHANGED <<window, refused>>

\* the post-load check: after fn load the store's library must be the build's;
\* a library that is still the old one refuses the adopt, and the old library
\* stays loaded; the build's library passes
PostCheck ==
    /\ loaded
    /\ ~refused
    /\ IF lib = "new"
       THEN UNCHANGED <<lib, window, loaded, refused>>
       ELSE /\ window' = FALSE
            /\ refused' = TRUE
            /\ UNCHANGED <<lib, loaded>>

\* the rescue: a refusal for any other check restores the old library before
\* the refusal is said, so a refused adopt never leaves the new library loaded
Rescue ==
    /\ loaded
    /\ ~refused
    /\ window
    /\ lib = "new"
    /\ window' = FALSE
    /\ refused' = TRUE
    /\ loaded' = FALSE
    /\ lib' = IF "keep-new" \in Broken THEN "new" ELSE "old"

Next ==
    \/ PreCheck
    \/ Open
    \/ Load
    \/ PostCheck
    \/ Rescue

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* a refused adopt leaves the old library loaded: the refusal and the rescue
\* that restores the old library are one step, so a store is never left with
\* the new library after the adopt is refused
RefusedLeavesOld == refused => lib = "old"

=============================================================================
