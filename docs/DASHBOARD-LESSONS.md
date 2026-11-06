# The v1 dashboard, extracted for v2

What nova-sprint v1's dashboard draws, how each cell is computed, what it looks like, and the
rulings that made it so, extracted 2026-11-06 so the v2 dashboard is a visual match. Glenn,
2026-11-06: "it should be exactly matching what we currently have, visually. no changes except i
think you added fail state." and "The dashboard design should be identical to the v1 in visual,
excluding the unimplemented stats part at bottom we didn't end up doing."

Sources (nova-sprint `origin/main`, read only): `internal/sprintdash/page/index.html` (CSS and
markup), `internal/sprintdash/page/app.js` (every cell), `internal/sprintdash/pageview.go` (the
data folds), `internal/sprintdash/dashboard.go` (reads, throughput), `docs/SPEC-SPRINT-DASHBOARD.md`,
`docs/SPEC-SPRINT.md` section 1, `assets/nova-sprint-dashboard.jpg`, the page lock
(`internal/sprintdash/pagelock.txt`), and the memory notes named in section 2.

**What the reference is.** The spec itself says (2026-10-11, "this is what it is."): where the
spec and the live page disagree, the live page wins. The live page is `index.html` + `app.js` as
last changed 2026-10-19 (commit 3edabe6d). This file describes that code. Where the spec text says
otherwise, section 0 lists the difference.

## 0. Where v1's own sources disagree (the code is the reference)

| topic | spec / screenshot says | the live code does |
|---|---|---|
| `assets/nova-sprint-dashboard.jpg` | the reference picture | dated 2026-10-05 (commit a2c5d1c7), before 30+ later asks. It shows a `machine running` chip, `Updated` with a green dot, the label "ALL CARDS BY STATE", "review + merge" in the In flight tile, review pink and merging amber, no `fix` state, no version chip. None of those are on the live page. **Do not match the jpg; match a fresh screenshot of the live v1 board.** |
| Refresh | page keeps `/events` open, polls as fallback | `/events` removed 2026-10-14 ("remove the /events and /landings.json requests"); the page polls `/api/sprint` on a fixed 1000 ms timer only |
| Work stream cell | display name (`stream set --display`), else key | `setText(r.nameT, k)`: the **key** always; app.js never reads a display name |
| Work subtitle | `<n> streams · <l> landed · <h> held` | `<n> streams · <l> streams landed · <h> held` (Glenn 2026-10-14: "landed" read as cards) |
| ETA while READY | "READY" in green (2026-10-13) | no READY anywhere in app.js; spec's own 10-11 paragraph: "no READY word in the ETA cell" |
| Friends panel | hidden by CSS for v1.2.x | shown (2026-10-13: "shows the friends table by default"); empty when there is no friends table (2026-10-20) |
| critical colour | "Both blocker and critical are the same bright red" (2026-10-08) | `--p-critical: #9b1c1c` (dark red) is the later rule in the sheet and wins on the fleet track; `--s-critical` #ff1a1a is unused by the track |
| Coin sound | asked 2026-10-11 | not on the live page; retired with the port (2026-10-14) |
| Fleet columns | `machine | status | ready | working | done | ok% | load` | eight grid children: machine, status, ready, track, n / width, done, ok%, load (header over the fraction is blank) |
| DONE in ETA | DONE when complete | DONE shows only when the summary has **no** `ETA <x>` token: a complete sprint whose summary ends `-> ETA -` shows `-` (app.js order: STOP, then the ETA match, then DONE) |

## 1. The screen, top to bottom

Global: dark only (Glenn 2026-10-04 2:21 PM: "just remove the toggle light/dark. always dark.");
`<html data-theme="dark">`; light tokens exist in the CSS but nothing selects them. Tab title
`nova-sprint`; favicon the robot tile (192 px PNG). Body background `--bg #0d0f12`, text
`--text #eef0f3`, padding `2rem 2rem 4rem` (16 px all round on a phone, 32 px bottom). rem = 16 px
(the root is never resized above 600 px). Body font `1.75rem/2.5rem` (28/40 px) system stack
`-apple-system, BlinkMacSystemFont, "SF Pro Text", "Inter", "Segoe UI", Roboto, "Helvetica Neue",
Arial, sans-serif`, antialiased, `font-variant-numeric: tabular-nums`. Numbers in table cells:
`--mono` = `ui-monospace, "SF Mono", SFMono-Regular, Menlo, monospace`, right-aligned; headers over
numeric columns right-aligned in the proportional face. Panels are full width and stacked; never
two columns; no horizontal scroll at any width.

Token table (dark): `--surface #14171b`, `--border #23272e`, `--border-strong #30353d`,
`--text-2 #a4aab4`, `--text-3 #6b727d`, `--text-4 #30353d`, `--track #262a31`; states
`--s-waiting #4a4f58`, `--s-ready #7d8490`, `--s-working #3d9bff`, `--s-fix #8b5cf6`,
`--s-review #fb8321`, `--s-merging #d55181`, `--s-landed #199e70`; track levels
`--p-blocker #ff1a1a`, `--p-critical #9b1c1c`, `--p-fix #8b5cf6`, `--p-reader #fb8321`;
status `--good #22b35e`, `--warning #fab219`, `--critical #e05252`; tints `--good-tint
rgba(34,179,94,.14)`, `--warning-tint rgba(250,178,25,.14)`, `--critical-tint rgba(224,82,82,.14)`,
`--neutral-tint rgba(164,170,180,.12)`; flash `--flash #7cb4ff`, `--flash-tint rgba(57,135,229,.24)`;
tiers `--tier-flash #3b82f6`, `--tier-pro #a855f7`, `--tier-heavy #f59e0b`, `--tier-frontier
#ec4899`, `--tier-other #4b5563`.

Order of regions: header, hero (5 tiles), Cost breakdown panel, Cards by state card, Work panel,
Fleet panel, Friends panel, Providers panel (only with data), Readers and merge panel (only
`?all=1`), Landings panel (always hidden), Last 24 hours panel (**omit in v2**), footer.

### 1.1 Header (`header.top`, one flex row, `align-items: center`, gap 1rem, wraps, margin-bottom 2rem)

1. **Logo**: robot tile PNG (`/logo-tile-192.png`, srcset 384 at 2x), `6rem` square (96 px),
   `7.5rem` (120 px) from 1200 px wide, 64 px at <= 600 px; `border-radius: 22.5%`. Box reserved
   before the image arrives. `--logo <file>` replaces it.
2. **Wordmark** `nova-sprint`: `ui-rounded, "SF Pro Rounded", "Nunito", system-ui`, weight 800,
   `5.5rem/6rem` (88 px), `7rem/7.5rem` from 1200 px, letter-spacing -.02em, lowercase, colour
   `--text`, `translateY(-.0795em)`, fixed width `5.262em`, nowrap. Nunito 800 woff2 is embedded,
   preloaded, `font-display: block`. Hidden (`.nofit`) when it does not fit (2026-10-12 ~03:08Z).
3. **Chips**, in order `version <v>`, `coordinator <name>`, `epoch <n>`: `.chip` inline-flex,
   height 3rem, padding `0 1rem .2333em`, 1 px `--border`, radius .75rem, text `--text-2` 1.5rem;
   the value in `<b>` `--text` weight 550. Version chip hidden unless `server_version` is set.
   The word "coordinator" hidden at <= 600 px (just the name). `fitMetaChips()` hides, in order,
   the wordmark, then epoch, then coordinator, while the clock is pushed off the logo's line or
   within 2rem of the last item; the version chip always stays.
4. **Release nav**: present in markup, always hidden (the envelope carries no releases).
5. **Clock**, far right (`margin-left: auto`): the time of the data on screen (`d.at`), not the
   wall clock. Markup `<span class="mono clk"><span class="hms">9:52:20</span> PM <span
   class="tz">EDT</span></span>`: hms in a fixed `8ch` box right-aligned (no jump 9 -> 10 o'clock);
   the spaces before AM/PM and the zone are proportional-face spaces (`.sp`), never mono; zone in
   `--text-3`; whole line 1.625rem `--text-2`, `translateY(-.1em)`. Viewer's own zone. No
   "Updated", no dot (2026-10-12 ~00:32Z). Reads "Connecting" until the first data. Never flashes.

### 1.2 Hero row (`section.hero`), five tiles

Layout: one row of five above 2000 px; at <= 2000 px a 6-column grid, tiles 1-3 span 2, tiles 4-5
span 3 (three then two); at <= 1100 px two per row, the last tile full width with its text
centred. Gap 2rem (12 px on a phone). Tile: `--surface`, 1 px `--border`, radius 1.5rem, padding
2rem (16 px on a phone). Label: 1.5rem/2rem, 600, letter-spacing .06em, uppercase, `--text-3`.
Figure `.big`: 4.5rem (72 px; 2.75rem on a phone), line-height 1.3, 600, nowrap,
`-apple-system, BlinkMacSystemFont, "SF Pro Display", "Inter", system-ui` (not mono: "a monospace
comma reads as a space", 2026-10-04 2:40 PM), tabular-nums, letter-spacing -.01em. Sub-line
1.625rem/2.25rem `--text-2`, one line, ellipsis.

Only the LANDED count flashes on change; every other tile figure is quiet.

1. **Landed**: figure `landed.toLocaleString("en-US")` (e.g. `1,544`) then `<small>` ` of 1,868`
   (0.5em, weight 500, `--text-2`, margin-left .5rem) above 1100 px. Sub: `82.7% complete` (one
   decimal) above 1100 px; `of 1,868 · 82.7%` at <= 1100 px. With `all == 0` the sub is only
   `not started` (2026-10-12 ~01:09Z, ~01:22Z). `landed` and `all` are counted over work cards,
   never sentinels (pageview.go foldSentinels).
2. **ETA**: in this order: (a) machine line contains `STOPPED` and the sprint is not complete:
   `STOP` in `color-mix(in srgb, #e05252 72%, #000)` (dark red), sub blank, at every width; (b)
   the summary carries `ETA <x>`: figure `<x>` with a proportional space between unit groups
   (`2h20m` -> `2h 20m`, `2d17h` -> `2d 17h`), sub `around 11:36 PM EDT` (= `d.at` + the ETA;
   `around Sun 3:15 AM EDT` when a day or more out), shortening to `11:36 PM EDT` then `11:36 PM`
   when it does not fit; `<x>` of `-` shows `-` with a blank sub; (c) `all > 0 && landed >= all`:
   `DONE` in `--good` and the sub the time it took, `1 day 3 hours 12 minutes` (from `cleared` to
   the end of the last 10-minute bucket with a landing in `landedSeries`); (d) else `-` with sub
   `not in the summary`. ETA itself (server side, foldSentinels): over non-sentinel primaries,
   rate = landings in the last hour (from `max(now-1h, cleared)`) x 3600 / span when there are 5
   or more, else `landed` x 3600 / (now - cleared); ETA minutes = ceil(left x 60 / rate); `-` when
   the rate is 0, fewer than 5 landed, or none left; written `XdYh` at >= 1440 min, `XhYm` at >= 60,
   else `Xm`.
3. **Cost**: `$937.62` (cents, rounded up, en-US grouping) above 1100 px; whole dollars rounded up
   (`$938`) at <= 1100 px (2026-10-12 ~00:30Z). Value = sum of `stream_costs[s].total_cost` over
   the streams on the table (archived ones out); once `done`, over every stream of the epoch. Sub
   `$0.61 per card` = ceil(cost / landed) cents; blank with nothing landed. Never "per landed card".
4. **In flight**: figure = working + fix + review + merging summed over the Work rows. Sub:
   `<working> working · <review+fix+merging> verify`, numbers `--text`, words `--text-2`; a part
   with 0 is dropped; blank (nbsp, height kept) when nothing is in flight (2026-10-12 ~01:10Z);
   shortens to `work/verify`, then `wk/vfy` when it does not fit. Tooltip:
   `N working, N review, N fix, N merging`.
5. **Throughput**: `Math.round(throughput)` cards per hour, or `—` (em dash) while null. Sub
   `cards / hour`. Tooltip `over the last N min` or `needs ten minutes of samples`. Throughput
   (server): samples of the page copy's `landed` at each good read, kept one hour, rate =
   (newest - oldest) x 3600 / span, null until the samples span 10 minutes; a lower `landed`
   (an archive or clear) restarts the samples; samples saved to a file at most every 10 s and
   reloaded on start, so a restart keeps the rolling hour.

No FLEET tile.

### 1.3 Cost breakdown panel (`section.panel.spend`)

Panel chrome as 1.6. Head: `h2` "Cost breakdown", then the tier legend on the title's baseline, one
line, no wrap: per tier with spend, a 1 rem square in the tier colour, the tier name `--text-2`,
the amount `--text` mono; ordered by spend, most first; `-` when no tier has spend. Body: grid
`3fr 7fr` above 2000 px, `3fr 9fr` at <= 2000 px, one column at <= 1100 px. Left: the pie (SVG
viewBox 100, radius 48, wedges from 12 o'clock clockwise in legend order, 1 px `--surface` stroke,
round joins; a grey `--tier-other` disc at opacity .35 when nothing is spent), max 40rem, a 1 px
`--border` rule to its right (below it when stacked). Right: the top streams table: columns
`stream | <each tier with spend, spend order> | total`; at <= 1100 px only `stream | cost`. Rows:
streams with a non-zero `cost`, most expensive first, count = max(3, min(rows, round((pieWidth -
1rem) / 5rem))). A tier cell with no spend is `-` in `--text-3`. Money format, decided once for
the panel: cents (rounded up) when every tier shown is under $10, else whole dollars rounded up.
Spend per tier = sum of `tables.work[s].cost_by_tier[tier]` over the four tiers (flash, pro, heavy,
frontier; "untiered" left out). With no stream spending: blank ruled rows, no message
(2026-10-12 ~01:07Z). Nothing in this panel flashes.

### 1.4 Cards by state (`section.progress-card`)

Card: `--surface`, 1 px border, radius 1.5rem, padding `2rem 3rem` (16 px at <= 400 px),
margin-bottom 3rem. Head: label `Cards by state` (the `.label` style, so rendered uppercase), then
the legend: seven items, `.sw` 1 rem square (radius 1px) + `<state> <count>` (plain integer, no
grouping), 1.5rem `--text-2`, gap `.5rem 2rem`. **State order (legend and bar): `landed, merging,
fix, review, working, ready, waiting`.** Counts = the Work table's column sums (archived out).

Bar: `.cells.thick`, a grid of cells 2rem (32 px) tall, 2 px gaps, radius 1px. Cards per cell:
the smallest of 1, 2, 3, 4, 5, 10, 20, 25, 50, 100 for which ceil(all / per) cells fit at 4 px
minimum each (fit = floor((width + 2) / 6)); cells shared among states by largest remainder, every
state with a card keeps at least one cell; no "1 cell = N" label. Recomputed on resize. Pulse:
every bar cell runs `pulse 2.4s ease-in-out infinite` driving a registered `--pulse` (keyframes
0%/17%/100% = 1, 58.5% = .55); merging cells use it at delay 0, review and fix at -2.25 s, working
at -2.1 s; landed, ready and waiting are still. The bar holds still (`#overall.stopped`) whenever the
machine line does not start with `running` or `STALE`. `prefers-reduced-motion` stops it. Bar
cells never flash.

### 1.5 Work panel (`section.panel.streams`)

Head: `Work`, subtitle `<n> streams · <l> streams landed · <h> held` (n = rows shown, l = rows whose
status is landed, h = held). **Columns (headers exactly, lowercase): `stream | status | waiting |
ready | working | review | fix | merging | landed | cost`.** Grid: `name-w status-w
repeat(6, minmax(5.625rem, 1fr)) 4rem minmax(5.625rem, 1fr) minmax(7.5rem, 1.25fr)`, gutter
2.5rem, padding `0 2rem`; landed spans the 4rem spacer plus its share and is right-justified; its
header is centred over the `n / total` cell (the header's width set by JS to the cell's). Status is
right-justified on the shared status edge (1.8).

Rows: the streams of `tables.work` minus archived ones. Order: by status rank landed 0, working 1,
stopped 2, held 3, anything else (waiting, idle) 3.5; then by key byte order. A 1 px `--border` rule
above the first row of each new group; no rules between rows of a group. Row min-height 5rem.

- **stream**: the key, weight 550, nowrap, ellipsis (full text as tooltip when cut); never dimmed.
  Then the tag (amber `--warning`, 1.375rem, 600, margin-left 1rem): `archived`, `<n> stuck`
  (`tables.merge[s].stuck > 0`), `ci red` (`tables.merge[s].ci == "red"`), joined ` · `.
- **status** pill, from `streamStatus`: all cards landed -> `landed` (dot `--s-landed`); stream
  State `stopped` -> `stopped` (critical); State `held` -> `held` (warning) (only a hold on the
  stream, never "all waiting", 2026-10-13); every card waiting -> `waiting` (neutral); any card in
  ready/working/review/fix/merging -> `working` (dot `--s-working`); State `landed` -> `landed`;
  some waiting -> `waiting`; else the State or `idle`. Tooltip `<status> · stream <State> since
  <h:mm AM>`.
- **waiting ready working review fix merging**: integers from the row; 0 in `--text-3`.
- **landed**: `n / total` (total = the row's seven counts summed): numerator right-aligned in a
  box `digits` ch wide, ` / ` in `--text-3`, denominator left-aligned in `--text-2`; digits = the
  digit count of the grand total, so every slash in the column lines up.
- **cost**: `money(w.cost)` with cents, or `-` grey; never flashes.

Total row: `Total`, blank status, the column sums, `sum / all` in the same frac form, the summed
cost; border-top `--border-strong`, weight 600. Compact form (when any row overflows, see 1.9):
`stream | status (dot only) | landed`, grid `minmax(0,1fr) status-w 10.5rem`, the landed figure
and its header centred (2026-10-12 ~00:57Z), status 1.5rem before it (~00:52Z "too close").

Clicking a stream name opens `/stream/<key>` (charts.js); see 1.10.

### 1.6 Panel chrome (Work, Fleet, Friends, Cost breakdown, Providers, Readers)

`--surface`, 1 px `--border`, radius 1.5rem, overflow hidden, 3rem between panels. Head: flex,
baseline, padding `2rem 3rem 1rem`; `h2` 2rem/3rem weight 600; subtitle 1.625rem `--text-2`. Fold
button at the right of every panel head: 2.25rem square, 1 px border, radius .5rem, glyph `⌃`
(U+2303) 1.5rem, rotated 180 deg when folded (160 ms ease-out); state kept in localStorage key
`sprint-fold-<h2 text lowercased>`; folded hides the body and the head's bottom padding becomes
2rem. Header row: min-height 4rem, 1.5rem/2rem, 600, `--text-3`, border-bottom `--border-strong`.
Data rows min-height 5rem; zero values `--text-3`.

### 1.7 Fleet panel and Friends panel (one renderer, `fleetLike`)

Head: `Fleet` / `Friends`; subtitle `<u> up · <h> held · <d> down`, plus ` · off` when the side is
switched off (`fleet_work` / `friends_work` == "off"), in which case the whole panel has opacity .45.
**Columns: `machine` (Friends: `friend`) `| status | ready | working (track) | n / width | done |
ok% | load`**; the header over `n / width` is blank in the full form and reads `working` in the
compact form. Grid: `name-w status-w minmax(4.5rem,1fr) minmax(0, calc(30.75rem + .5rem)) 10rem
5.5rem minmax(6.75rem,1fr) minmax(6.5rem,1fr)`, gutter 3.5rem, padding `0 2rem`; the fraction and
done columns are fixed, never content-sized (2026-10-06 9:28 PM).

Rows: one per member (Friends: per friend); order up, held, down, then name (localeCompare). A
"rowan-next" whose "rowan" is also a row shows as `rowan (next)` (2026-10-04 ~4:24 PM).

- **status** pill: up good (green tint, green dot), held warning, down critical; a stored "asleep"
  shows as `down`; only the first word of the status is shown.
- **ready**: integer.
- **track**: exactly `width` cells (nothing past the width) on a grid of `scale` columns; scale =
  the widest member's width, shared with Friends while the Friends panel is displayed, the
  fleet's own while it is not (2026-10-12 ~01:42Z). Cell width = (30.75rem - (scale - 1) x .25rem)
  / scale, so the widest track always spans what 16 cells of 27 px did (2026-10-04 ~4:08 PM);
  gap .25rem (4 px); height .6875rem (11 px). Lit cells left to right: blocker (`#ff1a1a`),
  critical (`#9b1c1c`), fix (`#8b5cf6`), reads (`#fb8321`, one cell a read), then working blue
  (`#3d9bff`); the rest `--track`. The lit cells number the row's working figure. A cell flashes
  once (brightness 1.9 -> 1, 1.5 s) when it lights or unlights; no steady pulse. Tooltip
  `<w> working of <width>: 2 blocker, 1 read`.
- **n / width**: `working / width` in the frac form (digits = the widest numerator or width in the
  table, the total's numerator included), then ` · <window>` muted .8em when the row carries a
  subscription `window`. Friends clamp working to width (raw count only in the tooltip).
- **done**: integer (counts from the last `stats tidy`).
- **ok%**: `okpct` (or `ok%`) as `97.3%`, `-` when absent; grey when done is 0.
- **load**: `12.5%` one decimal, `-` grey when absent (Friends: `-` until measured).

No numeric column in Fleet or Friends ever flashes. Total row: `Total`, blank, ready sum, blank
track, `sum(working) / sum(width)` (denominator digits may be wider), done sum, pooled ok% =
sum(ok) / sum(ok + failed) (failed read off the row, else back-computed from ok% ; 2026-10-14
"they lie"), blank load. No "n fix" under the tracks (2026-10-10: "Remove \"1 fix\"").

Fleet compact form: `machine | status (dot) | n / width`, the count centred under a centred
`working` header, no ok% (2026-10-12 ~00:45Z "too cluttered"). Friends compact: `friend | status
(dot) | n / width | ok%`.

Friends with no `tables.friends` (or empty): title only, no header row, no rows, no text, empty
subtitle (2026-10-20: "just leave empty").

Each fleet row's `reads_working` and `working` include its machine's reader rows' `reading`
(`reader-<m>`, `reader-<m>-<n>`) (2026-10-11: "you forgot to display the orange read cards").

### 1.8 Shared status edge

`.panel { --E: clamp(18.5rem, 45vw, 42rem) }`: the status column of Work, Fleet, Friends (and
Providers) ends at the same distance from the panel's left edge, pills right-justified, so the
pills line up down the page (Glenn 7:36 PM 2026-10-02). Name column = min(cap, E - pad - gap - S);
caps: Work 27.75rem, Fleet/Friends 15rem; S (minimum status width): Work 9.75rem, Fleet/Friends
9.5rem, compact 3.5rem. Status width = E - pad - gap - name.

### 1.9 Responsive

Compact form is chosen per table by measurement, not a breakpoint: `fitTables()` drops the
`compact` class, and adds it back if any row or non-name cell overflows (numbers get 7 px slack
for the flash tint; an ellipsised name is never a reason). At compact: padding/gap 1.5rem (1rem at
<= 600 px, row min-height 4.5rem). Pills in compact show the dot only. Hero steps at 2000 and
1100 px; `only-wide` / `only-narrow` swap at 1100 px. <= 600 px: root font 10.5px (10px at <= 400
px), logo 64 px, wordmark may wrap, body 16 px gutters, hero gap 12 px, tile padding 16 px, hero
figure 2.75rem. Checked by eye at 375, 430, 760, 1024, 1440, 2000.

### 1.10 Hidden or conditional panels

- **Providers** (`provider | status | balance | spend/hour | note`): shown only when
  `tables.providers` exists. Status up/resting/out of credit/unknown.
- **Readers and merge**: only with `?all=1`: `Reader | asked | reading | ok | broken`, total row
  `All`, subtitle `<n> readers`; then a strip of five cells `Queued, Merged, Stuck, CI, Stopped`
  (stuck amber when > 0, stopped red when > 0, CI red/green/grey).
- **Landings**: markup present, `hidden`, nothing unhides it (the 10-min stacked chart is off).
- **Last 24 hours** (`#history-panel`, charts.js + Pele, `/api/history`): the stats block asked
  2026-10-18. **Omit in v2** (Glenn 2026-11-06: "excluding the unimplemented stats part at bottom
  we didn't end up doing"). Its sub-line was `<n> landed · <n> attempts finished (<p>% ok) · <n>
  reads · <n> workers`.
- **Stream pages** (`/stream/<name>`, clicking a Work stream name, pointer cursor and underline on
  hover): asked the same day (2026-10-18) and served by the same charts.js. Not on the bottom of
  the page; whether it is part of "the stats part" is for Glenn (section 5).

### 1.11 Footer

One centred link `https://github.com/mas-bandwidth/nova-sprint`, 1.5rem/2.25rem, weight 200,
colour `--text-4 #30353d` (deliberately near-invisible), underline in `--border`, offset .25rem;
`--text` on hover; margin-top 3rem.

### 1.12 Motion and emphasis

- Flash on change (`flash`, 1.5 s ease-out, from `--flash` text on `--flash-tint` to normal),
  drawn on an inner `.fv` span (padding 0 .375rem, margin 0 -6px, radius .25rem) so the tint
  hugs the digits. The first render never flashes; only a value different from the last render
  does. What flashes: the hero Landed count, the Work count cells, the Work landed frac, the Work
  total row counts, a Work tag change. Quiet: every other hero figure and sub, subtitles, pills,
  Work cost, all of Fleet/Friends numbers, legend, chips, clock.
- Track cells flash once on lit <-> unlit; progress-bar cells never flash (they pulse).
- Rows are keyed nodes, moved not rebuilt, so a reorder never flashes.

### 1.13 Refresh and the copy

- Page: `fetch("/api/sprint", {cache: "no-store"})` immediately, then every 1000 ms on a fixed
  `setInterval` (never chained on the answer); at most 2 fetches in flight (a third tick is
  skipped); a fetch aborts at 10 s. A snapshot is drawn only when its `at` is newer than the one
  on screen. A failure changes nothing and says nothing. A changed `build` reloads the page and
  restores the scroll position (sessionStorage, retried at 0.5, 1.5, 3 s).
- Server: one poller; reads the sprint at most once per `--every` (1 s) while someone looked in the
  last 30 s; a request between ticks is answered from the copy; every answer no-store, gzip when
  asked. Envelope keys: `ok, data, fetchedAt, dataAgeSeconds, attemptAt, error, readSeconds,
  minInterval, throughput, throughputMinutes, build`. Stale alarm: data older than 2 s for 30 s
  logs `ALARM stale` and `/healthz` answers 503; the page shows nothing of it.
- Folds on the copy (pageview.go): stream_costs merged into work rows; reader rows' reading added
  to fleet rows; landed, all, summary x/y and ETA over work cards only (2026-10-11); archived
  streams' rows removed; `cards, merging, judgments, holds, lanes, rows` removed.

### 1.14 Where `fail` goes (the one addition)

Not ruled. Options for Glenn, both keeping every other column fixed: (a) Work columns `... |
merging | landed | fail | cost` and the legend/bar order `landed, fail, merging, fix, review,
working, ready, waiting` (the finals first); (b) `fail` after `waiting` at the bar's right end.
Also undecided: its colour (the palette's red `--critical #e05252` is "status, reserved: never a
series"), whether `landed`'s `n / total` total includes failed cards, and whether `fail` counts in
In flight (it should not: it is final).

## 2. The rulings, in Glenn's words

From the spec's lock sections, pagelock.txt, the CSS/JS comments and the memory notes. "Changed"
is what the code does because of it.

| date | words | changed |
|---|---|---|
| 2026-10-01 | "can you please add a final column to the work stream table, which is "cost"..." | Work `cost` column and total |
| 2026-10-02 | "the sprint table is the sprint dashboard"; "I like dark mode"; "Public so people can see it ... fine" | the page is the table's second view; may be public |
| 2026-10-02 7:19 PM | "Lowercase all column title names pls for all tables" | lowercase headers |
| 2026-10-02 | "honestly, 'working' is better." | state name `working`, never `active` |
| 2026-10-02 7:29 PM | "OK please lock this in. Do not change anymore. We are done." | spec locked |
| 2026-10-02 7:32 PM | "we just lost the nice state tabs in the workstream table. undo pls." | status pills in Work restored |
| 2026-10-02 7:34 PM | "Landed column in work stream table, please horizontal center align the column header" | landed header centred |
| 2026-10-02 7:36 PM | "Can we horizontally ALIGN the status columns across the three tables pls" / "aligned on the right align (column right side)" | shared status edge `--E` |
| 2026-10-02 7:40 PM | "ok this is perfect. lock this in." | LOCK 2 |
| 2026-10-02 | "It's honestly looking really good. well done. stylish." | (approval) |
| 2026-10-03 11:30 AM | "nova sprint website is not updating once per-second. something is chug." | 1 s refresh made strict |
| 2026-10-03 12:14 PM | (friends working clamped to width, soft) | Friends `n / width` clamped |
| 2026-10-03 11:03, 11:06 PM; 10-04 9:15 AM; 12:27 PM | (pulse timing; "hold max intensity about 50% longer") | 2.4 s pulse with plateau; 0.15 s stagger |
| 2026-10-04 | "I need to be able to always trust the dashboard" | one poller, freshness alarm |
| 2026-10-04 9:14 AM | "When the sprint is stopped ... the pulsing ... should stop" | bar still when not running |
| 2026-10-04 10:15 AM - 3:10 PM | Cost breakdown panel; "too small" (pie); "Round up to nearest $", "for this case"; $0 tiers dropped; untiered left out | pie + top streams, rounding rule |
| 2026-10-04 2:21 PM | "just remove the toggle light/dark. always dark." | dark only |
| 2026-10-04 2:40 PM | (a monospace comma reads as a space) | hero figures proportional face |
| 2026-10-04 2:55 PM; 10:15 PM | (tick lateness is the coordinator's); "STOPPED is plenty" | machine word only RUNNING/STOPPED/STALE |
| 2026-10-04 3:20, 3:25 PM | (a tile's subline is one line at every width) | nowrap + shorter forms |
| 2026-10-04 ~4:08 PM | the widest track always spans 16 cells, "so no matter the size, it works out" | TRACK_SPAN |
| 2026-10-04 ~4:24 PM | "this enforces it is one person, but multiple buds" | `rowan (next)` labels |
| 2026-10-04 10:40 PM | (the status word alone in the pill; reason is the coordinator's) | pill shows first word |
| 2026-10-04 ~11:55 PM; 10-05 4:09 PM | names were cut off; "plenty of room there to print the stream name" | Work name cap 27.75rem; top-streams name column takes spare room |
| 2026-10-04 8:04 AM; 8:07, 8:08, 8:10 PM | "change 'asleep' to 'down'"; "the friends table should LOOK exactly like the fleet table"; "for now, just put load as \"-\"" | one renderer; Total `x / y` |
| 2026-10-04 | "Is this progress visible in the sprint dashboard yet?" | merge row (later removed 10-08) |
| 2026-10-05 ~11:45 PM | "I would like you to remove all the already landed work streams." | archived streams off the table |
| 2026-10-06 2:43 PM | "I really don't think we have 2.8k cards post-archive..." | totals count only streams on the table |
| 2026-10-06 | "I would like a semi-fresh start to stats now" | done/ok%/cost from the last `stats tidy` |
| 2026-10-06 ~5:30 PM | "a blocker card bright red, a critical card dark red, a reader card the orange of the robot's shoes ..., work cards blue whatever their priority." | track level colours |
| 2026-10-06 | (working brighter; review takes the reader orange; merging takes the pink) | state palette |
| 2026-10-06 8:02 PM | "When [disabled], the table greys out a bit visually", "both chevron'd and open" | `.panel.off` opacity .45 |
| 2026-10-06 9:08, 9:25, 9:28, 9:33, 9:34 PM | no reads count beside the fraction; gap right of bars; "the totals are slightly misaligned"; "that did it"; "undo that last one" | fixed frac/done columns, digits rule |
| 2026-10-07 5:58-6:01 PM | "I would like the cards that are awaiting rework to be purple ..." / "they should be shown to the left of read cards, and to the right of critical cards in ordering." / "between review and merging" | `fix` state, purple, Work column, track order |
| 2026-10-08 | requested removals: priority strip, Merge panel, Lanes, archived summary/show-hide, tiers suffix on Fleet | those are absent |
| 2026-10-09 | "Something is wrong with the rendering for a friend" | one cell per read, lit cells = working |
| 2026-10-10 | review, fix and merge are one global state, verify; "Remove \"1 fix\"" | In flight `verify`; no fix count under tracks |
| 2026-10-11 | "this is what it is." | the Python live page became the reference |
| 2026-10-11 | "Remember that the reason we are in this mess is that you forgot to display the orange read cards." | reader-row fold into fleet rows |
| 2026-10-11 | "don't add things to the dashboard unless I ask for them"; "NEVER REMOVE THINGS FROM THE DASHBOARD UNLESS I ASK" | page lock |
| 2026-10-11 | "sentinel cards should not be counted when we are estimating time remaining, or cards per-hour"; "Right now they are inflating the ETA" | foldSentinels |
| 2026-10-11 | "agree that the name becomes the key, and you add a small display name. good tidy feature. pls add." | display name (spec); the page still shows the key (section 0) |
| 2026-10-11 | "Please add sounds ... 'ca-ching' ..." | coin; later retired with the Go port |
| 2026-10-11 | "weird slowness/juddering when I hold page down/up ... Please fix critical." | no layout work per tick beyond fitTables |
| 2026-10-11 | "Done is every card completed."; "The dashboard has to reflect the truth." | done counts every completed card |
| 2026-10-11 ~21:55Z | "for v1.2.* series, please hide the friends table entirely." | superseded 10-13 |
| 2026-10-12 ~00:28Z | "The nova sprint logo goes top left, and replaces the robot logo" | live page still shows the robot tile |
| 2026-10-12 ~00:30Z | "don't show the cents in the cost in iphone layout", "just round up to nearest dollar" | narrow cost whole dollars |
| 2026-10-12 ~00:32Z | "move the time to the right at the top, without 'Updated' and the green circle" | clock alone, right |
| 2026-10-12 ~00:32Z, ~00:36Z | "ETA DONE, with done in bright green"; "Too bright green. Less" | DONE in `--good` |
| 2026-10-12 ~00:35Z | "Below DONE show the time it took in days, hours, minutes" | tookText |
| 2026-10-12 ~00:42Z, ~00:55Z | top streams on a phone: "it's the cost per stream" | `stream | cost` narrow |
| 2026-10-12 ~00:43Z | (label) | "Cards by state" |
| 2026-10-12 ~00:45Z | Fleet compact ok%: "too cluttered" | no ok% in Fleet compact |
| 2026-10-12 ~00:50Z | "status should be same presentation in work and fleet panels. middle column. vertically aligned between both tables. close to the rightmost column as it can be" | compact grids share count width |
| 2026-10-12 ~00:52Z | "too close" | 1.5rem before the count |
| 2026-10-12 ~00:57Z | "center align working and landed" | compact count centred |
| 2026-10-12 ~00:58Z | "When stopped put STOPPED there in red" | STOP in the ETA tile; machine chip removed |
| 2026-10-12 ~01:00, 01:02Z | coordinator chip; "rowan" on the phone | coordinator chip |
| 2026-10-12 ~01:08Z | "too bright red. darker" | STOP 72% red mix |
| 2026-10-12 ~01:07, 01:09, 01:10, 01:22Z | (blank top-stream rows; "not started"; blank In flight) | empty states |
| 2026-10-12 ~01:20, 01:25Z | "put the version of nova sprint ... in a cell" | version chip; hide order |
| 2026-10-12 ~01:32Z | STOP at every width: "cleaner" | STOP not STOPPED |
| 2026-10-12 ~01:42Z | (scale from fleet alone while Friends is hidden) | sharedScale |
| 2026-10-12 ~03:08Z | "only when it fits, hide otherwise" | wordmark `.nofit` |
| 2026-10-12 ~03:10Z | (pie wedge spikes) | round stroke joins |
| 2026-10-13 ~02:10Z | "I don't want work stream names to look like variable names or tags. I want them to be human readable." | display names (spec) |
| 2026-10-13 | "When you are in it print 'READY' to ETA"; "Ready can be in regular text color." "or if you want, green." | spec only; not in live code |
| 2026-10-13 | "Please fix the sprint dashboard so it shows the friends table by default." | Friends shown |
| 2026-10-13 | "Once again the dashboard does not seem to be keeping up. It is supposed to regularly update at 1s ..."; "maybe gzip it on upload?" | background reads, gzip |
| 2026-10-13 | "don't show held on a work stream just because all its cards are in waiting state" | stream status rule |
| 2026-10-13 ~23:40 ET | (fix pulses with review) | fix cells pulse |
| 2026-10-14 | "You will port the existing python dashboard to golang."; "Yes, and then we retire the python dashboard, and only use the golang one."; "This is the one I look at! http://69.67.149.151" | Go serves the Python page byte for byte |
| 2026-10-14 21:16Z | "Remove the tiers list here: never asked for this." | no tiers on Fleet |
| 2026-10-14 | "remove the /events and /landings.json requests" | poll only; Landings hidden |
| 2026-10-14 | (subtitle "landed" read as cards) | "streams landed" |
| 2026-10-14 | (ok% total) "they lie" | pooled ok% over ok+failed |
| 2026-10-15 | "the dashboard is *required* to get up/down from the one source of friends being up/down ... down, whether held or not."; "the dashboard must reflect reality. no excuses." | status read, never derived; down > held > up |
| 2026-10-15 | "The dashboard is chugging 3-5 seconds per-update instead of every 1s" | the read's own server lane |
| 2026-10-17 | the dashboard updates every second while someone is looking | idle poller stops |
| 2026-10-18 | "I would like it if you could create a stats page showing me all the things you can do with https://pele.run ... below the existing stuff we have at the bottom." | Last 24 hours panel (**omitted in v2**) |
| 2026-10-18 | "Maybe we can click on a work stream by name, and this takes you to a page showing all details for that work stream ..." | stream pages |
| 2026-10-20 | "Please fix the sprint dashboard here: http://37.27.135.206"; "I don't want to see that text in the empty friends table. just leave empty" | empty Friends panel |
| 2026-11-05 | "if a card flaps around more than n times, it goes into blocked primary card state."; "blocked card is colored black." | v2 `blocked` state; its colour superseded 2026-11-06 ("You can make fail black"): fail is black, blocked its own token |
| 2026-11-06 | "The dashboard design should be identical to the v1 in visual, excluding the unimplemented stats part at bottom we didn't end up doing." | this file |
| 2026-11-06 | "I need it to check for visual correctness." "it should be exactly matching what we currently have, visually. no changes except i think you added fail state." | exact match + `fail` |
| 2026-11-06 | "remember that we iterated a lot on the dashboard, so if it comes out different in any way, it's going to make me sad." | side-by-side check (section 4) |
| 2026-11-06 | (setup) where the one dashboard runs: tailnet-private, or public and cache-hardened | SETUP.md |

## 3. The v2 mapping

v2 store (LAYER-1-STORE.md): `streams(name, repo, base, held, pos)`, `cards(id, kind
producer|sentinel|work|read, of, stream, state, attempt, bounces, review, holder, author, result,
cost µ$, priced, priority, tier, ...)`, `rows(name, kind worker|kin, width, state up|down, held,
reason, flaps, tiers, model, harness)`, `events(seq, op, kind, accepted, before, after, at,
epoch)`, `epoch(n)`, `judgements`, `cursors`. "Producer" below excludes sentinels.

| v1 cell | v2 source |
|---|---|
| clock (`d.at`) | MISSING as a field: `Snapshot` carries `Seq` and `Epoch`, no time; use the snapshot read's end time or the last event's `at` (decide) |
| version chip | the binary's version (no store read) |
| coordinator chip | MISSING (no seat/coordinator record in layers 1-2) |
| epoch chip | `epoch.n` |
| Landed / all | count producers with `state = landed` / count producers; whether `fail` is in `all` is undecided (1.14) |
| ETA rate | MISSING as a column: landing times are not on `cards`; derivable from `events` (accepted, after.state = landed, `at`) over the last hour; `cleared` = the clear event's `at` |
| STOP (machine STOPPED) | MISSING: no machine running/stopped state in layers 1-2 |
| DONE took-time | derivable: clear event `at` to the last landing event `at` (v1 used `landedSeries`, MISSING) |
| Cost tile / per card | sum `cards.cost` over consumers (µ$ -> cents, rounded up); unpriced = `priced = false` |
| In flight | producers in working + review + merging + fix |
| Throughput | the dashboard process's samples of the landed count (as v1), or landing events in the last hour |
| Cost breakdown by tier | sum consumer `cost` grouped by `tier` (fixed at add, the producer's; v1 used the tier each run actually ran on) |
| Top streams | per-stream sums of consumer `cost`, by tier |
| Cards by state counts | producers by `state`; `fix` MISSING as a state: derive (v1 rule: an attempt after the first, not blocker/critical; v2 candidate: producer in `ready` with `attempt >= 1` or in `working` with `attempt >= 2`, priority not blocker/critical); `blocked` and `fail` are new states with no v1 cell |
| Work rows | `streams` (all of the epoch; v2 has no archive, so "archived" is MISSING) |
| Work status `held` | `streams.held` |
| Work status `stopped` | MISSING (no stopped stream state) |
| pill tooltip "since" | MISSING (no stream state time; derivable from events) |
| tag `n stuck`, `ci red` | MISSING (no merge table; LANDING.md promises a merge table: queued, merged, stuck, CI, state) |
| display name | not used by the live v1 page; `streams` has none |
| Work cost | sum consumer `cost` where the consumer's `stream` = s |
| Fleet rows / Friends rows | `rows` with `kind = worker` / `kind = kin` |
| status pill | `state` + `held`: down if `state = down`, else held if `held`, else up |
| ready | consumers `state = ready`, `holder = row` |
| working | consumers `state = working`, `holder = row` (reads included) |
| width | `rows.width` |
| track: blocker / critical | working consumers whose producer's `priority` is blocker / critical |
| track: fix | working work consumers with `attempt >= 2` (rule to confirm) |
| track: reads | working consumers `kind = read` |
| done | consumers `state = done`, `holder = row` (v1 counted a work card only once its read said ok, and a read when it gave a verdict: decide whether v2 counts every done) |
| ok% | derivable: read consumers `done` with `author = row`: ok when `result = ok`, failed when `result = fix` (v1: the readers' verdicts on the row's work); a stats-tidy mark is MISSING |
| load | MISSING |
| window (subscription use) | MISSING |
| `fleet_work` / `friends_work` off | MISSING |
| Providers panel | MISSING (hidden when absent) |
| Readers table (`?all=1`) | read consumers by holder: asked = ready, reading = working, ok / broken = done with result ok / fix |
| Merge strip (`?all=1`) | queued = producers merging; merged = landed; stuck, CI, stopped MISSING |
| "needs you" | not on the v1 page; v2 docs promise a column reading `judgements` (section 5) |

**MISSING, collected**: data time on the snapshot; coordinator name; machine running/stopped
(STOP, bar pulse stop); landing timestamps as a column (ETA rate, took-time, landedSeries); `fix`
as a state (derivable, rule to confirm); stream `stopped` state and state-since time; archived
streams; merge stuck / CI red per stream; per-row load; per-row subscription window; fleet/friends
side on/off; providers; a stats-tidy mark for done / ok% / cost; the tier a run actually ran on.

## 4. What could differ by accident: the side-by-side checklist

Take both screenshots at 1440 and 375 px wide (and 2000, 1100, 760 for the hero breaks), same data
shape, same zone.

- [ ] Fonts: Nunito 800 wordmark loaded (block, not a fallback face); hero figures SF Pro
      Display, not mono; table numbers ui-monospace; clock digits mono with proportional spaces.
- [ ] Root 16 px, body 28/40 px; phone root 10.5 px (10 px <= 400 px).
- [ ] Every colour token in section 1 exactly (state, track, tier, pill tints, `--text-4` footer).
- [ ] Critical track cell dark red `#9b1c1c`, blocker `#ff1a1a`; review orange, merging pink.
- [ ] Region order: header, hero, Cost breakdown, Cards by state, Work, Fleet, Friends, footer.
- [ ] No Last 24 hours panel; no Landings; no Readers unless `?all=1`; no Providers without data.
- [ ] Header: logo 96/120/64 px with 22.5% corners; chip order version, coordinator, epoch; hide
      order wordmark, epoch, coordinator; clock alone at the right, no "Updated", no dot.
- [ ] Hero 5 / 3+2 / 2+2+1 at the 2000 and 1100 px breaks; last lone tile centred.
- [ ] Hero strings: `1,544` `of 1,868`, `82.7% complete`, `not started`, `2h 20m`, `around 11:36
      PM EDT`, `STOP` dark red, `DONE` green, `$937.62` / `$938` narrow, `$0.61 per card`,
      `69 working · 163 verify`, `—`, `cards / hour`.
- [ ] Label text "Cards by state" (uppercase by CSS), legend order landed, merging, fix, review,
      working, ready, waiting, plus `fail` where ruled.
- [ ] Bar: cell 32 px tall, 2 px gaps, >= 4 px wide, cards-per-cell ladder, pulse 2.4 s with the
      stagger, still when stopped.
- [ ] Work headers exactly `stream status waiting ready working review fix merging landed cost`
      (+ `fail` where ruled), lowercase; landed header centred; numeric headers right.
- [ ] Work row order landed, working, stopped, held, other; group rules only between groups.
- [ ] Work stream cell shows the key (as v1 does), tags amber.
- [ ] Landed `n / total` slashes on one vertical line, total row included.
- [ ] Fleet headers `machine status ready working (blank) done ok% load`; Friends `friend ...`.
- [ ] Track: exactly width cells, scaled to the widest member, span 30.75rem, 11 px tall, 4 px gap,
      lit order blocker, critical, fix, reads, working.
- [ ] Status pills line up on one right edge across Work, Fleet, Friends at 1440 px.
- [ ] Empty states: Friends empty (no text, no header); top streams blank ruled rows; In flight
      sub blank; Cost breakdown legend `-`; grey pie disc.
- [ ] Footer sums: Work total = column sums of rows shown; Fleet total `sum working / sum width`,
      ok% pooled, load blank, no fix count.
- [ ] Refresh: one request per second (Network tab), fixed timer, no `/events`, clock advances
      each second with the data; nothing flashes on a tick where nothing changed.
- [ ] Flash only on the cells in 1.12; track cells flash on lit change; Fleet numbers never.
- [ ] Fold chevrons on every panel; fold state survives a reload.
- [ ] Compact forms appear when a row overflows, not at a fixed width; Fleet compact has no ok%.
- [ ] No horizontal scroll at 375 px; 16 px gutters.

## 5. Contradictions with the "exact match" rule (for Glenn)

1. **The reference picture is stale.** `assets/nova-sprint-dashboard.jpg` predates 30+ rulings
   (2026-10-05). Matching it would undo them; match the live v1 board instead.
2. **`blocked`** is a v2 producer state Glenn ruled "colored black" on the board (2026-11-05); the
   exact-match rule names `fail` as the only addition.
3. **"needs you"**: v2's LAYER-1-STORE.md and CORRECT-USE.md promise a dashboard "needs you" column
   reading `judgements`; v1's page has none (judgments are stripped from its copy).
4. **Display names** (2026-10-11, 10-13 ~02:10Z: "human readable") are in the spec but the live
   page shows keys; matching the live page drops an asked-for feature.
5. **READY in the ETA tile** (2026-10-13) is in the spec, not in the live page.
6. **The logo** (2026-10-12 ~00:28Z: the nova-sprint logo replaces the robot) is not on the live
   page; it shows the robot tile.
7. **Stream pages** (2026-10-18) came with the stats work in charts.js; omitting the "stats part"
   may or may not include the stream-name click.
8. **Columns v2 cannot fill** (section 3 MISSING): load, window, stuck / ci red tags, STOP, the
   coordinator chip, stopped streams; an exact match needs those in the store or a stated blank.
