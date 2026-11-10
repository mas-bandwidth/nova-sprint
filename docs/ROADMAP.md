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
| next --json answers small: the finished cards of the epoch listed in brief, or only the last few (82-214 KB on nine benches on 2026-11-09, past the worker's old 64 KiB cap; the worker now reads 16 MiB and names a truncated answer, so the fix here is the size, not the parse) | ❌ |
| Functional flakes fixed: TestFunctionalClearWhileWorkersBeat (clear's seq fence refused: store at seq N+1, expected N, a beat landing between read and clear), TestFunctionalEvaluatorMemoryCap (failed once on main), a postgres start failure in a functional run; and the trace job near the 2-minute runner limit (55-130 s, cancelled on 7 of the 2026-11-09 landing runs) | ❌ |
| A model of the coordinator's seat: one coordinator; hook, unhook and up --take-over, the take-over moving the seat whether the old coordinator is on the bus or gone, with a reversed witness (two coordinators at once); no module covers it (Setup.tla has the walk's steps, Store.tla the PutRow write) | ❌ |
| A row that misses N takes in a row is unhooked by the sprint (its hand returned) with a DOWN line to the coordinator; a take or a re-hook resets the count (new mechanism: needs the owner's agreement) | ❌ |
| NO-PROGRESS: a row whose taken cards show no progress within the bound gets one line to the coordinator naming the row, the cards and the minutes since its last progress; last progress shown in show <row> and on the dashboard; detect and tell only (new mechanism: needs the owner's agreement) | ❌ |
| complete | 0% |

[Source data](roadmap.sexp)

<!-- nova-work:future:end -->
