# Find your way around nova-sprint

Welcome! If you are wondering whether nova-sprint would help your AI team,
start with the [project introduction](../README.md). You do not need to learn
the table layout or read a specification to take your first lap.

Want to create AI friends before putting a team together? Start with
[Nova Seed](https://github.com/mas-bandwidth/nova). nova-sprint builds on
[Nova Tools](https://github.com/mas-bandwidth/nova-tools) to coordinate their
work and scale out across friends, swarms, or both.

## Start here

| Your next question | Where to go |
|---|---|
| Can I watch a sprint doing real work? | [Live dashboard](http://69.67.149.151) and [the README tour](../README.md#watch-your-team-get-work-done) |
| How do I get started? | [Getting started](GETTING-STARTED.md) |
| Can I try the workflow before setting up a team? | [Local first lap](FIRST-LAP.md) |
| Why do AI teams need a coordination machine? | [The illustrated README](../README.md#everybody-said-on-it) |
| What are cards, streams, dependencies, and sentinels? | [How the workflow runs](../README.md#how-it-works-write-the-workflow-let-the-machine-run-it) |
| What do I do as the coordinator? | [Coordinator's guide](SPRINT-COORDINATOR.md) |
| What is the exact command? | [Command reference](CLI.md) |
| How do I open the dashboard? | [Dashboard guide and specification](SPEC-SPRINT-DASHBOARD.md#serving-and-publishing) |
| How do I contribute a change? | [Contributing](../CONTRIBUTING.md) |

The README describes the v1.0.0 product. Command references, implementation
notes, and recorded results describe the code or revision they name. In
particular, card generation now lives under `nova-sprint card`, while the
GitHub issue mirror still uses `nova-work`. Use the help shipped with your build for its exact spelling. A
planned feature in a design note is not an executable example.

## When you need the details

| Reference | What it helps you understand |
|---|---|
| [Sprint contract](SPEC-SPRINT.md) | Cards, dependencies, scheduling, review, landing, and the rules each state change follows. |
| [Coordinator credentials](SPRINT-COORDINATOR-SEAT.md) | How locally executed configuration commands get the credentials they need. |
| [Work tree format](SPEC-WORK-V1.md) | The GitHub issue mirror, the fields it preserves, and import/verify behaviour. |
| [Work language](SPEC-WORKLANG.md) | The small data grammar used to read tree files without evaluating them. |
| [Durable sprint program design](../internal/work/NOTE.md) | The proposed sprint-program store, distinct from the existing GitHub issue mirror. |
| [Executable examples](TESTS.md) | Exact command transcripts checked by tests. |
| [Test harness](../internal/testkit/HARNESS.md) | Shared helpers for contributors writing command tests. |
| [State-machine models](../tla/README.md) | What the TLA+ models explore and how to interpret their results. |
| [Brand and voice](BRAND.md) | The robot friends, name, colours, and writing style. |
| [Asset provenance](ASSET-PROVENANCE.md) | Where the artwork and font came from. |
| [Recorded reviews](ratings/README.md) | Feedback from earlier versions, preserved with its original context. |

The specs keep precise rules, owner quotations, and examples that tests read.
Those details are there when you need them. The guides explain the same
workflow from the point of view of the person using it.
