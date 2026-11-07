# dash

Package `dash` serves the sprint board: one embedded HTML page (`page/`) that polls `/api/sprint` once a second and patches itself in place, the JSON envelope it reads, and `/healthz`. The page and its assets are compiled into the binary.

There are three ways to feed a board:

| Feed | Constructor | Used by |
|---|---|---|
| The store, polled | `NewRecovering` / `New`, then `Cache.Run` | `nova-sprint dash --listen <addr>` on the private network |
| A snapshot published by `run` | `NewFromFeed`, then `Cache.Publish` | the board `run` serves itself |
| Another board's `/api/sprint`, pulled | `NewPullCache`, then `PullCache.Run` | `nova-sprint dash --listen <addr> --pull <url>` on a public host; it holds no store credentials |

```sh
nova-sprint dash --listen 127.0.0.1:8080
nova-sprint dash --listen :8080 --pull http://board.internal:8080
```

`--pull` takes `http(s)://host[:port]/path` with no user, query or fragment; `/api/sprint` is appended. The version chip shows `dash.Version`, which the binary sets (`dev` by default; a build can set it with `-ldflags '-X github.com/mas-bandwidth/nova-sprint/dash.Version=v1.2.3'`).

## Routes

| Route | Answer |
|---|---|
| `/`, `/index.html` | the board |
| `/public.html` | the board without the charts script |
| `/api/sprint` | the envelope below; `Cache-Control: no-store`; gzip when `Accept-Encoding` allows it (an explicit `gzip;q=0` refuses, otherwise `*` with a nonzero q allows) |
| `/healthz` | `ok`, or 503 `stale` once the data is 32 seconds old |
| `/stream/…`, `/api/stream/…`, `/api/history`, `/events` | redirect to `/` |
| `/app.js`, `/charts.js`, `/stream.html`, `/stream.js`, `/pele.js`, `/nunito-800.woff2`, `/logo-tile-{192,384}.png`, `OFL.txt`, `PELE.txt` | static assets |

A pull board answers only GET and HEAD, limits each client address to 60 requests per second (429 beyond that), serves `/api/sprint` with an `ETag` and `Cache-Control: public, max-age=1` (304 on a matching `If-None-Match`), and refuses an upstream envelope that is malformed (`ok` or `dataAgeSeconds` null, a negative age, unparsable `data`) without refreshing its age. Viewers never cause an upstream read; the pull runs once a second regardless of viewers. A failed pull keeps the last view, sets `ok` false with `error` `upstream pull failed`, and keeps ageing `dataAgeSeconds`.

## Envelope

`/api/sprint` returns `PageEnvelope`: `ok`, `data` (the board), `fetchedAt`, `dataAgeSeconds`, `attemptAt`, `error` (null on success), `readSeconds`, `minInterval` (1), `throughput` (landings per hour), `throughputMinutes` (the window the rate was measured over) and `build` (a hash of the embedded page). Table cells are strings (`"13"`, `"$2.13"`, `"33.3%"`); counts of unpriced runs and the `unpriced_runs` field stay numeric. Values the store does not carry are absent or `-`, never a fabricated zero.

`data` carries `landed`, `all`, `left`, `done`, `epoch`, `at`, `build`, `server_version`, `coordinator`, `machine`, `stopped`, `stop_reason`, `summary`, `eta`, `eta_window_minutes`, `eta_basis`, `eta_landings`, `cost`, `streams`, `stream_costs` and `tables` (`work`, `fleet`, `friends`, `merge`, `readers`).

## What the board shows

**Work.** One row per stream, counting producers by state: `blocked`, `waiting`, `ready`, `working`, `review`, `fix`, `merging`, `landed`, `fail`. A producer that is `ready` after its first attempt, or `working` at attempt 2 or more, counts as `fix` unless its priority is blocker or critical. An unknown producer state counts as `waiting` and logs `ALARM dashboard: producer "<id>" has unknown state "<state>"`. `blocked_cards` lists each blocked producer's reason (`held: <why>` for a card the coordinator holds, `missing-route: <what>`). The page shows `blocked` as its own column and legend entry only when nonzero, and the sprint is done when every producer has landed or failed.

**Streams.** A stream line is `landed` when all its cards landed, `fail` when all are final and at least one failed, `held` when held, `working` when any card is ready, working, in review, fix or merging, and `waiting` otherwise (empty streams included).

**Fleet and Friends.** The roster supplies the rows and widths: every roster worker is a Fleet row and every roster kin a Friends row, `down` until the store has the row; a store row the roster does not name is shown `down` at width 0. A row is `held` when it is up and held. Ready, working and done count a row's consumers (work and read cards) by holder. The Fleet and Friends fix track counts working work consumers at attempt 2 or more, unless the producer's blocker or critical priority takes precedence; reads count in a separate reads track. Tables are maps, so order is by name.

**ok%.** A worker's own finished work cards with result `ok`, over those with result `ok`, `fail` or `fix`, known the moment a card is done. A fault, a card with no result or any other result (a close, a release) and every read are not counted; a read adds to its reader's `done` only. The cell is `-` until there is one counted card. The total row weights each row by its own denominator.

**Load.** `load` is the row's last heartbeat: the 1-minute load average over the cores as a percent of all cores with one decimal (`18.6%`), with `slots` as `used / total` of the nova-reserve slots. A row that is down, or has never beaten, sends neither and the page shows `-`.

**Coordinator.** The first row by name whose role is coordinator, or empty.

**Machine.** `machine: RUNNING`, or `machine: STOPPED <reason>`. The page shows STOPPED in the machine chip, in the ETA tile and as a banner with the reason (also when all work has finished); starting the sprint hides it. The bar pulses only while the machine runs.

**Costs.** The cost tile is the API spend alone: priced API runs, work and read, by tier, divided by every landed card in this run (imported landings excluded; a card with no priced API run counts at $0). Subscription-seat runs add their tokens (`seat_tokens`) and never dollars. A stream with no priced run shows `-`. Unpriced runs are counted (`unpriced_runs`, split into no-tokens and no-price, with the models with no price in `missing_prices`) and never hide the priced sum. Amounts are exact micro-dollars internally; a total is rounded up to whole cents, and its tier cells (and a stream's work and read parts) are partitioned by largest remainder, alphabetical on ties, so they add up to the total. The per-card average is rounded to the nearest cent, or to the nearest hundredth of a cent under a cent. The Cost breakdown legend shows whole dollars rounded up.

**Throughput and ETA.** Throughput is landings per hour: the ETA's rate when the store provides landing times, otherwise the cache's own samples of the landed count over the last hour, which read 0 for the first ten minutes after a start and reset when the landed count falls or the epoch changes. The ETA uses the store's landing times when the store provides them: the last hour's landings when there are at least three, otherwise the sprint average (landings over the time since the first, at least ten minutes) when there are at least three in all; below that it is `-`. It is cards left at that rate, rounded up, as `47m`, `1h12m` or, from a day, `52d2h`. `eta_basis` is `last hour` or `sprint average`. With no estimate the ETA subline is blank.

## Freshness and health

The store-backed cache takes one Snapshot at startup (30-second deadline) and then, once a second, checks the identity of the last folded event, reads and folds the events after it (5-second deadline, `AdvanceDeadline`), and republishes the view. Requests read the cached view, so viewers never cause a store read. Running two dashboards on one store is unsupported.

- If the startup Snapshot fails, `NewRecovering` returns an unavailable cache (`/healthz` 503, `ok` false) together with the error, and `Run` keeps retrying the Snapshot, each attempt under its own 30-second deadline.
- A failed advance keeps the last view and its clock. Data older than 32 seconds logs `ALARM stale` and makes `/healthz` return 503; the next successful read clears both. `/api/sprint` keeps serving the last view meanwhile.
- If the log is restored behind the cache's position, the cache takes a new Snapshot. If the last folded event is missing or changed at the same or a later sequence, the read fails (a continuity check, not an authentication of the whole log).
- After consuming input the cache saves the global `dashboard` cursor with `SetCursor` and folds through that cursor's own receipt event, including events written by others in between. The saved cursor is the consumed prefix and excludes its own receipt; if an input arrives before the receipt, the next poll advances the cursor to cover it. Idle polls write no cursor events, including the first poll of an empty store. A failed cursor write logs a warning and is retried without suppressing the view that was read. A restart takes a fresh Snapshot; the cursor is not a stored image.
- The page judges pulse freshness from the server's `dataAgeSeconds` plus the client's monotonic elapsed time, so viewer clock skew does not matter.
- Throughput samples live in memory and reset on restart.

## Layout

At 2000 and 2800 px the ten-column Work table (no blocked cards) fits its row; with blocked cards the twelve-column layout uses a responsive clamp; narrow widths use a compact layout. `work_grid_layout_test.go` recomputes the grid from the rules in `page/index.html` and fails if they change.

## Tests

- Unit tests (`go test -skip Functional ./dash`) use fake stores and controlled clocks: `fold_test.go`, `cursor_test.go`, `cursor_race_test.go`, `wire_format_test.go`, `cost_wire_test.go`, `throughput_*_test.go`, `board_*_test.go`, `startup_*_test.go`, `history_anchor_test.go`, `page_source_test.go` (checks of the embedded JavaScript and HTML), `work_grid_layout_test.go`, `source_rules_test.go` (function length, unused symbols, deadlines on `store.Open`, this README's track rule).
- `testdata/reference-envelope.json` is a minimal envelope whose value types the wire-format tests compare against. `testdata/seed.json` is the golden for `TestFunctionalJSONGolden`; regenerate it with `UPDATE_GOLDEN=1 go test -run TestFunctionalJSONGolden ./dash` with PostgreSQL's `initdb` and `pg_ctl` on `PATH`. It carries the page's build hash, so it changes whenever anything under `page/` changes.
- Functional tests (`TestFunctional*`) run against a throwaway PostgreSQL where they need a store: the JSON golden, restart and terminated-backend recovery, and the pool surviving a cancelled open context. `TestFunctionalPublicDashboardThousandRequests` drives a pull board with a thousand loopback requests and runs only with `NOVA_DASH_LOAD=1`.
- Timing measurements (`TestLoad*`, `TestFunctionalPG5000IncrementalAdvance`, `TestDashboardIncremental5000`) run only with `NOVA_LOAD=1`, on a quiet Linux bench:

```sh
NOVA_LOAD=1 go test -count=1 -race -v -run '^(TestLoadRefresh5000|TestFunctionalPG5000IncrementalAdvance)$' ./dash
```

Both assert that one advance at 5,000 producers stays under 100 ms; the 20,000-producer pass only logs.
