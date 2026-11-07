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

(Stitch, 2026-11-07: this section's pusher is run's since drift row 6; the
edge keeps `Send` and `Who`. See Built, "Stitch".)

`Bus` has `Send(to, subject, body)` over `nova-bus send` and `Who()` over
`nova-bus who`. The run loop sends as its own name (`sprint`). nova-bus
refuses a send from a name that is not hooked, so `up` hooks it with
`nova-bus hook --as sprint --service --exec 'nova-sprint bus-reply'`;
`bus-reply` reads only the sender, the message it answers and the subject
(verbs.BusAnswer): the sprint's pushes are receipts, so a reply is never
answered and a message that is not a verb is ignored (one log line each); a
verb is answered once with where to run it. The engine never parses a bus message; every
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

Clone addresses. A stream keeps its repository as it was given (`<org>/<repo>`
is the forge's name for it: `gh -R`); `git.CloneURL(repo, forge)` is the one
place it becomes what git clones and ls-remotes. With forge `github` an
`<org>/<repo>` is `https://github.com/<org>/<repo>.git`; a URL, an scp-like
address or a path passes as it is; with no forge the repo is its own (a hand
stream names its repository by URL or path); a form a forge cannot address
is refused naming the form. Run's land and heads clone through it (one clone
per address under `ReposDir`), `stream add`, `finish` and `check` read tips
at it (`Client.Tip`: one `git ls-remote <address> refs/heads/<branch>`, no
clone). The scheme is https, not ssh: the sprint's GitHub identity is the
host's gh login (w3L: or the `GH_GATE_TOKEN` override), the forge edge
already runs as it, and an ssh key registered with GitHub is a second
credential to keep. The git edge makes `gh auth git-credential`, run with
every GitHub credential of the process unset (`env -u ...`), git's one
credential helper for `https://github.com` (`Config.GitHubCredential`,
through `GIT_CONFIG_COUNT`; an empty helper first clears the host's own
list, so a person's keychain never answers for the sprint); the
environment names no secret.

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

`Forge` has `PR(head, base)` (find), `OpenPR`, `Checks(shas)`,
`Merge(pr, sha)`, `FailedLines(url)`, `Inheritance(base, baseTip, head)`
(a red head's failing workflows split by the base) and `Authors(pr)` (see
Built, "w3L").

Credentials. The landing's `gh` runs as the host's own gh login (the run's
host; intent rule 32, 2026-11-07: "the run does the forge work with the
host's gh identity"). `GH_GATE_TOKEN`, when set in run's environment, is an
optional override: gh then gets it as `GH_TOKEN` in an environment with no
other GitHub credential. A row's gh, where a row uses one, runs with its
own `GH_TOKEN` from nova-secrets. The human's credentials are
never used. A row's token is minted by nova-tools for that row, scoped
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
   second channel (LESSONS 4.1). A periodic wake (every 10 s, a guess) runs
   a pass beside the hook's. A pass only plans: it recovers the row's jobs
   and takes; each job then runs on its own, so no pass waits on a job
   (defect 66, w3W below).
2. **Take** each ready consumer up to width: `nova-sprint take <id>@<gen>
   --as <row>` prints the brief, base, branch, done-when and tier.
3. **Stage** a worktree at `<root>/jobs/<consumer>/repo` from the bench's
   work clone, which borrows the mirror's objects and fetches from the
   mirror, never the network; never from the mirror itself, whose pruning
   fetch would delete branch refs. The worktree's commit identity is the
   row's, never the machine's (LESSONS 4.21). Siblings the card reads are
   staged read-only the same way. A staging failure or timeout is a `fault`
   (LESSONS 4.14). A job directory that exists is refused, so none is
   reused (LESSONS 4.15). Each repo a pass stages from is fetched once per
   pass, before its jobs stage (defect 72). The worker writes `JOB.md` in the
   job directory: the card, repo, base, checkout, paths, done-when and how
   the card finishes (defect 68).
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
7. **CI**: open no PR (the landing opens the stream's group PR, defect 34);
   CI is read by the landing on the group sha, not by the worker.
8. **Report** with a result id written to `job.json` before the first try;
   retry the verb with that id until it exits 0 or 1 (LESSONS 4.5).
9. **Clean** the job directory only after the report is recorded and the
   head is on origin; a worktree with uncommitted changes is logged and kept.

Results, classified by the adapter. `ok`: the result file says ok and a head
past `base_sha` and `prior_head` is pushed (a harness that said ok and left
its changes uncommitted has its tree committed by the worker). `fail`: the
result file says broken, no result file ("fail: no verdict" with the
harness's last output), a rebase conflict, the card's tests red. `fault`:
exit 134 or 137; a launch that died inside the 15 s grace on a provider 5xx,
three times; a rate-limit or credit refusal; a staging failure. A read
reports `ok`, `fix` with its finding, or `fault` (no result file is a
fault), never `fail`; a read's provider failure is a fault and spends no
reader (LPM T30). A job the worker's own stop cuts short is "runner
stopped": not reported, kept to resume.

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
card. The row hooks in only after it passes; a failed probe leaves the row
unhooked and held with the probe's reason, and is retried after a backoff
(30 s doubling to 10 min, guesses); it never exits the served worker.

A card can never, on a bench (LESSONS 4.19, 4.20): reach the store or the
bus (no DSN, no bus credentials, no sprint verb in the wall); start a
listening server (the wall refuses listen where it can; the card contract
refuses briefs that ask); write outside its job directory, so no `rm -rf`
of a home and no crawl of `/`, `~` or `/Volumes`; wait on a prompt (stdin
is closed; `sudo` has no terminal). Commit text goes in a file, `git
commit -F`. Which machines run cards is nova-config's bench role and
nothing else: the coordinator's machine runs a worker when it is a bench
(2026-11-07; LESSONS 4.18's refusal is gone).

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
resumed from its take receipt (`receipt.json`): staged again if it was
taken, run again if it was staged or running, harvested if it left its
result file; no fault. One with a recorded result id is reported again
under that id. A `working` consumer with no job directory, or one whose
job cannot be rebuilt, is released with no blame (`nova-sprint release`,
with the partial head it pushed). A job directory whose consumer is final
is cleaned. A record the sprint refuses three times the same way is moved
to `<root>/quarantine/` and raised. Ready consumers are taken as at a wake. The runner gates on no
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

`tla/LandEdge.tla` (rewritten 2026-11-07 for intent rules 16 and 32), the
ladder from a tick's land request to the base. Cards c1, c2 of stream s1
and c3 of s2, every branch cut from the first base commit; a tree is the
set of cards it holds and its count of outside commits. CI is truthful (a
tree with c1 and c3 is red; c2 with c3 does not merge) and may report
nothing. Variables: `base` (a sequence of trees, each with how it got there,
`init`, `auto`, `hand` or `ext`, and the tree CI gated), `cs` (merging,
landed, fix), `req` (each stream's request: cards, sent or done, outcome,
rung), `js` (cards under a land judgement), `held`, `ticked`, `passes`.
Actions: Tick (layer 3: consume results, landed moves the cards, anything
else judges them; ask one card while fewer than BackedUp wait, all of them
when backed up), Pass (one `LandPass`: on base already; rung 1 or 2 by the
heads and the tip; conflict; CI green merges the gated tree exactly, or
GitHub refuses, or base moves under the gate and the pass runs again),
BaseMoves (at most K), Answer (land by hand, fix, retry). Invariants:
`BaseNeverRed`, `NothingLandsUnpassed`, `MergingCovered`; one reversed
witness each (`LandEdgeRedLands`, `LandEdgeStaleGate`,
`LandEdgeJudgementHoldsStream`); reach checks for rung 2, a judgement, a
hand landing and a red gate. Size: 2 streams, 3 cards, K = 1, BackedUp 2,
MaxPass 6. The code is bound to it by `LandEdgeTrace.tla` and the walk in
`edge/git/trace_test.go`.

`tla/Runner.tla`, one bench. Variables: each consumer's store state
(`ready`, `working`, `done`, `fail`), its job state (`none`, `taken`,
`staged`, `running`, `harvested`, `rebased`, `ciwait`, `reporting`,
`recorded`, `cleaned`), slots in use, width, the job directory (exists,
result id, pushed head), harness alive, the store's applied op ids. Actions:
Wake (duplicated, lost), PassStart and PassEnd (the pass, a process of its
own: only a pass takes, and it ends whatever its jobs do), Take, Stage,
StageFail, Run, Die, Harvest, Rebase, Conflict, Push, Report, ReplyLost,
Clean, Withdraw, Release (the restart's no-blame give-back), Stop (the
worker's own stop: harnesses killed, jobs kept at staged), Crash and
Restart (at any step), StoreDown. Invariants: guarantees 7 to 11; `working`
within width; slots within capacity; `PassNeverWaitsOnJobs`;
`StopIsNeverAFault`. Liveness: every `working` consumer of the row
ends recorded or withdrawn. Reversed witnesses: a slot freed while its
harness lives; a reused job directory; Clean before Report (work lost on a
crash); a fresh result id on retry (two reports); Restart taking a working
consumer as new (two harnesses on one card); a Die classified `fail` (a
fault that bounces); a pass that waits on its jobs (`RunnerPassWaitsOnJobs`);
a stop reported as a fault (`RunnerStopIsAFault`). Reach checks: a take
while another job runs, a stop, a stopped job back in a pass, a release.
Size: 2 consumers, width 2, 1 slot, 1 Crash.

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

### Packages and models

- `edge` (`edge/exec.go`, `edge/request.go`, `edge/exec_test.go`, `edge/coldread_test.go`, `edge/coldread2_test.go`, `edge/break_test.go`): `Exec` (`OSExec`, `FakeExec`, clock starting at `1000`), `RunArgv`, `ValidateCmd`, `RequestID(epoch, kind, card, sha)`, `EdgeRequest` (carrying `After []string`, `Repo`, `Base`, `PR`, `Mode`, `Ledgers`), `EdgeResult` (stub for `steps.EdgeResult`), and `Deduplicator` (in-flight coalescing by `RequestID` with zero persistent memory after completion). Every child process runs in its own process group (`Setpgid: true`, killed as `-pid` on timeout/cancellation) with stdin closed (`nil`), bounded stdout/stderr ring buffers (`DefaultMaxBytes = 64 KiB`, preserving head and tail), `WaitDelay = 200ms` (`exec.ErrWaitDelay` treated as normal exit when the command exited before timeout), `OnStart(pid)` callback, and mandatory policy timeouts.
- `edge/bus` (`edge/bus/bus.go`, `edge/bus/bus_test.go`, `edge/bus/functional_test.go`): `Bus` (`Send`, `Who`) and `Edge`, which runs `nova-bus send` and `nova-bus who` through `edge.Exec`. `Send` refuses a body over 4096 bytes before nova-bus runs and returns the message id only when nova-bus exits 0 and prints `<recipient> <id>`; `Who` parses `WHO OK present=<n>` and its tab lines. The package holds no cursor and reads no store: what to send, to whom, and when the cursor moves are run's (layer 6, the one pusher; stitch, below).
- `edge/git` (`edge/git/git.go`, `edge/git/land.go`, `edge/git/fake_test.go`, `edge/git/git_test.go`, `edge/git/coldread_test.go`, `edge/git/coldread2_test.go`, `edge/git/break_test.go`, `edge/git/functional_test.go`, `edge/git/trace_test.go`): `Git` (`Client`, `Fake`, clock starting at `1000`) and `LandPass` (`StreamBranch`, `StreamRef`, `ValidateCardBranch`, `RunRemoteLandStep`). Implements the 6-step landing pass from `LANDING.md` (fetch, merge group in score order with union ledger merge, push `--force-with-lease=<ref>:<old>`, gate at exact SHA, land only while base tip matches the gated parent, fast-forward stream branch). Per `1794011119389-0` (`LANDING.md` step 1), the stream integration branch is `sprint/<stream>/group` (`refs/heads/sprint/<stream>/group`), coexisting cleanly with card branches `sprint/<stream>/<producer>`, and `ValidateCardBranch` / `Take` refuse a card or producer branch named `group` or bare `sprint/<stream>`.
- `edge/forge` (`edge/forge/forge.go`, `edge/forge/fake_test.go`, `edge/forge/forge_test.go`, `edge/forge/coldread_test.go`, `edge/forge/break_test.go`, `edge/forge/functional_test.go`): `Forge` (`Client`, `NewClient`, `Fake`, clock starting at `1000`), `PR`, `OpenPR`, `EnsurePR`, `Checks`, `Merge`, and `RestedUntil`. Queries `statusCheckRollup` via GraphQL at up to `MaxChecksBatch = 100` exact 40-hex commit SHAs per call with context pagination, classifies outcomes (`green`, `red`, `pending`, `fault`, `rate_limit`), verifies PR base and head SHA before fast-forwarding `refs/heads/<base>` in `Merge`, and enforces non-Glenn identity credentials (`GH_TOKEN` / `GH_CONFIG_DIR`) and per-identity token-bucket rate limiting.
- `edge/worker` (`edge/worker/worker.go`, `edge/worker/harness.go`, `edge/worker/proxy.go`, `edge/worker/verbs.go`, `edge/worker/runner.go`, `edge/worker/verbs_test.go`, `edge/worker/worker_test.go`, `edge/worker/coldread_test.go`, `edge/worker/coldread2_test.go`, `edge/worker/break_test.go`, `edge/worker/functional_test.go`, `edge/worker/trace_test.go`) and `cmd/nova-worker` (`cmd/nova-worker/main.go`, `cmd/nova-worker/main_test.go`, `cmd/nova-worker/coldread2_test.go`, `cmd/nova-worker/break_test.go`):
  - No host refusal: nova-config's bench role is the only word on where a worker runs, the coordinator's machine included (2026-11-07; `CheckHost` and `--coordinator-hosts` are gone, below). `--as <row>` is the one mandatory flag.
  - Adapters (`ClaudeHarness`, `CodexHarness`, `OpenCodeHarness`, `ScriptHarness`) implementing `Harness` (`Name`, `Command`, `Probe`, `Classify`, `Usage`). `cmd/nova-worker` defaults `--harness` to `claude`. `ScriptHarness.Command` passes card fields as positional arguments (`$1`..`$9`) to a static shell program constant, never interpolating card prose into a shell string. `ClassifyJob` treats `<job>/result.json` as authoritative once written and classifies provider HTTP 402/429/5xx faults from `KeyProxy` (`ProxyStatus`) or structured harness JSON events, never by substring-scanning agent prose or test output.
  - Loopback API key proxy (`KeyProxy` in `edge/worker/proxy.go`): binds strictly to `127.0.0.1` (`CheckLoopbackAddr`), holds one route per provider (`--providers`), reads each route's key from the worker's environment (`EnvKey`), enforces the per-route `AllowedHosts` allowlist (`403 Forbidden` on unlisted hosts), records upstream 402/429/5xx statuses (`RecordStatus` / `ConsumeStatus`), and injects the real `Authorization: Bearer <key>` header outside the sandbox while the harness inside the wall receives only `OPENAI_API_KEY=nova-proxy-placeholder` (`Guarantee 12`).
  - Sandbox & slot wall (`BuildWallArgv`, `SanitizeWallEnv`, `OSReserveWall`): both `OSReserveWall.RunHarness` and `OSReserveWall.RunTest` (which runs each `DoneWhen` line as the argv `edge.ParseGate` gives (the one parser admission uses too; a line with shell syntax outside quotes is an error and is never run)) always wrap execution in `nova-reserve run ... --wait <d> --why <consumer> -- nova-sandbox --write <jobDir> --read <toolchains...> [--unix] [--net-deny [--net-allow 127.0.0.1:<proxyPort>]] --env <name>... -- <argv...>` (the adapter's `Needs` decide `--unix` and the network, below) via `edge.Exec`, recording the child PID and `NOVA_WORKER_NONCE` at start via `Cmd.OnStart` and granting `--write` only to `JobDir` (shared `.git` and alternates are `--read` only). `SanitizeWallEnv` enforces an exact variable-name allowlist (`PATH`, `LANG`, `LC_ALL`, `GIT_TERMINAL_PROMPT`, `NOVA_WORKER_CONSUMER`, `NOVA_WORKER_ROW`, `NOVA_WORKER_NONCE`, `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENAI_BASE_URL`, `OPENAI_API_KEY` only when equal to `ProxyPlaceholderKey`, plus `HOME`, `TMPDIR`, and `XDG_DATA_HOME` rooted inside `jobDir`).
  - 9-step runner lifecycle (`RunConsumer`, `StepRow`, `ProbeRow`, `RecoverRow`, `RunWakeLoop`, `QueryEdge` in `edge/worker/runner.go`):
    * `FormatTakeOpID(epoch, consumer, gen, row)` (`take-e<epoch>-<consumer>-g<gen>-<row>`) and `FormatResultID(epoch, consumer, attempt, gen)` (`res-e<epoch>-<consumer>-a<attempt>-g<gen>`) fence store operations by epoch, consumer, attempt, and gen (`CLISprintClient.Take` passes `--id`).
    * `StepRow` launches ready consumers up to `Width` concurrently, tracks in-flight consumers so concurrent wakes never double-launch or clobber a running job directory, and cancels/kills any in-flight consumer whose store state becomes `fail` (`WITHDRAWN`).
    * `OSGitOps` executes all bench `git` operations through `edge.Exec` with `CleanEnv: true`, `-c core.hooksPath=/dev/null -c core.fsmonitor=false`, and `Timeouts` (`Stage`, `Push`, `RebaseAndTest`); `RebaseOntoStream` returns `*FetchError` on a fetch failure so `RunConsumer` propagates the failure without reporting a stale head.
    * `sendReportAndClean_internal` requires an accepted store receipt matching the result ID (`receipt.Accepted && receipt.Op == req.ResultID`) before marking `JobRecorded` or removing the job directory; `CLISprintClient.Report` returns a parse error if `--json` output fails to unmarshal, and a report the sprint did not acknowledge with exit 0 (`ErrRefused` included) keeps its job record at `reporting` and is sent again, same id, on the next wake (stitch, row 20).
    * `RecoverRow` matches disk `JobRecord` entries against the store's working summary by full identity `(epoch, consumer, attempt, gen)` (`jobMatchesSummary_internal`), verifies both PID and `NOVA_WORKER_NONCE` via `OSProcessTable.Alive`, kills and reports `fault` for any adopted harness past its recorded `DeadlineAt`, leaves `JobTaken`/`JobStaged` (`ErrNoSlot`) records untouched for retry, and completes `rebaseAndPRSteps_internal` for `JobHarvested` `"ok"` jobs before reporting; `QueryEdge` is a pure read of the store and `job.json` with zero side effects.
- `tla/Push.tla`, `tla/LandEdge.tla` (rewritten 2026-11-07 for the three rungs; its witnesses are now `LandEdgeRedLands`, `LandEdgeStaleGate`, `LandEdgeJudgementHoldsStream`, below), `tla/Runner.tla` and, as first built, 12 reversed witnesses in `tla/witnesses/` (`PushCursorBeforeConfirm`, `LandEdgeStaleGreen`, `LandEdgeWrongSHA`, `LandEdgePartialGroup`, `LandEdgeFaultBlamesHead`, `LandEdgeChaseBack`, `RunnerSlotFreedEarly`, `RunnerReusedJobDir`, `RunnerCleanBeforeReport`, `RunnerFreshResultIDOnRetry`, `RunnerRestartSpawnsSecondHarness`, `RunnerDieClassifiedFail`).

### Seam rulings (Rowan, 2026-11-06)

1. **Wake-up via `next --as <row> --json`, reports via `report` (`1794007469002-0`)**: There is no `queue` verb. On wake-up and on restart, `nova-worker` calls `SprintClient.Next(ctx, row)` (`nova-sprint next --as <row> --json` via `CLISprintClient.Next`) and acts on the row's `working` and `ready` consumers; `DEAL` overflow trailers end with `"and N more: nova-sprint next --as <row>"`. Reports apply directly to the store through `SprintClient.Report` (`CLISprintClient.Report` / `nova-sprint report --as <row>`) fenced on `Op.ID = ResultID`, never into a tick inbox.
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

### Stitch (2026-11-07, the stitch on v2-stitch, merging round 3 at 833d094a)

What the stitch changed in layer 4 and why, row by row of DRIFT-ROUTING:

- **Row 6, one pusher.** Round 3 had not deleted it: `edge/bus` still held
  `BuildRecipientMessages`, `PushRecipient`, `PushTick`, the subject table and
  the `push:<name>` cursor writes. Deleted, with their tests, the `Push` model,
  `PushTrace`, the `PushCursorBeforeConfirm` witness and the push walk in
  `tla/make-traces.sh`. The cursor rule (move only after a confirmed send) is
  run's and is checked by `Run.tla` at layer 6. `BusReply` and `TruncateField`
  had no caller and went too.
- **Row 10.** Verified: `LandPass` refuses a stream with no forge, and a stream
  in mode `hand`, naming the fix.
- **Forge ruling (Stella's handoff, 1794020909312).** No separate PR setting:
  a stream with a forge lands through a PR and its CI, so `LandRequest.RequirePR`
  is gone and a forge always means a PR; `EdgeRequest.PR bool` is now
  `EdgeRequest.Forge string`.
- **Row 27, a red result always blames a head.** Round 3 bisected only when
  `BisectOnRed` was set and returned an unblamed red for "cross-card red
  interaction" and for red after a remerge on a moved base. Steps consumes a
  red with no blame and reissues the same request, so a group red only
  together would be landed and refused forever. Now a group of more than one
  always bisects; two halves green alone and red together blame the right head
  (the one whose arrival made it red; the left is green); a red after the
  remerge bisects the same way. `TestRedTogetherGreenAloneBlamesTheLaterHead`.
- **Rows 28, 29, 34, dead code.** `RequestID` carried a switch with no body
  that existed only to make the dead-code probe count `FactCI`, `FactLand` and
  `FactRunner` as used; the kinds are layer 3's (`steps.EdgeRequest.Kind`:
  `land`, `stop`; lower layer defines the seam), so the constants and the
  switch are deleted. `LandRequest.PassedTrees` was cleared on entry and never
  read: deleted.
- **Worker, rows 14, 15, 20 (done on v2-stitch-worker).** The worker reads its
  sprint only through verbs, and the take receipt is the seam
  (`SEAMS.md`, section 6).
  * *Row 14.* `NewRunner` no longer defaults `Now` or `Sleep`: a runner with
    no clock is refused naming both, `cmd/nova-worker` passes `time.Now` and
    `time.Sleep`, and every unit test passes a fake clock. The job record's
    deadline is the start plus the receipt's `deadline`; the `CIWaitExtra`
    margin (a v1 guess with no reader but this sum) is deleted with it.
  * *Row 15.* `TakeInfo` gains `model`, `deadline` (seconds, policy's
    `timeouts().consumer[tier]`) and `forge` (from the stream, `""` for a hand
    stream) and loses `require_pr`: a forge always means a PR, so the PR step
    runs when `forge` is set, through the adapter named by it
    (`Runner.Forges`, `cmd/nova-worker` serves `github`); a forge with no
    adapter is a fault naming it. A receipt missing a key is refused with one
    line naming `nova-sprint take <card>@<gen> --as <row> --json`
    (`ErrReceipt`); the card already taken is reported `fault` through the
    durable path with that line, and the worker exits non-zero, because every
    further card would be refused the same way. Deleted: `TierDeadline`,
    `DeadlineFor` (and its 45-minute fallback), the three model defaults in
    `harness.go` (a job with no model, only the probe, runs the harness's own
    configured default, since the probe tests the path), `Row.Model` from
    `next --json`, `DefaultCoordinatorHosts`, the `--coordinator-hosts`
    default (a worker with no list is refused naming the flag: a check
    against nothing would pass on the coordinator) and the provider route
    literals in `cmd/nova-worker` (now `--provider-upstream`,
    `--provider-hosts`, `--provider-key`, none defaulted; given none the
    proxy has no route and answers 502). `TestStitchNoDeadlineModelHostOrTierLiteralsInTheWorker`
    keeps them out. nova-config v2.0.2 has no table for provider hosts or
    coordinator hosts, so the service setup passes them from its inventory.
  * *Row 20.* The startup order is `Runner.Start`: the bus hook (it claims
    the row's name and must say so), then `nova-sprint unhook --as <row>`
    (a refusal means no stale hook), then `nova-sprint hook --as <row> --role
    worker`, and only then `next --json`. `RunWakeLoop` calls it before any
    pass and `--once` goes through `RunOnce`, which calls it too; `StepRow`
    never hooks. The `nova-bus hook` is the one long-lived child
    (`OSBusHook`, `WakeHook`: edge.Exec returns only when its program ends,
    so the hook cannot be run through it and is the one child the worker
    starts itself, with only `NOVA_BUS_*` in its environment). A line is a
    wake; wakes that arrive while a pass runs coalesce into one more pass, a
    pass on a goroutine of its own; the hook's death ends the loop with the
    hook's exit and last stderr and the worker exits 1. A report is
    acknowledged only by `nova-sprint report` exit 0: until then the job
    record stays at state `reporting` and each pass that still lists the
    consumer as working sends it again under the same id. This reverses the
    old round 2 ruling that a refusal (exit 1) is final
    (`TestColdread2DefectRefusedReportResentEveryWake` now says so); the
    record goes when the consumer turns final.
  * *Owed, not done here.* `edge/forge/forge.go` (outside this branch's scope)
    still holds the forbidden-logins literal in `isForbiddenLogin_internal`;
    no table gives it a source (nova-config v2.0.2 has none and the receipt
    carries none), so it needs a policy datum and a `forge.Config` field. The
    rest of `Timeouts` (stage, launch grace, backoffs, rebase, push, `gh`,
    bus claim, reserve wait) are operational bounds still in `DefaultTimeouts`.
    `edge.Exec` has no streaming form; if layer 4's owner adds one, `OSBusHook`
    moves onto it. A sprint epoch that changes while the worker runs is not
    re-hooked (the hook is a startup step); the service restarts the worker.
    `tla/Runner.tla` models no refused report, so the keep-and-resend rule has
    a unit test (`TestStitchUnacknowledgedReportIsKeptAndSentOnTheNextWake`) and no model.

### Stitch: the landing's CI, identity and PRs (2026-11-07, branch `v2-stitch-c3`)

- *Defect 40, CI on the group branch.* `forge.Checks` reports a sha with no
  check rollup at all as `CheckAbsent` (it was pending); `LandPass` returns
  `ci_absent` for it (`OutcomeCIAbsent`) in the first gate, the re-merge on a
  moved base and the bisection's subsets. Run's landing loop waits on a
  pending CI as before and on an absent one for `CIAbsentWait` (10 minutes,
  a guess, unmeasured: GitHub creates a queued job's check within seconds of
  a push), then answers `no-ci` and layer 3 opens one judgement. The stream's
  repository must run CI on push to `sprint/**` (LANDING, "CI on the group
  branch", which says why not the merge queue). `LandEdge.tla` has
  `CIAbsent` (gating -> absent -> refused, no head blamed, nothing lands);
  `FaultBlamesNoHead` covers it; `witnesses/LandEdgeAbsentBlamesHead` is its
  reversed witness; the land walk takes it (1 in 8 gating moves).
- *Defect 39, the sprint's identity.* `forge.Identity` is `{Name,
  ExpectedLogin, Token}` (no `ConfigDir`); `Client.Ready` refuses a missing
  token with an `*edge.Refusal` (Problem, Fix; `verbs.AsRefusal` passes it
  through) and every gh call's clean environment carries only that token as
  `GH_TOKEN`. `forge.TokenEnv` is `GH_GATE_TOKEN`; `cmd` builds run's forge
  and git from it, and the sprint-run service's keys include it. `git.Config`
  has `Token`: set, every git call resets the host's credential helpers
  (`credential.helper` empty) and adds one that answers with the token from
  the child's environment (`GIT_CONFIG_COUNT`; checked by hand with
  `git credential fill`). Run's landing asks `Ready` first and answers
  `forge` instead of merging or pushing. The worker's forge (edge/worker,
  another owner) now takes `GH_TOKEN` only; its `GH_CONFIG_DIR` fallback is
  gone with the field.
- *Defect 34.* The verbs' `Origin` has no `PR` (finish asks no PR); `cmd`'s
  origin holds no forge.
- *A red's finding.* `run.landItem` puts the failing checks, the first
  failing URL, the gate's reason and the gated sha in the red item's
  `Reason` (`ciFinding`).
- Tests (unit, fake gh and fake exec): `TestForgeActsOnlyAsItsToken`,
  `TestCredentialsRefusals`, `TestTokenIsTheOnlyCredential`,
  `TestWaitAgainBoundsAnAbsentCI`, `TestLandItemRedCarriesTheFinding`,
  `TestLandRefusesWithoutAToken`.
- *Not done.* `stream add` does not read the repository's workflows for the
  push trigger (LANDING says why); a token without pull-request write fails
  at OpenPR as a fault, re-tried each tick, not as a judgement.

### Stitch: the landing to intent rule 32 (2026-11-07, branch `v2-stitch-w2L`)

Supersedes the landing parts of the two stitch notes above where they
differ (LANDING.md, "The ladder, as built").

- *`LandPass`* (`edge/git/land.go`, rewritten) is one pass of rung 1 or 2.
  All heads already on base: landed at base's tip. One head whose branch
  already contains base's tip: rung 1, its own branch's PR against base,
  refused as `head_moved` if the branch is no longer at the head. Otherwise
  rung 2 (`LandResult.Rung` 2, one head behind base included): the heads are
  merged onto base's tip (`MergeGroup`, deterministic), pushed to
  `sprint-group/<stream>` with a lease on its old tip, and that branch's PR
  is gated. The PR is found or opened against base; a PR whose head is not
  yet the gated sha is `ci_pending`; GitHub's `CONFLICTING` is `conflict`.
  Checks are read at the gated sha; green merges only while base is still
  the tip it read (`base_moved` otherwise) through `forge.Merge(pr, sha)`.
  Outcomes: `landed`, `ci_pending`, `ci_absent`, `base_moved`, `ci_red`,
  `ci_fault`, `conflict`, `merge_refused`, `pr_unopenable`, `branch_failed`,
  `missing_head`, `head_moved`; an error is a fault of git or the forge
  themselves. `LandResult` carries the rung, base tip, group tip, gated
  sha, merge sha, PR, CI URL, the failing checks and their first lines
  (`forge.FailedLines`), the conflicting files and a reason. Bisection
  (`BisectRedGroup`), the stream branch's fast-forward, the remote land step
  (`RunRemoteLandStep`) and the outcomes they had are gone.
- *The group branch* is `git.GroupBranch(stream)` = `sprint-group/<stream>`
  (defect 46; `StreamBranch`, `sprint/<stream>/group`, is gone):
  `GroupNameProblem` names a stream git cannot use in a branch,
  `GroupBlocker` a branch at origin that keeps it from being created, and
  `Client.Heads` lists origin's branches for them. `ValidateCardBranch`
  refuses a card branch under `sprint-group/`.
- *`forge`*: `Identity` is `{Name, Token}` (`ExpectedLogin`, `Ready` and
  the `gh auth status` check are gone): gh runs as the host's login, or
  with `Token` (`GH_GATE_TOKEN`) as an optional override. `PRInfo` gains
  `URL` and `Mergeable`. `Merge(pr, sha)` merges by the repository's
  allowed method (merge commit, then squash, then rebase) with
  `--match-head-commit` and returns base's merge commit. `FailedLines(url)`
  reads the first failing lines of an Actions job's log (`FirstFailing`).
- *Run* (`run/edges.go`): asks a pass again every `LandPoll` on
  `ci_pending` and `base_moved`, and on `ci_absent` until the request's
  `Wait` (policy's `ci_wait`; `CIWait` 10 minutes when none, a guess), when
  it answers `no-ci`; every other outcome goes to layer 3 with its facts
  (`landFacts`). A stream with mode hand or no forge answers `hand`.
- *`cmd`*: the forge and git act as the host's identity; the sprint-run
  service's keys no longer include `GH_GATE_TOKEN`. `cmd/nova-sprint/lander.go`
  is `verbs.Lander`: the hand merge (the PR merged, or with no forge a
  fast-forward push of the head merged onto base), `Heads`, and
  `CreateGroup` (pushed with a lease on its absence). The verbs' `Origin`
  lost `Contains` (finish no longer checks a head against any branch).
- Tests (unit, fakes): `edge/git/git_test.go` (each rung and every outcome,
  the group branch's namespace), `run/edges_land_test.go`
  (`TestAskAgainBoundsAnAbsentCI`, `TestLandItemRedCarriesTheFacts`,
  `TestLandByHandStreamTouchesNothing`), `edge/forge` (Merge, FailedLines,
  the identity). Functional (real git): `edge/git/functional_test.go`.
- *Models.* `LandEdge.tla` is the ladder: `Tick` (layer 3's landing),
  `Pass` (one `LandPass`), `BaseMoves`, `Answer` (land, fix, retry);
  invariants `BaseNeverRed`, `NothingLandsUnpassed`, `MergingCovered`, each
  with one reversed witness (`tla/make-landedge-witnesses.py`); reach
  checks `LandEdgeNever{RungTwo,Judged,HandLand,RedGated}`. The six old
  LandEdge witnesses and `edge/git/coldread_test.go` (probes of the old
  ladder's defects) are removed. The trace walk
  (`edge/git/trace_test.go`, `TestLandEdgeTraceRefinementWalk`: `LandPass`
  on the git fake and a truthful forge, 8 seeds walked to the end) writes
  `landedge-walk-<seed>`, checked against `LandEdgeTrace.tla`.
  Measured on the bench (TLC 1.8.0): `LandEdge.cfg` 46,288 distinct states
  in 4 s, no error; the 3 witnesses and 4 reach checks violated as
  required; the 8 walks (8 to 17 records each) all `TRACE OK`.

### Clone addresses (stitch, 2026-11-07, defect 21)

`stream add <s> --repo mas-bandwidth/nova-tools --forge github`, the form the
help prints, was refused at the fleet: the git edge passed `mas-bandwidth/nova-tools`
to `git clone` and `git ls-remote` as a path. `edge/git/url.go` adds
`CloneURL` and `IsOrgRepo`; `Client.Tip` reads a branch at an address with no
clone; `Config.GitHubCredential` sets gh as git's github.com helper. Callers:
`run/edges.go` (`Edges.clone`, for land and heads), `cmd/nova-sprint/edges.go`
(`origin.Tip` no longer clones; `gitEdge` sets the helper when `GH_TOKEN` or
`GH_CONFIG_DIR` is set), `verbs/stream.go` (`baseTip`, shared by `stream add`
and `check`) and `verbs/head.go` (finish). Not changed here: the worker's
staging still takes `--origin` (the per-stream-repository staging is another
change; it should take its address from `git.CloneURL(receipt.repo,
receipt.forge)`). Unit tests: `edge/git/url_test.go`,
`run/edges_test.go`, `verbs/stream_url_test.go`,
`cmd/nova-sprint/edges_test.go`. Not run here: a clone of a private GitHub
repository through the gh helper (functional, on a bench).
### The worker after the first bench runs (2026-11-07, stitch child c4)

The first `nova-sprint worker` runs on a bench found these; each is fixed
with a unit test (fake wall, fake exec, fake sprint; no process, socket or
clock), mostly in `edge/worker/probe_test.go`.

- **Harness directory (defect 24).** `OSReserveWall.RunHarness` grants
  `--read` on the directory `exec.LookPath` finds the harness binary in and,
  when that is a symlink, on its target's directory (`HarnessDirs`); a binary
  it cannot find is left to the wall's own refusal, which names the path.
- **The cause line (25).** `edge.CauseLine`: escapes stripped
  (`edge.StripANSI`), the first line holding error, refused or failed, else
  the last non-empty line, bounded; `edge.StderrLines` strips escapes too.
- **Keys from the environment (26).** `ReadProviderKey` (`nova-secrets get`,
  a verb nova-tools v2 does not have) is gone: each route's key is the named
  environment variable, which the bench seat's `nova-secrets exec --only ...`
  put there. A route whose key is absent refuses at the probe, naming the key
  and the exec line, before any harness runs; at request time the proxy
  answers 503 with the same words.
- **The probe's model (27, 36).** The probe runs after the hook (next
  --json answers only a hooked row) with, for each tier, the first of the
  policy's candidates (`tier_models`; for a bench since stitch w2D1 the one
  model the catalog's `bench` names for the tier) the harness serves: through the proxy
  a route for its provider, else the adapter's one provider, and in the
  harness's own list when it has one (`opencode models`, run outside the wall
  under the probe's home). A tier with no candidate refuses ("no model for
  tier <t>"); a tier none of whose candidates it serves is a warning naming
  why, and a card of that tier dealt here faults with the candidates; none
  served at all refuses, naming each tier's reasons and the harness's list for
  those providers. A take runs the receipt's `model`, the one the deal
  named (`chooseModel_internal`); a model the probe proved the harness does
  not serve faults the card naming the model and the tier (stitch
  w2D1: the receipt's `models` candidates are gone). A failed probe unhooks
  the row again (`ServeRow`).
- **Logs (28).** Every probe and job run keeps `stdout.log`, `stderr.log` and
  `command.txt` (the wall's command line; the environment's values are never
  written) in its directory; probe directories are
  `<root>/probes/<row>/<provider>_<model>` and are kept after a failure. The
  combined `harness.log` is gone.
- **Several providers (29).** `--providers <name>=<url>@<KEY>[,...]`; the
  harness's base URL is `http://<proxy>/<provider>/v1` (`ProxyBaseURL`), and
  the proxy picks the route by that first path segment and cuts it, so one
  proxy serves every provider; a route forwards only to its upstream's host.
- **Judgement errors (30).** At 0832c0c0 the probe already appended the
  judgement's error to its refusal. Found: nova-sprint v2 has no `judge-open`
  verb, so `CLISprintClient.OpenJudgement` always fails ("unknown verb") and
  every probe refusal says its judgement could not be opened. A row-raised
  judgement needs a verb (layer 6) or an engine judgement for a row that
  hooks and unhooks; not built here.
- **Each adapter's wall (32, intent rule 31).** `Harness.Needs()` declares
  `Unix`, `Proxy`/`NetOpen`, `Writes`, `ListModels`, `LogDir`, `Provider`,
  `Model` and `Limits`; `BuildWallArgv(cfg, job, env, argv)` honours
  `job.Needs` and passes every environment name but PATH, HOME, LANG and TERM
  with `--env` (nova-sandbox drops the rest otherwise; read from its help, not
  measured). opencode: `--unix` (its server's AF_UNIX socket; measured on a
  bench) and the proxy's port; the same-uid escape trade is in its comment.
  claude and codex: network open (each reaches its own provider with its own
  login), their config directory a `--write`; whether they need `--unix` is
  not measured. A probe that exits non-zero in its first second with an empty
  log directory (or, with none declared, no output but the WALL line) says
  "the harness died inside the wall before writing a log; nova-sandbox show
  <flags> -- <harness>".
- **Deferred, said (31, 33).** opencode through the proxy needs a provider
  config in the wall home (31), and its own write tool dies inside the wall
  (33). Neither is fixed; the opencode adapter's `Limits` names both in every
  failed probe's refusal.
- **Sprint verbs (35).** Run directly (`<sprint-bin> <verb> ...`), inheriting
  the worker's environment, which the seat's exec filled; `--secrets-bin` is
  gone.
- **Any bench (41).** `CheckHost`, `Runner.CoordinatorHosts` and
  `--coordinator-hosts` are gone.
- **A clone per stream repo (42).** `OSGitOps{WorkRoot, MirrorRoot}`: the
  repo is the receipt's, its clone URL a URL or path as it is, or
  `https://github.com/<owner>/<name>.git` for owner/name on the github forge
  (`CloneURL`, a local helper; the stream edge's own clone URL is another
  child's); its mirror `<mirror-root>/<name>.git` (mirror-refresh's layout,
  cloned `--mirror` on first use), its work clone `<work-root>/<name>`
  (cloned `--reference` the mirror on first use, once per URL under a lock),
  and a work clone of another repo in the way is refused. The worktree's
  origin is the clone URL, so its fetch, push and the clean step's head check
  go to the repo. Flags: `--work-root` (default `<root>/clones`) and
  `--mirror-root` (default `~/nova-bench/mirror`) replace `--work-clone`,
  `--mirror` and `--origin`.
- **The flag set** of `nova-worker` and `nova-sprint worker`: `--as <row>`,
  `--host`, `--root`, `--state-dir`, `--harness`, `--work-root`,
  `--mirror-root`, `--providers`, `--once`, and `--sprint-bin` on
  `nova-worker` only.
- **Owed.** A bench's tiers: the roster gives every bench every tier, so a
  bench whose harness serves no model of a tier is still dealt its cards,
  which fault and return to ready; the roster (or policy) needs a bench's
  served tiers. `rebaseAndPRSteps_internal` is 67 lines (over the 60 rule;
  untouched here). (Both done by w3W, below: the advertised tiers, and the
  PR step gone with the forge.)

## Built: admission reads and the probe before the hook (stitch w2A, 2026-11-07)

- `git.Client.Branches(url, prefix)`: one `git ls-remote --heads` for the
  branches under a prefix, by name with their tips; a line that is not
  `<sha> refs/heads/<prefix>...` is refused, never skipped.
  `git.Client.ListTree(dir, rev)`: `git ls-tree -r --name-only -z`. Both are
  the reads admission uses (CARD-CONTRACT.md "Admission").
- The worker's start order is now bus hook, probe, sprint hook
  (`busHook_internal`, `ProbeRow`, `sprintHook_internal`): a failed probe
  leaves the row off the sprint, with no hook to undo, and up's step 7 can
  read "hooked" as "probed". Test: `TestServeRowProbesBeforeTheSprintHook`.
- `roster.Worker.Routes`: a machine's `route:<name>` roles, sorted and
  unique; `Roster.ServesRoutes`. Kin serve no routes. A first-class routes
  field in nova-config is owed upstream.
- Still open (recorded above under "Judgement errors (30)"): no
  `judge-open` verb exists, so a failed probe opens no judgement; with this
  order the row stays unhooked and its worker log says why.
### Built: the worker's heartbeat (stitch, 2026-11-07)

- `edge/worker/beat.go`: with a `Meter` and a `Beater` set (`Serve` sets both), `ServeRow` pushes
  `nova-sprint beat` (layer 6) after the probe and then every `BeatEvery` (30 s, a guess) until
  the worker ends, on the runner's `Sleep`. `OSMeter` reads the 1-minute load average
  (`/proc/loadavg` on Linux, `sysctl -n vm.loadavg` on macOS and the BSDs, nothing on Windows:
  no load, no beat, and the warning says so), the logical cores (`runtime.NumCPU`), and the slots
  from `nova-reserve status`'s `free=n/N` line. A bench with no readable slots still beats, with
  0 of 0 and a warning. Each distinct failure is warned once, not every period. The worker
  keeps only the last warning's text; a beat that repeats the last one writes nothing.
- Choice: a periodic verb of the worker's own, not the hook's presence (the bus hook is nova-bus's
  and carries no numbers) and not a read verb that writes. Tests: `edge/worker/beat_test.go`.

### Built: the landing's red split, its timing, and the host identity (stitch w3L, 2026-11-07)

Directives 59, 63 (land side), 76; LANDING.md "Red that is not the card's".

- *`forge` (`edge/forge/actions.go`, new).* `Inheritance(repo, base,
  baseTip, head)`: the merge base (`gh api repos/<r>/compare/<tip>...<head>
  --jq .merge_base_commit.sha`), the workflow runs at the head and at the
  merge base (`actions/runs?head_sha=`, `--jq`-projected so a page of 100
  runs fits the 64 KiB output cap), the jobs of each failed or still
  running run at the head (`actions/runs/<id>/jobs`), and for a failing
  workflow with no completed run at the merge base the base branch's last
  completed run of it (`actions/workflows/<id>/runs?branch=&status=completed`).
  A workflow fails at the head when a run failed or is running with a
  failed job. Returns `RedSplit{MergeBase, Inherited, New}`; each
  `WorkflowRed` carries its `Basis` (what the base side was read from),
  `Running`, and its failed jobs (`FailedJob{Name, Runner, URL, Lines}`).
  Only new red's logs are read: `JobLines(id)` (`actions/jobs/<id>/logs`,
  readable once the job ends, `LogMaxBytes` 8 MiB, timestamps stripped,
  `FirstFailing` 5 lines of `LineMax` 240 bytes) for the first `NewJobsMax`
  3 jobs; all three bounds are guesses. `Authors(pr)` is the PR's distinct
  commit authors (`gh pr view --json commits`); `IsAgreement(check)`
  names a CLA/CAA/contributor-agreement check. `CheckRollup.Waiting`: a red
  rollup with a check still queued or running.
- *`LandPass`* (`edge/git/land.go`): a red rollup goes through
  `splitRed_internal`: an agreement check failing reads `Authors` into
  `LandResult.Unsigned`; the split fills `MergeBase`, `Inherited`, `NewRed`
  (plus each failing check that is no failed job of any workflow: new, not
  compared). New red is `ci_red` at once, even while the run goes on; all
  inherited while waiting is `ci_pending` ("waiting for the run"); all
  inherited with the run ended is the new outcome `ci_red_inherited` (a
  judgement); a split that cannot be read waits while the run goes on, then
  is `ci_red` with `FailingLines` and the cause. Run's `landFacts` leads
  with NEW red, inherited red and the authors; layer 3 words
  `ci_red_inherited` for a glance (`steps.InheritedRed`).
- *Identity (59).* gh runs with `forge.HostEnv()`: the process's
  environment less `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`,
  `GITHUB_ENTERPRISE_TOKEN` and `GH_CONFIG_DIR` (`CleanEnv`), plus the
  `GH_GATE_TOKEN` override as `GH_TOKEN` when set: before this a
  `GH_CONFIG_DIR` in run's environment made the landing act as that
  directory's login, not the host's. git's github.com helper is always gh
  as the host's login (`!env -u ... gh auth git-credential`; the
  `githubCredential` switch on `GH_TOKEN`/`GH_CONFIG_DIR` is gone), and
  admission's `hostForge` runs gh the same way. `Identity.Name` is the
  choice (`forge.HostLogin`, "this host's gh login", or `GH_GATE_TOKEN
  (the override of ...)`), never "sprint"; the missing-gh fix says `gh auth
  login on this host`. Already done at the base: the "identity sprint" and
  "expected sprint" refusals (`ExpectedLogin`, `Ready`) had been removed by
  w2L; what remained was the environment and the name.
- *Model.* `LandEdge.tla` unchanged and TLC not rerun: in the model every
  non-green, non-absent gate is one judgement, which `ci_red_inherited`
  still is, and waiting for the run is the re-ask a pending CI already is
  (a `Pass` is atomic up to its final outcome). The trace walk's truthful
  fake reports red as new (`fakeForge_internal.Inheritance`), so
  `walkOutcomes` is unchanged.
- *Tests (unit, no network).* `edge/forge/actions_test.go`: gh with canned
  JSON for compare, runs, workflow runs, jobs and logs
  (`TestInheritanceSplitsByTheMergeBase`: inherited at the merge base,
  inherited by the base branch's last run, new at the merge base, new with
  no run on the base, a running workflow with a failed job, inherited logs
  never read), compare faults, an unread log, `Authors` and `IsAgreement`.
  `edge/git/red_test.go`: inherited-only, waiting, new while waiting, the
  agreement check, an uncompared split. `run/edges_land_test.go`
  `TestLandItemSplitsRedByTheBase`; `steps/tick_test.go`
  `TestTickLandInheritedRedIsAGlance`; `cmd/nova-sprint/edges_test.go`
  `TestIdentityNamesTheChoice`; `edge/forge` `TestForgeActsAsTheHostsGh`
  now sets `GH_TOKEN` and `GH_CONFIG_DIR` and requires neither reaches gh.
- *Not done.* Which author is unsigned is not parsed from the agreement
  check's output (every author is named); no functional test against a real
  repository's Actions API (the calls mirror a hand tool used on the live
  fleet, not run here).
## Built: one gate parser; the install (stitch w3A, 2026-11-07; directives 77, 78)

- `edge.ParseGate` (`edge/argv.go`) is the one parser of a DONE-WHEN line.
  `OSReserveWall.RunTest` calls it (replacing `splitArgv_internal`, which was
  the tokenizer without the checks), so admission's add and the worker's run
  agree; a stored line with shell syntax is an error naming it, never run as
  literal arguments. The rule is in CARD-CONTRACT "Admission".
- `worker.CommitEmailDomain` is the domain of a bench's commit author email
  (`<row>@nova-sprint.local`); the job record and admission's agreement
  warning both read it.
- The binary at `~/.local/bin/nova-sprint` on a bench is installed by
  nova-fleet's play (another repository), whose replace-by-rename and ad-hoc
  signing is not changed here. In this repository `make install` now uses
  `tools/placebin`: a new file beside the old, `codesign --force --sign -` on
  darwin, rename over, never an open of the old file for writing. Tests:
  `tools/placebin/place_test.go` (the destination is a different file after
  and the old open file keeps its bytes).

### Built: the worker's live defects (stitch w3W, 2026-11-07)

From the first bench runs (directives items 61, 65 to 74). Files:
`edge/worker/pass.go` (passes and jobs), `recover.go` (restart, release,
quarantine), `outcome.go` (a read's outcome), `jobmd.go` (JOB.md).

- **Passes apart from jobs (66).** A pass recovers the row's jobs, reads
  the row and acknowledges (takes) each card dealt working into its hand
  that has no job yet (the deal holds the width: deal is take), then
  returns; the jobs of a pass run detached under the runner's WaitGroup,
  which the wake loop
  waits on when it ends. A refused receipt in a job still ends the worker
  (a fatal channel). A periodic wake (`WakeEvery`, 10 s, a guess) runs a
  pass beside `hook.Wake()`. Model: `Runner.tla` (PassStart, PassEnd,
  `PassNeverWaitsOnJobs`, witness `RunnerPassWaitsOnJobs`); the trace walk
  emits the pass. Test: `TestLivePassTakesWhileAJobBlocks`,
  `TestLivePeriodicWakeRunsAPass`.
- **Probe failure never exits (61).** `ServeRow` (served, not `--once`)
  holds the row unhooked with the probe's reason on stderr and probes again
  after `ProbeBackoff` (30 s) doubling to `ProbeBackoffMax` (10 min), both
  guesses; `--once` still exits on a failed probe.
- **Restart and stop (70, 73).** The take receipt is kept as
  `receipt.json` at take; a restart rebuilds the job from it (a re-take
  would replay the take's op id at the pre-take gen): taken is staged again,
  staged or running with no live harness is run again, running with a
  result file is harvested. A job with no slot stays staged and the next
  pass runs it. A job directory with no receipt is released with no blame:
  `release` gains `--head` so the worker hands back
  the partial head it pushed (checked as a finish's head is). "runner lost
  the job" is gone, and so is releasing a card in hand with no job
  directory: under deal is take that card is the pass's to run (its take,
  the acknowledgement, prints the same receipt again). The worker's own stop cancels its jobs: each is "runner
  stopped", its record kept at its state with no pid, never reported;
  `edge.Exec` classifies a cancelled command as `canceled` ("stopped by its
  caller"), never a timeout. Model: Stop, Release, `StopIsNeverAFault`,
  witness `RunnerStopIsAFault`.
- **A read's outcome (67).** `ReadOutcome` has three values (ok, fix,
  fault) and a read's report is built only from it, so a read cannot send
  "fail". No result file for a read is a fault; a harness's own fail
  verdict on a read is a fix with its reason as the finding. A record
  written before this (fail naming result.json) heals at its next send.
- **No verdict and a left tree (the harness note).** A work harness that
  exits 0 with no result file is "fail: no verdict: ..." with its last
  output. One that says ok, leaves its changes uncommitted and HEAD where
  it started has its tree committed by the worker (`GitOps.CommitAll`,
  `--no-verify`, the row's identity) and pushed; ok with nothing to commit
  is a fail saying so.
- **JOB.md (68).** Written at stage: card, repository, base, checkout,
  prior head and finding, paths, done-when, the card's prose, and "How this
  card finishes" (`finishRules_internal`), which the prompt carries word for
  word: the result file's forms, and no git push and no gh (the worker
  pushes; the landing opens the PR).
- **A refused record never wedges a pass (69).** Each send's refusal
  (the first included) is logged and counted on the record; after
  `RefusalLimit` (3, a guess) identical refusals the record moves to
  `<root>/quarantine/<consumer>@<gen>` and is raised on stderr with what to
  read. A quarantined card is neither resumed, run afresh nor released.
- **No forge in the worker (71).** The worker's contract ends at a pushed
  head: `Runner.Forges`, `EnsurePR`, the PR step and the gh timeout are
  gone. Of the three stitch tests that encoded worker-opens-PR,
  `TestStitchStreamForgeWithNoAdapterIsAFaultNamingIt` is deleted (the
  landing opens and gates the card's PR: `TestLandRungOneGatesTheCardsOwnPR`
  in `edge/git`), the PR assertion of
  `TestStitchDeadlineModelAndForgeComeFromTheReceipt` is dropped, and the
  forge gates of the store-backed fake (`RequirePR`, `RequireGreenAtReport`)
  with them.
- **One fetch per repo per pass (72).** `GitOps.Fetch` runs once per repo a
  pass stages from, the repos at once, under the work clone's write lock;
  staging takes the read lock, so no fetch runs on a clone while another
  job clones from it. Test: `TestLiveOneFetchPerRepoPerPass`.
- **Advertised tiers and slots (65, 74).** The probe records the tiers whose
  policy model the harness serves; at hook the worker beats once before its
  first pass and every beat carries `--tiers` with its nova-reserve slots.
  The store keeps them on the row (`Row.Tiers` beside `SlotsTotal`, the
  heartbeat's columns; cleared at unhook and at a new session).
  `roster.Advertised` gives an advertising worker those tiers at width
  min(nova-config width, slots); the run tick, every verb's roster read and
  the board use it. Why not a tier list in each adapter: the worker may hold
  no tier literal (rows 14, 15,
  `TestStitchNoDeadlineModelHostOrTierLiteralsInTheWorker`); an adapter
  declares what it reaches (provider, model list, proxy routes) and the
  probe turns that into tiers. opencode with only a deepseek route therefore
  advertises flash, the catalog's tier for deepseek-flash: a choice that
  rests on the catalog's tier map, not on a measured run (a guess as to
  whether it does pro work well). A beat moves no gen, so a deal made from a
  snapshot just before an advertisement is not fenced by it (one tick at
  most). The default policy deals no ahead (`ahead: 0`).
- **Fixed on the way.** A final card whose job is still in flight is left
  for that job to clean (two cleaners raced on the directory).
- **Checked (measured, a bench host, TLC v1.8.0, 2026-11-07).** `Runner.tla` with `Runner.cfg`:
  no error, 20,224 states generated, 5,269 distinct. The first run found `Liveness` violated: a
  released card, ready again, was never retaken because `Take` is enabled only while a pass
  plans and was only weakly fair; every pass takes the ready cards it has room for, so `Take`
  is now strongly fair. `RunnerPassWaitsOnJobs` and `RunnerStopIsAFault` report their
  invariant violated, as do the six older Runner witnesses; the four Runner reach checks are
  violated as required. The three runner walk traces (with Stop and Release in them) check
  against the model (`check-trace.sh Runner`: 29, 30 and 40 steps, TRACE OK).
