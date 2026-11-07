# Layer 5: policy

Draft for Glenn, 2026-11-06: not approved, not built. Policy is the part
of the sprint the coordinator changes while it runs
(promise 6: "change the rules of the sprint while it runs"). It is a Starlark
file, stored as an immutable version in `sprint.policy` (LAYER-1-STORE.md),
that answers a closed set of questions the steps (layer 3) ask. It never
writes the store and never raises a move: it supplies a number, an order or
a choice, and the engine checks the answer against its domain before any
move uses it. The split rule (Glenn, 2026-10-27 and 2026-11-06, as the
coordinator recorded it): if being wrong could corrupt the sprint, it is in
the engine, modelled; if being wrong could only make the sprint slow, costly
or badly ordered, it is policy. Glenn 2026-10-27, in LESSONS 5.1: "I want
our part that is not starlark to be immaculate". Package `policy`, pure Go
over go.starlark.net: no store, no clock, no bus; the steps hand it inputs
and take back validated answers.

## The list for Glenn

Bottom up: the structure, what it must guarantee, how it is checked.

1. **The stored version** (layer 1's `sprint.policy`, kind `script`):
   immutable, never deleted, one active, named by every event; layer 1's
   lock (L1-S6 in ENFORCED.md).
2. **The embedding**: compiled once per version; an answer is a pure
   function of version and inputs (no clock, random, I/O or `load`, frozen
   globals, bounded steps); unit tests, no time, no sockets.
3. **The inputs**: each question sees only its subject, as frozen values
   built from the snapshot; conformance against `store.Fake`.
4. **The validator**: every answer in its domain, and the answers together
   leave the sprint able to deal, or a refusal naming version, question,
   subject and line; a unit test per refusal and a fuzz of answers.
5. **The tick's answer**: one version answers a tick (active, else the
   previous active, else the built-in); a failure opens one judgement and
   never stalls; `tla/Policy.tla` and a conformance test that breaks the
   active version mid-sprint.
6. **The verbs** (`policy set`, `activate`, `show`, `diff`, `check`):
   checked before stored, stored before active, every change an event, the
   coordinator only; a refusal test per verb row, on Fake and PostgreSQL.
7. **The default policy**: in the binary, activated by `up`, every number
   marked v1 or guess; answers every snapshot the suite builds; a cold read.

## The boundary

| decision | engine | policy | why that side |
|---|---|---|---|
| whether a move is legal, what it writes | layer 2 | | a wrong move corrupts |
| the ladder blocker > critical > read > high > normal > low | layer 2, fixed | | Glenn's ruling, declared by request; a script must not put blocker below work |
| order among equals inside one level | | `order` | wrong order is only slow (LESSONS 5.3) |
| a card's priority level | a person's verb | never | "Priorities are manually set by request." |
| working consumers never exceed the row's width | layer 1, at the write | | over-filling a row corrupts |
| how many a row works and queues, at or below that width | | `width` | too few is only slow (PRIORITY D21) |
| a card's tier is its minimum, fixed at add; a consumer's is its producer's | layer 1 (round 9) | | one tier per card, one check at the write |
| never a row whose rank is below the card's tier | layer 1 and the validator | | below-tier work wastes hours and lands weak code (PRIORITY T12, T24) |
| which rows, by rank, take a card: first attempts, escalation, reads, streams | | `rows` | too high a rank is only costly, too narrow a set only slow |
| the reader is never the author; reads distinct, all asked in one tick | layers 2 and 3 | | a self-read is no read |
| how many reads | | `reads` | (LESSONS 3.4; PRIORITY D17) |
| yes or fix from all the done reads of a head | | `verdict` | ruled policy's in LAYER-2 (LESSONS 3.7) |
| a yes needs at least one done ok read of this review and head | layer 2 | | a yes on zero yes lands unread code |
| compare `bounces` with the limit; `Block` | layers 2 and 3 | | the move |
| the bounce, fault and same-finding limits | | `limits` | a wrong number only slows |
| which rung a stream lands on (flowing, backed up) | | `landing` | LANDING.md: "by a number policy states" |
| CI green at the exact base head; batch atomic; order kept | LANDING.md, engine | | a stale green corrupts main |
| silences before `Withdraw` and `Gone`, the stall bound, deadlines | | `timeouts` | a wrong bound is slow; the clock comparison is layer 3's, one place (LESSONS 1.14) |
| a row going down never withdraws its consumers | layer 2 | | (LESSONS 4.7) |
| which consumer a blocker preempts | layer 3, fixed: the youngest working consumer on the row, the row chosen by the same `rows` answer | | ruled by the coordinator 2026-11-06, refining LAYER-2's "policy chooses" (LESSONS 5.12) |
| the withdraw and the blocker's start in one op | layers 2 and 3 | | a half preemption strands a row |
| route rest thresholds | | `limits` | (LESSONS 4.24); the rest itself is layer 4's |
| reports and moves applied per tick | | `limits` | README: "The budget is policy's number; the carry is the engine's." |
| whether a stream is marked for the stitch pass at its sentinel | | `stitch` | the pass itself is manual (README) |
| cost per token per model, for the dashboard | | `prices` | a wrong price is a wrong number on a screen |
| an unpriced run shown as unpriced, never $0 | layer 1 (`priced`) | | (LESSONS 1.17) |
| which version is active; every change an event | layer 1 and this layer's verbs | | the trace from decision to rule (promise 6) |

## The question set

Closed: a top-level name in a policy file is one of the questions below or
starts with `_` (a helper), so a misspelt question (`teir`) is refused at
`policy set`, never silently ignored. Every question is a Starlark function
of the arity shown. Inputs are frozen `starlarkstruct` values; answers are
plain Starlark values. The validator's refusal names the version, the
question, the subject and the line of the function's `def`
(`Function.Position()`), in the form:

```
POLICY REFUSED v7 line 31 rows(c12): row r4 has rank pro, below the card's tier heavy
```

Before a file is stored (`policy check` and `policy set`) it has no version,
so the refusal names the file instead (`POLICY REFUSED mine.star line 31 ...`);
after storage, the version. Something the file does not define (a missing
question, a file over the size cap) is refused at its last line, as `line N,
end of file` (ruled 2026-11-07).

| question | inputs | answer and domain | the engine also refuses | default |
|---|---|---|---|---|
| `order(card)` | id, stream, score, priority, tier, attempt, bounces, prior_head | a tuple of up to 4 ints or strings; the engine sorts by (rung, answer, id) | a tuple of other types or longer than 4 | fix attempts first, then score |
| `rows(card, kind, candidates)` | the card: id, stream, tier (its minimum), attempt, bounces, priority (no prose: round 5), and for a read `writer_rank` (the rank of the row that wrote the head); `kind` `work` or `read`; `candidates`: the rows up, not held and serving the card's tier (the author excluded for a read), each with name, tiers, rank (the highest tier it lists), model, harness, width (the roster's, from nova-config: round 5), working, ready | the candidates to use, in preference order; the engine deals to the first with room (for a read, `count` distinct rows in that order) | a name not in `candidates`; a row whose rank is below the card's tier; an empty list while candidates exist | the card's own rank; one rank up at 2 bounces, frontier at 4; for a read the rank above `writer_rank`, frontier for a security card; every candidate listed: the wanted rank and up, nearest first, then the ranks between the tier and the wanted one, highest first; those with room first, all of them when none has room (ruled 2026-11-06, so the joint check passes whenever any row could take the card); within a rank, by name (round 5) |
| `reads(card)` | as `rows`' card | the count n, 1 <= n <= 5 | n out of range | 1 |
| `verdict(card, reads)` | the producer; its done reads of this head and review (row, rank, result ok, fix or fault, finding) | `"yes"`, `"fix"` or `"retry"` (every done read faulted: layer 3 asks one more read; ruled 2026-11-06 after the cold read) | `"yes"` with no ok read, `"fix"` with no fix read (layer 2's rules), `"retry"` while a done read is ok or fix | any fix read means fix; else an ok read means yes; else retry |
| `width(row)` | name, kind, width, tiers, model, harness (the roster's, from nova-config, by the row's name: round 5), flaps (the store's) | `{"work": w, "ahead": a}`, 0 <= w <= width, 0 <= a <= 4 x width | over the row's width | work = width, ahead = width (PRIORITY D21: 2 x width held) |
| `limits()` | none | dict: `bounces` 1..100, `faults` 1..100, `same_finding` 1..10, `reports_per_tick` 1..10000, `route_rest_window` 1..100, `route_rest_after` 1..window; layer 3's steering numbers (round 5): `escalate_after` 1..100 and `frontier_after` escalate_after..100 (the bounces at which work goes one rank up and to frontier), `flap_limit` 1..100, `idle_floor` and `land_target` numbers in (0, 1] | out of range, a missing key, a fraction that is not a number | below; the steering keys 2, 4, 3, 0.5 and 0.9, all guesses; `fleet_width_cap` 1..256 (the most consumers a worker machine works, its cores at most this; the roster's cap, drift row 26), 8, a guess |
| `landing(stream)` | name, merging, review, rung (check asks both rungs: round 5) | `{"backed_up": b, "flowing": f}`, 0 <= f < b <= 1000, optional `"ci_wait"`: seconds 1..86400 | f >= b, ci_wait outside 1..86400 | backed up (rung 2) at 2 merging, flowing again at 1; ci_wait 600 (a guess) |
| `timeouts()` | none | dict of seconds of running time, each 1..86400: `row_silent_gone`, `row_silent_withdraw` (>= gone), `landing_stall`, `base_regate` (a rising list), `judgement`, `route_rest_for`; optional `ready_nag` (the seconds after a deal from which a row is nagged each tick for a card in hand it has not taken: Glenn 2026-11-07, default 1) and `ready_take` (the seconds after which such a card is withdrawn and dealt to another row with room: default 180) | zero, negative, absent: no wait is ever unbounded | below |
| `stitch(stream)` | name, cards, landed | `True` or `False` | a non-bool | `True` when the stream has more than one card |
| `prices()` | none | dict: model name to `{"in": n, "out": n}`, micro-dollars per million tokens, n >= 0 | a negative or non-int price | `{}`: every run unpriced until the coordinator sets them |

Beyond the per-answer domain, the validator checks the answers together,
so no policy can leave the sprint unable to deal (LESSONS 5.6): it refuses
`width` and `rows` answers that leave a card with ready work and no row
able to take it when the candidates themselves could (a tier no row serves
at all is layer 3's "no route serves the tier" judgement, PRIORITY T12, not
a policy fault). `rows` also answers which rows may take a stream (the
card carries its stream). `verdict` and `ahead` in `width` are LAYER-2's
choices for policy that the first list left out.

## The default policy

`policy/default.star`, embedded with `go:embed`. A number marked `v1` is
v1's value, carried and not measured in v2; `guess` has no source; per the
rule, a number derived from other constants is a guess and says so.
`TIERS` is predeclared by the engine, in ladder order.

```python
# nova-sprint default policy. Every top-level name is a question or a _helper.

_RUNG = {t: i for i, t in enumerate(TIERS)}
_SECURITY_STREAMS = []  # the coordinator lists streams whose reads go to frontier

def _up(t, n):
    return TIERS[min(_RUNG[t] + n, len(TIERS) - 1)]

def _security(card):
    return card.stream in _SECURITY_STREAMS  # the prose is no input (round 5)

def order(card):
    # Inside one declared level; the ladder is the engine's.
    # LAND > FIX > READ > WORK (PRIORITY D1): a fix attempt before fresh work.
    fix = 0 if card.prior_head else 1
    return (fix, card.score)

def _want(card, kind):
    # The rank wanted. A card's tier is its minimum and never changes.
    if kind == "read":
        if _security(card):
            return "frontier"
        return _up(card.writer_rank, 1)  # the rank above the writer's row
    # Escalation counts bounces, never faults (LESSONS 2.24).
    if card.bounces >= 4:
        return "frontier"  # PRIORITY T19; 4 is a guess
    if card.bounces >= 2:
        return _up(card.tier, 1)  # guess
    return card.tier  # at its tier from its first deal (PRIORITY T17)

def rows(card, kind, candidates):
    want = _RUNG[_want(card, kind)]
    use = [r for r in candidates if _RUNG[r.rank] >= want]
    if not use:  # no row of the wanted rank is open: never below the tier
        use = [r for r in candidates if _RUNG[r.rank] >= _RUNG[card.tier]]
    return [r.name for r in sorted(use, key = lambda r: _RUNG[r.rank])]

def reads(card):
    return 1  # v1 standing rule, 2026-10-20

def verdict(card, reads):
    return "fix" if any([r.result == "fix" for r in reads]) else "yes"

def width(row):
    return {"work": row.width, "ahead": 0}  # no deal-ahead: a worker takes only what it can run now (defect 74)

def limits():
    return {
        "bounces": 6,              # guess (PRIORITY G1: no source for n)
        "faults": 3,               # guess
        "same_finding": 2,         # v1 DISPATCHER section 7
        "reports_per_tick": 1000,  # derived from the layer 1 load test: a guess
        "route_rest_window": 10,   # v1 (PRIORITY T20)
        "route_rest_after": 3,     # v1
    }

def landing(stream):
    # rung 2 at more than one head waiting (intent rule 32); ci_wait a guess
    return {"backed_up": 2, "flowing": 1, "ci_wait": 600}

def timeouts():
    return {
        "row_silent_gone": 600,       # guess; never one missed challenge (LESSONS 4.7)
        "row_silent_withdraw": 1800,  # guess; above a 600 s gate (LESSONS 2.9)
        "landing_stall": 60,          # v1 (LANDING step 10)
        "base_regate": [120, 300],    # v1 (LANDING step 8)
        "judgement": 3600,            # guess
        "route_rest_for": 1800,       # v1 (PRIORITY T20)
        "ready_nag": 1,               # Glenn 2026-11-07
    }

def stitch(stream):
    return stream.cards > 1

def prices():
    return {}  # micro-dollars per million tokens, by model: unpriced until set
```

## The embedding

From go.starlark.net, as the embedding is designed (to be confirmed with
`go doc` when the module is added to `go.mod`; it is not there today):

- Compile once per version: `starlark.ExecFile(thread, "v<n>.star", body,
  predeclared)` runs the top level and returns the globals as a
  `starlark.StringDict`, frozen, so no call can change state another call
  sees. The result is cached by version number; versions are immutable, so
  the cache is never invalidated.
- The dialect stays at its defaults (`syntax.FileOptions` zero): no `while`,
  no recursion, no top-level `if` or `for`, no global reassignment, no `set`.
  `thread.Load` is nil, so `load` fails. `thread.Print` discards.
- Predeclared: the Starlark universe plus `TIERS` and `LEVELS` as frozen
  tuples. No time, random, file or network: Starlark has none, none is added.
- Each answer is `starlark.Call(thread, fn, args, nil)` on one thread per
  tick, with `thread.SetMaxExecutionSteps(budget)`; passing the budget
  cancels the thread and is a runtime error like any other. An
  `*starlark.EvalError` carries the backtrace, whose innermost frame is the
  line reported.
- Batched: a tick builds every question it needs (`tier` for each producer
  about to start, `reads` for each entering review, `verdict` for each whose
  reads are complete, `width` for each row, `rows` for each consumer to
  cut, `landing` for each stream, the argument-free questions once per version) and answers
  them in one pass. Answers are cached by (version, hash of the inputs);
  determinism makes the cache exact, so a steady tick asks only about what
  changed (README: "steady-state reads incremental").

## The tick's answer and the fallback

The step asks; this layer runs the active version on the questions not
cached, then validates everything. When any question errors (a runtime
error, the step budget, a missing key) or any answer is refused, none of
that version's answers is used this tick: the whole tick is answered by
the version active before it (from the last accepted `policy activate`
event), else by the built-in default. One version answers a tick, and the
tick's receipt records `answered_by` beside the event's `policy` (the
active version). The same op opens, if none is open, a judgement of kind
`policy`, group key `policy:v<n>`, carrying the error, the line and
`run: nova-sprint policy check <file>`; its safe default is already in
force (the fallback), which never drops, accepts, merges or lands anything
(LESSONS 5.7). The next tick tries the active version again, since the
snapshot may have moved past the failing case. Activating a version that
runs clean answers the judgement in the activating op.

If even the built-in errors (a bug in this binary), the tick still applies
reports, landings and every move that needs no answer. A wait is bounded
by the `timeouts` of the version active when it began (its start event
records it), so lowering a silence never makes a card in hand due at once.

## The verbs

All are the coordinator's: Glenn asks, the coordinator edits. Every
refusal is in the CORRECT-USE form.

| verb | does | refuses, naming the fix |
|---|---|---|
| `policy set <file> --reason <r>` | compiles, checks names and arity, runs `check` on the current snapshot, stores the next version (kind `script`, name the file's base name, the reason in the event), not active; `next: nova-sprint policy check v<n>` | not the coordinator ("policy is the coordinator's: ask over the bus"); the file unreadable or over 64 KiB; a syntax or resolve error, with its line; a missing question; an unknown top-level name; a wrong arity; an answer refused on the current snapshot; a body equal to a stored version ("v4 has this body: `policy activate 4`"); `up` not run |
| `policy activate <v> --reason <r>` | re-runs `check` on the current snapshot, then one op: the active flag moves to `v`, the open policy judgement of the old version is answered; prints the count of answers that change; `next: nova-sprint next` | an unknown version; already active (layer 1's same-value refusal); a refused answer on the current snapshot; a version of kind `data` (unused by this layer) |
| `policy show [v]` | prints the version (active by default), its actor, time, reason and body; `--list` prints every version, newest first, the active one marked | an unknown version |
| `policy diff <a> [b]` | the text diff of two bodies (`b` defaults to active; `default` names the built-in) | an unknown version |
| `policy check <file \| v>` | read-only: runs it on the current snapshot and prints every answer that would differ from the active version's, by question and subject, then every refusal; exit 0 when nothing is refused, 1 otherwise | an unreadable file; an unknown version |

`up` stores the built-in default as version 1 and activates it when the
store holds none. A newer binary's default changes nothing by itself:
`policy diff default` shows it, and only `set` and `activate` adopt it.

## The lifecycle of a version

`set` (stored, inactive) -> `activate` (active) -> superseded (inactive,
kept). A version is never edited or deleted; a rollback is an activation
of an old number. What the log records:

- `policy-set`: actor, reason, the full version row in `after`, so the log
  alone rebuilds every body (layer 1 guarantee 5).
- `policy-activate`: actor, reason, both versions' rows before and after;
  `events.policy` on that op is the version before (layer 1, round 9).
- Every other op: `events.policy` is the active version; a tick's receipt
  adds `answered_by` and, on a fallback, the error and line.

## What it guarantees

1. **A policy change never moves a card.** `set` and `activate` write only
   policy rows and the policy judgement. A consumer in hand is never
   withdrawn, re-tiered or re-dealt because an answer changed; a lowered
   width stops new takes and withdraws nothing; a wait keeps the bound it
   began under.
2. **An answer out of domain is refused, never used**, naming version,
   question, subject and line; the fallback answers that tick.
3. **The active version is always a stored version.** From `up` on exactly
   one version is active; before `up` nothing runs (CORRECT-USE step 2).
4. **A runtime error never stalls a tick.** Every tick is answered by the
   active, the previous or the built-in version; moves that need no answer
   always proceed.
5. **One version answers a tick**, and the receipt names it.
6. **Answers are deterministic**: one version and one input give one
   answer, so the cache is exact and `check` on a snapshot predicts the
   tick on that snapshot.
7. **Policy never writes the store and never raises a move.** Every move is
   a step's, checked by layer 2 and fenced by layer 1; the worst in-domain
   answer yields legal plans, so a policy can make the sprint slow but
   cannot corrupt it.
8. **At most one open policy judgement per version**, by its group key.

## The model

`tla/Policy.tla`, small: versions 1..3 plus `Default`, two ticks of
lookahead, one card, one row.

- Variables: `stored` (set of versions), `active`, `prev`, `breaks` (the
  versions that fail on the current snapshot), `answeredBy`, `judgements`,
  `cards` (the card's state, for invariant 1), `waitBound`.
- Actions: `Set(v)` (adds v to stored); `Activate(v)` (v stored and not in
  `breaks`; `prev' = active`); `Drift` (the snapshot moves: `breaks` changes
  arbitrarily, `Default` never in it); `Tick` (answered by the first of
  active, prev, Default not in `breaks`; opens the judgement for active
  when it breaks; may move the card); `Answer` (an activation that runs
  clean answers the open judgement).
- Invariants: `ActiveStored`; `AnsweredByUsable` (never a breaking
  version); `TickAlwaysAnswered`; `ActivateMovesNoCard`; `OneJudgementPer
  Version`; `WaitKeepsItsBound`.
- Reversed witnesses, each a required violation, run by `tla/ci-tlc.sh`:
  an activation of an unstored version; no fallback when active breaks
  (the stall); an activation that moves the card; a breaking version used;
  a fallback to a never-active version; a judgement opened every tick.

## How it is checked

- **Unit, package `policy`**, no time, no sockets, no store: compile once
  per version (an `ExecFile` counter); recursion, `while`, `load` and global
  reassignment refused; a mutation of a frozen global is an error; the
  step budget stops an unbounded comprehension; the same inputs give the
  same answers over many calls; every name, arity and domain refusal with
  its exact text, version and line; the fallback order.
- **Conformance, against `store.Fake`**: every question answered by the
  default on every snapshot shape the layer 2 conformance suite builds;
  `set`, `activate`, `show`, `diff` and `check` with every refusal; a
  version broken mid-sprint (a stream name it divides by) falls back,
  opens one judgement, and the next activation answers it; `check` output
  equals the next tick's answers on the same snapshot. The verb refusals
  run again on PostgreSQL in the functional tier.
- **Fuzz**: `FuzzAnswers` builds a policy whose questions return fuzzed
  Starlark values (any type, any size, nested), runs a tick on Fake, and
  asserts no panic, every answer either used in domain or refused with its
  name, the store never returning `ErrInvalid`, and width never exceeded.
- **Load**: at 5,000 producers and a one-second tick, policy's share of the
  tick with a cold cache and a warm one, against a budget of 100 ms (a
  guess until measured; README's gate decides the number).
- **Cold-read questions**: Can an answer put a consumer on a row that does
  not serve its tier? What answers a tick after `rows` starts failing on a
  new stream, and who is told? When can `check` and the next tick disagree?
  Can policy change a priority level? Where is tick N's version written?
  Can lowering `row_silent_withdraw` withdraw a consumer next tick?

## Owed to other layers

- **Layer 1**: nothing. A card's tier is its fixed minimum and a consumer's
  stays its producer's (round 9); escalation and reads above the writer are
  row choices by rank (`rows`), and a row ranked above a tier lists it.
- **Layer 2**: "which consumer is youngest is policy's" becomes layer 3's
  fixed rule. **Layer 3**: preemption of the youngest working consumer on
  first row of the blocker's `rows` answer that has one; the questions per
  tick; the one clock in running time (LESSONS 1.14); `answered_by`; the
  policy judgement. **Layer 4**: the route rest (LESSONS 4.24); row facts.

## Size

An estimate: 1,300 lines of Go, 80 of Starlark, 150 of TLA+, 1,000 of tests.

## Lessons placed (LESSONS section 5, and others this layer holds)

- 5.1 engine or policy by what a wrong answer does: PLACED here (the boundary).
- 5.2 policy changes without a binary: PLACED here (the verbs, the lifecycle).
- 5.3 order within a level, tiers, escalation: PLACED here (`order`, and
  `rows` by rank with the tier as the fixed minimum).
- 5.4 cards declare facts, rows' facts from config or probe: placed in part
  (`rows` is where the match lives); the fact columns are MISSING, owed
  to layers 1 and 4.
- 5.5 a gates-only row gets no generic work: placed in part (`width` can
  give it zero once the fact exists); the fact is MISSING with 5.4.
- 5.6 no default leaves the sprint unable to deal: PLACED here (the
  validator's joint check; `up` activates the default).
- 5.7 every judgement has a deadline and a safe default: placed in part
  (`timeouts().judgement`; the policy judgement's safe default); the safe
  default per kind is layer 3's, MISSING.
- 5.8 every wait has a deadline: PLACED here for the numbers (no unbounded
  timeout is in domain); the one clock is layer 3's (with 1.14).
- 5.9 the same finding twice: placed in part (`same_finding`); comparing
  findings is a layer 3 step, MISSING.
- 5.10 favour by priority, never by holding: PLACED here (no question can
  hold a stream; the joint check refuses a `rows` answer that starves one).
- 5.11 spend capped per tier: placed in part (`prices`); the cap, the
  breaker and the burn-rate alarm are deferred to layers 3 and 6, MISSING.
- 5.12 a blocker preempts: PLACED in the boundary as layer 3's fixed rule
  (youngest working consumer, row from `rows`) with LAYER-2's moves.
- ENFORCED.md's L1-S6 cites LESSONS 5.9 for policy versions; by this
  section's numbering that looks like 5.2 (to be fixed there by Emma).
- Also held here: 3.4 (read count), 3.7 (`verdict`), 4.7
  (`row_silent_gone`), 4.24 in part (the rest numbers).

## Decided inside the layer (Rowan, 2026-11-06), easy to overrule

- One version answers a whole tick, so the receipt names one set of rules;
  kind `data` stays unused; a data table later is a question, not a second
  mechanism. Prices ship empty rather than invented.

## Built (2026-11-06, branch v2-l5-policy)

Package `policy/` and `tla/Policy.tla`. What differs from the design, and why;
every number is marked measured or guess.

**The embedding.** As designed: `ExecFileOptions` with zero `FileOptions`,
`Thread.Load` nil, `Thread.Print` discarding, globals frozen, the universe plus
`TIERS` and `LEVELS`. One compile per stored version, cached by number (an
unstored file in `check` or `set` is compiled fresh and never cached). The step
budget is per call, not per tick: each call gets a fresh thread with
`CallSteps` = 1,000,000 (guess), and the file's top level gets the same. Two
Starlark texts the design did not fix, as the library prints them: `set` is
"this Starlark dialect does not support sets"; `load` is "load not implemented
by this application". A syntax error at the end of a line names the next line
("got newline"), as Starlark reports it.

**The inputs.** The fields the design lists, with these readings of the
snapshot types: `order`'s `prior_head` is the producer's `head` (a producer has
no prior-head column; layer 2 copies its head into the work consumer's at
`Start`); `writer_rank` is on every `rows`, `reads` and `verdict` card, the
rank of the row named by `author`, or the card's tier when that row is unknown
or there is no author; a read's `finding` is the read consumer's `reason` (a
read has no finding column). Candidates are computed here from the snapshot
(up, not held, serving the card's tier, the author excluded for a read; by
name) and shared by every `rows` question with the same tier, kind and author.
`landing`'s `rung` is layer 3's state, so `LandingQuestion` carries it; check
and `FullQuestions` use `flowing` unless given. `width` is asked for every row
and the argument-free questions once, every tick (the joint check needs every
width). A subject the snapshot does not hold is not asked; its answer is absent.

**The validator.** Every refusal in the design, plus: an `order` answer must be
a tuple (a list is refused) of ints fitting 64 bits or strings, compared int
before string (`CompareOrder`); a `rows` answer naming a row twice is refused;
"rank below the card's tier" fires for a known row that is not a candidate (a
candidate always lists the card's tier, so is never below it). The joint check
as built: a `rows` answer with candidates must name at least one row whose
`width` work is above 0 whenever some candidate has a configured width above 0;
it is reported at the line of `rows`. Dicts are closed: an extra key is refused.

**The tick's answer.** `Engine.Answer(Versions, Snapshot, Questions) (Answers,
[]Failure)`: tried in order active, previous active (when different), built-in;
the first version with no error and no refusal answers everything. Each failing
version tried is one `Failure`, so a failing previous version also gets its own
judgement. `Answers.Version` is the version that answered (`answered_by`; 0 the
built-in, -1 when even the built-in failed). `Judgements(snap, failures)`
builds the `PutJudgement` for each failing version with none open (group
`policy:v<n>`, id `policy-v<n>-s<seq>` so an id is never reused after an
answer, command `nova-sprint policy check v<n>`); its deadline is left to layer
3, which holds the clock (now plus `timeouts().judgement`). The answer cache is
keyed by version, question and input (the arguments as printed; for `rows`, a
sha256 of the shared candidate list), bounded at 200,000 entries (guess) and
emptied when full. Load, measured on amd-3 without -race, 5,000 ready producers
over 50 rows, every question: cold 132 ms, warm 68 ms, against the 100 ms
budget (a guess); cold is over it.

**The verbs.** `Up`, `Set`, `Activate`, `Show`, `List`, `Diff`, `Check` and
`LoadVersions`/`PrevOf`, taking a `Caller` whose `Coordinator` is layer 6's
answer. Refusals are `*Refusal` in the CORRECT-USE form (`SPRINT REFUSED policy
<verb>: <what>: <fix>`). The reason goes in `Op.Note` (kept in the receipt; the
events' `reason` column is the refusal's), and an empty reason is refused. `up`
stores and activates the built-in in one op. `activate` pre-checks "already
active" with the store's own text and answers the open policy judgements of
both the old active and the version it activates; it has no `AtSeq` fence (the
snapshot moves every tick; a version that breaks after its check is caught by
the fallback). `Show`, `List` and `LoadVersions` walk the whole log; layer 6
should call `PrevOf` as it folds events rather than `LoadVersions` per tick.

**The default.** `policy/default.star` is the design's text unchanged. Not
marked v1 or guess, because they are rules rather than tunables: the 0/1 fix
flag in `order`, "one rank" in `_up(..., 1)`, and "more than one card" in
`stitch`.

**Tests.** Conformance shapes are the suite's own (empty; seeded; escalated
with a security prose and a down row; landed and merging with a narrowed row):
layer 2's builders are unexported test code in `card/`. The mid-sprint break
and every verb row run on Fake and on PostgreSQL. `FuzzAnswers` checks no
panic, in-domain answers or a named refusal with the built-in answering; the
design's "the store never returning ErrInvalid" is not checked here, since a
tick's answers reach the store only through layer 3's ops.

**The model.** As designed, with `FallbackWasActive` added (the never-active
fallback witness needs an invariant to break), a seventh witness
(`PolicyWaitRebound` for `WaitKeepsItsBound`), and five reach checks. There is
no separate `Answer` action: answering the judgement is part of `Activate`, as
in the code. TLC on hetzner2, MaxVersion 3: `Policy.cfg` passes (34,945
distinct states); the seven witnesses each break their target invariant; the
five reach checks are each reached: 13 runs, run one by one with
`tla/run-one.sh` because hetzner2 was rebooting every few minutes; the full
`tla/ci-tlc.sh`, which now includes them, was not run end to end.

**Names.** Layer 3's design names its questions differently (`regrace`,
`victim`, `rung`, `bounce_limit`, `escalate_after`, `budget`, `pack`,
`deadline`). Layer 6 defers to this design for policy, so these names stand;
`regrace`, `budget`, `pack` and `deadline(kind)` have no question here and are
owed to the layer 3 and 6 wiring.

**The catalog** (added at the coordinator's request, 2026-11-06, for the row
verbs). Neither a `models` question nor a data section existed, so `catalog()`
joined the question set: `{"models": {provider: [{"name": n, "tier": t}]},
"harnesses": [h]}`, names non-empty, tiers from `TIERS`, nothing listed twice,
validated every tick like the others. `policy.ReadCatalog(ctx, store, version)`
(0 the built-in) returns `Catalog{Version, Models map[string][]Model{Name,
Tier}, Harnesses}`; it is named ReadCatalog, not Catalog, because Go cannot
give a function and a type one name. `Catalog.Unknown(model, harness)` is the
row verbs' refusal text, naming the policy version; the fix is `CatalogFix`
(`nova-sprint policy set <file> --reason <why>`). The default's tier map is Glenn's (Hacker News, 2026-11-05),
quoted in `default.star`: haiku, luna and deepseek-flash at flash; sonnet,
terra and mercury-3 at pro; opus and sol at heavy; fable, astra and argon at
frontier. The providers are the coordinator's mapping (anthropic, openai,
google, deepseek, inception); openai and google are guesses, and astra sits
under `unknown`, a guess. 2026-11-07 (one vocabulary, defect 36): a name is
the harness's own model id under its provider, and `Catalog.ForTier(tier)`
hands a bench `provider/name` (a name holding a `/` as it is), providers in
name order; deepseek-flash-4.1 became deepseek-flash (the id opencode lists),
and mercury-2.5 (inception) and ultramarine-1-flash (paperinstruments) joined
at flash, their tiers a guess. Harnesses: claude, codex, opencode and api (the
API-rate adapter).

**Size.** At round 3: 2,529 lines of non-test Go, over the 1.5k budget by
about 1,030; 117 lines of Starlark.

**After the cold read of e33eea61** (seven defects; the probes are
`policy/coldread_test.go`, copied from the cold reader's clone; the rulings
are the coordinator's, 2026-11-06):

1. The answer cache keys by label (`v<n>` or `default`) and only a stored
   version or the built-in is cached (`program.cached`); `check` and `set`
   compile files uncached, so a file named `default` is just a file.
2. The default's `rows` lists every serving row as the question table now
   says, so a heavy card wanting a width-0 frontier row is dealt to the heavy
   row. Candidates reach `rows` by rank, lowest first, then by name (the
   default needs no sort; 140 ms cold, measured, against 214 ms with two
   sorts under load). The joint check stays as the guard.
3. `verdict` gains `retry`, in the question table above.
4. A judgement's question is made storable: invalid UTF-8 and NUL replaced
   by U+FFFD, at most 4,096 bytes, cut at a rune with " [cut at 4096 bytes]".
5. and 6. One thread per version per tick, with the per-call step budget
   kept, a per-tick step budget `TickSteps` = 10,000,000 (a guess: 1.5 x the
   default's 6,550,740 steps, measured at 5,000 producers over 50 rows), and a
   per-tick wall clock `TickTime` = 250 ms (the ruling; the cold tick measured
   129 ms, 140 ms after this change, on amd-3) enforced by `Thread.Cancel` from
   a timer. A spent budget is that version's failure: one judgement and the
   fallback, which gets a budget of its own, so a tick in which every version
   spends its budget takes at most three budgets. Starlark has no memory
   limit, so the wall clock is the only bound on what a policy allocates: one
   builtin step (a 512 MiB string) runs to its end and is refused after it,
   not interrupted. A `-race` build scales `TickTime` by 3 (`race.go`; the race
   detector runs Starlark five to ten times slower, measured), and the load
   test skips its timing under -race. The load test runs alone, not parallel,
   and now requires the cold tick inside `TickTime`.
7. `ReadCatalog` refuses an unknown, negative or data-kind version naming
   `nova-sprint policy show --list`; a model under two providers is refused;
   `catalog()` is optional in a version, and one without it has the built-in's
   catalog (in `ReadCatalog` and in the tick), so versions from before catalog
   stay valid.

The model now lets the built-in fail too: `Drift` may break `Default`; when
active, previous and built-in all fail, the tick is answered by `NoAnswer`
(`Answers.Version` -1), still applies the moves that need no answer, and deals
nothing. `TickAlwaysAnswered` is now proved against that case (`NoAnswer` only
when every version tried failed), with a new invariant `NoAnswerStartsNothing`,
three new witnesses (`PolicyDefaultSkipped`, `PolicyBrokenDefaultUsed`,
`PolicyNoAnswerDeals`), `PolicyTickStalls` re-cut against the new last resort,
and the reach check `NeverNoAnswer`.

Checked on vision (hetzner2 was down), `bash tla/ci-tlc.sh` at a9661204: 115 runs
all as required, 17 of them Policy (Policy.cfg no error; 10 witnesses each
violating their first invariant; 6 reach checks violated). Race suite green on
amd-3; cold tick 133 ms at 5,000 producers over 50 rows, without -race.

**Round 3** (2026-11-07; rulings 1 to 7 from the Haiku fuzz, Stella's source
review and cold read 2, and the Opus breaker's ruling; the probes are
`policy/coldread2_test.go` and `policy/fuzz_break_test.go`, folded in with
their readers' attribution in the headers). What each ruling became:

1. A refusal before storage names the file (`check`, `set`: the `Label` of
   an unstored source is its file name, its `Version` 0); after storage, the
   version (`v7`, `default`). Said under the refusal form above.
2. Every refusal has a line. Something the file does not define (a missing
   question, a file over `MaxBody`) is refused at the file's last line,
   printed `line N, end of file`; an unknown top-level name or a name bound
   to a non-function is refused at its own line, found from the parsed file
   (the engine now uses `SourceProgramOptions` and `Program.Init`, so the
   syntax tree gives every top-level binding's line). A failure the engine
   cannot place in a function (the top level running over the deadline) is
   at line 1 and names the range, "running the top level, lines 1 to N".
3. `TickTime` is 500 ms, marked measured (the ruling's cold 217 ms on amd-3,
   margin 2x); -race still scales it by 3.
4. One deadline per tick: `AnswerContext(ctx, ...)` takes the caller's
   context and adds `TickTime` with `context.WithTimeout`, from the first
   byte of input construction to the last answer returned; `Answer` is
   `AnswerContext` on `context.Background()`. The deadline is checked at
   every phase boundary (inputs, each version's top level, each question,
   the joint check) and inside the validator's loops (`late(ctx, i)`), and
   `context.AfterFunc` cancels the running thread. Each version runs in its
   own goroutine and thread; the versions after the one being waited on
   start at `fallbackAfter` = `TickTime`/2 (a guess) if it has not answered,
   and the first version in order that passes answers, so starting early
   never changes which version answers (the model's `Pick`). The per-tick
   step budget `TickSteps` is gone: the deadline is the one bound;
   `CallSteps` stays per call. The `TickTime` comment is corrected: no
   memory bound exists in-process; one builtin step (a large allocation)
   cannot be interrupted, and its goroutine is abandoned at the deadline,
   its version failed. The separate evaluator process that would bound
   memory is the 2.0.x item, so the child-process worker started in the
   half-finished round 3 was set aside, not built. Result and cache bytes
   are capped at 4 MiB per tick (`maxTickBytes`; an answer's weight plus
   `entryBytes` = 96 per entry, a guess); a version over the cap fails with
   "over 4194304 bytes". The answer cache now holds only the last passing
   tick's answers, committed when the whole tick passes (committing empties
   every other version's), so it is bounded by the same 4 MiB; equal `rows`
   answers share one slice per tick, read-only.
5. `Diff` is linear-space (Hirschberg's LCS after trimming the common
   prefix and suffix) and refuses either side above 10,000 lines.
6. Bounded readers: a policy file is read through a `LimitReader` at
   `MaxBody`+1 and refused as "over 65536 bytes".
7. `ReadCatalog` and a tick's `Answers.Catalog` are isolated copies (each
   call clones the maps and slices), and the effective version is named the
   same way in both: a version with no `catalog()` has the built-in's, and
   `Catalog.Version` is 0 in both.

**The breaker's ruling.** The built-in's `rows` walked candidates x
producers in Starlark and spent its step budget at 5,000 producers over 100
rows. The candidate ranking is now a Go builtin, `prefer(candidates,
rank)` (`policy/prefer.go`), memoised per candidate list and rank for one
version's tick; the script hands it the short list and the wanted rank.
The default's text otherwise stands. Load, measured on amd-3 without -race,
5,000 ready producers over 100 rows that all serve every tier, every
question: cold 56.8 ms, warm 22.0 ms, inside the 500 ms budget.

**Probes.** Each `TestColdread2Defect*` and fuzz probe passes as written,
except where a ruling changed the expected text: the fuzz probes match
`line \d+, end of file: ` for a missing question; cold read 1's alloc probe
asks for 32 GiB in 128 MiB steps, since no memory bound exists and the
deadline is what refuses it; its unbounded-steps probe now expects "time
budget" (2,000 producers x range(30000)); the cache probe measures the
engine's cached bytes against the 4 MiB cap.

**API.** Kept: `Arity`, `CompareOrder`, `RungBackedUp`, `DefaultBody`,
`Answer`, `Judgements`, `ReadCatalog`. Added: `AnswerContext`. Removed:
`TickSteps`. The question set's names and types are unchanged.

**The model.** `tla/Policy.tla` unchanged in its actions: the deadline is
one more way a version is in `breaks` on a tick, and the early fallback
does not change `Pick`; only its header names the round 3 code. TLC on hetzner2,
`bash tla/ci-tlc.sh` at 4e8969fb: the 17 Policy runs all as required
(Policy.cfg no error, 10 witnesses, 6 reach checks); 114 of 115 runs ok,
the one failure `CardDeps` (layer 2's model, not this layer's), killed
with exit 137 after 8 minutes and 3.85M distinct states.

**Owed.** The trace check binding `tla/Policy.tla` to the code (Glenn,
2026-11-07: the code's random walk emits traces, TLC checks them in CI) is
not built here; until it is, this layer does not lock. Layer 3 is told the
budget is 500 ms and that `AnswerContext` carries its deadline. (Built in
round 4, below.)

**Round 4** (2026-11-07). Emma's five break probes, the trace check, and
size.

*Emma's probes* (`policy/break_emma_test.go`, copied from
origin/v2-break-emma at 07993b6c with her attribution, gofmt only; every
probe passes as written, none weakened):

1. `TestBreakVerdictCountsDoneReadsFromDifferentHead`: the inputs counted a
   done read of the current review whatever its head, so a stale ok read let
   a fault-only head land. `verdict`'s reads (and the verdict question
   itself) now take only done reads of the producer's current review *and*
   head (`inputs.go` count), as the question table says ("its done reads of
   this head and review").
2. `TestBreakDefaultRowsDropsBusyCandidatesAndFailsJointCheck`: `prefer`
   returned only the candidates with room when any had room, dropping busy
   rows, so a gates-only row with room could fail the joint check while a
   busy row could work the card. It now lists every candidate in the
   default's order, those with room first (a stable sort), as the question
   table says ("every candidate listed ... those with room first").
3. `TestBreakCheckReportsRefusedJointAnswerAsValidChangeAndCascadesFromRefusedWidth`:
   `check` printed an answer the joint check refused as a change of value,
   and a refused `width` (absent from Answers) made the joint check refuse
   every rows answer. A refused answer is now shown as `(refused)`, and the
   joint check is not run when any width was refused (only `check` goes on
   past a refusal; a tick stops at the first).
4. `TestBreakDataPolicyRowBlocksUpAndCompilesInCheckShowDiff`: `up` took
   any row at version 1 as "already up", so a `data` row stored first left no
   active version; `check v<n>` compiled a data row as Starlark. `Up` now
   acts when no version is active, storing the default as the next version
   and activating it in one op fenced by `AtSeq` at its snapshot; `check`,
   `activate` and `catalog` refuse a data version through one helper
   (`script`), with one fix text: `name a script version: nova-sprint
   policy show --list` (activate's and catalog's tests updated to it).
5. `TestBreakLoadVersionsWalksPastSnapshotSeqCollapsingPrevIntoActive`:
   `LoadVersions` folded events past its snapshot's seq, so an activation
   between the two reads made Prev equal Active and the tick skipped the
   previous active. The walk now stops at the snapshot's seq.

No probe contradicts the design. `tla/Policy.tla` says "Init is the state
after up: the default stored as v1"; with a data row stored first the
default is stored as the next number. The model has no data rows, so
nothing in it changes.

*The trace check.* The shape of `tla/README.md` "Trace validation",
copied: `policy/trace.go` `TraceState` is the refinement mapping (every
variable of `Policy.tla` named: stored, active, prev, breaks, answeredBy,
judgements and card projected; everActive, tickBreaks, cardBefore,
waitBound, startBound and lastOp the model's own bookkeeping, computed by
the action the record names). `breaks` is each stored version and the
built-in run by check on the snapshot's full question set on an engine of
its own; a version not yet stored is never in it (Set checks before it
stores). `policy/trace_test.go` `TestTraceWalk` drives, on the Fake, up,
set, activate, a snapshot that drifts (row x<k>'s width between 1 and 2;
version k, and the built-in at k = 0, fail while it is 2) and ticks that
write their judgements and move one card as layer 3 would (a deal only
with an answer), seeds 1 to 10, 300 steps, versions up to 4; a drift that
changes no version's outcome is a stutter and is not recorded.
`tla/PolicyTrace.tla` constrains `Policy.tla`'s actions to the records
(each carries the whole projected state, which is small); it adds nothing
to the model. The breaker, `TestTraceRefusesDivergence`, is one line: the
tick's versions with the previous active dropped; its fixture
`tla/traces/policy-breaker` is refused at 5 of 5 (Tick: answeredBy 0 where
the model's Pick is 1). `tla/make-traces.sh` runs and checks the policy
walks; the CI job `trace` runs that script, so it gains them.
`tla/README.md` "Trace validation" does not yet list the policy walk (not
this layer's file).

Results on hetzner2 at a3b3143f (`bash tla/make-traces.sh check`, tla2tools
v1.8.0): policy-walk-1..10 TRACE OK steps=203 212 206 206 191 214 211 209
213 210; policy-breaker refused at 5 of 5 (expected); the store and card
traces as before (8 + 2 + 20 OK, three breakers refused). Seeds 2, 4, 6
and 8 reach NoAnswer (answeredBy -2: 3, 3, 7 and 4 ticks, measured); the
others do not. TLC: `bash tla/ci-tlc.sh` on hetzner2 at a3b3143f, 115 of 115 runs ok, the 17 Policy runs among them as required (Policy.cfg no error, 10 witnesses, 6 reach checks; Policy.tla unchanged this round).

*Size.* Before: 2,529 lines of non-test Go at cdeaddb2 (1,996 without
comments and blank lines). After: 2,545 (1,971), of which `trace.go`, new,
is 85 (55) and the probe fixes about 30; the code that was there is about
100 lines smaller (one `errAt` for a Starlark error's line and message,
`questionNames` sorted once, `maps.Keys` for sorted keys, `askOf` for every
one-value question, `candidates` folded into `candidateSet`, one generic
`intIn`, rows answers cached with their names so `intern` never splits,
`weight` over `Indexable`, the diff reversing its halves, `cmpErr` gone).
Every function is under 60 lines (golangci-lint funlen). What remains is,
I think, honest, and 1.5k is not reachable without dropping a guarantee.
Code lines by file (measured, without comments and blanks): validate.go
283 and catalog.go 151 (one refusal per out-of-domain shape, each with its
exact text, as the design requires); tick.go 251 (the one deadline: each
version on its own goroutine and thread, the early fallback, the late
failure naming the question in hand, cancellation at every phase, and the
answer cache with its 4 MiB bound: rulings 3 and 4); verbs.go 224 and
show.go 143 (the eight verbs and their CORRECT-USE refusals); engine.go 209
(compile, the closed name set, error lines); inputs.go 178 (seven subject
structs, the shared candidate sets, the counts); answer.go 146; policy.go
136 (the exported types and the judgement text); diff.go 107 (the
linear-space diff, ruling 5); prefer.go 84 (the 5,000-producer budget);
trace.go 55. Each was added by a design line or a ruling, not by drift;
the 1.5k budget was set before rulings 3 to 7, the catalog and the trace
check. Glenn's call whether to raise the budget or drop a feature (the
diff verb and the catalog are the two that could move to layer 6).

Tests: `go test -race -count=1 ./policy/` on amd-3 under nova-reserve at
35b61d2e: ok (9.6 s), functional tests included. Load on amd-3 without
-race: the built-in at 5,000 producers over 100 rows, cold 61.7 ms, warm
27.6 ms (measured; up from 56.8 ms, since `prefer` now orders every
candidate).

**Round 5** (2026-11-06, on v2-integrate-22 after store round 22). Six
parts: the roster, rowan2's seven probes, cold read 3's eight, the rulings
on its contradictions, layer 3's steering numbers in `limits`, and the
ledger.

*The roster.* A store row is sprint state only (round 22); every row fact a
question reads, kind, width, tiers, model and harness, now comes from a
`roster.Roster` (nova-config, read by `roster.Load` in layer 6) by the row's
name. The roster is a tick input beside the snapshot: `AnswerContext`,
`Answer`, `Judgements`, `Check`, `Set`, `Activate`, `TraceState` take it
(and `ReadCatalog` takes the engine). A row the snapshot holds and the
roster does not name is in no question (no `width`, never a candidate).
Its judgement is the steps' alone (`steps` unlisted: kind `roster`, about
the row, group `roster:<name>`); `Judgements` opens none for it (the e2e
found two for one coordinator when both did). A worker has no model (the roster's
`Worker` has none): `model` is "" for a worker, its harness `nova-worker`.
`Rank` takes the tiers. The trace walk's drift moves a row's width in the
roster (nova-config changing under the sprint), as it moved the store's.

*Rowan2's probes* (`policy/break_rowan2_test.go`, from origin/v2-break-rowan2
113e1fc0, adapted only to the round 5 API, as its header lists):

1. and 2. `TestBreakSetBodyOrNameTheStoreRefusesReachesItAsErrInvalid`,
   `TestBreakReasonInInvalidUTF8ReachesTheStoreAsErrInvalid`: every string a
   verb puts in an op (the actor, the op id, the reason, the file's name,
   the body) is checked as the store checks it before the op is built
   (`unstorable`, verbs.go): a `*Refusal` naming the field, never
   `ErrInvalid`.
3. `TestBreakSetRaceStoresOneBodyTwice`: `set` is fenced `AtSeq` at its
   snapshot, as `up` is; a set that loses the race is refused as "v<n>
   already stores this body" when the winner stored its body.
4. and 5. `TestFunctionalBreakProseMakesTheInputsOutrunTheTickDeadline`,
   `TestBreakProseLeavesTheBuiltInUnableToAnswer`: the prose is no input
   and no part of the answer cache's key, and the evaluator's request drops
   briefs and prose (`unworded`). **One conflict with the ruling's premise**:
   the built-in's `_security` read `"security" in card.prose`, so a
   question did read it; following the ruling, a security card is now only
   one of `_SECURITY_STREAMS` (empty by default), so a card whose prose
   says "security" no longer gets frontier reads by default. A Go-side
   `security` bool on the card would restore it at a cost linear in the
   briefs; Rowan's call. Measured on amd-3 (exclusive, NOVA_LOAD, through
   the evaluator, cold engine): 5,000 producers in review with 16 KiB
   prose each, answered by the built-in in 113 ms (was 952 ms and no
   answer on the studio at f62551fe).
6. `TestFunctionalBreakFallbackTimerStartsAfterTheInputs`: the fallback's wait and
   the deadline are both counted from the tick's first byte
   (`AnswerContext` reads the clock before the inputs are built; `race`
   waits `fallbackAfter` minus the time spent). The probe now skips on
   amd-3: its precondition, inputs over TickTime/2 that grow with prose, no
   longer exists after 4 and 5. `TestFallbackCountsFromTheFirstByte`
   checks the waits on a stepping fake clock.
7. `TestBreakJointCheckIgnoresTheReadCount` (and cold read 3's 2): the
   joint check (`jointRows`) needs as many named rows with work above 0 as
   the card needs, one for work and the `reads` answer for a read (the
   names are distinct candidates, so never the author), capped by the
   candidates with a configured width above 0: "reads is 2, and 1 row(s)
   named have work above 0 in width, while 2 candidate(s) could take a
   read".

*Cold read 3's probes* (`policy/coldread3_test.go`, adapted only to the
round 5 API, as its header lists, and one line, below):

1. `TestColdread3DefectAbandonedRunOutlivesDeadline`: **the evaluator**
   (`policy/evaluator.go`). `NewEngine(clock)` runs every version (a tick's,
   check's, the catalog's) in a child process, this binary as
   `<binary> policy-eval`, one per engine and reused across ticks, gob on
   its stdin and stdout. The child runs the in-process engine with the
   budget the parent leaves it, so a late version still fails there naming
   the question in hand. The child reports when a run it abandoned is still
   running, and the parent kills it; a child that has not answered by the
   deadline plus `evalGrace` (100 ms, a guess) is killed and every version
   it ran fails. The next request starts a new child; a cleanup stops it
   when the engine goes. `newLocal` (in-process) is the unit tests' engine
   and the child's own. Layer 6 owes: its `main` calls `policy.ServeMain()`
   when `os.Args[1] == policy.EvalArg` (the tests' `TestMain` does). No
   memory limit is set in the child (round 3's RLIMIT_DATA is not carried):
   a child that allocates without bound is still killed at the deadline.
   Concurrent ticks on one process engine are served one at a time.
2. `TestColdread3DefectJointCheckIgnoresReadCount`: as rowan2's 7. The
   probe's first line required `a.Rows` to hold v2's answer `["b"]`, which
   no passing run allows once v2 is refused (the built-in answers); it now
   requires the refusal's text instead. The only probe line changed.
3. `TestColdread3DefectCheckBlindToBackedUpRung`: check (and `activate`,
   `set`, the trace's `breaks`) asks `landing` on both rungs; a backed-up
   ask's subject is `<stream> backed_up`.
4. `TestColdread3DefectKeywordOnlyParamPassesArity`: a question with a
   keyword-only parameter is refused at its arity.
5. `TestColdread3DefectExpiredContextJudgesOnlyTheBuiltin`: an expired
   caller context fails every version tried, one failure (and judgement)
   each, as the model's `Failing`.
6. `TestColdread3DefectAnswerTakesVersionsTheSnapshotDoesNotHold`:
   `LoadVersions(ctx, st, snap)` reads at the snapshot it is given and
   records its seq (`Versions.Seq`); `AnswerContext` refuses a Versions read
   at another seq ("versions read at seq N, snapshot at M: reload",
   Version -1, a failure of no version, which opens no judgement). A
   Versions built by hand (Seq 0, the tests) is not checked.
7. `TestColdread3DefectTupleBoundNameRefusedWithoutLine`: every name a
   top-level assignment binds (plain, augmented, tuple, list, for) records
   its line.
8. `TestColdread3DefectSetEqualBodyNamesDataVersion`: the equal-body
   refusal compares script versions only.

*The contradictions, as ruled.* `TraceState` projects `everActive` (the
active version and every version the log deactivated), so
`FallbackWasActive` is checked against the code; `waitBound` and
`startBound` are layer 3's waits, which this layer holds no state of, so
the ledger no longer claims `WaitKeepsItsBound` of this code (L5-8).
`Activate` is one op fenced `AtSeq` at its snapshot, the model's one step.
The clock seam: `Clock` (Now, After), `RealClock` the binary's; every unit
test is on a fake (`testEngine`); the wall-clock timing probes run only
with `NOVA_LOAD` set (and never under -race or -short). Any clean
activation answers the open `policy:v0` judgement, in the code and in
`Policy.tla` (`Activate` sets `judgements[Default]` to 0). `Policy.tla`'s
Init comment follows the code (up stores the default as the next version;
the model has no data rows, so 1). `DefaultBody` and `CompareOrder` have
tests. `prefer`'s order within a rank is by name (the candidate list
comes by rank, then by name, and the room sort is stable).

*Layer 3's steering numbers.* `limits` gains five keys (names exact, the
bounce limit stays `bounces`), each required, with the built-in's values
from layer 3's engine defaults (origin/v2-l3-steps, `steps/asker.go`
`Engine` and LAYER-3's Built section): `escalate_after` 2 (guess),
`frontier_after` 4 (guess, PRIORITY T19), `flap_limit` 3 (unmeasured: a
guess), `idle_floor` 0.5 (guess, LESSONS 3.18), `land_target` 0.9 (guess,
over 20 landings), and `bounces` 6 (guess). Domains: `escalate_after`
1..100, `frontier_after` escalate_after..100, `flap_limit` 1..100, the two
fractions an int or a float in (0, 1]. The built-in's `_want` reads its
own `_ESCALATE_AFTER` and `_FRONTIER_AFTER`, the values its `limits`
gives. One `TestRefusals` row per key.

*The ledger.* `tests/enforced` indexed only store, card and steps after the
layer 3 merge, so every L5 row's tests read as missing; it now lists
`./policy` too, and fuzz targets (`FuzzAnswers`). Rows L5-15 (the roster),
L5-16 (the evaluator and the clock) and L5-17 (the verbs' fields and
fences) are added; L5-3, L5-4, L5-8 and L5-14 carry the new probes.

*Size.* 3,141 lines of non-test Go (2,402 without comments and blanks),
from 2,545 (1,971) at round 4; the evaluator is 241 code lines, the clock
24, the rest the probes' fixes and the roster. Over the 1.5k budget, as
round 4 said; Glenn's call.

*Tests.* `go test -race -count=1 ./policy/` on amd-3 under nova-reserve at
c0067a27: ok (5.0 s). The timing probes with `NOVA_LOAD=1`, exclusive, on
amd-3 at a33fb7ef: all pass but `TestFunctionalBreakFallbackTimerStartsAfterTheInputs`
(skips, above). Load through the evaluator, 5,000 producers over 100 rows:
cold 106.7 ms, warm 59.1 ms (measured).

*Checks on hetzner2 at e22c0593.* `tla/ci-tlc.sh`: 149 ok, 0 failing (12
models with no error found, Policy among them; every Policy witness and
reach run violated as required). `tla/make-traces.sh check`: 60 ok, 0
failing; 11 policy traces (10 walks TRACE OK, the breaker refused as
required).

*STATUS at the end of round 5* (the method moved to one holistic pass over
all layers; this round stops here). rowan2's seven: all seven built; six
pass, `TestFunctionalBreakFallbackTimerStartsAfterTheInputs` skips by construction
(above) and its ruling is checked by `TestFallbackCountsFromTheFirstByte`.
Cold read 3's eight: all eight built and pass. Rulings built: every one in
this section, the contradictions included. Half done or owed: layer 6's
`main` calling `ServeMain` on `EvalArg` and building
`NewEngine(RealClock{})` (no nova-sprint main exists on this branch); no
memory limit in the evaluator child; the built-in's prose security rule
(Rowan's call, above); the size over budget. The timing probes ran on
amd-3 at a33fb7ef; the commits after it change only tests and docs, so
they were not re-run.

### Stitch (2026-11-07, the stitch on v2-stitch, at policy round 5 6e1dfa2a)

What the stitch changed in layer 5 and why:

- **The evaluator's memory cap (Glenn's ruling 1).** `EvalMemory`, 512 MiB,
  the policy layer's number, passed to the child in
  `NOVA_POLICY_EVAL_MEMORY`. The child caps itself before it reads a request
  (`evalmem.go`): the Go runtime's memory limit, and a watchdog reading the
  live heap every 2 ms that ends the child with "over the evaluator's memory
  cap of 512 MiB" on stderr. RLIMIT_AS, the ruling's first choice, is not
  used, and that is the closest correct thing rather than the thing asked:
  the Go runtime reserves address space far past its heap (a -race build
  reserves terabytes of shadow), so an address-space cap near the heap cap
  stops the child before it starts, and darwin does not enforce one. A
  single allocation is bounded by Starlark's own 1 GiB, so the heap can pass
  the cap by at most that before the watchdog sees it. A child that dies is
  read as before: every version it ran fails, naming the line it wrote, and
  Judgements opens one per failing version. `TestFunctionalEvaluatorMemoryCap`.
- **Security reads (Glenn's ruling 2).** The prose rule stays gone: frontier
  reads come only from a card's tier or its stream. A security stream names
  its need in its setting, the policy's `_SECURITY_STREAMS` list (the
  coordinator adds the stream with `policy set`), never in a brief's prose.
  The design's listing of the built-in above now shows the stream-only rule.
- **Drift row 26 and row 15: numbers that were Go literals elsewhere are
  policy's.** `limits()` gains `fleet_width_cap` (the roster's cap, was
  `roster.FleetWidthCap`); `timeouts()` gains `tick` (run's budget for one
  tick, 10 s, a guess) and `consumer`, a deadline by tier naming every tier of
  `store.Tiers` (900, 2700, 5400, 7200 s: nova-worker's old Go literals,
  guesses), which run puts in the take receipt so the worker holds no
  deadline of its own. Every key is required and each has a refusal tested.
- **The roster judgement's command** named `nova-sprint answer`, a verb that
  does not exist; it now names the fix (`nova-config friend add <row>`) and
  the verb that closes it (`nova-sprint judge <id> retry --reason <why>`).
- **Owed at layer 6, done there:** main calls `ServeMain` on `EvalArg` and
  builds `NewEngine(RealClock{})`.

**Stitch, the bench model and the first set (2026-11-07, branch `v2-stitch-w2D1`).**
- *The catalog's `bench` (defects 54, 57).* `catalog()` may carry
  `"bench": {tier: "provider/model"}`, the one model a bench row runs for a
  card of that tier. Each key must be in `TIERS` and its model one of that
  tier's models in the catalog (`bench: <tier>'s model <m> is not one of
  its models (...)`); a list is refused. `Catalog.Bench` and
  `Catalog.BenchModel(tier)`; run passes the active version's map to the
  steps as `Answers.Models`, and the deal writes the model on the consumer
  (LAYER-3's Built). A tier with no bench model leaves bench rows undealt
  for it with a `no-model` judgement, never a guess. The default names
  deepseek/deepseek-flash at flash and heavy, inception/mercury-3 at pro,
  deepseek/deepseek-v4-pro at frontier (the coordinator's mapping of
  2026-11-07; guesses, unmeasured), and lists deepseek-flash at heavy and
  deepseek-v4-pro at frontier so the bench key names models of the tier.
  `next --json`'s `tier_models` is kept as the probe's input and now holds
  that one model per tier for a bench; the take receipt's `models` (the
  candidates) is gone: the receipt carries the deal's `model` only.
- *The first set activates (defect 53).* `policy set` with no version
  active stores the version and activates it in the same op (`Result.Activated`;
  the verb prints `POLICY STORED AND ACTIVE v<n>`), answering any open
  judgements about the missing policy; with one active it stores only, as
  before. While none is active, or the active one fails whole, layer 6
  tells the steps to deal nothing (`Answers.NoDeal`) and the steps open one
  `no-policy` judgement on the coordinator's row. Tests: `testSetFirst`,
  `TestCatalogBenchRefused`, run's `TestTickDealsNothingWithoutAnActivePolicy`.

### Built: no deal-ahead for a worker (stitch w3W, 2026-11-07)

- `default.star`'s `width(row)` answers `ahead: 0` (it was `row.width`, v1's DealAhead): a worker
  is dealt only what it can run now (defect 74: a bench dealt ahead held cards it could not
  start while other rows sat idle). `row.width` itself is now a worker's advertised width
  (min of nova-config's cores-cap width and its nova-reserve slots, `roster.Advertised`).
  `TestDefaultAnswersEverySnapshot` says `Ahead: 0`.
