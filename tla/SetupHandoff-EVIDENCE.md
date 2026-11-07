# Two-child setup handoff model

This separate state machine implements the approved two-phase contract
(Rowan 1794012874753 plus the accepted immutable continuation shape). It does
not change Setup.tla or any of the previous 31 acceptance cases.

The original Setup model establishes the serialized eight-step walk, saved
answers and restart from step one. Crash/Restart can project a successful
child boundary that retains answers and discards probes, but cannot prove
lease handoff, continuation validity, competitor refusal or wrapper secrecy.
Its liveness cannot guarantee success of every real attempt. The new model
permits refusal when another up wins the gap.

## Protocol and abstraction

Phase 1 acquires its lease, saves the credential/schema, then persists the
record while still holding that lease. SaveRecord abstracts the sprint-secret
writer child under its monitored admin parent. Release ends that scope before
phase 2 launches. No continuous lease spans the children.

Phase 2 prevalidates the record/readiness, probes step one, reacquires,
revalidates under the lease, freshly probes step one, then performs Effect.
Effect abstracts the remaining full Setup walk, not permission to skip steps
2–8. These models have not been mechanically composed. Reuse after completion
or crash clears fresh/validated state and restarts phase 2. A crash before the
parent receives an ID restarts phase 1. Durable prerequisites remain.

The selected record abstracts immutable inactive policy-data with format
version, epoch, actor and canonical non-secret-argument digest. Selecting a
record after admin retry represents a new immutable object, not mutation of
an earlier stored record. Random IDs, JSON, hashing, canonicalization, storage
fences and permissions remain implementation obligations. This abstraction
proves neither ID collision resistance nor replay authorization.

One epoch change and a binding mismatch may occur. A stale/mismatched record
refuses at validation. ValidatedCurrentRecord records validity at under-lease
validation, not atomic validity against a later Clear. The model also does
not exclude external completion after unobserved PG-session loss. Exclusive
acquisition, durability and returning probes are assumed primitives.

ParentProtocolOnly restricts an abstract alphabet to opaque IDs and status.
It does not prove Go memory, argv/environment, stdout/stderr, parsing, grants
or child-process confidentiality. Concrete transport tests remain required.

## Source correspondence status

The phase order was coordinated with the setup/run owners and approved by
the parent. Concrete continuation/wrapper APIs were still being implemented
at model construction. This is contract evidence, not an exact-final-source
or end-to-end setup claim. Correspondence review must cover transports, lease
lifetimes, double record/readiness checks, fresh step-one checks, full walk,
refusals and immutable durable storage. Earlier source checkpoints remain
historical. Prior whole-walk lease prose refers to one child, not both phases.

## Execution evidence

All 12 current cases were checked on authorized vision under shared
nova-reserve capacity 16, TLC workers 2 and 1 GiB heap. The unchanged pinned
jar SHA-256 is `7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04`.
The host was heavily contended (observed load 240 on 64 cores); elapsed times
are not performance evidence. The recorded runs completed without timeout.

`layer6-evidence/handoff-review/results.json` is an explicitly aggregated
acceptance record from three reserved invocations, identified per row by
source_run. It records exact current module/config hashes, exit status and
host. Adjacent raw logs retain complete traces. The two positive cases exited
0; every reversed case exited 12 for its exact named invariant.

| Case | Checked result / actual retained trace |
| --- | --- |
| SetupHandoff | 251 generated / 163 distinct, depth 22; eight invariants and recurrent terminal liveness pass. |
| SetupHandoffCrash | 708 generated / 388 distinct, depth 23; same checks with up to one crash pass. |
| SetupHandoffEarlyRecord | State 6 SaveRecord persists while schema=FALSE; DurableBeforeRecord fails. |
| SetupHandoffNoLease | State 11 Acquire2 leaves lease free; state 14 Effect fails EffectsOrdered. |
| SetupHandoffOldProbe | State 14 Effect executes with fresh=FALSE; EffectsOrdered fails. |
| SetupHandoffWrongSeat | State 14 Effect executes under admin seat; EffectsOrdered fails. |
| SetupHandoffRefusedEffect | State 7 changes epoch, state 9 Compete takes the gap; state 11 Refuse wrongly produces an effect while lease=other. The trace has both stale-epoch refusal and a competing holder; it is not a trace of acquisition refusal alone. |
| SetupHandoffParentSecret | State 7 Release exposes secret in parent alphabet; ParentProtocolOnly fails. |
| SetupHandoffLostRecord | State 7 Crash erases the already-written record; RecordSurvivesCrash fails. |
| SetupHandoffStaleRecord | Epoch changes at state 12 after prevalidation/acquisition; state 13 Validate accepts stale recordEpoch=0 against epoch=1; ValidatedCurrentRecord fails. |
| SetupHandoffWrongType | State 12 Validate accepts record=other after both checks are bypassed; ValidatedCurrentRecord fails. |
| SetupHandoffReuseSkips | State 15 Reuse jumps to effects; state 16 Effect has no fresh probe, validation or lease; EffectsOrdered fails. |

These are deliberately reversed protocols, not findings against the ongoing
Go implementation. Trace action order and terminal values were read directly
from the retained TLC logs. The liveness property permits done or refused,
assumes fair returning calls/retry, and checks zero/one crash plus one bounded
post-completion reuse. It proves neither eventual successful installation nor
arbitrary disruptions. Runtime prerequisite regressions and all eight repair
steps remain in the separate Setup abstraction, not expanded here.

Commands (the first invocation used an earlier witness manifest; only exact
current-hash successful cases from it are included):

```text
ssh vision nova-reserve run --slots 16 --wait 60s --why stella-l6-handoff-tlc -- python3 /home/glenn/jobs/stella-v2-l6-handoff-model-20261106/tla/run-layer6.py --manifest SetupHandoff-runs.json --tag handoff-aligned
ssh vision nova-reserve run --slots 16 --wait 60s --why stella-l6-handoff-witnesses -- python3 /home/glenn/jobs/stella-v2-l6-handoff-model-20261106/tla/run-layer6.py --manifest SetupHandoff-runs.json --tag handoff-witnesses --only SetupHandoffStaleRecord SetupHandoffWrongType SetupHandoffReuseSkips
ssh vision nova-reserve run --slots 16 --wait 60s --why stella-l6-handoff-competitor -- python3 /home/glenn/jobs/stella-v2-l6-handoff-model-20261106/tla/run-layer6.py --manifest SetupHandoff-runs.json --tag handoff-competitor --only SetupHandoffRefusedEffect
```

Historical draft/aligned/witness/competitor logs and unfiltered result sets are
preserved outside the checkout under
`/Volumes/nova/ai/stella/working/l6-model-development-evidence-20261106/handoff-*`.
The first draft had a TLC evaluation exception from reading a next-state
variable before assignment; this was corrected, not counted as a witness.
An obsolete wrong-type witness was also rejected, regenerated and checked.
Only the aggregate's exact current hashes are acceptance evidence. No original
Run, Setup, Clear or Install model was changed or rerun. The actual-code trace
refinement gate requested subsequently remains separate and open.
