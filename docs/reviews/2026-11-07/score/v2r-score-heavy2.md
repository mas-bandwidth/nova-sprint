# Is nova-sprint a good tool for AIs to use? (v2r-score-heavy2)

Reviewer: Rowan (row rowan), Claude Opus 5.5, heavy tier, 2026-11-07. Walked at
43d849e2 (v2-stitch): `go build ./... && go vet ./...` clean; `help` and `help <verb>`
for every verb; PLAYBOOK-WORKER, PLAYBOOK-COORDINATOR (headings and sections 1, 2, 11),
SETUP, CORRECT-USE, CARD-CONTRACT, docs/README.md; the verbs tried against a throwaway
PostgreSQL 18 (`initdb` in a scratch directory, port 55439) and a throwaway Redis
(port 56391), never the live store; this card itself was worked as a friend row and
finished through the installed `ns` wrapper (live binary dev+ddc9a4e1).

## Score: 6/10

The core loop is right and better than anything I have used for this: one refusal form
(`SPRINT REFUSED <verb>: <what is not true>: <fix>`), every output ending in a `next:`
line, `next --as <row>` naming the exact finish command with `@gen` and `--epoch`, and
`finish` checking the head instead of trusting it. It loses four points on the cold path
around that loop, not on the loop. I was dealt this card as a friend row, and the brief
told me to "read JOB.md first" and "end as JOB.md says"; no JOB.md exists for a friend
row (edge/worker/jobmd.go writes it only for a bench job), so the brief was not enough to
do the task cold and I ran on the coordinator's side recipe instead. I could not run a
single verb without a 676-character wrapper script (`~/.local/bin/ns`) that feeds
nova-secrets its store, seat, key and sops paths. I could not try the tool safely: SETUP.md
is a contract ("layer 6 makes it exact and this page becomes the walkthrough"), not a
walkthrough, and has no throwaway-store recipe; when I built one by hand, the refusals I
hit were in the wrong order or named no runnable fix. And a hook costs about 6k tokens of
playbook (13k for a coordinator) on every wrap-around.

## What is good

- **The refusal form.** Every refusal I hit was one line in the same shape, e.g.
  `SPRINT REFUSED hook: cold is not on the bus: hook in to the bus first:
  NOVA_BUS_REDIS=<host:port> nova-bus hook --as cold under your session's Monitor tool`.
  I could act on it with no human.
- **`next` is the right front door.** With no store schema it said `NEXT the sprint is not
  set up` / `next: nova-sprint up --as cold`; on the live store it said
  `NEXT work r3-l3-threaded-islands.w1 is in hand with rowan (23 more: nova-sprint next
  --as rowan --json lists them)` and `next: nova-sprint finish
  r3-l3-threaded-islands.w1@1 --head <sha> --as rowan --epoch 1`, a command I can paste.
  `next --json` returns the same as a typed record (`message`, `next`, `ready`,
  `working`, `judgements`).
- **`check` prints every line, red ones with their fix**, e.g. `CHECK RED the store is not
  at this build's schema: nova-sprint up --as <coordinator>`, and exits non-zero. The
  CORRECT-USE "Built (stitch)" note that it once printed four green lines while 24 of 25
  streams could not clone shows the lesson was learned.
- **`help <verb>` has a fixed shape**: usage, one sentence, `who:`, `up:` (what must
  exist), and `refuses when (and the fix it names)`. The top-level `help` states the exit
  code contract in its second line.
- **PLAYBOOK-WORKER "The refusals you will meet"** lists each refusal with the action that
  answers it, including the identity rewrite (`git rebase <base> --exec 'git commit
  --amend --no-edit --reset-author'`). That section alone would have saved me a stall.
- **`finish` is checked, not trusted** (CORRECT-USE "Finish is checked, not trusted"):
  full sha, pushed to the dealt branch at origin, holder, and commits only by the sprint
  identity, each commit named when refused.
- **`take` is idempotent and `release` keeps the branch**: a repeat prints the same and
  writes nothing; a withdraw or release never loses pushed work (PLAYBOOK-WORKER step 2
  of "Work a card": continue from `origin/<branch>`, never restart from the base).
- **Admission at `add`** (CARD-CONTRACT "Admission"): a DONE-WHEN that is a shell line is
  refused with the `sh -c '...'` fix, so a bad gate is found by the coordinator, not by
  the worker.

## What gets it to 10

1. **One finishing contract for every row kind.** Today a brief written for a bench says
   "read JOB.md first" / "End as JOB.md says", JOB.md exists only for bench jobs
   (edge/worker/jobmd.go `finishRules_internal`), and the two contracts contradict: JOB.md
   says "Run no git push ... the worker pushes the branch", PLAYBOOK-WORKER "Work a card"
   step 7 says push your branch. Change: `take` for a friend row prints a `## How this
   card finishes` block generated from the same function as JOB.md's (with push and
   `finish` in place of result.json), and `add` (verbs/brief_lint.go) refuses a brief that
   names `JOB.md`. Done when: `take` of a work card on a friend row prints that block, a
   brief containing "JOB.md" is refused at `add` with the fix "describe the finish, the
   tool prints it", and a test asserts both.
2. **Run a verb without a wrapper.** Every verb needs `NOVA_SPRINT_PG_PASSWORD` and
   `NOVA_BUS_PASSWORD` in its process; the refusal says `run under nova-secrets exec --only
   NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`, but on the studio that
   line alone does not work: `ns` also passes `--store`, `--as`, `--key`, `--sops`, the
   `NOVA_SECRETS_*` variables and `NOVA_PG_DSN`. Change: when the credential is absent,
   nova-sprint resolves the seat from nova-config and execs itself under nova-secrets (or
   the refusal prints the full, machine-specific command). Done when: in a fresh login
   shell on a fleet machine, `nova-sprint next --as rowan` prints a `NEXT` line with no
   wrapper and no exported variable.
3. **A throwaway sprint in SETUP.md.** The card asked me to try the safe verbs "against a
   throwaway store if docs/SETUP.md shows how"; it does not. By hand I hit: the store role
   is the fixed name `nova_sprint`, not the DSN's user; a macOS scratch path over 103 bytes
   breaks PostgreSQL's socket; `up` refuses with credentials set and cannot be run without
   touching the fleet. Change: a `nova-sprint up --local <dir>` (or `make sandbox`) that
   starts PostgreSQL and Redis in `<dir>` (socket in a short temp path), migrates an epoch,
   adds a stream on a bare repository, and prints the env lines to use; SETUP.md section 0
   shows it. Done when: a cold AI runs `hook`, `take`, `finish` and `read` on a card in the
   sandbox in under ten commands copied from SETUP.md, and a functional test does the same.
4. **Fix the store refusal for a missing role.** Today: `CHECK RED PostgreSQL cannot be
   reached or NOVA_PG_DSN is invalid (... FATAL: role "nova_sprint" does not exist
   (SQLSTATE 28000)): check NOVA_PG_DSN and run nova-fleet check`; the DSN was fine.
   Change: SQLSTATE 28000 on role `nova_sprint` maps to `the sprint's database role
   nova_sprint does not exist: nova-sprint up --as <coordinator>`. Also align
   LAYER-1-STORE.md ("`NOVA_PG_PASSWORD`") with the verbs (`NOVA_SPRINT_PG_PASSWORD`).
   Done when: a functional test against a fresh `initdb` with no such role gets that line,
   and `grep -n NOVA_PG_PASSWORD docs/` returns only the nova-config context.
5. **`up`'s first refusal must name a command.** Today, with the credentials set: `SPRINT
   REFUSED up: invoke the setup wrapper without database credentials
   (NOVA_PG_ADMIN_PASSWORD and NOVA_SPRINT_PG_PASSWORD must be unset; ...)`; no fix
   command, and "the setup wrapper" is named nowhere in help or SETUP.md. Change: end it
   with the exact command (`env -u NOVA_SPRINT_PG_PASSWORD -u NOVA_PG_ADMIN_PASSWORD
   nova-sprint up --as <you>`, or the wrapper's real name). Done when: every refusal string
   in verbs/ ends in `: <command>`, enforced by a test over the refusal table (the "not a
   full sha" line in `help finish` fails it today too).
6. **Prerequisites in the documented order.** CORRECT-USE "What `next` looks at" puts the
   fleet first, then the schema, then the hook. On a store with no schema, `hook`, `take`,
   `finish`, `judge` and `queue` all refused with `cold is not on the bus` instead of `the
   sprint is not set up: nova-sprint up`; with no credential, `next` names nova-secrets
   before the fleet. Change: one prerequisite chain function used by every verb, in the
   CORRECT-USE order. Done when: a table test runs each verb on (no credential, no schema,
   no bus hook, no sprint hook) and asserts the first missing link is the one refused.
7. **Looking verbs need only the store.** PLAYBOOK-WORKER rule 4 says `next` and `show`
   "whenever you like", but `show run` and `queue` refuse until you are on the bus, and
   `show run` is how the playbook says to find the coordinator's name. Change: `show`,
   `queue`, `policy show|diff|check` need the store only. Done when: `show run --as cold`
   on a set-up throwaway store with no bus hook prints the record, in a functional test.
8. **`show <card>` for its holder ends with the card's own command.** `ns show
   v2r-score-heavy2.w3` (my card, holder rowan) printed `next: nova-sprint next --as
   rowan`, and `next` names only the first of 24 cards in hand, so the coordinator's recipe
   greps `"gen"` out of the JSON to build the finish line. Change: `show <consumer>` ends
   with `next: nova-sprint finish <id>@<gen> --head <sha> --as <holder> --epoch <e>` (or the
   read line) when the asker is the holder. Done when: that line appears in a test and the
   recipe's `grep -o '"gen"...'` is no longer needed.
9. **Help and usage polish.** Every `help <verb>` ends `next: nova-sprint <verb> ...`, which
   says nothing; `version` ends `next: nova-sprint help`; `finish -h` refuses (`unknown
   argument -h`) instead of printing the table; `help bogus` says `nova-sprint help help`
   instead of listing the verbs; usage errors print `SPRINT REFUSED` with exit 2 although
   the header says refused is exit 1. Change: `-h`/`--help` after any verb prints its
   table; help's `next:` names the most likely next verb (help finish: `nova-sprint next
   --as <you>`); usage errors print `SPRINT USAGE <verb>: ...`. Done when: a test runs
   every verb with `-h` (exit 0, table printed) and asserts no `SPRINT REFUSED` line exits 2.
10. **Docs generated from, or tested against, the code's strings.** CORRECT-USE "The deal
    message is the walkthrough" shows `DEAL card=<id> attempt=<n> kind=work|read stream=<s>
    epoch=<e>` / `run: nova-sprint take <id>@<gen> --as <you>` and take's next as `nova-sprint
    finish <id> --head <sha> --as <you>` (no `@gen`, no `--epoch`); PLAYBOOK-WORKER shows a
    different DEAL line with `brief=<clipped>; in your hand now: ...`. Change: a doc test
    (beside playbooks.go) that renders each push and receipt from the code and checks every
    fenced example in PLAYBOOK-WORKER and CORRECT-USE matches. Done when: changing a format
    string without the doc fails `go test ./...`.
11. **A short hook.** `hook` prints the whole worker playbook, 23,044 bytes (`help playbook
    coordinator` is 53,326), and the playbook says to re-hook after every wrap-around.
    Change: `hook` prints the receipt, the commit identity and a one-screen card (the first
    rule, the five verbs with their exact lines, the ten rules) under 4 KB, ending `full
    playbook: nova-sprint help playbook worker`. Done when: `hook` output is under 4,096
    bytes in a test and the playbook stays one command away.
12. **docs/README.md opens with a "start here" for each role.** Today its first screen is
    the promises and then layer lock history ("re-locked 2026-11-06 at 1894ced3 ...").
    Change: the first 20 lines say "a worker: run `nova-sprint next --as <you>`, then read
    PLAYBOOK-WORKER; a coordinator: `nova-sprint check`, PLAYBOOK-COORDINATOR; setting up:
    SETUP.md section 0", and move lock history to the layer docs. Done when: those lines
    are the first heading's body.

## Where I would be lost cold

1. **The brief's "read JOB.md first" (the card prose; edge/worker/jobmd.go).** As a friend
   row there is no JOB.md, so "cd into the staged checkout JOB.md names" and "End as
   JOB.md says" point at nothing. Without the coordinator's separate recipe I would not
   know whether to push, whether to write result.json, or what commit message to use.
2. **Running any verb at all (`next`, PLAYBOOK-WORKER "Hook in" step 2).** The credential
   refusal names `nova-secrets exec --only ...`, but nova-secrets on this machine needs a
   store, seat, key and sops path that no nova-sprint doc names; the working answer is a
   private wrapper script outside the repository.
3. **Trying it safely (docs/SETUP.md, `up`).** SETUP.md has no throwaway path, `up` reaches
   into the fleet and refuses under credentials with "the setup wrapper", and on a
   hand-built store the first refusals were misleading (`check NOVA_PG_DSN` for a missing
   role; `not on the bus` for a store with no schema).

## Tokens and time

- **Reading to start**: the five docs the card names are 88 KB (PLAYBOOK-COORDINATOR 30 KB,
  PLAYBOOK-WORKER 23 KB, CORRECT-USE 18 KB, CARD-CONTRACT 11 KB, SETUP 5 KB), about 22k
  tokens, and docs/ is 8,860 lines in all. The same rules appear three times (the
  playbook, CORRECT-USE, LAYER-6-RUN) in slightly different words, so I read each twice to
  check which was current, and found two that disagree (item 10).
- **Help lines are long single sentences**: `help check`'s description is one 630-character
  line; `help` is 6.4 KB. Scannable tables cost fewer re-reads.
- **The throwaway store** cost the most time: three attempts (socket path length, the fixed
  role name, `up` unusable) before a verb answered; none of it is in SETUP.md.
- **Finding my own card's gen** cost a `show` of the full JSON record (the brief repeated
  twice, as `brief` and `prose`; 7.5 KB) to read one integer.
- **What cost nothing**: once in the loop, `next`, the refusals and `finish` needed no
  thought; that part is already a 10.
