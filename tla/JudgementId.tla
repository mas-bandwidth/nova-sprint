----------------------------- MODULE JudgementId -----------------------------
(***************************************************************************)
(* The identity of a judgement: kind.about.episode (modules/steps/judge.go *)
(* lastEpisodes, nextEpisode, judgeAt, put, judgements and lapse). This is *)
(* the layout model beside Steps.tla, whose judgements are a set of        *)
(* [kind, about] with no episode: there a judgement that expires and       *)
(* reopens is the same element, so it cannot say which id the reopened     *)
(* episode takes, and it passed while the code named an id that existed.   *)
(*                                                                         *)
(* One kind about one thing. js is its judgements as the store holds       *)
(* them: an episode number, a state, and the count the question reports    *)
(* (cnt; 0 for a kind no count names). The store keeps one judgement per   *)
(* id and lets an op write a judgement once: an op that writes one twice   *)
(* is invalid (the tick ends there, with nothing after it applied), and    *)
(* a put of an id that exists is refused. Either way the op is not         *)
(* applied, and the next tick plans it again.                              *)
(*                                                                         *)
(* Two kinds of cause, chosen by ByCount:                                  *)
(*   TRUE   a count that only grows (a row's flaps), detected from Limit   *)
(*          on. Each count is asked once (judgeAt): as the episode         *)
(*          max(count, next), never an id that exists, closing every open  *)
(*          episode before it in the same op.                              *)
(*   FALSE  a cause a step detects or not (judge): with none open it opens *)
(*          the next episode; one no longer detected expires.              *)
(* For both, the deadline of an open judgement whose cause is still        *)
(* detected expires it and reopens it as the next episode, in one op       *)
(* (lapse). The next episode is one past the highest episode there is      *)
(* (NextEp), whoever names it.                                             *)
(*                                                                         *)
(* A tick leaves at most one episode open: every open one but the latest   *)
(* expires as superseded, and so does the latest when the tick opens       *)
(* another. So several open ones past their deadline (a store written      *)
(* before this rule: Stores) reopen as one.                                *)
(*                                                                         *)
(* Variant is the planner. "fixed" is the code. The reversed witnesses     *)
(* are cfgs on this module:                                                *)
(*   "by-count"    the first planner: the next episode is the number of    *)
(*                 judgements plus one, a count names its own episode, and *)
(*                 every open one past its deadline reopens. With ids      *)
(*                 that do not run from 1 (a count's own) the reopened id  *)
(*                 exists: NoDoubleWrite, and DueIsServed (the tick is     *)
(*                 thrown away for ever), must be violated.                *)
(*   "by-highest"  the next episode one past the highest, a count still    *)
(*                 naming its own episode, every open one past its         *)
(*                 deadline still reopening: two of them put one new id    *)
(*                 (NoDoubleWrite must be violated), and a count whose     *)
(*                 number a reopened episode took is never asked           *)
(*                 (CountIsAsked must be violated).                        *)
(* JudgementIdTrace.tla binds the code to this model                       *)
(* (modules/steps/episodes_trace_test.go).                                 *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS ByCount,   \* the cause is a count (TRUE) or a step's detection (FALSE)
          Limit,     \* the count from which the cause is detected
          MaxCount,  \* bound: the count
          MaxEnv,    \* bound: the environment's moves
          Variant    \* the planner: "fixed", or a reversed witness

VARIABLES js,      \* the judgements: a set of [ep, st, cnt]
          count,   \* the count (a row's flaps)
          cause,   \* a step detects the cause (ByCount = FALSE)
          due,     \* the open judgements' deadline has passed
          last,    \* the last tick's op: what it was, for the invariants
          ticked,  \* the tick ran since the last environment move
          env

vars == <<js, count, cause, due, last, ticked, env>>

J(e, s, c) == [ep |-> e, st |-> s, cnt |-> c]

\* Stores a planner may be started on: empty; ids from 1; a count's own ids
\* (3, 4, 5 at 5 flaps), all open; the same with one answered and one
\* expired; and ids with a gap. The first field is the count.
Stores ==
  IF ByCount
    THEN {<<0, {}>>,
          <<5, {J(3, "open", 3), J(4, "open", 4), J(5, "open", 5)}>>,
          <<5, {J(3, "expired", 3), J(4, "answered", 4), J(5, "open", 5)}>>,
          <<4, {J(3, "open", 3), J(4, "answered", 4)}>>}
    ELSE {<<0, {}>>,
          <<0, {J(1, "expired", 0), J(2, "open", 0)}>>,
          <<0, {J(1, "open", 0), J(4, "open", 0), J(7, "open", 0)}>>,
          <<0, {J(2, "answered", 0), J(5, "open", 0)}>>}

Clean == [bad |-> "no", lapsed |-> FALSE, put |-> FALSE, superseded |-> FALSE, past |-> FALSE]

Init ==
  /\ \E s \in Stores : count = s[1] /\ js = s[2]
  /\ cause \in (IF ByCount THEN {FALSE} ELSE BOOLEAN)
  /\ due = FALSE /\ last = Clean /\ ticked = FALSE /\ env = 0

Eps == {j.ep : j \in js}
Max(S) == CHOOSE x \in S : \A y \in S : y <= x
Highest == IF Eps = {} THEN 0 ELSE Max(Eps)
Open == {j \in js : j.st = "open"}
Latest == CHOOSE j \in Open : \A k \in Open : k.ep <= j.ep
Detected == IF ByCount THEN count >= Limit ELSE cause

\* The next episode: one past the highest (lastEpisodes, nextEpisode). The
\* first planner counted the judgements.
NextEp == IF Variant = "by-count" THEN Cardinality(js) + 1 ELSE Highest + 1

\* A count is asked once. The code knows it by the question (judgeAt's
\* asked); the planners before it by the id the count names.
Asked == IF Variant = "fixed" THEN \E j \in js : j.cnt = count ELSE count \in Eps

\* judgeAt: the count's judgement, the episode it takes, and the open ones
\* it closes in its op.
RaiseC ==
  /\ ByCount /\ count >= Limit /\ ~Asked
  /\ Variant = "by-highest" => ~\E j \in Open : j.ep >= count
RaiseCEp == IF Variant = "fixed" /\ NextEp > count THEN NextEp ELSE count
RaiseCExp == CASE ~RaiseC                 -> {}
               [] Variant = "fixed"       -> Open
               [] Variant = "by-highest"  -> {j \in Open : j.ep < count}
               [] OTHER                   -> {}

\* judge: a detected cause with none open opens the next episode.
RaiseE == ~ByCount /\ cause /\ Open = {}
Pending == RaiseC \/ RaiseE

\* judgements and lapse, over the open ones no raise closed.
Rest == Open \ RaiseCExp
Cleared == {j \in Rest : ~Detected}
Superseded ==
  IF Variant = "fixed"
    THEN {j \in Rest \ Cleared : j # Latest \/ Pending}
    ELSE {}
Reopened == {j \in Rest \ (Cleared \cup Superseded) : due}

\* The op: its puts (src tells two puts of one episode apart) and its
\* expiries, one per judgement (the plan keeps them by id).
Puts ==
  (IF RaiseC THEN {[src |-> 0, ep |-> RaiseCEp, cnt |-> count]} ELSE {})
    \cup (IF RaiseE THEN {[src |-> 0, ep |-> NextEp, cnt |-> 0]} ELSE {})
    \cup {[src |-> j.ep, ep |-> NextEp, cnt |-> j.cnt] : j \in Reopened}
Expires == RaiseCExp \cup Cleared \cup Superseded \cup Reopened

\* The store's two rules: one write to a judgement in an op (its check: the
\* op is invalid), and no put of an id that exists (refused).
Double ==
  \/ \E p, q \in Puts : p # q /\ p.ep = q.ep
  \/ \E p \in Puts : \E j \in Expires : j.ep = p.ep
Exists == \E p \in Puts : p.ep \in Eps
Bad == IF Double THEN "double" ELSE IF Exists THEN "exists" ELSE "no"

Tick ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ last' = [bad |-> Bad, lapsed |-> Detected /\ Reopened # {}, put |-> Puts # {},
              superseded |-> Superseded # {}, past |-> RaiseC /\ RaiseCEp > count]
  /\ IF Bad = "no"
       THEN /\ js' = {IF j \in Expires THEN [j EXCEPT !.st = "expired"] ELSE j : j \in js}
                       \cup {J(p.ep, "open", p.cnt) : p \in Puts}
            /\ due' = FALSE   \* what was due is closed; a put has a new deadline
       ELSE UNCHANGED <<js, due>>
  /\ UNCHANGED <<count, cause, env>>

Env == env < MaxEnv /\ env' = env + 1 /\ ticked' = FALSE /\ UNCHANGED last

Flap == Env /\ ByCount /\ count < MaxCount /\ count' = count + 1 /\ UNCHANGED <<js, cause, due>>
Cause == Env /\ ~ByCount /\ cause' = ~cause /\ UNCHANGED <<js, count, due>>
\* The coordinator answers any open one, the latest or not.
Answer(j) ==
  /\ Env /\ j \in Open
  /\ js' = (js \ {j}) \cup {[j EXCEPT !.st = "answered"]}
  /\ due' = (due /\ Open # {j})
  /\ UNCHANGED <<count, cause>>
Deadline == Env /\ Open # {} /\ ~due /\ due' = TRUE /\ UNCHANGED <<js, count, cause>>

Next == Tick \/ Flap \/ Cause \/ Deadline \/ \E j \in js : Answer(j)

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

-----------------------------------------------------------------------------
TypeOK ==
  /\ \A j \in js : j.ep \in Nat \ {0} /\ j.st \in {"open", "answered", "expired"} /\ j.cnt \in Nat
  /\ count \in Nat /\ cause \in BOOLEAN /\ due \in BOOLEAN /\ ticked \in BOOLEAN
  /\ last.bad \in {"no", "double", "exists"}

\* One judgement per id.
IdsUnique == \A j, k \in js : j.ep = k.ep => j = k

\* No op writes one judgement twice (the store's check would throw the
\* tick away), and no put names an id that exists (refused each tick).
NoDoubleWrite == last.bad # "double"
PutIsFresh == last.bad # "exists"

\* After a tick at most one episode is open.
OneOpenAfterTick == ticked => Cardinality(Open) <= 1

\* A deadline never closes silently what still holds: the op that expires
\* it opens its next episode.
LapseNeverSilent == last.lapsed => last.put

\* Every count from the limit on is asked within a tick, though a reopened
\* episode holds the count's own number.
CountIsAsked == ByCount /\ ticked /\ count >= Limit => \E j \in js : j.cnt = count

\* A detected cause is open, or was answered or asked, after a tick.
CauseIsOpen == ~ByCount /\ ticked /\ cause => Open # {}

\* Liveness: a deadline that passed is served: the judgement is open again
\* as its next episode, superseded or cleared, never due for ever.
DueIsServed == due ~> ~due

\* Reach (expected violated, each in its own cfg): a lapse reopens; an open
\* episode is superseded; a count is asked past its own number.
NeverReopens == ~last.lapsed
NeverSuperseded == ~last.superseded
NeverPastCount == ~last.past
=============================================================================
