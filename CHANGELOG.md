# Changelog

## v1.5.2 — 2026-10-17

Cards stop getting stuck on the machine's own holds and on stale gate judgments.

- A bound judgment unanswered at twice its deadline holds a ready primary whose work card was withdrawn in place (it stays ready, dealt to no one, and release clears the hold where it stands), so check no longer breaks rule 2 on the machine's own step. Any other ready primary, and a review primary, is parked in waiting as before, so a sentinel insert never meets a held ready card.
- A held ready card is no dealable work: the deal, the due-move list, the ready alarm, the starving count and the idle-friend pool list all skip it, and the ready-over reason names the server-default hold and its judgment.
- The "no member up satisfies GATE-NEEDS" judgment is raised and closed by the tick itself, so it no longer stays open after a member that carries the need comes back. Its text names the need no member carries.
- Models: `tla/SprintTables.tla` Rule2Withdrawn and HeldReadyIsWithdrawn (reversed witnesses `parkmoves` and `parkinplaceall`, two-primary instance `MCSprintTablesDecisionParkingTwo.cfg`); `tla/JudgmentRetire.tla` GateClosedAfterTick and GateRaisedAfterTick (reversed witness `gate_stays`).
- Requires nova-tools v1.5.0 or later. No wire or record change.

## v1.5.1 — 2026-10-17

- Card tiers: only a card that adds a new TLA+ model (`.tla`) needs the heavy tier. A card that edits an existing model can run at pro. The path lint that enforces this lives in nova-sprint's own `pkg/swarm`.

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
