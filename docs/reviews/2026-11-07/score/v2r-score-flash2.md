# Tool score: nova-sprint v2 (cold read, row rowan, claude-haiku-5-5)

## Score: 7/10

This is a cold read, not a walk. I built the binary from the pinned checkout (43d849e2), read `help` for the verbs a worker and a coordinator touch (next, check, take, finish, read, show, release, run, hook, up, identity, add, judge, beat, report, queue), and read docs/PLAYBOOK-WORKER.md, docs/SETUP.md and the section headings of docs/CORRECT-USE.md and docs/PLAYBOOK-COORDINATOR.md. I did not stand up a store: docs/SETUP.md needs the nova-tools fleet (Tailscale, Redis, PostgreSQL) and I was not going to touch the live store. The verb tables are the best part of the tool: an AI can act from `help <verb>` alone, and every refusal names its fix. It loses points because the first working step depends on things the verb tables do not say (the secrets wrapper and the bus hook), because the worker playbook is long for a card-sized task, and because the job file the brief tells you to read (JOB.md) is not in the checkout or anywhere I could find on this bench.

## What is good

- The verb table format in `nova-sprint help <verb>`: each verb gives its usage, its role (`who: worker or coordinator`), and a `refuses when (and the fix it names)` list. For example `help finish` lists "the head is the base: do the work" and "not on the dealt branch at origin: git push origin <branch>", so an AI knows the fix before it hits it.
- `nova-sprint next` as the single chooser ("the one thing this name should do now, and its command; never refuses a missing prerequisite"). It answers "what do I do now" without a playbook read.
- `nova-sprint check` described as "the one doctor, one line each", with "any CHECK RED line exits non-zero". One command, one exit code.
- The push model in docs/PLAYBOOK-WORKER.md, section "Who you are now", item 5: "Push, not pull ... never poll, never sleep and check". A worker never has to remember to look.
- docs/CORRECT-USE.md, "The one rule" and "Finish is checked, not trusted": the tool enforces the process rather than trusting the worker to follow it.
- `nova-sprint hook` prints `COMMIT AS <name> <email>` with the two git config lines, so the identity problem is handled inside the hook output.

## What gets it to 10

1. Put the secrets wrapper in every verb's help. Today `help finish`, `help take` and the rest omit that every verb needs `nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`, which is stated only in docs/PLAYBOOK-WORKER.md step 2 of "Hook in". Change: the verb-help template in `cmd/nova-sprint` (the table printer, `playbooks.go` is the adjacent file). Done when `nova-sprint help finish` prints the exec line and a run without the credential refuses with the same line.
2. Make the bus address discoverable. The worker playbook says the coordinator gives the `NOVA_BUS_REDIS` address; `nova-sprint hook` only says "not on the bus: nova-bus hook ...". Change: `hook` refusal prints the address it expects from nova-config, or says "no bus address in nova-config: ask the coordinator" as its own line. Done when a fresh row with no address gets one line naming where the address comes from.
3. Name JOB.md in the code path that needs it. The card brief says "read JOB.md first" and the playbook says the harness reads the card from JOB.md, but neither says where it lives or who writes it for a friend row. Change: `take` prints the JOB.md path (or the brief text) alongside the brief, and docs/PLAYBOOK-WORKER.md "Who you are now" says where the file sits. Done when a friend row with only `take` output can find the card text without asking.
4. Shorten the worker playbook to a card. docs/PLAYBOOK-WORKER.md is 23010 bytes and the coordinator's is 30281; a worker needs the hook steps, the first rule, the finish and read commands, and a link to the rest. Change: split the worker playbook into a first-screen page of about 6 KB and move the rest to docs/CORRECT-USE.md. Done when `wc -c` on the first-screen page is under 8000 bytes and a cold worker finds the hook steps in it.
5. Add `--dry-run` to `finish` and `report`. Change: the verbs in `cmd/nova-sprint` and their help. A worker can then check that its head is on the dealt branch and that the author identity is right before the finish is accepted or refused. Done when `nova-sprint finish <card>@<gen> --head <sha> --as <row> --dry-run` prints each refusal check and writes nothing.
6. Say what to do after a commit-identity refusal. `help finish` says "rewrite those commits" for a commit by another author, with no command. Change: the refusal names the rewrite command (for example `git rebase --exec 'git commit --amend --reset-author --no-edit'`), or the playbook has one line for it. Done when the refusal line contains a runnable command.
7. Show the judge choices on the verb's help, not only on refusal. `help judge` shows the form but not the choices; an AI has to trigger a refusal to see them. Change: `judge` with no arguments lists the choices per open judgement, which the verb already does, and `help judge` gives the group form. Done when `help judge` names one example choice.
8. Run the cold-AI setup gate from docs/SETUP.md section 4 on a fresh fleet and record it. The gate is already written; it has not been run by a cold AI from the docs. Change: the run's transcript goes under docs/. Done when the record shows a cold run from docs/README.md and the tool's help alone, reaching `nova-sprint check` green.

## Where I would be lost cold

1. The hook step in docs/PLAYBOOK-WORKER.md, "Hook in: the bus first, then the sprint". The first command is `NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you>` run under a Monitor tool, and the address comes from the coordinator. Nothing in `help hook` names it, and the refusal text does not say where the Monitor tool lives in my harness.
2. Every verb after that needs the secrets wrapper. `nova-sprint hook --as <you> --role worker` does not say it needs the wrapper until the refusal "this process has no sprint credential". The first attempt on any verb fails here.
3. The job file. The brief says to read JOB.md first, and I could not find one in the job directory, the checkout or the scratchpad, so I cannot tell whether a friend row is expected to write one or receive one. The brief for this card assumes it exists.

## Tokens and time

- The most expensive part was the two playbooks (23 KB worker, 30 KB coordinator) and CORRECT-USE (18 KB). A worker reads about 76 KB of docs before the first useful command, and the worker playbook alone is most of it.
- Building the binary and reading each verb's help cost little (a few seconds per verb). The verb tables were fast to scan.
- The fleet setup in docs/SETUP.md (nova-tools first, then the dashboard, then the API-rate seats) is long and needs a fleet this card cannot provide, so the live walk was out of reach. The score is a read, not a run.
