# Changelog

## v1.5.0 — 2026-10-17

Schema-table adoption, read side: the sprint store reads its rows through one row reader that accepts the legacy form and the schema-table forms, and counts each read by form.

- `nova-sprint check --json` includes `row_forms`: the per-record, per-form read counts (`binary`, `base64`, `json`, `legacy`, `unknown_fields`, `refused_<reason>`). The plain `CHECK` line does not print them.
- Store row definitions as fixed and variable tables, with generated wire code and golden tests: op rows, friend rows, snapshot and cost rows, and view rows.
- The done record is read through the row reader.
- The wake reads legacy and typed bus envelopes alike, counted per form.
- Writers still write the legacy form.
- Requires nova-tools v1.5.0, where `nova-bus send` now refuses a message that breaks a schema bound with exit 2 before writing anything.

Cards: a8-defs-store-friend-rows, a8-defs-store-op-rows, a8-defs-store-snapshot-and-cost, a8-defs-store-view-rows, a8-store-done-read-through-row-codec, a8-store-row-codec, l1-bus-envelope-dual-read-2.

## v1.0.0 — 2026-10-06

Cut from faf07421c317c3436a275d7c958d5bc8b28626a8. 0 pull requests since this repository's first commit.

Dogfood gate waived: Glenn, 2026-10-06 11:03 AM ET: the dogfood gate is waived for this release; we cut, dogfood, rate and test, and that becomes the next version

- no pull request merged since the previous tag

## v1.0.0 (2026-10-06)

The first release of nova-sprint as its own repository: the sprint server, the coordinator's verbs, the friend
daemon, the lander and the public dashboard. The guarantees of this release are in the release notes at the
v1.0.0 tag; the roadmap for the next release is in ROADMAP.md.
