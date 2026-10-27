# Layer 1: the store, its contract as the code holds it today

Roadmap item `rewrite-l1-store` (docs/roadmap.sexp:79), part 1: the contract as found, at main
186b98d1, read 2026-10-26. Scope: `internal/sprint/store` (not the generated `*wire` subpackages,
which are the schema the store writes). Every claim cites a line read at that head; "unverified"
marks an inference. Line numbers drift: cite by function name when this is acted on.

The finding in one paragraph: the package is the store **and** most of layers 2 to 4. Its real
layer-1 core (epoch pin, fence, operation record, manifests, commit, repair) is about 4,000 of
25,299 non-test lines (engine.go's read/acquire/apply/release/repair, backend.go, epoch.go,
redis.go and mem.go's Backend methods). The rest is verb bindings (steps.go), the tick (tick.go),
friends, readers, routes, presence, seat, stats, snapshot and views. Of the core's 24 guarantees,
13 have no TLA+ module in this repo; the fence/commit/caller-op core is assumed, not modelled.

## 1. What the store owns

Stored under `sprint.Names` (prefix); "per epoch" keys are `Names.KeyAt(k, epoch)`.

| Thing | Kind | Where |
|---|---|---|
| The four tables: work, readers, merge, fleet | nova tables (ntable), per epoch | ops.go:16 (`All`), epoch.go:12-18 |
| Epoch hash: `n`, `cleared`, `restore` (owed), `defaults` (owed) | HASH, one per sprint | redis.go:54-77, 83-129 |
| Fence: the pending operation record | STRING, per epoch | redis.go:611, backend.go:308-325 |
| Fence generation | STRING counter, per epoch | redis.go:612, 702-705 |
| Event log: move lines, note lines, `updated` lines | STREAM `log`, per epoch, append-only | redis.go:614, 815-821, 851-856, 869-876 |
| Inbox: notifications | STREAM `inbox`, per epoch | redis.go:613, 856 |
| Judgments by id; open subjects; answered-by | HASH `notes`, `open`, `answered` | redis.go:615-618, 857-883 |
| Aliases `j<n>` and their count | HASH `aliases` | redis.go:616, 826-845 |
| Callers' recorded results (idempotence) | HASH `done`, per epoch | redis.go:621, 890-892 |
| Work-table queue (changes queued while RUNNING) | LIST `queue`, per epoch | redis.go:622, 805-814 |
| Streams' progress clocks; coordinator cursor | HASH `progress`, STRING `cursor` | redis.go:619-620, 887-889 |
| Log index for card history | ZSET `logindex`, STRING `logindexed` | load.go:385-390 |
| Machine-wide records (kept across clears): machine, heartbeat, stuck, coordinator, goals, timers, strangers, tickend, rules, friends, friend-status, fleet-drop-debt, seat, owner, where, stats, stops, donerecord | STRING/JSON via `KV` | teardown.go:26, tick.go:69-80 |
| Other records: archive, cardcheck, refusedrecord, server | STRING/JSON | archive.go:15, check_record.go:22, doctor.go:82, seat.go:149 |

Unverified gap: `archive`, `cardcheck` and `refusedrecord` are in neither `sprintKeys` nor
`machineKeys` (teardown.go:22,26) and no `init` adds them (load.go:390 is the only one), so
`Teardown`, which deletes only by exact name (teardown.go:135-140), may leave them.

## 2. What it guarantees

Each is a sentence a test can check. "Model" and "Test" are filled in sections 5 and 6.

| # | Guarantee | Enforced at |
|---|---|---|
| G1 | One manifest applies whole against one pre-state, or not at all, and moves its table's revision once. | backend.go:31-32; mem.go:478-600 (judge all, then commit); redis.go:277 delegates to ntable |
| G2 | A fenced read is never a partial state of another operation: the fence is read before and after the tables, a pending operation is finished first, and a moved generation means read again. | engine.go:429-462 |
| G3 | At most one writer commits per generation: `Acquire` succeeds only when the fence is empty, the generation is the one read, and the sprint is still at the pinned epoch; it sets gen+1. | redis.go:677-713; mem.go:1075-1100 |
| G4 | A commit writes queue trim and append, log lines, notes, inbox, open and closed judgments, aliases, answered-by, progress, the caller's result, seat and timers, and empties the fence, in one MULTI/EXEC; no notification is visible before it. | redis.go:718-747, 804-936; mem.go:1102-1218; engine.go:40-43 |
| G5 | Release is idempotent: releasing an operation the fence no longer holds writes nothing; a release whose transaction kept failing is success only if the fence no longer holds it and (for a commit) its result is recorded. | redis.go:734-735, 759-774, 797-802; mem.go:1110-1112 |
| G6 | A step with a recorded caller op returns the recorded result (`Replay`) for the same verb and arguments, and is refused for another verb or other arguments; checked before planning and again on every attempt. | engine.go:295-308, 516-520, 623-627, 1146-1154 |
| G7 | A caller op recorded at an earlier epoch is refused and never run again as new work. | engine.go:1155-1169 |
| G8 | Every operation id carries the pinned epoch and the attempt number: no id repeats across epochs or attempts. | engine.go:521-526, 817; internal/sprint/schema.go:190-201 |
| G9 | A step holding an epoch the sprint has left writes nothing and is refused naming the clear; a step holding none repins and reads again. | engine.go:601-622; epoch.go:84-89 |
| G10 | The epoch advances by exactly one, only from the epoch read, only while that epoch's fence is empty or held by the named owner, writing the restore-owed and defaults-owed flags in the same write; it never decreases. | redis.go:83-129; mem.go:235-260 |
| G11 | Pinning performs an owed restore before the epoch is used. | epoch.go:49-58 |
| G12 | A guarded row write applies only at the pinned epoch and in the restore phase it wants, check and write in one exchange; otherwise nothing is written and the outcome says why. | backend.go:111-117; redis.go:370-420; mem.go:285-311; clear.go:384-397 |
| G13 | A step cut between manifests leaves its operation in the fence; the next fenced read or `Repair` finishes it by resending the recorded manifests (an applied one replays), never overwriting newer state: an entry whose expectation no longer holds is skipped and listed in one judgment. | engine.go:436-451, 2176-2297, 2309-2373, 2731-2745 |
| G14 | A pending operation whose first manifest has not applied stays open within `Grace` (default 1 min) and is abandoned past it, or at once when refused on a bound or rule; a lock past its grace is released unwritten. | engine.go:401-406, 2178-2189, 2250-2282 |
| G15 | A lost reply is resent as the same bytes up to `Resends` (3); a write the store did not confirm is `ErrUnknown` and its operation is left pending, never assumed. | engine.go:2013-2029, 915-927, 970-976 |
| G16 | A manifest refused only on table REVISION is resent at the fresh revision only if every member still has its expected revision, place or absence. | engine.go:1879-1886, 2044-2110 |
| G17 | A first manifest refused on state applies nothing: the operation is released uncommitted and the step plans again; one refused on a bound or rule refuses the step whole and is not retried. | engine.go:957-969, 1352-1357, 1959-1962 |
| G18 | A step refused whole writes nothing: a card text over its bound, a manifest the table layer would refuse, a record over `MaxOpRecord` (256 MiB), or two disagreeing changes of one card. | engine.go:817-839, 898-900, 1280-1347, 1436-1447 |
| G19 | A `Named` step applies all or none. | engine.go:776-784, 1417-1432 |
| G20 | While the machine is RUNNING or the queue is non-empty, only the pump writes the work table; other steps queue; the drain's commit takes exactly the entries it read off the queue's head. | engine.go:655-699, 794-796, 833-836; redis.go:805-814 |
| G21 | After STOP, `StartsWork` and `ReportsWork` steps are refused, `RequiresStopped` steps are refused until STOP; a `Halts` part whose run sequence moved writes nothing. | engine.go:628-654 |
| G22 | A fenced read that keeps losing gives up after `Attempts` (FenceTries 12, defaults.go:23) with nothing changed; a step that loses every acquire returns `Lost` with every key refused. | engine.go:461, 580-584, 983-996 |
| G23 | Judgment and acknowledgement aliases are numbered consecutively per epoch, under the fence. | redis.go:826-845; mem.go:1127-1132 |
| G24 | Mem and Redis give the same results and refusals for the same calls. | backend.go:6-8 (stated) |

Known breaks of G24 (read, not run): Mem ignores an unreadable timer record (mem.go:1186) where
Redis fails the commit (redis.go:906-910); Mem's commit can return an error after writing notes
and the log without emptying the fence (mem.go:1171-1173), where Redis writes nothing (the
error is returned before EXEC, redis.go:737-746). Not concurrency-safe in the other direction:
`commit` reads the alias count and the timer record with `r.C`, not inside the WATCH
(redis.go:833, 902); safe only because every writer of those keys holds the fence
(timers.go:29-60), an invariant nothing checks (unverified for aliases outside commit).

## 3. Every refusal of the core

The core API is `Run`, `Fenced`, `Repair`, the epoch calls and the `Backend`. "Error" is a Go
error; "Refusal" is `Result.Refused` with a nil error. The rest of the package holds about 253
`fmt.Errorf`/`errors.New` sites (snapshot.go 34, clear.go 29, tick.go 26, restore.go 23, ...),
almost all verb policy of layers 3 to 6; they are not listed one by one here.

| # | Refusal | Condition | Where |
|---|---|---|---|
| R1 | Error: op id holds `~` | caller op contains the epoch marker | engine.go:506-508 |
| R2 | Error: op id not one word | fails `callerOpWord` | engine.go:509-511, 488-500 |
| R3 | `*OpConflictError` | caller op recorded for another verb, or other args | engine.go:281-291, 300-305 |
| R4 | Error: recorded result unreadable | `done` entry not JSON | engine.go:297-298 |
| R5 | Refusal `op <id>` | caller op recorded at an earlier epoch | engine.go:1166-1168 |
| R6 | Refusal `epoch <n>` | step holds an epoch the sprint left | engine.go:612-621 |
| R7 | `*ClearedError{Finished}` | display sync failed and the sprint left the epoch | engine.go:1207-1209, epoch.go:24-44 |
| R8 | `StaleRun` (no error) | `Halts` part saw another run sequence | engine.go:628-636 |
| R9 | Refusal: STOPPED, no new work | `StartsWork` and StopRevoked | engine.go:643-646 |
| R10 | Refusal: STOPPED, late report | `ReportsWork` and StopRevoked | engine.go:647-650 |
| R11 | Refusal: no halted run | `RequiresStopped` and not StopRevoked | engine.go:651-654 |
| R12 | Error: draining the queue | the pre-step drain failed | engine.go:663-666 |
| R13 | Refusal whole / `ErrRestorePending` | plan removes a member's control card while restore pending; guarded verb while pending | engine.go:749-753, 1364-1379; clear.go:292, 367 |
| R14 | Refusal whole: STOP owns | plan moves a card on the STOP debt | engine.go:757-773 |
| R15 | Refusal: all or none | `Named` step with any refusal | engine.go:776-784, 1424 |
| R16 | Refusal whole: changed twice | `*twiceError` from `operation` | engine.go:818-821, 1436-1442 |
| R17 | Refusal whole: queued text over bound | `unwritableQueued` | engine.go:829-831, 1304-1319 |
| R18 | Refusal whole: text bound or invalid manifest | `unwritable`/`invalid` | engine.go:837-839, 1280-1347 |
| R19 | Refusal whole: place did not hold | a `Place` still owed after the retry | engine.go:854-863 |
| R20 | Refusal whole: record over bound | `recordSize > MaxOpRecord` | engine.go:898-900 |
| R21 | Refusal whole: store did not take the record | acquire errored, fence unchanged | engine.go:923-926 |
| R22 | `ErrUnknown` (wrapped) | acquire, apply, release, commit unconfirmed; unreadable receipt | engine.go:351, 927, 962, 975, 1877, 2021, 2028 |
| R23 | `*CutError` | a later manifest refused | engine.go:363-374, 1964 |
| R24 | Refusal whole: bound or rule | first manifest refused not curably (`*boundError`) | engine.go:964-967, 1825-1836, 1960 |
| R25 | Refusal on every key, `Lost` | every attempt lost | engine.go:983-996 |
| R26 | `*PendingError` | a pending op that cannot finish, past the grace | engine.go:353-361, 445 |
| R27 | `*FenceBusyError` | fence moved through every try | engine.go:461, 464-480 |
| R28 | `*SyncError` | committed, display sync failed | engine.go:376-385, 1210 |
| R29 | Error: cannot be written | refuseWhole with no unit key to name | engine.go:1409-1411 |
| R30 | `*GuardError` | `RowsDelIf` during a pending restore or after the epoch moved | backend.go:212-227; redis.go:480 (delIf) |
| R31 | `ErrClearedUnderVerb` | guarded verb found another epoch | clear.go:294, 301 |
| R32 | Error: other epoch's judgment id | `SetReview` with an id of another epoch | epoch.go:155-157 |
| R33 | Error: epoch moved while settling | `SettleDefaults` transaction failed | redis.go:179-181 |
| R34 | Error: unreadable fence record | fence holds non-JSON | redis.go:664-668, 787-788 |
| R35 | `ErrReadOnly` | any write through `ReadOnly(b)` | tick.go:1987, 2023-2061 |
| R36 | `ErrNothingToStop` | stop of a READY machine | tick.go:42 |
| R37 | Error: start generation exhausted | run sequence overflow | machine_transition.go:161 |

## 4. Exported API surface

499 exported-name funcs and methods in 56 non-test files (60 of them on unexported types:
`readOnly` 52, `tripHook` 3, `boundError` 2, `movedError` 2, `twiceError` 1). Dead-check:
`grep -w` of each name over every non-test `.go` in the repo, definition excluded
(scratch script, not kept). A word match on a common name (`Clear`, `Load`) counts as a caller,
so "dead" below is a lower bound.

**Layer 1 proper (keep, contract):**

| Group | Members | Purpose |
|---|---|---|
| Backend interface | backend.go:21-157, 45 methods; impl `Redis` (redis.go, 48 exported), `Mem` (mem.go, 50) | tables, manifests, rows, epoch, fence, commit, notes, log, keys |
| Run loop | `Store.Run` engine.go:503, `Fenced` :411, `Repair` :2731, `IsFenceBusy` :477, `ArgsOf` :270 | one step as one operation; repair a pending one |
| Epoch | `Pinned`, `PinnedEpoch`, `At`, `EpochNow` epoch.go:100-135 | pin, read old epochs |
| Load | `Load`, `PipelinedLoadWithFence`, `NewLoadCache`, `ShareLoadCache`, `NewTwin`, `ShareTwin` | snapshot reads, cache, tick twin |
| Records | `getJSON`/`putJSON` are unexported; `KV` tick.go:69-80 (`GetKey`, `SetKey`, `SetKeyShowing`, `ShowState`) | machine-wide records |
| Constructors, bounds | `NewMem`, `NewID`, `TextBound`, `MaxOpRecord`, `MaxCardTextBytes`, `MaxBriefBytes` | |
| Teardown | `Teardown`, `TeardownKeys` | delete by exact name |

**Policy of a higher layer living here** (moves out in a later layer, not deleted now):

| File | Exports | Belongs to |
|---|---|---|
| steps.go (549 lines) | 50 `*Step` constructors (`AddStep` ... `ServerRestartStep`), each binding a verb to a `sprint` plan and its loads | layer 3 (steps) |
| tick.go, tickend.go, machine_transition.go, timers.go, stuck.go | `Tick`, `ShadowTick`, `TickPartStep`, `SetMachine`, `StopUntil`, `Machine*`, `WaitTickEnd`, `AddTimer`... | layer 4 (coordinator/machine) |
| friends.go, readers.go, readers_tiers.go, lane.go, presence.go, seat.go, live.go | 17 + 11 + 1 + 3 + 9 + 14 + 2 | layers 3 and 7 (friends, readers, seat) |
| routes.go, rate.go, rule_answers.go, rules.go, remind.go | routes, budgets, bars, goals | layer 3 policy |
| where.go, doctor.go, stats*.go, snapshot.go, restore.go, archive.go, check_record.go, done.go, waitlog.go, coordinator_delivery.go | views, stats, RDB parsing (snapshot.go:282-601), backups | layers 5 and 6 (edges, CLI) |

**Policy inside the layer-1 core** (engine.go `Run`): the STOP debt and lease fences
(engine.go:643-654, 757-773), the pump/queue policy (655-699, 794-796), snapshot enrichment for
routes, prices, bars, readers, NoRoom and friends (556-579, 700-741), the lifecycle check
`sprint.Applied` (743), the restore refusal of fleet control cards (749-753), the stuck judgment
(799-805), verb-name special cases in `after` for `add` and `stop-return` (1189-1204), friend-row
hiding in `addRows` (1229-1240), the tick deal's read-card `MEMBEREXISTS` drop inside `apply`
(1887-1958), and `sprint.Lawful` re-judging inside repair (`rejudge`, 2423). `Store` itself
carries 34 field declarations, among them tick and friend callbacks (engine.go:63-150).

**Store logic living above the store:** `cmd/nova-sprint/verbs.go:4351-4371` (stream remove)
plans on an unfenced `Load` and calls `st.B.RowsDel` on two tables outside any operation: no
fence, no generation check, not atomic across the two tables. `verbs.go:1338` calls
`st.B.RowsAdd` directly; `reads.go:2586` `st.B.SetCursor`. 19 CLI files reach through `st.B`
(mostly reads: `Coordinator` 21, `Shapes` 6, `OpenNotes` 6). The id/epoch encoding the store
relies on (`StoredID`, `OpFamily`, `IDEpoch`) and the queue (`QueueOf`, `Drain`, `WithQueue`)
live in `internal/sprint` (schema.go:190-205, queue.go:44-284).

**Dead: no caller outside tests (8):** `Store.SeatAndServer` seat.go:239 (no caller at all, a
comment only at cmd/nova-sprint/reads.go:1375), `Store.RestorePending` clear.go:371,
`Store.FriendBeat` friends.go:302, `Store.CardIDs` reads.go:89, `Store.StopsKept` stops.go:87,
`Store.RecordStoreRTT` where.go:430, `Store.SetServerActor` seat.go:190, `Machine.StoppedTotal`
tick.go:255.
**Test-only exports in production files (10):** `Mem.Touched` mem.go:168; `Mem.SetMaxAttempts`,
`SetGateBars`, `SetDecideBars`, `SetScoreBar`, `SetJudgmentBar`, `SetRoutes`, `SetTiers`,
`SetRulesOff`, `SetGitIdentity` routes.go:425-540.
Part 1 (2026-10-27) kept these 10: tests in other packages call them (SetRoutes from 33 files),
and a `_test.go` file is not visible outside its package. They move when the layer has a test
fixture package of its own (storetest), not before. Of the 8 dead exports, part 1 deleted
SeatAndServer, CardIDs, FriendBeat, SetServerActor and StopsKept (their test callers now use the
production call: SeatServerCoordinator, FriendBeatReport, SetServer, WhereFacts), and moved
RestorePending, RecordStoreRTT and StoppedTotal into observe_test.go, since only tests call them.
Not dead though unreferenced by name: `tripHook.DialHook/ProcessHook/ProcessPipelineHook`
stats.go:164-173 implement go-redis's hook interface.

## 5. Model coverage

tla/COVERAGE.tsv has 26 rows naming a store file. Its row `pending-fenced-op` (ops.go, stuck.go,
cmd repair.go) is `missing`. `tla/SprintEvents.tla:10-20` states that layers 1 and 2 "are
assumed, as their own models prove them (the table layer's models, tla/TABLE-MODEL.md)": the
fence, the commit and the receipts are taken as given, and that table-layer document is not in
this repo (unverified where it lives).

| # | Module and invariant | Status |
|---|---|---|
| G1 | assumed from the table layer (SprintEvents.tla:10-20) | GAP here (L0) |
| G2 | DirtyTickRead `ViewIsSnapshot` (:492), `TwinIsTheStore` | covered |
| G3 | DirtyTickRead Acquire (MCDirtyTickReadAcquire.cfg), `LockedApplyNotLost` (:497) | covered |
| G4 | none; SprintTables treats a step as one action | GAP |
| G5 | none | GAP |
| G6 | none (SprintEvents `ReplayNoop` :1930 is key delivery, not caller ops) | GAP |
| G7 | none | GAP |
| G8 | AttemptsEpochs `SuffixUnique` (cards' suffixes, not op ids) | partial |
| G9 | DirtyTickRead `HeldEpochIsTheWrite`, `OldEpochPartWritesNothing`; AttemptsEpochs `OldEpochWritesNothing` | covered |
| G10 | AttemptsEpochs `EpochMonotone` (PROPERTY); ClearDefaults `FinishOnlyWhatIsOwed`, `EpochZeroNeverOwed` | covered |
| G11 | AttemptsEpochs `Restore`; ClearDefaults `MembersRestored` | covered |
| G12 | ClearDefaults `RerunKeepsEpochWrites`, `MembersRestored` (EditRow, DeleteRow) | covered |
| G13 | SprintTables Crash/Repair, `NoCardLostOrTwice` (MCSprintTablesCrash.cfg); RepairLog `AppliedHasLine`, `LineOnlyIfApplied`, `ProbeSparesEntries` | covered |
| G14 | none; COVERAGE `pending-fenced-op` missing | GAP |
| G15 | none here (receipt replay assumed) | GAP |
| G16 | DirtyTickRead (display writes move rev outside the fence); RepairLog whole resend | partial |
| G17 | none | GAP |
| G18 | SprintEvents holds bounds as constants only | GAP |
| G19 | none | GAP |
| G20 | DirtyTickRead `StoppedStepDrainsFirst`; queue consumed once not modelled | partial |
| G21 | DirtyTickRead `StartsWorkNotAfterStop`, `NoStaleRunCommit`; StopReturn, LiveRuns | covered |
| G22 | none (giving up is liveness; nothing-changed is trivial) | GAP |
| G23 | none | GAP |
| G24 | n/a (a differential test, not a model) | GAP |

Model GAPs: 13. Stale citations: DirtyTickRead.tla's header and COVERAGE row `tick-read-fence`
cite engine.go:555-626, :582, :597-608 for code that is now at engine.go:601-672, :628,
:643-654 (about 46 lines of drift); the model is right, its pointers are not.

## 6. Test coverage

Unit tests run on `Mem`; the "real store" bar is met only by `//go:build functional` files
against Redis (redis_sprint_functional_test.go, redis_functional_test.go).

| # | Test | Real store |
|---|---|---|
| G1 | store_test.go:513 `TestMemRefusesAsTheBatchDoes` | no |
| G2 | store_test.go:286 `TestD1APendingOperationIsFinishedFirst`, :373 | functional :73 |
| G3 | store_test.go:373; redis_sprint_functional_test.go:914 `TestRedisTheFenceRefusesWhatItShould` | yes |
| G4 | store_test.go:319 `TestD1NotificationsAtTheCommitOnly` | no |
| G5 | redis_sprint_functional_test.go:914 (second release); redis_functional_test.go:109 | yes |
| G6 | callerop_test.go:13, :64; store_test.go:492 | redis_sprint_functional_test.go:417 |
| G7 | clear_ids_test.go:13 | no |
| G8 | clear_ids_test.go:49 | no |
| G9 | clear_told_test.go:103, :126 | redis_sprint_functional_test.go:510, :582 |
| G10 | clear_start_fence_test.go, ready_clear_race_test.go, redis_cover_test.go (AdvanceEpoch) | fake client |
| G11 | clear_owed_test.go (unverified which test) | no |
| G12 | redis_guarded_rows_test.go:18; clear_restore_guard_test.go:19; clear_concurrent_test.go:96, :103 | fake client |
| G13 | store_test.go:414; repair_test.go:128, :227 | redis_sprint_functional_test.go:295, :370 |
| G14 | store_test.go:450; bounds_test.go:210; engine_cover_test.go:63 | no |
| G15 | store_test.go:608; engine_cover_test.go, finished_op_test.go | redis_functional_test.go (ErrUnknown) |
| G16 | store_test.go:394 | no |
| G17 | bounds_test.go:137 | no |
| G18 | bounds_test.go:42, :72; changed_twice_test.go:52; MaxOpRecord: none | no |
| G19 | seat_cover_test.go (message text only) | no |
| G20 | drain_test.go; dirty_tick_test.go (MaxDrains) | no |
| G21 | stop_return_test.go:68, :91, :184 | no |
| G22 | tick_busy_test.go; machine_twoloops_test.go; machine_duties_test.go (Lost) | no |
| G23 | none | GAP |
| G24 | store_test.go:513 (Apply only); timer and partial-commit divergences untested | partial |

Refusals: tested R1 (clear_ids_test.go), R2 (callerop_test.go:43), R3 (callerop_test.go:13,64),
R5 (clear_ids_test.go:13), R6, R7 (clear_told_test.go:103), R8 (stop_return_test.go:91),
R9 (stop_return_test.go:68), R10 (stop_return_test.go:184), R13 (clear_restore_guard_test.go:19),
R14 (stop_return_test.go:302, :321), R15 (seat_cover_test.go), R16 (changed_twice_test.go:52),
R18 (bounds_test.go:42, :72), R22, R23 (repair_test.go, store_test.go), R24
(bounds_test.go:137), R25 (machine_duties_test.go), R26 (engine_cover_test.go:23, :63),
R27 (tick_busy_test.go), R31 (clear_concurrent_test.go). No test names: **R4, R11, R12, R17,
R19, R20, R21, R28, R29, R30, R32, R33, R34, R35, R36, R37** (16 GAPs; found by grepping each
refusal's type or text in `*_test.go`, so a test asserting only `Refused` non-empty is missed).
On a real store: only R3, R6, R22, R23 and the fence refusals of G3.

House-rule flags (unit tests, no build tag, using real time or sockets):
- machine_twoloops_test.go:31 sets `Sleep = time.Sleep` and races two goroutine tick loops on
  the real clock (its comment says the real sleep is what made it pass): a functional test.
- tick_ask_test.go:235 reads `time.Now()` for a deadline (arithmetic only, low risk).
- waitlog_test.go:69 `context.WithTimeout(30*time.Second)` as a guard.
- redis_cover_test.go:39, acl_steps_test.go:80, lock_cover_test.go:65, stats_cover_test.go:99,
  :114, :130 build a go-redis client at `store.invalid` answered by an in-process hook; no socket
  if the hook short-circuits every dial (unverified).
- retry_test.go:137 uses `time.Now` inside `synctest.Test` (fake clock): fine.

## 7. Size

| | Non-test | Test |
|---|---|---|
| Package (56 / 259 files) | 25,299 | 48,320 |
| storetest/ | 80 | |
| Generated `*wire` (out of scope) | donewire 9,645; friendrowswire 44,945; oprowswire 69,221; snaprowswire 92,474; viewrowswire 183,239 | |

Largest files (non-test; tests by file-name prefix, approximate): engine.go 2,758 (87),
tick.go 2,387 (4,410), mem.go 1,573 (681), redis.go 1,569 (3,232), friends.go 891 (782),
twin.go 837 (767), snapshot.go 812 (456), where.go 716 (1,175), load.go 702 (263), ops.go 691
(136), done.go 683 (733), clear.go 633 (2,651). engine.go has one 87-line cover test: its tests
are spread across ~40 behaviour files.

Five longest functions:

| Lines | Function | At |
|---|---|---|
| 495 | `(*Store).Run` | engine.go:503-997 |
| 293 | `(*Store).tick` | tick.go:1067 |
| 254 | `(*Store).pipelinedLoadOnceWithFence` | pipeline_load.go:51 |
| 230 | `(*tickRun).parts` | tick.go:1448 |
| 230 | `(*Store).reconcilePlan` | repair_skipped.go:232 |

Next: `(*Store).operation` 213 (engine.go:1608), `Tick` 163 (tick.go:814), `TidyStats` 151.

## 8. Smallest next parts, in order, each landable alone

1. **Delete the 8 dead exports** (section 4) and move the 10 test-only `Mem` helpers into a
   `_test.go` file. Pure deletion; no behaviour change.
2. **Refresh the stale engine.go citations** in DirtyTickRead.tla's header and the
   COVERAGE.tsv `tick-read-fence` row (cite by function, not line).
3. **Model the fence core: `tla/StoreFence.tla`** with gen, fence, done, epoch: Acquire, Apply,
   Crash, Release(commit), Finish, Abandon-past-grace, and caller-op replay. Invariants: one
   commit per generation (G3), commit-all-or-nothing (G4), idempotent release (G5), replay
   returns the recorded result and conflicts otherwise (G6), earlier-epoch op never reruns (G7),
   abandoned only if nothing applied (G14). Reversed witnesses: `releaseanyop`,
   `replayignoresargs`, `rerunoldepoch`, `abandonapplied`. Closes COVERAGE `pending-fenced-op`.
4. **Fix the two Mem/Redis divergences** (unreadable timer record; Mem's partial commit) with a
   test each that runs the same call on Mem and the fake Redis client.
5. **Test the 16 untested refusals on Mem**, one small test each (R20 `MaxOpRecord` can use a
   lowered bound via a test hook; R30 `GuardError` on Mem's `RowsDelIf`).
6. **Same refusals on a real store**: extend redis_sprint_functional_test.go to every refusal of
   section 3, as the layer bar requires.
7. **Move machine_twoloops_test.go to the functional tier** (house rule), or give it a fake clock.
8. **Put stream remove behind a store step** (cmd/nova-sprint/verbs.go:4351-4371): one fenced
   operation, or record why an unfenced two-table delete is acceptable.
9. **Decide the teardown keys** `archive`, `cardcheck`, `refusedrecord`: a functional test that
   writes each then tears down, then add them to `machineKeys` if they survive.
10. **Split `Run` along its seams** (after 3 to 6 are green): pin and caller-op check; the
    pre-plan enrichment hook (routes, prices, bars, readers, friends, NoRoom: engine.go:556-579,
    700-741) handed in by layer 3; the STOP and queue fences as a policy hook of layer 2; and the
    core read, plan, acquire, apply, release loop left in engine.go.
11. **Move steps.go out of the store package** into layer 3: the 50 step constructors import
    the store, not the other way round.
