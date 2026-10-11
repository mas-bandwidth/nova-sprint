# Release playbook

How a nova-sprint point release is cut, published, switched in and adopted, in order, with the
command for each step and the evidence that it passed. nova-sprint and nova-tools release in
lockstep: the same version number, and the nova-tools release bundles the nova-sprint, nova-card and
nova-work binaries of the nova-sprint release byte for byte.

This page is the procedure. The rules of the parts it drives are in the contract and are not
repeated here:

- the server switch, its shadow tick and its rollback window:
  [SPEC-SPRINT.md section 14](SPEC-SPRINT.md#14-the-machine) and
  ["install-canary-shadow-tick-r.w1"](SPEC-SPRINT.md#install-canary-shadow-tick-rw1-a-shadow-tick-before-every-server-swap);
- the seat's adoption window (`nova-sprint live`, `nova-sprint adopt`, the tools play):
  [SPEC-SPRINT.md, "Adopting a build"](SPEC-SPRINT.md#adopting-a-build), and the command forms in
  [CLI.md](CLI.md#adopting-a-build-nova-sprint-live-and-nova-sprint-adopt);
- the table schema the store must carry: [internal/sprint/TABLES.lock](../internal/sprint/TABLES.lock);
- what each point release carries: [FIXES.md](../FIXES.md), generated from
  [docs/fixes.sexp](fixes.sexp).

## Why this page exists

On 2026-10-11 v1.2.10 was cut and published, nova-tools first and then nova-sprint, and then had
to be rolled back:

- The server was switched with `server switch --rollback`. A card's own land refusal triggered the
  rollback (the window rolled back on any land exit other than 0), and the server was switched again
  without `--rollback`.
- The members' play ended `rc=2` at its new loops step, its first fleet run: on Linux it looked for
  launchd agents where systemd runs the members; on macOS hosts whose members are LaunchDaemons,
  and on the rest, it listed no member whose binary it had replaced, racing the members' own
  restart on the replaced binary. The
  members came up only because they restart on a replaced binary; the plays after the loops step
  did not run.
- The v1.2.10 server refused every done-path finish for about 40 minutes: the fleet table's new
  columns in TABLES.lock (`finished`, `redealt`) had never been added to the live store. The shadow
  tick passed, because it plans a tick and does not take a finish.
- Members whose finishes were refused ran the same generation again, met a non-fast-forward
  rejection on push, were classed as having an environment fault and were held: five of them. That
  hold could not be released on v1.2.10.
- The way back: an explicit switch to the v1.2.9 binary was refused by the base check (`the sprint
  base "" is not a branch name`), so the server went back with `server switch --rollback` (to its
  `.prev`), and the members were released with `fleet unhold`.

Each step below names what would have caught its part of that day.

## How to read a step

Every step has a **command**, a **pass** line (the evidence that it passed, read, not assumed) and,
where a tool is coming that will do it, a **replaced by** line naming the tooling item (the key is at
the end of this page). Until that item lands, the step is done by hand as written. A step that does
not pass stops the release; nothing after it starts.

Each step, when it passes or fails, writes one line to the release's progress file (step 13).

## 1. Scope and order

The point releases after v1.2.10, in the owner's order:

| Release | Scope |
|---|---|
| v1.2.11 | Small fixes, one concern each, no model change. |
| v1.2.12 | Tests and models only: no product change. |
| v1.2.13 | Contraction: fewer lines, no behaviour change, on v1.2.12's tests and checked models. |
| v1.2.14 | The next small fixes, including the defects v1.2.12's tests find. |

Point releases continue until only architectural items are open; those are v1.3 (step 11).

A release is cut from the heads, nova-sprint `main` and nova-tools `dev`, not from a list of
cards: a cut includes whatever has landed. So after the cut, every entry in `docs/fixes.sexp` whose
change is in the cut is re-filed to the release that shipped it (its `:release` set to the cut
version, `:status "shipped"`), wherever it was planned, and `make roadmap` regenerates the pages.

- **Command:** `git log --oneline <previous tag>..<head>` on both repos; for each landing, its
  `docs/fixes.sexp` entry; `make roadmap`.
- **Pass:** every landing in the range has an entry, each entry names this release, and
  `go test ./internal/roadmapdoc/` passes (FIXES.md is the sexp's).
- **Replaced by:** TOOLING 16 for "which build has behaviour Y".

## 2. Green heads

- **Command:** the last CI run of each head: `gh run list -R mas-bandwidth/nova-sprint --branch main
  --workflow ci.yml -L 1 --json conclusion,headSha` and nova-tools' certification run on `dev`.
  Then the pull requests merged since the previous release:
  `gh pr list -R <repo> --state merged --search "merged:>=<previous release date>"`.
- **Pass:** both runs are `success` at the exact head SHAs being cut; every merged PR in the range
  has its cold read on record (two independent reads before a merge). A red check is fixed, or
  proven to exist already on the previous tag, before the cut. The cut refuses a CI waiver on a
  green commit, so a waiver is only for a red or missing check, and names who allowed it.
- **Then:** `release/<V>` is created at each head, and the progress line records both SHAs.
- **Replaced by:** TOOLING 5 (the release verb resolves named green heads); TOOLING 19 for the
  reads of hand-made PRs.

## 3. Build both on a bench, check the bundle, dry run

Builds run on a bench, never on the coordinator machine.

- **Command:** build the nova-sprint set at the `release/<V>` head (nova-sprint, nova-card and
  nova-work for darwin-arm64, darwin-amd64 and linux-amd64, a `SHA256SUMS_<platform>` each, and a
  `COMMIT` file); build nova-tools' release with `sprint=<V>`; then `shasum -a 256 -c` on each
  platform, `go version -m` on every binary, and `cmp` of each bundled sprint binary against the
  sprint set. Then both cuts with `--dry-run`: `nova-update release cut --repo <repo> --from
  release/<V> --version <V> ... --dry-run`.
- **Pass:** the nova-tools build prints `RELEASE BUILD OK version=<V>`; every tool prints `<V>`;
  every binary's `vcs.revision` is its repo's head and `vcs.modified=false`; the three bundled
  sprint binaries per platform are byte for byte the sprint set's (`cmp` silent); each dry run prints
  `RELEASE CUT version=<V> sha=<head> ... dry-run=yes`. No tag, release or publish exists yet.
- **Replaced by:** TOOLING 5.

## 4. Publish nova-tools first, then nova-sprint

The owner's rule: nova-sprint always waits on nova-tools. A real nova-sprint cut is refused while
the nova-tools release of the same version is missing or still a draft.

- **Command:** for each repo in that order: check the tag is free and `release/<V>` has not moved;
  the real cut (`nova-update release cut` without `--dry-run`); check the tag peels to the head; for
  nova-tools, cancel any release workflow run the tag started and check it left no draft; stage the
  assets; check their names are the previous release's with the version changed; create the release
  as a draft; download the draft and `diff -r` it against the staged assets; only then publish.
- **Pass:** `RELEASE CUT version=<V> sha=<head> ... dry-run=no`, then `TAGGED <repo> <V> at <head>`,
  the downloaded draft identical to the staged assets, then `PUBLISHED <release URL>` for nova-tools
  and after it for nova-sprint. A draft that differs is left in place and the release stops.
- **Replaced by:** TOOLING 5.

## 5. Schema: TABLES.lock on the live store

The shadow tick does not prove the store carries the candidate's columns. The store does not grow
them by itself either: a TABLES.lock Change names the `nova-table` commands the live store takes,
and it takes them before the build that writes them is installed.

- **Command:** `git diff <previous tag> <V> -- internal/sprint/TABLES.lock`. For each new
  `# Change` block, run its `nova-table` commands against the live store, in the order the block
  gives (for v1.2.10: `nova-table col add fleet finished --after withdrawn`, `nova-table col add
  fleet redealt --after okpct`, `nova-table set fleet --hide finished`). Then `nova-table show
  <table>` for every table the diff touches.
- **Pass:** each `show` lists every column the new TABLES.lock names for that table, in its place,
  with its visibility. A drawn table that is never stored (the Change says so) takes no write.
  Only then is the server switched.
- **Replaced by:** TOOLING 20 (`doctor --preflight` refuses a store table missing a column of the
  candidate). No item yet applies the Change's commands; that step stays by hand.

## 6. Preflight: doctor before any switch or adopt

`nova-sprint doctor --preflight <candidate>` is the gate in front of every switch and every adopt.

- **Until it lands, the manual check is:**
  - the store: step 5's `nova-table show` evidence;
  - every member has what the candidate needs: `command -v go java sqlite3`, the tla2tools jar, the
    push credential, and the provider keys its routes use (names present, values never printed);
  - every bench has a mirror for every repo the cards land on;
  - no member's network link flapped in the window: the kernel's link-change lines in each Linux
    member's journal (`journalctl -k --since <t>`);
  - no member held or draining: `nova-sprint where` (standing holds the owner set are written in
    the progress line by name and cause);
  - no version skew the candidate cannot serve: `nova-sprint version` on every member against the
    candidate's;
  - today's `nova-sprint doctor` is green.
- **Pass:** every item holds; the progress line lists each.
- **Replaced by:** TOOLING 20 (`doctor --preflight`, folding in TOOLING 7 for link flaps).

## 7. Record the stable pair first

The owner's rule: the sprint lands its own fixes, so a server that cannot land cannot fix itself.
We keep a stable sprint version, revert to it, and never regress. Before anything is switched, the
seat writes down the pair it will come back to.

- **Command:** write `~/nova-bench/builds/STABLE` on the coordinator machine: `recorded_utc`
  (from `date -u`), the server build (version, commit, the kept binary's path and its sha256), the
  member build (the nova-tools version and commit, its build directory and its release), the
  evidence it is stable (finishes reaching done or review on this pair), and the commands that
  revert the server and the members to it. The file is read-only; the binaries it names are never
  deleted.
- **Pass:** `shasum -a 256` of the named server binary equals `server_sha256`, and the member
  build's sums match its release.
- **Changed:** only after a candidate passes its canary (steps 8 and 9), never before.
- **Replaced by:** TOOLING 20 (`doctor --stable` checks the pair's files exist and match their sums).

## 8. Server switch, then the canary on outcomes

- **Command:** `nova-sprint server switch <candidate binary>`.
- **Pass (the switch):** `SHADOW TICK OK ...` and `SERVER SWITCH OK target ... switched to ...`.
  This proves the candidate starts and plans a tick, nothing more.
- **Pass (the canary):** within the first minutes, cards reach done or review on the new server,
  and the members' logs show finishes exiting 0. **Zero done or review moves while finishes arrive
  means roll back now** (step 12): that is what refused finishes look like from outside.
- **`--rollback`:** the window is armed by the candidate's own land loop. Up to v1.2.10 it rolled
  back on any land exit other than 0, a card's own refusal included (2026-10-11). The fix,
  a-card-that-fails-its-build-never-rolls-back-the-server, is on `main` for v1.2.11: only a land
  pass fault (exit 2) rolls back. Switch with `--rollback` only to a candidate that carries the fix;
  otherwise switch without it and watch by hand. Either way the window sees land faults only, not
  refused finishes, so the canary is watched by hand until TOOLING 9 and 20 land.
- **Until it lands, the manual check is:** the server log's `working -> done` (or review) moves
  since the switch; each member's log for its finishes' exit codes and any refused finish's text;
  `non-fast-forward` in member logs; `nova-sprint where` for new holds, with their cause.
- **Replaced by:** TOOLING 20 (`doctor --canary`: FAIL is the rollback signal), TOOLING 9 (the
  server judges its own switch by its finishes), TOOLING 10 (every refused verb logged with its
  text, on both sides), TOOLING 13 (`doctor --since`).

## 9. Members: a staged adopt through the play

Members change through the ansible tools play of the nova-tools release and nothing else, the
coordinator machine excluded (its server moved in step 8). Use a private ssh control path directory
for the play (`ANSIBLE_SSH_CONTROL_PATH_DIR`): a shared one hung every play reusing it.

- **Command:** `ansible-playbook -i <inventory> fleet/tools.yml -e nova_version=<V> -e
  nova_source=<nova-tools checkout at the tag> -e nova_sprint_release=<the downloaded nova-sprint
  release> ...`, first with `--syntax-check`, then `--check --diff` over all members, then for real:
  **one canary member first** (`--limit <member>`), and the rest only after it passes.
- **Pass (check):** syntax `rc=0`; the diff installs `<V>` over the previous version on each host,
  replaces only the nova tools, removes nothing, leaves friends unchanged. (A `--check` failure at
  the report-only `table: one row` task alone is a known defect of check mode.)
- **Pass (the canary member):** the play's recap shows `failed=0` for it, it prints `<V>` from
  `nova-swarm version` and `nova-sprint version`, and it **finishes a card to done on the new
  build**. A failed canary stops the play, and the canary goes back to STABLE's member build.
- **Pass (the rest):** the recap shows `failed=0` on every host and the play `rc=0`; every host
  prints `<V>` and the head commit; the installed bytes match the release's `SHA256SUMS`; the build
  fact names `<V>`; `nova-sprint where` shows every member up, none held, none draining. A member
  that came up only by restarting itself on a replaced binary is not a pass: on 2026-10-11 the plays
  after the loops step (retired tools, build fact, friends, identity, table) did not run.
- **Replaced by:** TOOLING 12 (the staged adopt), TOOLING 13 (`doctor --since`), TOOLING 15
  (`fleet holds`), TOOLING 14 and 18 (a refused finish never becomes a re-run and a false
  environment hold).

## 10. Stable: move the record, then check it

- **Command:** once steps 8 and 9 have passed, rewrite `~/nova-bench/builds/STABLE` to the new pair
  (step 7's fields, the canary's evidence quoted), then check it.
- **Pass:** the new record's binaries exist and match its sums.
- **Until it lands, the manual check is:** `shasum -a 256` of each file STABLE names against the
  record.
- **Replaced by:** TOOLING 20 (`doctor --stable`).

## 11. Dogfood

The next release's cards run as cards on the release just adopted: v1.2.11's on v1.2.10, and so
on. A defect they find is a card for a later point release, never a hand fix on the live server.
Point releases continue until the only open items are architectural (the v1.3 re-architecture,
v1.4 its stable release); each architectural item names the point fixes it replaces.

- **Pass:** the release's cards land on the new build, and the counts in `nova-sprint where` move.

## 12. Rollback: always to STABLE

A rollback goes to the pair in `~/nova-bench/builds/STABLE`, one step, and is written down as it
happens.

- **The server:** `nova-sprint server switch --rollback` (no binary) restores `<target>.prev`, one
  step back, with no shadow tick. First check that the sha256 of `.prev` is STABLE's
  `server_sha256`; a second switch since the last stable one makes `.prev` something else. An
  explicit `nova-sprint server switch <STABLE's binary>` to an older build was refused on 2026-10-11
  by the base check (`the sprint base "" is not a branch name`); its remedy names `--base <branch>`
  and `--repo <clone>`, not yet proven as a way back.
- **The members:** the tools play with STABLE's member build, as in step 9.
- **Held members:** `nova-sprint fleet unhold <member>...` for each member the bad build held, with
  the cause written in the progress line. On v1.2.10 an environment-fault hold cannot be released:
  that is a v1.2.11 blocker.
- **What does not roll back:** configuration migrations, and columns added in step 5. The old
  build runs on the new schema, and the canary against STABLE (the pass line below) is what proves
  it does.
- **Pass:** the server prints the STABLE version; finishes reach done or review again (step 8's
  canary, against STABLE); no member held by the bad build.
- **Replaced by:** TOOLING 11 (`server switch --to <version|binary>`), TOOLING 15 (`fleet holds`
  and `fleet unhold` by cause), TOOLING 16 (`version --features`).

## 13. Record

- **A progress line per step**, in `<V>-progress.txt` (and `<V>-adopt-progress.txt` for steps 8 to
  10): `<date -u +%Y-%m-%dT%H:%M:%SZ> step<N> <name> ok|FAILED <evidence>`. The time is always from
  `date -u`, never typed. The evidence is the line the step printed, the SHAs, the counts. A line
  written before its check passed is corrected by a new line saying so, never edited.
- **Any manual step** (a script, a grep over member logs, a hand `nova-table` command, a judgment
  read one by one) is written down as a tooling card for the next sprint: work the seat does by hand
  is work a tool is missing.

## Lessons

- **The binary is half the release.** The other half is the store's schema, the members' units and
  the hosts' prerequisites. v1.2.10's binaries were right and the release still failed, on columns
  the store did not have.
- **Health checks measure outcomes, not readiness.** The shadow tick passed while every finish was
  refused. A check that the server answers is not a check that cards reach done.
- **A misclassified fault cascades.** A refused finish became a re-run, a non-fast-forward push,
  an environment fault and a hold that could not be released, on five members. An unknown failure
  must never hold a member.
- **Small steps with a known-good fallback.** One canary before the fleet; a STABLE pair recorded
  before the switch; a one-step way back that is checked before it is needed.
- **Duplicate code wastes fixes.** A fix made in one copy of a package is missing from the other
  (nova-sprint and nova-tools still share about 59 packages as separate copies): v1.2.13's
  contraction gives each one owner.
- **Check briefs against the code before admission.** Of ten v1.2.11 drafts, eight were stale
  against the code at their base.
- **Manual seat work points at a missing tool.** Each script and hand check above is a tooling
  item below.

## Tooling key

The items of the seat's tooling list of 2026-10-11 that replace the hand steps above.

| Item | What it does | Replaces |
|---|---|---|
| 5 | A lockstep point release verb: cut nova-tools then nova-sprint from named green heads, check the bundled sprint binaries byte for byte, dry run then real, publish, then adopt. | Steps 2-4, 8, 9 |
| 7 | doctor names a member whose network link flapped. | Step 6 |
| 9 | A server switch judged by its own finishes: zero done or review moves while finishes arrive is a pushed judgment, and inside the window an automatic rollback. | Step 8 |
| 10 | Every refused verb logged by the server with its text, and the member logs a refused finish's stderr. | Steps 8, 12 |
| 11 | `server switch --to <version\|binary>`: switch to any kept build, older included. | Step 12 |
| 12 | A staged adopt: one canary member to done on the new build, then the rest; a failed canary stops the play and rolls it back. | Step 9 |
| 13 | `nova-sprint doctor --since <t>`: per member, finishes ok or refused, non-fast-forward pushes, holds by cause. | Steps 8, 9 |
| 14 | A member never re-runs a generation it already pushed; the server answers a refused finish with what it holds. | Step 9 |
| 15 | `fleet holds` (cause and the releasing command) and `fleet unhold` by cause in one call. | Steps 9, 12 |
| 16 | `nova-sprint version --features`: does build X have behaviour Y. | Steps 1, 12 |
| 18 | A fault classifier that fails safe: only a named environment fault holds a member. | Step 9 |
| 19 | `nova-sprint read --pr <url>`: fleet reads of a hand-made PR, the merge refused until both land. | Step 2 |
| 20 | doctor as the release gate at both ends: `--preflight <candidate>`, `--canary` (FAIL means roll back), `--stable`. | Steps 5, 6, 7, 8, 10 |
