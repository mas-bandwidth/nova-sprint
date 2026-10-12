----------------------------- MODULE DownRecovery -----------------------------
\* Protocol interface model: lease ordinals, not literal production generation increments.
\* Reserve linearizes a durable operation; Apply represents fenced replay/release.
\* Git Publish remains possible on an old branch after reservation and resolution.
EXTENDS Naturals, FiniteSets, TLC
CONSTANT Broken, Pinned
Friends == {"a", "b"}
Gens == 1..3
Heads == 0..2
VARIABLES phase, gen, owner, work, reader, readGen, reviewOwed, spent,
          plan, pending, debt, sample, carry, refs, admitted, finishes, reads,
          reserves, resolved, launch, retirements
vars == <<phase, gen, owner, work, reader, readGen, reviewOwed, spent,
          plan, pending, debt, sample, carry, refs, admitted, finishes, reads,
          reserves, resolved, launch, retirements>>
Up(f) == f = "b" \/ phase \in {0, 2}
Active == work \in {"ready", "running"}
NoPlan == [kind |-> "none", rev |-> 0, gen |-> 0]
NoDebt == [old |-> 0, current |-> 0, repo |-> "r", branch |-> 0]
NoSample == [old |-> 0, current |-> 0, repo |-> "r", branch |-> 0, head |-> 0]
Init == /\ phase = 0 /\ gen = 1 /\ owner = "a"
        /\ work \in {"ready", "running"} /\ reader = "a" /\ readGen = 1
        /\ reviewOwed = TRUE /\ spent = 0
        /\ plan = NoPlan /\ pending = NoPlan /\ debt = NoDebt
        /\ sample = NoSample /\ carry = <<0, 0>> /\ refs = [g \in Gens |-> 0]
        /\ admitted = {} /\ finishes = {} /\ reads = {} /\ reserves = {} /\ resolved = {}
        /\ launch = <<1, 1>> /\ retirements = {}
Evidence == /\ phase < 3 /\ phase' = phase + 1
            /\ UNCHANGED <<gen, owner, work, reader, readGen, reviewOwed, spent,
               plan, pending, debt, sample, carry, refs, admitted, finishes, reads,
               reserves, resolved, launch, retirements>>
Plan(k) == /\ pending.kind = "none" /\ ~Up("a")
           /\ ((Active /\ owner = "a" /\ gen < 3) \/ reader = "a")
           /\ plan' = [kind |-> k, rev |-> phase, gen |-> gen]
           /\ UNCHANGED <<phase, gen, owner, work, reader, readGen, reviewOwed,
              spent, pending, debt, sample, carry, refs, admitted, finishes, reads,
              reserves, resolved, launch, retirements>>
Reserve == /\ pending.kind = "none" /\ plan.kind # "none"
           /\ plan.gen = gen
           /\ ((Broken = "acquire" /\ plan.kind = "acquire") \/
               (Broken = "relock" /\ plan.kind = "relock") \/
               (plan.rev = phase /\ ~Up("a")))
           /\ pending' = plan /\ plan' = NoPlan
           /\ reserves' = reserves \cup {<<plan.kind, plan.rev, phase, Up("a")>>}
           /\ UNCHANGED <<phase, gen, owner, work, reader, readGen, reviewOwed,
              spent, debt, sample, carry, refs, admitted, finishes, reads, resolved,
              launch, retirements>>
Apply == /\ pending.kind # "none"
         /\ LET revoke == Active /\ owner = "a" /\ gen < 3 IN
            /\ gen' = IF revoke THEN gen + 1 ELSE gen
            /\ work' = IF revoke THEN "withdrawn" ELSE work
            /\ owner' = IF revoke THEN "pool" ELSE owner
            /\ debt' = IF revoke THEN [old |-> gen, current |-> gen+1,
                           repo |-> "r", branch |-> gen] ELSE debt
         /\ reader' = IF reader = "a" THEN "pool" ELSE reader
         /\ readGen' = IF reader = "a" /\ Broken # "readgen" THEN readGen + 1 ELSE readGen
         /\ reviewOwed' = IF Broken = "review" THEN FALSE ELSE reviewOwed
         /\ retirements' = IF reader = "a" THEN retirements \cup {<<readGen, IF Broken = "readgen" THEN readGen ELSE readGen+1>>}
                           ELSE retirements
         /\ spent' = IF Broken = "spend" THEN spent + 1 ELSE spent
         /\ pending' = NoPlan
         /\ UNCHANGED <<phase, plan, sample, carry, refs, admitted,
                        finishes, reads, reserves, resolved, launch>>
ReadHead == /\ pending.kind = "none" /\ debt.old # 0
            /\ reads' = reads \cup {<<refs[debt.branch], debt.old>>}
            /\ sample' = [old |-> debt.old, current |-> debt.current,
                           repo |-> debt.repo, branch |-> debt.branch,
                           head |-> refs[debt.branch]]
            /\ UNCHANGED <<phase, gen, owner, work, reader, readGen, reviewOwed,
               spent, plan, pending, debt, carry, refs, admitted, finishes,
               reserves, resolved, launch, retirements>>
ReadFailure == /\ debt.old # 0 /\ pending.kind = "none"
               /\ IF Broken = "readerror"
                      THEN /\ debt' = NoDebt /\ carry' = <<0, debt.old>>
                           /\ resolved' = resolved \cup {<<0, debt.old, 0, gen, 0, carry, <<0, debt.old>>>>}
                           /\ UNCHANGED <<phase, gen, owner, work, reader, readGen,
                              reviewOwed, spent, plan, pending, sample, refs,
                              admitted, finishes, reads, reserves, launch, retirements>>
                      ELSE UNCHANGED vars
NewCarry == IF Broken = "wrongcarry" THEN carry
            ELSE IF sample.head = 0 /\ (Broken # "emptycarry" \/ carry[1] = 0) THEN carry
            ELSE <<sample.head, sample.old>>
Resolve == /\ pending.kind = "none" /\ debt.old # 0 /\ sample.old # 0
           /\ (Broken = "identity" \/
               (sample.old = debt.old /\ sample.current = gen /\
                sample.branch = debt.branch /\ sample.repo = debt.repo))
           /\ resolved' = resolved \cup {<<sample.old, debt.old, sample.current, gen, sample.head, carry, NewCarry>>}
           /\ carry' = NewCarry /\ debt' = NoDebt
           /\ UNCHANGED <<phase, gen, owner, work, reader, readGen, reviewOwed,
              spent, plan, pending, sample, refs, admitted, finishes, reads, reserves,
              launch, retirements>>
Publish(g) == /\ g <= launch[1] /\ refs[g] < 2 /\ refs' = [refs EXCEPT ![g] = @ + 1]
              /\ UNCHANGED <<phase, gen, owner, work, reader, readGen, reviewOwed,
                 spent, plan, pending, debt, sample, carry, admitted, finishes, reads,
                 reserves, resolved, launch, retirements>>
Admit(f) == /\ pending.kind = "none" /\ work = "withdrawn" /\ gen < 3
            /\ Up(f) /\ (Broken = "pin" \/ ~Pinned \/ f = "a")
            /\ (Broken = "debt" \/ debt.old = 0)
            /\ admitted' = admitted \cup {<<debt.old, f, Up(f)>>}
            /\ owner' = f /\ work' = "running"
            /\ launch' = <<gen, IF Broken = "branch" THEN 1 ELSE gen>>
            /\ UNCHANGED <<phase, gen, reader, readGen, reviewOwed, spent, plan,
               pending, debt, sample, carry, refs, finishes, reads, reserves, resolved,
               retirements>>
Finish(g) == /\ g <= launch[1]
             /\ (pending.kind = "none" \/ Broken = "pending") /\ Active
             /\ (Broken = "generation" \/ g = gen)
             /\ finishes' = finishes \cup {<<g, IF pending.kind = "none" THEN gen ELSE gen+1>>} /\ work' = "finished"
             /\ UNCHANGED <<phase, gen, owner, reader, readGen, reviewOwed, spent,
                plan, pending, debt, sample, carry, refs, admitted, reads, reserves,
                resolved, launch, retirements>>
Quiesce == /\ pending.kind = "none" /\ debt.old = 0
            /\ ~(Active /\ owner = "a" /\ ~Up("a"))
            /\ ~(reader = "a" /\ ~Up("a")) /\ UNCHANGED vars
Next == Quiesce \/ Evidence \/ Plan("acquire") \/ Plan("relock") \/ Reserve \/ Apply \/
        ReadHead \/ ReadFailure \/ Resolve \/ (\E g \in Gens : Publish(g) \/ Finish(g)) \/
        (\E f \in Friends : Admit(f))
Spec == Init /\ [][Next]_vars /\ WF_vars(Plan("acquire")) /\ WF_vars(Reserve)
        /\ WF_vars(Apply) /\ WF_vars(ReadHead) /\ WF_vars(Resolve)
TypeOK == /\ phase \in 0..3 /\ gen \in Gens /\ refs \in [Gens -> Heads]
          /\ owner \in Friends \cup {"pool"} /\ reader \in {"a", "pool"}
AtomicEvidence == \A r \in reserves : r[2] = r[3] /\ ~r[4]
OldGenCannotFinish == \A r \in finishes : r[1] = r[2]
DebtBlocksAdmission == \A r \in admitted : r[1] = 0
PinPreserved == \A r \in admitted : ~Pinned \/ r[2] = "a"
NoAdmissionDown == \A r \in admitted : r[3]
ExactDebtIdentity == \A r \in resolved : r[1] = r[2] /\ r[3] = r[4]
CarryConserved == \A r \in resolved :
                  IF r[5] = 0 THEN r[7] = r[6] ELSE r[7] = <<r[5], r[1]>>
SeparateLaunchBranch == launch[1] = launch[2]
AwaySpendsNothing == spent = 0
CarryIsSuccessfulSnapshot == carry = <<0, 0>> \/ carry \in reads
ReaderGenerationRevoked == \A r \in retirements : r[2] = r[1]+1
ReviewStillOwed == reviewOwed
DebtEventuallyResolved == (debt.old # 0) ~> (debt.old = 0)
PersistentDownRevoked == (phase = 3) ~> (~(Active /\ owner = "a") /\ reader = "pool")
ReservationFinishes == (pending.kind # "none") ~> (pending.kind = "none")
NeverRecovered == ~(gen = 2 /\ work = "running" /\ debt.old = 0 /\ carry[1] # 0)
NeverLatePush == ~(gen >= 2 /\ carry[2] = 1 /\ refs[1] > carry[1] /\ carry[1] > 0)
=============================================================================
