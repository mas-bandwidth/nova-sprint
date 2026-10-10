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
   :text "Documentation suites, the TLA+ ledger, and the models.")
  (group "friends" :title "Friend AIs"
   :text "Friend AIs, their lanes, harnesses and the way the sprint deals to them."))

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
 
  (item "add-refuses-need-on-dropped-card" :group "sprint"
   :title "Refuse a need that names a dropped card, and list dependants on drop"
   :text "Admission already refuses a need that names no record, but a dropped card is still a record. Add
    refuses a need on a card that can no longer land, and dropping a card lists the cards that need
    it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "card-template-wording-accuracy" :group "sprint"
   :title "Generated card wording matches the card contract"
   :text "The GOCACHE sentence and the Deadline line in generated cards are stale or misleading. Every
    generator and the template say that GOCACHE is the shared warm cache, and that a card past its
    deadline is judged by the coordinator."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "card-ledger-plan-single-wave" :group "sprint"
   :title "Plan a ledger of cards in one wave"
   :text "Now that the lander unions generated ledger conflicts, nova-card no longer needs alternating
    waves for one ledger. It plans the cards in one wave."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "card-help-banner-no-repeated-example" :group "docs"
   :title "nova-card help prints each example line once"
   :text "The usage block of nova-card generate lists the same example line in the flow section and again
    as the example. The help renderer prints it once."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "card-view-by-tier" :group "sprint"
   :title "View cards in review by tier"
   :text "Finding what is in review by tier takes a hand-written scan of store keys and parsing of each
    brief. A nova-card view lists cards by state and tier directly."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-report-first-lines-shape" :group "friends"
   :title "Pin the first two lines of a friend report"
   :text "A friend report starts with an exact Verdict line and an exact Head line, and the friend card
    sync keeps reading older reports leniently."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-fleet-operation-guards" :group "sprint"
   :title "Guards for fleet staging, trials and width changes"
   :text "Refuse staging when every member is ineligible, bound the fleet trial, and make a width change
    one operation, each with a model scenario and a named test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-inbox-and-seat-guards" :group "sprint"
   :title "Single inbox judgment and password-free seat connection"
   :text "The inbox view prints one bounded judgment, and the sprint seat reaches its store without a
    password in the environment."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "lander-empty-commit-handling" :group "sprint"
   :title "Lander handles an empty commit"
   :text "The lander does not fail or mislead when a card produces an empty commit; this is pinned by unit
    and functional tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friends-table-presence-from-tools" :group "friends"
   :title "Friends table reads presence owned by nova-tools"
   :text "The sprint consumes friend presence from the friend tool instead of owning it, so the friends
    table stays right while the sprint server is stopped or hung."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "seat-check-merge-queue-versions" :group "sprint"
   :title "Seat check for the merge queue and versions"
   :text "A check verb reports the merge queue and tool versions on the seat, carried by an open pull
    request."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "reads-served-by-friend-cards" :group "friends"
   :title "Any read can be served by a friend with room"
   :text "A read at any tier is dealt as a card to a friend of a class at or above it, and paid fleet
    readers are used only when no friend has room."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-tla-models-catch-up" :group "docs"
   :title "Bring the sprint TLA+ models up to the state machine"
   :text "The models still ask reads as a pair, pin round-robin, miss the taken-back reader and omit the
    friend ready queue; update them to match the code."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "card-verb-bounded-reads" :group "sprint"
   :title "One card read touches only that card"
   :text "The card verb reads the whole work table and log on each call; make a read use per-card indexes
    and finish under 100 ms at 2,000 cards."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "log-since-wide-window" :group "sprint"
   :title "Fix log --since for windows wider than 22 hours"
   :text "A wide --since window returns nothing; pin the window and return every event in it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04); card from the sprint store (2026-10-10)")
  (item "store-backup-and-demo-load-verbs" :group "ops"
   :title "Backup, restore and demo-load verbs for the sprint store"
   :text "Replace the hand backup procedure with verbs that dump, split, checksum, scan for secrets and
    restore into a throwaway store, so a dump also works as a demo load."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "card-column-single-field" :group "sprint"
   :title "One column field every reader agrees on"
   :text "Card output carries a single column field so a card's state is not confused with the state of
    its need."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "auto-promotion-and-drift-alarms" :group "sprint"
   :title "Automatic promotion to dev and drift alarms"
   :text "The machine promotes the base to dev itself and raises an alarm when the live server, a card
    branch or the base drifts."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "branch-topology-model" :group "docs"
   :title "Model the branch topology"
   :text "Write a model of the branch rules so a side branch, a temporary landing branch or a red base is
    caught by rule."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-notification-and-delivery-reliability" :group "friends"
   :title "Friends are told of cards and push work reliably"
   :text "Delivery of a card to a friend uses known names, the friend is told, finished work is returned
    and the dashboard stays accurate."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "append-only-files-auto-merge" :group "sprint"
   :title "Append-only and generated files merge themselves"
   :text "Conflicts on append-only records and generated files are resolved by a check at landing, not by
    a model by hand."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-dealing-correctness" :group "sprint"
   :title "Dealing to friends honors tier, start and down state"
   :text "Cards are re-dealt when the holder does not serve the card's tier. A card counts as working only
    once started, cards pinned to a down friend raise one judgment, and the overload alarm covers
    friends."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "rework-keeps-who-pin-and-each-finding" :group "sprint"
   :title "Rework keeps the owner pin and each finding"
   :text "A rework of a card pinned to one friend stays with that friend. A grouped rework gives each card
    its own finding instead of the first card's text."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "reads-route-to-capable-readers" :group "sprint"
   :title "Reads reach a capable reader and keep flowing"
   :text "Heavy and frontier reads go to readers that can serve them, read slots are separate from work
    lanes and delivered like cards, and a reader row with no process behind it is flagged. A read
    whose child never ran is asked again without counting."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "reader-spend-on-the-table" :group "ops"
   :title "Reader spend summed on the readers table"
   :text "Every read is a priced record on its card. Sum them by reader on the readers table and in the
    where output, beside work spend."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "judgments-answered-by-rule" :group "sprint"
   :title "Routine judgments are answered by rule in the tick"
   :text "Reworks, take-backs, path blockers and red-promotion fix cards are answered by named rules in
    the tick and logged. Only real judgments reach the coordinator."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "lander-lock-and-bench-gates" :group "sprint"
   :title "One lander per clone, gates run on a bench"
   :text "The lander takes an exclusive lock per clone and reports a stuck pass. Its build gate runs on a
    bench instead of the coordinator machine, and a dead base is refused once with one judgment."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "single-base-branch-discipline" :group "sprint"
   :title "One base branch, watched by the machine"
   :text "Cards name only the real base, the server runs only from the base, and a periodic reminder shows
    when branches drift apart."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "dashboard-dark-only" :group "docs"
   :title "Dashboard is dark only, theme toggle removed"
   :text "The sprint dashboard drops the light theme and its toggle and always renders dark."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "worker-ok-percent-counts-real-work" :group "ops"
   :title "Worker ok percent counts only work the worker could do"
   :text "Holds that name a brief defect are counted against the card generator, not the worker, and
    reader verdicts and past-deadline friend cards are counted correctly in the ok percent."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04); card from the sprint store (2026-10-10)")
  (item "container-sandboxed-functional-tier" :group "ops"
   :title "Container-sandboxed functional tier with a runtime column"
   :text "Functional tests run inside a container runtime such as podman, the machine kind names its
    runtime, and the dashboard fleet table shows a runtime column."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "unify-friend-and-member-deadline-rule" :group "docs"
   :title "One deadline rule for friends and fleet members"
   :text "The friend deadline rule and the member deadline rule become one function with one spec
    paragraph used by both tables."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-card-lint-names-rule-set" :group "sprint"
   :title "Friend card lint says which rule set applies"
   :text "The add lint accepts a friend card with its own rules line and says exactly whether the friend
    or the child rule set applies."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "held-friend-returns-dealt-and-started-cards" :group "friends"
   :title "A held or down friend returns its cards to ready"
   :text "A coordinator verb takes dealt unstarted cards back for redeal, and hold, friend down and a
    usage limit return started cards to ready so the next attempt resumes from the pushed branch."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "client-defaults-to-local-server" :group "sprint"
   :title "Verbs default to the local sprint server"
   :text "With no store settings, nova-sprint verbs go to the local server address, so no wrapper script
    or secret is needed."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "batch-selector-grammar-for-card-verbs" :group "sprint"
   :title "One batch selector grammar for card verbs"
   :text "Brief, recut, rework, return, release, rank and drop share selectors by stream, friend, state
    and ids file with a dry run, and a cheap listing of cards by repo."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "landing-and-merge-visibility" :group "sprint"
   :title "Merge row on the dashboard and live land progress"
   :text "The dashboard shows a merge row with the backlog, rate, oldest age and base gate state, and a
    land pass prints and records its current phase."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "promote-rebase-and-stream-batch-merge-verbs" :group "sprint"
   :title "Promote, rebase and stream batch merge as verbs"
   :text "One verb gates and promotes the base, one moves unlanded cards between base branches, and stream
    merges go in batches in work order instead of by hand."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04); card moved out of the sprint (work record, 2026-10-04)")
  (item "tests-leave-source-tree-clean" :group "ops"
   :title "Tests leave the source tree clean"
   :text "A sprint test that wrote into the source tree uses a temp directory, and a CI class test checks
    the tree is clean after the package tests."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "tla-cards-forced-to-frontier-tier" :group "sprint"
   :title "Cards that edit TLA+ models get the frontier tier"
   :text "Adding or generating a card whose paths name tla sets the frontier tier and refuses a lower one,
    except for run records."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "external-operands-and-single-wait" :group "sprint"
   :title "External dependency operands and one wait for hold, sentinel and wave"
   :text "A card can depend on a merged pull request, a commit on a branch, or a time, and the tick
    releases it when the operand holds. Hold, sentinel and wave become one wait in the code."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "speculative-dispatch-renaming-vector" :group "sprint"
   :title "Speculative dispatch, shared-path renaming and vector cards"
   :text "Long-range ideas for the card machine: deal a card on a need still in review, run cards that
    share paths in parallel and resolve at retire, and deal one batch of sub-cards with a verdict
    each. Each needs a model first and a measured gain."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "route-predictor-and-spend-governor" :group "sprint"
   :title "Route predictor in shadow, spend circuit breaker and per-tier governor"
   :text "Record a predicted route and tier beside the rule's choice and score it. Add dollar caps per
    route and friend per hour and per-tier budgets, narrowing dealing as spend nears a cap and
    resting at the cap with one judgment."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "protected-bases-promotion-stream" :group "sprint"
   :title "Protect dev and main bases; mark a promotion stream"
   :text "Refuse adding a card with base dev or main outside a promotion stream. A stream can be marked as
    promotion, and land refuses a batch that would reach a protected base."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "local-only-address-check" :group "docs"
   :title "One function for the loopback-or-tailnet address rule"
   :text "Gather every address check for the sprint and its stores into one function, cited from each
    caller, so local-only mode has one definition."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "store-latency-and-batched-ticks" :group "sprint"
   :title "Batch tick round trips and alarm on slow store"
   :text "Pipeline or batch each tick part's reads and writes to under twenty round trips per tick. Raise
    one judgment per episode when the store round trip median passes a bar."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04); card moved out of the sprint (work
    record, 2026-10-04)")
  (item "coordinator-heavy-read-record" :group "sprint"
   :title "The coordinator can record its own heavy read as a read row"
   :text "Add an accept option that records the coordinator's heavy read, with its evidence path and hash,
    as a counted read row. This ends the wait for another read after a false bounce."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "add-requires-tla-run-records" :group "docs"
   :title "Refuse a card that edits a TLA+ model without refreshing the run records"
   :text "The add verb refuses a brief whose paths cover a model file but not the run records file, and
    names the remedy. This stops landed cards from making the records stale."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "fleet-quiet-and-overload-alarms" :group "ops"
   :title "Quiet a machine by verb and alarm on runaway test processes and overload"
   :text "A fleet quiet verb stops dealing to a member for a set time, and the beat reports the count of
    live test processes so the tick raises one judgment per episode. The overload alarm covers
    friends as well as machines."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-work-restriction-by-stream-kind" :group "friends"
   :title "Restrict the work a friend is dealt by stream and card kind"
   :text "A friend row may list stream patterns and card kinds, and the dealer never deals that friend a
    card outside them. Add refuses a card that names a friend outside her restriction."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "landed-record-reconciliation" :group "sprint"
   :title "Reconcile landed records with the branch in both directions"
   :text "A verify-landed verb checks that every card recorded landed is on its branch, and lists work
    that is on the branch but not recorded landed. It gives the coordinator a way to correct either
    mistake."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "read-retry-after-no-verdict" :group "sprint"
   :title "Ask again when a read ended with no verdict, and keep live reads across restart"
   :text "A read taken back or run without a verdict is asked again of another reader and does not count
    against the reads bound. A server restart keeps reads whose lease is live, modelled first in
    TLA+."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04); card from the sprint store (2026-10-10)")
  (item "verbs-take-many-targets" :group "sprint"
   :title "Let resume and wait take several streams or notes at once"
   :text "Resume takes more than one stream and wait takes several notes, each set or refused on its own
    line. This replaces hand loops by the coordinator."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "stream-base-move-and-bases-view" :group "sprint"
   :title "Move a stream to a live base and show the health of every base"
   :text "A stream set verb moves unstarted and queued cards to a new base, and a bases view lists each
    base with its cards, drift and last gate result. Add refuses cards on protected bases outside a
    promotion stream, and a recut verb can widen paths from a proposal in a hold report."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "land-gate-whole-tree-and-base-health" :group "sprint"
   :title "Gate landing on the whole tree and treat base health as one fact"
   :text "The lander's gate runs the wider package and whole-tree checks, so a batch cannot turn the base
    red. A red base stops landing with one judgment and resumes streams by rule, and the base and
    development branch are synced every cycle."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04); card from the sprint store (2026-10-10)")
  (item "sprint-timers" :group "sprint"
   :title "Timers on the sprint machine that wake an actor at a chosen time"
   :text "A remind verb writes a timer with a due time and note, and the machine wakes the named actor
    when it falls due."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "subscription-first-dealing" :group "sprint"
   :title "Deal to subscription executors first and skip those that are down"
   :text "A switch keeps paid routes unused while a subscription friend with room covers the tier. A
    preference for a friend who is down, held or limited is skipped at the deal."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-reconcile-and-stall-ladder" :group "friends"
   :title "Reconcile friends and run the stall ladder every tick"
   :text "The tick reconciles each friend's store with her inbox and outbox, and a mechanical stall ladder
    returns cards held by a stalled friend with no step needing the coordinator."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-deal-fill-idle-friends-first" :group "friends"
   :title "The friend deal fills idle friends before topping up a full one"
   :text "A card for any friend goes to the up friend of the class with the most room. A full friend is
    not refilled while another of her class is idle."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-view-locked-by-file" :group "docs"
   :title "The sprint view frame is pinned byte for byte by a lock file"
   :text "The where frame and the watch writes are compared against a committed lock file so the view
    cannot drift from its design."
   :date "2026-10-10"
   :release "v1.4"
   :origin "PR #5014")
  (item "member-beat-age-at-tick-read" :group "sprint"
   :title "A beat's age is measured at the tick's read so a long tick downs no member"
   :text "A tick that runs long over a slow link no longer marks members down who are beating on time."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5015")
  (item "reader-four-checks-before-approval" :group "sprint"
   :title "Readers check four things before approving a card"
   :text "Readers check for comments contradicting code, stranded fragments, edits outside declared paths,
    and broken cross-package references before approving."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5113")
  (item "cold-start-balance-unknown-never-out" :group "sprint"
   :title "A cold start never judges a provider out of funds from old refusals"
   :text "On its first tick the server must not halt the sprint because of stored refusals. An unknown
    balance is never treated as out of credit."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5228; issue #5220")
  (item "dirty-tick-twin-catchup-robust" :group "ops"
   :title "The dirty-tick twin catches up from set, reorder and delete events without a whole read"
   :text "The change-stream catch-up recognizes row order and row delete events, and the test waits on the
    twin state, not the clock. This removes a flaky merge-queue failure."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5230; PR #5233; issue #5197; issue #5214")
  (item "finish-head-default-and-land-sha-check" :group "sprint"
   :title "finish --head defaults to what land accepts; a land with no full sha fails"
   :text "The finish help and default match the head that land accepts. A land without a full sha is a
    failed finish, and stale reports stay fenced."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5235; PR #5246; issue #5154")
  (item "card-tier-ceiling-before-deal" :group "sprint"
   :title "A card's tier is its ceiling before the first deal"
   :text "Before a deal, and in a store with no route, the card's own tier is its ceiling and the card
    verb prints it. The model calls the one tier function."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5243")
  (item "admission-contract-lint-statement" :group "sprint"
   :title "State the brief admission contract in lint and add"
   :text "Lint and add print one admission sentence about child rules, step checks and model lines. A bare
    lint drift on any other token prints a note and still admits the text."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "PR #5247")
  (item "friend-take-epoch-fence" :group "sprint"
   :title "Fence a friend take across epoch boundaries"
   :text "A friend take is rejected when the brief's epoch differs from the live epoch, and the epoch is
    carried into the store step so a concurrent clear fences it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5268")
  (item "nova-work-help-clarity" :group "docs"
   :title "Clarify nova-work help semantics"
   :text "Help gives a direct first-run path and explains what import dry-run and verify max do. The
    shared dry-run flag help promises only the result and no changes."
   :date "2026-10-10"
   :release "v1.4"
   :origin "PR #5275")
  (item "dashboard-state-pulse-wave" :group "sprint"
   :title "Dashboard merging, review and working cells pulse in a left-to-right wave"
   :text "Each of the three card states pulses on its own, offset in time from the last, so the pulse
    moves as a wave from left to right."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "PR #5278")
  (item "auto-sentinels-released-by-tick" :group "sprint"
   :title "Auto sentinels, released by the tick when their needs land"
   :text "A third kind of sentinel is released by the tick as soon as its needs have landed, with no
    person's release."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "PR #5308")
  (item "sprint-refmodel-lint-clean" :group "docs"
   :title "Staticcheck clean in the sprint reference model"
   :text "Remove unused variables so staticcheck is clean in the reference model package."
   :date "2026-10-10"
   :release "v1.4"
   :origin "PR #5335")
  (item "where-costs-by-tier-and-late-tick" :group "sprint"
   :title "where --costs carries tiers and cost by tier; a late tick reads running"
   :text "The dashboard reads tiers and per-card cost by tier from where. A running machine with a late
    tick reads running, not stopped."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5323; PR #5326")
  (item "server-beats-and-reads-off-the-line" :group "sprint"
   :title "Server beats and reads run off the control line; a gone caller is never run"
   :text "Verbs that only read or beat must not queue behind the single line of control, and a verb whose
    caller is gone is dropped."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5325")
  (item "priority-plain-set-blocker-rung" :group "sprint"
   :title "Priority is a plain set; a blocker is stored, dealt first and evicts lower work"
   :text "The priority verb stores every level. A blocker is dealt first and evicts the lowest level, then
    the shortest running card."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5405; PR #5459; card from the sprint store (2026-10-10)")
  (item "read-cards-sole-path" :group "sprint"
   :title "Read cards are the only read path; every ok primary in review gets its read"
   :text "Remove the old ask path. Every ok primary in review gets a read, and withdrawals and returns
    spend no reader."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5419; PR #5428; PR #5434")
  (item "needs-verb-edit-in-place" :group "sprint"
   :title "Edit a card's needs in place"
   :text "A needs verb shows a card's needs and edits them with a reason, so cards waiting on a dropped
    need can be freed."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "PR #5427; PR #5461")
  (item "sprint-reliability-repairs-staging" :group "sprint"
   :title "Selective sprint reliability repairs: rework priority and staged land gates"
   :text "Rework cards get fix priority and land gates are staged from the tested tree."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5451")
  (item "structured-brief-intact-when-tiering" :group "sprint"
   :title "Keep structured briefs intact when tiering TLA work"
   :text "The tier line goes before structured header fields so REPO and CARRY briefs stay parseable."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5452")
  (item "batch-friend-presence-from-engine-beat" :group "friends"
   :title "A batch friend's presence is her engine's beat"
   :text "A friend with no session but a beating engine must not read down. The engine's beat counts as
    presence."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5455")
  (item "sprint-friend-redeal-and-lane-models" :group "docs"
   :title "TLA+ models for friend redeal, friend rows and live friend lanes"
   :text "The sprint models cover a card taken back from a friend and redealt, and the live friend lane
    judgments, each with reversed witnesses. A CI guard keeps the model records current."
   :date "2026-10-10"
   :release "v1.4"
   :origin "PR #5511; PR #5556; PR #5239; PR #5508; PR #5510")
  (item "deal-by-cost-and-priority-order" :group "sprint"
   :title "The deal fills free lanes by cost, honors priority, and routes frontier cards to friends"
   :text "Free working lanes fill first across friends and the fleet, cheapest capable first, then the
    ready stack grows to twice the width. Priority ladder order and tie breaks hold, and a frontier
    card goes to a friend whose tiers hold it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5531; PR #5545; PR #5552")
  (item "sprint-pause-and-stop-replay" :group "sprint"
   :title "Sprint pause and unpause verbs with idempotent stop return replay"
   :text "The sprint can be paused and unpaused, and a replayed stop return with an operation id is safe.
    The pause state machine has a checked TLA+ model."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "PR #5532")
  (item "dashboard-status-dot-and-first-read" :group "sprint"
   :title "Dashboard live dot shows stopped, and a failed first read refuses"
   :text "The dashboard header dot turns red when the machine is stopped, green when running, and neutral
    when disconnected. A dashboard whose first read fails exits 2 with one stderr line instead of
    serving."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5526; PR #5528")
  (item "friend-health-future-proof-refused" :group "friends"
   :title "Friend health refuses a proof dated after the server clock"
   :text "A health proof with a time ahead of the server clock is refused, so a friend cannot be kept up
    by a future date. The proof order check then stays sound."
   :date "2026-10-10"
   :release "v1.3"
   :origin "nova-sprint PR #2")
  (item "role-views-for-models" :group "sprint"
   :title "Role views for a model: view coordinator, view worker, and a view API"
   :text "Compact ranked views serve a model what needs it, instead of the human dashboard. Items carry
    the next command and a cursor for incremental reads."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint PR #3")
  (item "machine-feeds-itself-idle-alarm" :group "sprint"
   :title "The machine feeds itself: inherited twins, rule answers and an idle alarm"
   :text "Cards re-cut as twins inherit the state of the original, mechanical judgments are answered by
    rule, and an idle fleet raises an alarm. This reduces manual judgment when many cards are held."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint PR #4")
  (item "land-gate-flaky-retry" :group "sprint"
   :title "Land retries a flaky red gate once and re-polls unknown mergeability"
   :text "A gate batch that is red for a known flaky reason retries once with deduplication, and a
    mergeable state of unknown is polled again. The retry needs only its own lock key."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint issue #35")
  (item "sprint-redis-acl-auth-wiring" :group "sprint"
   :title "The sprint client consumes the Redis ACL credential and never echoes it"
   :text "The card push and table verbs accept the injected ACL password from the environment. Address
    errors never print credentials."
   :date "2026-10-10"
   :release "v1.3"
   :origin "nova-sprint issue #36")
  (item "sprint-server-memory-wedge" :group "sprint"
   :title "The sprint server does not grow without bound or stop answering"
   :text "The server once reached a very large resident size and stopped answering verbs while the fleet
    stalled behind it. Memory is bounded and a watchdog restarts a wedged server."
   :date "2026-10-10"
   :release "v1.3"
   :origin "nova-sprint issue #38")
  (item "coordinator-handoff-writes-row-and-role" :group "sprint"
   :title "The coordinator verb also writes the config row and the role"
   :text "Moving the coordinator seat also updates the config sprint row and the stored coordinator role.
    A later config apply then does not write the old holder back."
   :date "2026-10-10"
   :release "v1.3"
   :origin "nova-sprint issue #42")
  (item "sprint-failed-card-fast-and-raised" :group "sprint"
   :title "A failed card is known in seconds and raised, not redealt"
   :text "A card that fails at launch returns to ready and is dealt again. It is known to have failed
    within seconds, is not hammered back in, and is raised to the coordinator."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2040")
  (item "lane-registry-tool" :group "friends"
   :title "Start, brief, watch and stop a child lane through a registry tool"
   :text "Lanes are started with long prose briefs and hope. A tool starts, briefs, budgets, watches and
    stops a lane and records its handoff."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2064")
  (item "work-bug-fields-and-open-bug-blocking" :group "docs"
   :title "Enforce work bug fields and let an open bug block its ancestor"
   :text "nova-work refuses a bug node that lacks its found-during field or its test evidence. An open bug
    keeps its ancestors from reading done, and who, check and stale print bug counts."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2335; issue #2336")
  (item "work-lease-expiry-derivation" :group "docs"
   :title "Derive lease expiry, extend-once and escalation in nova-work"
   :text "A lease past its deadline reads as expired, may be extended once, and can escalate. A lease with
    no end is rejected."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2338")
  (item "work-v2-recursive-coordination-nodes" :group "sprint"
   :title "nova-work v2 recursive coordination nodes"
   :text "Coordination nodes follow one uniform contract at any depth. The spec's five behaviours get
    tests that drive a seeded kernel with no model."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2354")
  (item "deal-handback-and-redeal-guard" :group "sprint"
   :title "Hand back launcher refusals and refuse re-dealing a dealt label"
   :text "A launcher refusal before any process starts returns the card to the queue instead of raising it
    unknown. The queue remembers dealt labels and refuses a repeat unless forced."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2381; issue #2382")
  (item "route-choice-from-measured-failure-rates" :group "ops"
   :title "Choose provider routes per bench from measured failure rates"
   :text "Routes per bench are picked automatically from measured zero-token and nonzero-exit rates by
    bench and model, not from a hand-written list."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2385")
  (item "landing-gate-and-transitions-owned-by-machinery" :group "sprint"
   :title "Machinery runs the gate and owns returned, verified and landed transitions"
   :text "Harvest runs the gate itself outside the wall and records the result; a card's own gate claim is
    data. Returned, verified and landed are recorded transitions per item, head and base, and the
    landing lane is plain verbs with a model only for exceptions."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2386; issue #2530; issue #2508; issue #2407")
  (item "reader-pin-check-for-test-only-prs" :group "sprint"
   :title "Mechanical pin-check read for test-only pull requests"
   :text "A reader reverts the commit each new test claims to pin, expects red, restores it and expects
    green, and reports per test."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2387")
  (item "sprint-row-per-machine-and-table" :group "sprint"
   :title "Each machine computes its sprint row and pushes it to a store"
   :text "Every machine builds its own row of queue, working, done, ok, fail and rates every ten seconds,
    and the viewer reads the store."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2389")
  (item "card-cost-reductions" :group "sprint"
   :title "Cut the per-card cost in setup, tests and parallelism"
   :text "Self-contained cards need no repo, cards run only the useful test command with cached tool
    discovery, and Go parallelism is bounded per card."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2399; issue #2400; issue #2401")
  (item "priority-fixes-to-front-of-queue" :group "sprint"
   :title "Reviewer findings on the current batch jump to the front"
   :text "A finding on the current batch becomes a card at the front of the swarm and merge queue, keeping
    the fix close to the base."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2417")
  (item "cheap-reads-ci-evidence-and-pr-brief" :group "sprint"
   :title "Make reads cheap with CI evidence and a short mechanical PR brief"
   :text "CI carries the machine-checkable evidence and the PR body opens with title, goal, write scope
    and test command. Report and read start when the PR opens, and the reader sees only the diff and
    statuses."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2449; issue #2531; issue #2509")
  (item "promote-up-tier-escalation" :group "sprint"
   :title "Escalate a node that fails twice to the next tier"
   :text "A node that fails the bar twice on a cheap route moves up a tier with both attempts attached,
    never a third recut at the same tier."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2450")
  (item "merge-typed-verdict-parsing" :group "sprint"
   :title "Parse typed approve verdicts and release untyped holds"
   :text "A typed approve line is counted, and an untyped hold can be released by a read."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2454")
  (item "typed-card-results-and-resume-keys" :group "sprint"
   :title "Typed card outputs and resume by content key and deal-time base"
   :text "Result and read files follow a schema parsed without a model. A finished result is cached by
    card text and base, and the base is pinned at deal time."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2506; issue #2507; issue #2529")
  (item "reader-score-calibration" :group "ops"
   :title "Calibrate the model grader against typed scores"
   :text "The grader's prompt is tuned until its scores agree with friend scores on a calibration set,
    then it scores low-risk kinds."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2536")
  (item "sprint-table-counts-from-events" :group "sprint"
   :title "Sprint table counts come from a card event stream, not file times or comment scans"
   :text "Done, ok, fail, read, landed and calibration counts per friend and per bench are folded from
    card events, so the table needs no GitHub scan and shows cut to landed."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2563; issue #2678; issue #2680")
  (item "sprint-progress-from-work-set" :group "sprint"
   :title "Sprint x/y percent and ETA come from the work set"
   :text "One reader loads the roadmap and work files, a landed acceptance kind exists, and a sprint is
    any bounded task set answering x/y percent in under a second, with GitHub issues ingested into
    the file."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2595; issue #2664; issue #2679; issue #2593; issue #3141")
  (item "sprint-start-stamped-by-verb" :group "sprint"
   :title "Opening a sprint stamps its start on every machine"
   :text "The sprint verb records the start once, and bench counts read it, so counts never include an
    earlier sprint."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2727")
  (item "card-identity-ruling-links" :group "sprint"
   :title "Cards carry a stable id, ruling and spec links, and a reverse walk from PR to reason"
   :text "Each card gets a stable id carried to job, result, event and PR body, names its ruling and spec
    anchor, and one query answers why a PR exists, with its siblings and attempts."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2589; issue #2596; issue #2597")
  (item "needs-human-line" :group "sprint"
   :title "A needs-you line lists what waits on a person, oldest first"
   :text "The sprint table and status print the items waiting on a human, such as asked cards, PRs with no
    typed line and held cards, with who is expected, and a supervisor owns the notifications."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2592")
  (item "merge-gate-evidence-capture" :group "sprint"
   :title "The merge gate keeps raw test output and full review text, and stores its receipt"
   :text "The batch test step keeps the raw go test JSON stream, paginated review captures are not
    truncated at the output cap, and the gate's receipt is stored where a reader can fetch it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2626; issue #2654; issue #2693; issue #3183")
  (item "hold-pinned-to-head-sha" :group "sprint"
   :title "A hold pinned to an older head is not a hold at the current head"
   :text "The lander ignores a hold whose sha is not the PR head when a later line at the head clears it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2710")
  (item "github-events-stream" :group "sprint"
   :title "GitHub events arrive in a stream so loops are event-driven"
   :text "A webhook receiver writes GitHub events to a stream with a durable consumer and a polling
    fallback, so PR, CI and typed-line changes reach loops and the table within seconds."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2657; issue #2685")
  (item "decision-records" :group "sprint"
   :title "Owner decisions are records with options, default, deadline and ruling"
   :text "A decide verb opens, shows and rules decision records, and the table shows one count of open
    decisions."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3143")
  (item "work-ingest-from-github" :group "sprint"
   :title "nova-work ingest from GitHub: capture, continuous poll, verify and cutover"
   :text "nova-work reads issues from GitHub into its own store with conditional requests, keeps in sync
    from deliveries and a poll, and has a verify run. The tracker becomes a mirror once the cutover
    is done."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3167; issue #3169; issue #3172; issue #3173; issue #2080; issue #2081; issue #2082")
  (item "work-writeback-and-fold" :group "sprint"
   :title "nova-work write-back: one writer, roadmap batch, lag and fold of landed work"
   :text "One writer applies roadmap batches back to the tracker, reports how far the mirror lags, and
    settles units when landed pull requests and verified criteria arrive."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3168; issue #3171")
  (item "work-specs-in-forest-single-writer" :group "sprint"
   :title "nova-work keeps specs in its store behind a single-writer kernel"
   :text "Specs, their versions and reader scores live in the nova-work store, and the ready gate is
    computed from them. One kernel owns the store and every mutating verb is a kernel command."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3220; issue #3340")
  (item "coordination-state-in-redis-and-go" :group "sprint"
   :title "All coordination state in Redis, batched, driven by Go tools"
   :text "Queues, leases and tables live in the sprint store, not in files. Every store access is a
    pipelined batch, every loop ticks at one second, and coordination scripts move into Go verbs."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3279; issue #3303; issue #3309")
  (item "central-decisions-fleet-executes" :group "sprint"
   :title "Decisions are made centrally; machines only execute what the queue says"
   :text "A worker machine takes the next ready item, runs it and reports. It never decides readiness from
    a partial view."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3251")
  (item "coordinator-role-portable" :group "sprint"
   :title "The coordinator is a role any friend AI can hold on any machine"
   :text "Every coordinator action is a verb, and the loops and tables run on any Mac or Linux machine
    with failover, so no step depends on one person or host."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3295; issue #3543; issue #2061; issue #2062; issue #2065; issue #2066; issue #2069; issue
    #509; card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-down-detection" :group "friends"
   :title "Detect a friend AI that is unreachable or out of credits and route around it"
   :text "A friend counts as down when it cannot be reached or cannot work because credits ran out. The
    state is detected mechanically and its work is routed elsewhere."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #3185")
  (item "pitstop-state-in-store" :group "sprint"
   :title "A pit stop and its lift are one store state every friend reads"
   :text "The stop and its lift are set and cleared by a verb and held in the sprint store, so a friend
    never relies on a bus note to know whether to work."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3371")
  (item "task-redistribute-verb" :group "sprint"
   :title "One verb moves ready work to whoever has open slots"
   :text "A single atomic verb cancels stale items and moves ready work across queues to friends with open
    slots, honouring the named owner, instead of hand loops."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3407")
  (item "dependency-form-single-parser" :group "sprint"
   :title "One DEPENDS-ON form and one parser that refuses other spellings"
   :text "Cards, pull requests and issues state dependencies in one canonical form read by one parser.
    Unknown dependency state is never treated as satisfied."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #3409")
  (item "spec-approval-machinery" :group "sprint"
   :title "Make spec approval reliable: one SPEC line form, a verdict verb, sha-keyed reads"
   :text "Spec reads use one machine-checked line form written by a verb. A verb computes the gate verdict
    from the store, reads are keyed to the body hash so stale ones never count, and a rewrite names
    the gaps it closes."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3564; issue #3565; issue #3566; issue #3568; issue #3569")
  (item "merge-streams-landing-unit" :group "sprint"
   :title "Merge streams: the stream is the landing unit with open, take, batch, land verbs"
   :text "A stream is opened, taken, batched, marked and landed as a unit, holding at its boundary.
    Cancelled or timed-out shards count as red."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3478")
  (item "worklang-done-field-parsed" :group "docs"
   :title "The worklang parser keeps the done field of a unit"
   :text "Unit done reads a field the parser never keeps, so a done unit is never counted. The key is
    added and sibling skipped keys are checked, with a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #3499")
  (item "children-visibility-on-table" :group "sprint"
   :title "Show every seat's children on the sprint table within a second"
   :text "The table shows each live child with task, step, age and cost, read from the store, including
    reads and spec reviews that hold no queue lease."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3519")
  (item "card-turn-and-reasoning-bounds" :group "sprint"
   :title "Bound turns and reasoning on the live card templates"
   :text "Read-family cards carry a low turn limit and low reasoning, and writing-family cards a higher
    one, to cut tokens per card."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3534")
  (item "sprint-table-host-and-consumer-columns" :group "sprint"
   :title "Sprint table: disk room per host and one consumer table for friends and hosts"
   :text "The sprint table shows free disk per host, red under the floor, and one table shape (ready,
    working, done, ok, fail, ok%) for friends and hosts."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3584; issue #4071")
  (item "card-record-is-the-only-record" :group "sprint"
   :title "The card record is the only record; GitHub is a remote written at the edges"
   :text "Cards carry their fields as structured data imported once at creation. Nothing parses prose or
    edits a pull request body, and GitHub is touched only to close out. A call budget guard counts
    remaining GitHub calls meanwhile."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3594; issue #3596; issue #3601; issue #3607; issue #3633; issue #3603; issue #3967")
  (item "reads-are-one-record-written-by-verb" :group "sprint"
   :title "A read is one record written only by the verb"
   :text "Reads live in one record with one writer, and a read counts only when the verb writes its score
    line. A score posted only on the bus cannot exist."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3874; issue #3897; issue #2063")
  (item "card-cut-context-and-worker-notes" :group "sprint"
   :title "Cards carry what changed on the base, and merge notes reach live workers"
   :text "The card cut lists base commits touching its paths since the issue, and a note verb lets merge
    learnings reach a worker mid-task."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #4189; issue #4186")
  (item "friend-lifecycle-automation" :group "friends"
   :title "Friend lifecycle and widths run as machinery: wake, take, refill, redistribute"
   :text "A free child lane refills at once, a seat under its width with ready work gets one wake note
    from the tick, and redistribute is one verb and duty. Bulk assignment stays accurate by counting
    only cards actually taken."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3635; issue #4095; issue #4096; issue #4098; issue #3915; card moved out of the sprint
    (work record, 2026-10-04)")
  (item "automatic-card-flow-and-single-writers" :group "sprint"
   :title "Cards flow automatically through the tables, moved only by single-writer verbs"
   :text "A waiting card is complete, so a deal is one move. Verbs deal, work, end, land and cancel are
    the only writers of stream, host and friend sets, and everything stays in the store under one
    second."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3636; issue #3662; issue #3911; issue #3929")
  (item "stream-table-events-and-failed-card-review" :group "sprint"
   :title "The stream table is true end to end; every refusal and failed card is raised"
   :text "Each column change is an event, a nightly control walks a card through all columns, every
    refusal and error is raised to the coordinator, and a failed card goes to a typed review
    verdict."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3999; issue #4155; issue #4072")
  (item "sprint-store-keys-and-reset-verb" :group "sprint"
   :title "Sprint store keys do not expire, and a reset to zero is a backed-up verb"
   :text "Beats, presence and history keys drop their TTLs and length caps in favor of recorded time and
    reader judgment. A reset verb deletes the sprint state families as admin after a backup."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #3878; issue #4195")
  (item "remove-superseded-sprint-verbs" :group "sprint"
   :title "Remove the superseded sprint verbs and machinery in one batch"
   :text "Once the unused-verb check confirms it, the old friend-queue, routing, lander and sprint-file
    verbs are deleted together."
   :date "2026-10-10"
   :release "v1.4"
   :origin "issue #4191")
  (item "card-spec-fields-at-push" :group "sprint"
   :title "A card is a spec, refused at push without its fields"
   :text "A card carries evidence, paths with lines, seams, rules, receipts and keep notes. Push refuses a
    work card missing them, and requires a stream."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #4313; issue #5494")
  (item "parent-card-cuts-children" :group "sprint"
   :title "A parent card cuts children, waits and stitches"
   :text "A parent fans out from a plan, depends on its children, takes corrections mid-flight, then
    reviews and stitches in its second phase."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #4317")
  (item "card-dependencies-and-sentinels" :group "sprint"
   :title "Dependencies come from paths, with sentinels per layer"
   :text "Ready means no live path overlap. Streams follow the file graph, overlap is refused at push, and
    a sentinel card reviews each layer and across streams."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #4318; issue #4322; issue #4188; issue #4185; issue #4028")
  (item "coordinator-console-and-comfort" :group "sprint"
   :title "The coordinator console is one grammar and settles what it can"
   :text "One spelling per concept, one receipt shape, fast quiet calls and a home screen. The machine
    settles what it can before waking the coordinator."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #4352; issue #5096")
  (item "coordinator-push-wake-adapters" :group "sprint"
   :title "Supervised wake adapters for coordinator seats"
   :text "A fresh coordinator seat gets a durable, supervised recipe that turns new judgments and notes
    into a wake for its harness. Delivery, wake and acknowledgement are separate steps."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #5131")
  (item "sprint-test-redis-version-check" :group "ops"
   :title "Sprint test Redis helper checks the pinned version"
   :text "The helper returns whatever Redis is first on the path. It should pin the version and fail on a
    mismatch, like the shared helper."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5172")
  (item "inbox-push-write-once-race" :group "sprint"
   :title "Inbox push cannot overwrite a published judgment"
   :text "Two overlapping clients share one temporary path, so a second write can change the published
    file. Use a unique temporary path per writer."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5160")
  (item "tick-watchdog-bounded-diagnostics" :group "sprint"
   :title "The tick watchdog bounds diagnostics and refuses negative deadlines"
   :text "Watchdog exit must not wait on diagnostics that can block. A negative deadline is refused."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5173")
  (item "sprint-cost-rules" :group "sprint"
   :title "Cost rules: flash first, escalate repeats, rest failing routes"
   :text "Cheap tier first, pro only on escalation. Two identical failures escalate at once. A route that
    returns no result rests. Rules are kept by reference, each with a unit test."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #5174; PR #5300")
  (item "refmodel-reads-needed-and-tiers" :group "docs"
   :title "The reference model learns reads needed and tiers"
   :text "The reference model assumes two reads per card and has no tier. Teach it reads needed and add
    flash cards and routed stores to its fixtures."
   :date "2026-10-10"
   :release "v1.4"
   :origin "issue #5195")
  (item "generated-ledger-merge-conflicts" :group "sprint"
   :title "Generated ledgers merge without conflict and rework starts at the tip"
   :text "Two cards that shrink the same ledger conflict by line. Regenerate the ledger at the merged
    tree, and stage rework from the current head."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5215")
  (item "rest-properties-cap" :group "sprint"
   :title "Rested routes must not overflow the fleet table property cap"
   :text "Rule 3 writes one property per rested route, so the cap is reached at about 58 routes and the
    tick writes nothing. Use one property for all."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5210")
  (item "second-tick-leveling-after-deal" :group "sprint"
   :title "A second tick must not move a card the first deal placed"
   :text "Leveling runs at tick start, but the deal places cards later in the tick without regard to it.
    Make the deal and the level agree."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5408; card from the sprint store (2026-10-10)")
  (item "card-kinds-mechanical-tool-work" :group "sprint"
   :title "Card kinds for fix-red work cut from triaged defects"
   :text "A fix-red card kind names its source and test files and its reproducing test, and is accepted
    only when that test goes green. One card per triaged defect."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #1655")
  (item "work-client-stop-waiting-bound" :group "sprint"
   :title "nova-work client must stop waiting after a bound"
   :text "A work client call without a deadline waits forever on a session that never answers. The spec
    fixes a default bound and the client enforces it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #1643")
  (item "cards-issue-to-landed-no-hand-step" :group "sprint"
   :title "A card goes from issue to landed with no coordinator hand step"
   :text "List what remains before accept, routing, reads and landing all run by machinery alone, and
    close each gap."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #1725")
  (item "capacity-discovery-and-matching" :group "sprint"
   :title "Discover offered capacity and match ready work without double-counting shared pools"
   :text "A coordinator learns what each friend can do now and whether workers share one account limit.
    Stale advertisements do not count."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #176")
  (item "control-events-reprioritize-stop" :group "sprint"
   :title "Reprioritize, correct and stop work already dealt"
   :text "Authenticated control events with scope, update number and priority reach every running
    descendant, and the sprint can prove each one applied."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #179")
  (item "autonomous-goal-execution" :group "sprint"
   :title "Carry an accepted goal to its endpoint without routine human steering"
   :text "The sprint persists the goal, gates, priorities and budget, and recovers from a failed worker,
    exhausted provider or lost context while the owner is away."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #187")
  (item "dealer-proportional-per-member-queues" :group "sprint"
   :title "One dealer fills each member in proportion to its free slots"
   :text "A single dealer with per-member queues stops fast-ping machines from winning every card while
    slow ones sit idle."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2008")
  (item "down-member-never-hangs-loops" :group "sprint"
   :title "A down machine must never hang the dealer, sampler or harvest"
   :text "Every remote call has a timeout and each host is isolated, so a dead machine and stale
    connection sockets cannot freeze the loops."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2009")
  (item "provider-capacity-ceilings-and-spill" :group "sprint"
   :title "Per-provider and per-model concurrency ceilings with error watch and spill"
   :text "Provider capacity is a shared resource. Ceilings, an error-rate watch and automatic spill to the
    next route keep retries from holding slots."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2010")
  (item "width-from-measured-memory" :group "sprint"
   :title "Derive a machine's width from measured per-card memory with a controlled ramp"
   :text "Widths are measured, not guessed, and limits are found by a controlled ramp."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2019")
  (item "work-supply-from-enumerated-sources" :group "sprint"
   :title "Cut cards by loop from enumerated sources, never duplicating open pull requests"
   :text "Card generators become verbs that cut from a work spec, a repair or a read, and skip work an
    open or merged pull request already carries."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2021; issue #2041; issue #2047")
  (item "pause-signal-honored-everywhere" :group "sprint"
   :title "A fleet-wide run, pause and stop signal honored by every loop"
   :text "One signal with a generation is checked by dealing, launch, harvest and landing before each
    action, and bad signal states cause no side effect."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2022")
  (item "preflight-and-canary-before-dealing" :group "sprint"
   :title "Preflight every machine and canary each new card family before dealing wide"
   :text "The first few cards of a new family run alone, and a machine is checked at loop start, so mass
    failures show in minutes."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2030")
  (item "funnel-and-status-report" :group "sprint"
   :title "A standing report and status verbs for the funnel from launch to landed"
   :text "Launched, returned, gate result, pull requests and landed are counted per wave and per model,
    with live cards and widths by machine, all from records."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2034; issue #2049")
  (item "merge-simulate-shared-commit-stacks" :group "sprint"
   :title "Merge simulation and planning detect stacked pull requests by ancestry"
   :text "Two members sharing a commit are not a conflict. The planner finds stacks by ancestry, not by
    base ref."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2036")
  (item "harvest-wave-into-one-gated-pr" :group "sprint"
   :title "Harvest a wave into one gated pull request and write verdicts back"
   :text "One verb collects returns, applies commits, runs the suite, opens the pull request and records
    each verdict on its source."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2067")
  (item "work-global-uids-and-issue-index" :group "sprint"
   :title "Globally unique node ids and a two-way issue index"
   :text "Give every node, card, attempt and evidence record a kernel-minted unique id, and keep O(1)
    lookups from node to issues and from issue to nodes, rebuilt from the journal."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2084; issue #2085; issue #3174")
  (item "work-link-mode-dogfood-and-absorb-decision" :group "sprint"
   :title "Link-mode dogfood ledgers and the absorb-mode decision"
   :text "Measure tokens and wall clock per question and log every drift with its repair cost while
    running in link mode. Absorb mode stays unscheduled until those numbers and its safety triggers
    are met."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2089; issue #2090")
  (item "sprint-behaviour-visualization" :group "sprint"
   :title "Visualize the sprint as linked behaviour views"
   :text "Show the whole behaviour at once: a card timeline with a lane per bench, the pipeline from
    source to landing as linked views, and measures plotted against width, route and card kind."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2110; issue #2111; issue #2112; issue #2114")
  (item "work-tree-live-picture" :group "sprint"
   :title "nova-work tree as a live picture beside its Lisp"
   :text "Render the work tree with state, evidence, counts and linked issues on the nodes, filling in as
    work lands."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2113")
  (item "fleet-probe-loaded-bench-not-down" :group "sprint"
   :title "A loaded bench must not be marked down by the fleet probe"
   :text "The fleet-state probe marks a heavily loaded but healthy bench down after a few missed probes,
    and the dealer then kills its loops and sweeps its queue. Define state-entry timing and tell
    loaded from down."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2161")
  (item "sprint-event-log-merge-and-expander" :group "sprint"
   :title "Emit merge-queue and expander events to the structured log"
   :text "Log enqueue, group start, group verdict and park events from the merge queue, and derive and cut
    events from the work language expander, as the logging spec requires."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2185; issue #2187")
  (item "sprint-queue-fence-and-order-tests" :group "ops"
   :title "Queue fence, lane order and unknown-call tests"
   :text "Prove that a worker that lost its lease cannot write, lanes are read in priority order with a
    directory fallback, and an expired remote call stays unknown and keeps its seat until answered
    or bounded."
   :date "2026-10-10"
   :release "v1.4"
   :origin "issue #2196; issue #2197; issue #2198")
  (item "work-nodes-projection-rebuild-test" :group "ops"
   :title "Nodes table is a rebuildable projection of the journal"
   :text "Add acceptance tests that drop the nodes projection and rebuild it from the journal, and that a
    disagreement is reported as a projection bug."
   :date "2026-10-10"
   :release "v1.4"
   :origin "issue #2202")
  (item "work-derive-fold-expansion" :group "sprint"
   :title "Implement derive and fold expansion in nova-work"
   :text "Expand a derive form to one node per matching issue and a fold form to one node over its green
    sibling branches, excluding non-green ones."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2246")
  (item "jobs-kernel-uncertain-reservation-and-executor" :group "sprint"
   :title "Kernel: uncertain reservations, lease re-grant and executor seam"
   :text "Hold an uncertain reservation and never re-grant it on expiry, reap on lease expiry, and give
    the executor seam a parent grant, a version-bound result and uncertain-on-disconnect."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2239; issue #2240; issue #2247")
  (item "jobs-kernel-tool-version-refusal" :group "sprint"
   :title "Kernel: tool key invalidation and below-version refusal"
   :text "Compare a unit's declared tool versions and semantic keys at admission: invalidate outputs when
    a key moves, and refuse a unit whose tool is below its version."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2242; issue #2248")
  (item "jobs-kernel-collections-warm-done-when" :group "sprint"
   :title "Kernel: collection harvest, warm state, done-when and acceptance"
   :text "Bind collection members at harvest, keep retained warm state out of active capacity, make the
    set finish line a report not a gate, and refuse a unit without acceptance at load."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2241; issue #2243; issue #2249; issue #2250")
  (item "duty-tier-policy-and-escalation" :group "sprint"
   :title "Duty tier: resident session executes policy, escalations as nodes, learned admission"
   :text "The resident session executes an approved finite policy and never authors it, and every judgment
    it cannot make becomes an escalation node with a rule, a default and an age. Admission checks
    can be learned from the history of abstain reasons, with preservation tests for handoff records."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #500; issue #585")
  (item "typed-decision-models-for-routine-judgments" :group "sprint"
   :title "Typed-decision models for card routing and other routine judgments"
   :text "A fast schema-typed model with a calibrated confidence makes bounded routine decisions such as
    card type, abstain reason and note triage. It saves tokens, cost and wall clock against a
    general model."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #589; issue #896; issue #5241")
  (item "card-dependencies-and-verdicts-as-state" :group "sprint"
   :title "Card dependencies as edges, and verdicts and holds as state"
   :text "Cards carry needs and blocks edges, and a card is ready only when every dependency is merged and
    green. Read verdicts are state re-evaluated each tick, holds are edges, and every hand action is
    a verb."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #785; issue #854; issue #2437; issue #3219")
  (item "one-verb-card-moves" :group "sprint"
   :title "Any card moves anywhere with one verb"
   :text "One verb moves a card from where it is to where the coordinator wants it, including handing a
    dealt card back. A pin to a down friend never holds a card."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "brief-and-form-checks-by-machine" :group "sprint"
   :title "Brief defects and report form are judged by the machine"
   :text "A card whose brief is wrong is shown as its own column, not as review. Report form is checked by
    lint at finish, and add applies the header fixes its lint computes."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "machine-sends-width-goal-to-friends" :group "friends"
   :title "The machine sends the width goal to idle or under-width friends"
   :text "The machine detects a friend who is idle or under width with ready cards and sends the take
    request itself. The deal is bounded by lanes actually started."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "friend-delivery-state" :group "friends"
   :title "Friend delivery is a visible state"
   :text "A delivery a friend's session refuses is shown on the friends table with the harness message.
    The friend daemon delivers cards in a fixed order, and a hand verb runs the same path."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "friend-ok-counts-after-read" :group "friends"
   :title "A friend card counts ok only after a reader accepts it"
   :text "A friend card that is only reported finished is not counted ok until a reader accepts it or it
    lands. This keeps the friends table honest."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "hold-fix-applied-by-machine" :group "sprint"
   :title "A hold that names its fix is applied by the machine"
   :text "When a hold names its own fix, the machine applies it instead of waiting for the coordinator to
    read it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "landing-resilience" :group "sprint"
   :title "Landing survives a red base, a red batch and a rejected push"
   :text "A red base is said as the base's and no card is blamed. A red batch ejects the failing card and
    lands the rest. A push rejected because the base moved is retried, never a stop."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "verb-version-skew-and-restart-safety" :group "sprint"
   :title "Verbs are safe across a switch and across client versions"
   :text "A verb sent during a server switch gets a restarting reply and waits. Every verb of one client
    version is readable by the last release."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "job-dirs-removed-and-volume-guarded" :group "ops"
   :title "Job directories are removed and the volume is guarded"
   :text "A card's job directory is removed once its results are moved and its pull request is confirmed,
    bench lanes clean their own temporary files, and the working volume is guarded."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); issue #2379; issue #2566; issue #2632")
  (item "route-rate-budget-and-jitter" :group "sprint"
   :title "A route carries a request budget and a jittered rest end"
   :text "A route can carry a requests-per-minute budget that the deal keeps. The end of a route rest is
    jittered so routes do not return together."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "tla-stop-and-backup-models" :group "docs"
   :title "TLA+ models for the machine stop and the backup path"
   :text "Models for how a stop returns reads and claimed cards, and for the promise that a backup path
    holds only a verified file. Each is checked on a small instance and cited from the code."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "tla-read-and-friend-card-models" :group "docs"
   :title "TLA+ models for reads on a missing branch and the friend card"
   :text "A model for a read on a missing branch that spends no reader, and for a friend card's lifecycle
    and the friend's liveness. Each names the evidence the code reads."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "stats-tidy-large-fleet" :group "sprint"
   :title "A stats tidy succeeds over a fleet of more than 64 rows"
   :text "Tidying stats works when the fleet table has more than sixty-four rows, instead of hitting a
    property bound."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "complete-cost-headlines" :group "ops"
   :title "Cost headlines are complete and carry their coverage"
   :text "Cost per landed and total cost include work, reads, landing and no-result runs, each in its own
    column. Headlines show their denominators and coverage, and an unpriced run cannot make a route
    look cheaper."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "delivery-milestone-records" :group "sprint"
   :title "Delivery milestones are explicit records"
   :text "Each milestone is a record on the card and stream with repository, target ref, commit and gate
    or review evidence: staged, verified in dev, released."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "deal-latency-measure" :group "ops"
   :title "Deal latency is measured per member and friend"
   :text "The time from ready to dealt to started is measured per card, shown as median and p90, and
    alarmed once per episode when the p90 passes its target."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "dead-package-removal" :group "docs"
   :title "Dead packages are removed"
   :text "Packages with no users are deleted together with their tests, one step each, checked by the
    build."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "card-cutting-narrow-paths" :group "sprint"
   :title "Card cutting keeps sweeps small and paths narrow"
   :text "A sweep over a whole tool is cut by site so each card finishes inside its deadline. A card that
    edits one test file gets only that file as its paths, so two cards do not collide at land."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); cards moved out of the sprint (work record, 2026-10-04)")
  (item "server-cold-start-warmup" :group "sprint"
   :title "Server warms its in-memory mirror before ticking"
   :text "A restarted server warms its in-memory copy of the store before its first tick, so early ticks
    do not miss the deadline and crash-loop."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-state-machine-tla-models" :group "docs"
   :title "TLA+ models for gc, landing prune and the lane check"
   :text "Small TLA+ models for the gc rule, the landing cleanup queue and the tick's friend-lane check,
    each with invariants checked on a small instance and cited from the code."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "judgment-and-read-start-latency" :group "sprint"
   :title "Judgments and reads start within seconds"
   :text "Mechanical judgments are answered in the step that raises them, and a finished card is offered
    to a free reader at once, with a measured target of under 30 seconds."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "lander-live-progress-lines" :group "sprint"
   :title "The lander prints its phase as it runs"
   :text "A land pass prints a progress line for each phase, so the coordinator can tell a slow pass from
    a stuck one."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "land-protected-stream-pull-request" :group "sprint"
   :title "Landing into a protected branch opens a pull request"
   :text "For a stream marked land-protected, land pushes a land branch, opens a pull request with the
    batch details and enables auto-merge where allowed."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "deal-never-waits-on-coordinator" :group "sprint"
   :title "Failures and dealing never wait on the coordinator"
   :text "A failed attempt is handled by the machine, and the deal keeps friends supplied with work even
    when the coordinator is out, so no friend idles or polls for lack of cards."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "redo-verb-restores-dropped-card" :group "sprint"
   :title "Redo restores a dropped card"
   :text "The redo verb restores a card that was dropped, covered by a test."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "release-held-cards-verb" :group "sprint"
   :title "Release takes a stream or every held card"
   :text "The release verb accepts a stream name or releases every held card, building on a verb that
    moves any card anywhere."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "verbs-answer-fast-and-stop-is-instant" :group "sprint"
   :title "Verbs answer fast, and stop is instant"
   :text "The server answers verbs in well under a second even while busy, and stop writes its state in
    one store step without waiting for the tick."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "stats-reset-from-a-mark" :group "sprint"
   :title "One verb zeroes every figure on the sprint page"
   :text "A stats reset verb clears total cost, per-card and per-tier cost, stream cost cells and each
    row's done and ok percent from a recorded mark."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "bench-chosen-by-load" :group "sprint"
   :title "A lane picks the least loaded bench"
   :text "The bench a lane gates on is chosen at lane start from the fleet rows by cores and current load,
    and the choice and reason are written into the brief."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "card-lint-defects" :group "sprint"
   :title "The card lint refuses known defects and reads place names right"
   :text "The card lint refuses the listed brief defects, and a repository or branch name is not mistaken
    for a personal name."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "idle-trace-and-alarm-unit-tests" :group "ops"
   :title "Unit tests for the idle trace and idle alarm"
   :text "The idle trace and idle alarm code gets unit tests for every root, raising its statement
    coverage from a small fraction."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "lander-records-own-landings" :group "sprint"
   :title "The lander records its own landings"
   :text "A pushed head never waits in merging for a manual merge step. The lander writes the landing
    record itself when it pushes a stream's batch."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "promote-from-throwaway-branch" :group "sprint"
   :title "Promote merges from a throwaway branch after a whole-tree gate"
   :text "Promotion never names the base branch as a PR head. It opens and merges from a throwaway branch
    after a whole-tree gate, or the report names what blocks it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "table-single-write-new-stream" :group "sprint"
   :title "The table tells the truth: one write per new stream"
   :text "Adding a stream records its rows and places in one write, so the table never disagrees with the
    facts."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "brief-repo-line-lint" :group "sprint"
   :title "A brief's repo line is always owner/name"
   :text "Admission lint, recut and rework refuse a repo line that is not owner/name, and the tier writer
    stamps only the result line."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "tidy-tombstone-for-landed-cards" :group "sprint"
   :title "Tidy leaves a tombstone for every landed card"
   :text "A need on a landed card that tidy removed must read as landed, not as a missing id. A tombstone
    lets the card listing tell landed-and-tidied from never-existed."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "sprint-models-dealing-and-presence" :group "docs"
   :title "TLA+ models for dealing, presence, leases and priority"
   :text "Model the cap deal, deal routes and width, presence beats and leases, down windows and
    take-back, and the priority ladder in TLA+, checked with TLC."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04);
    cards moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-models-cards-and-promotion" :group "docs"
   :title "TLA+ models for briefs, needs, reads, seat push and the merge tree"
   :text "Model brief lint and twins, needs and sentinels, read cards, seat push, and the merge tree with
    promotion in TLA+, and check them with TLC."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "seat-install-push-verb" :group "sprint"
   :title "A seat install verb installs the push loop"
   :text "The seat install verb installs the push loop as the tool's own verb and pushes judgments to the
    seat."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "friend-row-matches-live-lanes" :group "friends"
   :title "A friend's row equals her live lanes"
   :text "A friend's working set on the dashboard must equal her live child lanes, and work and its report
    are one act."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "add-time-already-done-check" :group "sprint"
   :title "Check at add whether a card's work is already on its base"
   :text "At add, by git and without a model, a card is refused when its work already sits on the base
    branch."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "false-bounce-detector" :group "sprint"
   :title "Detect false bounces by readers"
   :text "Readers must not bounce cards for harness failures or trailer-only misreads. A detector
    separates these from real defects."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); cards moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-store-test-environment" :group "ops"
   :title "Shared test environment for the sprint store packages"
   :text "The sprint store and its test utility packages get the same test environment scaffold as the
    other packages. Their tests stay under the time bound."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "stage-carries-prior-head-onto-moved-base" :group "sprint"
   :title "The stage carries a prior attempt's head onto a moved base"
   :text "A lane never carries an earlier attempt onto a moved base by hand. The stage does it and reports
    the result."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "sprint-card-and-lander-tla-models" :group "docs"
   :title "TLA+ models for card counters, the card lifecycle, holds and the lander"
   :text "Model the attempt, generation and epoch counters, the card columns, held cards and sentinels,
    and the lander streams. Each model is checked and cited from the code."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); cards moved out of the sprint (work record, 2026-10-04)")
  (item "repeat-refusal-alarm" :group "sprint"
   :title "The third refusal of one cause in five minutes raises one judgment"
   :text "Three refusals of the same cause within five minutes raise one judgment, rewritten in place and
    closed after five quiet minutes; watch --wake wakes on it. Its TLA+ model is rebuilt with it."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit fbcb205 (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "nova-card-new" :group "sprint"
   :title "nova-card new writes a lint-clean brief from its parts"
   :text "A new verb builds a brief that passes the card lint from flags: task file, paths, test, gate,
    tier, rules and batch."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 4dde8e4 (not carried by the re-seed, nova-sprint PR #28)")
  (item "merge-health-judgment" :group "sprint"
   :title "Merge health is one coordinator judgment every ten minutes"
   :text "The coordinator pass raises one merge-health judgment every ten minutes naming the base, the
    main line, stopped streams, the promotion PR and unstitched branches. The drift facts it reads
    already exist."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 412f684 (not carried by the re-seed, nova-sprint PR #28)")
  (item "frontier-reader-rows" :group "sprint"
   :title "A frontier read goes to a reader row that declares frontier"
   :text "When no frontier friend AI has room, a frontier read is routed to a reader row that declares the
    frontier class."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 39e4253 (not carried by the re-seed, nova-sprint PR #28), routing half")
  (item "dashboard-live-pin" :group "sprint"
   :title "The served dashboard page is pinned to the live page"
   :text "A sha256 test pins the page the dashboard serves to the live page, so the two cannot drift
    apart."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 7a4988d (not carried by the re-seed, nova-sprint PR #28), remainder; card
    moved out of the sprint (work record, 2026-10-04)")
  (item "nova-work-roadmap-verbs" :group "sprint"
   :title "nova-work roadmap check, add, remove, pull and note"
   :text "Verbs over the roadmap s-expression files with byte-stable round trips; each writing verb
    refuses a file that check rejects."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 8c87a4b (not carried by the re-seed, nova-sprint PR #28)")
  (item "gate-stack" :group "sprint"
   :title "Work lint, then a machine gate before a read, then gate lint"
   :text "One ordered stack: ten mechanical lint checks rework a finished attempt before any read; a bench
    gate runs the TEST line, vet and tests at the head before a read, and a red gate reworks without
    spending a read; reach analysis checks the TEST line can reach the change."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commits 5d3742c, 6019a68, 3129e31, 16d4e33, fa3cb2c, 5faebca and b223df5 (not
    carried by the re-seed, nova-sprint PR #28)")
  (item "processor-counters" :group "sprint"
   :title "Processor counters: IPC and stall reasons from the stage stamps"
   :text "where --json reports stage-time counters (IPC, stall reasons, the top stall) with a dashboard
    row, and the card model proves stalls partition wall time, with a reversed witness."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 415d761 (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "release-check-cold-audit" :group "sprint"
   :title "release check asks for a cold audit of twenty landed cards"
   :text "release check --audit asks for cold reads of twenty landed cards, and a plain release check
    passes only with an all-ok audit under 48 hours old."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 1123e21 (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "land-eject" :group "sprint"
   :title "Land ejects a card that cannot merge and lands the rest"
   :text "The lander ejects one unmergeable card with its dependents and lands the rest; a third eject for
    one cause raises a wrong-brief judgment and a 15 minute stall raises merge stuck once. The land
    model gains Eject with its configs."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit d4999b3 (not carried by the re-seed, nova-sprint PR #28)")
  (item "fleet-lane-route-mark" :group "sprint"
   :title "A route mark on local-endpoint lanes in the fleet track"
   :text "The dashboard fleet track shows a route mark and an endpoint tooltip on lanes served by a local
    endpoint."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 5018ecc (not carried by the re-seed, nova-sprint PR #28); cards moved out of
    the sprint (work record, 2026-10-04)")
  (item "land-tests-functional-tier" :group "ops"
   :title "The lander git and go tests move to the functional tier"
   :text "Lander tests that run git and go move to the functional tier, and the land decisions are pinned
    in a memory test."
   :date "2026-10-10"
   :release "v1.4"
   :origin "nova-sprint commit 2b74be0 (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "coordinator-bring-up" :group "sprint"
   :title "Start prints the coordinator bring-up, one line per thing"
   :text "start, coordinator, handover and check --bring-up print one BRING-UP line per thing with its
    state and command; a missing line raises a bring-up judgment, and watch --events folds the epoch
    log."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 639ccef (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04); PR #5281")
  (item "verified-working" :group "friends"
   :title "A friend AI working count holds only verified cards"
   :text "A friend AI working count includes only cards with a push or named by beat --running, a dealt
    column is added to the friends table, and an idle friend AI gets one judgment after 15 minutes."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "nova-sprint commit 2c831fa (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "adopt-carries-store-library-change" :group "ops"
   :title "The adopt can carry a change of the store library"
   :text "The shadow tick refuses any other library digest before the window, so an adopt cannot carry a
    library change; run it inside the window after the load, with TLA+, and digest code bytes only."
   :date "2026-10-10"
   :release "v1.3"
   :origin "seat decision, 2026-10-10")
  (item "lander-bench-tree-own-git" :group "sprint"
   :title "The lander bench tree carries its own git, not a pointer"
   :text "The bench tree git points at a worktree that exists only on the coordinator machine, so git
    fails on benches; give the bench tree its own repository."
   :date "2026-10-10"
   :release "v1.3"
   :origin "v1.2.4 held ledger")
  (item "tick-under-one-second-at-load" :group "sprint"
   :title "The tick stays under one second at load"
   :text "Drain and deal take 0.4 to 0.7 seconds at load, so the tick gate is over one second; bring the
    whole tick under it."
   :date "2026-10-10"
   :release "v1.3"
   :origin "v1.2.4 held ledger; PR #5557")
  (item "store-rating-and-dogfood-rounds" :group "ops"
   :title "A rating, review, dogfood and audit round of the current release"
   :text "Each friend AI rates, dogfoods and audits each tool as released and writes the reports the
    earlier rounds asked for. The planned rounds of past releases are kept here as one recurring
    practice."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "cards from the sprint store (2026-10-10); cards moved out of the sprint (work record,
    2026-10-04)")
  (item "fix-card-tiered-context" :group "sprint"
   :title "Three-tier context for fix cards"
   :text "A fix card carries only what it needs, in three tiers: the error line; then the enclosing AST
    block (go/ast); then the exported signatures of that block's direct imports. A rework for a
    wrong brief widens the context one tier, capped at three, the way a reader's finding widens a
    card's PATHS."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "a Gemini dialog the owner shared, 2026-10-10")
  (item "read-collapsing" :group "sprint"
   :title "Read cards share one immutable snapshot"
   :text "N read cards of one branch share one immutable snapshot, a bundle at one ref per host, instead
    of each fetching its own. The dedupe happens in the scheduler, not in the card state machine.
    Today each read is a card, dealt cheapest-first, and nothing collapses them."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "a Gemini dialog the owner shared, 2026-10-10")
  (item "spend-burn-rate-sentinel" :group "sprint"
   :title "A spend burn-rate sentinel"
   :text "Spend per hour past a threshold raises a manual sentinel for the coordinator. Today the spend
    gate runs only at a release cut. It complements the automatic throttles in spend-circuit-breaker
    and route-predictor-and-spend-governor: this one is an alarm for the coordinator, not a
    throttle."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "a Gemini dialog the owner shared, 2026-10-10")
  (item "false-positive-fix-loop-case" :group "sprint"
   :title "A TLA+ case for the false-positive fix loop"
   :text "A fix passes its own layer but keeps re-failing a sentinel above it. A TLA+ case couples
    components to domains: a fix confined to PATHS that never meet the regression's component
    exhausts its attempt bound and halts. When widened PATHS intersect the component, the fix clears
    it nondeterministically (it may still fail), and every failure counts toward MaxAttempts:
    intersection is necessary, not sufficient. Properties: a reachable fix can land (liveness under
    fairness), and a fix that is never right halts at the bound (safety). Open question: does a
    merge-queue ejection count toward the card's attempt bound? It extends the attempt-bound models
    in tla-card-lifecycle-counters-and-waits."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "a Gemini dialog the owner shared, 2026-10-10")
  (item "coordinator-drift-metrics" :group "sprint"
   :title "Coordinator drift: a root brief and two metrics"
   :text "Every escalation carries the root brief, with a test that holds it. Every card cites a
    release-scope or fixes item directly, never through its parent card, which closes scope
    laundering. Drift is measured three ways: the share of newly written cards that cite no item in
    the release-scope or fixes file (cards carry their release); the Jaccard overlap of non-trivial
    tokens between a card's brief and its cited scope item, warning below a ratio such as 15%; and
    the branching ratio, consumer cards per producer card per wave. The blind spots these close are
    scope laundering, semantic dilution and scope bloat."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "a Gemini dialog the owner shared, 2026-10-10")
  (item "public-dashboard-served-from-files" :group "ops"
   :title "Serve the public sprint dashboard as files from one puller"
   :text "A fleet role serves the page and data as static files refreshed by one puller, with no reverse
    proxy to the coordinator machine."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "PR #5329")
  (item "fleet-monitoring-dashboard" :group "ops"
   :title "One monitoring dashboard for the sprint, load, network and store"
   :text "Redis metrics are scraped and one ready-made dashboard shows per-bench cards, load, memory,
    network and store latency."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2556")
  (item "friend-per-tier-model-choice" :group "friends"
   :title "Each friend's model per tier is decided and recorded"
   :text "Friend rows record which model serves each tier, so the deal and the friend agree on it. Friends
    set up with missing tiers are found and corrected."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04); PR #5387")
  (item "decision-evaluation" :group "sprint" :area "nova-decide"
   :title "Evaluating the decision model's decisions"
   :text "An evaluation harness that scores the decision model's past decisions against their outcomes, a detector for
    reads that bounced good work, a classifier for why a card is held, and the promotion of a decision
    from shadow to acting once it measures well."
   :why "New capability."
   :cards 4
   :date "2026-10-09")
  (item "swarm-finish-derives-shas-and-format" :group "sprint"
   :title "The swarm finish derives head and step shas from git and refuses an unformatted file"
   :text "The result finish is mechanical: shas come from git, not from the model, and an unformatted Go
    file is refused before landing."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5276")
  (item "record-replication-and-restore" :group "ops"
   :title "Replicate the sprint record to a second machine with a restore verb"
   :text "The playbook replicates the SQLite record continuously, and a restore from the replica matches
    the row count."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2555")
  (item "coordinator-leased-shared-resources" :group "sprint"
   :title "Shared resources are leased by the coordinator"
   :text "Benches, branches, ports and accounts are claimed and released only through coordinator verbs
    with leases, so a down holder cannot keep one."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "brief-lint-honest-attribution" :group "sprint"
   :title "Brief lint refuses hiding the model or harness"
   :text "nova-swarm lint gains a default rule that refuses a brief telling a worker to deny, hide or
    misstate its model or harness."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-status-counts-current-assignments" :group "friends"
   :title "Scope friend status and pong counts to current assignments"
   :text "Queue sync writes a versioned snapshot of the friend's current Ready and Working row so counts
    do not include old records."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5486")
  (item "swarm-lint-card-validation" :group "sprint"
   :title "nova-swarm lint card validation closes its escapes"
   :text "Drive letter paths, more than eight globs, comma only paths, unknown kinds, and no test on gated
    kinds pass lint. The card header shape and the worker card practice agree so a spec shaped card
    is admitted."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #1853; issue #1728; issue #2584; issue #2605; issue #2728")
  (item "swarm-lint-card-false-refusals" :group "sprint"
   :title "Card lint refuses valid cards: make gates and text inside fenced blocks"
   :text "Card lint rejects make-driven gates and flags parent paths or absolute paths quoted inside
    fenced blocks. It should accept both."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #1994; issue #2302; issue #3470")
  (item "review-guard-check-verb" :group "sprint"
   :title "A mechanical guard check as a review verb"
   :text "The unguarded verdict came from a model and was wrong about a third of the time. A verb reverts
    non-test files, runs the named tests and reports mechanically."
   :date "2026-10-10"
   :release "after v1.4"
   :origin "issue #2042")
  (item "merge-approval-head-from-disposition" :group "ops"
   :title "Approval at head is read from the disposition line"
   :text "The review API commit id can differ from the head the reviewer read. Merge checks parse the head
    named in the disposition and treat commit id as untrusted."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2037")
  (item "decide-review-uses-confidence" :group "sprint"
   :title "nova-decide review uses the provider confidence in its verdict"
   :text "The review verdict passes any rounded score of eight or more whatever the confidence. Low
    confidence lowers or flags the verdict."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #3393")))
