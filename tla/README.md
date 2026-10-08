# Checking the coordination rules

Several AIs finishing, retrying, and handing work to one another can expose
ordering bugs that a happy-path example will miss. These TLA+ models explore
small, explicit versions of the sprint's state machines and look for broken
invariants or work that can stop making progress.

You do not need TLA+ to use nova-sprint. This directory is for contributors
changing scheduling, handoffs, recovery, or the work-tree design.

## Choose a model

| Model | What it explores |
|---|---|
| [SprintEvents](README-SprintEvents.md) | Event-driven scheduling, leases, deadlines, notifications, and interruptions. |
| [DirtyTick](README-DirtyTick.md) | How a tick drains queues and moves work between tables. |
| `CapDeal` | The attempt cap's cap deal: a card past its cap to a friend-tier friend up with room, and the judgment at its redeal bound with none. |
| `DirtyTickRead` | Reader handoffs within the tick. |
| `RouteIndex` | Route selection and its counters. |
| `Level` | Distribution of queued work across available capacity. |
| `Land` | The landing protocol. |
| `WorkImport` | Issue import and the specified work-tree modes. |

The `.tla` files define the models. `MC*.tla` and `.cfg` files provide bounded
instances. [CASES.tsv](CASES.tsv) lists the gated cases; [RUNS.tsv](RUNS.tsv)
records results. The two longer guides explain the assumptions, findings,
and repairs for their models.

## Read a result in context

An invariant is a condition that should always hold. A liveness property
asks whether progress eventually happens under the model's stated fairness
assumptions. A **reversed witness** deliberately breaks a rule: its expected
counterexample checks that the property can catch that defect.

A recorded pass applies to the model, bounds, revision, and configuration in
that run. It does not prove that a live deployment has no bugs, that a model
provider is available, or that all possible fleet sizes were checked. Keep
counterexamples and measured results intact when improving their explanation.

## CapDeal's reversed witnesses

`CapDeal.tla` takes a `Broken` constant naming the rule the instance leaves out
(`"none"` is the design). The gated control is `MCCapDeal.cfg`; the five
reversed-witness instances that turn one defect on at a time are fixtures under
[testdata/](testdata/) (`MCCapDealBroken{ MachineFirst,NoJudge,Twice,IdNoJudge,
IdTwice }.cfg`). They are fixtures rather than gated cases because the case plan
reads only the top-level `tla/MC*.cfg` files. Each was run by hand on the Linux
bench and each reaches its declared property: `FriendFirst` for `machinefirst`,
`NoCardLost` for `nojudge` and `idnojudge`, and `ExactlyOneJudgment` for `twice`
and `idtwice`.

## Run checks on a Linux bench

The TLC runner remains in
[Nova Tools](https://github.com/mas-bandwidth/nova-tools/tree/dev/tools/tlacheck).
Build it there and use its `--root` option to point at this nova-sprint checkout.
Put the built `tlacheck` on `PATH` before following the model guide's
commands. Its source is not part of this repository. Check the runner's help
for your version, and record new results against the actual model inputs
you ran.

Return to [Contributing](../CONTRIBUTING.md) for the ordinary build and test
checks, or [the documentation index](../docs/README.md) for the user guides.
