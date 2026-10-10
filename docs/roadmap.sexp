; The nova-sprint roadmap: the data ROADMAP.md is generated from.
; Read by internal/roadmapdoc through internal/worklang (data, never evaluated).
; Edit this file, then run `make roadmap`; never edit ROADMAP.md by hand.
; The shape is in internal/roadmapdoc's package comment.
(roadmap "v1"
 :title "nova-sprint roadmap"
 :text "This is the nova-sprint, nova-card and nova-work work planned after v1.4. The release ladder is
  fixed (the owner, 2026-10-09): v1.2.1 is small fixes; v1.3 is larger fixes, and fixes only; v1.4 is
  v1.3 plus unit tests, cleanup, dead code, adoption of the standard library and modules, and test
  refactors. Then the line stops, and anything outside the ladder is recorded here instead of being
  worked. These items were the sprint half of nova-tools' roadmap and moved here with the split, with the
  sprint's docs and models; nova-tools' ROADMAP.md holds the rest. Point-release work is in FIXES.md, not here. Each item says what it is and why it
  waits. Where the code already has part of an item, the item says what exists and covers only the gap."

 :groups
 ((group "lessons"
   :title "Lessons from Prime Agent's rewrite"
   :text "Prime Intellect's write-up of rewriting Prime Agent with AI workers
    (https://www.primeintellect.ai/blog/prime-agent-rust) names what made a large AI-built rewrite
    hold together. Each item is one of those lessons, checked against nova's code at dev 9ff68178:
    what already exists is named by file and line, and the item covers the rest.")
  (group "sprint"
   :title "The sprint machine"
   :text "New verbs, stages and policies for nova-sprint. Each is new capability, so none of it fits the
    fixes-only ladder.")
  (group "ops"
   :title "Setup, release and operations"
   :text "Setting machines up, installing and releasing safely, and the checks that keep a fleet honest.")
  (group "docs"
   :title "Docs, models and the repository"
   :text "Documentation suites, the TLA+ ledger, and the models."))

 :items
 (
  ; Lessons from Prime Agent's rewrite
  (item "verifier-stage" :group "lessons" :area "nova-sprint"
   :title "The verifier is its own machine stage"
   :text "A card's checks are written before the work starts. After the work, a verifier stage of the
    machine runs them in a fresh bench, apart from the reader, and its verdict is the merge gate: the
    named test must fail at the card's base and pass at its head, run by the machine rather than
    reported by the worker. This changes the card contract (the TEST line becomes a check the machine
    runs), the lander (one red-at-base, green-at-head run per card before the batch gate) and the
    reader's job (it reads a change already shown to do what the card says)."
   :exists "The card lint requires the named test to be absent at the base: rule donewhen-test-name,
    internal/swarm/lintbase.go:198-236 and internal/swarm/lintpaths.go:174-192. The member's native
    gate runs failing tests at the base only to classify a failure the worker already reported as not
    done (cmd/nova-swarm/nativegate.go:26-42). The lander's tree gate runs the batch at its head and
    reads the TEST line only to time the package (internal/sprint/gate_wall.go:46-59). No step runs the
    named test red at base and green at head as a gate; that is the gap. The machine gates before reads
    item, under the sprint machine, holds the cards that started on it."
   :why "A new machine stage, not a fix."
   :date "2026-10-09")
  (item "reader-model-diversity" :group "lessons" :area "nova-sprint"
   :title "The reader is a different model from the implementer"
   :text "A reader catches more when it does not share the implementer's blind spots. The rule: a card's
    reads go to a model other than the one that did the work, with an adversarial brief (find what is
    wrong; do not summarize) and access to the repository at the card's head. This changes read
    dealing and the read brief, and adds the rule to SPEC-SPRINT."
   :exists "nova-sprint set --read-tier and stream set --read-tier raise the tier reads draw from
    (cmd/nova-sprint/verbs.go:3375 and 3469), and a friend never reads her own work
    (internal/sprint/friend_read.go:333). Nothing requires a different model; that rule is the gap."
   :why "A new dealing rule, not a fix."
   :date "2026-10-09")
  (item "findings-to-the-same-implementer" :group "lessons" :area "nova-sprint"
   :title "Findings go back to the same implementer"
   :text "A reader's findings go back to the worker that wrote the change, as a rework of the same card:
    never a new card, and never a fresh worker who has to learn the change again."
   :exists "This is the rule for cards and friends today. A card is edited in place, never twinned,
    and a friend's rework comes back to her (ReworkPinned, internal/sprint/friend_deal.go:89-96). The
    gap is the fleet, where a machine card's rework can go to any member. That is card
    rework-goes-to-the-same-worker-b, held under the dealing policy item."
   :why "Mostly in place; the rest is a dealing policy change, listed once under the dealing policy item."
   :date "2026-10-09")
  ; The sprint machine
  (item "processor-layers" :group "sprint" :area "nova-sprint"
   :title "Processor layers 3 to 8"
   :text "The upper layers of the nova-sprint processor design: cards that name operands outside the
    sprint, a route predictor run in shadow first, speculative dispatch, vector cards that make one
    change across many targets, and a spend governor."
   :why "New capability; no new features through v1.4."
   :cards 6
   :date "2026-10-09")
  (item "merge-tree" :group "sprint" :area "nova-sprint"
   :title "A merge tree for promotion"
   :text "Promotion through a merge tree: a root, a shadow, and a switch between them; and a stream's
    batch built from its cards' branches. Its TLA+ model goes with it: the merge-tree half of card
    tla-merge-tree-promotion-b. The promotion half of that card models shipped code and stays in v1.4."
   :why "New capability."
   :cards 4
   :date "2026-10-09")
  (item "dealing-policy" :group "sprint" :area "nova-sprint"
   :title "Dealing policy"
   :text "Changes to how work is dealt: subscription-billed friends before paid API, fewer tokens per
    friend card, a rework back to the same worker on the fleet as well as for friends, shared paths
    worked in parallel, and deal latency measured and bounded."
   :why "A policy change, not a fix."
   :cards 5
   :date "2026-10-09")
  (item "machine-gates-before-reads" :group "sprint" :area "nova-sprint"
   :title "Machine gates before reads"
   :text "Checks the machine can make run before any reader is paid: a mechanical card is proved by the
    machine and not read, and the work lint proves that a card's test pins its change. This is where
    the verifier stage, under the lessons, starts."
   :why "New capability."
   :cards 3
   :date "2026-10-09")
  (item "fsck-integrity-verbs" :group "sprint" :area "nova-sprint"
   :title "Integrity checks (fsck)"
   :text "Checks the seat or a person can run at any time: each friend's queue agrees with the store,
    nothing is held without a beat, every pushed head is recorded, the server's runs and pushes agree,
    and every landed card is on the base."
   :why "New verbs."
   :cards 5
   :date "2026-10-09")
  (item "new-alarms" :group "sprint" :area "nova-sprint"
   :title "Two new alarms"
   :text "An alarm when the store's latency passes its bound, and one when a test process is left
    running on a fleet machine."
   :why "New capability."
   :cards 2
   :date "2026-10-09")
  (item "spend-circuit-breaker" :group "sprint" :area "nova-sprint"
   :title "A spend circuit breaker with a budget per tier"
   :text "Dealing to a tier stops when its spend passes the tier's budget, and says so."
   :why "New capability. The per-model request budget the owner named is v1.3 and separate."
   :cards 1
   :date "2026-10-09")
  (item "retire-stopgaps" :group "sprint" :area "nova-sprint"
   :title "Retire the stopgaps once their verbs exist"
   :text "Each stopgap goes when the verb that replaces it exists: the bud runners, buswatch, the ping
    and beat loops, the opencode runners, the coordinator's shell lanes, and the hand steps of
    promotion."
   :why "Each one waits on a new verb."
   :cards 7
   :date "2026-10-09")
  (item "reland-from-commits-verb" :group "sprint" :area "nova-sprint"
   :title "A verb to reland a card from its commits"
   :text "A verb that lands a card again from named commits."
   :why "A new verb."
   :cards 1
   :date "2026-10-09")
  (item "briefs-by-reference" :group "sprint" :area "nova-card"
   :title "Briefs carry the card contract by reference"
   :text "A brief names the card contract instead of carrying its text, so briefs are shorter and the
    contract has one copy."
   :why "A format change."
   :cards 1
   :date "2026-10-09")
  (item "friends-token-cost-category" :group "sprint" :area "nova-sprint"
   :title "A cost category for friend work, in tokens"
   :text "Friend work counted in tokens as its own cost category beside the tiers, so subscription work
    shows its real size."
   :why "New capability."
   :cards 1
   :date "2026-10-09")
  (item "roadmap-defer-verb" :group "sprint" :area "nova-sprint"
   :title "A verb to defer a card to the roadmap"
   :text "A nova-sprint verb that moves a card to the roadmap. This file may make it unneeded: a record
    here plus a drop does the same."
   :why "A new verb, which the roadmap file may make unneeded."
   :cards 1
   :date "2026-10-09")
  (item "lander-bench-fault-classifier" :group "sprint" :area "lander"
   :title "The tree gate tells a bench fault from a red test"
   :text "When a gate fails because of the bench rather than the code, the lander says so and does not
    charge the card for it."
   :exists "Part of it is on dev (c5922306bf, 5d8e164a50); pull request 5476 carries more."
   :why "Parked by the owner, 2026-10-09."
   :cards 1
   :date "2026-10-09")
  ; Setup, release and operations
  (item "dashboard-projections-and-freshness" :group "ops" :area "dashboard"
   :title "Dashboard projections and freshness"
   :text "Union slice I: projections on the dashboard with a freshness mark on each, and a column for
    work verified working."
   :why "Union slice I, parked by the owner, 2026-10-09."
   :cards 1
   :date "2026-10-09")
  (item "install-canary-and-rollback" :group "ops" :area "nova-sprint"
   :title "Server install canary and rollback"
   :text "A server install that rolls itself back when the new server misses ticks, a cold twin warmed
    before the switch, a rollback drill run on purpose, and a check for version skew."
   :why "New capability."
   :cards 4
   :date "2026-10-09")
  (item "release-check-gates" :group "ops" :area "nova-sprint"
   :title "More release check gates"
   :text "release check also requires the chaos suites green, fsck clean for 24 hours, and the tools'
    own checks."
   :why "New capability."
   :cards 3
   :date "2026-10-09")
  (item "chaos-suites" :group "ops" :area "nova-sprint"
   :title "Friend and sprint chaos suites"
   :text "Faults injected on purpose into friends and into the sprint machine, each required to show
    and to recover within its bound."
   :why "New test capability beyond the v1.4 quality list."
   :cards 2
   :date "2026-10-09")
  ; Docs, models and the repository
  (item "nova-sprint-docs-and-brand-suite" :group "docs" :area "docs"
   :title "nova-sprint docs suite and brand"
   :text "A full documentation suite for nova-sprint (start, guides, reference, the coordinator's
    runbook, the processor) with a prose pass, a brand sheet, and a flagship README."
   :why "New documentation, not a fix."
   :cards 8
   :date "2026-10-09")
 ))
