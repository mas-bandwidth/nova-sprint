# nova-sprint roadmap

Generated from [docs/roadmap.sexp](docs/roadmap.sexp), the roadmap's data: each planned card is kept there with its whole brief. Edit the data, not this page.

## v1.0.0 (current release)

In progress: the landing, friend and read fixes now in review and merging, protected bases, deletes only in the job directory, the dashboard verb, the flagship README and the v1.0.0 acceptance.

## v1.1.0 (planned)

128 cards in 25 streams, moved out of the v1.0.0 sprint on 2026-10-04 to be done after it.

| stream | cards |
|---|---:|
| [sprint-v1-verbs](#sprint-v1-verbs) | 16 |
| [sprint-v1-1-0](#sprint-v1-1-0) | 15 |
| [sprint-next](#sprint-next) | 8 |
| [sprint-v1-processor](#sprint-v1-processor) | 8 |
| [sprint-v1-docs](#sprint-v1-docs) | 7 |
| [sprint-v1-models](#sprint-v1-models) | 7 |
| [sprint-v1-comfort](#sprint-v1-comfort) | 6 |
| [sprint-v1-install](#sprint-v1-install) | 6 |
| [sprint-v1-integrity](#sprint-v1-integrity) | 6 |
| [sprint-v1-release](#sprint-v1-release) | 6 |
| [sprint-v1-simplicity](#sprint-v1-simplicity) | 6 |
| [friend-reserve](#friend-reserve) | 5 |
| [sprint-v1-sre](#sprint-v1-sre) | 5 |
| [sprint-v1-wallclock](#sprint-v1-wallclock) | 5 |
| [sprint-v1-friends](#sprint-v1-friends) | 4 |
| [sprint-v1-jev](#sprint-v1-jev) | 4 |
| [sprint-v1-yes](#sprint-v1-yes) | 3 |
| [dead](#dead) | 2 |
| [sprint-v1-safety](#sprint-v1-safety) | 2 |
| [sprint-v1-setup](#sprint-v1-setup) | 2 |
| [frictions2](#frictions2) | 1 |
| [friends-sprint-v1-0-0](#friends-sprint-v1-0-0) | 1 |
| [friends-v1-2-0-reliability](#friends-v1-2-0-reliability) | 1 |
| [libs](#libs) | 1 |
| [nova-sprint-split](#nova-sprint-split) | 1 |

### sprint-v1-verbs

- `land-verify-landed-ancestry` Two cards are recorded landed but are not on their branch: harness-pkg-delayproxy-tools-tlacheck-t and fix-general-tla-tableorder-tla
- `friend-deal-most-roomb` The deal tops up a friend who is already full while another friend of her class is idle
- `land-record-unreported-push` The reverse of a false landed record: work that is on the branch but not recorded landed
- `friends-okpct-verified` A friend card counts as ok only once a reader accepted it or it landed
- `resume-many-streams` The coordinator resumed five streams in a loop at 1:35 PM (debt, lint, findings, libs, missed-2026-10-04) after one transient tree-gate failure
- `land-one-lander` Two landers ran in one checkout: a hand land pass at 3:21 PM beside the server run --land, which left the cache clone dirty and refused every stream
- `store-latency-row` The store latency was measured by hand with redis-cli (20 pings, then 500 on one connection) at 3:26 PM while landing was slow
- `fleet-quiet-machine` Quieting a machine is a hand bus broadcast
- `wait-many-notes` The coordinator waited judgments one at a time in a loop at 8:33 AM (wait <id> --for 3h per note)
- `add-tla-edit-refreshes-records` Cards that edit tla/*.tla land without refreshing tla/RUNS.tsv, and the TLC records class test (internal/ci/tlc_records_class_test.go) then calls the records stale on the base
- `stream-set-base` A stream whose base branch is gone, merged or red has no verb to move to a live base
- `fleet-test-process-alarm` Runaway test processes are found only by hand
- `land-clone-self-heals` A dirty land clone refused every stream
- `coordinator-ping-verb` Replace the coordinator's hand ping loop (a zsh loop that pinged every few minutes; one friend's daemon reported "no ping for 3m") with an installed verb of nova-friend that pings every friend each se
- `accept-heavy-verdict` The coordinator cannot record its own heavy read
- `friends-tokens-column` Each friend card's RESULT.md carries `usage: in=<n> out=<n> cache=<n>`

### sprint-v1-1-0

- `v11-batch-selectors` On 2026-10-04 the coordinator changed hundreds of cards one process at a time (brief edits, recuts, reworks, re-tiers, releases), and a recut of 195 pinned cards took too long to finish
- `v11-dev-sync-every-cycle` On 2026-10-04 the base and the development branch drifted for an afternoon while hundreds of cards landed on each; folding them took 105 conflicts and an evening (the owner: "promote every cycle or dr
- `v11-streams-by-repo` On 2026-10-04 the owner asked which open work was not for nova-tools or nova-sprint, so it could be removed, and which of the rest was essential to the next release
- `v11-test-writes-no-source` On 2026-10-04 the adoption pass found that a cmd/nova-sprint test writes cmd/nova-sprint/wake.json into the source tree; the dirty checkout made `nova-update release build` record no commit
- `v11-base-red-auto-resume` On 2026-10-04 the base went red for a few minutes and every stream that tried to land was stopped, each with its own judgment; once the base was fixed the coordinator resumed eight streams by hand
- `v11-reland-from-commits` On 2026-10-04 a merge left 94 landed card commits out of the tree, and the coordinator wrote their re-land briefs with a hand loop (one per commit: cherry-pick it onto the current base, keep its inten
- `v11-conflict-rule-in-tick` On 2026-10-04 every head that did not merge, changed files outside its PATHS, or failed the tree gate stopped its whole stream until the coordinator answered (return, rework on the tip, resume), so 19
- `v11-promote-verb` On 2026-10-04 the coordinator promoted by hand three times: a PR whose head was the base branch got the base auto-deleted on merge (the repository deletes merged heads), the deleted-test rule then fai
- `v11-hold-withdraws-started` On 2026-10-04 a held friend still showed two working cards, because friend take and friend down keep any card with a push: "a held friend keeps started cards" (the owner: "Notice how alex is held, but
- `v11-stream-batch-from-branches` The owner, 2026-10-04: "the key to merging is to merge batch into a work stream branch in work order, then merge that branch into dev"; "don't merge a card at a time when you get behind, do it in batc
- `v11-merge-row-on-dashboard` On 2026-10-04 the owner asked several times what percentage of the merge backlog was cleared, and the hand merge was invisible on the dashboard ("Is this progress visible in the sprint dashboard yet?"
- `v11-tla-paths-frontier` The owner, 2026-10-04: "When we do TLA+ modeling work, I would like that to go to frontier models." Make it mechanical: nova-sprint add and nova-card generate set tier frontier on a card whose PATHS n
- `v11-rebase-verb` On 2026-10-04 an integration branch was merged and auto-deleted while 29 cards still named it as BASE, and 118 held cards named dev; every landing on the deleted branch failed its fetch, and the coord
- `v11-client-default-server` On 2026-10-04 the coordinator ran every nova-sprint verb through a shell wrapper that decrypted the store's Redis password per call and pointed the verb at Redis directly
- `v11-stuck-pin-fallback` On 2026-10-04, 212 cards sat pinned to executors that were down or held (four Claude accounts out of weekly usage, a held friend), 65 of them ready, while the fleet idled; the coordinator un-pinned th

### sprint-next

- `sn-config-dsn-redf` internal/config TestDsnCoverResolveDSNKeywordFlagWithPasswordStands is red on dev: fix it (the test or the resolver, whichever is wrong, and say which)
- `sn-deadline-rule-unifiedb` Unify the friend deadline rule of PR 5306 (the larger of 2 h and 3x her median run wall) with PR 5300 member deadline rule into one rule once both are on dev; one function, one spec paragraph, both ta
- `sn-tokens-update-bus-leftoversf` Leftovers of sn-tokens-redis-bus and sn-update-redis-busb (both LAND; base origin/rowan/bus-rename until PR 5303 merges): docs/SPEC-TOKENS.md says the Redis bus for --bus <host:port>; the docs/TESTS.m
- `dash-always-darkb` Glenn, 2:21 PM on 2026-10-04: "just remove the toggle light/dark
- `sn-overload-alarm-friendsb` The overload alarm (cards timing out on a member; overload.go in PR 5283) covers friends too: a friend whose cards time out raises the same judgment as a machine, with the same numbers
- `sn-dash-container-runtime` Layer 8 of the container-sandboxed functional tier; counts toward nova-sprint v1.0.0
- `sn-functional-in-container` Design card, heavy, layer 7 of the container-sandboxed functional tier; counts toward nova-sprint v1.0.0
- `sn-machine-container-runtime` Layer 6 of the container-sandboxed functional tier, the first nova-sprint layer; counts toward nova-sprint v1.0.0

### sprint-v1-processor

- `wait-replaces-sentinel` Layer 3 of the processor, the reduction: make hold, sentinel and wave one wait in the code, as docs/SPEC-ISA.md already says on paper
- `processor-counters` Layer 2 of the processor: performance counters, built on cycle-time-breakdown's per-card stage stamps (do not duplicate them)
- `rename-shared-paths` ON PROBATION
- `spend-governor` Layer 8 of the processor: the spend governor, which is the spend circuit breaker with per-tier budgets
- `route-predictor-shadow` Layer 4 of the processor: the route and tier predictor, in shadow
- `speculative-dispatch` ON PROBATION
- `vector-cards` Layer 7 of the processor: vector cards
- `external-depends-on` Layer 3 of the processor: external operands

### sprint-v1-docs

- `sprint-flagship-readme` Glenn, 2026-10-04 4:54 PM ET: "I want Stella to design the branding and write the README.md." Write nova-sprint's flagship README in the brand of your sheet (sprint-brand-sheet, docs/sprint/BRAND.md)
- `sdocs-stella-prose-pass` Glenn, 2026-10-04: "I want Stella to design the branding and write the README.md." The suite was written by several hands after your brand sheet and flagship README; this card is your final pass over 
- `sdocs-suite-reference` nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: "nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand
- `sdocs-coordinator-runbook` nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: "nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand
- `sdocs-processor` nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: "nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand
- `sdocs-suite-guides` nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: "nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand
- `sdocs-suite-start` nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: "nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand

### sprint-v1-models

- `tla-card-lifecycle` nova-sprint v1.0.0, lens tla-coverage
- `tla-holds-sentinels-stops` nova-sprint v1.0.0, lens tla-coverage
- `tla-merge-tree-promotion` nova-sprint v1.0.0, lens tla-coverage
- `tla-presence-and-leases` nova-sprint v1.0.0, lens tla-coverage
- `tla-attempts-and-epochs` nova-sprint v1.0.0, lens tla-coverage
- `tla-friend-lanes-and-take` nova-sprint v1.0.0 and nova-tools v1.2.0, lens tla-coverage
- `tla-judgments` nova-sprint v1.0.0, lens tla-coverage

### sprint-v1-comfort

- `stop-is-instant` stop failed today on a store timeout while the machine was busy, and the coordinator retried it
- `add-first-check` Cards were added and dealt whose work was already on their base (dash-tier-costs-now, done by PR 5323; several re-cuts of landed work)
- `land-live-progress` A land pass ran 25 to 40 minutes today with no line until its end, so the coordinator could not tell a slow pass from a stuck one
- `bases-view` Cards today sat on bases nobody was watching (sprint-next on rowan/bus-rename, red from 12:04 PM)
- `reads-ran-false-silent` A read whose child did not run (the member reports read --return with the reason no verdict (ran=false ...): a harness failure, not a finding) is today handed back, counted toward the reads bound and 
- `recut-widen` A HOLD for PATHS too narrow is answered today by the coordinator reading the report and re-cutting by hand

### sprint-v1-install

- `install-canary-shadow-tick` nova-sprint v1.0.0, lens install
- `install-rollback-drill` nova-sprint v1.0.0, lens install
- `install-server-unit-by-verb` nova-sprint v1.0.0, lens install
- `install-cold-twin-warmup` nova-sprint v1.0.0, lens install
- `install-rollback-on-missed-ticks` nova-sprint v1.0.0, lens install
- `install-version-skew-check` nova-sprint v1.0.0, lens install

### sprint-v1-integrity

- `fsck-pushed-heads-recorded` nova-sprint v1.0.0, lens integrity
- `fsck-server-runs-and-pushes` nova-sprint v1.0.0, lens integrity
- `fsck-seat-agreement` nova-sprint v1.0.0, lens integrity
- `fsck-held-without-beat` nova-sprint v1.0.0, lens integrity
- `fsck-verb-landed-on-base` nova-sprint v1.0.0, lens integrity (Glenn 2026-10-04 4:50 PM: more lenses for the highest-quality release)
- `fsck-friend-queue-agreement` nova-sprint v1.0.0, lens integrity

### sprint-v1-release

- `release-check-merge-queue-p90` nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)
- `release-check-cold-audit` nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)
- `release-check-tools` nova-tools v1.2.0, lens release: the same release gate for nova-tools
- `release-check-fsck-24h` nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)
- `release-check-acceptance` nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)
- `release-check-chaos-green` nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)

### sprint-v1-simplicity

- `simp-retire-bud-runners` nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap
- `simp-retire-opencode-runners` nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap
- `simp-retire-buswatch` nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap
- `simp-retire-ping-and-beat-loops` nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap
- `simp-unused-verbs-flags` nova-sprint v1.0.0 and nova-tools v1.2.0, lens simplicity: unused verbs and flags
- `simp-duplicate-paths` nova-sprint v1.0.0, lens simplicity: duplicate paths (two ways of doing one thing are two places to get it wrong)

### friend-reserve

- `bounded-fleet-trial` bounded-fleet-trial
- `all-members-staging-refusal` all-members-staging-refusal
- `inbox-single-judgment` inbox-single-judgment
- `sprint-seat-credential` sprint-seat-credential
- `width-one-operation` width-one-operation

### sprint-v1-sre

- `deal-subscription-first` The buds (rowan-space, rowan-mas, rowan-next: Claude subscription accounts, Opus 5.5) are effectively free, yet heavy and pro work is dealt to per-token routes while they sit idle
- `friend-token-cap` nova-sprint v1.0.0, lens SRE (Glenn ~5:30 PM 2026-10-04, from Freddy's runner: "Mercury re-sends its whole context, a long card snowballs")
- `store-latency-alarm` Builds on store-latency-row (the server's measured p50 and p99)
- `friend-token-efficiency` One-shot and api friends (Freddy on a Mercury flash route, Alex) pay per token, and each delivery spends tokens that do not land work
- `restart-keeps-reads` A server restart takes back every read in flight, so reads are lost and cards strand in review

### sprint-v1-wallclock

- `reads-start-on-finish` A finished card can wait a tick cycle or more for its read
- `friend-idle-wake` Builds on friend-session-liveness (a friend's session activity, not just its daemon pong)
- `rework-goes-to-the-same-worker` A bounced card is redealt anywhere and recloned from nothing
- `deal-latency` Measure ready to dealt to started (the worker's take) per card, and show the median and p90 per member and friend in where; the targets are under 60 s for fleet members and under 5 minutes for friends
- `shared-paths-run-parallel` add already accepts overlapping PATHS when both briefs name the file on a SHARED line (cmd/nova-sprint/verbs.go sharedPaths); that part is done

### sprint-v1-friends

- `sprint-chaos-suite` nova-sprint v1.0.0, stream sprint-v1-friends
- `friend-reconcile-every-tick` nova-sprint v1.0.0, stream sprint-v1-friends
- `friend-stall-ladder` nova-sprint v1.0.0, stream sprint-v1-friends
- `friend-chaos-suite` nova-sprint v1.0.0, stream sprint-v1-friends

### sprint-v1-jev

- `false-bounce-detector` Readers bounced cards today for things that were not defects in the work: harness failures (ran=false, a git shim that swallowed a push, loopback denied, the wall refusing an exec) and trailer-only fi
- `decide-eval-harness` nova-decide eval --kind <k> [--set <file>] [--record <file>] [--every <duration>] evaluates a decision kind on its labelled set (default: the record's cases with outcomes): per-class precision and rec
- `hold-reason-classifier` HOLD reports are read by hand
- `shadow-to-act-promotion` Each decision kind has a mode in nova-config: off, shadow or act (a decide_mode row per kind, default shadow; a migration at the next free number at your base, renumbered at land if taken)

### sprint-v1-yes

- `merge-tree-root` nova-sprint "feels good" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator o
- `merge-tree-switch` nova-sprint "feels good" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator o
- `merge-tree-shadow` nova-sprint "feels good" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator o

### dead

- `dead-pkg-sprint-store-t2b` A tree of 1 work step (STEP 2 to STEP 2), walked in order
- `dead-pkg-subproc-pkg-sprint-t2b` A tree of 2 work steps (STEP 2 to STEP 3), walked in order

### sprint-v1-safety

- `buds-in-the-wall` The bud runners on the Studio (/Volumes/nova/ai/buds/<bud>/runner.zsh and reader.zsh, stopgaps until claude-oneshot-lanes lands) run claude -p --permission-mode bypassPermissions with no wall: a bud c
- `protected-bases` Cards with BASE dev were landed straight onto dev today and ejected the promotion cut three times

### sprint-v1-setup

- `sprint-local-only-mode` nova-sprint v1.0.0, lens setup (Glenn 2026-10-04: a stranger sets the whole thing up from the docs and the tools, with nothing hidden; no bash, zsh or Python in anything that ships; nothing the coordi
- `sprint-dashboard-verb` nova-sprint v1.0.0, lens setup (Glenn 2026-10-04: a stranger sets the whole thing up from the docs and the tools, with nothing hidden; no bash, zsh or Python in anything that ships; nothing the coordi

### frictions2

- `fp-fric-alex3-report-first-two-lines` A friend's REPORT.md is read by the sprint for its first Verdict: and Head: lines wherever they sit (docs/FRIENDS.md, a sprint card), and friends have lost cards by putting a heading or prose before t

### friends-sprint-v1-0-0

- `fs-friends-table-reads-presence` The sprint consumes friend presence; it does not own it

### friends-v1-2-0-reliability

- `daemon-supervised` nova-tools v1.2.0, stream friends-v1-2-0-reliability (general, never sprint-specific)

### libs

- `libs-goleak-t` A tree of 2 work steps (STEP 2 to STEP 3), walked in order

### nova-sprint-split

- `split-one-binary-dashboardf` The sprint dashboard server becomes a nova-sprint verb (nova-sprint dashboard, as the help already lists), the separate binary and its plist retired

