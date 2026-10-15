# Schema tables for nova: the union of three studies

Card `schema-adoption-union.w1` (attempt 1, gen 1, epoch 21), worked by Rowan (friend.rowan), 2026-10-15.
Research merge only: no product code was written, built, run or benchmarked. Owner and reader: Glenn
Fiedler (he/him). The three inputs are by frontier friends (all she/her). Labels are kept as the inputs
gave them: SOURCED (a path:line or link opened), REPORTED (from the research brief, not reopened),
ESTIMATED (arithmetic shown), OPINION (judgment). My own additions are marked `U-new` with the reason.
Where I opened a source to rule on a contradiction the ruling says what I opened and at which SHA.

**Revised 2026-10-15** by card `schema-adoption-union-fixes.w1` (gen 1, epoch 21, Rowan): the five fixes a
later independent reader asked for, the owner's rulings of 2026-10-15, and the two tracers' findings are
folded into sections 3, 4, 6 and 7. Section 8 lists every change, each with its cause. Where a sentence
below is marked **(rev)**, it was changed by that revision.

## 1 Method and inputs

**The inputs, at the base this card was staged on** (`origin/sprint/v1.3-research` = `1ae18735`):

| File id | Commit (git log -1 on the file) | Date | Author as the file states it | Inventory rows | Experiments | Sources named in its section 9 |
|---|---|---|---|---|---|---|
| `emma` | `b99ac5e96ebefef6bd5e127b87e4c83ab0de39ff` | 2026-10-14 02:20 -0400 | Emma Antigravity, card `schema-adoption-research-emma.w3` | 16 (M1-M16) | 12 (S1-S12) | 12 URLs; a citation index of 80 distinct files across schema `38af9571`, nova-sprint `c3b66834`, nova-tools `b8d29662` and `83d0e15b` (v1.2.23) |
| `aurora` | `761aad092a0c1aef674202a8815bf56c1c02cd0c` | 2026-10-14 00:55 -0400 | Aurora, card `schema-adoption-research-aurora` | 17 (M1-M17) | 12 (S1-S12) | 20 paths (10 nova-sprint at `c3b66834`, 3 nova-tools marked UNVERIFIED, 7 schema docs at `38af9571`); 8+ links |
| `stella` | `8213e8e10f53e403f6bb1713c9c0366e434c2ee2` | 2026-10-14 08:30 -0400 | **Worked by Aurora** under the stella card while Stella was down (the file's author note and commit subject both say so) | 17 (M1-M17) | 12 (S1-S12) | 12 paths (4 nova-sprint at `5bc72c47`, 8 nova-tools at `b8d29662` incl. 4 package listings, 4 schema docs at `46f8834d`); 11 links |

**Independence caveat (U-new, from the author note in `stella` §header):** `aurora` and `stella` were
written by the same friend, Aurora, on two cards about eight hours apart; the stella file says it did not
read `aurora.md`. Where only those two agree, the agreement is one author's view reached twice, not two
views. I count it as two proposers in the ledger (the brief says proposers are file ids) but I do not treat
an aurora+stella agreement against emma as a majority. Stella's own position (she/her) is not in this
corpus; the coordinator asked that each friend's position be preserved, so the `stella` id below means
"the stella file", and its author is Aurora.

**What I opened to check claims (read-only clones outside the checkout):**

- `mas-bandwidth/schema` at `4d9941fbc7fd420b560739e48256d5b5e2892cdc` (main, 2026-10-14 12:35), and the
  inputs' SHAs `38af9571b62e81a735909cfa843e7ac257d50649` and `46f8834db44ded27460000fd4f4174f20bdf6304`
  through `git show`. Between `38af9571` and `4d9941fb` only `ROADMAP.md`, `docs/FIXED-FORM-ALGORITHM.md`
  and two `tables/cards/cs` files changed among the files the inputs cite.
- `mas-bandwidth/nova-tools` at `b8d2966200302ae6e350961a2c5c1801f6ca40d6` (main, the SHA all three read)
  **and** at `4f7697d143bcdde768b333166b6432d0a7125acc` (`origin/sprint/v1.3-tools`), plus `83d0e15b` for
  emma's v1.2.23 citations.
- `mas-bandwidth/nova-sprint` at `1ae18735` (this checkout).

**How I merged.** I read the three files whole, section by section, and listed every distinct idea per
file before writing the ledger. A ledger row is one idea; two inputs' ideas share a row only when they are
the same idea (same claim, same object); close-but-different ideas get separate rows with a
cross-reference. Inventory items are merged separately into section 5 (M rows); experiments into section 7
(R rows), each also carried as one ledger row so the ledger stays the full index. Contradictions on facts
were settled by opening the source (section 3); contradictions on judgment are surfaced for Glenn with each
position and its evidence, and my lean is labelled OPINION.

**Counts.** The ledger holds 183 rows (U1-U183). By proposer: rows naming `emma` 115, `aurora` 77,
`stella` 81; 8 rows are `U-new`. I count an input's distinct ideas as the ledger rows that name it, so the
largest count in any one input is emma's 115, which the 183 rows cover. A stricter count that also takes emma's 16 inventory rows and each of the 34
tables she declares as separate ideas gives 165, still under 183. Section 5 holds 28 merged inventory rows
(M1-M28); section 6 holds 30 cards (C1-C30; C27-C30 added by the revision, **(rev)**); section 7 holds 24
experiments (R1-R24) merged from 36.

## 2 Idea ledger

Topic is the input section the idea came from (1 Inventory, 2 Fit, 3 Design, 4 Reliability, 5 Migration,
6 Risks, 7 Experiments, 8 Own ideas). Proposers are file ids. Evidence is the inputs' label; where an input
gave none, "unlabelled (OPINION)". "See Ux" is a cross-reference to a close but different idea.

| Id | Idea (one line) | Topic | Proposers | Evidence | Pointer |
|---|---|---|---|---|---|
| U1 | Nova moves a large amount of structured data as hand-parsed text; inventory it before designing | 1 | emma, aurora, stella | SOURCED | emma §1; aurora §1; stella §1 |
| U2 | Five ad-hoc representations today: markdown header docs, one-line refusal/finding parsers, ad-hoc JSON, Redis field maps, tokenized JSON manifests + Lua | 1 | emma | SOURCED | emma §1 list 1-5 |
| U3 | The same parsers exist as diverged copies across repos (pkg/ vs internal/; `typedrec/result.go` 13 lines in nova-sprint vs 823 in nova-tools main) | 1 | emma | SOURCED | emma §1 box, M5 |
| U4 | Three typed-record packages (typedrec, ntable, sprinttable) are three partial answers to one question | 1 | stella | OPINION | stella M17 |
| U5 | Typed precedent exists: `typedrec.ParseTableCheck` and the `TableRefusal` taxonomy; the refusal design should reuse it, not invent a parallel one | 1, 3 | aurora | SOURCED | aurora M10, §3(c) |
| U6 | schema already ships `tables/cards` (Card.schema + Parse, Render, ToJSON, FromJSON, Upgrade, TaskHash) and nova imports none of it | 1 | emma | SOURCED | emma §1 box |
| U7 | `PacketOf` scrapes `REPO:`/`BASE:` from the brief when the server omits them; `omitempty` JSON silently zero-fills on version skew | 1 | emma | SOURCED | emma M2 |
| U8 | `REPORT.md` is scraped by `reportKey`; a missing `Verdict:` leaves it unread (2026-10-05: 8 cards unread 2h); LAND without Head flips to failed | 1 | emma | SOURCED | emma M4 |
| U9 | Rework is judged addressed by a keyword scan (`unaddressed`), false positives and negatives both possible | 1, 8 | emma | SOURCED | emma M4, idea 2 |
| U10 | `sprintwire /verbs` tunnels CLI argv and returns raw stdout that callers scrape | 1 | emma | SOURCED | emma M10 |
| U11 | `sprint.Beat` mixes hot scalars with an unbounded `live_seen` map; Pacing stays nil because FRIEND-BEAT scraping never plumbed it | 1 | emma | SOURCED | emma M11 |
| U12 | Unversioned local `presence.json`: a field or time-format change drops the outstanding nonce and forces a full re-prove | 1 | emma | SOURCED | emma M12 |
| U13 | `pkg/ntable/manifest_validate.go` is 851 lines of hand JSON-token validation for properties schema gives natively | 1 | emma | SOURCED | emma M13 |
| U14 | `FinishedAs` infers verdicts from legacy-vs-current `Line` transitions because log lines carry no version | 1 | emma | SOURCED | emma M14 |
| U15 | `config.Row.Fields` is an untyped string map; `DisallowUnknownFields` makes an older binary refuse a newer `--file` | 1 | emma | SOURCED | emma M15 |
| U16 | Four overlapping hook/proof representations; a malformed stamp fails closed; 2026-10-11/12 the seat unhooked and ~100 pushes replayed | 1 | emma | SOURCED | emma M9 |
| U17 | A malformed pong is read as silence, i.e. as death: a liveness false negative | 1, 4 | stella | REPORTED (class) | stella M4, §4 |
| U18 | Send receipt is `SEND OK id=` plus a prose `NOTE push=down/stale` line; a refused send and a stale push look alike | 1 | stella | SOURCED | stella M7 |
| U19 | Pong token order is the contract; a shifted token parses as a different fact | 1 | aurora | REPORTED | aurora M4 |
| U20 | Session check: an echo of one's own prompt was counted as a receipt | 1 | aurora | REPORTED | aurora M7 |
| U21 | Landing: a stale head is accepted if the generation fence is missed | 1 | aurora | SOURCED (test names) | aurora M9 |
| U22 | Config: partial apply before refusal | 1 | aurora | REPORTED | aurora M12 |
| U23 | Card header extras (`GATE-NEEDS`, `SIBLINGS`, `SHARED`) are structured lines the admission lint parses | 1 | stella | SOURCED | stella M11 |
| U24 | Redis key and stream field names drift when one writer renames | 1 | aurora | OPINION | aurora M17 |
| U25 | Lint findings travel as prose `LINT DRIFT` lines and a regex `FindingPattern` | 1 | emma | SOURCED | emma M7 |
| U26 | Mixed-build fleet ⇒ the self-describing variable table wire (skip and count unknown, default missing) is the right default wire | 2 | emma, aurora, stella | SOURCED | emma §2.1; aurora §2, §3(b); stella §2 |
| U27 | The packet wire (`type`) is exact-build only and wrong for nova's mixed fleet | 2 | emma, stella | SOURCED | emma §2.1 table; stella §2 |
| U28 | Small, hot, fully bounded frames (pong, beat, challenge/proof, ack/receipt) fit `fixed table` | 2 | emma, aurora, stella | SOURCED / ESTIMATED | emma §2.2 M9, M11; aurora §2; stella §2 table |
| U29 | A fixed table reads backward only: an older reader refuses a newer writer (`layout_newer`); readers must deploy before writers | 2 | emma | SOURCED | emma §2.1 WARNING, §3.2 |
| U30 | Fixed tables evolve append-only (see U29: consistent, but aurora draws no deployment-order rule from it) | 2 | aurora | SOURCED | aurora §3(b) |
| U31 | A refusal is small and fixed-shape: type it as a fixed table and render it to text or JSON | 2 | emma, stella | OPINION | emma §2.2 M6; stella §2 M8 row |
| U32 | Lint findings as a fixed `FindingWire` (file, line, check, remedy) | 2 | emma | OPINION | emma §2.2 M7 |
| U33 | A fixed `CardResultWire` summary beside the variable result | 2 | emma | OPINION | emma §2.2 M5 |
| U34 | Bus envelope as a variable table with the prose body as `*string` | 2, 3 | emma, aurora | SOURCED | emma §2.2 M8, §3.1 Block 3; aurora §2, §3(a) |
| U35 | Bus envelope as a fixed table with an explicit `v` field and no body field (contradicts the same file's §2, which puts M3 in variable form) | 3 | stella | OPINION | stella §3(a) vs §2 table |
| U36 | Typed frame, unchecked prose: the frame is verified, the argument is not | 2 | emma, aurora, stella | SOURCED | emma §2.2 lead; aurora §2; stella §2 |
| U37 | Cairn and memory prose stays human text; no parser should grade a journal | 2 | aurora, stella | OPINION | aurora §2; stella §2 "wrong tool" (1) |
| U38 | Cairn entry metadata as a variable `CairnEntry` (session, id, stamp, publish policy, prose hash) with the prose untouched | 2, 3 | emma | OPINION | emma §2.2 M16, §3.1 Block 6 (see U37) |
| U39 | Schema is the wrong tool for the roadmap/fixes s-expression ledgers (lander merge; byte-offset splicing keeps comments) | 2 | emma, aurora, stella | SOURCED / OPINION | emma §2.2 M16; aurora §2; stella §2 (2) |
| U40 | Schema is the wrong tool for Redis key naming (a convention, not a payload) | 2 | aurora | OPINION | aurora §2 |
| U41 | Schema is the wrong tool for data read only by the process that wrote it (Go structs suffice) | 2 | stella | OPINION | stella §2 (3) |
| U42 | Dashboard keeps JSON unless a generated JS reader is adopted; the win there is a shared definition | 2 | aurora | OPINION | aurora §2 (see U146) |
| U43 | The gain is refusal quality, not bytes or speed; nobody should adopt it for speed | 2, 6 | aurora, stella | ESTIMATED | aurora §2; stella §2, §6 |
| U44 | Split the heartbeat: a fixed `BeatPulse` on the wire, a variable `StoredBeat` in Redis | 2, 3 | emma | OPINION | emma §2.2 M11, §3.1 Block 4 |
| U45 | Receipts, config and store records are real gains but queue behind the message layer | 2, 5 | aurora, stella | OPINION | aurora §5 Layer 4; stella §2 table row 3 |
| U46 | Config store as a variable table with retain-unknown so an older binary edits a newer file without loss | 2, 3 | emma | SOURCED / OPINION | emma §2.2 M15, §3.1 Block 6 |
| U47 | Table batch manifest as a variable `BatchManifest`/`BatchReceipt`, replacing the 851-line validator | 2, 3 | emma | OPINION | emma §2.2 M13, §3.1 Block 5 |
| U48 | `sprintwire` as typed `WorkerStepReq`/`WorkerStepResp` instead of argv tunnelling | 2, 3 | emma | OPINION | emma §2.2 M10, §3.1 Block 4 |
| U49 | Aurora's top five: `BusEnvelope`, `CardResult`, `Pong` (fixed), `CardBrief`, `Judgment` | 3 | aurora | OPINION (syntax SOURCED) | aurora §3(a) |
| U50 | Stella-file top five: `Envelope` (fixed), `Result`, `CardFrame`, `Pong` (fixed), `Refusal` (fixed) | 3 | stella | OPINION (syntax SOURCED) | stella §3(a) |
| U51 | Emma's six blocks: Card+JobPacket; WorkResult, CardResultWire, TableRefusalWire, FindingWire; PongFrame, BusEnvelope, BusReceipt, HookLeaseStamp, FriendPushProof; BeatPulse, PresenceRecord, WorkerStep; BatchManifest, SprintNote; ConfigStoreFile, CairnEntry | 3 | emma | SOURCED (syntax) / OPINION | emma §3.1 Blocks 1-6 |
| U52 | Reuse the upstream `tables/cards` Card declaration for the brief instead of a new one | 3, 5 | emma, aurora | SOURCED | emma §3.1 Block 1, §5.1 L2; aurora §5 Layer 2 |
| U53 | Extend Card with `rework_fix` and `rework_fix_hash`, carry `task_hash` in the job packet, echo both in the result: a cryptographic rework proof | 3, 8 | emma | SOURCED / OPINION | emma §3.1 Block 1, idea 2 |
| U54 | A nova-owned `CardFrame` (id, repo, base, stream, tier enum, test, done, body, deadline) instead of the upstream Card | 3 | stella | OPINION | stella §3(a) (see U52) |
| U55 | `JobPacket`: the staged job as a variable table wrapping `*Card` plus attempt, gen, epoch, lane, route, base_sha, carry | 3 | emma | OPINION | emma §3.1 Block 1 |
| U56 | Verdict enum `{ land, hold, fail }` | 3 | aurora, stella | OPINION | aurora §3(a); stella §3(a) |
| U57 | One `WorkResult` for RESULT.md and REPORT.md with `WorkVerdict {ok, not_done, nothing, broken, hold, fail}`, `RunEndKind`, `GateRoute` enums and cost/usage fields | 3 | emma | OPINION | emma §3.1 Block 2 (see U56) |
| U58 | Head SHA as `string(40)` hex | 3 | aurora, stella | OPINION | aurora §3(a); stella §3(a), §3(c) |
| U59 | Head SHA as `bytes(20)` raw digest | 3 | emma | OPINION | emma §3.1 Block 2, §3.3 refusal 4 (see U58) |
| U60 | `kind` as an enum now (the empty kind becomes unrepresentable) | 3, 4 | emma, stella | OPINION | emma §3.1 Block 3 `BusKind`; stella §3(a) `Kind`, §4 |
| U61 | `kind` stays a bounded string until the fleet's kind set is observed closed for a full stream; enums punish discovery | 3, 8 | aurora | OPINION | aurora §3(a), §8 idea 3 (see U60) |
| U62 | Envelope carries typed `card_ref`, `head_ref`, `authority` and a bounded recipient list `[..16]` | 3 | emma | OPINION | emma §3.1 Block 3 |
| U63 | Envelope carries an explicit version field `v` so the coordinator can count old-wire speakers during cutover | 3, 5 | stella | OPINION | stella §3(b), §5 |
| U64 | A fixed `BusReceipt` with stage `queued -> claimed -> pushed -> acted` replaces the receipt hash | 3, 4 | emma | OPINION | emma §3.1 Block 3, §4.1 row 7 |
| U65 | Pong frame: a nonce plus bounded queue, working, width counters (aurora string nonce; stella int64 nonce, tighter bounds; emma adds friend name and send time) | 3 | emma, aurora, stella | OPINION | emma `PongFrame`; aurora `Pong`; stella `Pong` |
| U66 | A single 136-byte `HookLeaseStamp` with int64 ms times and a `FriendPushProof` replace four hook/proof shapes | 3 | emma | OPINION | emma §3.1 Block 3, S9 |
| U67 | A fixed `PresenceRecord` for the friend's local state file | 3 | emma | OPINION | emma §3.1 Block 4 |
| U68 | A variable `SprintNote` (26 fields) and `SprintLogLine` carry notes, judgments and log lines | 3 | emma | OPINION | emma §3.1 Block 5 |
| U69 | A variable-table list of bounded strings needs a one-field element table (the compiler refuses `[]string(N)`) | 3 | emma | SOURCED | emma §3.1 bullets |
| U70 | Validate at both ends: the sender before writing, the receiver before acting, trusting nothing the sender checked | 3 | emma, aurora, stella | SOURCED | emma §3.3; aurora §3(c); stella §3(c) |
| U71 | House-style named refusals, each naming the bound that tripped and the help to run | 3 | emma, aurora, stella | OPINION | emma §3.3 (9 refusals); aurora §3(c) (5); stella §3(c) (4) |
| U72 | Sender-side checks as the cards facade does them: UTF-8, NUL-freedom, byte bounds, enum ordinals, `refuseLossy` | 3 | emma | SOURCED | emma §3.3 |
| U73 | The `.schema` home is a `schemas/` directory in nova-sprint, owned by the coordinator repo | 3 | aurora | OPINION | aurora §3(d) |
| U74 | The `.schema` home is a small shared directory under nova-tools, "which both repos already depend on for the wire" | 3 | stella | OPINION (premise false at `1ae18735`, true at nova-sprint `v1.3/comms` `5dea8434`: section 3, D2 (rev)) | stella §3(d) |
| U75 | The `.schema` home is `mas-bandwidth/schema/tables/<unit>/`; schema owns the generated Go and nova imports it through go.mod | 3 | emma | OPINION | emma §3.4 items 1-2 |
| U76 | Generated Go is checked in and a regenerate-and-diff CI gate refuses drift; nobody hand-edits it | 3 | emma, aurora, stella | SOURCED (mechanics) / OPINION | emma §3.4 item 3; aurora §3(d); stella §3(d) |
| U77 | nova CI checks that go.mod pins an approved schema commit and that no hand-rolled duplicate parser is reintroduced | 3 | emma | OPINION | emma §3.4 item 3 |
| U78 | No AI hand-writes the wire: a build CLI takes flags or a small JSON spec, validates, and only then writes | 3 | emma, aurora, stella | OPINION | emma §3.5; aurora §3(e); stella §3(e) |
| U79 | Each verb's help teaches a cold AI: fields, bounds, enum words, refusal names, one worked example | 3 | emma, aurora, stella | OPINION | emma §3.5 item 2; aurora §3(e); stella §3(e) |
| U80 | One generic builder `nova-msg build/check` for every kind | 3 | aurora | OPINION | aurora §3(e) (see U81) |
| U81 | Per-domain builders: `nova-bus send`, `nova-work result`, `nova-card build` | 3 | emma, stella | OPINION | emma §3.5; stella §3(e) |
| U82 | Builder contract: exit 0/1/2, writes nothing on refusal, one refusal line on stderr; `--from-md` transcodes a hand-written RESULT.md into RESULT.md + RESULT.bin | 3 | emma | OPINION | emma §3.5 items 1, 3 |
| U83 | Under `--json` a refusal is structured fields; by default one line of text; never truncated JSON | 3 | stella, emma | OPINION | stella §3(e); emma §4.1 row 3 |
| U84 | The name-keyed text form is the human reading surface and the cross-major bridge; a `show` verb prints it | 3 | aurora, stella | SOURCED (promise 7) / OPINION | aurora §3(f); stella §3(f) |
| U85 | Bus log prints a deterministic `oneline` header then the prose; `--json` uses generated `ToJSON()` | 3 | emma | OPINION | emma §3.6 item 1 |
| U86 | Markdown stays on disk beside binary sidecars (`JOB.card`, `REPORT.bin`); humans and LLMs read Markdown, tools read the sidecar | 3, 6 | emma | OPINION | emma §3.6 item 3, §6 row 7 |
| U87 | Parse human text once at admission through a `Parse` facade; store wire thereafter | 3 | aurora | OPINION | aurora §3(f) |
| U88 | Pin the compiler beside the definitions in a one-line file (`SCHEMA-VERSION`, like `NOVA-TOOLS-VERSION`) | 3 | aurora, stella | OPINION | aurora §3(g); stella §3(g) |
| U89 | Pin the schema module by SHA in go.mod; wire history in `tables.baseline` (variable) and `schema.lock` (fixed) | 3 | emma | SOURCED / OPINION | emma §3.7 item 1 (see U88) |
| U90 | A bump is one reviewed change: regenerate, run the conformance corpus, then roll readers before writers | 3 | aurora, emma | OPINION | aurora §3(g); emma §3.7 item 2 |
| U91 | Commit a baseline before data leaves the building; a retired name reused is the silent hole, schema #441 | 3, 6 | stella, aurora | SOURCED | stella §3(b); aurora §3(g) |
| U92 | Advance the `schema.lock` floor only after no live binary runs an older build stamp | 3 | emma | OPINION | emma §3.7 step 3 |
| U93 | The table wire is pre-release until 3.0.0 and may change in a minor, announced first: pin accordingly | 3, 6 | aurora, stella, emma | SOURCED | aurora §3(b), §6; stella §6; emma §6 row 1 |
| U94 | The text form is the exit: if schema is abandoned, render to text and leave | 3, 6 | aurora, stella | SOURCED (promise 7) | aurora §6; stella §6 |
| U95 | Malformed RESULT.md is removed: a typed result refuses missing verdict or bad head at the sender | 4 | emma, aurora, stella | REPORTED / SOURCED | emma §4.1 row 1; aurora §4; stella §4 |
| U96 | Truncated JSON refusals are removed: a typed refusal is framed (form-1 trailer, form-3 fixed length) and cannot be half-parsed | 4 | emma, aurora, stella | SOURCED / OPINION | emma §4.1 row 2; aurora §4; stella §4 |
| U97 | Flag and format drift is only reduced: it is a CLI-convention problem and needs CLI work, not just a wire | 4 | stella | OPINION | stella §4 (see U98) |
| U98 | Flag drift is fixed by hoisting global flags before verb dispatch and binding every verb's output and refusal to a table's `ToJSON()` | 4 | emma, aurora | OPINION | emma §4.1 row 3; aurora §4 |
| U99 | Subject-convention parsing is removed: kind, re, card, verdict become fields; subjects become prose again | 4 | emma, aurora, stella | OPINION | emma §4.1 row 4; aurora §4; stella §4 |
| U100 | The oversize-note freeze is removed by refusing at the sender with the bound named | 4 | emma, aurora, stella | REPORTED | emma §4.1 row 5; aurora §4; stella §4 |
| U101 | Drain isolation: a stored record that fails `Load` moves its one card to held with a typed receipt instead of aborting the tick | 4 | emma | OPINION | emma §4.1 row 5 |
| U102 | Ambiguous kinds are removed by enum (or a bounded string) | 4 | emma, aurora, stella | OPINION | emma §4.1 row 6; aurora §4; stella §4 |
| U103 | Duplicates: a typed id makes dedupe checkable; dedupe policy stays coordinator logic | 4 | aurora, stella | OPINION | aurora §4; stella §4 |
| U104 | `BusReceipt.stage` plus envelope id deduplicates replays atomically | 4 | emma | OPINION | emma §4.1 row 7 (see U103) |
| U105 | Move pongs off `bus2:log` into the beat/push channel: removes >75% of bus volume (1,300 of ~1,700) | 4 | emma | REPORTED / ESTIMATED | emma §4.1 row 7, S5 |
| U106 | Cannot fix: valid-but-wrong verdicts, lying prose, liveness faults, an overloaded bus, a red gate; the line is well-formedness | 4 | emma, aurora, stella | OPINION | emma §4.2; aurora §4; stella §4 |
| U107 | Catching wrong messages needs SHA verification against origin, diffcheck, gates and independent readers | 4 | emma | OPINION | emma §4.2 item 6 |
| U108 | Layer 0 (pin and prove: pin, baseline, golden round trip, corpus, build-CLI skeleton) before any wire | 5 | aurora, stella | OPINION | aurora §5; stella §5 Layer 0 |
| U109 | Layer 1: envelope, beat, pong, ack | 5 | emma, aurora, stella | OPINION (brief's layer) | emma §5.1; aurora §5; stella §5 |
| U110 | Layer 1 also takes hook challenge/proof and the friend presence record | 5 | emma | OPINION | emma §5.1 Layer 1 |
| U111 | Layer 2: card and result | 5 | emma, aurora, stella | OPINION (brief's layer) | emma §5.1; aurora §5; stella §5 |
| U112 | Layer 3: judgments and reports | 5 | emma, aurora, stella | OPINION (brief's layer) | emma §5.1; aurora §5; stella §5 |
| U113 | Layer 3 also takes refusals, lint findings and sprint notes/log lines, with universal `--json` | 5 | emma | OPINION | emma §5.1 Layer 3 |
| U114 | Layer 4: receipts, config, store records, only after Layers 1-3 are boring | 5 | aurora, stella | OPINION | aurora §5; stella §5 Layer 4 |
| U115 | Layer 4: store manifests, the worker-step RPC, config and cairn | 5 | emma | OPINION | emma §5.1 Layer 4 (see U114) |
| U116 | Layer 5: delete the old parsers, after two releases with zero old-wire reads | 5 | aurora | OPINION | aurora §5 |
| U117 | TLA+ for Layer 1: a pong/session-check module (challenged, answered, expired) before the code | 5 | aurora, stella | OPINION | aurora §5 L1; stella §5 L1 |
| U118 | TLA+ for Layer 1: extend `Hook.tla`, `SeatHook.tla`, `Bus2Receipts.tla` with `TypedFrameBeforeTransition` | 5 | emma | OPINION | emma §5.1 L1 |
| U119 | TLA+ for Layer 2: a result-shape invariant on the card model; card lifecycle with typed transitions | 5 | aurora, stella | OPINION | aurora §5 L2; stella §5 L2 |
| U120 | TLA+ for Layer 2: `CardContract.tla` `Finish` requires `shaped` and `task_hash = TaskHash(c)` | 5 | emma | OPINION | emma §5.1 L2 |
| U121 | TLA+ for Layer 3: model the hook handshake (challenge/proof) | 5 | stella | OPINION | stella §5 L3 |
| U122 | TLA+ for Layer 3: a `JudgmentLoop` model with `BrokenVerdictHasTypedFinding` | 5 | emma | OPINION | emma §5.1 L3 |
| U123 | TLA+ for adopt: `Adopt.tla` gains `ReadersBeforeFixedWriters` | 5 | emma | OPINION | emma §5.1 L4 |
| U124 | Dual format: readers accept both wires, writers switch per message type at each layer's acceptance | 5 | emma, aurora, stella | OPINION | emma §5.2 item 1; aurora §5; stella §5 |
| U125 | Two phases per layer: A (dual-read, legacy-write) fleet-wide first, then B (dual-read, schema-write) | 5 | emma | OPINION | emma §5.2 item 1 |
| U126 | Rollback by switches (`NOVA_BUS_WIRE=legacy` and siblings) and detection by magic bytes or Content-Type | 5 | emma | OPINION | emma §5.1 rollback column |
| U127 | Deletion is decided by a counter, never a calendar: old-wire reads logged per message type | 5, 8 | aurora, stella, emma | OPINION | aurora §5, §8 idea 2; stella §5 (`v` count zero for an adopt cycle); emma §5.2 item 3 (`legacy_fallback_reads == 0` for an epoch) |
| U128 | Old parser deletion also waits for every legacy Redis TTL (7 d) to expire or an epoch clear | 5 | emma | OPINION | emma §5.2 item 3 |
| U129 | `nova-sprint adopt` already runs store -> server -> dashboard -> friends, which satisfies readers-before-writers for fixed tables | 5 | emma | SOURCED | emma §5.2 item 2 |
| U130 | The build CLI is the only documented way to send; the hand-written path keeps working during dual format so nobody is stranded | 5 | stella | OPINION | stella §5 |
| U131 | Layer 1 acceptance: a full night with zero hand-parsed pongs / a fuzzed malformed pong refused 100% / zero pongs on `bus2:log` over a 2 h soak | 5 | aurora, stella, emma | OPINION | aurora §5 L1; stella §5 L1; emma §5.1 L1 |
| U132 | v1.3 and later only; v1.2.x untouched | 5 | emma, aurora, stella | SOURCED (brief) | emma §5; aurora §5; stella §5 |
| U133 | Coupling to a pre-3.0 compiler: pin, check in or vendor the generated code, keep the text form as an exit | 6 | emma, aurora, stella | SOURCED | emma §6 row 1; aurora §6; stella §6 |
| U134 | A compiler bug lands fleet-wide at once; nova must run the conformance corpus, Go oracle and wire fuzz itself, not trust them | 6 | emma, aurora, stella | SOURCED | emma §6 row 2; aurora §6; stella §6 |
| U135 | Go only at first; JS only if the dashboard adopts a generated reader | 6 | emma, aurora, stella | SOURCED | emma §6 row 4; aurora §6; stella §6 |
| U136 | Keep fixed tables off int128, compressed floats and nested union guards until those Go cells are green | 6 | emma | SOURCED | emma §6 row 4 |
| U137 | Debuggability: the renderer ships with Layer 1 or debugging gets worse, not better | 6 | stella, aurora, emma | OPINION | stella §6; aurora §6; emma §6 row 7 |
| U138 | Cost: aurora 28 worker-days over L0-L5; stella 20-31 plus an open-ended L4; emma 18 over L1-L4 | 6 | aurora, stella, emma | ESTIMATED / OPINION | aurora §6; stella §6; emma §6.1 |
| U139 | Advise against if the corpus cannot run in nova CI, or the text rendering is not kept; then JSON with a checked registry is the cheaper honest step | 6 | aurora | OPINION | aurora §6 |
| U140 | Advise against if the build CLI cannot run everywhere a message originates, or the corpus cannot stay green in CI | 6 | stella | OPINION | stella §6 |
| U141 | Advise against fixed tables on untrusted boundaries until Go's fixed-table oracle and hostile-input cells are green; against any design where AIs emit binary | 6 | emma | SOURCED / OPINION | emma §6.2 items 1-2 |
| U142 | Learning cost for friends is near zero: they touch Markdown and one-line CLIs, never `.schema` grammar | 6 | emma, stella | OPINION | emma §6 row 5; stella §6 |
| U143 | R3: cold AI builds a valid message from help alone | 7 | emma, aurora, stella | ESTIMATED | emma S1; aurora S2; stella S2 |
| U144 | R2: a typed result refuses the recorded fault class at the sender | 7 | emma, aurora, stella | ESTIMATED | emma S2; aurora S1; stella S1 |
| U145 | R6: mixed-version fleet exchange (variable both ways; fixed old->new only) | 7 | emma, aurora, stella | ESTIMATED | emma S3; aurora S3; stella S4 |
| U146 | Generate the dashboard JSON from the same definitions so drift is a compile error | 8, 3 | aurora, emma | OPINION | aurora §8 idea 1; emma §3.6 item 2 |
| U147 | The read report's skip counts are a fleet-health metric: a rising skip count is the adopt progress bar | 8 | stella | OPINION | stella §8 |
| U148 | Give every refusal a stable machine name and count refusals by name; prose refusals are uncountable today | 8 | stella | OPINION | stella §8 |
| U149 | Generate the card-lifecycle TLA+ model's column enum from the same `.schema` file as the wire | 8 | stella | OPINION | stella §8 |
| U150 | Keep one deliberately untyped "note to humans" bus kind so nobody smuggles prose into typed fields | 8 | stella | OPINION | stella §8 |
| U151 | Publish the fault corpus as the permanent acceptance fixture every future wire change runs against | 8 | stella | OPINION | stella §8 |
| U152 | Trusted-boundary transcoding: the host process outside the sandbox wall is the schema gateway; nothing downstream re-parses text | 8 | emma | OPINION | emma idea 1 |
| U153 | Generate a `oneline` formatter and parser from each fixed table so log tokens and wire cannot drift | 8 | emma | OPINION | emma idea 3 |
| U154 | Redis Lua reads epoch/revision guards at fixed offsets with `string.unpack`, no Lua decoder needed | 8 | emma | SOURCED / OPINION | emma idea 4 |
| U155 | One CI evolution gate (`schema check --baseline` + `schema.lock`) across all three repos | 8 | emma | SOURCED / OPINION | emma idea 5 (see U77) |
| U156 | `U-new` — **The v1.3 nova-tools code is not on nova-tools main.** All three inputs read nova-tools `b8d29662` (main, 2026-10-08); the v1.3 line is `origin/sprint/v1.3-tools` (`4f7697d1`, 8,847 commits ahead) with the `pkg/` layout, `cmd/nova-bus`, `cmd/nova-friend`, `hook/`, and the TLA modules emma cites. Reason: this settles the aurora/stella path contradictions and retargets every nova-tools card | 1 | U-new | SOURCED (git rev-list, ls-tree) | section 3, F1 |
| U157 | `U-new` — **nova-sprint does not import nova-tools**; its `go.mod` requires neither, and `NOVA-TOOLS-VERSION` says the tree was seeded from nova-tools v1.2.5. `pkg/bus`, `pkg/typedrec`, `pkg/friend` exist as copies in both repos. Reason: one `.schema` home must feed two copied trees, which shapes the home decision. **(rev)** True at `1ae18735` only: at `v1.3/comms` `5dea8434` nova-sprint's go.mod requires nova-tools and the copies of `pkg/cardhdr`, `pkg/typedrec` and `pkg/friend/machine.go` are gone (section 3, F3) | 1, 3 | U-new | SOURCED | section 3, F3 |
| U158 | `U-new` — **The generated `tables/cards` package is not importable by module path**: it is generated into `build/` as a local module `cards` with checkout-relative replace directives, and `tables/` is the compiler's test-corpus directory. Reason: emma's import plan (U75) needs a schema-side change or a vendoring decision. **(rev)** Moot for adopters: both tracers showed generated table Go imports only the standard library, so nothing is imported from schema (section 3, tracer lessons) | 3 | U-new | SOURCED | section 3, F5 |
| U159 | `U-new` — **The bus kind is already validated today**: `pkg/bus` `Send` calls `CheckKinds`; the kinds are report, ack, status, request, blocker, and `""` means status. No input's enum carries `status`. Reason: an enum that refuses `""` breaks today's status messages | 3, 4 | U-new | SOURCED | section 3, F6 |
| U160 | `U-new` — **The brief size refusal already exists in nova at 16 KiB** (`pkg/cardlimits.MaxBriefBytes`, refused at `nova-sprint add`), below the cards facade's 64 KiB. Reason: nova's own bound, not the facade's, is the one to declare; the reported freeze was a note, not a brief | 3, 4 | U-new | SOURCED | section 3, F7 |
| U161 | `U-new` — **Readers-before-writers holds for friend-written frames only.** Adopt upgrades the server before friends, so a server-written fixed frame (judgment push, job packet) reaches old friend readers first and is refused. Reason: U129 is half true; fixed tables are safe only in the friend-to-server direction | 5 | U-new | OPINION (from SOURCED adopt order and §21) | section 3, D1 |
| U162 | `U-new` — **First experiment is a fault census**: classify the recorded incidents as malformed (schema can refuse) or wrong (it cannot) before spending on any layer. Reason: every input's gain claims rest on REPORTED incidents nobody recounted | 7 | U-new | OPINION | section 7, R1 |
| U163 | `U-new` — **Schema's own docs disagree on Go retain-unknown**: VERSIONING promise 10 says only the C++ reference carries it, while `ROADMAP.md` marks Go ✅ and `internal/codegen/gotable/retain.go` emits `<Name>LoadRetain`/`SaveRetain`. Reason: emma's and aurora's retain plans depend on Go having it; the evidence says it does, the promise page is stale | 3, 6 | U-new | SOURCED | section 3, F9 |
| U164 | R4: overturn A, strict JSON with a registry or DisallowUnknownFields plus the same builder | 7 | emma, aurora | ESTIMATED | emma S4, §6.2 item 3; aurora S10 |
| U165 | R5: overturn B, a strict text lint on today's format | 7 | stella | ESTIMATED | stella S12 |
| U166 | R7: brief parse parity through the upstream Card parse | 7 | emma, aurora | ESTIMATED | emma S7; aurora S5 |
| U167 | R8: refusals whole under --json on every verb | 7 | emma, stella | ESTIMATED | emma S6; stella S7 |
| U168 | R9: a cut-off typed refusal is always detected | 7 | aurora | ESTIMATED | aurora S6 |
| U169 | R10: text rendering round trip loses nothing | 7 | aurora, stella | ESTIMATED | aurora S9; stella S11 |
| U170 | R11: census of subject-scanning call sites | 7 | aurora | ESTIMATED | aurora S7 |
| U171 | R12: verdicts survive subject rewording once typed | 7 | stella, emma | ESTIMATED | stella S8; emma S10 |
| U172 | R13: typed pongs end malformed-pong-as-silence | 7 | stella | ESTIMATED | stella S3 |
| U173 | R14: cost of the conformance corpus in nova CI | 7 | aurora | ESTIMATED | aurora S8 |
| U174 | R15: the oversize bound refuses before the drain | 7 | aurora, stella | ESTIMATED | aurora S12; stella S9 |
| U175 | R16: pong wire bytes and CPU vs text | 7 | aurora, stella | ESTIMATED | aurora S4; stella S6 |
| U176 | R17: retain-unknown survives a downgrade-save-upgrade | 7 | aurora | ESTIMATED | aurora S11 |
| U177 | R18: pong offload from the bus stream | 7 | emma | ESTIMATED | emma S5 |
| U178 | R19: rework proof by TaskHash and rework_fix_hash | 7 | emma | ESTIMATED | emma S8 |
| U179 | R20: unify the hook stamps onto one record | 7 | emma | ESTIMATED | emma S9 |
| U180 | R21: typed results accepted end to end, never fault-blame | 7 | stella | ESTIMATED | stella S5 |
| U181 | R22: config file and notes keep unknown fields across versions | 7 | emma | ESTIMATED | emma S12 |
| U182 | R23: consumer receipts as tables end billing by prose | 7 | stella | ESTIMATED | stella S10 |
| U183 | R24: the compiled manifest replaces the 851-line validator | 7 | emma | ESTIMATED | emma S11 |

Experiments are carried one row per merged experiment: R1 is U162, R2 U144, R3 U143, R6 U145, and the
other twenty are U164-U183 in R order. Section 7 holds their full design.

## 3 Agreements and disagreements

### Where all three agree

1. **Adopt for refusals, not speed.** The gain is that a malformed message is refused by name at the
   earliest reader instead of being misread (U36, U43, U95-U102). Shared evidence: the variable-table
   promises in schema `docs/VERSIONING.md` (identity is the name, skip and count, never misdecode) and the
   `tables/cards` precedent of a typed frame around a `*string` body.
2. **Variable tables are the default wire for a mixed fleet; the packet wire is wrong for nova** (U26,
   U27). Shared evidence: VERSIONING.md "Two wires, two stories".
3. **The prose stays prose.** Brief, report and note bodies ride as `*string`; cairn prose and the
   roadmap/fixes s-expression ledgers stay text (U36, U37, U39).
4. **No AI hand-writes the wire.** A build CLI with teaching help is part of the design, and a cold-AI test
   decides it (U78, U79, R3 **(rev)**: R3 is the cold-AI experiment; R1 is the fault census).
5. **Layers 1-3 as the brief names them, bottom up, with dual-read during cutover**, and an extra layer
   beyond them (U109, U111, U112, U124).
6. **What schema cannot fix**: wrong-but-valid content, lying prose, liveness, overload, red gates (U106).
7. **Every one proposes an overturn experiment** that could withdraw the recommendation (R4, R5).
8. **Pin the compiler, check in the generated code, run the corpus in nova CI** (U76, U93, U133, U134).

### Where they disagree (positions, evidence, my ruling)

| # | Question | emma | aurora | stella file | Evidence I opened | Ruling |
|---|---|---|---|---|---|---|
| D1 | Fixed tables for Layer 1 frames, and the fleet rule | Fixed for pong, beat, receipt, hook, presence; fixed is backward-only, so readers deploy first; adopt order gives that (U29, U129) | Fixed for pong, beat, challenge; envelope variable; "append-only" (U30) | Fixed for envelope, pong, beat, receipt, refusal; "friends adopt independently" (U35, §3b) | schema `docs/SPEC-TABLES.md` §21 (at `38af9571` and `4d9941fb`): "An older reader given a newer file refuses it by name"; §21.6 "Readers ship before writers". `ROADMAP.md` at `4d9941fb`: Go column ❌ for "Shared byte oracle and round-trip conformance" and "Hostile-input checks and negative controls"; the fixed-tables section still says the work is "not yet on main" | **Evidence settles the fact for emma**: fixed tables are backward-only, so the stella file's "adopt independently" does not hold for its fixed envelope. **(rev 2)** Emma also raised the gate herself: her §6.2 item 1 advises against fixed tables on untrusted boundaries until Go's fixed-table oracle and hostile-input cells are green (U141); at `4d9941fb` they are still ❌ (F11). One further limit emma did not state: readers-first holds only friend-to-server (U161). ~~Lean: variable first; promote pong/beat to fixed after C1 and R3.~~ **(rev) SETTLED by Glenn, 2026-10-15: adopt both forms in nova-tools and nova-sprint and dogfood both.** Fixed tables for records whose reader is always the same version or newer (server-owned store records); variable tables for messages an older reader may meet (every friend-exchanged frame, including pong and beat). The tracers bound it further: fixed only for small, bounded, server-owned records (done-record tracer: an unbounded record does not fit, 95 entries max under the 65,536-byte body). The mixed-version matrix that tests the rule is R6, not R3 (R3 is the cold-AI test). |
| D2 | Where `.schema` and generated Go live | schema repo `tables/<unit>`, imported via go.mod (U75) | nova-sprint `schemas/` (U73) | nova-tools, "which both repos already depend on" (U74) | nova-sprint `go.mod` requires no nova-tools module; `NOVA-TOOLS-VERSION`: seeded from v1.2.5; schema `tables/AGENTS.md` lists `tables/` as compiler corpora; `docs/CARDS.md` "Import and module setup": local module `cards`, replace directives ~~The stella premise is false (no dependency).~~ **(rev) Corrected per Glenn, 2026-10-15:** at `1ae18735` (this card's first base) the premise was false, but nova-sprint's go.mod at `v1.3/comms` `5dea8434` (`fbb51563` "delete pkg/units, import nova-tools/pkg/units") requires `github.com/mas-bandwidth/nova-tools v1.2.26-0.20261015045441-f9ef81e091a2`, and at `v1.3/comms` `dfae414b` nova-sprint imports `nova-tools/pkg/cardhdr`, `pkg/typedrec`, `hook`, `pkg/bus` and others, its own copies deleted. **The stella premise holds on the v1.3 line.** Emma's import path is unnecessary (U158): both tracers showed generated table Go imports only the standard library, so there is no schema runtime module to import and option (b) and card C3 as written are not needed. The dependency worry is settled; **the one open question is where the one definition lives.** Lean (OPINION, Glenn decides): a type read by both repos is defined once in nova-tools `schemas/<area>/`, generated into nova-tools `pkg/<area>wire/`, and nova-sprint imports it through the require it already has (no copy, no vendoring, no byte-compare sync); a record only one repo reads lives beside its one consumer (the done-record tracer's `schemas/store/` + `internal/sprint/store/donewire/` in nova-sprint). |
| D3 | Scope of v1.3 | Four layers incl. manifest validator, worker RPC, config, cairn, hook unification; 18 wd (U115, U138) | L0-L5 with config/receipts as L4 and a delete layer; 28 wd | L0-L4, L4 "open-ended, only after 1-3 are boring" | Costs are ESTIMATED by all three; nothing to open settles it | **UNSETTLED; Glenn decides.** Lean (OPINION): Layers 0-3 and the delete layer in v1.3; Layer 4 items each come back as their own decision after Layer 3 is used for real. |
| D4 | Card definition | Reuse upstream Card + rework fields (U52, U53) | Reuse upstream Card (U52) | New `CardFrame` (U54) | `tables/cards/Card.schema` at `4d9941fb` (79 lines) already has id, sha, tier enum, repo, base, stream, paths, needs, test, done, body, column, head, branch, extra, consumers | Evidence favours reuse: the upstream Card is a superset of `CardFrame` and has a tested facade. Lean: reuse, nova's 16 KiB bound in nova's facade (U160). |
| D5 | Verdict vocabulary | `WorkVerdict {ok, not_done, nothing, broken, hold, fail}` (U57) | `{land, hold, fail}` (U56) | `{land, hold, fail}` (U56) | nova-sprint `pkg/friend/outbox.go` reads `Verdict: LAND/HOLD/FAIL`; `pkg/typedrec/cardresult.go` has its own verdict set | **UNSETTLED.** The two sets describe different things: emma's is the worker's run outcome, aurora's is the judgment. Lean: both, as two fields (run outcome on the result, verdict on the judgment). Glenn decides. **(rev)** Section 4.3 now keeps emma's `WorkVerdict` by name with its six values, beside her `RunEndKind` (all seven values) and `GateRoute`; the first draft had substituted a five-value `RunEnd` for `WorkVerdict` and lost it. |
| D6 | `kind` enum now or bounded string first | enum (U60) | bounded string until observed closed (U61) | enum (U60) | `pkg/bus/bus.go:93-110` at `1ae18735`: five kinds, validated on Send, `""` is status | The set is already closed and enforced in code, so an enum loses nothing **if it includes `status`**; neither enum does (U159). Ruling: enum, seeded from `bus.Kinds`, `status` included, new kinds appended at the end. |
| D7 | Head as `string(40)` or `bytes(20)` | `bytes(20)` | `string(40)` | `string(40)` | Card.schema `head string(40)`, `sha string(40)` | Lean `string(40)` with a hex check in the facade: matches the upstream Card and the text form stays readable. OPINION. |
| D8 | Flag drift: fixed by schema or only reduced | fixed by hoisting flags + `ToJSON()` (U98) | removed for covered verbs (U98) | only reduced; a CLI problem (U97) | none needed | The stella file is right that the fix is CLI work; emma names that work. Both stand; carded as C23. |
| D9 | Pongs on the bus at all | move off `bus2:log` (U105) | typed but on the bus | typed but on the bus | volume is REPORTED | Unsettled until R1 counts them; R18 measures it. |
| D10 | Cutover counter | `legacy_fallback_reads == 0` for an epoch + TTLs expired | old-wire reads, two releases | `v` field count zero for an adopt cycle | none | Same idea (U127); take emma's two conditions, aurora's per-type counter, no `v` field needed because the form byte already tells old from new. OPINION. **(rev)** Glenn, 2026-10-15: returning friends simply update (a paste at worst), so there is no offline-compatibility policy and no long dual-emit window: readers update before writers in a short coordinated window, and the per-form read counter (one per type, legacy vs table) is the evidence the window closed. The epoch-long and two-release waits are dropped; stored legacy records (Redis TTLs) are still read until rewritten. |

### Factual claims one input made that another contradicts, or that I checked

| # | Claim | Who | Contradicted by / checked against | Ruling |
|---|---|---|---|---|
| F1 | nova-tools packages are under `internal/` (bus `note.go:140 ParseNote`, `cardhdr.go:164 ParseTest`) | stella SOURCED; emma for main | aurora calls these UNVERIFIED and withdraws `internal/cardhdr/cardhdr.go:164` as nonexistent | **stella right at `b8d29662`**: `internal/bus/note.go:140,160,305` and `internal/cardhdr/cardhdr.go:164` exist. Aurora checked the nova-sprint checkout, not nova-tools. But main is not v1.3 (U156): on `sprint/v1.3-tools` the layout is `pkg/`. |
| F2 | `internal/bus/address.go:173 ResolveOne` | aurora (UNVERIFIED) | not found at `b8d29662` nor at `4f7697d1` | **Refuted**: no `func ResolveOne` in either tree. |
| F3 | Both repos already depend on nova-tools for the wire | stella §3(d) | nova-sprint `go.mod` at `1ae18735`; **(rev)** and at `v1.3/comms` `5dea8434` / `dfae414b` | ~~Refuted (U157).~~ **(rev)** Refuted at `1ae18735` only; **true on `v1.3/comms`**: go.mod requires nova-tools `v1.2.26-0.20261015045441-f9ef81e091a2`, and nova-sprint imports `pkg/cardhdr` (126 import sites), `pkg/typedrec` (20), `hook` (32), `pkg/bus` (1), `pkg/cardlimits` (3) from nova-tools. |
| F4 | `ParseCardResult`/`shapeFault` not found in nova-tools | stella | aurora, emma cite nova-sprint `pkg/typedrec/cardresult.go:54,153` | **Both true**: absent from nova-tools main; present at nova-sprint `pkg/typedrec/cardresult.go:54` (`CardResultKeys` :37, `shapeFault` :153) and at nova-tools `sprint/v1.3-tools` `pkg/typedrec/cardresult.go:54`. |
| F5 | Nova can import `github.com/mas-bandwidth/schema/tables/...` via go.mod | emma §3.4 | schema `go.mod` (module `github.com/mas-bandwidth/schema/v2`), `docs/CARDS.md:100-115` | **Not today** (U158). |
| F6 | `kind` is an unvalidated string; one empty kind tonight | emma M8, §4.1; stella §3(c) | nova-sprint `pkg/bus/bus.go:93-110, 368` | **Refuted at `1ae18735`**: Send refuses a kind outside the five; `""` is status by design. Whether tonight's empty kind was a status message is UNVERIFIED (no bus log). |
| F7 | The 16 KB brief cap; oversize refused late | stella (REPORTED), aurora (facade 64 KiB) | nova-sprint `pkg/cardlimits/cardlimits.go:11` `MaxBriefBytes = 16 << 10`, refused by `nova-sprint add` | 16 KiB is SOURCED and already refused at admission (U160). The freeze in the brief was a *note*; its path is UNVERIFIED. |
| F8 | `MaxListEntries` | aurora 512; emma 256 at `cards.go:35` | schema `tables/cards/cards.go:26` | **aurora right: 512.** Emma's line and value are wrong. |
| F9 | Go has retain-unknown (`LoadRetain`/`SaveRetain`) | emma (SOURCED SPEC-TABLES), aurora S11 assumes it | VERSIONING promise 10: "The C++ reference carries it; the eight ports do not" | **Go has it** by `ROADMAP.md` (variable tables, retain-unknown, go ✅) and `internal/codegen/gotable/retain.go:135-137`; the promise text is stale (U163, card C4). |
| F10 | `ParseTableRefusal` classifies by `strings.Contains` on English prose | emma M6 | nova-sprint `pkg/typedrec/tablerefusal.go:66` | **Refuted**: an exact `switch` on reason tokens (`"STALE"`, `"DRIFT"`, ...), zero `strings.Contains`. The truncated `lane-fault refusal={` (REPORTED) is a different path. |
| F11 | Go fixed-table oracle and hostile-input cells are ❌ | emma §6.2 at `38af9571` | `ROADMAP.md` at `4d9941fb` | **Still ❌ at `4d9941fb`.** Go fixed completion moved 85% -> 92% (128-bit, fixed-point, enum ordinals, nested union guards turned ✅). A branch `merge/schema-vt-core-into-main-20261012` exists while the ROADMAP still says "not yet on main": whether vt-core is on main is UNVERIFIED. |
| F12 | Spec tension: §3.4 compiles a plan from a peer's layout at runtime; §21 says never | emma §2.1 note | SPEC-TABLES.md lines 6963-6976 vs §21.5 | **Confirmed** at `4d9941fb`; §21 is the later rule. |
| F13 | TLA modules `Hook.tla`, `Bus2Receipts.tla`, `CardContract.tla` exist | emma §5.1 | nova-sprint `tla/` has `SeatHook.tla`, `HookCache.tla`, `Adopt.tla`, `FriendReadyTake.tla`, `CardMachine.tla`, `JudgmentRetire.tla`, not the three | **Confirmed in nova-tools `sprint/v1.3-tools`** (`tla/Hook.tla`, `tla/Bus2Receipts.tla`, `tla/CardContract.tla`); `JudgmentLoop.tla` does not exist (emma proposes it). Aurora's `CardMachine.tla` exists in both. |
| F14 | adopt runs store -> server -> dashboard -> friends | emma SOURCED | `cmd/nova-sprint/adopt_play_test.go:33-45, 85` | **Confirmed**; see U161 for what it does not cover. |
| F15 | Form bytes: variable `1`, fixed `3`, message `2` | emma, aurora | SPEC-TABLES.md lines 70-108 | **Confirmed.** |
| F16 | A fixed table refuses `*string`, maps, unbounded arrays, `if` fields, nested plain tables in its closure | emma | SPEC-TABLES.md §2.2 lines 919-931 | **Confirmed**: so the stella file's fixed `Envelope` cannot carry a body, which it does not (U35). |
| F17 | Table wire pre-release until 3.0.0; #441 retired-name hole | aurora, stella | VERSIONING.md lines 17-31, 319 | **Confirmed**; #441's issue page not opened (UNVERIFIED beyond the page's own text). |
| F18 | `ParseTest` at nova-sprint `pkg/cardhdr/cardhdr.go:103`, file 153 lines | aurora | checkout | **Confirmed.** |

### The two tracers and what they settled (rev)

Both tracers ran on 2026-10-15 against schema main `4d9941fbc7fd420b560739e48256d5b5e2892cdc` (Go
pseudo-version `v2.5.1-0.20261014163537-4d9941fbc7fd`). Both are tracer bullets: what is kept is what they
taught, not their code.

| Tracer (card) | Branch, head, finding | Form | Verdict | What it settled |
|---|---|---|---|---|
| `schema-tracer-friend-pong` | mas-bandwidth/nova-tools `sprint/schema-tracer-friend-pong.w1.g1.e21` at `9ef91022`, `docs/research/schema-tracer/pong.md` | variable `table Pong` (nonce string(32), queue/working/width uint32) | **SHOWED** | end to end once: `.schema` -> pinned compiler -> committed Go -> `nova-friend pong` writes, `ParsePong` reads, named refusals; today's `ParsePong` read 6 of 6 hostile lines as pongs, each now refused by name. internal/ci and internal/docs went red on generated files and the root map's byte cap (fixes proposed outside its PATHS) |
| `schema-tracer-done-record-fixed` | mas-bandwidth/nova-sprint `sprint/schema-tracer-done-record-fixed.w1.g1.e21` at `d53dd1a4`, `docs/research/schema-tracer/done-record.md` | `fixed table DoneRecord` (`done [..95]Completion`) | **SHOWED** | the fixed form works end to end (round trip, v2 reads v1 once locked, v1 refuses v2 as `layout_newer`, every hostile image refused), **but the done record is unbounded and does not fit**: at most 95 entries under the 65,536-byte body against 310+ completions today; recommend variable, or compact (one entry per row, kind, card) plus paged fixed records |

**Lessons carried into sections 4 and 6** (SOURCED from the two findings):

1. **The compiler pin is a `go:generate` line**, `go run github.com/mas-bandwidth/schema/v2/cmd/schema@<pseudo-version> generate --lang go --out . <schemas dir>`. It needs no `go get`, changes nothing in go.mod or go.sum, works under `GOFLAGS=-mod=readonly`, and keeps the AGPL compiler out of the build graph. The output was byte-identical across two build paths in both tracers. This replaces the `SCHEMA-VERSION` file (U88) and the go.mod pin (U89).
2. **Generated table Go imports only the standard library** (`encoding/binary`, `errors`, `math`, `unicode/utf8`, `unsafe`, and for the pong `sync`, `strconv`, `fmt`). No runtime module, no `require`, no `replace`. This settles D2's dependency worry and makes C3 unnecessary; the open question is only where the one definition lives (D2).
3. **Readers update before writers.** An old `ParsePong` reads the table pong as "not a pong", so a friend whose writer switched first looks deaf. Under Glenn's just-update ruling this is a short coordinated window, not a long dual-emit period; for fixed tables an older build refuses a newer layout (`layout_newer`) and must never write over it.
4. **Absent reads as zero, and the runtime clamps.** In a variable table a zero field is not written, so an absent field and a zero read the same; the runtime clamps an over-bound count or length and counts it instead of refusing. So **the facade refuses any clamp** (`clamped`), refuses absence where a zero is invalid (or the field is `?T`), and **validates before writing**, because the generated Go table writer checks no bound and no UTF-8 (both tracers).
5. **Fixed form only for small, bounded, server-owned records.** A fixed record writes every declared byte (65,412 bytes stored for 0 or 95 completions) and, for a small record, decoded about 44 times slower than JSON with 100 times the allocation; the binary grew 248 KB. The table pong costs 2.7x the legacy line with base64, which does not matter at tens of bytes.
6. **A per-form read counter**: the reader records which form it read (`Pong.Legacy`; the done record's first byte, `{` legacy JSON or `3` fixed), which is the hook for the cutover counter (U127, D10).
7. **Lock before the edit.** A fixed table's `schema.lock` (and a `tables.baseline`) must be committed beside the definition before the first edit, or the next build refuses every existing image as `layout_newer`; `generate` does not insist.
8. **Generated files need a CI exemption and a regenerate-and-diff check before the first generated package lands** (nova-tools `internal/ci` class tests and the `internal/docs` catalog went red on the pong tracer's generated files).

**Schema improvements wanted** (25 items, schema-side inputs for schema-repo cards, not nova cards): the done-record finding's 15 (fixed-form framing damage unnamed; refusal names not exported; clamp instead of refuse contrary to SPEC-TABLES §3.4; writer emits what its reader refuses; generate does not insist on the lock; unknown hash reads as `layout_newer`; no fixed-only surface; `FixedLoad` plan argument; per-call setup on load; no sizing guidance; wrong generated headers; package name bound by the lock; no timestamp idiom; no Go fixed-form usage page; pins are pseudo-versions) and the pong finding's 10 (`schema-go-table-writer-validates`, `schema-unsigned-range-implies-min-zero`, `schema-cli-error-noise`, `schema-go-table-strict-load`, `schema-table-required-or-presence-docs`, `schema-text-transport-encoding-doc`, `schema-go-unit-size`, `schema-generated-go-lint-clean`, `schema-go-adopter-recipe`, `schema-tables-baseline-notice-location`). Each is listed with its evidence and smallest fix in its finding. Per Glenn's ruling every adoption card in section 6 carries its own "Schema improvements wanted" section, so this list grows with the adoption.

### Merged source table

| Source | SHA read | Opened by | What it settles here |
|---|---|---|---|
| schema `docs/VERSIONING.md` (promises 1-11, "Two wires", evolution table) | `38af9571`, `46f8834d`, `4d9941fb` (unchanged) | emma, aurora, stella, union | variable wire semantics, pre-3.0 caveat, #441, retain text (F9, F17) |
| schema `docs/SPEC-TABLES.md` §2.2, §2.10, §3.4, §21 | `38af9571`, `4d9941fb` | emma (whole cited ranges), aurora and stella (targeted), union | fixed backward-only, closure rule, form bytes, spec tension (D1, F12, F15, F16) |
| schema `docs/FIXED-FORM-ALGORITHM.md` | `38af9571` (1,800 lines), `4d9941fb` (1,817) | emma | ONE PATH static plans |
| schema `ROADMAP.md` | `38af9571`, `4d9941fb` (14 lines changed) | emma, union | per-language completion; Go fixed gates still ❌ (F11) |
| schema `docs/CARDS.md`, `tables/cards/*` | `38af9571`, `4d9941fb` | emma (code), aurora and stella (doc), union | Card declaration, facade, bounds, module setup (D4, F5, F8) |
| schema `docs/SECURITY.md`, `USAGE.md`, `PORTING.md`, `README.md` | `38af9571` | emma, aurora (outline), stella (README) | fuzz/oracle gates, CLI, AGPL compiler with generated code the adopter's |
| schema `tables/AGENTS.md`, `go.mod`, `internal/codegen/gotable/retain.go` | `4d9941fb` | union | `tables/` is corpora; module path; Go retain (D2, F9) |
| nova-sprint `pkg/bus/bus.go` | `1ae18735` (emma read `c3b66834`) | emma, union | Message fields, MaxBody, MaxName, kinds validated (F6) |
| nova-sprint `pkg/typedrec/*`, `pkg/cardhdr/cardhdr.go`, `pkg/cardlimits/cardlimits.go` | `1ae18735` | aurora, emma, union | result parser, refusal taxonomy, 16 KiB bound (F4, F7, F10) |
| nova-sprint `pkg/friend/outbox.go`, `inbox.go`, `internal/sprint/hook.go`, `pkg/config/file.go`, `pkg/ntable/manifest_validate.go` | `1ae18735` | emma, union | reportKey, PacketOf fallback, the 10-11/12 incident comment, DisallowUnknownFields, 851 lines |
| nova-sprint `cmd/nova-sprint/adopt_play_test.go`, `tla/` | `1ae18735` | emma, aurora (listing), union | adopt order, which models exist (F13, F14) |
| nova-sprint `go.mod`, `NOVA-TOOLS-VERSION` | `1ae18735` | stella (listing), union | no nova-tools dependency (F3) |
| nova-tools `internal/bus/note.go`, `internal/cardhdr/cardhdr.go`, `cmd/nova-bus/*_functional_test.go` | `b8d29662` | stella, aurora (UNVERIFIED), union | F1, F2 |
| nova-tools `origin/sprint/v1.3-tools` tree, `tla/`, `pkg/typedrec/cardresult.go` | `4f7697d1` | union | where v1.3 code lives (U156, F4, F13) |
| nova-tools v1.2.23 `hook/hook.go`, `pkg/hookgate`, `cmd/nova-swarm/member.go` | `83d0e15b` | emma, union (existence only) | emma's hook citations exist |
| **(rev)** nova-sprint `go.mod`, import sites, `tla/` on `v1.3/comms` | `5dea8434`, `dfae414b` | union (revision) | nova-sprint requires and imports nova-tools (D2, F3); `tla/DoneRecord.tla`, `JudgmentRetire.tla`, `CardMachine.tla` present |
| **(rev)** nova-tools tree on `v1.3/comms` | `c435e3fa` | union (revision) | `pkg/cardhdr`, `pkg/typedrec`, `pkg/friend/{machine,outbox}.go`, `hook/`, `pkg/bus`, `tla/{CardContract,Hook,Bus2Receipts}.tla` live here (card repos in section 6) |
| **(rev)** the two tracer findings | nova-tools `9ef91022`, nova-sprint `d53dd1a4` | union (revision) | the tracer lessons above |
| The research brief's incident counts (~1,700 messages, >1,300 pongs, truncated refusal, oversize note, empty kind) | not in this checkout | all three as REPORTED | nothing here recounts them: UNVERIFIED, which is why R1 comes first |

## 4 Design synthesis

The design below takes the strongest version of each part; where the inputs differ the alternative is
named. Bounds follow nova's code where nova already holds a bound, the upstream Card where it exists, and
an input's figure otherwise. All snippets are illustrative; the definitions are settled with Glenn in
Layer 0 and each layer, not here.

### 4.1 The typed envelope that carries prose (Layer 1)

Variable table (D1), fields from today's `bus.Message` (`pkg/bus/bus.go:75-86`: ID, From, To, CC, Subject,
Re, Kind, At, Body, Token), plus emma's typed references (U62) so judgments stop living in subjects (U99).

```schema
package nova

enum Kind { status, report, ack, request, blocker }   // pkg/bus Kinds; "" is status today (U159); append only

table Name { s string(64) }                            // MaxName = 64 (pkg/bus/bus.go:36); element table (U69)

table Envelope
{
    id      string(64)
    from    string(64)
    to      [..16]Name          // emma's recipient bound (U62); today there is none: decide in Layer 1
    cc      [..16]Name
    re      string(64)
    kind    Kind
    subject string(200)         // all three use 200
    card    string(64)          // emma card_ref; empty when not about a card
    head    string(40)          // emma head_ref; hex checked in the facade (D7)
    token   string(64)          // the sender's retry token (pkg/bus/bus.go:83-85)
    at_ms   int64
    body    *string             // prose, unchecked, capped by MaxBody = 1 MiB in the facade
}
```

What stays text: the subject and body. The stella file's idea of one deliberately untyped kind (U150) is
met by `status`/`report` with prose bodies. The `authority` field (emma) and the `v` field (stella, U63)
are left out: authority is who `from` is, and the form byte already distinguishes old wire from new (D10).

### 4.2 Beat, pong and ack (Layer 1)

```schema
table Pong                      // (rev) variable, per Glenn's ruling (D1): an older reader may meet it
{
    friend  string(64)          // emma; the reader should not infer it from the stream (not in the tracer)
    nonce   string(32)          // (rev) text: today's nonces are 6 chars of [a-z0-9] or 6 clock digits (pong tracer)
    queue   uint32 | min = 0, max = 100000    // (rev) uint32 as the tracer ran it; unsigned still spells min = 0
    working uint32 | min = 0, max = 10000     // stella's tighter bounds
    width   uint32 | min = 0, max = 1024
    sent_ms int64               // emma; a newer writer's sent_ms is skipped and counted by an older reader (tracer)
}

table Ack                       // the receipt: emma BusReceipt, stella send receipt (U18, U64)
{
    id      string(64)
    stage   Stage               // enum Stage { queued, claimed, pushed, acted }
    push    PushState           // enum PushState { up, down, stale }: the NOTE line today
    reason  string(152)
}
```

**(rev)** The pong tracer ran the four-field form (nonce, queue, working, width) and carried it on the bus as
`pongwire ` + padded standard base64 of the binary wire, which keeps the form byte (`unknown_form`) and the
framing check (`wire_damaged`). Absent equals zero in a variable table, so a liveness count needs `?uint32`
or a schema-side `required` before the definition is fixed (section 4.6).

The beat: emma's split (U44) — the wire pulse carries the hot scalars; the unbounded `live_seen` map
stays in the stored record. The hook stamp: emma's `HookLeaseStamp` with int64 millisecond times (U66),
which removes the RFC3339 parse that failed closed on 2026-10-11/12 (`internal/sprint/hook.go:33-38`).

### 4.3 Card and result (Layer 2)

- **Card**: the upstream `tables/cards` Card (D4), with nova's 16 KiB body bound in nova's facade (U160),
  plus emma's `rework_fix` and `rework_fix_hash` (U53) proposed as a schema-side addition only after R19
  shows the keyword check is the failure.
- **Job packet**: emma's `JobPacket` (U55) wrapping `*Card`, which ends the `PacketOf` brief scraping (U7).
- **Result**: one table for RESULT.md and REPORT.md (emma U57), but with the judgment verdict separate from
  the run outcome (D5). **(rev)** Emma's `WorkVerdict` is restored by name with all six values (the first
  draft replaced it with `RunEnd` and lost `ok`/`not_done`/`nothing`/`broken`), her `RunEndKind` keeps all
  seven values (`budget_unverifiable` had been dropped), and her `GateRoute` and cost fields come back:

```schema
enum WorkVerdict { ok, not_done, nothing, broken, hold, fail }      // emma: the worker's run outcome
enum RunEndKind  { completed, provider_failure, staging_refused, no_result, budget_exceeded,
                   deadline_exceeded, budget_unverifiable }         // emma: how the run ended
enum GateRoute   { passed, pre_existing, flaky_green, red }         // emma: the gate's route
enum Verdict     { land, hold, fail }                               // aurora/stella: the judgment (D5)

table Result
{
    card       string(64)
    job        string(128)
    attempt    int32 | min = 1, max = 1000
    gen        int32 | min = 1, max = 10000
    epoch      int32 | min = 0, max = 1000000
    outcome    WorkVerdict      // (rev) restored: the run outcome a worker or friend reports
    end        RunEndKind       // (rev) emma's name and all seven values
    gate       GateRoute        // (rev) emma's enum; aurora's free-text gate words go in gate_tests
    gate_tests string(256)
    verdict    ?Verdict         // the judgment, present only on a reader's REPORT.md (D5)
    head       string(40)
    branch     string(128)
    elapsed_ms     int64 | min = 0, max = 86400000      // (rev) emma's cost and usage fields
    tokens_used    int64 | min = 0, max = 100000000
    cost_micro_usd int64 | min = 0, max = 1000000000
    report     *string
}
```

  **(rev 2)** Every enum does carry an unset zero: the Go emitter writes `<Enum>None = 0` before the
  variants, which are dense from 1 (schema `4d9941fb` `internal/codegen/golang/golang.go:240-245`), the
  table codec writes None as reference id zero (`internal/codegen/gotable/codecs.go:287, 291`), and an id
  this build cannot name reads back as None (`codecs.go:298-306`). So under the absent-reads-as-zero lesson
  an absent or unknown `outcome` reads as `WorkVerdictNone`, which is not a run outcome, and the facade can
  refuse it by value with no presence bit: a non-optional `outcome` that reads None is refused
  (`RESULT REFUSED: verdict missing`). The risk differs by type. A numeric field has no such spare value:
  the pong's `queue = 0` is a legitimate reading, indistinguishable on the read from an absent field, so
  only a presence check or the schema-side `required` the pong tracer asks for closes it there. Enums are
  the case the facade can already guard; numbers are the case that needs schema help.

### 4.4 Judgments, refusals, findings (Layer 3)

- **Judgment**: aurora's table (card, verdict, head, finding, body) as the record the sprint stores; the bus
  push carries it as an Envelope with `card` and `head` set.
- **Refusal**: one table with a stable machine name (stella U148), reason, remedy and bound, reusing the
  existing `TableRefusal` names (aurora U5) as the first variants. Variable first; fixed is the stella file's
  and emma's later option (U31).
- **Finding**: emma's `FindingWire` fields (U32) as a variable table.

### 4.5 Versioning and the mixed-fleet rule

**(rev)** Rewritten for Glenn's rulings of 2026-10-15 and the tracers. The first draft said "variable
everywhere until C1 is green and R3 passes"; R3 was the wrong reference (the mixed-version test is R6) and
the owner has since settled D1.

1. **Both forms, dogfooded in both repos.** A **variable table** for every message an older reader may
   meet: everything friends exchange (envelope, pong, beat, ack, card, result, judgment push). Variable
   tables read both ways within a major (VERSIONING promises 1-3 from 3.0.0; pre-3.0 the wire may change in
   an announced minor). A **fixed table** for records whose reader is always the same version or newer:
   server-owned store records written and read by the sprint server, and only when the record is small
   and bounded (done-record tracer). R6 tests the rule on two builds before Layer 1's fixed card ships.
2. **Fixed-table discipline**: commit `schema.lock` and `tables.baseline` beside the definition before the
   first edit; an older build that meets `layout_newer` refuses the tick and never writes over the newer
   record; legacy stored values are told apart by their first byte (`{` legacy JSON, `3` fixed) and read
   until rewritten. Go's fixed oracle and hostile-input cells (C1) are schema-side gates for offering the
   form to others, not for nova's own dogfooding on server-owned records, where the tracer's hostile-image
   set already refuses by name.
3. **No offline-compatibility policy.** Returning friends simply update (a paste at worst). A cutover is:
   readers dual-read, adopted fleet-wide, then writers switch, in a short coordinated window; the per-form
   read counter shows when legacy reads have stopped. No long dual-emit windows.
4. A baseline is committed before any data leaves the building (U91); the retired-name hole (#441) is
   closed schema-side (C2) or guarded by a nova review rule until then.
5. Bumps: change the pseudo-version in the `go:generate` line, regenerate, run the regenerate-and-diff
   check and conformance, roll readers before writers (U90).
6. **The bar before offering the table forms to other people is full nova adoption**: every layer of
   section 6 used for real in nova-tools and nova-sprint, both forms dogfooded.

### 4.6 Validation at both ends and the named refusals

Sender: the build CLI and Go callers check UTF-8, NUL, bounds, enums and loss before writing (U72).
Receiver: the generated reader, then the facade, before any state moves (U70). House style
`NAME REFUSED: reason; run: <help>` with the bound named (U71). The refusals merged from the inputs:

- `ENVELOPE REFUSED: kind "x" is not one of status, report, ack, request, blocker; run: nova-bus send -h`
- `ENVELOPE REFUSED: subject over 200 bytes; run: nova-bus send -h`
- `PONG REFUSED: queue over 100000; run: nova-friend pong -h`
- `CARD REFUSED: body over 16384 bytes; run: nova-sprint brief -h` (nova's bound, F7)
- `RESULT REFUSED: verdict missing; run: nova-work result -h`
- `RESULT REFUSED: head is not 40 lowercase hex; run: nova-work result -h`
- `<NAME> REFUSED: layout_newer <hash>; run: <verb> -h` (fixed tables only, §21.3)
- **(rev)** `PONG REFUSED: clamped (queue); run: nova-friend pong -h` and the other tracer-proven names:
  `wire_damaged`, `unknown_form`, `kind_mismatch`, `malformed`, `not_a_pong`, `nonce_absent`,
  `legacy_shape` (pong); `layout_malformed`, `record_count`, `batch_too_large`, `no_layout`,
  `previous_form`, `message_form_as_file`, `newer_form`, `text_ill_formed` (done record)

**(rev) Facade rules from the tracers.** One shared strict-read helper per repo (not per type, and
generated if the schema-side `ReadStrict` lands): measure the reason, load, then refuse on malformed,
clamped or kind-mismatched; tolerate an unknown field (skipped and counted) and a widened one. The facade
**refuses every clamp**, because the runtime clamps by design and a clamped nonce or count is a different
value. It **refuses absence where zero is not a valid value**, because absent reads as zero. And it
**validates before writing** (bounds, UTF-8, NUL, enum ordinals), because the generated Go table writer
checks none of them: the writer must emit only what its own reader accepts.

### 4.7 Go codegen and pinning

~~Option (a): `schemas/nova/` in nova-sprint, nova-tools vendors a byte-identical copy; compiler pinned in
`SCHEMA-VERSION`, `serialize.go` in go.mod.~~ **(rev)** Rewritten from the tracers and the corrected D2:

- **Pin**: each generated package has a hand-written `generate.go` holding its package doc and the
  `//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@<pseudo-version> generate --lang go --out . <schemas dir>` line. No go.mod or go.sum change, `-mod=readonly` throughout, and the compiler is
  never a build dependency. The first cold run fetched and built the compiler in 1m43s; a warm rerun took
  2.9s.
- **Dependencies**: none. Generated table Go imports only the standard library. A future unit with a wide
  value would add a plain `require` of serialize.go (public, tagged v1.9.0), never a replace (UNVERIFIED:
  nothing has needed it yet).
- **Home** (D2 lean, Glenn decides): a type both repos read lives once in nova-tools (`schemas/<area>/`
  -> `pkg/<area>wire/`), and nova-sprint imports it through its existing nova-tools require. A record only
  one repo reads lives beside its consumer, such as `schemas/store/` -> `internal/sprint/store/donewire/` in
  nova-sprint.
- **Checks**: generated Go is committed. A `make schemawire-check` (go generate, then `git diff --exit-code` over the generated and `schemas/` paths) runs in CI. internal/ci's line linters skip files
  with Go's `// Code generated ... DO NOT EDIT.` header, and the `internal/docs` catalog gets rows for
  `schemas/`. Both of these land **before** the first generated package (pong tracer).
- **Size**: a 4-field table generates 10,804 lines (308 KB); two fixed tables grew nova-sprint by 248 KB.
  This is accepted for now, and `--surface` is a schema-side improvement wanted.

### 4.8 The build CLI for other harnesses

Per-domain verbs (U81) because each already exists as a verb family: `nova-bus send`, `nova-friend pong`,
`nova-work result`, `nova-sprint brief`. Contract from emma (U82): flags or `--from-md`/`--spec`, validate,
write nothing on refusal, one refusal line on stderr, exit 2. Help per the three inputs (U79). R3 (the cold-AI
experiment; **(rev)** the first draft said R1, which is the fault census) decides whether the help is enough.

### 4.9 Human-readable rendering

The name-keyed text form is the reading surface (U84); a `show` verb prints it and ships with Layer 1
(U137). Markdown stays on disk beside any binary sidecar (U86). Bus log lines use `oneline` (U85).

### 4.10 What stays text

Cairn and memory prose; brief, report, note and message bodies; the roadmap and fixes s-expression ledgers;
Redis key names; anything read only by the process that wrote it (U37, U39-U41).

## 5 Merged inventory and fit

Format, parser and incident cells keep the inputs' labels; "Layer" is the layer of section 6.

| Id | Thing | From | Format today | Parser today | Fit | Layer | Incident that shows the failure |
|---|---|---|---|---|---|---|---|
| M1 | Card brief (header lines + prose) | emma M1, aurora M1, stella M1 | line text, 16 KiB refused at add (F7) | nova-sprint `pkg/cardhdr/cardhdr.go:103 ParseTest`, `:48-61 KeyValue`; `pkg/swarm/lintheader.go` | variable (upstream Card) + `*string` body | 2 | oversize note froze the drain (REPORTED; a note, not a brief, F7) |
| M2 | Staged job packet (JOB.md, `member.Packet`, `HeldCard`) | emma M2 | JSON over sprintwire + rendered Markdown | `pkg/friend/inbox.go:38-111 ParseHeld`; `PacketOf` scrapes the brief | variable (`JobPacket`) | 2 | silent zero-fill on skew; brief scraping (SOURCED code path, no dated incident) |
| M3 | Worker result RESULT.md | emma M3, aurora M2, stella M2 | `key: value` + body, two historical shapes | `pkg/typedrec/cardresult.go:54 ParseCardResult`, `:153 shapeFault` | variable (`Result`) | 2 | "no RESULT.md shape" endings blaming the worker (SOURCED test `attempts_test.go:31`; counts REPORTED) |
| M4 | Friend outbox REPORT.md | emma M4 | free Markdown, 64 KiB cap | `pkg/friend/outbox.go:58-65 reportKey` | variable (same `Result`, transcoded at the gateway) | 2 | 2026-10-05: 8 cards unread 2 h for a missing `Verdict:` (SOURCED by emma) |
| M5 | Result summary and judgment contract (`typedrec.Contract`, `swarm.Report`) | emma M5 | header lines + sections + JSON | `pkg/typedrec/contract.go:46-118` | variable; emma's fixed `CardResultWire` is optional | 3 | the two repos' `result.go` diverged (13 vs 823 lines) |
| M6 | Bus message envelope | emma M8, aurora M3, stella M3 | Redis stream `bus2:*` + hash (nova-sprint `pkg/bus`); git note files (nova-tools main `internal/bus`) | `pkg/bus/bus.go` Recv/Peek; nova-tools `internal/bus/note.go:140 ParseNote` | variable (`Envelope`) | 1 | subjects carry structure (`#202 HOLD at ...`, `PING <nonce>`) (REPORTED) |
| M7 | Pong / liveness reply | emma M9/M11, aurora M4, stella M4 | positional text tokens on the bus | nova-tools `pkg/friend/machine.go:14 ParsePong` on `v1.3/comms` (rev) | variable (D1 ruling, rev; pong tracer SHOWED) | 1 | >1,300 of ~1,700 messages one night were pongs (REPORTED); malformed pong read as silence (REPORTED class) |
| M8 | Beat / presence heartbeat | emma M11, aurora M5, stella M5 | JSON `sprint.Beat` in Redis, FRIEND-BEAT text | `internal/sprint/presence.go:89-250`; `pkg/friend/daemon.go:158-230` | variable (rev: friend-exchanged, so variable under the D1 ruling) | 1 | Pacing nil because never plumbed (SOURCED) |
| M9 | Hook challenge/proof and push proof | emma M9, aurora M6, stella M6 | four Redis shapes, RFC3339 stamps, hex token in a CLI line | `hook/hook.go` (nova-tools v1.3-tools), `internal/sprint/hook.go:76-222` | variable `HookStamp` (rev: read by friends) | 1 | 2026-10-11/12 seat unhooked, ~100 pushes replayed (SOURCED comment `internal/sprint/hook.go:33-38`) |
| M10 | Session check request/result | aurora M7 | prose + nonce convention | bus layer | Envelope + Pong fields | 1 | own-prompt echo counted as receipt (REPORTED) |
| M11 | Send receipt / bus receipt | stella M7, emma M8 | `SEND OK id=` + prose NOTE; `bus2:rec:<id>` hash | substring match (`beat_functional_test.go:84`) | variable `Ack` | 1 | refused vs stale send look alike (REPORTED class) |
| M12 | Judgment (HOLD/FIX/merge) | aurora M8, stella M9, emma M14 | prose, ids in subject | subject convention | variable `Judgment` + Envelope refs | 3 | reworded subject loses the verdict (REPORTED class) |
| M13 | Landing / merge verdict, gate result, head pin | aurora M9, stella M10 | Go structs to store; prose report | `internal/sprint` FinishReq/MergeReq; `cmd/nova-sprint/land.go` | variable | 3 | stale head if the generation fence is missed (SOURCED test names) |
| M14 | Refusals (lane fault, flag, lint, table) | aurora M10, stella M8, emma M6 | truncated JSON or prose on stderr; table refusal tokens | `pkg/typedrec/tablerefusal.go:66` (token switch, F10) | variable `Refusal` (fixed optional) | 3 | `lane-fault refusal={` cut mid-object (REPORTED) |
| M15 | Lint findings | emma M7 | prose `LINT DRIFT` lines | `pkg/typedrec/finding.go:13-20` regex | variable `Finding` | 3 | `--json` refused on lint verbs (REPORTED) |
| M16 | Card header extras (GATE-NEEDS, SIBLINGS, SHARED) | stella M11 | header lines | admission lint, `docs/SPEC-CARD-CONTRACT.md` | fields of Card (`extra` today) | 2 | admission refusals (SOURCED, spec) |
| M17 | Card store records | aurora M11, stella M14 | Go structs, JSON views | `internal/sprintdash/dashboard.go:22` | in-process: schema not needed (U41); views variable if shared. **(rev)** Server-owned store records are the fixed-table dogfood under the D1 ruling when small and bounded: the done record, compacted and paged (C28) | 1 (done record, C28); 4 (the rest) | cost rows dropped on carry (REPORTED) |
| M18 | Sprint notes, log lines, stops | emma M14 | JSON blobs in Redis streams | `internal/sprint/notes.go:195-246`, `verdicts.go:161-176` | variable (`SprintNote`, log line) | 3 | `FinishedAs` heuristics (SOURCED) |
| M19 | Config rows and settings | aurora M12, stella M12, emma M15 | JSON file, Postgres, Redis hashes | `pkg/config/file.go:46-114`, `kind.go` | variable with retain | 4 (deferred) | older binary refuses newer file (SOURCED `file.go:65`); partial apply (REPORTED) |
| M20 | Subscription / consumer receipts | aurora M13, stella M13 | table-like rows | `docs/SPEC-SUBSCRIPTION-RECEIPTS.md`; schema `Consumer` table | variable (upstream `Consumer`) | 4 (deferred) | billing by prose (REPORTED class) |
| M21 | Roadmap and fixes ledgers | aurora M14, stella M16, emma M16 | s-expressions, lander-merged | lander; `internal/roadmap/roadmap.go` | **stays text: wrong tool** | none | none attributable to format |
| M22 | Dashboard JSON / events feed | aurora M15 | ad-hoc JSON | `internal/sprintdash/dashboard.go:22`, `pull.go` | JSON generated from the definitions (U146) | 4 (deferred) | drift invisible until render (OPINION) |
| M23 | Cairn / memory notes | aurora M16, stella M15, emma M16 | Markdown; cairn JSON entries | human; `internal/cairn/cairn.go` (nova-tools) | prose stays text; entry metadata variable (U38) | 4 (deferred) | none; human text by design |
| M24 | Redis keys and stream field maps | aurora M17, emma §1 list 4 | field maps | each writer | **naming convention: wrong tool** (U40) | none | renamed in one writer (OPINION) |
| M25 | Existing typed-record packages (typedrec, ntable, sprinttable) | stella M17 | Go structs + text binding | each package | absorbed as the facades of the generated types | 1-3 | three partial answers (OPINION) |
| M26 | sprintwire `/verbs` RPC | emma M10 | HTTP JSON of argv and stdout | `pkg/sprintwire/wire.go:23-56` | variable `WorkerStep` | 4 (deferred) | stdout scraping (SOURCED) |
| M27 | Friend local state files (presence, limit, status, lanes) | emma M12 | local JSON via atomicfile | `pkg/friend/presence.go:161-210` | variable (emma: fixed) | 4 (deferred) | nonce dropped across an upgrade (SOURCED path) |
| M28 | Table batch manifest, receipt, check | emma M13 | JSON to a Redis function, 851-line validator | `pkg/ntable/manifest_validate.go`, Lua `ns_table_apply` | variable `BatchManifest` | 4 (deferred) | none named; the cost is the validator's size |

**Reliability gain, class by class** (merged from emma §4.1, aurora §4, stella §4):

| Class | Gain | Where it is refused | What carries the rest |
|---|---|---|---|
| Malformed result | removed for the malformed part | the builder at the sender; the gateway outside the sandbox (U152); the member before acting | a valid result with the wrong verdict: readers and gates |
| Truncated refusal | removed | framing at the receiver (form-1 trailer) | the CLI must emit the record whole (C23) |
| Flag and format drift | reduced | `--json` backed by generated output | flag hoisting and one verb dispatcher: CLI work (D8) |
| Subject conventions | removed | fields in the envelope | readers must stop scraping subjects: a census (R11) |
| Oversize | already refused at add for briefs (F7); for notes removed by declaring their bound | sender | drain isolation for anything already stored (U101) |
| Ambiguous kinds | already refused by `CheckKinds` for nova-sprint's bus (F6); the enum makes it a wire fact | sender and reader | tools on older builds |
| Replays/duplicates | enabled, not fixed | typed id and stage | dedupe policy and the hook model (U103) |
| Liveness false negatives from malformed pongs | removed | reader refuses by name instead of reading silence | a friend that sends nothing: unchanged |

**What schema cannot fix** (all three): a well-formed message that is wrong, a body that lies, a friend
that never sends, an overloaded bus or store, a red gate. Those stay with SHA checks, diffcheck, gates,
TLA+ models and readers (U106, U107). Where schema is the wrong tool: the s-expression ledgers, Redis key
names, prose, in-process structs (M21, M23, M24, M17).

## 6 Recommended scope, layer plan and implementation cards

### The recommended scope

**Adopt in v1.3:** Layers 0-3 and the delete step (Layer 5). **Leave as text:** prose bodies, cairn and
memory, s-expression ledgers, Redis key names. **Defer** (Layer 4, each its own decision after Layer 3 is
used for real): config store, consumer receipts, store views and dashboard, worker-step RPC, the manifest
validator, local state files, cairn metadata. This follows aurora and the stella file on scope and emma on
design detail; D3 is Glenn's call.

**(rev) The owner's rulings of 2026-10-15, folded in:**

- **Both forms, in both repos, dogfooded.** Variable tables for every message an older reader may meet;
  fixed tables for records whose reader is always the same version or newer (server-owned store
  records), small and bounded only (section 4.5). Layer 1 therefore carries one fixed server-owned record
  (C28) beside the variable frames.
- **Just update.** Returning friends update (a paste at worst): no offline-compatibility policy, no long
  dual-emit windows. Each cutover is readers first, fleet-wide, then writers, in a short coordinated window,
  with the per-form read counter as the evidence (D10).
- **D2 corrected**: nova-sprint's go.mod at `v1.3/comms` `5dea8434` already requires nova-tools, and on
  that line nova-sprint imports nova-tools' `pkg/cardhdr`, `pkg/typedrec`, `hook` and `pkg/bus`. So
  shared definitions live once in nova-tools and nova-sprint imports them; no copy, no sync card.
- **Full nova adoption is the bar** before the table forms are offered to other people: every layer below
  used for real in both repos, both forms dogfooded, and the schema-side gates (C1-C4) green.
- **Every adoption card carries a "Schema improvements wanted" section** in its finding (title, evidence,
  smallest fix), as both tracers did; the 25 items they found (section 3) are the first schema-side inputs.

**The layers, bottom up. Each is settled with Glenn before it is built, then secured (modelled, tested
including every refusal, a second reader's probes, used for real, documented) before the next starts.**

| Layer | What | Guarantee (one line) | Oracle check | TLA+ | Rollback | Acceptance |
|---|---|---|---|---|---|---|
| **0 Pin and prove** | **(rev)** `go:generate` compiler pin, generated-file CI exemption and regenerate-and-diff check in both repos, the shared strict-read helper with its per-form counter, conformance in CI, the fault corpus; schema-side gates C1-C4 | the toolchain is reproducible, every nova definition round-trips through the pinned compiler, and no generated file is linted line by line | schema Go oracle + wire fuzz on nova's units (C7); strict-read refusals (C29) | none (no state machine) | delete the generated packages; nothing reads the wire yet | `make schemawire-check` green in both repos with a generated package present; fault corpus classified (C5) |
| **1 Envelope, beat, pong, ack** (+ hook stamp, send receipt) **and one fixed server record** | the frames friends exchange to be heard (variable); **(rev)** the done record as the fixed-table dogfood (C28) | no malformed liveness or envelope message is ever acted on: it is refused by name at the reader; an older build never writes over a newer fixed layout | golden per type + every named refusal + fuzzed malformed frames; the done-record tracer's hostile images | **yes**: `TypedFrames` (C9) with the tracers' invariants; `DoneRecord.tla` gains the layout version (C28) | readers stay dual for the window; writers switch back per type | **(rev)** readers adopted fleet-wide before writers switch (C30); one full night: zero hand-parsed pongs, every malformed frame refused, legacy reads per form counted to zero (C16); Glenn signs Layer 1 off |
| **2 Card and result** (+ job packet, REPORT gateway) | what a worker is given and what it returns | no card is admitted and no result accepted unless its frame validates; no shape fault is ever blamed on a worker | parity with today's admitted briefs; replay of the fault corpus | **yes**: `CardContract` gains "result typed before review" (C17) | text briefs and RESULT.md still read | zero shape-fault blames across a stream; parity 0 diffs; **(rev) C17-C22 signed off by Glenn (C27) before C23 or C26 starts** |
| **3 Judgments and reports** (+ refusals, findings, notes) | what readers and the coordinator decide | a verdict is a field, never a subject or a log-shape inference; every refusal is whole and named | golden + every verb's `--json` refusal under truncation | **yes**: `JudgmentRetire` gains the typed-verdict invariant (C24) | dual-read of notes | subjects reworded freely with zero verdict loss; `FinishedAs` reads the field |
| **5 Delete old parsers** | per type, by counter | one parser per message, and the old one is gone | `go test ./...` with the legacy path removed | none | revert the deletion commit | **(rev)** the per-form counter shows zero legacy reads for one adopt cycle after the writers switched (was: a full epoch plus 7-day TTLs) |
| *4 Deferred* | config, receipts, store views, RPC, manifest, local state, cairn metadata | each decided on its own after Layer 3 | — | — | — | — |

**Fixed tables** ~~enter at most as a promotion inside Layer 1 or later, for friend-to-server frames only,
after C1 is green and R3 passes (D1).~~ **(rev)** Per Glenn's ruling, fixed tables are dogfooded from
Layer 1 on server-owned store records whose reader is always the same version or newer, small and bounded
(C28); every friend-exchanged frame is variable. R6 (the mixed-version matrix; the first draft wrongly
said R3, the cold-AI test) runs before C28 ships. C1 gates offering the fixed form to other people, not
nova's own server-owned use.

**Dual format and cutover. (rev)** Per type: readers dual-read (legacy and table, counted per form),
adopted fleet-wide; then writers switch, in a short coordinated window. Returning friends update. The old
parser is deleted (Layer 5) when the per-form counter shows zero legacy reads for one adopt cycle after the
writers switched. Legacy stored records are read until rewritten. ~~Phase A/B, a full epoch and the 7-day
TTLs (U125, U127, U128)~~ are dropped by the just-update ruling.

### Implementation cards

**(rev) Bases, dependency slugs and repos.** Every card names the branch it starts from (column Base) and
its needs by id **and slug**. The first draft named a `sprint/v1.3-schema` base that does not exist and
put nova-tools cards on `sprint/v1.3-tools`; the actual v1.3 line is **`v1.3/comms`** in both repos
(nova-sprint `dfae414b`, nova-tools `c435e3fa` at this revision; both tracers branched from it). On that
line nova-sprint's `pkg/cardhdr`, `pkg/typedrec`, `pkg/friend/{machine,outbox}.go` and
`internal/sprint/hook.go` are gone, imported from nova-tools, so the cards that touch them moved to
nova-tools. Schema-side cards (SCHEMA-SIDE) land on the schema repo's existing streams, never on a nova
stream: `sprint/schema-go` (Go codegen), `sprint/schema-shared` (the shared compiler), `sprint/schema-cards`
(docs). The coordinator may cut a stream base from `v1.3/comms`; the column names where the work starts.
No card lands on a v1.2.x stream. Worker-days are ESTIMATED as before. Cards with `tla` in their
acceptance need a `GATE-NEEDS: tla` bench, never the working machine. **Every card's finding carries a
"Schema improvements wanted" section** (owner's ruling).

| Id | Card (slug) | Repo | Base | PATHS | Needs (id slug) | Tier | Acceptance (a cold worker can run it) | Layer | Worker-days |
|---|---|---|---|---|---|---|---|---|---|
| C1 | schema-go-fixed-oracle-gates (SCHEMA-SIDE) | mas-bandwidth/schema | `sprint/schema-go` | `internal/codegen/gotable/`, `make/go.mk`, `docs/roadmap-evidence/fixed-go.sexp` | - | heavy | in `ROADMAP.md` regenerated from the evidence, `grep -E '^\| (Shared byte oracle\|Hostile-input)' ROADMAP.md \| awk -F'\|' '{print $6}'` prints ✅ twice (the go column) | 0 (gate for offering fixed tables to others) | 3 |
| C2 | schema-retired-name-refusal (SCHEMA-SIDE, #441) | mas-bandwidth/schema | `sprint/schema-shared` | `internal/check/`, `docs/VERSIONING.md` | - | frontier | `go test ./internal/check -run TestRetiredNameReuseRefused` passes, and the VERSIONING.md evolution-table row "a retired name re-added with a new meaning" no longer reads "passes today" | 0 | 2.5 |
| C3 | schema-go-adopter-recipe (SCHEMA-SIDE; **(rev)** was schema-go-adopter-module, not needed: generated table Go is stdlib-only) | mas-bandwidth/schema | `sprint/schema-cards` | `docs/USAGE.md` | - | pro | `grep -c '^## Adopting from Go' docs/USAGE.md` prints 1, and that section names the `go run github.com/mas-bandwidth/schema/v2/cmd/schema@<pseudo-version>` pin, states table-only units import only the standard library, and says when serialize.go is required (pong tracer improvement 9) | 0 | 0.5 |
| C4 | schema-versioning-retain-text (SCHEMA-SIDE) | mas-bandwidth/schema | `sprint/schema-cards` | `docs/VERSIONING.md` | C2 schema-retired-name-refusal | flash | `grep -c 'the eight ports do not' docs/VERSIONING.md` prints 0 and promise 10 names the ports that the ROADMAP retain-unknown row marks ✅ | 0 | 0.25 |
| C5 | nova-schema-fault-corpus (**(rev)** moved to nova-tools, where the result, report and pong parsers now live) | mas-bandwidth/nova-tools | `v1.3/comms` | `testdata/schema-faults/` | - | pro | `testdata/schema-faults/INDEX.tsv` has one row per fixture file with columns file, class, incident; `awk -F'\t' 'NR>1 && $2!~/^(malformed\|wrong)$/' testdata/schema-faults/INDEX.tsv` prints nothing; at least 40 fixtures across result, report, pong, envelope, refusal | 0 | 1 |
| C6 | nova-tools-schema-home-and-pin (**(rev)** was nova-schema-home-and-pin in nova-sprint) | mas-bandwidth/nova-tools | `v1.3/comms` | `schemas/README.md`, `Makefile`, `internal/ci/generality_class_test.go`, `internal/ci/namedpaths_class_test.go`, `internal/docs/catalog.go`, the docs maps `make map` writes | - | heavy | `make schemawire-check` runs `go generate` over every `pkg/*wire` and `git diff --exit-code -- pkg schemas` and exits 0; with a generated file carrying Go's `// Code generated ... DO NOT EDIT.` header present, `go test ./internal/ci ./internal/docs` passes (the line linters skip it; the root map stays under its byte cap) | 0 | 2 |
| C7 | nova-schema-conformance-ci | mas-bandwidth/nova-tools | `v1.3/comms` | `Makefile`, `internal/ci/` | C6 nova-tools-schema-home-and-pin | pro | `make schemawire-conformance` runs the schema Go oracle and wire fuzz over every unit in `schemas/`, exits 0, and prints its elapsed time (dead end: over 10 minutes, aurora S8) | 0 | 1 |
| C8 | nova-sprint-schema-pin-and-import (**(rev)** was nova-tools-novawire-sync: no copy is needed now that nova-sprint imports nova-tools) | mas-bandwidth/nova-sprint | `v1.3/comms` | `schemas/README.md`, `Makefile`, `internal/ci/`, `go.mod`, `go.sum` | C6 nova-tools-schema-home-and-pin | pro | `make schemawire-check` exits 0 for nova-sprint's local units; `go test ./internal/ci` passes with a generated package present; go.mod requires a nova-tools version that contains C6 | 0 | 1 |
| C29 | nova-tools-strict-read-and-form-counter (**(rev)** new, from the tracers) | mas-bandwidth/nova-tools | `v1.3/comms` | `pkg/schemaread/` | C6 nova-tools-schema-home-and-pin | heavy | `go test ./pkg/schemaread -run TestStrictRead` passes: `clamped`, `kind_mismatch`, `malformed`, `unknown_form`, `wire_damaged` and an absent required field are each refused by name; an unknown field is tolerated and counted; the per-form counter moves once per read, on the form read | 0 | 1.5 |
| C9 | l1-tla-typed-frames | mas-bandwidth/nova-tools | `v1.3/comms` | `tla/TypedFrames.tla`, `tla/MCTypedFrames.tla`, `tla/MCTypedFrames*.cfg` | - | heavy | TLC on `tla/MCTypedFrames.cfg` passes; each `tla/MCTypedFramesBroken*.cfg` fails on its named invariant: `FrameRefusedBeforeTransition`, `DualReadNoLoss`, **(rev)** `ReadersBeforeWriters`, `NoClampAccepted`, `AbsentIsNotZero`, `WriterRefusesWhatReaderRefuses` | 1 | 2 |
| C10 | l1-defs-envelope-pong-ack-beat-hook (**(rev)** moved to nova-tools: both repos read these) | mas-bandwidth/nova-tools | `v1.3/comms` | `schemas/bus/`, `pkg/buswire/` | C6 nova-tools-schema-home-and-pin, C7 nova-schema-conformance-ci, C9 l1-tla-typed-frames, C29 nova-tools-strict-read-and-form-counter | heavy | `go test ./pkg/buswire -run TestLayer1Golden` passes: each type round-trips, each named refusal in section 4.6 fires on its bound, each renders to the text form; `schemas/bus/tables.baseline` is committed | 1 | 1.5 |
| C11 | l1-bus-envelope-dual-read | mas-bandwidth/nova-sprint | `v1.3/comms` | `pkg/bus/`, `go.mod`, `go.sum` | C8 nova-sprint-schema-pin-and-import, C10 l1-defs-envelope-pong-ack-beat-hook | heavy | `go test ./pkg/bus -run TestEnvelopeDualRead` passes: legacy and typed entries read alike, a malformed typed entry is refused by name, the per-form counter moves on the form read | 1 | 2 |
| C12 | l1-nova-bus-reader-builder-and-show | mas-bandwidth/nova-tools | `v1.3/comms` | `cmd/nova-bus/`, `pkg/bus/` | C10 l1-defs-envelope-pong-ack-beat-hook | heavy | `go test ./cmd/nova-bus -run 'TestSendBuilderRefusals\|TestShowRendersText\|TestRecvDualRead'` passes: every refusal writes nothing, prints one line on stderr and exits 2; `show` prints the text form; recv reads both forms (the writer switch is C30) | 1 | 2.5 |
| C13 | l1-pong-dual-read (**(rev)** reader first; was l1-pong-typed) | mas-bandwidth/nova-tools | `v1.3/comms` | `pkg/friend/machine.go`, `pkg/friend/machine_test.go`, `cmd/nova-friend/` | C10 l1-defs-envelope-pong-ack-beat-hook | heavy | `go test ./pkg/friend -run TestPongDualRead` passes: a legacy and a table pong read alike while `PongLine` still writes legacy; a fuzzed malformed pong of either form is refused by name in 100% of cases and never read as silence; a clamp is refused | 1 | 2 |
| C14 | l1-hook-stamp-typed | mas-bandwidth/nova-tools | `v1.3/comms` | `hook/` | C10 l1-defs-envelope-pong-ack-beat-hook | heavy | `go test ./hook -run TestHookStampTyped` passes: int64 ms stamps; a malformed stamp is refused by name, never silently failed closed | 1 | 1.5 |
| C15 | l1-seat-hook-typed-reader (**(rev)** moved to nova-tools `hook/`: nova-sprint's `internal/sprint/hook.go` is gone on `v1.3/comms`) | mas-bandwidth/nova-tools | `v1.3/comms` | `hook/` | C14 l1-hook-stamp-typed | heavy | `go test ./hook -run TestSeatHookTypedStamp` passes with both legacy JSON and typed stamps | 1 | 1.5 |
| C28 | l1-fixed-done-record-compacted-paged (**(rev)** new: the fixed-table dogfood, from the done-record tracer) | mas-bandwidth/nova-sprint | `v1.3/comms` | `schemas/store/`, `internal/sprint/store/donewire/`, `internal/sprint/store/done.go`, `internal/sprint/store/teardown.go`, `tla/DoneRecord.tla`, `tla/MCDoneRecord*` | C8 nova-sprint-schema-pin-and-import, C9 l1-tla-typed-frames | heavy | TLC on `tla/MCDoneRecord.cfg` passes with the layout version and `OlderBuildNeverWritesNewerLayout`, and a new `MCDoneRecordBrokenOverwriteNewer.cfg` fails on it; `go test ./internal/sprint/store -run TestDoneRecordFixedPaged` passes: compaction to one entry per (row, kind, card) keeps `CountDone` equal at every mark, every page holds at most 95, a 310-completion record round-trips, legacy JSON is read once and rewritten; `schemas/store/schema.lock` and `tables.baseline` are committed. If Glenn prefers, this card instead makes the done record a variable table and the fixed dogfood moves to another small, bounded server record | 1 | 3 |
| C30 | l1-writers-switch (**(rev)** new: writers switch only after readers adopted) | mas-bandwidth/nova-tools | `v1.3/comms` | `cmd/nova-friend/`, `cmd/nova-bus/`, `pkg/friend/machine.go` | C11 l1-bus-envelope-dual-read, C12 l1-nova-bus-reader-builder-and-show, C13 l1-pong-dual-read, C15 l1-seat-hook-typed-reader | heavy | `go test ./cmd/nova-friend ./cmd/nova-bus -run 'TestWritesTableForm\|TestRefusesNotFallsBack'` passes: pong and send write the table form and refuse (exit 2, one `REFUSED` line) instead of falling back to legacy; help names the table form; the card's report names the adopt that put C11-C15 on every seat | 1 | 1 |
| C16 | l1-acceptance-soak | mas-bandwidth/nova-sprint | `v1.3/comms` | `docs/research/schema-tables/L1-SOAK.md` | C11 l1-bus-envelope-dual-read, C12 l1-nova-bus-reader-builder-and-show, C13 l1-pong-dual-read, C14 l1-hook-stamp-typed, C15 l1-seat-hook-typed-reader, C28 l1-fixed-done-record-compacted-paged, C30 l1-writers-switch | pro | the report states, from logs of one full night: hand-parsed pongs 0, malformed frames refused N (each by name), legacy reads per form; Glenn signs Layer 1 off | 1 | 1 |
| C17 | l2-tla-card-contract-result-typed | mas-bandwidth/nova-tools | `v1.3/comms` | `tla/CardContract.tla`, `tla/MCCardContract*.cfg` | C16 l1-acceptance-soak | heavy | TLC on `tla/MCCardContract.cfg` passes; a new `tla/MCCardContractBrokenUntypedResult.cfg` fails on `ResultTypedBeforeReview` | 2 | 1.5 |
| C18 | l2-defs-card-jobpacket-result (**(rev)** moved to nova-tools) | mas-bandwidth/nova-tools | `v1.3/comms` | `schemas/card/`, `pkg/cardwire/` | C10 l1-defs-envelope-pong-ack-beat-hook, C16 l1-acceptance-soak, C29 nova-tools-strict-read-and-form-counter | heavy | `go test ./pkg/cardwire -run TestLayer2Golden` passes, including `CARD REFUSED: body over 16384 bytes`, both RESULT refusals, every `WorkVerdict` (6) and `RunEndKind` (7) value round-tripping, and an absent `outcome` refused | 2 | 1.5 |
| C19 | l2-brief-parse-parity (**(rev)** moved to nova-tools) | mas-bandwidth/nova-tools | `v1.3/comms` | `pkg/cardhdr/` | C18 l2-defs-card-jobpacket-result | heavy | `go test ./pkg/cardhdr -run TestCardTableParity` passes: every brief in the test corpus that today's parser admits is admitted by the table parse with equal fields (0 diffs), and every one it refuses is refused | 2 | 2 |
| C20 | l2-result-table-reader (**(rev)** moved to nova-tools; the builder is C22) | mas-bandwidth/nova-tools | `v1.3/comms` | `pkg/typedrec/cardresult.go` | C5 nova-schema-fault-corpus, C17 l2-tla-card-contract-result-typed, C18 l2-defs-card-jobpacket-result | heavy | `go test ./pkg/typedrec -run TestResultTableFaultCorpus` passes: 100% of `testdata/schema-faults/result/*` classed malformed are refused with a named refusal; both historical shapes transcode; no output contains "shape fault" | 2 | 2 |
| C21 | l2-friend-report-gateway (**(rev)** moved to nova-tools) | mas-bandwidth/nova-tools | `v1.3/comms` | `pkg/friend/outbox.go` | C20 l2-result-table-reader | heavy | `go test ./pkg/friend -run TestOutboxGatewayTyped` passes: a REPORT.md without `Verdict:` is refused by name at the gateway and is never left unread | 2 | 2 |
| C22 | l2-nova-work-result-builder (**(rev)** was l2-nova-tools-result-sync) | mas-bandwidth/nova-sprint | `v1.3/comms` | `cmd/nova-work/`, `go.mod`, `go.sum` | C8 nova-sprint-schema-pin-and-import, C11 l1-bus-envelope-dual-read, C21 l2-friend-report-gateway | heavy | go.mod requires a nova-tools version containing C21; `go test ./cmd/nova-work -run TestResultBuilderRefusals` passes: every refusal writes nothing, prints one stderr line and exits 2; an `outcome` outside `WorkVerdict`'s six is refused | 2 | 1.5 |
| C27 | l2-signoff-fence (**(rev)** new: the Layer 2 sign-off the first draft lacked) | mas-bandwidth/nova-sprint | `v1.3/comms` | `docs/research/schema-tables/L2-SIGNOFF.md` | C17 l2-tla-card-contract-result-typed, C18 l2-defs-card-jobpacket-result, C19 l2-brief-parse-parity, C20 l2-result-table-reader, C21 l2-friend-report-gateway, C22 l2-nova-work-result-builder | pro | the report states, across one full stream: shape-fault blames 0, brief parity diffs 0, C17's TLC result, legacy result reads per form; Glenn signs Layer 2 off. **C23 and C26 do not start before this card lands** | 2 | 0.5 |
| C23 | l3-defs-and-refusals-whole (**(rev)** moved to nova-tools: the refusal and finding types are printed by both repos) | mas-bandwidth/nova-tools | `v1.3/comms` | `schemas/refusal/`, `pkg/refusalwire/`, `pkg/typedrec/tablerefusal.go`, `pkg/nsprint/verbflag/` | C27 l2-signoff-fence, C18 l2-defs-card-jobpacket-result, C21 l2-friend-report-gateway | heavy | `go test ./pkg/nsprint/verbflag -run TestEveryVerbJSONRefusalWhole` passes: every verb under `--json` prints a refusal as one complete object on stdout, and a refusal cut at every byte offset is detected (the dispatcher's package is to be confirmed at take; `verbflag` is where nova-sprint's verbs import their flags from) | 3 | 3 |
| C24 | l3-judgment-verdict-field | mas-bandwidth/nova-sprint | `v1.3/comms` | `schemas/sprint/judgment.schema`, `internal/sprint/judgmentwire/`, `internal/sprint/notes.go`, `internal/sprint/verdicts.go`, `tla/JudgmentRetire.tla`, `tla/MCJudgmentRetire*` | C8 nova-sprint-schema-pin-and-import, C23 l3-defs-and-refusals-whole | frontier | `go test ./internal/sprint -run TestFinishedAsReadsVerdictField` passes and TLC on the JudgmentRetire config passes with the typed-verdict invariant; its broken config fails on it | 3 | 2.5 |
| C25 | l3-subject-census-and-rewrite-fuzz | mas-bandwidth/nova-sprint | `v1.3/comms` | `pkg/friend/alive.go`, `pkg/friend/presence.go` | C24 l3-judgment-verdict-field | heavy | `go test ./pkg/friend -run TestNoVerdictFromSubject` passes: every subject in the corpus reworded at random loses no verdict, card or kind | 3 | 1.5 |
| C26 | l5-retire-legacy-result-parser (**(rev)** moved to nova-tools) | mas-bandwidth/nova-tools | `v1.3/comms` | `pkg/typedrec/cardresult.go`, `pkg/friend/outbox.go` | C27 l2-signoff-fence, C20 l2-result-table-reader, C21 l2-friend-report-gateway, C25 l3-subject-census-and-rewrite-fuzz | pro | the evidence file shows zero legacy result reads on the per-form counter for one adopt cycle after the writers switched; then `go test ./...` passes and `grep -n 'shapeFault' pkg/typedrec/cardresult.go` prints nothing | 5 | 1 |

**The first cards (rev).** No decision is needed for C1, C2, C3, C5, C6 and C9; C6 is the first nova card
because every generated package waits on its CI exemption and check. Then C7, C8 and C29 complete Layer 0.
In Layer 1, C10 is first, and C28 can run beside it once C8 and C9 land.

Totals (ESTIMATED): schema-side 6.25 wd (C1-C4); Layer 0 nova 6.5 (C5-C8, C29); Layer 1 18 (C9-C16,
C28, C30); Layer 2 11 (C17-C22, C27); Layer 3 7; Layer 5 1; about 50 worker-days in all (49.75). The first
draft's ~45 counted a two-repo copy (C8, C22 as sync cards) that the import removes. This revision adds
the reader-first split (C30), the strict-read helper (C29), the fixed dogfood (C28) and the Layer 2
fence (C27). This is OPINION on top of the inputs' ESTIMATED figures.

Shared paths and their needs edges (SPEC-CARD-CONTRACT shared-path rule) **(rev)**: nova-tools `schemas/`
C6 -> C10 -> C18 -> C23; `Makefile` C6 -> C7 (nova-tools), C8 alone (nova-sprint); nova-sprint `go.mod`
C8 -> C11 -> C22 (each bumps the nova-tools require); `hook/` C14 -> C15;
`pkg/friend/machine.go` C13 -> C30; `cmd/nova-bus/` C12 -> C30; `pkg/typedrec/cardresult.go` C20 -> C26;
`pkg/friend/outbox.go` C21 -> C26; `docs/VERSIONING.md` C2 -> C4; `make/go.mk` C1 alone now that C3 is
docs only; nova-sprint `internal/ci/` C8 alone; `tla/` in nova-sprint C28 and C24 touch different modules.

## 7 Ranked R&D plan

Ranked by reliability gain per unit of effort, best first (OPINION ranking; costs are the inputs'
ESTIMATED figures, a range where they differ). 36 input experiments merge to 24: true duplicates are
merged and named; close ones are kept and cross-referenced. Nothing below was built or run, except
where marked **(rev)**: the two tracers of 2026-10-15 ran parts of R6 and R16.

| Id | Question | Hypothesis | What would be built (described) | Measurable outcome | Cost (wd) | Dead end | Unblocks | Proposers (S ids) |
|---|---|---|---|---|---|---|---|---|
| R1 | Which recorded incidents were malformed (schema can refuse) and which were wrong (it cannot)? | most RESULT/refusal/pong faults are malformed | the fault corpus of C5, each item classified with its source | share malformed vs wrong per class; corpus size | 1 | under 30% malformed: schema's ceiling is low, re-scope | R2, R4, R5, C20 | U-new; feeds stella idea (publish corpus); emma S2 and aurora S1 assume such a corpus |
| R2 | Does a typed result refuse the recorded fault class at the sender? (merged: same question, same corpus) | 90-100% refused at the writer | `Result` codec + builder check replayed over R1's result faults | share refused at sender with a named refusal; **(rev, restored from emma S2)** encode/decode latency in microseconds and wire bytes vs text RESULT.md (emma ESTIMATED ~1.2 us decode, ~650 B vs ~600 B) | 1-2 | any historical fault validates (aurora) / under 50% (stella) / work and read results cannot share one table (emma: split into a union) | Layer 2 | emma S2, aurora S1, stella S1 |
| R3 | Cold AI from help alone (merged) | at least 90% valid on the first try, 100% by the second | builder prototype + help page, 30 cold prompts across tiers | first-try and second-try valid rate vs hand-writing | 0.5-3 | below 50% (aurora) / under 95% within 2 turns (emma) / no better than hand-writing (stella) | the build-CLI design for every layer | emma S1, aurora S2, stella S2 |
| R4 | Overturn A: would strict JSON (registry, `DisallowUnknownFields`, same builder) give the gain? (merged: the same alternative) | schema wins on forward compatibility and compile-time evolution locks | both designs over 20 fault classes incl. v2->v1 reads and retain round trips | classes caught per design; lines of hand validation; per worker-day score; **(rev, restored from emma S4)** bytes on the wire and decode cost (`ns/op`, `allocs/op`) over 10,000 messages per design | 1-2 | JSON catches 95% at under 25% of the cost (emma) / 80% at under 30% (aurora): recommend JSON | the go/no-go | emma S4, aurora S10 |
| R5 | Overturn B: would a strict text lint on today's format match the typed refusal rate? | a gap of more than 5% for the wire | hardened text parser + lint over R1's corpus | refusal-rate gap vs R2 | 3 | gap under 5%: withdraw the wire recommendation | the go/no-go | stella S12 (kept apart from R4: a different alternative) |
| R6 | Mixed-version fleet (merged) | variable reads both ways; fixed reads old->new and refuses new->old with `layout_newer` | two builds of a packet and a beat exchanging through simulated adopt steps | pass/refuse matrix of 4 pairs x 2 forms; misdecode count 0. **(rev) Partly run by the tracers**: a newer variable writer's unknown field is skipped and counted (pong); fixed v2 reads v1 once locked and v1 refuses v2 as `layout_newer` (done record); the 4x2 matrix through adopt steps is still owed | 1-3 | any silent misdecode; or adopt cannot order fixed readers first: variable only in v1.3 | D1, every layer | emma S3, aurora S3, stella S4 |
| R7 | Brief parse parity (merged) | the upstream Card parse admits exactly today's admitted briefs | corpus test through Parse -> Save -> Load -> Render | parity diffs; oversize refused at admission; **(rev, restored from emma S7)** bytes and microseconds per card parse vs `pkg/swarm/lintheader.go` | 1.5-2 | any silent admission change; unknown keys must first move out of `extra` | C19 | emma S7, aurora S5 |
| R8 | Do refusals come out whole under `--json` on every verb? (merged) | 0 unparsable refusals | a `Refusal` renderer behind the verb dispatcher | count of verbs refusing `--json` or printing prose; unparsable count | 1.5-2 | a verb streams unbounded output: use batch framing for it (emma) | C23 | emma S6, stella S7 |
| R9 | Is a cut-off typed refusal always detected? | 100% at every truncation point | sample refusal truncated at every byte | detection rate | 0.5 | any silent accept | C23 | aurora S6 (close to R8, kept: framing, not rendering) |
| R10 | Text rendering round trip (merged) | the text form loses nothing for the top types | `show` renderer over recorded messages | field-loss diffs (target 0) | 1-2 | any field invisible to humans | Layers 1-3 acceptance | aurora S9, stella S11 |
| R11 | Subject census: what scrapes subjects today? | a short list | grep and read every subject consumer | count of call sites; consumers that cannot migrate | 1 | a consumer that cannot migrate | C25 | aurora S7 |
| R12 | Do verdicts survive subject rewording once typed? (merged) | 0 verdict loss under rewording fuzz | judgment envelope pilot + rewording fuzz; envelope + receipt prototype | verdict-loss count; ambiguous kinds refused at send; **(rev, restored from emma S10)** Redis memory per receipt (emma's 120 B `BusReceipt` vs today's `bus2:rec:<id>` hash overhead) | 2 | any verdict recovered from subject text (stella) / humans omit card refs: require them only for judgment kinds (emma) | Layers 1, 3 | stella S8, emma S10 |
| R13 | Typed pong ends malformed-pong-as-silence? | false-negative events from malformed pongs drop to 0 | `Pong` codec in one member | ambiguous liveness events in test | 2 | malformed pongs still read as silence | C13 | stella S3 |
| R14 | Corpus in nova CI: what does it cost? | minutes per change | the conformance run in nova CI | CI minutes added | 1 | over 10 minutes | C7 | aurora S8 |
| R15 | Oversize refused early enough? (merged) | sender refuses; drain never stalls | declared bound + sender check on a staged oversize note | drain-stall reproductions (target 0) | 0.5-1 | a path still freezes | Layer 2; partly settled for briefs by F7 | aurora S12, stella S9 |
| R16 | Pong wire cost (merged) | within 2x text bytes; negligible CPU | fixed and variable pong vs text in a test harness | bytes and microseconds per pong, and **(rev, restored from stella S6)** CPU at fleet pong rate; **(rev) partly measured by the pong tracer**: the variable pong is 69 wire bytes vs a 37-byte legacy line (101 with base64; 2.7x), CPU not measured | 1 | over 5x bytes or CPU (stella) / slower or larger (aurora) | the fixed-vs-variable choice for pong | aurora S4, stella S6 |
| R17 | Retain-unknown across a rollback | downgrade, save, upgrade keeps new fields | variable record through v2 -> v1 edit -> v2 | fields kept vs `retain_lost` | 2 | silent loss | adopt safety; close to R22 | aurora S11 |
| R18 | Pong offload from the bus | moving pongs off `bus2:log` removes >75% of bus volume | pong frames on the beat/push channel vs bus messages | stream entries and bytes; **(rev, restored from emma S5)** Redis memory, bytes per beat/pong (96 B vs ~450 B stream entry, ESTIMATED) and CPU in microseconds per 1,000 beats | 1 | session check still needs a bus entry: keep a one-key ring per friend | D9 | emma S5 |
| R19 | Rework proof by hash | `TaskHash` + `rework_fix_hash` remove keyword false negatives | extended Card, 20 reworked cards | false-negative rework rejections (target 0%) | 1.5 | `TaskHashStable` breaks: keep the fix hash separate | a schema-side Card change | emma S8 |
| R20 | Unify the hook stamps | one stamp ends RFC3339 drops and push replays | trace-driven test against `Hook.tla`/`SeatHook.tla` states | shapes 4 -> 1; parse drops 0; **(rev, restored from emma S9)** bytes (136 B) and decode time (emma ESTIMATED < 50 ns) vs JSON stamps | 1.5 | unbounded `acked` history: keep it in a Redis set | C14, C15 | emma S9 |
| R21 | Typed results end to end | both historical shapes refuse or read, never blame | result reader shim in the member | fault-blame messages after cutover (target 0) | 3 | workers route around the shim | C21, C26 | stella S5 (close to R2, kept: end to end, not at the sender) |
| R22 | Config file and notes keep unknown fields across versions | v1 edit of a v2 file keeps v2 fields | `ConfigStoreFile` and `SprintNote` round trip | preservation 100%; heuristic fallbacks 0 | 2 | a signature breaks on retained bytes: hash known + retained nodes | Layer 4 (deferred) | emma S12 (close to R17) |
| R23 | Receipts as tables end billing by prose? | consumer rows reconcile exactly | `Consumer`-style receipt pilot | reconciliation mismatches | 4 | mismatches from non-wire causes | Layer 4 (deferred) | stella S10 |
| R24 | Replace the 851-line manifest validator | the compiled manifest passes every existing test in under 80 lines of adapter | `BatchManifest` against `manifest_validate_test.go` | pass rate; lines removed; **(rev, restored from emma S11)** decode microseconds and bytes vs raw JSON | 2 | Lua needs raw JSON: validate in Go and emit canonical JSON | Layer 4 (deferred) | emma S11 |

### Critical path: the first three, in order

1. **R1, the fault census (1 wd).** Decides whether schema's ceiling is worth the ~50 worker-days (rev): if most
   recorded faults are *wrong* rather than *malformed*, no wire fixes them and the plan shrinks to the
   build CLI and the counters. It also builds the corpus R2, R4 and R5 replay.
2. **R2 with R4 and R5 run on the same corpus (about 3-5 wd together).** Decides the wire: typed tables,
   strict JSON, or a strict text lint, scored on the same faults. If R4 or R5 meets its overturn threshold,
   Glenn gets an honest no before Layer 0 is built.
3. **R6, the mixed-version matrix (1-3 wd).** ~~Decides D1~~ **(rev)** D1 is settled by Glenn; R6 now tests the
   ruling (fixed only where the reader is never older) on two builds before C28 ships, and confirms the adopt order. R3 (cold AI) runs next, before the first builder
   card.

### What I could not settle

- Every incident count and the nature of the "empty kind" and the "oversize note" are REPORTED from the
  research brief; no bus log was available to any input or to me (UNVERIFIED). R1 exists for this.
- Whether schema's fixed-table work (`sprint/schema-vt-core`) is on main: the ROADMAP says not, a merge
  branch dated 2026-10-12 exists (UNVERIFIED).
- Schema issue #441's current state: only the VERSIONING page's mention was read (UNVERIFIED).
- ~~Whether pong nonces are numeric or hex text.~~ **(rev) Settled by the pong tracer**: `cmd/nova-friend` draws 6
  characters from `[a-z0-9]`, or 6 clock digits: text, `string(32)`.
- ~~Whether `sprint/v1.3-schema` is the right base.~~ **(rev)** It does not exist; cards now name `v1.3/comms`
  (section 6). Whether to cut a dedicated stream base from it is the coordinator's call.

### UNVERIFIED claims that matter to the plan

- ~1,700 messages and >1,300 pongs in one night (drives D9 and R18).
- The truncated `lane-fault refusal={` path (drives C23; F10 shows the table-refusal path is not it).
- Emma's size and speed figures (`< 50 ns`, 3-5x faster, 96-752 B frames): ESTIMATED, not measured.

### What I would do next

~~Take D1, D2, D3 to Glenn; run R1; admit C1, C2 and C5.~~ **(rev)** D1 is settled (both forms) and D2's
dependency question is settled (nova-sprint imports nova-tools; generated Go is stdlib-only). Take the
remaining choices to Glenn: D2's home rule (shared types in nova-tools, single-repo records beside their
consumer), D3 (v1.3 scope), and C28's shape (compacted and paged fixed, or variable). Run R1. Admit C1, C2,
C3, C5, C6 and C9, none of which waits on a decision, while Layer 0 is settled with him.

## 8 Revision 2026-10-15

Card `schema-adoption-union-fixes.w1` (gen 1, epoch 21), worked by Rowan (friend.rowan) on branch
`sprint/schema-adoption-union-fixes.w1.g1.e21` from `origin/sprint/v1.3-research` (`ae5f858c`, which holds
this file as landed from `0799ded0`). This is a document edit only: nothing was built or run. Sources
opened for the revision: nova-sprint `v1.3/comms` at `5dea8434` (go.mod) and `dfae414b` (tree, imports);
nova-tools `v1.3/comms` at `c435e3fa` (tree); the schema repo's branch list; and the two tracer findings
(nova-tools `9ef91022`, nova-sprint `d53dd1a4`). **Both tracers' findings were on origin when this card
started**, and both verdicts are cited (section 3).

### The five reader fixes

1. **R3/R6 and R1 help references.** The first draft said fixed tables wait for "R3 passes" in D1, in the
   section 4.2 pong comment, in 4.5 item 1 and under section 6 "Fixed tables". The mixed-version test is
   **R6**; R3 is the cold-AI test. It also said "R1 decides whether the help is enough" (section 4.8) and
   cited R1 as the cold-AI test in agreement 4 (section 3). That is **R3**; R1 is the fault census. All six
   references are corrected.
2. **The Layer 2 sign-off fence.** Layer 1 had a sign-off card (C16) and Layer 2 had none, so C23 and C26
   could start on an unsecured Layer 2. Added **C27 `l2-signoff-fence`**, which needs C17-C22 and ends with
   Glenn signing Layer 2 off. C23 and C26 now need C27, and the Layer 2 acceptance cell says so.
3. **WorkVerdict restored.** Section 4.3 had replaced emma's `WorkVerdict {ok, not_done, nothing, broken, hold, fail}` with a five-value `RunEnd`, which lost the run outcome and `budget_unverifiable`. Restored
   `WorkVerdict` by name as the Result's `outcome`, `RunEndKind` with all seven values, `GateRoute`, and the
   cost and usage fields. The judgment `Verdict` stays as a separate optional field (D5). C18 and C22 test
   every value.
4. **The receipt-memory, CPU and decode measures.** The merge of experiments had dropped measures the inputs
   named. Restored: Redis memory per receipt (emma S10, in R12); CPU (emma S5 in R18, and stella S6's
   CPU at fleet rate in R16); and decode latency, bytes and allocations (emma S2 in R2, S4 in R4, S7 in R7,
   S9 in R20, S11 in R24).
5. **Every C card's dependency slugs and actual base.** The card table gains a Base column. Every Needs
   cell now names id and slug. The bases are real branches: `v1.3/comms` in nova-sprint and nova-tools, and
   `sprint/schema-go`, `sprint/schema-shared` and `sprint/schema-cards` in schema. The first draft's
   `sprint/v1.3-schema` does not exist, and its nova-tools base `sprint/v1.3-tools` is not the line the
   tracers used. Cards whose files moved to nova-tools on `v1.3/comms` changed repo: C5, C10, C15, C18,
   C19, C20, C21, C23 and C26. The shared-path edges were recomputed.

### The owner's rulings (Glenn, 2026-10-15)

- **Both forms, both repos, dogfooded**: fixed for records whose reader is never older (server-owned
  store records), variable for messages an older reader may meet. D1 is marked settled, section 4.5 is
  rewritten, and the section 6 scope adds C28 (the fixed done record, compacted and paged, from the
  tracer) to Layer 1. The M7, M8, M9 and M17 fits are updated.
- **Returning friends simply update**: no offline-compatibility policy and no long dual-emit windows. D10,
  section 4.5 item 3, the cutover paragraph and Layer 5's acceptance are rewritten around a short
  readers-first window and the per-form read counter. The epoch-plus-TTL wait is dropped.
- **D2 and its premise corrected**: nova-sprint's go.mod at `v1.3/comms` `5dea8434` requires nova-tools.
  D2, F3, U74, U157 and section 4.7 are corrected. C8 and C22 changed from copy-and-sync cards into an
  import card and a builder card. Option (b) and C3 as a module card are withdrawn.
- **Full nova adoption is the bar** before the table forms are offered to other people (sections 4.5 item
  6 and 6 scope).
- **Every adoption card carries a "Schema improvements wanted" section** (section 6 preamble). The
  tracers' 25 items are listed as schema-side inputs (section 3).

### The tracer findings folded in

- Both verdicts cited: pong **SHOWED**; done record fixed **SHOWED**, but the done record is unbounded and
  does not fit, at most 95 entries under the 64 KiB body (section 3 table).
- Lessons carried into sections 3, 4 and 6:
  - the `go:generate` pin, with no go.mod change (4.7, C6, C8);
  - stdlib-only generated Go (D2, U158, C3);
  - readers before writers in a short window (4.5, C13 and C30 split reader from writer);
  - absent reads as zero, and the runtime clamps, so the facade refuses clamps and validates before
    writing (4.3, 4.6, C29, the C9 invariants);
  - the fixed form only for small, bounded, server-owned records (D1, 4.5, C28);
  - the per-form read counter (D10, C29, Layer 5);
  - lock before edit (4.5 item 2, C28);
  - the CI exemption for generated files before the first generated package (4.7, C6).
- Section 4.2's pong takes the tracer's uint32 fields and settles the nonce question (text, 6 chars
  today). The R6 and R16 rows record what the tracers measured.

### Cards added, changed or withdrawn

- **Added**:
  - C27 `l2-signoff-fence`;
  - C28 `l1-fixed-done-record-compacted-paged`;
  - C29 `nova-tools-strict-read-and-form-counter`;
  - C30 `l1-writers-switch`.
- **Renamed or re-scoped**:
  - C3 `schema-go-adopter-recipe` (was `schema-go-adopter-module`);
  - C6 `nova-tools-schema-home-and-pin` (was `nova-schema-home-and-pin`, nova-sprint);
  - C8 `nova-sprint-schema-pin-and-import` (was `nova-tools-novawire-sync`);
  - C12 `l1-nova-bus-reader-builder-and-show`;
  - C13 `l1-pong-dual-read` (was `l1-pong-typed`);
  - C20 `l2-result-table-reader` (was `l2-result-table-and-builder`);
  - C22 `l2-nova-work-result-builder` (was `l2-nova-tools-result-sync`).
- **Withdrawn**: none; C3's module plan is replaced by a docs recipe.
- **Counts**: 30 cards, about 50 worker-days (ESTIMATED).

### Not changed, and why

- **The ledger stays at 183 rows.** It indexes the three input files; the tracers are not inputs to the
  merge. Their findings live in section 3 and in the rows they amend (U74, U157, U158).
- **The 24 experiments keep their ids.** Their outcomes gained the restored measures.

### Second reader's fixes (attempt 2, card `schema-adoption-union-fixes.w2`)

An independent reader passed the five fixes above and found two errors in this file at `22a80181`.

1. **Enums do have an unset zero (section 4.3).** The first revision said the enums carry no "unset"
   zero value. False at schema `4d9941fb`: the Go emitter writes `<Enum>None = 0`
   (`internal/codegen/golang/golang.go:240-245`), and the table codec writes None as id zero and reads an
   unknown id as None (`internal/codegen/gotable/codecs.go:287-306`). The argument is corrected: a
   non-optional `WorkVerdict` can refuse None for an absent or unknown outcome, while a numeric field
   such as the pong's `queue = 0` has a legitimate zero and needs a presence check or schema `required`.
2. **The oracle warning is emma's (D1).** D1 said the Go fixed-table oracle and hostile-input gate was a
   limit emma did not state. Emma's §6.2 item 1 states it explicitly (U141; `emma.md` line 792 on
   `sprint/v1.3-research`); neither aurora nor stella raises it. D1 now credits emma and keeps U161
   (readers-first holds only friend-to-server) as the one limit she did not state.

### Not done

- No source was rebuilt or rerun; the tracer figures are theirs.
- C23's verb-dispatcher package is named "to be confirmed at take".
- Whether the coordinator cuts a dedicated stream base from `v1.3/comms` is not decided here.
- R1's fault census is still the first experiment and is still unrun.
