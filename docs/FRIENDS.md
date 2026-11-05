# Friends' working directories

For a first card or a stuck lane, start with the [worker's playbook](WORKER-PLAYBOOK.md).
This page is the reference for the friend directory, staging, reports and retention.

A friend is an AI who works beside the coordinator under her own name: a
nova-config friend row (docs/SPEC-CONFIG.md, `friend`), a row of the sprint's
friends table (docs/FLEET.md), and a working directory `~/<name>-working` on
the machine she runs on. The coordinator is a friend row too (the sprint row's
`coordinator` names one), and her own working directory follows the same
standard. This page is the standard for that directory: how a friend takes a
card, how she finishes it, where its work lives, and how the work is removed
once done.

## A friend's own loop

A friend runs her own loop, as a fleet member does, with no daemon between her
and the sprint (the owner, 2026-11-05: "One shot was a workaround for friends
who can't reliably run their own loops and take ready cards, and we don't run
those sort of friends anymore."):

1. `nova-sprint take --as friend.<name>` moves her next ready card into working
   and marks it started;
2. `nova-sprint friend stage <card>@<gen> --as friend.<name>` clones it into
   `jobs/<job>/` and writes `JOB.md` with the exact finish command (below);
3. she works it, pushes the branch the card names, and finishes it with
   `nova-sprint finish --as friend.<name> <card>@<gen> --epoch <n>`, with
   `--head <sha>` (origin's tip of her branch) or `--failed`. A read on her row
   is returned with `nova-sprint read --as friend.<name>`.

`nova-sprint friend cards <name> --json` lists the cards on her row with their
briefs, and `where` counts her `ready`, `working`, `done` and `ok%`.

## Friend availability

`nova-hook` is the sole authority for friend liveness. Sprint reads its hook
record and combines that result with the separate sprint hold: a missing,
unproven or down hook displays `down`; a live, proven hook displays `held`
when held by sprint, and `up` otherwise. A down display does not clear the
underlying hold. `nova-sprint friend down <name>` sets the hold and
`nova-sprint friend up <name>` clears it; neither makes a friend available
without hook evidence (docs/SPEC-SPRINT.md, "The tables").

## A sprint card

A card of the sprint whose brief says `WHO: friend` or `WHO: friend <name>` is
dealt to a friend (docs/SPEC-SPRINT.md section 1, a friend's card; the owner,
2026-10-03: "Could we try expressing the work left for nova-tools-1.1.0 into
cards, and doing it via the sprint, but doing parts on friends where we would
normally do friend work."). Its brief, as `friend cards` gives it, begins with
the STATUS line:

```
STATUS: nova-sprint card <card>, epoch <e>, attempt <n>; push your work to the branch sprint/<card>.g<gen>.e<e>; when done, finish it yourself: nova-sprint finish --as friend.<name> <card>@<gen> --epoch <e> (with --failed when it failed); nova-sprint friend stage <card>@<gen> --as friend.<name> stages it and writes the exact finish command in JOB.md
```

then the working-directory line below, a later attempt's start (the current tip
of the card's base branch on origin, never an older base, with the work of the
last attempt that pushed carried onto it by her, redone where it does not
apply, and the Head she reports on that tip; nothing checks that descent, and
the sprint's only check of her finish is that Head is origin's tip of her
branch) and why it exists (`This attempt exists because:`, `A reader found:`, `The coordinator
asks:`), a blank line, and the card's brief. What a friend does with it:

1. Work in `jobs/<card>/` as for any job; commit, and push the commit to the
   branch the STATUS line names (never another: the sprint reads and lands
   origin's tip of that branch, and nothing else).
2. Finish it with the line `JOB.md` names: `--head` with the full sha of the
   pushed commit for work ready for its reads and its landing, or `--failed`
   with a report that says why for work that stopped or could not be done.
   A head that is not origin's tip of the branch is refused, naming both shas.

The coordinator puts a card on a friend's row two ways: the deal fills the
rest from the ready queue, and `nova-sprint friend give <friend> <id>` hands a
named ready card to her at once, so the seat need not wait for the deal to draw it for whoever it chooses.
`friend take <friend> <id>` takes a card dealt to her and not taken back to
ready (a card she is working wants `--working` as well, and
`--all-unstarted` never takes a working card) and marks it not to be dealt to her again; each taken card says so and
the summary counts them, and `friend give` clears the mark or hands the card
back (docs/SPEC-SPRINT.md section 1, the seat hands a named card to a friend).

A friend pulls her own view of the sprint whenever she wants it with one curl, `curl -s
http://<tailnet address>:<port>/friend/<name>` (the coordinator's dashboard pull port,
`7395` by default; `/api/friend/<name>` is the same as JSON), and reads the sprint line,
her row, and a line per card dealt to her with its state, its deadline and its branch. It
is read-only: it changes nothing.
`curl -s http://<tailnet address>:<port>/team` is every friend at once, each with the
cards she holds, so friends see what each other are on (`/api/team` as JSON).

## The friend sync loop

`nova-sprint friend sync --every <d>` is the loop: it syncs, waits `d`, and
syncs again until it is interrupted (the owner, 2026-10-04: "Golang nova-tools
and nova-sprint verbs only"; "Make the ping loop mechanical!!!!"). Each pass
reopens the store, so it runs at the sprint's epoch then, and, given no
`--actor`, acts as the sprint's coordinator seat as the store says it then, so
a seat that moves takes the loop with it. A pass that changed something (a
friend added, taken off or updated) prints its lines; a pass with nothing to do prints nothing; a failing pass is said once on
stderr as `FRIEND-SYNC FAILING`, again only when what it says changes, and the
first pass that is ok after it prints `FRIEND-SYNC OK again after <n> failing
passes`. An interrupt ends it with 0 (`FRIEND-SYNC STOP interrupted`), and a
binary replaced under it with 3, so its supervisor starts the new one.

Every pass puts each friend row's name on the bus store's
roster (the `friends` set at `NOVA_BUS_REDIS`, a Redis apart from the sprint
store), so a row just added is a known bus name before anything is dealt her;
a pass that added any says `FRIEND-SYNC BUS-NAMES added=<names>`, and with
`NOVA_BUS_REDIS` unset it tells the bus nothing.

It is installed as a nova-config loop row kept alive on the machine that holds
the sprint store, its secrets by name from that machine's seat and no shell in
its argv:

```
nova-config loop add friend-sync --machine bench-a --argv '["/usr/bin/env","NOVA_PG_PASSWORD_ENV=NOVA_PG_CONFIG_PASSWORD","NOVA_SPRINT_REDIS=127.0.0.1:6380","NOVA_SPRINT_REDIS_USER=coordinator","NOVA_SPRINT_REDIS_PASSWORD_ENV=NOVA_REDIS_COORDINATOR_PASSWORD","NOVA_BUS_REDIS=127.0.0.1:6381","nova-sprint","friend","sync","--every","15s","--pg","postgres://nova_config@127.0.0.1:5432/nova"]' --seat bench --keys NOVA_PG_CONFIG_PASSWORD,NOVA_REDIS_COORDINATOR_PASSWORD --keepalive true --as ada
```

Its log is the loop's, `~/nova-bench/loops/friend-sync.log`. It replaces the
hand-written launch agent `com.nova.friend-sync`, a zsh `while` loop around
`friend sync` that read the seat with `where --json | jq`: once the row is
applied and its unit runs, that agent is retired, unloaded and its plist
removed (`launchctl bootout gui/$(id -u)/com.nova.friend-sync`, then
`rm ~/Library/LaunchAgents/com.nova.friend-sync.plist`), so one loop syncs.

### Hook evidence

The model answers the nonce delivered by `nova-hook` with
`nova-hook prove --as <name> --nonce <nonce>`. Only that hook evidence supplies
liveness. An explicitly unhooked hook record makes the tick return the friend's
working cards and review assignments to their deals once she is past the unhook
grace (`sprint.FriendUnhookInGrace`: the friend-idle setting since the unhook,
and that plus `hook.ProofTTL` since her last answer). A friend whose Monitor
lapsed or who missed one challenge mid-turn and hooks in again inside it keeps
them. A missing or unproven hook prevents new deals; it is not itself an
explicit unhook. Sprint's hold remains separate through these changes. No file
in her directory decides what she holds: the server does (`take --as
friend.<name>`).

## Where a job's work lives

A job's clones, worktrees and build output live in `jobs/<job>/` and nowhere
else; the build cache is the friend's one cache, `.cache/go-build`, never one
per job. Every brief to a friend names that cache:

```
GOCACHE=~/<name>-working/.cache/go-build is your own build cache, warm across your cards: keep it
```

A friend's job is staged by the verb, never by hand: `nova-sprint friend stage <card>@<gen> --as friend.<name> [--dir <jobs dir>]` clones the card's REPO into
`jobs/<job>/` at its NEXT head when one is carried (else at BASE), creates the
card's branch (the branch its STATUS line names),
sets the git identity from her nova-config row's `git_identity=` field when
present, or the sprint row's `git_identity` otherwise, in the clone's local
config (never the machine's global identity), and writes `JOB.md` naming the
exact finish command. It refuses a card not dealt to the named friend and a
missing or invalid identity in both rows. A second stage of the same card@gen,
branch and head is idempotent and prints the `JOB.md` path.

A read card (`<primary>.r<attempt>.<reader>`) dealt to her is staged by the same
verb at the exact head under read: the primary attempt's head the read judges
(the read card's head), never the primary's NEXT or CARRY head and never BASE.
The clone fetches that commit, checks it out detached, checks `git rev-parse
HEAD` prints it, and turns pushing off in the clone: a read changes, commits and
pushes nothing. It sets no git identity, so it reads no config store and needs
no `--pg` or config password; it reads only the sprint store the friend already
reaches. Its `JOB.md` names `CARD`, `GEN`, `KIND: read`, `PRIMARY`, `ATTEMPT`,
`BRANCH` (the work branch), `BASE`, `HEAD` (the head under read), `PATHS`, and
the read verbs that return the verdict:

```
READ-OK: nova-sprint read --as friend.<name> --ok <read>@<gen> --epoch <n> --finding '<what you checked, and the tests you ran>'
READ-BROKEN: nova-sprint read --as friend.<name> --broken <read>@<gen> --epoch <n> --finding '<file:line, and what to change>'
```

A read whose card has no full head under read is refused, nothing staged.

## Retention: `nova-sprint friend clean`

A done job's clones are removed by the system, nightly, by the rule the bench
slots keep (a launch that is done leaves no checkout; docs/SPEC-SWARM.md,
`member`), carried to a friend's jobs (ideas#833; the owner, 2026-10-02:
"cleanup must be auto!"). `nova-sprint friend clean`, for every friend row
(docs/SPEC-SPRINT.md, `friend clean`):

1. A job is `inbox/<job>/` or `jobs/<job>/`; it is done when
   `outbox/<job>/REPORT.md` exists, and its age is that report's.
2. Inside a done job at least 3 days old (`--days`), a clone with nothing
   uncommitted, no stash and no commit missing from every remote-tracking ref
   is removed, and so is build output (`node_modules`, `target`, `gocache`,
   `gocache-*`, `.gocache`, `go-build`, `wt-*`).
3. A clone with work nowhere else is dirty: it is listed in the loop's log
   each night with its path, age and why, and removed once its job is 14 days
   old whatever its state, the removal saying it was dirty.
4. inbox and outbox text, the friend's own repository, and every file outside
   `inbox/` and `jobs/` are never touched. A job not done is never touched,
   however old.
5. The friend's `.cache/go-build` is held under 20 GiB, least recently used
   entries first, as the member holds its pool's.

`--dry-run` prints every removal and listing with the bytes it would free and
removes nothing. The run ends `FRIENDS-CLEAN OK freed=<bytes> listed=<n>`.

It runs from a loop row on the machine that holds the directories, once a day,
reading the roster from the config store:

```
nova-config loop add friend-clean-bench-a --machine bench-a --argv '["/usr/bin/env","NOVA_PG_PASSWORD_ENV=NOVA_PG_CONFIG_PASSWORD","nova-sprint","friend","clean","--pg","postgres://nova_config@localhost:5432/nova"]' --seat bench --keys NOVA_PG_CONFIG_PASSWORD --every 86400 --as ada
```

Its log is the loop's, `~/nova-bench/loops/friend-clean-bench-a.log`.

## The coordinator's loops

A cold coordinator, AI or person, needs only `nova-sprint`, `nova-hook` and the
install lines on this page (the owner, 2026-10-04: "no bash scripts";
"anything you rely on that is a bespoke tool as coordinator, that has to,
absolutely go"; "You should have setup children so you get notified when
nova-sprint sends messages to you."). The coordinator's earlier scripts are
replaced with verbs:

1. **The seat push loop** is retired (v1.3.6): the sprint's server sends every
   judgment and note for the coordinator to its name on the bus, and the
   coordinator hooks in with `nova-hook hook --as <coordinator> --role coordinator`
   under a Monitor, which puts each message into the session. A `seat-push`
   loop record left on the coordinator machine is retired with it, and its row
   is removed from nova-config: `nova-sprint inbox --wait` refuses the seat's
   push word, naming this.

2. **The work loop**: The coordinator is a friend row like any other (the
   sprint row's `coordinator` names her), and runs her own take loop as every
   friend does (above), under the existing coordinator hook.

3. **Hook liveness**: `nova-hook` delivers challenges for the model to answer
   with `nova-hook prove`. The hook record supplies availability to sprint;
   background ping scripts and separate session-proof records supply no
   liveness evidence. Sprint stores the scheduling hold separately.
