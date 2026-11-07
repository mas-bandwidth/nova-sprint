# Layer 4: the edges

An edge is what nova-sprint does outside the store: send a message over
nova-bus, move git, ask GitHub, run work on a bench. Each edge is a Go
interface with a fake for unit tests (no sockets, clock or network) and one
real adapter that execs the outside tool, which is tested in the functional
tier.

Rules every edge follows:

- No edge runs under the store's writer lock. Layer 3 (`steps/`) gathers
  facts through the edges before a tick's op and acts through them after it.
- An edge decides no move. It reports facts (a send confirmed, a head on
  origin, a check green) and layer 3 and policy decide from them.
- An edge runs outside programs only through `edge.Exec`: an argv, never a
  shell string.
- Every refusal names its fix.

## Packages

| Package | What it is |
|---|---|
| `edge` | `Exec` (the one way to run a program), failure text and redaction, the gate parser (`ParseGate`), `RequestID`, `Deduplicator`, `FakeExec` |
| `edge/bus` | `Bus`: `Send` and `Who` over the `nova-bus` binary |
| `edge/git` | `Git` and `Client`: fetch, ls-remote, ancestry, group merge, rebase, push; `LandPass` (one pass of the landing); `CloneURL`; the group branch namespace |
| `edge/forge` | `Forge` and `Client`: PRs, check rollups, merge, failed-log lines, red-versus-base split, over the `gh` binary |
| `edge/worker` | `nova-worker`: the per-bench runner that takes a dealt card, runs a harness on it inside a wall and reports |
| `cmd/nova-worker` | the `nova-worker` binary; `nova-sprint worker` runs the same code (`worker.Serve`) |

The run loop (`run/`, layer 6) is the only caller of the bus, git and forge
edges. `run/edges.go` runs landing requests; `run/push.go` builds and sends
messages.

## `edge.Exec`

```go
type Exec interface {
    Run(ctx context.Context, cmd Cmd) (Result, error)
}
```

`Cmd` carries `Argv`, `Dir`, `Env`, `CleanEnv`, `Timeout`, `MaxBytes` and an
`OnStart(pid)` callback. `OSExec` is the real implementation.

Behaviour of `OSExec.Run`:

- `ValidateCmd` refuses an empty `Argv[0]` and NUL bytes in argv, dir or env
  (`invalid_argv`).
- stdin is closed. The child runs in its own process group (`Setpgid`) and a
  timeout or cancellation kills the whole group.
- `Timeout` defaults to 30 s (`DefaultTimeout`); callers pass a policy
  number. stdout and stderr are each capped at `MaxBytes` (default 64 KiB,
  `DefaultMaxBytes`), keeping the head and the tail with a
  `...[truncated]...` marker between them; `Result.Truncated` says so.
- `Env` entries are appended to the process environment, or are the whole
  environment when `CleanEnv` is set.
- A child that exits non-zero is not an error: `Result.ExitCode` carries it.
  An `*ExecError` is returned only when the run could not complete, with
  `Kind` one of `invalid_argv`, `missing_binary`, `timeout`, `start_failed`,
  `canceled`. `canceled` means the caller's context ended (a worker
  stopping), which is never reported as a timeout. `missing_binary` names the
  install fix (`nova-*` tools: install nova-tools or run `nova-fleet up`;
  `gh`: install it and `gh auth login`; `git`: install it).

Failure text (`edge/failure.go`). A failed child becomes a `*CommandError`
holding the redacted command line (the program's base name; environment
entries by name only, never value), the exit code, every non-empty stderr
line (up to 2000 bytes, nova-secrets' banner dropped) and the first lines of
stdout. `Redact`/`RedactN` replace `NAME=value` values, URL credentials,
password words and key-like tokens, and cut absolute paths to their base
name. `ParseFailure` and `ParseRefused` describe a parse failure with the
byte offset and the first 200 redacted bytes seen. `Refusal{Problem, Fix}` is
the error type verbs pass through to the user unchanged. `CauseLine` picks
the line of a harness's output that says why it failed (ANSI escapes
stripped; first line mentioning error, refused or failed, else the last
non-empty line).

`ParseGate(line)` is the one parser of a card's DONE-WHEN line, used by
admission (add, check) and by the worker that runs the line, so what is
admitted is what runs. A line is words split at white space; single or double
quotes join what they enclose, with no escapes and no expansion. Outside a
quote these are refused as shell syntax: backquote, `;` `&` `|` `<` `>`
`(` `)`, backslash, `$(`, `${` and `$` before a letter or `_`. The first word
must be a program: a path (contains `/`), or a lower-case-letter-or-digit
start followed by letters, digits and `. _ + - @`; a shell builtin (`cd`,
`export`, `source`, ...), a `~` path or a `NAME=value` assignment is refused.
`GateFix` is the standard fix text: a gate needing a shell is written
`sh -c '...'`.

`RequestID(epoch, kind, card, sha)` is `req-` plus 24 hex characters of the
SHA-256 of the four fields: the name of an edge fact. `EdgeRequest` and
`EdgeResult` are the shapes of a request from layer 3 and its answer.
`Deduplicator.Do` coalesces concurrent calls with the same request key so
the work runs once and every caller gets the same result; it remembers
nothing once the call completes.

`FakeExec` is the unit-test double: queued or handler-driven outcomes per
program, a recorded call list, a settable missing binary, a fake clock
starting at 1000.

## The bus edge (`edge/bus`)

```go
type Bus interface {
    Send(ctx, to, subject, body string) (msgID string, err error)
    Who(ctx) (map[string]Presence, error)
}
```

`Edge` runs `nova-bus send --as sprint --to <to> <subject> <body>` and
`nova-bus who`. `Reply` is `send` with `--re <id>`. It holds no cursor and
reads no store; it never parses a message's content.

- `Send` refuses an empty recipient or subject, and a body over 4096 bytes
  (`MaxMessageBytes`), before running anything. It returns the message id
  only when nova-bus exits 0 and prints a line `<recipient> <id>`. A non-zero
  exit, a timeout (default 5 s) or output without that line is an error
  ("leaving cursor unmoved"), and the caller does not advance anything.
- `Who` parses `WHO OK present=<n>` then one `name<TAB>uptime[<TAB>detail]`
  line per name; any other shape is refused with the first bytes seen.
- nova-bus refuses a send from a name that is not hooked. `nova-sprint up`
  hooks `sprint` with a service hook whose exec is `nova-sprint bus-reply`.
  `bus-reply` (`verbs.BusAnswer`) reads only the sender, the message it
  answers and the subject. A reply, or a message that is not a verb, is
  ignored with one log line; a message whose subject is a verb is answered
  once with where to run it ("the sprint takes no verbs over the bus").

Presence. `nova-bus who` shows that a hook process holds a name, not that a
session is acting behind it, so a row counts as seen when a verb runs as the
name, never from `who` alone.

### What the run loop sends (`run/push.go`, `run/notices.go`)

Messages are built from committed events, so nothing is sent for an op that
did not commit.

| subject | to | when |
|---|---|---|
| `DEAL` | the holder row | a card is cut into the row's hand (`working`) |
| `WITHDRAWN` | the holder row | a card it holds went `fail` while `ready` or `working` |
| `NEEDS YOU` | the coordinator | a judgement opened; lines grouped by judgement group |
| `LANDED` | the coordinator | a producer card reached `landed` |
| `EXPIRED`, `ANSWERED` | the coordinator | a judgement expired, or was answered by someone else |
| `NUDGE` | a row | a card in its hand it has not acknowledged (derived each tick from the view, no cursor) |
| `STOP` | a row | an edge `stop` request (a consumer to cancel) |

A recipient is a row with a session recorded. Each tick a recipient gets at
most one message holding every notice since its cursor: the subject of the
first notice, or `SPRINT` when notices differ. The body is cut at 4 KB with a
last line `and N more: nova-sprint next --as <row>`.

The cursor `push:<row>` moves to the last event covered only after `Send`
returned an id; a failed send leaves the cursor and is not retried within the
tick. A send that landed but whose output was lost is sent again, so
delivery is at least once and every message is safe twice (a take is fenced
on gen; a judgement is shown from its record). A `DEAL` line looks like:

```
DEAL card=rn-ring.w2 attempt=2 kind=work stream=rocketnet tier=pro model=<model> epoch=3 repo=<repo> base=<sha> branch=<branch> prior=<sha> reason="..." brief=...; in your hand now: run nova-sprint take rn-ring.w2@7 --epoch 3 --as amd-1.ds (acknowledges, prints the whole brief), then do it and run: nova-sprint finish rn-ring.w2@7 --head <sha> --as amd-1.ds --epoch 3
```

A read deal says `kind=read head=<sha>` and ends with `nova-sprint read <card>@<gen> yes|fix`.
Logs, diffs, secrets and the full brief are never in a message; `take`
prints the brief from the store.

## The git edge (`edge/git`)

```go
type Git interface {
    Fetch(ctx, repoDir string, refs ...string) error
    LsRemote(ctx, repoDir, ref string) (sha string, ok bool, err error)
    IsAncestor(ctx, repoDir, ancestor, descendant string) (bool, error)
    TreeHash(ctx, repoDir, rev string) (string, error)
    MergeGroup(ctx, repoDir, onto string, heads []string, ledgers []LedgerRule) (MergeResult, error)
    Rebase(ctx, worktree, onto string) (RebaseResult, error)
    Push(ctx, repoDir, sha, ref, expectOld string) error
}
```

`Client` (from `git.New(Config)`) implements it by running `git` through
`edge.Exec`. Every git call runs with `GIT_TERMINAL_PROMPT=0`, so nothing
ever waits for a prompt. Beyond the interface it offers `Tip(url, branch)`
(one `git ls-remote <url> refs/heads/<branch>`, no clone), `Heads(url)` (all
branches at an address), `Branches(url, prefix)`, `ListTree(dir, rev)`,
`Commits(dir, head, not...)` (author and committer of each commit, used by
`finish`'s identity check) and `Clone(repo, dir)` (once: a directory that
already holds `.git` is left as it is).

`Config`: `Binary`, `Exec`, `AuthorName`/`AuthorEmail`, `PushTimeout`
(default 60 s), `RebaseTimeout` (default 2 min), `Token`, `GitHubCredential`.

Behaviour:

- `LsRemote(ref)`: `(sha, true)` for a branch on origin, `("", false)` when
  absent. Given a 40-hex sha, it reports whether some origin tip contains
  it; an unfetchable tip makes the answer an error, never "absent".
- `MergeGroup(onto, heads, ledgers)` checks out `onto` detached and merges
  each head with `--no-ff` in the order given. One head that already
  contains `onto` is reused as is (`ReusedHead`). Merge commits are authored
  and committed as the sprint's identity, dated at `onto`'s commit date so
  the same inputs give the same sha (CI is not restarted by a repeated
  pass); without an identity the merge is refused. A conflict outside
  `ledgers` aborts and resets and returns `MergeConflict` naming the files.
  A conflict in a ledger file (a path in `LedgerRule`) is resolved by the
  rule's script if it has one (run in the repository with the "ours" side
  checked out), else by taking the union of both sides' lines; a failing
  script returns `MergeLedgerFailed`.
- `Rebase` runs `git rebase`; a conflict aborts it and names the files.
- `Push(sha, ref, expectOld)`: a ref under `sprint-group/` is rewritten with
  `--force-with-lease=<ref>:<expectOld>`. Any other ref with `expectOld` set
  must be a fast-forward (`IsAncestor(expectOld, sha)` is checked first and a
  non-fast-forward is refused before git runs); with no `expectOld` it is a
  plain push, which git refuses unless it fast-forwards. A refusal is a
  `*PushRefusedError` (`errors.Is(err, ErrPushRefused)`).
- Credentials. With `Token` set, the host's credential helpers are reset and
  one is added that answers with the token from the child's environment
  (`GIT_CONFIG_COUNT`; the token is never in argv). Otherwise, with
  `GitHubCredential` set, git's only credential helper for
  `https://github.com` is `gh auth git-credential`, run through `env -u ...`
  with every GitHub credential of the process unset, and an empty helper
  first clears the host's own list so a person's keychain never answers for
  the sprint. The environment names no secret.
- Commit identity. `git.WithIdentity(ctx, name, email)` sets the identity of
  every commit made under that context; it overrides `Config`'s.

### Clone addresses

A stream keeps its repository as given (`<org>/<repo>` is the forge's name
for it, used with `gh -R`). `git.CloneURL(repo, forge)` is the one place it
becomes what git clones:

| repo | forge | result |
|---|---|---|
| URL (`scheme://`), scp-like (`user@host:path`), absolute, `./`, `../` or `~` path | any | as given |
| `<org>/<repo>` | `github` | `https://github.com/<org>/<repo>.git` |
| anything else | `github` | refused, naming the form |
| any | `""` | as given (a hand stream names its repository by URL or path) |
| any | another forge | refused: the forges are `github` and none |

The scheme is https: the sprint's GitHub identity is the host's gh login (or
the `GH_GATE_TOKEN` override), and an ssh key registered with GitHub would be
a second credential to keep. `run` keeps one clone per address under its
repos directory, named by a hash of the address.

### Group branches

Rung 2 of the landing merges a stream's heads onto a branch named
`sprint-group/<stream>` (`GroupBranch`, `GroupRef`). It lives beside
`sprint/`, never inside it, because git cannot hold a ref that is both a leaf
and a directory. `GroupNameProblem` says why a stream name cannot be a branch
component (leading or trailing dot, `..`, `.lock` suffix). `GroupBlocker`
names an existing origin branch that would keep the group branch from being
created (`sprint-group` itself, or one under `sprint-group/<stream>/`).
`ValidateCardBranch` refuses a card branch that is empty or under
`sprint-group/`.

## The GitHub edge (`edge/forge`)

```go
type Forge interface {
    PR(ctx, repo, head, base string) (PRInfo, bool, error)
    OpenPR(ctx, repo, head, base, title, body string) (PRInfo, error)
    Checks(ctx, repo string, shas []string) (map[string]CheckRollup, error)
    Merge(ctx, repo string, pr int, sha string) (mergeSHA string, err error)
    FailedLines(ctx, repo, url string) ([]string, error)
    Inheritance(ctx, repo, base, baseTip, head string) (RedSplit, error)
    Authors(ctx, repo string, pr int) ([]string, error)
}
```

`Client` (from `forge.New(Config)`) runs `gh` through `edge.Exec` with a
30 s default timeout.

Credentials. gh runs in a clean environment: the process environment minus
`GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`
and `GH_CONFIG_DIR` (`forge.HostEnv`), plus `GH_PROMPT_DISABLED=1` and
`NO_COLOR=1`. It therefore acts as the host's own gh login. `Identity.Token`
(set from `GH_GATE_TOKEN` in the run process, `forge.TokenEnv`) is an
optional override passed to gh as `GH_TOKEN`. A worker row's token never
reaches this edge.

Behaviour:

- `PR` finds the open PR for `head` into `base` (`gh pr list`);
  `PRInfo.Mergeable` is GitHub's `MERGEABLE`, `CONFLICTING` or `UNKNOWN`.
  `OpenPR` returns an existing PR or creates one.
- `Checks` reads the status-check rollup of up to 100 commits per call
  (`MaxChecksBatch`) with one GraphQL query, following context pagination.
  It refuses a batch over 100, a repo that is not `org/repo` and a sha that is
  not 40 hex. A `CheckRollup` has a `State`:

  | state | meaning |
  |---|---|
  | `green` | every check succeeded, was neutral or was skipped |
  | `red` | a check failed; `Failing` names them, `URL` is the first one's, `Waiting` says some check is still queued or running |
  | `pending` | checks queued or running |
  | `absent` | no check has been reported for the commit (no workflow has queued a job) |
  | `fault` | the gate did not run: a check was cancelled, timed out, failed to start, needed action or was stale; or GraphQL returned a different oid |

- `Merge(pr, sha)` merges by the first method the repository allows (merge
  commit, then squash, then rebase; read once per repo) with
  `--match-head-commit <sha>`, so GitHub refuses if the PR head moved. It
  then reads the PR back and returns the merge commit; a PR that is left open
  (a merge queue or auto-merge took it) is an error, since the landing
  records only a merge it saw. It refuses `pr <= 0`, a non-40-hex sha, and a
  repo that allows no method.
- `FailedLines(url)` reads the log of the Actions job a check URL names
  (`gh run view --job <id> --log-failed`) and returns the first five lines
  that say error, fail or panic, else the last five.
- `Inheritance(repo, base, baseTip, head)` compares the head's failing
  workflows with the base: the merge base of `baseTip` and `head`
  (`gh api repos/<r>/compare`), each side's workflow runs, and, for a
  workflow with no completed run at the merge base, the base branch's last
  completed run of it. A workflow fails at the head when a run failed or is
  still running with a failed job. The result `RedSplit{MergeBase, Inherited,
  New}` holds `WorkflowRed` values (`Workflow`, `Basis` saying what the base
  side was read from, `Running`, failed `Jobs` with name, runner, URL). Logs
  are read only for new red: the first 3 failed jobs (`NewJobsMax`), at most
  8 MiB of log each (`LogMaxBytes`), 5 lines of at most 240 bytes
  (`LineMax`).
- `Authors(pr)` is the PR's distinct commit authors. `IsAgreement(name)`
  recognises a contributor or license agreement check (CLA, CAA, "contributor
  ... agreement").

Rate limits. A token bucket per client sized by `HourlyLimit` (default 5000)
and `Burst` (default 100) meters every gh call. gh output that signals a rate
limit (HTTP 429, "rate limit", a 403 mentioning a limit) rests the client
until the `Retry-After` it names (60 s when none). While resting, and when the
bucket is empty, calls return a `*FaultError{Kind: rate_limit}`: a fault to
the caller that spends nothing. `Client.RestedUntil` says until when.

## The landing pass (`git.LandPass`)

`LandPass(ctx, g Git, fg Forge, req LandRequest) (LandResult, error)` is one
pass of the landing for one stream. `LandRequest` holds the clone directory,
repo, stream, base branch, the heads to land (in landing order) and the card
branch each is the tip of; `Ledgers` is available for `MergeGroup`. The pass
never retries; every non-landed result is an outcome carrying its facts, and
the caller decides what to do. An error means git or the forge themselves
failed. A stream with no forge is refused (it lands by hand).

One head whose branch already contains base's tip is rung 1; any other group
is rung 2.

Steps:

1. Read base's tip and each card branch's tip from origin
   (`LsRemote`). A branch missing from origin is `missing_head`. Then fetch
   base and the heads.
2. If every head is already an ancestor of base's tip, the result is
   `landed` at base's tip with nothing to merge.
3. A card branch no longer at the head the request names is `head_moved`.
   Otherwise choose the gated branch and sha. Rung 1: the card's own branch at its
   head. Rung 2: `MergeGroup` the heads onto base's tip and push the result
   to `sprint-group/<stream>` with a lease on the tip just read (a push of
   the same sha is skipped, so a repeated pass does not restart CI). A
   conflict is `conflict` (naming the files); a failed push is `branch_failed`.
4. Find or open the PR from the gated branch into base (`pr_unopenable` when
   it cannot). A PR whose head is not yet the gated sha is `ci_pending`;
   GitHub's `CONFLICTING` is `conflict`.
5. Read checks at the exact gated sha (below).
6. Read base's tip again. If it moved since step 1, the result is
   `base_moved`.
7. `forge.Merge(pr, sha)`; a refusal is `merge_refused`. Success is `landed`
   with the merge sha and the heads landed.

Outcomes:

| outcome | meaning | run's reaction |
|---|---|---|
| `landed` | merged | tell layer 3 the heads and the merge sha; re-read base |
| `ci_pending` | checks queued or running, or only inherited red while the run goes on | ask again after `LandPoll` (30 s) |
| `base_moved` | base moved after the gate | ask again after `LandPoll` |
| `ci_absent` | no check reported for the gated sha | ask again until the request's wait (policy `ci_wait`; 10 min when none), then answer `no-ci` |
| `ci_red` | a check failed that is not red on the base (new red), or red that could not be compared | judgement |
| `ci_red_inherited` | the run ended and every failing workflow is also red on the base | judgement (worded for a glance) |
| `ci_fault` | the gate did not run (cancelled, timed out, not started) or its checks could not be read | judgement; blames no head |
| `conflict` | the heads do not merge onto base, or GitHub says the PR conflicts | judgement naming files |
| `merge_refused` | GitHub refused the merge | judgement |
| `pr_unopenable` | the PR could not be opened or read | judgement |
| `branch_failed` | the group branch could not be created or pushed | judgement |
| `missing_head` | a card's branch is not on origin | judgement |
| `head_moved` | a card's branch is not at the head the read passed | judgement |

A stream whose mode is `hand`, or that has no forge, is answered `hand` at
once without touching git or GitHub.

Red is split by the base. For a red rollup, an agreement check among the
failures reads `Authors` into `LandResult.Unsigned`; `Inheritance` fills
`MergeBase`, `Inherited` and `NewRed`; each failing check that is no Actions
workflow is new red (nothing on the base compares with it). New red is
`ci_red` at once, even while the run continues. Red that is all inherited is
`ci_pending` while the run goes on and `ci_red_inherited` when it ended. If
the split cannot be read, the pass waits while the run goes on, then is
`ci_red` with the failing log lines and the cause.

`LandResult` carries: outcome, rung, base tip, group tip, gated sha, merge
sha, PR, CI URL, failing checks, their first lines, conflicting files,
merge base, inherited and new red workflows, unsigned authors and a reason.
`run` words them for the judgement (`landFacts`): new red first (with each
failed job's runner, URL and first error lines), then inherited red, the
authors of an agreement failure, then rung, tips, gated sha, PR, failing
checks and the reason.

A landing never force-pushes base, merges a sha CI did not pass, merges
while base is not the tip it was gated on, rebases a card's branch, or runs
under the writer lock.

## The runner: `nova-worker`

One service per bench (`nova-worker --as <row>` or `nova-sprint worker`; the
same code). It serves one row, a machine nova-config gives the bench role
paired with a harness and models (`amd-1.ds`). Any such machine may run one,
the coordinator's included. Its binary is replaced by a new file and a
rename, never overwritten in place (`tools/placebin`).

### Flags

| flag | meaning |
|---|---|
| `--as <row>` | the row to serve (required) |
| `--host <name>` | the bench's hostname (default `os.Hostname`) |
| `--root <dir>` | job and probe directories (default `~/.local/state/nova-worker/<row>`) |
| `--state-dir <dir>` | where the step log goes (default `~/.local/state`) |
| `--harness <name>` | `claude` (default), `codex`, `opencode` or `script` |
| `--work-root <dir>` | work clones, one per stream repository (default `<root>/clones`) |
| `--mirror-root <dir>` | bare mirrors `<name>.git` (default `~/nova-bench/mirror`) |
| `--providers <spec>` | key-proxy routes, see below |
| `--once` | probe, hook, run one pass and exit |
| `--sprint-bin <path>` | `nova-sprint` binary (`nova-worker` only; default `nova-sprint`) |

The worker runs `nova-sprint` verbs directly, inheriting its own
environment, which the bench seat's `nova-secrets exec` filled; the
store's DSN reaches only those verb processes, never a harness.

### Startup

1. `nova-bus hook --as <row>` starts as the worker's one long-lived child
   (only `NOVA_BUS_*` in its environment). The worker waits up to 60 s for
   `HOOK OK name=<row>` on its stderr, which proves it holds the name.
2. `nova-sprint unhook --as <row>` removes a hook a dead worker left (a
   refusal means there was none).
3. The probe (below) must pass. Served (not `--once`), a failed probe never
   exits the worker: the row stays unhooked, the reason goes to stderr and the
   step log, and the probe runs again after 30 s, doubling to 10 min.
   `--once` exits with the probe's refusal.
4. `nova-sprint hook --as <row> --role worker`.
5. The first heartbeat is pushed, then a pass runs, then the wake loop.

Hooking after the probe means a hooked row is a probed row.

### Wake and pass

A line printed by the bus hook is only a wake. A pass reads the store through
`nova-sprint next --as <row> --json` and acts on what it says, so a lost,
late or repeated message changes nothing. A pass also runs every 10 s
(`WakeEvery`) without a wake. Wakes that arrive while a pass runs coalesce
into one more pass. If the bus hook dies the worker exits 1 and the service
restarts it; a refused take receipt also ends the worker.

A pass only plans: it recovers the row's jobs (below), reads the row, and
starts a job for each card in the row's hand (`working`) that has none yet.
A deal is the take: a card is `working` the moment it is dealt, within the
row's width, and the job's `nova-sprint take <card>@<gen> --as <row> --json`
is the acknowledgement that also prints the receipt. The jobs run detached
and no pass waits for one. Before staging, each repository the pass stages
from is fetched once, the repositories at once, under that clone's write
lock; staging takes its read lock.

Width and slots are separate counts. Width caps a row's `working` consumers;
nova-reserve's slots, shared with everything on the bench, cap running
programs.

### The take receipt

`take --json` prints, among the card's fields: `stream_ref`, `base_sha`,
`prior_head`, `paths`, `done_when`, `model`, `deadline` (seconds, policy's
timeout for the tier), `forge` (empty for a hand stream), `commit_name` and
`commit_email` (the sprint's commit identity). A receipt missing `model`,
`deadline`, `forge`, or the identity is refused with one line naming
`nova-sprint take <card>@<gen> --as <row> --json`; the card is reported
`fault`, the worker exits non-zero (every further card would be refused the
same way), and the fix is updating nova-sprint. The receipt is kept as
`receipt.json` in the job directory. The worker holds no model, deadline,
host, tier or provider literal: they come from the receipt and flags.

### A job, deal to report

State is kept in `<root>/jobs/<consumer>/job.json`, written atomically at
each step; the states are `taken`, `staged`, `running`, `harvested`,
`rebased`, `ciwait`, `reporting`, `recorded`, `cleaned`.

1. **Take.** Refused if the job directory already exists: a job directory is
   never reused.
2. **Stage.** The row's work clone for the repository (made on first use
   with `git clone --no-checkout --reference <mirror>`, after cloning the bare
   mirror `--mirror` if absent; a work clone of another repository in the way
   is refused) is fetched once per pass, never from the mirror itself. The
   worktree `<job>/repo` is a `git clone --shared` of the work clone whose
   `origin` is the repository's clone URL (`worker.CloneURL`: same rules as
   `git.CloneURL` for URL, absolute path, `git@`, and `<owner>/<name>` with
   forge `github`). A work card checks out its branch at `prior_head` (else
   `base_sha`); a read is detached at the head under review with its push URL
   set to `no-push-on-read`. The clone's `user.name` and `user.email` are the
   receipt's identity, never the machine's; staging with none is refused. The
   branch tip found on origin is recorded (`StartTip`) for push leases. The
   worker writes `JOB.md` (card, repository, base, checkout, prior head and
   finding, paths, done-when, the card's prose, and how the card finishes).
   A staging failure or timeout (120 s) is a `fault`.
3. **Run** the harness (below) inside `nova-reserve` and `nova-sandbox`. The
   slot lives exactly as long as the harness. No slot within the wait (5 s) is
   "no start yet": the job stays staged and the next pass runs it. A launch
   that dies inside the 15 s grace on a provider 5xx is retried up to three
   times with a jittered 5-20 s then 30-60 s backoff. The job record's pid
   and nonce are written before the harness is relied on, and the deadline is
   start plus the receipt's `deadline`.
4. **Harvest.** Outside the wall, the worker classifies the outcome, reads the
   harness's usage, and for a work card pushes the head: `git push
   --force-with-lease=refs/heads/<branch>:<expect> origin <sha>:refs/heads/<branch>`
   where `<expect>` is the head this job last pushed, or before its first push
   the tip found at stage. Every push in the worker goes through one function
   that refuses a job that no longer holds its card (below) and never uses a
   plain force. A harness that says ok but left its changes uncommitted with
   HEAD at its start has its tree committed by the worker (`git add -A`,
   `--no-verify`, the receipt's identity) and pushed; ok with nothing to
   commit is a `fail` saying so. On a wall death or a kill past the deadline,
   while the job still holds its card, whatever commits the worktree holds are
   pushed so the next attempt continues from them.
5. **Rebase and test** (work cards that said ok). Fetch the receipt's
   `stream_ref` and rebase onto it, then run each done-when line inside a slot
   (each parsed by `edge.ParseGate`, a line with shell syntax is an error and
   is never run), bounded by 2 minutes each, then push the rebased head under
   the lease. A rebase conflict is `fail` naming the files; a failed fetch is a
   `fault`; red tests are `fail` with the command and its output tail; a
   refused post-rebase push is a `fault` that reports the head that did land.
   After a report the stream moving is the landing's to absorb: a card's
   branch is never rebased again.
6. **Report** with a result id fixed before the first try, so every retry
   carries the same id: `nova-sprint report <consumer>@<gen> --result ...
   --head ... --id <id> --as <row> ...` (also `--reason`, `--finding`,
   `--cost-microusd`, `--priced`, `--usage`). Up to three tries while the store
   is unreachable. The report counts only when the receipt is accepted for that
   id. A refused or unacknowledged report leaves the job record at `reporting`
   and is sent again, same id, by every later pass, until the consumer turns
   final.
7. **Clean.** Only after the report is recorded, and for a work card only if
   the branch on origin is at the pushed head (otherwise `kept-unconfirmed-head`)
   and the worktree is clean (otherwise `DIRTY WORKTREE KEPT`), the job
   directory is removed.

The worker opens no PR and watches no CI: its contract ends at a pushed head.
The landing opens the group PR and reads CI on the group sha.

A card can never, from the bench: reach the store or the bus (no DSN, no bus
credentials, no sprint verb in its wall); start a listening server (the wall
refuses listen where it can); write outside its job directory; wait on a
prompt (stdin is closed; `sudo` has no terminal). Commit text goes in a file
(`git commit -F`).

### Results

`ClassifyJob` decides the outcome. A `result.json` in the job directory is
authoritative once written:

| harness wrote | outcome |
|---|---|
| `{"result":"ok"}` (or `yes`) with exit 0 | `ok` (a work card also needs a pushed head past `base_sha` and `prior_head`) |
| `ok` with a non-zero exit | `fail`, no verdict |
| `{"result":"fix","finding":"..."}` | `fix` (reads) |
| `broken` or `fail` | `fail` with the reason |
| `fault` | `fault` with the reason |
| any other value | `fail`, "unknown verdict" |

With no result file: exit 134 or 137 is a `fault` (killed); a provider HTTP
402, 429 or 5xx seen by the key proxy, or `{"http_status":N}` structured
output, is a `fault` (5xx within the launch grace is retried); otherwise a
work card is `fail: no verdict: ...` with the harness's last output (up to
4096 bytes become the reason). Wall or binary failures (exit 125-127,
`SANDBOX REFUSED`, a run that never completed) are a `fault`. A result.json
that does not parse is said in the reason. Faults spend no attempt and no
bounce.

A read reports only `ok`, `fix` or `fault`, never `fail`: the store refuses
it. A harness's own fail verdict on a read becomes a `fix` carrying its reason
as the finding; no verdict at all is a `fault`.

A run the worker's own stop cut short is "runner stopped": nothing is
reported, the job record is kept at its state with no pid, and the next start
resumes it.

### Harnesses

```go
type Harness interface {
    Name() string
    Command(j Job) (argv, env []string)  // pure, from the job
    Probe(j Job) Job                     // a one-turn job on the same path
    Classify(exit int, tail []byte) Outcome
    Usage(j Job) (Cost, bool)            // the harness's own accounting
    Needs() Needs                        // what its wall allows
}
```

`worker.NewHarness(name)`:

| name | command | wall needs |
|---|---|---|
| `claude` | `claude --print --dangerously-skip-permissions --disallowedTools Task [--model m] -- <prompt>` (subagent spawns refused) | network open (own login), config dir writable, provider `anthropic` |
| `codex` | `codex exec --full-auto [--model m] --cd <worktree> <prompt>` | network open, `CODEX_HOME` writable, provider `openai` |
| `opencode` | `opencode run [--model provider/model] <prompt>`; `OPENAI_BASE_URL` is the key proxy's route for the model's provider | `--unix`, key proxy only, lists models with `opencode models` |
| `script` | a fixed static shell program driven by `ScriptMode` (test double: probe, commit, fail, exit137, 5xx, ratelimit, read results) | none |

Card fields reach the script harness as positional arguments of a constant
program, never interpolated into a shell string. A model flag drops the
`provider/` prefix for a harness that serves one provider. A job with no
model runs the harness's own configured default (only the probe). The prompt
points at `JOB.md` and carries the same finish rules word for word: the forms
of `result.json`, no `git push` and no `gh`, and the commit-identity rule
(`CommitRules`). An adapter's `Needs` is declared in one place and
`BuildWallArgv` honours it; nothing else widens a wall.

`Usage` reads `usage.json` (a harness's own `micro_usd`/`priced` and tokens by
class); opencode falls back to its session database. Tokens are sent as
`--usage`; the engine prices them, never the worker.

### The wall

Every harness and every done-when command runs as

```
nova-reserve [--dir D] run [--slots <weight>] --wait <d> --why <consumer> -- \
  nova-sandbox --write <job dir> [--write <needs.Writes>...] --read <paths...> \
    [--unix] [--net-deny [--net-allow <proxy host:port>]] --env <NAME>... -- <argv...>
```

- `--write` is the job directory (worktree, `HOME`, `TMPDIR`, `XDG_DATA_HOME`
  all inside it) plus the adapter's declared writes. The shared git directory
  and the alternates of the clone are `--read` only, as are the directory of
  the harness binary (and of its symlink target) and the toolchains.
- The network is open for harnesses that reach their own provider, and denied
  with one allowed loopback port (the key proxy's) for harnesses that go
  through the proxy; it is denied outright otherwise.
- The environment is an exact allow-list (`SanitizeWallEnv`): `PATH`, `LANG`,
  `LC_ALL`, `GIT_TERMINAL_PROMPT`, `NOVA_WORKER_CONSUMER`, `NOVA_WORKER_ROW`,
  `NOVA_WORKER_NONCE`, `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENAI_BASE_URL`,
  `OPENAI_API_KEY` only when equal to the proxy placeholder, and `HOME`,
  `TMPDIR`, `XDG_DATA_HOME` rooted inside the job directory. Every name but
  `PATH`, `HOME`, `LANG`, `TERM` is passed with `--env`, since the sandbox drops
  the rest. stdin is closed.
- A harness's wall is shown without running anything by
  `nova-sandbox show <flags> -- <harness>` (`SandboxShow`).

### The key proxy

API-rate keys never enter the wall. `--providers
<name>=<upstream url>@<KEY NAME>[,...]` (for example
`deepseek=https://api.example.com@DEEPSEEK_API_KEY`) declares one route per
provider: `<name>` is the provider prefix of `provider/model` ids, the
upstream is an http(s) URL with a host and no credentials, and the key's
value is read from the named environment variable at each request (put there
by the seat's `nova-secrets exec --only ...`; a key is never a flag).

The worker starts a proxy on `127.0.0.1:0` only. The harness gets
`OPENAI_BASE_URL=http://<proxy>/<provider>/v1` and the placeholder key
`nova-proxy-placeholder`. The proxy picks the route by the first path segment
(and cuts it), or the `X-Nova-Route` header, or the one route there is; it
forwards only to that route's upstream host (403 otherwise), replaces the
`Authorization` header with the real key, and answers 503 naming the key and
the fix when the key is absent. It records an upstream 402, 429 or 5xx so the
runner classifies the run as a provider fault. With no routes it answers 502.
A harness that logs in itself (claude, codex) keeps its credentials in its own
config directory, granted into the wall; that is a weaker boundary and is
shown as such on the row.

### The probe

Before the row hooks in, and again after a failed probe, the worker runs the
adapter's probe job through the same wall, flags and proxy as a card:

1. Every route's key must be present, else the probe refuses naming the key
   and the exec line.
2. For a harness that takes a model, `next --json` names the policy's
   candidate models for each tier the row serves (`tier_models`). For each
   tier the first candidate the harness serves is chosen: a key-proxy route
   for its provider when it uses the proxy, its own provider otherwise, and
   present in the harness's own list when it has one. A tier with no candidate
   refuses ("no model for tier ..."); a tier none of whose candidates the
   harness serves is a warning and is not advertised; no tier served at all is
   a refusal naming each reason and the harness's list.
3. Each chosen model gets one probe run, with output kept in
   `<root>/probes/<row>/<model with / written _>/` (`stdout.log`, `stderr.log`,
   `command.txt`; the command's environment values are never written). A
   probe that exits non-zero within a second with an empty log directory says
   the harness died inside the wall before writing a log and prints the
   `nova-sandbox show` command.
4. The tiers served become the row's advertised tiers. A take runs the
   receipt's model; a model the probe proved the harness does not serve is a
   `fault` naming the model and the tier.

A probe refusal names the cause, the harness's known limits (`Needs.Limits`),
and where its output is kept.

### Heartbeat

After the probe and then every 30 s, the worker runs `nova-sprint beat --as
<row> --load1 <n> --cores <n> --slots-used <n> --slots-total <n> --tiers
<list>`. Load is the 1-minute average (`/proc/loadavg` on Linux, `sysctl -n
vm.loadavg` on macOS and the BSDs; none on other systems, which is warned),
slots come from the `free=n/N` line of `nova-reserve status` (a bench whose
slots cannot be read still beats, with 0 of 0). The advertised tiers give the
roster the row's real tiers and width (the lesser of nova-config's width and
the slots). Each distinct failure is warned once, not every period.

### Restart, withdrawal and retirement

The store is the truth. The runner keeps `job.json` and `receipt.json` per
job. On start and at every pass, `RecoverRow` matches each job directory
against the row's `working` and final consumers by (epoch, consumer, attempt,
gen):

- `reporting`, `rebased` or `ciwait`: the owed report is sent again under its
  recorded id.
- `running` with a live harness (pid and nonce both match): adopted; its
  deadline is the recorded one, and past it the harness is killed and the card
  faulted.
- Otherwise the job resumes from its receipt: `taken` is staged again;
  `staged`, or `running` with no live harness, runs again; `running` with a
  result file is harvested; `harvested` goes on to rebase and report. A job with
  no slot stays staged.
- A job with no readable receipt is released with no blame: the worker pushes
  the commits the worktree holds (once, under its lease, while it still holds
  the card), then `nova-sprint release <card>@<gen> --reason ... --head <pushed
  head>`; the card is redealt, with no fault and no bounce.
- A `working` card with no job directory is the pass's to run as a fresh take.

A branch belongs to the consumer that holds the card now. A job holds its
consumer while the consumer is not final and no newer generation of it is in
the row's hand. A job whose consumer is final (all generations), whose
generation is stale, whose in-flight consumer was withdrawn, or that was
released, is retired on the first pass that sees it, pushing nothing: its
record is written to `<root>/retired/e<epoch>/<consumer>@<gen>/job.json`, the
directory is made writable (`chmod 0700` on every directory, symlinks not
followed, since a Go module cache is read-only) and removed. A removal that
fails is raised once (a `WORKER WARNING` and the step log, with the hand fix)
and the directory moved to `<root>/quarantine/`; the retired record means it is
never tried again. A job directory of a different epoch is kept. A harness
still running for a withdrawn card is stopped.

A record whose report the sprint refuses three times in a row the same way
(`RefusalLimit`) is moved to `<root>/quarantine/<consumer>@<gen>` and raised
with what to read; a quarantined card is neither resumed, run afresh nor
released. The runner gates on no other daemon: with the store unreachable its
harnesses run on and its reports wait to retry.

### Logs and files

| path | contents |
|---|---|
| `<state-dir>/nova-worker/<row>.log` | one line per step: `ts row consumer step result dur_ms`; rotated at 10 MiB to `.log.1` |
| `<root>/jobs/<consumer>/` | `job.json`, `receipt.json`, `JOB.md`, `repo/`, `result.json`, `usage.json`, `stdout.log`, `stderr.log`, `command.txt`, `home/`, `tmp/`, `data/` |
| `<root>/probes/<row>/<model>/` | the probe's logs |
| `<root>/retired/`, `<root>/quarantine/` | as above |
| `<root>/clones/<name>` and `~/nova-bench/mirror/<name>.git` | work clones and bare mirrors |

The last 4 KB of a harness's output become the report's reason when the
result is not `ok`. Nothing a log holds is sent on the bus.

### Timeouts

Each is a number in `worker.DefaultTimeouts` or a package constant, and the
card's deadline comes from policy through the receipt.

| bound | default | basis |
|---|---|---|
| stage | 120 s | measured: staging ran 73-79 s median under load |
| launch grace | 15 s, 3 tries | measured: 9 of 40 launches died on a provider 5xx within 15 s |
| launch backoff | 5-20 s, then 30-60 s (jittered) | guess |
| terminate grace | 3 s | process-group SIGTERM then SIGKILL |
| rebase and each test | 2 min | guess |
| push | 60 s | guess |
| nova-reserve wait | 5 s | guess |
| bus hook claim | 60 s | guess |
| periodic pass | 10 s | guess |
| heartbeat | 30 s | guess |
| probe retry | 30 s doubling to 10 min | guess |
| card deadline | per tier, from policy | the receipt's `deadline` |

There is no silence kill: a silent harness is not necessarily stuck, and
removing a running card's directories destroys its work.

## Guarantees

1. No edge runs while the store's writer lock is held.
2. A bus cursor passes an event only after nova-bus confirmed its send; a
   repeat is possible and harmless.
3. A bus message is at most 4 KB with no log, diff, brief or secret; the
   engine never reads a message's content.
4. Base advances only to a sha whose checks passed at that exact sha, from
   the base tip it was gated on; a moved base is a new pass.
5. A group lands whole or not at all; a gate that did not run blames no head.
6. A card's branch is never rebased after its report.
7. One live harness per job directory; a job directory is never reused.
8. A harness runs only inside a nova-reserve slot, freed when it ends.
9. A job directory is removed only after its report is recorded and its head
   is on origin; committed work is pushed before any non-ok end while the job
   holds its card.
10. A report is recorded at most once per result id and is retried until
    recorded or refused.
11. A fault (harness, bench, staging, provider, rate limit, restart) spends no
    attempt and no bounce.
12. No key or DSN is in the wall's environment or files.
13. The card's branch is moved only by the job that holds its card, only by
    lease, never by plain force.
14. Every timeout is a number with a stated default and every refusal names
    its fix.

## Models

State machines in this layer are specified in TLA+ in `tla/` and bound to
the code by trace checks (the code's random walk emits traces; TLC checks
each is a behaviour of the spec).

- `tla/LandEdge.tla` - the landing ladder. Cards of two streams, every
  branch cut from the first base commit; a tree is the set of cards it holds
  plus outside commits; CI is truthful and may report nothing. Actions: `Tick`
  (layer 3's landing request), `Pass` (one `LandPass`), `BaseMoves`, `Answer`
  (land by hand, fix, retry). Invariants `BaseNeverRed`, `NothingLandsUnpassed`,
  `MergingCovered`, each with a reversed witness (`LandEdgeRedLands`,
  `LandEdgeStaleGate`, `LandEdgeJudgementHoldsStream`), plus reach checks for
  rung 2, a judgement, a hand landing and a red gate. The code is bound by
  `LandEdgeTrace.tla` and the walk in `edge/git/trace_test.go`
  (`LandPass` on the git fake and a truthful forge).
- `tla/Runner.tla` - one bench. Per consumer: store state, job state, slots,
  width, job directory, result id, pushed head, harness liveness, the applied
  op ids and the card's branch on origin (who wrote its tip). Actions cover
  wake (duplicated, lost), pass start and end, take, stage, run, die, harvest,
  rebase, conflict, push, report, reply lost, clean, withdraw, retire, holder
  push, release, stop, crash and restart, store down. Invariants: guarantees 7
  to 11 and 13, `working` within width, slots within capacity,
  `PassNeverWaitsOnJobs`, `StopIsNeverAFault`, `NoPushAfterFinal`,
  `BranchOnlyByHolder`; liveness: every `working` consumer ends recorded or
  withdrawn, and a withdrawn job's directory is removed. Reversed witnesses:
  slot freed while the harness lives, reused job directory, clean before report,
  fresh result id on retry, restart taking a working card as new, die
  classified `fail`, pass waiting on its jobs, stop reported as a fault,
  withdrawn job pushing (once and repeatedly), and a plain force over the next
  holder's branch. The code is bound by `RunnerTrace.tla` and the runner walks
  in `edge/worker/trace_test.go`.

The bus cursor rule (move only after a confirmed send) is checked in
`tla/Run.tla` at layer 6.

## Testing

Unit tests use no real time, sockets, network or outside program; time is an
argument and fake clocks start at 1000. The doubles are `edge.FakeExec`,
`git.Fake` (an in-memory commit graph with conflicts by path),
`forge.Fake` (PRs and check states; refuses a stale merge as GitHub does), a
recording `bus.Bus` in `run`'s tests, and in `edge/worker` a fake wall, sprint client, git operations,
process table and clock. Every adapter's `Command` and `Classify` and the
runner's lifecycle are driven step by step against them, each refusal with
its text.

```
go test -count=1 -skip Functional ./edge/...        # unit tier
go test -count=1 -run Functional ./edge/...         # functional tier, on a bench
```

Functional tests (`TestFunctional*`) use real git against throwaway bare
repositories in `t.TempDir()`, a throwaway PostgreSQL and Redis the test
starts, the real `nova-bus`, `nova-reserve` (temporary `--dir`) and
`nova-sandbox` (skipped where `nova-sandbox check` fails), a scratch GitHub
repository for the forge, and the script harness. They are isolated, parallel and run on a bench, never on the working
machine.

The TLA+ models and their witnesses run with `tla/ci-tlc.sh`, and the code
traces with `tla/make-traces.sh check`.
