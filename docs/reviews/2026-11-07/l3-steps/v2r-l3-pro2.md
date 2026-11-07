# Review: layer 3, the steps — v2r-l3-pro2

Cold review of package `steps` (steps/**), `docs/LAYER-3-STEPS.md`, `docs/ENFORCED.md` §7, and `tla/Steps.tla`. Commands run on the staged checkout:

```
go build ./...          # clean
go vet ./...            # clean
go test ./steps/...     # ok   github.com/mas-bandwidth/nova-sprint/steps 65.489s
```

The unit suite uses no real time/sockets/services (only `store.Fake`; `Inputs.Now` drives every bound). `go build`/`go vet` clean.

## Verdict

The layer is broadly sound: the tick order, one-group-per-card claiming, the budget/pack/carry machinery, and the model/code refinement are implemented and tested as promised, and build/vet/unit are green. The single worst thing is that preemption and the deal measure row fullness against two different thresholds — preemption against the roster **width** (`steps/preempt.go:82`), the deal against policy's `work` answer (`steps/deal.go:188`) — so under a policy that answers `width(row).work < width`, a ready blocker whose serving rows sit in the gap `work <= working < width` is neither dealt nor preempted and can wait indefinitely, violating guarantee 4 in a way the TLA+ model (which models width as a constant, with no `work` oracle) cannot see. A close second is that a `land` judgement is never auto-cleared when its card lands through the automatic `landed` path and carries no deadline, so it can linger open forever.

## Defects

1. **serious — Preemption and the deal disagree on when a row is full; guarantee 4 can stall a blocker.**
   `steps/preempt.go:82` (`noFreeWidth`: `t.v.Working(r) < t.width(r)` — the roster width) vs `steps/deal.go:174` (`withRoom`: `t.used(r) < t.room(r)`) and `steps/deal.go:184-191` (`room` = policy's `width(row).work` answer, which the oracle allows to be `< width`, `steps/asker.go:282`).
   Why: when policy answers `work < width`, a row with `work <= working < width` has no deal room (the deal won't `Start`) yet is not "at width" (so `preempt` skips it). A ready blocker whose every serving row is in that state is neither dealt nor preempted and sits ready forever; `undealt` only judges it. This breaks guarantee 4 ("A ready blocker with no room is dealt by preemption in the tick that finds it ready") and the `BlockerPreemptsInOneTick` invariant's spirit.
   Repair: make `noFreeWidth` use the same policy-`work` measure as `withRoom` (compare against room/used, not roster width).

2. **serious — An open `land` judgement is never cleared when its card reaches `landed` by any non-answer path, and it has no deadline, so it stays open indefinitely.**
   `steps/judge.go:135` (no deadline for `land`), `steps/judge.go:166-211` (`cleared` switch has no `land` case), and `steps/judge.go:216-236` (`clearedDeal` has none either). `cleared` handles `unserved`, `cannot-read`, `blocked`, `unacked`, `undealt`, `stall`, `idle`, `land-rate`; `clearedDeal` handles `overlap`, `no-model`, `no-policy`; neither handles `land`.
   Why: a `land` judgement can only close by an explicit coordinator answer (`land`/`fix`/`retry`). The model and code both allow a merging card to land by a non-answer route (hand-land, or a store op such as reconcile/restore — the model's `HandLandsOf`/`EnvMove`), and in those cases the card becomes `landed` with the judgement still open, no deadline, and no `cleared` case to close it. It lingers on the board forever, and a later non-answer landing of a judged card is also not covered by guarantee/ENFORCED L3-L1's "the cause clears".
   Repair: add a `land` case to `cleared` closing the judgement when its card is no longer `merging` at the judged head.

3. **minor — Escalation is a single rank bump, not "one more per further step".**
   `steps/deal.go:300-308` (`desired`): `want++` once at `escalate_after`, then a jump to the top at `frontier_after`. The doc promises "one rank up once the producer's bounces reach escalate_after …, one more per further step".
   Why: between `escalate_after` and `frontier_after` bounces all escalate identically, so the promised per-bounce step (favouring the busy card to the next tier) never happens.
   Repair: set `want = tierIndex(c.Tier) + (c.Bounces - Engine.EscalateAfter + 1)` capped at `top`, keeping the `frontier_after` jump.

4. **minor — `ReportsPerTick` (and `SameFinding`, `RouteRestWindow`) are accepted and validated as limit answers but never consulted.**
   `steps/asker.go:109` (fields), `steps/asker.go:315-319` (validated in `limitsOK`); a package-wide grep finds no reader of `ReportsPerTick`, `SameFinding` or `RouteRestWindow` outside `asker.go` and tests.
   Why: a policy that sets `reports_per_tick` changes nothing — report burst is bounded only by `Budget`/`Pack`; a policy author believes they are enforcing a limit. Silent dead knob.
   Repair: enforce `ReportsPerTick` in `reports()` (or remove the field from `Limits` and `limitsOK`).

5. **minor — The row-rest fault rule does not match LPM R34 "3 faults of the last 10 on a row in 30 minutes".**
   `steps/limits.go:71-87` (`rest`): counts every fault inside a `RouteRestFor` window and rests when `recent >= RouteRestAfter`, with no "of the last 10" cap.
   Why: a row with many stale faults (or an unbounded fault list) can rest on total volume rather than a recent burst, diverging from the documented rule.
   Repair: count only within the last 10 faults and the documented window.

6. **nit — `holdMoves` synthesizes the producer's state by hand for a `Block` from working instead of delegating the whole transition to the machine.**
   `steps/request.go:180-186`: it rewrites `after.State` to `Waiting`/`Ready` and mutates `cons` in place before `card.Move`. This is special-case logic the doc claims lives in the engine (LAYER-2 `Block`), and it is not trace-bound.
   Why: a divergence between this synthesis and `CardMachine`'s `Block` guards would not be caught by the model.
   Repair: derive the post-withdraw producer state from the machine, or add a trace/unit case for a working-producer card hold.

## Promise gaps

- "A ready blocker with no room is dealt by preemption in the tick that finds it ready" (guarantee 4) — **not kept** when policy `work < width` (defect 1). The preemption gate uses roster width, not the same `work` the deal uses.
- "An `after` orders only producers both queued and never holds on one that is not" (LESSONS 2.15) — kept (`steps/land.go:186-215` `eligible`); no gap found.
- "A report for a stale head, attempt or session is refused … a repeated report is acknowledged and applied once" (guarantee 6) — implemented in `steps/reports.go:41-69`; the old-attempt consumer id change (`<p>.w<attempt>`) naturally routes stale-attempt reports to "card does not exist", so the refusal set is covered.
- "The budget … the carry is arrival order and reports come before deals" (guarantee 7) — kept (tick order, `pack.go:60-73` order, `reports()` first). Budget is enforced twice (plan-time in `group()` and admit-time in `admit()`); both stop after the first non-blocker group. No gap found.
- "No policy answer, absence or error … plan nothing where the default would have planned" (guarantee 10) — kept except the documented single exception: the policy gate (`no-policy`) stops all dealing when no version answers (`steps/model.go:75-81`), which the doc itself exempts.
- "A hold never touches landing" (LANDING 9) — kept; `holds()` does not touch merging/requests.
- "Judgement ids are `kind.about.episode` (a cause opens once)" — kept, but see defect 2: a `land` cause never reads as cleared.

## Model vs code

- **Width/`work` oracle missing from the model.** `tla/Steps.tla` models width as a constant and has no `work` answer; therefore the preemption-vs-deal threshold mismatch (defect 1) is invisible to `BlockerPreemptsInOneTick`/`NoFreeWidth`. The model's `RoomOn` guard cannot see policy `work < width`.
- **Inbox report path not modelled.** The model's `Report` is a verb op applied to the store; the entire inbox `report` refusal path (`steps/reports.go`) — consumer-exists, holder, session, withdrawn, done-ack, attempt, head refusals — is unmodelled (the doc's "Built" section says so; `LAYER-6`'s path is the model's).
- **Unmodelled judgement kinds and alarms.** Steps.tla produces only `unserved`, `cannot-read`, `policy`, `overlap`, `no-policy`, `land`, `undealt`. `roster`, `flap`, `stall`, `idle`, `land-rate`, `brief`, `engine`, `blocked` (route faults) and the per-card request `Block`/holds are not in the model; nudges/`unacked`/deadlines need a clock the model does not have.
- **Ticks vs seconds.** The model counts `undealt` in ticks (`rdy`, `UndealtTicks`) and the code in seconds (`UndealtAfter`); the doc marks this "not trace-bound".
- **Consistent by design:** no `Take` in either (deal-is-take, defect 52); the `victim` oracle is fixed in both (youngest working), not a policy question.

## Simplifications

- Escalation collapsed to one rank bump + a frontier jump (defect 3) vs the documented per-step climb.
- The row-rest rule is a simple window count, not "3 of the last 10" (defect 5).
- `ReportsPerTick`, `SameFinding`, `RouteRestWindow` are accepted-but-inert knobs (defect 4).
- `ahead` removed (width/`work` only) and `flowing` carries no logic — both match the doc.
- The `victim` rule is fixed (youngest by take time, id tie-break) rather than policy — matches "the engine's fixed rule".

## Missing tests

- No test drives policy `width(row).work < width` against preemption and the deal (defect 1); the existing preemption test (`steps/paths_test.go:19-44`) uses work == width and cannot see it.
- No test asserts an automatic `landed` result closes (or that the code should close) an open land judgement on the same card (defect 2).
- No test exercises escalation across multiple bounces between `escalate_after` and `frontier_after` (defect 3); `steps/trace_test.go:32-34` only flips `EscalateAfter`.
- No test asserts `ReportsPerTick`/`SameFinding`/`RouteRestWindow` are honored — or documents that they are not (defect 4).
- No unit test covers the inbox `take` kind (a documented `InboxItem.Kind` that `reports()` never consumes).

## Questions for the coordinator

- Is the preemption threshold meant to be policy's `work` (sharing the deal's room measure) or the roster width? Which promise wins — guarantee 4 or the LESSONS width model?
- Should a `land` judgement auto-clear when the card lands automatically, or is explicit coordinator answering the only intended close path?
- Should `ReportsPerTick` bound the report step, or is the plan budget the sole bound?
- Is the hand-rolled `holdMoves` state synthesis acceptable, or should `Block`-from-working go through the card machine only?
- Are `take` inbox items a real layer-6 path (and thus should be answered/consumed), or dead format?
