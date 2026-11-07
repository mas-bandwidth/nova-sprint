# Is nova-sprint a good tool for AIs to use? Rowan (fable), 2026-11-07, at 43d849e2

Walked as the AI holding card `v2r-score-frontier2.w3` in the live dogfood sprint: `help` and
`help <verb>` for all 40 verbs from a build of this commit; the four docs the card names plus
docs/README.md, CARD-CONTRACT.md and LAYER-6-RUN.md; the verbs cold with no store and no bus;
the read-only verbs against the live store (`next`, `show`, `queue`, `judge`, `check`,
`take` as a repeat, a `finish` with a short sha, which refuses and writes nothing); the
dashboard's `/healthz` and `/api/sprint`; JOB.md's generator and the deal and take text in the
code; `go build ./...`, `go vet ./...`, and the unit tests of `verbs`, `steps` and `card`
(all ok). The live sprint runs an older build (`dev+ddc9a4e1`, pin v2.0.2), so where the live
output and this commit's code differ I say which I am judging. This is attempt 3 on the card's
branch; attempts 1 and 2 (hulk, `9e3b7941` and `9fb11074`) stay in its history, `take` carried
no finding for them, and the one defect of theirs I reproduced is item 16.

## Score: 7/10

The chain holds and the refusals are honest: a bare `nova-sprint` answers with the next
command, every refusal I met was one line with a fix, `finish --head abc` told me exactly which
`git ls-remote` prints the sha it wants, and the worker playbook is complete enough that a cold
session with only the deal message could work a card. What keeps it at 7 is that the tool's own
words and its state disagree in the places a cold AI first touches: the gen `show` prints is one
a `take` refuses, the brief I was dealt tells me to read a JOB.md that only a bench worker ever
gets (and the lint that should have refused that brief did not), `show <stream>` and
`show <row>` print bare records while the playbook promises producers by state and a row's
width and holdings, a build one schema hash ahead of the store calls a live, running sprint
"not set up" and names `up` as the fix, and ten open judgements all offer the single choice
`retry`, which is a notification wearing a judgement's clothes. None of these is hard; each is a
card.

## What is good

- `nova-sprint` with no arguments runs `next` (CORRECT-USE "The one rule" says it, `verbs/next.go` does it): with no credential it printed `NEXT this process has no sprint credential` and the `nova-secrets exec` line, exit 0, not usage.
- `finish`'s refusal for a short sha, live: `SPRINT REFUSED finish: head abc is not a full commit sha: pass the 40-hex sha git ls-remote origin sprint/v2-review/v2r-score-frontier2 prints`. That is the standard every refusal should meet: what is false, and the exact command that prints the true value, with my branch filled in.
- `show nosuch` refuses by name (`no card, stream or row nosuch`), never an empty answer; LESSONS 2.26 is kept.
- `check` is honest about what it cannot see: cold on this machine it printed `CHECK fleet green`, `CHECK nova-tools v2.0.3 (pin v2.0.3)`, then two `CHECK RED` lines each with its fix, then `SPRINT REFUSED check: 2 lines are red; the first: ...`, exit 2. On the live store with this build it said `the store is not at this build's schema` rather than guessing.
- `help` is grouped by role (start, work, coordinate, operate) and every verb's table has `who:` and `up:` lines, so a worker can see at a glance that `drop` is not theirs. `help take` says the one thing a worker most needs: "a repeat prints the same and writes nothing".
- The deal message (`run/notices.go` `dealNotice`) is one line that carries repo, base, branch, prior head, the reason, the brief clipped at 300 bytes with `… full text: nova-sprint take <id>@<gen>`, and the take and finish commands with `--epoch` filled in. Nothing to remember.
- JOB.md (`edge/worker/jobmd.go`) and the prompt carry the same finish rules word for word (`finishRules_internal`), with the three `result.json` forms spelled out; a bench harness cannot disagree with its own file.
- PLAYBOOK-WORKER.md "The refusals you will meet" lists each refusal with the action it wants, and "When you are stuck" tells me to count rather than feel, and what evidence a "stuck" message carries.
- `queue` is one line per producer with state, priority, tier, needs and holders: the cheapest view of a sprint I have seen, about 150 bytes a card.
- The DONE-WHEN parser is shared between admission and the bench (`edge.ParseGate`, CARD-CONTRACT "Admission"), so a gate that is admitted is a gate that runs; and the fix line for shell syntax says to wrap it in `sh -c '...'`, which my own card's gate does.

## What gets it to 10

1. **`take` accepts the gen that `show` and `next --json` print.** Live: my card shows `"gen": 1`, `take v2r-score-frontier2.w3@1` refused with `consumer ... is in working, Take needs ready: nova-sprint show v2r-score-frontier2.w3`, and `take ...@0` (the deal's gen) printed the receipt. The refusal's fix sends me to `show`, which prints gen 1 again: a loop. In `verbs/take.go` `heldBy`, a card held by this row in `working` is a repeat whatever gen is passed (or `show` prints the dealt gen as `take_gen`), and the refusal for a wrong gen names the gen that works. Done when a test in `verbs/core_test.go` runs `take` at the gen `show` printed after the first take and gets the same receipt, and no refusal text contains a card-machine state name ("Take needs ready") a worker was never told about.

2. **A friend row gets a JOB.md, or the brief form stops naming one.** My brief says "Start in the job directory; read JOB.md first" and "End as JOB.md says"; `edge/worker/jobmd.go` writes JOB.md only for a bench job, and a kin's `take` prints the brief with no checkout, no finish rules, no result forms. Either `take --json` grows a `job_md` field that the playbook tells a kin to write into its clone (same text as the bench's), or the coordinator's card template drops the JOB.md sentences. Done when `take` for a kin prints, after the brief, the same "How this card finishes" block the bench's JOB.md has, with the kin's finish command in place of `result.json`; and `verbs/brief_lint.go`'s `JOB.md` refusal is proved on the `add` path this brief came in by (the live brief contains "JOB.md" four times and was admitted: either the lint is newer than the card or that path skips it; `admit_test.go` covers the parser, not the path).

3. **`show <stream>` and `show <row>` print what the coordinator playbook says they print.** PLAYBOOK-COORDINATOR section 1 step 6 and 7: a stream's "producers by state, its landing rung, what is stuck and why"; a row's "its width and the cards it holds". `verbs/look.go` `record` returns the bare `store.Stream` (seven fields) and `store.Row` (eight, no width, no holdings). Done when `show <stream>` adds `counts` by state, the landing rung and the `check` waits line for that stream, `show <row>` adds `width`, `tiers` and `holds` (consumer ids with their step command), and the playbook's two sentences are tested against the output the way the playbook's commands are tested against the verbs.

4. **A judgement with one choice is not pushed as NEEDS YOU.** Live `judge` lists ten open judgements (`stall` x4, `land-rate` x4, `idle`, `policy`), every one `choices=retry`; `steps/judge.go` `choices` gives `retry` alone to 14 of 18 kinds. A single choice is a notice, not a decision, and ten of them bury the one `land` or `blocked` that needs a mind. Done when single-choice kinds are pushed once as a `NOTE` line and shown by `check` and the board, `judge` lists only kinds with two or more choices (or a `--all`), and the coordinator playbook section 2 says so.

5. **A build ahead of the store says "schema skew", never "not set up".** With this commit's binary against the live store, `next` printed `NEXT the sprint is not set up / next: nova-sprint up --as rowan` and `identity`, `show` and `judge` refused the same way, while `run` held the lock and 16 rows were on the bus. `up` on a live sprint is the wrong fix to hand a cold AI. Done when `verbs/hooked.go` `notSetUp` distinguishes an empty schema from `sprint.meta` at another version or hash, the latter printing `the store is at schema <v>/<hash>, this build wants <v>/<hash>: nova-sprint up --version <v> on the coordinator, or run the installed build (<path>)`, and `check` prints the same line as a `CHECK RED`.

6. **The live tool and the playbook agree on PRs.** The live `take` printed `the stream wants a PR: gh pr create --base sprint/v2-review/group --head sprint/v2-review/v2r-score-frontier2`; PLAYBOOK-WORKER rule 7 is "Open no PR" and JOB.md says "run no gh". This commit's `takenText` no longer prints that line, so it is a version-skew defect: the dogfood run serves advice this tree has retracted. Done when `check` prints the run service's binary version beside the installed one (`CHECK run build <v> (installed <v>)`, RED when behind) and `up --version` is what the coordinator playbook section 9 says it is.

7. **`hook` stops paying the playbook on every hook.** PLAYBOOK-WORKER is 23 KB and PLAYBOOK-COORDINATOR 30 KB; `verbs/hook.go` prints one or both on every `hook`, which the playbooks say to run after every wrap-around and lost session (about 6k tokens for a worker, 13k for the coordinator, each time). Done when `hook` prints the playbook the first time a session id hooks in an epoch and afterwards prints `HOOKED`, `COMMIT AS`, the two config lines, `playbook: nova-sprint help playbook <role>` and `next:`; with `--playbook` to force it.

8. **A throwaway sprint in one command, so the chain can be tried without a fleet.** SETUP.md is a contract page; nothing in docs shows how to try `up`, `stream add`, `add`, `take`, `finish` on a laptop, and `up` cold refuses with four `NOVA_SECRETS_*` variables to set. The functional tests already start their own PostgreSQL (`initdb`) and the e2e test (`cmd/nova-sprint/e2e_functional_test.go`) walks the chain. Done when `nova-sprint up --throwaway <dir>` (or a documented `make try`) brings up PostgreSQL and Redis in `<dir>`, a stream on a local bare repository, one brief, and prints the deal; docs/SETUP.md gets a "Try it in five minutes" section whose commands are held to real verbs by the same test that holds the playbooks.

9. **Every `help <verb>` ends with a runnable `next:` and lists its refusals.** All 40 tables end `next: nova-sprint <verb> ...` with a literal `...`; 13 verbs (`queue`, `unhold`, `relink`, `replace`, `policy set/activate/show/diff/check`, `worker`, `dash`, `stop`, `start`) have no "refuses when" block at all, though `stop` refuses without `--reason` (`verbs/operate.go`) and `policy set` and `relink` refuse in code. Done when `verbs/help.go`'s `next:` is the verb's own full synopsis or `nova-sprint next --as <you>`, every verb with a refusal has the block, and a test asserts no help text contains `...`.

10. **Refusals fill in what the process already knows.** Live `next --as stranger` printed `next: hook in to the bus first: NOVA_BUS_REDIS=<host:port> nova-bus hook --as stranger ...` while the same process had just answered the bus at `NOVA_BUS_REDIS`; cold, `take`/`show`/`hook` with no bus refused with `nova-bus who failed (bus who: nova-bus who [env TMPDIR,...]: exit 2; stderr "...missing NOVA_BUS_REDIS...")`, a quoted stderr inside a refusal, fix `nova-sprint check`. Done when the hook recipe prints the real address when the process has it, the no-bus refusal is one line (`NOVA_BUS_REDIS is not set: export NOVA_BUS_REDIS=<host:port> (the fleet's bus; nova-fleet check prints it)`), and `show` and `next` read the store without the bus (they look; the bus is for acting).

11. **`show <card>` prints the brief once.** The live record carries `brief` and `prose` with the same 4.4 KB text (the prose is the brief minus its header lines), so every `show` of a card costs double. Done when `show` prints `prose` only when it differs from the brief's body, or prints `brief` and the parsed header fields and drops `prose` from the human form (`--json` may keep both).

12. **A fix deal with a prior head always carries a finding, or says why not.** Live `take` printed `continue from 9fb11074... (git fetch origin ...); the finding: ` with nothing after the colon (attempt 3; `reason` and `finding` both empty in the record). Done when `takenText` prints `the finding: none recorded (attempt <n-1> was released|withdrawn|faulted: <reason>)` from the prior consumer's record, and the deal line does the same.

13. **One deadline per card.** The brief says `DEADLINE: finish within 90 minutes`; `take` printed `deadline=7200s` from policy's `timeouts().consumer[frontier]`. Done when a brief's `DEADLINE:` line is either admitted into the record and shown by `take`, or refused by the lint as a retired mechanic ("the deal names the deadline"), and `take` prints the one that binds.

14. **Where the credential and addresses come from is written where a worker reads.** The refusal says `run under nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`; no worker-facing doc says what `NOVA_PG_DSN`, `NOVA_BUS_REDIS`, `NOVA_BUS_USER`, `NOVA_SECRETS_STORE/KEY/SEAT/PINS` are or who sets them (the studio's `ns` wrapper sets seven). Done when `hook`'s refusal and PLAYBOOK-WORKER "Hook in" step 2 name each variable in one table (name, what it is, who gives it: `up`, the coordinator's invite, nova-fleet), and `up` step 7 prints the worker's full wrapper line for a kin on invite.

15. **The dashboard JSON says one true thing about the machine.** `/api/sprint` live: `"machine": "machine: RUNNING"` (the label doubled) and `"stopped": false` beside a `stop_reason` of `v1 import reconciled: 151 producers landed ...`. Done when the feed's `machine` is the bare state and `stop_reason` is empty while `stopped` is false (a test in `dash/` on the folded view).

16. **One output stream for refusals** (hulk's finding at `9fb11074`, reproduced here). Cold, `up --as x` exits 1 with 0 bytes on stdout and `SPRINT REFUSED up: control-seat secrets configuration is incomplete: ...` on stderr; `take foo@1 --as x` exits 2 with its 250-byte refusal on stdout and nothing on stderr. A harness that reads one stream sees an empty exit 1 from `up`. Done when every `SPRINT REFUSED` and `next:` line of every verb goes to the same stream (`cmd/nova-sprint/main.go` and `cmd/nova-sprint/up_wrapper.go`), `help`'s header names it, and a test runs each verb's first refusal and asserts the other stream is empty.

## Where I would be lost cold

1. **My own brief's step 1.** "Start in the job directory; read JOB.md first" and "cd into the staged checkout JOB.md names": there is no JOB.md for a kin row and no job directory; the brief was written for a bench harness (`edge/worker/jobmd.go`) and reached a Claude session. A cold AI either searches for a file that does not exist or invents one, and the lint in `verbs/brief_lint.go` that refuses "JOB.md" in a brief did not fire on this card.

2. **`show` gen vs `take` gen** (`verbs/take.go` `heldBy`). Lose the deal message (a wrap-around is the playbook's own scenario), run `show <card>`, copy its gen into `take`, be refused with a state name ("Take needs ready") and sent back to `show`. The playbook's "a repeat prints the same" is true only at the gen the deal printed, which is the one thing a wrapped session no longer has.

3. **A build one hash ahead of the store** (`verbs/hooked.go` `notSetUp`, `verbs/next.go` line 112). `nova-sprint` with the newer binary says the sprint is not set up and names `up --as rowan`. A cold AI that trusts "every output names the next command" runs `up` against a live sprint. The honest line is in `check` (`not at this build's schema`); `next` should say the same.

## Tokens and time

- The docs, not the tool: 178 KB across the seven pages this card names or links (LAYER-6-RUN.md 77 KB, PLAYBOOK-COORDINATOR.md 30 KB, PLAYBOOK-WORKER.md 23 KB, CORRECT-USE.md 18 KB, README.md 14 KB, CARD-CONTRACT.md 11 KB, SETUP.md 5 KB), about 45k tokens, against 19 KB for all 40 `help <verb>` tables. docs/README.md is a design log (locks, quotes, an HN thread), not a start page; the one transcript of a cold start lives in LAYER-6-RUN.md section 4; SETUP.md is a contract, not a walk. One 4 KB "Start here" with the transcript and the variable table would have replaced most of that reading.
- Finding out why `take @1` refused: five verb runs and a read of `verbs/take.go`, because the refusal named a state instead of the gen it wanted.
- Reaching the store at all: the `ns` wrapper on this machine sets seven environment variables and runs `nova-secrets exec`; none of that is derivable from a refusal or a worker doc. I copied it; a cold AI on another machine could not.
- Running this commit's binary against the live store cost nothing but taught nothing: every verb refused with "not set up" (item 5), so the sprint's health lines, `show`, and `judge` had to be judged from the older live build and from the code.
- `hook`: not run here (it writes), but at 23 to 53 KB per hook, and the playbooks telling me to hook again after every wrap-around, it is the single largest recurring token cost of using the tool as designed.
- Cheap and worth it: `queue` (one line a card), the deal message (one line), `check` (six lines, two seconds), `finish`'s refusal (one line that was exactly right).
