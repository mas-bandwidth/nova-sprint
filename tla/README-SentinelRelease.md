# Automatic sentinel release

`SentinelRelease` models the P8 automatic-release contract at a bounded cut:
two required producer streams, one sentinel, and two successive deals from a
consumer stream. Each producer has at most two head revisions. It complements
`Holds`; it is not a composition/refinement proof of that older model's manual
and cut-sentinel transitions or of the entire sprint scheduler.

## Release and proof boundary

A producer landing and its stream head entering the actual remote development
branch are different events. `MergeProducer` puts a stream-qualified head in
`remoteDev`; `ObserveDev` reads that remote snapshot. `Probe` records ancestry
for the exact producer/head and proof scope. `NewHead` and `ChangeScope` retain
old probe results, so the release consumer must reject stale evidence itself.
The scope counter abstracts invalidation by a needs/row/epoch revision. It does
not model clear, graph editing, authentication, or Git commit parsing.

`AutoRelease` requires every producer landed and a current proof for every
producer's head. It commits the release with its notification and the observed
development revision. The release snapshot is immutable history: a later
remote advance cannot rewrite what authorized that release. There is no
coordinator response, sentinel hold, manual-gate flag, or cancel window in this
path. A second producer stream cannot borrow the first stream's proof.

`EarlyRelease` is a separate, explicitly authorized override. It can release
while work is still in flight or dropped, and is recorded as early. Automatic
release's landed-and-ancestry invariant does not falsely claim to cover this
intentional exception. No fairness premise schedules the override.

Current draft policy is strict: a dropped producer is not landed and cannot
authorize automatic release. The runtime owner must confirm this policy and
the handling of a sentinel with no producers before integration. The instance
requires a nonempty producer set and makes no empty-cut claim. The abstract
name `dev` denotes the actual configured integration target; literal `dev`
versus another recorded protected target remains a runtime policy decision.

## Consumer refresh and the distributed boundary

`MergeConsumer` means a successful merge/push of the observed development
snapshot into the consumer branch, with a receipt for its next deal. A failed
or conflicting merge cannot write a success receipt. `Deal` consumes that
receipt once, requires the same observed snapshot, and refuses to use one
merge for two successive deals. A newly observed development revision makes
an older consumer receipt insufficient.

Remote Git and the sprint store do not share a transaction. `remoteDev` may
advance between a fetch and the store's deal commit, before `ObserveDev` reads
that advance. The model deliberately permits that race. Its guarantee is a
successful merge of the freshly observed revision, including the release's
required heads, with current store-side proof checks. It does not assert that
a consumer includes an unobserved remote advance at the instant of deal.
Git history is append-only here; force pushes, history replacement, and a
server accepting fabricated ancestry evidence are outside the abstraction.

## Safety, liveness, and negative controls

The control checks types, automatic landed/ancestry authorization, current
proof scope, early-override authority, the atomic notification, successful
consumer integration, observed-development freshness and one merge per deal.
The automatic-only control excludes early overrides. The second control
allows them and checks the same conditional safety rules.

`StableEligibilityReleases` says that eventually stable landed producers whose
heads are actually in the development branch eventually release the sentinel.
Fair scheduling of successful observation/probe and the automatic tick is
assumed; a permanently unavailable forge is not modeled as guaranteed progress.
`ReleasedEventuallyDeals` additionally assumes successful consumer integration
and deal retries eventually run. These are weak-fairness premises on machine
and integration actions, never on coordinator actions. Changes of head/scope
and unrelated development moves are bounded; infinite moving-target histories
are outside this finite instance.

Terminal-state deadlock detection is disabled because an asynchronous server
may wait with no state change. Temporal properties still inspect infinite
stuttering behavior: the `manualgate` mutation must fail liveness even though
all eligible facts hold. No elapsed-time bound is claimed.

The reachability cases deliberately assert that automatic or early release
never happens; their expected counterexamples establish those paths are not
vacuous. Other witnesses remove exactly one guard or corrupt one outcome:
unlanded producer, absent development ancestry, stale head, another stream's
proof, stale scope, missing notice, a coordinator prerequisite, stale consumer
snapshot, false success after merge failure, reuse of a prior merge, relabeling
an early override as automatic, and a worker claiming override authority.

Cases and exact expected properties are in `CASES.tsv`, group
`sentinelrelease`. Measured Linux records belong in `RUNS.tsv`; prior records
remain in `RUNS-HISTORY.tsv`. No run is claimed until those records exist.
