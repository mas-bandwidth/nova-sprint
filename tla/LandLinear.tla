---------------------------- MODULE LandLinear ----------------------------
EXTENDS Naturals, Sequences, FiniteSets, TLC

CONSTANT Cards, Broken
VARIABLES source, base, cursor, phase, refused, before

vars == <<source, base, cursor, phase, refused, before>>

Init == /\ source = Cards /\ base = <<>> /\ cursor = 0
        /\ phase = "idle" /\ refused = FALSE /\ before = <<>>
Replay == /\ ~refused /\ cursor < Len(source)
          /\ base' = Append(base, [card |-> source[cursor + 1], parents |-> IF Broken = "mergepushed" THEN 2 ELSE 1])
          /\ cursor' = cursor + 1 /\ phase' = "replayed"
          /\ UNCHANGED <<source, refused, before>>
Conflict == /\ ~refused /\ refused' = TRUE /\ before' = base
            /\ base' = IF Broken = "pushonrefusal" /\ Len(source) > 0
                        THEN Append(base, [card |-> source[1], parents |-> 1]) ELSE base
            /\ phase' = "idle" /\ UNCHANGED <<source, cursor>>
Check == /\ phase = "replayed" /\ phase' = "checked"
         /\ UNCHANGED <<source, base, cursor, refused, before>>
Push == /\ phase = "checked" /\ phase' = "pushed"
        /\ UNCHANGED <<source, base, cursor, refused, before>>
Crash == /\ phase = "pushed" /\ phase' = "idle"
         /\ UNCHANGED <<source, base, cursor, refused, before>>
Resume == /\ phase = "idle" /\ ~refused /\ phase' = "replayed"
          /\ base' = IF Broken = "twice" /\ Len(base) > 0 THEN Append(base, base[1]) ELSE base
          /\ UNCHANGED <<source, cursor, refused, before>>
Next == Replay \/ Conflict \/ Check \/ Push \/ Crash \/ Resume
Spec == Init /\ [][Next]_vars /\ WF_vars(Replay)

BaseStaysLinear == \A i \in 1..Len(base): base[i].parents = 1
EachLandedCardOnce == \A c \in Cards: Cardinality({i \in 1..Len(base): base[i].card = c}) <= 1
NothingPushedOnRefusal == refused => base = before
AllLand == <> (\A c \in Cards: \E i \in 1..Len(base): base[i].card = c)
=============================================================================
