# Setup handoff trace refinement

SetupHandoffTrace constrains the existing SetupHandoff actions with actual
implementation observations. The required fields are listed in Required(r).
Every additional recognized field is checked when supplied. Model-only control
positions and safety monitors are derived from the matched action history;
they are not asserted to be observed process memory.

Credential means a usable verified role credential, not merely a secret value
saved before PostgreSQL accepted it. Schema means the required schema readiness
was observed. A secret-save/role-alter crash gap or stale-secret repair is outside
this successful-readiness abstraction; it cannot be labeled SaveCredential.

When the real role operation exposes credential and schema together, PreparedRole
uses one fixed hidden SaveCredential followed by SaveSchema and requires both
actual readiness observations at the endpoint. This checks readiness before the
record, not an unobserved internal ordering of the role operation.

Release samples the endpoint after the actual lease is closed and the parent
receives the status/opaque ID. The interval between close and signaling is not
claimed atomic. Effect samples completed post-validation walk and released
lease, as in SetupHandoff.Effect; it is not a label for the first external call.
The attemptEffect observation records ordinary phase2 walk activity, including
probes of already-ready steps. Refusal must show no such activity.

The actual phase2 must prevalidate before bootstrap, then acquire a fresh lease,
validate again, and freshly verify step1 before its ordinary walk. No invented
Prevalidate observation or relaxed Next is used.
A prefix ending at Launch2 can match without proving the rest of the protocol.

A fixture using real runUpChild/private-FD handling, actual walker and actual
file-store continuation record helpers can bind those paths while still injecting
lease and role-readiness dependencies. It does not thereby prove production PG
lease exclusion, secret infrastructure, process secrecy, or undetected-loss
behavior. The source-bound success (13 records) and stale-epoch refusal (9 records)
match the model. Isolated SetupHandoff cases are separate model evidence. These two traces cover no crash, reuse, competing up, or lock loss.


The actual fixture invokes the real walker inside test subprocesses through
runUpChild and the real private-FD status handling. It persists and validates
continuations with the actual helpers and a file-backed fake store. Lease,
credential/schema readiness, secret command and ready-step dependencies are
injected. The phase1 under-lock step1 probe is an unrepresented stuttering action
in this protocol model; detailed walk order belongs to SetupTrace. Launch2 is
recorded retrospectively from the actual runUpChild invocation, placed before
its child's captured events. The stale case performs actual store.Clear between
children; the actual prevalidation refusal is checked to precede bootstrap,
lease acquisition, and all ordinary phase2 activity.


This is explicit fixture composition: the test calls runUpChild twice and
constructs a verbs.Engine in its subprocess helper. It does not execute the
production outer setup coordinator, executeUpPhase dispatch, or production
constructor. Matching therefore binds the observed child-channel/walker/record
helper endpoints, not the omitted production orchestration or provisioning.
