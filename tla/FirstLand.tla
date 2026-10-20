------------------------------ MODULE FirstLand ------------------------------
\* The first-land rules (docs/SPEC-SPRINT.md sections 5 and 7): a card lands
\* first time when the five rework causes are made mechanical.
\*
\*   1. The base did not move under the card. A card finishes only on the head
\*      it merged (head = base), and a base move never spends an attempt.
\*   2. No release-only green. The finish carries the stream command's receipt
\*      and a Debug build; a Release receipt is no finish (CI builds Debug).
\*   3. A proven plant. The red the worker proves is at its own line; a hang,
\*      a timeout and an unrelated red are no proof.
\*   4. No silent outside file. Every file the head touches outside its PATHS
\*      is named by its PATHS-PROPOSED.
\*   5. A shared registry hunk is flagged. The add lint flags two open cards
\*      that edit one registry hunk, so a serial conflict is seen before the
\*      lander meets it.
\*
\* The model's control (MCFirstLand.cfg, Broken = "none") checks that the five
\* rules hold and that a correctly worked card lands. Each MCFirstLandBroken*
\* configuration is one reversed witness: its Broken word removes one rule and
\* violates exactly the invariant below that names it.
\*
\* The actions map to the staged code (BASE: 83dfad6ae739):
\*   Finish -> cmd/nova-sprint/read_missing.go finishStep and
\*             internal/sprint/steps_work.go finishPlan
\*   Land   -> cmd/nova-sprint/landpass.go and internal/sprint/redo.go
\*             landRefused
\*   Add    -> internal/sprint/cardcheck.go sameFile
\*
\* Broken:
\*   "none"          the design.
\*   "stalebase"     Finish skips head = base (ReviewHeadHoldsBaseAtFinish).
\*   "basemovespends" Land counts a base clash as an attempt
\*                   (AttemptsAreOwnReds).
\*   "releasegreen"  Finish takes a Release receipt (NoReleaseOnlyGreen).
\*   "anyred"        Finish and Read take a hang, timeout or unrelated red
\*                   (PlantProvenAtItsLine).
\*   "silentfile"    Finish skips the PATHS diff (NoSilentOutsideFile).
\*   "nohunklint"    Add never flags two open cards' shared hunk
\*                   (SharedHunkFlagged).
\*****************************************************************************
EXTENDS Naturals, FiniteSets

CONSTANTS
    Cards,          \* the cards of one stream
    MaxBase,        \* the base tip moves at most this often (0..MaxBase)
    MaxAttempts,    \* the attempts a card may spend on its own red
    PathsFiles,     \* the files a card's PATHS names
    OutsideFiles,   \* files outside PATHS that a head may touch
    Hunks,          \* registry hunks of one shared file
    Broken          \* "none" or a reversed witness, below

ASSUME Cards # {}
ASSUME Broken \in
    {"none", "stalebase", "basemovespends", "releasegreen", "anyred",
     "silentfile", "nohunklint"}

BaseTips == 0..MaxBase
States == {"working", "review", "landed"}
Files == PathsFiles \cup OutsideFiles
Receipts == [cmd : {"stream", "other", "none"}, build : {"Debug", "Release", "none"}]
Plants == {"own", "unrelated", "hang", "timeout", "none"}
HunkKinds == Hunks \cup {"none"}

VARIABLES
    base,       \* the base tip, raised by BaseMoves
    head,       \* card -> the base tip its head contains
    finBase,    \* card -> the tip it finished on
    state,      \* card -> "working" | "review" | "landed"
    attempts,   \* card -> attempts spent on the card's own red
    ownReds,    \* card -> the refusals that were the card's own
    receipt,    \* card -> [cmd: stream|other|none, build: Debug|Release|none]
    plant,      \* card -> own|unrelated|hang|timeout|none
    touched,    \* card -> the files its head changes
    proposed,   \* card -> its PATHS-PROPOSED
    hunk,       \* card -> the registry hunk its brief edits, or "none"
    flagged     \* the pairs of open cards the add lint flagged

vars == <<base, head, finBase, state, attempts, ownReds, receipt, plant,
          touched, proposed, hunk, flagged>>

TypeOK ==
    /\ base \in BaseTips
    /\ head \in [Cards -> BaseTips]
    /\ finBase \in [Cards -> BaseTips]
    /\ state \in [Cards -> States]
    /\ attempts \in [Cards -> 0..MaxAttempts]
    /\ ownReds \in [Cards -> 0..MaxAttempts]
    /\ receipt \in [Cards -> Receipts]
    /\ plant \in [Cards -> Plants]
    /\ touched \in [Cards -> SUBSET Files]
    /\ proposed \in [Cards -> SUBSET Files]
    /\ hunk \in [Cards -> HunkKinds]
    /\ flagged \in SUBSET (SUBSET Cards)

Init ==
    /\ base = 0
    /\ head = [c \in Cards |-> 0]
    /\ finBase = [c \in Cards |-> 0]
    /\ state = [c \in Cards |-> "working"]
    /\ attempts = [c \in Cards |-> 0]
    /\ ownReds = [c \in Cards |-> 0]
    /\ receipt = [c \in Cards |-> [cmd |-> "none", build |-> "none"]]
    /\ plant = [c \in Cards |-> "none"]
    /\ touched = [c \in Cards |-> {}]
    /\ proposed = [c \in Cards |-> {}]
    /\ hunk = [c \in Cards |-> "none"]
    /\ flagged = {}

---------------------------------------------------------------------------
\* The worker's actions on a working card.

\* The base tip moves.
BaseMoves ==
    /\ base < MaxBase
    /\ base' = base + 1
    /\ UNCHANGED <<head, finBase, state, attempts, ownReds, receipt, plant,
                    touched, proposed, hunk, flagged>>

\* The worker merges the base tip into the card's head.
Merge(c) ==
    /\ state[c] = "working"
    /\ head' = [head EXCEPT ![c] = base]
    /\ UNCHANGED <<base, finBase, state, attempts, ownReds, receipt, plant,
                    touched, proposed, hunk, flagged>>

\* The worker gates its tree and reports the receipt. The model takes the
\* stream command; the control and every witness but "releasegreen" gate in
\* Debug, and "releasegreen" gates in Release, so the release-only green is
\* reachable without carrying both builds in every instance. (The type admits
\* "other" and "none" as well.)
GateBuilds == IF Broken = "releasegreen" THEN {"Release"} ELSE {"Debug"}
Gate(c, cmd, build) ==
    /\ state[c] = "working"
    /\ cmd \in {"stream"}
    /\ build \in GateBuilds
    /\ receipt' = [receipt EXCEPT ![c] = [cmd |-> cmd, build |-> build]]
    /\ UNCHANGED <<base, head, finBase, state, attempts, ownReds, plant,
                    touched, proposed, hunk, flagged>>

\* The worker plants a red at a line.
Plant(c, kind) ==
    /\ state[c] = "working"
    /\ kind \in Plants
    /\ plant' = [plant EXCEPT ![c] = kind]
    /\ UNCHANGED <<base, head, finBase, state, attempts, ownReds, receipt,
                    touched, proposed, hunk, flagged>>

\* The head changes a file. The model touches outside files: a PATHS file is
\* free by the rule, so it changes no check.
Touch(c, f) ==
    /\ state[c] = "working"
    /\ f \in OutsideFiles
    /\ f \notin touched[c]
    /\ touched' = [touched EXCEPT ![c] = touched[c] \cup {f}]
    /\ UNCHANGED <<base, head, finBase, state, attempts, ownReds, receipt,
                    plant, proposed, hunk, flagged>>

\* The worker proposes a file in PATHS-PROPOSED. The model proposes outside
\* files: only those are owed by the outside-file rule.
Propose(c, f) ==
    /\ state[c] = "working"
    /\ f \in OutsideFiles
    /\ f \notin proposed[c]
    /\ proposed' = [proposed EXCEPT ![c] = proposed[c] \cup {f}]
    /\ UNCHANGED <<base, head, finBase, state, attempts, ownReds, receipt,
                    plant, touched, hunk, flagged>>

(* The five rules. *)
HeadOnBase(c) == head[c] = base
StreamDebug(c) == receipt[c] = [cmd |-> "stream", build |-> "Debug"]
StreamRelease(c) == receipt[c] = [cmd |-> "stream", build |-> "Release"]
PlantProven(c) == plant[c] = "own"
PlantSome(c) == plant[c] # "none"
OutsideProposed(c) == (touched[c] \ PathsFiles) \subseteq proposed[c]

\* The finish accepts a card only when the rules hold; each Broken word removes
\* the one rule its witness is about.
FinishGuard(c) ==
    CASE Broken = "stalebase" ->
            /\ StreamDebug(c)
            /\ PlantProven(c)
            /\ OutsideProposed(c)
      [] Broken = "releasegreen" ->
            /\ HeadOnBase(c)
            /\ StreamRelease(c)
            /\ PlantProven(c)
            /\ OutsideProposed(c)
      [] Broken = "anyred" ->
            /\ HeadOnBase(c)
            /\ StreamDebug(c)
            /\ PlantSome(c)
            /\ OutsideProposed(c)
      [] Broken = "silentfile" ->
            /\ HeadOnBase(c)
            /\ StreamDebug(c)
            /\ PlantProven(c)
      [] OTHER ->
            /\ HeadOnBase(c)
            /\ StreamDebug(c)
            /\ PlantProven(c)
            /\ OutsideProposed(c)

\* The finish: review on the base it finished on; every other case is refused
\* with no attempt spent.
Finish(c) ==
    /\ state[c] = "working"
    /\ FinishGuard(c)
    /\ state' = [state EXCEPT ![c] = "review"]
    /\ finBase' = [finBase EXCEPT ![c] = base]
    /\ UNCHANGED <<base, head, attempts, ownReds, receipt, plant, touched,
                    proposed, hunk, flagged>>

---------------------------------------------------------------------------
\* The reader re-runs the receipt and the plant; a broken one returns the card
\* to working. "anyred" lets any red plant pass.
ReceiptReads(c) == receipt[c] = [cmd |-> "stream", build |-> "Debug"]
ReadPlantOK(c) ==
    CASE Broken = "anyred" -> plant[c] # "none"
      [] OTHER -> plant[c] = "own"
ReadBroken(c) == ~(ReceiptReads(c) /\ ReadPlantOK(c))

Read(c) ==
    /\ state[c] = "review"
    /\ state' = IF ReadBroken(c)
                THEN [state EXCEPT ![c] = "working"]
                ELSE state
    /\ UNCHANGED <<base, head, finBase, attempts, ownReds, receipt, plant,
                    touched, proposed, hunk, flagged>>

---------------------------------------------------------------------------
\* The lander: a card at the tip it finished on lands; a card whose base moved
\* is reworked at the tip (a clash, no attempt) or, for its own red, at the price
\* of an attempt.
LandHere(c) ==
    /\ state[c] = "review"
    /\ base = finBase[c]
    /\ state' = [state EXCEPT ![c] = "landed"]
    /\ UNCHANGED <<base, head, finBase, attempts, ownReds, receipt, plant,
                    touched, proposed, hunk, flagged>>

Clash(c) ==
    /\ state[c] = "review"
    /\ base # finBase[c]
    /\ (Broken # "basemovespends" \/ attempts[c] < MaxAttempts)
    /\ state' = [state EXCEPT ![c] = "working"]
    /\ head' = [head EXCEPT ![c] = base]
    /\ finBase' = [finBase EXCEPT ![c] = base]
    /\ attempts' = IF Broken = "basemovespends"
                   THEN [attempts EXCEPT ![c] = attempts[c] + 1]
                   ELSE attempts
    /\ UNCHANGED <<base, ownReds, receipt, plant, touched, proposed, hunk,
                    flagged>>

OwnRed(c) ==
    /\ state[c] = "review"
    /\ base # finBase[c]
    /\ attempts[c] < MaxAttempts
    /\ state' = [state EXCEPT ![c] = "working"]
    /\ head' = [head EXCEPT ![c] = base]
    /\ finBase' = [finBase EXCEPT ![c] = base]
    /\ attempts' = [attempts EXCEPT ![c] = attempts[c] + 1]
    /\ ownReds' = [ownReds EXCEPT ![c] = ownReds[c] + 1]
    /\ UNCHANGED <<base, receipt, plant, touched, proposed, hunk, flagged>>

Land(c) == LandHere(c) \/ Clash(c) \/ OwnRed(c)

---------------------------------------------------------------------------
\* The add lint: a card's brief names a registry hunk; two open cards naming
\* one hunk are flagged. "nohunklint" never flags.
Open(c) == state[c] # "landed"

SameHunk(c, h) == { d \in Cards \ {c} : Open(d) /\ hunk[d] = h }
NewFlags(c, h) == { {c, d} : d \in SameHunk(c, h) }

Add(c, h) ==
    /\ Open(c)
    /\ hunk[c] = "none"
    /\ h \in Hunks
    /\ hunk' = [hunk EXCEPT ![c] = h]
    /\ flagged' = IF Broken = "nohunklint"
                  THEN flagged
                  ELSE flagged \cup NewFlags(c, h)
    /\ UNCHANGED <<base, head, finBase, state, attempts, ownReds, receipt,
                    plant, touched, proposed>>

---------------------------------------------------------------------------

Next ==
    \/ BaseMoves
    \/ \E c \in Cards : Merge(c)
    \/ \E c \in Cards, cmd \in {"stream"},
             build \in GateBuilds : Gate(c, cmd, build)
    \/ \E c \in Cards, kind \in Plants : Plant(c, kind)
    \/ \E c \in Cards, f \in Files : Touch(c, f)
    \/ \E c \in Cards, f \in OutsideFiles : Propose(c, f)
    \/ \E c \in Cards : Finish(c)
    \/ \E c \in Cards : Read(c)
    \/ \E c \in Cards : Land(c)
    \/ \E c \in Cards, h \in Hunks : Add(c, h)

Spec == Init /\ [][Next]_vars

MergeAny == \E c \in Cards : Merge(c)
GateAny == \E c \in Cards, cmd \in {"stream"},
                    build \in GateBuilds : Gate(c, cmd, build)
PlantAny == \E c \in Cards, kind \in Plants : Plant(c, kind)
FinishAny == \E c \in Cards : Finish(c)
ReadAny == \E c \in Cards : Read(c)
LandAny == \E c \in Cards : Land(c)

FairSpec ==
    /\ Spec
    /\ WF_vars(MergeAny)
    /\ WF_vars(GateAny)
    /\ WF_vars(PlantAny)
    /\ WF_vars(FinishAny)
    /\ WF_vars(ReadAny)
    /\ WF_vars(LandAny)

---------------------------------------------------------------------------
\* The rules.

\* A card in review is on the head it finished on.
ReviewHeadHoldsBaseAtFinish ==
    \A c \in Cards : state[c] = "review" => head[c] = finBase[c]

\* An attempt is spent only on the card's own red; a base move or a clash is free.
AttemptsAreOwnReds ==
    \A c \in Cards : attempts[c] = ownReds[c]

\* A review or landed card carries the stream command's receipt in Debug.
NoReleaseOnlyGreen ==
    \A c \in Cards :
        state[c] \in {"review", "landed"} =>
            receipt[c] = [cmd |-> "stream", build |-> "Debug"]

\* A review or landed card's plant is proven at its own line.
PlantProvenAtItsLine ==
    \A c \in Cards : state[c] \in {"review", "landed"} => plant[c] = "own"

\* A review or landed card names every outside file the head touches.
NoSilentOutsideFile ==
    \A c \in Cards :
        state[c] \in {"review", "landed"} =>
            (touched[c] \ PathsFiles) \subseteq proposed[c]

\* Two open cards editing one registry hunk are flagged.
SharedHunkFlagged ==
    \A a, b \in Cards :
        (a # b /\ Open(a) /\ Open(b) /\ hunk[a] = hunk[b] /\
         hunk[a] # "none") => {a, b} \in flagged

\* A card whose worker settles on the merged tip, a Debug stream receipt, its
\* own plant and every outside file proposed lands.
WorkerDone(c) ==
    /\ head[c] = base
    /\ receipt[c] = [cmd |-> "stream", build |-> "Debug"]
    /\ plant[c] = "own"
    /\ (touched[c] \ PathsFiles) \subseteq proposed[c]

Liveness ==
    \A c \in Cards : (<>[]WorkerDone(c)) => <>(state[c] = "landed")

=============================================================================
