---------------------------- MODULE SettingsWarn ----------------------------
\* The design settings' warnings (internal/sprint/settings_warn.go, TickWarnings; the
\* owner, 2026-10-11: "There must be an alert, every time cards are added to "review" set,
\* if read cards is off, and you get it via push as a warning." "These must be push
\* notifications."), written beside the code. A set that leaves a design setting off writes
\* a stored note to the coordinator at once, and its record (settings.go, setWarning); the
\* tick's warnings part writes one for every card that enters review while read cards are
\* off (and for an off it finds unsaid); the hook delivers every stored note to the coordinator as a push
\* (SeatHook.tla, Deliver: a note to the coordinator is delivered like a judgment). This
\* module is the warning source, the epoch a clear advances, and the delivery.
\*
\* THE STATE.
\*   off        a design setting is off its working value (the work table's property)
\*   epoch      the sprint's epoch: a clear starts the next, every record empty
\*   srec       the work table's record that the settings off were said
\*              (PropSettingsWarned not none); a set off writes it with its note, a set
\*              back on clears it
\*   inrev      a card's place: 0 not in review, a in review at attempt a
\*   att        a card's attempt
\*   ent        a card's entries into review so far (FieldReviewEntered: the revision it
\*              entered from, a new one each entry)
\*   rrec       the cards in review the tick warned of (PropReviewWarned),
\*              <<card, attempt, entry>>
\*   notes      the stored notes: [e |-> epoch, k |-> kind, c |-> card, a |-> attempt];
\*              a settings note carries the set's count in a, each set its own note
\*   hooked     a hook is attached
\*   delivered  the notes pushed over a hook
\*   owedS, owedR, ticked   ghosts: an off the tick has not said yet; the cards that entered
\*              review while read cards were off and the tick has not said yet; the last
\*              step was a tick
\*
\* THE RULES. SetWarned: no off is owed after a set, the set itself wrote the note (an
\* off -> on -> off between two ticks is two notes, a stopped machine's set included).
\* OffWarned: after every tick nothing is owed, every card into review while off is a
\* stored note (a clear's next epoch included, since its records start empty).
\* EveryNoteDelivered: every stored note is pushed over a hook once one is attached (the
\* notes are stored, so a note written with no hook attached is pushed by the next hook).
\* Broken: "none"; "noreset" is the set writing no note and never clearing srec (the tick
\* alone said it, from its record): the second off is silent; "noepoch" carries the records over a clear: a card id in review in the
\* next epoch is silent; "noentry" keys the record by card and attempt alone: a card that
\* leaves review and comes back at the same attempt between two ticks is silent (TLC found
\* it in the first design, 2026-10-11; FieldReviewEntered is the fix).
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, MaxEpoch, MaxAttempt, MaxSets, MaxEnter, Broken

VARIABLES off, epoch, srec, inrev, att, ent, rrec, notes, hooked, delivered, owedS, owedR, ticked, sets

vars == <<off, epoch, srec, inrev, att, ent, rrec, notes, hooked, delivered, owedS, owedR, ticked, sets>>

\* a card's key in the review record: card, attempt and entry ("noentry": no entry)
Key(c, a, e) == <<c, a, IF Broken = "noentry" THEN 0 ELSE e>>
Keys == Cards \X (1..MaxAttempt) \X (0..MaxEnter)

Kinds == {"settings", "review"}
MaxN == IF MaxAttempt > MaxSets THEN MaxAttempt ELSE MaxSets
NoteSpace == [e : 0..MaxEpoch, k : Kinds, c : Cards \cup {"-"}, a : 0..MaxN]

TypeOK ==
    /\ off \in BOOLEAN /\ srec \in BOOLEAN /\ hooked \in BOOLEAN /\ ticked \in BOOLEAN
    /\ epoch \in 0..MaxEpoch
    /\ inrev \in [Cards -> 0..MaxAttempt]
    /\ att \in [Cards -> 1..MaxAttempt]
    /\ ent \in [Cards -> 0..MaxEnter]
    /\ rrec \subseteq Keys
    /\ notes \subseteq NoteSpace /\ delivered \subseteq notes
    /\ owedS \in BOOLEAN /\ owedR \subseteq Keys
    /\ sets \in 0..MaxSets

Init ==
    /\ off = FALSE /\ epoch = 0 /\ srec = FALSE
    /\ inrev = [c \in Cards |-> 0] /\ att = [c \in Cards |-> 1] /\ ent = [c \in Cards |-> 0]
    /\ rrec = {} /\ notes = {} /\ hooked = FALSE /\ delivered = {}
    /\ owedS = FALSE /\ owedR = {} /\ ticked = FALSE /\ sets = 0

\* set turns a design setting off: the set writes the note and its record (correct code)
SetOff ==
    /\ ~off /\ sets < MaxSets
    /\ off' = TRUE /\ sets' = sets + 1
    /\ IF Broken = "noreset"
       THEN /\ srec' = srec /\ owedS' = TRUE /\ notes' = notes
       ELSE /\ srec' = TRUE /\ owedS' = FALSE
            /\ notes' = notes \cup {[e |-> epoch, k |-> "settings", c |-> "-", a |-> sets + 1]}
    /\ ticked' = FALSE
    /\ UNCHANGED <<epoch, inrev, att, ent, rrec, hooked, delivered, owedR>>

\* set puts it back at its working value: nothing is owed any more (read cards on deals
\* the reads of the cards in review)
SetOn ==
    /\ off /\ sets < MaxSets
    /\ off' = FALSE /\ sets' = sets + 1
    /\ srec' = IF Broken = "noreset" THEN srec ELSE FALSE
    /\ owedS' = FALSE /\ owedR' = {} /\ ticked' = FALSE
    /\ UNCHANGED <<epoch, inrev, att, ent, rrec, notes, hooked, delivered>>

\* a card's work comes back: it enters review at its attempt
Enter(c) ==
    /\ inrev[c] = 0 /\ ent[c] < MaxEnter
    /\ inrev' = [inrev EXCEPT ![c] = att[c]]
    /\ ent' = [ent EXCEPT ![c] = ent[c] + 1]
    /\ owedR' = IF off THEN owedR \cup {<<c, att[c], ent[c] + 1>>} ELSE owedR
    /\ ticked' = FALSE
    /\ UNCHANGED <<off, epoch, srec, att, rrec, notes, hooked, delivered, owedS, sets>>

\* it leaves review, at the same attempt (merged, refused, back) or the next (reworked)
Leave(c, n) ==
    /\ inrev[c] # 0
    /\ n \in {att[c], att[c] + 1} /\ n <= MaxAttempt
    /\ inrev' = [inrev EXCEPT ![c] = 0]
    /\ att' = [att EXCEPT ![c] = n]
    /\ owedR' = {x \in owedR : x[1] # c}
    /\ ticked' = FALSE
    /\ UNCHANGED <<off, epoch, srec, ent, rrec, notes, hooked, delivered, owedS, sets>>

\* a clear: the next epoch, every setting at its working value, every record empty
Clear ==
    /\ epoch < MaxEpoch
    /\ epoch' = epoch + 1
    /\ off' = FALSE
    /\ srec' = IF Broken = "noepoch" THEN srec ELSE FALSE
    /\ rrec' = IF Broken = "noepoch" THEN rrec ELSE {}
    /\ inrev' = [c \in Cards |-> 0] /\ att' = [c \in Cards |-> 1] /\ ent' = [c \in Cards |-> 0]
    /\ owedS' = FALSE /\ owedR' = {} /\ ticked' = FALSE
    /\ UNCHANGED <<notes, hooked, delivered, sets>>

\* the cards in review now, by their record key
InNow == {Key(c, inrev[c], ent[c]) : c \in {d \in Cards : inrev[d] # 0}}
\* the true entries in review now, whatever the record keys them by
TrueNow == {<<c, inrev[c], ent[c]>> : c \in {d \in Cards : inrev[d] # 0}}

\* the tick's warnings part (TickWarnings)
Tick ==
    LET newS == off /\ ~srec
        newR == IF off THEN InNow \ rrec ELSE {}
        sNote == IF newS THEN {[e |-> epoch, k |-> "settings", c |-> "-", a |-> sets]} ELSE {}
        rNotes == {[e |-> epoch, k |-> "review", c |-> x[1], a |-> x[2]] : x \in newR}
        saidR == {x \in TrueNow : Key(x[1], x[2], x[3]) \in newR}
    IN /\ notes' = notes \cup sNote \cup rNotes
       /\ srec' = IF Broken = "noreset" THEN (srec \/ off) ELSE off
       /\ rrec' = IF ~off THEN {} ELSE InNow
       /\ owedS' = (owedS /\ ~newS)
       /\ owedR' = owedR \ saidR
       /\ ticked' = TRUE
       /\ UNCHANGED <<off, epoch, inrev, att, ent, hooked, delivered, sets>>

Attach == ~hooked /\ hooked' = TRUE /\ UNCHANGED <<off, epoch, srec, inrev, att, ent, rrec, notes, delivered, owedS, owedR, ticked, sets>>
Detach == hooked /\ hooked' = FALSE /\ UNCHANGED <<off, epoch, srec, inrev, att, ent, rrec, notes, delivered, owedS, owedR, ticked, sets>>
\* SeatHook.tla Deliver: a stored note to the coordinator not yet pushed is pushed
Deliver(n) == hooked /\ n \in notes \ delivered /\ delivered' = delivered \cup {n}
    /\ UNCHANGED <<off, epoch, srec, inrev, att, ent, rrec, notes, hooked, owedS, owedR, ticked, sets>>

Next ==
    \/ SetOff \/ SetOn \/ Clear \/ Tick \/ Attach \/ Detach
    \/ \E c \in Cards : Enter(c) \/ \E n \in 1..MaxAttempt : Leave(c, n)
    \/ \E n \in NoteSpace : Deliver(n)

Spec == Init /\ [][Next]_vars

\* the safety cases: no hook attaches, so no delivery (SetWarned and OffWarned are of the
\* warning source alone; the delivery is LiveSpec's, on a smaller instance)
SafeSpec == Init /\ [][Next /\ ~hooked']_vars

\* the hook stays attached once attached, and the tick and the delivery run: the liveness case
Fair == WF_vars(Tick) /\ WF_vars(Attach) /\ \A n \in NoteSpace : WF_vars(Deliver(n))
LiveSpec == Init /\ [][Next /\ ~Detach]_vars /\ Fair

\* a set that leaves a setting off has said so: nothing off is owed after a set
SetWarned == ~owedS

\* after every tick, nothing is owed: every off and every card into review while off is a note
OffWarned == ticked => (~owedS /\ owedR = {})

\* every stored note is pushed over a hook
EveryNoteDelivered == \A n \in NoteSpace : [](n \in notes => <>(n \in delivered))
=============================================================================
