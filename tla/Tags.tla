----------------------------- MODULE Tags -----------------------------
\* The tag rule of v1.9 (docs/SPEC-SPRINT.md, the gate-host rule; v19-tags-model).
\*
\* A TAG is a word in nova-config's tag registry. A machine's tags have exactly two
\* sources: the tags its nova-config machine row declares (declared, written only by
\* ConfigApply) and the tags its member proves on its heartbeat (beat, written only by
\* Beat(m)). A probed tag counts only when the beat proves it (ProbedWins); a declared
\* probed tag whose probe failed opens probeJudged[m,t] (ProbeDisagrees), a coordinator
\* judgment. The sprint server keeps no copy of either source: Eligible is an operator
\* over declared, beat and registry. (The servercopy witness is the one broken mode
\* that carries a copy, so that its violation is the copy's.)
\*
\* The registry is the set of names a build knows; RegistryAdd(t) is a new config row,
\* with or without a probe. A card names its required tags in Need[c] (TAGS:, the
\* GATE-NEEDS alias); today's exclusive tags are exclusive[m] (gates_only). One rule
\* serves every path:
\*
\*   EffectiveTrue(m) == beat[m] \cup {t \in declared[m] : t \notin Probed}
\*   Eligible(c,m)   == up[m] /\ Need[c] \subseteq Effective(m)
\*                      /\ (exclusive[m] = {} \/ exclusive[m] \cap Need[c] # {})
\*
\* The ten Paths are the code's ten places that put an attempt or a read on a machine:
\* deal (steps_work.go GateHostPool), route (route.go dealPool), next
\* (steps_review.go), redo (redo.go), downredeal (steps_work.go downPlan), level
\* (steps_work.go level), rebalance (rebalance.go), friend (friend_deal.go), take and
\* read (read_cards.go). Place(p,c,m) holds Eligible for each; Read is the read's own
\* step (a read runs the card's gate). Unmet opens one judgment with a deadline and
\* waits; Resume deals the card when a machine gains the tag; Deadline keeps the one
\* judgment, never a second and never a silent wait.
\*
\* Broken = "none" is the code. Each reversed witness is its own cfg:
\*   movepath (MovePath holds Admits only), declaredwins (Effective is declared alone),
\*   servercopy (Eligible reads a copy written only by Sync), silentwait (Unmet waits
\*   with no judgment), noresume (Resume disabled), exclusivereads (Read ignores
\*   exclusive), registrycompiled (RegistryAdd disabled, so a needed new tag never
\*   enters the registry and the card never deals).
\*
\* The instance constants beyond the rule bound what a TLC run explores: Rows are the
\* machine rows ConfigApply may write, Capable[m] the tags m's installed tools can
\* prove, Gates[m] the machine row's exclusive words, Registry0 the registry at Init
\* and Declared0 the machine rows at Init. Moved is a ghost for the cover NeverMoved.
\* The pass instance runs the deal path alone (MCTags.cfg Paths) and leaves
\* ConfigApply and Sync to the servercopy witness, so its state space stays inside
\* the bench budget; MCTagsMove.cfg runs every path. A needed tag must also be in
\* the registry to be placed (the registrycompiled witness). The model abstracts the
\* probe shapes and the nova-config live read to the growing registry and the beat.
\*
\* This module is the model every v19-tags card cites. tla/GatesOnly.tla is its
\* predecessor; GatesOnly's gateEligible is Eligible here with exclusive and facts
\* folded into one rule.

EXTENDS Naturals, FiniteSets

CONSTANTS Machines, Tags, Probed, Cards, Need, Paths
CONSTANTS Broken, MovePath
CONSTANTS Rows, Capable, Gates, Registry0, Declared0

VARIABLES declared, exclusive, beat, up, registry, placed, read, judged, probeJudged,
          moved, copy
vars == <<declared, exclusive, beat, up, registry, placed, read, judged, probeJudged,
          moved, copy>>

EffectiveWith(D, P) == P \cup {t \in D : t \notin Probed}

\* the truth the deal must never break: the beat's proof, the declared tags that need no
\* probe, and a declared probed tag only when the beat proves it
EffectiveTrue(m) == EffectiveWith(declared[m], beat[m])

\* the rule the model runs; only a reversed witness changes it
Effective(m) ==
  CASE Broken = "declaredwins" -> declared[m]
    [] Broken = "servercopy" -> EffectiveWith(copy[m], beat[m])
    [] OTHER -> EffectiveTrue(m)

\* today's gates_only: an exclusive member takes only cards naming one of its words
Admits(c, m) == exclusive[m] = {} \/ exclusive[m] \cap Need[c] # {}

Eligible(c, m) ==
  /\ up[m]
  /\ Need[c] \subseteq Effective(m)
  /\ Admits(c, m)

ReadyOrWaiting(c) == placed[c] \in {"ready", "waiting"}

Init ==
  /\ declared = Declared0
  /\ exclusive = [m \in Machines |-> Declared0[m] \cap Gates[m]]
  /\ beat = [m \in Machines |-> {}]
  /\ up = [m \in Machines |-> TRUE]
  /\ registry = Registry0
  /\ placed = [c \in Cards |-> "ready"]
  /\ read = [c \in Cards |-> "none"]
  /\ judged = [c \in Cards |-> {}]
  /\ probeJudged = {}
  /\ moved = FALSE
  /\ copy = Declared0

\* nothing placed or read on m is left without its tags by a config change
CanApply(m, S) ==
  /\ S \in Rows
  /\ S \subseteq registry
  /\ \A c \in Cards :
       (placed[c] = m \/ read[c] = m) =>
         /\ Need[c] \subseteq EffectiveWith(S, beat[m])
         /\ (S \cap Gates[m] = {} \/ (S \cap Gates[m]) \cap Need[c] # {})

ConfigApply(m, S) ==
  /\ Broken = "servercopy"
  /\ CanApply(m, S)
  /\ declared' = [declared EXCEPT ![m] = S]
  /\ exclusive' = [exclusive EXCEPT ![m] = S \cap Gates[m]]
  /\ UNCHANGED <<beat, up, registry, placed, read, judged, probeJudged, moved, copy>>

\* a new tag is a config row: the registry grows, with or without a probe
RegistryAdd(t) ==
  /\ Broken # "registrycompiled"
  /\ t \in Tags \ registry
  /\ registry' = registry \cup {t}
  /\ UNCHANGED <<declared, exclusive, beat, up, placed, read, judged, probeJudged,
                 moved, copy>>

\* a tool installed or removed: the beat proves the tags its tools can prove, and
\* nothing placed or read on m loses its tags
CanBeat(m, P) ==
  /\ P \subseteq Capable[m] \cap registry
  /\ \A c \in Cards :
       (placed[c] = m \/ read[c] = m) =>
         /\ Need[c] \subseteq EffectiveWith(declared[m], P)
         /\ Admits(c, m)

Beat(m, P) ==
  /\ CanBeat(m, P)
  /\ beat' = [beat EXCEPT ![m] = P]
  /\ UNCHANGED <<declared, exclusive, up, registry, placed, read, judged, probeJudged,
                 moved, copy>>

Down(m) ==
  /\ up[m]
  /\ up' = [up EXCEPT ![m] = FALSE]
  /\ UNCHANGED <<declared, exclusive, beat, registry, placed, read, judged, probeJudged,
                 moved, copy>>

Up(m) ==
  /\ ~up[m]
  /\ up' = [up EXCEPT ![m] = TRUE]
  /\ UNCHANGED <<declared, exclusive, beat, registry, placed, read, judged, probeJudged,
                 moved, copy>>

\* the server's copy, written only by Sync (the servercopy witness)
Sync ==
  /\ Broken = "servercopy"
  /\ copy # declared
  /\ copy' = declared
  /\ UNCHANGED <<declared, exclusive, beat, up, registry, placed, read, judged,
                 probeJudged, moved>>

\* a declared probed tag whose probe failed is one coordinator judgment
ProbeDisagrees(m, t) ==
  /\ t \in Probed
  /\ t \in declared[m]
  /\ t \notin beat[m]
  /\ <<m, t>> \notin probeJudged
  /\ probeJudged' = probeJudged \cup {<<m, t>>}
  /\ UNCHANGED <<declared, exclusive, beat, up, registry, placed, read, judged, moved,
                 copy>>

MovePaths == {"next", "redo", "downredeal", "level", "rebalance"}

\* Place is every path that puts an attempt on a machine. In the fixed model each
\* holds Eligible; only the movepath witness has the one chosen path hold Admits.
Place(p, c, m) ==
  /\ p \in Paths
  /\ p # "read"
  /\ m \in Machines
  /\ Need[c] \subseteq registry
  /\ IF p = "take"
       THEN placed[c] = m
       ELSE IF p \in MovePaths
              THEN placed[c] \in Machines
              ELSE placed[c] = "ready"
  /\ IF Broken = "movepath" /\ p = MovePath
       THEN Admits(c, m)
       ELSE Eligible(c, m)
  /\ placed' = [placed EXCEPT ![c] = m]
  /\ moved' = TRUE
  /\ UNCHANGED <<declared, exclusive, beat, up, registry, read, judged, probeJudged,
                 copy>>

\* a read runs the card's gate, and only where the card is not the worker
Read(c, m) ==
  /\ "read" \in Paths
  /\ placed[c] \in Machines
  /\ m # placed[c]
  /\ read[c] = "none"
  /\ Need[c] \subseteq registry
  /\ (IF Broken = "exclusivereads"
        THEN up[m] /\ Need[c] \subseteq EffectiveTrue(m)
        ELSE Eligible(c, m))
  /\ read' = [read EXCEPT ![c] = m]
  /\ UNCHANGED <<declared, exclusive, beat, up, registry, placed, judged, probeJudged,
                 moved, copy>>

\* a ready card no up machine can take: one judgment with a deadline, and the card waits
Unmet(c) ==
  /\ placed[c] = "ready"
  /\ judged[c] = {}
  /\ ~(\E m \in Machines : Eligible(c, m))
  /\ placed' = [placed EXCEPT ![c] = "waiting"]
  /\ judged' = IF Broken = "silentwait" THEN judged
               ELSE [judged EXCEPT ![c] = {"open"}]
  /\ UNCHANGED <<declared, exclusive, beat, up, registry, read, probeJudged, moved, copy>>

\* a waiting card some up machine is eligible for is dealt and its judgment closes
Resume(c) ==
  \E m \in Machines :
    /\ Broken # "noresume"
    /\ placed[c] = "waiting"
    /\ Need[c] \subseteq registry
    /\ Eligible(c, m)
    /\ placed' = [placed EXCEPT ![c] = m]
    /\ judged' = [judged EXCEPT ![c] = {}]
    /\ UNCHANGED <<declared, exclusive, beat, up, registry, read, probeJudged, moved,
                   copy>>

\* the deadline passes: the one judgment stays the one judgment
Deadline(c) ==
  /\ placed[c] = "waiting"
  /\ judged[c] = {"open"}
  /\ UNCHANGED vars

Next ==
  \/ \E m \in Machines, S \in Rows : ConfigApply(m, S)
  \/ \E t \in Tags : RegistryAdd(t)
  \/ \E m \in Machines, P \in SUBSET Tags : Beat(m, P)
  \/ \E m \in Machines : Down(m) \/ Up(m)
  \/ \E p \in Paths, c \in Cards, m \in Machines : Place(p, c, m)
  \/ \E c \in Cards, m \in Machines : Read(c, m)
  \/ \E c \in Cards : Unmet(c) \/ Resume(c) \/ Deadline(c)
  \/ \E m \in Machines, t \in Probed : ProbeDisagrees(m, t)
  \/ Sync

Spec == Init /\ [][Next]_vars
        /\ WF_vars(Sync)
        /\ \A cw \in Cards : WF_vars(\E mw \in Machines : Place("deal", cw, mw))
        /\ \A cr \in Cards : WF_vars(Resume(cr))

TypeOK ==
  /\ declared \in [Machines -> SUBSET Tags]
  /\ exclusive \in [Machines -> SUBSET Tags]
  /\ \A m \in Machines : exclusive[m] \subseteq declared[m]
  /\ beat \in [Machines -> SUBSET Tags]
  /\ up \in [Machines -> BOOLEAN]
  /\ registry \subseteq Tags
  /\ Probed \subseteq registry
  /\ placed \in [Cards -> Machines \cup {"ready", "waiting"}]
  /\ read \in [Cards -> Machines \cup {"none"}]
  /\ judged \in [Cards -> SUBSET {"open"}]
  /\ probeJudged \in SUBSET (Machines \times Probed)
  /\ moved \in BOOLEAN
  /\ copy \in [Machines -> SUBSET Tags]

\* a card reaches a machine only when every tag it names is effective there
NeverPlacedWithoutTags ==
  \A c \in Cards : placed[c] \in Machines => Need[c] \subseteq EffectiveTrue(placed[c])

\* and a read runs the same rule
ReadWithoutTags ==
  \A c \in Cards : read[c] \in Machines => Need[c] \subseteq EffectiveTrue(read[c])

\* a member exclusive on some words takes no card naming none of them, and no such read
ExclusiveHolds ==
  \A c \in Cards :
    /\ (placed[c] \in Machines => Admits(c, placed[c]))
    /\ (read[c] \in Machines => Admits(c, read[c]))

\* one judgment a card, never a second
OneJudgmentPerUnmetCard == \A c \in Cards : Cardinality(judged[c]) <= 1

\* a waiting card is never a silent wait
NoSilentWait == \A c \in Cards : placed[c] = "waiting" => judged[c] # {}

\* a declared probed tag whose probe failed is never effective
ProbedWins ==
  \A m \in Machines, t \in Probed :
    t \in declared[m] /\ t \notin beat[m] => t \notin Effective(m)

\* every card a machine can take is eventually dealt, under fair deal and resume
EventuallyDealt ==
  \A c \in Cards :
    [](ReadyOrWaiting(c) /\ (\E m \in Machines : Eligible(c, m)))
      ~> (placed[c] \in Machines)

\* the cover: a placement is reachable (expected violated in the Move config)
NeverMoved == ~moved

=============================================================================
