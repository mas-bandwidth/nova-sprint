----------------------------- MODULE MCTags -----------------------------
\* The instances of Tags.tla (tla/CASES.tsv, group tags): three machines (one
\* exclusive on {windows}), four tags (tla is probed and initially unproven; fuzz is
\* not in the registry at Init), four cards (generic, windows, a probed-tag card, and
\* a card nobody can take until a Beat proves fuzz). "later" is the fourth card.
EXTENDS Tags

MCNeed ==
  [c \in Cards |->
    CASE c = "gen"    -> {}
      [] c = "win"    -> {"windows"}
      [] c = "probed" -> {"tla"}
      [] OTHER        -> {"fuzz"}]

MCRows == {{}, {"windows"}}

MCCapable ==
  [m \in Machines |->
    CASE m = "lin" -> {"tla"}
      [] m = "mac" -> {"fuzz"}
      [] OTHER    -> {}]

MCGates == [m \in Machines |-> IF m = "win" THEN {"windows"} ELSE {}]

MCRegistry0 == {"windows", "linux", "tla"}

MCDeclared0 ==
  [m \in Machines |->
    CASE m = "win" -> {"windows"}
      [] m = "lin" -> {"linux", "tla"}
      [] OTHER    -> {"linux"}]

\* the registrycompiled witness: mac's row names fuzz before the registry has it, so
\* the card is eligible but the fixed registry cannot place it until RegistryAdd
MCDeclared0New ==
  [m \in Machines |->
    CASE m = "win" -> {"windows"}
      [] m = "lin" -> {"linux", "tla"}
      [] OTHER    -> {"linux", "fuzz"}]

=============================================================================
