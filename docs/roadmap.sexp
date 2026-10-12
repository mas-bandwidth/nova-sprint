; The nova-sprint roadmap: the data ROADMAP.md is generated from.
; Read by internal/roadmapdoc through internal/worklang (data, never evaluated).
; Edit this file, then run `make roadmap`; never edit ROADMAP.md by hand.
; The shape is in internal/roadmapdoc's package comment.
(roadmap "v1"
 :title "nova-sprint roadmap"
 :text "This is the nova-sprint, nova-card and nova-work work after the point releases. The release plan
  (the owner, 2026-10-11) replaces the ladder of 2026-10-09. The v1.2.x point releases come first and
  are cut in turn: v1.2.10 (being cut), v1.2.11 small fixes, v1.2.12 tests and models only, v1.2.13
  contraction (fewer lines, no behaviour change), v1.2.14 the next small fixes; FIXES.md holds each
  one's scope and fixes, and an item below whose target is a v1.2.x release is waiting for its card.
  The aim is v1.2.x working pretty well, so it is the reference for intended behaviour: v1.3, unstable,
  is the re-architecture (the client/server conversion, then each layer secured from the bottom), built
  by the sprint running on v1.2.x and checked against it, rather than a blind rewrite; like getting a
  compiler self-hosting, then using it to build a better compiler. v1.4 is the stable release of v1.3,
  architecturally correct top to bottom. Every item's :release is one of four values (the owner,
  2026-10-12): v1.2.x (a point release, named in the item's text), v1.3, v1.4 (stabilization: tests,
  models and cleanups, no features) or later (a list, not a release); the roadmap tool refuses any
  other. The v1.3 work is grouped by theme below, in build order, behind the v1.3 test plan that every
  v1.3 entry lands after; each v1.3 item ends with the behaviour that proves it done (Done when),
  measurable on the bench, and names the items it absorbed in the de-duplication of 2026-10-12.
  Point releases continue until only architectural items are
  open: an area stops taking point fixes when a fix there is the same problem in another form, or would
  be its third, and each v1.3 architectural item names the point fixes it replaces. Friend work is
  dogfooded within the week, once more friends are online: friends alone, then friends and fleet
  together, each friend bug a point-release card. These items were the sprint half of nova-tools' roadmap and moved here with the split, with the
  sprint's docs and models; nova-tools' ROADMAP.md holds the rest. Point-release work is in FIXES.md, not here. Each item says what it is and why it
  waits. Where the code already has part of an item, the item says what exists and covers only the gap."

 :groups
 ((group "v13-plan"
   :title "v1.3: the test plan comes first"
   :text "The plan every v1.3 entry lands behind: the test tiers, the bench hosts per tier and the pass bar.
    Nothing in v1.3 lands before it (the owner, 2026-10-12). The document lives in nova-tools.")
  (group "v13-comms"
   :title "v1.3 A. Comms foundation, the sprint side"
   :text "The sprint as a client of the shared comms module (nova-tools): presence read, not owned; every
    acting verb gated on the nova-hook record; the seat's wait on the module's timers. Entries are in
    build order, the theme's test plan first.")
  (group "v13-lanes"
   :title "v1.3 B. One lane protocol, the sprint side"
   :text "The server's half of the lane protocol: friends and members as thin clients whose rows equal their
    live lanes; one deadline rule for both tables. Build order.")
  (group "v13-server"
   :title "v1.3 C. Sprint server conversion"
   :text "The umbrella of the release: the server is the one truth, everything keyed by epoch, the tick
    bounded and batched, every bound a setting; then each layer secured from the bottom as the gate to
    the v1.4 cut. Build order.")
  (group "v13-landing"
   :title "v1.3 D. Landing flow and merge tree"
   :text "Card heads land in a batch onto a branch per work stream, oldest first, one writer per stream;
    stream branches merge serially into dev; a stream based on dev or main is a defect. The whole-tree
    gate, the lander's own records, and the merge tree as the general case. Build order.")
  (group "v13-deal"
   :title "v1.3 E. Deal, reads and judgments"
   :text "The cost-ordered deal with blockers first, reads that always reach a capable reader, the attempt
    bound, judgments answered by rule, the verifier stage and the plan gate. Models first. Build order.")
  (group "v13-cards"
   :title "v1.3 F. Cards as records"
   :text "The owner, 2026-10-12: the roadmap is just future work that feeds into sprints, and also a place to
    push (defer) work in a sprint out to a future point. Cards as schema records that remember their
    repository and roadmap entry, the roadmap tool with its doctor, import and defer as nova-sprint verbs
    (the roadmap is a part of nova-sprint, it is all holistically the same thing; the sprint reads the
    sexp directly when it cuts cards from entries), nova-work and nova-card folded into nova-sprint, and
    the needs grammar with one parser. Build order.")
  (group "v13-tooling"
   :title "v1.3 G. Tooling hygiene"
   :text "The card lint as one umbrella, the ten brief-flaw classes of 2026-10-12 each as its own rule, and
    the add, brief and finish verbs the seat has lacked. Build order.")
  (group "v13-release"
   :title "v1.3 H. Release, adopt, CI and ledger hygiene, the sprint side"
   :text "The server install with its canary and rollback, and the release check gates. Build order.")
  (group "v13-routes"
   :title "v1.3 I. Routes, rate limits and spend, the sprint side"
   :text "Balances, the spend circuit breaker and friend cost accounting; the per-model budget itself is
    nova-tools'. Build order.")
  (group "v13-fleet"
   :title "v1.3 J. Bench operations, the sprint side"
   :text "The lane picks its bench by load; the lander's gate runs on a bench. Build order.")
  (group "v13-models"
   :title "v1.3 K. TLA+ models and test tiers"
   :text "The TLC catch-up pass, the models owed by theme, the test scaffolds and the chaos suites. Build
    order.")
  (group "v13-cleanup"
   :title "v1.3 L. Cleanup of old code"
   :text "Each deletion runs after its theme's new path carries the load. Build order.")
  (group "v13-docs"
   :title "v1.3 M. Docs"
   :text "The docs and brand suite, and the tool teaching the landing flow to a newcomer. Build order.")
  (group "v13-observe"
   :title "v1.3 N. Observability"
   :text "fsck as the integrity check, the alarms and measures, and the one dashboard, changed only on the
    owner's ask. Build order.")
  (group "sprint"
   :title "The sprint system"
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
  ; v1.3: the test plan comes first
  (item "v1-3-test-plan" :group "v13-plan" :area "tests"
   :title "The v1.3 test plan"
   :text "The mirror of nova-tools' entry of the same id: the plan, docs/TEST-PLAN-v1.3.md in nova-tools,
    is written before any v1.3 code. The tiers every theme ships with: unit (twin or fake store, parallel,
    under a minute); functional (real Redis and Postgres, one test per Lua function); integration (seat,
    member and friend together on a bench); stress and load (deals of 3,000 cards, a 140-width fleet, 200
    reads, the bus at 10 messages a second); TLA+ (a model with reversed witnesses, TLC on a bench);
    release checks (the parallel functional suite under two minutes). The Linux benches hetzner2, space
    and vision run the functional, integration, stress and TLC tiers; the studio never runs a tier. Green
    before and after each change, the measured numbers recorded. No v1.3 card lands without its tier's
    tests; every v1.3 entry below lands only after this one.
    Done when: nova-tools docs/TEST-PLAN-v1.3.md is committed naming every tier, bench host and load
    number, and nova-sprint add refuses a v1.3 card whose brief names no tier of it."
   :why "The owner, 2026-10-12: the plan is created before a line of v1.3 code is written."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  ; v1.3 A. Comms foundation
  (item "comms-test-plan" :group "v13-comms" :area "tests"
   :title "Comms foundation, the sprint side: test plan"
   :text "The tests of the sprint as a client of the comms module, written from docs/TEST-PLAN-v1.3.md
    before the theme's first implementation entry, which cites this one: unit tests on the twin store;
    functional tests on a real Redis against the comms functions; an integration run of the seat, a
    member and a friend hooked to one server on the bench; the stress drag (presence of 140 friends and
    members read at every tick); the friends-table model, checked by TLC on a bench; and its rows in the
    parallel release checks, under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test for the sprint's comms consumers, listed in docs/TEST-PLAN-v1.3.md, red before the theme's
    code and green after, with the 140-friend presence drag recorded."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "friends-table-presence-from-tools" :group "v13-comms"
   :title "Friends table reads presence owned by nova-tools"
   :text "The sprint consumes friend presence from the friend tool instead of owning it, so the friends
    table stays right while the sprint server is stopped or hung.
    Absorbs (de-duplicated 2026-10-12): A batch friend's presence is her engine's beat
    (batch-friend-presence-from-engine-beat); Supervised wake adapters for coordinator seats
    (coordinator-push-wake-adapters). The sprint reads presence from the comms module
    (the-comms-module-holds-presence-capability-and-timers, nova-tools roadmap) and the wake
    capability from nova-wake's record; the nova-tools items friend-presence-from-the-session and
    friend-daemon-liveness retire with the old comms paths. Its tests are comms-test-plan. Lands
    only after the v1.3 test plan (v1-3-test-plan). Done when: the friends table shows up, blocked,
    asleep and down from the comms presence record alone, with no beat loop of the sprint's own,
    pinned by a functional test on a real Redis."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-down-detection" :group "v13-comms"
   :title "Detect a friend AI that is unreachable or out of credits and route around it"
   :text "A friend counts as down when it cannot be reached or cannot work because credits ran out. The
    state is detected mechanically and its work is routed elsewhere.
    Absorbs (de-duplicated 2026-10-12): A held or down friend returns its cards to ready
    (held-friend-returns-dealt-and-started-cards); A broken session or a limit means down
    (friend-broken-session-and-limits-mean-down, nova-tools roadmap); Friends and machines noticed
    back up and made current (friend-back-up-detection-and-adopt, nova-tools roadmap); A broken
    friend session renews itself (friend-session-auto-recovery, nova-tools roadmap). A refusal, a
    limit or an empty balance is said once and reads down; a hold or down carries its cause and end;
    a returning friend adopts first (the adopt half is adoption-pipeline-automated, nova-tools
    roadmap); a down friend's cards bounce back at the tick; a partner who goes down is pushed to
    the other. Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a friend whose
    presence lapses or whose provider refuses reads down within one tick, her dealt and started
    cards return to ready, and one judgment is raised, pinned by a functional test with a planted
    refusal."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #3185")
  (item "nova-sprint-gates-on-the-nova-hook-record" :group "v13-comms" :area "nova-sprint"
   :title "nova-sprint gates every acting verb on the nova-hook record"
   :text "The sprint side of the comms module: every verb that acts (take, finish, deal, land, set, the
    seat's judgments) refuses until the caller is hooked, through the one shared gate module that
    nova-hook (nova-tools roadmap) provides, and the seat's push loop is installed by the hook, never by
    a seat install verb of the sprint's own. The v1.2.17 row every-verb-refuses-until-its-pushes-are-hooked
    of the fixes ledger is this entry's point-release half; the bus-presence store card of the same name
    is its evidence.
    Absorbs (de-duplicated 2026-10-12): A seat install verb installs the push loop
    (seat-install-push-verb). Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every
    acting nova-sprint verb refuses with the hook line until the caller is hooked and runs once it
    is, pinned by a functional test over the shared gate module."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the bus-presence store cards of 2026-10-11, from the owner's direction: nova-hook is the one way in")
  (item "a-seat-wait-survives-a-reconnect-and-a-restart" :group "v13-comms" :area "nova-sprint"
   :title "A seat wait survives a reconnect and a restart"
   :text "The seat's wait is a client of the comms module's timers and listener: a store reconnect or a
    server restart during a wait neither loses the push nor ends the wait early, and the wait resumes from
    the drain cursor the comms link holds. The sprint's own timers are the comms module's.
    Absorbs (de-duplicated 2026-10-12): Timers on the sprint system that wake an actor at a chosen
    time (sprint-timers). Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a seat
    wait outlives a store reconnect and a server restart on the bench and returns the push it was
    waiting for, pinned by a functional test that restarts the server mid-wait."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the freeze triage draft of 2026-10-12")
  (item "local-only-address-check" :group "v13-comms"
   :title "One function for the loopback-or-tailnet address rule"
   :text "Gather every address check for the sprint and its stores into one function, cited from each
    caller, so local-only mode has one definition.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: one loopback-or-tailnet
    function is the only address check in the tree and a class test finds no other."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  ; v1.3 B. One lane protocol
  (item "lanes-test-plan" :group "v13-lanes" :area "tests"
   :title "One lane protocol, the sprint side: test plan"
   :text "The tests of the server's half of the lane protocol, written from docs/TEST-PLAN-v1.3.md before
    the theme's first implementation entry, which cites this one: unit tests on the twin store;
    functional tests on a real Redis, one per Lua function of the lanes; an integration run of a friend
    and a member as thin clients on the bench; the stress drag (a 140-width fleet, every row equal to its
    live lanes for an hour); the lane models with their reversed witnesses, checked by TLC on a bench;
    and its rows in the parallel release checks, under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test or TLC case for the sprint side of the lane protocol, listed in docs/TEST-PLAN-v1.3.md, red
    before the theme's code and green after, with the 140-width fleet number recorded."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "sprint-friend-redeal-and-lane-models" :group "v13-lanes"
   :title "TLA+ models for friend redeal, friend rows and live friend lanes"
   :text "The sprint models cover a card taken back from a friend and redealt, and the live friend lane
    judgments, each with reversed witnesses. A CI guard keeps the model records current.
    Re-aimed at the lane protocol model: a thin client asks the server what it holds at its epoch,
    runs, reports by the server's ids and asks again. Its tests are lanes-test-plan. Lands only
    after the v1.3 test plan (v1-3-test-plan). Done when: the friend redeal, row and live-lane
    models have reversed witnesses and current TLC records, and the lane code cites them."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5511; PR #5556; PR #5239; PR #5508; PR #5510")
  (item "unify-friend-and-member-deadline-rule" :group "v13-lanes"
   :title "One deadline rule for friends and fleet members"
   :text "The friend deadline rule and the member deadline rule become one function with one spec
    paragraph used by both tables.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: one deadline function serves
    both the friend and the member table and a test shows the same input giving the same deadline in
    both."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-row-matches-live-lanes" :group "v13-lanes"
   :title "A friend's row equals her live lanes"
   :text "A friend's working set on the dashboard must equal her live child lanes, and work and its report
    are one act.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a friend's row equals her live
    lanes after every tick over an hour at width on the bench, pinned by a functional test that
    kills a lane."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "friend-status-counts-current-assignments" :group "v13-lanes"
   :title "Scope friend status and pong counts to current assignments"
   :text "Queue sync writes a versioned snapshot of the friend's current Ready and Working row so counts
    do not include old records.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: friend status prints a
    versioned snapshot of the friend's current rows that equals the server's, pinned by a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5486")
  (item "friend-report-first-lines-shape" :group "v13-lanes"
   :title "Pin the first two lines of a friend report"
   :text "A friend report starts with an exact Verdict line and an exact Head line, and the friend card
    sync keeps reading older reports leniently.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a friend report with the wrong
    first two lines is refused at finish naming the line, pinned by a lint test per shape."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  ; v1.3 C. Sprint server conversion
  (item "server-test-plan" :group "v13-server" :area "tests"
   :title "Sprint server conversion: test plan"
   :text "The server conversion's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: unit tests on the twin store; functional tests on a real
    Redis and Postgres, one per Lua function the server adds; an integration run of seat, member and
    friend against one server on the bench; the stress drags (deals of 3,000 cards, a 140-width fleet,
    the tick under one second, every verb under a second while busy); the server's models with their
    reversed witnesses, checked by TLC on a bench; and its rows in the parallel release checks, under two
    minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test or TLC case for the server conversion, listed in docs/TEST-PLAN-v1.3.md, red before the
    theme's code and green after, with the 3,000-card deal and the one-second tick recorded."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "client-server-conversion" :group "v13-server"
   :title "Convert every client-held copy of sprint state to client/server"
   :text "The first v1.4 rewrite step. Each item in the 2026-10-10 inventory where a client keeps its own copy
    of sprint state is converted in place so the server is the one truth and clients ask it: the friend
    daemon's lanes and epoch, the member loop's lane table and local recovery (one lane protocol for friends
    and fleet), the seat's file proof, friend messages through folders and the separate bus Redis, config
    split across Postgres and the store, hosts following versions, the dashboard's own snapshot, and the
    lander as a second truth in git. All state is keyed by epoch. A TLA+ model of the lane protocol comes
    first. Faults found while the point releases run are recorded here as input.
    Absorbs (de-duplicated 2026-10-12): Friends are told of cards and push work reliably
    (friend-notification-and-delivery-reliability); The system sends the width goal to idle or
    under-width friends (machine-sends-width-goal-to-friends); Friend delivery is a visible state
    (friend-delivery-state); Friend lifecycle and widths run through the system: wake, take, refill,
    redistribute (friend-lifecycle-automation); All coordination state in Redis, batched, driven by
    Go tools (coordination-state-in-redis-and-go); Decisions are made centrally; machines only
    execute what the queue says (central-decisions-fleet-executes). The umbrella of v1.3, not v1.4:
    the roadmap's own text says v1.3 is this conversion and v1.4 its stable release. Model first
    (the lane protocol model, then the server's). The lane half is the one lane protocol for friends
    and fleet: the in-progress v1.2.6 fixes friend-lanes-are-thin-clients (nova-tools) and
    lanes-ask-the-server-what-they-hold are its first steps, one-shot-workers (nova-tools roadmap)
    its rule, and the batch delivery items named here retire with it. Every tool reaches the store
    through redisconn. Its tests are server-test-plan. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: no client keeps a copy of sprint state (a class test finds no lane
    table, cached epoch or local recovery in a client), the seat, dashboard, lander and friends ask
    the server, and a 140-width fleet plus friends runs a day on the bench with every judgment
    traceable to one server record."
   :why "Point releases continue meanwhile; their faults are the input to this rewrite cleanup."
   :date "2026-10-11"
   :release "v1.3"
   :origin "the owner, 2026-10-11")
  (item "friend-take-epoch-fence" :group "v13-server"
   :title "Fence a friend take across epoch boundaries"
   :text "A friend take is rejected when the brief's epoch differs from the live epoch, and the epoch is
    carried into the store step so a concurrent clear fences it.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a take carrying an older epoch
    than the brief's is refused, pinned by a functional test across a clear."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5268")
  (item "sprint-start-stamped-by-verb" :group "v13-server"
   :title "Opening a sprint stamps its start on every machine"
   :text "The sprint verb records the start once, and bench counts read it, so counts never include an
    earlier sprint.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the sprint verb stamps the
    start once and a second start does not move it, pinned by a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2727")
  (item "table-single-write-new-stream" :group "v13-server"
   :title "The table tells the truth: one write per new stream"
   :text "Adding a stream records its rows and places in one write, so the table never disagrees with the
    facts.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: adding a stream is one store
    write, counted by the round-trip counter in a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "tick-under-one-second-at-load" :group "v13-server"
   :title "The tick stays under one second at load"
   :text "Drain and deal take 0.4 to 0.7 seconds at load, so the tick gate is over one second; bring the
    whole tick under it.
    Absorbs (de-duplicated 2026-10-12): Batch tick round trips and alarm on slow store
    (store-latency-and-batched-ticks). The method: Lua in Redis, one round trip per operation,
    always batched; a poisoned queued change never stops the drain; the slow tick parts profiled at
    3,000 cards and MaxTickWall never raised. Lands only after the v1.3 test plan (v1-3-test-plan).
    Done when: every tick finishes under one second at CI load with 3,000 cards and a 140-width
    fleet, under twenty round trips per tick, measured on the bench and recorded with the run."
   :date "2026-10-10"
   :release "v1.3"
   :origin "v1.2.4 held ledger; PR #5557")
  (item "server-cold-start-warmup" :group "v13-server"
   :title "Server warms its in-memory mirror before ticking"
   :text "A restarted server warms its in-memory copy of the store before its first tick, so early ticks
    do not miss the deadline and crash-loop.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the server warms its in-memory
    mirror before its first tick and the first tick after a restart is under the gate, pinned by a
    functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "card-verb-bounded-reads" :group "v13-server"
   :title "One card read touches only that card"
   :text "The card verb reads the whole work table and log on each call; make a read use per-card indexes
    and finish under 100 ms at 2,000 cards.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a card read touches only that
    card and answers under 100 ms with 2,000 cards in the store, measured in a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "server-beats-and-reads-off-the-line" :group "v13-server"
   :title "Server beats and reads run off the control line; a gone caller is never run"
   :text "Verbs that only read or beat must not queue behind the single line of control, and a verb whose
    caller is gone is dropped.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: beats and reads travel off the
    control line and a gone caller is dropped within its bound, pinned by a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5325")
  (item "verbs-answer-fast-and-stop-is-instant" :group "v13-server"
   :title "Verbs answer fast, and stop is instant"
   :text "The server answers verbs in well under a second even while busy, and stop writes its state in
    one store step without waiting for the tick.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every verb answers under a
    second while the server is busy and stop is one store step, measured on the bench at load."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "verb-version-skew-and-restart-safety" :group "v13-server"
   :title "Verbs are safe across a switch and across client versions"
   :text "A verb sent during a server switch gets a restarting reply and waits. Every verb of one client
    version is readable by the last release.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a verb against a restarting
    server gets the restarting reply and every verb reads the last release's records, pinned by
    tests over two builds."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "down-member-never-hangs-loops" :group "v13-server"
   :title "A down machine must never hang the dealer, sampler or harvest"
   :text "Every remote call has a timeout and each host is isolated, so a dead machine and stale
    connection sockets cannot freeze the loops.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a planted unreachable member
    delays no tick past its bound and other hosts are unaffected, pinned by a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2009")
  (item "member-beat-age-at-tick-read" :group "v13-server"
   :title "A beat's age is measured at the tick's read so a long tick downs no member"
   :text "A tick that runs long over a slow link no longer marks members down who are beating on time.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: beat age is measured at the
    tick's read and a slow tick never calls a live member down, pinned by a test with a slowed tick."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5015")
  (item "sprint-server-memory-wedge" :group "v13-server"
   :title "The sprint server does not grow without bound or stop answering"
   :text "The server once reached a very large resident size and stopped answering verbs while the fleet
    stalled behind it. Memory is bounded and a watchdog restarts a wedged server.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: server memory stays bounded
    over a day at load and a planted wedge is restarted by the watchdog within its bound, on the
    bench."
   :date "2026-10-10"
   :release "v1.3"
   :origin "nova-sprint issue #38")
  (item "client-defaults-to-local-server" :group "v13-server"
   :title "Verbs default to the local sprint server"
   :text "With no store settings, nova-sprint verbs go to the local server address, so no wrapper script
    or secret is needed.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every verb reaches the local
    server with no address flag and prints the address it used, pinned by a transcript test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-redis-acl-auth-wiring" :group "v13-server"
   :title "The sprint client consumes the Redis ACL credential and never echoes it"
   :text "The card push and table verbs accept the injected ACL password from the environment. Address
    errors never print credentials.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the client authenticates with
    the ACL credential, never prints it, and a class test finds no credential in any output."
   :date "2026-10-10"
   :release "v1.3"
   :origin "nova-sprint issue #36")
  (item "sprint-inbox-and-seat-guards" :group "v13-server"
   :title "Single inbox judgment and password-free seat connection"
   :text "The inbox view prints one bounded judgment, and the sprint seat reaches its store without a
    password in the environment.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the inbox raises one bounded
    judgment per fault and the seat reaches its store without a password, pinned by functional
    tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-fleet-operation-guards" :group "v13-server"
   :title "Guards for fleet staging, trials and width changes"
   :text "Refuse staging when every member is ineligible, bound the fleet trial, and make a width change
    one operation, each with a model scenario and a named test.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: staging, trials and width
    changes each have a guard with a model and a refusal test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "secure-the-layers-bottom-up" :group "v13-server"
   :title "Secure the sprint's layers from the bottom up"
   :text "Name the layers that are really there and secure each in turn, lowest first, each an anchor so a
    later fault stops there: L0 machine (one join entry point, drift check, doctor member-env green); L1
    transport (loops re-dial, seat proof, push delivery); L2 providers and routes (the global limiter live, a
    provider fault never fails a card); L3 deal and take (a machine fault holds the machine, no silent
    ready); L4 read (no lost verdict, no cannot-ask while readers idle); L5 land (ledger merge done,
    judgments rare and hands-off); L6 observability (doctor and pushes catch every fault first). A layer is
    secured when it has a TLA+ model with reversed witnesses checked by TLC, tests on a real store including
    every refusal, a second reader's probes green, real use by the seat and by friends, docs, and a lock
    agreed with the owner.
    Last in v1.3 and the gate to the v1.4 cut, after the conversion; each layer locked with the
    owner. Lands only after the v1.3 test plan (v1-3-test-plan). Done when: layers L0 to L6 each
    have a model with reversed witnesses checked by TLC, tests on a real store including every
    refusal, a second reader's probes green, real use by the seat and friends, docs, and a lock
    recorded with the owner."
   :why "Comes after the client/server conversion of the 2026-10-10 inventory, so the layers are secured on
    the converted design rather than on client-held state."
   :date "2026-10-11"
   :release "v1.3"
   :origin "the owner, 2026-10-11")
  ; v1.3 D. Landing flow and merge tree
  (item "landing-test-plan" :group "v13-landing" :area "tests"
   :title "Landing flow and merge tree: test plan"
   :text "The landing theme's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: unit tests over throwaway repositories; functional tests
    of the lander on a real store and a bench tree; an integration run of a stream of cards landing into
    dev through a stream branch on the bench; the stress drag (a 50-card stream batch landed and
    promoted, timed); the branch topology and merge tree models with their reversed witnesses, checked by
    TLC on a bench; and its rows in the parallel release checks, under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test or TLC case for the landing flow, listed in docs/TEST-PLAN-v1.3.md, red before the theme's
    code and green after, with a 50-card stream batch landing timed."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "branch-topology-model" :group "v13-landing"
   :title "Model the branch topology"
   :text "Write a model of the branch rules so a side branch, a temporary landing branch or a red base is
    caught by rule.
    With tla-landpass-dev-moves-only-by-stream-merge: dev moves only by a stream merge. The four
    v1.2.x refusals land first as the stopgap. Its tests are landing-test-plan. Lands only after the
    v1.3 test plan (v1-3-test-plan). Done when: the branch topology model has its reversed witnesses
    and a current TLC record, and the lander cites it at every base check."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "protected-bases-promotion-stream" :group "v13-landing"
   :title "Protect dev and main bases; mark a promotion stream"
   :text "Refuse adding a card with base dev or main outside a promotion stream. A stream can be marked as
    promotion, and land refuses a batch that would reach a protected base.
    Absorbs (de-duplicated 2026-10-12): One base branch, watched by the system
    (single-base-branch-discipline); Landing into a protected branch opens a pull request
    (land-protected-stream-pull-request); Move a stream to a live base and show the health of every
    base (stream-base-move-and-bases-view). Refusals at add, stream set and card base; a lander
    refusal of a whole stream is a judgment naming the fix; the doctor and the tick judge a card
    landing on dev or main; dev-is-behind names the branches and real card ids. Lands only after the
    v1.3 test plan (v1-3-test-plan). Done when: add, stream set and land each refuse a base of dev
    or main outside a promotion stream and a card whose base differs from its stream's, pinned by
    tests, and the doctor finds none on the bench fleet."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "auto-promotion-and-drift-alarms" :group "v13-landing"
   :title "Automatic promotion to dev and drift alarms"
   :text "The system promotes the base to dev itself and raises an alarm when the live server, a card
    branch or the base drifts.
    Absorbs (de-duplicated 2026-10-12): Promote merges from a throwaway branch after a whole-tree
    gate (promote-from-throwaway-branch). Stream branches merge into dev one at a time, by the
    machine; promotion from a throwaway branch after a whole-tree gate. Lands only after the v1.3
    test plan (v1-3-test-plan). Done when: the system promotes each stream branch into dev serially
    with no hand step, a drift between a stream and dev raises an alarm within a tick, and a week on
    the bench shows no hand promotion."
   :date "2026-10-10"
   :release "v1.3"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "land-gate-whole-tree-and-base-health" :group "v13-landing"
   :title "Gate landing on the whole tree and treat base health as one fact"
   :text "The lander's gate runs the wider package and whole-tree checks, so a batch cannot turn the base
    red. A red base stops landing with one judgment and resumes streams by rule, and the base and
    development branch are synced every cycle.
    Absorbs (de-duplicated 2026-10-12): Landing survives a red base, a red batch and a rejected push
    (landing-resilience); One lander per clone, gates run on a bench (lander-lock-and-bench-gates);
    The tree gate tells a bench fault from a red test (lander-bench-fault-classifier); Land retries
    a flaky red gate once and re-polls unknown mergeability (land-gate-flaky-retry); Land ejects a
    card that cannot merge and lands the rest (land-eject). The gate runs the unit tier of the
    touched packages and their reverse dependencies; a red base is one fact, never blamed on the
    card; a tree-gate failure in a file the card never touched is rerun once; the gate tells a bench
    fault from a red test; a red batch ejects one card; the bench half (one lander per clone, the
    gate on a bench) is bench-chosen-by-load. Lands only after the v1.3 test plan (v1-3-test-plan).
    Done when: a landing never goes onto a red base, a red batch ejects the one red card and lands
    the rest, and a bench fault is reported as such, each pinned by a functional test on a throwaway
    repository."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04); card from the sprint store (2026-10-10)")
  (item "lander-records-own-landings" :group "v13-landing"
   :title "The lander records its own landings"
   :text "A pushed head never waits in merging for a manual merge step. The lander writes the landing
    record itself when it pushes a stream's batch.
    Absorbs (de-duplicated 2026-10-12): System runs the gate and owns returned, verified and landed
    transitions (landing-gate-and-transitions-owned-by-machinery). Lands only after the v1.3 test
    plan (v1-3-test-plan). Done when: the lander writes the landing record itself in the same step
    as the merge and verify-landed finds no landing without a record after a 50-card batch."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "lander-bench-tree-own-git" :group "v13-landing"
   :title "The lander bench tree carries its own git, not a pointer"
   :text "The bench tree git points at a worktree that exists only on the coordinator machine, so git
    fails on benches; give the bench tree its own repository.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the bench tree carries its own
    git and a landing needs no git from the coordinating machine, pinned by a functional test on a
    bench."
   :date "2026-10-10"
   :release "v1.3"
   :origin "v1.2.4 held ledger")
  (item "stage-carries-prior-head-onto-moved-base" :group "v13-landing"
   :title "The stage carries a prior attempt's head onto a moved base"
   :text "A lane never carries an earlier attempt onto a moved base by hand. The stage does it and reports
    the result.
    A rework at the tip re-derives PATHS through renames. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: a prior head is carried onto a moved base and its PATHS re-derived
    through a rename, pinned by a test on a throwaway repository."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "merge-simulate-shared-commit-stacks" :group "v13-landing"
   :title "Merge simulation and planning detect stacked pull requests by ancestry"
   :text "Two members sharing a commit are not a conflict. The planner finds stacks by ancestry, not by
    base ref.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: merge simulate finds shared
    commit stacks by ancestry and reports them, pinned by a test with two stacked branches."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2036")
  (item "merge-gate-evidence-capture" :group "v13-landing"
   :title "The merge gate keeps raw test output and full review text, and stores its receipt"
   :text "The batch test step keeps the raw go test JSON stream, paginated review captures are not
    truncated at the output cap, and the gate's receipt is stored where a reader can fetch it.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the raw go test JSON, the
    untruncated review text and the receipt are stored for every landing and read back by one verb,
    pinned by a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2626; issue #2654; issue #2693; issue #3183")
  (item "sprint-reliability-repairs-staging" :group "v13-landing"
   :title "Selective sprint reliability repairs: rework priority and staged land gates"
   :text "Rework cards get fix priority and land gates are staged from the tested tree.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a rework card gets fix priority
    and gates are staged from the tested tree, pinned by tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5451")
  (item "generated-ledger-merge-conflicts" :group "v13-landing"
   :title "Generated ledgers merge without conflict and rework starts at the tip"
   :text "Two cards that shrink the same ledger conflict by line. Regenerate the ledger at the merged
    tree, and stage rework from the current head.
    Absorbs (de-duplicated 2026-10-12): Plan a ledger of cards in one wave
    (card-ledger-plan-single-wave); Append-only and generated files merge themselves
    (append-only-files-auto-merge). Mostly shipped in v1.2.13 and v1.2.15
    (lander-resolves-ledger-conflicts, the-lander-merges-evidence-ledgers); the residual is rework
    from the tip and a ledger's cards planned in one wave. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: two cards editing docs/roadmap.sexp land in one batch with both
    entries present and the pages regenerated, and a rework starts from the tip, pinned by a
    functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5215")
  (item "merge-tree" :group "v13-landing" :area "nova-sprint"
   :title "A merge tree for promotion"
   :text "Promotion through a merge tree: a root, a shadow, and a switch between them; and a stream's
    batch built from its cards' branches. Its TLA+ model goes with it: the merge-tree half of card
    tla-merge-tree-promotion-b. The promotion half of that card models shipped code and stays in v1.4.
    Absorbs (de-duplicated 2026-10-12): Merge streams: the stream is the landing unit with open,
    take, batch, land verbs (merge-streams-landing-unit); Promote, rebase and stream batch merge as
    verbs (promote-rebase-and-stream-batch-merge-verbs). After the owner answers the four questions
    of 2026-10-12 (one stream per root merge or several; the tree default at switch or after a
    shadow; whether the node is still worth building; card ids into roadmap.sexp); the landing flow
    is its depth-two case and lands first; its model is tla-merge-tree-promotion. Lands only after
    the v1.3 test plan (v1-3-test-plan). Done when: a root, a shadow and a switch between them
    promote a stream batch built from its cards' branches on the bench, the model's TLC record is
    current, and clear loses no card headed to the roadmap."
   :why "New capability."
   :cards 4
   :date "2026-10-09"
   :release "v1.3")
  ; v1.3 E. Deal, reads and judgments
  (item "deal-test-plan" :group "v13-deal" :area "tests"
   :title "Deal, reads and judgments: test plan"
   :text "The deal theme's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: unit tests on the twin store; functional tests on a real
    Redis, one per Lua function of the deal and the reads; an integration run of a deal to friends and
    fleet with reads on the bench; the stress drags (deals of 3,000 cards over a 140-width fleet, 200
    reads started under 30 seconds each); the dealing, presence, card and lander models with their
    reversed witnesses, checked by TLC on a bench; and its rows in the parallel release checks, under two
    minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test or TLC case for the deal theme, listed in docs/TEST-PLAN-v1.3.md, red before the theme's
    code and green after, with the 200-read and 3,000-card deal drags recorded."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "sprint-models-dealing-and-presence" :group "v13-deal"
   :title "TLA+ models for dealing, presence, leases and priority"
   :text "Model the cap deal, deal routes and width, presence beats and leases, down windows and
    take-back, and the priority ladder in TLA+, checked with TLC.
    Its tests are deal-test-plan. Lands only after the v1.3 test plan (v1-3-test-plan). Done when:
    the cap deal, routes, width, beats, leases and priority ladder each have a model with reversed
    witnesses and a current TLC record on a bench."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04);
    cards moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-card-and-lander-tla-models" :group "v13-deal"
   :title "TLA+ models for card counters, the card lifecycle, holds and the lander"
   :text "Model the attempt, generation and epoch counters, the card columns, held cards and sentinels,
    and the lander streams. Each model is checked and cited from the code.
    The dealfix catch-ups (the start level never moves a card off a free lane; WhoPreference reads
    are rebalanced) and tla-readcards-review-has-a-live-read-or-a-judgment belong here. Lands only
    after the v1.3 test plan (v1-3-test-plan). Done when: the counters, lifecycle, holds and lander
    stream models have reversed witnesses and current TLC records, and every counterexample was
    checked against the code by hand."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); cards moved out of the sprint (work record, 2026-10-04)")
  (item "deal-by-cost-and-priority-order" :group "v13-deal"
   :title "The deal fills free lanes by cost, honors priority, and routes frontier cards to friends"
   :text "Free working lanes fill first across friends and the fleet, cheapest capable first, then the
    ready stack grows to twice the width. Priority ladder order and tie breaks hold, and a frontier
    card goes to a friend whose tiers hold it.
    Absorbs (de-duplicated 2026-10-12): Dealing policy (dealing-policy); The friend deal fills idle
    friends before topping up a full one (friend-deal-fill-idle-friends-first); Priority is a plain
    set; a blocker is stored, dealt first and evicts lower work (priority-plain-set-blocker-rung);
    Deal to subscription executors first and skip those that are down (subscription-first-dealing);
    One dealer fills each member in proportion to its free slots
    (dealer-proportional-per-member-queues); Findings go back to the same implementer
    (findings-to-the-same-implementer); The reader is a different model from the implementer
    (reader-model-diversity). The policy: free lanes first across friends and fleet, the cheapest
    capable member first, subscription friends before metered routes, an idle friend before topping
    up a full one, ready overflow at most twice the width, priority stored at every level with a
    blocker evicting lower work, PriorityFix at the front, reader priority apart, reads to a model
    other than the implementer's, rework to the same worker, shared paths in parallel, and priority
    as a group property (ideas #854); the start level never moves a card off a free lane and the
    deal honours a TLC requirement in the beat. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: a deal of 3,000 ready cards over a 140-width fleet and friends
    fills every free lane cheapest first with blockers ahead, leaves ready under twice the width,
    and matches the model's trace, measured on the bench and recorded."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5531; PR #5545; PR #5552")
  (item "second-tick-leveling-after-deal" :group "v13-deal"
   :title "A second tick must not move a card the first deal placed"
   :text "Leveling runs at tick start, but the deal places cards later in the tick without regard to it.
    Make the deal and the level agree.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the deal and the level agree on
    the second tick with no card moved off a free lane, pinned by a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #5408; card from the sprint store (2026-10-10)")
  (item "friend-dealing-correctness" :group "v13-deal"
   :title "Dealing to friends honors tier, start and down state"
   :text "Cards are re-dealt when the holder does not serve the card's tier. A card counts as working only
    once started, cards pinned to a down friend raise one judgment, and the overload alarm covers
    friends.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a friend's tier is honoured,
    started means working, and a down friend raises one judgment, each pinned by a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "card-tier-ceiling-before-deal" :group "v13-deal"
   :title "A card's tier is its ceiling before the first deal"
   :text "Before a deal, and in a store with no route, the card's own tier is its ceiling and the card
    verb prints it. The model calls the one tier function.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a card's tier is its ceiling
    before the first deal and a lower-tier member is never dealt it, pinned by a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5243")
  (item "rework-keeps-who-pin-and-each-finding" :group "v13-deal"
   :title "Rework keeps the owner pin and each finding"
   :text "A rework of a card pinned to one friend stays with that friend. A grouped rework gives each card
    its own finding instead of the first card's text.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a rework stays with the pinned
    friend and each card carries its own finding, pinned by functional tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "reads-route-to-capable-readers" :group "v13-deal"
   :title "Reads reach a capable reader and keep flowing"
   :text "Heavy and frontier reads go to readers that can serve them, read slots are separate from work
    lanes and delivered like cards, and a reader row with no process behind it is flagged. A read
    whose child never ran is asked again without counting.
    Absorbs (de-duplicated 2026-10-12): Ask again when a read ended with no verdict, and keep live
    reads across restart (read-retry-after-no-verdict); Read cards are the only read path; every ok
    primary in review gets its read (read-cards-sole-path); Judgments and reads start within seconds
    (judgment-and-read-start-latency); Any read can be served by a friend with room
    (reads-served-by-friend-cards); A frontier read goes to a reader row that declares frontier
    (frontier-reader-rows); The coordinator can record its own heavy read as a read row
    (coordinator-heavy-read-record). Heavy and frontier reads go to capable readers with read slots
    apart from lanes; a read that never ran or returned no verdict is re-asked without counting; a
    read returned ran=false is re-dealt at once and counted against the reader; live reads survive a
    restart; the old ask path is removed and every ok primary gets its read (a dealt read is held or
    returned and ask agrees with itself; accept-head lands a named ok head); mechanical judgments
    are answered in-step and a read starts under 30 seconds. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: 200 reads over the bench fleet each start within 30 seconds on a
    capable reader, none is lost across a server restart, and audit-unlanded lists no ok head
    without a read, measured and recorded."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-ok-counts-after-read" :group "v13-deal"
   :title "A friend card counts ok only after a reader accepts it"
   :text "A friend card that is only reported finished is not counted ok until a reader accepts it or it
    lands. This keeps the friends table honest.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a friend card counts ok only
    after a reader accepts it, pinned by a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "merge-typed-verdict-parsing" :group "v13-deal"
   :title "Parse typed approve verdicts and release untyped holds"
   :text "A typed approve line is counted, and an untyped hold can be released by a read.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a typed approve is counted and
    an untyped hold is releasable, pinned by tests over the verdict forms."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #2454")
  (item "false-bounce-detector" :group "v13-deal"
   :title "Detect false bounces by readers"
   :text "Readers must not bounce cards for harness failures or trailer-only misreads. A detector
    separates these from real defects.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a harness failure or a misread
    is classified apart from a real defect with its evidence, pinned by fixtures of each."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); cards moved out of the sprint (work record, 2026-10-04)")
  (item "decide-review-uses-confidence" :group "v13-deal"
   :title "nova-decide review uses the provider confidence in its verdict"
   :text "The review verdict passes any rounded score of eight or more whatever the confidence. Low
    confidence lowers or flags the verdict.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a low-confidence verdict is
    lowered or flagged before it counts, pinned by tests over the confidence bands."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #3393")
  (item "a-card-runs-at-most-max-attempts-between-coordinator-decisions" :group "v13-deal" :area "nova-sprint"
   :title "A card runs at most max-attempts between coordinator decisions"
   :text "The sprint row carries max-attempts, a setting; a card that has failed that many times since the
    coordinator's last decision on it is held with a judgment, never dealt again by the machine. A
    coordination failure is never the worker's: the counter half (attempts the worker could not run are
    not counted against the brief) and the routing half (a finish that names a coordination fault is
    routed to the coordinator, not redealt) are its two slices. A launch failure is known in seconds and
    raised; a launcher refusal hands the card back and a dealt label is not re-dealt; ok percent counts
    only work the worker could do. A card's consumer history (a-card-carries-its-consumer-history, fixes
    v1.2.15) is the record it reads.
    Absorbs (de-duplicated 2026-10-12): Worker ok percent counts only work the worker could do
    (worker-ok-percent-counts-real-work); A failed card is known in seconds and raised, not redealt
    (sprint-failed-card-fast-and-raised); Hand back launcher refusals and refuse re-dealing a dealt
    label (deal-handback-and-redeal-guard). Lands only after the v1.3 test plan (v1-3-test-plan).
    Done when: a card is never dealt past the row's max-attempts without a coordinator decision, a
    launch failure is known in seconds and raised rather than redealt, and ok percent counts only
    work the worker could do, each pinned by a functional test."
   :why "The owner asked for it directly, 2026-10-11."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the freeze triage store cards the-sprint-row-carries-max-attempts and a-card-runs-at-most-max-attempts-between-coordinator-decisions, 2026-10-12")
  (item "judgments-answered-by-rule" :group "v13-deal"
   :title "Routine judgments are answered by rule in the tick"
   :text "Reworks, take-backs, path blockers and red-promotion fix cards are answered by named rules in
    the tick and logged. Only real judgments reach the coordinator.
    Absorbs (de-duplicated 2026-10-12): A hold that names its fix is applied by the system
    (hold-fix-applied-by-machine); Failures and dealing never wait on the coordinator
    (deal-never-waits-on-coordinator); The system feeds itself: inherited twins, rule answers and an
    idle alarm (machine-feeds-itself-idle-alarm). A hold naming its fix is applied by the system;
    failures are handled by the system and friends kept supplied; a wait holds a condition judgment
    by kind and re-raises at review time; rule 2 accepts waiting-on-a-need after a withdrawal; a
    withdrawn attempt answers its deadline alarm; a-card-holding-judgment-is-never-left-unanswered
    (fixes v1.2.14) is the first slice. The brief-flaw rules 7, 8 and 10 are their own entries after
    this one. Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every judgment with a
    rule is answered in the tick that raises it, a planted hold naming its fix is applied with no
    hand step, and a day on the bench shows no card held by an answered judgment, recorded."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-reconcile-and-stall-ladder" :group "v13-deal"
   :title "Reconcile friends and run the stall ladder every tick"
   :text "The tick reconciles each friend's store with her inbox and outbox, and a mechanical stall ladder
    returns cards held by a stalled friend with no step needing the coordinator.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the tick reconciles each
    friend's store and a stalled card climbs the mechanical ladder on schedule, pinned by functional
    tests with a planted stall."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "deal-waits-for-the-depends-on-land-in-the-staged-tip" :group "v13-deal" :area "nova-sprint"
   :title "A dependent card is dealt only once its DEPENDS-ON land is in the staged tip"
   :text "Brief-flaw class 7 of 2026-10-12: a card whose DEPENDS-ON has landed on its base but is not yet in
    the tip the card would stage from was dealt and failed on code it could not see. The deal waits for
    the staged tip to carry the land, and says so in the card's wait reason.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a dependent card is not dealt
    until its DEPENDS-ON land is in the staged tip, pinned by a functional test that lands the
    dependency late."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 7")
  (item "bound-judgment-never-quotes-a-stale-finding" :group "v13-deal" :area "nova-sprint"
   :title "A bound judgment never quotes a stale finding"
   :text "Brief-flaw class 8 of 2026-10-12: a judgment raised at the attempt bound quoted the first
    attempt's finding after later attempts had answered it. The judgment quotes the latest attempt's
    finding and names the attempt it came from.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a bound judgment quotes only
    the latest attempt's finding and a planted stale finding is refused, pinned by a test."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 8")
  (item "third-paths-widening-is-a-brief-defect-judgment" :group "v13-deal" :area "nova-sprint"
   :title "A third PATHS widening goes to the coordinator as a brief-defect judgment"
   :text "Brief-flaw class 10 of 2026-10-12: a card whose PATHS was widened twice by machine rework and
    needs a third is a brief defect, not a fourth widening. The third request raises a judgment to the
    coordinator naming the paths asked for, and the card waits.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a third PATHS widening on one
    card raises a brief-defect judgment to the coordinator instead of a fourth widening, pinned by a
    test."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 10")
  (item "a-precedent-log" :group "v13-deal" :date "2026-10-11"
   :title "A precedent log"
   :text "An append-only log of the coordinator's decisions, which the system reads before it raises a judgment, so a question already settled is not asked again.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every coordinator decision is
    appended to the log and a judgment already settled there is not raised again, pinned by a
    functional test replaying a settled question."
   :why "New capability."
   :release "v1.3"
   :origin "the owner, 2026-10-11: a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)")
  (item "verifier-stage" :group "v13-deal" :area "nova-sprint"
   :title "The verifier is its own system stage"
   :text "A card's checks are written before the work starts. After the work, a verifier stage of the
    system runs them in a fresh bench, apart from the reader, and its verdict is the merge gate: the
    named test must fail at the card's base and pass at its head, run by the system rather than
    reported by the worker. This changes the card contract (the TEST line becomes a check the system
    runs), the lander (one red-at-base, green-at-head run per card before the batch gate) and the
    reader's job (it reads a change already shown to do what the card says).
    Absorbs (de-duplicated 2026-10-12): System gates before reads (machine-gates-before-reads); Work
    lint, then a system gate before a read, then gate lint (gate-stack); A review audit
    (a-review-audit); Readers check four things before approving a card
    (reader-four-checks-before-approval); A mechanical guard check as a review verb
    (review-guard-check-verb); Make reads cheap with CI evidence and a short mechanical PR brief
    (cheap-reads-ci-evidence-and-pr-brief); Mechanical pin-check read for test-only pull requests
    (reader-pin-check-for-test-only-prs); Re-land and wire script-card self-verification
    (script-verify-wired, nova-tools roadmap). Mechanical cards are proved, not read; the work lint
    proves the test pins the change; the system runs the card's check red at base and green at head
    as the merge gate; readers check comments, fragments, paths and cross-package references before
    approval; the last gate audits the reads themselves; a finish whose brief names a STEP carries
    that STEP's last line; store.MergeStep is reachable only through its verb. Lands only after the
    v1.3 test plan (v1-3-test-plan). Done when: no card lands whose named test is not red at its
    base and green at its head in a fresh bench run by the system, and a planted green-at-base test
    is refused, pinned by a functional test."
   :exists "The card lint requires the named test to be absent at the base: rule donewhen-test-name,
    internal/swarm/lintbase.go:198-236 and internal/swarm/lintpaths.go:174-192. The member's native
    gate runs failing tests at the base only to classify a failure the worker already reported as not
    done (cmd/nova-swarm/nativegate.go:26-42). The lander's tree gate runs the batch at its head and
    reads the TEST line only to time the package (internal/sprint/gate_wall.go:46-59). No step runs the
    named test red at base and green at head as a gate; that is the gap. The system gates before reads
    item, under the sprint system, holds the cards that started on it."
   :why "A new system stage, not a fix."
   :date "2026-10-09"
   :release "v1.3")
  (item "a-plan-gate-before-code" :group "v13-deal" :date "2026-10-11"
   :title "A plan gate before code"
   :text "A card above a size bound writes a plan first, and a reader reviews the plan before implementation starts. The design agreed with the owner on 2026-10-11 has two levels. Inside a card, working splits into planning, then a plan read, then implementing: the plan is committed to the card's branch as PLAN.md, and implementing is a second take on the same branch, so a one-shot child starts from the file, not from memory; a refused plan is a re-plan take. Above cards, a plan node turns a roadmap entry into cards with their dependencies (its scope, the cards, their DEPENDS lines and a read), feeds a stream, holds the cap of about 10 unadmitted drafts, and is what nova-work later imports as a node. Both levels are modelled in TLA+ with the card machine before either is built.
    Absorbs (de-duplicated 2026-10-12): Evaluate plan-then-execute cards and worktree-isolated
    subagents (worker-plan-mode-subagent-eval, nova-tools roadmap). A producer card is planned and
    checked before it waits; the plan node that turns a roadmap entry into cards reads nova-roadmap
    (cards-remember-their-repo-and-roadmap-entry). Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: a card above the plan threshold has a plan and a plan read before
    its first implementation attempt, pinned by a functional test that refuses an unplanned start."
   :why "A new system stage, not a fix."
   :release "v1.3"
   :origin "the owner, 2026-10-11: a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)")
  ; v1.3 F. Cards as records
  (item "cards-test-plan" :group "v13-cards" :area "tests"
   :title "Cards as records: test plan"
   :text "The cards theme's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: unit tests of the record and its derivations; functional
    tests on a real Postgres and Redis (the schema card table, nova-roadmap's ledgers); an integration run
    of a card from a roadmap entry to a landed record that marks the entry done, on the bench; the stress
    drag (3,000 card records loaded and read under the tick gate); and its rows in the parallel release
    checks, under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test for the cards theme, listed in docs/TEST-PLAN-v1.3.md, red before the theme's code and
    green after, with 3,000 card records loaded and read on the bench."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "cards-are-schema-records" :group "v13-cards" :area "nova-sprint"
   :title "Cards are schema records"
   :text "A card is one structured record in a schema table (variable tables for cards; ideas #856): typed
    fields for the header lines, the result, the resume keys, the identity and ruling links, and the spec
    fields at push, with the brief text derived from the record and never the record from the text. The
    sprint adopts the schema card table once the schema-cards stream lands; the schema work itself is
    the product's, not tooling's. This is the owner's own ordering of the v1.3 steps: point releases,
    then variable tables for cards, then the client/server conversion.
    Absorbs (de-duplicated 2026-10-12): The card record is the only record; GitHub is a remote
    written at the edges (card-record-is-the-only-record); Cards carry a stable id, ruling and spec
    links, and a reverse walk from PR to reason (card-identity-ruling-links); Typed card outputs and
    resume by content key and deal-time base (typed-card-results-and-resume-keys); A card is a spec,
    refused at push without its fields (card-spec-fields-at-push). Its tests are cards-test-plan.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a card is one record in the
    schema card table with typed fields, the brief file is derived from it and never the other way,
    and 3,000 cards load and read on the bench under the tick gate."
   :why "The owner, 2026-10-11: variable tables for cards come before the conversion."
   :date "2026-10-12"
   :release "v1.3"
   :origin "ideas #856; the v1.3-variable-tables plan")
  (item "cards-remember-their-repo-and-roadmap-entry" :group "v13-cards" :area "nova-sprint"
   :title "Cards remember their repository and roadmap entry"
   :text "A card carries its repository, its ledger file (docs/roadmap.sexp or docs/fixes.sexp) and the id
    of the entry it serves; a landing marks the entry done with the card as its evidence, through
    nova-roadmap; clear never loses a card headed to the roadmap; defer-cards-to-the-roadmap (fixes
    v1.2.6) is the move in the other direction. This is the pragmatic workflow for nearly all nova work,
    and the owner named it the first v1.3 entry on 2026-10-10.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a card carries its repository,
    ledger file and entry id, a landing marks the entry done with the card as evidence, and clear
    loses no card headed to the roadmap, pinned by functional tests."
   :why "The owner, 2026-10-10: the first v1.3 entry."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-10")
  (item "roadmap-tool-becomes-nova-roadmap" :group "v13-cards" :area "nova-roadmap"
   :title "Extract the roadmap tool into nova-roadmap"
   :text "The roadmap and fixes code exists twice: here as internal/roadmap, internal/roadmapdoc,
    tools/roadmap and the nova-work roadmap verbs, and in nova-tools as internal/roadmap,
    internal/roadmap/sexp and tools/roadmap, with two diverging copies of the generator. Unify the
    s-expression format first; then one module and one CLI, nova-roadmap, living in this repository (a
    module and cmd/nova-roadmap): it reads and writes the data, renders ROADMAP.md and FIXES.md, and runs
    the sync check. Both repositories use it and delete their own copies. Net fewer lines. Moved from the
    v1.2.13 fixes ledger on 2026-10-12: a new module, not a fix. The roadmap s-expression is the roadmap
    record for every repository (ideas #835), read by the plan node of a-plan-gate-before-code and by
    cards-remember-their-repo-and-roadmap-entry.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: cmd/nova-roadmap in this
    repository renders both ledgers of both repositories byte for byte, nova-tools imports it, and
    the two internal copies are deleted."
   :why "A new module shared by two repositories, not a point fix."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-11: v1.2.13 contraction, pass 4; moved to v1.3 on 2026-10-12 (fix roadmap-tool-becomes-nova-roadmap)"
   :kept (:fix-status "planned" :fix-release "v1.2.13"))
  (item "nova-work-becomes-nova-sprint-issues" :group "v13-cards" :area "nova-sprint"
   :title "nova-work becomes the nova-sprint issues verb"
   :text "The one-tool direction (the owner, 2026-10-12): nova-work's verbs become one nova-sprint verb,
    issues, and the fleet stops installing nova-card and nova-work. The verbs it absorbs: the roadmap
    verbs (check, list, add, remove, pull, done, note, render; the v1.2.6 fix nova-work-roadmap-verbs is
    folded here and removed from the fixes ledger), the issue and bug fields with open-bug blocking, lease
    expiry, the recursive coordination nodes, ingest from and write-back to the forge, the specs forest
    with its single writer, supply from enumerated sources, global uids and the issue index, link mode
    with its dogfood and absorb decision, the live tree picture, the derive and fold expansion, the nodes
    projection rebuilt from the journal, bounded client waits, and the help. The fourteen roadmap items
    this collapses are named below and removed.
    Absorbs (de-duplicated 2026-10-12): nova-work client must stop waiting after a bound
    (work-client-stop-waiting-bound); Clarify nova-work help semantics (nova-work-help-clarity);
    Nodes table is a rebuildable projection of the journal (work-nodes-projection-rebuild-test);
    Enforce work bug fields and let an open bug block its ancestor
    (work-bug-fields-and-open-bug-blocking); Derive lease expiry, extend-once and escalation in
    nova-work (work-lease-expiry-derivation); nova-work v2 recursive coordination nodes
    (work-v2-recursive-coordination-nodes); nova-work ingest from GitHub: capture, continuous poll,
    verify and cutover (work-ingest-from-github); nova-work write-back: one writer, roadmap batch,
    lag and fold of landed work (work-writeback-and-fold); nova-work keeps specs in its store behind
    a single-writer kernel (work-specs-in-forest-single-writer); Cut cards by loop from enumerated
    sources, never duplicating open pull requests (work-supply-from-enumerated-sources); Globally
    unique node ids and a two-way issue index (work-global-uids-and-issue-index); Link-mode dogfood
    ledgers and the absorb-mode decision (work-link-mode-dogfood-and-absorb-decision); nova-work
    tree as a live picture beside its Lisp (work-tree-live-picture); Implement derive and fold
    expansion in nova-work (work-derive-fold-expansion). Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: nova-sprint issues carries the verbs named, nova-work and nova-card
    are no longer installed by the fleet, and every former nova-work transcript test passes against
    the new verb."
   :why "The owner, 2026-10-12: one tool."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12; the freeze triage store cards nova-work-becomes-nova-sprint-issues and the-fleet-stops-installing-nova-card-and-nova-work")
  (item "dependency-form-single-parser" :group "v13-cards"
   :title "One DEPENDS-ON form and one parser that refuses other spellings"
   :text "Cards, pull requests and issues state dependencies in one canonical form read by one parser.
    Unknown dependency state is never treated as satisfied.
    Absorbs (de-duplicated 2026-10-12): Refuse a need that names a dropped card, and list dependants
    on drop (add-refuses-need-on-dropped-card); Edit a card's needs in place
    (needs-verb-edit-in-place); Dependencies come from paths, with sentinels per layer
    (card-dependencies-and-sentinels). One DEPENDS-ON form and one parser; a replaced card is an
    alias every need follows; an answers alias resolves in the current epoch; a need on a card
    landed in an earlier epoch is satisfied, never missing; add refuses a need on a dropped card and
    drop lists its dependants; needs set shipped in v1.2.14. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: every need in the store parses with the one parser, a need on a
    replaced or earlier-epoch card resolves, and a need on a dropped card is refused at add, pinned
    by functional tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #3409")
  (item "redo-verb-restores-dropped-card" :group "v13-cards"
   :title "Redo restores a dropped card"
   :text "The redo verb restores a card that was dropped, covered by a test.
    drop --cascade prints and refuses without --expect; redo is the undrop. Lands only after the
    v1.3 test plan (v1-3-test-plan). Done when: a dropped card is restored by redo with its needs
    and a cascade drop refuses without the expected count, pinned by functional tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  ; v1.3 G. Tooling hygiene
  (item "tooling-test-plan" :group "v13-tooling" :area "tests"
   :title "Tooling hygiene: test plan"
   :text "The tooling theme's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: a red and a green fixture per lint rule and per
    refusal; transcript tests per verb; functional tests of add, brief and finish on a real store; the
    stress drag (a lint of 3,000 stored briefs, timed); and its rows in the parallel release checks,
    under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test for the tooling theme, listed in docs/TEST-PLAN-v1.3.md, red before the theme's code and
    green after, with a lint of 3,000 briefs timed."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "card-lint-defects" :group "v13-tooling"
   :title "The card lint refuses known defects and reads place names right"
   :text "The card lint refuses the listed brief defects, and a repository or branch name is not mistaken
    for a personal name.
    Absorbs (de-duplicated 2026-10-12): nova-swarm lint card validation closes its escapes
    (swarm-lint-card-validation); Brief defects and report form are judged by the system
    (brief-and-form-checks-by-machine); Card cutting keeps sweeps small and paths narrow
    (card-cutting-narrow-paths); State the brief admission contract in lint and add
    (admission-contract-lint-statement); nova-card new writes a lint-clean brief from its parts
    (nova-card-new). The umbrella: lint refuses the listed brief defects and place names read right;
    drive paths, more than eight globs, unknown kinds and a missing test are refused; brief defects
    are their own column and the report form is linted at finish; sweeps are cut by site and a
    one-file card gets one path; a card rule for agent-written tests (ideas #796); the brief
    boilerplate moves into the card contract and briefs go by reference (the v1.2.13 fixes). The ten
    brief-flaw classes of 2026-10-12 follow as their own entries. Its tests are tooling-test-plan.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every listed defect has a red
    fixture the lint refuses and a green one it admits, and a lint of 3,000 stored briefs finds
    none, timed on the bench."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "brief-lint-new-path-under-a-gate-glob" :group "v13-tooling" :area "card lint"
   :title "Lint refuses a NEW path that matches a glob a base gate enumerates"
   :text "Brief-flaw class 1 of 2026-10-12: a brief named a NEW path (make/*.mk among them) that a base
    gate already enumerates, so the gate ran on the new file before the card meant it to. The lint
    refuses the path naming the gate and its glob.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a brief whose NEW path matches
    a glob a base gate enumerates is refused naming the glob, pinned by a fixture."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 1")
  (item "brief-lint-stream-card-base-is-a-trunk" :group "v13-tooling" :area "card lint"
   :title "Lint refuses a stream card whose BASE is a trunk"
   :text "Brief-flaw class 2 of 2026-10-12: a stream card named dev or main as its BASE. The lint refuses
    it at add and at brief; the v1.2.x fix no-stream-lands-a-card-on-a-protected-branch is the stopgap
    refusal at land, and protected-bases-promotion-stream is the flow it serves.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a stream card whose BASE is dev
    or main is refused at add and at brief, pinned by a fixture, with the v1.2.x refusal as its
    stopgap."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 2")
  (item "brief-lint-test-names-a-red-at-base-form" :group "v13-tooling" :area "card lint"
   :title "A TEST line may name an existing red test only in the red-at-base form"
   :text "Brief-flaw class 3 of 2026-10-12: the TEST line could not name a test that already exists and is
    red at the base, so fix cards had to invent a new test name. A red-at-base form says the named test
    fails at the base and passes at the head; any other naming of an existing test is refused.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a TEST line naming an existing
    red test is admitted only in the red-at-base form and otherwise refused, pinned by fixtures of
    both."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 3")
  (item "brief-lint-read-paths-marker" :group "v13-tooling" :area "card lint"
   :title "The (read) PATHS marker is parsed as read-only"
   :text "Brief-flaw class 4 of 2026-10-12: a PATHS entry marked (read) was parsed as a write path and
    widened the card's write set and its shared-path collisions. The marker is parsed, the path is
    read-only, and the lint says so in its listing.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a (read) PATHS marker is parsed
    as read-only and never widens the write set, pinned by a fixture."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 4")
  (item "brief-lint-shared-path-check-reads-repo" :group "v13-tooling" :area "card lint"
   :title "The shared-path check compares paths within one REPO"
   :text "Brief-flaw class 5 of 2026-10-12: two cards naming the same path in different repositories were
    held as a collision. The check reads the REPO line and compares paths within one repository only.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the shared-path check compares
    paths within one REPO and two cards on the same path in different repositories do not collide,
    pinned by a fixture."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 5")
  (item "brief-lint-tla-reference-is-not-an-edit" :group "v13-tooling" :area "card lint"
   :title "A tla/ path named only as a reference is not an edit"
   :text "Brief-flaw class 6 of 2026-10-12: a brief that cited a model under tla/ as reading material was
    treated as editing it and asked for a TLC record. A path under START or marked (read) is a reference;
    only a PATHS entry is an edit.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a tla/ path named only as a
    reference is not counted as edited and does not require a TLC record, pinned by a fixture."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 6")
  (item "add-dry-run-is-the-brief-check" :group "v13-tooling" :area "nova-sprint"
   :title "add --dry-run runs every add check without a write"
   :text "Brief-flaw class 9 of 2026-10-12: there was no way to run add's checks on a brief without
    adding it. add --dry-run runs them all and writes nothing, and an add refusal points to it; the
    v1.2.14 fix brief-check-runs-the-add-checks-without-a-write is its first slice, with add --partial
    and refusals that name their fix as data beside it.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: add --dry-run runs every add
    check without a write and an add refusal points to it, pinned by a transcript test."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the brief-flaws-v1.3 list, the owner 2026-10-12, class 9; TOOLING-FROM-THE-SEAT 1 to 4")
  (item "structured-brief-intact-when-tiering" :group "v13-tooling"
   :title "Keep structured briefs intact when tiering TLA work"
   :text "The tier line goes before structured header fields so REPO and CARRY briefs stay parseable.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a tier change writes the tier
    line before the structured header fields and the brief still parses, pinned by a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5452")
  (item "friend-card-lint-names-rule-set" :group "v13-tooling"
   :title "Friend card lint says which rule set applies"
   :text "The add lint accepts a friend card with its own rules line and says exactly whether the friend
    or the child rule set applies.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every lint refusal names the
    rule set that applies, pinned by a test per rule set."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "finish-head-default-and-land-sha-check" :group "v13-tooling"
   :title "finish --head defaults to what land accepts; a land with no full sha fails"
   :text "The finish help and default match the head that land accepts. A land without a full sha is a
    failed finish, and stale reports stay fenced.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: finish without --head defaults
    to the head land accepts and a mismatched sha is refused, pinned by tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5235; PR #5246; issue #5154")
  (item "swarm-finish-derives-shas-and-format" :group "v13-tooling"
   :title "The swarm finish derives head and step shas from git and refuses an unformatted file"
   :text "The result finish is mechanical: shas come from git, not from the model, and an unformatted Go
    file is refused before landing.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: finish derives its shas from
    git and refuses unformatted Go, pinned by tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5276")
  (item "add-time-already-done-check" :group "v13-tooling"
   :title "Check at add whether a card's work is already on its base"
   :text "At add, by git and without a model, a card is refused when its work already sits on the base
    branch.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: add refuses a card whose work
    is already on the base, naming the commit, pinned by a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "reland-from-commits-verb" :group "v13-tooling" :area "nova-sprint"
   :title "A verb to reland a card from its commits"
   :text "A verb that lands a card again from named commits.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a card is landed again from
    named commits by one verb with the landing record written, pinned by a functional test."
   :why "A new verb."
   :cards 1
   :date "2026-10-09"
   :release "v1.3")
  ; v1.3 H. Release, adopt, CI and ledger hygiene
  (item "release-test-plan" :group "v13-release" :area "tests"
   :title "Release, adopt and CI, the sprint side: test plan"
   :text "The release theme's tests on the sprint side, written from docs/TEST-PLAN-v1.3.md before the
    theme's first implementation entry, which cites this one: unit tests of the install and rollback
    decisions; functional tests of a server switch on a real store; an integration run of an install, a
    missed-tick rollback and a release check on the bench; the roadmap doctor's seeded broken roadmap and
    a refused import, one fixture per finding (roadmap-doctor); and the release checks themselves,
    parallel and under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test for the release theme, listed in docs/TEST-PLAN-v1.3.md, red before the theme's code and
    green after, with a server install and rollback timed on the bench."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "roadmap-doctor" :group "v13-release" :area "nova-roadmap"
   :title "The roadmap is nova-sprint's: doctor, import and defer, on correct data"
   :text "The owner, 2026-10-12: the roadmap is just future work that feeds into sprints, and also a place to
    push (defer) work in a sprint out to a future point. Importing things from roadmaps into sprints has
    been a constant pain point, because the imported entries carry dependencies that no longer exist; and
    this is why the roadmap is a part of nova-sprint, it is all holistically the same thing. So the
    roadmap tool and its doctor are nova-sprint verbs, and the sprint reads the sexp directly when it
    cuts cards; nova-tools runs these verbs on its own ledgers until the fold. The data: the sexp is the
    single source (the pages are generated, never edited) and is written only through the verbs (defer,
    import, generate, and an edit verb if one is needed), never by hand in the normal flow; the doctor
    runs in CI on every change to the sexp and at every import and defer, so a write that leaves the
    roadmap inconsistent is refused, not committed. The verbs: roadmap doctor checks the sexp itself
    (every dependency, a :depends key or a DEPENDS-ON line in a card draft, names an entry that exists
    and is not later in release order than the dependent, with no cycle and nothing dangling; every
    :release is one of the four values; every v1.3 entry has its Done-when sentence; every entry with a
    card draft, a brief in the repository or under tmp/next-sprint-cards, passes card lint, nova-swarm
    lint --card --child-rules with zero rule or step findings, and its PATHS exist at the named base; no
    duplicate ids or titles; ROADMAP.md and FIXES.md byte-identical to what the sexp renders; every
    shipped entry's landing commit reachable from the named tag; a landed card commit since the last tag
    has an entry), one DOCTOR line per finding with its remedy, exit 1 on any, run in CI as a class test
    and by make roadmap in both repositories. roadmap generate writes the pages. roadmap import cuts
    cards from entries and is the boundary: it, nova-card generate and the sprint's add from a roadmap
    entry refuse an entry whose dependencies are dangling, landed but not on the base, or cyclic, and
    print the same DOCTOR lines. defer pushes a card out of the sprint into a roadmap entry at a named
    release, carrying its brief, needs, last pushed head and reason, so nothing is lost and it can be
    imported again; it replaces the hand-parking of dropped cards under tmp/next-sprint-cards. defer
    --stream <s> --release <r> defers a whole work stream atomically, in one verb: (1) it stops every
    in-flight attempt of the stream (working lanes told to stop within one beat, as a moved card's old run
    is; reads returned; nothing redealt), recording each card's last pushed head as its carry; (2) it
    moves every unlanded card into roadmap entries with brief, needs re-pointed to the deferred ids, carry
    head, the stream's branch name and release; (3) it removes the stream from the current sprint
    (archives its control card; the branch stays in git). All or nothing: the store side is one Lua step
    after the stops are confirmed, the sexp write is refused whole if the doctor fails, and a failure at
    any step rolls the sprint back to before, the cards back on the table in their prior states. A later
    import --stream <s> restores the whole set with its internal dependency order intact.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a seeded broken roadmap (a dangling
    dependency, a cycle, a bad release, a missing Done-when, a lint-failing draft, a stale generated page,
    a missing landing entry) produces one finding each and the real roadmaps produce none; importing a
    roadmap entry with a dependency that no longer exists is refused with the entry and the missing id
    named, never admitted; a hand edit that breaks a rule is caught by CI with one DOCTOR line; a
    dropped card round-trips defer then import with brief, needs and carry head intact; and a stream of
    N cards with chained needs and two carry heads round-trips defer then import with nothing lost and
    no partial state observable in between, a failure mid-way leaving the sprint and the roadmap exactly
    as before; and a stream with one working card and one in review defers in one verb, the worker's lane
    stopped within a beat, the carry head recorded, the stream gone from where, and import restoring it."
   :why "The owner, 2026-10-12: roadmap imports into the sprint arrive with dependencies that no longer exist; the data must be correct; whole streams must defer atomically."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "install-canary-and-rollback" :group "v13-release" :area "nova-sprint"
   :title "Server install canary and rollback"
   :text "A server install that rolls itself back when the new server misses ticks, a cold twin warmed
    before the switch, a rollback drill run on purpose, and a check for version skew.
    The sprint half of adoption-pipeline-automated (nova-tools roadmap); a server rollback is always
    printed. Its tests are release-test-plan. Lands only after the v1.3 test plan (v1-3-test-plan).
    Done when: a server install that misses ticks rolls back within its window and prints the
    rollback, the cold twin and the version skew check pass, and the rollback drill runs on the
    bench, pinned by functional tests."
   :why "New capability."
   :cards 4
   :date "2026-10-09"
   :release "v1.3")
  (item "release-check-gates" :group "v13-release" :area "nova-sprint"
   :title "More release check gates"
   :text "release check also requires the chaos suites green, fsck clean for 24 hours, and the tools'
    own checks.
    release-acceptance-is-a-parallel-functional-suite (fixes v1.2.14) is its suite. Lands only after
    the v1.3 test plan (v1-3-test-plan). Done when: release check refuses a cut without chaos green,
    fsck clean for 24 hours and the tools' checks, and passes one that has them, pinned by tests."
   :why "New capability."
   :cards 3
   :date "2026-10-09"
   :release "v1.3")
  ; v1.3 I. Routes, rate limits and spend
  (item "routes-test-plan" :group "v13-routes" :area "tests"
   :title "Routes and spend, the sprint side: test plan"
   :text "The tests of the sprint's spend and balance rules, written from docs/TEST-PLAN-v1.3.md before
    the theme's first implementation entry, which cites this one: unit tests with planted spend and
    balances; functional tests on a real store; an integration run with the per-model budget of
    nova-tools on the bench; and its rows in the parallel release checks, under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test for the sprint's routes and spend theme, listed in docs/TEST-PLAN-v1.3.md, red before the
    theme's code and green after."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "cold-start-balance-unknown-never-out" :group "v13-routes"
   :title "A cold start never judges a provider out of funds from old refusals"
   :text "On its first tick the server must not halt the sprint because of stored refusals. An unknown
    balance is never treated as out of credit.
    Its tests are routes-test-plan. Lands only after the v1.3 test plan (v1-3-test-plan). Done when:
    an unknown balance at cold start is never read as out of credit and a route rests only on a
    provider refusal, pinned by tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5228; issue #5220")
  (item "spend-circuit-breaker" :group "v13-routes" :area "nova-sprint"
   :title "A spend circuit breaker with a budget per tier"
   :text "Dealing to a tier stops when its spend passes the tier's budget, and says so.
    Absorbs (de-duplicated 2026-10-12): Route predictor in shadow, spend circuit breaker and
    per-tier governor (route-predictor-and-spend-governor); A spend burn-rate sentinel
    (spend-burn-rate-sentinel); Cost rules: flash first, escalate repeats, rest failing routes
    (sprint-cost-rules); Cost headlines are complete and carry their coverage
    (complete-cost-headlines); Reader spend summed on the readers table (reader-spend-on-the-table).
    The per-model rate budget is a-model-has-a-requests-per-second-budget (nova-tools roadmap);
    spend reconciliation is spend-reconciliation-and-route-price-refresh there. Lands only after the
    v1.3 test plan (v1-3-test-plan). Done when: dealing to a tier stops at its budget and raises one
    judgment, and a burn rate past the bound is alarmed within a tick, pinned by functional tests
    with planted spend."
   :why "New capability. The per-model request budget the owner named is v1.3 and separate."
   :cards 1
   :date "2026-10-09"
   :release "v1.3")
  (item "friends-token-cost-category" :group "v13-routes" :area "nova-sprint"
   :title "A cost category for friend work, in tokens"
   :text "Friend work counted in tokens as its own cost category beside the tiers, so subscription work
    shows its real size.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: friend work is counted in
    nova-tokens as its own category and the headline equals the folded day files, pinned by a test."
   :why "New capability."
   :cards 1
   :date "2026-10-09"
   :release "v1.3")
  ; v1.3 J. Fleet, bench and wall operations
  (item "fleet-test-plan" :group "v13-fleet" :area "tests"
   :title "Bench operations, the sprint side: test plan"
   :text "The tests of the sprint's bench choice and the lander's bench half, written from
    docs/TEST-PLAN-v1.3.md before the theme's implementation entry, which cites this one: unit tests
    with planted load samples; an integration run of a landing gated on a chosen bench; and its rows in
    the parallel release checks, under two minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test for the sprint's bench use, listed in docs/TEST-PLAN-v1.3.md, red before the theme's code
    and green after."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "bench-chosen-by-load" :group "v13-fleet"
   :title "A lane picks the least loaded bench"
   :text "The bench a lane gates on is chosen at lane start from the fleet rows by cores and current load,
    and the choice and reason are written into the brief.
    The bench half of the lander: one lander per clone, the gate runs on a bench where the card is
    dealt (a-card-gate-runs-where-the-card-is-dealt, fixes v1.2.14), through bench-run-verb
    (nova-tools roadmap). Its tests are fleet-test-plan. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: a lane picks the least loaded bench from live samples and a planted
    overloaded bench is never chosen, pinned by a functional test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  ; v1.3 K. TLA+ models and test tiers
  (item "models-test-plan" :group "v13-models" :area "tests"
   :title "TLA+ models and test tiers: test plan"
   :text "The models theme's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: the TLC catch-up pass with a current record for every
    sprint model; the twin property tests; the class tests that keep every test off the tree and on the
    shared store scaffold; the chaos suite with each fault's bound; and the suite's own rows in the
    release checks, timed.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test or TLC case for the models theme, listed in docs/TEST-PLAN-v1.3.md, with every model's
    record current and the suite timed."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "sprint-tla-models-catch-up" :group "v13-models"
   :title "Bring the sprint TLA+ models up to the state machine"
   :text "The models still ask reads as a pair, pin round-robin, miss the taken-back reader and omit the
    friend ready queue; update them to match the code.
    With the v1.2.16 every-model-checked rows and tla-ledger-union (nova-tools roadmap);
    tlc-suite-runs-independent-cases-in-parallel (fixes v1.2.15) is its runner. Its tests are
    models-test-plan. Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every sprint
    model is level with the state machine it models, with a current TLC record and no stale group in
    tlacheck groups --stale."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "tla-stop-and-backup-models" :group "v13-models"
   :title "TLA+ models for the system stop and the backup path"
   :text "Models for how a stop returns reads and claimed cards, and for the promise that a backup path
    holds only a verified file. Each is checked on a small instance and cited from the code.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the stop and backup path models
    have reversed witnesses and current TLC records, and the code cites them."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "tla-read-and-friend-card-models" :group "v13-models"
   :title "TLA+ models for reads on a missing branch and the friend card"
   :text "A model for a read on a missing branch that spends no reader, and for a friend card's lifecycle
    and the friend's liveness. Each names the evidence the code reads.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the read-on-a-missing-branch
    and friend card models have reversed witnesses and current TLC records."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "sprint-state-machine-tla-models" :group "v13-models"
   :title "TLA+ models for gc, landing prune and the lane check"
   :text "Small TLA+ models for the gc rule, the landing cleanup queue and the tick's friend-lane check,
    each with invariants checked on a small instance and cited from the code.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: gc, landing prune and lane
    check have models with reversed witnesses and current TLC records."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "sprint-models-cards-and-promotion" :group "v13-models"
   :title "TLA+ models for briefs, needs, reads, seat push and the merge tree"
   :text "Model brief lint and twins, needs and sentinels, read cards, seat push, and the merge tree with
    promotion in TLA+, and check them with TLC.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: lint and twins, needs, read
    cards, seat push and the merge tree have models with reversed witnesses and current TLC records."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "dirty-tick-twin-catchup-robust" :group "v13-models"
   :title "The dirty-tick twin catches up from set, reorder and delete events without a whole read"
   :text "The change-stream catch-up recognizes row order and row delete events, and the test waits on the
    twin state, not the clock. This removes a flaky merge-queue failure.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the twin catches up from set,
    reorder and delete events and equals the store after each, pinned by a property test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5230; PR #5233; issue #5197; issue #5214")
  (item "tests-leave-source-tree-clean" :group "v13-models"
   :title "Tests leave the source tree clean"
   :text "A sprint test that wrote into the source tree uses a temp directory, and a CI class test checks
    the tree is clean after the package tests.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every test uses a temp dir and
    a class test fails when the tree is dirty after the suite, proven by a planted fixture."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-store-test-environment" :group "v13-models"
   :title "Shared test environment for the sprint store packages"
   :text "The sprint store and its test utility packages get the same test environment scaffold as the
    other packages. Their tests stay under the time bound.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every store package test uses
    the shared scaffold and a class test finds no private store setup."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "chaos-suites" :group "v13-models" :area "nova-sprint"
   :title "Friend and sprint chaos suites"
   :text "Faults injected on purpose into friends and into the sprint system, each required to show
    and to recover within its bound.
    Late in v1.3; stall-and-stuck-classes-of-2026-10-11-clipped-in (fixes v1.2.15) and the mock-seat
    e2e suite are build gates beside it. Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each injected fault (a killed lane, a lost store reply, a down friend, a slow tick)
    recovers within its bound on the bench and the suite is red when a bound is missed, recorded."
   :why "New test capability beyond the v1.4 quality list."
   :cards 2
   :date "2026-10-09"
   :release "v1.3")
  ; v1.3 L. Cleanup of old code
  (item "cleanup-test-plan" :group "v13-cleanup" :area "tests"
   :title "Cleanup of old code: test plan"
   :text "The cleanup theme's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: a class test per deletion that keeps the deleted verb or
    package deleted, proven by a planted fixture; the dead-code check under every GOOS and tag; the
    unchanged transcript tests as proof that a removal changed no kept behaviour; and its rows in the
    parallel release checks.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each deletion in the cleanup
    theme has a class test that keeps it deleted, listed in docs/TEST-PLAN-v1.3.md and proven by a
    planted fixture."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "remove-superseded-sprint-verbs" :group "v13-cleanup"
   :title "Remove the superseded sprint verbs and system in one batch"
   :text "Once the unused-verb check confirms it, the old friend-queue, routing, lander and sprint-file
    verbs are deleted together.
    Absorbs (de-duplicated 2026-10-12): Retire the stopgaps once their verbs exist
    (retire-stopgaps). Bud runners, buswatch, ping and beat loops, opencode runners, shell lanes and
    hand promotion go when their verbs exist; the nova-tools half is retire-stopgap-scripts. Its
    tests are cleanup-test-plan. Lands only after the v1.3 test plan (v1-3-test-plan). Done when:
    the old friend-queue, routing, lander and sprint-file verbs are deleted in one batch, every
    stopgap script is gone, and a class test refuses their return."
   :date "2026-10-10"
   :release "v1.3"
   :origin "issue #4191")
  (item "dead-package-removal" :group "v13-cleanup"
   :title "Dead packages are removed"
   :text "Packages with no users are deleted together with their tests, one step each, checked by the
    build.
    The sprint half of dead-code-ledger-shrink (nova-tools roadmap). Lands only after the v1.3 test
    plan (v1-3-test-plan). Done when: every dead package is removed and the dead-code check is clean
    under every GOOS and tag."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  ; v1.3 M. Docs and the rename
  (item "docs-test-plan" :group "v13-docs" :area "tests"
   :title "Docs: test plan"
   :text "The docs theme's checks, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: the terminology lint over every doc; the transcript
    tests that run every documented example; and a cold run of the landing flow by a newcomer AI from
    the pages alone, on the bench.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the docs theme's checks are
    listed in docs/TEST-PLAN-v1.3.md: the terminology lint, the transcript tests and a cold
    onboarding run from the pages alone."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "nova-sprint-docs-and-brand-suite" :group "v13-docs" :area "docs"
   :title "nova-sprint docs suite and brand"
   :text "A full documentation suite for nova-sprint (start, guides, reference, the coordinator's
    runbook, the processor) with a prose pass, a brand sheet, and a flagship README.
    Its tests are docs-test-plan. Lands only after the v1.3 test plan (v1-3-test-plan). Done when:
    the docs suite, prose pass, brand sheet and flagship README are in place and a newcomer AI runs
    the landing flow cold from them on the bench."
   :why "New documentation, not a fix."
   :cards 8
   :date "2026-10-09"
   :release "v1.3")
  (item "the-tool-teaches-the-landing-flow-to-a-newcomer" :group "v13-docs" :area "docs"
   :title "The tool teaches the landing flow to a newcomer"
   :text "Once the landing flow is final: the help of add, stream and land, and docs/SPEC-SPRINT, say the
    flow in one place (card heads land in a batch onto a branch per work stream, oldest first, one writer
    per stream; stream branches merge serially into dev; a stream based on dev or main is a defect) so a
    newcomer AI lands a card without a hand step. Last in the landing theme.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a newcomer AI given only the
    tool's help and docs lands a card through a stream branch into dev on the bench without a hand
    step, recorded."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the freeze triage store card the-tool-teaches-the-landing-flow-to-a-newcomer, 2026-10-12")
  ; v1.3 N. Observability
  (item "observe-test-plan" :group "v13-observe" :area "tests"
   :title "Observability: test plan"
   :text "The observability theme's tests, written from docs/TEST-PLAN-v1.3.md before the theme's first
    implementation entry, which cites this one: a planted inconsistency per fsck kind and a clean store;
    unit tests of each alarm and measure with a fake clock; an integration run of fsck and the alarms
    over the bench fleet, timed over 3,000 cards; and its rows in the parallel release checks, under two
    minutes.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: each tier above has a named
    test for the observability theme, listed in docs/TEST-PLAN-v1.3.md, red before the theme's code
    and green after, with fsck over 3,000 cards timed."
   :date "2026-10-12"
   :release "v1.3"
   :origin "the owner, 2026-10-12")
  (item "fsck-integrity-verbs" :group "v13-observe" :area "nova-sprint"
   :title "Integrity checks (fsck)"
   :text "Checks the seat or a person can run at any time: each friend's queue agrees with the store,
    nothing is held without a beat, every pushed head is recorded, the server's runs and pushes agree,
    and every landed card is on the base.
    Absorbs (de-duplicated 2026-10-12): Reconcile landed records with the branch in both directions
    (landed-record-reconciliation); A read is one record written only by the verb
    (reads-are-one-record-written-by-verb). The queue agrees with the store, nothing is held without
    a beat, every pushed head is recorded, every landed card is on the base, verify-landed runs in
    both directions, audit-unlanded lists uncovered PRs and unlanded ok heads, and a read is one
    record written by its verb; the design lock's three-ways cards follow once the owner corrects
    the audit. Its tests are observe-test-plan. Lands only after the v1.3 test plan
    (v1-3-test-plan). Done when: fsck finds every planted inconsistency of the kinds listed and
    reports clean on a healthy store, timed over 3,000 cards on the bench."
   :why "New verbs."
   :cards 5
   :date "2026-10-09"
   :release "v1.3")
  (item "new-alarms" :group "v13-observe" :area "nova-sprint"
   :title "Two new alarms"
   :text "An alarm when the store's latency passes its bound, and one when a test process is left
    running on a fleet machine.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: a store latency past its bound
    and a test process left running each raise an alarm within a tick, pinned by tests."
   :why "New capability."
   :cards 2
   :date "2026-10-09"
   :release "v1.3")
  (item "deal-latency-measure" :group "v13-observe"
   :title "Deal latency is measured per member and friend"
   :text "The time from ready to dealt to started is measured per card, shown as median and p90, and
    alarmed once per episode when the p90 passes its target.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: ready to dealt to started is
    measured as median and p90 and alarmed past the bound, shown on the bench with the numbers
    recorded."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10); card moved out of the sprint (work record, 2026-10-04)")
  (item "lander-live-progress-lines" :group "v13-observe"
   :title "The lander prints its phase as it runs"
   :text "A land pass prints a progress line for each phase, so the coordinator can tell a slow pass from
    a stuck one.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every land phase prints its
    progress line, pinned by a transcript test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "where-costs-by-tier-and-late-tick" :group "v13-observe"
   :title "where --costs carries tiers and cost by tier; a late tick reads running"
   :text "The dashboard reads tiers and per-card cost by tier from where. A running system with a late
    tick reads running, not stopped.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: where --costs groups by tier
    and a late tick reads running, pinned by tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5323; PR #5326")
  (item "stats-reset-from-a-mark" :group "v13-observe"
   :title "One verb zeroes every figure on the sprint page"
   :text "A stats reset verb clears total cost, per-card and per-tier cost, stream cost cells and each
    row's done and ok percent from a recorded mark.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: one verb zeroes every figure on
    the page from a mark and the page reads zero after it, pinned by a test."
   :date "2026-10-10"
   :release "v1.3"
   :origin "card from the sprint store (2026-10-10)")
  (item "dashboard-projections-and-freshness" :group "v13-observe" :area "dashboard"
   :title "Dashboard projections and freshness"
   :text "Union slice I: projections on the dashboard with a freshness mark on each, and a column for
    work verified working.
    Absorbs (de-duplicated 2026-10-12): Merge row on the dashboard and live land progress
    (landing-and-merge-visibility); Dashboard merging, review and working cells pulse in a
    left-to-right wave (dashboard-state-pulse-wave); The served dashboard page is pinned to the live
    page (dashboard-live-pin); One monitoring dashboard for the sprint, load, network and store
    (fleet-monitoring-dashboard); Serve the public sprint dashboard as files from one puller
    (public-dashboard-served-from-files). Only on the owner's explicit ask: the one dashboard
    changes in neither direction unasked. Lands only after the v1.3 test plan (v1-3-test-plan). Done when: every projection carries a freshness mark and a cell that disagrees with the store fails a
    test, on the one dashboard the owner asked for."
   :why "Union slice I, parked by the owner, 2026-10-09."
   :cards 1
   :date "2026-10-09"
   :release "v1.3")
  (item "dashboard-status-dot-and-first-read" :group "v13-observe"
   :title "Dashboard live dot shows stopped, and a failed first read refuses"
   :text "The dashboard header dot turns red when the system is stopped, green when running, and neutral
    when disconnected. A dashboard whose first read fails exits 2 with one stderr line instead of
    serving.
    Lands only after the v1.3 test plan (v1-3-test-plan). Done when: the live dot reads red, green
    or neutral from the store and a failed first read exits 2, pinned by tests."
   :date "2026-10-10"
   :release "v1.3"
   :origin "PR #5526; PR #5528")
  ; Later, v1.4 stabilization and point-release items, in the order the file had them
  (item "processor-layers" :group "sprint" :area "nova-sprint"
   :title "Processor layers 3 to 8"
   :text "The upper layers of the nova-sprint processor design: cards that name operands outside the
    sprint, a route predictor run in shadow first, speculative dispatch, vector cards that make one
    change across many targets, and a spend governor."
   :why "New capability; no new features through v1.4."
   :cards 6
   :date "2026-10-09"
   :release "later")
  (item "card-template-wording-accuracy" :group "sprint"
   :title "Generated card wording matches the card contract"
   :text "The GOCACHE sentence and the Deadline line in generated cards are stale or misleading. Every
    generator and the template say that GOCACHE is the shared warm cache, and that a card past its
    deadline is judged by the coordinator.
    Point release: v1.2.14."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "card-help-banner-no-repeated-example" :group "docs"
   :title "nova-card help prints each example line once"
   :text "The usage block of nova-card generate lists the same example line in the flow section and again
    as the example. The help renderer prints it once.
    Point release: v1.2.14."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "card-view-by-tier" :group "sprint"
   :title "View cards in review by tier"
   :text "Finding what is in review by tier takes a hand-written scan of store keys and parsing of each
    brief. A nova-card view lists cards by state and tier directly."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "lander-empty-commit-handling" :group "sprint"
   :title "Lander handles an empty commit"
   :text "The lander does not fail or mislead when a card produces an empty commit; this is pinned by unit
    and functional tests.
    Point release: v1.2.14."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "seat-check-merge-queue-versions" :group "sprint"
   :title "Seat check for the merge queue and versions"
   :text "A check verb reports the merge queue and tool versions on the seat, carried by an open pull
    request."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "store-backup-and-demo-load-verbs" :group "ops"
   :title "Backup, restore and demo-load verbs for the sprint store"
   :text "Replace the hand backup procedure with verbs that dump, split, checksum, scan for secrets and
    restore into a throwaway store, so a dump also works as a demo load."
   :date "2026-10-10"
   :release "later"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "dashboard-dark-only" :group "docs"
   :title "Dashboard is dark only, theme toggle removed"
   :text "The sprint dashboard drops the light theme and its toggle and always renders dark.
    The owner's call; the dashboard changes only on his ask."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "batch-selector-grammar-for-card-verbs" :group "sprint"
   :title "One batch selector grammar for card verbs"
   :text "Brief, recut, rework, return, release, rank and drop share selectors by stream, friend, state
    and ids file with a dry run, and a cheap listing of cards by repo."
   :date "2026-10-10"
   :release "later"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "tla-cards-forced-to-frontier-tier" :group "sprint"
   :title "Cards that edit TLA+ models get the frontier tier"
   :text "Adding or generating a card whose paths name tla sets the frontier tier and refuses a lower one,
    except for run records."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "external-operands-and-single-wait" :group "sprint"
   :title "External dependency operands and one wait for hold, sentinel and wave"
   :text "A card can depend on a merged pull request, a commit on a branch, or a time, and the tick
    releases it when the operand holds. Hold, sentinel and wave become one wait in the code."
   :date "2026-10-10"
   :release "later"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "speculative-dispatch-renaming-vector" :group "sprint"
   :title "Speculative dispatch, shared-path renaming and vector cards"
   :text "Long-range ideas for the card system: deal a card on a need still in review, run cards that
    share paths in parallel and resolve at retire, and deal one batch of sub-cards with a verdict
    each. Each needs a model first and a measured gain."
   :date "2026-10-10"
   :release "later"
   :origin "cards moved out of the sprint (work record, 2026-10-04)")
  (item "add-requires-tla-run-records" :group "docs"
   :title "Refuse a card that edits a TLA+ model without refreshing the run records"
   :text "The add verb refuses a brief whose paths cover a model file but not the run records file, and
    names the remedy. This stops landed cards from making the records stale."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "fleet-quiet-and-overload-alarms" :group "ops"
   :title "Quiet a machine by verb and alarm on runaway test processes and overload"
   :text "A fleet quiet verb stops dealing to a member for a set time, and the beat reports the count of
    live test processes so the tick raises one judgment per episode. The overload alarm covers
    friends as well as machines."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "friend-work-restriction-by-stream-kind" :group "friends"
   :title "Restrict the work a friend is dealt by stream and card kind"
   :text "A friend row may list stream patterns and card kinds, and the dealer never deals that friend a
    card outside them. Add refuses a card that names a friend outside her restriction."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "verbs-take-many-targets" :group "sprint"
   :title "Let resume and wait take several streams or notes at once"
   :text "Resume takes more than one stream and wait takes several notes, each set or refused on its own
    line. This replaces hand loops by the coordinator."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "sprint-view-locked-by-file" :group "docs"
   :title "The sprint view frame is pinned byte for byte by a lock file"
   :text "The where frame and the watch writes are compared against a committed lock file so the view
    cannot drift from its design."
   :date "2026-10-10"
   :release "v1.4"
   :origin "PR #5014")
  (item "auto-sentinels-released-by-tick" :group "sprint"
   :title "Auto sentinels, released by the tick when their needs land"
   :text "A third kind of sentinel is released by the tick as soon as its needs have landed, with no
    person's release."
   :date "2026-10-10"
   :release "later"
   :origin "PR #5308")
  (item "sprint-refmodel-lint-clean" :group "docs"
   :title "Staticcheck clean in the sprint reference model"
   :text "Remove unused variables so staticcheck is clean in the reference model package.
    Point release: v1.2.12."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "PR #5335")
  (item "sprint-pause-and-stop-replay" :group "sprint"
   :title "Sprint pause and unpause verbs with idempotent stop return replay"
   :text "The sprint can be paused and unpaused, and a replayed stop return with an operation id is safe.
    The pause state machine has a checked TLA+ model.
    The pause admission and the idempotent STOP-return replay by op id (the slice-d draft) belong
    here unless the conversion needs them."
   :date "2026-10-10"
   :release "later"
   :origin "PR #5532")
  (item "friend-health-future-proof-refused" :group "friends"
   :title "Friend health refuses a proof dated after the server clock"
   :text "A health proof with a time ahead of the server clock is refused, so a friend cannot be kept up
    by a future date. The proof order check then stays sound.
    Point release: v1.2.14."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "nova-sprint PR #2")
  (item "role-views-for-models" :group "sprint"
   :title "Role views for a model: view coordinator, view worker, and a view API"
   :text "Compact ranked views serve a model what needs it, instead of the human dashboard. Items carry
    the next command and a cursor for incremental reads."
   :date "2026-10-10"
   :release "later"
   :origin "nova-sprint PR #3")
  (item "coordinator-handoff-writes-row-and-role" :group "sprint"
   :title "The coordinator verb also writes the config row and the role"
   :text "Moving the coordinator seat also updates the config sprint row and the stored coordinator role.
    A later config apply then does not write the old holder back.
    Point release: v1.2.14."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "nova-sprint issue #42")
  (item "lane-registry-tool" :group "friends"
   :title "Start, brief, watch and stop a child lane through a registry tool"
   :text "Lanes are started with long prose briefs and hope. A tool starts, briefs, budgets, watches and
    stops a lane and records its handoff."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2064")
  (item "route-choice-from-measured-failure-rates" :group "ops"
   :title "Choose provider routes per bench from measured failure rates"
   :text "Routes per bench are picked automatically from measured zero-token and nonzero-exit rates by
    bench and model, not from a hand-written list."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2385")
  (item "sprint-row-per-machine-and-table" :group "sprint"
   :title "Each machine computes its sprint row and pushes it to a store"
   :text "Every machine builds its own row of queue, working, done, ok, fail and rates every ten seconds,
    and the viewer reads the store."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2389")
  (item "card-cost-reductions" :group "sprint"
   :title "Cut the per-card cost in setup, tests and parallelism"
   :text "Self-contained cards need no repo, cards run only the useful test command with cached tool
    discovery, and Go parallelism is bounded per card."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2399; issue #2400; issue #2401")
  (item "priority-fixes-to-front-of-queue" :group "sprint"
   :title "Reviewer findings on the current batch jump to the front"
   :text "A finding on the current batch becomes a card at the front of the swarm and merge queue, keeping
    the fix close to the base."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2417")
  (item "promote-up-tier-escalation" :group "sprint"
   :title "Escalate a node that fails twice to the next tier"
   :text "A node that fails the bar twice on a cheap route moves up a tier with both attempts attached,
    never a third recut at the same tier."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2450")
  (item "reader-score-calibration" :group "ops"
   :title "Calibrate the model grader against typed scores"
   :text "The grader's prompt is tuned until its scores agree with friend scores on a calibration set,
    then it scores low-risk kinds."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2536")
  (item "sprint-table-counts-from-events" :group "sprint"
   :title "Sprint table counts come from a card event stream, not file times or comment scans"
   :text "Done, ok, fail, read, landed and calibration counts per friend and per bench are folded from
    card events, so the table needs no GitHub scan and shows cut to landed."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2563; issue #2678; issue #2680")
  (item "sprint-progress-from-work-set" :group "sprint"
   :title "Sprint x/y percent and ETA come from the work set"
   :text "One reader loads the roadmap and work files, a landed acceptance kind exists, and a sprint is
    any bounded task set answering x/y percent in under a second, with GitHub issues ingested into
    the file."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2595; issue #2664; issue #2679; issue #2593; issue #3141")
  (item "needs-human-line" :group "sprint"
   :title "A needs-you line lists what waits on a person, oldest first"
   :text "The sprint table and status print the items waiting on a human, such as asked cards, PRs with no
    typed line and held cards, with who is expected, and a supervisor owns the notifications."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2592")
  (item "github-events-stream" :group "sprint"
   :title "GitHub events arrive in a stream so loops are event-driven"
   :text "A webhook receiver writes GitHub events to a stream with a durable consumer and a polling
    fallback, so PR, CI and typed-line changes reach loops and the table within seconds."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2657; issue #2685")
  (item "decision-records" :group "sprint"
   :title "Owner decisions are records with options, default, deadline and ruling"
   :text "A decide verb opens, shows and rules decision records, and the table shows one count of open
    decisions."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3143")
  (item "coordinator-role-portable" :group "sprint"
   :title "The coordinator is a role any friend AI can hold on any machine"
   :text "Every coordinator action is a verb, and the loops and tables run on any Mac or Linux machine
    with failover, so no step depends on one person or host."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3295; issue #3543; issue #2061; issue #2062; issue #2065; issue #2066; issue #2069; issue
    #509; card moved out of the sprint (work record, 2026-10-04)")
  (item "pitstop-state-in-store" :group "sprint"
   :title "A pit stop and its lift are one store state every friend reads"
   :text "The stop and its lift are set and cleared by a verb and held in the sprint store, so a friend
    never relies on a bus note to know whether to work."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3371")
  (item "task-redistribute-verb" :group "sprint"
   :title "One verb moves ready work to whoever has open slots"
   :text "A single atomic verb cancels stale items and moves ready work across queues to friends with open
    slots, honouring the named owner, instead of hand loops."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3407")
  (item "spec-approval-machinery" :group "sprint"
   :title "Make spec approval reliable: one SPEC line form, a verdict verb, sha-keyed reads"
   :text "Spec reads use one machine-checked line form written by a verb. A verb computes the gate verdict
    from the store, reads are keyed to the body hash so stale ones never count, and a rewrite names
    the gaps it closes."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3564; issue #3565; issue #3566; issue #3568; issue #3569")
  (item "children-visibility-on-table" :group "sprint"
   :title "Show every seat's children on the sprint table within a second"
   :text "The table shows each live child with task, step, age and cost, read from the store, including
    reads and spec reviews that hold no queue lease."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3519")
  (item "card-turn-and-reasoning-bounds" :group "sprint"
   :title "Bound turns and reasoning on the live card templates"
   :text "Read-family cards carry a low turn limit and low reasoning, and writing-family cards a higher
    one, to cut tokens per card."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3534")
  (item "sprint-table-host-and-consumer-columns" :group "sprint"
   :title "Sprint table: disk room per host and one consumer table for friends and hosts"
   :text "The sprint table shows free disk per host, red under the floor, and one table shape (ready,
    working, done, ok, fail, ok%) for friends and hosts."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3584; issue #4071")
  (item "card-cut-context-and-worker-notes" :group "sprint"
   :title "Cards carry what changed on the base, and merge notes reach live workers"
   :text "The card cut lists base commits touching its paths since the issue, and a note verb lets merge
    learnings reach a worker mid-task."
   :date "2026-10-10"
   :release "later"
   :origin "issue #4189; issue #4186")
  (item "automatic-card-flow-and-single-writers" :group "sprint"
   :title "Cards flow automatically through the tables, moved only by single-writer verbs"
   :text "A waiting card is complete, so a deal is one move. Verbs deal, work, end, land and cancel are
    the only writers of stream, host and friend sets, and everything stays in the store under one
    second."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3636; issue #3662; issue #3911; issue #3929")
  (item "stream-table-events-and-failed-card-review" :group "sprint"
   :title "The stream table is true end to end; every refusal and failed card is raised"
   :text "Each column change is an event, a nightly control walks a card through all columns, every
    refusal and error is raised to the coordinator, and a failed card goes to a typed review
    verdict."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3999; issue #4155; issue #4072")
  (item "sprint-store-keys-and-reset-verb" :group "sprint"
   :title "Sprint store keys do not expire, and a reset to zero is a backed-up verb"
   :text "Beats, presence and history keys drop their TTLs and length caps in favor of recorded time and
    reader judgment. A reset verb deletes the sprint state families as admin after a backup."
   :date "2026-10-10"
   :release "later"
   :origin "issue #3878; issue #4195")
  (item "parent-card-cuts-children" :group "sprint"
   :title "A parent card cuts children, waits and stitches"
   :text "A parent fans out from a plan, depends on its children, takes corrections mid-flight, then
    reviews and stitches in its second phase."
   :date "2026-10-10"
   :release "later"
   :origin "issue #4317")
  (item "coordinator-console-and-comfort" :group "sprint"
   :title "The coordinator console is one grammar and settles what it can"
   :text "One spelling per concept, one receipt shape, fast quiet calls and a home screen. The system
    settles what it can before waking the coordinator."
   :date "2026-10-10"
   :release "later"
   :origin "issue #4352; issue #5096")
  (item "sprint-test-redis-version-check" :group "ops"
   :title "Sprint test Redis helper checks the pinned version"
   :text "The helper returns whatever Redis is first on the path. It should pin the version and fail on a
    mismatch, like the shared helper.
    Point release: v1.2.12."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "issue #5172")
  (item "tick-watchdog-bounded-diagnostics" :group "sprint"
   :title "The tick watchdog bounds diagnostics and refuses negative deadlines"
   :text "Watchdog exit must not wait on diagnostics that can block. A negative deadline is refused.
    Point release: v1.2.14."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "issue #5173")
  (item "refmodel-reads-needed-and-tiers" :group "docs"
   :title "The reference model learns reads needed and tiers"
   :text "The reference model assumes two reads per card and has no tier. Teach it reads needed and add
    flash cards and routed stores to its fixtures."
   :date "2026-10-10"
   :release "v1.4"
   :origin "issue #5195")
  (item "card-kinds-mechanical-tool-work" :group "sprint"
   :title "Card kinds for fix-red work cut from triaged defects"
   :text "A fix-red card kind names its source and test files and its reproducing test, and is accepted
    only when that test goes green. One card per triaged defect."
   :date "2026-10-10"
   :release "later"
   :origin "issue #1655")
  (item "cards-issue-to-landed-no-hand-step" :group "sprint"
   :title "A card goes from issue to landed with no coordinator hand step"
   :text "List what remains before accept, routing, reads and landing all run by the system alone, and
    close each gap."
   :date "2026-10-10"
   :release "later"
   :origin "issue #1725")
  (item "capacity-discovery-and-matching" :group "sprint"
   :title "Discover offered capacity and match ready work without double-counting shared pools"
   :text "A coordinator learns what each friend can do now and whether workers share one account limit.
    Stale advertisements do not count."
   :date "2026-10-10"
   :release "later"
   :origin "issue #176")
  (item "control-events-reprioritize-stop" :group "sprint"
   :title "Reprioritize, correct and stop work already dealt"
   :text "Authenticated control events with scope, update number and priority reach every running
    descendant, and the sprint can prove each one applied."
   :date "2026-10-10"
   :release "later"
   :origin "issue #179")
  (item "autonomous-goal-execution" :group "sprint"
   :title "Carry an accepted goal to its endpoint without routine human steering"
   :text "The sprint persists the goal, gates, priorities and budget, and recovers from a failed worker,
    exhausted provider or lost context while the owner is away."
   :date "2026-10-10"
   :release "later"
   :origin "issue #187")
  (item "width-from-measured-memory" :group "sprint"
   :title "Derive a machine's width from measured per-card memory with a controlled ramp"
   :text "Widths are measured, not guessed, and limits are found by a controlled ramp."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2019")
  (item "pause-signal-honored-everywhere" :group "sprint"
   :title "A fleet-wide run, pause and stop signal honored by every loop"
   :text "One signal with a generation is checked by dealing, launch, harvest and landing before each
    action, and bad signal states cause no side effect."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2022")
  (item "preflight-and-canary-before-dealing" :group "sprint"
   :title "Preflight every machine and canary each new card family before dealing wide"
   :text "The first few cards of a new family run alone, and a machine is checked at loop start, so mass
    failures show in minutes."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2030")
  (item "funnel-and-status-report" :group "sprint"
   :title "A standing report and status verbs for the funnel from launch to landed"
   :text "Launched, returned, gate result, pull requests and landed are counted per wave and per model,
    with live cards and widths by machine, all from records."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2034; issue #2049")
  (item "harvest-wave-into-one-gated-pr" :group "sprint"
   :title "Harvest a wave into one gated pull request and write verdicts back"
   :text "One verb collects returns, applies commits, runs the suite, opens the pull request and records
    each verdict on its source."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2067")
  (item "sprint-behaviour-visualization" :group "sprint"
   :title "Visualize the sprint as linked behaviour views"
   :text "Show the whole behaviour at once: a card timeline with a lane per bench, the pipeline from
    source to landing as linked views, and measures plotted against width, route and card kind."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2110; issue #2111; issue #2112; issue #2114")
  (item "sprint-event-log-merge-and-expander" :group "sprint"
   :title "Emit merge-queue and expander events to the structured log"
   :text "Log enqueue, group start, group verdict and park events from the merge queue, and derive and cut
    events from the work language expander, as the logging spec requires."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2185; issue #2187")
  (item "sprint-queue-fence-and-order-tests" :group "ops"
   :title "Queue fence, lane order and unknown-call tests"
   :text "Prove that a worker that lost its lease cannot write, lanes are read in priority order with a
    directory fallback, and an expired remote call stays unknown and keeps its seat until answered
    or bounded."
   :date "2026-10-10"
   :release "v1.4"
   :origin "issue #2196; issue #2197; issue #2198")
  (item "jobs-kernel-uncertain-reservation-and-executor" :group "sprint"
   :title "Kernel: uncertain reservations, lease re-grant and executor seam"
   :text "Hold an uncertain reservation and never re-grant it on expiry, reap on lease expiry, and give
    the executor seam a parent grant, a version-bound result and uncertain-on-disconnect."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2239; issue #2240; issue #2247")
  (item "jobs-kernel-tool-version-refusal" :group "sprint"
   :title "Kernel: tool key invalidation and below-version refusal"
   :text "Compare a unit's declared tool versions and semantic keys at admission: invalidate outputs when
    a key moves, and refuse a unit whose tool is below its version."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2242; issue #2248")
  (item "jobs-kernel-collections-warm-done-when" :group "sprint"
   :title "Kernel: collection harvest, warm state, done-when and acceptance"
   :text "Bind collection members at harvest, keep retained warm state out of active capacity, make the
    set finish line a report not a gate, and refuse a unit without acceptance at load."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2241; issue #2243; issue #2249; issue #2250")
  (item "duty-tier-policy-and-escalation" :group "sprint"
   :title "Duty tier: resident session executes policy, escalations as nodes, learned admission"
   :text "The resident session executes an approved finite policy and never authors it, and every judgment
    it cannot make becomes an escalation node with a rule, a default and an age. Admission checks
    can be learned from the history of abstain reasons, with preservation tests for handoff records."
   :date "2026-10-10"
   :release "later"
   :origin "issue #500; issue #585")
  (item "typed-decision-models-for-routine-judgments" :group "sprint"
   :title "Typed-decision models for card routing and other routine judgments"
   :text "A fast schema-typed model with a calibrated confidence makes bounded routine decisions such as
    card type, abstain reason and note triage. It saves tokens, cost and wall clock against a
    general model."
   :date "2026-10-10"
   :release "later"
   :origin "issue #589; issue #896; issue #5241")
  (item "card-dependencies-and-verdicts-as-state" :group "sprint"
   :title "Card dependencies as edges, and verdicts and holds as state"
   :text "Cards carry needs and blocks edges, and a card is ready only when every dependency is merged and
    green. Read verdicts are state re-evaluated each tick, holds are edges, and every hand action is
    a verb."
   :date "2026-10-10"
   :release "later"
   :origin "issue #785; issue #854; issue #2437; issue #3219")
  (item "one-verb-card-moves" :group "sprint"
   :title "Any card moves anywhere with one verb"
   :text "One verb moves a card from where it is to where the coordinator wants it, including handing a
    dealt card back. A pin to a down friend never holds a card."
   :date "2026-10-10"
   :release "later"
   :origin "card from the sprint store (2026-10-10)")
  (item "delivery-milestone-records" :group "sprint"
   :title "Delivery milestones are explicit records"
   :text "Each milestone is a record on the card and stream with repository, target ref, commit and gate
    or review evidence: staged, verified in dev, released."
   :date "2026-10-10"
   :release "later"
   :origin "card from the sprint store (2026-10-10)")
  (item "release-held-cards-verb" :group "sprint"
   :title "Release takes a stream or every held card"
   :text "The release verb accepts a stream name or releases every held card, building on a verb that
    moves any card anywhere."
   :date "2026-10-10"
   :release "later"
   :origin "card from the sprint store (2026-10-10)")
  (item "idle-trace-and-alarm-unit-tests" :group "ops"
   :title "Unit tests for the idle trace and idle alarm"
   :text "The idle trace and idle alarm code gets unit tests for every root, raising its statement
    coverage from a small fraction."
   :date "2026-10-10"
   :release "v1.4"
   :origin "card from the sprint store (2026-10-10)")
  (item "brief-repo-line-lint" :group "sprint"
   :title "A brief's repo line is always owner/name"
   :text "Admission lint, recut and rework refuse a repo line that is not owner/name, and the tier writer
    stamps only the result line.
    Point release: v1.2.14."
   :date "2026-10-10"
   :release "v1.2.x"
   :origin "card from the sprint store (2026-10-10)")
  (item "tidy-tombstone-for-landed-cards" :group "sprint"
   :title "Tidy leaves a tombstone for every landed card"
   :text "A need on a landed card that tidy removed must read as landed, not as a missing id. A tombstone
    lets the card listing tell landed-and-tidied from never-existed."
   :date "2026-10-10"
   :release "later"
   :origin "card from the sprint store (2026-10-10)")
  (item "repeat-refusal-alarm" :group "sprint"
   :title "The third refusal of one cause in five minutes raises one judgment"
   :text "Three refusals of the same cause within five minutes raise one judgment, rewritten in place and
    closed after five quiet minutes; watch --wake wakes on it. Its TLA+ model is rebuilt with it."
   :date "2026-10-10"
   :release "later"
   :origin "nova-sprint commit fbcb205 (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "merge-health-judgment" :group "sprint"
   :title "Merge health is one coordinator judgment every ten minutes"
   :text "The coordinator pass raises one merge-health judgment every ten minutes naming the base, the
    main line, stopped streams, the promotion PR and unstitched branches. The drift facts it reads
    already exist."
   :date "2026-10-10"
   :release "later"
   :origin "nova-sprint commit 412f684 (not carried by the re-seed, nova-sprint PR #28)")
  (item "processor-counters" :group "sprint"
   :title "Processor counters: IPC and stall reasons from the stage stamps"
   :text "where --json reports stage-time counters (IPC, stall reasons, the top stall) with a dashboard
    row, and the card model proves stalls partition wall time, with a reversed witness."
   :date "2026-10-10"
   :release "later"
   :origin "nova-sprint commit 415d761 (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "release-check-cold-audit" :group "sprint"
   :title "release check asks for a cold audit of twenty landed cards"
   :text "release check --audit asks for cold reads of twenty landed cards, and a plain release check
    passes only with an all-ok audit under 48 hours old."
   :date "2026-10-10"
   :release "later"
   :origin "nova-sprint commit 1123e21 (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "fleet-lane-route-mark" :group "sprint"
   :title "A route mark on local-endpoint lanes in the fleet track"
   :text "The dashboard fleet track shows a route mark and an endpoint tooltip on lanes served by a local
    endpoint."
   :date "2026-10-10"
   :release "later"
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
   :release "later"
   :origin "nova-sprint commit 639ccef (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04); PR #5281")
  (item "verified-working" :group "friends"
   :title "A friend AI working count holds only verified cards"
   :text "A friend AI working count includes only cards with a push or named by beat --running, a dealt
    column is added to the friends table, and an idle friend AI gets one judgment after 15 minutes."
   :date "2026-10-10"
   :release "later"
   :origin "nova-sprint commit 2c831fa (not carried by the re-seed, nova-sprint PR #28); card moved out of
    the sprint (work record, 2026-10-04)")
  (item "store-rating-and-dogfood-rounds" :group "ops"
   :title "A rating, review, dogfood and audit round of the current release"
   :text "Each friend AI rates, dogfoods and audits each tool as released and writes the reports the
    earlier rounds asked for. The planned rounds of past releases are kept here as one recurring
    practice."
   :date "2026-10-10"
   :release "later"
   :origin "cards from the sprint store (2026-10-10); cards moved out of the sprint (work record,
    2026-10-04)")
  (item "fix-card-tiered-context" :group "sprint"
   :title "Three-tier context for fix cards"
   :text "A fix card carries only what it needs, in three tiers: the error line; then the enclosing AST
    block (go/ast); then the exported signatures of that block's direct imports. A rework for a
    wrong brief widens the context one tier, capped at three, the way a reader's finding widens a
    card's PATHS."
   :date "2026-10-10"
   :release "later"
   :origin "a Gemini dialog the owner shared, 2026-10-10")
  (item "read-collapsing" :group "sprint"
   :title "Read cards share one immutable snapshot"
   :text "N read cards of one branch share one immutable snapshot, a bundle at one ref per host, instead
    of each fetching its own. The dedupe happens in the scheduler, not in the card state machine.
    Today each read is a card, dealt cheapest-first, and nothing collapses them."
   :date "2026-10-10"
   :release "later"
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
   :release "later"
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
   :release "later"
   :origin "a Gemini dialog the owner shared, 2026-10-10")
  (item "friend-per-tier-model-choice" :group "friends"
   :title "Each friend's model per tier is decided and recorded"
   :text "Friend rows record which model serves each tier, so the deal and the friend agree on it. Friends
    set up with missing tiers are found and corrected."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04); PR #5387")
  (item "decision-evaluation" :group "sprint" :area "nova-decide"
   :title "Evaluating the decision model's decisions"
   :text "An evaluation harness that scores the decision model's past decisions against their outcomes, a detector for
    reads that bounced good work, a classifier for why a card is held, and the promotion of a decision
    from shadow to acting once it measures well."
   :why "New capability."
   :cards 4
   :date "2026-10-09"
   :release "later")
  (item "record-replication-and-restore" :group "ops"
   :title "Replicate the sprint record to a second machine with a restore verb"
   :text "The playbook replicates the SQLite record continuously, and a restore from the replica matches
    the row count."
   :date "2026-10-10"
   :release "later"
   :origin "issue #2555")
  (item "coordinator-leased-shared-resources" :group "sprint"
   :title "Shared resources are leased by the coordinator"
   :text "Benches, branches, ports and accounts are claimed and released only through coordinator verbs
    with leases, so a down holder cannot keep one."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")
  (item "brief-lint-honest-attribution" :group "sprint"
   :title "Brief lint refuses hiding the model or harness"
   :text "nova-swarm lint gains a default rule that refuses a brief telling a worker to deny, hide or
    misstate its model or harness."
   :date "2026-10-10"
   :release "later"
   :origin "card moved out of the sprint (work record, 2026-10-04)")))
