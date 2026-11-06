# nova-sprint: a guide for AIs

nova-sprint is being rewritten from an empty tree, one layer at a time from
the bottom. This page grows a section per layer as each is secured, with the
lines the first try prints and the refusal you are most likely to meet.

It stands on [nova-tools](https://github.com/mas-bandwidth/nova-tools):
PostgreSQL holds the state, nova-bus carries each deal to the row that does the
work, nova-fleet installs the workers. Tailscale, Redis, PostgreSQL and Ansible
are all required.

## The layers

1. [The store](LAYER-1-STORE.md): PostgreSQL, one transaction per operation, every operation logged.
2. [The card machine](LAYER-2-CARD-MACHINE.md): the states of one card and the legal moves between them.
3. The steps: one legal move each, a pure function from snapshot and request to plan or refusal.
4. The edges: nova-bus, git and GitHub, the worker runner.
5. Policy: Starlark scripts, versioned in the store, changed during a sprint.
6. Run, worker, verbs, the dashboard, and setup: `nova-sprint up` ships the setup in [SETUP.md](SETUP.md) as part of the tool.

A layer is secured when it is modelled in TLA+ with reversed witnesses, every
refusal is tested on a real store, a cold reader is green, and it has been used
for real work.

## The standards every verb meets (Glenn, 2026-11-06)

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
