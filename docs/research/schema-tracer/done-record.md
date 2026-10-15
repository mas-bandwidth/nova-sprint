VERDICT: SHOWED (the fixed table ran end to end once; it also showed the done record, as shaped today, does not fit the fixed form: see "The bound")

# Tracer: the done record as a FIXED table

Card schema-tracer-done-record-fixed.w1 (attempt 1, gen 1, epoch 21), 2026-10-15. Tracer
bullet: one pass, end to end, then stop. What is kept is what it taught; the code is
evidence, not the product, unless the owner decides otherwise. Sibling (the VARIABLE half):
schema-tracer-friend-pong in nova-tools. When this pass started, the sibling branch
`sprint/schema-tracer-friend-pong.w1.g1.e21` did not exist on mas-bandwidth/nova-tools
(`git ls-remote` returned nothing), so this pass pinned schema main itself.

## What ran

| step | what | where |
|---|---|---|
| pin | mas-bandwidth/schema `4d9941fbc7fd420b560739e48256d5b5e2892cdc` (main, 2026-10-14, "Merge pull request #1908"); Go pseudo-version `v2.5.1-0.20261014163537-4d9941fbc7fd` | `internal/sprint/store/donewire/generate.go` |
| define | `fixed table Completion`, `fixed table DoneRecord` (`done [..95]Completion`) | `schemas/store/done.schema` |
| generate | `go generate ./internal/sprint/store/donewire/...` runs `go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.1-0.20261014163537-4d9941fbc7fd generate --lang go --out . ../../../../schemas/store` | `internal/sprint/store/donewire/*.go` (generated, committed) |
| v2 fixture | the same schema plus `wrote int64` appended, locked over v1 (`schema lock .` twice: v1, then v2), generated the same way | `internal/sprint/store/donewire/v2/` |
| wire it | `encodeDoneRecord` / `decodeDoneRecord` behind `keepDone` and `doneRecordOf` (same signatures as before) | `internal/sprint/store/done.go` |
| test | round trip, v2-reads-v1, v1-refuses-v2, hostile images, legacy JSON, the model's invariants through real ticks | `internal/sprint/store/done_schema_test.go` |

Reproducibility: the committed output is byte-identical to the output of a compiler built
locally from a clone at the same SHA (`go build ./cmd/schema`, then the same `generate`), so
the two build paths agree. Generation needs the network (go run fetches the module); the
build does not. It runs under `GOFLAGS=-mod=readonly` and touches no go.mod.

## The definition

```
fixed table Completion
{
    kind string(16)    // sprint.DoneKinds: "work", "fix", "read" today; room for a registered kind
    card string(515)   // sprint.ValidCardID: 4 dot-joined ValidID words of MaxIDLen 128: 4*128+3
    row  string(135)   // a member (ValidID, 128) or "friend." + a ValidID (sprint.FriendRow): 7+128
    at   int64         // Unix nanoseconds UTC; 0 is the zero time
}

fixed table DoneRecord
{
    epoch  uint64
    cursor string(41)  // a Redis stream id "<ms>-<seq>", two uint64 in decimal: 20+1+20
    done   [..95]Completion
}
```

Each text bound is the code's own (`internal/sprint/schema.go` MaxIDLen, ValidCardID,
`sprint.FriendRow`); nothing narrower is enforced anywhere, so nothing narrower can be
declared without a write the form refuses.

### The bound

The done list has no bound in the code: it is every completion of the epoch, plus the
duplicates a retried or stale tick adds (tla/DoneRecord.tla, StaleWrite). The fixed form's
record body ceiling is 65536 bytes (schema SPEC-TABLES §3.4), and at the code's text bounds a
Completion is 686 bytes, so the most a record holds is 95 (8 + 45 + 4 + 95 * 686 = 65227).
`[..256]` was tried first: the compiler warns and stops emitting the fixed form for the table
(74816 bytes, "keeps form 1").

Today's data is past it. `nova-sprint where` at 12:15Z showed 310 counted completions since
the stats reset (friends 82, fleet 228), and the record holds every completion since epoch 21
began, with duplicates. (I could not read the live `donerecord` key itself: redis-cli in this
sandbox fails to connect with `setsockopt(TCP_NODELAY): Invalid argument`.) So the record the
sprint server holds right now would be written in the legacy JSON form by this code, every
tick. The fixed path is correct and tested, and today it would not be taken.

## Cost against today's encoding

Bytes stored (`encodeDoneRecord`, records shaped like today's: a read card id, a member row):

| completions | fixed image | JSON |
|---|---|---|
| 0 | 65,412 | 39 |
| 3 | 65,412 | 464 |
| 95 | 65,412 | 13,327 |
| 96 | (JSON: over_max_count) | 13,467 |
| 310 | (JSON: over_max_count) | 43,613 |

The fixed image is the file form: form byte, 7 reserved, the layout hash (16), the layout
behind its u32 length (4 + 157), then one record (8 + 65,227). It is the same size whatever
the record holds: every byte of declared slack is written and stored. The where exchange that
reads this key reads 65 KB every tick and every `where`.

Decode and encode, `go test -run '^$' -bench DoneRecord -benchtime 2000x ./internal/sprint/store/`
(Mac Studio, Go 1.27.2):

```
BenchmarkDoneRecordDecode/fixed/3-32     2000   81609 ns/op  119205 B/op   19 allocs/op
BenchmarkDoneRecordDecode/json/3-32      2000    1849 ns/op    1239 B/op    8 allocs/op
BenchmarkDoneRecordDecode/fixed/95-32    2000   93170 ns/op  133248 B/op  295 allocs/op
BenchmarkDoneRecordDecode/json/95-32     2000   45963 ns/op   41179 B/op  105 allocs/op
BenchmarkDoneRecordEncode/fixed/3-32     2000   22470 ns/op  131072 B/op    2 allocs/op
BenchmarkDoneRecordEncode/json/3-32      2000     850 ns/op     579 B/op    3 allocs/op
BenchmarkDoneRecordEncode/fixed/95-32    2000   25823 ns/op  131072 B/op    2 allocs/op
BenchmarkDoneRecordEncode/json/95-32     2000   20745 ns/op   13824 B/op    3 allocs/op
```

The decode path: JSON is reflection over the used bytes. The fixed path is header and
reserved-byte checks, the layout compared byte for byte against the known lineage entry, a
per-call holes computation and default fill over a 66 KB Go struct, the plan (383 entries)
copying the whole body, the clamp pass, and then this store's copy out of fixed arrays into
`[]sprint.Completion` (one string per text field). For a small record the fixed form is about
44 times slower and 100 times the allocation, because the record is mostly declared slack.
The nova-sprint binary grows 248 KB (40,787,314 to 41,035,298 bytes).

What it gave: named refusals at load for every hostile image tried (below), a layout identity
on every image, an older build that refuses a newer layout by name instead of reading it as
garbage and overwriting it, and a schema-checked definition of the record's bounds.

## Versioning, in practice

- v2 (a field appended: `wrote int64`) reads a v1 image silently: every v1 field lands, `wrote`
  takes its default, the report is all zero (`TestDoneRecordV2ReadsAV1Image`). That needed v2's
  `schema.lock` to hold v1's layout, which means v1 had to be locked BEFORE the edit: `schema
  lock` on v1, edit, `schema lock` again, then `generate`. Without the lock, generate succeeds,
  and the v2 build would refuse every v1 image as `layout_newer`.
- v1 refuses a v2 image as `layout_newer`, reads nothing, and this store refuses the tick
  loudly rather than write its older layout over the newer record; `where` reads the log whole
  meanwhile (`TestDoneRecordV1RefusesAV2Image`).
- The lock binds the schema `package` name, and the Go package is the schema package, so the
  v2 fixture is a second package also named `donewire`, imported as `donewirev2`.
- The old form is detected by its first byte: `{` is the legacy JSON, read for one release;
  byte 3 is the fixed form (form registry, SPEC-TABLES §3). The two never collide.

## Refusals, by name

`decodeDoneRecord` returns the schema runtime's name where it gives one (`report.Reason`), and
this store's name where the runtime gives only a flag or a counter:

| hostile image | refusal | named by |
|---|---|---|
| empty value | `empty` | store |
| truncated record, truncated header, a reserved byte set | `malformed` | store (the runtime sets `Malformed`, no name) |
| truncated layout, a layout byte flipped | `layout_malformed` | schema |
| a file with the layout but no record | `record_count` | store |
| two records | `batch_too_large` | schema |
| the file's layout hash flipped | `layout_newer` | schema (indistinguishable from a newer writer) |
| a record's hash flipped | `no_layout` | schema |
| form byte 1 / 2 / 9 | `previous_form` / `message_form_as_file` / `newer_form` | schema |
| the done count past 95 | `clamped` | store (the runtime clamps and counts) |
| a text length past its bound, zero slack | `text_ill_formed` | schema |
| a text length past its bound, text in the slack | `clamped` | store |
| a text byte not UTF-8 | `text_ill_formed` | schema |
| a v2 image | `layout_newer` | schema |
| broken legacy JSON | `legacy_json_unreadable` | store |

On write, a record the fixed form cannot hold keeps JSON, named in the form string:
`json:over_max_count`, `json:text_over_bound`, `json:text_ill_formed` (a NUL),
`json:time_out_of_range` (outside int64 nanoseconds). The generated writer refuses none of
these itself.

## The model (tla/DoneRecord.tla)

The record's semantics did not change, so NoDoubleCount, CaughtUpIsTruth, ResetShowsZero and
ClearStartsAgain hold as before: the store still appends entries and counts with
`sprint.CountDone`'s dedupe. `TestDoneRecordFixedKeepsTheModel` drives real ticks on the Mem
store with the record in the fixed form: caught up, `DoneSince` is the truth (9: three work
cards, two reads each); an older image written back (StaleWrite) catches up to the same
truth; a retried tick (the caught-up entries under the older cursor) reads every line again,
the record holds them twice (18 entries), and the count is unchanged. The round-trip test
checks `CountDone` reads the same off the decoded record at a zero and a non-zero mark.

One behaviour is new and not in the model: an older build meeting a newer layout refuses
the tick instead of rewriting the record. The model has no version; it is owed (below).

## The go.mod and import story for nova-sprint

Nothing changed in go.mod. The generated Go imports only the standard library
(`encoding/binary`, `errors`, `math`, `unicode/utf8`, `unsafe`); there is no runtime module to
require, and the compiler is not a dependency of the build: it is pinned in the go:generate
line and fetched by `go run` when someone regenerates. The answer to the plan's D2 from this
pass: the definition lives at the repository root under `schemas/<area>/`, the generated Go
lives beside its one consumer (`internal/sprint/store/donewire/`) and is committed, and the
lock must live beside the definition (below), so it belongs in `schemas/<area>/` too.
The generated files skip the deadcode tool (they are marked generated), so the 300 KB of
unused surface does not land on a dead-code ledger.

## Failures met, as candidate invariants for the layer model

1. **The record fits its form.** A fixed table holds at most its declared max; a record whose
   data has no bound is not a fixed table. Today's done record breaks this (310+ completions
   against 95), so this invariant decides the real design, not a bound to raise.
2. **No clamp reaches a count.** A read that clamps a count or a length is refused, never
   used: a clamped done list would undercount silently (`clamped`).
3. **A refusal reads nothing.** Every refusal returns the zero record; nothing partial lands.
4. **An older build never writes over a newer layout** (`layout_newer` stops keepDone).
5. **The lineage is locked before the edit.** Every layout a reader must accept is in the
   lock beside the schema; a layout committed without its lock cannot be read back later.
6. **The writer emits only what its own reader accepts.** The generated writer does not check
   bounds or text; the store checks before Save, and a record it cannot hold keeps JSON.
7. **Form detection is total and disjoint** during the one-release window: `{` or byte 3.

## What the real cards must do differently

- Do not make the done record a fixed table in its current shape. Either make it a variable
  table (an unbounded `[]Completion`, the sibling's form), or reshape it to fit:
  compact to one entry per (row, kind, card) keeping the latest time, which preserves
  `CountDone` exactly for every mark (a key counts after `since` iff its latest time is after
  `since`) and removes the StaleWrite duplicates, then page it across fixed records of at most
  95 entries. Paging needs new keys, so it touches `teardown.go`'s machine keys and the model.
- Commit `schemas/store/schema.lock` and a `tables.baseline` with the first layout (proposed
  diff below); generate does not insist.
- Extend tla/DoneRecord.tla with the layout version (a newer build's record and an older
  build's tick) and the invariant that the older build never writes over it, before the
  refusal ships.
- Check the committed generated output in CI (proposed diff below); a unit test cannot, since
  generating needs the network.
- Decide whether a 65 KB read per tick for a few completions is acceptable; at today's record
  size the fixed form is never taken, and for small records it costs more than JSON on every
  axis measured.

## Proposed diffs (outside this card's paths)

`schemas/store/schema.lock` and `schemas/store/tables.baseline`, written by the compiler:

```
go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.1-0.20261014163537-4d9941fbc7fd lock schemas/store
go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.1-0.20261014163537-4d9941fbc7fd tables-baseline --update --reason "first baseline" schemas/store
```

`.github/workflows/ci.yml`, in the unit job after `go vet`:

```
      - name: generated schema wire is fresh
        run: |
          go generate ./internal/sprint/store/donewire/...
          git diff --exit-code -- internal/sprint/store/donewire schemas
```

## Schema improvements wanted

Each is a candidate mas-bandwidth/schema card: title, evidence from this pass, smallest fix.

1. **Fixed-form framing damage has no refusal name.** Evidence: `DoneRecordFixedLoad` on a
   truncated record, a truncated header or a non-zero reserved byte sets `report.Malformed`
   and returns -1 with `Reason` empty; every other refusal is named. Fix: set `Reason` to a
   name (`malformed`, or `truncated` / `reserved_not_zero`) on those returns.
2. **Fixed-form refusal names are not exported.** Evidence: the names live only as string
   literals in `report.Reason`; an adopter matches `"layout_newer"` by hand. Fix: export
   constants (or a typed reason, as `TableRefuseReason` is for the cooked form).
3. **A count or length past its bound is clamped, not refused, contrary to the spec.**
   Evidence: SPEC-TABLES §3.4's slack rule says "A length past the field's own bound ... is
   `malformed`"; the Go runtime clamps the count (200 to 95) and the length (99 to 41) and
   counts `Clamped`. With zero slack the same forged length refuses as `text_ill_formed`
   instead, because the text check runs over the clamped units first. Fix: refuse
   `count_over_bound` / `length_over_bound` before the plan runs, in every leg, and say it in
   FIXED-FORM-ALGORITHM.md.
4. **The writer emits records its own reader refuses.** Evidence: `DoneRecordFixedWriteBody`
   writes `DoneCount` and each `*Length` verbatim (a hand-set count of 200 over 95 slots
   saves "successfully"), and copies text bytes without the UTF-8 / no-NUL rule the reader
   enforces. Fix: `FixedSave` refuses (returns -1 with a name) a count or length past its
   bound and ill-formed text, or the docs state the caller owns it.
5. **Generate does not insist on the lock for a fixed table.** Evidence: `schema generate`
   with no `schema.lock` succeeds with only a notice about `tables.baseline`; the generated
   lineage is the current layout alone, so the next edit's build refuses every existing
   image as `layout_newer`. Fix: warn (or refuse under a flag) when a `fixed table` has no
   lock entry beside it.
6. **An unknown hash always reads as `layout_newer`.** Evidence: flipping a byte of the
   file's hash, or an older image whose layout was never locked, refuses as `layout_newer`,
   which sends the operator looking for a newer build. Fix: document that `layout_newer`
   means "a layout this build does not hold", or rename it (`layout_unknown`), keeping
   `layout_newer` only where a lineage can prove direction.
7. **No way to emit only the fixed form.** Evidence: two fixed tables generate six files and
   about 300 KB (Block, Cook, Json, View, message and variable codecs); the nova-sprint binary
   grew 248 KB for one record. Fix: `generate --surface fixed` (or per-surface opt-in).
8. **`FixedLoad`'s `plan` argument is a capacity check.** Evidence: the plan slice the caller
   passes is only compared against the selected plan's count (`plan_too_large`); the size a
   lineage plan needs is not exported, so adopters guess (`2 * len(DoneRecordFixedPlan.Entries)`
   here, `make(..., 4096)` in the conformance tests). Fix: drop the parameter, or export
   `<T>FixedPlanMax` over the whole lineage.
9. **Per-call setup on the load path.** Evidence: each `FixedLoad` computes holes
   (`tableFixedHoles`, allocating) and resets a default value of the full struct; 81 us and
   119 KB to decode a record of three completions. Fix: compute the holes and the default
   image once per plan at init, as the lineage plans already are.
10. **No sizing guidance before the ceiling.** Evidence: the compiler reports a fixed table's
    body only past 4096 bytes, and the arithmetic "686-byte element, so 95 fit" had to be
    done by hand. Fix: `schema check --verbose` prints each fixed table's body size and, for
    each counted array, the max that fits under 65536.
11. **Generated headers name a file that does not exist and a dependency not used.**
    Evidence: `DonewireView.go` says "generated ... from DonewireView.schema"; every table
    file says "Wide values use serialize.go storage" while importing nothing from it. Fix:
    name the real source file; say serialize.go only when imported.
12. **The schema package name is the Go package name, and the lock binds it.** Evidence: a
    v2 test fixture cannot be `package donewirev2` ("lock is for package donewire"), so it is
    a second package named `donewire` imported by alias. Fix: a `--go-package` override that
    the lock does not bind.
13. **No timestamp idiom.** Evidence: a time is an `int64` of Unix nanoseconds with 0 as the
    zero time, a convention every adopter writes (and must range-check: `UnixNano` is
    undefined outside 1678..2262). Fix: document the idiom in USAGE.md, or a timestamp kind.
14. **No Go usage page for the fixed form.** Evidence: the fixed-form surface
    (`FixedSave`, `FixedLoad`, the plan, `TableReport.Reason`, the lineage) was learned from
    the generated code; SPEC-TABLES.md is about 1 MB and FIXED-FORM-ALGORITHM.md is language
    neutral. Fix: a short "fixed tables in Go" section in USAGE.md: save one record, load it,
    the refusal names, locking before an edit.
15. **Pins are pseudo-versions.** Evidence: main at 4d9941fbc7fd is
    `v2.5.1-0.20261014163537-4d9941fbc7fd`; no tag names it. Fix: tag the compiler releases
    adopters pin to.

What worked without friction, for balance: `go run ...@<pseudo-version>` pinned and fetched
the compiler under `-mod=readonly`; the output was byte-identical across two build paths; the
generated code needs no runtime module; the lineage mechanics (`schema lock` twice) did what
SPEC-TABLES §21 says; and every refusal the runtime names, it names consistently.

## Test result lines

```
$ GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test -p 2 -count=1 ./internal/sprint/store/ ./internal/ci/
ok  	github.com/mas-bandwidth/nova-sprint/internal/sprint/store	7.825s
ok  	github.com/mas-bandwidth/nova-sprint/internal/ci	6.314s

$ GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test -p 2 -count=1 -run Done ./cmd/nova-sprint/ ./internal/sprint/ ./internal/sprint/store/donewire/...
ok  	github.com/mas-bandwidth/nova-sprint/cmd/nova-sprint	1.398s
ok  	github.com/mas-bandwidth/nova-sprint/internal/sprint	0.299s
```

Red first (before the fixed path, against today's JSON behind the same seam): every new test
failed, for example `TestDoneRecordV1RefusesAV2Image: expected "layout_newer", actual
"legacy_json_unreadable"` (today's reader reads a newer image as unreadable JSON, takes an
empty record and rewrites it), `TestDoneRecordFixedRoundTrip: expected "fixed", actual
"json"`, `TestDoneRecordPastTheFixedBoundsKeepsTheLegacyForm: expected
"json:over_max_count", actual "json"`, `TestDoneRecordFixedKeepsTheModel: expected 0x3,
actual 0x7b`.

## Not done

- TLC was not run: the model is unchanged, and its extension for the layout version is owed.
- The live `donerecord` key was not read (the sandbox could not reach Redis); the data bound
  is argued from `where`'s counts.
- The lock, baseline and CI freshness check are proposed diffs, not committed.
- Nothing is merged; no PR.
