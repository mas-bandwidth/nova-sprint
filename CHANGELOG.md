# Changelog

## v1.6.0 — 2026-10-18

The stable release of the v1.5 line: v1.5.4 plus the night's audit of what the sprint server does for nothing. The server now does its work once per change instead of once per read, tick or poll, and the sprint is the same sprint: no wire or record change, every table as it was.

- The inbox is decoded once. Every read of the coordinator's inbox (the tick, the push loop, the views, the tick-end wait every tenth of a second) decoded up to 10,000 notes again from a cursor 20 hours back: ~400 MB/s of garbage, three cores and every verb waiting 20 to 26 s for the line of control on 2026-10-17. `NotesSince` keeps the decoded window per client and stream key and serves every cursor from it; open notes and the done record's pages are kept decoded beside the text they came from and decoded again only when the text changes; the caches are bounded. (#484, #486, #491)
- A quiet log ticks every 10 s, not every second, and a line still wakes the tick within 100 ms. The server's own record is written every second inside the quiet wait, so it never lapses between ticks; the Redis log wait blocks in runs of at most 4 s, under the client's read timeout. (#484, #488)
- A fleet beat that changes nothing but its clock and load runs on the beat lane, off the line of control; the model `tla/ServerLanes.tla` carries the narrowed witness. (#485)
- The where record is counted once per change, not once per load cell: members' beats rewrite a fleet row's load text, which bumped the fleet revision and recounted the whole sprint (fifteen passes over ~3,400 cards and ~2,400 cost parses) on most ticks; the twin now counts record, row and property changes apart from display cells, the record is restamped without a recount when only cells moved, and recounted at least every minute and at every UTC day so the time-relative figures stay true; `CardCostOf` parses each card once per recount; `tla/StatsReset.tla` carries RestampRead and the reversed witness `restampany`. (#487)
- The coordinator push pass runs once per change, at least a second apart, with a 30 s quiet look, both lanes under one hold of the line; a pass over an unchanged sprint is skipped; the push lease is renewed first, off the line, before the lease can lapse; an overdue group keeps its delivery keys when it comes back; a failed shadow read waits a minute doubling to thirty instead of spawning git every five seconds; the decide lane re-decodes its JSONL records only when they grew; a bus refusal ("glenn is no known name") backs off a minute doubling to thirty and is said once; the seat-silent look reads the open notes once at the pinned epoch; `tla/DecisionsOwed.tla` carries the refused-send wait with witnesses `busyretry` and `noretry`. (#489)
- An idle land loop waits on the log instead of polling the merge table every 2 s on the line (43,200 serial-held loads a day with nothing queued); the queue is counted from the table's shape first; `LAND IDLE` is said once a minute. The dashboard reads the sprint only while someone is looking (any request but `/healthz` in the last 30 s, or an open `/events` stream), one read a second then, and a returning viewer's first page is a fresh read. The RUNNING tick prints its lines after giving up the line, and the sprint line is rebuilt on change or once a minute. A reader's lease is renewed when a quarter of it has passed, not on every poll; `tla/ServerLanes.tla` carries the renewal mark and the reversed witness `MCServerLanesBrokenRenewLast`. (#490)
- The open cards of stream v15-schema-tables leave the sprint for the v1.7 roadmap (`docs/roadmap.sexp`, group `export-v15-schema-tables`, one item per card with its needs and pushed heads), on the owner's word of 2026-10-17: v1.5.x becomes v1.6.0 and the rest of v1.5 is v1.7 work.
- Requires nova-tools v1.6.0 (idle members, the fleet beat's lane, the hook server's pushes). A machine on v1.5.4 reads everything v1.6.0 writes.

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
