# `abshot` — Dashboard A/B Screenshot & DOM Region Diff Harness

`abshot` renders two dashboard URLs (`--a` and `--b`) side by side in headless Chromium across a list of viewport widths, captures full-page PNGs and visual pixel-diff overlays, and attributes every differing pixel region to the underlying DOM elements and their visible text in A and B.

## Driver & Dependencies

`abshot` uses the Go standard library only (`net`, `net/http`, `image/png`, `encoding/json`) and drives headless Chrome/Chromium (`chrome-headless-shell`, `google-chrome`, or `chromium`, overrideable via `--chrome` or `ABSHOT_CHROME`) directly over the Chrome DevTools Protocol (CDP) WebSocket in `cdp.go`. No browser driver package is added to `go.mod`.

## Running Against Live v1 and a Seeded v2 Board

`2000` is the primary desktop width the owner checks, alongside `1440` and `2800`:

```sh
# 1. Start a v2 board served from a migrated/seeded store (or fixture):
NOVA_PG_DSN='postgres://user@host/db?sslmode=disable' NOVA_PG_PASSWORD='...' \
  go run ./cmd/nova-sprint-dash --listen 127.0.0.1:18082 &

# 2. Run abshot at the owner's widths (2000 first, plus 1440 and 2800):
go run ./tools/abshot \
  --a http://37.27.135.206/ \
  --b http://127.0.0.1:18082/ \
  --widths 2000,1440,2800 \
  --out /var/tmp/abshot-out
```

### Window Height, Settle, and Anti-Aliasing Rules

1. **Window height rule (matching `v2-dash-shim`)**: Each page loads in an isolated browser profile with timezone `America/New_York` (`--tz`) at an initial viewport of `<width>x1000` (`deviceScaleFactor = 1`, `mobile = false`). After load and settle, the viewport height expands to `max(1000, ceil(document.documentElement.scrollHeight))` so full-page screenshots and `getBoundingClientRect()` coordinates share a single unscrolled `(0..W, 0..H)` page coordinate frame.
2. **1-second refresh settle**: After `document.readyState === 'complete'`, `document.fonts.ready`, and initial stream rows render, `abshot` waits `--settle` (default `1500ms`) so the dashboard's 1-second `/api/sprint` poll settles before extracting DOM boxes and capturing PNGs.
3. **Output files**:
   - `<out>/<width>-a.png`: full-page capture of `--a` at `<width>`
   - `<out>/<width>-b.png`: full-page capture of `--b` at `<width>`
   - `<out>/<width>-diff.png`: dimmed grayscale baseline with differing pixels highlighted in red (`#ff3050`)
   - `<out>/report.md`: markdown summary table and per-width region diff breakdown (`Width 2000` always placed first when present)
4. **Exit code & tolerance**: A pixel is considered identical when all RGBA channels are within `--channel-tol` (default `8`). `abshot` exits `1` if any width has more than `--pixel-tol` (default `64`) differing pixels, `0` if all widths pass within tolerance, and `2` on usage or runtime error.

## Reading the Report

Width `2000` is always ordered first in `<out>/report.md`, followed by `1440`, `2800`, and any other requested widths. Within each width section:

- **Structural and header mismatches** (table column headers, panel headers, or sections present on one board and absent/hidden on the other) are listed first in top-to-bottom page order.
- **Value and row differences** follow in top-to-bottom page order.
- Each region line reports the tight bounding box `(x, y, w, h)` in page coordinates of the differing pixels inside that semantic region, the differing pixel count `diff_px`, and the visible DOM text under that region in A and in B:
  ```text
  2000: Work table header (x=728, y=1675, w=1069, h=23, diff_px=9996): A has 'stream status waiting ready working review fix merging landed cost', B has 'stream status blocked waiting ready working review fix merging landed fail cost'
  ```
  Notice how `x=728` pinpoints the exact horizontal offset where the header diverges (`stream` and `status` at `x=33..728` match pixel-for-pixel; `waiting` on v1 vs `blocked` on v2 starts at `x=728`). Regions with zero differing pixels (`diff_px=0`, such as `Hero: Landed`, `Hero: Cost`, `Hero: In flight`, and `Cost breakdown` at `2000`) are omitted.

## Worked Example on Linux Bench (`vision`)

Command run on `vision` under `nova-reserve` comparing the live v1 board (`http://37.27.135.206/`) against Stella's `origin/v2-dash-shim` fixture server (`http://127.0.0.1:18082/`) at widths `1440` and `2000`:

```sh
abshot \
  --a http://37.27.135.206/ \
  --b http://127.0.0.1:18082/ \
  --widths 1440,2000 \
  --out /home/user/.cache/friendC-abshot-example
```

Pasted `<out>/report.md` (exit code `1`):

```markdown
# A/B Screenshot Report

- **A**: `http://37.27.135.206/`
- **B**: `http://127.0.0.1:18082/`
- **Tolerance**: per-channel delta <= 8, max differing pixels <= 64

| Width | Size A | Size B | Diff Pixels | Total Pixels | Diff % | Status |
|---:|---|---|---:|---:|---:|---|
| 2000 | 2000x6400 | 2000x6254 | 667585 | 12800000 | 5.22% | DIFF |
| 1440 | 1440x6288 | 1440x6190 | 1294327 | 9054720 | 14.29% | DIFF |

## Width 2000

- **Differing pixels**: 667585 / 12800000 (5.22%, tolerance <= 64 px at channel delta <= 8)
- **Viewport captures**: `2000-a.png` (2000x6400), `2000-b.png` (2000x6254), `2000-diff.png`

- 2000: Work table header (x=728, y=1675, w=1069, h=23, diff_px=9996): A has 'stream status waiting ready working review fix merging landed cost', B has 'stream status blocked waiting ready working review fix merging landed fail cost'
- 2000: Last 24 hours (x=32, y=6154, w=1936, h=98, diff_px=188503): A has 'Last 24 hours', B has '(absent)'
- 2000: Top bar (x=782, y=68, w=1093, h=48, diff_px=4911): A has 'nova-sprint version v1.6.29 coordinator friendF epoch 2 4:41:21 PM EST', B has 'nova-sprint version v2-dev epoch 1 4:41:24 PM EST'
- 2000: Hero: ETA (x=726, y=275, w=205, h=54, diff_px=5796): A has 'ETA STOP', B has 'ETA'
- 2000: Hero: Throughput (x=1049, y=533, w=138, h=112, diff_px=3694): A has 'THROUGHPUT 0 cards / hour', B has 'THROUGHPUT cards / hour'
- 2000: Cards by state (x=519, y=1390, w=1172, h=22, diff_px=11937): A has 'CARDS BY STATE landed 151 merging 0 fix 5 review 19 working 3 ready 23 waiting 175', B has 'CARDS BY STATE landed 151 fail 0 merging 0 fix 5 review 19 working 3 ready 23 blocked 0 waiting 175'
- 2000: Work table rows (x=566, y=1744, w=1365, h=2421, diff_px=129760): A has 'schema-main landed 0 0 0 0 0 0 5 / 5 $0.54 | schema-python landed 0 0 0 0 0 0 3 / 3 $0.02 | serialize-audit-c landed 0 0 0 0 0 0 1 / 1 $0...', B has 'schema-main landed 0 0 0 0 0 0 0 5 / 5 0 $0.54 | schema-python landed 0 0 0 0 0 0 0 3 / 3 0 $0.02 | serialize-audit-c landed 0 0 0 0 0 0 ...'
- 2000: Fleet table rows (x=1691, y=4452, w=245, h=979, diff_px=17214): A has 'amd-1 up 2 0 / 8 72 33.3% 0.4% | amd-2 up 2 0 / 8 75 33.3% 4.9% | amd-3 up 3 0 / 8 55 38.5% 1.7% | captain up 0 0 / 8 0 0.0% 0.0% | hetzn...', B has 'amd-1 up 2 0 / 8 72 | amd-2 up 2 0 / 8 75 | amd-3 up 3 0 / 8 55 | captain up 0 0 / 8 0 | hetzner1 up 2 0 / 16 56 | hetzner2 up 2 0 / 16 4...'
- 2000: Friends table rows (x=1691, y=5718, w=240, h=339, diff_px=3774): A has 'friendA down 0 0 / 2 0 0.0% - | friendC down 0 0 / 16 94 49.2% - | friendF down 2 0 / 32 309 61.8% - | friendG down 0 0 / 64 240 67.4% - | Total 2...', B has 'friendA down 0 0 / 2 0 | friendC down 0 0 / 16 94 | friendF down 2 0 / 32 309 | friendG down 0 0 / 64 240 | Total 2 0 / 114 643'
- 2000: Footer (x=32, y=6154, w=1936, h=182, diff_px=347255): A has 'https://github.com/mas-bandwidth/nova-sprint', B has 'https://github.com/mas-bandwidth/nova-sprint'

## Width 1440

- **Differing pixels**: 1294327 / 9054720 (14.29%, tolerance <= 64 px at channel delta <= 8)
- **Viewport captures**: `1440-a.png` (1440x6288), `1440-b.png` (1440x6190), `1440-diff.png`

- 1440: Last 24 hours (x=32, y=6042, w=1376, h=98, diff_px=133623): A has 'Last 24 hours', B has '(absent)'
- 1440: Top bar (x=173, y=50, w=1142, h=81, diff_px=17809): A has 'version v1.6.29 coordinator friendF epoch 2 4:41:14 PM EST', B has 'nova-sprint version v2-dev epoch 1 4:41:17 PM EST'
- 1440: Hero: ETA (x=539, y=275, w=205, h=54, diff_px=5798): A has 'ETA STOP', B has 'ETA'
- 1440: Hero: Throughput (x=769, y=533, w=138, h=112, diff_px=3694): A has 'THROUGHPUT 0 cards / hour', B has 'THROUGHPUT cards / hour'
- 1440: Cards by state (x=32, y=1278, w=1376, h=166, diff_px=121935): A has 'CARDS BY STATE landed 151 merging 0 fix 5 review 19 working 3 ready 23 waiting 175', B has 'CARDS BY STATE landed 151 fail 0 merging 0 fix 5 review 19 working 3 ready 23 blocked 0 waiting 175'
- 1440: Work panel header (x=33, y=1445, w=1374, h=136, diff_px=78033): A has 'Work 30 streams · 7 streams landed · 0 held', B has 'Work 30 streams · 7 streams landed · 0 held'
- 1440: Work table header (x=33, y=1541, w=1374, h=112, diff_px=15553): A has 'stream status landed', B has 'stream status landed'
- 1440: Work table rows (x=33, y=1611, w=1374, h=2523, diff_px=319051): A has 'schema-main landed 5 / 5 | schema-python landed 3 / 3 | serialize-audit-c landed 1 / 1 | serialize-audit-lobster landed 1 / 1 | serialize...', B has 'schema-main landed 5 / 5 | schema-python landed 3 / 3 | serialize-audit-c landed 1 / 1 | serialize-audit-lobster landed 1 / 1 | serialize...'
- 1440: Fleet panel header (x=33, y=4151, w=1374, h=141, diff_px=74738): A has 'Fleet 11 up · 1 held · 0 down', B has 'Fleet 11 up · 1 held · 0 down'
- 1440: Fleet table header (x=33, y=4247, w=1374, h=112, diff_px=13393): A has 'machine status working', B has 'machine status working'
- 1440: Fleet table rows (x=33, y=4317, w=1374, h=1083, diff_px=156320): A has 'amd-1 up 0 / 8 | amd-2 up 0 / 8 | amd-3 up 0 / 8 | captain up 0 / 8 | hetzner1 up 0 / 16 | hetzner2 up 0 / 16 | hulk up 0 / 8 | intel up ...', B has 'amd-1 up 0 / 8 | amd-2 up 0 / 8 | amd-3 up 0 / 8 | captain up 0 / 8 | hetzner1 up 0 / 16 | hetzner2 up 0 / 16 | hulk up 0 / 8 | intel up ...'
- 1440: Friends panel header (x=33, y=5417, w=1374, h=141, diff_px=75836): A has 'Friends 0 up · 0 held · 4 down', B has 'Friends 0 up · 0 held · 4 down'
- 1440: Friends table header (x=33, y=5513, w=1374, h=112, diff_px=14639): A has 'friend status working ok%', B has 'friend status working ok%'
- 1440: Friends table rows (x=33, y=5583, w=1374, h=443, diff_px=92303): A has 'friendA down 0 / 2 0.0% | friendC down 0 / 16 49.2% | friendF down 0 / 32 61.8% | friendG down 0 / 64 67.4% | Total 0 / 114 60.6%', B has 'friendA down 0 / 2 | friendC down 0 / 16 | friendF down 0 / 32 | friendG down 0 / 64 | Total 0 / 114'
- 1440: Footer (x=32, y=6090, w=1376, h=134, diff_px=114605): A has 'https://github.com/mas-bandwidth/nova-sprint', B has 'https://github.com/mas-bandwidth/nova-sprint'
```
