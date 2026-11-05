# The fleet worker: what `nova-swarm member` does, and what `nova-sprint worker` keeps

Every behaviour of `nova-swarm member` (the member loop) and `nova-swarm member --reader`
(the reader loop) that the live sprint relies on, with the nova-tools source it comes
from, marked KEEP or DROP. Sources are nova-tools at `a916ddacb` (v1.6.29):
`cmd/nova-swarm/*.go` (abbreviated `ns/`), `pkg/member/member.go` (`member.go`) and
`pkg/swarm/*.go` (`swarm/`).

## How each mark was checked

- **Loop units.** `systemctl --user cat nova-loop-member-hetzner2 nova-loop-reader-hetzner2`
  on hetzner2, the units on amd-2, and `ps` on the studio. The live argv is
  `nova-swarm member --as <m> --server <studio>:6390 --harness <opencode> --root <dir> --pass <keys>`.
  The reader adds `--reader --identity <pool identity>`. Every other flag runs at its
  default. No unit sets `NOVA_SWARM_MIRRORS` or `NOVA_SWARM_KEYS`.
- **Logs.**
  - hetzner2 `~/nova-bench/loops/member-hetzner2.log` (6.8 MB) and `reader-hetzner2.log` (1.9 MB):
    marker lines counted with `grep -c`.
  - The 180 retained `<slots>/*.native.log` files: native's end lines counted with `grep -l`.
  - Counts are quoted in the evidence column.
- **Callers.** `grep` across nova-tools and nova-sprint for each flag, packet field and
  verb word.

A behaviour is DROP when no live unit passes the flag that turns it on, or when the logs
show it never fires. Nothing marked DROP is something the sprint server needs from a
worker in order to deal, take, finish or read a card.

## The loop (pkg/member)

| # | behaviour | from | mark | evidence / note |
|---|---|---|---|---|
| 1 | Every verb goes to the sprint's server, never a store. A verb with no answer is retried on the next pass. | member.go:48, ns/sprintredial.go:77 | KEEP | The server is the one writer. It is reached through `NOVA_SPRINT_SERVER`, or the seat record, as every forwarding nova-sprint verb is. |
| 2 | Redial backoff, 1 s doubling to 30 s | ns/sprintredial.go:107 | DROP | The pass interval is the backoff. A pass that gets no answer waits for the next one. |
| 3 | Queue every pass: `queue --as <row> --json --packets <n> [--have <ids>]` | member.go:2573 | KEEP | This is how the worker reads width, epoch, its cards and their packets. |
| 4 | Retry the queue without `--packets` against an old server | member.go:2670 | DROP | No server older than `--packets` is running. |
| 5 | Idle passes read the queue every 15 s instead of every 3 s | member.go:2683 | DROP | One queue a pass keeps the pass simple. The verb is cheap on the server. |
| 6 | Width is the queue's answer every pass. Lowering it never kills a card. | member.go:1310 | KEEP | `width 0 -> 16 (nova-config)` appears on every start. |
| 7 | Work beat: `fleet beat <m> [--live <set>] [--draining] [--facts <list>] --load <pct>` | member.go:1035 | KEEP | The server reconciles a working card that the live set does not name back to ready (cmd/nova-sprint/fleet.go:188). Facts gate which cards a machine is dealt. |
| 8 | Beat `--stop-returns <n>` | member.go:1052 | DROP | The new worker sends each stop-return in the same pass it stops a lane, so nothing is ever owed. |
| 9 | `--load-unmeasured` probe of `fleet beat -h` | ns/member.go:365 | DROP | A load the worker cannot read is sent as 0. |
| 10 | Facts retry and 10-minute back-off when the server refuses `--facts` | member.go:1099 | DROP | Every running server takes `--facts`. |
| 11 | Beat refused marks `<root>/<as>.status`, which `nova-swarm doctor` reads | member.go:1148, ns/member.go:353 | DROP | The reader of that file is `nova-swarm doctor`, which is not moving. The refusal is logged instead. |
| 12 | Beat on its own goroutine. The beat stops when a pass has stalled for 5 minutes. | member.go:1170 | DROP | The beat is part of the pass. No part of the pass blocks, because staging and the child run on the lane's own goroutine (worker row 1 below). So a stalled pass is a stalled beat, which is the signal the stall rule existed to give. |
| 13 | Reader beat is a `queue --packets 0` | member.go:1044 | KEEP | The reader's pass queue is its beat. |
| 14 | Reader not in the readers table: say so once | member.go:1295 | KEEP | This is the reader's queue answering `reader:false`. |
| 15 | Take work: `take --as <m> --limit <room> --json --epoch <e>` | member.go:1859 | KEEP | 4743 `start` lines. |
| 16 | Room in half slots: a work card costs 2 and a read card 1, against twice the width | member.go:904,1797 | KEEP | The member is dealt read cards: 2529 `read` lines in the member log. |
| 17 | Begin reads: `read --as <reader> --begin <id@gen>... --epoch <e>` for asked cards that carry a packet | member.go:1849 | KEEP | 181 `start` lines and 735 `read` lines in the reader log. |
| 18 | Start stagger: 300 ms between harness starts | member.go:3381 | DROP | Each lane stages on its own goroutine. Starts are already spread out by the time each one takes to stage. |
| 19 | Disk floor: no card starts with less than 10 GiB free on the slots volume | ns/slotclean.go:216 | KEEP | This is a default-on guard. It never fired (0 `take REFUSED`), and it costs one statfs a pass. |
| 20 | Refused staging is handed back: `finish --failed --report "staging refused: ..."` or `read --return` | member.go:2504 | KEEP | 165 `staging refused` lines, mostly "no bench mirror". |
| 21 | A launch that fails to start is handed back: `finish --failed` or `read --return` | member.go:2523,2538 | KEEP | This is the same path as row 20. |
| 22 | Recover working cards after a restart, and adopt live lanes by pid file | member.go:1905, ns/member_identity.go:15 | DROP | Row 7's `--live` reconciliation returns an orphan to ready, and the worker kills its lanes when it stops. The 3231 `recover` lines all say "held: its finish is sent again", which is row 31. |
| 23 | Progress stamps: `progress --as <m> <id@gen>... --epoch <e>`, every 3 min, for a child that printed since the last stamp | member.go:2725 | KEEP | The server's working-card liveness reads it. |
| 24 | Report the pushed head early: `progress ... --head <sha>` | member.go:2772 | DROP | The finish carries `--head` in the same pass, right after the push. |
| 25 | Push a work card's commit from outside the wall. The commit is fetched from the checkout into `<root>/push.git`, which must descend from the staged commit and add at least one commit. Then `git push --porcelain --no-verify <url> <sha>:refs/heads/<branch>`, never forced. | ns/memberpush.go:123,159,221,237,248 | KEEP | 1859 `push` lines. The bare repository means git never runs inside a checkout that the child wrote. |
| 26 | Push retry on `[remote rejected]`, 1/2/4 s | ns/memberpush.go:248 | DROP | A refused push is reported, and the sprint deals the card again. |
| 27 | Head repair: push the one tip that descends from the staged commit when the result names an unknown sha | ns/memberpush.go:308 | KEEP | The result head comes from the child's RESULT.md, and a model mistypes it. |
| 28 | Stale RESULT.md check: a RESULT.md that is the repository's own file at the staged commit is refused | ns/memberpush.go:280 | KEEP | This is a cheap guard against a child that wrote nothing being read as done. |
| 29 | Open a pull request when the result has a title (`gh pr create`) | ns/memberpush.go:360 | DROP | 0 `pr=` in 1859 pushes. The sprint's land opens what it lands. |
| 30 | `git repack` and `git prune` of push.git once an hour | ns/memberpush.go:446 | KEEP | push.git grows with every card. Without this the volume fills. |
| 31 | Finish work: `finish --as <m> <id@gen> --report <r> [--head <sha> --branch <b>] [--failed] [--usage <u>] [--exit <n> --harness <tail>] --epoch <e>` | member.go:1613 | KEEP | 1917 `finish` lines. The report is built by `member.Judge` and `CarryProposed`. |
| 32 | Finish verdict rules, in order: staging, budget, push refused, provider, no RESULT.md shape, verdict, no commit, ok | member.go:303 (Judge) | KEEP | This is reused as is, by calling `member.Judge`. |
| 33 | Read verdict: `read --as <r> --ok\|--broken <id> --finding <f> [--usage <u>] --epoch <e>` | member.go:1594 | KEEP | 1277 `read ... ok=true`. |
| 34 | A read with no verdict, or a broken verdict with no defect named, is handed back: `read --return <id> --reason <why>` | member.go:1562,1586 | KEEP | `returned exit=0: no verdict` appears often in both logs. |
| 35 | A read whose staging failed retries once after 15 s | member.go:1522 | DROP | The read is returned, as in row 34. The server deals it again on the next ask. |
| 36 | Strip `--exit` and `--harness` and resend for a server that does not know them | member.go:1648 | DROP | Every running server knows them. |
| 37 | A refused finish is resent with back-off (1 to 8 min). After 3 refusals the card is handed back with `stop-return`. | member.go:2356,2372,2418 | DROP | 0 `taken on retry` and 0 `finish refused` in either log. A finish that gets no answer (exit 2) is still resent on the next pass (row 1). A finish that is refused is logged and dropped, and the server's own rules take the card back. |
| 38 | Claim moved, or card left the queue: reap the lane and send no finish | member.go:2553,2795 | KEEP | 3078 `reaped` lines. |
| 39 | Machine STOPPED: stop every lane and send `stop-return --as <row> <id@gen> --epoch <e> --reason "owned process stopped"`. Take nothing while stopped. | member.go:2221,2316 | KEEP | 27 `LANE CANCELLED BY STOP`. |
| 40 | Drain on SIGTERM: take nothing, report each lane as it ends, and end what is left after `DrainBound`. The beat says `--draining`. | member.go:845,869; ns/member.go:437 | KEEP | 53 `MEMBER DRAIN`. |
| 41 | Drain and exit 3 when the running binary is replaced | ns/member.go:481 | KEEP | `MEMBER STOP the binary this member runs was replaced` appears on each install. |
| 42 | A terminal interrupt ends every lane at once | ns/member.go:473 | KEEP | This is the same path as row 40, with no drain. |
| 43 | Attempt decision (JEV) on the finish: `--decision` | member.go:3092, ns/memberattempt.go:44 | DROP | No unit passes `JEV_API_KEY` (`--pass` lists none), so `workAttempt` is nil. The 24 `decision` matches are reader findings that use the word. |
| 44 | Script reads: verify a script card's program in place of a model read | member.go:3412, ns/member.go:1840 | DROP | 0 `script read` lines. |
| 45 | Tree cards: step verdicts and pushing the last good step | pkg/member/tree.go, stepsha.go | DROP | 0 `every work step is a script step` lines. |
| 46 | One-shot friend identity prompt (`--prompt`) | pkg/member/identity.go; ns/member.go:85 | DROP | No unit passes it. Glenn 2026-11-05 removed one-shot. |

## The launch (`nova-swarm native`, the child of every card)

| # | behaviour | from | mark | evidence / note |
|---|---|---|---|---|
| 47 | Launch name `<card>.g<gen>.e<epoch>` (work) or `<card>.a<attempt>.e<epoch>` (read). The slot is `<slots>/<name>` and the job is `<slot>/jobs/<card>`. | ns/member.go:1007,737 | KEEP | |
| 48 | Route from the packet: model, tokens and deadline. A card missing any of them is refused. | ns/member.go:939 | KEEP | `route=heavy-deepseek41-direct model=deepseek/deepseek-flash` on the start lines. |
| 49 | Member `--model`, `--tokens` and `--deadline` overrides | ns/member.go:61,66,67 | DROP | No unit passes them. |
| 50 | Card text: the brief with held rules (`StagedBrief`), then `member.CardText`, which adds the wall text, the fix and the notes | ns/member.go:922; member.go:3201 | KEEP | This is reused by calling `member.CardText` and `swarm.StagedBrief`. |
| 51 | Stage from the bench mirror only. Never clone from GitHub. Check out the sha or base on the card's branch. A rework is restaged at the tip with the earlier attempt's push carried on top. 120 s bound. | swarm/stage.go:385, native.go:892 | KEEP | 61 `STAGE CARRY` in 180 native logs. This is reused by calling nova-sprint's own `pkg/swarm.StageCard`, which friend stage already uses. |
| 52 | A read's start: fetch the review base from origin and take the merge base, so the read sees only the work | native.go:3040 (workStart) | KEEP | Without it, a read diff names every card landed since the mirror was refreshed. |
| 53 | A read's gate: the tests the work changed (`cardcontract.ReadGate`) | native.go:3075 | KEEP | This is JOB.md's gate section. |
| 54 | The frame: JOB.md, and the git and gh shims that record pushes and finish (`cardcontract.Install`) | native.go:2983 | KEEP | This is reused by calling `cardcontract.Install`. The card text tells the child to read JOB.md first. |
| 55 | Recipes: `Stage:` files copied into the job | native.go:3010 | KEEP | This is reused by calling `cardcontract.StageRecipes`. |
| 56 | The wall: the harness runs under `nova-sandbox`, which can read the slot and write the job, data home and tmp. Its cwd is the job. The harness directory and toolchain roots are readable, and the mirror's objects are read with no exec. A run with no wall is refused. | native.go:1045,2002 | KEEP | `sandbox=landlock` on every NATIVE line. |
| 57 | `--no-wall` | ns/member.go:74 | DROP | No unit passes it. The functional test hands the worker a stand-in wall instead (see the tests). |
| 58 | The harness: `opencode run --model <m> --title <label> -- <prompt>`, where the prompt is `cardcontract.Prompt(job, card)` | swarm/providers.go:125, native.go:2930 | KEEP | Every unit's harness is opencode. |
| 59 | Headless harnesses (claude, codex, grok) and Muse | native.go:527, ns/member.go:975 | DROP | No route of a live unit names one, and every unit's harness is opencode. |
| 60 | The child's environment is an allowlist: PATH, HOME, TMPDIR and locale, with HOME and XDG set to the data home and TMPDIR to tmp. A name that looks like a secret is dropped. Only the route's own provider key is passed. The pool identity is set as `GIT_AUTHOR_*` and `GIT_COMMITTER_*`. | ns/member.go:1697, ns/keys.go:136, native.go:2156,1155 | KEEP | This keeps every provider key but one out of each child. |
| 61 | In-process secret reads for keys absent from the environment (`NOVA_SWARM_KEYS`, seat) | ns/keys.go | DROP | `nova-secrets exec` puts every key in the environment before the loop starts. |
| 62 | Shell shims that unset every KEY, TOKEN and SECRET before exec'ing sh or bash | ns/shellshim.go | KEEP | This keeps the passed provider key out of every tool the model runs. |
| 63 | Go shim, shared GOCACHE and GOMODCACHE, and `-p=4` build parallelism | native.go:760,2222 | KEEP | `GOFLAGS=-p=4 MAKEFLAGS=-j4` with no shared caches. The shared caches go. Each card's cache lives in its own data home and is removed with it. |
| 64 | Model catalog: `opencode models` refreshed once at start into `<root>/catalog/models.json`, then seeded into each launch with `OPENCODE_DISABLE_MODELS_FETCH=1` | ns/catalog.go:36,96 | KEEP | `CATALOG refreshed` appears on every start. Without the seed, each launch fetches models from the network inside the wall. |
| 65 | Deadline: the child's process group is killed at the deadline, with no grace. The end is `deadline`. | native.go:317,1377 | KEEP | 283 `deadline` lines in the member log. |
| 66 | Token budget: the harness's sqlite usage is sampled every 5 s, and the group is killed at the budget | ns/nativesample.go:239,288 | KEEP | `budget=134886/2000000` on the NATIVE lines. The worker samples once a pass (3 s) through `sqlite3 -readonly`. A machine with no sqlite3 runs to the deadline alone and says so, where native refused the card (192 `native refused: ... sqlite3 is on no PATH` returns). |
| 67 | USD and per-worker card budgets, and the `unverifiable` stop | ns/nativesample.go:288 | DROP | 0 `stopped=` of any kind in 180 native logs. |
| 68 | Usage: `--usage` on the finish and the read, from the harness's own database | native.go:2539; ns/member.go:1391 | KEEP | Cost records on every card. |
| 69 | Receipt reconciliation, billing restore and raw usage records | ns/member.go:1107,1893 | DROP | The usage is read once, from one source. |
| 70 | Idle reap after 300 s with no output and no process activity | native.go:1382 | DROP | 0 `reason=idle-reap` in 180 native logs. The deadline bounds every run. |
| 71 | Provider proxy: stall detection, 429/503 retry and rate budget (`--budget-server`) | native.go:845, ns/nativebudget.go | DROP | 0 `budget-server` launches. The direct routes reach the provider themselves. |
| 72 | Harness start retries and the provider retry ladder | native.go:1416 | DROP | A failed start is a failed finish, and the sprint deals the card again. |
| 73 | Gate decision on a red gate (JEV) | ns/nativegate.go | DROP | 0 `NATIVE GATE`. |
| 74 | Decide reads (JEV) | ns/nativedecide.go | DROP | 0 `DECIDE`. |
| 75 | The result: `.sprint/finish.md` (gh shim) first, else RESULT.md at the job root or in the repo (`typedrec.ParseCardResult`). The head falls back to the shim's last push. | ns/member.go:1391-1508 | KEEP | This is reused by calling `cardcontract.ReadFinish`, `typedrec.ParseCardResult` and `cardcontract.LastPushed`. |
| 76 | Harness output tail (40 lines) on a failed finish | member.go:3317, ns/member.go:1526 | KEEP | `--exit` and `--harness` on a death. |
| 77 | Lane lock: hold a Windows winlock around each lane (`--lane-lock`) | ns/lanelock.go | DROP | No unix unit passes it. Captain, the one Windows member, stays on nova-swarm (see the end of this file). |
| 78 | GPU, load gates, and Windows paths (MSVC env, Job Objects, taskkill) | ns/member.go:75,80; msvc_windows.go | DROP | No unit passes them. This worker is unix-only. |

## Cleanup

| # | behaviour | from | mark | evidence / note |
|---|---|---|---|---|
| 79 | A launch the worker is done with leaves no checkout. The newest 5 failed launches are kept for reading. | ns/slotclean.go:54,133 | KEEP | 17591 `CLEAN` lines. The worker removes each launch as it is reported. |
| 80 | Lazy round of old epochs, and Go build cache trim | ns/lazyclean.go | DROP | There is no shared cache to trim (row 63). Each launch is removed as it ends (row 79), so an old epoch leaves nothing behind. |
| 81 | Sweep ended launches at start | ns/member.go:308 | KEEP | This is the same as row 79, run once at start. |

## Whole verbs and modes

| # | behaviour | from | mark | evidence / note |
|---|---|---|---|---|
| 82 | `template`, `lint`, `verify`, `profile`, `step`, `slots`, `worker`, `install`, `uninstall`, `doctor`, `version` | ns/main.go:203 | DROP | These are not part of the member loop. |
| 83 | `disk-guard` and `mirror` verbs | ns/diskguard.go, ns/mirror.go | DROP here | They are separate loops (`nova-loop-disk-guard`, `mirror-refresh`), not member or reader. They need a home before nova-swarm is deleted. |
| 84 | Card templates, card directories, swarm-of-many runs, one-shot | ns/main.go, swarm/templates.go | DROP | As asked. |
| 85 | The generated wire (`pkg/swarmwire`) | pkg/swarmwire | DROP | The worker speaks the sprint server's verbs over `pkg/sprintwire`. No new wire is added. |

## Count

**KEEP: 47. DROP: 38.** (85 rows, counted by the mark column.)

## What `--as <m>` runs

One `nova-sprint worker --as <m>` runs both rows of the machine:

- `<m>`, the work row.
- `reader-<m>`, the read row.

The two rows are two passes over one launcher, under `<root>/work` and `<root>/read`. This
replaces the two units `member-<m>` and `reader-<m>` with one.

## Captain

Captain (Windows, `--lane-lock`) stays on nova-swarm until a Windows worker is decided.
