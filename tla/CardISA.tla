---------------------------- MODULE CardISA ----------------------------
(***************************************************************************)
(* The card instruction set (docs/SPEC-ISA.md, layer 1): a card is one      *)
(* instruction, and its kind says what operands and results it carries.     *)
(*                                                                         *)
(* This module does not restate the card life; it EXTENDS CardMachine and   *)
(* reuses its actions. Every life action below is the base action, called   *)
(* through Frame(A) == A /\ UNCHANGED <<the ISA's own variables>>; the      *)
(* base predicate still names the Lua it was read from. What this module    *)
(* adds is what the kinds add:                                             *)
(*                                                                         *)
(*   - the one `wait` kind. Today a card waits for one of several          *)
(*     unrelated reasons, each its own path (admitted held, a sentinel, a   *)
(*     wave, DEPENDS-ON, a proposed external wait). CardMachine's Release   *)
(*     is the `release` path alone. This module replaces Release with ONE   *)
(*     action, Wait(c), guarded by ONE predicate, OperandHolds(c), whose    *)
(*     operand is WaitFor[c]: another card id (today's needs), "release"    *)
(*     (today's held and the wave behind it), or "external" (the proposed   *)
(*     external wait). The paths are data now, not code.                    *)
(*                                                                         *)
(*     Replaced: CardMachine.Release. Also replaced, so a waiting card can  *)
(*     never be dispatched around the operand: CardMachine.DealWork (the    *)
(*     base deal reads waiting as well as ready, line 165) is here Deal,    *)
(*     which deals a ready card alone; a waiting card becomes ready only    *)
(*     through Wait. CardMachine.Land and LandEvent are IsaLand/            *)
(*     IsaLandEvent, which also record the retire.                         *)
(*                                                                         *)
(*   - the per-kind results. A finish records the result the kind owes:     *)
(*     think, script and merge a head and a verdict; verify a verdict;      *)
(*     wait neither (its results column is none). RecordResult writes it    *)
(*     once a card is landed.                                              *)
(*                                                                         *)
(* The card life itself is CardMachine's; the kinds' vocabulary and the     *)
(* one wait kind are this module's. A reader counts the wait actions here   *)
(* (one, Wait) against the paths it replaced (hold, sentinel, wave: three   *)
(* in CardMachine plus the code it was read from).                         *)
(*                                                                         *)
(*   - the stall reason (docs/SPEC-SPRINT.md, processor-counters; layer 2, *)
(*     the performance counters). stall[c] is the one reason a card's time *)
(*     goes to now, read off its place by StallOf: waiting by its operand   *)
(*     (need, release, external), ready (slot, or rework once it has been   *)
(*     dealt before), working (work), review (read), merging (merge), and   *)
(*     none outside the life. Every life action restamps it (S(A)); Clock(c) *)
(*     is a second of the card's wall time, charged to that one reason.     *)
(*     The code (internal/sprint/counters.go) splits a landed card's wall   *)
(*     time the same way from its stage stamps; this module checks that the *)
(*     split is a partition: every second charged once, to one reason.      *)
(*                                                                         *)
(* What must always hold (TLC):                                            *)
(*   NoCardRetiresTwice       no card lands twice                          *)
(*   RetiredHeadOnBase        a landed card's head is an ancestor of its base *)
(*   WaitDispatchesOnlyWhenOperandHolds  a card never leaves waiting for a  *)
(*                            consumer before its operand holds            *)
(*   StallsPartitionWallTime  one reason at a time, its place's, and the    *)
(*                            parts charged sum to the card's wall time     *)
(* and on MCCardISALive, a wait whose operand comes to hold is dealt.       *)
(*                                                                         *)
(* Reversed witnesses (Broken, the same shape as CoordinatorWake):          *)
(*   "doubleland"  a landed card is counted landed a second time           *)
(*                 (breaks NoCardRetiresTwice)                             *)
(*   "wrongbase"   a card lands with its head past its base, not on it     *)
(*                 (breaks RetiredHeadOnBase)                              *)
(*   "waitfirst"   Wait releases a card whose operand does not hold        *)
(*                 (breaks WaitDispatchesOnlyWhenOperandHolds)             *)
(*   "tworeasons"  a second of a card not working is charged to its reason *)
(*                 and to work as well (breaks StallsPartitionWallTime)    *)
(*                                                                         *)
(* LandTwoLevel (the merge-tree-proof branch, head                         *)
(* 40e9dc34ea18f7362f3b1b914ac204fe48263ae7, tla/LandTwoLevel.tla) is the   *)
(* two-level landing design. That branch has not landed on this base, so    *)
(* it is cited here only: IsaLand is the retire action the two-level        *)
(* landing will refine; no line of it is copied.                            *)
(***************************************************************************)
EXTENDS CardMachine

CONSTANTS
  Kind,     \* [Cards -> {"think", "verify", "script", "merge", "wait"}]
  WaitFor,  \* [Cards -> Cards \cup {"release", "external"}]: the wait operand
  MaxBase,  \* the base branch's bound; the ISA's base starts here
  MaxWall,  \* the clock's bound: the seconds of wall time TLC counts per card
  Broken    \* the reversed witnesses to turn on

ASSUME Kind \in [Cards -> {"think", "verify", "script", "merge", "wait"}]
ASSUME WaitFor \in [Cards -> Cards \cup {"release", "external"}]
ASSUME \A c \in Cards : WaitFor[c] /= "release" /\ WaitFor[c] /= "external" => WaitFor[c] /= c
ASSUME MaxBase \in Nat /\ MaxBase >= 1
ASSUME MaxWall \in Nat

Kinds       == {"think", "verify", "script", "merge", "wait"}
ProducesHead(k) == k \in {"think", "script", "merge"}
ProducesVerdict(k) == k /= "wait"
WaitCards   == {c \in Cards : Kind[c] = "wait"}

VARIABLES
  released,  \* BOOLEAN: the coordinator's release, the operand "release"
  external,  \* BOOLEAN: the outside's condition, the operand "external"
  lands,     \* [Cards -> Nat]: how many times a card has landed (0 or 1, or 2 broken)
  base,      \* [Cards -> Nat]: the base branch the card's head must be an ancestor of
  verdict,   \* [Cards -> {"-", "ok", "broken"}]: the result the kind owes
  stall,     \* [Cards -> Reasons]: the one reason the card's time goes to now
  spent,     \* [Cards -> [Reasons -> Nat]]: the seconds charged to each reason
  wall       \* [Cards -> Nat]: the card's wall time, in seconds

\* the new variables as one tuple; ivars is the base's vars and these
ivars == <<released, external, lands, base, verdict>>
\* the counters' variables, apart: every life action restamps stall (S)
svars == <<stall, spent, wall>>
avars == <<vars, ivars, svars>>

\* the stall reasons (counters.go's StallOrder), and none: outside the life
Reasons == {"need", "release", "external", "rework", "slot", "work", "read", "merge", "none"}
\* the places a card's wall time runs in: from its add to its retire
Running == {"waiting", "ready", "working", "review", "merging"}

\* Frame(A): a reused CardMachine action, with the ISA's own variables still.
\* An action that does not name a variable leaves it free; this is the one
\* place that keeps the reuse honest.
Frame(A) == A /\ UNCHANGED ivars

IsaTypeOK ==
  /\ TypeOK
  /\ released \in BOOLEAN
  /\ external \in BOOLEAN
  /\ lands \in [Cards -> Nat]
  /\ base \in [Cards -> Nat]
  /\ verdict \in [Cards -> {"-", "ok", "broken"}]
  /\ stall \in [Cards -> Reasons]
  /\ spent \in [Cards -> [Reasons -> Nat]]
  /\ wall \in [Cards -> Nat]

\* StallOf(c): the reason the card's time goes to in its place now.
StallOf(c) ==
  CASE where[c] = "waiting" ->
         IF WaitFor[c] = "release" THEN "release"
         ELSE IF WaitFor[c] = "external" THEN "external"
         ELSE "need"
    [] where[c] = "ready"   -> IF ncut[c] > 0 THEN "rework" ELSE "slot"
    [] where[c] = "working" -> "work"
    [] where[c] = "review"  -> "read"
    [] where[c] = "merging" -> "merge"
    [] OTHER                -> "none"

StallNow == [c \in Cards |-> StallOf(c)]

IsaInit ==
  /\ Init
  /\ released = FALSE
  /\ external = FALSE
  /\ lands = [c \in Cards |-> 0]
  /\ base = [c \in Cards |-> MaxBase]
  /\ verdict = [c \in Cards |-> "-"]
  /\ stall = StallNow
  /\ spent = [c \in Cards |-> [r \in Reasons |-> 0]]
  /\ wall = [c \in Cards |-> 0]

\* S(A): a life action, with the stall restamped from the places it leaves and
\* the clock still. The life actions name none of svars; this is the one place
\* that keeps the stall reason in step with them.
S(A) == A /\ stall' = StallNow' /\ UNCHANGED <<spent, wall>>

----------------------------------------------------------------------------
(* The one wait kind *)

\* OperandHolds(c): the one guard of the one wait kind. WaitFor[c] says what
\* the card waits for, in the four forms the spec reads off DEPENDS-ON:
\* a card id (waits for it to land), "release" (the coordinator's release,
\* which today is admitted held and the wave behind a held sentinel), or
\* "external" (an external condition).
OperandHolds(c) ==
  LET o == WaitFor[c] IN
  IF o = "release"  THEN released
  ELSE IF o = "external" THEN external
  ELSE where[o] \in {"landed"} \/ (where[o] = "done" /\ ok[o] = "ok")

\* Wait(c): the one wait action, replacing CardMachine.Release (the judged
\* path's own release) and, as one action, the release of a held card and of
\* a wave. One guard, OperandHolds.
Wait(c) ==
  /\ where[c] = "waiting"
  /\ (OperandHolds(c) \/ "waitfirst" \in Broken)
  /\ where' = [where EXCEPT ![c] = "ready"]
  /\ UNCHANGED <<ok, copy, reads, pending, low, ncut, author, head, prHead, ci, cvars, up, ivars>>

\* Release(): the coordinator releases. It makes the "release" operand hold.
\* This is the verb, not a card's wait: the operand's value, not a path.
CoordRelease ==
  /\ ~released
  /\ released' = TRUE
  /\ UNCHANGED <<pvars, cvars, up, external, lands, base, verdict>>

\* External(): the outside's condition comes to hold.
Extern ==
  /\ ~external
  /\ external' = TRUE
  /\ UNCHANGED <<pvars, cvars, up, released, lands, base, verdict>>

----------------------------------------------------------------------------
(* The per-kind results *)

\* RecordResult(c): once a card is landed, write the result its kind owes.
\* producehead -> the head is the base it landed on; eververdict -> a word;
\* the wait kind owes neither, so its result stays none.
RecordResult(c) ==
  /\ where[c] = "landed" /\ verdict[c] = "-" /\ ProducesVerdict(Kind[c])
  /\ verdict' = [verdict EXCEPT ![c] = "ok"]
  /\ head' = IF ProducesHead(Kind[c])
              THEN [head EXCEPT ![c] = base[c]]
              ELSE head
  /\ UNCHANGED <<where, ok, copy, reads, pending, low, ncut, author, prHead, ci, cvars, up, released, external, lands, base>>

----------------------------------------------------------------------------
(* Replaced life actions that touch the new variables *)

\* Deal(c, k): a ready card is dealt. Replaces CardMachine.DealWork: the base
\* deal also reads a waiting card (line 165), which would dispatch a card
\* around its operand; here only a ready card is dealt, so every waiting card
\* passes through Wait first.
Deal(c, k) ==
  /\ where[c] = "ready" /\ Bare(c) /\ DepsMet(c)
  /\ up[k] /\ Room(k) > 0 /\ CanCut(c)
  /\ where' = [where EXCEPT ![c] = "working"]
  /\ copy' = [copy EXCEPT ![c] = NextCopy(c)]
  /\ ncut' = [ncut EXCEPT ![c] = @ + 1]
  /\ cw' = CutCW(c, k) /\ ck' = CutCK(c, k) /\ cl' = CutCL(c, "work")
  /\ UNCHANGED <<ok, reads, pending, low, author, head, prHead, ci, leased, up, ivars>>

\* IsaLand(c): CardMachine.Land (merging -> landed), plus the retire record:
\* the head is set to the base it lands on (an ancestor, the same commit),
\* the base is fixed, and the land is counted. "wrongbase" lands with the
\* head one past the base: a land onto a base the head is not on.
IsaLand(c) ==
  /\ where[c] = "merging"
  /\ where' = [where EXCEPT ![c] = "landed"]
  /\ head' = [head EXCEPT ![c] = IF "wrongbase" \in Broken THEN base[c] + 1 ELSE base[c]]
  /\ lands' = [lands EXCEPT ![c] = @ + 1]
  /\ UNCHANGED <<ok, copy, reads, pending, low, ncut, author, prHead, ci, cw, ck, cl, leased, up, released, external, base, verdict>>

\* IsaLandEvent(c): CardMachine.LandEvent (a landing event for a card that is
\* not merging), with the same retire record as IsaLand.
IsaLandEvent(c) ==
  /\ where[c] \in {"waiting", "ready", "working"}
  /\ where' = [where EXCEPT ![c] = "landed"]
  /\ head' = [head EXCEPT ![c] = IF "wrongbase" \in Broken THEN base[c] + 1 ELSE base[c]]
  /\ lands' = [lands EXCEPT ![c] = @ + 1]
  /\ cw' = LET r == RetireReads(cw, c) IN
           IF copy[c] /= NoCopy /\ cw[copy[c]] \in Live THEN [r EXCEPT ![copy[c]] = "fail"] ELSE r
  /\ reads' = [reads EXCEPT ![c] = {}]
  /\ copy' = [copy EXCEPT ![c] = NoCopy]
  /\ UNCHANGED <<ok, pending, low, ncut, author, prHead, ci, ck, cl, leased, up, released, external, base, verdict>>

\* DoubleLand(c): the reversed witness only. A landed card is counted landed
\* a second time, which the design never does. "doubleland" turns it on.
DoubleLand(c) ==
  /\ "doubleland" \in Broken
  /\ where[c] = "landed"
  /\ lands' = [lands EXCEPT ![c] = @ + 1]
  /\ UNCHANGED <<pvars, cvars, up, released, external, base, verdict>>

----------------------------------------------------------------------------
(* The clock: the counters' second *)

\* Charge(c): the card's parts with one second more on its one reason.
\* "tworeasons" charges work as well, for a card not working.
Charge(c) ==
  IF "tworeasons" \in Broken /\ stall[c] /= "work"
  THEN [spent[c] EXCEPT ![stall[c]] = @ + 1, !["work"] = @ + 1]
  ELSE [spent[c] EXCEPT ![stall[c]] = @ + 1]

\* Clock(c): a second of a card in its life passes; it is its wall time's, and
\* its stall reason's. Bounded by MaxWall for TLC.
Clock(c) ==
  /\ where[c] \in Running /\ wall[c] < MaxWall
  /\ wall' = [wall EXCEPT ![c] = @ + 1]
  /\ spent' = [spent EXCEPT ![c] = Charge(c)]
  /\ UNCHANGED <<vars, ivars, stall>>

----------------------------------------------------------------------------

\* the life actions, each restamping the stall reason through S
IsaMove ==
  \/ \E c \in Cards :
       Frame(Push(c)) \/ Wait(c) \/ Frame(CIWord(c)) \/ Frame(PRHeadMoves(c))
       \/ Frame(Verdict(c)) \/ Frame(CancelPrimary(c))
       \/ IsaLand(c) \/ IsaLandEvent(c) \/ RecordResult(c) \/ DoubleLand(c)
  \/ \E c \in Cards, k \in Consumers : Deal(c, k) \/ Frame(DealRead(c, k))
  \/ \E i \in CopyId :
       Frame(Work(i)) \/ Frame(Beat(i)) \/ Frame(Lapse(i)) \/ Frame(Expire(i))
       \/ Frame(DownReady(i)) \/ Frame(GiveBack(i)) \/ Frame(EndFail(i))
       \/ Frame(EndWorkPR(i)) \/ Frame(EndWorkDone(i)) \/ Frame(EndReadHigh(i))
       \/ Frame(EndReadStale(i)) \/ Frame(EndReadLow(i)) \/ Frame(EndReadFail(i))
       \/ Frame(EndFixOK(i))
  \/ \E k \in Consumers : Frame(Down(k)) \/ Frame(Up(k))
  \/ CoordRelease \/ Extern

IsaNext == S(IsaMove) \/ \E c \in Cards : Clock(c)

\* Fairness as CardMachine's, on the actions this module uses: the duties and
\* the workers, and the coordinator's release. The wait is strongly fair too,
\* so a card whose operand holds is not starved of its release.
IsaFairness ==
  /\ \A c \in Cards : WF_avars(S(Wait(c)))
  /\ WF_avars(S(CoordRelease)) /\ WF_avars(S(Extern))

IsaSpec == IsaInit /\ [][IsaNext]_avars /\ IsaFairness

----------------------------------------------------------------------------
(* What must always hold *)

\* no card lands twice: the land count never passes one
NoCardRetiresTwice == \A c \in Cards : lands[c] <= 1

\* a landed card's head is an ancestor of its base: in this linear model the
\* head is at or behind the base, never past it
RetiredHeadOnBase == \A c \in Cards : where[c] = "landed" => head[c] <= base[c]

\* a wait card is never dispatched before its operand holds: once it has been
\* released for a consumer (ready or working), its operand holds. The operands
\* only grow, so a card released by Wait keeps the property. A card the
\* coordinator cancels or a landing event walks out of waiting is not a
\* dispatch, so it is not covered here.
WaitDispatchesOnlyWhenOperandHolds ==
  \A c \in WaitCards : where[c] \in {"ready", "working"} => OperandHolds(c)

\* the stall reasons partition wall time: one reason at a time, the card's
\* place's, and the seconds charged to the reasons sum to its wall time
Spent(c) ==
  LET p == spent[c] IN
  p["need"] + p["release"] + p["external"] + p["rework"] + p["slot"]
    + p["work"] + p["read"] + p["merge"] + p["none"]
StallsPartitionWallTime ==
  \A c \in Cards : stall[c] = StallOf(c) /\ Spent(c) = wall[c]

IsaSafety == /\ IsaTypeOK /\ Safety /\ NoCardRetiresTwice /\ RetiredHeadOnBase
              /\ WaitDispatchesOnlyWhenOperandHolds /\ StallsPartitionWallTime

----------------------------------------------------------------------------
(* What must eventually happen: a wait whose operand comes to hold is dealt *)

\* dealt: it leaves waiting and moves on (ready, working, or a terminal word)
Dealt(c) == where[c] \in {"ready", "working", "review", "merging", "landed", "done"}
WaitDealt == \A c \in WaitCards : (OperandHolds(c) /\ where[c] = "waiting") ~> Dealt(c)

=============================================================================
