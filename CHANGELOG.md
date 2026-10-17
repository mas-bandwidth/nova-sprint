# Changelog

## v1.5.4 — 2026-10-17

A hotfix that gets a stopped sprint back to a clean check: a repair no longer drops the log lines of changes that applied, the dropped lines can be put back, and a stopped machine can retire a judgment whose cause is gone.

- Repair of a cut operation: an entry the table layer refuses whose member the store holds as the entry's result (one revision past what it expected, at its target, its fields set) counts as applied and keeps its move line; the manifest's table properties already at their new values count as applied too. A later manifest refused when sent again is probed first, as the first one is: when its writer applied it whole, none of its entries is sent again. On 2026-10-17 a tick drain's repair skipped 159 applied cards and dropped their lines, and check reported 106 rule 13 violations.
- `nova-sprint repair --skipped <op>.skip [--dry-run] [--op <id>]` (coordinator) reconciles a repair's skip judgment: each listed card the store holds as the skip found it gets its missing move line (expected -> found, with its queued lines' words and fields, its generation as stored, and why); a card the log has since moved on, an already restored line and a table property are settled; anything else is refused by name; the judgment is closed when nothing is refused.
- `nova-sprint tick --retire-only [--dry-run]` runs the tick's retire checks alone, as the machine, STOPPED or RUNNING: each judgment whose cards left the table or moved past it, each orphan, and each need-blocked judgment of a need no card has any more is closed with the machine's answer. It moves no card. Start's check gate is unchanged.
- Models: new `tla/RepairLog.tla` (AppliedHasLine, LineOnlyIfApplied, ProbeSparesEntries, ReconciledHasLine; reversed witnesses `skipapplied`, the 2026-10-17 bug, `noprobe` and `reconcileblind`); `tla/JudgmentRetire.tla` RetireOnly with NoOrphanAfterRetire and RetireMovesNothing (reversed witness `stopped_stays`).
- Requires nova-tools v1.5.2. No wire or record change.

## v1.5.3 — 2026-10-17

- Requires nova-tools v1.5.2: a nova table holds 1024 properties at an epoch, not 64, so the work table's tick drain is no longer refused at 65.

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
