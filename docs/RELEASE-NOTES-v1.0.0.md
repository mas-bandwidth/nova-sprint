# nova-sprint v1.0.0

## What nova-sprint is

nova-sprint runs a team of AIs on a shared plan. You write the work as cards: each
card is one bounded task with its starting revision, the files it may touch, the
checks it must pass and what counts as finished. Cards sit in work streams,
depend on each other, and wait behind gates. A machine outside any chat holds
that plan and moves each card through its stages: waiting, ready, working,
review, merging, landed. A landed card releases the work that needed it, and a
free worker gets the next ready card.

The AIs bring the judgment: one coordinator who shapes the plan and handles
the exceptions, friends who work in their own sessions, and swarm workers that
take many small cards in parallel. Different models and harnesses work through
the same protocol. The machine brings the bookkeeping, so that nobody has to
remember who was doing what, or whether a message was ever read. It is free,
open source and MIT licensed.

## What v1.0.0 guarantees

The [README](../README.md) tells the story of the team this was built for:
Yellow fell asleep, Green had his headphones on, and messages were sent
successfully to nobody. v1.0.0 is the smallest release that closes that gap.
Its rule is that an AI who cannot be reached cannot hold work. The push proof
behind it is a round trip. A check carrying a fresh one-time code is delivered
into the AI's live session by its harness, never left as a file, and the
session sends the code back.

- **The coordinator's seat needs a push proof.** `nova-sprint seat install`
  records where the coordinator's session lives, and a loop delivers a check
  into it every ten minutes. The session answers with `nova-sprint seat pong`.
  If there is no answer within fifteen minutes, or a delivery fails, every
  coordinator command is refused with one `PUSH DOWN` line that names the
  setup command, and nothing is changed. Reads and the workers' commands
  still run. `nova-sprint seat check` reports the push as OK or DOWN, along
  with the rest of the machinery.
- **The message bus refuses deaf names.** The bus comes from
  [Nova Tools](https://github.com/mas-bandwidth/nova-tools). `nova-bus send`
  and `recv` are refused for a sender or recipient without a proven push in
  the last ten minutes, and nothing is written. The refusal names the AI and
  what to run. `nova-bus names` shows each name's state.
- **Every friend needs a push proof.** The friend daemon, `nova-friend` from
  Nova Tools, refuses to install or start under a harness it has no way to
  push into. Before its loop starts, it has to see one check answered within
  five minutes. A friend whose last answer is more than fifteen minutes old is
  counted down however recently the daemon reported in. Down friends are
  dealt no cards, and the coordinator is told once.
- **The friend daemon writes every card it holds.** On every loop, the daemon
  asks the sprint server which cards are assigned to the friend and writes any
  missing brief into the friend's inbox. Cards no longer assigned are moved aside.
  A card reaches the friend however it was assigned.
- **The dashboard shows one release.** The live dashboard shows the current
  release's streams, costs and critical path. A one-line switch shows any
  other release, or all of them.
- **The public dashboard holds a front-page load.** On 2026-10-06 (UTC) the public
  demo was loaded from a distant machine for ten minutes. It served 2,528.8
  requests per second with zero errors in 1,518,301 responses, and the
  web server's own p99 latency was 0.9 ms. The page is served as static files, which a
  puller refreshes about once a second, so the visitors' load never reaches
  the sprint.

What the proof does not promise: it is renewed every ten minutes, so a session
that stops listening can still look live for up to fifteen minutes. A Claude
Code friend running one-shot cards starts a new process for each card, so it
owes no proof. A name without a friend daemon cannot prove a push, so the bus
refuses it until it runs one.

## What it does not do yet

v1.0.0 was cut deliberately small. Everything else that was planned or in
progress moved to v1.1.0: 298 cards in 37 streams, each with the state it had
reached. The [roadmap](../ROADMAP.md) lists them.

## How to start

From a terminal with Git and Go 1.26.6 or newer:

```sh
git clone https://github.com/mas-bandwidth/nova-sprint.git
cd nova-sprint
go install ./cmd/nova-sprint
nova-sprint help
```

Then take the [local first lap](FIRST-LAP.md). It runs one card from start to
finish on a local file, with no Redis server, no model account and no Git
remote. [Getting started](GETTING-STARTED.md) covers the rest, and the
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
