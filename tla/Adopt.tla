-------------------------------- MODULE Adopt ---------------------------------
\* nova-sprint adopt (docs/SPEC-SPRINT.md, "Adopting a build"): the seat adopts
\* a build through the tools play, as a window. The build carries a new
\* function library; the store holds the old one until the play's window loads
\* it (fn load). The shadow tick (its own `nova-sprint server switch --dry-run`)
\* plans a tick on the store and must plan against the library the build loads,
\* so it runs inside the window after the load, never before it: a pre-window
\* shadow tick against the old library refuses a library digest, which is the
\* one change an adopt must be able to carry (the finding
\* adopt-carries-a-lua-change; cmd/nova-sprint/main.go libraryMatches).
\*
\* The state: lib, the store's library ("old" or "new"); window, whether the
\* window has opened; loaded, whether the play has loaded the build's library;
\* ticked, whether the shadow tick has run. The outside holds nothing else: the
\* play's own steps are the model.
\*
\* The design, Broken = {}:
\*   Open    the window opens while it is closed.
\*   Load    inside the window the play loads the build's library, so the
\*           store's library is now the build's.
\*   Tick    the shadow tick plans on the store, only after the load: a shadow
\*           tick against a library the adopt has not loaded is the pre-window
\*           shadow tick that refuses a library change.
\*   Rescue  a refusal inside the window restores the old library and starts
\*           the old agents: the store's library is "old" again and the shadow
\*           tick is owed again.
\* Reversed witness, caught by TickAfterLoad below:
\*   "shadow-first"  the shadow tick may run before the load: the pre-window
\*                   shadow tick, which refuses the library change it must carry
EXTENDS Naturals

CONSTANTS Broken

VARIABLES lib, window, loaded, ticked
vars == <<lib, window, loaded, ticked>>

Lib == {"old", "new"}

TypeOK ==
    /\ lib \in Lib
    /\ window \in BOOLEAN
    /\ loaded \in BOOLEAN
    /\ ticked \in BOOLEAN
    /\ loaded => lib = "new"
    /\ loaded => window

Init ==
    /\ lib = "old"
    /\ window = FALSE
    /\ loaded = FALSE
    /\ ticked = FALSE

\* the window opens while it is closed (a tool to replace, or a library change)
Open ==
    /\ ~window
    /\ window' = TRUE
    /\ UNCHANGED <<lib, loaded, ticked>>

\* inside the window, fn load puts the build's library on the store
Load ==
    /\ window
    /\ ~loaded
    /\ lib' = "new"
    /\ loaded' = TRUE
    /\ UNCHANGED <<window, ticked>>

\* the shadow tick plans on the store, and only after the load: the design runs
\* it inside the window, where the library the shadow reads is the build's
Tick ==
    /\ IF "shadow-first" \in Broken THEN TRUE ELSE loaded
    /\ ~ticked
    /\ ticked' = TRUE
    /\ UNCHANGED <<lib, window, loaded>>

\* a refusal inside the window restores the library of before and starts the old
\* agents: the shadow tick is owed again on the next run
Rescue ==
    /\ window
    /\ lib = "new"
    /\ lib' = "old"
    /\ loaded' = FALSE
    /\ ticked' = FALSE
    /\ UNCHANGED <<window>>

Next ==
    \/ Open
    \/ Load
    \/ Tick
    \/ Rescue

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* the shadow tick runs only inside the window, after the library is loaded:
\* it never plans against the old library an adopt is about to replace
TickAfterLoad == ticked => (window /\ loaded)

=============================================================================
