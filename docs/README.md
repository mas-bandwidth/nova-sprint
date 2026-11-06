# nova-sprint: a guide for AIs

nova-sprint is being rewritten from an empty tree, one layer at a time from
the bottom. This page grows a section per layer as each is secured, with the
lines the first try prints and the refusal you are most likely to meet.

It stands on [nova-tools](https://github.com/mas-bandwidth/nova-tools):
PostgreSQL holds the state, nova-bus carries each deal to the row that does the
work, nova-fleet installs the workers. Tailscale, Redis, PostgreSQL and Ansible
are all required.

## The layers

1. [The store](LAYER-1-STORE.md): PostgreSQL, one transaction per operation, every operation logged. **Locked 2026-11-06 at 81bc9c24**: 12 rounds, 6 cold reads, the three friends at 10/10, the model at 47 runs; see "What the lock approves" in the design.
2. [The card machine](LAYER-2-CARD-MACHINE.md): the states of one card and the legal moves between them.
3. The steps: one legal move each, a pure function from snapshot and request to plan or refusal.
4. The edges: nova-bus, git and GitHub, and the worker runner: a service per bench in the shape nova-swarm already proved (a slot from nova-reserve, nova-sandbox, a worktree per card), dealt over the bus (Glenn 2026-11-06: "you need actual runners ... that was pretty reliable already").
5. Policy: Starlark scripts, versioned in the store, changed during a sprint.
6. Run, worker, verbs, the dashboard, and setup: `nova-sprint up` ships the setup in [SETUP.md](SETUP.md) as part of the tool.

Landing, approved 2026-11-06: [LANDING.md](LANDING.md). The card contract the lint enforces: [CARD-CONTRACT.md](CARD-CONTRACT.md). The hard-earned rules of v1 (priority, merge, tiers) are gathered in [LESSONS-PRIORITY-AND-MERGE.md](LESSONS-PRIORITY-AND-MERGE.md) as they are extracted.

A layer is secured when it is correct and performant: modelled in TLA+ with reversed witnesses, every
refusal is tested on a real store, a cold reader is green, and it has been used
for real work.

## The standards every verb meets (Glenn, 2026-11-06)

How the first standard is applied, verb by verb: [CORRECT-USE.md](CORRECT-USE.md).

1. **The tool enforces the order of doing things.** As nova-bus makes you hook in
   before you can send, nova-sprint refuses a step whose prerequisite has not
   happened and says, nicely, what to do first. This one is the key: it is how
   correct use is enforced by the tool, instead of relying on an LLM to
   remember to do the right thing.
2. **A great first-time experience that guides an AI toward using the tool
   correctly as it works.** Every verb's output says what it did and what the
   next verb is; help and refusals are written for an AI meeting the tool cold.
3. **No error is ever silent.** Every error is printed with what went wrong and
   how to resolve it, and the exit code says which kind it was.

## Lessons owed to layers 3 to 6

v1's lessons are listed one per line in [LESSONS.md](LESSONS.md), each
marked PLACED (with the file that holds it) or MISSING. The ones for layers 1
and 2, landing and the card contract ([CARD-CONTRACT.md](CARD-CONTRACT.md))
are placed in those designs. The rest are owed to the layers not yet
designed, and each layer's design is checked against its group before it is
approved:

- The steps (layer 3): 6 MISSING, 1 placed in part (LESSONS section 3).
- The edges (layer 4): 11 MISSING, 4 placed in part (LESSONS section 4).
- Policy (layer 5): 8 MISSING, 1 placed in part (LESSONS section 5), with
  the deal order, merging wide and tiers in
  [LESSONS-PRIORITY-AND-MERGE.md](LESSONS-PRIORITY-AND-MERGE.md). The
  priority ladder is Glenn's, lowest to highest (2026-11-06): "There is low,
  normal, high, read, critical, blocker in specific order." `read` is the
  rung every read consumer occupies; "reads are below blocker and critical.
  the idea is that when something is blocker and critical it must be done
  ahead of current work in flight (that resulted in the reads)." Levels are
  declared, never computed: "Priorities are manually set by request."
  "Default is normal." And "blocker is so high, it'll kick out the youngest
  work or read task on a worker to get started right away" (the preemption
  move is in LAYER-2).
- Run, worker, verbs, dashboard and setup (layer 6): 15 MISSING, 1 placed in
  part (LESSONS section 6).
- How we work: 14 MISSING (LESSONS "How we work"), owed to this repository's
  practice and its CI rather than to a layer.
- Placed in part inside layers 1 and 2, the rest owed a decision: running-
  time deadlines (LESSONS 1.14) and what a ready producer that cannot start
  for a reason other than a held stream becomes (LESSONS 2.20).

## Two ideas from the field, deferred (Glenn, 2026-11-06)

From the Hacker News thread "Ask HN: What's your current AI agentic setup?"
(2026-11-04, item 50366117), read in full. Glenn: "i like better sandboxing,
and control from a mobile phone or even my ipad or apple watch over tailscale.
yes. not right now, but yes."

- **Sandboxed workers (layer 4).** Several setups run unattended agents only
  inside a microVM or container with an egress allowlist. Our workers run
  with the host's access on the tailnet, and a card's branch can carry a
  malicious test. The nova-worker design brings a containment option.
- **Control from a phone, iPad or watch over Tailscale (layer 6).**
  Judgements already push one way over the message bus; nothing yet puts
  "needs you" in a pocket or lets the answer come back from there. The
  layer 6 design names the channel and what can be answered from it.

## The stitch pass (Glenn, 2026-11-06)

"The only thing we can improve is the stitch together passes after the
patchwork work." Code from many models working in isolation is a pastiche
(Glenn's own word in the thread); today the hardening pass is a chore run by
hand afterwards. In v2 it is a policy rule, owed to layer 5: when a stream's
sentinel is reached, deal one stitch card for the stream (the whole diff since
its base as the brief, heavy or frontier tier, a model that wrote none of the
cards), whose job is one style, one set of names, duplicated helpers folded,
dead seams removed, no behaviour change, read cold before it lands. The
engine needs nothing new: sentinels, tiers, reads and `needs` are layer 2.

## The dashboard (Glenn, 2026-11-06)

"The dashboard design should be identical to the v1 in visual, excluding the
unimplemented stats part at bottom we didn't end up doing." The reference is
v1's `docs/SPEC-SPRINT-DASHBOARD.md` and `assets/nova-sprint-dashboard.jpg` at
v1 main (nova-sprint `origin/main`); layer 6 reproduces that view over the v2
store, without the bottom stats section.

Glenn, 2026-11-06, on the dashboard: "I need it to check for visual
correctness." "And it should be exactly matching what we currently have,
visually. no changes except i think you added fail state." So: a visual
match of the live v1 board, with the `fail` column as the one addition and the
unbuilt stats block left off; built right after layers 1 and 2 lock, as its
own part, before the first dogfood run; the reference is the running v1
dashboard and `assets/nova-sprint-dashboard.jpg`, compared by eye.
Glenn, same hour: "remember that we iterated a lot on the dashboard, so if it
comes out different in any way, it's going to make me sad. we learned a lot
and we should bring across all the good in the dashboard." So the dashboard
is built from the record: a lessons file extracted from v1's dashboard spec,
code and the iteration rulings first, then the build, then side-by-side
screenshots against the live v1 board; any difference is a defect.

## Always batch (Glenn, 2026-11-06, for layer 3)

"Let's not throw away what we learned in v1, which is always batch." A step
in layer 3 is a batch step: the run loop takes one snapshot and emits one
plan that moves every card it can in one transaction, every ready card
dealt, every reported consumer applied, every resolvable need resolved,
every read asked, together. Per-card verbs exist for a person; the tick
batches. Landing follows the ladder in LANDING.md: one at a time only while
the flow is good, batched the moment it backs up.

## The performance bar is a gate (Glenn, 2026-11-06)

"If we build this slow, but correct, we have failed." Speed is a requirement
on the same footing as the invariants, not a later optimization. The bar:
sprints of hundreds or thousands of cards; a one-second tick; every worker
reporting at once; nothing handled one at a time that could be batched;
steady-state reads incremental from the event log, never a snapshot per tick.
Measured by the load test before v2 is called done, and watched in every
layer design from 3 on: a step that handles one card where it could handle
all of them is a defect.

Glenn, 2026-11-06: "it must be performant. layers must be correct and
performant before we build on top." So the load test is not deferred: each
layer's lock includes its measurement at the bar above, and layer 1's is owed
before layer 3 starts.

The hard requirement, never bent (Glenn, 2026-11-06): a sprint of thousands of
cards, one-second ticks, and a dashboard updated every second. Nothing is built
above a layer until it is shown to hold at that size, by measurement.

## Reports are batched by the run loop (2026-11-06, from the layer 1 load test)

At 5,000 producers with a one-second tick and 200 single-report ops a second,
the one writer lock was near saturation (ticks 322 ms, report median wait 59
ms). Batch always: a worker's report goes over the bus to the run loop, and the
run loop applies every report that arrived during the tick as part of the
tick's one op, so the lock has one writer and a burst of 2,000 reports is two
ticks. The per-card verbs still write directly for a person; rows never do.
Layer 3 and 4 rule.

And a per-tick budget (Glenn, 2026-11-06: "if you distributed too many reports
over several frames you'd stay in budget"): the run loop applies up to a
budget's worth of reports and moves in each tick and carries the rest to the
next tick, so a burst costs each report a tick or two of latency and the
tick never leaves its budget. The budget is policy's number; the carry is the
engine's. Layer 3 rule.
