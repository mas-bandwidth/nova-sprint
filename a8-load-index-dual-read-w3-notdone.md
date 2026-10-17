# a8-load-index-dual-read attempt 3: not-done

Attempt 3 of the card (a8-load-index-dual-read, part 2 of 5, the sprint store load
index read site at internal/sprint/store/load.go:629) ran under the 20-minute
wall and did not complete the dual-read adoption, so it reports not-done honestly.

Findings recorded for the coordinator:
- The record is the Mem `indexLog` card log index, today a legacy JSON
  `map[string][]string` (card name -> log stream ids) read at load.go:639.
- The generated table is `LogIndex` (schemas/viewrows/viewrows.schema:891), a
  VARIABLE region/builder table (`cursor *string`, `entries []LogIndexEntry` with
  `name *string`, `ids []Line`). Its adoption follows the `viewRowFns`/`viewOps`
  shape (where.go:774), not the done record's fixed-table shape.
- The blocking gap: `log[string][]string` (string ids) does not map field-for-field
  to `LogIndex` (`ids []Line`, `name *string`); an explicit conversion through the
  `LogIndexBuilder`/`TableList` API is required and was not built within the wall.
- No source file under PATHS was changed; no gates were run. The full plan
  (codec, conversion, read-site swap, `TestLoadIndexDualRead` with malformed cases
  and per-form counts) is in the RESULT.md body.
