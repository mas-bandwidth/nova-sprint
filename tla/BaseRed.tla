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
(* Tick is the steps' baseRed and its clear, one atomic step: red with     *)
(* live cards opens one judgement, or keeps the open one with the same red *)
(* set, or expires the open one and opens the next episode in the same op  *)
(* when the red set changed; an answered one with the same red set stays   *)
(* quiet until its quiet ends (the judgement timeout), then opens again;   *)
(* green expires the open one. Lapse is the open one's deadline: reopened  *)
(* (escalated) when the tick still detects its cause, else closed as       *)
(* cleared. Answer is the coordinator's retry. The landing, the deal and   *)
(* the streams are untouched: nothing here moves a card.                   *)
(*                                                                         *)
(* Reversed witness beside it: BaseRedNoPush.tla (the old behaviour: a red *)
(* base, no judgement) must violate RedIsPushed.                           *)
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
          ticked,   \* the tick ran since the last environment move
          env

vars == <<fact, live, js, quiet, ticked, env>>

Init == /\ fact = "none" /\ live \in BOOLEAN /\ js = <<>> /\ quiet = FALSE /\ ticked = FALSE /\ env = 0

Last == IF js = <<>> THEN [set |-> "none", st |-> "none"] ELSE js[Len(js)]
Opens == {i \in 1..Len(js) : js[i].st = "open"}
Red == fact \in Sets

Expire == [js EXCEPT ![Len(js)].st = "expired"]
Open(s) == <<[set |-> s, st |-> "open"]>>

\* The steps' tick: baseRed, then the clear.
Tick ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ IF Red /\ live
       THEN CASE Last.st = "open" /\ Last.set = fact -> UNCHANGED js            \* held: the same red set
              [] Last.st = "open" -> js' = Expire \o Open(fact)                  \* a changed red set: next episode
              [] Last.st = "answered" /\ Last.set = fact /\ quiet -> UNCHANGED js \* answered: quiet
              [] OTHER -> js' = js \o Open(fact)                                 \* opened (or STILL RED)
     ELSE IF fact = "green" /\ Last.st = "open"
       THEN js' = Expire                                                         \* cleared: green
       ELSE UNCHANGED js
  /\ UNCHANGED <<fact, live, quiet, env>>

Env == env < MaxEnv /\ env' = env + 1 /\ ticked' = FALSE

\* Layer 6 reads the tip: a completed read is the fact; pending is none.
Read(r) == /\ Env
           /\ fact' = IF r = "pending" THEN fact ELSE r
           /\ UNCHANGED <<live, js, quiet>>

Cards == /\ Env /\ live' = ~live /\ UNCHANGED <<fact, js, quiet>>

Answer == /\ Env /\ Last.st = "open"
          /\ js' = [js EXCEPT ![Len(js)].st = "answered"] /\ quiet' = TRUE
          /\ UNCHANGED <<fact, live>>

QuietEnds == /\ Env /\ quiet /\ quiet' = FALSE /\ UNCHANGED <<fact, live, js>>

\* The open one's deadline: reopened escalated while the tick detects it.
Lapse == /\ Env /\ Last.st = "open"
         /\ js' = IF Red /\ live /\ Last.set = fact THEN Expire \o Open(fact) ELSE Expire
         /\ UNCHANGED <<fact, live, quiet>>

Next == Tick \/ (\E r \in Reads : Read(r)) \/ Cards \/ Answer \/ QuietEnds \/ Lapse

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

-----------------------------------------------------------------------------
TypeOK == /\ fact \in Facts /\ live \in BOOLEAN /\ quiet \in BOOLEAN /\ ticked \in BOOLEAN
          /\ \A i \in 1..Len(js) : js[i].set \in Sets /\ js[i].st \in {"open", "answered", "expired"}

\* At most one open base-red per repository and base.
OneOpen == Cardinality(Opens) <= 1

\* A red completed base tip with live cards has an open base-red judgement
\* within a tick (unless the coordinator answered that same red set and its
\* quiet has not ended).
RedIsPushed == ticked /\ Red /\ live =>
                 \/ Last.st = "open" /\ Last.set = fact
                 \/ Last.st = "answered" /\ Last.set = fact /\ quiet

\* Pending checks, a rested forge and a failed read never open one: with no
\* completed read there is none.
NoReadNoJudgement == fact = "none" => js = <<>>

\* A green tip leaves none open after the tick.
GreenNoneOpen == ticked /\ fact = "green" => Opens = {}

\* Liveness: a green tip clears it.
GreenClears == [](fact = "green" => <>(Opens = {} \/ fact # "green"))

\* Reach (expected violated in BaseRedNever*.cfg): a judgement opens, and a
\* second episode opens (a changed red set, or an escalation).
NeverOpens == js = <<>>
NeverReplaced == Len(js) < 2
=============================================================================
