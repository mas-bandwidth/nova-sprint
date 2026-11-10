# nova-sprint roadmap

Ideas and future work for nova-sprint. Nothing here is scheduled; the owner decides when scope widens. Entries are data; the tables are rendered by nova-roadmap.

<!-- nova-work:future:start -->

| feature | done |
|---|:---:|
| add refuses a DONE-WHEN that runs functional tests, which cannot run inside the wall | ❌ |
| The dealer never deals to a row whose advertised binary is older than the run's version | ❌ |
| A missing toolchain inside the wall is a refusal at admission or a fault, never a bounce | ❌ |
| Exclude a stream from a row (needs a schema change) | ❌ |
| Runner registrations stored as nova-config records | ❌ |
| Steps.tla gains the holds/limits/silence/Gone step (the model gap under #702's bug) | ❌ |
| The nine validated policy limits read by the steps instead of Engine.* constants | ❌ |
| policy diff vA vB: a line diff of two policy versions (deleted 2026-11-08; policy show each and diff them) | ❌ |
| cost reconcile: provider balance falls set beside the priced API records, a cost-gap judgement hourly (deleted 2026-11-08 with modules/cost; the sprint.balances table stays) | ❌ |
| Delete the land-rate and flap alarms, the flaps column and SetRowFlaps (decision 6, not done: engine code under PR #714) | ❌ |
| Engine dead code (dropped from the 2026-11-08 pass): card/values.go mirror of store/check.go; replay.go write-applier; WaiveRed and the red column; the Edit event; Rescore and score; Released, single AskRead, SetCardAfter, ImportedReason; the deadcode list (verbs, edge, policy, card, steps, store, worker, dash, run); schema column drops | ❌ |
| The faults limit, its hold and the Steps limits witness (wip: model the limits step) | ❌ |
| up simplified: no two-phase handoff, no provider key step, no saved choices or --run-on, no --version adoption, no setup-time backup verify (S6 S3 S10 S14 S21) | ❌ |
| A stop holds a landing or promotion already in flight: it merges nothing until start (PR #741, parked) | ❌ |
| add adopts a card's existing branch as its head, so a continuing card starts from it (PR #726, parked) | ❌ |
| Steps.tla gains the rest after faults: a resting row is offered to policy but not dealt to (the model gap under #740) | ❌ |
| complete | 0% |

[Source data](roadmap.sexp)

<!-- nova-work:future:end -->
