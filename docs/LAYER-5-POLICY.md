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

| question | inputs | answer and domain | the engine also refuses | default |
|---|---|---|---|---|
| `order(card)` | id, stream, score, priority, tier, attempt, bounces, prior_head | a tuple of up to 4 ints or strings; the engine sorts by (rung, answer, id) | a tuple of other types or longer than 4 | fix attempts first, then score |
| `rows(card, kind, candidates)` | the card: id, stream, tier (its minimum), prose, attempt, bounces, priority, and for a read `writer_rank` (the rank of the row that wrote the head); `kind` `work` or `read`; `candidates`: the rows up, not held and serving the card's tier (the author excluded for a read), each with name, tiers, rank (the highest tier it lists), model, harness, width, working, ready | the candidates to use, in preference order; the engine deals to the first with room (for a read, `count` distinct rows in that order) | a name not in `candidates`; a row whose rank is below the card's tier; an empty list while candidates exist | the card's own rank; one rank up at 2 bounces, frontier at 4; for a read the rank above `writer_rank`, frontier for a security card; every candidate listed: the wanted rank and up, nearest first, then the ranks between the tier and the wanted one, highest first; those with room first, all of them when none has room (ruled 2026-11-06, so the joint check passes whenever any row could take the card) |
| `reads(card)` | as `rows`' card | the count n, 1 <= n <= 5 | n out of range | 1 |
| `verdict(card, reads)` | the producer; its done reads of this head and review (row, rank, result ok, fix or fault, finding) | `"yes"`, `"fix"` or `"retry"` (every done read faulted: layer 3 asks one more read; ruled 2026-11-06 after the cold read) | `"yes"` with no ok read, `"fix"` with no fix read (layer 2's rules), `"retry"` while a done read is ok or fix | any fix read means fix; else an ok read means yes; else retry |
| `width(row)` | name, kind, width (nova-config's), tiers, model, harness, flaps | `{"work": w, "ahead": a}`, 0 <= w <= width, 0 <= a <= 4 x width | over the row's width | work = width, ahead = width (PRIORITY D21: 2 x width held) |
| `limits()` | none | dict: `bounces` 1..100, `faults` 1..100, `same_finding` 1..10, `reports_per_tick` 1..10000, `route_rest_window` 1..100, `route_rest_after` 1..window | out of range, a missing key | below |
| `landing(stream)` | name, merging, review, rung | `{"backed_up": b, "flowing": f}`, 0 <= f < b <= 1000 | f >= b | backed up at 4 merging, flowing again at 1 |
| `timeouts()` | none | dict of seconds of running time, each 1..86400: `row_silent_gone`, `row_silent_withdraw` (>= gone), `landing_stall`, `base_regate` (a rising list), `judgement`, `route_rest_for` | zero, negative, absent: no wait is ever unbounded | below |
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
    return card.stream in _SECURITY_STREAMS or "security" in card.prose.lower()

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
    return {"work": row.width, "ahead": row.width}  # v1 DealAhead 2

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
    return {"backed_up": 4, "flowing": 1}  # guess

def timeouts():
    return {
        "row_silent_gone": 600,       # guess; never one missed challenge (LESSONS 4.7)
        "row_silent_withdraw": 1800,  # guess; above a 600 s gate (LESSONS 2.9)
        "landing_stall": 60,          # v1 (LANDING step 10)
        "base_regate": [120, 300],    # v1 (LANDING step 8)
        "judgement": 3600,            # guess
        "route_rest_for": 1800,       # v1 (PRIORITY T20)
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
quoted in `default.star`: haiku, luna and deepseek-flash-4.1 at flash; sonnet,
terra and mercury-3 at pro; opus and sol at heavy; fable, astra and argon at
frontier. The providers are the coordinator's mapping (anthropic, openai,
google, deepseek, inception); openai and google are guesses, and astra sits
under `unknown`, a guess. Harnesses: claude, codex, opencode and api (the
API-rate adapter).

**Size.** 2022 lines of non-test Go, over the 1.5k budget by about 522; 121
lines of Starlark.

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
