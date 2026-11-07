# Tool score: nova-sprint v2, as an AI worker

Reviewed at `377c14c8c31b` (base `43d849e29d0e`), branch
`sprint/v2-review/v2r-score-flash1`. I built `./cmd/nova-sprint` with Go 1.26.6,
ran `help` and `help <verb>` for every verb, read `docs/PLAYBOOK-WORKER.md`,
`docs/PLAYBOOK-COORDINATOR.md`, `docs/SETUP.md`, `docs/CORRECT-USE.md` and
`docs/CARD-CONTRACT.md`, and ran `version`, bare `nova-sprint`, `next`, `next
--json` and `check` against this machine. No live sprint and no throwaway-store
path exists, so I did not run a store-mutating verb.

## Score: 7/10

The engine-facing experience is genuinely good: a bare `nova-sprint` answers
with `next`, help is generated from the live verb table, every refusal has one
shape and names a fix, `take` prints the whole brief plus the finish command,
and `finish` verifies the head, branch and commit identity instead of trusting
the worker. The card form is strong enough that this review itself could be
done cold from the brief and `JOB.md`. It is not a 10 because the first mile is
still closed to an AI without a live fleet: `docs/SETUP.md` starts at
nova-tools/Tailscale/ansible and never shows a local or throwaway sprint, the
credential wrapper an AI meets first is absent from `help`, every `help <verb>`
ends in a literal `nova-sprint <verb> ...`, and `check` on a bare host returns
1,384 bytes in five lines with a 494-byte apt/ansible transcript inside the
final refusal. Those are first-contact costs, not engine defects, so 7 rather
than lower.

## What is good

- **Bare invocation runs `next`, not usage.** Running `nova-sprint` with no
  arguments printed `NEXT this process has no sprint credential` and a `next:`
  line, exactly as `docs/CORRECT-USE.md` ("The one rule") promises. A cold AI
  typing only the tool's name is guided instead of scolded.
- **The help index cannot drift from the verb table.** `verbs/help.go:41-54`
  builds the index from `verbs.table`, and the index I saw had the same 40
  verbs as `help <verb>`, grouped `start/work/coordinate/operate`. That is the
  right architecture, and it makes the stale `edit` references below stand out.
- **Refusals are one contract, twice enforced.** `docs/CORRECT-USE.md` fixes
  `SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>`
  with exit 1, and `check` on this host ended with exactly that shape and exit
  2 for a could-not-run. The prerequisite chain in `docs/CORRECT-USE.md` is
  also enforced in code, not just prose.
- **`finish` is checked, not trusted.** `docs/CORRECT-USE.md` ("Finish is
  checked, not trusted") and `verbs/table.go` refuse a short sha, a head not on
  the dealt branch at origin, a non-holder, and a commit not made as the
  sprint's identity. This is the check an AI most needs to be unable to lie
  about, and `help finish` states each refusal with its fix.
- **The worker playbook is a complete operating manual.** `docs/PLAYBOOK-WORKER.md`
  covers clone reuse, the base-reds rule, PATHS, force-with-lease only on the
  card branch, `report --result fault`, and every refusal an AI will meet. The
  section "The refusals you will meet" is the most valuable page in the repo.
- **The card contract reaches the harness.** `edge/worker/jobmd.go` writes
  `JOB.md` from the same fields the prompt uses (`finishRules_internal`), and
  the `JOB.md` I was handed named the checkout, path, done-when and
  `result.json` form explicitly. Nothing in the brief cost me a retry.
- **`version` is honest about the build.** `nova-sprint version` printed
  `dev+377c14c8c31b`, the actual head, which let me tie observations to a sha
  instead of guessing which binary I had.

## What gets it to 10

1. **Ship a one-machine throwaway sprint.** `docs/SETUP.md:6-12` begins at
   nova-tools/Tailscale/ansible and the only documented path is `up` against a
   real fleet, so a cold AI cannot exercise a single mutating verb safely. Add
   `nova-sprint up --local` (or `nova-sprint demo up`) that starts a throwaway
   PostgreSQL and Redis, makes a local bare repo and one stream, and add a
   `docs/QUICKSTART.md` that walks it. Where: `cmd/nova-sprint/setup_production.go`,
   `verbs/setup_runtime*.go`, `docs/SETUP.md`, new `docs/QUICKSTART.md`. Done:
   on a fresh host with Go 1.26.6 and PostgreSQL 18, following only QUICKSTART,
   `up --local --as demo`, `stream add`, `add`, `start` and `take` reach a
   `TAKEN` line with no nova-fleet installed; a functional test runs that
   sequence.

2. **Make every `help <verb>` end in a runnable command.** `verbs/help.go:75`
   ends with `done("nova-sprint "+v.name+" ...")`; observed:
   `nova-sprint help take` ends `next: nova-sprint take ...`. Replace that with
   the verb's real minimal invocation, or the next verb in the role's journey,
   so the last line can always be copied. Where: `verbs/help.go:75`, the
   `verb` rows in `verbs/table*.go` where a minimal example belongs. Done: for
   every verb, `nova-sprint help <verb> | tail -1` matches `^next: nova-sprint `
   and contains no `...`; a table test in `verbs/` asserts it for all 40 verbs.

3. **Put the credential wrapper in the help index and echo it concretely.**
   `verbs/result.go:50` sets `CredentialFix` to `run under nova-secrets exec
   --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`, and
   `next`/`check` print that template verbatim. Add the one-line rule to the
   help index (`verbs/help.go:41`) and make the refusal name the actual verb and
   `--as`, e.g. `nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD
   -- nova-sprint next --as cold`. Where: `verbs/result.go`, `verbs/next.go`
   (`guidance`), `verbs/help.go`. Done: a no-credential run prints a
   copy-pasteable command; `grep -R -- '-- nova-sprint \.\.\.' verbs` is empty;
   a table test pins the rendered fix.

4. **Make `check` one short line per fact, with the raw output behind a log or
   `--verbose`.** `verbs/check.go` passes whole tool errors into `CHECK` lines
   and the final refusal. Observed on this host: 5 lines, 1,384 bytes, longest
   line 494 bytes, including an apt/ansible transcript repeated inside the
   `SPRINT REFUSED` line. Print the first actionable sentence per line and
   write the full stderr/stdout to `~/.local/state/nova-sprint/check.log`, then
   have the refusal point at it. Where: `verbs/check.go`, `verbs/tool_error.go`,
   `verbs/check_sprint.go`. Done: no `CHECK` or `SPRINT REFUSED` line exceeds
   300 bytes or contains a newline; the refusal is one line plus the log path;
   a unit test feeds a multi-line tool error and asserts single-line output.

5. **Add a real start-here page and move the chronicle out of the way.**
   `README.md:20` sends the AI to `docs/README.md`, which opens with layer locks,
   re-lock dates and design history before it is a user guide. Add
   `docs/QUICKSTART.md` (install, `up`, `hook`, `stream add`, `add`, `start`,
   `take`, `finish`) and link it first from both READMEs; move the layer
   chronicle to `docs/HISTORY.md`. Where: `README.md`, `docs/README.md`, new
   `docs/QUICKSTART.md`, new `docs/HISTORY.md`. Done: a cold reader can reach a
   `TAKEN` line from README plus QUICKSTART without opening any
   `docs/LAYER-*.md`.

6. **Show one complete brief and one generated `JOB.md`.**
   `docs/CARD-CONTRACT.md` describes the fields but contains no full example;
   the only complete briefs in the tree are Go test strings. Add
   `docs/EXAMPLE-CARD.md` with `tier:`, `PATHS`, `TEST`, one `DONE-WHEN`, the
   prose, the `add --dry-run` output, the `take` receipt and the `JOB.md` the
   worker writes. Where: new `docs/EXAMPLE-CARD.md`, `docs/CARD-CONTRACT.md`,
   `docs/PLAYBOOK-WORKER.md` ("Work a card"). Done: the example's gate passes
   `edge.ParseGate`; a test parses the example with `parseBrief`; the worker
   playbook links to it.

7. **Test docs against the verb table and remove stale verbs.** `edit` is
   documented as a live coordinator verb in `docs/PLAYBOOK-COORDINATOR.md:293`
   ("fix the brief with `edit`"), `docs/SEAMS.md:75`, and
   `docs/LAYER-6-RUN.md:48,130`, but `verbs/core_test.go:68` asserts `edit` is
   an unknown verb and the real path is `replace`. Replace the stale references
   and add a doc test: every backticked `nova-sprint <verb>` and every bare verb
   named as a command in the playbooks, SEAMS and LAYER-6 must exist in
   `verbs.table`. Where: the three docs, new test beside `verbs/core_test.go`.
   Done: the test passes; `grep -rn 'with `edit`' docs` is empty.

8. **Give `check` and `help` a `--json` form.** `next`, `take` and `report`
   already take `--json`, but the two verbs an unattended AI uses most are
   prose-only. Add `check --json` (one object per check: name, status, fix,
   detail) and `help --json` (the verb table). Where: `verbs/table.go` flags,
   `verbs/check.go`, `verbs/help.go`. Done: on a bare host `check --json` emits
   valid JSON with one object per line, `help --json` emits every verb, and
   tests pin the schema.

9. **Make `next` distinguish "nothing to do" from "cannot act".** With no
   credential, `nova-sprint next --as cold` prints `NEXT this process has no
   sprint credential` and exits **0**, the same exit code as a healthy
   `next: nothing; wait for the bus`; `next --json` likewise carries
   `"message"` but no machine-readable blocked flag. An unattended wrapper
   cannot tell a misconfigured row from an idle one. Keep `next` non-refusing
   but prefix the line `NEXT BLOCKED` and add `"blocked": true` plus the fix to
   `nextView`. Where: `verbs/next.go` (`nextView`, `runNext`, `guidance`),
   `docs/CORRECT-USE.md` ("What `next` looks at"). Done: a no-credential
   `next --json` has `blocked: true`, a healthy idle `next --json` has
   `blocked: false`, and a test covers both.

## Where I would be lost cold

1. **Install/setup, at `docs/SETUP.md:6` and `docs/README.md`.** README says
   only "ask your AI to read the docs"; `docs/README.md` opens with layer-lock
   history; `docs/SETUP.md` says "nova-tools first" but never gives a command
   that produces a tryable sprint. I would stall deciding whether to install Go,
   apt packages, nova-fleet, PostgreSQL, Redis, or run `up`, and I could not
   test a single mutating verb.

2. **The credential wrapper, at bare `nova-sprint` and `verbs/result.go:50`.**
   The first thing the tool printed was `NEXT this process has no sprint
   credential` with a fix ending `-- nova-sprint ...`. `help` never mentions
   `nova-secrets exec`, and the playbook examples pass `--as` but not the
   wrapper. I would stall trying to compose the missing verb and `--as` by
   hand.

3. **The per-verb `next:` line, at `verbs/help.go:75` and `nova-sprint help
   take`.** The index lists 40 verbs with no examples and no role-based "first
   five"; the playbooks are printed only after `hook` (or `help playbook`), and
   following the tool's own advice gives `next: nova-sprint take ...`, which
   cannot be run. I would stall choosing between `up`, `stream add`, `add` and
   `hook`.

## Tokens and time

- Go was not on `PATH`; I found 1.26.6 under `/home/nova/sdk` and spent time
  locating it. `go.mod` pins `go 1.26.6`, but `README.md` has no "you need Go
  1.26.6 and PostgreSQL 18" line, so a cold AI pays that search again.
- The help index is 6,372 bytes / 54 lines and the 40 per-verb pages are 257
  lines; each page repeats the same `up:` paragraph and ends in the same
  unusable `next: nova-sprint <verb> ...`. Cheap individually, but the
  placeholder makes the reading non-actionable.
- The operational docs are about 1,576 lines across the two playbooks,
  `CORRECT-USE`, `CARD-CONTRACT` and `SETUP`; the whole `docs/` tree is about
  7,076 lines. The parts a worker actually needs are repeated in several
  files, so most of the first hour went to reading rather than doing.
- `check` on a bare host cost 1,384 bytes in 5 lines, with a 494-byte
  apt/ansible transcript repeated in the final refusal; the actionable bits
  (`set NOVA_BUS_REDIS`, run under `nova-secrets exec`) were buried in it.
- The card brief and `JOB.md` I was handed were dense but clear: the checkout
  path, done-when command, path, deadline and `result.json` form were all
  explicit. Nothing there cost me a retry.
