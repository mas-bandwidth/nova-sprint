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
\*   lbatch    the batch it built: lq without the cards it ejects, up to the
\*             first card left that needs a card neither landed nor ahead of it
\*             in the batch (that card stops the stream; THE EJECT below)
\*   lej       the heads the lander ejects from this batch (THE EJECT)
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
\*   deplanded a ghost: a report landed a card that needs a card the same
\*             report ejected
\*   ejland    a ghost: a report ejected a card and landed another (the
\*             reversed witness ReachEjectAndLand: the eject is not vacuous)
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
\* THE EJECT (cmd/nova-sprint/land.go, build and eject; the owner, 2026-10-05: "true,
\* unless the cards after DEPEND on the card that is in front of the merge"). Bad is
\* the heads that cannot be merged (a git failure of that head, a conflict, the
\* lander's checks, the tree gate); Needs is each card's needs. Build ejects every
\* bad head of the batch and, in queue order, every later card whose needs reach an
\* ejected card (EjectOf: directly, or through another ejected card), and the batch
\* is the rest, cut at the first card left that needs a card neither landed nor
\* ahead of it in the batch (Blocked: the stream stops there, as a conflict stopped
\* it before). Report is one store step: it lands the batch and returns the ejected
\* cards to review, guarded as before with the ejected cards looked through (the
\* queue without them starts with the batch) and every ejected card still queued at
\* the head pinned.
\*
\* THE RULES.
\*   LandedInBase: the store records a card landed only at a head the base
\*     holds.
\*   LandsInOrder: a card lands only with every card ahead of it in the queue
\*     that the same report does not eject.
\*   NeedsLanded: no landed card needs a card that has not landed.
\*   DependentsNotLanded: no report lands a card that needs a card it ejects.
\*   EjectLands: under fairness, once the outside is quiet, the queue is empty
\*     or its first card needs a card that has not landed (a stopped stream):
\*     a bad card at the front never holds the cards behind it that do not
\*     need it.
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
\* so Recovers is not vacuous), ReachStalePush, and ReachEjectAndLand (one
\* report ejects a card and lands another, so the eject's rules are not vacuous).
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
\*   "noclosure" ejects the bad heads alone and reads no needs, the naive fix
\*     (NeedsLanded and DependentsNotLanded fail: a card that needs the ejected
\*     one lands without it).
\*   "stall" is land before the eject: the batch ends before the first bad
\*     head and nothing is ejected (EjectLands fails: the cards behind it that
\*     do not need it wait for ever; the 2026-10-05 stall of 16 cards).
\*
\* WHAT IS NOT MODELLED. The check (--check), the facts that stop a stream
\* (conflict, red, rejected) and resume: a refusal is the lander going idle
\* with the store unchanged. A stream stopped on a blocked card is the queue
\* left with that card at its front. Which git failure is a head's and which
\* the environment's (land.go, headFault), the eject's count of the same
\* reason (a third raises "the brief is wrong" instead) and the merge-stuck
\* clock are the code's and its tests'; here a head is bad or it is not. A card's attempts are counted across a clear, so
\* a head is never reused by another card. One stream, one base.
EXTENDS Integers, Sequences, FiniteSets, TLC

CONSTANTS Cards, MaxAttempts, MaxEpoch, MaxEvents, Broken,
          Needs, \* [Cards -> SUBSET Cards]: each card's needs
          Bad    \* SUBSET Heads: the heads that cannot be merged

VARIABLES queue, att, landed, epoch, base, tip,
          lphase, lq, lbatch, lep, lrep, ltip, tries, lej,
          events, badcaller, stalepush, stalerec, lpushed, deplanded, ejland

store == <<queue, att, landed, epoch>>
remote == <<base, tip>>
lander == <<lphase, lq, lbatch, lep, lrep, ltip, tries, lej>>
ghosts == <<badcaller, stalepush, stalerec, lpushed, deplanded, ejland>>
vars == <<queue, att, landed, epoch, base, tip, lphase, lq, lbatch, lep, lrep, ltip, tries, lej,
          events, badcaller, stalepush, stalerec, lpushed, deplanded, ejland>>

Heads == Cards \X (1..MaxAttempts)

Range(s) == {s[i] : i \in 1..Len(s)}

\* A card's current head in the store.
Current(c) == <<c, att[c]>>

\* The queue as heads, and a sequence of heads as ids.
QHeads == [i \in 1..Len(queue) |-> Current(queue[i])]
Ids(b) == [i \in 1..Len(b) |-> b[i][1]]

\* A card has landed (at any attempt).
LandedCard(c) == \E a \in 1..MaxAttempts : <<c, a>> \in landed

\* THE EJECT. EjectOf(s) is the heads of s the lander ejects: each bad head, and
\* in order each later head whose card needs a card already ejected (the closure:
\* through another ejected card too). noclosure ejects the bad heads alone.
RECURSIVE EjectUpTo(_, _)
EjectUpTo(s, i) ==
  IF i = 0 THEN {}
  ELSE LET prev == EjectUpTo(s, i - 1)
       IN IF s[i] \in Bad \/ (Broken # "noclosure" /\ Needs[s[i][1]] \cap {h[1] : h \in prev} # {})
          THEN prev \cup {s[i]} ELSE prev
EjectOf(s) == EjectUpTo(s, Len(s))

\* Blocked: the i-th head of s needs a card neither landed nor ahead of it in s.
Blocked(s, i) == \E n \in Needs[s[i][1]] : ~LandedCard(n) /\ n \notin {s[j][1] : j \in 1..(i - 1)}

\* The longest prefix of s with no blocked head (noclosure reads no needs).
RECURSIVE Unblocked(_, _)
Unblocked(s, i) ==
  IF i > Len(s) \/ (Broken # "noclosure" /\ Blocked(s, i)) THEN SubSeq(s, 1, i - 1)
  ELSE Unblocked(s, i + 1)

\* stall (land before the eject): the batch ends before the first bad head.
RECURSIVE BeforeBad(_, _)
BeforeBad(s, i) == IF i > Len(s) \/ s[i] \in Bad THEN SubSeq(s, 1, i - 1) ELSE BeforeBad(s, i + 1)

\* The ids of the heads the lander ejects, and the queue without them, as heads
\* and as ids.
EjIds == {h[1] : h \in lej}
Kept == SelectSeq(QHeads, LAMBDA h : h[1] \notin EjIds)
KeptIds == SelectSeq(queue, LAMBDA c : c \notin EjIds)

\* Every ejected card is still queued at the head the lander pinned.
EjFresh == lej \subseteq Range(QHeads)

\* The queue, the ejected cards looked through, still starts with exactly
\* these heads, at the epoch the lander holds (headWhy: ids, heads and
\* attempts, by name).
Fresh(b) == epoch = lep /\ EjFresh /\ Len(Kept) >= Len(b) /\ SubSeq(Kept, 1, Len(b)) = b

\* idguard's check: the ids only.
FreshIds(b) == epoch = lep /\ EjFresh /\ Len(KeptIds) >= Len(b) /\ SubSeq(KeptIds, 1, Len(b)) = Ids(b)

Guard(b) == IF Broken = "idguard" THEN FreshIds(b) ELSE Fresh(b)

\* noepoch's report guard: the heads, no epoch fence.
FreshAnyEpoch(b) == EjFresh /\ Len(Kept) >= Len(b) /\ SubSeq(Kept, 1, Len(b)) = b

\* The lander's steps in the order the variant takes them.
PushFrom == IF Broken = "reportfirst" THEN "reported" ELSE "checked"
PushTo == IF Broken = "reportfirst" THEN "idle" ELSE "pushed"
RebuildTo == IF Broken = "reportfirst" THEN "reported" ELSE "built"
ReportFrom == IF Broken = "reportfirst" THEN "built" ELSE "pushed"
ReportTo == IF Broken = "reportfirst" THEN "reported" ELSE "idle"

TypeOK ==
  /\ queue \in Seq(Cards) /\ Len(queue) <= Cardinality(Cards)
  /\ att \in [Cards -> 1..MaxAttempts]
  /\ landed \subseteq Heads
  /\ epoch \in 0..MaxEpoch
  /\ base \subseteq Heads
  /\ tip \in Nat
  /\ lphase \in {"idle", "read", "built", "checked", "pushed", "reported"}
  /\ lep \in 0..MaxEpoch
  /\ lrep \in 0..MaxEpoch
  /\ tries \in 0..1
  /\ lej \subseteq Heads
  /\ deplanded \in BOOLEAN
  /\ ejland \in BOOLEAN
  /\ events \in 0..MaxEvents
  /\ badcaller \in BOOLEAN
  /\ stalepush \in BOOLEAN
  /\ stalerec \in BOOLEAN
  /\ lpushed \subseteq Heads

Init ==
  /\ queue = <<>> /\ att = [c \in Cards |-> 1] /\ landed = {} /\ epoch = 0
  /\ base = {} /\ tip = 0
  /\ lphase = "idle" /\ lq = <<>> /\ lbatch = <<>> /\ lep = 0 /\ lrep = 0 /\ ltip = 0 /\ tries = 0 /\ lej = {}
  /\ events = 0 /\ badcaller = FALSE /\ stalepush = FALSE /\ stalerec = FALSE /\ lpushed = {}
  /\ deplanded = FALSE /\ ejland = FALSE

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

\* Build: the pinned heads merged in queue order; the bad heads and the cards
\* that need them ejected (EjectOf), the rest cut at the first blocked card
\* (build, eject). stall ends the batch before the first bad head and ejects
\* nothing.
Build ==
  /\ lphase = "read"
  /\ IF Broken = "stall"
     THEN /\ lej' = {} /\ lbatch' = BeforeBad(lq, 1)
     ELSE LET ej == EjectOf(lq)
          IN /\ lej' = ej
             /\ lbatch' = Unblocked(SelectSeq(lq, LAMBDA h : h \notin ej), 1)
  /\ lphase' = "built"
  /\ UNCHANGED store /\ UNCHANGED remote /\ UNCHANGED <<lq, lep, lrep, ltip, tries>>
  /\ UNCHANGED events /\ UNCHANGED ghosts

\* Check: the queue's heads and the epoch read again just before the push
\* (queueHead); a stale batch is refused, nothing pushed. latepoch checks
\* nothing before the push; reportfirst has no check.
Check ==
  /\ lphase = "built" /\ Broken # "reportfirst"
  /\ lphase' = IF Broken = "latepoch" \/ Guard(lbatch) THEN "checked" ELSE "idle"
  /\ UNCHANGED store /\ UNCHANGED remote /\ UNCHANGED <<lq, lbatch, lep, lrep, ltip, tries, lej>>
  /\ UNCHANGED events /\ UNCHANGED ghosts

\* Push: git push, which the store cannot fence. A moved tip is rejected and
\* the batch rebuilt on the new tip, to be checked again, once; then given up
\* (the rejected fact). Else the base takes the batch's pinned heads (a batch
\* the base holds already is a push of nothing).
Push ==
  /\ lphase = PushFrom
  /\ UNCHANGED store /\ UNCHANGED <<lq, lep, lrep, lbatch, lej>> /\ UNCHANGED events
  /\ UNCHANGED <<stalerec, deplanded, ejland>>
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
\* the batch only while the queue starts with exactly the heads pushed; the
\* store records each card at its current head. Refused, it writes nothing and
\* the lander says LAND FAILED. noguard lands the first Len(lbatch) queued,
\* whatever they are (merge --batch n alone); idguard compares ids; noepoch
\* compares heads with no epoch fence. The same step returns the ejected cards
\* to review: off the queue (MergeReq.Ejects, sprint.ejectUnits).
Report ==
  /\ lphase = ReportFrom
  /\ LET n == Len(lbatch)
         ok == CASE Broken = "noguard" -> epoch = lep /\ EjFresh /\ Len(KeptIds) >= n
                 [] Broken = "noepoch" -> FreshAnyEpoch(lbatch)
                 [] OTHER -> Guard(lbatch)
     IN /\ IF ok
           THEN /\ landed' = landed \cup {Current(KeptIds[i]) : i \in 1..n}
                /\ queue' = SubSeq(KeptIds, n + 1, Len(KeptIds))
                /\ stalerec' = (stalerec \/ epoch # lep)
                /\ deplanded' = (deplanded \/ \E i \in 1..n : Needs[KeptIds[i]] \cap EjIds # {})
                /\ ejland' = (ejland \/ (lej # {} /\ n > 0))
           ELSE UNCHANGED <<queue, landed, stalerec, deplanded, ejland>>
  /\ lphase' = ReportTo
  /\ UNCHANGED <<att, epoch>> /\ UNCHANGED remote /\ UNCHANGED <<lq, lbatch, lep, lrep, ltip, tries, lej>>
  /\ UNCHANGED events /\ UNCHANGED <<badcaller, stalepush, lpushed>>

Land == Read \/ Build \/ Check \/ Push \/ Report

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
  /\ Len(queue) > 0
  /\ Current(Head(queue)) \notin Bad /\ \A n \in Needs[Head(queue)] : LandedCard(n)
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
  /\ UNCHANGED store /\ UNCHANGED remote /\ UNCHANGED <<lq, lbatch, lep, lrep, ltip, lej>>

Outside ==
  /\ events < MaxEvents
  /\ events' = events + 1
  /\ UNCHANGED <<badcaller, stalepush, stalerec, deplanded, ejland>>
  /\ (Accept \/ Return \/ Rework \/ OtherLand \/ Clear \/ MoveBase \/ Crash)
  /\ lpushed' = IF epoch' # epoch THEN {} ELSE lpushed

Next == Land \/ Outside

Spec == Init /\ [][Next]_vars

\* The lander runs again whenever it can: the coordinator re-runs land.
FairSpec == Spec /\ WF_vars(Land)

\* ---- the rules ----

LandedInBase == landed \subseteq base

LandsInOrder ==
  [][LET new == {h[1] : h \in landed' \ landed} IN
       /\ new \subseteq Range(queue)
       /\ \A i \in 1..Len(queue) : queue[i] \in new =>
            \A j \in 1..(i - 1) : queue[j] \in new \/ (queue[j] \in EjIds /\ queue[j] \notin Range(queue'))
    ]_vars

NeedsLanded == \A h \in landed : \A n \in Needs[h[1]] : LandedCard(n)

DependentsNotLanded == ~deplanded

EjectLands == <>[](Len(queue) = 0 \/ Blocked(QHeads, 1))

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

\* One report ejects a card and lands another: the eject is not vacuous.
\* Shortest: Accept c1 (bad), Accept c3, Read, Build, Check, Push, Report.
ReachEjectAndLand == ~ejland
=============================================================================
