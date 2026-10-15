# Friends' working directories

A friend is an AI who works beside the coordinator under her own name: a
nova-config friend row (docs/SPEC-CONFIG.md, `friend`), a row of the sprint's
friends table (docs/FLEET.md), and a working directory `~/<name>-working` on
the machine she runs on. The coordinator is a friend row too (the sprint row's
`coordinator` names one), and her own working directory follows the same
standard. This page is the standard for that directory: how a job arrives, how
it is reported, where its work lives, and how the work is removed once done.

## The inbox/outbox standard

Only the coordinator reaches out. A job is a directory:

- the coordinator delivers `inbox/<job>/`, with its `BRIEF.md`; `<job>` begins
  with its date, `2026-10-02-cold-rating`;
- the friend makes `outbox/<job>/` when she starts;
- the friend writes `outbox/<job>/REPORT.md` when she is done, with a
  `Verdict:` line (its first word HOLD, FAIL, FAILED or BROKEN is a failed job;
  any other word, or no such line, is ok).

A job is ready while `outbox/<job>/` is absent, working while it is there
without `REPORT.md`, and done once `REPORT.md` exists. `nova-sprint friend
sync` reads these directories (writing in them only a sprint card's brief, below)
into the friends table's `ready`, `working`, `done` and `ok%`.

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
normally do friend work."). It arrives as a job like any other:
`inbox/<card>/BRIEF.md` (after a clear, `<card>~<epoch>`), written by
`nova-sprint friend sync`. Its first line is the STATUS line:

```
STATUS: nova-sprint card <card>, epoch <e>, attempt <n>; push your work to the branch sprint/<card>.g<gen>.e<e>; when done, write outbox/<card>/REPORT.md with Verdict: LAND|HOLD|FAIL and Head: <sha>
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
2. Write `outbox/<card>/REPORT.md` in this form, exactly:

   ```
   Verdict: LAND
   Head: <the full 40-character sha of the commit pushed>

   <one paragraph: what changed and the gate's result>
   ```

   `LAND` is work ready for its reads and its landing; `HOLD` is work stopped
   for the coordinator's decision, `FAIL` work that could not be done; for
   either, `Head:` may be left out, and the first paragraph says why. The
   first `Verdict:` and `Head:` lines are read (markdown marks around them
   are fine); the first paragraph that is neither of them nor a heading goes
   onto the card, cut to 1 KiB.

The sync finishes the card on its next run after the report (the period of the
loop that runs it: 15 s in the friend sync loop, below): `LAND` goes to review at
origin's tip of the branch when that tip is the full sha `Head:` names; a Head
that is not the tip (a commit not pushed there, or pushed to after the report)
or a branch origin does not hold finishes nothing, and the sync says so naming
both shas each run until the report's Head is the tip (or the card's deadline
passes); a `LAND` with no full sha, or any other word, comes back failed saying
what it lacks; `HOLD` and `FAIL` come back failed to the coordinator with the
paragraph. A sprint card is not counted among the
inbox's jobs: the friends table counts it from the sprint.

The coordinator puts a card on a friend's row two ways: the deal fills the
rest from the ready queue, and `nova-sprint friend give <friend> <id>` hands a
named ready card to her at once (the same work card `friend sync` delivers),
so the seat need not wait for the deal to draw it for whoever it chooses.
`friend take <friend> <id>` takes a card dealt to her and not taken back to
ready (a card she is working wants `--working` as well, and
`--all-unstarted` never takes a working card) and marks it not to be dealt to her again; each taken card says so and
the summary counts them, and `friend give` clears the mark or hands the card
back (docs/SPEC-SPRINT.md section 1, the seat hands a named card to a friend).

A friend pulls her own view of the sprint whenever she wants it with one curl, `curl -s
http://<tailnet address>:<port>/friend/<name>` (the coordinator's dashboard pull port,
`7395` by default; `/api/friend/<name>` is the same as JSON), and reads the sprint line,
her row, and a line per card dealt to her with its state, its deadline and its branch. It
is read-only: it changes nothing, and her inbox and outbox stay how work arrives and is
reported.
`curl -s http://<tailnet address>:<port>/team` is every friend at once, each with the
cards she holds, so friends see what each other are on (`/api/team` as JSON).

## The friend sync loop

`nova-sprint friend sync --every <d>` is the loop: it syncs, waits `d`, and
syncs again until it is interrupted (the owner, 2026-10-04: "Golang nova-tools
and nova-sprint verbs only"; "Make the ping loop mechanical!!!!"). Each pass
reopens the store, so it runs at the sprint's epoch then, and, given no
`--actor`, acts as the sprint's coordinator seat as the store says it then, so
a seat that moves takes the loop with it. A pass that changed something (a
friend added, taken off or updated, a card delivered or finished) prints its
lines; a pass with nothing to do prints nothing; a failing pass is said once on
stderr as `FRIEND-SYNC FAILING`, again only when what it says changes, and the
first pass that is ok after it prints `FRIEND-SYNC OK again after <n> failing
passes`. An interrupt ends it with 0 (`FRIEND-SYNC STOP interrupted`), and a
binary replaced under it with 3, so its supervisor starts the new one.

Every pass, cards or none, puts each friend row's name on the bus store's
roster (the `friends` set at `NOVA_BUS_REDIS`, a Redis apart from the sprint
store), so a row just added is a known bus name before anything is dealt her;
a pass that added any says `FRIEND-SYNC BUS-NAMES added=<names>`, and with
`NOVA_BUS_REDIS` unset it tells the bus nothing.

It is installed as a nova-config loop row kept alive on the machine that holds
the friends' working directories, its secrets by name from that machine's seat
and no shell in its argv; `--root` is left out, so the friends' directories are
under the unit's HOME:

```
nova-config loop add friend-sync --machine bench-a --argv '["/usr/bin/env","NOVA_PG_PASSWORD_ENV=NOVA_PG_CONFIG_PASSWORD","NOVA_SPRINT_REDIS=127.0.0.1:6380","NOVA_SPRINT_REDIS_USER=coordinator","NOVA_SPRINT_REDIS_PASSWORD_ENV=NOVA_REDIS_COORDINATOR_PASSWORD","NOVA_BUS_REDIS=127.0.0.1:6381","nova-sprint","friend","sync","--every","15s","--pg","postgres://nova_config@127.0.0.1:5432/nova"]' --seat bench --keys NOVA_PG_CONFIG_PASSWORD,NOVA_REDIS_COORDINATOR_PASSWORD --keepalive true --as ada
```

Its log is the loop's, `~/nova-bench/loops/friend-sync.log`. It replaces the
hand-written launch agent `com.nova.friend-sync`, a zsh `while` loop around
`friend sync` that read the seat with `where --json | jq`: once the row is
applied and its unit runs, that agent is retired, unloaded and its plist
removed (`launchctl bootout gui/$(id -u)/com.nova.friend-sync`, then
`rm ~/Library/LaunchAgents/com.nova.friend-sync.plist`), so one loop syncs.

### Work reporting and hook evidence

`nova-sprint friend beat <friend>` carries the daemon's work report: running
cards, queued and working counts, width, load and other work observations.
Those fields support scheduling and reconciliation; receiving a report or
losing reports does not establish friend availability. The model supplies
hook proof inside its own turn.

The model answers the nonce delivered by `nova-hook` with
`nova-hook prove --as <name> --nonce <nonce>`. Only that hook evidence supplies
liveness. An explicitly unhooked hook record makes the tick return the friend's
working cards and review assignments to their deals (`sprint.LiveReturns`),
even without a new work report. A missing or unproven hook prevents new deals;
it is not itself an explicit unhook. Sprint's hold remains separate through
these changes. No file in her directory decides what she holds: the friend
reconcile against her queue file is retired, and her lanes ask the server
(`take --as friend.<name> --lane <n>`; nova-tools `tla/FriendLaneClient.tla`).

## Where a job's work lives

inbox/ and outbox/ hold text: the brief, the report, the evidence. A job's
clones, worktrees and build output live in `jobs/<job>/`, the same `<job>` as
its inbox directory, and nowhere else; the build cache is the friend's one
cache, `.cache/go-build`, never one per job. Every brief to a friend (and every
brief to a coordinator's child) carries this line, with the name and the job
filled in:

```
Work in ~/<name>-working/jobs/<job>/: every clone, worktree and build output goes inside it, GOCACHE=~/<name>-working/.cache/go-build, and the report goes to ~/<name>-working/outbox/<job>/REPORT.md.
```

A clone left inside `inbox/<job>/`, beside its brief (the layout before this
line), is found there too.

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

A cold coordinator, AI or person, needs only `nova-sprint` and `nova-friend`
and the install lines on this page (the owner, 2026-10-04: "no bash scripts";
"anything you rely on that is a bespoke tool as coordinator, that has to,
absolutely go"; "You should have setup children so you get notified when
nova-sprint sends messages to you."). The coordinator's three earlier scripts
are replaced with verbs:

1. **The seat push loop** is retired (v1.3.6): the sprint's server sends every
   judgment and note for the coordinator to its name on the bus, and the
   coordinator hooks in with `nova-hook hook --as <coordinator> --role coordinator`
   under a Monitor, which puts each message into the session. A `seat-push`
   loop record left on the coordinator machine is retired with it, and its row
   is removed from nova-config: `nova-sprint inbox --wait` refuses the seat's
   push word, naming this.

2. **The receive thread**: The coordinator is a friend row like any other (the
   sprint row's `coordinator` names her). Her receive thread is her own
   `nova-friend` daemon, installed once:

```
nova-friend install --as ada --harness opencode --dir ~/ada-working --server 127.0.0.1:6390
```

   It parks on her bus stream, pushes incoming messages into the session, and
   reports work to the sprint server. Its process does not prove the model's
   availability. It replaces `com.nova.loop.wake-serve-<seat>` and
   `nova-wake`.

3. **Hook liveness**: `nova-hook` delivers challenges for the model to answer
   with `nova-hook prove`. The hook record supplies availability to sprint;
   background ping scripts, daemon reports and separate session-proof records
   supply no liveness evidence. Sprint stores the scheduling hold separately.
