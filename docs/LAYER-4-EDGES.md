# Layer 4: the edges

Draft for Glenn, 2026-11-06. Not approved. Glenn on the runner: "you need
actual runners ... that was pretty reliable already" (README, layer 4), and
"The runners do all the stuff automatically" (LESSONS 4.16).

An edge is what nova-sprint does outside the store: send over nova-bus,
move git, ask GitHub, run work on a bench. Each is a Go interface with a fake
for unit tests (no sockets, clock or network) and one real adapter that
execs the outside tool, tested in the functional tier. No edge runs under
the writer lock (LESSONS 3.23): layer 3 gathers through the edges before a
tick's op and acts through them after it. No edge decides a move; it reports
facts (a send confirmed, a head on origin, a check green) as evidence.

## The list for Glenn (bottom up: structure; guarantee; check)

1. `edge.Exec`, the one way an edge runs a program: argv only, never a shell
   string; stdin closed; a timeout from policy; output capped and kept; a
   timeout or missing binary is an error naming the program and the fix.
   Checked by unit tests on a fake process table, functional per adapter.
2. `bus.Edge` over `nova-bus send` and `nova-bus who` only: messages bounded,
   built from the event log past a per-recipient cursor that moves only after
   nova-bus prints the id; never parses a message. Checked by `tla/Push.tla`,
   `bus.Fake`, and a throwaway Redis on a bench with the real nova-bus.
3. `git.Edge` (fetch, merge, rebase, push, ancestry): scripts that never ask
   a mind; a failure is a typed result (conflict with files, push refused,
   host silent). Checked by `tla/LandEdge.tla`, `git.Fake` (an in-memory
   commit graph), and throwaway bare repositories.
4. `forge.Edge` over `gh` with the acting identity's own credentials: merges
   only the exact sha CI passed, only onto the base it was gated on; a rate
   limit rests the identity and fails no card. Checked by `forge.Fake` and a
   scratch GitHub repository with its own token.
5. `nova-worker`, one service per bench: deal to report by script, a slot
   from nova-reserve, a worktree per card inside nova-sandbox, an adapter per
   harness, restartable with nothing lost. Checked by `tla/Runner.tla`, fakes
   of every edge it touches, and a bench run with a script harness.
6. The report verbs: check, plan, one `Apply` fenced by gen; layer 1 stays
   closed. Checked by refusal tests on a real store and `tla/Runner.tla`.

## The bus edge

`Bus` has `Send(to, subject, body)` over `nova-bus send` and `Who()` over
`nova-bus who`. The run loop sends as its own name (`sprint`). nova-bus
refuses a send from a name that is not hooked, so `up` hooks it with
`nova-bus hook --as sprint --service --exec 'nova-sprint bus-reply'`;
`bus-reply` reads only the sender and answers once: "replies are verbs: run
nova-sprint next --as <you>". The engine never parses a bus message; every
answer is a verb a row runs against the store (CORRECT-USE: "There is ONE
WAY").

Sent the moment the op that caused it commits:

| subject | to | when | answered by |
|---|---|---|---|
| `DEAL` | the row | consumers were cut for it (`Start`, `AskRead`) | `nova-sprint take` |
| `WITHDRAWN` | the row | a consumer it holds was withdrawn (preempted, silence) | stop; the runner pushes what it has |
| `NEEDS YOU` | the coordinator | a judgement opened, one per `group_key` per episode | the verb the judgement names |
| `LANDED` | the coordinator | a landing into base (LESSONS 3.20) | nothing; it is the record |

One message per recipient, subject and tick: every deal to a row in a tick
is one `DEAL`; every judgement of one group is one `NEEDS YOU` with its count
(LESSONS 4.9). A message is at most 4 KB and 20 card lines; past that it ends
"and N more: nova-sprint queue --as <row>". A free-text field (reason,
finding) is cut at 300 bytes with "full text: nova-sprint show <id>". Logs,
diffs, briefs and secrets are never in a message; `take` fetches the brief
from the store by id (LESSONS 4.2, 4.4).

What a claude row sees, one line from its hook (`\n` is nova-bus's escape):

```
1762444800123-0	sprint	-	DEAL	1 card for amd-1.ds (epoch 3)\ncard=rn-ring.w2 attempt=2 kind=work stream=rocketnet tier=pro\nprior=<40 hex> reason="read: ring overwrites at capacity"\nrun: nova-sprint take rn-ring.w2@7 --as amd-1.ds
```

A retry carries `prior=` and `reason=` (LESSONS 4.3); a read deal says
`kind=read head=<sha>`. A judgement is CORRECT-USE's `NEEDS YOU` shape (the
question, then `run:` with the answering verb), its first line carrying
`group=<key> n=<count>`.

Delivery. Messages are made from events past the recipient's cursor
(`sprint.cursors`), and the cursor moves past an event only after `nova-bus
send` printed `<name> <id>` for it. A failed send (`SEND FAILED`, a timeout)
leaves the cursor, and the next tick sends from it; nova-sprint never
retries within a tick and never touches Redis. A send that landed but whose
output was lost goes twice, and every message is safe twice (a take is
fenced on gen; a judgement is shown from its record). So: never skipped, at
most repeated: pushes are at-least-once and safe twice. Layer 1's "never
pushes twice" is read as never two different messages for one cursor
position; a repeat of the same message stays possible until `nova-bus send`
carries an idempotency key, an item for nova-tools, deferred.

Presence. `nova-bus who` proves that a hook process holds the name (a
crashed hook drops out within 30 seconds), not that a session is behind it
(LESSONS 4.6). So `Seen` is raised when a verb runs as the name, never on
`who` alone; `who` dropping a name is evidence for policy's silence, never
`Gone` by itself. A worker row hooks only after its harness probe passes,
so a worker in `who` has a path that works. Never sent: an ack, heartbeat
or digest; anything to a row with nothing to change, or to Glenn directly
(layer 6's channel); a row's reply.

## Reports reach the store

A report is an ordinary store op applied by the verb itself. `nova-sprint
report <consumer> --result ... --head ... --id <result id> --as <row>` (and
`finish` and `read`, which are the same path) runs the checks: the holder;
the head on the branch at origin (`git ls-remote`); for an ok, checks green
at that exact head and the PR when the stream wants one. It then builds the
card machine's plan (the consumer's `Report` to done plus the producer's
`Finished`, `Broken` or `Faulted`, in one plan) and calls `Apply` directly:
one transaction, fenced by gen, its op id the result id, so a repeat returns
the first receipt (LAYER-2, `Report`). The run loop sees it in its next
snapshot and parses nothing. Layer 1 stays closed.

The bound is 200 reports a second: layer 1's measurement at 5,000
producers (2026-11-06) put the report median wait at 2.1 ms, with the
writer lock near saturation at about 200 a second, far above any real rate
of a 20-machine fleet. The DSN reaches only the verb process, through
`nova-secrets exec`: "workers never get the DSN" (layer 1) means no harness,
no agent, nothing in a wall.

## The git edge

`Git` has `Fetch`, `MergeGroup(onto, heads, ledgers)`, `Rebase(worktree,
onto)` (conflicts name files), `Push(sha, ref, expect)` and `IsAncestor`.
The rung and what a red means are layer 3's (LANDING.md); this edge does the
steps by script, one attempt per stream at a time, streams in parallel, base
pushes one at a time in stream order:

1. **Fetch** base, the stream branch and every head of the group into the
   lander's clone on the landing host. A head origin does not hold refuses
   the landing for this pass, naming it (LPM M16).
2. **Merge** the group in score order onto the stream branch at the base
   tip. Ledger files (policy's list per repository) are merged entry by
   entry or regenerated by the repository's ledger script. Rung 1 is a group
   of one; a head already on the base tip needs no merge.
3. **Push** the merge to `sprint/<stream>` with the expected old value
   (`--force-with-lease=<ref>:<old>`), so a stale pass is refused by git.
4. **Gate**: the forge edge finds or opens the group's PR and waits for
   checks at that exact sha. CI runs once per group; a group of one whose
   head already passed at this same tree reuses that result.
5. **Land** only while base is the tip it was merged on. Without PRs: `git
   push origin <sha>:refs/heads/<base>`, refused by git unless it
   fast-forwards. With PRs: `gh pr merge --match-head-commit <sha>` on a base
   whose protection requires branches up to date, so GitHub refuses a moved
   base; `stream add` refuses a protected base without it, naming it.
6. **Fast-forward** the stream branch to the new base in the same pass.
7. **Record**: each landed head is checked an ancestor of the pushed base
   tip (LPM M21), and the fact goes to layer 3 for `Landed`.

When base moved before step 5, the group is re-merged onto the new tip and
gated once at the combined tree (LANDING step 6); its cards are never sent
back. A re-merge that conflicts outside ledgers is a rung 3 judgement naming
the files. A red gate goes to layer 3's bisection (LANDING step 7); a gate
that did not run (no CI runner, a cancelled job, a timeout) is a fault and
blames no head.

The rebase rule: a work consumer is rebased once, by the runner, at report,
onto the stream head as it is then. After the report the stream moving is
the landing's to absorb, never the card's to chase (Glenn, LANDING step 3:
"this has always been the rule. you guys just forget it over and over"). A
rebase conflict is a `fail` report naming the files.

A landing refuses for this pass, spending nothing, on: a head not on
origin; a re-merge conflict outside ledgers; a failed ledger script; a push
refused for a moved ref; checks not green at the exact sha; base moved
between gate and land; no landing host answering. Each is a typed result
that layer 3 counts toward the stall alarm and the stream's judgement.

A landing never: force-pushes base; merges a sha CI did not pass; puts two
streams in one branch (LPM M5); rebases a card's branch; resolves a
non-ledger conflict by script; runs under the writer lock or on the
coordinator's machine.

The landing host is a bench from policy's list. The edge runs a fixed
binary there (`nova-sprint land-step`, argv only) over ssh with a connect
timeout and no shared control socket; a host that does not answer is
skipped for this pass and the next tried (LESSONS 4.13). A hold never
removes a landing host (LANDING step 9).

## The GitHub edge

`Forge` has four methods: `PR(head, base)` (find), `OpenPR`, `Checks(shas)`
and `Merge(pr, sha)`.

Credentials. Every `gh` call runs with an explicit identity: `GH_TOKEN` from
nova-secrets for a row, or `GH_CONFIG_DIR` for the sprint's own identity (on
the studio, rowan's directory; bare `gh` there is Glenn's account). The
edge refuses to run `gh` with neither set, and refuses when `gh auth status`
names another login than the stream's, naming the fix. Glenn's credentials
are never used. A row's token is minted by nova-tools for that row, scoped
to its streams' repositories; minting is nova-tools', this layer only uses
it. Workers push and open PRs with their row's token; the lander merges as
the sprint.

CI reaches the sprint by the run loop, never by someone running a verb
(LESSONS 4.10). Before each tick, at policy's interval (default 15 s,
unmeasured), the forge edge asks for the check rollup of every head waiting
on CI: one GraphQL query per repository for up to 100 shas. The answer is
evidence (green; red with the failing checks and URL; pending) recorded on
the consumer by the tick. Webhooks would make it an event in the strict
sense; they need a public endpoint and are deferred to layer 6.

Rate limits: a token bucket per identity, sized by policy under GitHub's
documented limits (5,000 requests an hour; PR opens far fewer). A 403 or
429 with a retry time rests that identity until then: a fault to its caller
that spends nothing. Rung 2's one PR per group keeps a backlog inside it.

## The runner: nova-worker

One service per bench, installed by nova-fleet's play as a launchd or
systemd unit; its binary is replaced by a new file and a rename, never
overwritten in place (LESSONS 4.23). It serves the bench's rows (one per
harness and model: `amd-1.ds`, `amd-1.mercury`), each with its width from
the store. Width and slots are separate counts: width caps a row's
`working` consumers; nova-reserve's slots, shared with everything on the
bench, cap running programs. A consumer waiting on CI holds width, no slot.

Deal to report:

1. **Wake.** The runner runs `nova-bus hook --as <row>` as its own child per
   row and reads its stdout. A line is only a wake: the runner runs
   `nova-sprint queue --as <row>` and acts on what the store says, so a
   lost, late or repeated message changes nothing. No poller, wake file or
   second channel (LESSONS 4.1).
2. **Take** each ready consumer up to width: `nova-sprint take <id>@<gen>
   --as <row>` prints the brief, base, branch, done-when and tier.
3. **Stage** a worktree at `<root>/jobs/<consumer>/repo` from the bench's
   work clone, which borrows the mirror's objects and fetches from the
   mirror, never the network; never from the mirror itself, whose pruning
   fetch would delete branch refs. The worktree's commit identity is the
   row's, never the machine's (LESSONS 4.21). Siblings the card reads are
   staged read-only the same way. A staging failure or timeout is a `fault`
   (LESSONS 4.14). A job directory that exists is refused, so none is
   reused (LESSONS 4.15).
4. **Run** under `nova-reserve run --slots <weight> --wait <d> --why
   <consumer> -- nova-sandbox ... -- <harness>`: the slot lives exactly as
   long as the harness (LESSONS 4.17); no slot within the wait is no start
   yet, never a failure. The wall: `--write` the job directory (worktree,
   `HOME`, `TMPDIR`, data home), `--read` toolchains and siblings,
   `--net-deny --net-allow localhost:<proxy>` where the host's wall can deny,
   stdin closed. The model is nova-config's for (row, tier), chosen by code
   and recorded in the report (LPM T3, T28).
5. **Harvest** outside the wall: read the result file, push the branch with
   the row's token. On a wall death, a kill or a withdraw, push whatever
   commits the worktree holds, so the next attempt continues from them.
6. **Rebase** onto the stream head as it is now and re-run the card's test
   command, inside a slot, bounded by the two-minute rule; push. A conflict
   is a `fail` naming the files.
7. **PR and CI**: open the PR when the stream wants one; wait, woken by the
   bus, for the run loop's check evidence at the head.
8. **Report** with a result id written to `job.json` before the first try;
   retry the verb with that id until it exits 0 or 1 (LESSONS 4.5).
9. **Clean** the job directory only after the report is recorded and the
   head is on origin; a worktree with uncommitted changes is logged and kept.

Results, classified by the adapter. `ok`: the result file says ok and a head
past `base_sha` and `prior_head` is pushed. `fail`: the result file says
broken, no result file, a rebase conflict, the card's tests red. `fault`:
exit 134 or 137; a launch that died inside the 15 s grace on a provider 5xx,
three times; a rate-limit or credit refusal; a staging failure; a restart
that lost the harness. A read reports `ok` or `fix` with its finding file;
a read's provider failure is a fault and spends no reader (LPM T30).

```go
type Harness interface {
    Name() string                                // claude, codex, opencode
    Command(j Job) (argv, env []string)          // pure, from the job and nova-config
    Probe(j Job) Job                             // a one-turn job on the same path
    Classify(exit int, tail []byte) Outcome      // pure
    Usage(j Job) (Cost, bool)                    // the harness's own accounting
}
```

Adapters: `claude` (its settings refuse subagent spawns, as v1's hook did),
`codex`, and `opencode`, which also carries the API-rate models (DeepSeek,
Mercury, OpenRouter) through an OpenAI-shaped base URL. nova-worker writes
no agent loop of its own.

Keys never reach the agent. An API-rate key goes to a loopback proxy the
runner starts under `nova-secrets exec`; the harness gets the proxy's
address and a placeholder key, and the wall lets it reach only that port.
The proxy injects the key and forwards only to the hosts policy lists for
that route, so wall plus proxy is the egress allowlist where the wall can
deny the network. A harness that logs in (claude, codex on a subscription)
keeps its credentials in its own store, granted read-only into the wall;
that is weaker and is shown as such on the row. The git token is used only
by the runner, outside the wall, after the harness exits.

The probe (LESSONS 4.22): at start and after any harness fault, the runner
runs the adapter's probe job through the same wall, flags and proxy as a
card. The row hooks in only after it passes; a failed probe stops the row's
hook and opens a judgement carrying the probe's last line.

A card can never, on a bench (LESSONS 4.19, 4.20): reach the store or the
bus (no DSN, no bus credentials, no sprint verb in the wall); start a
listening server (the wall refuses listen where it can; the card contract
refuses briefs that ask); write outside its job directory, so no `rm -rf`
of a home and no crawl of `/`, `~` or `/Volumes`; wait on a prompt (stdin
is closed; `sudo` has no terminal). Commit text goes in a file, `git
commit -F`. The coordinator's machine runs no card (LESSONS 4.18):
`nova-worker` refuses a host policy names as a coordinator host.

Timeouts, each a policy number. From v1, measured: stage 120 s (copies ran
73 to 79 s median under load); launch grace 15 s, three launches, 5 to 20 s
then 30 to 60 s jittered (9 of 40 launches died on a provider 5xx);
terminate grace 3 s. From nova-config: the card's deadline per tier.
Unmeasured until the first dogfood sprint: rebase and re-test 2 min; push
60 s; `gh` 30 s; bus send 5 s; ssh connect 10 s; CI wait, the repository's
job timeout plus 10 min (v1's land deadline). No silence kill: v1's
silence heuristic once deleted a running card's home and temp directory.

Restart loses nothing. The store is the truth; the runner keeps only
`job.json` per job (consumer, gen, attempt, result id, harness pid and
nonce). On start it lists its job directories and runs `queue --as` per
row. A `working` consumer whose harness is alive is adopted by pid and
nonce, its deadline from the recorded start. One whose harness is gone is
harvested and reported `fault`. One with a recorded result id is reported
again under that id. A `working` consumer with no job directory is reported
`fault` ("runner lost the job"). A job directory whose consumer is final is
cleaned. Ready consumers are taken as at a wake. The runner gates on no
other daemon (LESSONS 4.25): with the store unreachable its harnesses run
on and its reports wait to retry.

Logs: one line per step (consumer, result, duration) in
`~/.local/state/nova-worker/<row>.log`, rotated by size; harness output in
`<job>/harness.log`, whose last 4 KB become the report's reason text when
the result is not `ok`. Nothing a log holds is sent on the bus.

Deferred, named (README, "Two ideas from the field"; Glenn: "not right now,
but yes"): containment running each card in a microVM or container with an
egress allowlist enforced outside the guest, for hosts whose wall cannot
deny the network and for untrusted tests. It fits the `Harness` and wall
seams; it is not built now.

## Guarantees

1. No edge runs while the store's writer lock is held.
2. No message is skipped: a cursor passes an event only after nova-bus
   confirmed its send. A repeat is possible and harmless.
3. A bus message is at most 4 KB, with no log, diff, brief or secret; the
   engine never reads a bus message's content.
4. Base advances only to a sha whose checks passed at that exact sha, from
   the base tip it was gated on; a moved base is a re-merge and one gate.
5. A group lands whole or not at all; a gate that did not run blames no head.
6. A card's branch is never rebased after its report.
7. One live harness per job directory; a job directory is never reused.
8. A harness runs only inside a nova-reserve slot, freed when it ends.
9. A job directory is removed only after its report is recorded and its
   head is on origin; committed work is pushed before any non-ok end.
10. A report is recorded at most once per result id, retried until recorded
    or refused.
11. A fault (harness, bench, staging, provider, rate limit, restart) spends
    no attempt and no bounce.
12. No key or DSN is in the wall's environment or files.
13. Every timeout is a policy number with a stated default; every refusal
    names its fix.

## The models

`tla/LandEdge.tla`, one landing attempt per stream. Variables: `base` (a
sequence of shas, each with its gate result), `stream`, `group` (heads in
score order), `step` (`idle`, `fetched`, `merged`, `pushed`, `gating`,
`green`, `red`, `fault`, `landed`, `refused`), `gatedAt` (the base tip and
sha a green belongs to), `landed`. Actions: Fetch, Merge, MergeConflict,
PushStream, Gate, CIGreen, CIRed, CIFault, BaseMoves (the environment, at
most K times), Land, Remerge, Refuse. Invariants: every sha on `base` was
gated green at that sha from its parent tip; a head lands at most once; a
group is all in `landed` or none; a fault blames no head; `stream` equals
`base` after every Land. Liveness, with weak fairness on the lander and
BaseMoves finite: a conflict-free green group lands. Reversed witnesses:
Land without the up-to-date check (stale green); Land at another sha than
the gated one; a partial Land; CIFault blaming a head; Remerge sending heads
back to their cards (the chase, a liveness failure). Size: 2 streams, 3
heads, K = 2, CI green, red, fault.

`tla/Runner.tla`, one bench. Variables: each consumer's store state
(`ready`, `working`, `done`, `fail`), its job state (`none`, `taken`,
`staged`, `running`, `harvested`, `rebased`, `ciwait`, `reporting`,
`recorded`, `cleaned`), slots in use, width, the job directory (exists,
result id, pushed head), harness alive, the store's applied op ids. Actions:
Wake (duplicated, lost), Take, Stage, StageFail, Run, Die, Harvest, Rebase,
Conflict, Push, Report, ReplyLost, Clean, Withdraw, Crash and Restart (at
any step), StoreDown. Invariants: guarantees 7 to 11; `working` within
width; slots within capacity. Liveness: every `working` consumer of the row
ends recorded or withdrawn. Reversed witnesses: a slot freed while its
harness lives; a reused job directory; Clean before Report (work lost on a
crash); a fresh result id on retry (two reports); Restart taking a working
consumer as new (two harnesses on one card); a Die classified `fail` (a
fault that bounces). Size: 2 consumers, width 2, 1 slot, 1 Crash.

`tla/Push.tla`, the cursor: events, cursor, sends, lost replies, a crash.
Invariant: every event below the cursor was confirmed sent. Witness: the
cursor set before the send's id came back (a skip). Size: 3 events, 1 crash.

## Tests

Unit (no clock, sockets, network or outside program; time is an argument;
clocks start at 1000): `bus.Fake` records and fails on command; `git.Fake`
is an in-memory commit graph with conflicts by path; `forge.Fake` holds PRs
and check states and refuses a stale merge as GitHub does; `reserve.Fake`;
the sandbox argv; every adapter's `Command` and `Classify`; the runner's
lifecycle driven step by step against them, every refusal with its text.

Functional, each test under two minutes (longer runs are owed, never cut
and called a pass): a throwaway bare repository in `t.TempDir()` as origin;
a throwaway Redis the test starts on a bench (`redis-server` on a free port,
skipped with a clear message where absent) with the real `nova-bus`; a
throwaway PostgreSQL by layer 1's recipe; a script harness that commits
fixed changes, dies on cue or sleeps; the real `nova-reserve` with a
temporary `--dir`; the real `nova-sandbox`, skipped where `nova-sandbox
check` fails; the forge on a scratch GitHub repository with a workflow that
finishes in seconds. Every test isolated and `t.Parallel()`, run on a bench,
never on the working machine.

## Size

An estimate from the interfaces and v1's runner, lines of Go without tests:
`edge.Exec` 150; bus edge 350; git edge 800 (ledger scripts are counted
with their repositories); GitHub edge 500; nova-worker 1,900 (lifecycle
600, staging 300, adapters 400, key proxy 250, restart and logs 350); fakes
450. About 4,100. v1's `pkg/swarm` alone is 8,227 non-test lines at
`origin/main` (measured); here the bus, wall and slots are exec'd.

## Lessons placed (LESSONS section 4)

1. Pushes by exec of nova-bus only: the bus edge (already in CORRECT-USE).
2. Self-contained deal, brief by id: the `DEAL` shape; `take` prints it.
3. Retry carries prior head and reason: `DEAL` (`prior=`, `reason=`).
4. Bounded messages: 4 KB, 300-byte fields; guarantee 3.
5. Who redelivers: the cursor resends; the runner retries by result id.
6. Up means a session acts: `Seen` on a verb as the name, never `who` alone.
7. A missed challenge is not down: placed in LAYER-2; unchanged.
8. Verbs attributed to their name: each row runs its verbs `--as <row>`.
9. Grouping: one message per recipient, subject and tick; one per group.
10. CI by event: the run loop reads checks before each tick as evidence.
11. Merge on green at the current base: git edge steps 4, 5; guarantee 4.
12. Stream branch, PR to a protected base: git edge steps 2 to 5.
13. Gate host moves on: the landing host paragraph.
14. Staging from a mirror; its failure a fault: runner step 3.
15. Fresh worktree per card: runner step 3; guarantee 7.
16. The runner does the mechanical work: deal to report, steps 1 to 9.
17. Slot lock per core count: runner step 4 (nova-reserve); guarantee 8.
18. No builds on the coordinator's machine: the coordinator host refusal.
19. Workers start no servers or sprint verbs: "A card can never".
20. No prompts, unchecked deletes or crawls: the same paragraph.
21. Commit identity from the row: runner step 3.
22. Liveness proof on the same path: the probe.
23. Binaries replaced by rename: the runner's install paragraph.
24. Rate limit spends nothing: `fault` here; resting a route is layer 5's.
25. No daemon gates on another: the restart paragraph.

## Built

### Packages and models (3,936 non-test lines across 12 `.go` files; max function 56 lines)

- `edge` (`edge/exec.go`, `edge/request.go`, `edge/exec_test.go`, `edge/coldread_test.go`, `edge/break_test.go`): `Exec` (`OSExec`, `FakeExec`, clock starting at `1000`), `RunArgv`, `ValidateCmd`, `RequestID(epoch, kind, card, sha)`, `EdgeRequest` (carrying `After []string`, `Repo`, `Base`, `PR`, `Mode`, `Ledgers`), `EdgeResult` (stub for `steps.EdgeResult`), and `Deduplicator` (in-flight coalescing by `RequestID` with zero persistent memory after completion). Every child process runs in its own process group (`Setpgid: true`, killed as `-pid` on timeout/cancellation) with stdin closed (`nil`), bounded stdout/stderr ring buffers (`DefaultMaxBytes = 64 KiB`, preserving head and tail), `WaitDelay = 200ms` (`exec.ErrWaitDelay` treated as normal exit when the command exited before timeout), and mandatory policy timeouts.
- `edge/bus` (`edge/bus/bus.go`, `edge/bus/fake_test.go`, `edge/bus/bus_test.go`, `edge/bus/coldread_test.go`, `edge/bus/break_test.go`, `edge/bus/functional_test.go`, `edge/bus/trace_test.go`): `Bus` (`Edge`, `Fake`, clock starting at `1000`). Wraps `nova-bus send` and `nova-bus who` via `edge.Exec` without opening a Redis connection. `BuildRecipientMessages`, `PushRecipient`, and `PushTick` build at most one message per `(recipient, subject)` per tick (`DEAL`, `WITHDRAWN`, `NEEDS YOU`, `LANDED`, `STREAM LANDED`), enforce `<= 4096 B` and `<= 20` card lines with overflow trailer `"and N more: nova-sprint next --as <row>"` and `300 B` field truncation (`"full text: nova-sprint show <id>"`), and advance `push:<recipient>` (`store.SetCursor`) only after `nova-bus send` exits 0 and prints `<recipient> <id>`. `BusReply(sender)` returns `"replies are verbs: run nova-sprint next --as <sender>"`.
- `edge/git` (`edge/git/git.go`, `edge/git/land.go`, `edge/git/fake_test.go`, `edge/git/git_test.go`, `edge/git/coldread_test.go`, `edge/git/break_test.go`, `edge/git/functional_test.go`, `edge/git/trace_test.go`): `Git` (`Client`, `Fake`, clock starting at `1000`) and `Lander` (`LandPass`, `Execute`, `MergeLedgerLines`, `StreamBranch`, `StreamRef`, `ValidateCardBranch`). Implements the 6-step landing pass from `LANDING.md` (fetch, merge group in score order with union ledger merge, push `--force-with-lease=<ref>:<old>`, gate at exact SHA, land only while base tip matches the gated parent, fast-forward stream branch). Per `1794011119389-0` (`LANDING.md` step 1), the stream integration branch is `sprint/<stream>/group` (`refs/heads/sprint/<stream>/group`), coexisting cleanly with card branches `sprint/<stream>/<producer>`, and `ValidateCardBranch` / `Take` refuse a card or producer branch named `group` or bare `sprint/<stream>`.
- `edge/forge` (`edge/forge/forge.go`, `edge/forge/fake_test.go`, `edge/forge/forge_test.go`, `edge/forge/coldread_test.go`, `edge/forge/break_test.go`, `edge/forge/functional_test.go`): `Forge` (`Client`, `Fake`, clock starting at `1000`), `PR`, `OpenPR`, `EnsurePR`, `Checks`, and `Merge`. Queries `statusCheckRollup` via GraphQL at up to `MaxChecksBatch = 100` exact 40-hex commit SHAs per call with context pagination, classifies outcomes (`green`, `red`, `pending`, `fault`, `rate_limit`), verifies PR base and head SHA before fast-forwarding `refs/heads/<base>` in `Merge`, and enforces non-Glenn identity credentials (`GH_TOKEN` / `GH_CONFIG_DIR`) and per-identity token-bucket rate limiting.
- `edge/worker` (`edge/worker/worker.go`, `edge/worker/harness.go`, `edge/worker/proxy.go`, `edge/worker/verbs.go`, `edge/worker/runner.go`, `edge/worker/verbs_test.go`, `edge/worker/worker_test.go`, `edge/worker/coldread_test.go`, `edge/worker/break_test.go`, `edge/worker/functional_test.go`, `edge/worker/trace_test.go`) and `cmd/nova-worker` (`cmd/nova-worker/main.go`, `cmd/nova-worker/main_test.go`, `cmd/nova-worker/break_test.go`):
  - Coordinator host refusal (`CheckHost` refuses `studio`, `studio.local`, and policy-listed coordinator hosts, `LESSONS 4.18`).
  - Adapters (`ClaudeHarness`, `CodexHarness`, `OpenCodeHarness`, `ScriptHarness`) implementing `Harness` (`Name`, `Command`, `Probe`, `Classify`, `Usage`). `cmd/nova-worker` defaults `--harness` to `claude`. `ScriptHarness.Command` passes card fields as positional arguments (`$1`..`$9`) to a static shell program constant, never interpolating card prose into a shell string.
  - Loopback API key proxy (`KeyProxy` in `edge/worker/proxy.go`): binds strictly to `127.0.0.1` (`CheckLoopbackAddr`), enforces the per-route `AllowedHosts` allowlist (`403 Forbidden` on unlisted hosts), and injects the real `Authorization: Bearer <key>` header outside the sandbox while the harness inside the wall receives only `OPENAI_API_KEY=nova-proxy-placeholder` (`Guarantee 12`).
  - Sandbox & slot wall (`BuildWallArgv`, `SanitizeWallEnv`, `OSReserveWall`): both `OSReserveWall.RunHarness` and `OSReserveWall.RunTest` always wrap execution in `nova-reserve run ... --wait <d> --why <consumer> -- nova-sandbox --write <jobDir> --read <toolchains...> --net-deny --net-allow 127.0.0.1:<proxyPort> -- <argv...>` via `edge.Exec`. `SanitizeWallEnv` enforces an exact variable-name allowlist (`PATH`, `LANG`, `LC_ALL`, `GIT_TERMINAL_PROMPT`, `NOVA_WORKER_CONSUMER`, `NOVA_WORKER_ROW`, `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENAI_BASE_URL`, `OPENAI_API_KEY` only when equal to `ProxyPlaceholderKey`, plus `HOME`, `TMPDIR`, and `XDG_DATA_HOME` rooted inside `jobDir`), dropping `DATABASE_URL`, `SSH_AUTH_SOCK`, `NOVA_SPRINT_DSN`, `NOVA_PG_DSN`, `PGPASSWORD`, `GITHUB_TOKEN`, `GH_TOKEN`, provider API keys, and any unlisted variable.
  - 9-step runner lifecycle (`RunConsumer`, `StepRow`, `ProbeRow`, `RecoverRow`, `QueryEdge` in `edge/worker/runner.go`):
    * `FormatTakeOpID(epoch, consumer, gen, row)` (`take-e<epoch>-<consumer>-g<gen>-<row>`) and `FormatResultID(epoch, consumer, attempt, gen)` (`res-e<epoch>-<consumer>-a<attempt>-g<gen>`) fence store operations by epoch, consumer, attempt, and gen.
    * `StepRow` launches ready consumers up to `Width` concurrently, tracks in-flight consumers so concurrent wakes never double-launch or clobber a running job directory, and cancels/kills any in-flight consumer whose store state becomes `fail` (`WITHDRAWN`).
    * `OSGitOps` executes all bench `git` operations through `edge.Exec` with `Timeouts` (`Stage`, `Push`, `RebaseAndTest`); `RebaseOntoStream` returns `*FetchError` on a fetch failure so `RunConsumer` propagates the failure without reporting a stale head.
    * `sendReportAndClean_internal` requires an accepted store receipt matching the result ID (`receipt.Accepted && receipt.Op == req.ResultID`) before marking `JobRecorded` or removing the job directory; `CLISprintClient` classifies exit code `1` as `ErrRefused` (never retried as `ErrStoreDown`) and exit code `2` as `ErrStoreDown`.
    * `RecoverRow` matches disk `JobRecord` entries against the store's working summary by full identity `(epoch, consumer, attempt, gen)` (`jobMatchesSummary_internal`); a mismatch is kept on disk for requery and never adopted or overwritten; a job recovered at `JobHarvested` with `Outcome.Result == "ok"` completes `rebaseAndPRSteps_internal` (rebase onto stream, `DoneWhen` tests, post-rebase push, PR open) before reporting; `QueryEdge` is a pure read of the store and `job.json` with zero side effects.
- `tla/Push.tla`, `tla/LandEdge.tla`, `tla/Runner.tla` and 12 reversed witnesses in `tla/witnesses/` (`PushCursorBeforeConfirm`, `LandEdgeStaleGreen`, `LandEdgeWrongSHA`, `LandEdgePartialGroup`, `LandEdgeFaultBlamesHead`, `LandEdgeChaseBack`, `RunnerSlotFreedEarly`, `RunnerReusedJobDir`, `RunnerCleanBeforeReport`, `RunnerFreshResultIDOnRetry`, `RunnerRestartSpawnsSecondHarness`, `RunnerDieClassifiedFail`).

### Seam rulings (Rowan, 2026-11-06)

1. **Wake-up via `next --as <row> --json`, reports via `report` (`1794007469002-0`)**: There is no `queue` verb. On wake-up and on restart, `nova-worker` calls `SprintClient.Next(ctx, row)` (`nova-sprint next --as <row> --json` at the CLI, or `StoreSprintClient.Next` over `store.Snapshot` in-process) and acts on the row's `working` and `ready` consumers; `DEAL` overflow trailers end with `"and N more: nova-sprint next --as <row>"`. Reports apply directly to the store through `SprintClient.Report` (`StoreSprintClient.Report` / `nova-sprint report --as <row>`) fenced on `Op.ID = ResultID`, never into a tick inbox.
2. **Pre-satisfied `After []OpID` and `EdgeResult` (`1794007773302-0`)**: Layer 3's `EdgeRequest` carries `After []OpID` (`After []string`); `run` dispatches an `EdgeRequest` to Layer 4 only after every operation in `After` has an accepted store receipt, so edges never see a request whose store precondition failed. Each edge returns `EdgeResult` (`steps.EdgeResult`) for the next tick's inbox.
3. **Every edge fact is re-queryable by `(epoch, kind, card, sha)` idempotently (`1794007844911-0`, `1794011583522-0`)**: Request IDs are `RequestID(epoch, kind, card, sha)` (`sha256(epoch + "\x00" + kind + "\x00" + card + "\x00" + sha)`). In-flight duplicate requests with the same `RequestID` are deduplicated inside the edge (`edge.Deduplicator`), not by `run`. Every edge answers requests as pure queries of the world at `(epoch, kind, card, sha)` without relying on process memory (`edge/forge` queries GitHub check runs at `sha`; `edge/git` checks ancestry and base tip on origin; `edge/worker` inspects `job.json` and the store). Restarting the edge struct or process and querying `(epoch, kind, card, sha)` again returns the identical `EdgeResult`.
4. **Stream settings arrive in the edge request (`1794008719695-0`)**: A stream's `repo`, `base` branch, `pr`-required flag, and landing `mode` are policy data (a policy row of kind `data` named `stream:<name>`, JSON `{repo, base, pr, mode}`). Layer 3 puts `base` and `pr` into each landing `EdgeRequest`, so the git and forge edges take them as arguments and never read the store for them.

### Timeouts and bounds (`measured` vs `guess`)

| Parameter | Default | Status | Basis |
| :--- | :--- | :--- | :--- |
| `StageTimeout` | `120s` | **measured** | v1 mirror worktree staging ran 73–79 s median under load |
| `LaunchGrace` | `15s` (3 tries, `5–20s` then `30–60s` jitter) | **measured** | 9 of 40 launches in v1 died on a provider 5xx within 15 s |
| `TerminateGrace` | `3s` | **measured** | Process-group `SIGTERM` -> `SIGKILL` grace |
| `MaxMessageBytes` / `MaxFieldBytes` | `4096 B` / `300 B` | **measured** | v1 bus size discipline (`Guarantee 3`) |
| `MaxLogTailBytes` | `4096 B` | **measured** | Harness failure tail carried in report reason |
| `RebaseTestBound` | `2m` | **guess** | Unmeasured until the first dogfood sprint |
| `PushTimeout` | `60s` | **guess** | Unmeasured until the first dogfood sprint |
| `ForgeTimeout` (`gh`) | `30s` | **guess** | Unmeasured until the first dogfood sprint |
| `BusSendTimeout` | `5s` | **guess** | Unmeasured until the first dogfood sprint |
| `SSHConnectBound` | `10s` | **guess** | Unmeasured until the first dogfood sprint |
| `CIWaitMargin` | `10m` | **guess** | v1 land deadline margin over repo job timeout; unmeasured in v2 |

### Deferred containment option: microVM / container wall

For bench hosts whose OS sandbox (`nova-sandbox`) cannot enforce `--net-deny` at the kernel level, or for cards executing untrusted code, containment runs each job inside a disposable microVM (Firecracker / KVM on Linux benches, `Virtualization.framework` on macOS benches) or rootless network-isolated container (`bwrap` + network namespace) behind the existing `ReserveWall` and `Harness` seams:

1. **Seam**: `MicroVMWall` implements `worker.ReserveWall` (`RunHarness` and `RunTest`). `Runner`, `Harness`, `KeyProxy`, and `job.json` recovery are unchanged.
2. **Filesystem isolation**: `MicroVMWall` boots a read-only root image exporting `WallConfig.ReadPaths` (toolchains and staged siblings) and mounts only `Job.JobDir` read-write via virtiofs at the same path, with `HOME`, `TMPDIR`, and `XDG_DATA_HOME` inside `Job.JobDir` (`Guarantee 7`).
3. **Host-enforced egress allowlist**: The guest NIC attaches to a host tap/vsock bridge where host firewall rules (outside the guest kernel) drop all outbound traffic except TCP to the host loopback `KeyProxy` (`127.0.0.1:<ProxyPort>`). Credentials stay in `KeyProxy` on the host (`Guarantee 12`), which validates the target `Host` against the route's `AllowedHosts` before injecting the Bearer key.
4. **Lifecycle**: The microVM runs inside the `nova-reserve run --slots <weight>` lease and is destroyed before the slot is released (`Guarantee 8`).
