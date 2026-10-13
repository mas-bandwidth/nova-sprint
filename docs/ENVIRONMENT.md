# Environment for nova-sprint

Use these variables to choose where a command connects and which actor it
represents. A local twin uses a file; a live team normally sends client
commands through the running sprint server.

| Variable | Purpose | Example | Used by |
|---|---|---|---|
| `NOVA_SPRINT_REDIS` | Preferred environment address of the sprint store. `mem:<file>` names a local twin file. The `--redis` flag takes precedence. | `mem:sprint.twin` | Commands that access a store directly, including `run`; twin exercises |
| `NOVA_SPRINT_SERVER` | Address of a running sprint server. When set, served commands are sent to that server. | `127.0.0.1:<port>` | Coordinator, workers, and readers using the server |
| `NOVA_BUS_REDIS` | Address of the bus store used by the coordinator hook. | `<bus-host>:<port>` | Sprint server and coordinator hook session |
| `NOVA_SPRINT_ACTOR` | Default actor name for commands that need one. Explicit `--as` names a worker or reader. | `boss` | Commands that act for a coordinator, worker, or reader |

For a local twin, `--redis mem:sprint.twin` makes a command use that file-backed
simulation even if `NOVA_SPRINT_SERVER` or a server in the seat record is
configured. The twin has no background server, so advance it with
`nova-sprint tick --redis mem:sprint.twin` after each handoff. A reader becomes
available after its row is declared and it beats with `queue --as <reader>`;
the next tick can then ask it to review. See the
[first-lap walkthrough](FIRST-LAP.md) for a local card flow.

For a live sprint, the coordinator starts one server beside the Redis store.
The server process uses `NOVA_SPRINT_REDIS` and `NOVA_BUS_REDIS`; the
coordinator's interactive shell sets `NOVA_SPRINT_SERVER` to the server's
local address, `NOVA_BUS_REDIS` to the same bus store, and `NOVA_SPRINT_ACTOR`
to the coordinator named by the sprint. The hook session needs both server
and bus addresses. Worker and reader processes use the server address and
identify themselves with `--as`. Do not start another server or run direct
store-writing commands alongside the live server. See the
[coordinator's guide](SPRINT-COORDINATOR.md) for the hook and live setup
sequence.

For direct store commands, `--redis` takes precedence over
`NOVA_SPRINT_REDIS`, which takes precedence over the older `NOVA_REDIS_ADDR`
variable and then the store address recorded by `nova-sprint seat login`.

`NOVA_SPRINT_SERVER` selects the server for client commands unless the command
names its own store with `--redis`; that flag bypasses forwarding. When the
environment variable is unset, a server recorded by `seat install` can still
be selected from the local seat record. Commands such as `run`, `tick`,
`land`, and `play` operate where they are run and use a direct store address.
Follow each verb's `nova-sprint help <verb>` text for its exact connection
behavior.
