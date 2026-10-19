---------------------------- MODULE LandLinear ----------------------------
EXTENDS Naturals, Sequences, FiniteSets, TLC

CONSTANT Cards, Broken
VARIABLES source, batch, base, cursor, phase, refused, initial

vars == <<source, batch, base, cursor, phase, refused, initial>>

Init == /\ source = Cards /\ batch = <<>> /\ base = <<>> /\ cursor = 0
        /\ phase = "idle" /\ refused = FALSE /\ initial = <<>>
Replay == /\ ~refused /\ cursor < Len(source)
          /\ batch' = Append(batch,
               [card |-> source[cursor + 1], parents |-> IF Broken = "mergepushed" THEN 2 ELSE 1])
          /\ cursor' = cursor + 1 /\ phase' = "replayed"
          /\ UNCHANGED <<source, base, refused, initial>>
Conflict == /\ Broken = "pushonrefusal" /\ ~refused /\ cursor > 0
            /\ refused' = TRUE /\ phase' = "idle"
            /\ base' = batch
            /\ UNCHANGED <<source, batch, cursor, initial>>
Check == /\ phase = "replayed" /\ cursor = Len(source)
         /\ phase' = "checked"
         /\ UNCHANGED <<source, batch, base, cursor, refused, initial>>
Push == /\ phase = "checked" /\ phase' = "pushed" /\ base' = batch
        /\ UNCHANGED <<source, batch, cursor, refused, initial>>
Crash == /\ phase = "pushed" /\ phase' = "idle"
         /\ base' = IF Broken = "twice" /\ Len(base) > 0
                     THEN Append(base, base[1]) ELSE base
         /\ UNCHANGED <<source, batch, cursor, refused, initial>>
Resume == /\ phase = "idle" /\ ~refused /\ cursor > 0 /\ cursor < Len(source)
          /\ phase' = "replayed"
          /\ UNCHANGED <<source, batch, base, cursor, refused, initial>>
Next == Replay \/ Conflict \/ Check \/ Push \/ Crash \/ Resume
Spec == Init /\ [][Next]_vars /\ WF_vars(Replay) /\ WF_vars(Check) /\ WF_vars(Push)

BaseStaysLinear == \A i \in 1..Len(base): base[i].parents = 1
EachLandedCardOnce == \A k \in 1..Len(Cards): Cardinality({i \in 1..Len(base): base[i].card = Cards[k]}) <= 1
NothingPushedOnRefusal == refused => base = initial
AllLand == <> (\A k \in 1..Len(Cards): \E i \in 1..Len(base): base[i].card = Cards[k])
=============================================================================
