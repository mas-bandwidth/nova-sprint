# Down-friend recovery protocol

This bounded proposal checks the new protocol envelope before production implementation.
It is an interface abstraction alongside AttemptsEpochs, StopReturn and ReadCards, not
a composition or refinement proof of those modules or FriendPresence. Source baseline:
sprint main 3673301260b5f396fa9ed240ad3636865a1c4dd8. Production integration and faithful
normal-tick/store fixtures still require independent review and tests.

Two friends, one ready-or-running work card, one open reader, two recovery reservations,
and two nonempty head values are explored. Friend b stays up; a has a finite sequence
up/down/up/down. Pinned control forbids b. With no permitted receiver, work remains
unowned and review remains owed; no fairness assumption promises an eligible receiver.
The reader abstraction is retirement only: completed reviews and stale reader reports
are covered separately in ReadCards and must be rechecked by implementation tests.

`Plan` captures the exact external evidence revision and owner generation. `Reserve`
represents either Acquire with a full operation or Relock from an empty planning lock
to a full operation. Both validate the snapshot atomically. The full durable operation
reserves its manifests under the sprint-wide pending fence. `Apply` represents repair,
application and release: later proof does not cancel already-reserved revocation, and
no other admission or finish may cross that pending fence. Crash/restart between these
steps is stuttering with durable state preserved; multi-table physical repair correctness
is an assumption supplied by the existing operation protocol, not reproved here.

Generation values are lease ordinals. The concrete implementation increments at withdrawal
and again on redeal (old1 -> withdrawn2 -> launch3); model ordinal2 abstracts that fresh
launch and its reserved withdrawn identity. Equality checks and branch separation are
covered; exact production increment counts are not. Epoch/repository/card identity are
fixed in this bound; production head requests must bind all of them, exact old branch,
revoked generation and withdrawn current generation. Mapping: liveGen (steps_work.go),
withdrawUnit, BranchOf/PacketOf/PushedBranch (packet.go), and the proposed recovery sweep.

`ReadHead` records a successful read of the exact old branch. `ReadFailure` covers error,
malformed output and unavailable callback by keeping debt. Only a successful empty read
may clear debt without a new head; that preserves any prior carry. A nonempty
successful snapshot replaces carry with its SHA and old generation. CarryConserved
checks this per resolution (historical read membership alone was insufficient). `Resolve` checks exact debt identity. All new admissions
must reject debt, including explicit give/take paths. `Publish` can advance an old ref
before or after resolution; the carried SHA is the acknowledged successful snapshot,
not every future push. Branches are retained; new launches name a distinct branch. Git
content retention/GC and destructive external ref rewrites are outside this model.

Reader retirement increments its lease and preserves the pending review obligation,
regardless of receiver availability. Neither reader retirement nor work revocation spends
retry budget. Manual take exclusions and timed pin release are outside this protocol;
recovered friend a may receive work again when genuinely up. Pin bypass is forbidden.

Safety properties do not depend on fairness. Spec adds weak fairness for planning,
reservation, replay, successful head reads and resolution. Consequently persistent down
ownership eventually ends, durable reservations finish, and debt eventually resolves.
The last property assumes eventual successful remote reads; permanent Git outage leaves
visible debt safely pending and is not promised to make progress. No wall-clock bound
follows from weak fairness. Admission itself is not fair: receiver availability is not
assumed. Explicit Quiesce is enabled only when no reservation/debt or down-owned active
work/read remains; all cases keep TLC deadlock detection enabled.

Literal FriendPresence.NoCardOffUp is not an inductive property of this split protocol:
Down may become visible before a sweep reserves/replays withdrawal. The verified replacement
is no new admission while down, eventual withdrawal while persistently down, and no
ownership after recovery settles. Claiming the old atomic invariant needs an explicit
observation/linearization mapping rather than silently identifying these models.

Cases: two controls (free and pinned), fourteen guard mutants (Acquire, Relock, pending fence,
stale finish, admission through debt, stale debt identity, failed read as empty success,
branch reuse, retry spend, pin bypass, reader generation, dropped review obligation, lost carry on empty read, wrong nonempty carry), and
two reversed reachability witnesses (nonempty carry then launch; later old-generation push).
Independent cold read caught and corrected an initial empty-read carry overwrite and a
future-generation negative report; reports now name only previously launched generations.
A mutant passes its test only when TLC violates its named invariant. The reachability
witnesses use the correct protocol, not fault injection. Raw logs live in the job evidence
directory; RUNS rows are generated and merged by tools/tlacheck on Linux.
