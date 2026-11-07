# Cold review: layer 6 run

Base: `43d849e29d0e4568ba6a1348a076a59259639938`.

## Verdict

The layer has a serious lock-check gap in the roster-error branch: when a roster read fails, the loop bypasses the tick's chooser health check but still folds, pushes and publishes. The highest-risk unresolved point beyond that defect is delivery recovery under envelope overflow, where the no-loss interpretation depends on a `next` integration test that is still open.

## Defects

- **serious — roster failure bypasses the chooser health check, then pushes.** When `ReadRosterAt` returns an error, `loop.tick` calls `Ticker.Fold` directly and then continues into `Ticker.Push` (`run/serve.go:143-164`); the only health check on the normal path is inside `Ticker.Tick` (`run/tick.go:149-155`). If the separate config read fails while the dedicated chooser session or sprint hook has also been lost, this branch can still send notices and write a push cursor before the next tick detects the lost chooser. That permits a process which no longer owns the chooser to perform a push side effect alongside its replacement. Check the chooser on the roster-error path before fold/push and return `ErrLockLost` immediately when it has been lost.

## Promise gaps

- “Every committed event that needs someone is pushed at least once” (`docs/LAYER-6-RUN.md:405`). `run/envelope` replaces the remaining individual lines with `and N more: nova-sprint next --as <row>` when the byte bound is reached (`run/push.go:167-178`), then `Pusher.Push` advances the cursor to the last queued event and deletes that queue after the single send succeeds (`run/push.go:138-149`). `next` exposes the row's inventory but selects one next command (`verbs/next.go:142-165,186-200`). Thus delivery of each individual notice depends on the recipient repeatedly consulting and acting through `next`; the source establishes a summary fallback, not an individual notice for every event. I could not establish from this source path alone that this violates the intended recoverability contract, so this is recorded as an unproven promise rather than a defect.

## Model vs code

- The active stopped-service model explicitly captures observed stopped state, physical sequence and the store's `AtSeq` fence (`tla/layer6-evidence/stopped-traces/checked-spec/Run.tla`, `RunTrace-MAPPING.md`). `run/tick.go:149-185` folds before planning and `run/tick.go:244-247` adds `store.Running{}` to each operation; the run trace test describes its projection assumptions and exclusions in `run/run_trace_test.go:15-34`. This supports the exercised trace only; it does not prove every production schedule or external dispatch atomicity.
- In `Run.tla`, `PreparePush` requires `Active(p)` (`tla/layer6-evidence/stopped-traces/checked-spec/Run.tla:264-270`), while `loop.tick`'s roster-error branch bypasses `Ticker.Tick`'s `Health.Check` and proceeds to push (`run/serve.go:151-164`). The existing lock-loss unit test exercises `Ticker.Tick`, not this `Serve` branch (`run/tick_test.go:215-229`); the current trace harness injects lock loss through the ticker path (`run/run_trace_test.go:529-538`).
- Layer 6's documented evidence note acknowledges that overflow recovery requires a `next` integration test (`docs/LAYER-6-RUN.md:581`). The source inventory in `verbs/next.go:142-165` and single-action selection in `verbs/next.go:186-200` do not by themselves demonstrate recovery of all omitted notices.

## Simplifications

- The run loop keeps the tick state in one `Ticker` and folds bounded event pages, while the pusher derives notices from committed event images (`run/tick.go:93-130`, `run/push.go:56-73`). I found no supported simplification that preserves these fencing and replay behaviors without broader layer review.

## Missing tests

- Add an integration test that fills an envelope past its byte/item limit, confirms the cursor advances only as designed, then follows the emitted `next` path until every omitted actionable obligation is surfaced. The current `TestEnvelopeIsBoundedWithNext` checks the byte cap and suffix (`run/tick_test.go:232-240`); it does not establish end-to-end recovery. This is consistent with the open evidence note at `docs/LAYER-6-RUN.md:581`.
- Add a `loop.tick` regression test where the roster read fails and chooser `Health.Check` reports lock loss; assert the loop returns `ErrLockLost` and performs no push, cursor write, or board publication. Existing `TestLostLockAppliesNothing` covers only direct `Ticker.Tick` (`run/tick_test.go:215-229`).
- The source/model coverage reviewed does not establish real crash recovery during send-success/cursor-write failure or the external stop/check/dispatch race; the stopped-model mapping explicitly excludes atomic external dispatch (`tla/layer6-evidence/stopped-traces/checked-spec/RunTrace-MAPPING.md`). I did not run functional tests for these cases.

## Questions for the coordinator

- Is “pushed at least once” intended to mean that each event's full notice is delivered, or that an overflow summary plus eventual `next` inventory is sufficient? The docs leave this interpretation dependent on an integration test.
- Does the common Linux receipt cover the exact base and the requested build, vet, and ordinary unit scopes? I did not run those gates; the job instructed me not to duplicate the parent's validation.

## Review scope and validation

Read `docs/LAYER-6-RUN.md` first, followed by `docs/ENFORCED.md`, `docs/LANDING.md`, and `docs/DASHBOARD-LESSONS.md`. The source pass covered the run loop/ticker/pusher/chooser, setup production and worker orchestration, CLI composition and lifecycle paths, dashboard cache/pull/read handlers and wire shapes, and the associated focused tests; the defect and promise-gap citations were checked against the fixed base. I inspected the active stopped Run model and mapping, the run trace harness, and Layer 6 witness/evidence inventory. I did not complete a line-by-line read of every `verbs/`, `cmd/nova-sprint/`, and `dash/` source and test file, every historical TLA+ artifact, or run the full model suite. No Go build, vet or test command was run here, per the parent-run gate instruction; the parent owns the shared Linux receipt.

### Final coordinator validation receipt

This postscript supersedes earlier statements that shared validation or help capture was pending. On the unchanged source base `43d849e29d0e4568ba6a1348a076a59259639938`, the parent ran Linux checks under a shared reservation with `GOMAXPROCS=4`: `go build -p 4 ./...` and `go vet -p 4 ./...` passed. `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` failed in policy's `TestDeadlineOnTheClock` (`policy/round5_test.go:26` and `:27`: expected version -1/three refusals, observed version 0/two refusals); run, verbs, command and dash packages passed. All 44 existing CLI verb/subverb/playbook help queries exited zero. These are parent-run results, not independent reviewer executions. Functional tests, live service setup, fuzzing and TLC were not run; source-review coverage limits above remain. The source archive SHA256 is `73b811e91414713188e0c24dbb6fdafa8580cd476d3e69104751b7c2e573788c`; the 53-file shared evidence manifest SHA256 is `49e12541e3756608ae7205d68b95f85105d510ca9b5e4c26ce003477209e3e3e`.
