-------------------------------- MODULE Land --------------------------------
\* nova-sprint land (cmd/nova-sprint/land.go): the coordinator's landing step,
\* which pushes a batch of a stream's merge queue to a remote base and then
\* reports it to the store through the merge step. The push and the report
\* are two operations on two systems; this module is that sequence, the
\* outside events that can come between them, and the fences land keeps.
\*
\* A HEAD. A card keeps its id and its epoch across attempts: a return, a
\* rework and a new accept put the same id back in the queue at a new head.
\* A head is the pair <<card, attempt>>; the base holds heads, the store
\* records a card landed at a head, and the lander pins the heads it read.
\*
\* THE STATE.
\*   queue     the stream's merge queue in the store: card ids in work order
\*             (the cards queued before the first stuck one)
\*   att       each card's current attempt in the store
\*   landed    the heads the store records landed, at the store's epoch
\*   epoch     the store's epoch (a clear moves it and empties the tables)
\*   base      the heads the remote base holds
\*   tip       the remote base's tip, a counter: every change to the base moves it
\*   lphase    the lander's step: idle, read, built, checked, pushed
\*             (reportfirst: idle, read, built, reported)
\*   lq        the queue as the lander read it, as heads (pinned)
\*   lbatch    the batch it built: lq less the cards it ejects, in lq's order
\*   lej       the cards it ejects, as heads in lq's order (THE EJECT, below)
\*   lep       the epoch the lander holds (the caller's --epoch, else the one read)
\*   lrep      the store's epoch when the lander read it
\*   ltip      the base's tip the batch was built on
\*   tries     the rebuilds on a moved base (one is allowed)
\*   events    the outside events so far, bounded by MaxEvents
\*   badcaller a ghost: a push was made for a caller whose epoch the store was
\*             not at when the lander read it
\*   stalepush a ghost: a push was made after the store left the epoch the
\*             lander holds (a clear between the check and the push)
\*   stalerec  a ghost: a report recorded a landing at an epoch other than
\*             the one the lander holds
\*   needbroke a ghost: a report landed a card one of whose needs was a card
\*             of the batch read and is not landed
\*   ejland    a ghost: a report landed a card that needs a card ejected from
\*             its batch
\*   ejclean   a ghost: a build ejected a card that cannot be blamed: neither
\*             its own head fails (Bad) nor does it need a card ejected before it
\*   lpushed   a ghost: the heads this lander itself pushed for the store's
\*             epoch (a clear empties it, and a push made for an epoch the
\*             store has left adds nothing), so the stranded witness names the
\*             lander's own push in this epoch and not another lander's
\*             landing re-queued, nor a stale push
\*
\* THE ACTIONS. The lander's, each a call in land.go: Read (the queue with its
\* heads, the epoch and the tip; a caller's epoch the store is not at is
\* refused here, before any git: cmdLand), Build, Check (the queue's heads and
\* the epoch read again just before the push: queueHead), Push (git push: a
\* moved tip is rejected and the batch rebuilt and checked again once, then
\* given up), Report (one store step, lander.step, fenced to the epoch held,
\* that lands the batch only while the queue still starts with exactly the
\* heads pushed). The outside's: Accept (a card queued anywhere, by its
\* score), Return (a card taken off the queue), Rework (a queued card replaced
\* by its next attempt, keeping its id, its epoch and its place: return,
\* rework, deal, take, finish, ask, reads and accept between two of the
\* lander's calls), OtherLand (another lander lands the queue's head,
\* correctly), Clear (the epoch moves, the store's tables empty), MoveBase
\* (another commit on the base), Crash (the lander stops at any step).
\*
\* THE RULES.
\*   LandedInBase: the store records a card landed only at a head the base
\*     holds.
\*   LandsQueued: a card lands only from the queue (the eject's rules, below).
\*   CallerEpochCheckedBeforePush: no push is made for a caller whose epoch
\*     the store was not at when the lander read it.
\*   ReportHoldsTheEpoch: no report records a landing at an epoch the lander
\*     does not hold.
\*   Recovers: a batch pushed and not reported (a crash, or a report the guard
\*     refused) is recorded by running the lander again: under fairness, once
\*     the outside is quiet, no queued card's current head stays in the base.
\* WHAT IS NOT ATTAINABLE, AND IS NOT CLAIMED. The check and the push are two
\* calls, and a git push cannot be fenced by the store: a clear between them
\* makes a push for an epoch the store has just left (ReachStalePush, a reversed
\* witness, reaches it). What is attained is that nothing is recorded for it
\* (ReportHoldsTheEpoch, LandedInBase); the external push itself is not undone,
\* and after a clear the store holds nothing to report it to. The same window
\* lets a rework replace a card's head after the check: the report's guard,
\* which compares heads and not ids, refuses it.
\* Reversed witnesses: ReachStranded (the lander's own push left unreported,
\* so Recovers is not vacuous) and ReachStalePush.
\*
\* WHAT RECOVERS DOES NOT COVER. It is proved once the outside goes quiet
\* (the instance caps outside events at MaxEvents), so it says nothing of an
\* outside that never stops: (a) a lander that crashes between the push and
\* the report on every run never records the batch (its heads stay in the base
\* and its cards stay queued, so the first run that is not cut short records
\* it); (b) one rebuild on a moved base and then the lander gives up (tries <
\* 1): a base that moves twice inside every read-to-push window makes every
\* run give up, with nothing pushed and the cards still queued, so running it
\* again loses nothing.
\*
\* Broken: "none" is the design.
\*   "reportfirst" reports the batch before it is pushed (LandedInBase fails).
\*   "latepoch" checks the caller's epoch only at the report, after the push
\*     (CallerEpochCheckedBeforePush fails).
\*   "noguard" reports `merge --batch n` with no guard that the queue still
\*     starts with the batch (LandedInBase fails: a card accepted ahead of the
\*     batch between the push and the report lands unpushed).
\*   "idguard" guards with the cards' ids and not their heads (LandedInBase
\*     fails: a card reworked between the check and the report lands at the
\*     new head while the base holds the old one).
\*   "noepoch" reports with the heads guarded and no fence on the epoch
\*     (ReportHoldsTheEpoch fails: a push, a clear, the same head accepted
\*     again in the new epoch, and the old run's report records it there).
\*
\*   "noclosure" ejects the card that cannot merge and not the cards that
\*     need it (NoLandedNeedsUnlanded and EjectedDependentsNotLanded fail:
\*     its dependent lands without it).
\*   "stopall" is the lander before the eject: a card that cannot merge ends
\*     the batch before it and nothing is ejected, so it stays queued at the
\*     front of every later batch (Drains fails: the stream never drains).
\*
\* THE EJECT (cmd/nova-sprint/land.go, build and eject; the owner, 2026-10-05:
\* one bad card no longer stalls its batch). Bad is the cards whose heads can
\* never merge (a git failure of the head itself, a conflict, the lander's
\* checks, the tree gate): the lander's merge of such a card fails on every run.
\* Needs is each card's needs. Build merges lq in order; a card in Bad is
\* ejected, and so is every card of the batch whose needs reach an ejected
\* card, directly or through another ejected card, ahead of it or behind it
\* (the eject closure, EjIds: land.go rebuilds the batch without them when a
\* card merged already needs one); the rest is the batch, in order. After the report, one store step (Eject),
\* fenced to the epoch held and guarded on the ejected cards' heads as the
\* report is on the batch's, takes every ejected card off the queue (back to
\* review with its reason: the merge step's eject, internal/sprint/merge_eject.go).
\* A batch whose report was refused ejects nothing (the next run reads again).
\* The guard is by name, wherever the cards stand (headWhy: cards accepted
\* ahead of the batch since it was read change nothing), so the batch is no
\* longer a prefix of the queue and LandsInOrder (a card lands only with every
\* card ahead of it) is replaced by the needs: a card lands only from the
\* queue (LandsQueued), never without a need of its batch (NoLandedNeedsUnlanded),
\* never with an ejected card it needs (EjectedDependentsNotLanded), no card
\* is ejected without cause (NoEjectWithoutCause), and under fairness, once
\* the outside is quiet, the queue drains (Drains): with NoEjectWithoutCause,
\* every card that does not depend on an ejected one lands. ReachEjectAndLand
\* is its reversed witness: one run ejects a card and lands the rest.
\*
\* WHAT IS NOT MODELLED. The check (--check), the facts that stop a stream
\* (red, rejected) and resume: a refusal is the lander going idle with the
\* store unchanged. The judgments the eject raises (one per eject, the brief
\* is wrong on a third eject for one cause, merge stuck after 15 minutes) are
\* notices and change no queue. A card's attempts are counted across a clear,
\* so a head is never reused by another card. One stream, one base.
EXTENDS Integers, Sequences, FiniteSets, TLC

CONSTANTS Cards, MaxAttempts, MaxEpoch, MaxEvents, Broken, Needs, Bad

VARIABLES queue, att, landed, epoch, base, tip,
          lphase, lq, lbatch, lej, lep, lrep, ltip, tries,
          events, badcaller, stalepush, stalerec, lpushed,
          needbroke, ejland, ejclean

store == <<queue, att, landed, epoch>>
remote == <<base, tip>>
lander == <<lphase, lq, lbatch, lej, lep, lrep, ltip, tries>>
ghosts == <<badcaller, stalepush, stalerec, lpushed, needbroke, ejland, ejclean>>
needs == <<needbroke, ejland, ejclean>>
vars == <<queue, att, landed, epoch, base, tip, lphase, lq, lbatch, lej, lep, lrep, ltip, tries,
          events, badcaller, stalepush, stalerec, lpushed, needbroke, ejland, ejclean>>

Heads == Cards \X (1..MaxAttempts)

Range(s) == {s[i] : i \in 1..Len(s)}

\* A card's current head in the store.
Current(c) == <<c, att[c]>>

\* The queue as heads, and a sequence of heads as ids.
QHeads == [i \in 1..Len(queue) |-> Current(queue[i])]
Ids(b) == [i \in 1..Len(b) |-> b[i][1]]

\* A head is queued: its card is in the queue at it.
Queued(h) == h[1] \in Range(queue) /\ Current(h[1]) = h

\* The queue still holds exactly these heads, wherever they stand, at the
\* epoch the lander holds (headWhy: ids, heads and attempts, by name).
Fresh(b) == epoch = lep /\ \A i \in 1..Len(b) : Queued(b[i])

\* idguard's check: the ids only.
FreshIds(b) == epoch = lep /\ \A i \in 1..Len(b) : b[i][1] \in Range(queue)

Guard(b) == IF Broken = "idguard" THEN FreshIds(b) ELSE Fresh(b)

\* noepoch's report guard: the heads, no epoch fence.
FreshAnyEpoch(b) == \A i \in 1..Len(b) : Queued(b[i])

\* The eject closure over a read queue q (heads): a card is ejected when its
\* own head cannot merge, or when it needs an ejected card of the batch,
\* wherever that card stands in it (a rank can put a card ahead of a card it
\* needs: the first model had "every later card", and TLC found c2 ahead of c1
\* landing while c1 was ejected). noclosure ejects only the cards that cannot
\* merge.
RECURSIVE Close(_, _, _)
Close(ids, e, k) == IF k = 0 THEN e ELSE Close(ids, e \cup {c \in ids : Needs[c] \cap e # {}}, k - 1)
EjIds(q) == LET ids == Range(Ids(q))
            IN IF Broken = "noclosure" THEN ids \cap Bad ELSE Close(ids, ids \cap Bad, Len(q))

\* stopall's batch: the read queue up to its first card that cannot merge.
FirstBad(q) == IF \E i \in 1..Len(q) : q[i][1] \in Bad
               THEN CHOOSE i \in 1..Len(q) : q[i][1] \in Bad /\ \A j \in 1..(i - 1) : q[j][1] \notin Bad
               ELSE Len(q) + 1

\* The lander's steps in the order the variant takes them.
PushFrom == IF Broken = "reportfirst" THEN "reported" ELSE "checked"
PushTo == IF Broken = "reportfirst" THEN "idle" ELSE "pushed"
RebuildTo == IF Broken = "reportfirst" THEN "reported" ELSE "built"
ReportFrom == IF Broken = "reportfirst" THEN "built" ELSE "pushed"
ReportTo == IF Broken = "reportfirst" THEN "reported" ELSE "eject"

TypeOK ==
  /\ queue \in Seq(Cards) /\ Len(queue) <= Cardinality(Cards)
  /\ att \in [Cards -> 1..MaxAttempts]
  /\ landed \subseteq Heads
  /\ epoch \in 0..MaxEpoch
  /\ base \subseteq Heads
  /\ tip \in Nat
  /\ lphase \in {"idle", "read", "built", "checked", "pushed", "reported", "eject"}
  /\ lep \in 0..MaxEpoch
  /\ lrep \in 0..MaxEpoch
  /\ tries \in 0..1
  /\ events \in 0..MaxEvents
  /\ badcaller \in BOOLEAN
  /\ stalepush \in BOOLEAN
  /\ stalerec \in BOOLEAN
  /\ lpushed \subseteq Heads
  /\ needbroke \in BOOLEAN /\ ejland \in BOOLEAN /\ ejclean \in BOOLEAN

Init ==
  /\ queue = <<>> /\ att = [c \in Cards |-> 1] /\ landed = {} /\ epoch = 0
  /\ base = {} /\ tip = 0
  /\ lphase = "idle" /\ lq = <<>> /\ lbatch = <<>> /\ lej = <<>> /\ lep = 0 /\ lrep = 0 /\ ltip = 0 /\ tries = 0
  /\ events = 0 /\ badcaller = FALSE /\ stalepush = FALSE /\ stalerec = FALSE /\ lpushed = {}
  /\ needbroke = FALSE /\ ejland = FALSE /\ ejclean = FALSE

\* ---- the lander (land.go) ----

\* Read: the queue with its heads, the epoch and the tip. The caller's epoch
\* cep (its --epoch, or the store's when it gives none) is held to the
\* store's here, before any git (cmdLand); latepoch skips it.
Read ==
  \E cep \in 0..MaxEpoch :
    /\ lphase = "idle" /\ Len(queue) > 0
    /\ Broken = "latepoch" \/ cep = epoch
    /\ lphase' = "read" /\ lq' = QHeads /\ lep' = cep /\ lrep' = epoch /\ ltip' = tip /\ tries' = 0
    /\ UNCHANGED store /\ UNCHANGED remote /\ UNCHANGED <<lbatch, lej>>
    /\ UNCHANGED events /\ UNCHANGED ghosts

\* Build: the pinned heads merged in queue order; a card that cannot merge
\* is ejected with every card of the batch whose needs reach it (EjIds), and the rest
\* is the batch, in order. stopall ends the batch at the first card that
\* cannot merge and ejects nothing.
Build ==
  /\ lphase = "read"
  /\ IF Broken = "stopall"
     THEN /\ lbatch' = SubSeq(lq, 1, FirstBad(lq) - 1) /\ lej' = <<>>
     ELSE LET ej == EjIds(lq)
          IN /\ lbatch' = SelectSeq(lq, LAMBDA h : h[1] \notin ej)
             /\ lej' = SelectSeq(lq, LAMBDA h : h[1] \in ej)
  /\ ejclean' = (ejclean \/ \E i \in 1..Len(lej') :
                   lej'[i][1] \notin Bad /\ Needs[lej'[i][1]] \cap Range(Ids(lej')) = {})
  /\ lphase' = "built"
  /\ UNCHANGED store /\ UNCHANGED remote /\ UNCHANGED <<lq, lep, lrep, ltip, tries>>
  /\ UNCHANGED events /\ UNCHANGED <<badcaller, stalepush, stalerec, lpushed, needbroke, ejland>>

\* Check: the queue's heads and the epoch read again just before the push
\* (queueHead); a stale batch is refused, nothing pushed. latepoch checks
\* nothing before the push; reportfirst has no check.
Check ==
  /\ lphase = "built" /\ Broken # "reportfirst"
  /\ lphase' = IF Broken = "latepoch" \/ Guard(lbatch) THEN "checked" ELSE "idle"
  /\ UNCHANGED store /\ UNCHANGED remote /\ UNCHANGED <<lq, lbatch, lej, lep, lrep, ltip, tries>>
  /\ UNCHANGED events /\ UNCHANGED ghosts

\* Push: git push, which the store cannot fence. A moved tip is rejected and
\* the batch rebuilt on the new tip, to be checked again, once; then given up
\* (the rejected fact). Else the base takes the batch's pinned heads (a batch
\* the base holds already is a push of nothing).
Push ==
  /\ lphase = PushFrom
  /\ UNCHANGED store /\ UNCHANGED <<lq, lep, lrep, lbatch, lej>> /\ UNCHANGED events /\ UNCHANGED stalerec
  /\ UNCHANGED needs
  /\ IF ltip # tip
     THEN IF tries < 1
          THEN /\ ltip' = tip /\ tries' = tries + 1 /\ lphase' = RebuildTo
               /\ UNCHANGED remote /\ UNCHANGED <<badcaller, stalepush, lpushed>>
          ELSE /\ lphase' = "idle"
               /\ UNCHANGED remote /\ UNCHANGED <<ltip, tries, badcaller, stalepush, lpushed>>
     ELSE /\ base' = base \cup Range(lbatch)
          /\ tip' = IF Range(lbatch) \subseteq base THEN tip ELSE tip + 1
          /\ ltip' = tip'
          /\ badcaller' = (badcaller \/ lep # lrep)
          /\ stalepush' = (stalepush \/ lep # epoch)
          /\ lpushed' = IF lep = epoch THEN lpushed \cup Range(lbatch) ELSE lpushed
          /\ lphase' = PushTo
          /\ UNCHANGED tries

\* Report: one store step (lander.step), fenced to the epoch held, that lands
\* the batch by name only while the queue holds exactly the heads pushed; the
\* store records each card at its current head. Refused, it writes nothing,
\* the lander says LAND FAILED and ejects nothing. noguard lands the first
\* Len(lbatch) queued, whatever they are (merge --batch n alone); idguard
\* compares ids; noepoch compares heads with no epoch fence.
Report ==
  /\ lphase = ReportFrom
  /\ LET n == Len(lbatch)
         ok == CASE Broken = "noguard" -> epoch = lep /\ Len(queue) >= n
                 [] Broken = "noepoch" -> FreshAnyEpoch(lbatch)
                 [] OTHER -> Guard(lbatch)
         lands == IF Broken = "noguard" THEN {queue[i] : i \in 1..n} ELSE Range(Ids(lbatch))
         read == Range(Ids(lq))
     IN /\ IF ok
           THEN /\ landed' = landed \cup {Current(c) : c \in lands}
                /\ queue' = SelectSeq(queue, LAMBDA x : x \notin lands)
                /\ stalerec' = (stalerec \/ epoch # lep)
                /\ needbroke' = (needbroke \/ \E c \in lands : \E m \in Needs[c] :
                                   m \in read /\ m \notin {h[1] : h \in landed'})
                /\ ejland' = (ejland \/ \E c \in lands : Needs[c] \cap Range(Ids(lej)) # {})
                /\ lphase' = ReportTo
           ELSE /\ UNCHANGED <<queue, landed, stalerec, needbroke, ejland>>
                /\ lphase' = IF ReportTo = "eject" THEN "idle" ELSE ReportTo
  /\ UNCHANGED <<att, epoch>> /\ UNCHANGED remote /\ UNCHANGED <<lq, lbatch, lej, lep, lrep, ltip, tries>>
  /\ UNCHANGED events /\ UNCHANGED <<badcaller, stalepush, lpushed, ejclean>>

\* Eject: one store step (the merge step's eject, lander.step), fenced to the
\* epoch held and guarded on the ejected cards' heads as the report is on the
\* batch's, that takes every ejected card off the queue (back to review with
\* its reason); refused, it writes nothing and the next run reads again.
Eject ==
  /\ lphase = "eject"
  /\ IF Fresh(lej)
     THEN queue' = SelectSeq(queue, LAMBDA x : x \notin Range(Ids(lej)))
     ELSE UNCHANGED queue
  /\ lphase' = "idle"
  /\ UNCHANGED <<att, landed, epoch>> /\ UNCHANGED remote /\ UNCHANGED <<lq, lbatch, lej, lep, lrep, ltip, tries>>
  /\ UNCHANGED events /\ UNCHANGED ghosts

Land == Read \/ Build \/ Check \/ Push \/ Report \/ Eject

\* ---- the outside, each event counted ----

Accept ==
  \E c \in Cards, i \in 1..(Len(queue) + 1) :
    /\ c \notin Range(queue) /\ Current(c) \notin landed
    /\ queue' = SubSeq(queue, 1, i - 1) \o <<c>> \o SubSeq(queue, i, Len(queue))
    /\ UNCHANGED <<att, landed, epoch>> /\ UNCHANGED remote /\ UNCHANGED lander

Return ==
  \E c \in Range(queue) :
    /\ queue' = SelectSeq(queue, LAMBDA x : x # c)
    /\ UNCHANGED <<att, landed, epoch>> /\ UNCHANGED remote /\ UNCHANGED lander

Rework ==
  \E c \in Range(queue) :
    /\ att[c] < MaxAttempts
    /\ att' = [att EXCEPT ![c] = @ + 1]
    /\ UNCHANGED <<queue, landed, epoch>> /\ UNCHANGED remote /\ UNCHANGED lander

OtherLand ==
  /\ Len(queue) > 0 /\ Head(queue) \notin Bad
  /\ base' = base \cup {Current(Head(queue))} /\ tip' = tip + 1
  /\ landed' = landed \cup {Current(Head(queue))} /\ queue' = Tail(queue)
  /\ UNCHANGED <<att, epoch>> /\ UNCHANGED lander

Clear ==
  /\ epoch < MaxEpoch
  /\ epoch' = epoch + 1 /\ queue' = <<>> /\ landed' = {}
  /\ UNCHANGED att /\ UNCHANGED remote /\ UNCHANGED lander

MoveBase ==
  /\ tip' = tip + 1
  /\ UNCHANGED store /\ UNCHANGED base /\ UNCHANGED lander

Crash ==
  /\ lphase # "idle"
  /\ lphase' = "idle" /\ tries' = 0
  /\ UNCHANGED store /\ UNCHANGED remote /\ UNCHANGED <<lq, lbatch, lej, lep, lrep, ltip>>

Outside ==
  /\ events < MaxEvents
  /\ events' = events + 1
  /\ UNCHANGED <<badcaller, stalepush, stalerec>> /\ UNCHANGED needs
  /\ (Accept \/ Return \/ Rework \/ OtherLand \/ Clear \/ MoveBase \/ Crash)
  /\ lpushed' = IF epoch' # epoch THEN {} ELSE lpushed

Next == Land \/ Outside

Spec == Init /\ [][Next]_vars

\* The lander runs again whenever it can: the coordinator re-runs land.
FairSpec == Spec /\ WF_vars(Land)

\* ---- the rules ----

LandedInBase == landed \subseteq base

\* A card lands only from the queue (LandsInOrder before the eject: the
\* order is now the needs, below).
LandsQueued ==
  [][{h[1] : h \in landed' \ landed} \subseteq Range(queue)]_vars

\* No landed card needs a card of its batch that is not landed.
NoLandedNeedsUnlanded == ~needbroke

\* An ejected card's dependents are never landed in that batch.
EjectedDependentsNotLanded == ~ejland

\* A card is ejected only when its own head cannot merge or it needs a card
\* ejected before it.
NoEjectWithoutCause == ~ejclean

\* Under fairness, once the outside is quiet, every queued card leaves the
\* queue: landed, or ejected with cause (a batch with an ejectable card still
\* lands every card that does not depend on it).
Drains == <>[](queue = <<>>)

CallerEpochCheckedBeforePush == ~badcaller

ReportHoldsTheEpoch == ~stalerec

Recovers == <>[](\A c \in Range(queue) : Current(c) \notin base)

\* Reachability (reversed witnesses, written to be false where the design
\* must reach).
\* A head this lander pushed, its card still queued at it, the lander idle:
\* pushed and not reported (a crash between the push and the report, or a
\* report the guard refused). Shortest: Accept, Read, Build, Check, Push, Crash.
ReachStranded == ~(lphase = "idle" /\ \E c \in Range(queue) : Current(c) \in lpushed)

\* A push made after the store left the epoch the lander holds: a clear between
\* the check and the push. Shortest: Accept, Read, Build, Check, Clear, Push.
ReachStalePush == ~stalepush

\* One run ejects a card and lands the rest of its batch.
\* Shortest (TLC): two accepts, Read, Build, Check, Push, Report.
ReachEjectAndLand == ~(lphase = "eject" /\ Len(lej) > 0 /\ Len(lbatch) > 0 /\ Range(lbatch) \subseteq landed)
=============================================================================
