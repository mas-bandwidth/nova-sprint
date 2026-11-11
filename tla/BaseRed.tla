------------------------------ MODULE BaseRed ------------------------------
(***************************************************************************)
(* A red base wakes the coordinator (2026-11-10; modules/steps/basered.go, *)
(* modules/run/basewatch.go). One repository and base, its streams' live  *)
(* cards, and the base-red judgements about it, episode by episode.        *)
(*                                                                         *)
(* fact is the last completed read of the base tip's checks that layer 6   *)
(* passes the tick (Inputs.Bases): none yet, green, or red with a red set  *)
(* (the failing checks; another tip with the same checks red is the same   *)
(* red set). A read of running checks, a rate-limited forge or a failed    *)
(* read is no fact: it leaves fact as it was (Read with "pending").        *)
(*                                                                         *)
(* Tick is one run tick (steps.judgements: baseRed, then each open         *)
(* judgement's clear or deadline), one atomic step: red with live cards    *)
(* opens one judgement, or keeps the open one with the same red set (and   *)
(* past its deadline reopens it, escalated), or expires the open one and   *)
(* opens the next episode in the same op when the red set changed; an      *)
(* answered one with the same red set stays quiet until its quiet ends     *)
(* (the judgement timeout), then opens again, unless the red stretch broke *)
(* since the answer (broke: a green read, or a red read that began a new   *)
(* stretch, which the watch dates after the answer), when the same red set *)
(* opens again at once; green expires the open one,                        *)
(* and so does its deadline when no step detects its cause. Lapse is the   *)
(* clock passing the open one's deadline (due), which the next tick acts   *)
(* on. Answer is the coordinator's retry. The landing, the deal and the    *)
(* streams are untouched: nothing here moves a card.                       *)
(*                                                                         *)
(* Reversed witnesses beside it: BaseRedNoPush.tla (the old behaviour: a   *)
(* red base, no judgement) and BaseRedQuietSwallows.tla (the first quiet:  *)
(* green then the same red again inside the timeout stayed unpushed) must  *)
(* violate RedIsPushed. BaseRedTrace.tla binds                             *)
(* the code to this model (modules/run/basered_trace_test.go).             *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS MaxEnv   \* environment moves at most (reads, cards, answers, deadlines)

Sets  == {"A", "B"}                 \* two red sets
Facts == {"none", "green"} \cup Sets
Reads == {"pending", "green"} \cup Sets

VARIABLES fact,     \* the last completed read (Inputs.Bases)
          live,     \* the base's streams have live producers
          js,       \* the base-red episodes: [set, st] with st open, answered or expired
          quiet,    \* the last answer's quiet (the judgement timeout) has not ended
          broke,    \* the red stretch broke since the last answer
          due,      \* the clock passed the open one's deadline
          ticked,   \* the tick ran since the last environment move
          env

vars == <<fact, live, js, quiet, broke, due, ticked, env>>

Init == /\ fact = "none" /\ live \in BOOLEAN /\ js = <<>> /\ quiet = FALSE /\ broke = FALSE /\ due = FALSE
        /\ ticked = FALSE /\ env = 0

Last == IF js = <<>> THEN [set |-> "none", st |-> "none"] ELSE js[Len(js)]
Opens == {i \in 1..Len(js) : js[i].st = "open"}
Red == fact \in Sets

Expire == [js EXCEPT ![Len(js)].st = "expired"]
Open(s) == <<[set |-> s, st |-> "open"]>>

\* The answered one is quiet: its red set, its quiet running, its stretch unbroken.
Quiet == Last.st = "answered" /\ Last.set = fact /\ quiet /\ ~broke

\* One tick: baseRed, then the open one's clear or deadline.
Tick ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ due' = FALSE
  /\ IF Red /\ live
       THEN CASE Last.st = "open" /\ Last.set = fact /\ due -> js' = Expire \o Open(fact)  \* still red at its deadline: reopened, escalated
              [] Last.st = "open" /\ Last.set = fact /\ ~due -> UNCHANGED js           \* held: the same red set
              [] Last.st = "open" /\ Last.set # fact -> js' = Expire \o Open(fact)     \* a changed red set: next episode
              [] Quiet -> UNCHANGED js                                                 \* answered: quiet
              [] ~(Last.st = "open" \/ Quiet)
                   -> js' = js \o Open(fact)                                           \* opened (or STILL RED)
     ELSE IF Last.st = "open" /\ (fact = "green" \/ due)
       THEN js' = Expire                     \* cleared: green, or its deadline with no step detecting it
       ELSE UNCHANGED js
  /\ UNCHANGED <<fact, live, quiet, broke, env>>

Env == env < MaxEnv /\ env' = env + 1 /\ ticked' = FALSE

\* Layer 6 reads the tip: a completed read is the fact; pending is none. A
\* green read, or a red read after no red (the watch begins a new stretch,
\* dated now), breaks an answered one's stretch.
Read(r) == /\ Env
           /\ fact' = IF r = "pending" THEN fact ELSE r
           /\ broke' = (broke \/ (Last.st = "answered" /\ (r = "green" \/ (r \in Sets /\ fact \notin Sets))))
           /\ UNCHANGED <<live, js, quiet, due>>

Cards == /\ Env /\ live' = ~live /\ UNCHANGED <<fact, js, quiet, broke, due>>

Answer == /\ Env /\ Last.st = "open"
          /\ js' = [js EXCEPT ![Len(js)].st = "answered"] /\ quiet' = TRUE /\ broke' = FALSE /\ due' = FALSE
          /\ UNCHANGED <<fact, live>>

QuietEnds == /\ Env /\ quiet /\ quiet' = FALSE /\ UNCHANGED <<fact, live, js, broke, due>>

\* Layer 6 stops watching a base with no live cards and forgets its read
\* (a stale red never comes back after a gap); the open one stays until
\* its deadline.
Unwatch == /\ ~live /\ fact # "none" /\ fact' = "none"
           /\ UNCHANGED <<live, js, quiet, broke, due, ticked, env>>

\* The clock passes the open one's deadline; the next tick acts on it.
Lapse == /\ Env /\ Last.st = "open" /\ ~due /\ due' = TRUE
         /\ UNCHANGED <<fact, live, js, quiet, broke>>

Next == Tick \/ (\E r \in Reads : Read(r)) \/ Cards \/ Unwatch \/ Answer \/ QuietEnds \/ Lapse

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

-----------------------------------------------------------------------------
TypeOK == /\ fact \in Facts /\ live \in BOOLEAN /\ quiet \in BOOLEAN /\ broke \in BOOLEAN /\ due \in BOOLEAN
          /\ ticked \in BOOLEAN
          /\ \A i \in 1..Len(js) : js[i].set \in Sets /\ js[i].st \in {"open", "answered", "expired"}

\* At most one open base-red per repository and base.
OneOpen == Cardinality(Opens) <= 1

\* A red completed base tip with live cards has an open base-red judgement
\* within a tick, unless the coordinator answered that same red set, its
\* quiet has not ended, and the base stayed red since: a base that went
\* green and red again is pushed.
RedIsPushed == ticked /\ Red /\ live =>
                 \/ Last.st = "open" /\ Last.set = fact
                 \/ Quiet

\* Only a red completed read with live cards opens one: pending checks, a
\* rested forge, a failed read and a forgotten base never do.
OpensOnlyOnRed == [][Len(js') > Len(js) => fact' \in Sets /\ live']_vars

\* A green tip leaves none open after the tick.
GreenNoneOpen == ticked /\ fact = "green" => Opens = {}

\* Liveness: a green tip clears it.
GreenClears == [](fact = "green" => <>(Opens = {} \/ fact # "green"))

\* Reach (expected violated in BaseRedNever*.cfg): a judgement opens, and a
\* second episode opens (a changed red set, or an escalation).
NeverOpens == js = <<>>
NeverReplaced == Len(js) < 2
=============================================================================
