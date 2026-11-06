# Comparison provenance

- Dashboard: `be777a828e33d33fd2d99cdbc7d23d08d10cb9c2`, seeded from `fixture-input.json` via `../visual-fixture.go.txt`; the input is a read-only live v1 response, not the v2 store.
- Harness: `tools/abshot` at origin/v2 `f70cf5e1e6151a5127df48e17ef2d59ced5f5172`, built on Linux amd-1 with Go, unmodified and outside this branch.
- Browser: Playwright `chromium_headless_shell-1254`, timezone `America/New_York`, default settle 1500 ms; channel tolerance 8, pixel tolerance 64.
- Exit status: **1 (DIFF)**. `verified.log` is the complete harness output and `report.md` is the unmodified generated report.

```sh
abshot --a http://37.27.135.206/ --b http://127.0.0.1:18082/ \
  --widths 2000,1440,2800,1100,760,375 \
  --chrome /home/nova/.cache/ms-playwright/chromium_headless_shell-1254/chrome-headless-shell-linux64/chrome-headless-shell \
  --out /var/tmp/stella-abshot-20261106/verified
```
