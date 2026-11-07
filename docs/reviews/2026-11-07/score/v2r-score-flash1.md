# Tool score: nova-sprint v2, by v2r-score-flash1 (flash tier)

## Score: 8/10

The tool does the hard things right for a cold AI: every success prints the next command, every refusal is one line that names the fix, and a deal message carries the exact `take` command. What keeps it from a 10 is that the worker's entry path is spread across several documents and an environment the worker playbook never mentions, and that three verbs overlap for the finish step. I walked the help index, the worker playbook and the setup and correct-use pages; I did not run the live verbs against the store, so the refusal claims below come from the docs, not from observed output.

## What is good

- The `nova-sprint help` index tags every verb with who may run it (`[cold (no hook needed)]`, `[worker or coordinator]`, `[coordinator]`). A worker can see at once which of its ~40 verbs are its own.
- The refusal format is one fixed line, `SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>` (docs/PLAYBOOK-WORKER.md, "The refusals you will meet"; docs/CORRECT-USE.md, "The one rule"). Each refusal names a command to run, so an AI does not have to guess the fix.
- Every successful verb ends with `next: <command>` (docs/CORRECT-USE.md, "The one rule"). The chain is always one step ahead of the reader.
- The deal message is the whole instruction (docs/CORRECT-USE.md, "The deal message is the walkthrough"): `DEAL card=... kind=work ... run: nova-sprint take <id>@<gen> --as <you>`. A worker does not have to remember the next step.
- `finish` is checked, not trusted (docs/CORRECT-USE.md, "Finish is checked, not trusted"): a non-holder, a short sha or a head not pushed to the dealt branch is refused with the fix named.
- The stuck rule in docs/PLAYBOOK-WORKER.md ("Count, do not feel") gives a cold AI a concrete test for when to stop and ask, and the evidence list for the bus message.

## What gets it to 10

1. Make the worker playbook name the environment every verb needs. In docs/PLAYBOOK-WORKER.md, "Hook in" step 2 says to run verbs under `nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`, but the non-secret variables the verbs also need (`NOVA_PG_DSN`, `NOVA_BUS_REDIS`, `NOVA_BUS_USER`, `NOVA_SPRINT_AS`) appear only in docs/CORRECT-USE.md and docs/LAYER-1-STORE.md, and the local wrapper that sets them (`/Users/glenn/.local/bin/ns`) is not in the repo. Done when: a fresh shell with only docs/PLAYBOOK-WORKER.md can run `hook` and `take` without a wrapper, and the playbook lists each variable with its value form.
2. Remove the overlap between `finish`, `read` and `report`. `nova-sprint help report` says it is "nova-worker's form of finish and read", and the playbook uses `finish` and `read` for the same outcomes. Done when: `help report` says "for the bench runner only; a worker uses finish or read", and the playbook's "Work a card" and "Read a card" sections never mention `report` except for the fault case.
3. Cut the repeated `up:` line from each verb in `help <verb>`. Every verb table repeats "the store up makes at step 2 (an epoch), at any later step of up; without it: nova-sprint up --as <coordinator>". Done when that text appears once in the `help` index and `help take`, `help finish` and `help read` each fit in under 10 lines.
4. Put a short first-five-commands block at the top of docs/PLAYBOOK-WORKER.md. The playbook is 312 lines and a worker must read all of it before its first act. Done when the first 15 lines give the three hook commands in order (bus hook, sprint hook, next), then take, finish and read with their argument shape, and the rest of the page is reference.
5. Resolve the shell-call rule against the git steps. docs/PLAYBOOK-WORKER.md rule 1 says "one plain command per shell call: no `&&` chains", but "Work a card" step 2 and step 9 are git sequences that a worker runs constantly. Done when the playbook either lists the git steps as one command each, or allows `&&` for git only, and the rule text and the steps agree.
6. Give the coordinator a one-screen decision table for `up`, `start`, `run` and `stop`. The help index lists all four under different headings, and `up` says "nothing; it runs before up" in its own help. Done when `nova-sprint help` has a "which verb when" block of four lines, and `help up` no longer says "runs before up".
7. Define where a card's JOB.md lives, or drop the reference. docs/PLAYBOOK-WORKER.md says "its harness reads the card from JOB.md in the job directory", but the playbook never says where that directory is or who creates it. The brief I was dealt named JOB.md as the first file to read, and no JOB.md existed in the clone or the given paths. Done when the playbook says that `take`'s printed brief is the authority when no JOB.md exists, and `add` refuses a brief whose text names a JOB.md the harness will not supply.

## Where I would be lost cold

1. The hook step, docs/PLAYBOOK-WORKER.md "Hook in" step 1. It sends the reader to `nova-bus help hook` for the harness form. A cold AI with no Monitor tool has no fallback in the playbook, and the first `hook` refusal is the only prompt it gets.
2. The environment for every verb, docs/PLAYBOOK-WORKER.md "Hook in" step 2. The `nova-secrets exec` prefix is given, but the `NOVA_PG_DSN` and `NOVA_BUS_REDIS` values are not, so the first `nova-sprint hook` run fails for a reason the playbook does not name.
3. Which of `finish`, `read`, `report` and `release` to use when a card is done, failed, faulted or returned, `nova-sprint help report` (the work section of the help index). The playbook's refusals list uses all of them, and the difference between `--failed` and `report --result fault` is one sentence deep.

## Tokens and time

The largest cost was reading before the first act: docs/PLAYBOOK-WORKER.md (312 lines), docs/CORRECT-USE.md (287 lines, I read 120), docs/CARD-CONTRACT.md (181 lines, I read 60) and docs/README.md (223 lines, I read 40). The per-verb help repeats the same `up:` line on every verb, so a cold reader pays for it about forty times to learn it once. The help index (about 40 lines) was the cheapest orientation and the most useful single read. Time went to finding the environment: the working checkout had no JOB.md, and the wrapper that sets the environment lives outside the repo. Verbs were not run against the live store, as the review rules require, so the refusal wording above is what the docs promise, not what the tool printed.
