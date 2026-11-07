## Score: 8/10

The engine is the best multi-agent work system I have used: the prerequisite chain, the
push-not-pull deal, the one refusal shape and the checked finish are all real behaviour, not
documentation, and `help playbook worker|coordinator` hands a cold AI the operating model from
inside the binary. It is not 10 because the cold path is unfinished: there is no quickstart or
documented throwaway store, the `JOB.md` I was told to read is a 15-line envelope rather than
the card, `check` buries its first action in a redacted wall, `help` names a bad fix, and the
committed end-to-end transcript is a failed run. I could run a sprint with this today; I would
not hand it to an AI that had never seen it and expect the first hour to be cheap.

## What is good

- `nova-sprint help` (`verbs/help.go:41`) is a real map, not a usage dump: verbs grouped by
  start/work/coordinate/operate, each marked with its role, and the exit contract stated first
  ("Exit 0: done ... Exit 1: refused ... Exit 2: bad usage"). `help <verb>` adds usage, who may
  run it, what it needs from `up`, and the refusal/fix pairs.
- The one refusal shape in `docs/CORRECT-USE.md` is the actual output. `take x@1 --as warm`
  printed `SPRINT REFUSED take: nova-bus who failed (...): nova-sprint check`, exit 2, and
  `show` with no name printed `SPRINT REFUSED show: no name given: pass --as <you>, or set
  NOVA_SPRINT_AS=<you>`, exit 1. No stack trace, no silent zero.
- `docs/CORRECT-USE.md` "The chain" is a table of ten steps, each with the verb, what it
  refuses until, and what the refusal names. The code enforces the table twice (verb table and
  card machine), which is why the refusals above are terse and actionable.
- `docs/PLAYBOOK-WORKER.md` "The first rule: run everything in your hand" and "Push, not pull"
  are the right model for an AI: cards live outside context, every message carries its command,
  and `take` prints the brief, branch, done-when and next command. `help playbook worker` served
  the whole page from `docs/playbooks.go`, so I did not have to find a file path.
- `docs/CARD-CONTRACT.md` "Admission" is unusually concrete: `verbs/brief.go` requires tier,
  PATHS, TEST, DONE-WHEN and task prose; `edge.ParseGate` is the one argv parser; retired
  mechanics (`JOB.md`, `WHO:`, locks, `ssh to <host>`) are named and refused at `add`.
- The sprint commit identity is enforced where it matters: `hook`, `take`, `finish` and `check`
  all print or check the one name/email, so an AI cannot accidentally author a head that the
  repository's contributor agreement will reject at landing.
- `docs/PLAYBOOK-COORDINATOR.md` "Keep everybody at width" and `steps.Waits` make "the fleet is
  idle" a set of named facts (longest NEEDS chain, top blocking card, overlaps) that `check`,
  the board and the idle judgement all read from the same function.

## What gets it to 10

1. **Ship a quickstart and link it from the first line of `docs/README.md`.** Where:
   `docs/README.md:16-18` opens with "being rewritten from an empty tree, one layer at a time"
   and a layer log; `docs/SETUP.md:3-4` says "this page becomes the walkthrough" in the future
   tense. Add `docs/QUICKSTART.md` (about 40-60 lines) with one copy-paste block per step:
   `nova-fleet check`, bus hook, `nova-sprint up --as <coordinator>`, identity, `stream add`,
   `add --dry-run`, worker hook, `next`, `take`, `finish`, read. Done when the cold-setup gate
   in `docs/SETUP.md` section 4 can be run using only `QUICKSTART.md` plus `nova-sprint help`,
   reaches `UP DONE` and a `LANDED` line without opening any `LAYER-*.md`, and a docs test fails
   if the README no longer links it first.

2. **Document and expose a throwaway store and bus.** Where: the only local seam is
   `run/filestore`, described in `docs/LAYER-6-RUN.md:589` as a test store, and it is reachable
   only from tests such as `cmd/nova-sprint/filestore_test.go`; no verb, help page or Makefile
   target exposes it. Add `nova-sprint up --file <dir>` (fake store and fake bus) or a
   `make demo` target built on it, and document it in `docs/SETUP.md` and `QUICKSTART.md`. Done
   when one documented command creates a local store, runs bus hook, `stream add`, `add`,
   `take`, `finish` and a read against it, exits 0, and a `cmd/nova-sprint` test runs the same
   sequence without touching the live store.

3. **Fix the fix line that `help` prints for bad usage.** Where: `verbs/help.go:38` calls
   `misuse("help", ...)`, and `verbs/result.go:70` appends `nova-sprint help help`; I got
   `SPRINT REFUSED help: no verb frobnicate: nova-sprint help help` and the same for
   `help take extra`, exit 2. Make `misuse` accept the valid next step, and have `help` return
   `nova-sprint help` with the valid verb names. Done when both invocations print a usable
   command and exit 2, pinned by a new `verbs/help_test.go` table.

4. **Stop committing a failed end-to-end transcript.** Where: `run/testdata/e2e-transcript.txt`
   is 23 lines and ends `exit 1` with `column "set_option" does not exist (SQLSTATE 42703)`;
   `cmd/nova-sprint/e2e_fixtures_functional_test.go:58-70` writes it directly, and
   `cmd/nova-sprint/e2e_functional_test.go:54` calls `save` from a `defer`, so a failed run
   overwrites the good one. Save to a temporary path and rename only after the full pass, and
   refuse to commit a transcript containing `exit 1`, `SQLSTATE` or `REFUSED`. Done when the
   committed transcript ends with the success note and `LANDED`/`UP DONE`, and a failed test no
   longer rewrites it.

5. **Make the delivered `JOB.md` the card, not a cover sheet.** Where: the `JOB.md` I was given
   (`/home/nova/nova-bench/card-jobs/jobs/v2r-score-flash2.w2/JOB.md`) has the id, kind,
   repository, paths and done-when, but no task prose and none of the five required review
   sections; `edge/worker/jobmd.go:40-42` already renders `## The card` from `j.Prose`/`j.Brief`
   when it is present, so the staging path is dropping it or this envelope bypasses that writer.
   Pass the full brief prose and the section list into the job and assert they were rendered.
   Done when an AI given only that one `JOB.md` can perform the card cold, including all five
   review sections, and `TestLiveJobMDAndPromptAgree` in `edge/worker/live_test.go` fails if the
   prose or any required section is missing.

6. **Gate review cards on their rubric, not on two greps.** Where: `verbs/brief.go` requires
   tier, PATHS, TEST, DONE-WHEN and prose, and `docs/CARD-CONTRACT.md` has no `RUBRIC:` or
   `SECTIONS:` key; this card's done-when checks only that the file is non-empty, that the score
   line matches, and that `## What gets it to 10` exists, although the rubric also requires
   `## What is good`, `## Where I would be lost cold` and `## Tokens and time` in order. Add a
   `SECTIONS:`/`RUBRIC:` metadata key that `add` compiles into gates (or require one DONE-WHEN
   per required header) and refuse a review brief missing a section by name. Done when a review
   brief without `## Where I would be lost cold` is refused at `add --dry-run`, and this card's
   own done-when lists all five headers in order.

7. **Make no-name `show` cheap, orienting and documented.** Where: `verbs/look.go:30-31`
   marshals the entire `store.Snapshot` when `show` has no name, `help show` never says so, and
   even a single record requires `--as` and a live bus before anything is printed. Make no-name
   `show` print counts and a short index (with the command for a record or the full dump), make
   `show all --json` the explicit snapshot, and print one
   `CARD <id> state=... holder=... needs=...` line before a card's JSON. Done when
   `show --as <row>` on a 5,000-card store prints under 20 lines and tests pin index, record and
   full-dump forms.

8. **Make `check`'s first failure actionable without the redacted wall.** Where: on this cold
   host `nova-sprint check` printed `CHECK RED nova-fleet check is not green: ... missing=<redacted>
   ... PATH=<redacted> ...` before the useful `CHECK MISSING ansible-playbook`; the redaction
   hid ordinary paths while the install command was already in nova-fleet's output. Have
   `check` print one line per missing prerequisite with its exact install or set command, and
   redact only secrets, not program paths or environment names. Done when a cold host's check
   prints "install Ansible" and "set TAILSCALE_API_KEY" each on its own line with a fix, and a
   golden test pins the shape.

9. **Make cold `next` follow its documented order and refuse when it cannot look.** Where:
   `docs/CORRECT-USE.md` "What `next` looks at, in order" starts with the fleet, then the store;
   in this empty environment `nova-sprint` and `nova-sprint next --as warm` both printed
   `NEXT this process has no sprint credential` and exited 0, although neither fleet nor bus
   was reachable. Either check the fleet before the credential or return exit 2 with the
   credential/fleet command, as `CORRECT-USE.md` says an exit 2 means "could not run". Done when
   an empty environment prints the documented intro and `NEXT the fleet is not up` (or the
   credential refusal) with exit 2, pinned by a test.

## Where I would be lost cold

1. **`JOB.md`, line 1, in the job directory.** I was told to read it first, and it looks like
   the card, but it is only an envelope: no task prose, no rubric, and its done-when can be
   satisfied by two sections out of five. A cold AI will do exactly the two greps and report
   success; the real instructions exist only in the bus prompt it may not have.
2. **`docs/README.md` first screen and `docs/SETUP.md` section 4.** README opens with a
   layer-by-layer rewrite log, and SETUP says the page "becomes the walkthrough" rather than
   being one. I stalled deciding whether the real path was README, SETUP, `LAYER-6-RUN.md`'s
   step table, `help playbook coordinator`, or `help up`; there is no single first command.
3. **The first acting verb with no fleet (`take`, `show --as`, `finish`, `judge`).** Every one
   hits the same `nova-bus who failed ... missing NOVA_BUS_REDIS ...: nova-sprint check` wall,
   while `next` says only "this process has no sprint credential" and exits 0. With no
   documented throwaway store I cannot tell whether the store is not set up, the bus is down,
   or my process is missing a credential, and every probe repeats the same 250-character line.

## Tokens and time

- Extracting the roughly 20 commands actually needed from `docs/` was the largest cost: 19
  files, 8,860 lines, with the two playbooks alone at 729 lines. `docs/README.md` is a release
  and layer history, not a user guide; nothing says which few of the nineteen files a worker or
  coordinator needs. A single `QUICKSTART.md` would remove most of this.
- There is no safe store to try the acting verbs on. I exercised `help` for every verb,
  `version`, `check`, `next`, `show`, `queue`, `take` and `finish` only on their refusal paths;
  `take`, `finish`, `read` and `show <card>` were never exercised against real state. I did not
  run `up`, `run`, `worker` or `dash`, because the docs forbid them outside a real fleet and
  SETUP documents no sandbox.
- `go` is not on `PATH` in this job; I found it at `/home/nova/sdk/go1.26.6/bin/go`, and the
  first build downloaded the module graph. A cold AI without that hint would spend working
  memory locating a toolchain before it could even run `help`.
- `nova-sprint check` printed a long, heavily redacted `nova-fleet` blob before the one action
  that mattered (install Ansible, set the Tailscale key), and every refused verb repeated the
  same bus line. I avoided no-name `show` after reading `verbs/look.go`, since on a real sprint
  it would put the whole snapshot into context; items 1, 2, 7 and 8 are the token savings I
  would most want as the AI holding the next card.
