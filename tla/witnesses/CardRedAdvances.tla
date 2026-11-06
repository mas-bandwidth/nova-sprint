-------------------------- MODULE CardRedAdvances --------------------------
(* REVERSED WITNESS: a red CI result moves merging to landed. TLC must    *)
(* report RedNeverAdvances violated.                                      *)
(***************************************************************************)
(* Layer 2 of nova-sprint v2: the card machine                             *)
(* (docs/LAYER-2-CARD-MACHINE.md).                                         *)
(*                                                                         *)
(* Each action is one accepted event of the two tables of moves: the card  *)
(* table and the row table. Its guard is the design's from-state plus its  *)
(* extra conditions, and nothing else; an event that does not match is not *)
(* enabled (the Go code refuses it with a value, which changes nothing).   *)
(* The model is the reachable state space of accepted moves.               *)
(*                                                                         *)
(* Small instance: two cards (c2 needs c1), two rows (r1, r2), one stream  *)
(* with a held flag. A head is a small natural (0 is "no head yet"); a     *)
(* read, a landing or a red result carries a head, and is enabled only     *)
(* when it equals the card's head. Bounds that are the model's and not the *)
(* design's: attempt stops at MaxAttempt (Deal is not enabled past it),    *)
(* flaps stop at Limit + 1 (Gone is not enabled past it), scores are       *)
(* 0..MaxScore and widths 1..MaxWidth.                                     *)
(*                                                                         *)
(* Left out, because no guard reads them: branch, base_sha, reason, the    *)
(* by of Replace and the need named in the reason of NeedFailed. They add  *)
(* columns, not behaviour.                                                 *)
(*                                                                         *)
(* Width: Deal and DealRead write the holder, and layer 1 refuses a holder *)
(* write to a row that already holds as many cards as its width. So both   *)
(* are enabled only when the row's held count is below its width          *)
(* (HolderWriteFits); the guard is the write's refusal, stated here.       *)
(*                                                                         *)
(* last is a history variable: the event, its card and row, and every     *)
(* card as it was before the step. The step invariants below read it.     *)
(* Hold and Unhold are the environment: the stream's held flag is written  *)
(* elsewhere; this layer only reads it on Deal.                            *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  MaxHead,     \* heads are 1..MaxHead; 0 means no head yet
  MaxAttempt,  \* model bound: Deal is not enabled at this attempt
  Limit,       \* the flap limit policy passes to Flapped
  MaxScore,    \* scores are 0..MaxScore
  MaxWidth     \* widths are 1..MaxWidth

Cards      == {"c1", "c2"}
Rows       == {"r1", "r2"}
Needs      == [c \in Cards |-> IF c = "c2" THEN {"c1"} ELSE {}]
CardStates == {"waiting", "ready", "working", "review", "merging", "landed", "fail"}
Final      == {"landed", "fail"}
Rest       == {"waiting", "ready", "merging", "landed", "fail"}   \* holder none
RowStates  == {"up", "down", "blocked"}
None       == "none"
NoActor    == "-"
Heads      == 1..MaxHead
Scores     == 0..MaxScore
Widths     == 1..MaxWidth
MaxFlaps   == Limit + 1

CardEvents == {"Resolve", "Deal", "Finish", "DealRead", "ReadYes", "ReadFix", "Landed",
               "Red", "Release", "Cancel", "Replace", "NeedFailed", "Rescore"}
RowEvents  == {"Seen", "Gone", "Flapped", "Unblock", "Width"}
EnvEvents  == {"Init", "Hold", "Unhold"}

NewCard == [state |-> "waiting", holder |-> None, author |-> None,
            attempt |-> 0, head |-> 0, score |-> 0]
NewRow  == [state |-> "down", flaps |-> 0, width |-> 1]

VARIABLES
  cs,    \* card id -> [state, holder, author, attempt, head, score]
  rs,    \* row name -> [state, flaps, width]
  held,  \* the stream's held flag
  last   \* history: [ev, c, r, pre]: the last event and the cards before it

vars == <<cs, rs, held, last>>

Holders(r)         == {c \in Cards : cs[c].holder = r}
HolderWriteFits(r) == Cardinality(Holders(r)) < rs[r].width
NeedsLanded(c)     == \A n \in Needs[c] : cs[n].state = "landed"

Init ==
  /\ cs   = [c \in Cards |-> NewCard]
  /\ rs   = [r \in Rows |-> NewRow]
  /\ held = FALSE
  /\ last = [ev |-> "Init", c |-> NoActor, r |-> NoActor, pre |-> [c \in Cards |-> NewCard]]

(* One card event: card c becomes rec; nothing else moves.                 *)
Step(ev, c, r, rec) ==
  /\ cs'   = [cs EXCEPT ![c] = rec]
  /\ last' = [ev |-> ev, c |-> c, r |-> r, pre |-> cs]
  /\ UNCHANGED <<rs, held>>

(* One row event: row r becomes rec; no card moves.                        *)
RowStep(ev, r, rec) ==
  /\ rs'   = [rs EXCEPT ![r] = rec]
  /\ last' = [ev |-> ev, c |-> NoActor, r |-> r, pre |-> cs]
  /\ UNCHANGED <<cs, held>>

-----------------------------------------------------------------------------
(* The card table                                                          *)

Resolve(c) ==
  /\ cs[c].state = "waiting"
  /\ NeedsLanded(c)
  /\ Step("Resolve", c, NoActor, [cs[c] EXCEPT !.state = "ready"])

Deal(c, r) ==
  /\ cs[c].state = "ready"
  /\ rs[r].state = "up"
  /\ ~held
  /\ HolderWriteFits(r)
  /\ cs[c].attempt < MaxAttempt
  /\ Step("Deal", c, r, [cs[c] EXCEPT !.state = "working", !.holder = r, !.attempt = @ + 1])

Finish(c, r, h) ==
  /\ cs[c].state = "working"
  /\ cs[c].holder = r
  /\ Step("Finish", c, r, [cs[c] EXCEPT !.state = "review", !.holder = None,
                                        !.author = r, !.head = h])

DealRead(c, r) ==
  /\ cs[c].state = "review"
  /\ cs[c].holder = None
  /\ rs[r].state = "up"
  /\ r # cs[c].author
  /\ HolderWriteFits(r)
  /\ Step("DealRead", c, r, [cs[c] EXCEPT !.holder = r])

ReadYes(c, r, h) ==
  /\ cs[c].state = "review"
  /\ cs[c].holder = r
  /\ h = cs[c].head
  /\ Step("ReadYes", c, r, [cs[c] EXCEPT !.state = "merging", !.holder = None])

(* Score untouched: the record keeps it.                                   *)
ReadFix(c, r, h) ==
  /\ cs[c].state = "review"
  /\ cs[c].holder = r
  /\ h = cs[c].head
  /\ Step("ReadFix", c, r, [cs[c] EXCEPT !.state = "ready", !.holder = None])

Landed(c, h) ==
  /\ cs[c].state = "merging"
  /\ h = cs[c].head
  /\ Step("Landed", c, NoActor, [cs[c] EXCEPT !.state = "landed"])

Red(c, h) ==
  /\ cs[c].state = "merging"
  /\ h = cs[c].head
  /\ Step("Red", c, NoActor, [cs[c] EXCEPT !.state = "landed", !.holder = None])   \* FAULT: was "review"

(* Release carries the holder it takes back: the design's refusal "a       *)
(* release by a row that is not the holder". working -> ready; review with *)
(* a reader -> review with holder none. Attempt kept.                      *)
Release(c, r) ==
  /\ cs[c].state \in {"working", "review"}
  /\ cs[c].holder = r
  /\ Step("Release", c, r, [cs[c] EXCEPT !.state = IF @ = "working" THEN "ready" ELSE "review",
                                         !.holder = None])

Cancel(c) ==
  /\ cs[c].state \notin Final
  /\ Step("Cancel", c, NoActor, [cs[c] EXCEPT !.state = "fail", !.holder = None])

Replace(c, by) ==
  /\ cs[c].state \notin Final
  /\ Step("Replace", c, NoActor, [cs[c] EXCEPT !.state = "fail", !.holder = None])

(* The design's only condition: n is in the card's needs. It does not      *)
(* require n to have failed (that is layer 3's trigger).                   *)
NeedFailed(c, n) ==
  /\ cs[c].state \notin Final
  /\ n \in Needs[c]
  /\ Step("NeedFailed", c, NoActor, [cs[c] EXCEPT !.state = "fail", !.holder = None])

Rescore(c, s) ==
  /\ cs[c].state \notin Final
  /\ Step("Rescore", c, NoActor, [cs[c] EXCEPT !.score = s])

-----------------------------------------------------------------------------
(* The row table                                                           *)

Seen(r) ==
  /\ rs[r].state = "down"
  /\ RowStep("Seen", r, [rs[r] EXCEPT !.state = "up"])

Gone(r) ==
  /\ rs[r].state = "up"
  /\ rs[r].flaps < MaxFlaps
  /\ RowStep("Gone", r, [rs[r] EXCEPT !.state = "down", !.flaps = @ + 1])

Flapped(r) ==
  /\ rs[r].state \in {"up", "down"}
  /\ rs[r].flaps >= Limit
  /\ RowStep("Flapped", r, [rs[r] EXCEPT !.state = "blocked"])

Unblock(r) ==
  /\ rs[r].state = "blocked"
  /\ RowStep("Unblock", r, [rs[r] EXCEPT !.state = "down", !.flaps = 0])

(* Layer 1's put row refuses a width below the row's held count.           *)
Width(r, n) ==
  /\ n >= Cardinality(Holders(r))
  /\ RowStep("Width", r, [rs[r] EXCEPT !.width = n])

-----------------------------------------------------------------------------
(* The environment: the stream's held flag                                 *)

Hold ==
  /\ ~held
  /\ held' = TRUE
  /\ last' = [ev |-> "Hold", c |-> NoActor, r |-> NoActor, pre |-> cs]
  /\ UNCHANGED <<cs, rs>>

Unhold ==
  /\ held
  /\ held' = FALSE
  /\ last' = [ev |-> "Unhold", c |-> NoActor, r |-> NoActor, pre |-> cs]
  /\ UNCHANGED <<cs, rs>>

Next ==
  \/ \E c \in Cards :
       \/ Resolve(c) \/ Cancel(c)
       \/ \E h \in Heads : Landed(c, h) \/ Red(c, h)
       \/ \E r \in Rows :
            \/ Deal(c, r) \/ DealRead(c, r) \/ Release(c, r)
            \/ \E h \in Heads : Finish(c, r, h) \/ ReadYes(c, r, h) \/ ReadFix(c, r, h)
       \/ \E by \in Cards \ {c} : Replace(c, by)
       \/ \E n \in Cards : NeedFailed(c, n)
       \/ \E s \in Scores : Rescore(c, s)
  \/ \E r \in Rows :
       \/ Seen(r) \/ Gone(r) \/ Flapped(r) \/ Unblock(r)
       \/ \E n \in Widths : Width(r, n)
  \/ Hold \/ Unhold

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Invariants                                                              *)

CardRec == [state : CardStates, holder : Rows \cup {None}, author : Rows \cup {None},
            attempt : 0..MaxAttempt, head : 0..MaxHead, score : Scores]
RowRec  == [state : RowStates, flaps : 0..MaxFlaps, width : Widths]

TypeOK ==
  /\ cs \in [Cards -> CardRec]
  /\ rs \in [Rows -> RowRec]
  /\ held \in BOOLEAN
  /\ last.ev \in CardEvents \cup RowEvents \cup EnvEvents
  /\ last.c \in Cards \cup {NoActor}
  /\ last.r \in Rows \cup {NoActor}
  /\ last.pre \in [Cards -> CardRec]

(* One holder column per card, a row or none. Structural; stated anyway.   *)
OneHolder == \A c \in Cards : cs[c].holder \in Rows \cup {None}

HolderNoneInRestStates == \A c \in Cards : cs[c].state \in Rest => cs[c].holder = None

ReaderNeverAuthor ==
  \A c \in Cards : (cs[c].state = "review" /\ cs[c].holder # None) => cs[c].holder # cs[c].author

WorkingHasHolder == \A c \in Cards : cs[c].state = "working" => cs[c].holder \in Rows

WidthKept == \A r \in Rows : Cardinality(Holders(r)) <= rs[r].width

(* A card that was landed or fail before the step is unchanged after it.   *)
FinalNeverMoves == \A c \in Cards : last.pre[c].state \in Final => cs[c] = last.pre[c]

(* Attempt rises by one on Deal of that card and is unchanged otherwise.   *)
AttemptOnlyOnDeal ==
  \A c \in Cards :
    cs[c].attempt = IF last.ev = "Deal" /\ last.c = c THEN last.pre[c].attempt + 1
                    ELSE last.pre[c].attempt

(* A Red step never results in merging or landed. A step property: after  *)
(* a Red, a new read may say yes and the card merges, which is legal.     *)
RedNeverAdvances == last.ev = "Red" => cs[last.c].state \notin {"merging", "landed"}

FixKeepsScore == last.ev = "ReadFix" => cs[last.c].score = last.pre[last.c].score

(* Finish, a read result and a release are accepted only from the holder. *)
OnlyHolderActs ==
  last.ev \in {"Finish", "ReadYes", "ReadFix", "Release"} => last.r = last.pre[last.c].holder

(* A need going fail takes its dependants. Layer 3 raises NeedFailed in    *)
(* the same tick, so between the two steps the dependant is not yet fail;  *)
(* as a layer-2 property: whenever a need has failed and its dependant is  *)
(* not final, NeedFailed for it is enabled. reach/NeverNeedFailed shows it *)
(* fires.                                                                  *)
NeedFailedTakesDependants ==
  \A c \in Cards : \A n \in Needs[c] :
    (cs[n].state = "fail" /\ cs[c].state \notin Final) => ENABLED NeedFailed(c, n)

-----------------------------------------------------------------------------
(* Reachability: these are expected to be VIOLATED (reach/*.cfg). Each    *)
(* violation shows that the passing run above is not vacuous.             *)

NeverLanded     == \A c \in Cards : cs[c].state # "landed"
NeverFixed      == last.ev # "ReadFix"
NeverRed        == last.ev # "Red"
NeverNeedFailed == last.ev # "NeedFailed"
NeverBlocked    == \A r \in Rows : rs[r].state # "blocked"

=============================================================================
