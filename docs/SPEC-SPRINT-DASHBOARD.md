# Sprint dashboard: the specification (the owner's decisions of 2026-10-02, written at the clock below)

This is the sprint dashboard: a page that is a second view of `nova-sprint where --json`,
served by `nova-sprint dashboard`. The terminal table that `where` draws stays the
canonical view, and its output is locked (internal/sprint/TABLES.lock); the page reads
the same JSON and adds nothing to the sprint. The page's files live in
`internal/sprintdash/page/` and are embedded in the binary, the wordmark's face
(Nunito 800, SIL Open Font License, its licence beside it) included, so the page loads
nothing from anywhere else.

The page is the live page the owner watches (the owner, 2026-10-05: "The live dashboard is
what I am watching and expect not to change (visually) ... the one I'm watching is the one
I want."): its files are the live files, copied unchanged but for the owner's name in
their comments, which reads "the owner" here. The bytes are the specification.
`TestThePageIsTheLivePageByteForByte` (internal/sprintdash) holds each file's sha256 and
the sha256 of what it renders (comments stripped), the latter equal to the live file's
after the same stripping; with `NOVA_LIVE_PAGE` naming a directory of the live files it
checks those too. A change to any byte of the page is a change to that test's sums, named
in the same commit with the owner's words. Where a line of this file and the page differ
in what is shown, the page wins.

A user tunes the page by editing the page and this specification together.
`TestDashboardPageIsTheSpec` (internal/sprintdash) holds part of the two equal: it reads
the quoted rules of this file and the page's markup (comments stripped, and app.js's
legend states) and compares the panels shown at load and their order, the panel titles,
the hero tiles and their labels, the progress bar's label and legend, the header's
wordmark and pills, and the footer's link. The table headers are app.js's (it draws
them) and are held by the byte pin. The rest (sizes, colours, motion, layout) is checked
by eye against this file, at the widths its Responsive section names. In this
repository the specification is locked (its lock sections): a line changes only with
the owner's words, quoted with the date. A copy of nova-tools is its owner's to tune the
same way.

## Serving and publishing

`nova-sprint dashboard [--listen <address:port>[,<address:port>...] | none] [--pull <address:port>[,<address:port>...] | none] [--logo <file>] [--every 1s]`
serves the page on each `--listen` address (default `127.0.0.1:7390`), one listener
each, sharing one cached copy of the sprint. It reads the sprint in-process the way
`where --json --cards` does (through the sprint's server when `NOVA_SPRINT_SERVER` names
one, else on the store `--redis` names), or with `--upstream <url>` from another
dashboard's `/api/sprint` (its `data`), at most once per `--every` and only while a page
or a puller asks or an event stream is open: `/api/sprint` is that copy as `where --json`
prints it (the cards and judgments `--cards` adds are the pull routes' alone), with the
build number and the throughput, and `/events` pushes each new copy as it is read
(server-sent events). The pull routes a worker reads its own view from (`/friend/<name>`,
`/machine/<name>`, their `/api/` and `/events/` forms) are served on the `--pull`
listeners (default `127.0.0.1:7395`), never on the page's, from the same copy:
[SPEC-SPRINT.md](SPEC-SPRINT.md), the dashboard. The copy also names the ready
buffer: `ready`, the ready primaries across streams; `width`, the total width of the
members that are up; `buffer`, the string `"<ready>/<2*width>"`; and `low`, true while
`ready` is under `width`. Every answer
is no-store; the page reloads itself when the build number changes (a new binary, or a
new `--logo` file). A read that fails holds the last good copy, the page says nothing,
and the dashboard's output takes one line per new failure, and once a minute a line of
the reads' count, failures and read times. `--logo` names an image file
served as the logo and the favicon; `--logo-dir` names a directory of logo files read
as the live page's server read the files beside the page (below); with neither, the slot
renders nothing. `/healthz` answers `ok`.

The live page ran under a Python server (server.py, beside the live files, run by the
units `com.nova.dashboard.local` and `com.nova.dashboard.public`). The verb serves every
path it served, with the same JSON, so the units run the verb in its place with no
visible change:

| path | server.py | the verb |
|---|---|---|
| `/`, `/index.html` | the page, `app.js` versioned by the build, the logo slot and favicon filled | the same, from the embedded page |
| `/api/sprint` | `{ok, data, fetchedAt, attemptAt, error, readSeconds, minInterval, throughput, throughputMinutes, build}`, data `where --json`'s, times Python's isoformat in UTC | the same keys in the same order, the same forms |
| `/app.js`, `/nunito-800.woff2`, `/OFL.txt` | the files beside the page | the embedded files, the same bytes |
| `/healthz` | `ok` | `ok` |
| `/favicon.svg` | `logo.svg` as the favicon; else 404 `no logo.svg` | the same, from `--logo-dir` |
| `/logo-tile-192.png`, `/logo-tile-384.png` | the tile's copies (sips); else 404 `no tile logo` | the same, from `--logo-dir` |
| `/logo-icon.png`, `/favicon.png` | the photo keyed (ffmpeg); else 404 `no raster logo` | the same, from `--logo-dir` |
| `/logo.webp`, `/logo.png` | the file when it exists; else 404 `not found` | the same, from `--logo-dir` |
| any other path | 404 `not found` | 404 `not found`, but `/events` and `/logo` (`--logo`), the verb's own |

The logo directory is read in this order: `logo.svg` (inlined in the title, 32 px,
currentColor, and served as `/favicon.svg`); else a tile, `logo-robot.webp` then
`logo-stella.png`, shown through its 192 and 384 px copies (`<stem>-192.png`,
`<stem>-384.png`, made with `/usr/bin/sips` when missing or when their time is not the
tile's); else a photo on white, `logo.webp` then `logo.png`, keyed with ffmpeg (on PATH)
into `logo-icon.png` (144 px tall) and `favicon.png` (64 px square), cropped first to
`1420:660:90:120`. Each copy takes its source's time. The files are read on each request
and are part of the build number, so a new logo shows on the next load. The page polls
`/landings.json` for its Landings panel; server.py never served it, so neither does the
verb, and the panel stays hidden on the page the verb serves, as it did.

The units' two lines, in place of `python3 server.py`: the tailnet listener reads the
sprint, `nova-sprint dashboard --listen <tailnet-address>:7390 --pull none --every 1s
--logo-dir <live dir>` (with `NOVA_SPRINT_SERVER` set as server.py had it), and the
loopback listener takes the first's copy, `nova-sprint dashboard --listen 127.0.0.1:7390
--pull none --every 1s --logo-dir <live dir> --upstream
http://<tailnet-address>:7390/api/sprint`. `--every 1s` is server.py's
`DASHBOARD_MIN_INTERVAL=1`.

The verb itself listens only inside the fleet's private network: `--listen
127.0.0.1:7390,<tailnet-address>:7390` serves this machine and the tailnet, and an
address every network reaches (0.0.0.0, ::) or any public address is refused, because
the page checks no credential. The page itself may be public: it carries no credential,
and what it shows of the sprint is fine for anyone to see. To publish it, put a reverse
proxy (Caddy, for example) on a machine of the owner's choosing in front of a loopback
listener; the proxy holds the public address and the verb never binds one. The sprint's
server, the verbs and the Television token stay inside the tailnet. The dashboard
exits 3 when its binary is replaced on disk, so its supervisor starts the new build
(docs/FLEET.md shows its loop row).

In Television the dashboard is a URL artifact on a channel, pointing at the page's
address (`http://127.0.0.1:7390/` on the machine that runs it); the Electron app shows a
URL artifact in a webview, a browser client a placeholder. The token that publishes to
Television is the owner's: it is never in this repository, never on the dashboard's
command line, and the dashboard never reads it.

## The live page (the owner, 2026-10-05)

The owner, 2026-10-05 ~11:30 AM ET: "The live dashboard is what I am watching and expect not
to change (visually) ... the one I'm watching is the one I want." The live page's own lines
(its SPEC.md beside the live files, which this section carries), over the locked text below:

The page's specification is nova-sprint's docs/SPEC-SPRINT-DASHBOARD.md (locked there). This
file holds the lines of this local copy (live/, served by ../server.py on 127.0.0.1:7390)
that the canonical spec does not carry yet; each moves into it with the card that does the
same in nova-sprint.

- Landings chart (the owner 2026-10-04 4:20 PM ET: "Can I get a cool graph showing cards landed for 'friends' and 'fleet' over time, like # of cards landed per-10 minutes as the sample." / "Put this graph underneath all tables" / "full width."): one panel titled "Landings", the last panel on the page, below every table, full width; stacked bars, one per 10-minute bucket over the last 24 hours, fleet at the base and friends on top, colours --series-fleet and --series-friends from :root; header legend: a swatch and total per series over the 24 hours, then the last hour's counts; y gridlines with counts, x labels every 2 hours in 12-hour time; no tooltips, no title attributes; dark; folds like the other panels; read from /landings.json (written every 60 s by ../bin/landings.sh, a stopgap) and redrawn when its "generated" changes; the panel stays hidden while that file cannot be fetched.

### Freshness: once per second, end to end (hard requirement)
the owner, 2026-10-04 ~6:03 PM ET: "once per-second updates are a hard requirement." / "lock that in."
- The public page (served from space) shows data at most 2 s old: the Studio's poller makes a fresh snapshot every second, space's puller fetches once per second, Caddy serves the JSON with max-age=1, and the page polls every 1000 ms.
- It holds under any viewer count: the Studio sees one request per second from space, never one per viewer.
- A freshness check measures the served snapshot's age, and an age over 2 s for 30 s is an alarm to the coordinator.
- The machine pill says RUNNING, STALE or STOPPED and nothing more; a stop's reason is the coordinator's view only (the owner 2026-10-04 ~10:15 PM: "STOPPED is plenty").
- The cost tile shows the recorded total and, under it, the cost per landed card (the owner 2026-10-04 ~10:50 PM: "Please bring that back"). No other text is added to the page unless the owner asks for it.
- A status pill shows the status word only (up, held, down); a reason the server carries after it is the coordinator's view (the owner 2026-10-04 ~10:40 PM).
- The Work name column fits the longest stream name with its tag (cap 27.75rem); the shared status edge --E is clamp(18.5rem, 45vw, 42rem), so the pills of Work, Fleet and Friends still end on one line (the owner 2026-10-04 ~11:58 PM).

## The specification

From here to the end is the owner's locked text.

Every change to the page is a change to this file first; the page conforms to the file; before every restart the
builder checks each line below against a screenshot at 1440 and 375 and fixes any drift before serving. A request
from the owner edits one line here and nothing else moves.

## Page
- Dark always: no light theme and no toggle (the owner, 2026-10-04 2:21 PM). Panels full width, stacked: header,
  hero row, Cost breakdown, progress bar, Work, Fleet, Friends, then Providers (when the JSON carries
  tables.providers) and Landings (above), footer. No readers or merge panel (available at ?all=1 only). No
  two-column layout at any width.
- Base type 28 px (doubled). Labels and headers: system proportional face. All numbers: monospace (ui-monospace,
  Menlo), right-aligned. Headers over numeric columns right-aligned too.
- Refresh: the page keeps `/events` open and patches in place as each copy arrives; while the stream is not open it
  polls the server's cached copy every 1 s on a fixed timer (never after an answer); the server reads the sprint at
  most once a second. A failed read: hold the previous data, change nothing, say nothing on the page (log only).
- Caching: no-store on everything; versioned asset links; the page reloads itself when the build number changes.

## Header (one flex row, align-items center)
- Logo: the image file `--logo` names (the owner's: a robot on the blocks, the sky version), 96 px (120 px from 1200 px wide), the
  tile's own rounded corners; also the favicon. With no `--logo` the slot and the favicon render nothing.
- The word "nova-sprint" in Nunito 800 (lowercase), the page's primary white (never cream), cap height about two
  thirds of the tile, optically centered with the pills.
- Pills: coordinator <name>, epoch <n>, machine <state>; then the Updated clock with its live dot (no theme toggle).
  The clock never flashes.

## Hero row: five tiles, one row at 2000 px, three and two below, two per row below 1100 px, large figure (72 px)
1. LANDED: n of all; sub-line "<pct>% complete". Narrow: the number alone, sub-line "of <all> · <pct>%".
2. ETA: "2h 9m"; sub-line "around 9:06 PM".
3. COST: total to the cent; sub-line "$0.24 per card" (never "per landed card").
4. IN FLIGHT: n; sub-line "14 working · 9 review" on one line.
5. THROUGHPUT: cards landed per hour over the last 60 min; "—" until ten minutes of samples; sub-line "cards / hour".
   A lone tile on its row spans the width with its figure centered.
- No FLEET tile. Flash on change: LANDED only; the others never.

## Cost breakdown (the owner, 2026-10-04 10:15 AM to 3:10 PM): one panel under the hero row, folding like the others
- Title "Cost breakdown"; its header line is the pie's legend (the tiers with spend, by spend, most first, the amount
  white), open and folded.
- A square pie of the spend by tier (flash, pro, heavy, frontier; a record with no tier is left out, a tier at $0 is
  left out), and beside it the most expensive work streams, as many as fit the pie's height: stream, then the tiers
  with spend and the total, at equal widths, plain headers. The tiers are nudged to add up to the total shown.
- One money format for the whole panel: cents (rounded up) when every tier shown is under $10, else whole dollars
  rounded up.

## Progress bar: "ALL CARDS BY STATE" with the legend (landed, merging, review, working, ready, waiting) and counts;
  one cell per card (per N cards when they would be under 4 px; no "1 cell = N" label); working cells pulse steadily
  (2 s); other cells still.

## Work (title exactly "Work"; subtitle "<n> streams · <l> landed · <h> held")
- Columns: stream | status | waiting | ready | working | review | merging | landed | cost (headers exactly so, all lowercase). The "landed" header is centred over its "n / total" cell (the owner, 7:34 PM: "Landed column in work stream table, please horizontal center align the column header"); every other numeric header stays right-aligned. The status column with its pills stays (the owner, after the lock, 7:32 PM: "we just lost the nice state tabs in the workstream table. undo pls."); the sort by status stays; a thin rule separates the groups (landed, working, stopped, held).
- Stream column capped (~220 px) and the Status column takes part in the even spread like the count columns, so the gap between the name and Status is as generous as the gap between any two count columns; names in full-strength text always (a label, never dimmed); the remaining width is
  spread evenly across the count columns (fixed table layout); Landed and Cost a little wider; gutters at least 40 px.
- Rows sorted by status like the fleet table: landed first, then working, then stopped, then held; within a group by stream name. A row moves when its status changes (no animation).
- Status pill in Work: landed (green), working (blue), held (amber), stopped (red); the subtitle keeps "<n> streams · <l> landed · <h> held". Zero counts muted grey.
- Landed as "n / total" with the slash on one vertical line (left number right-aligned to it, total left of it), the
  gap before Landed (after merging) equal to every other column gutter in the row, never tighter. No per-stream bar. Total row: numbers only.
- Flash: the count columns and Landed flash when their value differs from the previous second; Cost never.

## Fleet (title "Fleet"; subtitle "<u> up · <h> held · <d> down")
- Columns: machine | status | ready | working | done | ok% | load (headers exactly so, all lowercase).
- Machine column capped, and the Status column takes part in the even spread like the numeric columns (as in Work
  streams); names never dimmed. Status pill: up (green), held (amber), down (red).
- Working: a cell track, one cell per slot of the machine's width (nothing drawn beyond its width), cells 1.5x their current
  width (~27 px wide, height unchanged) with a 4 px gap, so the Working column is about 1.5x as wide, lit blue for working, dark fill for free, aligned on one grid down
  the column; then the "n / width" figure right after the track. Gaps: Ready to track and track to figure equal and
  wide (double the first attempt, ~64 px); every column gutter ~56 px.
- Cells: no steady pulse; a cell flashes once when it lights or unlights. No numeric column in Fleet ever flashes.
- OK% and Load: plain numbers, right-aligned, no bars, no dots. Total row: Ready and Done and OK% numbers, no cells.

## Friends (title "Friends"): same shape as Fleet without load (ready, working, done, ok%, status; headers lowercase); honest empty
  state until the JSON carries tables.friends.

## Footer: one line, the link "https://github.com/mas-bandwidth/nova-sprint", its text the address.

## Responsive (change what is shown, never squeeze; no horizontal scroll at any width; 16 px gutters on a phone)
- Below the breakpoint (where the full layout no longer fits): Fleet shows Machine | Status (dot only) | Working as
  "n / width" | OK%; no Ready, no cells, no Done, no Load. Work shows stream | landed (no status column); Friends like
  Fleet. Hero figures step down one size on a phone; the logo 64 px. Tested at 375, 430, 760, 1024, 1440, 2000.
7:19 PM

- Column headers in every table are lowercase (the owner, after trying capitals: "Lowercase all column title names pls for all tables").
- The stream state with cards in flight is called "working" wherever it is named (not "active"); the owner: "honestly, 'working' is better."

## LOCKED (the owner, 7:29 PM ET 2026-10-02: "OK please lock this in. Do not change anymore. We are done.")
This specification is locked. No line changes without his words, quoted here with the date, as TABLES.lock does for the terminal tables.
- 7:32 PM, the owner, a quoted change after the lock: the status pills in Work are restored ("undo pls").

## Column alignment across the three tables (the owner, 7:36 PM)
"Can we horizontally ALIGN the status columns across the three tables pls: work, fleet, friends" / "so they scan nicely as the eye goes top to bottom scrolling down." / "aligned on the right align (column right side)". The status column of Work, Fleet and Friends shares one right edge (one grid template or one fixed column width and offset for all three tables, so the pills line up as the page scrolls). Nothing else changes.

## LOCK 2 (the owner, 7:40 PM): "ok this is perfect. lock this in." The page as checked at this time (landed header centred, one status right edge across the three tables, the Fleet width column at 8rem) is the page. No change without a quoted line from him.
- 2026-10-03 11:30 AM, the owner, a quoted change after the lock: "nova sprint website is not updating once
  per-second. something is chug." The page's refresh is the event stream, the timer's poll its fallback (the
  Refresh line above); nothing else moves.
- 2026-10-05, the owner, a quoted change after the lock: "the one I'm watching is the one I want." The page is the
  live page, byte for byte in what it renders (the top of this file); the lines above that it changed (dark always,
  the Cost breakdown panel, the footer's link) say what it shows.
