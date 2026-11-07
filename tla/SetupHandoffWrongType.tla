-------------------------- MODULE SetupHandoffWrongType --------------------------
EXTENDS Naturals, FiniteSets

(***************************************************************************
 Separate two-child setup protocol, modelled apart from the Setup walk.
 A typed record is immutable and reusable, but only authorizes fresh checks.
 recordEpoch and binding abstract epoch and actor/canonical-argument binding.
 Phase 1 holds a monitored lease while a sprint-secret helper writes the
 record, then releases. Phase 2 validates before and under its fresh lease,
 rechecks step one, then continues the full Setup walk (abstract Effect).
 Another up may win the gap and this attempt then refuses.

 Durability and exclusive lease acquisition are assumed primitives. Epoch
 can change between checks: validity is checked AT validation, not an atomic
 fence against later Clear or undetected PG loss/external completion.
 Parent-visible alphabet is status/opaque ID; no Go/process secrecy proof.
 Retry with a known record restarts phase 2; otherwise phase 1 rechecks durable
 prerequisites. IDs/JSON/hash collision resistance are not modeled. A record
 selected after an admin retry represents a new immutable record, not mutation
 of an earlier one. No continuous lease spans children. External calls must
 return for fair progress; refusal is a permitted terminal outcome.
***************************************************************************)
CONSTANT MaxCrashes
VARIABLES reuses, epoch, recordEpoch, binding, validationOrder, seat, pc, lease, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives
vars == <<reuses, epoch, recordEpoch, binding, validationOrder, seat, pc, lease, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Init == /\ reuses = 0
        /\ epoch = 0 /\ recordEpoch = 0 /\ binding = TRUE /\ validationOrder = TRUE
        /\ seat = "admin"
        /\ pc = "pre1"
        /\ lease = "free"
        /\ credential = FALSE
        /\ schema = FALSE
        /\ record = "absent"
        /\ prefix = FALSE
        /\ fresh = FALSE
        /\ validated = FALSE
        /\ attemptEffect = FALSE
        /\ exposed = {}
        /\ crashes = 0
        /\ recordOrder = TRUE
        /\ effectOrder = TRUE
        /\ recordSurvives = TRUE
ValidRecord == record = "setup" /\ recordEpoch = epoch /\ binding /\ credential /\ schema
Prefix1 ==
  /\ pc = "pre1"
  /\ pc' = "acquire1"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, lease, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Acquire1 ==
  /\ pc = "acquire1" /\ lease = "free"
  /\ lease' = "ours"
  /\ pc' = "credential"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
SaveCredential ==
  /\ pc = "credential" /\ lease = "ours"
  /\ credential' = TRUE
  /\ pc' = "schema"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, lease, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
SaveSchema ==
  /\ pc = "schema" /\ lease = "ours"
  /\ schema' = TRUE
  /\ pc' = "record"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, lease, credential, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
SaveRecord ==
  /\ pc = "record" /\ lease = "ours"
  /\ record' = "other"
  /\ recordEpoch' = epoch
  /\ recordOrder' = (recordOrder /\ credential /\ schema)
  /\ pc' = "release"
  /\ UNCHANGED <<reuses, epoch, binding, validationOrder, seat, lease, credential, schema, prefix, fresh, validated, attemptEffect, exposed, crashes, effectOrder, recordSurvives>>
Release ==
  /\ pc = "release"
  /\ lease' = "free"
  /\ pc' = "gap"
  /\ exposed' = exposed \cup {"status", "id"}
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, credential, schema, record, prefix, fresh, validated, attemptEffect, crashes, recordOrder, effectOrder, recordSurvives>>
Compete ==
  /\ pc \in {"gap", "prevalidate", "pre2", "acquire2"} /\ lease = "free"
  /\ lease' = "other"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, pc, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Launch2 ==
  /\ pc = "gap"
  /\ seat' = "sprint"
  /\ pc' = "prevalidate"
  /\ fresh' = FALSE
  /\ prefix' = FALSE
  /\ validated' = FALSE
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, lease, credential, schema, record, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Prefix2 ==
  /\ pc = "pre2"
  /\ prefix' = TRUE
  /\ pc' = "acquire2"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, lease, credential, schema, record, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Acquire2 ==
  /\ pc = "acquire2" /\ prefix /\ lease = "free"
  /\ lease' = "ours"
  /\ pc' = "validate"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Verify2 ==
  /\ pc = "verify2"
  /\ fresh' = TRUE
  /\ pc' = "effects"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, lease, credential, schema, record, prefix, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Validate ==
  /\ pc = "validate"
  /\ validated' = TRUE
  /\ pc' = "verify2"
  /\ validationOrder' = (validationOrder /\ ValidRecord)
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, seat, lease, credential, schema, record, prefix, fresh, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Effect ==
  /\ pc = "effects"
  /\ attemptEffect' = TRUE
  /\ effectOrder' = (effectOrder /\ seat = "sprint" /\ lease = "ours" /\ fresh /\ validated)
  /\ pc' = "done"
  /\ lease' = "free"
  /\ exposed' = exposed \cup {"status"}
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, credential, schema, record, prefix, fresh, validated, crashes, recordOrder, recordSurvives>>
Refuse ==
  /\ (pc \in {"acquire1", "acquire2"} /\ lease = "other") \/ (pc \in {"prevalidate", "validate"} /\ ~ValidRecord)
  /\ pc' = "refused"
  /\ lease' = IF lease = "ours" THEN "free" ELSE lease
  /\ exposed' = exposed \cup {"status"}
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, credential, schema, record, prefix, fresh, validated, attemptEffect, crashes, recordOrder, effectOrder, recordSurvives>>
Crash ==
  /\ seat' = "none"
  /\ crashes < MaxCrashes /\ pc \notin {"done", "refused", "crashed"}
  /\ pc' = "crashed"
  /\ lease' = IF lease = "ours" THEN "free" ELSE lease
  /\ crashes' = crashes + 1
  /\ fresh' = FALSE
  /\ prefix' = FALSE
  /\ validated' = FALSE
  /\ record' = record
  /\ recordSurvives' = (recordSurvives /\ record' = record)
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, credential, schema, attemptEffect, exposed, recordOrder, effectOrder>>
Retry ==
  /\ pc = "crashed"
  /\ seat' = IF "id" \in exposed THEN "sprint" ELSE "admin"
  /\ pc' = IF "id" \in exposed THEN "prevalidate" ELSE "pre1"
  /\ attemptEffect' = FALSE
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, lease, credential, schema, record, prefix, fresh, validated, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Prevalidate ==
  /\ pc = "prevalidate"
  /\ pc' = "pre2"
  /\ UNCHANGED <<reuses, epoch, recordEpoch, binding, validationOrder, seat, lease, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
ChangeEpoch ==
  /\ epoch = 0 /\ pc \notin {"done", "refused"}
  /\ epoch' = 1
  /\ UNCHANGED <<reuses, recordEpoch, binding, validationOrder, seat, pc, lease, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
ChangeBinding ==
  /\ binding /\ pc \in {"gap", "prevalidate", "pre2", "acquire2"}
  /\ binding' = FALSE
  /\ UNCHANGED <<reuses, epoch, recordEpoch, validationOrder, seat, pc, lease, credential, schema, record, prefix, fresh, validated, attemptEffect, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Reuse ==
  /\ pc = "done" /\ reuses = 0
  /\ reuses' = 1 /\ pc' = "prevalidate" /\ seat' = "sprint"
  /\ prefix' = FALSE /\ fresh' = FALSE /\ validated' = FALSE
  /\ attemptEffect' = FALSE
  /\ UNCHANGED <<epoch, recordEpoch, binding, validationOrder, lease, credential,
       schema, record, exposed, crashes, recordOrder, effectOrder, recordSurvives>>
Progress == Prefix1 \/ Acquire1 \/ SaveCredential \/ SaveSchema \/ SaveRecord
            \/ Release \/ Launch2 \/ Prevalidate \/ Prefix2 \/ Acquire2 \/ Verify2 \/ Validate
            \/ Effect \/ Refuse \/ Retry \/ Reuse
Next == Progress \/ Compete \/ ChangeEpoch \/ ChangeBinding \/ Crash
Spec == Init /\ [][Next]_vars /\ WF_vars(Progress)
TypeOK == /\ reuses \in 0..1
          /\ epoch \in 0..1 /\ recordEpoch \in 0..1
          /\ seat \in {"admin","sprint","none"}
          /\ pc \in {"pre1","acquire1","credential","schema","record","release",
                       "gap","prevalidate","pre2","acquire2","verify2","validate","effects",
                       "done","refused","crashed"}
          /\ lease \in {"free","ours","other"}
          /\ record \in {"absent","setup","other"}
          /\ crashes \in 0..MaxCrashes
          /\ exposed \subseteq {"status","id","secret"}
DurableBeforeRecord == recordOrder
ReleaseHasRecord == pc = "gap" => record = "setup"
EffectsOrdered == effectOrder
ValidatedCurrentRecord == validationOrder
RefusalHasNoEffects == pc = "refused" => ~attemptEffect
ParentProtocolOnly == exposed \subseteq {"status","id"}
RecordSurvivesCrash == recordSurvives
EventuallyTerminal == []<>(pc \in {"done","refused"})
=============================================================================
