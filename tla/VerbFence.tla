----------------------------- MODULE VerbFence -----------------------------
(***************************************************************************)
(* A coordinator verb on a store others write (layer 6: modules/verbs,     *)
(* apply.go fenced, moves.go move and targetChange), shown on replace:     *)
(* `nova-sprint replace old --by new` fails the old card and relinks its   *)
(* dependant to the new one, in one op planned on the verb's read and      *)
(* fenced on the gens of the cards it read (card.Replace fences new's gen  *)
(* so that it is still not final (a held card is live) at the commit; the  *)
(* dependant's Relink fences its own and new's).                           *)
(*                                                                         *)
(* Other writers move the store between the verb's read and its commit:    *)
(* the run deals the new card (live 2026-11-10: `add` then `replace`, the  *)
(* deal inside the window, "card new is at gen 2, expected gen 1"), lands  *)
(* or holds it, deals the old card, or writes a card the verb names        *)
(* nowhere (x). With Coarse the op is also fenced on every card, as hold   *)
(* of a stream is (UnchangedSince cards): x's write refuses it (live       *)
(* 2026-11-10: "seq 95583 wrote cards" on a bench's report).               *)
(*                                                                         *)
(* The rule: a refused op is planned again on a fresh read, up to Tries    *)
(* applies in all, while the verb's own record (old, as first read) is as  *)
(* it was (OwnCheck); a change to it, or the plan refusing on the fresh    *)
(* read (new landed or failed), is the refusal. Tries 1 is the old rule:   *)
(* the first refusal is the verb's (witnesses VerbFenceOneTry*.cfg).       *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
  Tries,     \* applies in all (the code's fenceTries)
  MaxEnv,    \* model bound on the other writers' moves
  Coarse,    \* the op is fenced on every card too
  Writers,   \* which other writers move: a subset of WriterNames
  OwnCheck   \* a re-read refuses when the verb's own record changed

WriterNames == {"deal", "land", "hold", "old", "touch"}
Targets     == {"waiting", "ready", "working", "held"}   \* a card not final
NewStates   == Targets \cup {"landed", "fail"}
OldStates   == {"ready", "working", "fail"}

VARIABLES
  old,    \* the card replaced: [st, gen]
  new,    \* the card replacing it: [st, gen]
  dep,    \* old's dependant: [need: "old" or "new", gen]
  x,      \* the gen of a card the verb names nowhere
  pc,     \* the verb: "read" (to read), "planned", "done", "refused"
  snap,   \* the verb's last read: [old, new, dep, x]
  first,  \* old as the verb first read it: its own record
  tries,  \* applies so far
  env,    \* the other writers' moves so far
  at      \* ghost: new's state and old's gen at the commit

vars == <<old, new, dep, x, pc, snap, first, tries, env, at>>

Cur == [old |-> old, new |-> new, dep |-> dep, x |-> x]

Init ==
  /\ old = [st |-> "ready", gen |-> 1]
  /\ new \in [st : {"ready", "working", "held"}, gen : {1}]
  /\ dep = [need |-> "old", gen |-> 1]
  /\ x = 1
  /\ pc = "read"
  /\ snap = Cur
  /\ first = old
  /\ tries = 0
  /\ env = 0
  /\ at = [new |-> "none", old |-> 0]

\* The plan's precondition on a read (card.Replace, steps' dropMoves).
Pre(s) == s.old.st # "fail" /\ s.new.st \in Targets

\* The store's conditions on the op planned on snap.
Fenced ==
  /\ old.gen = snap.old.gen
  /\ new.gen = snap.new.gen
  /\ dep.gen = snap.dep.gen
  /\ Coarse => x = snap.x

\* The verb reads; a re-read is checked against its own record, then against
\* the tries left (the last refusal is checked for its own first), then planned.
Read ==
  /\ pc = "read"
  /\ snap' = Cur
  /\ first' = IF tries = 0 THEN old ELSE first
  /\ pc' = IF (OwnCheck /\ tries > 0 /\ old # first) \/ tries = Tries \/ ~Pre(Cur) THEN "refused" ELSE "planned"
  /\ UNCHANGED <<old, new, dep, x, tries, env, at>>

\* The op reaches the store: accepted (old fails, its dependant follows new,
\* in one step), or refused and read again.
Apply ==
  /\ pc = "planned"
  /\ tries' = tries + 1
  /\ IF Fenced
       THEN /\ old' = [st |-> "fail", gen |-> old.gen + 1]
            /\ dep' = [need |-> "new", gen |-> dep.gen + 1]
            /\ pc' = "done"
            /\ at' = [new |-> new.st, old |-> old.gen]
       ELSE /\ pc' = "read"
            /\ UNCHANGED <<old, dep, at>>
  /\ UNCHANGED <<new, x, snap, first, env>>

\* The other writers, while the verb is between its read and its end.
Other(w) == w \in Writers /\ env < MaxEnv /\ pc \in {"read", "planned"} /\ env' = env + 1
Deal  == Other("deal")  /\ new.st = "ready"   /\ new' = [st |-> "working", gen |-> new.gen + 1] /\ UNCHANGED <<old, x>>
Land  == Other("land")  /\ new.st = "working" /\ new' = [st |-> "landed", gen |-> new.gen + 1]  /\ UNCHANGED <<old, x>>
Hold  == Other("hold")  /\ new.st = "ready"   /\ new' = [st |-> "held", gen |-> new.gen + 1]    /\ UNCHANGED <<old, x>>
Moved == Other("old")   /\ old.st = "ready"   /\ old' = [st |-> "working", gen |-> old.gen + 1] /\ UNCHANGED <<new, x>>
Touch == Other("touch") /\ x' = x + 1 /\ UNCHANGED <<old, new>>
Env == (Deal \/ Land \/ Hold \/ Moved \/ Touch) /\ UNCHANGED <<dep, pc, snap, first, tries, at>>

Next == Read \/ Apply \/ Env

Spec == Init /\ [][Next]_vars /\ WF_vars(Read) /\ WF_vars(Apply)

-----------------------------------------------------------------------------
TypeOK ==
  /\ old \in [st : OldStates, gen : Nat]
  /\ new \in [st : NewStates, gen : Nat]
  /\ dep \in [need : {"old", "new"}, gen : Nat]
  /\ x \in Nat
  /\ pc \in {"read", "planned", "done", "refused"}
  /\ tries \in 0..Tries
  /\ env \in 0..MaxEnv
  /\ Writers \subseteq WriterNames

\* The replace done: the old card failed and its dependant follows the new.
Follows == pc = "done" => old.st = "fail" /\ dep.need = "new"

\* Nothing is orphaned: the old card fails only in the step its dependant
\* moves to the new one.
NoOrphan == dep.need = "old" => old.st # "fail"

\* The new card was not final at the commit: never a landed or failed
\* one.
TargetLive == pc = "done" => at.new \in Targets

\* The commit is of the old card as the verb first read it.
OwnKept == pc = "done" => at.old = first.gen

\* The verb refuses only for its own: its own record changed, or the plan
\* refused on its last read. Holds while Tries > MaxEnv (each refused apply
\* is one other writer's move).
NeverStuck == pc = "refused" => (old # first \/ ~Pre(snap))

\* The verb ends.
Ends == <>(pc \in {"done", "refused"})

\* Reach (expected violated in VerbFenceNever*.cfg): a refused op planned
\* again commits, and a dealt new card is replaced to.
NeverRetried == ~(pc = "done" /\ tries > 1)
NeverDealt   == ~(pc = "done" /\ at.new = "working")
=============================================================================
