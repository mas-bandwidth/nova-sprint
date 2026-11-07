# Cold review: layer 3, the steps (rowan, opus; card v2r-l3-heavy2)

Base `43d849e2` (branch `v2-stitch`). Read: `docs/LAYER-3-STEPS.md`, the layer 3
rows of `docs/ENFORCED.md`, every file of `steps/` (non-test and test),
`tla/Steps.tla` (the parts the code cites). Ran:

```
$ go build ./... && go vet ./...
BUILD-VET-OK
$ go test -count=1 ./steps/...
ok  	github.com/mas-bandwidth/nova-sprint/steps	42.351s
```

The unit tests use no real time, sockets or services: `store.Fake`, a fake
clock in `world` (`t0`, one second per op), `Inputs.Now`. The PG and load
paths are gated (`NOVA_LOAD`, `-skip Functional`) and were not run.

Four findings were reproduced with a throwaway test file
(`steps/zz_review_test.go`, written, run, deleted; not committed). Each repro
is quoted at its finding with the command and its output.

## Verdict

The layer is well shaped (one view, one oracle, groups claimed per card,
packed ops fenced on the gens read, a model bound by trace) and its suite is
green, but it is not sound enough to lock. The single worst thing: the
review gate merges on fewer reads than policy asked for. `dealRead` cuts only
as many reads as rows have room this tick (`steps/deal.go:155`), and `settled`
calls a review final as soon as no read is live and one is done, never
comparing against `reads(card)` (`steps/fix.go:46-57`); `fix` runs before the
deal in the same tick (`steps/tick.go:59-60`), so the owed read is never cut.
Reproduced: with `reads = 2` and one free non-author row, one read was cut and
its ok moved the producer to `merging`. Close behind: a blocker is neither
preempted nor dealt when policy's `work` is below the roster width
(`steps/preempt.go:80-87` vs `steps/deal.go:183-191`), with no `undealt`
judgement to say so (guarantee 4); `Verb`'s actor rule is default-allow for
every event it does not list (`steps/request.go:85-110`); and the landing's
`after` reorder loops forever on an `after` cycle (`steps/land.go:203-213`).

## Defects

Ranked, most severe first.

1. **serious: a review is decided on fewer reads than `reads(card)` asks for.**
   `steps/deal.go:155` cuts `pickRows(..., t.owed(c))`, which returns at most
   the rows with room (`steps/deal.go:223-242`), so an `AskReads` may cut 1 of 2.
   `steps/fix.go:46-57` (`settled`) returns the done reads once
   `!hasLive && len(done) > 0`, with no check of `readsWanted`; `fix()`
   (`steps/fix.go:16-41`) runs before `deal()` (`steps/tick.go:59-60`) and
   claims the producer, so the owed second read is never dealt and the verdict
   is over one read. `steps/fix.go:76` even says "a withdrawn one is owed
   again", while `settled` treats a withdrawn read as final. Breaks "AskReads
   cutting every read a head needs in one plan" (LAYER-3 "Deal and reads",
   LESSONS 3.4) and makes policy's `reads` a no-op under load. Repro (asker
   answering `Reads[p] = 2`; rows r1, r2, r3 width 1; r1 the author, r2 busy
   with p9):
   ```
   $ go test -count=1 -run TestZZPartialReadsVerdict -v ./steps/
   zz_review_test.go:72: p1 review, reads cut: [p1.r1.1@r3:working]
   zz_review_test.go:79: after the one read reports ok: p1 merging
   ```
   Repair: `settled` returns nil while `done + live < readsWanted(c)` (a
   withdrawn read stays owed), and the cut of a partial `AskReads` is either
   refused or the review waits until the remainder is dealt.

2. **serious: a ready blocker is neither preempted nor dealt when policy's
   `work` < roster width, and nothing says why.** `noFreeWidth`
   (`steps/preempt.go:80-87`) compares `Working(r)` with the roster `width(r)`;
   the deal's room is policy's `Work` (`steps/deal.go:183-191`, accepted when
   `0 <= Work <= width` at `steps/asker.go:279-288`). A row working at `Work`
   but under `width` is "free" to preemption ("the deal starts it",
   `steps/preempt.go:33-35`) and full to the deal (`withRoom`,
   `steps/deal.go:171-179`). `undealt` counts room with the same `withRoom`
   (`steps/hand.go:104`), so no `undealt` judgement opens. Breaks guarantee 4
   ("dealt by preemption in the tick that finds it ready") and guarantee 10
   (an in-domain answer plans nothing where the default would have). Repro
   (row r1 width 2, asker `Width[r].Work = 1`, p1 working on r1, b a ready
   blocker, five ticks 10 s apart):
   ```
   $ go test -count=1 -run TestZZBlockerUnderWork -v ./steps/
   zz_review_test.go:110: b ready after 5 ticks over 50s; open judgements map[engine:[] idle:[idle.fleet.1] policy:[...] undealt:[] unserved:[]]
   ```
   Repair: `noFreeWidth` uses `t.used(r) >= t.room(r)` (the deal's own room),
   so preemption and the deal agree on what full means.

3. **serious: `Verb`'s actor rule allows any actor any event it does not
   list.** `steps/request.go:85-110` refuses only `coordinatorEvents`,
   `runEvents` and a non-holder `Report`; everything else (`Start`,
   `AskReads`, `ReadYes`, `ReadFix`, `Resolve`, `Park`, `Reached`,
   `NeedFailed`, `Finished`, ...) falls to `producerMove` at
   `steps/request.go:149-156` for any actor. LAYER-3 "Requests" states the
   actor table as an engine fact ("a wrong coordinator could corrupt"), and
   ENFORCED L3-E5 says "a verb refused for its actor writes nothing". Today
   layer 6's verbs only build listed events (`verbs/moves.go`, `verbs/judge.go`,
   `verbs/land.go`), so the hole is latent, but the guard is in the wrong
   direction. Repro (actor `emma`, not coordinator, not holder):
   ```
   $ go test -count=1 -run TestZZVerbDefaultAllow -v ./steps/
   zz_review_test.go:17: p1 state before: ready
   zz_review_test.go:19: refusal=<nil> accepted=true reason=""
   zz_review_test.go:20: p1 state after: working
   ```
   Repair: make the actor table an allow-list (Add: anyone hooked; Report:
   holder; Seen/Gone: run; the coordinator list), refusing every other event
   name.

4. **serious: `eligible` never terminates on an `after` cycle, and reorders
   with a stale card.** `steps/land.go:203-213` loops `for changed` swapping
   `out[i]`, `out[j]` whenever `c.After` names `out[j]`; with `a.After=[b]`,
   `b.After=[a]` both merging it swaps forever, hanging the whole tick (and
   so the run loop). `c` is the range copy taken before the swap, so after a
   swap the inner loop keeps testing the old card's `After` against later
   entries and swaps the new `out[i]`. `add` and `Relink` refuse cycles, but
   `store.SetCardAfter` does not (ENFORCED `L2-U1`: "held by nothing"), and
   `steps/waits.go:140-141` already guards a cycle "a store op could still
   write". Repro:
   ```
   $ go test -count=1 -run TestZZEligibleAfterCycle -v ./steps/
   zz_review_test.go:35: eligible did not return within 2s (after cycle a<->b)
   ```
   Repair: order the picks by a topological sort over `after` restricted to
   the list (Kahn's, ties by queue order), dropping cycle members to an
   `engine` judgement.

5. **serious: a group refused alone on fresh state is never left out and
   never judged.** LAYER-3 "The tick" step 7: "A group refused alone on fresh
   state is left out until its cards change, with an `engine` judgement naming
   the plan and the store's reason". The only consumer of `Inputs.Refused` is
   `ops()` (`steps/pack.go:103-113`), which splits only `len(r.Groups) > 1`;
   a single-group refusal is replanned every tick, refused every tick, and
   `RefusedOp.Reason` (`steps/inputs.go:62-65`) is read by nothing. A
   deterministic layer-2/store disagreement becomes a silent per-tick
   refusal loop (guarantee 9: nothing waits unexplained). The model does not
   carry this rule either (`tla/Steps.tla:198`, `split` only of ops with two
   groups or more). Repair: for a refused op of one group, keep its key in
   the plan's carry-out and skip a group with the same cards and gens next
   tick, opening `engine` with `r.Reason`.

6. **serious: the judgement op is one fenced op, so one stale expiry loses
   every judgement the tick opened, including land judgements whose result
   was consumed.** `judgementOp` (`steps/judge.go:334-349`) puts every open
   and every expiry in one op with a `JudgementOpen` fence per expiry; its
   `Note` is empty, so `OpGroups` returns nil and nothing is split next tick
   (`steps/pack.go:103-113`). `landResults` consumes a non-landed result
   (`steps/land.go:36-38`, `t.consume`) while its `land` judgements ride only
   in that op (`steps/land.go:91`). A coordinator answering any expiring
   judgement between the snapshot and the apply refuses the op: the result
   is gone from the inbox, no land judgement is written, and the next tick
   asks the same landing again (a retry nobody chose). Breaks ENFORCED L3-L1
   ("opens one land judgement ... in that tick"). Repair: put expiries in an
   op of their own (or one op per expiry) and let a land result's judgements
   ride in the item's own group, as `riding` does for `blocked`.

7. **minor: "applied at seq N" says seq 0 after a cold build.**
   `steps/reports.go:58-59` acks a repeated report with `t.v.touched[c.ID]`,
   but `Build` (`steps/view.go:54-75`) never fills `touched`; only `foldCard`
   does (`steps/view.go:147`). After a restart every redelivered report is
   acked "applied at seq 0". Guarantee 6, LAYER-3 "Reports" ("acked from the
   record"). Repair: ack with the consumer's record (its `Gen`, or the seq the
   store's receipt names) rather than a fold-only witness.

8. **minor: a report with no session, or no head, skips the session and head
   checks.** `steps/reports.go:54` refuses a stale session only when
   `it.Session != ""`; `steps/reports.go:58` acks a duplicate for any head when
   `it.Head == ""`. LAYER-3 "Reports": "a session the row no longer has" is
   refused; the ack is for a report "matching a done consumer's result and
   head". Repair: treat an empty session or head as a refusal ("report names
   no session/head"), not a wildcard.

9. **minor: reports do not count toward the planning budget, so `undealt`
   blames the engine when reports spent the budget.** `reportGroup` adds with
   `t.add` (`steps/reports.go:80-86`), not `t.group`, so `t.planned`
   (`steps/tick.go:122-124`) never counts report groups; `overBudget()` then
   reads false during the deal although `admit` (`steps/pack.go:80-97`) will
   cut the deals, and `undealt` writes "the deal skipped it: an engine defect"
   (`steps/hand.go:108-111`). Repair: count report groups in `t.planned` as
   every other non-blocker group is.

10. **minor: a correct `rows` answer is scored a policy miss when its rows
    filled earlier in the same tick.** `pickRows` refuses `rows` when no
    answered row is in `rows` with room (`steps/deal.go:224-226`), but the
    candidates policy was offered do not include room (`steps/tick.go:211-215`:
    "Room is the engine's check at the deal"). Under load an in-domain answer
    opens a `policy` judgement (`steps/judge.go:148-150`). Repair: refuse only
    an answer naming a row outside the offered candidates; a full answered
    row is the engine's fallback, not a miss.

11. **minor: per-tick work grows with history (guarantee 11).** `alarms`
    calls `landRate` per stream (`steps/judge.go:262`), which scans every
    landed producer (`steps/judge.go:315-330`): O(streams x landed) every tick.
    `rest` scans every fault ever folded (`steps/limits.go:71-87`) and
    `View.Faults` is appended forever (`steps/view.go:152-153`); `TakenAt`,
    `ReadyAt`, `born`, `touched` (`steps/view.go:37-44`) are never pruned.
    Guarantee 11: "Nothing per tick is O(C) after the cold build". Repair:
    keep per-stream landed/clean counters and a per-row ring of the last N
    fault times, updated by the fold.

12. **minor: the fault rest ignores "of the last 10".** `rest`
    (`steps/limits.go:71-87`) counts faults inside `RouteRestFor` only;
    `Limits.RouteRestWindow` (validated at `steps/asker.go:318`) is never read.
    LAYER-3 "Retire and fail limits": "3 faults of the last 10 on a row in 30
    minutes". Repair: count within the last `RouteRestWindow` consumers'
    results on the row, then apply the time window.

13. **minor: `landRequest` sends an empty base without a judgement.**
    `steps/land.go:223` reads `t.in.Heads[s]` unchecked, where every deal path
    goes through `base()` (`steps/preempt.go:122-129`), which opens an `engine`
    judgement on a missing head. Repair: use `t.base(s)` and skip the stream
    when it has none.

14. **minor: a resolve refused by the machine still holds its files for the
    tick.** `steps/needs.go:42-48` records the producer in `resolvedP` before
    `t.group` may refuse it (`steps/tick.go:117-121`), so later producers open
    `overlap` "held behind <p>" for a producer that did not resolve. Repair:
    tally `resolvedP` only when `t.group` returns a group (and the budget
    case keeps its current rule).

15. **nit: the land queue sorts by `a.Score - b.Score`** (`steps/land.go:180`),
    an int subtraction that can overflow; `cmp.Compare` is the house form
    used everywhere else in the package (`steps/deal.go:69`).

16. **nit: `dealWork`'s comment contradicts its code.** `steps/deal.go:125`
    says the tally counts the deal "whether or not the budget admits it", but
    `steps/deal.go:118-119` returns before the tally when over budget; and the
    tally is kept even when `t.group` refuses (`steps/deal.go:125-129`).

## Promise gaps

- LAYER-3 "Deal and reads": "`AskReads{rows}` cutting every read a head
  needs in one plan, to distinct rows". Code cuts `min(owed, rows with room)`
  (`steps/deal.go:155`) and decides on the rest (Defect 1).
- Guarantee 4: "A ready blocker with no room is dealt by preemption in the
  tick that finds it ready". Code preempts only when `Working >= width`, the
  deal only when `used < Work` (Defect 2).
- Guarantee 10: "No policy answer ... plan nothing where the default would
  have planned". `WidthAnswer{Work: 0}` is accepted (`steps/asker.go:282`) and
  deals nothing to that row, with no `policy` judgement; with every row at
  `Work: 0` nothing is dealt.
- LAYER-3 "Requests" / ENFORCED L3-E5: the actor table. Code is default-allow
  (Defect 3).
- LAYER-3 "The tick", step 7: "A group refused alone on fresh state is left
  out until its cards change, with an `engine` judgement". Not built (Defect 5).
- LAYER-3 "Deal and reads", `rows` default: "for a read, one read on a row
  whose model differs from the author's when one is open, LESSONS 3.8".
  `ranked` (`steps/deal.go:256-287`) has no model term, and `rowFacts`
  (`steps/tick.go:180-194`) never fills `RowFacts.Model` or `Harness`
  (`steps/asker.go:45`), so policy's `rows` cannot do it either.
- LAYER-3 "Deal and reads": "never a row the card was withdrawn from, LPM
  D22". Code makes such a row last, not never (`steps/deal.go:270-272`,
  `-1000`), and its comment says so; the doc and the code disagree.
- LAYER-3 "Escalation": "one rank up once the producer's bounces reach
  `escalate_after` ..., one more per further step". `desired`
  (`steps/deal.go:300-307`) is +1 at `EscalateAfter` and the top at
  `FrontierAfter`, nothing between.
- LAYER-3 "Reports": refusals include "not taken (`take it first ...`)" and
  "Takes ride the same inbox ... the excess is refused (`row R is at width 2
  ...`)". There is no take item (`steps/reports.go:13-14`); the doc text is
  stale against the deal-is-take rebuild (the "Built" section says so, the
  "Reports" section does not).
- LAYER-3 "Judgements": "one `policy` judgement per version and question".
  The about is the question only (`steps/judge.go:149`), so while version N's
  judgement for `rows` is open, version N+1 failing `rows` opens nothing new;
  the version lives only in the group key (`steps/judge.go:126-128`).
- Guarantee 11 (cost): per-tick scans grow with landed and fault history
  (Defect 11); `needs`, `limits`, `holds`, `questions` and `unacked` also
  read every waiting/ready/review/live card each tick (the "Built" section
  admits "every tick re-reads the state"), so the "nothing per tick is O(C)"
  sentence is not true as written.
- LAYER-3 "Retire and fail limits": `silence` "2 hours", `bounce_limit` 3,
  "3 faults of the last 10". Code: `RowSilentWithdraw` 1800 s, `Bounces` 6
  (`steps/asker.go:157-158`; the Built section explains layer 5 is the later
  word), and the "last 10" is not read (Defect 12).

## Model vs code

- **Width vs work.** `Steps.tla` has only `Width[r]` (`tla/Steps.tla:123`):
  `Eligible` uses `Used < Width` (`:467`) and `NoFreeWidth` uses `Working >=
  Width` (`:374`), so the two agree in the model. The code's deal uses
  policy's `Work` (`steps/deal.go:183-191`) and preemption the roster width
  (`steps/preempt.go:80-87`); the gap of Defect 2 is outside every model run.
- **Reads per review.** The model cuts one read per review: `ReadOwed` is "no
  live or done read" (`tla/Steps.tla:265`) and `ReadOK` refuses a second live
  read (`:258`). The code's `reads(card)` (1..5) and partial `AskReads` are
  not modelled, so `Settled` (`:268`) agreeing with `settled`
  (`steps/fix.go:46-57`) proves nothing about Defect 1.
- **Escalation.** The model has `EscalateAfter` only (`tla/Steps.tla:129`,
  `:459-462`); the code's `FrontierAfter` jump to the top rank
  (`steps/deal.go:302-303`) is unmodelled.
- **Row score.** `Score` (`tla/Steps.tla:471-473`) has no withdrawn-from term;
  `ranked` subtracts 1000 for a row the card was withdrawn from
  (`steps/deal.go:270-272`). The trace walk only matches because its asker
  answers `rows` (`steps/trace_test.go:67`), so the default ranking after a
  preemption is never trace-checked.
- **Single-group refusal.** Neither the model (`split` only for ops of two
  groups or more, `tla/Steps.tla:770`) nor the code implements the doc's
  "left out ... with an engine judgement" (Defect 5).
- **Judgement op.** The model's `js` opens are not an op that a fence can
  refuse; the code's opens share one fenced op with the expiries
  (`steps/judge.go:334-349`), so `RefusalChangesNothing` holds while a
  consumed land result's judgement is lost (Defect 6).
- **Trace mapping of a rung-2 landing.** `ReqID` maps a land request to
  `<<e, "land", <<first card, 0>>, head of the first>>` (`steps/trace.go:168-177`);
  two requests differing only in the cards after the first map to one model
  id. Whether the model also names a group request by its first card I could
  not settle cold (Questions).
- **Inbox reports** are outside the model (documented), and so are the
  nudge, `unacked` and route-block paths (documented in "Built").

## Simplifications

- `judgeAgain` (`steps/judge.go:35-41`) is called by nothing (its only caller,
  the base-red path, went with intent rule 32). Delete it.
- `shares` (`steps/needs.go:95-103`) is used only by `walk_test.go:417`; move it
  into the test.
- `Limits.Faults`, `SameFinding`, `ReportsPerTick`, `RouteRestWindow`
  (`steps/asker.go:109`) are validated (`steps/asker.go:315-319`) and never read;
  `Defaults.IdleCeiling` (`steps/asker.go:141`) is unread while `cleared`
  hard-codes `working*4 >= open*3` (`steps/judge.go:202`); `LandingAnswer.Flowing`
  is validated and unused; `Timeouts.BaseRegate` is validated and unused
  in this package. Either read them or drop them from the engine's struct.
- `RowFacts.Model` and `Harness` (`steps/asker.go:45`) are never set; either
  fill them from the roster (and use them for LESSONS 3.8) or remove them.
- `holdMoves` computes a released state with
  `cmp.Or(map[bool]string{true: card.Waiting}[held], card.Ready)`
  (`steps/request.go:183`): a plain `if` says it.
- `noFreeWidth` re-derives width with a second definition of "full"
  (`steps/preempt.go:80-87`); using `withRoom` makes one definition (and fixes
  Defect 2).
- `eligible`'s fixed-point swap loop (`steps/land.go:203-213`) is a hand-rolled
  sort with an unbounded loop; a topological sort is shorter and bounded
  (Defect 4).
- `cleared` and `clearedDeal` (`steps/judge.go:166-236`) are one switch split
  in two for the 60-line lint; a map of kind to clear function would be one
  table, the shape `choices` (`steps/judge.go:20-25`) already has.

## Missing tests

- `reads(card) > 1` through `Tick`: no test drives a review with policy
  asking two reads and fewer rows with room (Defect 1). Only `asker_test.go`
  touches `Reads`, at the oracle.
- A blocker under a policy `work` answer below width (Defect 2); every
  preemption test uses the default `Work = width` (`steps/paths_test.go:19-80`).
- `TestVerbActorTable` (`steps/request_test.go:29`) checks the listed
  coordinator, row and stream events only; no case submits an unlisted event
  (`Start`, `ReadYes`) from a stranger (Defect 3).
- No test of `eligible` / `landRequests` with `after` among queued cards, and
  none with a cycle (Defect 4); `grep -l eligible steps/*_test.go` is empty.
- No test of a single-group refused op on the next tick (Defect 5);
  `TestTickRefusedOpSplitByGroup` (`steps/paths_test.go:245`) covers only the
  multi-group split.
- No test refuses the judgement op (Defect 6); `grep -l judgementOp
  steps/*_test.go` is empty.
- The repeated-report ack ("applied at seq N") is untested in any form,
  cold or folded (`grep -l "applied at seq" steps/*_test.go` is empty;
  Defect 7), as are the empty-session and empty-head report cases (Defect 8).
- `FrontierAfter` is untested (`grep -l FrontierAfter steps/*_test.go` is
  empty), as is the withdrawn-from ranking (`withdrawnFrom`).
- `landRate`, `land-rate` judgement and its clearing are untested.
- The load test (`steps/load_test.go`) seeds no landed history and no faults,
  so the per-tick growth of Defect 11 is outside the measured 47 ms plan.

## Questions for the coordinator

- Is a partial read set ever meant to decide a review (e.g. `reads = 2` but
  only one able row exists)? If yes, `cannot-read` or a judgement should say
  so; if no, `settled` must wait for `readsWanted` (Defect 1). LAYER-5's joint
  check (ENFORCED L5-3) refuses answers that the candidates cannot supply,
  but room this tick is not a candidate property.
- Is `WidthAnswer{Work: 0}` a legitimate "pause this row" answer? If so it
  needs its own judgement so the row is not silently idle; if not,
  `oracle.width` should require `Work >= 1` (`steps/asker.go:282`).
- Does layer 6 ever route reports through the inbox (`Kind report`)? If not,
  `reports.go` and Defects 7 and 8 are dead paths; if yes, they need tests.
- For the rung-2 trace mapping (`steps/trace.go:168-177`), does `Steps.tla`
  name a group land request by its first card and head only? If not, two
  distinct rung-2 requests share one model id in the trace.
- `verbs/moves.go:20` calls `steps.Verb` with `Inputs{Now}` only, no `Roster`,
  so `t.rows()` (`steps/group.go:182-190`) gives the machine every row with
  width 0 and no tiers. Out of this layer's scope; flagging it for layer 6's
  reviewer in case a verb's move reads width or tiers.
