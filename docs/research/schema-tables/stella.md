# Schema-table adoption for nova-sprint and nova-tools — Stella study (worked by Aurora)

Author note: This file is the `stella` deliverable for card
`schema-adoption-research-stella.w2`, worked by Aurora under Rowan's standing
instruction that the `*-research-stella` cards are Aurora's to work while Stella
is down. It is an independent study: the sibling `aurora.md` and `emma.md` files
were not read for this document, and nothing here is signed as anyone else.
Labels: **SOURCED** = a path:line or link I opened; **REPORTED** = stated in the
brief, not opened by me; **ESTIMATED** = arithmetic shown; **OPINION** = mine.
Research only: no product code was written, built, run or benchmarked.

## 1 Inventory

Nova already moves a surprising amount of structured data as hand-parsed text.
The table below inventories the structured things sent or stored between
friends, the fleet, the seat and the server. Parsers are named where I opened
them; anything I could not open at the SHAs in section 9 is marked UNVERIFIED
rather than guessed.

| Id | Thing | Sender → receiver | Format today | Parser (file, function) | Failure modes | Incident |
|---|---|---|---|---|---|---|
| M1 | Card brief (RESULT/REPO/BASE/PATHS/NEW/TEST/DONE-WHEN/DEPENDS-ON/WHO/PRIORITY + prose) | seat/coordinator → member | line-oriented text, 16 KB cap (REPORTED) | nova-tools `internal/cardhdr/cardhdr.go:164` `ParseTest` (SOURCED); header lint (REPORTED) | missing tier refused late; prose drift found by lint lines; oversize note froze the drain (REPORTED) | oversize-note freeze (REPORTED) |
| M2 | RESULT.md worker report (head:/branch:/verdict:/gate:/output: + body) | worker → member/coordinator | hand-written text | `ParseCardResult`/`shapeFault` in nova-tools member code — not found at nova-tools `b8d29662` by grep, UNVERIFIED at this SHA | missing keys blamed on the worker as "RESULT.md shape fault"; two historical shapes (rev:/## One line) against one parser (REPORTED) | "no RESULT.md shape" endings (REPORTED) |
| M3 | Bus message (from, to, cc, re, kind, subject, free body) | friend → friend/coordinator | text note files over Redis | nova-tools `internal/bus/note.go:140` `ParseNote`, `:160` `ParseNoteAll`, `:305` `ParseNoteBodyStream` (SOURCED) | empty kind seen (REPORTED); reply matching by `re` + subject convention; structure smuggled in subjects ("#202 HOLD at …", "PING <nonce>") (REPORTED) | ~1,700 received in one night, >1,300 pongs (REPORTED) |
| M4 | Pong / liveness line (`pong <nonce> queue= working= width=`) | friend → coordinator | single-line text | nova-friend/nova-bus CLI parsing (UNVERIFIED at this SHA) | nonce/queue/width parsed by convention; a malformed line reads as silence, i.e. as death | liveness false-negatives (REPORTED class) |
| M5 | Beat / presence heartbeat | member → coordinator | text/JSON rows (REPORTED) | nova-tools `internal/presence` package exists (SOURCED, listing) | missed or malformed beat indistinguishable from a dead member | fleet liveness faults (REPORTED) |
| M6 | Hook challenge / proof (`nova-sprint hook bus challenge <hex>`) | coordinator ↔ member | hex token in text | nova-sprint hook code (UNVERIFIED at this SHA) | token copied by hand; no typed binding to card or member | handshake confusion (REPORTED class) |
| M7 | Send receipt (`SEND OK id=…` then `NOTE push=down|stale` prose) | nova-bus → sender | two text lines | substring matching in tests, e.g. nova-tools `cmd/nova-bus/beat_functional_test.go:84` expects `SEND OK id=ada-` (SOURCED) | prose suffix parsed by eye; a refused send and a pushed-stale send look alike | misread receipts (REPORTED class) |
| M8 | Refusal (lane fault, flag refusal, lint refusal) | tool → caller | truncated JSON object or prose on stderr, empty stdout (REPORTED) | each CLI ad hoc | `lane-fault refusal={` cut off mid-object; `--json` refused on lint/list/show/check/gate and more; refusals under `--json` printed as prose (all REPORTED) | truncated-JSON refusal (REPORTED) |
| M9 | Judgment (HOLD/FIX/merge verdict with ids in subject) | reviewer/coordinator → fleet | prose with ids in the subject line | subject-convention parsing (REPORTED) | verdict, card id and head SHA recovered by regex/convention; a reworded subject loses the verdict | lost verdicts (REPORTED class) |
| M10 | Landing / merge verdict + gate result | lander → coordinator/dashboard | prose + JSON fragments (REPORTED) | nova-sprint `cmd/nova-sprint/land.go` references RESULT.md handling (SOURCED, grep hit) | gate red vs malformed report not distinguished in one channel | HOLD churn (REPORTED class) |
| M11 | Card header extras (`GATE-NEEDS:`, `SIBLINGS:`, `SHARED:`, needs edges) | seat → coordinator lint | header lines in the brief text | nova-sprint `docs/SPEC-CARD-CONTRACT.md` (SOURCED, whole file): shared-path rule, gate-host rule, SIBLINGS header | a sibling path without a covering header refused at admission; shared non-ledger path refused (SOURCED from the spec) | admission refusals (SOURCED, spec) |
| M12 | Config rows (nova-config rows, sprint settings) | seat → tools | key/value rows, JSON (REPORTED) | nova-config CLI (UNVERIFIED at this SHA) | drift between row text and consumer expectation; no shared definition | config drift (REPORTED class) |
| M13 | Subscription / consumer receipts (route, model, outcome, cost) | coordinator → ledger | table-like rows | schema repo's own model: `tables/cards/Card.schema` `Consumer` table with kind/who/route/model/started/ended/outcome/cost fields (SOURCED, schema `docs/CARDS.md`) | cost/outcome rows kept as prose lose totals; double-count risk | billing disputes (REPORTED class) |
| M14 | Sprint store records (cards, attempts, reviews, landing state) | coordinator internal | store records + dashboard JSON (REPORTED) | nova-sprint store (UNVERIFIED at this SHA) | record shape evolves with no version marker; dashboard reads what the store wrote by convention | store drift (REPORTED class) |
| M15 | Cairn / memory files (front-matter + prose) | friend → future self | Markdown with hand conventions | human/AI reading (SOURCED as practice) | free prose; no machine check wanted (see section 2) | none — human text by design |
| M16 | Roadmap / fixes ledgers (`docs/roadmap.sexp`, `docs/fixes.sexp`) | seat → lander | s-expression ledgers merged entry-by-entry | lander ledger merge; ledgers listed in `docs/SPEC-CARD-CONTRACT.md` (SOURCED) | merge is textual; a malformed entry surfaces at land time | ledger collisions admitted only for lander-merged files (SOURCED, spec) |
| M17 | Typed records already in nova-tools (`internal/typedrec`, `internal/ntable`, `internal/sprinttable`) | tools internal | Go structs + text binding | package listings SOURCED (`internal/typedrec`, `internal/ntable`, `internal/sprinttable` exist at `b8d29662`) | each package hand-rolls its own binding; three partial answers to one question | duplicated parsers (OPINION from listings) |

## 2 Fit

Schema offers two wires from one language (SOURCED, `docs/VERSIONING.md`
"Two wires, two stories"): the positional **packet wire** (`type`), compact
and exact-build only, and the self-describing **table wire** (`table` /
`fixed table`), where identity is the field name, unknown fields are skipped
and counted, and missing fields read as defaults. Nova's fleet is mixed-build
by nature — friends update at different times — so almost everything below
belongs on the **table wire**, not the packet wire. That is this study's
central judgment (OPINION, argued from the versioning story in section 3b).

| Rows | Verdict | Why |
|---|---|---|
| M4 pong, M5 beat, M6 challenge/proof, M7 send receipt | **fixed-form tables** | Small, hot, fixed shape: a `fixed table` is a plain struct of known sizeof with no arena machinery (SOURCED, `docs/SPEC-TABLES.md` §2.2). A pong is a nonce plus three counters; at fleet rates the win is not bytes but that a malformed pong is refused instead of read as silence. |
| M1 card brief frame, M2 result, M3 bus envelope, M9 judgment, M10 landing verdict | **variable-form tables** | Cards, results and judgments grow fields over time and must cross builds; the table wire's skip-and-default rule is exactly the mixed-fleet semantics needed. The prose body rides as an opaque `*string` field (the pattern schema's own `Card` uses: `body *string`, SOURCED `docs/CARDS.md`), so the frame is checked and the prose is unconstrained. |
| M11 header extras, M12 config rows, M13 receipts, M14 store records, M16 ledgers | **variable-form tables, later** | Real gains (typed receipts end billing-by-prose), but none of these caused tonight's failures; they queue behind the message layer. M16's s-expression ledgers already have a working lander merge (SOURCED, spec) — schema there is optional, not urgent. |
| M15 cairn/memory prose, brief prose bodies, report bodies | **stays human text** | The value is the prose. Schema's job is the envelope around it, never the prose itself. A typed frame carrying an unchecked body is the correct split; constraining the body would be the wrong tool. |
| M8 refusals | **fixed-form table, rendered to text** | A refusal is small and fixed in shape (name, reason, remedy, bound). Typed at the source, it can be rendered as JSON, prose, or a dashboard cell without ever being truncated mid-object. |

**Where schema is the wrong tool (OPINION):** (1) cairn/memory prose (M15) —
no parser should grade a journal; (2) the roadmap sexp ledgers (M16) while the
lander merge works — replacing them buys churn, not reliability; (3) anything
read only by the build that wrote it in the same process (M14 internals) —
Go structs already give that, and a wire format adds cost without a second
reader. Size/rate (ESTIMATED): pongs and beats are tens of bytes as text and
stay tens of bytes as fixed tables — the gain is refusal quality, not size.
Cards and results are kilobytes dominated by prose; the typed frame adds a
small constant and removes a parser, so it wins on reliability per byte.

## 3 Design

Syntax below follows the real language as opened: `fixed table` vs `table`
(SOURCED, `docs/SPEC-TABLES.md` §2.2), bounded `string(N)`, counted arrays
`[..N]T`, unbounded `[]T`, optional `?T`, byte-buffer pointers `*string`,
bounds `| min = …, max = …`, defaults `= v`, and `was = "old"` rename aliases
(SOURCED, `docs/CARDS.md` `Card` declaration, which this section mirrors).

### (a) The five highest-ranked message types

Ranked by tonight's failure cost (OPINION): bus envelope first, then result,
card frame, pong, refusal.

```schema
package nova.bus

enum Kind { report, ack, request, blocker, judgment, pong }

fixed table Envelope
{
    v       int32  | min = 1, max = 1
    kind    Kind
    id      string(64)
    re      string(64)
    from    string(32)
    to      string(32)
    subject string(200)
}
```

```schema
package nova.card

enum Verdict { land, hold, fail }

table Result
{
    card    string(64)
    head    string(40)
    branch  string(128)
    verdict Verdict
    gate    string(32)
    body    *string
}
```

```schema
package nova.card

enum Tier { flash, pro, heavy, frontier }

table CardFrame
{
    id       string(64)
    repo     string(96)
    base     string(96)
    stream   string(32)
    tier     Tier
    test     string(192)
    done     *string
    body     *string
    deadline int32 | min = 0, max = 100000
}
```

```schema
package nova.bus

fixed table Pong
{
    nonce   int64
    queue   int32 | min = 0, max = 100000
    working int32 | min = 0, max = 10000
    width   int32 | min = 0, max = 1024
}
```

```schema
package nova.bus

fixed table Refusal
{
    name    string(48)
    reason  string(240)
    remedy  string(240)
    bound   string(64)
}
```

### (b) Versioning and compatibility

The table wire's promise (SOURCED, `docs/VERSIONING.md` §15–§120, the ten
numbered promises): identity is the hash of the field **name**; fields may be
added, removed or reordered freely; a rename that must keep its data declares
`was = "old"`; an old reader **skips and counts** what it cannot name, a new
reader **defaults** what an old writer never wrote, and every difference lands
in a read report — none is fatal. Two ids exist and no third: the *protocol
id* (packet wire only, compared before peers talk) and the *build version*
(cooked/blocked forms, matched exactly or refused). A schema **text form**
keyed by names carries no version and is the bridge between majors.
Consequences for a mixed fleet (OPINION): friends adopt independently; the
envelope's explicit `v` field is still worth carrying so a *coordinator* can
count old-wire speakers during cutover (section 5), even though the wire
itself tolerates the mix. The one sharp edge to respect: a retired field name
must never be reused for a new meaning (the silent class, refused by a
committed baseline; the rename-reuse hole is schema's open #441, SOURCED).

### (c) Validation at both ends

Sender checks before writing, receiver checks before acting; the generated
reader already refuses out-of-bound scalars and oversize extents against the
declaration (SOURCED, §2.2 on clamped bounds and int32 extent caps). House-style
refusals (format `NAME REFUSED: reason; run: help` per the brief):

- `ENVELOPE REFUSED: kind missing or unknown; run: nova-bus send --help` — trips when `kind` is absent (tonight's one empty kind, REPORTED).
- `RESULT REFUSED: head is not 40 lowercase hex; run: nova-card result --help` — trips on a hand-typed short SHA.
- `CARD REFUSED: body over 16384 bytes; run: nova-sprint brief --help` — the oversize bound, refused at the sender instead of freezing the drain (bound REPORTED from the brief).
- `PONG REFUSED: queue over 100000; run: nova-friend pong --help` — a corrupt counter refused, not read as a fleet size.

### (d) Go codegen into nova-tools and nova-sprint

One `.schema` home: a small shared directory (e.g. `nova-bus` schemas under
nova-tools, which both repos already depend on for the wire) is the single
definition; `bin/schema generate --lang go` (SOURCED, schema `README.md`
install section) emits the codecs; generated files are checked in with their
generated-file headers, regenerated only by an explicit `make schemas` target,
and CI fails if regeneration produces a diff. nova-sprint consumes the same
generated Go package version it pins for nova-tools, so one definition serves
both repos and neither hand-edits generated code (OPINION as plan, mechanics
SOURCED).

### (e) Friends in other harnesses

No AI should hand-write the wire. A `nova-bus send` (and siblings) that takes
flags or a small JSON spec, builds the table, validates it, and only then
writes is the whole answer — the pattern the brief itself demands. Its
`--help` must teach a cold AI: one worked example per kind, the exact refusal
names, and the remedy line in every refusal. It fails closed: an invalid flag
combination is a `Refusal` (M8), printed as one line of text by default and as
structured fields under `--json`, never as truncated JSON (OPINION; the
failure mode it replaces is REPORTED).

### (f) Human-readable rendering

Every table has a name-keyed text form (SOURCED: the text form is the
cross-major bridge, `docs/VERSIONING.md` promise 7). The bus log, the
dashboard and a human reading a card all render that text form; the binary
wire is never the reading surface. A `nova-bus show <id>` that prints the
text form makes the typed migration invisible to humans (OPINION as plan).

### (g) Where the version is pinned

The schema compiler version is pinned beside the generated code (one line,
like nova-sprint's existing `NOVA-TOOLS-VERSION` file, SOURCED by listing in
the sprint checkout). A bump is: upgrade the pin, regenerate, run the
conformance corpus, and commit pin + generated code together; the table
wire's name-identity means a compiler bump never silently reinterprets
existing messages (OPINION from SOURCED promises).

## 4 Reliability gain

One by one, against tonight's classes (incidents REPORTED from the brief):

- **Malformed RESULT.md (M2):** removed. The result is a `Result` table; a
  missing verdict or a short head is refused by the generated reader at the
  member, named as a field, never blamed on the worker as a "shape".
- **Truncated JSON refusals (M8):** removed. A refusal is a small fixed table
  rendered whole; there is no hand-assembled JSON object to cut in half.
- **Flag and format drift (M8/tooling):** reduced, not removed. A build CLI
  (3e) removes wire-guessing, but flag drift across dozens of verbs is a
  CLI-convention problem; schema fixes the *payload*, not the flag surface.
  Honest boundary: this class needs the CLI work, not just the wire.
- **Subject-convention parsing (M3/M9):** removed. Kind, re, and verdict move
  from subject strings into envelope fields; subjects become human text again.
- **Oversize-note freeze (M1):** removed. The 16 KB bound is declared and
  refused at the sender with a named refusal; the drain never sees it.
- **Ambiguous kinds (M3):** removed. `Kind` is an enum; the empty kind is
  unrepresentable.
- **Replayed/duplicate messages (M3):** partially addressed. A typed `id`
  field makes dedup checkable, but dedup policy (who remembers what, for how
  long) is coordinator logic, not wire format. The wire enables the fix; it
  is not the fix.

**What it cannot fix (the malformed/wrong boundary):** a worker that writes a
*valid* `Result` with the wrong verdict; a prose body that lies inside a
well-formed envelope; a liveness fault (a dead friend sends no pong, typed or
not — typing only stops a *malformed* pong from masquerading as silence);
an overloaded bus; a gate that is genuinely red. Schema draws the line
exactly at well-formedness: everything malformed becomes a named refusal at
the earliest reader; everything well-formed-but-wrong remains a human and
review problem, and no honest plan should claim otherwise (OPINION).

## 5 Migration plan

Layers, bottom up; each secured before the next starts. TLA+ notes follow
Rowan's standing rule that every state machine gets a model beside it.

- **Layer 0 — pin and prove (missing from the brief's list; added, OPINION).**
  Pin the schema compiler, vendor the Go oracle + conformance corpus as the
  acceptance gate, and write one golden nova message round-trip. Guarantees:
  the toolchain is reproducible. Rollback: delete the pin. Acceptance: corpus
  green plus the golden test.
- **Layer 1 — envelope + beat, pong, ack (brief's Layer 1).** Guarantees: no
  malformed liveness or envelope message is ever acted on. Oracle: schema Go
  oracle/conformance + a nova golden test per message. TLA+: model the pong /
  session-check exchange (states: challenged, answered, expired) before the
  code changes. Rollback: dual-format read stays on. Acceptance: a fuzzed
  malformed pong is refused 100% at the receiver in test.
- **Layer 2 — card and result (brief's Layer 2).** Guarantees: no card is
  admitted and no result accepted unless its frame validates. TLA+: the card
  lifecycle (waiting → ready → working → review → merging → landed, the
  columns in schema's own `Column` enum, SOURCED `docs/CARDS.md`) modelled
  with the typed transitions. Rollback: text briefs still parse. Acceptance:
  the two historical RESULT shapes both refuse or read — never fault-blame.
- **Layer 3 — judgments and reports (brief's Layer 3).** Guarantees: a verdict
  is a field, never a subject convention. TLA+: the hook handshake
  (challenge/proof) modelled beside it. Acceptance: subject text can be
  reworded freely with zero verdict loss in test.
- **Layer 4 — receipts, config, store records (added).** Only after Layers
  1–3 are boring. Acceptance per row in section 2.

**Dual-format period and cutover:** readers accept both wires from Layer 1
day one; writers switch per message type at each layer's acceptance. The
cutover rule: when the coordinator's count of old-wire speakers (the envelope
`v` field, 3b) reaches zero for a full adopt cycle, the old parser for that
type is deleted in the same layer's final card — not before, and not "later".
Friends learn the new wire because the build CLI (3e) is the only documented
way to send; the old hand-written path keeps working during dual-format, so
nobody is stranded mid-adopt (OPINION as plan). v1.2.x is out of scope per
the brief: this is v1.3 and later.

## 6 Risks and cost

- **Coupling to an evolving compiler.** The table wire is pre-release until
  schema 3.0.0 and its files "may change in a minor version, announced first"
  (SOURCED, `docs/VERSIONING.md` promise preamble). Mitigation: pin the
  compiler version (3g), commit a baseline before data leaves the building
  (promise 2), and treat the text form as the escape bridge (promise 7).
- **A compiler bug** produces a wrong codec everywhere at once — the blast
  radius is the fleet. The conformance corpus + nova golden tests (Layer 0)
  are the containment; without them, do not adopt.
- **Build/CI cost (ESTIMATED):** one regeneration step and one corpus run per
  schema change — minutes, not hours, at nova's scale; the real cost is
  discipline, not compute.
- **Ports needed:** Go first (nova-tools and nova-sprint are Go). The nine
  language ports (SOURCED, schema `README.md`) matter only if a friend's
  harness cannot shell out to the Go CLI — the build-CLI design (3e) makes
  that unlikely.
- **Learning cost:** one page of help per verb (3e); the failure mode to
  avoid is a friend hand-writing wire, which the CLI exists to prevent.
- **Size/speed vs text (ESTIMATED):** fixed tables are comparable to the
  current one-line text at pong scale and far cheaper than JSON; variable
  tables pay a small constant over text for self-description. Nobody should
  adopt this for speed; adopt it for refusals.
- **Debuggability:** the binary wire is not the reading surface — the text
  form is (3f). If a `show` renderer is not shipped with Layer 1, debugging
  gets worse, not better; treat the renderer as part of Layer 1.
- **Cost per layer (ESTIMATED, worker-days):** Layer 0: 2–3; Layer 1: 5–8;
  Layer 2: 8–12; Layer 3: 5–8; Layer 4: open-ended, by row.
- **What would make me advise against (OPINION):** if the fleet cannot run
  the build CLI everywhere a message originates (then hand-written wire
  returns and the gains evaporate), or if Layer 0's corpus cannot be kept
  green in CI (then a compiler bump is an unmeasured risk). Either one is a
  stop, not a delay.

## 7 Ranked R&D experiments

Ranked by reliability gain per unit of effort, best first, cheapest decisive
experiments first (OPINION ordering). Each names the result that ends it.

| Id | Question | Hypothesis | Built (described) | Measurable outcome | Cost (worker-days) | Dead-end result | Unblocks |
|---|---|---|---|---|---|---|---|
| S1 | Does sender-side validation kill the RESULT fault class? | ≥90% of recorded malformed results refuse at the writer | `Result` codec + writer check on replayed fault corpus | share of recorded failures refused at sender | 2 | <50% refused at sender | Layer 2 |
| S2 | Can a cold AI send a valid message from help alone? | ≥9/10 first-try valid sends with the build CLI, vs hand-written baseline | build CLI prototype + help page | count of hand-written wire mistakes, with vs without CLI | 3 | CLI no better than hand-writing | Layer 1, §3e design |
| S3 | Do typed pongs end liveness false-negatives? | malformed-pong-as-silence events drop to zero | `Pong` codec in one member | count of ambiguous liveness events in test | 2 | malformed pongs still read as silence | Layer 1 |
| S4 | Does the mixed-version fleet hold? | old reader skips/counts new fields, never misdecodes | two builds exchanging evolved envelope | misdecode count (must be 0); skip counts in read report | 3 | any silent misdecode | all layers |
| S5 | Is the RESULT.md replacement accepted end-to-end? | both historical shapes refuse-or-read, never fault-blame | `Result` reader shim | fault-blame messages after cutover (target 0) | 3 | workers route around the shim | Layer 2 cutover |
| S6 | What does the wire cost at pong rate? | within 2× text bytes, negligible CPU at fleet rate | fixed-table pong benchmark in test harness | bytes and µs vs text form | 1 | >5× byte or CPU cost | Layer 1 sizing |
| S7 | Does a typed Refusal end truncated-JSON ambiguity? | 100% of refusals parse whole under `--json` | `Refusal` renderer | unparsable refusal count (target 0) | 2 | renderers still hand-assemble JSON | tooling-wide |
| S8 | Can subjects become pure prose? | verdicts survive arbitrary subject rewording | judgment envelope pilot | verdict-loss count under rewording fuzz | 2 | any verdict recovered from subject text | Layer 3 |
| S9 | Does the oversize bound refuse early enough? | oversize cards refuse at sender, drain never stalls | declared bound + sender check | drain-stall reproductions (target 0) | 1 | stall reproduces with typed frame | Layer 2 |
| S10 | Would receipts-as-tables end billing-by-prose? | consumer rows reconcile exactly against the ledger | `Consumer`-style receipt pilot (schema's own shape, SOURCED) | reconciliation mismatches | 4 | mismatches persist from non-wire causes | Layer 4 |
| S11 | Does the text-form renderer keep humans whole? | a reader loses nothing vs today's bus log | `show` text renderer | field-loss diff on recorded messages (target 0) | 2 | any field invisible to humans | Layers 1–3 acceptance |
| S12 | **Overturn test:** is hand-text plus a strict lint enough? | a strict text lint matches typed-wire refusal rates | hardened text parser + lint on the same fault corpus | refusal-rate gap vs S1/S3 (if <5%, the wire is not worth it) | 3 | lint matches the wire — then I withdraw the recommendation | the whole adoption decision |

## 8 Ideas of your own

- idea: Treat the read report (skipped/counted unknowns, SOURCED promise 1)
  as a fleet-health metric on the dashboard; a rising skip count *is* the
  adopt progress bar.
- idea: Give every refusal a stable machine name (3c) and count refusals by
  name; tonight's prose refusals cannot be counted, so the worst friction is
  currently invisible.
- idea: Let the card lifecycle TLA+ model (Layer 2) generate its column enum
  from the same `.schema` file the wire uses, so the model and the messages
  cannot drift apart.
- idea: Keep one deliberately untyped channel — a free "note to humans" bus
  kind — so friends never feel pressure to smuggle prose into typed fields.
- idea: Publish the fault corpus (tonight's malformed messages, anonymised)
  as the permanent acceptance fixture; every future wire change runs against
  the night that motivated it.

## 9 Sources and not done

Sources opened (repo, path, SHA):

- mas-bandwidth/nova-sprint @ `5bc72c47ae6bc4a7a8b51e7cccc57c97c78c554a`
  (base `sprint/v1.3-research` tip, fetched first): `README.md` (read),
  `docs/SPEC-CARD-CONTRACT.md` (read whole), `NOVA-TOOLS-VERSION` (listing),
  `cmd/nova-sprint/land.go` (grep hit only).
- mas-bandwidth/nova-tools @ `b8d2966200302ae6e350961a2c5c1801f6ca40d6`:
  `internal/bus/note.go:140,160,305`, `internal/cardhdr/cardhdr.go:164`,
  `cmd/nova-bus/beat_functional_test.go:84`, package listings for
  `internal/typedrec`, `internal/ntable`, `internal/sprinttable`,
  `internal/presence`.
- mas-bandwidth/schema @ `46f8834db44ded27460000fd4f4174f20bdf6304`:
  `README.md` (read), `docs/SPEC-TABLES.md` §1–§2.3 (read; rest by heading),
  `docs/VERSIONING.md` promises + "Two wires" (read), `docs/CARDS.md`
  declaration section including the verbatim `Card.schema` (read).

Links:
- https://github.com/mas-bandwidth/nova-sprint — the sprint coordinator repo.
- https://github.com/mas-bandwidth/nova-tools — the shared tools repo.
- https://github.com/mas-bandwidth/schema — the schema compiler repo.
- https://github.com/mas-bandwidth/nova — the public nova seed.
- https://www.patreon.com/MasBandwidth/membership — schema support page (from its README).
- https://github.com/mas-bandwidth/schema/blob/main/docs/SPEC-TABLES.md — the tables spec opened here.
- https://github.com/mas-bandwidth/schema/blob/main/docs/VERSIONING.md — the versioning promises cited.
- https://github.com/mas-bandwidth/schema/blob/main/docs/CARDS.md — the cards corpus and `Card.schema`.
- https://github.com/mas-bandwidth/schema/blob/main/docs/USAGE.md — the language usage reference.
- https://github.com/mas-bandwidth/nova-tools/blob/main/internal/bus/note.go — the bus note parser cited.
- https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-CARD-CONTRACT.md — the card contract read whole.

Not done / could not settle:

- `ParseCardResult`/`shapeFault` were not found by grep at nova-tools
  `b8d29662`; M2's parser is therefore UNVERIFIED at this SHA (the brief names
  `cmd/nova-swarm/member.go`, which does not exist in this clone — the swarm
  code lives under `internal/swarm` here). Locating the current result parser
  is the first follow-up.
- `docs/SPEC-TABLES.md` (16,520 lines), `docs/USAGE.md`, `docs/PORTING.md` and
  `docs/SECURITY.md` were read by heading plus targeted sections, not whole;
  claims lean only on the sections actually opened.
- All tonight-incident counts and several failure specifics are REPORTED from
  the brief (no bus log was available), and are labelled as such in place.
- Nothing was built, run or benchmarked, per the card; every size/speed figure
  is ESTIMATED and labelled.
- The union merge of the three sibling studies is a later card, not this one.
