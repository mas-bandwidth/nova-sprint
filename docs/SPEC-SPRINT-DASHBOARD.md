# See your team at work

The dashboard gives you a browser view of the same sprint state as
`nova-sprint where --json`. Use it to see the work moving, notice a review
queue growing, and check how your friends and fleet are doing. It is a
read-only view; decisions still go through the coordinator and sprint commands.

For a quick feature tour, [watch the live demo](http://69.67.149.151) and
[read the README](../README.md#watch-your-team-get-work-done). Follow streams,
check friends and fleet capacity, track per-stream and total spend, and watch
the estimated finish time change as work lands. This page describes the
embedded dashboard contract in this checkout; a deployed demo can run a
newer build with additional panels.

The page is embedded in the binary, including its Nunito 800 font. It loads
no external fonts or scripts. The terminal table remains the canonical view.

## Serving and publishing

To view an existing sprint server from the same machine, set its address
and start the dashboard:

```sh
export NOVA_SPRINT_SERVER=127.0.0.1:<sprint-server-port>
nova-sprint dashboard
```

Open `http://127.0.0.1:7390/` in your browser. The full command is:

```text
nova-sprint dashboard [--listen <address:port>[,<address:port>...] | none] [--pull <address:port>[,<address:port>...] | none] [--logo <file>] [--every 1s]
```

| Setting | What it does |
|---|---|
| `--listen` | Serves the page; defaults to `127.0.0.1:7390`. |
| `--pull` | Serves workers' own views; defaults to `127.0.0.1:7395`. |
| `none` | Disables the corresponding listener. |
| `--logo` | Uses a supplied image as the page logo and favicon. Without it, the slot is empty. |
| `--every` | Limits how often the shared snapshot is refreshed; defaults to one second. |

With `NOVA_SPRINT_SERVER`, the dashboard reads through the sprint server.
Otherwise it reads the store named by `--redis`. Page and pull listeners
share one cached snapshot, read at most once per interval while a client is
asking for it or an event stream is open. The page uses `/api/sprint` and
`/events`; worker routes such as `/friend/<name>` and `/machine/<name>` and
their API/event forms are served only by the pull listeners.

The snapshot includes build information, throughput, and the ready buffer:
`ready` counts ready primaries, `width` totals the width of members that are
up, `buffer` is `"<ready>/<2*width>"`, and `low` is true when `ready < width`.
See the [sprint contract](SPEC-SPRINT.md) for the underlying state.

The listeners have no authentication and accept only loopback or private
fleet addresses; wildcard and public bindings are refused. If you choose to
publish a page through a reverse proxy, decide which sprint details you want
to expose and put access control at that proxy as needed. Keep the sprint
server and its credentials private. `/healthz` returns `ok` for the dashboard
service; it is not proof that every worker or model is healthy.

Responses are `no-store`. The page reloads when its build number changes,
including a changed logo. If a sprint read fails, the last good snapshot
stays visible and the failure is logged; the page does not announce it.
Check the log when the display seems stale. The dashboard logs a summary of
reads, failures, and timings once a minute and exits 3 when its binary is
replaced so a supervisor can restart it.

A Television channel can display the page as a URL artifact. Its publishing
token belongs to the owner, stays out of the repository and dashboard command
line, and is not read by the dashboard itself.

### Changing the dashboard

The section below is the owner's locked visual contract. Keep its dated
requirements and the implementation together when an authorised design change
is made. `TestDashboardPageIsTheSpec` compares titles, column order, numeric
alignment, tiles, labels, legend states, wordmark, pills, and footer with the
page. Layout, colour, and motion also need visual checks at the stated widths.
A documentation tone pass does not change that design contract.

## The specification

From here to the end is the owner's locked text.

Every change to the page is a change to this file first; the page conforms to the file; before every restart the
builder checks each line below against a screenshot at 1440 and 375 and fixes any drift before serving. A request
from the owner edits one line here and nothing else moves.

## Page
- Dark by default, light by a small toggle. Panels full width, stacked: header, progress bar, Work, Fleet,
  Friends, footer. No readers or merge panel (available at ?all=1 only). No two-column layout at any width.
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
- Pills: coordinator <name>, epoch <n>, machine <state>; then the Updated clock with its live dot; then the theme toggle.
  The clock never flashes.

## Hero row: five tiles, one row at 2000 px, three and two below, two per row below 1100 px, large figure (72 px)
1. LANDED: n of all; sub-line "<pct>% complete". Narrow: the number alone, sub-line "of <all> · <pct>%".
2. ETA: "2h 9m"; sub-line "around 9:06 PM".
3. COST: total to the cent; sub-line "$0.24 per card" (never "per landed card").
4. IN FLIGHT: n; sub-line "14 working · 9 review" on one line.
5. THROUGHPUT: cards landed per hour over the last 60 min; "—" until ten minutes of samples; sub-line "cards / hour".
   A lone tile on its row spans the width with its figure centered.
- No FLEET tile. Flash on change: LANDED only; the others never.

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

## Footer: one line, "nova-sprint" bold white, then "from https://github.com/mas-bandwidth/nova-tools" (link).

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
