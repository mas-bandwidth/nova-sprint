# Layer 5: policy

Policy is the part of the sprint the coordinator changes while it runs. It is
a Starlark file, stored as an immutable version in the store's `sprint.policy`
table, that answers a closed set of questions the steps (layer 3) ask each
tick. It supplies numbers, orders and choices; it never writes the store
during a tick and never raises a move. The engine checks every answer against
its domain before any step uses it.

Package `policy` is pure Go over go.starlark.net. Only the verbs (`verbs.go`)
touch the store, and only through its policy writes. Related packages:
`roster/` (the fleet's rows from nova-config, which policy reads for row
facts) and `cost/` (provider-balance reconciliation, which reads prices the
policy sets).

## Engine or policy

The split: if being wrong could corrupt the sprint, it is in the engine and
modelled; if being wrong could only make the sprint slow, costly or badly
ordered, it is policy.

| decision | where | why |
|---|---|---|
| whether a move is legal, what it writes | engine (layers 1-2) | a wrong move corrupts |
| the priority ladder `blocker > critical > read > high > normal > low` | engine, fixed | a script must not put blocker below work |
| a card's priority level | a person's verb, never policy | priorities are set by request |
| order among cards of one level | policy `order` | wrong order is only slow |
| working consumers never exceed a row's width | engine, at the write | over-filling corrupts |
| how many a row works, at or below its width | policy `width` | too few is only slow |
| a card's tier is its minimum; never a row ranked below it | engine and validator | below-tier work lands weak code |
| which rows take a card (first attempts, escalation, reads) | policy `rows` | too high a rank is only costly |
| the reader is never the author; reads are distinct rows | engine | a self-read is no read |
| how many reads a review needs | policy `reads` | |
| yes or fix from the done reads | policy `verdict` (engine requires a matching read) | a yes on zero ok reads lands unread code |
| bounce, fault and same-finding limits; escalation and flap numbers | policy `limits` | a wrong number only slows |
| which landing rung a stream is on | policy `landing` | |
| CI green at the exact base head, batches atomic | engine | a stale green corrupts main |
| silences, stall bounds, deadlines | policy `timeouts` (the clock comparison is layer 3's) | a wrong bound is slow |
| whether a stream is marked for the stitch pass | policy `stitch` | |
| cost per token per model | policy `prices` | a wrong price is a wrong number on a screen |
| the models rows may name and the bench model per tier | policy `catalog` | |
| which version is active, every change an event | store and the policy verbs | the trace from decision to rule |

## The question set

The set is closed. A top-level name in a policy file is one of the questions
below or starts with `_` (a helper), so a misspelt question (`teir`) is
refused when the file is checked, never silently ignored. Each question is a
function of the arity shown. Inputs are frozen `starlarkstruct` values built
from the snapshot and the roster; answers are plain Starlark values.

| question | inputs | answer (domain) |
|---|---|---|
| `order(card)` | `id stream score priority tier attempt bounces prior_head` | a tuple of up to 4 ints (64-bit) or strings; the deal sorts by (ladder rung, answer, id) |
| `rows(card, kind, candidates)` | card: `id stream tier attempt bounces priority writer_rank`; `kind` `"work"` or `"read"`; candidates: rows that are up, not held, serve the card's tier (and for a read are not its author), each `name tiers rank model harness width working ready`, ordered by rank then name | a list of candidate names, in preference order, no name twice |
| `reads(card)` | as rows' card | an int 1..5 |
| `verdict(card, reads)` | the card; its done reads of the current review and head, each `row rank result finding` | `"yes"`, `"fix"` or `"retry"` |
| `width(row)` | `name kind width tiers model harness flaps` | `{"work": w, "ahead": a}`, `0 <= w <= width`, `0 <= a <= 4 x width` |
| `limits()` | none | dict, every key required, below |
| `landing(stream)` | `name merging review rung` | `{"backed_up": b, "flowing": f}` with `0 <= f < b <= 1000`, optional `"ci_wait"` seconds 1..86400 |
| `timeouts()` | none | dict of seconds, below |
| `stitch(stream)` | `name cards landed` | `True` or `False` |
| `prices()` | none | dict of model name to price, below |
| `catalog()` | none | dict of models, harnesses and optional bench, below; optional to define |

`rank` is a row's highest tier in ladder order (`TIERS`: `flash pro heavy
frontier`). `writer_rank` is the rank of the row that wrote the head under
review, or the card's tier when that row is unknown. `width`, `tiers`, `model`
and `harness` of a row are the roster's (nova-config); `flaps`, `working` and
`ready` are the store's. A card's prose and brief are never an input, so
inputs grow with the card count, not the briefs. A row the roster does not
name is in no question.

Details of the domains:

- `order`: elements are compared left to right, an int before a string, a
  shorter prefix first.
- `rows`: the answer may be a list or a tuple. It must name only candidates,
  never a row whose rank is below the card's tier, and be non-empty while any
  candidate exists. Beyond that, the answers together must leave the sprint
  able to deal (the joint check): if the candidates could take the card (some
  has `width > 0`), the rows named must include at least one whose `width`
  answer has `work > 0` (for a read, as many as `reads` asks, capped by what
  the candidates could take). A tier no row serves is not a policy fault; layer
  3 raises "no route serves the tier".
- `verdict`: `"yes"` needs a done `ok` read, `"fix"` a done `fix` read,
  `"retry"` is only for when every done read faulted (layer 3 asks one more
  read).
- `width`: the engine deals a row to `work` and no further. `ahead` is
  validated but no step deals ahead of it.
- `limits()` keys, all required, with ranges:

  | key | range | meaning |
  |---|---|---|
  | `bounces` | 1..100 | bounces before a producer is blocked |
  | `faults` | 1..100 | faults before a card is blocked |
  | `same_finding` | 1..10 | the same finding this many times blocks |
  | `reports_per_tick` | 1..10000 | reports applied per tick |
  | `route_rest_window` | 1..100 | window for a route rest |
  | `route_rest_after` | 1..`route_rest_window` | faults in the window that rest a route |
  | `escalate_after` | 1..100 | bounces at which work goes one rank up |
  | `frontier_after` | `escalate_after`..100 | bounces at which work goes to frontier |
  | `flap_limit` | 1..100 | flaps that open a flap judgement |
  | `fleet_width_cap` | 1..256 | the most consumers a worker machine works (the roster caps its cores at this) |
  | `idle_floor` | (0, 1] | fraction of the open width working under which the idle alarm opens |
  | `land_target` | (0, 1] | first-time land rate under which the land-rate alarm opens |

  A fraction may be an int or a float. The run loop passes the first six
  keys (`bounces` to `route_rest_after`) to layer 3 and reads
  `fleet_width_cap` itself. `escalate_after`, `frontier_after`, `flap_limit`,
  `idle_floor` and `land_target` are validated and shown by `check`, but the
  steps read their own constants for them (`steps.Engine`, equal to the
  default's values), so changing them in `limits()` does not by itself change
  steering. Escalation by `rows` is the policy's own: the default's `rows`
  uses `_ESCALATE_AFTER` and `_FRONTIER_AFTER`, which its `limits()` echoes.
- `timeouts()` keys, in seconds 1..86400 unless noted: `row_silent_gone`,
  `row_silent_withdraw` (at or above `row_silent_gone`), `landing_stall`,
  `base_regate` (a non-empty strictly rising list), `judgement`,
  `route_rest_for`, `tick` (the run loop's budget for one tick), `consumer`
  (a dict naming every tier, each 1..86400: a consumer's deadline by tier,
  which the worker reads from the take receipt), and the optional `ready_nag`
  (seconds after a deal from which a row is nagged each tick for a card in
  its hand it has not taken; default 1) and `ready_take` (seconds after which
  such a card is withdrawn and dealt to another row with room; default 180).
  No wait is ever unbounded: zero, negative and absent are refused.
- `prices()`: `{model: {"in": n, "out": n}}` in micro-dollars per million
  tokens, with optional `"cache_read"` and `"cache_write"` (each defaults to
  `in`), all ints at or above 0. A model is named as a usage record reports it
  (`provider/model`) or by the model alone. An unlisted API model is recorded
  unpriced, never as $0.
- `catalog()`: `{"models": {provider: [{"name": n, "tier": t}, ...]},
  "harnesses": [h, ...], "bench": {tier: "provider/model"}}`. Names are
  non-empty, tiers come from `TIERS`, nothing is listed twice (a model under
  two providers, or the same name and tier twice, is refused; one name may
  serve two tiers as two entries), and each `bench` model must be one of its
  tier's models (`Catalog.ForTier`, in `provider/model` form). The row verbs
  refuse a model or harness not in the catalog of the active version. `bench`
  is the one model a bench runs per tier: the deal names it, the take receipt
  carries it, and a tier with no bench model deals to no bench. A version
  that does not define `catalog()` uses the built-in's.

A refusal names the version, the question, the subject and the line of the
question's `def`:

```
POLICY REFUSED v7 line 31 rows(c12 work): row r4 has rank pro, below the card's tier heavy
```

Before a file is stored (`policy check <file>`, `policy set`) it has no
version, so the refusal names the file. Something the file does not define (a
missing question, a file over the size cap) is reported at its last line, as
`line N, end of file`. A subject is the card, row or stream the question was
about; `rows` subjects are `<card> <kind>`.

Engine defaults. Every question also has an engine default in
`steps.Engine` (`steps/asker.go`), used when no version's answer is usable.
Layer 3 re-checks each answer it reads; one it cannot use (absent, failed,
out of domain, or naming a row not offered) is the default for that question
that tick, and a miss under a real version opens one policy judgement for the
question and version. With no policy (version 0) every default applies by
design and no miss is recorded.

## The default policy

`policy/default.star`, embedded in the binary (`policy.DefaultBody`), is
stored as version 1 by `nova-sprint up` and is the built-in fallback.
`TIERS`, `LEVELS` and `prefer` are predeclared. What it does:

- `order`: a fix attempt (a card with a prior head) before fresh work, then
  by score.
- `rows`: the rank wanted is the card's tier; one rank up at 2 bounces
  (`escalate_after`), frontier at 4 (`frontier_after`); for a read, the rank
  above `writer_rank`, or frontier for a stream listed in `_SECURITY_STREAMS`
  (empty by default). The answer is `prefer(candidates, rank)`: every
  candidate, the wanted rank and up nearest first, then the ranks between the
  tier and the wanted one highest first, with rows that have room
  (`working < width`) before those that do not. Every candidate is listed so
  the joint check passes whenever any row could take the card; escalation counts
  bounces, never faults.
- `reads`: 1. `verdict`: any fix read means `"fix"`; else any ok read
  `"yes"`; else `"retry"`.
- `width`: `{"work": row.width, "ahead": 0}`.
- `limits`: bounces 6, faults 3, same_finding 2, reports_per_tick 1000,
  route_rest_window 10, route_rest_after 3, escalate_after 2, frontier_after
  4, flap_limit 3, fleet_width_cap 8, idle_floor 0.5, land_target 0.9.
- `landing`: backed up (rung 2) at 2 heads merging, flowing again at 1,
  `ci_wait` 600.
- `timeouts`: row_silent_gone 600, row_silent_withdraw 1800, landing_stall 60,
  base_regate [120, 300], judgement 3600, route_rest_for 1800, tick 10,
  ready_nag 1, ready_take 180, consumer deadlines flash 900, pro 2700, heavy
  5400, frontier 7200.
- `stitch`: `True` when the stream has more than one card.
- `prices`: `{}`. Every API-rate model is unpriced until the coordinator
  prices it (`policy check` prints a warning per catalog model billed at API
  rate and missing a price).
- `catalog`: anthropic (haiku flash, sonnet pro, opus heavy, fable frontier),
  openai, google, deepseek, inception, paperinstruments and unknown providers,
  harnesses `claude codex opencode api`, and a `bench` map of one model per
  tier.

Every number in it is a stated guess, not a measurement, so changing it is
cheap: use `policy diff default` to see what a newer binary's default would
change, and adopt it only through `set` and `activate`.

## The embedding

- A version's top level runs once (`starlark.SourceProgramOptions` then
  `Init`), is frozen, and the compiled program is cached by (label, body).
  Versions are immutable, so the cache is never stale.
- The dialect is the zero `syntax.FileOptions`: no `while`, no recursion, no
  top-level `if` or `for`, no global reassignment, no `set`. `load` fails
  (`Thread.Load` is nil) and `print` discards. Starlark has no clock, random,
  file or network, and none is added. Predeclared: the Starlark universe,
  `TIERS`, `LEVELS` and `prefer`.
- `prefer(candidates, rank)` is the default's row ordering in Go so a tick is
  linear in its producers; it is memoised per candidate list and rank and
  returns a frozen list.
- Limits: a file is at most 64 KiB (`MaxBody`); the top level and each
  question call get 1,000,000 execution steps (`CallSteps`), and exceeding it
  is a refusal "over the step budget".
- A tick has one deadline, `TickTime` (500 ms, from the first byte of its
  input), measured on an injected `Clock` (the real clock in the binary, a
  fake in unit tests). Each phase checks it (compile, each answer, each
  validation loop, the joint check). A version's answers are used and cached
  only when its whole run passed before the deadline.
- In the binary an engine (`NewEngine`) runs every version in a child
  process, this binary as `nova-sprint policy-eval`, reused across ticks and
  fed gob-encoded requests (tick, check or catalog) on its stdin. The child
  caps its own heap at 512 MiB (`EvalMemory`, watched every 2 ms; it exits
  with "over the evaluator's memory cap" when exceeded). A run abandoned at
  the deadline that is still burning a core is killed with the child, as is a
  child that has not answered within 100 ms of the deadline; every version it
  was running then fails and the next request starts a new child. Card briefs
  and prose are dropped from the snapshot before it crosses. The in-process
  engine (`newLocal`) is the unit tests' and the child's own.
- Answers are cached for one tick (the last tick's, at most 4 MiB) by the
  hash of the question and its input; determinism makes the cache exact, so a
  steady tick asks only about what changed.

## The tick's answer and the fallback

`Engine.Answer` (`AnswerContext`) answers a whole tick from one version.
`Versions` holds the active version and the one active before it (from the
last accepted activation, `LoadVersions`/`PrevOf`), read at a snapshot seq; a
tick on a different snapshot is refused ("reload"). The versions tried are:
the active version, else the previous active, else the built-in default
(version 0).

The versions run in order, each started when the one before it fails, or
speculatively after 250 ms (`fallbackAfter`) if the first has not answered,
so the built-in still has time to answer. The first in order that passes
before the deadline answers. A runtime error, a missing key, the step budget,
the deadline or any refused answer in a version means none of that version's
answers is used. The tick's `Answers.Version` is the version that answered
(0 is the built-in) or -1 when none did; with -1 the steps use the engine
defaults and still apply every move that needs no answer, so a tick is never
stalled by policy.

Each failing version yields one `Failure`. `policy.Judgements(snap, ro,
failures)` returns, for each failing version with no open policy judgement,
one judgement: kind `policy`, group key `policy:v<n>`, id
`policy-v<n>-s<seq>`, the refusal as the question and `nova-sprint policy
check v<n>` as the command. A stale-versions refusal (version -1) opens none.
Its safe default is already in force. The next tick tries the active version
again. Activating a version that runs clean answers the open policy
judgements of the old version, of the new one and of the built-in.

## The verbs

`nova-sprint policy ...` (`verbs/policy.go` calls package `policy`). `set` and
`activate` are the coordinator's and need `--reason`; the others are for any
row. Every refusal is `SPRINT REFUSED policy <verb>: <what>: <fix>`.

| verb | does | refuses |
|---|---|---|
| `policy set <file> --reason <why>` | checks the file on the current snapshot and stores it as the next version (kind `script`, name the file's base name). It is not made active, unless no version is active, in which case the same op activates it and answers the built-in's open policy judgements. `next: nova-sprint policy check v<n>` | not the coordinator; no reason; a file unreadable or over 64 KiB; a syntax or resolve error (with its line); a missing question; an unknown top-level name; a wrong arity; an answer refused on the current snapshot (run `policy check <file>`); a body equal to a stored version (`v4 has this body: nova-sprint policy activate 4`); text that is not UTF-8 or holds NUL |
| `policy activate <version> --reason <why>` | re-runs the check on the current snapshot, then in one op (fenced at the snapshot's seq) moves the active flag to the version and answers the open policy judgements; prints how many answers change | not the coordinator; no reason; an unknown version; already active; a version of kind `data`; a refused answer on the current snapshot |
| `policy show [<version>] [--list]` | prints a version (the active one by default): actor, time, reason and body; `--list` prints every version, newest first, the active one marked | an unknown version; no policy active (`nova-sprint up`) |
| `policy diff <version\|default> [<version\|default>]` | text diff of two bodies (the second defaults to the active version; `default` is the built-in) | an unknown version; a body over 10,000 lines |
| `policy check <file\|version>` | read-only: runs the file, a stored version (`7` or `v7`) or `default` on every question the snapshot poses (`FullQuestions`, with `landing` asked on both rungs) and prints every answer that would differ from the active version's, then every refusal, then warnings; exit 0 when nothing is refused, 1 otherwise | an unreadable file; an unknown version |

`check` output looks like:

```
policy check v7: 2 answer(s) change, 0 refused, 1 warning(s)
  limits(): {...} -> {...}
  width(amd-1.ds): {"work": 4, "ahead": 0} -> {"work": 2, "ahead": 0}
POLICY WARNING prices(): deepseek/deepseek-flash (tier flash) is billed at API rate and has no price: ...
```

A warning never changes the exit code.

`nova-sprint up` stores the built-in as the next version and activates it in
one op when none is active; otherwise it changes nothing. Versions are
numbered from 1 without gaps and never edited or deleted; a rollback is an
activation of an old number.

What the log records: `policy-set` and `policy-activate` events carry the actor,
the reason and the version rows, so the log alone rebuilds every body;
every other event's `policy` field is the active version.

## Guarantees

1. A policy change never moves a card. `set` and `activate` write policy rows
   and policy judgements only. A consumer in hand is never withdrawn,
   re-tiered or re-dealt because an answer changed; a lowered width stops new
   deals and withdraws nothing; a wait keeps the bound it began under.
2. An answer out of its domain is refused, never used; the fallback answers
   that tick.
3. The active version is always a stored version. Before `up` nothing runs.
4. A runtime error never stalls a tick: it is answered by the active, the
   previous active or the built-in version, or by the engine defaults.
5. One version answers a whole tick.
6. Answers are deterministic: one version and one input give one answer, so
   the cache is exact and `check` on a snapshot predicts the tick on it.
7. Policy never writes the store and never raises a move. Every move is a
   step's, checked by layer 2 and fenced by layer 1, so the worst in-domain
   answer yields legal plans: a policy can make the sprint slow but not
   corrupt it.
8. At most one open policy judgement per version, by its group key.
9. The evaluator child bounds time (the tick deadline) and memory (512 MiB);
   a runaway version fails and is replaced, never left running.

## Roster (`roster/`)

The roster is the sprint's view of who can work, read from nova-config and
never copied into the store. `roster.Load(ctx, src, cap)` reads a `Source`
(machines and friends) once, `cap` being policy's `fleet_width_cap`:

- Every machine with the `bench` role is a worker: width `min(cores, cap)`,
  every tier, harness `nova-worker`, routes from its `route:<name>` roles
  (sorted). A bench with no cores loads at width 0, `Held` = "no cores in
  nova-config", and is dealt nothing until its cores are set.
- Every enabled friend is kin at its own width, tiers, model and harness.
- A machine or friend that cannot be loaded (no name, a name given twice, a
  negative width, an unknown tier) is skipped and listed in `Roster.Refused`;
  one bad row never empties the roster.
- `Roster.Advertised(rows)` gives a worker that has advertised the tiers it
  serves and its nova-reserve slots (its heartbeat, stored on the row) exactly
  those tiers and width `min(width, slots)`. Kin are untouched. The
  advertisement lasts one hook session.
- `ServesRoutes(name, routes)` tells whether a worker serves every route a
  card declares.

`roster.Fake` is the tests' `Source`; the real source (run's config source)
reads machines and friends in one REPEATABLE READ transaction per tick.

## Cost reconciliation (`cost/`)

`nova-sprint cost reconcile` (coordinator; also run hourly by the run loop,
`cost.Every`) sets each provider's own count of what its key spent beside the
store's priced API records of that provider. For each `Reader`:

1. Read the balance. `DeepSeek` reads `GET https://api.deepseek.com/user/balance`
   with `DEEPSEEK_API_KEY` from the environment (put there by `nova-secrets
   exec --only DEEPSEEK_API_KEY`) and takes the USD `total_balance`.
   `Inception` publishes no balance endpoint, so its reading is unknown and its
   spend is the store's records alone.
2. Record the balance with the current store seq (`sprint.balances`).
3. With a previous read, the provider's spend is the fall in balance. A balance
   that rose (a top-up) measures nothing for the window. The first read
   measures nothing.
4. `Recorded` sums the cost of every priced API consumer record of that
   provider written to the log between the two reads' seqs.
5. A gap (spent less recorded) over 5% of the provider's figure (`GapShare`)
   and over $1 (`GapFloor`) opens one judgement on the coordinator row, kind
   `cost-gap`, choices `ack | retry`, never a second while one is open.

Output is one `COST ...` line per provider: `unknown`, a first read or top-up
reason, or `COST <provider> <from>..<to> provider=$X recorded=$Y gap=$Z`,
with `judgement=<id>` when one opened. Answer a gap with `nova-sprint judge
<id> ack|retry --reason <why>`. A gap means a cost not recorded (a model with
no price, a run that reported no tokens, spend outside the sprint) or a wrong
price.

## The model

`tla/Policy.tla` models versions, activation and the per-tick fallback, with
versions 1..3 plus the built-in (`Default`), one card and one row.

- Variables: `stored`, `active`, `prev`, `everActive`, `breaks` (versions
  that fail on the current snapshot), `answeredBy`, `tickBreaks`,
  `judgements`, the card's state, and the waits' bounds.
- Actions: `up`, `set`, `activate`, `drift` (the snapshot moves, so `breaks`
  changes, never including the built-in) and `tick` (answered by the first of
  active, previous, built-in that is not breaking; opens the judgement for the
  active version when it breaks).
- Invariants: `ActiveStored`, `AnsweredByUsable`, `TickAlwaysAnswered`,
  `ActivateMovesNoCard`, `OneJudgementPerVersion`, `WaitKeepsItsBound`,
  `FallbackWasActive`, `NoAnswerStartsNothing`.
- Reversed witnesses in `tla/witnesses/` (`PolicyActivateUnstored`,
  `PolicyTickStalls`, `PolicyActivateMovesCard`, `PolicyBreakingUsed`,
  `PolicyFallbackNeverActive`, `PolicyDefaultSkipped`,
  `PolicyJudgementEveryTick`, `PolicyNoAnswerDeals`, `PolicyWaitRebound` and
  others), each a required violation.

The code is bound to the model by `policy.TraceState` (the refinement
mapping from the store and the last tick's answers onto the model's
variables) and the walk in the package's trace test, checked against
`tla/PolicyTrace.tla`.

## Testing

Unit tests in package `policy` use no real time, sockets or store process:
they run the in-process engine on a fake `Clock` and `store.Fake`. They cover
compile-once caching, the dialect restrictions (recursion, `while`, `load`,
global reassignment refused; frozen globals), the step budget, determinism,
every name, arity and domain refusal with its exact text, version and line,
the fallback order and the tick deadline, the default policy on every
snapshot shape, the verbs with every refusal, `check` agreeing with the next
tick's answers, and fuzzed answers (a policy returning arbitrary Starlark
values must never panic, never be used out of domain and never exceed a
row's width). `TestMain` also serves as the evaluator child
(`policy.test policy-eval`), so process-engine paths run in unit tests.
Wall-clock tests are skipped unless `NOVA_LOAD` is set.

```
go test -count=1 -skip Functional ./policy/... ./roster/... ./cost/...
go test -count=1 -run Functional ./policy/...        # functional tier, on a bench
```

The functional tier runs the verbs against a real PostgreSQL store.
