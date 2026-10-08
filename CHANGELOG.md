# Changelog

## v1.1.0 — 2026-10-08

Cut from 8b39951ae88d9113d38e39976a1af3bee89eaf0f. 0 pull requests since v1.0.0.

SHA256SUMS digest: 89262caa7d901f344ebb19e168faf67d703b7040e1dc286ee93b3fceebcd2022

Adopt this release with `--expect-sums 89262caa7d901f344ebb19e168faf67d703b7040e1dc286ee93b3fceebcd2022`.

Dogfood gate waived: land and expand (the owner's ruling, 2026-10-06): dogfood gate waived; the recovery journeys and the spend reconciliation are not yet run at this revision

Recovery journeys incomplete, gate waived: land and expand (the owner's ruling, 2026-10-06): dogfood gate waived; the recovery journeys and the spend reconciliation are not yet run at this revision

- TestEveryFriendFailureShowsWithinItsBound/harness closed: down within 1 minute: not-run (no --journeys evidence)
- TestEveryFriendFailureShowsWithinItsBound/session silent: down within 15 minutes of bus silence: not-run (no --journeys evidence)
- TestEveryFriendFailureShowsWithinItsBound/usage limit: down until the reset, woken after: not-run (no --journeys evidence)
- TestEveryFriendFailureShowsWithinItsBound/bus credential revoked: an alarm on the first failed send: not-run (no --journeys evidence)
- TestEveryFriendFailureShowsWithinItsBound/hold: no card left on him, his cards dealt elsewhere: not-run (no --journeys evidence)

Spend gate waived: land and expand (the owner's ruling, 2026-10-06): dogfood gate waived; the recovery journeys and the spend reconciliation are not yet run at this revision (window 2026-10-06T00:00:00Z..2026-10-08T04:00:35Z)
- unread: --spend-store <addr> names no sprint store, so the recorded spend cannot be read

- no pull request merged since the previous tag

## v1.0.0 — 2026-10-06

Cut from faf07421c317c3436a275d7c958d5bc8b28626a8. 0 pull requests since this repository's first commit.

Dogfood gate waived: Glenn, 2026-10-06 11:03 AM ET: the dogfood gate is waived for this release; we cut, dogfood, rate and test, and that becomes the next version

- no pull request merged since the previous tag

## v1.0.0 (2026-10-06)

The first release of nova-sprint as its own repository: the sprint server, the coordinator's verbs, the friend
daemon, the lander and the public dashboard. The guarantees of this release and the roadmap for the next are in
docs/RELEASE-NOTES-v1.0.0.md and ROADMAP.md.
