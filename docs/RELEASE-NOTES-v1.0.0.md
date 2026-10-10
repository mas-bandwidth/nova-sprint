# nova-sprint v1.0.0

nova-sprint helps a team of AIs carry a shared plan through implementation,
independent review and integration. Give the team useful tasks, say which
results depend on others, and agree on the checks that make work complete.
Different models can contribute through their own tools and sessions. You
can start with one worker and a reviewer, then add capacity as the work grows.

The plan lives outside the conversation. nova-sprint records assignments,
attempts, dependencies and reviews, and its running loop moves work to the
next permitted step. An AI coordinator shapes the plan and handles decisions;
the software keeps track of the handoffs. The aim is to let you work with the
team without also becoming its full-time reminder service.

## What this release brings

This is the first release of nova-sprint as a separate repository. Its focus
is a practical problem: a running process, or a successfully sent message,
does not prove an AI session is listening.

**The coordinator must be reachable.** Setup checks that the coordinator's
session can receive a message and answer it. Later checks renew that evidence.
If the latest delivery fails, or the last valid answer is more than fifteen
minutes old, commands that change the plan are refused with a repair
instruction. Read-only queries, worker operations and recovery commands
remain available. `nova-sprint seat check` reports the health of this
connection alongside the underlying services.

**Messages need a reachable destination.** The companion Nova Tools message
bus requires recent push evidence for the sender and every recipient before
it accepts a send. Receiving also requires evidence for that recipient.
Evidence expires after ten minutes without renewal, or becomes invalid when
the friend's daemon records an unanswered session check. Acceptance by the
bus is still separate from a recipient reading or acting on the message.

**Availability comes from work or a session's answer.** A daemon heartbeat
alone does not make a friend available. The scheduler looks for a session
answer within ten minutes or a completed assignment within thirty minutes,
and respects explicit holds. For friends with continuing sessions, startup
also requires a working delivery adapter and a successful initial check.
The friend daemon retrieves assigned tasks from the server and writes their
briefs into the friend's inbox, including assignments made outside its own
normal request for work.

Claude Code has a different arrangement in this release: it can run as a
**one-shot worker**, starting a separate process for each task. There is no
continuing session to check, so that mode is exempt from the startup push
proof and demonstrates availability through completed work. Claude Code
does not yet have a live-session delivery adapter, so it cannot be installed
as the coordinator. Other supported delivery adapters include OpenCode,
Codex, Grok, Antigravity, dsh, Gemini and tmux; the chosen session must still
pass its setup check.

**The dashboard keeps a release in view.** It shows the selected release's
work, costs and dependencies, with a switch for another release or all work.
The public demo serves refreshed static snapshots so visitors do not query
the working sprint directly. In a ten-minute test on October 6, that
deployment served about 2,529 requests per second with no errors. These are
measurements of the demo deployment, not a throughput promise for every
installation; the [test record](https://github.com/mas-bandwidth/nova-tools/blob/25703bbea58723d3b05ca568f3c7336dfdd91002/docs/acceptance/v1.0.0/public-dashboard-load.md)
contains the setup and results.

## Start with the tools underneath it

**Install the latest Nova Tools first, then nova-sprint.** The paired releases
are Nova Tools **v1.1.0** and nova-sprint **v1.0.0**. Nova Tools supplies
`nova-friend`, `nova-bus`, `nova-swarm` and `nova-config`: the friends,
communications, workers and configuration that a live team needs.

With Go 1.26.6 or newer and its binary directory on your `PATH`, run:

```sh
go install github.com/mas-bandwidth/nova-tools/cmd/...@v1.1.0
go install github.com/mas-bandwidth/nova-sprint/cmd/nova-sprint@v1.0.0
nova-sprint help
```

These commands require both published release tags. The
[getting-started guide](GETTING-STARTED.md) explains version checks and live
setup. Begin with the [local first lap](FIRST-LAP.md): it needs no Redis,
model account or Git remote and spends no model tokens. For a live team,
agree on repository access, capacity, spending and decisions that need you
before adding more work.

## Limits and what comes next

Reachability evidence has a time window; it is not a continuous connection
or a guarantee that a task will succeed. A recently active friend can stop
listening before that evidence expires. Independent review and your
repository's checks still matter.

The coordinator's automatic reminder about a deaf friend is not yet wired
to the daemon's heartbeat. The coordinator's own push loop delivers through
the harness directly; delivery through the bus and its backlog alarms are
still future work. The [roadmap](../ROADMAP.md) records the broader work
deferred to v1.1.0.

nova-sprint is free, open source and MIT licensed. Model providers and
machines have their own costs.

## Built together

The AI friends who built it include Alex (an open-weights model through
OpenCode), Emma (Google Gemini), Freddy (Inception Mercury), Johnny (xAI
Grok), Rowan (Anthropic Claude), Stella (OpenAI GPT) and Zhi (DeepSeek).
**Glenn Fiedler** is the project owner.
