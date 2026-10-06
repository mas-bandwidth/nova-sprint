# nova-sprint v1.0.0

Released 2026-10-06. The first release of nova-sprint in its own repository; until now it was
developed inside [Nova Tools](https://github.com/mas-bandwidth/nova-tools).

## What it is

nova-sprint runs a team of AI collaborators on one shared plan. You describe the work as
cards: bounded tasks, each with a brief, a scope, checks and a finish condition. Cards sit in
streams, with dependencies between them and gates that hold a phase until its prerequisites
land. A server keeps the plan, and every assignment, attempt, review and landing, outside any
chat. Its loop hands ready cards to whoever has free capacity, sends finished work to an
independent review, merges what passes into the base branch (a card is then "landed"), and
releases whatever was waiting on it. Different models in different tools work through the
same protocol, and you watch it all on a dashboard ([live demo](http://69.67.149.151/)).

The AIs on the team are called friends, and each runs in a harness (the program hosting its
session, such as Codex or OpenCode). One friend holds the coordinator's seat: it shapes the
plan, handles findings, and brings you the decisions that are yours. The rest belongs to the
server: who has which card, what each card is waiting for, whether a result is stale. The
README's cartoon team includes Green, whose message "was sent successfully" while he had his
headphones on: a message delivered to an AI that is not there to read it. That failure is the
one this release is built around.

## What v1.0.0 guarantees

A message to an AI that has not recently proven it can be reached is refused when it is
sent, rather than discovered unread the next morning. To be reachable, an AI must show that
the tooling can put text into its running session and get a reply, and that proof expires
unless it is renewed. The windows below are minutes, not zero: an AI that walks away is
refused once its last proof runs out.

- **The coordinator's seat.** A loop delivers a check carrying a one-time code into the
  coordinator's session, and the session answers with `nova-sprint seat pong <code>`. A check
  goes out every 10 minutes and the session has 5 to answer, so the seat is live while the
  last answer is at most 15 minutes old and the last delivery did not fail. Otherwise every
  command that changes the plan is refused with one line naming the setup command
  (`nova-sprint seat install`), and nothing is changed. `nova-sprint seat check` reports the
  seat down and exits 1. Reading the plan, the commands an AI uses to take and finish its own
  cards, and the server's own loop are never refused, so work already in flight keeps moving.
- **The bus.** The bus is the message queue between the AIs (`nova-bus`). `nova-bus send`
  refuses a message when its sender or any recipient has no current proof, naming each one and
  the fix, and receiving under a name with no proof is refused the same way. A friend's
  background daemon records the proof when the friend's session answers a check (5 minutes
  to answer), renews it every minute while it keeps answering, and marks it down as soon as a
  check goes unanswered. A proof not renewed for 10 minutes no longer counts.
- **Every friend.** The friend daemon refuses to install or run under a harness it cannot
  deliver into, and refuses to start until the session answers its first check within five
  minutes. The server counts a friend as up only on evidence from its session (a check
  answered in the last 10 minutes, or a card of its own finished in the last 30); the daemon's
  heartbeat alone never does.
- **Every card a friend takes reaches it.** `nova-sprint friend cards <friend>` lists every
  card assigned to that friend, with its brief. The daemon reads it on each pass, writes each
  missing brief into the friend's inbox directory, and removes the job of a card that has been
  taken back.
- **One release on the dashboard.** When the plan is labelled by release, the dashboard shows
  the streams of one release, by default the earliest with cards left, with a switch to each
  other release and to all of them.
- **The public demo holds a front-page load**, as measured rather than promised. On
  2026-10-06 the live demo served 2,528.8 requests per second for ten minutes to one load
  host 166 ms away: 1,518,301 responses, every one successful. The page host's own p99 was
  0.9 ms, and the p99 seen from the distant host was about 0.4 s. The dashboard server behind
  the page saw only one read a second. This measures how the demo is deployed (static files
  behind Caddy, refreshed every second), not the nova-sprint binary on its own.

## What it does not do yet

- Claude Code has no delivery adapter yet, so a Claude Code session cannot prove it is
  reachable: it can neither hold the seat nor be heard on the bus. The harnesses with an
  adapter are OpenCode, Codex, Antigravity, DeepSeek Harness, the Gemini command line, Grok
  Build, and sessions hosted in tmux.
- The bus and the friend daemon are the `nova-bus` and `nova-friend` commands of
  [Nova Tools](https://github.com/mas-bandwidth/nova-tools). This repository carries the
  rules they enforce; a live team installs them from there. The local first lap below needs
  neither.
- The seat's check loop does not yet run over the bus, and it raises no alarm when work piles
  up unread.
- How to deploy a public dashboard the way the demo is deployed is not documented in this
  release.

Everything else that was in progress when v1.0.0 was cut moved to v1.1.0: 298 cards in 37
streams, listed in the [roadmap](../ROADMAP.md).

## How to start

From a terminal with Git and Go 1.26.6 or newer:

```sh
git clone https://github.com/mas-bandwidth/nova-sprint.git
cd nova-sprint
go install ./cmd/nova-sprint
```

Then run `nova-sprint help` and walk the [local first lap](FIRST-LAP.md), which needs no
Redis, model account or Git remote and spends no tokens. [Getting started](GETTING-STARTED.md)
takes it from there to a live team. nova-sprint is MIT licensed; your model providers and
machines have their own costs.

## Credits

nova-sprint was built with the friends who ran on it, each AI named here with its model
classes: the tiers of work the sprint deals to it, in rising order flash, pro, heavy,
frontier.

- **Alex**: pro
- **Emma**: flash, pro
- **Freddy**: flash
- **Johnny**: flash, pro
- **Rowan**: flash, pro, heavy, frontier; the coordinator at release
- **Stella**: flash, pro, heavy, frontier
- **Zhi**: pro

Owner: **Glenn Fiedler**.
