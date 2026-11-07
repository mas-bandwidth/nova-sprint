# abshot

`abshot` renders two dashboard URLs (`--a`, the baseline, and `--b`, the candidate) in headless Chromium at each of a list of viewport widths, captures full-page PNGs and a pixel-diff overlay, and attributes every differing region to the DOM elements under it and their visible text in A and in B.

It uses the Go standard library only and drives headless Chrome or Chromium over the DevTools Protocol WebSocket (`cdp.go`); no browser-driver module is in `go.mod`.

## Usage

    go run ./tools/abshot --a <url> --b <url> --out <dir> [--widths 1440,2000,2800] [flags]

| Flag | Default | Meaning |
|---|---|---|
| `--a` | required | URL of board A (baseline). |
| `--b` | required | URL of board B (candidate). |
| `--out` | required | Output directory, created if missing. |
| `--widths` | `1440,2000,2800` | Comma-separated viewport widths, each 200 to 10000. |
| `--channel-tol` | `8` | Per-channel (RGBA) delta, 0 to 255, within which two pixels are identical. |
| `--pixel-tol` | `64` | Differing pixels allowed per width before it counts as a difference. |
| `--settle` | `1500ms` | Wait after load so the board's 1-second poll settles. |
| `--tz` | `America/New_York` | IANA timezone for rendering, so clocks agree. |
| `--chrome` | none | Path to the browser; else `ABSHOT_CHROME`, else the first of `chrome-headless-shell`, `google-chrome`, `google-chrome-stable`, `chromium`, `chromium-browser`, `chrome` on `PATH`. |

The whole run has a 3-minute limit.

Example: compare a running board with a candidate served from a store.

    nova-sprint dash --listen 127.0.0.1:18082 &
    go run ./tools/abshot \
      --a http://board.example:8080/ \
      --b http://127.0.0.1:18082/ \
      --widths 2000,1440,2800 \
      --out /var/tmp/abshot-out

## Capture rules

1. Each page loads in an isolated browser profile at `<width>x1000` (`deviceScaleFactor` 1, not mobile). After load, `document.fonts.ready` and the `--settle` wait, the viewport height becomes `max(1000, ceil(document.documentElement.scrollHeight))`, so the full-page screenshot and every `getBoundingClientRect()` share one unscrolled `(0..W, 0..H)` frame.
2. A pixel is identical when all RGBA channels are within `--channel-tol`.
3. A width passes when at most `--pixel-tol` pixels differ.

## Output

In `<out>`:

- `<width>-a.png`, `<width>-b.png`: full-page captures.
- `<width>-diff.png`: a dimmed grayscale baseline with differing pixels in red (`#ff3050`).
- `report.md`: the same text printed to stdout.

Exit codes: `0` every width passes, `1` at least one width exceeds `--pixel-tol`, `2` a usage or runtime error (message on stderr, prefixed `abshot:`).

## Reading the report

`report.md` has a header (the two URLs and the tolerances), a summary table, then one section per width. Width 2000 is always first when requested, then the rest in the order given.

    | Width | Size A | Size B | Diff Pixels | Total Pixels | Diff % | Status |
    |---:|---|---|---:|---:|---:|---|
    | 2000 | 2000x6400 | 2000x6254 | 667585 | 12800000 | 5.22% | DIFF |

Each width section gives the differing pixel count and the capture names, then one line per differing region:

    2000: Work table header (x=728, y=1675, w=1069, h=23, diff_px=9996): A has 'stream status waiting ready working review fix merging landed cost', B has 'stream status blocked waiting ready working review fix merging landed fail cost'

- The label names the semantic region of the page (top bar, hero tiles, table headers, table rows, footer, and so on). A region present in one board and absent from the other shows `(absent)` for its text.
- `(x, y, w, h)` is the tight bounding box of the differing pixels inside that region, in page coordinates. Here `x=728` is the offset at which the header text first diverges.
- `diff_px` is the count of differing pixels in the region. Regions with zero are omitted.
- `A has '...'` and `B has '...'` are the visible text of the DOM elements under the region in each board.

Structural and header mismatches (column headers, panel headers, sections present on one board only) come first in top-to-bottom page order; value and row differences follow in the same order.

Comparison is by absolute position, so content shifted by a layout difference elsewhere also marks as different. Timestamps and version strings are not normalized.
