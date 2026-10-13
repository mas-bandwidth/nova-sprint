------------------------------ MODULE HookCache ------------------------------
\* The process-local hook proof cache in cmd/nova-sprint/hook.go. Each identity
\* keeps a generation even when its cache entry is absent: deleting a generation
\* tombstone could let an old in-flight read match a later entry (ABA).
\*
\* ReadStart snapshots a generation while holding hookerBox.mu, then stands for
\* Hooker.Shared running without that mutex. For refill, one ReadStart abstracts
\* one identity from the batch whose names and generations are captured together.
\* Forget is the mutex-protected
\* invalidation after this process writes a new proof or unhooks. Commit is the
\* subsequent mutex-protected cache write. A cache entry has a ghost origin
\* generation; the Go hookCopy does not store it. An old bus reply is represented
\* by its captured generation. Bus payload, TTL, read errors and cancellation are
\* outside this safety model; those have the code's fail-closed gate and tests.
\*
\* A shared read may create an entry. A refill may replace only an entry still
\* present at commit. On generation mismatch, shared retries and refill drops
\* the reply; both are represented by discarding this in-flight read. There is
\* no liveness claim under continuous invalidation. Generations do not wrap or
\* prune within this bounded instance.

EXTENDS Naturals

CONSTANTS Identities, Readers, MaxGeneration, Broken
VARIABLES generation, held, origin, phase, identity, captured, kind

vars == <<generation, held, origin, phase, identity, captured, kind>>

Init ==
  /\ generation = [i \in Identities |-> 0]
  /\ held = [i \in Identities |-> FALSE]
  /\ origin = [i \in Identities |-> 0]
  /\ phase = [r \in Readers |-> "idle"]
  /\ identity = [r \in Readers |-> CHOOSE i \in Identities : TRUE]
  /\ captured = [r \in Readers |-> 0]
  /\ kind = [r \in Readers |-> "shared"]

ReadStart(r, i, k) ==
  /\ phase[r] = "idle"
  /\ k \in {"shared", "refill"}
  /\ (k = "shared" \/ held[i])
  /\ phase' = [phase EXCEPT ![r] = "reading"]
  /\ identity' = [identity EXCEPT ![r] = i]
  /\ captured' = [captured EXCEPT ![r] = generation[i]]
  /\ kind' = [kind EXCEPT ![r] = k]
  /\ UNCHANGED <<generation, held, origin>>

Forget(i) ==
  /\ generation[i] < MaxGeneration
  /\ generation' = [generation EXCEPT ![i] = @ + 1]
  /\ held' = [held EXCEPT ![i] = FALSE]
  /\ UNCHANGED <<origin, phase, identity, captured, kind>>

Commit(r) ==
  /\ phase[r] = "reading"
  /\ LET i == identity[r]
         generationOK == captured[r] = generation[i]
         guardDropped == (kind[r] = "shared" /\ Broken = "nosharedguard")
                         \/ (kind[r] = "refill" /\ Broken = "norefillguard")
         mayWrite == (generationOK \/ guardDropped)
                     /\ (kind[r] = "shared" \/ held[i])
     IN IF mayWrite
          THEN /\ held' = [held EXCEPT ![i] = TRUE]
               /\ origin' = [origin EXCEPT ![i] = captured[r]]
          ELSE /\ UNCHANGED <<held, origin>>
  /\ phase' = [phase EXCEPT ![r] = "idle"]
  /\ UNCHANGED <<generation, identity, captured, kind>>

Next ==
  \/ \E r \in Readers, i \in Identities, k \in {"shared", "refill"} : ReadStart(r, i, k)
  \/ \E i \in Identities : Forget(i)
  \/ \E r \in Readers : Commit(r)

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ generation \in [Identities -> 0..MaxGeneration]
  /\ held \in [Identities -> BOOLEAN]
  /\ origin \in [Identities -> 0..MaxGeneration]
  /\ phase \in [Readers -> {"idle", "reading"}]
  /\ identity \in [Readers -> Identities]
  /\ captured \in [Readers -> 0..MaxGeneration]
  /\ kind \in [Readers -> {"shared", "refill"}]

NoStaleCache == \A i \in Identities : held[i] => origin[i] = generation[i]

=============================================================================
