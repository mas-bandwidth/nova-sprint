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
5. `nova-sprint worker`, one service per bench: deal to report by script, a slot
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
`bus-reply` reads only the sender (verbs.BusAnswer): the sprint's pushes are
receipts, so no message is answered (one log line each). The engine never parses a bus message; every
answer is a verb a row runs against the store (CORRECT-USE: "There is ONE
WAY").

Sent the moment the op that caused it commits:

| subject | to | when | answered by |
|---|---|---|---|
| `DEAL` | the row | consumers were cut for it (`Start`, `AskRead`) | `nova-sprint take` |
| `WITHDRAWN` | the row | a consumer it holds was withdrawn (preempted, silence) | stop; the runner pushes nothing (the branch is the next holder's, defect 78) and retires the job directory |
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
report <consumer> --result ... --head ... --as <row>` (and
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

`Git` has `Fetch`, `MergeGroup(onto, heads)` (a conflict names its files),
`Push(sha, ref, expect)` and `IsAncestor`.
The rung and what a red means are layer 3's (LANDING.md); this edge does the
steps by script, one attempt per stream at a time, streams in parallel, base
pushes one at a time in stream order:

1. **Fetch** base, the stream branch and every head of the group into the
   lander's clone on the landing host. A head origin does not hold refuses
   the landing for this pass, naming it (LPM M16).
2. **Merge** the group in score order onto the stream branch at the base
   tip. Any conflict refuses the merge, naming the files. Rung 1 is a group
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
back. A re-merge that conflicts is a rung 3 judgement naming
the files. A red gate goes to layer 3's bisection (LANDING step 7); a gate
that did not run (no CI runner, a cancelled job, a timeout) is a fault and
blames no head.

The stream rule: a work consumer has the stream head as it is then merged
into it once, by the runner, at report; merged, never rebased, so a
conflict the card resolved stays resolved and the head keeps its history.
After the report the stream moving is the landing's to absorb, never the card's to chase (Glenn, LANDING step 3:
"this has always been the rule. you guys just forget it over and over"). A
merge conflict with the stream is a `fail` report naming the files.

A landing refuses for this pass, spending nothing, on: a head not on
origin; a re-merge conflict; a push
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
conflict by script; runs under the writer lock or on the
coordinator's machine.

The landing host is a bench from policy's list. The edge runs a fixed
binary there (`nova-sprint land-step`, argv only) over ssh with a connect
timeout and no shared control socket; a host that does not answer is
skipped for this pass and the next tried (LESSONS 4.13). A hold never
removes a landing host (LANDING step 9).

## The GitHub edge

`Forge` has `PR(head, base)` (find), `OpenPR`, `Checks(shas)`,
`Merge(pr, sha)`, `FailedLines(url)`, `Failures(sha)` and `Rerun(url)`.
A rollup says how many checks are still pending (`Pending`) and whether a
red one still has checks running (`Waiting`); the landing takes a verdict
only when it does not (LANDING.md, "A verdict only from completed
checks"). `Failures` is each failing check at a sha with how GitHub says
it was stopped, if it was (its conclusion, and its failure annotations:
"exit code 137" or "143", a time limit, a cancel; GitHub's words only).
`Rerun` runs the failed jobs of the Actions run a check's URL names
(`gh run rerun <run> --failed`), once per landing when every failing
check was stopped.

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

Rate limits: GitHub's own answer is the limit, per bucket: GraphQL (`gh
api graphql`, `gh pr`, `gh repo`) and REST (`gh run`, other `gh api`
paths) rest apart, so a spent GraphQL bucket does not stop a rerun. A 403
or 429 with a retry time rests that bucket until then; a primary limit
with none rests it until the bucket's reset, read once from `gh api
rate_limit` (60 s when that read fails or shows calls left). A rested call
is a fault to its caller that spends nothing. The landing asks a pass whose PR or checks read was rate-limited
again, as it does a pending CI (`forge_rested`): never a land judgement.
A checks read pages past a commit's first 100 checks only when GitHub's
per-state counts show a failed or faulted check (or do not account for
every check), so a poll of a commit with nothing failed is one call. Rung 2's one PR per group keeps a backlog inside GitHub's documented
limits (5,000 requests an hour; PR opens far fewer).

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
   the row's token. On a wall death or a kill past the deadline, while the
   job still holds its card, push whatever commits the worktree holds, so
   the next attempt continues from them. Every push is a lease
   (`--force-with-lease=refs/heads/<branch>:<expect>`, from the head the job
   last pushed or the tip it found at stage), never a plain force, and none
   once the consumer is final or a newer generation holds it: a withdrawn
   job pushes nothing (defect 78).
6. **Merge the stream** head as it is now in (never rebase), bounded by the
   two-minute step bound, and run the card's done-when gates, inside a slot,
   each until the card's deadline (what is left of it, less the push's 60 s
   kept for the push and the report; the step bound is never a gate's);
   push. A job restarted from taken or staged keeps the deadline its record
   holds. A conflict is a `fail` naming the files; a gate still running at
   its bound (the wall's own timeout of the gate, never of a setup step
   before it, which is a `fault`) is a `fail` saying how long it ran; one
   not started because no time was left is a `fault` that says so (no limit
   counts it: the card is dealt again and continues from the pushed head).
   A red gate's reason names the gates not run after it. Then the
   stream's gates (the take receipt's `stream_gates`) run the same way at
   the same head. One red at the head (it ran and exited non-zero by
   itself, not killed) runs at the stream tip the merge used
   (`GitOps.StreamTip`, the clone's `FETCH_HEAD`), in a worktree of the
   job's tree under the job directory (`base`), removed on every path so
   the job's tree stays at the head: red there too, it is the base's and
   does not fail the attempt, whose report's reason says `stream gate
   <command> is red at base <sha>`; green there, it is a `fail` naming the
   stream gate; not judged there (no tip, no checkout, a wall fault, a
   timeout, a kill, a stop), it is a `fault`. Only a green answer is kept,
   per repository, base and gate, for the life of the worker.
   Runner.tla's Rebase and Conflict are this step's outcomes as before: a
   stream gate adds inputs to the step, no transition.
7. **CI**: open no PR (the landing opens the stream's group PR, defect 34);
   CI is read by the landing on the group sha, not by the worker.
8. **Report** with a result id written to `job.json` before the first try;
   retry the verb with that id until it exits 0 or 1 (LESSONS 4.5).
9. **Clean** the job directory only after the report is recorded and the
   head is on origin; a worktree with uncommitted changes is logged and kept
   until the next pass, which retires it (the consumer is final then).

Results, classified by the adapter. `ok`: the result file says ok and a head
past `base_sha` and `prior_head` is pushed (a harness that said ok and left
its changes uncommitted has its changes inside the card's PATHS committed by
the worker; anything outside PATHS is logged, never committed). `fail`: the
result file says broken, no result file ("fail: no verdict" with the
harness's last output), a merge conflict with the stream, the card's tests red
(the reason keeps the lines that say what failed, make's `***`, `FAIL`,
`--- FAIL`, `error:`, then the output's end, in 4096 bytes) or still
running at the card's deadline. `fault`:
exit 134 or 137; no result file after a provider failure the harness did
not get past ("provider HTTP N": a 402, 429 or 5xx, an upstream that could
not be reached or a response cut mid-body as 502, and no request sent after
it answered); a launch that died of a 5xx inside the 15 s grace is launched
again, three times in all; a rate-limit or credit refusal; a staging failure. A read
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
Unmeasured until the first dogfood sprint: the stream merge, the seal and
the apply of the merged head 2 min each (a done-when gate has no bound of
its own: it runs until the card's deadline, less the push's bound; one
measured 524 s); push 60 s; `gh` 30 s; bus send 5 s; ssh connect 10 s; CI wait, the repository's
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
with the partial head it pushed). A job directory whose consumer is final,
or whose generation is stale, is retired on the first pass that sees it:
nothing pushed, the directory made writable and removed, its record kept in
`<root>/retired/`; a removal that fails is raised once and moved to
`<root>/quarantine/`. A record the sprint refuses three times the same way is moved
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
`modules/edge/git/trace_test.go`.

`tla/Runner.tla`, one bench. Variables: each consumer's store state
(`ready`, `working`, `done`, `fail`), its job state (`none`, `taken`,
`staged`, `running`, `harvested`, `rebased`, `ciwait`, `reporting`,
`recorded`, `cleaned`), slots in use, width, the job directory (exists,
result id, pushed head), harness alive, the store's applied op ids. Actions:
Wake (duplicated, lost), PassStart and PassEnd (the pass, a process of its
own: only a pass takes, and it ends whatever its jobs do), Take, Stage,
StageFail, Run, Die, Harvest, Rebase, Conflict, Push, Report, ReplyLost,
Clean, Withdraw (the sprint's: the store moves, the job does not), Retire
(the next pass removes a withdrawn job's directory, pushing nothing),
HolderPush (the card's next holder pushes its branch from another row),
Release (the restart's no-blame give-back), Stop (the
worker's own stop: harnesses killed, jobs kept at staged), Crash and
Restart (at any step), StoreDown; and the card's branch on origin (who
wrote its tip, the next holder's write, the job's pushes after final).
Invariants: guarantees 7 to 11; `working`
within width; slots within capacity; `PassNeverWaitsOnJobs`;
`StopIsNeverAFault`; `NoPushAfterFinal`; `BranchOnlyByHolder`;
`NoSlotNeverReleases` (a staged job with every slot held waits for one, never
gives its card back; the sprint's deadline withdraw bounds the wait). Liveness:
every `working` consumer of the row ends recorded or withdrawn, and a
withdrawn job's directory is removed (`FinalDirRemoved`). Reversed witnesses: a slot freed while its
harness lives; a reused job directory; Clean before Report (work lost on a
crash); a fresh result id on retry (two reports); Restart taking a working
consumer as new (two harnesses on one card); a Die classified `fail` (a
fault that bounces); a pass that waits on its jobs (`RunnerPassWaitsOnJobs`);
a stop reported as a fault (`RunnerStopIsAFault`); a withdrawn job that
pushes (`RunnerWithdrawnPushes`), again on a later pass
(`RunnerWithdrawnPushesRepeatedly`), and by plain force over the next
holder's branch (`RunnerForcePushRewinds`); a release for want of a slot
(`RunnerNoSlotReleases`). Reach checks: a take
while another job runs, a stop, a stopped job back in a pass, a release, a
retire, the next holder's push over a head the job pushed.
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
`edge.Exec` 150; bus edge 350; git edge 800; GitHub edge 500; nova-worker 1,900 (lifecycle
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

- `edge` (`modules/edge/exec.go`, `modules/edge/request.go`, `modules/edge/exec_test.go`, `modules/edge/coldread_test.go`, `modules/edge/coldread2_test.go`, `modules/edge/break_test.go`): `Exec` (`OSExec`, `FakeExec`, clock starting at `1000`), `RunArgv`, `ValidateCmd`, `RequestID(epoch, kind, card, sha)`, `EdgeRequest` (carrying `After []string`, `Repo`, `Base`, `PR`, `Mode`, `Ledgers`), `EdgeResult` (stub for `steps.EdgeResult`), and `Deduplicator` (in-flight coalescing by `RequestID` with zero persistent memory after completion). Every child process runs in its own process group (`Setpgid: true`, killed as `-pid` on timeout/cancellation) with stdin closed (`nil`), bounded stdout/stderr ring buffers (`DefaultMaxBytes = 64 KiB`, preserving head and tail), `WaitDelay = 200ms` (`exec.ErrWaitDelay` treated as normal exit when the command exited before timeout), `OnStart(pid)` callback, and mandatory policy timeouts.
- `modules/edge/bus` (`modules/edge/bus/bus.go`, `modules/edge/bus/bus_test.go`, `modules/edge/bus/functional_test.go`): `Bus` (`Send`, `Who`) and `Edge`, which runs `nova-bus send` and `nova-bus who` through `edge.Exec`. `Send` refuses a body over 4096 bytes before nova-bus runs and returns the message id only when nova-bus exits 0 and prints `<recipient> <id>`; `Who` parses `WHO OK present=<n>` and its tab lines with `ParseWho`, the one parse of `nova-bus who` (up's presence and the setup walk call it too). The package holds no cursor and reads no store: what to send, to whom, and when the cursor moves are run's (layer 6, the one pusher; stitch, below).
- `modules/edge/git` (`modules/edge/git/git.go`, `modules/edge/git/land.go`, `modules/edge/git/fake_test.go`, `modules/edge/git/git_test.go`, `modules/edge/git/coldread_test.go`, `modules/edge/git/coldread2_test.go`, `modules/edge/git/break_test.go`, `modules/edge/git/functional_test.go`, `modules/edge/git/trace_test.go`): `Git` (`Client`, `Fake`, clock starting at `1000`) and `LandPass` (`StreamBranch`, `StreamRef`, `ValidateCardBranch`, `RunRemoteLandStep`). Implements the 6-step landing pass from `LANDING.md` (fetch, merge group in score order, push `--force-with-lease=<ref>:<old>`, gate at exact SHA, land only while base tip matches the gated parent, fast-forward stream branch). Per `1794011119389-0` (`LANDING.md` step 1), the stream integration branch is `sprint/<stream>/group` (`refs/heads/sprint/<stream>/group`), coexisting cleanly with card branches `sprint/<stream>/<producer>`, and `ValidateCardBranch` / `Take` refuse a card or producer branch named `group` or bare `sprint/<stream>`.
- `modules/edge/forge` (`modules/edge/forge/forge.go`, `modules/edge/forge/fake_test.go`, `modules/edge/forge/forge_test.go`, `modules/edge/forge/coldread_test.go`, `modules/edge/forge/break_test.go`, `modules/edge/forge/functional_test.go`): `Forge` (`Client`, `NewClient`, `Fake`, clock starting at `1000`), `PR`, `OpenPR`, `EnsurePR`, `Checks`, `Merge`, and `RestedUntil`. Queries `statusCheckRollup` via GraphQL at up to `MaxChecksBatch = 100` exact 40-hex commit SHAs per call with context pagination, classifies outcomes (`green`, `red`, `pending`, `fault`, `rate_limit`), verifies PR base and head SHA before fast-forwarding `refs/heads/<base>` in `Merge`, and enforces non-Glenn identity credentials (`GH_TOKEN` / `GH_CONFIG_DIR`) and a per-identity rest after GitHub rate-limits it.
- `modules/edge/worker` (`modules/edge/worker/worker.go`, `modules/edge/worker/harness.go`, `modules/edge/worker/proxy.go`, `modules/edge/worker/verbs.go`, `modules/edge/worker/runner.go`, `modules/edge/worker/verbs_test.go`, `modules/edge/worker/worker_test.go`, `modules/edge/worker/runner_guarantees_test.go`, `modules/edge/worker/coldread2_test.go`, `modules/edge/worker/break_test.go`, `modules/edge/worker/functional_test.go`, `modules/edge/worker/trace_test.go`), served by `nova-sprint worker` (one binary):
  - No host refusal: nova-config's bench role is the only word on where a worker runs, the coordinator's machine included (2026-11-07; `CheckHost` and `--coordinator-hosts` are gone, below). `--as <row>` is the one mandatory flag.
  - Adapters (`ClaudeHarness`, `CodexHarness`, `OpenCodeHarness`; `ScriptHarness` in the tests only) implementing `Harness` (`Name`, `Command`, `Probe`, `Classify`, `Usage`). `--harness` defaults to `claude`. `ClassifyJob` treats `<job>/result.json` as authoritative once written and classifies provider HTTP 402/429/5xx faults from `KeyProxy` (`ProxyStatus`) or structured harness JSON events, never by substring-scanning agent prose or test output.
  - Loopback API key proxy (`KeyProxy` in `modules/edge/worker/proxy.go`): binds strictly to `127.0.0.1` (`CheckLoopbackAddr`), holds one route per provider (`--providers`), reads each route's key from the worker's environment (`EnvKey`), enforces the per-route `AllowedHosts` allowlist (`403 Forbidden` on unlisted hosts), keeps each job's last provider failure (`RecordStatus` / `ConsumeStatus`, keyed by the job the request's placeholder names: an upstream 402/429/5xx, or 502 for an upstream that could not be reached or a response cut mid-body; never a request the harness itself hung up on; ended only by an answer to a request sent after it; the runner's launch loop hands the last launch's status to the harvest), sends the upstream its own `Accept-Encoding` so the harness gets plain bytes, flushes each chunk of a response as it arrives, aborts a response the upstream cut or that passes 16 MiB (the harness sees a broken connection, never a short body that ends cleanly), reaches the upstream over HTTP/1.1 when given no transport (a connection per request, so an upstream reset cuts one job's request; 32 idle connections kept per provider), and injects the real `Authorization: Bearer <key>` header outside the sandbox while the harness inside the wall receives only its job's placeholder `nova-proxy-placeholder-<consumer>` (`ProxyKey`; opencode: as each provider's `apiKey` in `OPENCODE_CONFIG_CONTENT`) (`Guarantee 12`).
  - Sandbox & slot wall (`BuildWallArgv`, `SanitizeWallEnv`, `OSReserveWall`): both `OSReserveWall.RunHarness` and `OSReserveWall.RunTest` (which runs each `DoneWhen` line as the argv `edge.ParseGate` gives (the one parser admission uses too; a line with shell syntax outside quotes is an error and is never run)) always wrap execution in `nova-reserve run ... --wait <d> --why <consumer> -- nova-sandbox --write <jobDir> --read <toolchains...> [--net-deny [--net-allow 127.0.0.1:<proxyPort>]] --env <name>... -- <argv...>` (the adapter's `Needs` decide the network, below) via `edge.Exec`, recording the child PID and `NOVA_WORKER_NONCE` at start via `Cmd.OnStart` and granting `--write` only to `JobDir` (shared `.git` and alternates are `--read` only). `SanitizeWallEnv` enforces an exact variable-name allowlist (`PATH`, `LANG`, `LC_ALL`, `GIT_TERMINAL_PROMPT`, `NOVA_WORKER_CONSUMER`, `NOVA_WORKER_ROW`, `NOVA_WORKER_NONCE`, `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENCODE_CONFIG_CONTENT` only when it is exactly an `OpenCodeConfig` (one `@ai-sdk/openai-compatible` provider per routed provider name, `baseURL` that provider's route on one loopback proxy, `apiKey` a placeholder (`ProxyKey`), models `{}`), plus `HOME`, `TMPDIR`, and `XDG_DATA_HOME` rooted inside `jobDir`).
  - 9-step runner lifecycle (`ServeRow`, `StepRow`, `ProbeRow` in `modules/edge/worker/runner.go`, recovery in `recover.go`):
    * `store.ConsumerOpID(kind, epoch, consumer, gen)` is the one op id of a take or report, derived by the verb; a report the store refused is retried under the next link of that id's chain (`store.ConsumerRetryID`), and the worker checks its report's receipt against the id or a link of its chain (`store.IsConsumerOp`).
    * `StepRow` launches ready consumers up to `Width` concurrently, tracks in-flight consumers so concurrent wakes never double-launch or clobber a running job directory, and cancels/kills any in-flight consumer whose store state becomes `fail` (`WITHDRAWN`).
    * `OSGitOps` executes all bench `git` operations through `edge.Exec` with `CleanEnv: true`, `-c core.hooksPath=/dev/null -c core.fsmonitor=false`, and `Timeouts` (`Stage`, `Push`, `RebaseAndTest`); `RebaseOntoStream` returns `*FetchError` on a fetch failure so the pass propagates the failure without reporting a stale head.
    * `sendReportAndClean_internal` requires an accepted store receipt matching the result ID (`receipt.Accepted && store.IsConsumerOp(receipt.Op, req.ResultID)`) before marking `JobRecorded` or removing the job directory; `CLISprintClient.Report` returns a parse error if `--json` output fails to unmarshal, and a report the sprint did not acknowledge with exit 0 (`ErrRefused` included) keeps its job record at `reporting` and is sent again, same id, on the next wake (stitch, row 20).
    * Each pass's recovery (`recoverRow_internal`) matches disk `JobRecord` entries against the store's working summary by full identity `(epoch, consumer, attempt, gen)` (`jobMatchesSummary_internal`), verifies both PID and `NOVA_WORKER_NONCE` via `OSProcessTable.Alive`, kills and reports `fault` for any adopted harness past its recorded `DeadlineAt`, leaves `JobTaken`/`JobStaged` (`ErrNoSlot`) records untouched for retry, and completes `rebaseAndPRSteps_internal` for `JobHarvested` `"ok"` jobs before reporting.
- `tla/Push.tla`, `tla/LandEdge.tla` (rewritten 2026-11-07 for the three rungs; its witnesses are now `LandEdgeRedLands`, `LandEdgeStaleGate`, `LandEdgeJudgementHoldsStream`, below), `tla/Runner.tla` and, as first built, 12 reversed witnesses in `tla/witnesses/` (`PushCursorBeforeConfirm`, `LandEdgeStaleGreen`, `LandEdgeWrongSHA`, `LandEdgePartialGroup`, `LandEdgeFaultBlamesHead`, `LandEdgeChaseBack`, `RunnerSlotFreedEarly`, `RunnerReusedJobDir`, `RunnerCleanBeforeReport`, `RunnerFreshResultIDOnRetry`, `RunnerRestartSpawnsSecondHarness`, `RunnerDieClassifiedFail`).

### Seam rulings (Rowan, 2026-11-06)

1. **Wake-up via `next --as <row> --json`, reports via `report` (`1794007469002-0`)**: There is no `queue` verb. On wake-up and on restart, `nova-worker` calls `SprintClient.Next(ctx, row)` (`nova-sprint next --as <row> --json` via `CLISprintClient.Next`) and acts on the row's `working` and `ready` consumers; `DEAL` overflow trailers end with `"and N more: nova-sprint next --as <row>"`. Reports apply directly to the store through `SprintClient.Report` (`CLISprintClient.Report` / `nova-sprint report --as <row>`) fenced on `Op.ID = ResultID`, never into a tick inbox.
2. **Pre-satisfied `After []OpID` and `EdgeResult` (`1794007773302-0`)**: Layer 3's `EdgeRequest` carries `After []OpID` (`After []string`); `run` dispatches an `EdgeRequest` to Layer 4 only after every operation in `After` has an accepted store receipt, so edges never see a request whose store precondition failed. Each edge returns `EdgeResult` (`steps.EdgeResult`) for the next tick's inbox.
3. **Every edge fact is re-queryable by `(epoch, kind, card, sha)` idempotently (`1794007844911-0`, `1794011583522-0`)**: Request IDs are `RequestID(epoch, kind, card, sha)` (`sha256(epoch + "\x00" + kind + "\x00" + card + "\x00" + sha)`). In-flight duplicate requests with the same `RequestID` are deduplicated inside the edge (`edge.Deduplicator`), not by `run`. Every edge answers requests as pure queries of the world at `(epoch, kind, card, sha)` without relying on process memory (`modules/edge/forge` queries GitHub check runs at `sha`; `modules/edge/git` checks ancestry and base tip on origin; `modules/edge/worker` inspects `job.json` and the store). Restarting the edge struct or process and querying `(epoch, kind, card, sha)` again returns the identical `EdgeResult`.
4. **Stream settings arrive in the edge request (`1794008719695-0`)**: A stream's `repo`, `base` branch, `pr`-required flag, and landing `mode` are policy data (a policy row of kind `data` named `stream:<name>`, JSON `{repo, base, pr, mode}`). Layer 3 puts `base` and `pr` into each landing `EdgeRequest`, so the git and forge edges take them as arguments and never read the store for them.

### Timeouts and bounds (`measured` vs `guess`)

| Parameter | Default | Status | Basis |
| :--- | :--- | :--- | :--- |
| `StageTimeout` | `120s` | **measured** | v1 mirror worktree staging ran 73–79 s median under load |
| `LaunchGrace` | `15s` (3 tries, `5–20s` then `30–60s` jitter) | **measured** | 9 of 40 launches in v1 died on a provider 5xx within 15 s |
| `TerminateGrace` | `3s` | **measured** | Process-group `SIGTERM` -> `SIGKILL` grace |
| `MaxMessageBytes` / `MaxFieldBytes` | `4096 B` / `300 B` | **measured** | v1 bus size discipline (`Guarantee 3`) |
| `MaxLogTailBytes` | `4096 B` | **measured** | Harness failure tail carried in report reason |
| `RebaseTestBound` | `2m` | **guess** | The stream merge, the seal and the apply of the merged head, each; never a done-when gate, which runs until the card's deadline less `PushTimeout` (a gate measured 524 s) |
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

- **Row 6, one pusher.** Round 3 had not deleted it: `modules/edge/bus` still held
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
    no clock is refused naming both, `nova-sprint worker` passes `time.Now` and
    `time.Sleep`, and every unit test passes a fake clock. The job record's
    deadline is the start plus the receipt's `deadline`; the `CIWaitExtra`
    margin (a v1 guess with no reader but this sum) is deleted with it.
  * *Row 15.* `TakeInfo` gains `model`, `deadline` (seconds, policy's
    `timeouts().consumer[tier]`) and `forge` (from the stream, `""` for a hand
    stream) and loses `require_pr`: a forge always means a PR, so the PR step
    runs when `forge` is set, through the adapter named by it
    (`Runner.Forges`, `nova-sprint worker` serves `github`); a forge with no
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
    literals in `nova-sprint worker` (now `--provider-upstream`,
    `--provider-hosts`, `--provider-key`, none defaulted; given none the
    proxy has no route and answers 502). `TestStitchNoDeadlineModelHostOrTierLiteralsInTheWorker`
    keeps them out. nova-config v2.0.2 has no table for provider hosts or
    coordinator hosts, so the service setup passes them from its inventory.
  * *Row 20.* The startup order is `Runner.ServeRow`: the bus hook (it claims
    the row's name and must say so), then `nova-sprint unhook --as <row>`
    (a refusal means no stale hook), then the probe (`ProbeRow`), then
    `nova-sprint hook --as <row> --role worker`, and only then `next --json`;
    `StepRow` never hooks. The `nova-bus hook` is the one long-lived child
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
  * *Owed, not done here.* `modules/edge/forge/forge.go` (outside this branch's scope)
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
  `forge` instead of merging or pushing. The worker's forge (modules/edge/worker,
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

- *`PromotePass`* (`modules/edge/git/land.go`, 2026-11-08) is one pass of a
  stream's base to main (LANDING.md "Landed means on main"): merges already
  on main land at main's tip; a merge not on base is `missing` and a base
  behind main is `base_behind`, both the coordinator's; otherwise the PR
  base -> main is gated at base's tip as rung 1 gates a card. `Git.Main` is
  the default branch and its tip at origin.
- *`LandPass`* (`modules/edge/git/land.go`, rewritten) is one pass of rung 1 or 2.
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
  `ci_unfinished` (a check failed while others run), `ci_no_verdict`
  (every failing check stopped, run again once), `ci_rerun`, `ci_fault`,
  `conflict`, `merge_refused`, `pr_unopenable`, `branch_failed`,
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
- *Run* (`modules/run/edges.go`): asks a pass again every `LandPoll` on
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
- Tests (unit, fakes): `modules/edge/git/git_test.go` (each rung and every outcome,
  the group branch's namespace), `modules/run/edges_land_test.go`
  (`TestAskAgainBoundsAnAbsentCI`, `TestLandItemRedCarriesTheFacts`,
  `TestLandByHandStreamTouchesNothing`), `modules/edge/forge` (Merge, FailedLines,
  the identity). Functional (real git): `modules/edge/git/functional_test.go`.
- *Models.* `LandEdge.tla` is the ladder: `Tick` (layer 3's landing),
  `Pass` (one `LandPass`), `BaseMoves`, `Answer` (land, fix, retry);
  invariants `BaseNeverRed`, `NothingLandsUnpassed`, `MergingCovered`, each
  with one reversed witness (`tla/make-landedge-witnesses.py`); reach
  checks `LandEdgeNever{RungTwo,Judged,HandLand,RedGated}`. The six old
  LandEdge witnesses and `modules/edge/git/coldread_test.go` (probes of the old
  ladder's defects) are removed. The trace walk
  (`modules/edge/git/trace_test.go`, `TestLandEdgeTraceRefinementWalk`: `LandPass`
  on the git fake and a truthful forge, 8 seeds walked to the end) writes
  `landedge-walk-<seed>`, checked against `LandEdgeTrace.tla`.
  Measured on the bench (TLC 1.8.0): `LandEdge.cfg` 46,288 distinct states
  in 4 s, no error; the 3 witnesses and 4 reach checks violated as
  required; the 8 walks (8 to 17 records each) all `TRACE OK`.

### Clone addresses (stitch, 2026-11-07, defect 21)

`stream add <s> --repo mas-bandwidth/nova-tools --forge github`, the form the
help prints, was refused at the fleet: the git edge passed `mas-bandwidth/nova-tools`
to `git clone` and `git ls-remote` as a path. `modules/edge/git/url.go` adds
`CloneURL` and `IsOrgRepo`; `Client.Tip` reads a branch at an address with no
clone; `Config.GitHubCredential` sets gh as git's github.com helper. Callers:
`modules/run/edges.go` (`Edges.clone`, for land and heads), `cmd/nova-sprint/edges.go`
(`origin.Tip` no longer clones; `gitEdge` sets the helper when `GH_TOKEN` or
`GH_CONFIG_DIR` is set), `modules/verbs/stream.go` (`baseTip`, shared by `stream add`
and `check`) and `modules/verbs/head.go` (finish). Not changed here: the worker's
staging still takes `--origin` (the per-stream-repository staging is another
change; it should take its address from `git.CloneURL(receipt.repo,
receipt.forge)`). Unit tests: `modules/edge/git/url_test.go`,
`modules/run/edges_test.go`, `modules/verbs/stream_url_test.go`,
`cmd/nova-sprint/edges_test.go`. Not run here: a clone of a private GitHub
repository through the gh helper (functional, on a bench).
### The worker after the first bench runs (2026-11-07, stitch child c4)

The first `nova-sprint worker` runs on a bench found these; each is fixed
with a unit test (fake wall, fake exec, fake sprint; no process, socket or
clock), mostly in `modules/edge/worker/probe_test.go`.

- **Harness directory (defect 24).** `OSReserveWall.RunHarness` grants
  `--read` on the directory `exec.LookPath` finds the harness binary in and,
  when that is a symlink, on its target's directory (`HarnessDirs`); a binary
  it cannot find is left to the wall's own refusal, which names the path.
- **Toolchain grants (`--wall-read`).** A card's done-when (`go test ...`)
  runs in its own wall (`OSReserveWall.RunTest`), and the harness may run it
  in its wall too; neither could read a toolchain outside the system
  directories. `--wall-read <dir>` (repeatable) is `WallConfig.ReadPaths`:
  `--read`, never `--write`, on every job, test, step and probe wall.
  `CheckWallReads` refuses a relative path, a missing directory, the home or
  a directory holding it, anything under a dot-directory of the home, and
  any overlap with `--root`, `--state-dir`, `--work-root`, `--mirror-root`
  or `NOVA_SECRETS_STORE` when set. At start the worker runs
  `go env GOROOT GOMODCACHE` outside the wall (`DetectGo`; no go on PATH is
  no Go) and grants both read-only as if passed with `--wall-read`, after
  the same checks; a detected directory they refuse is skipped with one
  start line saying why, and the worker starts. `GoWallEnv` sets `GOROOT`,
  `GOTOOLCHAIN=local` and `GOROOT/bin` first on `PATH` inside every wall
  only when a grant covers GOROOT, and `GOMODCACHE` with `GOPROXY=off` only
  when a grant covers the cache; one start line names what was granted.
  `GOCACHE` stays go's default under the job's own home. `~/.cargo/registry`,
  when it is there, is granted read-only too (the dot-directory rule is
  waived for it alone; the overlap checks still apply, and a registry that
  resolves elsewhere gets every check), with one start line; `CargoWallEnv`
  links it, through an `os.Root` on the job directory, as the job's own
  `home/.cargo/registry` and sets
  `CARGO_NET_OFFLINE=true`, so cargo builds from the bench's crates under
  `--net-deny` and a crate the bench lacks fails at once, named.
- **The bench's SDK environment.** When `~/sdk` is granted and
  `~/sdk/env.sh` is there (the fleet's sdk-toolchain role writes it), the
  worker says so at start, and before every wall command
  (`sdkWallEnv_internal`) sources it outside the wall in the command's own
  environment, the job's `HOME` included, and passes what it exports with
  `--env`: its `PATH`, the make variables and the toolchains' homes, as a
  make run on the bench gets them. `HOME`, `TMPDIR`, `XDG_DATA_HOME`,
  `DOTNET_CLI_HOME` and the `NOVA_WORKER_` names keep the wall's values. An
  env.sh that fails refuses the command, naming the file.
- **A FIFO jobserver for every gate.** cargo's own pipe jobserver adds a
  `pre_exec`, so Rust's std forks through `socketpair(AF_UNIX,
  SOCK_SEQPACKET)`, which the wall's seccomp refuses: rustc is never
  executed. Before each done-when gate `RunTest` makes
  `<jobDir>/jobserver.fifo` (in the wall's `--write` tree), holds it open
  read-write with N-1 tokens, and passes
  `CARGO_MAKEFLAGS=-jN --jobserver-auth=fifo:<path>` with `--env` (replacing
  any the bench set); with a `fifo:` jobserver std uses posix_spawn. N is one
  nova-reserve slot at its default capacity (CPU/4 slots): 4, or the
  bench's cores when fewer. Every gate gets it (a non-cargo gate ignores it),
  and the FIFO is removed after the gate. rustc runs its linker by name
  under its own `PATH`, which std also spawns by fork, so on Linux the gate
  passes `CARGO_TARGET_<host>_LINKER` set to the absolute `cc` on the
  worker's `PATH` (the glibc triple on amd64 and arm64; it overrides a
  project's own linker setting). Whatever the job left at the FIFO's name is
  removed, never followed, and the tokens are written without blocking. A
  cargo too old for `fifo:` still makes its own pipe jobserver and meets the
  refusal. `TMPDIR` is already the job's
  `tmp` (cc needs it). The wall is unchanged: no `--unix`. Code under test
  that itself spawns with a `pre_exec` (or sets uid/gid) still meets the
  refusal.
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
  every probe refusal says its judgement could not be opened. Resolved in
  v2.0.11 by removal: the worker's `OpenJudgement` call path (the
  `SprintClient` method, its CLI argv, `probeRefusal_internal`) is gone, and
  no new verb or judgement kind was added. A failed probe is the worker's
  refusal alone, in its log; `nova-sprint check` is the surface (`CHECK RED
  worker <row>: ...` with the unit's last log line). Test:
  `TestProbeRefusalRunsNoJudgementVerb`.
- **Each adapter's wall (32, intent rule 31).** `Harness.Needs()` declares
  `Proxy`/`NetOpen`, `Writes`, `ListModels`, `LogDir`, `Provider`,
  `Model` and `Limits`; `BuildWallArgv(cfg, job, env, argv)` honours
  `job.Needs` and passes every environment name but PATH, HOME, LANG and TERM
  with `--env` (nova-sandbox drops the rest otherwise; read from its help, not
  measured). No adapter gets `--unix`. opencode: the proxy's port (1.18.20
  runs strict on Linux, measured on amd-3, 2026-11-10). claude and codex:
  network open (each reaches its own provider with its own login), their
  config directory a `--write`. A probe that exits non-zero in its first second with an empty
  log directory (or, with none declared, no output but the WALL line) says
  "the harness died inside the wall before writing a log; nova-sandbox show
  <flags> -- <harness>".
- **opencode's providers (31, v2.0.11).** `OpenCodeConfig` declares each
  model under an OpenAI-compatible provider (`@ai-sdk/openai-compatible`)
  whose `baseURL` is the proxy's route for it (`ProxyBaseURL`) and whose
  `apiKey` is the placeholder; `OpenCodeHarness.Command` passes it in
  `OPENCODE_CONFIG_CONTENT` (read by opencode 1.18 and 2.2), the job's model
  for a run and every routed tier candidate for the probe's `opencode models`.
  Still said in `Limits`: opencode 2.x needs `--standalone`, which 1.x refuses
  and the adapter does not pass (run 1.x); the write tool works inside the
  strict wall (33; amd-3, 2026-11-10).
- **Sprint verbs (35).** Run directly (`<sprint-bin> <verb> ...`), inheriting
  the worker's environment, which the seat's exec filled; `--secrets-bin` is
  gone.
- **Any bench (41).** `CheckHost`, `Runner.CoordinatorHosts` and
  `--coordinator-hosts` are gone.
- **A clone per stream repo (42).** `OSGitOps{WorkRoot, MirrorRoot}`: the
  repo is the receipt's, its clone URL a URL or path as it is, or
  `https://github.com/<owner>/<name>.git` for owner/name on the github forge
  (`CloneURL`, a local helper; the stream edge's own clone URL is another
  child's); its mirror `<mirror-root>/<name>.git` (the bench's object
  cache, made empty on first use), its work clone `<work-root>/<name>` (made
  empty on first use, borrowing the mirror's objects, once per URL under a
  lock), and a work clone of another repo in the way is refused. Staging
  fetches shallow (2026-11-08, `stage.go`): a pass's one fetch per repo
  takes only the commits its jobs check out (base, prior head, head) and
  the tips of the branches they name (the stream's base, the card's branch,
  by one `ls-remote`), at depth 1, never every branch; the mirror from the
  clone URL under a bench-wide lock, the work clone from the mirror. A head
  that shares no fetched commit with its base is deepened for (64, 1024,
  then unshallow), since the rebase and the job's sandbox need the fork
  point. A deep mirror and work clone from before stay deep. A job's tree
  and the worker's clone share the work clone's objects and copy its
  shallow boundary (`clone --shared` of a shallow repo copies objects). The worktree's
  origin is the clone URL, so its fetch, push and the clean step's head check
  go to the repo. Flags: `--work-root` (default `<root>/clones`) and
  `--mirror-root` (default `~/nova-bench/mirror`) replace `--work-clone`,
  `--mirror` and `--origin`.
- **The flag set** of `nova-sprint worker`: `--as <row>`, `--root`,
  `--state-dir`, `--harness`, `--work-root`, `--mirror-root`, `--providers`
  and `--wall-read` (repeatable).
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
- A failed probe opens no judgement (see "Judgement errors (30)"): with
  this order the row stays unhooked, its worker log says why, and
  `nova-sprint check` reports it red with that log line.
### Built: the worker's heartbeat (stitch, 2026-11-07)

- `modules/edge/worker/beat.go`: with a `Meter` and a `Beater` set (`Serve` sets both), `ServeRow` pushes
  `nova-sprint beat` (layer 6) after the probe and then every `BeatEvery` (30 s, a guess) until
  the worker ends, on the runner's `Sleep`. `OSMeter` reads the 1-minute load average
  (`/proc/loadavg` on Linux, `sysctl -n vm.loadavg` on macOS and the BSDs, nothing on Windows),
  the logical cores (`runtime.NumCPU`), and the slots from `nova-reserve status`'s `free=n/N`
  line. A bench with no readable load still beats, with load 0 and a warning, and one with no
  readable slots beats with 0 of 0 and a warning: the cores, tiers and version the deal needs
  are always sent. Each distinct failure is warned once, not every period. The worker
  keeps only the last warning's text; a beat that repeats the last one writes nothing.
- Choice: a periodic verb of the worker's own, not the hook's presence (the bus hook is nova-bus's
  and carries no numbers) and not a read verb that writes. Tests: `modules/edge/worker/beat_test.go`.

### Built: the landing's red split, its timing, and the host identity (stitch w3L, 2026-11-07)

Directives 59, 76; LANDING.md "A red base". (The per-job split by the merge
base, `forge.Inheritance`, `Authors`, `IsAgreement` and `ci_red_inherited`
were removed 2026-11-08: a red base is the coordinator's judgement, not
classified here.)

- *`LandPass`* (`modules/edge/git/land.go`): a red rollup is `ci_red` once
  every check completed and one failed on its own (2026-11-11: no longer at
  once; LANDING.md, "A verdict only from completed checks"), with
  `FailingLines`; the pass also reads the base tip's own checks
  (`Checks` on `BaseTip`) and, when the base is red too, fills
  `LandResult.BaseFailing`. Run's `landFacts` then leads with "the base
  itself is red at its tip <sha> on: ...; the card's head is red on: ...".
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
- *Model.* `LandEdge.tla` unchanged: in the model every non-green,
  non-absent gate is one judgement.
- *Tests (unit, no network).* `modules/edge/git/red_test.go`
  `TestLandRedOnARedBaseNamesBothSides`; `modules/run/edges_land_test.go`
  `TestLandItemRedBaseNamesBothSides`; `cmd/nova-sprint/edges_test.go`
  `TestIdentityNamesTheChoice`; `modules/edge/forge` `TestForgeActsAsTheHostsGh`
  now sets `GH_TOKEN` and `GH_CONFIG_DIR` and requires neither reaches gh.

- `edge.ParseGate` (`modules/edge/argv.go`) is the one parser of a DONE-WHEN line.
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
  `cmd/placebin`: a new file beside the old, `codesign --force --sign -` on
  darwin, rename over, never an open of the old file for writing. Tests:
  `cmd/placebin/place_test.go` (the destination is a different file after
  and the old open file keeps its bytes).

### Built: the worker's live defects (stitch w3W, 2026-11-07)

From the first bench runs (directives items 61, 65 to 74). Files:
`modules/edge/worker/pass.go` (passes and jobs), `recover.go` (restart, release,
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
- **Probe failure never exits (61).** `ServeRow`
  holds the row unhooked with the probe's reason on stderr and probes again
  after `ProbeBackoff` (30 s) doubling to `ProbeBackoffMax` (10 min), both
  guesses.
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
  it started has its changes inside the card's PATHS committed by the
  worker's seal step (`--no-verify`, the row's identity) and pushed; what
  is outside PATHS (build output) is named in the step log, never
  committed; ok with nothing inside PATHS to commit is a fail saying so.
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
  in `modules/edge/git`), the PR assertion of
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
- **Merged with deal is take (measured, a bench host, TLC 2.19, 2026-11-07, head a675488a).**
  `Take` is now the pass's acknowledgement of a card dealt working (no ready state, no width
  guard: the deal holds it). `Release(c)` gives back a job the restart cannot resume (staged or
  harvested, directory present, no live harness): the consumer is withdrawn (fail, no result,
  no report), its commits pushed, its directory gone. A card in hand with no job directory is
  no release: the pass runs it. `Runner.tla` with `Runner.cfg`: no error, 16,928 states
  generated, 4,245 distinct. The whole of `ci-tlc.sh`: 266 ok, 0 FAIL (Runner, the eight Runner
  witnesses and the four Runner reach checks among them). `make-traces.sh check`: 88 ok, 0
  FAIL; the runner walks check at 22, 29 and 30 steps.

### Built: a branch is its holder's (stitch w4W, 2026-11-07, defect 78)

What happened on the live fleet: a bench's work consumer was withdrawn and
its card re-dealt to another row. Every recovery pass found the consumer
final and called `withdrawRunningJob_internal`, which ran
`harvestPartialPush_internal`: `git push --force` of the job's dirty head to
the card's branch. `cleanJobDir_internal` kept the directory (a dirty tree;
the Go module cache inside it is read-only, so a plain remove fails too),
so this happened again on every pass for as long as the worker lived, each
time rewinding the new holder's branch to the stale head. Files:
`modules/edge/worker/retire.go` (new), `runner.go`, `recover.go`, `pass.go`,
`worker.go`.

- **One push, by lease.** `OSGitOps.Push` (the worker's only `git push`)
  is `push --force-with-lease=refs/heads/<branch>:<expect> origin
  <sha>:refs/heads/<branch>`; `GitOps.Push` takes `expect` ("" : the
  branch must not exist). Every push goes through `pushBranch_internal`,
  which refuses a job that no longer holds its consumer and leases from
  `leaseFrom_internal`: the head this job last pushed (`PushedHead`), or
  before its first push the tip it found at stage (`StartTip`, read by the
  new `GitOps.BranchTip` from the work clone the pass fetched; a record
  from before it falls back to `PriorHead`). Why the stage tip and not
  only the last pushed sha: a never-pushed job has no sha of its own, and a
  card branch can already exist (a cleared sprint's next epoch reuses card
  ids and branch names; the breaker `TestBreakStaleEpochJobReplaysOldReceipts`
  found it); leasing on an older tip than origin's can only refuse, never
  overwrite. A lint test (`TestWorkerSourcesHaveNoPlainForce`) fails on a
  plain `--force` in the worker's non-test sources.
- **The push sites, each now.** `harvestWorkHead_internal` (the harness's
  head): `pushBranch_internal`, lease from the stage tip. `rebaseAndTestStep_internal`
  (the rebased head): lease from the head just pushed; a refused push is a
  fault that reports the head that did land, so the next attempt's prior
  head is the branch as it stands. `harvestPartialPush_internal`: only for
  a job that still holds its card (`faultAndReportJob_internal`, an adopted
  harness past its deadline; `releaseNoBlame_internal`, once before the
  release, skipping a head already pushed). `withdrawRunningJob_internal`:
  pushes nothing (it stops the harness only).
- **Who holds.** `holds_internal`: the worker records, per epoch and
  consumer, the generation below which its jobs hold nothing
  (`markGone_internal`): every final consumer in a `next` read (all
  generations), every working one (below its generation), a release once
  recorded, a retired job.
- **Retire.** `finalJob_internal` (a final consumer's job, not in flight),
  a job of a stale generation (same epoch, `rec.Gen < w.Gen`: the store
  refuses its report anyway), the in-flight job withdrawn while its harness
  ran, and a recorded release all go to `retireJob_internal`: the record is
  written to `<root>/retired/e<epoch>/<consumer>@<gen>/job.json`, the
  directory made writable (`chmod` 0700 on every directory under it,
  symbolic links not followed) and removed. A removal that fails is raised
  once (a WORKER WARNING and the step log, with the hand fix) and the
  directory moved to `<root>/quarantine/`; the retired record means it is
  never tried again. A job of a stale generation retired frees its card for
  the pass to run at its new generation (before, it sat with
  "identity-mismatch-kept" every pass). An epoch mismatch is still kept.
- **Model.** `Runner.tla` gains `branch[c]` (tip writer none / job /
  holder, the next holder's write, the job's pushes after final), `Retire`,
  `HolderPush`, the holder guard on the job's own steps, the lease on
  `Push` and `Release`; `Withdraw` no longer cleans the job (that was the
  model hiding the bug). Invariants `NoPushAfterFinal`, `BranchOnlyByHolder`;
  liveness `FinalDirRemoved`; `CleanOnlyAfterRecordedAndPushed` now asks
  for the push only of a job that ended done (a retired one pushed nothing).
  Reversed witnesses `RunnerWithdrawnPushes`, `RunnerWithdrawnPushesRepeatedly`,
  `RunnerForcePushRewinds` (the live bug: no retire, no lease); reach checks
  `RunnerNeverRetired`, `RunnerNeverHolderPushedOverJob`. The trace walk
  binds Withdraw, Retire (the worker's own `finalJob_internal`, on a fake
  origin that records every push) and HolderPush; every walk push goes
  through `pushBranch_internal`; eight seeds are written, and the test
  fails if together they miss any of Push, Report, Clean, Release, Stop,
  Withdraw, Retire, HolderPush.
- **Tests.** `retire_test.go`: the lease argv; the lint; a normal job's
  two leases; the defect's path (three passes over a final consumer: no
  push, the holder's branch untouched, one removal of a tree with a
  read-only module cache, the retired record kept); a failed removal
  raised once and quarantined; a stale generation retired with no push;
  the gate after final (and not across epochs); the release's one leased
  push, refused when the holder moved the branch.
- **Checked (measured, a bench host, TLC v1.8.0, 2026-11-07, head b5f98342).** `Runner.tla`
  with `Runner.cfg` (both new invariants, `Liveness` and `FinalDirRemoved`): no error, 67,836
  states generated, 16,839 distinct (was 16,928 and 4,245). Each new witness reports its
  target violated, and its counterexample was checked against the base code by hand:
  `RunnerForcePushRewinds` is Take, Stage, Withdraw, HolderPush, StalePush (the live
  sequence: the withdrawn job's pass force-pushes over the new holder);
  `RunnerWithdrawnPushesRepeatedly` is Take, Stage, Withdraw, StalePush, StalePush;
  `RunnerWithdrawnPushes` is Take, Stage, Withdraw, StalePush. (The live job was at running
  with a harness gone; the model's Withdraw takes a job with no harness, so the witness
  withdraws it at staged: the same push, from the same kept directory.) The eight older
  Runner witnesses and all six Runner reach checks (the two new ones among them) are violated
  as required. `make-traces.sh check`: 93 ok, 0 FAIL; the eight runner walks check at 22 to
  28 steps. The whole of `ci-tlc.sh`: 263 ok, 0 FAIL.

### Built: the job commits as the sprint's identity (stitch w4I, 2026-11-07)

- The per-row author (`<row> <row@nova-sprint.local>`, `CommitEmailDomain`) is gone. The take
  receipt carries the sprint's identity (`commit_name`, `commit_email`, store round 24);
  `buildJob_internal` sets `Job.CommitName`/`CommitEmail` from it and `checkoutJob_internal`
  writes them into the job's clone (`git config user.name/user.email`). A receipt without them
  is an `ErrReceipt` (the fault names it and the worker ends: an older nova-sprint); staging a
  job with none (a record resumed from before) is refused, never committed as the bench. The
  harness's environment was already clean of `GIT_AUTHOR_*`/`GIT_COMMITTER_*` and its HOME is
  the job's, so the clone's config is what its commits carry; the worker's own `CommitAll`
  commits in the same clone.
- `CommitRules(name, email)` (modules/edge/worker/identity.go) is the rule word for word in hook's
  output, take's text, JOB.md and the prompt: the identity, the two `git config` lines, the
  `By:`/`Co-Authored-By:` rule, and that finish refuses another email. `ValidIdentity` refuses a
  value the printed lines could not carry (a quote, backslash, `$`, backtick, angle bracket or
  control character; an email that is not one `local@domain`).
- The git edge gains `Client.Commits(repoDir, head, not...)`: one `git log
  --format=%H%x1f%an%x1f%ae%x1f%cn%x1f%ce head --not <bases> --`, the commits finish checks
  (`TestCommits`).
- The proof up and check give for each bench (directive item 4), chosen as the cheapest real
  one: a bench's job clones take the identity from the receipt by construction, so what can
  differ on a bench is the binary (step 7's version check per bench, already there) and whether
  the sprint has an identity at all (step 7 asks for it first; `hook --role worker` refuses
  without one, so a bench that is hooked and up hooked under one). What this proves: every
  bench runs code that commits as the store's identity, and the store has one. What it does not
  prove: that the account is on a repository's allowlist (no forge exposes that; the agreement
  warning names the identity to sign for), or what a friend's own clone is set to; finish's
  check of every head's commits is the proof for both. Reading each bench's clones over ssh was
  rejected: an idle bench has no job clone to read.
