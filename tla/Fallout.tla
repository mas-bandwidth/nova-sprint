----------------------------- MODULE Fallout -----------------------------
\* The fallout of a card's change (docs/SPEC-SPRINT.md section 7, the lander's
\* checks; the owner, 2026-10-19: "I want this class of bugs fixed in v1.9.
\* This is slowing us down."). A change that renames an identifier or moves a
\* path leaves stale references in files just outside the brief's PATHS; each
\* one costs a read cycle, a rework or a follow-up card. The rule is that
\* fallout is in scope: a file outside PATHS may change when every hunk in it
\* only follows the card's own change (a rename of an old name to its new one,
\* a line dropped because its only reference is a removed name). Three gates
\* read the same sweep:
\*
\*   Finish   the sweep gate. While any file still mentions an old or removed
\*            name the finish is refused, naming the files (StaleFiles); with
\*            the sweep zero the attempt is finished.
\*   Widen    a refused finish whose remaining files' hunks are all fallout
\*            widens PATHS by those files by rule, with no coordinator step,
\*            and takes the card's own change through them.
\*   Land     E12. A file outside PATHS is accepted when every hunk in it is
\*            fallout; a non-fallout hunk outside PATHS refuses the landing.
\*   Read     a reader's pass is only ever given on a sweep of zero. A finding
\*            of "follow-up only: outside PATHS" is not a pass.
\*
\* The change is one rename (Renames, OldName -> NewName) and a set of removed
\* names (Removes), and each file's hunks are classed fallout or other. The
\* worker edits files in PATHS freely and files outside PATHS with hunks of
\* either class; a pure edit maps the old names and drops the removed ones.
\*
\* Broken: "none" is the design.
\*   "nosweep"       Finish has no sweep: an attempt finishes with stale
\*                   references, and Land lands it (LandedHasNoStaleRefs).
\*   "anyhunk"       Land accepts any hunk outside PATHS, fallout or not
\*                   (NonFalloutOutsidePathsNeverLands).
\*   "readerpasses"  Read passes with "follow-up only: outside PATHS" while
\*                   the sweep is not zero (ReaderNeverPassesStale).
\*
\* What is not modelled: the contents of a hunk (a line is one reference to a
\* name), the repository's directories and globs (a PATHS entry is a file
\* here), the lander's other checks (E4, the repairs, the tree gate), and the
\* worker's own runs. The model is cited by tla/CASES.tsv group fallout and by
\* the v19-fallout code cards (tla/COVERAGE.tsv).
EXTENDS FiniteSets

CONSTANTS Files, Names, OldName, NewName, Removes,
          FalloutHunks, OtherHunks, InPaths, StaleAt, Broken

\* the card's change: one rename, and the names it removes
Renames == {<<OldName, NewName>>}
OldNames == {OldName}
NewNames == {NewName}
StaleNames == OldNames \cup Removes

VARIABLES refs, hunks, paths, state, readVerdict
vars == <<refs, hunks, paths, state, readVerdict>>

\* the names a file mentions, and the file's hunks by class

\* every file that still mentions an old or removed name
StaleFiles == {f \in Files : refs[f] \cap StaleNames # {}}
SweepZero == StaleFiles = {}

\* the file's references after the card's own change alone: every old name
\* becomes its new name, every removed name is dropped
PureRefs(f) == (refs[f] \ StaleNames) \cup
               {r[2] : r \in {s \in Renames : s[1] \in refs[f]}}

\* every hunk in the file follows the card's own change
FalloutOnly(f) == "other" \notin hunks[f]

TypeOK ==
  /\ refs \in [Files -> SUBSET Names]
  /\ hunks \in [Files -> SUBSET {"fallout", "other"}]
  /\ paths \subseteq Files
  /\ state \in {"working", "finished", "refused", "landed"}
  /\ readVerdict \in {"none", "pass", "hold"}

Init ==
  /\ refs = [f \in Files |-> IF f \in StaleAt THEN StaleNames ELSE {}]
  /\ hunks = [f \in Files |->
       (IF f \in FalloutHunks THEN {"fallout"} ELSE {}) \cup
       (IF f \in OtherHunks THEN {"other"} ELSE {})]
  /\ paths = InPaths
  /\ state = "working"
  /\ readVerdict = "none"

\* the worker takes the card's own change through a file: a fallout hunk
EditPure(f) ==
  /\ state = "working"
  /\ refs[f] # PureRefs(f)
  /\ refs' = [refs EXCEPT ![f] = PureRefs(f)]
  /\ hunks' = [hunks EXCEPT ![f] = hunks[f] \cup {"fallout"}]
  /\ UNCHANGED <<paths, state, readVerdict>>

\* the worker changes a file whose diff carries a non-fallout hunk too
EditOther(f) ==
  /\ state = "working"
  /\ f \in OtherHunks
  /\ "side" \notin refs[f]
  /\ refs' = [refs EXCEPT ![f] = (refs[f] \ StaleNames) \cup {"side"}]
  /\ hunks' = [hunks EXCEPT ![f] = hunks[f] \cup {"other"}]
  /\ UNCHANGED <<paths, state, readVerdict>>

\* the sweep gate: refused while a file mentions an old or removed name
Finish ==
  /\ state = "working"
  /\ IF Broken = "nosweep" \/ SweepZero
       THEN state' = "finished"
       ELSE state' = "refused"
  /\ UNCHANGED <<refs, hunks, paths, readVerdict>>

\* a refused finish whose remaining hunks are all fallout is widened by rule:
\* the files join PATHS and the card's own change goes through them
Widen ==
  /\ state = "refused"
  /\ StaleFiles # {}
  /\ StaleFiles \subseteq Files \ paths
  /\ \A f \in StaleFiles : FalloutOnly(f)
  /\ paths' = paths \cup StaleFiles
  /\ refs' = [f \in Files |-> IF f \in StaleFiles THEN PureRefs(f) ELSE refs[f]]
  /\ hunks' = [f \in Files |->
       IF f \in StaleFiles THEN hunks[f] \cup {"fallout"} ELSE hunks[f]]
  /\ state' = "working"
  /\ UNCHANGED readVerdict

\* E12: a file outside PATHS lands only when every hunk in it is fallout
Land ==
  /\ state = "finished"
  /\ IF Broken = "anyhunk" \/ \A f \in Files \ paths : FalloutOnly(f)
       THEN state' = "landed"
       ELSE state' = "refused"
  /\ UNCHANGED <<refs, hunks, paths, readVerdict>>

\* a reader's verdict: a pass only on a sweep of zero (readerpasses: a pass
\* with "follow-up only: outside PATHS" while stale files remain outside)
Read ==
  /\ state # "landed"
  /\ readVerdict = "none"
  /\ IF SweepZero \/
        (Broken = "readerpasses" /\
         \A f \in StaleFiles : f \notin paths)
       THEN readVerdict' = "pass"
       ELSE readVerdict' = "hold"
  /\ UNCHANGED <<refs, hunks, paths, state>>

Next ==
  \/ (\E f \in Files : EditPure(f))
  \/ (\E f \in Files : EditOther(f))
  \/ Finish
  \/ Widen
  \/ Land
  \/ Read

Spec == Init /\ [][Next]_vars
FairSpec == Spec /\ WF_vars(Finish) /\ WF_vars(Widen) /\ WF_vars(Land)

\* ---- the rules ----

\* landed => no file mentions an old or removed name
LandedHasNoStaleRefs == state = "landed" => SweepZero

\* a non-fallout hunk outside PATHS never lands
NonFalloutOutsidePathsNeverLands ==
  state = "landed" => \A f \in Files \ paths : FalloutOnly(f)

\* a reader passes only when the sweep is zero
ReaderNeverPassesStale == readVerdict = "pass" => SweepZero

\* a card whose fallout is all pure lands, under fairness, with no
\* coordinator action: Finish refuses, Widen widens by rule, Finish accepts,
\* Land lands
PureFalloutLands ==
  (\A f \in Files : FalloutOnly(f)) ~> (state = "landed")

=============================================================================
