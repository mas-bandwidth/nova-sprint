# Tool score: nova-sprint v2, as an AI worker

Reviewed at `43d849e29d0e4568ba6a1348a076a59259639938`. I built `./cmd/nova-sprint`
with Go 1.26.6, ran `help` for every verb, read `docs/PLAYBOOK-WORKER.md`,
`docs/PLAYBOOK-COORDINATOR.md`, `docs/SETUP.md`, `docs/CORRECT-USE.md`,
`docs/CARD-CONTRACT.md` and this card's `JOB.md`, and ran `version`, `next`, `check`
and `help` against the machine. There was no live sprint and `docs/SETUP.md` shows
no throwaway-store path, so I did not run the store-mutating verbs.

## Score: 7/10

The spine is excellent: `next` is the front door, help is generated from the verb
table, refusals have one shape and name the fix, the worker playbook carries the
whole job, and `finish` actually verifies the head and commit identity. It is not
a 10 because the first mile is still a hunt: SETUP starts at a nova-tools fleet and
never shows a throwaway run, the credential wrapper is absent from `help`, every
`help <verb>` ends in a literal `...`, and `check` prints a 494-character shell
transcript as one line. Those are first-contact costs, not engine defects, so 7
rather than 5.

## What is good

- `next` never refuses and always names a command (`verbs/next.go`). Running
  `nova-sprint` with no arguments ran `next` and printed one `NEXT ...` line plus
  one `next:` line, exactly as `docs/CORRECT-USE.md:15` promises.
- `help` is built from the live verb table (`verbs/help.go:41-54`), so the index
  cannot list a verb that does not exist. That is the right answer to doc drift,
  and it is why the stale `edit` references below are so conspicuous.
- The refusal contract is real, not aspirational: `docs/CORRECT-USE.md:15-27`
  fixes the shape `SPRINT REFUSED <verb>: <what is not true>: <the command that
  makes it true>`, and the observed `check` refusal used it with exit 2.
- `docs/PLAYBOOK-WORKER.md` is a genuinely complete operating manual. `:5` says
  "every output you will see names the next command"; `:131-256` ("Work a card")
  covers clone reuse, the base-reds rule, PATHS, the identity, force-with-lease on
  the card branch only, and when to fail rather than finish a non-green head.
- `finish` is checked, not trusted (`docs/CORRECT-USE.md`, "Finish is checked, not
  trusted"): full 40-hex head, head present on the dealt branch at origin, and a
  commit-author check that names the offending commits. This is the part an AI
  most needs to be unable to lie about.
- `JOB.md` and the harness prompt carry the same finish rules word for word
  (`edge/worker/jobmd.go:21-44`, pinned by `edge/worker/live_test.go:163-190`), so
  the card the harness reads cannot disagree with the card the worker reports.

## What gets it to 10

1. **Ship a one-machine throwaway sprint.** Today `docs/SETUP.md:6-12` begins at
   nova-tools/Tailscale/ansible and the only documented path is `nova-sprint up`
   against a real fleet; a cold AI cannot exercise a single mutating verb safely.
   Add `nova-sprint up --local` (or `nova-sprint demo up`) that starts a throwaway
   PostgreSQL and Redis, creates a local bare repo and one stream, and a
   `docs/QUICKSTART.md` that uses it. Where: `cmd/nova-sprint/setup_production.go`,
   `verbs/setup_runtime*.go`, `docs/SETUP.md`, new `docs/QUICKSTART.md`. Done:
   on a fresh host with Go 1.26.6 and PostgreSQL 18, following only QUICKSTART,
   `up --local --as demo`, `stream add`, `add`, `start` and `take` reach a `TAKEN`
   line with no nova-fleet installed; a functional test runs that sequence.

2. **Make every `help <verb>` end in a runnable command.** `verbs/help.go:75` ends
   with `done("nova-sprint "+v.name+" ...")`; observed: `nova-sprint help take`
   ends `next: nova-sprint take ...`. Replace it with the verb's real minimal
   invocation (or the next verb in that role's journey). Done: for every verb,
   `nova-sprint help <verb> | tail -1` matches `^next: nova-sprint ` and contains
   no `...`; a table test in `verbs/` asserts it.

3. **Put the credential wrapper in the help index and echo it in the refusal.**
   `verbs/result.go:50` (`CredentialFix`) ends `-- nova-sprint ...`; observed:
   `nova-sprint next --as cold` prints `NEXT this process has no sprint credential`
   and a template, not a command. Add the one-line rule to `helpIndex`
   (`verbs/help.go:41`) and make the refusal repeat the actual verb and `--as`,
   e.g. `nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD --
   nova-sprint next --as cold`. Done: a no-credential run prints a copy-pasteable
   command; `grep -R -- '-- nova-sprint \.\.\.' verbs` is empty; a table test pins it.

4. **Make `check` one short line per fact, with the raw output behind a log or
   `--verbose`.** `verbs/check.go:64,77,100` passes whole tool errors into CHECK
   lines and the final refusal. Observed across a no-fleet run: 5 lines, 1,384
   bytes, longest line 494 bytes, including an apt/ansible transcript repeated in
   the `SPRINT REFUSED` line. Print the first actionable sentence per line, write
   the full stderr/stdout to `~/.local/state/nova-sprint/check.log`, and have the
   refusal point at it. Done: no CHECK line exceeds 300 bytes or contains a
   newline; the refusal is one line plus the log path; a unit test feeds a
   multi-line tool error and asserts single-line output.

5. **Add a real start-here page and move the chronicle out of the way.** The root
   `README.md:20` sends the AI to `docs/README.md`, which is a design history
   (layer locks, release gates, HN ideas, stitch pass) before it is a user guide.
   Add `docs/QUICKSTART.md` (install, `up`, `hook`, `stream add`, `add`, `start`,
   `take`, `finish`) and link it first from both READMEs; move the chronicle to
   `docs/HISTORY.md`. Done: a cold reader can reach a `TAKEN` line from README
   plus QUICKSTART without opening any `docs/LAYER-*.md`.

6. **Show one complete brief and one generated JOB.md.** `docs/CARD-CONTRACT.md`
   describes the fields but contains no full example; the only complete briefs in
   the tree are Go test strings. Add `docs/EXAMPLE-CARD.md` with a `tier:` line,
   `PATHS`, `TEST`, one `DONE-WHEN`, the prose, the `add --dry-run` output, the
   `take` receipt, and the `JOB.md` the worker writes. Done: the example's gate
   passes `edge.ParseGate`; a test parses the example brief with `parseBrief`;
   `docs/PLAYBOOK-WORKER.md:131` links to it.

7. **Test docs against the verb table.** `edit` is documented as a live verb in
   `docs/PLAYBOOK-COORDINATOR.md:293` ("fix the brief with `edit`"),
   `docs/SEAMS.md:75`, and `docs/LAYER-6-RUN.md:48,130`, but
   `verbs/core_test.go:68` asserts `edit` is an unknown verb and the real path is
   `replace` (`docs/PLAYBOOK-COORDINATOR.md:244`). Replace the stale references
   and add a doc test: every backticked `nova-sprint <verb>` and every bare verb
   named as a command in the playbooks, SEAMS and LAYER-6 must exist in
   `verbs.table`. Done: the test passes; `grep` for the old `edit` sentence is empty.

8. **Give `check` and `help` a `--json` form.** `next`, `take` and `report` already
   take `--json`, but the two verbs an unattended AI uses most are prose-only. Add
   `check --json` (one object per check: name, status, fix, detail) and
   `help --json` (the verb table). Where: `verbs/table.go` flags, `verbs/check.go`,
   `verbs/help.go`. Done: on the no-fleet host `check --json` emits valid JSON with
   one object per line, `help --json` emits every verb, and tests pin the schema.

9. **Make the brief and JOB.md carry the same contract.** The brief has line 1
   `tier:` and requires `TEST:` (`docs/CARD-CONTRACT.md:15,58-78`), while
   `edge/worker/jobmd.go:21-44` drops `tier` and `TEST` and lowercases the keys to
   `paths:`/`done-when:`. A worker that reads only JOB.md cannot tell it is a
   rendering rather than the admitted brief. Carry `tier` and `TEST` into JOB.md
   (or print a `brief source:` line and state the mapping in CARD-CONTRACT). Done:
   a test parses a valid brief, writes JOB.md, and asserts the JOB.md contains the
   tier and every gate is accepted by `edge.ParseGate`; the doc says which file is
   authoritative.

## Where I would be lost cold

1. **Install/setup, at `docs/SETUP.md` and `docs/README.md`.** README says only
   "ask your AI to read the docs" (`README.md:20`); `docs/README.md` opens with
   layer-lock history; `docs/SETUP.md:6` says "nova-tools first" but never gives a
   command that produces a tryable sprint. I would stall deciding whether to
   install Go, apt packages, nova-fleet, PostgreSQL, Redis, or run `up`.

2. **The credential wrapper, at `nova-sprint next` and `verbs/result.go:50`.**
   The first store verb I tried printed `NEXT this process has no sprint credential`
   and a fix ending in `nova-sprint ...`. `help` never mentions
   `nova-secrets exec`. I would stall trying to compose the wrapper from the
   playbook's examples, which pass `--as` but not the wrapper.

3. **The per-verb `next:` line, at `verbs/help.go:75` and `nova-sprint help take`.**
   The index lists 40 verbs with no examples and no role-based "first five"; the
   playbooks are only printed after `hook`, which needs a working bus and store.
   Following the tool's own next advice gives `next: nova-sprint take ...`, which
   cannot be run, so I would stall deciding between `hook`, `up`, `stream add`, and
   `add`.

## Tokens and time

- Go was not on `PATH`; I downloaded 64 MB of Go 1.26.6 and spent about three
  minutes before I could run `help`. The repo builds cleanly once the toolchain is
  there, but the README has no "you need Go 1.26.6 and PostgreSQL 18" line.
- The operational docs are about 1,700 lines across the two playbooks,
  `CORRECT-USE`, `CARD-CONTRACT`, `SETUP` and `docs/README`; the parts a worker
  actually needs are repeated in several of them. Most of the first hour went to
  reading, not working.
- `help <verb>` for all 40 verbs produced roughly 900 lines, most of it the same
  `up:` paragraph and the same unusable `next: ...` placeholder.
- `check` on a machine with no fleet cost 1,384 bytes in 5 lines, with the apt and
  ansible noise repeated in the final refusal; the actionable bits (`set
  NOVA_BUS_REDIS`, `run under nova-secrets exec`) were buried in it.
- The card brief and `JOB.md` I was handed were dense but clear: the done-when
  command, the paths, the deadline and the `result.json` form were all explicit.
  Nothing there cost me a retry.
