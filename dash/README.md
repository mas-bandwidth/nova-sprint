# Dashboard shim

The embedded page and assets are carried from v1 `internal/sprintdash/page`. The HTTP server is a reduced v2 adapter, with the v1 page routes and JSON envelope; it is not the full v1 Go server. Run against an existing v2 store:

```sh
NOVA_PG_DSN='postgres://user@host/db?sslmode=disable' NOVA_PG_PASSWORD='...' \
  go run ./cmd/nova-sprint-dash --listen 127.0.0.1:8080
```

`--pg` overrides `NOVA_PG_DSN`. The store must already be migrated. A build may set the version chip with `-ldflags '-X github.com/mas-bandwidth/nova-sprint/dash.Version=v1.2.3'`; the default is `dev`.

The process takes one cold Snapshot at every start, then pages Events from that snapshot's sequence; requests read the cached view. After consuming input it saves the global `dashboard` cursor with `SetCursor`, then folds through that cursor's receipt event, including intervening writers. The saved watermark is the consumed input prefix and excludes its own receipt; idle polls do not create cursor events. Restart deliberately takes a fresh snapshot rather than treating a cursor as a stored image. Throughput samples live in memory and reset on restart.

The page maps producers to Work and the state bar; sentinels are excluded. `blocked` appears as a separate red-brown column before waiting and in the legend only when nonzero; it has a separate subline count and is excluded from the in-flight figure; `fail` is black after landed and excluded from in-flight. Both count in the total; DONE means all producers are landed or failed. A non-blocker/non-critical producer ready after its first attempt or working after its second appears as fix. Streams show landed when all cards landed, fail when all are final and at least one failed, held when held, working when work is in flight, and waiting otherwise, including empty streams. Rows supply Fleet (worker) and Friends (kin), state/held, and width. Ready, working, done and track colours count consumers by holder, including reads. Read results attributed to their author supply ok% as yes / (yes + no); faults and fails are excluded; read consumers also supply the optional readers table. Queued and merged count producers in merging and landed.

Costs are the sum of priced consumers by stream and tier, even when other runs are unpriced; a stream with no priced run displays `-`. Exact micro-dollar tier totals are partitioned into display cents by largest remainder, with alphabetical ties, so tier cells sum to the rounded stream cost. The `stream_costs` accounting total remains unrounded. Throughput samples landed over the last hour, with a ten-minute warmup displayed as 0 and a reset when the count falls. The server polls every second even without viewers (unlike v1's 30-second viewer gate), and each advance has a 500 ms context deadline. The clock is the last successful poll's end time. Healthy cache data pulses the bar; a failed or stale poll holds the last figures and clock. Pulse freshness uses server-reported data age plus client monotonic elapsed time, so viewer clock skew does not change it. Data older than two seconds for 30 seconds raises `ALARM stale` and `/healthz` returns 503 until recovery. `/api/sprint` is no-store and supports gzip. `/public.html` omits charts; stream and history routes redirect to `/` until their layer-6 implementations exist.

The store has no coordinator, machine stop, row load or subscription window, provider status, stream state-since, stuck or CI status, or stats-tidy mark. These are left blank or omitted; the coordinator chip shows `coordinator -` as v1 does. ETA is unavailable. The last-24-hours stats panel is omitted. Missing ok% retains v1's `-`.

The tests in `coldread_test.go` originated with Rowan's 2026-11-06 cold reader; the unchanged original is [preserved](testdata/coldread-original.go.txt). The health test now checks the ruled 30-second grace, the cursor test permits the additional cursor events while requiring strictly increasing event-read cursors, the throughput test uses a controlled clock, and the unpriced test checks the actual priced sum instead of unconditionally failing from the v1 capture alone. The second cold-reader probes are attributed in `coldread2_test.go`, and their [unchanged original](testdata/coldread2-original.go.txt) is preserved.

## Visual comparison

The live v1 board was captured on 2026-11-06 at 21:50 UTC, in `America/New_York`, with a Linux browser. The v2 fixture was captured at 21:53 UTC and projects its captured work-state counts and row shape into synthetic store records: 30 streams, 151 landed of 376 producers, 12 worker and four kin rows, with representative per-tier costs and one additional unpriced waiting consumer. It is not the live store; synthetic read verdicts reproduce captured ok% denominators where available. Rows without verdicts show `-`, including the v1 zero-denominator cases. [Fixture source](testdata/visual-fixture.go.txt), captured envelopes and browser checks are in `testdata/`.

| Width | Live v1 | v2 fixture |
|---|---|---|
| 1440 | ![Live v1 at 1440 px](testdata/v1-live-1440.png) | ![V2 fixture at 1440 px](testdata/v2-1440.png) |
| 375 | ![Live v1 at 375 px](testdata/v1-live-375.png) | ![V2 fixture at 375 px](testdata/v2-375.png) |
| 2000 | ![Live v1 at 2000 px](testdata/v1-live-2000.png) | ![V2 fixture at 2000 px](testdata/v2-2000.png) |
| 2800 | ![Live v1 at 2800 px](testdata/v1-live-2800.png) | ![V2 fixture at 2800 px](testdata/v2-2800.png) |
| 1100 | [v1](testdata/v1-live-1100.png) | [v2](testdata/v2-1100.png) |
| 760 | [v1](testdata/v1-live-760.png) | [v2](testdata/v2-760.png) |

With no blocked cards, the full eleven-column Work table fits at both 2000 and 2800 px: its gutter is 35 px at 2000 and 40 px at 2800. At 2000, 36 px did not fit, so the five-pixel reduction is the smallest whole-pixel reduction measured. When blocked is present the twelve-column layout uses the earlier responsive clamp; at 375 px Work is compact. The [round-three browser probe](testdata/round3-browser-probe.json) checks conditional blocked/fail, zero cost and zero percent, string cells, redirects, DONE, clock skew and stale-poll recovery; its synthetic cases are not a live-store capture. The [ok% comparison](testdata/okpct-comparison.json) checks every row; the [gutter measurements](testdata/gutter-measurement.json) record the fit checks. The browser checks report loaded fonts, no horizontal overflow, no console or HTTP errors, and a one-second poll timer (actual request intervals have browser jitter).

## Validation

On Linux amd-1 with Go 1.26.10 and PostgreSQL 18.6, format, build, vet, unit race and PG functional race checks passed. One earlier full functional run saw a PostgreSQL connection reset in the store's rebuild test; the targeted test and subsequent full functional run passed ([failure](testdata/gates5.log), [recheck](testdata/functional-recheck.log)). Rowan's independent cold read reported approximately 14 ms for a one-event tick and 29 ms under race at 5,000 producers. Our 5,000-producer PostgreSQL bench measured a worst one-event advance of 5.98 ms without race and 15.61 ms under race over 20 iterations, with one Snapshot ([log](testdata/round3-pg5000.log)). Each event rebuilds the cached copy and view; this bounded measurement is not fleet-scale acceptance.


The full-width adjustment passed the focused Linux build, vet and race tests, including the PostgreSQL golden ([log](testdata/fullwidth-gates.log)).

## Round-two rulings

1. Throughput samples the last hour, rounds as v1, and displays 0 during warmup or no landings.
2. Priced costs remain visible alongside unpriced runs; no priced run displays `-`.
3. Empty and blocked-only streams wait; all-final streams with failures show fail.
4. Missing values stay blank; v1's cost, ok%, ETA and throughput fallbacks remain.
5. Stream/history and retired `/events` routes redirect to `/`; the link styling remains.
6. The bar pulses with fresh cache health, and stops after a lost or stale poll; a future machine state takes precedence.
7. The page envelope uses v1's field types, including numeric timings and null error on success; table cells use v1 string types.
8. The version comes from `dash.Version`, settable with `-ldflags -X`; its default is `dev`.
9. Blocked has a separate subline count and is excluded from in-flight.
10. Landed and fail together determine DONE; blocked and fail remain in the total.
11. A durable global `dashboard` cursor records each consumed prefix; a restart takes one new Snapshot.
12. Fail is black and blocked is a desaturated red-brown, both sheet tokens.
13. Two seconds of staleness held for 30 seconds alarms and fails health; `/public.html` omits charts.
14. Cost aggregation checks pricing once per consumer; the stray cache-loop blank line is removed.

Round-two Linux format, build, vet, unit race and PostgreSQL functional race checks passed ([log](testdata/round2-gates.log)). The attributed cold-reader's 5,000-producer PostgreSQL test measured a worst incremental advance of 15.418 ms across 20 iterations, with one Snapshot ([log](testdata/round2-pg5000.log)); this is a bounded bench result, not fleet acceptance. The original cold-reader probe file remains in `testdata/coldread-original.go.txt`.

Round-three Linux format, build, vet, unit race and PostgreSQL functional race checks passed ([log](testdata/round3-gates.log)); the fresh six-width capture and its browser probes are in `testdata/`.
