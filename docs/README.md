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
