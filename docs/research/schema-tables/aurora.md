# Schema tables for nova: an adoption study (aurora)

Research only; no product code. Bases read: nova-sprint `c3b66834edf9de4cbb94222816b7111de008c721` (branch `sprint/v1.3-research`), nova-tools `b8d2966200302ae6e350961a2c5c1801f6ca40d6`, schema `38af9571b62e81a735909cfa843e7ac257d50649`. Labels: SOURCED = opened path/line or link; ESTIMATED = arithmetic shown; OPINION = judgment. Incidents from the brief that were not opened are marked REPORTED.

## 1 Inventory

| ID | Thing | Sender -> Receiver | Format today | Parser (file:function) | Failure modes | Incident |
|---|---|---|---|---|---|---|
| M1 | Card brief (RESULT/REPO/BASE/PATHS/TEST/DONE-WHEN header + prose) | coordinator -> worker | line-oriented text, 16 KB class limit | nova-tools `internal/cardhdr/cardhdr.go:164` `ParseTest`; sprint `docs/SPEC-CARD-CONTRACT.md` | missing tier line refused; prose drift found by lint lines; oversize note froze a drain | REPORTED (brief); tier/TEST rules SOURCED in `docs/SPEC-CARD-CONTRACT.md` |
| M2 | Worker result (`RESULT.md`: verdict/head lines + body) | worker -> member/coordinator | hand-written text, two historical shapes | sprint `internal/card/card.go:271`; tests `internal/sprint/attempts_test.go:31` | missing lines blamed on worker; two shapes, one parser | SOURCED: `attempts_test.go:31` expects `no result: no RESULT.md shape` |
| M3 | Bus message (from/to/cc/re/kind/subject/body) | any friend -> any friend | header text + free body over Redis streams | nova-tools `internal/bus/address.go:173` `ResolveOne` | empty/ambiguous From; kind free text incl. empty; structure smuggled in subject (`PING <nonce>`, `#202 HOLD ...`) | REPORTED volume (~1,700/night, ~1,300 pongs); header shape SOURCED in `internal/bus/bus_test.go:18` |
| M4 | Pong / liveness reply (`pong <nonce> queue working width`) | friend -> coordinator | positional text tokens | nova-tools `cmd/nova-bus` pong tests | token order is the contract; a shifted token parses as a different fact | SOURCED shape in `closecollision_functional_test.go:38` (pong ids collide when hand-made) |
| M5 | Beat / presence heartbeat | friend -> server | small periodic record | nova-sprint `internal/sprint` presence paths (UNVERIFIED line) | stale beat read as live; clock skew | UNVERIFIED |
| M6 | Hook challenge / proof (`hook bus challenge <hex>`) | server -> friend -> server | hex token in prose command | nova-sprint hook verbs (UNVERIFIED line) | challenge replay; proof bound to nothing typed | REPORTED |
| M7 | Session check request/result | coordinator -> friend | prose + nonce convention | bus layer (M3) | echo of own prompt counted as receipt | REPORTED |
| M8 | Judgment (HOLD/FIX/merge verdicts) | reader/coordinator -> coordinator | prose with ids in subject | human + bus subject scan | verdict not machine-checkable; id mistyped in subject silently unlinks | REPORTED |
| M9 | Landing / merge verdict + head pin | lander -> store | Go structs -> store records; prose report | sprint `internal/sprint` `FinishReq`/`MergeReq` (tests `accepted_bound_test.go:39`) | stale head accepted if generation fence missed | SOURCED test names in `accepted_bound_test.go` |
| M10 | Refusal (lane fault, flag refusal) | tool -> caller | JSON or prose on stderr, sometimes truncated | caller stderr scrape | truncated JSON indistinguishable from half-written refusal; `--json` refused on many verbs; global flags before verb refused | REPORTED |
| M11 | Card store record (column, holder, attempt, consumers, cost) | server -> store/dashboard | Go structs, JSON views | sprint `internal/sprintdash/dashboard.go:22` (`encoding/json`) | field added in one view, missing in another; cost rows dropped on carry | REPORTED cost holes; JSON use SOURCED |
| M12 | Config rows / sprint settings | operator -> tools/server | text rows, `nova-config` | nova-tools `internal/config` (UNVERIFIED line) | partial apply before refusal | REPORTED |
| M13 | Subscription receipts / consumer rows | billing -> store | structured rows | sprint `docs/SPEC-SUBSCRIPTION-RECEIPTS.md` | tier/billing not copied between row shapes | REPORTED; spec file SOURCED (exists at base) |
| M14 | Roadmap / fixes ledgers (`roadmap.sexp`, `fixes.sexp`) | coordinator -> lander | s-expression text, lander-merged | sprint `docs/SPEC-CARD-CONTRACT.md` ledger list | two writers collide unless lander-merged or needs-chained | SOURCED in `SPEC-CARD-CONTRACT.md` |
| M15 | Dashboard JSON / events feed | server -> browser | ad-hoc JSON | `internal/sprintdash/dashboard.go:22` | schema drift invisible until render breaks | SOURCED import; drift class OPINION |
| M16 | Cairn / memory note files | friend -> future self | markdown with header conventions | human/tooling grep | header drift; unverifiable claims | OPINION |
| M17 | Redis keys / stream entries themselves | tools -> Redis | stream field maps | nova-tools bus store | field renamed in one writer only | OPINION |

## 2 Fit

Fixed-form tables (form byte `3`, [SPEC-TABLES.md](https://github.com/mas-bandwidth/schema/blob/main/docs/SPEC-TABLES.md)) suit small, hot, fully bounded records whose cost must not depend on version skew: M4 pong, M5 beat, M6 challenge/proof, and the ack frame inside M3. These are tens of bytes, high rate, and every field boundable today. ESTIMATED: a pong is ~30 bytes of text tokens; a fixed record is an 8-byte layout hash plus values at bound — comparable size, but parsed without token scanning and refused by name on form mismatch.

Variable-form tables (form byte `1`) suit M1 brief, M2 result, M8 judgment, M11 store record, M12 settings, M13 receipts: sparse, evolving, list-bearing, with an unbounded prose body carried as `*string` outside declared bounds — exactly the pattern the existing card declaration uses ([CARDS.md](https://github.com/mas-bandwidth/schema/blob/main/docs/CARDS.md): body is `*string`, collections are lists of bounded elements, policy ceilings live in the facade). The typed envelope checks the frame (ids, tier, head, verdict enum); the prose rides unchecked inside it, which is the point: the frame is verified, the argument is not.

Stays text: M16 cairn notes and the prose bodies of M1/M2/M8. Schema is the wrong tool for M14 ledgers (the lander already merges them entry-by-entry; a binary ledger would break that merge — SOURCED merge rule in `SPEC-CARD-CONTRACT.md`), for M17 Redis key naming (a naming convention, not a payload), and for one-off human chat bodies in M3, where only the envelope should be typed. JSON remains better than the variable wire for M15 dashboard payloads consumed directly by a browser, unless a generated JS reader is adopted; the win there is a shared definition, not bytes.

## 3 Design

### (a) Top five definitions (real syntax, after the card corpus style)

```schema
package nova

enum Verdict { land, hold, fail }

table BusEnvelope
{
    id      string(26)
    from    string(32)
    to      string(32)
    kind    string(16)
    subject string(200)
    re      string(26)
    body    *string
}
```

```schema
package nova

table CardResult
{
    card    string(64)
    verdict Verdict
    head    string(40)
    branch  string(128)
    gate    string(200)
    body    *string
}
```

```schema
package nova

fixed table Pong
{
    nonce   string(32)
    queue   int32 | min = 0, max = 100000
    working int32 | min = 0, max = 100000
    width   int32 | min = 0, max = 100000
}
```

```schema
package nova

table CardBrief
{
    id       string(64)
    sha      string(40)
    tier     string(16)
    repo     string(96)
    test     string(192)
    deadline int32 | min = 0, max = 100000
    body     *string
}
```

```schema
package nova

table Judgment
{
    card    string(64)
    verdict Verdict
    head    string(40)
    finding string(200)
    body    *string
}
```

Bounds mirror the card corpus (`string(64)` ids, `string(40)` heads, `*string` bodies; facade ceilings `MaxBodyBytes = 64<<10`, `MaxListEntries = 512` in [CARDS.md](https://github.com/mas-bandwidth/schema/blob/main/docs/CARDS.md)).

### (b) Versioning and mixed fleet

The table wire identifies fields by name hash; add/remove/reorder is tolerated, renames declare `was`, and every difference lands in a read report, never a crash ([VERSIONING.md](https://github.com/mas-bandwidth/schema/blob/main/docs/VERSIONING.md) promises 1-3). An old reader skips unknown fields and counts them; opt-in retain-unknown keeps their bytes across a rewrite (promise 10, variable class only). The form byte is checked first: a later form is refused by name, not misread (promise 7). During an adopt, old and new members therefore exchange variable-form messages safely in both directions; fixed-form peers pay skew-independent cost and evolve append-only (§2.10 of SPEC-TABLES). The pre-3.0.0 caveat is real: the wire may still change in a minor, announced first — pin accordingly (section 6).

### (c) Validation and named refusals

Sender validates before writing (bounds, enum membership, required fields); receiver validates before acting and trusts nothing the sender checked. House-style refusals, each with its bound: `RESULT REFUSED: verdict missing; run: nova-result help` (required field absent); `BRIEF REFUSED: body over 65536 bytes; run: nova-card help` (facade ceiling); `PONG REFUSED: queue over 100000; run: nova-friend help` (declared `max`); `ENVELOPE REFUSED: unknown form byte 9; run: nova-bus help` (form registry, SPEC-TABLES §3); `CARD REFUSED: tier not a declared variant; run: nova-card help` (enum check, SECURITY.md scope list).

### (d) Go codegen

`.schema` files live in one shared directory owned by the coordinator repo (e.g. `schemas/` in [nova-sprint](https://github.com/mas-bandwidth/nova-sprint)), generated Go is checked in per repo with a `make`-style regenerate-and-diff CI gate (the pattern `make cards-generate` / `make cards-test` already proves in the schema repo). Both repos consume the same definition; neither hand-edits generated code. The Go runtime dependency is [serialize.go](https://github.com/mas-bandwidth/serialize.go), named by the generated module in CARDS.md.

### (e) Build CLI for other harnesses

No AI hand-writes the wire: a `nova-msg build --kind pong --nonce X --queue 0 ...` (flags) or `--spec msg.json` validates against the schema and emits wire bytes or the text rendering; `nova-msg check <file>` validates; failures are the named refusals above on stderr with empty stdout, fixing M10's prose-on-stdout drift. Help is the teaching surface: each verb's `-h` lists fields, bounds and one worked example, so a cold AI builds a valid message from help alone (tested by S2 below).

### (f) Human rendering

Every type has a text form keyed by names (the version bridge in VERSIONING.md promise 7) and a `Render` view (CARDS.md facade): bus log and dashboard show the rendering; humans edit text, tools parse it once at admission via a `Parse` facade and thereafter store wire.

### (g) Version pin

Pin the compiler and the `.schema` baseline together: a `SCHEMA-VERSION` file beside the definitions, the committed baseline the versioning doc requires before data leaves the building, and bumps made as one reviewed change that regenerates, runs the conformance corpus, and only then rolls readers before writers.

## 4 Reliability gain

Removed, one by one: malformed RESULT.md (M2) — `CardResult` refuses a missing verdict/head at the sender, before the member ever prefixes a shape fault; truncated JSON refusals (M10) — refusals become typed records with a length, so a cut-off refusal fails framing instead of parsing as prose; flag/format drift (M10) — the build CLI has one flag set generated from the definition, so `--json`-style guessing disappears for covered verbs; subject-convention parsing (M3/M8) — `re`, `kind`, ids become fields, not substrings; oversize-note freeze (M1) — the body ceiling refuses at the sender with the bound named, not at drain time; ambiguous kinds (M3) — `kind` becomes a bounded string now, an enum when the set closes; replayed/duplicates (M4/M7) — nonce and id are fields a receiver can dedupe on, though dedupe policy stays nova's.

Cannot fix: a wrong verdict in valid form, a lying prose body, a liveness fault (a friend who never sends), an overloaded bus, a red gate. The boundary is exact: schema proves a message is *well-formed and in-bounds*; it says nothing about whether it is *true*. That stays with readers, gates and review.

## 5 Migration plan

Layer 1 — envelope, beat, pong, ack (M3 frame, M4, M5): guarantees every liveness fact is typed and form-checked; oracle = schema Go oracle + conformance corpus + a nova golden test per message; TLA+ note: extend the existing card/session models (`tla/` at base, e.g. `CardMachine.tla`) with a pong/session-check module before coding; rollback = dual-read, old text accepted; acceptance = one full night with zero hand-parsed pongs.

Layer 2 — card and result (M1, M2), reusing the existing card declaration rather than inventing one: guarantees no shape faults and sender-side ceiling refusals; oracle as above plus brief `Parse` parity tests against today's admitted set; TLA+ note: card lifecycle already modelled — add the result-shape invariant; rollback = text brief still admitted; acceptance = shape-fault count zero across a stream.

Layer 3 — judgments and reports (M8, M9): guarantees verdicts are enums with pinned heads; acceptance = no subject-scanned verdicts remain.

Missing layers I would add: Layer 0 — pin, baseline and build CLI skeleton (before any wire); Layer 4 — config/receipts (M12, M13); Layer 5 — delete old parsers. Dual-format period: readers accept both wires, writers emit the new one; cutover per message type when its golden tests and one adopt cycle pass; old parser deleted only in Layer 5, after two releases with zero old-wire reads logged. This is v1.3+ only; v1.2.x is untouched.

## 6 Risks and cost

Coupling: nova would depend on a compiler whose table wire is pre-3.0.0 (VERSIONING.md promise section) — mitigate by pinning, vendoring the generated code (generated output is the adopter's, per the schema README licence note), and keeping the text form as an exit. A compiler bug is fleet-wide by construction (SECURITY.md: one bad read path lands in nine languages at once); the conformance corpus and wire fuzzer are the counterweight, and nova must run them, not trust them. Ports needed: Go first and only at the start; JS only if the dashboard adopts a generated reader. Learning cost is the build CLI's help surface, measured by S2. Binary debuggability is paid for with the text rendering (3f); without it, do not adopt. ESTIMATED cost: Layer 0 ~3 worker-days, Layer 1 ~5, Layer 2 ~8, Layer 3 ~5, Layer 4 ~5, Layer 5 ~2 (OPINION estimates). Advise against if: the corpus cannot run in nova CI, or the team will not keep the text rendering — then JSON with a schema registry is the cheaper honest step. Background reading: [USAGE.md](https://github.com/mas-bandwidth/schema/blob/main/docs/USAGE.md), [PORTING.md](https://github.com/mas-bandwidth/schema/blob/main/docs/PORTING.md), [SECURITY.md](https://github.com/mas-bandwidth/schema/blob/main/docs/SECURITY.md), and the [schema repository](https://github.com/mas-bandwidth/schema) itself.

## 7 Ranked R&D experiments

Ranked by reliability gain per unit effort, best first (OPINION ranking).

### S1 Result-shape refusal at the sender

Question: does a typed `CardResult` kill the shape-fault class? Hypothesis: 100% of recorded shape faults refuse at write time. Build: definition + facade only. Measurable outcome: share of replayed historical faults refused at sender (target 100%). Cost: 1 worker-day. Dead end if any historical fault validates. Unblocks Layer 2.

### S2 Cold AI builds from help alone

Question: can a cold AI produce a valid pong/envelope using only build-CLI help? Hypothesis: >=90% first-try valid. Measurable outcome: valid-first-try rate with vs without the CLI. Cost: 1 worker-day. Dead end below 50%. Unblocks 3e fleet-wide.

### S3 Mixed-version fleet exchange

Question: do old/new readers exchange variable-form messages with reports, never crashes (VERSIONING promise 1)? Measurable outcome: mismatch classes observed vs report counters. Cost: 2 worker-days. Dead end: any misdecode. Unblocks the adopt plan.

### S4 Pong fixed-form vs text

Question: bytes/microseconds of fixed-form pong vs positional text. Measurable outcome: bytes and parse cost on recorded pong shapes. Cost: 1 worker-day. Dead end: slower or larger. Unblocks Layer 1.

### S5 Brief Parse parity

Question: does `Parse` on the existing card declaration admit exactly today's admitted briefs? Measurable outcome: parity diffs over the card corpus. Cost: 2 worker-days. Dead end: any silent admission change. Unblocks Layer 2.

### S6 Truncated-refusal framing

Question: are cut-off typed refusals always detected? Measurable outcome: detection rate over every truncation point of a sample refusal. Cost: 0.5 worker-days. Dead end: any silent accept. Unblocks M10 fix.

### S7 Envelope subject-convention removal

Question: what breaks when ids/kind leave the subject? Measurable outcome: count of subject-scanning call sites found. Cost: 1 worker-day. Dead end: a consumer that cannot migrate. Unblocks Layer 3.

### S8 Corpus in nova CI

Question: cost of running the schema conformance corpus per nova change. Measurable outcome: CI minutes added. Cost: 1 worker-day. Dead end: >10 minutes. Unblocks every layer's oracle.

### S9 Text-rendering round trip

Question: is the text form a lossless human surface for the top five? Measurable outcome: round-trip diffs over fixtures. Cost: 1 worker-day. Dead end: any field lost. Unblocks 3f and the version bridge.

### S10 Overturn test: JSON registry instead

Question: would JSON plus a checked registry deliver >=80% of the gain at <30% of the cost? Measurable outcome: failure classes covered per worker-day, both designs scored on section 4's list. Cost: 2 worker-days. Dead end for schema adoption: if yes, recommend JSON. Unblocks an honest no.

### S11 Retain-unknown across rollback

Question: does opt-in retention survive a downgrade-save-upgrade cycle (promise 10)? Measurable outcome: fields retained vs `retain_lost` counts. Cost: 2 worker-days. Dead end: silent loss. Unblocks adopt safety.

### S12 Oversize-body sender refusal

Question: does the facade ceiling move the freeze from drain to sender? Measurable outcome: refusal point in a staged oversize brief. Cost: 0.5 worker-days. Dead end: any path that still freezes. Unblocks M1 fix.

## 8 Ideas of your own

idea: generate the dashboard's JSON from the same definitions so M15 drift becomes a compile error, not a render bug.

idea: log old-wire reads per message type during dual-format; the deletion date in Layer 5 should come from that counter, not a calendar.

idea: keep `kind` a bounded string until the fleet's kind set is observed closed for a full stream; enums punish discovery.

## 9 Sources and not done

Sources (repo, path, SHA): nova-sprint `c3b66834edf9de4cbb94222816b7111de008c721` — `README.md`, `docs/SPEC-CARD-CONTRACT.md`, `internal/card/card.go:271`, `internal/sprint/attempts_test.go:31`, `internal/sprintdash/dashboard.go:22`, `tla/` listing. nova-tools `b8d2966200302ae6e350961a2c5c1801f6ca40d6` — `internal/bus/address.go:173`, `internal/bus/bus_test.go:18`, `internal/cardhdr/cardhdr.go:164`, `cmd/nova-bus/closecollision_functional_test.go:38`. schema `38af9571b62e81a735909cfa843e7ac257d50649` — `README.md`, `docs/CARDS.md` (Card declaration verbatim), `docs/SPEC-TABLES.md:36` (three wires), `docs/VERSIONING.md:15` (promises), `docs/SECURITY.md`, plus headings of `docs/USAGE.md` and `docs/PORTING.md`. Links are listed inline in sections 2, 3 and 6 (8+ unique). The compiler home is [mas-bandwidth/schema](https://github.com/mas-bandwidth/schema); the adopters are [nova-sprint](https://github.com/mas-bandwidth/nova-sprint) and [nova-tools](https://github.com/mas-bandwidth/nova-tools).

Not done: SPEC-TABLES, USAGE and PORTING were read by outline and targeted sections, not whole; several parser lines are marked UNVERIFIED in section 1 where the function was not opened; tonight's incident counts are REPORTED from the brief, not recounted; nothing was built, run or benchmarked, per the card. Next: read the three documents whole, open every UNVERIFIED parser, then run S1 and S10 before any adoption decision. The later union merge is a separate card, not done here.
