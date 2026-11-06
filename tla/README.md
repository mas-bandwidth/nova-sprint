# tla/: the store model (layer 1) and the card machine (layer 2)

Two models live here: `Store.tla` (layer 1, below) and `CardMachine.tla`
(layer 2, in its own section after the store's results). `ci-tlc.sh` runs
both, with all their witnesses and reach checks, in one parallel pass.

`Store.tla` models the contract in `docs/LAYER-1-STORE.md`. Each operation is
one atomic step over `epoch`, `cards`, `rows`, `events` and `applied`. In
order, the step:
1. checks the op's epoch against the store's epoch;
2. checks its conditions;
3. runs its writes in order, each under its own rule;
4. either lands everything and appends an accepted event, or changes nothing
   and appends a refused event.

Epochs start at 1. `Clear` is an op with no writes and its epoch is checked
too. It opens the next epoch, carries the rows forward (same width, state
`down`) and leaves the cards behind. It is idempotent by id and logged with
the epoch it closed.

The conditions are:
- `CardGen`, `CardState`, `HolderIs` and `CardAbsent` on a card;
- `NeedsLanded`: every need of the card is landed, read from the store in the
  step;
- `RowIs` on a row.

There is no width condition. Width is kept by two write rules:
- **Set holder (to a row):** refused when that row already holds as many
  cards as its width, counted at the moment of the write. So one op with two
  holder writes to a width-1 row is refused on the second, and nothing lands.
- **Put row:** refused if it would set a width below the number of cards the
  row holds.

An op that writes a card raises that card's gen by one, however many of its
writes touch the card. A card the op itself adds stays at gen 0.

The card states are `waiting`, `ready`, `working`, `review`, `merging`,
`landed` and `fail`.

The caller's view is modelled too:
- `phase` per op: `idle`, `unknown` or `done`;
- `receipt`: the receipt the caller holds;
- `lost`: the transport failures so far.

A transport failure is either a request lost before the store saw it, or a
reply lost after the store applied it. Either way the caller is left
`unknown` and recovers with `Replay`, or with `Apply` again.

## What one model step stands for in SQL

One step of `Store.tla` is one SQL transaction. It runs at `READ COMMITTED`
from `LOCK TABLE sprint.epoch` to `COMMIT`, and checks the epoch, the
conditions, the writes with their rules, and the event row inside it. The
lock is what makes the atomic step real. Without it, two transactions at
`READ COMMITTED` can each read the rows, decide, and then write. Two
witnesses are exactly that interleaving:
- **StaleCheck:** every condition is judged on an earlier read, so two takes
  of one card both land.
- **WidthStale:** the holder write's room check is judged on an earlier
  count, so two deals overfill a width-1 row.

With the lock, writers run one at a time and each one reads the state the
previous one committed. That serial order is the order of `events` here.

## The instance

3 cards, 2 rows, 15 fixed ops. Callers issue their ops in program order
(`Pred`). A setup caller goes first:
- s1 to s3 add c1, c2, and c3 (which needs c1);
- s4 and s5 put rows r1 and r2, width 1, up;
- s6 deals c2 and c3 to r2 in one op. It is always refused on its second
  holder write.

Then six callers run concurrently:

| Caller | Ops | What it does |
|---|---|---|
| a | a1, a2 | a1 takes c1 for r1; a2 lands c1 if r1 holds it |
| b | b1, b2 | b1 takes c1 for r2 (racing a1 on the same expectation); b2 reassigns c1 to r1 (refused by the holder write if r1 is full) |
| w | w1 | deals c2 to r1 (racing a1 for r1's one slot) |
| n | n1 | readies c3 once its need c1 is landed |
| e | e1 | lowers r1's width to 0: accepted when r1 holds nothing, refused when it holds a card; also an epoch-1 op that may arrive after the clear |
| k | k1, k2 | k1 clears epoch 1; k2 adds c2 in epoch 2 |

Transport failures happen only on a1 and k1 (`Lossy`, `MaxLoss = 1`).

Left out:
- streams, policy and the seq fence;
- the card and row fields that no rule reads (attempt, branch, head, pr,
  reason, score, kind, session, flaps). They add columns, not behaviour.

There is no heartbeat: rows have no seen field.

`witnesses/` holds eight reversed witnesses. Each is a copy of `Store.tla`
with one deliberate fault, and TLC must report a violation for each. In each
witness `.cfg` the target invariant is listed first: when one state breaks
several invariants, TLC names the first one in the list. `reach/` holds nine
reachability checks on `Store.tla`. Each must be violated, which shows the
passing run is not vacuous.

## Running

Run TLC on a bench host, never on the working machine. `ci-tlc.sh` is the
gate. It runs every module in parallel:
- `Store.tla` must pass;
- every `witnesses/*.tla` and every `reach/*.cfg` must report a violation
  (TLC exit 12).

It exits non-zero on any other outcome: a pass where a violation is
required, a parse error, a crash, or a run that wrote no result. It uses a
pinned, sha256-checked `tla2tools.jar` (v1.8.0), gives each run its own
`java.io.tmpdir`, and passes `-noGenerateSpecTE`, so a rerun in the same
tree never sees trace modules.

```
bash ci-tlc.sh
```

By hand, from this directory:

```
java -jar tla2tools.jar -workers 4 -config Store.cfg Store.tla
java -jar tla2tools.jar -config reach/<Name>.cfg Store.tla
```

and from `witnesses/`:

```
java -jar tla2tools.jar -config <Witness>.cfg <Witness>.tla
```

The helper scripts:
- `sh run-tlc.sh /path/to/tla2tools.jar [java]` runs the same set, keeps one
  log per run in `logs/`, and gives the state counts below. It runs the
  card-machine set too (see the card machine section).
- `sh run-one.sh` runs a single one.
- `sh summarize-logs.sh` prints the result lines.
- `sh show-trace.sh <name>` prints a counterexample's steps and its event log.

## Invariants

- **TypeOK**: every variable stays in its type.
- **SingleStateSingleHolder**: every card has exactly one state from the
  closed set (or is absent), at most one holder, and that holder is a row of
  this epoch. This is structural in `Store.tla` and stated anyway.
- **WidthKept**: no row has more cards holding it than its width.
- **IdempotentById**: no op id appears twice in the event log.
- **AppliedIsLog**: the op ids the store remembers are exactly the ids in the
  event log.
- **LogMatchesState**: folding the accepted events from empty, with a clear
  applied the same way the store applies it, gives the current state. Each
  event records the epoch it ran in. It is checked as a chain, which is the
  fold by induction:
  - each event starts where the one before it ended;
  - each accepted event's after-state is its writes applied to its
    before-state;
  - the last event ends at the current state.
- **OpsInTheirEpoch**: every accepted op, clear included, was planned against
  the epoch the store was at when it applied. So after a clear, a delayed op
  planned against the old epoch is refused.
- **ConditionsAtomic**: every accepted op's conditions held against the state
  immediately before its own writes, so no update is lost.
- **NoChangeOnRefusal**: a refused op's before- and after-states are equal,
  and both are the state immediately before it.
- **ReceiptMatches**: a caller that thinks it knows the outcome holds exactly
  the stored receipt. Unknown is never reported as unchanged.

## Expected results

| Module | Fault | Expected result |
|---|---|---|
| `Store` | none | pass, no violation |
| `witnesses/StoreNoEvent` | an accepted op's writes land without appending its event | violation of LogMatchesState |
| `witnesses/StoreReplayApplies` | Replay (and Apply of a known id) runs the op again and logs it again | violation of IdempotentById |
| `witnesses/StoreStaleCheck` | the epoch and conditions are checked against a snapshot read before another op's writes (no lock) | violation of ConditionsAtomic |
| `witnesses/StoreTwoHolders` | holder is a set; set holder adds without clearing the previous holder | violation of SingleStateSingleHolder |
| `witnesses/StoreEpochUnfenced` | ops other than clear are not checked against the store's epoch | violation of OpsInTheirEpoch |
| `witnesses/StoreWidthStale` | the holder write counts the row's holders from a snapshot read before another deal (no lock) | violation of WidthKept |
| `witnesses/StoreWidthUnchecked` | put row accepts a width below the row's holders | violation of WidthKept |
| `witnesses/StoreWidthPerOp` | the holder write counts holders as at the start of the op, not at the moment of the write | violation of WidthKept |
| `Store` + `reach/NeverTaken.cfg` | none | violation: a take (a1 or b1) is accepted |
| `Store` + `reach/NeverRecovered.cfg` | none | violation: a caller left unknown reaches done |
| `Store` + `reach/NeverStaleEpochRefused.cfg` | none | violation: an old-epoch op arriving after the clear is refused |
| `Store` + `reach/NeverNeedsReady.cfg` | none | violation: n1 passes NeedsLanded |
| `Store` + `reach/NeverNewEpochOp.cfg` | none | violation: an op planned against epoch 2 is accepted |
| `Store` + `reach/NeverWidthFull.cfg` | none | violation: a row fills to its width |
| `Store` + `reach/NeverWidthLowered.cfg` | none | violation: e1 lowers r1's width |
| `Store` + `reach/NeverWidthRefused.cfg` | none | violation: e1 is refused because r1 holds a card |
| `Store` + `reach/NeverInOpWidthRefused.cfg` | none | violation: s6 is refused on its second holder write |

## Results, TLC 2.19 (tla2tools v1.8.0, the CI jar) on hetzner2, 2026-11-06

All eighteen ran in one parallel pass with `run-tlc.sh`. `ci-tlc.sh` then
passed twice in a row in the same tree. With StoreNoEvent replaced by the
fault-free model, it failed exactly that witness and exited 1.

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `Store` | No error has been found | 1035189 / 618632 | 18 | 35s |
| `StoreNoEvent` | Invariant LogMatchesState is violated | 2 / 2 | 2 | 01s |
| `StoreReplayApplies` | Invariant IdempotentById is violated | 26 / 26 | 9 | 01s |
| `StoreStaleCheck` | Invariant ConditionsAtomic is violated | 463 / 272 | 17 | 01s |
| `StoreTwoHolders` | Invariant SingleStateSingleHolder is violated | 46 / 43 | 9 | 01s |
| `StoreEpochUnfenced` | Invariant OpsInTheirEpoch is violated | 86 / 79 | 9 | 01s |
| `StoreWidthStale` | Invariant WidthKept is violated | 526 / 323 | 17 | 01s |
| `StoreWidthUnchecked` | Invariant WidthKept is violated | 22 / 22 | 9 | 01s |
| `StoreWidthPerOp` | Invariant WidthKept is violated | 7 / 7 | 7 | 01s |
| reach `NeverTaken` | violated | 8 / 8 | 8 | 01s |
| reach `NeverRecovered` | violated | 26 / 26 | 9 | 01s |
| reach `NeverStaleEpochRefused` | violated | 80 / 74 | 9 | 01s |
| reach `NeverNeedsReady` | violated | 107 / 90 | 10 | 01s |
| reach `NeverNewEpochOp` | violated | 87 / 80 | 9 | 01s |
| reach `NeverWidthFull` | violated | 8 / 8 | 8 | 01s |
| reach `NeverWidthLowered` | violated | 14 / 14 | 8 | 01s |
| reach `NeverWidthRefused` | violated | 22 / 22 | 9 | 01s |
| reach `NeverInOpWidthRefused` | violated | 7 / 7 | 7 | 01s |

The distinct state count barely moves between design rounds. That is
expected: every op's id is logged in order, so the states correspond one to
one with the interleavings allowed by program order, whatever each op's
outcome. The reach checks are what show the new behaviour is there.

The counterexamples were checked by hand (`show-trace.sh`):
- **StoreNoEvent:** s1 lands with no event.
- **StoreReplayApplies:** a1 is accepted, its reply is lost, and the retried
  Apply runs a1 again and logs it a second time.
- **StoreStaleCheck:** a1 and b1 both begin, then both commit and are both
  accepted.
- **StoreTwoHolders:** b1 takes c1 for r2, then b2 adds r1.
- **StoreEpochUnfenced:** k1 clears epoch 1, then the epoch-1 e1 is accepted
  in epoch 2.
- **StoreWidthStale:** s6 is refused; then a1 and w1 both begin with a count
  of 0 on r1, and both land.
- **StoreWidthUnchecked:** a1 takes c1 for r1, then e1 sets r1's width to 0.
- **StoreWidthPerOp:** s6 lands both of its holder writes on the width-1 row
  r2.

# The card machine (layer 2)

`CardMachine.tla` models the two tables of moves in
`docs/LAYER-2-CARD-MACHINE.md`. Each action is one accepted event. Its guard
is the design's from-state plus the event's extra conditions, and nothing
else. An event that does not match is simply not enabled: the Go code
refuses it with a value and nothing changes. So the model is the reachable
state space of accepted moves.

- Card events: Resolve, Deal, Finish, DealRead, ReadYes, ReadFix, Landed,
  Red, Release, Cancel, Replace, NeedFailed, Rescore.
- Row events: Seen, Gone, Flapped (with the constant `Limit`), Unblock,
  Width.
- Environment: Hold and Unhold set the stream's held flag, which only Deal
  reads.

Deal and DealRead write the holder. Layer 1 refuses a holder write to a row
that already holds as many cards as its width, so both are enabled only
below the width (`HolderWriteFits`). The guard is that write's refusal,
stated in the model. Width refuses a value below the row's held count, as
layer 1's put row does.

`last` is a history variable: the last event, its card and row, and every
card as it was before the step. The step invariants read it.

## The instance

- Two cards: c2 needs c1.
- Two rows, r1 and r2: they start down, width 1, flaps 0.
- One stream with a held flag.
- Heads are 1..2; 0 means no head yet. A read result, a landing or a red
  result carries a head and is enabled only when it equals the card's head.

These bounds are the model's, not the design's:
- attempt stops at 2 (Deal is not enabled past it);
- flaps stop at Limit + 1 = 2 (Gone is not enabled past it);
- scores are 0..1;
- widths are 1..2.

Left out, because no guard reads them: branch, base_sha, reason, the `by`
of Replace and the need named in NeedFailed's reason.

Release carries a row, and is enabled only for the holder. The design's
refusal list says "a release by a row that is not the holder", but its table
writes `Release{reason}` with no row.

## Invariants

- **TypeOK**: every variable stays in its type.
- **OneHolder**: one holder column per card, a row or none. Structural;
  stated anyway.
- **HolderNoneInRestStates**: holder is none in waiting, ready, merging,
  landed and fail.
- **ReaderNeverAuthor**: in review, a reader is never the card's author.
- **WorkingHasHolder**: in working, the holder is a row.
- **WidthKept**: no row holds more cards than its width.
- **FinalNeverMoves**: a card that was landed or fail before a step is
  unchanged after it, and that includes Rescore.
- **AttemptOnlyOnDeal**: attempt rises by one on a Deal of that card, and is
  unchanged by every other step.
- **RedNeverAdvances**: a Red step never ends in merging or landed. It is a
  step property: after a red, a new read may say yes, which is legal.
- **FixKeepsScore**: a ReadFix step leaves the score as it was.
- **OnlyHolderActs**: Finish, ReadYes, ReadFix and Release are accepted only
  from the card's holder.
- **NeedFailedTakesDependants**: whenever a need has failed and its dependant
  is not final, NeedFailed for that dependant is enabled. The design's "a card
  whose need failed is failed" holds only after layer 3's tick; between the
  two steps the dependant is still live, so this is the layer-2 form of the
  rule. `reach/NeverNeedFailed` shows the move fires.

## Expected results

| Module | Fault | Expected result |
|---|---|---|
| `CardMachine` | none | pass, no violation |
| `witnesses/CardLandedMoves` | Rescore enabled from landed and fail | violation of FinalNeverMoves |
| `witnesses/CardAuthorReads` | DealRead without the author check | violation of ReaderNeverAuthor |
| `witnesses/CardRedAdvances` | Red moves merging to landed | violation of RedNeverAdvances |
| `witnesses/CardNeedFailedIgnored` | NeedFailed refused once the need has failed | violation of NeedFailedTakesDependants |
| `witnesses/CardReleaseBumpsAttempt` | Release raises attempt | violation of AttemptOnlyOnDeal |
| `witnesses/CardNonHolderFinishes` | Finish without the holder check | violation of OnlyHolderActs |
| `CardMachine` + `reach/NeverLanded.cfg` | none | violation: a card lands |
| `CardMachine` + `reach/NeverFixed.cfg` | none | violation: a read says fix |
| `CardMachine` + `reach/NeverRed.cfg` | none | violation: a merging card goes red |
| `CardMachine` + `reach/NeverNeedFailed.cfg` | none | violation: NeedFailed fires after its need failed |
| `CardMachine` + `reach/NeverBlocked.cfg` | none | violation: a row flaps to blocked |

## Results, on hetzner2, 2026-11-06

TLC as the pinned jar reports itself: `TLC2 Version 2026.11.06.100558 (rev:
3ec1cc4)`. All twelve ran in one parallel pass with the store's runs
(`run-tlc.sh`, CardMachine on 8 workers, the others on 1). `ci-tlc.sh` then
passed on hetzner2.

A reach cfg named in `CARD_REACH` (in `ci-tlc.sh` and `run-tlc.sh`) checks
`CardMachine.tla`; every other cfg in `reach/` checks `Store.tla`. A card
reach cfg left off that list fails to parse against Store, so it shows as a
FAIL, never as a silent skip.

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `CardMachine` | No error has been found | 169196946 / 13704193 | 33 | 1min 02s |
| `CardLandedMoves` | Invariant FinalNeverMoves is violated | 36 / 36 | 3 | 01s |
| `CardAuthorReads` | Invariant ReaderNeverAuthor is violated | 15244 / 3485 | 6 | 02s |
| `CardRedAdvances` | Invariant RedNeverAdvances is violated | 313954 / 48375 | 9 | 04s |
| `CardNeedFailedIgnored` | Invariant NeedFailedTakesDependants is violated | 3 / 3 | 2 | 01s |
| `CardReleaseBumpsAttempt` | Invariant AttemptOnlyOnDeal is violated | 3154 / 1079 | 5 | 01s |
| `CardNonHolderFinishes` | Invariant OnlyHolderActs is violated | 3157 / 1082 | 5 | 02s |
| reach `NeverLanded` | violated | 313953 / 48374 | 9 | 03s |
| reach `NeverFixed` | violated | 130847 / 22285 | 8 | 03s |
| reach `NeverRed` | violated | 313954 / 48375 | 9 | 03s |
| reach `NeverNeedFailed` | violated | 37 / 37 | 3 | 01s |
| reach `NeverBlocked` | violated | 1461 / 652 | 4 | 01s |

The counterexamples were checked by hand:
- **CardLandedMoves:** c1 is cancelled (fail, final), then Rescore changes
  its score.
- **CardAuthorReads:** r1 is dealt c1 and finishes it, then r1 is dealt the
  read of its own head.
- **CardRedAdvances:** c1 is dealt to r1, finished, read yes by r2 and is
  merging; then Red lands it.
- **CardNeedFailedIgnored:** c1 is cancelled. c2 is waiting, and NeedFailed
  for it is not enabled.
- **CardReleaseBumpsAttempt:** c1 is dealt to r1 (attempt 1); then Release
  makes it attempt 2.
- **CardNonHolderFinishes:** c1 is dealt to r1; then r2 finishes it.
- **reach:** NeverLanded and NeverRed each run deal, finish, read and yes,
  then Landed or Red. NeverFixed is the same path ending in ReadFix.
  NeverNeedFailed cancels c1, then fires NeedFailed for c2. NeverBlocked runs
  Seen, Gone and Flapped on r1.
