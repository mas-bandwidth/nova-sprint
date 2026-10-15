# Changelog

## v1.3.16 — 2026-10-15

Cut from cede8bdb5129801c7e123c48764b224b8b984c79. 3 pull requests since v1.3.15.

CI waived: Glenn Fiedler, 2026-10-15T20:05Z
CI waived checks: none judged this commit

Recovery journeys incomplete, gate waived: Glenn live 2026-10-15 coordinator: ship v1.3.16 now, waivers granted as for v1.3.15, fix forward

- TestASilentClientIsUnhookedAfterExactlyTwoAnnouncedOnce: not-run (no --journeys evidence)
- TestNoServerOrNoAnswerIsNoEvidence: not-run (no --journeys evidence)
- TestTheGateRefusesEveryoneWhileTheServerIsStale: not-run (no --journeys evidence)
- TestAStaleAnswerIsDownWithAFreshServer: not-run (no --journeys evidence)
- TestALostAnswerIsNeverToldTwice: not-run (no --journeys evidence)

Spend gate waived: Glenn live 2026-10-15 coordinator: ship v1.3.16 now, waivers granted as for v1.3.15, fix forward (window 2026-10-15T00:00:00Z..2026-10-15T20:05:44Z)
- unread: --spend-store <addr> names no sprint store, so the recorded spend cannot be read

- #409 dashboard: keep the throughput samples across a restart
- #414 cmd/nova-sprint: friend seats carry worker evidence (e948ab19 broke 28 tests)
- #415 Fix three reds from e948ab19: neutral wording in two verb help strings, WorkerLive on refmodel scenario seats

## v1.0.0 — 2026-10-06

Cut from faf07421c317c3436a275d7c958d5bc8b28626a8. 0 pull requests since this repository's first commit.

Dogfood gate waived: Glenn, 2026-10-06 11:03 AM ET: the dogfood gate is waived for this release; we cut, dogfood, rate and test, and that becomes the next version

- no pull request merged since the previous tag

## v1.0.0 (2026-10-06)

The first release of nova-sprint as its own repository: the sprint server, the coordinator's verbs, the friend
daemon, the lander and the public dashboard. The guarantees of this release are in the release notes at the
v1.0.0 tag; the roadmap for the next release is in ROADMAP.md.
