# L1: can a real plan revive MEMBEREXISTS?

Answer: **no for the member-removal plans built by the current steps**. A member
being absent means its record does not exist, not that it has no placement.
`createEntry` requires `Expect.Absent`; `removeEntry` emits `Remove: true`, which
unplaces the card and keeps its record. `Mem.judge` returns `MEMBEREXISTS` for an
unplaced record too. `memTable.commit` never deletes a member record, whatever
combination of create, move, remove, field set or field unset a plan emits.
`Store.operation` translates these entries into manifests; it adds no record-delete
operation. A later plan therefore cannot turn a refused absence expectation true.

Plans checked: the constructors in `store/steps.go` and their plan functions,
including add/replace/recut, drop/defer, continue, drain, friend escalation,
friend read/return, hold, live/needs-stop, read-card retirement/missing reads,
reader retirement/level changes, rebalance, redo/rework/brief/accept/return,
tick retirement, fleet removal and stats tidy. Their removal sites all use the
same `removeEntry` operation. Queue combination preserves `Remove`; it does not
change unplacement into record deletion. The ancillary row deletion paths also
unplace rather than erase member records (`Mem.rowsDel`). Administrative table
teardown and advancing the epoch are outside the question about plans within
one epoch. This finding does not certify every other rule refusal as permanent.

`TestRevivedRefusalPlanExists` builds the delayed manifest from a real `AddStep`
plan, confirms its `MEMBEREXISTS` refusal, then runs real drop-ready,
drop-working and defer-waiting steps on Mem. After each removal, while the real
removing operation still holds its fence, the test requires the record to remain
unplaced. It resends the delayed add at the **current table revision** and with a
fresh operation id, so neither a stale revision nor receipt replay can mask a
revived expectation. It must still refuse `MEMBEREXISTS`, preserve the current
fence, and let the removing step finish. The working drop also checks retirement
of the associated fleet work card.

Negative control: change `memTable.commit`'s `Remove` branch to delete the member
record. The test must fail at “removal erased member”; revert that change before
the passing focused and package gates. No production code or model changes are
needed for this finding. `MCStoreFenceRevivedRefusal`'s abstract record deletion
is not a current plan operation; if record deletion is introduced, the model's
suggested fence check on manifests/table guard must be reconsidered first.
