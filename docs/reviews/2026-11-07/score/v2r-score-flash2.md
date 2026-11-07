## Score: 8/10

The enforced chain, the push model, and the per-verb refusals are the best I have used for
multi-agent work, and `help`, `check`, the playbooks and the card contract are already written
for an AI rather than a human. What keeps it from 10 is the cold path, not the engine: the
`JOB.md` I was given is a 20-line envelope rather than the card, there is no quickstart or
documented throwaway store, and several reporting paths (`show` with no name, the 19-document
reading list, a stale end-to-end transcript) cost tokens or actively mislead. I could run a
sprint with this tool today; I would not hand it to an AI that had never seen it and expect the
first hour to be smooth.

## What is good

- The `help` index in `verbs/help.go` is a real map: every verb grouped, every role marked,
  and the three exit kinds stated up front ("Exit 0: done ... Exit 1: refused ... Exit 2: bad
  usage"). I typed a nonsense verb and got `SPRINT REFUSED nova-sprint: unknown verb
  frobnicate: nova-sprint help`, exit 2 — the documented contract applied to the tool itself.
- The one refusal shape in `docs/CORRECT-USE.md` ("`SPRINT REFUSED <verb>: <what is not true>:
  <the command that makes it true>`") is the tool's actual behaviour, not documentation
  aspiration. `take` with no bus returned one line naming `nova-sprint check`; `finish` and
  `show` did the same, with no stack trace and no silent zero.
- `docs/PLAYBOOK-WORKER.md` "The first rule: run everything in your hand" and "Push, not pull"
  give an AI a complete operating model — every card in its own child, nudges, withdraw keeps
  the branch, a fix comes back as a new deal — without requiring a human to remember state.
- `docs/PLAYBOOK-COORDINATOR.md` "Keep everybody at width" plus `steps.Waits` in
  `docs/CORRECT-USE.md` turn "the fleet is idle" into named facts: the longest NEEDS chain, the
  top card blocking M others, the overlaps, and the same lines come from `check`, the board and
  the idle judgement.
- `docs/CARD-CONTRACT.md` "Admission" is unusually concrete for a card system: what `add`
  proves against origin and the roster, the retired v1 mechanics, and the single-argv
  `edge.ParseGate` rule. A brief that cannot be worked is refused before a row ever sees it.
- `docs/SETUP.md` §3 ("one API key per service, pasted once ... everything past that point is
  automatic") and §4 (the cold-AI dogfood gate, twice) state the right goal: setup is a verb,
  and an AI with no memory must be able to repeat it from the docs and help alone.
- The sprint commit identity is enforced where the damage happens: `hook`, `take` and `finish`
  print and check one identity (`docs/CORRECT-USE.md`, "Built: one commit identity"), so an AI
  cannot accidentally author a CAA-rejected head and discover it at landing.

## What gets it to 10

1. **Make `JOB.md` the card, not a cover sheet.** Where: `edge/worker/jobmd.go:21` already
   renders `## The card` from `j.Prose`/`j.Brief`, but the `JOB.md` staged for this job
   (`/home/nova/nova-bench/card-jobs/jobs/v2r-score-flash2.w1/JOB.md`) has only the id, kind,
   repository, paths and done-when; the task prose and the five-section rubric exist only in
   the prompt. Pass the full brief prose and rubric into the job and assert it is rendered.
   Done when a fresh AI given only that one `JOB.md` can perform the card (all five sections
   for a review) without the bus prompt, and `TestLiveJobMDAndPromptAgree` in
   `edge/worker/live_test.go:166` fails if the prose or section list is missing.

2. **Add `docs/QUICKSTART.md` and link it first.** Where: `docs/README.md` opens with "being
   rewritten from an empty tree" and layer/lock history, while `docs/SETUP.md` §4 says "this
   page becomes the walkthrough an AI runs" but remains a contract. Write about 40 lines of
   exact commands for the happy path (`check`, bus hook, `up`, identity, `stream add`, `add`,
   worker hook/next) with one copy-paste block per step, and point the top of `docs/README.md` at
   it. Done when the cold-setup dogfood gate in `docs/SETUP.md` §4 can be run using only
   `QUICKSTART.md` plus `nova-sprint help`, and its transcript reaches `UP DONE` and a
   `LANDED` line without opening any `LAYER-*.md`.

3. **Document and ship a throwaway store.** Where: `docs/SETUP.md` has no sandbox, and
   `cmd/nova-sprint/filestore_test.go` is test-only; a cold AI can therefore not safely see
   `take`, `finish` or `read` without a real fleet. Add a `nova-sprint up --file <dir>`
   (file/fake-bus backend), or a `make demo` target built on it, and document it in
   `docs/SETUP.md`. Done when `make demo` (or the documented command) creates a local store,
   runs bus hook, `stream add`, `add --dry-run`, `take`, `finish` and a read against it, exits
   0, and a test in `cmd/nova-sprint` runs the same sequence.

4. **Fix the help refusal's fix line.** Where: `verbs/help.go:24,38` call
   `misuse("help", ...)`, and `verbs/result.go:70` appends `nova-sprint help help`; both
   `nova-sprint help frobnicate` and `nova-sprint help take extra` currently end
   `: nova-sprint help help`. Make the fix `nova-sprint help` (with the valid names in the
   message) and add table cases in `verbs/core_test.go` or a new `verbs/help_test.go`. Done
   when both invocations print a usable fix and exit 2, and the test pins it.

5. **Make `show` cheap and orienting.** Where: `verbs/look.go:16-41` marshals the entire
   `store.Snapshot` when `show` has no name, and the help (`nova-sprint help show`) never says
   that; a card is raw JSON with no state/holder/next summary. Make no-name `show` print a
   short index (store counts, then `show all` for the full snapshot) and print a
   `CARD <id> state=... holder=... needs=...` line before the JSON, with `--json` for the raw
   form. Done when `nova-sprint show --as <row>` on a 5,000-card store prints under 20 lines,
   `show all` is the only full dump, and tests pin both.

6. **Gate review cards on their rubric.** Where: `verbs/brief_lint.go` and
   `docs/CARD-CONTRACT.md` "What a brief has"; this card's done-when checks only that the file
   is non-empty, the score line matches, and `## What gets it to 10` exists, while the actual
   rubric also requires `## What is good`, `## Where I would be lost cold` and
   `## Tokens and time` in order. Add a `SECTIONS:`/`RUBRIC:` key (or require one DONE-WHEN per
   section) that `add` compiles into gates. Done when a review brief missing
   `## Where I would be lost cold` is refused at `add`, and this card's done-when lists all
   five headers in order.

7. **Regenerate or delete the committed end-to-end transcript.** Where:
   `run/testdata/e2e-transcript.txt` is only 23 lines and ends
   `nova-sprint: schema creation failed: ... column "set_option" does not exist (SQLSTATE
   42703)`, because `cmd/nova-sprint/e2e_functional_test.go` saves the transcript even when the
   run fails. Save only after a full pass, regenerate on the documented PostgreSQL 18 bench,
   and refuse to commit a transcript containing `exit 1`, `SQLSTATE` or `REFUSED`. Done when
   the committed file ends with a `LANDED`/`UP DONE` line and the test still rewrites it only
   on success.

8. **Make the base unit gate deterministic.** Where: `policy/round5_test.go:26`
   `TestDeadlineOnTheClock` failed in `go test -count=1 -skip Functional ./...` on this
   machine (expected `-1`, got `0`) and then passed alone three times; it asserts a wall-clock
   budget while the full suite runs in parallel. Inject the policy clock/budget or skip the
   deadline case when the process is loaded. Done when `go test -count=1 -skip Functional
   ./...` is green 10/10 on a busy 2-core and the test still fails when the budget is actually
   ignored.

9. **Make the cold `next` honest.** Where: `verbs/next.go` `unready`/`guidance`; with no
   credentials `nova-sprint` and `nova-sprint next --as x` print `NEXT this process has no
   sprint credential` and exit 0, while `docs/LAYER-6-RUN.md` §4 shows the intro line followed
   by `NEXT the fleet is not up`. Add the intro and either check the fleet before the
   credential or return exit 2 when `next` cannot look. Done when an empty environment prints
   the `LAYER-6` header and `NEXT the fleet is not up` with exit 2, and a test pins it.

## Where I would be lost cold

1. **`JOB.md`, line 1 (`# v2r-score-flash2.w1`).** It looks like a card but contains no task
   prose and no rubric; `docs/PLAYBOOK-WORKER.md` says "its harness reads the card from
   `JOB.md` in the job directory." A cold AI follows that sentence, opens the file, and finds
   only an envelope with a done-when that can be satisfied by two sections out of five.

2. **`docs/README.md` first screen and `docs/SETUP.md` §4.** The cold-setup gate tells the AI
   to use "docs/README.md and the tool's own help", but README is a layer-by-layer design log
   and SETUP is a contract, not a command list. I would stall deciding whether the real path is
   README, SETUP, `docs/LAYER-6-RUN.md` §4's walkthrough, `help playbook coordinator`, or
   `help up`, and the transcript README calls the walkthrough is stale.

3. **The first acting verb without fleet credentials (`hook`, `take`, `show`, `judge`).** The
   actual first wall is the same 250-character `nova-bus who failed ... missing
   NOVA_BUS_REDIS ... check NOVA_BUS_REDIS, then nova-bus who: nova-sprint check` line on
   every verb, not the `this process has no sprint credential: run under nova-secrets exec ...`
   line `docs/PLAYBOOK-WORKER.md` promises. With `next` returning exit 0 for the same missing
   credential and no documented throwaway store, I cannot tell whether the sprint is unset up,
   the bus is down, or I am missing a credential, and every verb repeats the same wall.

## Tokens and time

- Reading the files under `docs/` to extract the 20 commands actually needed was the largest
  token cost: the directory is 8,860 lines across 19 files, and the two playbooks alone are
  729 lines. `docs/README.md` is a
  release-and-layer history, not a user guide, and nothing says which few of the nineteen files
  a worker needs. A single quickstart would remove most of this.
- There is no safe store to try the verbs on. SETUP shows no throwaway path, so I could only
  exercise `help`, `version`, `check` and the refusal paths; `take`, `finish`, `read` and
  `show <card>` remain untested by me. I did not run `up`, `run`, `worker` or `dash` because
  the docs forbid it outside a real fleet. Building the binary also cost a detour: `go` is not
  on `PATH` here and I found it at `/home/nova/sdk/go1.26.6/bin/go`.
- `nova-sprint check` on a cold machine printed a long, heavily redacted `nova-fleet` blob
  (`CHECK MISSING ansible-playbook: sudo apt-get update -qq && ... PATH=<redacted>`) before the
  useful line; the one action needed was buried in the final refusal. The redaction also
  removes ordinary paths, which makes the output harder to act on.
- The full unit run took about two minutes and failed on `TestDeadlineOnTheClock`; proving it
  was a load flake (three passes alone) cost another minute. Combined with the 91-second
  `card` package and 52-second `steps` package, the base suite is not cheap to use as a
  pre-flight gate on a busy machine.
- I avoided `nova-sprint show` with no name once I read `verbs/look.go`: on a real sprint it
  would dump the whole snapshot into the context. The fixes in items 1, 2, 5 and 7 above are
  the token savings I would most want as the AI holding the next card.
