; The nova-sprint roadmap: the data ROADMAP.md is generated from.
; Read by internal/roadmapdoc through internal/worklang (data, never evaluated).
; Edit this file, then run `make roadmap`; never edit ROADMAP.md by hand.
; The shape is in internal/roadmapdoc's package comment.
; The roadmap as it stood until 2026-10-27 is kept in docs/roadmap_old.sexp.
(roadmap "v1"
 :title "nova-sprint roadmap"
 :text "The plan from 2026-10-27. nova-sprint is rebuilt as a tight engine and a policy layer, one
  layer at a time from the bottom, each layer secured to the bar and adopted into the live sprint
  before the next part starts. Every release cuts nova-tools and nova-sprint together at one
  version. The roadmap before this plan, with every item it held, is in docs/roadmap_old.sexp; an
  item there comes back here only when it is part of this plan."

 :groups
 ((group "layered-rewrite"
   :title "The layered rewrite: an immaculate engine and Starlark policy"
   :text "nova-sprint is far too large for what it is: at main 16b1c740 (2026-10-26) about 196k
    hand-written lines of Go, 223k of tests and 400k generated, with the CLI as heavy as the engine
    and steps functions of 400 to 600 lines. At heart it is a card dealer: a queue of cards with
    needs, dealt to workers, tracked through reads, CI and landing, refusing what breaks the rules.
    The rewrite splits it in two with one interface between. The engine is Go, fixed, modelled in
    TLA+ and locked: the store, the card machine, the steps (each one legal move with its refusals,
    checking a move and never choosing one) and the edges. The policy is changed during a sprint
    without a new binary: data tables for what is only numbers and lists, Starlark scripts for
    logic. The interface is the snapshot policy may see and the decisions it may ask for; the
    script proposes and the engine disposes. The test of which side code belongs on: if being wrong
    can corrupt the sprint (two writers, a lost landing, a card in two states) it is engine; if it
    only makes the sprint slow, costly or badly ordered it is policy. The engine is held to an
    immaculate bar: small, every line earning its place, no special case, every guarantee modelled
    and every refusal tested; whatever cannot meet that goes to Starlark or data. Each layer is
    secured to the bar (modelled with reversed witnesses, tested on a real store including every
    refusal, a second reader's probes green, used for real work, documented, then locked with
    Glenn) before anything above it starts. Every part is adopted at once: it lands on main, ships
    in the next release within a day, and runs the live sprint before the next part of its layer
    starts; a part not yet run in real use is not done. Until the rewrite is through, a change to
    nova-sprint deletes more than it adds. Source: Glenn, 2026-10-26 and 2026-10-27: \"I also agree
    it is very large for what it actually is.\"; \"Start with the first layer securing tonight.
    Work in small parts.\"; \"once we have secured layers, actually use and adopt the fixed stuff
    soon, so we are not off doing large changes without actually verifying them\"; \"OK approved.
    go starlark\"; \"I want our part that is not starlark to be immaculate\"; \"We create a tight
    engine here now.\"")
  (group "layer-1"
   :title "Layer 1, the store: the parts in order"
   :text "The store's contract is docs/layers/L1-STORE-CONTRACT.md: what it owns, its 24
    guarantees, its refusals, which model and test covers each, and the parts below in order, each
    landable alone. Parts 1 and 2 shipped in v1.6.26 and ran the live sprint on 2026-10-27.")
  (group "store-holes"
   :title "Holes the fence model found in the store"
   :text "StoreFence v2 (branch l1/store-fence-v2, 2026-10-27) models operation ids as the code
    builds them and abandonment at once on a rule refusal. Two of its counterexamples are holes in
    the code, each checked against the code by hand. Each is fixed engine-side, modelled first.")
  (group "releases"
   :title "Releases"
   :text "Every release cuts nova-tools and nova-sprint together at one version (Glenn,
    2026-10-27: \"Please unify version between nova-tools and nova-sprint. This means always cut
    both at the same time.\"; \"go back to releasing together so the same version is deployed
    across both.\"). One combined build: the nova-tools release carries the nova-sprint tools of the
    same version, and every machine runs that one version.")
  (group "found-fixes"
   :title "Small fixes found on the way"
   :text "Defects the cold reads of 2026-10-27 found in work already on main. Each is a small fix
    for the next release, modelled where it touches a state machine."))

 :items
 (
  ; The layered rewrite
  (item "rewrite-l1-store" :group "layered-rewrite" :area "nova-sprint"
   :title "Layer 1: the store"
   :text "Engine. Cards, rows and the event log on nova tables; the layer to secure is the engine
    and tick above the generated wire code. Its parts are the layer-1 group below."
   :exists "internal/sprint/store, about 64k lines with tests; store/engine.go 2,750 lines,
    store/tick.go 2,379, store/mem.go 1,573; the contract docs/layers/L1-STORE-CONTRACT.md."
   :why "In progress since 2026-10-26."
   :date "2026-10-26")
  (item "rewrite-l2-card-machine" :group "layered-rewrite" :area "nova-sprint"
   :title "Layer 2: the card machine"
   :text "Engine. The states and transitions of one card (add, take, finish, read, land, hold,
    retire) as one module, model first, with the reference model as the oracle."
   :exists "tla/CardMachine.tla and internal/sprint/refmodel (about 10k lines)."
   :why "Waits for layer 1 and rocketnet M1."
   :date "2026-10-26")
  (item "rewrite-l3-steps" :group "layered-rewrite" :area "nova-sprint"
   :title "Layer 3: the steps, the engine's vocabulary"
   :text "Engine. One step per legal move (deal, take, finish, read, land, hold, stop), each a pure
    function from snapshot and request to plan or refusal, split along the card machine's
    transitions; no function longer than a screen. A step checks a move and never chooses one."
   :exists "internal/sprint/steps_*.go; steps_work.go alone is 4,322 lines with functions of 494
    and 593 lines, much of it policy inside the moves."
   :why "Waits for layer 2 and the interface."
   :date "2026-10-26")
  (item "rewrite-l4-edges" :group "layered-rewrite" :area "nova-sprint"
   :title "Layer 4: the edges"
   :text "Engine. Git and landing, CI receipts, friends' hooks and the bus: each a separate adapter
    with a narrow contract, faked in unit tests."
   :exists "pkg/friend, internal/ci, cmd/nova-sprint/reads.go, cmd/nova-sprint/land.go."
   :why "Waits for layer 3."
   :date "2026-10-27")
  (item "rewrite-interface" :group "layered-rewrite" :area "nova-sprint"
   :title "The interface: snapshot and decisions"
   :text "Between engine and policy: the snapshot policy may see (a typed, versioned schema) and the
    decisions it may ask for, each mapping onto one step. The models take policy as any choice of
    these decisions, so every invariant holds for every script. Drafted as docs/layers/INTERFACE.md
    while layers 1 to 3 are secured, settled with Glenn before layer 3 is cut to it."
   :why "Drafted alongside layers 1 to 3."
   :date "2026-10-27")
  (item "rewrite-l5-policy-data" :group "layered-rewrite" :area "nova-sprint"
   :title "Layer 5: policy data"
   :text "Policy. Numbers and lists (tiers, routes, widths, deadlines, thresholds, read mix, costs)
    in one versioned policy table in the store, changed by a coordinator verb that checks it and
    replays it against recent real snapshots before it takes effect; every decision records the
    version that made it; rollback is one verb."
   :exists "Spread over nova-config rows and the set and stream set knobs."
   :why "Waits for the interface."
   :date "2026-10-27")
  (item "rewrite-l6-policy-scripts" :group "layered-rewrite" :area "nova-sprint"
   :title "Layer 6: policy scripts in Starlark"
   :text "Policy. Deal order, when to raise a judgment, when to stop a stream, rework routing, as
    Starlark (go.starlark.net) functions from snapshot to decisions: deterministic, hermetic, with a
    step bound per tick. Scripts live in the store, versioned, set only from the coordinator seat;
    a new version replays against recent snapshots and is refused on any error; a failure at tick
    time falls back to the last good version. Policy has its own unit tests. A tracer bullet may
    come first: the deal order alone, behind a flag, on the live sprint."
   :why "Waits for layer 5."
   :date "2026-10-27")
  (item "rewrite-l7-cli-and-readers" :group "layered-rewrite" :area "nova-sprint"
   :title "Layer 7: the CLI, the dashboard and replay"
   :text "Thin verbs that parse, call a step and print, under 10k lines from about 115k; a policy
    change goes through the same checked path. Dashboard and replay are read-only views."
   :exists "cmd/nova-sprint/verbs.go is 4,930 lines with 140 functions; internal/sprintdash,
    internal/sprintreplay."
   :why "Waits for layer 6."
   :date "2026-10-26")

  ; Layer 1, the store
  (item "l1-part-1-dead-exports" :group "layer-1" :area "nova-sprint"
   :title "Part 1: the dead exports out of production"
   :text "Five exports with no production caller deleted, three read only by tests moved into
    observe_test.go; the ten Mem setters stay, as tests in other packages call them."
   :exists "Landed in #620, shipped in v1.6.26, ran the live sprint 2026-10-27."
   :why "Done."
   :date "2026-10-27")
  (item "l1-part-2-citations" :group "layer-1" :area "nova-sprint"
   :title "Part 2: the models cite Store.Run by its parts"
   :text "DirtyTickRead, AttemptsEpochs and the COVERAGE row cite the parts of Store.Run, not line
    numbers; the 12 groups re-ran on a bench."
   :exists "Landed in #622, shipped in v1.6.26."
   :why "Done."
   :date "2026-10-27")
  (item "l1-part-3-storefence" :group "layer-1" :area "nova-sprint"
   :title "Part 3: StoreFence, the fence and commit core modelled"
   :text "tla/StoreFence.tla holds G3 to G7 and G14: Acquire, the manifests, Commit, the caller-op
    check, Repair (the repairer sends the manifests itself; abandons past the grace, or at once on a
    rule refusal), writes outside the fence and Clear, with operation ids as the code builds them.
    It lands with its records once the two holes it found are fixed or held as named failing
    cases, and the COVERAGE file list names lock.go."
   :exists "#625 (the first draft, which a cold read found unsound) is held; branch
    l1/store-fence-v2 at c9404753 is the revision: ten cases on hetzner2, each as declared."
   :why "Waits for the store-holes decisions."
   :date "2026-10-27")
  (item "l1-part-4-mem-redis" :group "layer-1" :area "nova-sprint"
   :title "Part 4: Mem and Redis agree"
   :text "Fix the two divergences (Mem ignores an unreadable timer record; Mem's commit can stop
    partway, recording the caller's result without emptying the fence), each with a test that runs
    the same call on Mem and the fake Redis client."
   :why "After part 3."
   :date "2026-10-27")
  (item "l1-part-5-refusals-mem" :group "layer-1" :area "nova-sprint"
   :title "Part 5: every refusal tested on Mem"
   :text "The 16 refusals no test names, one small test each."
   :why "After part 4."
   :date "2026-10-27")
  (item "l1-part-6-refusals-real" :group "layer-1" :area "nova-sprint"
   :title "Part 6: every refusal tested on a real store"
   :text "redis_sprint_functional_test.go extended to every refusal of the contract's section 3."
   :why "After part 5."
   :date "2026-10-27")
  (item "l1-part-7-real-time-tests" :group "layer-1" :area "nova-sprint"
   :title "Part 7: no real time in the store's unit tests"
   :text "machine_twoloops_test.go (a real sleep), tick_ask_test.go (time.Now) and waitlog_test.go
    (a real 30-second timeout) take a fake clock or move to the functional tier."
   :why "After part 6."
   :date "2026-10-27")
  (item "l1-part-8-stream-remove" :group "layer-1" :area "nova-sprint"
   :title "Part 8: stream remove behind a store step"
   :text "cmd/nova-sprint/verbs.go deletes rows of two tables outside the fence and not atomically;
    make it one fenced operation."
   :why "After part 7."
   :date "2026-10-27")
  (item "l1-part-9-teardown-keys" :group "layer-1" :area "nova-sprint"
   :title "Part 9: the teardown keys"
   :text "archive, cardcheck and refusedrecord are on neither teardown list: a functional test that
    writes each and tears down decides whether they join machineKeys."
   :why "After part 8."
   :date "2026-10-27")
  (item "l1-part-10-split-run" :group "layer-1" :area "nova-sprint"
   :title "Part 10: Store.Run split along its seams"
   :text "The pin and caller-op check; the pre-plan enrichment handed in from above; the STOP and
    queue fences as a hook of layer 2; and the core read, plan, acquire, apply, release loop left in
    engine.go."
   :why "After parts 3 to 6 are green."
   :date "2026-10-27")
  (item "l1-part-11-steps-out" :group "layer-1" :area "nova-sprint"
   :title "Part 11: the step constructors out of the store"
   :text "steps.go's 50 step constructors move to layer 3: the steps import the store, never the
    other way round."
   :why "After part 10."
   :date "2026-10-27")

  ; Holes the fence model found
  (item "hole-shared-operation-ids" :group "store-holes" :area "nova-sprint"
   :title "Two runs of one caller operation share an operation id"
   :text "Two runs of caller operation c at one epoch both build the id c~e-1 (engine.go operation
    ids, schema.go OpFamily), and Release matches the fence by that id alone. When the first run's
    operation is abandoned at once on a rule refusal, the retry acquires the fence under the same
    id, and the first run's late Release(op, false) empties the retry's fence mid-operation; the
    retry's commit then writes nothing and reports ok. No timing assumption is needed. The fix: a
    per-writer nonce in the operation id, which closes it in the model (MCStoreFence passes with
    it; MCStoreFenceSharedOpIds is the code today)."
   :why "Found 2026-10-27; the fix is the next engine change."
   :date "2026-10-27")
  (item "hole-revived-refusal" :group "store-holes" :area "nova-sprint"
   :title "A refusal a later write undoes"
   :text "A writer delayed between Acquire and its first send can have its operation abandoned at
    once on a rule refusal (MEMBEREXISTS); if a later operation's plan makes that expectation hold
    again (deleting the member), the delayed writer's manifest applies inside the other
    operation's fence, and its release writes nothing and reports ok. A nonce does not close it;
    a fence check on manifests or a guard in the table layer would. First confirm that a real plan
    can undo such a refusal (unverified), then model the fix (MCStoreFenceRevivedRefusal)."
   :why "Found 2026-10-27; one step unverified."
   :date "2026-10-27")

  ; Releases
  (item "release-v1627-unified" :group "releases" :area "nova-sprint"
   :title "v1.6.27: nova-tools and nova-sprint together again"
   :text "The first release cut together since the split at v1.6.13: one combined build (the
    nova-tools tools and the nova-sprint tools at v1.6.27 on the four Unix platforms; nova-tools
    alone on windows-amd64, since nova-sprint does not build there), both tags cut together,
    installed on the studio and adopted across the fleet."
   :exists "Built on hetzner1 2026-10-27; the nova-sprint cut dry-runs clean."
   :why "nova-tools main is red: TestASendIsNotRetriedWhenTheStoreStalls (pkg/bus/timeout_test.go)
    failed in the nightly soak; it is a timing test, to be given an injected clock and landed
    before the cut."
   :date "2026-10-27")
  (item "release-sprint-windows" :group "releases" :area "nova-sprint"
   :title "nova-sprint builds for Windows"
   :text "cmd/nova-sprint/seat_environment.go uses syscall.Stat_t, so nova-sprint does not build for
    windows-amd64 and the Windows members carry the nova-tools tools alone. Split the file by
    build tag so the combined release covers every platform."
   :why "Small; the next release."
   :date "2026-10-27")

  ; Small fixes found on the way
  (item "fix-log-json-cap" :group "found-fixes" :area "nova-sprint"
   :title "log --json cuts at 20 lines by default"
   :text "nova-decide reads full nova-sprint log --json exports and ignores the more field, so an
    export made without --max 0 is silently short. Make the export path ask for every line, or
    have nova-decide refuse more: true."
   :why "The next release."
   :date "2026-10-27")
  (item "fix-serverlanes-stage-lane" :group "found-fixes" :area "nova-sprint"
   :title "The friend-stage lane is not in ServerLanes.tla"
   :text "The served friend stage runs on its own lane and takes the serial line twice with the
    line released mid-batch (serve.go); model the lane and its two-lock order in ServerLanes.tla."
   :why "The next release."
   :date "2026-10-27")
  (item "fix-friendstage-epoch" :group "found-fixes" :area "nova-sprint"
   :title "Friend-stage revalidation does not compare the epoch"
   :text "revalidateFriendStageSnapLocked compares kind, branch and start head but not the epoch,
    so a clear and re-deal during a clone can publish a JOB.md whose FINISH names the old epoch.
    Add the epoch to the comparison."
   :why "The next release."
   :date "2026-10-27")
 ))
