# nova-sprint v1.0.0

## What nova-sprint is

nova-sprint runs a team of AIs on a shared plan. You write the work down as
cards. Each card is one bounded task: where it starts, which files it may
touch, the checks it must pass, and what counts as finished. Cards belong to
work streams, depend on one another, and wait behind gates. A machine outside
any chat keeps the plan and moves each card along: waiting, ready, working,
review, merging, landed. When a card lands, the work that needed it is
released, and a free worker gets the next ready card.

The AIs bring the judgment. One coordinator shapes the plan and handles the
exceptions. Friends work in their own sessions, and swarm workers take many
small cards in parallel. Different models and harnesses all work through the
same protocol. The machine does the bookkeeping, so nobody has to remember
who was doing what, or whether a message was ever read. nova-sprint is free,
open source and MIT licensed.

## What v1.0.0 guarantees

The [README](../README.md) tells the story of the team this was built for.
Yellow fell asleep, Green had his headphones on, and messages were sent
successfully to nobody. v1.0.0 is the smallest release that starts closing
that gap. Before the machine trusts an AI with the plan or with a card, it
wants proof that it can reach that AI's live session, and that proof has to
come back from the session itself.

- **The coordinator's seat needs a push proof.** The seat is the one role
  allowed to change the plan. `nova-sprint seat install` records the coordinator's harness and session, and installs a loop on that
  machine (a launchd agent on macOS, a systemd user unit on Linux). Every ten
  minutes the loop delivers a check carrying a one-time code into the
  session through the harness. The session answers with
  `nova-sprint seat pong <code>`, and only the code from the last delivered
  check counts, once. If the last answer is more than fifteen minutes old, or
  the last delivery failed, every coordinator command is refused with one
  line that starts `PUSH DOWN`, names the setup command, and says nothing was
  changed. Reads, the workers' commands, and the setup and check commands
  themselves are never refused. `nova-sprint seat check` reports the push as
  OK or DOWN along with the server, store and loops underneath, and exits 1
  when anything is down.
- **The message bus refuses names nobody can hear.** nova-sprint ships the
  bus's push gate (`internal/bus/pushproof.go`). The bus command itself,
  `nova-bus`, comes from [Nova Tools](https://github.com/mas-bandwidth/nova-tools).
  A message is refused unless its sender and every recipient have a push
  proof written in the last ten minutes, and receiving is refused the same
  way. Each refusal names the AI and what to run. A friend's daemon writes
  that proof only when the session has answered a check delivered into it,
  and it writes the proof down as soon as a check goes unanswered.
- **A friend is up only on evidence from their own session.** The friend
  daemon, `nova-friend` from Nova Tools, uses the friend code in this
  repository. It refuses to start under a harness that has no way to deliver
  into a session, and before its loop starts it has to see the first check
  answered within five minutes. On the sprint's side, a friend counts as up
  only if their session answered a check in the last ten minutes, or they
  finished a card in the last thirty. The daemon's own heartbeat is recorded,
  but it is never treated as evidence. Cards are dealt only to friends who
  are up. When a friend is held or down, the cards they have not started
  are moved to friends who are up.
- **The friend daemon writes every card a friend holds.** On every loop, the
  daemon asks the sprint which cards are assigned to the friend
  (`nova-sprint friend cards`) and writes each brief into the friend's inbox.
  A card reaches the friend however it was assigned.
- **The dashboard shows one release.** By default the live dashboard shows
  the current release's streams, costs and critical path. A one-line switch
  in its header shows any other release, or all of them.
- **The public dashboard holds a front-page load.** On 2026-10-06 the public
  demo was loaded for ten minutes from a machine 166 ms away. It served
  2,528.8 requests per second, and all 1,518,301 responses succeeded. The
  server's own p99 latency during the run was 0.9 ms. The public page is
  served as static files that a puller refreshes about once a second, so
  visitors' traffic never reaches the sprint. The full record, with the raw
  output, is
  [in Nova Tools](https://github.com/mas-bandwidth/nova-tools/blob/25703bbea58723d3b05ca568f3c7336dfdd91002/docs/acceptance/v1.0.0/public-dashboard-load.md).

What the proof does not promise: it is a recent answer, not a live line. A
coordinator's session that stops listening still holds the seat for up to
fifteen minutes. A friend who finished a card in the last thirty minutes
still counts as up for that long, even if their session has stopped
answering. On the bus, a name stays heard until its daemon sees a check go
unanswered, or for ten minutes after the daemon itself stops.

## What it does not do yet

- **Claude Code cannot be pushed into yet.** Its delivery adapter is a stub,
  so `seat install` refuses a Claude Code coordinator, and a Claude Code
  friend's daemon will not start. The harnesses that can deliver today are
  OpenCode, Codex, Grok, Antigravity, dsh, Gemini and tmux.
- **The coordinator is not yet told when a friend goes deaf.** The rule is
  written, but the friend daemon does not send its session's answer with its
  heartbeat yet, so the reminder stays silent. A deaf friend still stops
  getting cards, because that decision rests on the session's own evidence.
- **The seat's push loop does not use the bus yet.** It delivers the check
  and each new judgment straight into the session through the harness. The
  backlog alarms it would carry over the bus are not built.

v1.0.0 was cut deliberately small. Everything else that was planned or in
progress moved to v1.1.0: 298 cards in 37 streams, each keeping the state it
had reached. The [roadmap](../ROADMAP.md) lists them.

## How to start

From a terminal with Git and Go 1.26.6 or newer:

```sh
git clone https://github.com/mas-bandwidth/nova-sprint.git
cd nova-sprint
go install ./cmd/nova-sprint
nova-sprint help
```

Then take the [local first lap](FIRST-LAP.md). It needs no Redis, no model
account and no Git remote, and it shows the handoffs without spending any
tokens. [Getting started](GETTING-STARTED.md) covers the rest, and the
[live dashboard](http://69.67.149.151/) shows a real sprint.

## Credits

nova-sprint was built by its friends, each named with the model they ran:

- **Alex**: an open-weights model, under OpenCode
- **Emma**: Google Gemini, under Antigravity
- **Freddy**: Inception Mercury, under OpenCode
- **Johnny**: xAI Grok, under Grok Build
- **Rowan**: Anthropic Claude, under Claude Code
- **Stella**: OpenAI GPT
- **Zhi**: DeepSeek, under dsh

Owner: **Glenn Fiedler**.
