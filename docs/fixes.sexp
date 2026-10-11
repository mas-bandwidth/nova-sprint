; The nova-sprint point releases: the data FIXES.md is generated from.
; Read by internal/roadmapdoc through internal/worklang (data, never evaluated).
; Edit this file, then run `make roadmap`; never edit FIXES.md by hand.
; The shape is in internal/roadmapdoc/fixes.go.
(fixes "v1"
 :title "nova-sprint fixes"
 :text "The point releases from v1.2.3, the first release from this repository: what each one shipped, is
  shipping or will ship. A point release carries fixes; new work is in ROADMAP.md. The one
  exception is v1.2.5, which also carries the roadmap machinery (the owner's scope for it), including the
  verb that defers cards to the roadmap. nova-tools' point
  releases are in that repository's FIXES.md. The point releases after v1.2.10 (the owner, 2026-10-11): v1.2.11
  small fixes, v1.2.12 tests and models only, v1.2.13 contraction, v1.2.14 the next small fixes, each
  release's scope below. Point releases continue until only architectural items are open; those are
  v1.3's re-architecture, and ROADMAP.md says how the two fit. An area stops taking point fixes when a fix
  there is the same problem in another form, or would be its third fix: from then on it is
  architectural, and its v1.3 item names the point fixes it replaces. Friend work is dogfooded within the
  week once more friends are online: the current point release adopted, friends alone first, then
  friends and fleet together, and each friend bug found becomes a point-release card."
 :releases
 ((release "v1.2.3" :status "shipped" :date "2026-10-10"
   :text "The first release from this repository: nova-sprint, nova-card and nova-work, re-seeded from nova-tools.")
  (release "v1.2.4" :status "shipped" :date "2026-10-10"
   :text "One fix: the sprint store's function library is back at its v1.2.2 digest, so an adopt can load it.")
   (release "v1.2.6" :status "planned"
    :text "The fixes held during the split, the point-release candidates found on 2026-10-10, the owed
     tests, and the sprint store cards that are point-release work. Each is re-applied or written in
     this repository, then cut as one release."
    :date "2026-10-10")
   (release "v1.2.7" :status "planned" :date "2026-10-10"
    :text "The adopt can carry a change of the store's function library: its digest covers code, not
     comments, the pre-window shadow tick passes when only the library differs, and the post-load check
     refuses a library that is not the new build's.")
   (release "v1.2.9" :status "planned" :date "2026-10-10"
   :text "Seat, tick and fleet fixes: judgments waiting on the seat re-pushed, and a standing judgment pushed
    once while it holds; the tick under its gate at load; holds and approvals read at the current head;
    log --since windows wider than 22 hours; a loaded bench not marked down; a reader-only friend dealt no work; a
    machine fault hold that clears itself.")
   (release "v1.2.10" :status "shipped" :date "2026-10-11"
    :text "Cut from main between sprint releases: no deal to a draining member, the dashboard as the live page, main green after the night's landings, and two CI flakes fixed.")
   (release "v1.2.11" :status "planned" :date "2026-10-11"
    :text "Small bug fixes, one concern each, and no model change. They run as cards on v1.2.10 once it is
     adopted, so the sprint dogfoods the release it just cut.")
   (release "v1.2.12" :status "planned" :date "2026-10-11"
    :text "Tests and models only, no product change. More unit and functional tests, and every TLA+ model
     checked against the code it claims to model (its state, actions and invariants) and run again with
     TLC on a bench; owed models and stale or timed-out records are made real. The new tests are written
     against behaviour (the verbs, the store's steps, the wire, the TLA+ invariants), not internals, so
     the same tests run unchanged against v1.3 and say whether it keeps the behaviour. TLC runs where the
     cards run first. A real defect a new test finds is written as a v1.2.14 card, never fixed inside a
     test card.")
   (release "v1.2.13" :status "planned" :date "2026-10-11"
    :text "Contraction: fewer lines, no behaviour change, with v1.2.12's tests and checked models as the net
     under it. Passes in this order, one writer per package: remove dead and stale code; adopt the Go
     standard library or established packages where they replace hand-rolled code; run go fix (and gopls
     modernize) over all Go code; pull code the tools share into modules, then code the modules share
     into modules; make each test minimal, on test frameworks and shared helper modules. The module pass also covers
     the copies across the two repositories: 59 same-named packages exist as separate copies in
     nova-sprint and nova-tools (about 71,500 shared lines, measured 2026-10-11 at nova-sprint 15c779c
     and nova-tools 2a648d8). Each gets one owner, nova-sprint as with nova-roadmap, and the other
     repository imports it: the identical copies first, then the diverged ones (member, fleet, cardtree,
     tool, and parts of friend and swarm), reconciled under v1.2.12's tests. The first named entries are
     the nova-roadmap extraction and the brief boilerplate moving into the card contract. Measured as
     lines per repository and package, product against test, from a baseline taken before the first
     pass.")
   (release "v1.2.14" :status "planned" :date "2026-10-11"
    :text "The next small fixes, one concern each, including the defects v1.2.12's tests find, a circuit
     breaker on the review loop, and a red base pushed to the coordinator."))
 :items
 ((fix "reseed-from-nova-tools" :release "v1.2.3" :status "shipped"
   :title "The sprint tools re-seeded from nova-tools"
   :text "The three commands, their packages, models and docs, at nova-tools dev, under the re-seed recipe."
   :origin "the split, layer L0; PR #28")
  (fix "lua-digest-restore" :release "v1.2.4" :status "shipped"
   :title "Six Lua comment lines restored"
   :text "The function library's digest is its v1.2.2 value again, so the adopt's shadow tick accepts it."
   :origin "PR #33, head 1f403558d")
  (fix "adopt-carries-a-lua-change" :release "v1.2.7" :status "shipped"
   :title "The adopt can carry a change of the store's function library"
   :text "libraryMatches judges the library by its code, not its bytes: luaCode keeps each string (quoted
    or long) and each token, and turns every comment and run of blank space outside a string into one
    space, so a library that differs only in comments or blank space matches (v1.2.3's adopt refused the
    store over six comment lines). The shadow tick's pre-window check (libraryShadow) tolerates a library
    whose code differs, saying \"library differs: loaded in the window\"; the post-load check
    (libraryMatches) still refuses a store whose library is not the new build's, so a refusal restores
    the old library. TLA+ model Adopt, property RefusedLeavesOld, with the reversed witness
    MCAdoptBrokenKeepNew."
   :origin "the seat ledger v1.2.4-held-2026-10-10.md (a v1.3 card moved to v1.2.7); this pull request")
  (fix "roadmap-and-fixes-as-data" :release "v1.2.6" :status "shipped"
   :title "ROADMAP.md and FIXES.md generated from s-expression data"
   :text "docs/roadmap.sexp and docs/fixes.sexp are the data; `make roadmap` writes both pages, and a test
    fails when a page differs from what its data renders. The roadmap machinery the v1.2.3 re-seed left
    out comes back."
   :origin "this pull request; the machinery from commit b559389")

  (fix "no-land-without-reads" :release "v1.2.6" :status "shipped"
   :title "Nothing merges or lands short of its reads"
   :text "A merging card needs the ok reads a card in review needs (the reads setting, else its tier's rule).
    One short of them, the setting raised or its tier pinned after its accept, goes back to review on
    the next tick for the read it lacks (never one a lander marked landing before its push); the lander lands none, naming
    it and its count, checked again and marked landing in one step just before the push. The accept records
    the count it ran on.
    tla/Land.tla NoLandWithoutReads and PushedNeverSentBack, with the reversed witnesses
    MCLandBrokenNoCheckReads and MCLandBrokenBounceMarked."
   :origin "the Studio sprint, epoch 16, 2026-10-10: rule 6 raised on cards merging with one read of two, and two landed on one read")

  (fix "the-coordinator-hooks-in" :release "v1.2.6" :status "shipped"
   :title "The coordinator must hook in, and prove it, before any command runs"
   :text "nova-sprint hook holds the seat's subscription on the server, which streams every judgment and
    push with an id and a challenge; the hook counts once the session answers the challenge over the same
    connection, and again every 10 minutes. Every command run as the coordinator fails with exactly 'You
    must hook in first: run nova-sprint hook' until then, and names a push left unacknowledged past 10
    minutes; there is no override. unhook records the seat away, a drop records it gone at once, and each
    is pushed to the owner. seat watch, the PROOF files and seat pong are gone. tla/SeatHook.tla:
    CommandSafe, AckedOrBlocking, ProofMeansRead, ProvenIsSubscribed, RecordTrue, KeepsHook, HookStays,
    with seven reversed witnesses. Supersedes the v1.2.8 card the-coordinator-must-hook-in."
   :origin "the owner, 2026-10-10 ~22:55Z: about 1,200 pushes went unread for hours while the seat answered PROOF files by hand")

  (fix "provider-key-refusal-rests-until-woken" :release "v1.2.6" :status "shipped"
   :title "A provider that refuses its key rests until woken, never for a time"
   :text "A route whose provider refuses the key rests until routes wake, never for a time, and the rest
    reason reads until woken rather than until paid: the providers table, the stop and the judgment all
    say woken. TLA+ model RouteRest, property AuthEndsOnlyWoken, with a reversed witness. Tests cover
    routes wake on a key rest."
   :origin "nova-tools PR #5574, head 0c6706f2d; the providers table's until-paid wording is v1.2.5 candidate 6")
  (fix "bare-merge-stream-refused" :release "v1.2.6" :status "shipped"
   :title "A bare merge --stream is refused on a real store"
   :text "Landings are recorded by name or by land; a bare merge --stream on a real store is refused."
   :origin "nova-tools PR #5565, head 52618fa02")
  (fix "pin-release-and-rule-answers" :release "v1.2.6" :status "shipped"
   :title "A hard pin past its bound is offered on; a rule answer is consumed by its attempt"
   :text "A hard pin past its bound is offered to others, a card cannot ask once it waits, and a rule
    answer is consumed by its attempt. The pin release clock comment is corrected: the hour runs from
    the stop's latest note, so a coordinator's ack or wait starts it again, and a test says so."
   :origin "nova-tools PR #5573, head daab75763, re-applied here")
  (fix "paths-widening-edits-card-in-place" :release "v1.2.6" :status "shipped"
   :title "A PATHS widening on a reader finding edits the card in place"
   :text "A reader finding that widens PATHS edits the card in place, never a twin. Three review notes are
    fixed before merge: the finding reader is unset only when the card holds it, the bound subtest
    reaches the attempt cap, and the friend card claim says a widen does not pin her next attempt."
   :origin "nova-tools PR #5570, head 22d72154d, re-applied here")
  (fix "stop-return-settles-its-debt" :release "v1.2.6" :status "shipped"
   :title "A stop-return settles its STOP debt, so the owner's row can be held before START"
   :text "A same-owner stop-return takes its lease off the machine's STOP debt under the operation fence,
    a replay too, so a returned card no longer pins its owner's row: hold and fleet down may move it
    while STOPPED, and START is not refused. A lease without its receipt on its own row stays owed.
    TLA+ model StopReturn, properties NoLiveChildAcrossStart and ReturnedFreesItsRow, with two
    reversed witnesses."
   :origin "the seat, 2026-10-10: hold alex and hold emma refused after their stop-returns")

  (fix "a-refusal-always-says-why" :release "v1.2.6" :status "shipped"
   :title "Every refusal says why, and what to run instead, in the text output"
   :text "A step's refused card prints its reason and its remedy in the default text output, as --json's
    refused array has carried them all along: the verb result printer appends what changes the card, by
    the lifecycle's moves (docs/SPEC-SPRINT.md section 3), when the step's reason names no remedy of its
    own and is about the card's state, so return on a working card no longer reads RETURN FAILED
    moved=0 refused=1 alone. A refusal that is not about the card's state stands on its reason: a read
    of a primary is refused no such card, its why naming the read card to call
    (<primary>.r<attempt>.<reader>), and takes no work-table remedy. A test walks every verb's refusals
    and fails on any refusal printed without a reason, or a card's without a remedy."
   :origin "dogfood 2026-10-10: BRIEF FAILED and RETURN FAILED with no reason in text; the seat cold
    read of 2026-10-10 found that remedy appended to a read's no-such-card refusal")

  (fix "deal-draws-launchable-route" :release "v1.2.6" :status "in-progress"
   :title "A deal draws only a route its member can launch"
   :text "The deal, the redeal, the escalation and the rebalance onto a machine draw only routes whose
    harness the member can launch, and a move that keeps a card's route never goes to a member that
    cannot launch it (tla/RouteIndex.tla, NeverUnlaunchable). DEPLOY STEP for the final adopt: a member
    names no headless harness (claude, codex, grok) until `nova-sprint fleet up <member> --harnesses
    <h,...>` runs, so it is dealt opencode routes only; after the adopt, run it for each member with
    the headless harnesses on its PATH, or the deal draws no headless route for it."
   :origin "nova-tools PR #5576, head 065bf6b73, re-applied in nova-sprint")
  (fix "lander-bench-fault-and-bisect" :release "v1.2.6" :status "in-progress"
   :title "The lander blames no head for a bench fault and bisects a red batch"
   :text "A bench fault blames no head, a red batch is bisected, and landings go as batches are built. The
    bench fault match reads only the bench's own lines, never a test's output, so a real failure that
    prints a disk string is still blamed."
   :origin "nova-sprint PR #48, re-applying nova-tools PR #5579 (head 5bb7de4f2) with v1.2.5 candidate 4")
  (fix "check-waived-read-and-where-tiers" :release "v1.2.6" :status "shipped"
   :title "check honours a waived second read; where shows tiers once"
   :text "check judges an accept made before the second-read count by the count then; where shows a tier
    once and hides an expired until. A stale health seen time beside a live status is also fixed."
   :origin "nova-tools PR #5584, head 881df6ff")
  (fix "where-json-landed-series-fast" :release "v1.2.6" :status "shipped"
   :title "where --json reads no log line for its landed series"
   :text "The tick counts landings into the where record, so where --json answers in well under a second
    (8.9 to 12.8 s before, 0.4 to 0.7 s after, measured on a copy of the live store); released sentinels
    are not counted as landings. The public dashboard mirror's server.py (internal/sprintdash/stopgap)
    is now versioned here: it stamps fetchedAt with the snapshot's own time and adds dataAgeSeconds.
    AFTER THE DEPLOY, in the final adopt: rebuild the dashboard's nova-sprint-int2
    (~/nova-bench/dashboard/bin/nova-sprint-int2) from this release, copy
    internal/sprintdash/stopgap/server.py over ~/nova-bench/dashboard/server.py, and restart the
    public mirror unit."
   :origin "nova-tools PR #5588, head 08f6af5a8")

  (fix "operator-wording-bare-merge-and-key-rest" :release "v1.2.14" :status "planned"
   :title "What an operator reads matches the bare-merge refusal and the key rest"
   :text "A late-merge judgment prints land for a real store and the bare merge only as the twin's form; the
    CLI, first-lap and test docs mark the bare merge twin-only (play is a twin tool); the spec and comments
    say a key rest ends only by routes wake, never by funded or a payment, and a test shows a balance poll
    cannot end one."
   :origin "readers' notes on nova-sprint PR #49 and PR #50")
  (fix "empty-run-harness-fault" :release "v1.2.6" :status "in-progress"
   :title "An empty run is its own harness fault"
   :text "An empty run is classed as a harness fault, and a rework never returns to the friend AI whose
    lane ran it empty, with a TLA+ model and witness. The failed rule writes her on both its early
    returns too (the brief's bound and a brief defect), so the attempt that hits the cap after her
    empty run names her even when it is left to a mind; the attempt cap's deal acts only on a
    machine's card. Test: TestTheFailedRuleCarriesTheEmptyRunFriendOnItsEarlyReturns (reversed: a
    cost-line fault leaves no one)."
   :origin "nova-sprint #45; reader G BLOCK (the rule's early returns); nova-tools PR #5583, head e8fdaecc")
  (fix "done-at-base-is-brief-duplicate" :release "v1.2.6" :status "shipped"
   :title "A card already done at its base is a duplicate brief, not a worker failure"
   :text "Nothing to do because the base already contains the change is a brief defect, a duplicate of
    landed work, outside ok%, and raises a re-cut judgment."
   :origin "nova-tools PR #5585, head 0beb0508")

  (fix "lanes-ask-the-server-what-they-hold" :release "v1.2.6" :status "in-progress"
   :title "A worker's lane asks the server what it holds, and the server keeps it"
   :text "take --as <member> --lane <n> names the lane and nothing else: the server picks the epoch,
    answers the card the lane holds or gives it one (a friend's take is her start, so the tick no
    longer puts it back ready), and says it on a LANE line. Progress, finish and read verdicts name
    the lane, a lane unheard for ten minutes is gone and its card bounces back ready, a reader's
    lanes begin its reads, stop-return is served as a worker's verb, and the worker verb lists in
    serve.go and coordinator.go are unified (take, finish, progress, read, queue, stop-return,
    remind, fleet beat, friend beat, friend cards, lane take, lane give)."
   :origin "a one-shot friend host, 2026-10-10: took logged while the server showed the card ready, progress sent at epoch 0, every stop-return refused by the server; verbs misaligned in serve and coordinator")

  (fix "fleet-verbs-skip-member-under-disk-floor" :release "v1.2.6" :status "shipped"
   :title "Fleet verbs give a member under its disk floor nothing"
   :text "fleet up, down, hold and level, outside the tick, read the members' no-room words themselves,
    so a manual level, an up's level or a down's redeal gives a member under its disk floor nothing;
    the deal and read fix they follow already merged."
   :origin "nova-tools PR #5578, head fcdbaa441, re-applied in nova-sprint")

  (fix "draining-member-clears-no-room" :release "v1.2.6" :status "shipped"
   :title "A draining member does not keep a stale no-room word"
   :text "The room check runs before the drain return, so a draining member's beat carries this tick's
    no-room word, not a stale one (the member code in nova-tools, with its test, and the same lines in
    this repository's copy); the deal and read disk floor fix also gets its TLA+ reversed witness."
   :origin "v1.2.5 candidate list; nova-tools PR #5569 follow-up")

  (fix "bench-tree-standalone" :release "v1.2.6" :status "shipped"
   :title "A bench tree is a standalone clone, and one that is not is refused with its reason"
   :text "The mirror stage ends by checking that the staged tree's .git is a directory in the tree, and a
    .git file (a worktree pointer at a path of another machine) is refused naming the gitdir and whether
    it exists on the host. The tar copy refuses a worktree's .git file instead of carrying it over, so git
    no longer exits 128 on a fleet host with nothing saying why."
   :origin "lander fault 5579, space git exit 128 (held-PR ledger row 5579)")

  (fix "live-library-uses-store-login" :release "v1.2.6" :status "shipped"
   :title "live reads the store's function library through the seat login"
   :text "live runs nova-redis fn check with the recorded seat login's user and the password read in
    process, handing that password to the child's environment: on a store whose default user has NOAUTH
    the library state is now read (loaded, wanted, match), not UNKNOWN. The test observes the delivered
    password, so it is red until the child actually holds it."
   :origin "dogfood 2026-10-10: live LIBRARY state=UNKNOWN; card live-library-check-uses-the-store-loginb.w1")

  (fix "inbox-wait-survives-a-clear" :release "v1.2.14" :status "planned"
   :title "The seat's push loop goes on at the epoch a clear leaves it"
   :text "inbox --wait and --push re-read the sprint's active epoch when a clear advances it under the
    loop: the look at the inbox is never refused MEMBEREPOCH by the epoch the clear left, the wait
    ends at the clear as at a machine stop, the inbox reporting the clear's happened line, and the
    seat's push loop keeps running, pushing the new epoch's judgments. A step in flight still holds
    its epoch: a tick at a clear stops there."
   :origin "the seat ledger v1.2.4-held-2026-10-10.md, bug 6")

  (fix "frontier-read-judged-by-real-cause" :release "v1.2.6" :status "shipped"
   :title "A frontier read no one may take is judged by its real cause"
   :text "A frontier read the ask leaves waiting (the friend ask and the read-card ask alike) raises one
    judgment naming each such read and who is full, or why no one may take it. It is written once,
    rewritten in place when its facts change, never raised anew, and closed when the reads are asked."
   :origin "this pull request; frontier reader judgment")

  (fix "repair-says-what-it-changes" :release "v1.2.6" :status "shipped"
   :title "repair says what it changes, takes --dry-run, and returns a rule-6 merging card to review"
    :text "repair's help names its effect, it takes --dry-run that prints each move it would make (and leaves a
    pending operation open), and it refuses naming the remedy when it can do nothing. It learns check rule
    6: a card merging with ok reads from fewer different readers at its head than it needs goes back to
    review for its missing read, with the reason recorded, and never a card a lander marked landing (or
    pushed and not reported, or with a change queued for it): a landing once committed is completed, so
    repair skips it exactly as the tick's ShortReadsBack does."
   :origin "this pull request")

  (fix "clear-is-a-blank-slate" :release "v1.2.6" :status "shipped"
   :title "Clear starts a blank epoch and reports only live leases"
   :text "Clear lists every live card generation and owner row it refuses on; a returned or nonworking
    lease does not block. A successful clear advances to an empty epoch — no work, merge, stream or
    reader rows — but carries the fleet's members across: their rows, widths, status and holds survive
    (the machines are still there), while the old epoch's streams, cards and judgments reset."
   :origin "seat ledger v1.2.4-held-2026-10-10, bugs 2, 4 and 11")

  (fix "worker-brief-never-rewrites-history" :release "v1.2.6" :status "planned"
   :title "The worker brief says never amend, rebase or reset onto origin"
   :text "One line in the worker brief (the child rule no-rewrite-history, internal/fleetrules/child-rules.txt,
    byte for byte nova-tools' fleet/child-rules.txt, with a test pinning the digest in both repositories)
    stops children rewriting history, which caused the does-not-descend refusals."
   :origin "v1.2.5 candidate list")
  (fix "seat-proof-renews-while-the-seat-works" :release "v1.2.6" :status "in-progress"
   :title "A busy seat's own verb renews its stale push proof, and only its own"
   :text "A coordinator verb refused with PUSH DOWN for a proof past its 15m bound is answered by the
    seat's own session when the folder holds the check the push loop wrote but nothing answered: the verb
    answers the outstanding proof itself and records the pong (the way seat pong does), so a busy seat is
    never locked out; a seat with no outstanding check to answer, or truly unreachable, is still refused.
    The renewal is confined to the acting seat: a coordinator handover to a destination gates on the
    destination's proof with plain pushGate, so the acting session never answers the destination's
    outstanding check, and a destination whose session never answered is still refused."
   :origin "the seat, 2026-10-10: PUSH DOWN mid-add")

  (fix "working-is-a-live-run" :release "v1.2.6" :status "shipped"
   :title "Working means a live run, RUNNING or STOPPED; a returned card is no longer STOP-owned"
   :text "A worker's beat carries its live set (fleet beat and friend beat --live), and the tick returns a
    working card its row's live set has not named for 60 s to ready, while running and while stopped; the
    dashboard's working cell counts live runs. A stop-returned card is no longer STOP-owned, so a hold
    of its row is not refused before start, and a report refused for the stop is kept and taken after.
    Modelled in tla/LiveRuns.tla."
   :origin "the STOPPED sprint of 2026-10-10: 59 stale takes, and the hold of friend.zhi refused; nova-sprint PR #54")

  (fix "defer-cards-to-the-roadmap" :release "v1.2.6" :status "in-progress"
   :title "A verb defers waiting cards to the roadmap with their whole briefs"
   :text "nova-sprint defer writes the named waiting cards, or a stream or repository of them, into the
    roadmap data with each whole brief, and drops them from the store; --expect checks the count."
   :origin "this pull request")

  (fix "cut-sentinel-releases-itself" :release "v1.2.6" :status "shipped"
   :title "A cut sentinel releases itself on the tick, and pushes the seat that the release is ready to cut"
   :text "A sentinel that names its needs (a cut: a stream's reopen stop, a release sentinel) is released by
    the next tick's resolve when every card it needs has landed: released as the coordinator's release
    lands it, released_by the machine, what waited behind it to ready in the same step, its reached
    judgment closed, and one notice addressed to the seat that the release is ready to cut -- the publish
    itself stays a gated step with a cold read. A plain gate (a stop by position alone, no needs named)
    and a held sentinel keep the coordinator's release; the machine releases through no stream's hold and
    waives nothing in flight. The release lives in the resolve part, the duty the reference model names,
    so the tick's shape is the model's."
   :origin "the owner, 2026-10-10: what else is manual that should be automatic from the machine")

  (fix "promotion-mark-at-add" :release "v1.2.14" :status "planned"
   :title "add can mark a new stream for protected branches"
   :text "add --land-protected marks a new stream atomically with its first card, so a point-release
    stream can admit a card based on main or dev without a sentinel card or a follow-up stream set."
   :origin "the seat ledger bug 14")

  (fix "stream-remove-removes-what-exists" :release "v1.2.6" :status "in-progress"
   :title "stream remove takes every stream that exists and names each that does not"
   :text "One name that is no row of the work or merge table no longer refuses the whole batch: stream
    remove takes off every named stream that may leave, reports each name that may not, and exits 1
    only when nothing was removed or a removal failed."
   :origin "the seat ledger v1.2.4-held-2026-10-10, bug 12; card stream-remove-removes-what-exists")

  (fix "friend-dealt-cards-return" :release "v1.2.6" :status "shipped"
   :title "A friend's dealt card not taken returns to the pool; a friend that has not taken is dealt no more"
   :text "A card dealt to a friend and not taken within the dealt bound (dealt-max) is returned to the
    pool and dealt to the next capable member, and a friend that has not taken a dealt card is dealt
    no more until she takes one (a fleet property marks her, her start clears it). The tick's friend
    deal returns the untaken cards (friendDealtReturn, no redeal of its bound spent) before the level,
    so the idle fleet machines take the cards the friends' daemons could not, instead of the friends
    first deal stranding every card on friends that never start. tla/FriendDealtReturn.tla
    DealtNoMoreHolds, with the reversed witness MCFriendDealtReturnBrokenGate."
   :origin "dogfood 2026-10-10: the deal starved the fleet behind friends that could not take")

  (fix "friend-working-start-clears-dealt-no-more" :release "v1.2.6" :status "shipped"
   :title "A friend's working-card start clears the dealt-no-more mark, and a friend who took one is not marked"
   :text "The re-admission half of the dealt-no-more mark covers the tick's working-card start: a card a
    finish's next moved to working that she is working (friendStarted) is confirmed started by the tick's
    friendUnstartedWorking, which clears the mark as FriendStart and the ready-card start (friendStartUnits)
    do, and friendDealtReturn leaves alone a friend with such a working card, so a friend who has taken a
    card is never dealt no more. tla/FriendDealtReturn.tla Start and DealtNoMoreHolds, with the reversed
    witness MCFriendDealtReturnBrokenReturn."
   :origin "dogfood 2026-10-10: a friend who took a working card stayed dealt no more despite having taken one")

  (fix "friend-has-taken-one-predicate" :release "v1.2.6" :status "shipped"
   :title "A friend who has taken a card is never marked dealt no more, by one predicate"
   :text "The return's leave-alone gate and every clear of the dealt-no-more mark read the one predicate
    friendHasTaken (her beat names a job running, or any card on her row, ready or working, is started or
    carries her progress), so a ready card carrying progress that the tick promotes to working this same
    tick is a start: the friend is not marked dealt no more, and her untaken cards are not returned, in
    the same tick she took one. friendDealPass, friendUnstartedWorking and FriendStart each clear the mark
    through the one helper. tla/FriendDealtReturn.tla Return guards on no card of hers in started, with
    Start and Progress its start paths, and the reversed witness MCFriendDealtReturnBrokenReady."
   :origin "dogfood 2026-10-10; seat rebrief 2026-10-11: a ready card carrying progress was invisible to the return's gate")

  (fix "land-merges-the-roadmap-data" :release "v1.2.6" :status "shipped"
   :title "Land merges the roadmap data, not returns the card"
   :text "A head whose conflict is in docs/fixes.sexp or docs/roadmap.sexp, with or without FIXES.md and
    ROADMAP.md, lands: the data is merged entry by entry (each side only adding or only removing whole
    lines in each conflicted stretch, the tip's additions first), checked by decoding all three sides
    and the result, and each page is taken from the tip and regenerated from the merged data as `make
    roadmap` does. Both sides changing one entry is refused as before. Also: main was red on its class
    gofmt from PR #67 on, so the lander merged no head onto it (PR #74)."
   :origin "the Studio sprint, epoch 16, 2026-10-10: merging stood at 15 to 21 cards with none landed in 30
    minutes, every second card refused on FIXES.md and docs/fixes.sexp and reworked at the tip")

   (fix "dashboard-empty-and-stopped-states" :release "v1.2.6" :status "shipped"
    :title "Cost breakdown empty state, landed tile text, and stopped dot red"
    :text "When the epoch has no spend, the cost breakdown table keeps its header and shows three empty rows with \"-\" in each cell, instead of showing prose. When landed is 0 of 0, the landed tile reads \"nothing complete\" instead of \"- complete\". When the machine is STOPPED, the Updated clock dot is red (the stopped class) instead of green (the ok class). The fix preserves the stopped state across the setLive call that updates the clock."
    :origin "seat fix, 2026-10-10")
   (fix "land-refused-head-never-reoffered" :release "v1.2.6" :status "shipped"
   :title "A head the landing refused is never offered to accept again; a ledger conflict is no brief defect"
   :text "A card whose head the landing refused keeps that head (land_refused_head), and while it is still
    its head no step raises ready to accept on it: at the brief's bound the card is in review with the
    bound's judgment alone, and with nothing open it is stranded (rework or drop), never offered the
    accept that queues the refused head again. A conflict only in the shared ledgers (the roadmap's data
    and pages, the class ledgers, the AGENTS maps, the keyed TLA+ tables, the tables lock) is the tip
    moving under the card: reworked at the tip every time, never counted toward the brief's bound, so
    never marked a brief defect, and its finding seeds no bound for a later file conflict."
   :origin "the Studio sprint, epoch 16, 2026-10-11 00:07-00:20Z: about 30 cards across v126-land, v126-fix,
    v128 and v129 looped accept, refused on FIXES.md and docs/fixes.sexp, ready to accept, accept; rework
    refused as a brief defect (land-ns56-backup-live-beats)")

  (fix "backup-file-needs-no-lastsave" :release "v1.2.6" :status "planned"
   :title "backup --file and snapshot do not need LASTSAVE"
   :text "The Redis snapshot source detects a completed BGSAVE from rdb_saves (or rdb_last_save_time) in
    INFO persistence, so the coordinator ACL user, which may not run LASTSAVE, can take the backup. The
    BGSAVE-failure and timeout refusals are unchanged."
   :origin "pre-clear backup, 2026-10-10 (NOPERM on LASTSAVE)")

  (fix "backup-out-restore-check-live-fleet" :release "v1.2.14" :status "planned"
   :title "backup --out passes on a fleet that is beating"
   :text "The restore check sets aside what a live member's beat rewrites while the dump is taken (the
    fleet table's revision, a fleet row's place, load text and up or down status) and compares
    everything else exactly, so a running fleet no longer fails the check; a held status, a missing
    row or a changed card still does."
   :origin "pre-clear backup, 2026-10-10 (8 fleet parts differed)")

  (fix "backup-restores-the-fleet-rows" :release "v1.2.6" :status "planned"
   :title "backup --file passes on a fleet that is beating"
   :text "The restore check of backup --file sets aside what a live member's beat rewrites while the dump
    is taken (the fleet table's revision, a fleet row's place, load text and up or down status) and
    compares everything else exactly, so the pre-adopt backup of a running fleet no longer refuses with
    the dump does not restore the sprint the store held; a held status, a missing row or a changed card
    still does. store.SemanticRestore (internal/sprint/store/restore.go) is the check, run from runBackup
    (cmd/nova-sprint/backup.go)."
   :origin "dogfood 2026-10-10 evening: fleet-only restart; pre-clear backup, 2026-10-10 (8 fleet parts differed)")

   (fix "inbox-push-write-once-race" :release "v1.2.11" :status "planned"
    :title "Inbox push cannot overwrite a published judgment"
    :text "Two overlapping clients share one temporary path, so a second write can change the published
     file. Use a unique temporary path per writer."
    :origin "issue #5160")

   (fix "seat-judgment-repush" :release "v1.2.9" :status "planned"
    :title "Judgments waiting on the seat are re-pushed each look"
    :text "The seat push loop was only pushing judgments once because it cached the 'seen' file list
     for all directories. When following the seat, cached seen prevents re-pushing judgments that are
     still waiting on the seat. The fix modifies pushTarget to not cache seen for seat directories
     (only for fixed directories), so judgments are re-pushed on every look while unanswered."
    :origin "seat-only restart, 2026-10-10: fleet at 18 of 76, ten cards waiting on seat's judgment")

   (fix "rest-properties-cap" :release "v1.2.11" :status "planned"
    :title "Rested routes must not overflow the fleet table property cap"
    :text "Rule 3 writes one property per rested route, so the cap is reached at about 58 routes and the
     tick writes nothing. Use one property for all."
    :origin "issue #5210")

   (fix "tick-under-its-gate-at-load" :release "v1.2.9" :status "planned"
    :title "The deal builds no per-card route map, so the tick stays under its gate"
    :text "Drain and deal take 0.4 to 0.7 seconds at load, so the tick is over its one-second gate, and the
     dominant cost is the deal's routeOf, which built a served map (route.go, served[r.Name] = r) and a
     set of route fields (the map literal and set[FieldTierNow]) for every ready primary each tick, and
     again per up member in the launch check (launchersOf): 65% of the deal's alloc_space, which the GC
     spent near half the deal on (gcDrain 44% of cpu). The model read the deal also paid (cardhdr.ReadModel's
     regexp over line 1) is a minor share (4.7%). routeOf now builds served as an on-demand scan over the
     store's few routes (servedRoute) instead of a map, and the tick's per-card check asks only whether a
     route serves, never its fields (routeServed, called by the machines' pass, launchersOf and noRoute),
     so it builds no set; only the callers that deal (deal, redeal, the rebalance) build the fields, at most
     the width's cards a tick. The model read is also memoized once per brief (modelOf), wired through the
     call sites the tick reaches (friend_deal.go, friend_read.go, readers.go, lane_cap.go). On the loaded
     store of 80,000 ready beside 80,000 done and 8 friends the steady deal drops about 0.64s to 0.29s on
     this bench, both under the one-second gate, and TestTickDrainAndDealUnderTheGateAtLoad shows the deal
     stays under the gate; TestTickDealAllocatesNoPerCardRouteMapAtScale holds the removed allocations with
     a count and no clock (near 57 a ready card to near 23, so it fails on the unfixed code and passes
     after), BenchmarkTickDealAtLoad measures the deal and BenchmarkTickDrainAtLoad the drain, and
     TestTheTickDealReadsEachBriefOnceAtScale holds the parse to one a brief."
    :origin "seat ledger v1.2.4-held-2026-10-10.md 5577 tick gate")

   (fix "hold-pinned-to-head-sha" :release "v1.2.9" :status "shipped"
    :title "A hold pinned to an older head is not a hold at the current head"
    :text "The lander ignores a hold whose sha is not the PR head when a later line at the head clears it."
    :origin "issue #2710")

   (fix "merge-approval-head-from-disposition" :release "v1.2.9" :status "planned"
    :title "Approval at head is read from the disposition line"
    :text "The review API commit id can differ from the head the reviewer read. Merge checks parse the head
     named in the disposition and treat commit id as untrusted."
    :origin "issue #2037")

   (fix "log-since-wide-window" :release "v1.2.9" :status "planned"
    :title "Fix log --since for windows wider than 22 hours"
    :text "A wide --since window returns nothing; pin the window and return every event in it. Did not
     reproduce at 6f3323a6c83f9e7b340bf477886862c39001315e."
    :origin "card moved out of the sprint (work record, 2026-10-04); card from the sprint store (2026-10-10)")

   (fix "fleet-probe-loaded-bench-not-down" :release "v1.2.9" :status "shipped"
    :title "A loaded bench must not be marked down by the fleet probe"
    :text "The fleet-state probe marks a heavily loaded but healthy bench down after a few missed probes,
     and the dealer then kills its loops and sweeps its queue. Define state-entry timing and tell loaded
     from down."
    :origin "issue #2161")

   (fix "bound-judgment-names-its-remedy" :release "v1.2.6" :status "shipped"
    :title "A bound judgment names the exact command that answers it"
    :text "A card at its redeal bound says which tier above to rework on and the drop command whole, and a
     card at its brief's bound names brief --brief-file and the drop command, in the judgment's own text as
     a rework's refusal already said, so the coordinator runs the remedy as printed instead of guessing."
    :origin "dogfood 2026-10-10 evening: a fleet-only restart bounded several cards, and answering them took
     four tries -- brief --tier, redo, rework, rework --group")

   (fix "overload-alarm-covers-friends" :release "v1.2.6" :status "shipped"
    :title "The overload alarm covers friends too"
    :text "A friend up whose cards keep ending on a timeout raises the overload judgment as a member
     does: three timeouts within OverloadWindow on her row friend.<name>, counted after her finishes'
     `friend <name> <VERDICT>: ` prefix, her width the roster's. Its remedy is her width in
     nova-config, applied by friend sync."
    :origin "sprint card overload-alarm-covers-friends")

   (fix "restore-tests-owed-from-split" :release "v1.2.6" :status "in-progress"
    :title "Restore the tests the repository split moved out of nova-tools"
    :text "The four doc-pin tests moved out of nova-tools restored in nova-sprint, each wired to the behavior
     it pins: the coordinator rules (cmd/nova-sprint/handover_rules_test.go,
     TestEveryCoordinatorRuleNamesVerbsThatExist, holds each rule of docs/SPRINT-COORDINATOR.md section 10 to
     nova-sprint's verb table and the verbs of the tools the runbook names), the processor doc
     (cmd/nova-sprint/processor_doc_test.go, pinned to docs/PROCESSOR.md's mapping rows), and the coordinator
     tools register and tool class (cmd/nova-sprint/coordinator_tools_test.go,
     TestEveryCoordinatorToolMapsToARealNovaVerb and TestEveryCoordinatorStopgapNamesTheVerbThatReplacesIt,
     pinned to docs/COORDINATOR-TOOLS.md's table). The handover now prints every rule by number
     (cmd/nova-sprint/handover_rules.go, wired in seat.go, so TestHandoverPrintsEachRuleByNumber exercises
     the real path; docs/SPEC-SPRINT.md's handover note names the numbered rules, not the old single waves
     RULE line). The tick wall-clock gate job is restored too (.github/workflows/ci.yml, tick-gate: the dirty
     tick drive against a real store with NOVA_SPRINT_GATE_STORE=1, every tick held to MaxTickWall). The seat
     play test, the member and contract functional tests and the eight contract cases exercise nova-tools
     code and stay in nova-tools; they are still owed there."
    :origin "repository split cold reads (nova-tools PR #5571 and PR #5591); this card")

   (fix "doctor-checks-the-definition-of-done" :release "v1.2.7" :status "shipped"
    :title "The doctor names every definition-of-done fault with its evidence"
    :text "The read-only doctor joins the seat check's centralized machinery verdicts (the friends and
     the fleet up, the loop ticking, the dashboard answering, and a dashboard whose served snapshot
     is a second or more old) with the coordinator's automatic-stop ledger, section 9's always-true
     rules, the deal dry with cards waiting and the seat's overdue waits, and prints one
     evidence-bearing line per fault. It exits 0 when clean and 1 when red, and --json carries the
     fault lines and the machinery evidence with the push proof redacted. Three faults the card
     names are held for the dependencies that decide them, not judged here: working equals reality
     (a card working whose row's beat names no live run) waits on land-ns54-working-is-live, whose
     beat live set (RowWorkingKeys/LiveCountKeys) is not landed on main; the members' and friends'
     at-width facts are that dependency's width fact, the seat check's fleet and friends lines
     counting up/held/down alone; and the machine's own tick late (pushlate.go) is not joined, the
     loop line holding only the LoopSilence bound."
    :origin "this pull request")

   (fix "friend-card-carries-own-rules" :release "v1.2.6" :status "shipped"
    :title "A friend card that carries its own RULES line is accepted at add"
    :text "A friend is not a child: her card is not held for the child RULES paragraph, the rule- presence
     checks of the card lint, when it carries its own RULES line, and a friend card with none is still
     held to it, as a child's brief is. The step- scans hold a friend's card still: a line of it outside
     its RULES paragraph that contradicts its carried rules is a finding under any rule set."
    :origin "this pull request")

   (fix "per-landed-spend-on-landed" :release "v1.2.7" :status "shipped"
    :title "Per-landed cost uses spend on landed cards only; in-flight shown separately"
    :text "The per-landed figure now divides total spend on landed cards (including failed attempts
     and reworks) by the landed count, rather than dividing all spend so far by landed count.
     In-flight spend on cards in waiting, placed, or merging is shown separately, so the total
     is still visible and nothing is hidden. Stream table and sprint totals now agree."
    :origin "dogfood 2026-10-10: $7.74 per landed at 6 of 82; stream table $0.03 vs total $46.39")

   (fix "swarm-lint-card-false-refusals" :release "v1.2.14" :status "planned"
    :title "Card lint refuses valid cards: make gates and text inside fenced blocks"
    :text "Card lint rejects make-driven gates and flags parent paths or absolute paths quoted inside
     fenced blocks. It should accept both."
    :origin "issue #1994; issue #2302; issue #3470")

   (fix "card-column-single-field" :release "v1.2.14" :status "planned"
    :title "One column field every reader agrees on"
    :text "Card output carries a single column field so a card's state is not confused with the state of
     its need."
    :origin "card moved out of the sprint (work record, 2026-10-04)")

   (fix "reader-only-friend-no-work" :release "v1.2.9" :status "shipped"
    :title "A reader-only friend is dealt no work card; a builder-only friend is asked no read"
    :text "The deal and the ask honour a friend's roles: a friend whose nova-config row's roles do not
     name builder (RoleBuilder) is dealt no work card, and one whose roles do not name reader (RoleReader)
     is asked no read, in the read-card ask and the friends' ask alike. A row with no role list is a row
     from before roles and keeps the pre-role behavior: it is dealt work and asked reads (friendHasRole).
     The dogfood of 2026-10-10 evening: a reader-only friend was dealt 16 work cards once it came up,
     and builder-only friends were asked reads."
    :origin "dogfood 2026-10-10 evening: fleet-only restart")

   (fix "stats-tidy-large-fleet" :release "v1.2.11" :status "planned"
    :title "A stats tidy succeeds over a fleet of more than 64 rows"
    :text "Tidying stats works when the fleet table has more than sixty-four rows, instead of hitting a
     property bound."
    :origin "card from the sprint store (2026-10-10)")
   (fix "a-joined-reader-gets-the-fleet-reader-tiers" :release "v1.2.11" :status "planned"
    :title "Fleet readers inherit common tier when added without --tiers"
    :text "A fleet reader added without --tiers now inherits the common tier when all other fleet readers
     agree; otherwise stores empty tier with from=default message. Doctor warns about fleet readers whose
     tiers differ from the fleet's common tier."
    :origin "a-joined-reader-gets-the-fleet-reader-tiers.w5.g4.e17 (2026-10-11)")

   (fix "fleet-sync-retires-a-renamed-machines-row" :release "v1.2.6" :status "shipped"
    :title "The tick retires a fleet row whose machine is gone from the inventory"
    :text "A rename (hetzner -> hetzner1) left the old fleet row held on the table forever, because
     nothing ever removed a row: fleet sync adds and resizes only. The tick now reads the same machine
     records fleet sync reads (nova-config, no new store) and, every tick and with no verb run by anyone,
     retires a row whose record is absent (sprint.TickRetireAbsent, internal/sprint/fleet_retire.go): a row
     with no record and no cards leaves the fleet table and the dashboard, its reader reader-<old> is
     retired with the row and only once it leaves (the rows store.DropMembers deleted that tick), its stats
     stay queryable under the old name, and one HAPPENED note records it; a row with no record that still
     holds cards is held and its cards dealt away, retired on the first tick its set is empty; a config read
     that fails retires nothing (never act on a missing read); a machine whose record exists but is offline
     stays down. fleet sync --check already lists what the next tick would retire, as its remove drift for
     an absent member with no card. Model tla/RetireAbsent.tla: RecordsKeepRows is the state invariant (a
     recorded machine keeps its row) and FleetRowsMatchConfig and LiveRetire are the leads-to the tick's
     fair reads and retirement make true; two reversed witnesses are declared, MCRetireAbsentBroken retiring
     a recorded row on a failed read as the counterexample to RecordsKeepRows. The TLC pass is recorded in
     tla/RUNS.tsv (MCRetireAbsent.cfg 2563 states, 468 distinct, exit 0; MCRetireAbsentBroken.cfg exit 12
     on RecordsKeepRows). Tests: a record removed, a tick, and the row and reader are gone while the stats
     stay; a row with working cards drains then retires; a draining absent row keeps its reader until its
     row leaves; a failed read retires nothing; an offline machine with a record is untouched."
    :origin "the owner, 2026-10-11 ~01:00Z and ~01:05Z, after hetzner was renamed hetzner1 and its old fleet
     row stayed held; this card; attempt 2")

    (fix "worklang-done-field-parsed" :release "v1.2.11" :status "planned"
     :title "The worklang parser keeps the done field of a unit"
     :text "Unit done reads a field the parser never keeps, so a done unit is never counted. The key is
      added and sibling skipped keys are checked, with a test."
     :origin "issue #3499")

   (fix "release-never-archives-a-stream" :release "v1.2.11" :status "planned"
    :title "The tick never archives streams; stream archive is the only way"
    :text "Streams leave the work and merge tables only through the explicit nova-sprint stream archive
     command. The tick no longer archives streams whose every card lands, which took them off the
     table and dashboard without anyone asking. keepArchive only draws again an archived stream that
     takes a new card."
    :origin "the owner, 2026-10-11 ~04:05Z; this card")

   (fix "brief-file-over-the-bound-is-refused" :release "v1.2.11" :status "planned"
    :title "Brief over the bound is refused with exit 2"
    :text "brief <id> --brief-file and brief <id> --brief <text> had no size check before the lint. Add a
     helper that checks the bound with the same message as brief --dir, used by add, brief --dir and
     these two forms. On a RUNNING machine, queued changes are now checked at the step before logging."
    :origin "epoch 16 stall, 2026-10-11 04:17:19Z to 04:54:23Z")

   (fix "add-refuses-card-off-sprint-base" :release "v1.2.6" :status "shipped"
   :title "add and brief refuse a card cut off the sprint's base"
   :text "Once the coordinator records the sprint's base (set --base, the work table's sprint_base), add and
    brief refuse a card whose BASE is any other branch outside the promotion stream, exit 2, nothing
    written; the step holds the same rule against a racing write. With no base recorded, dev alone is
    refused as before, and the promotion stream keeps its mark."
   :origin "the one-base rule; found 2026-10-04, cards folded back onto the base by hand through 17 conflicts")

  (fix "held-friend-reader-serves-nothing" :release "v1.2.6" :status "shipped"
   :title "A held friend's reader serves nothing"
   :text "docs/SPEC-SPRINT.md section 11 (hold): a friend and her reader row are one member to the
    server. A held friend's reader row (reader-<friend>) serves nothing: ownModelReader and
    readerServesTier both say so, no route judgment counts her up, and the TLA+ model ReaderTiers
    carries the held reader beside its reversed witness. A hold with --return also takes back the
    reads asked of her reader row, where a reader up is free to read them."
   :origin "dogfood 2026-10-10 evening: fleet-only restart")

  (fix "brief-on-a-working-card" :release "v1.2.6" :status "shipped"
   :title "A brief replaces a working card's in place, for its next attempt"
   :text "A card working takes a new brief in place, as one in review does (briefKept, docs/SPEC-SPRINT.md,
    the brief verb): the brief is stored on the same card, its id, stream, score and needs kept and no
    twin cut, and the record and the card's next attempt's why read <id> brief edited in place by <actor>
    at attempt <n>: <what changed>, so the coordinator corrects a running card without waiting for
    review. Only a merging or landed card keeps its brief, with what changes it instead (briefStarted).
    The verb's help and docs/SPEC-SPRINT.md say so."
   :origin "the owner, 2026-10-10: what else is manual that should be automatic from the machine; this card")
  (fix "personal-name-check-exempts-paths" :release "v1.2.6" :status "shipped"
   :title "The personal-name check exempts names inside file paths"
   :text "The card's personal-name check refuses a brief whose SOURCE or PATHS line contains a name of the
    deployment outside a double-quoted span. A name inside a file path (a whitespace- or comma-separated
    token containing a slash or ending in .md) is fine; only names in prose are refused. Before this fix,
    a name in a path triggered the check, so a brief like 'See /home/ada/projects/' was refused, and a
    line mixing a path with prose on the same line was incorrectly allowed."
   :origin "dogfood 2026-10-10: add refused briefs over a path segment")

  (fix "nova-work-roadmap-verbs" :release "v1.2.6" :status "planned"
   :title "nova-work roadmap check, list, add, remove, pull, done, note and render"
   :text "Verbs over the roadmap s-expression files with byte-stable round trips; each writing verb
    refuses a file that check rejects. Pulled into v1.2.6 by its own verb: the verbs edit docs/roadmap.sexp and docs/fixes.sexp and write both pages, locally, with no GitHub call per entry."
   :origin "nova-sprint commit 8c87a4b (not carried by the re-seed, nova-sprint PR #28)"
   :kept (:item-group "sprint"
          :item-date "2026-10-10"
          :item-release "after v1.4"
           :earlier-title "nova-work roadmap check, add, remove, pull and note"))

   (fix "machine-fault-hold-clears-when-healthy" :release "v1.2.9" :status "shipped"
    :title "A machine fault hold clears itself once the member is healthy"
    :text "A hold the machine places for a failure (a harness fault, a failed take, or the member going
     down) is marked held_by=fault and clears itself on the tick once the member beats and has finished a
     card cleanly since the hold: the machine's down is its fault hold, presence writes the mark and its
     stamp, and the tick lifts the mark on the clean finish, telling the seat. A hold a person made carries
     no held_by mark and is never lifted by the machine. TLA+ model DealFill, properties FaultHoldClears
     and PersonHoldStays, with two reversed witnesses."
    :origin "the owner, 2026-10-10: what else is manual that should be automatic from the machine")

  (fix "broken-verdict-always-records" :release "v1.2.6" :status "shipped"
   :title "A broken verdict at the same head always records"
   :text "Every reader, the coordinator included, records broken on a card in review at any bound: a
    broken verdict at the same head outweighs any number of oks, so accept never queues the head a
    reader found broken, ask --another never answers it, and the card goes back to work with the
    finding (reworked or widened in place) rather than accepted on a fleet ok. In tla/SprintRules.tla
    the read machine's fixed findings (rdfixed) bound the new rework at the brief's bound, and the
    reversed witness MCSprintRulesReadsBrokenBoundInside leaves an inside finding at the bound,
    violating ReworkAtBound."
   :origin "dogfood 2026-10-10 evening: fleet-only restart")

  (fix "down-member-is-down-not-fault" :release "v1.2.6" :status "planned"
   :title "A member whose host is offline is shown down, not as a fault"
   :text "The seat check reports a member that does not beat on an offline host as down (offline=name, the
    fleet line stays OK); DOWN when the host answers, or gives no clear no-answer (a name that does not resolve
    is named on the line). The seat probes the ssh port of each such member's machine row at once under one
    3 s bound, with a beating member other than the seat's own machine as the control of its network: no
    control answer, no member excused."
   :origin "v1.2.5 candidate list")

  (fix "readers-behind-judgment-once" :release "v1.2.9" :status "planned"
   :title "A standing judgment is one judgment, pushed once while it holds"
   :text "A standing condition is one judgment, keyed by its type and subject, rewritten in place while
    it holds and pushed again only when its own numbers move. The readers-are-behind judgment is keyed
    by type and subject (condKey, NReadersBehind), but its rewrite test compared the whole line,
    review's count included, and review moves every tick as cards flow through it: the tick rewrote and
    pushed the one judgment every tick while the readers' numbers held (dogfood 2026-10-10: j407, j409,
    j410 within a minute). notify now compares a material: for the readers behind, the readers' own
    numbers, never review's count (MaterialWhat, readers_behind.go), so the judgment is pushed once when
    it rises, again when the readers move, and not for review."
   :origin "dogfood 2026-10-10 evening: fleet-only restart; j407, j409, j410 within a minute")

  (fix "server-build-descends-from-base" :release "v1.2.6" :status "in-progress"
   :title "A sprint server build must descend from the origin base"
   :text "server switch refuses a build that does not descend from the origin base, so an off-base build
    cannot become the server."
   :origin "nova-sprint commit 9338f08 (not carried by the re-seed, nova-sprint PR #28)")
   (fix "widen-loop-capped" :release "v1.2.6" :status "shipped"
    :title "The widen loop is capped: every widen counts toward the brief's bound"
    :text "A card whose heads keep naming files outside its PATHS was widened and redealt without end: the
     E12 branch of the merge step skipped the brief's bound, and the widen rule's brief edit reset the
     attempts the bound counts. Now one widen count (FieldWidens, shared by the read-broken and widen
     rules) caps every widen, and the merge step checks the brief's bound before its E12 branch, so a
     card widened MaxWidens (3) times, or refused for files outside PATHS at its brief's bound, goes back
     to review as the brief is wrong, a mind's, never widened again. TLA+ model Land, properties
     AttemptsBounded, SameRefusalHalts and Settles, with the reversed witness ReachWidenLands."
    :origin "nova-sprint #64; the seat ledger v1.2.4-held-2026-10-10.md (the ejection-bound audit of 2026-10-10)")
  (fix "stop-debt-settles-from-the-members-beat" :release "v1.2.6" :status "shipped"
   :title "STOP debt settles from the owner's beat, so START never waits on its receipt"
   :text "After a STOP the server settles each owed lease whose owner has beaten since the STOP and whose
    beat no longer names the job, returning the card with a recorded reason, so START never waits on the
    owner's own stop-return receipt. A friend's beat names the jobs she runs: a lease whose job it still
    names stays owed, one it no longer names is settled. A machine's beat names no job; its stop-returns
    count is its word how many its lanes still owe, so a machine lease (a reader machine's too) is settled
    at zero and stays owed above zero. An owner that has not beaten since the STOP is reported to the seat
    in a judgment. TLA+ model StopReturn, property AbsentJobSettles, with the reversed witness
    nosettlebybeat; the model's Beat is drop-only while STOPPED, so a job a stopped owner's beat shows gone
    stays gone."
   :origin "the seat, 2026-10-10: START refused on 33 fleet jobs whose members never sent stop-return receipts")
  (fix "a-card-ready-over-a-minute-pushes-the-seat" :release "v1.2.6" :status "planned"
   :title "A ready card that sits past a minute pushes the seat"
   :text "A card that sits in ready past the ready bound (the sprint setting ready_max, default one
    minute) raises one judgment per (reason, tier), with the reason the deal leaves it: the tier, each route
    that serves no card and why (disabled, resting until paid, resting until woken, out of budget), no member up
    with free width to launch a route of its tier (fleet up), a hold, or a sentinel, and the remedy verb
    (routes wake, funded, fleet up, release). Cards that share a cause are one alert, never one per card, and it
    clears itself the tick the card is dealt. The rule is tla/ReadyOver.tla."
   :origin "card a-card-ready-over-a-minute-pushes-the-seatb.w8")
  (fix "a-member-environment-fault-holds-it-and-pushes-the-seat" :release "v1.2.6" :status "planned"
   :title "A member's environment fault holds it and pushes the seat"
   :text "A finish failure whose cause is the member's environment -- a push refusal (no credential, auth,
    unreachable remote), a missing tool or harness, a sandbox/wall refusal, a disk-full, or any class the
    member reports as environment -- is the member's, never the card's or the model's. The first one
    returns the card to ready untouched (the work card withdraws without an ended take, so no redeal is
    spent, the attempt is unchanged, and it is never failed work), holds the member so the deal gives it
    no new card, and raises one judgment pushed to the seat for the member, kept naming its current cause
    and its remedy (one per member and cause: a later fault with a different cause is surfaced, never
    left as the first cause's stale remedy; for a push refusal the remedy is the fleet push-credential
    play with --limit <member>). One judgment per member and cause holds within a finish step too: two of
    one member's cards finishing in one multi-card finish call (one report, so one cause) raise one
    judgment and write the member's control card once, not once per card, since the plan the model admits
    changes a control card once a step. An environment fault records itself beside a hold already on the
    member (a coordinator's, the machine's fault hold, the sync's): its clear -- a push, or fleet up's
    passed probe -- releases only the machine's own environment hold (held_by=environment), so a person's
    hold, its reason and its finish marker stay until a person lifts it. The fault clears when the
    member's next card pushes, or when fleet up runs the member's own probe (the push-credential probe,
    the harness check) and it passes; fleet up refuses a member whose last environment fault is not
    cleared, and on a new member runs its own probe before accepting width (init bootstraps by passing
    the probe). Modeled by tla/ProviderBudget.tla."
   :origin "sprint card a-member-environment-fault-holds-it-and-pushes-the-seat; the owner, 2026-10-11Z:
    the failure here is not noticing this until now; attempts 3 and 5")
  (fix "ok-percent-counts-reader-verdicts" :release "v1.2.6" :status "in-progress"
   :title "ok% counts the readers' verdicts; a friend's card past its deadline is a redeal"
   :text "A worker's finish, ok or failed, moves its work card to the fleet's hidden finished cell, where
    it counts in no ok%; the readers' verdict on the attempt moves it on, to ok when as many different
    readers as the primary needs read it ok, to failed when a reader read it broken, and work no reader
    reads stays finished. A friend's card past its deadline unfinished is redealt, counted in the shown
    redealt column on her row, never a failure and never a late judgment. The fleet table gains the
    finished and redealt columns, the friends table the redealt column, and a migration on a stopped
    machine takes the live store to the locked shape (migrations/0001_fleet_finished_redealt.sh)."
   :origin "the owner, 2026-10-04: trust but VERIFY; I want to trust the ok%")
  (fix "sentinels-are-not-work-in-eta-or-throughput" :release "v1.2.6" :status "shipped"
   :title "A sentinel is not work: it leaves the ETA, the landing rate and the summary counts"
   :text "A sentinel (sprint.IsSentinel) is a stop the coordinator releases, not work, so the sprint
    line's x/y and percent, where --json's landed and all (and archived_cards and archived_landed), the
    ETA, its five-landed floor and the landing rate count work cards only, across the epoch's streams on
    the table and archived, and never a sentinel's released stamp (docs/SPEC-SPRINT.md sections 1 and
    16). The work table's cells still count sentinels. The where record carries each stream's sentinels,
    all and landed, counted from the cards the tick already reads, so where reads no card for them;
    sprintLine agrees with where. Releasing a sentinel never archives its stream: the tick's archive part
    leaves a stream holding a sentinel drawn, and a stream is archived only by an explicit stream
    archive. The Go dashboard port (branch dashboard-is-the-live-page) must carry the same exclusion in
    its /api/sprint."
   :origin "the owner, 2026-10-11 ~03:55Z: sentinel cards should not be counted when estimating time
    remaining or cards per hour; the seat, ~04:00Z: releasing cut sentinels made the tick archive their
    streams")
  (fix "tlc-records-match-their-models" :release "v1.2.6" :status "planned"
   :title "TLC records match their models"
   :text "TLC records refreshed for 44 stale groups and plan rows added for MCProviderBudget and MCReadyOver; cardmachine still owed"
   :origin "sprint card tlc-records-match-their-models; the seat ledger v1.2.4-held-2026-10-10.md RUNS.tsv drift")

  (fix "low-balance-pushes-the-seat-before-the-refusalc" :release "v1.2.6" :status "planned"
   :title "A provider's low balance is raised before a refusal; a credit refusal spends no redeal"
   :text "The seat hears a provider is running dry before a refusal costs a card. The balance poll
    reads DeepSeek's balance (GET /user/balance with the key as a bearer token from DEEPSEEK_API_KEY,
    the USD entry's total_balance; an answer with no USD entry is unknown, never a currency
    conversion) beside OpenRouter's, so a provider the poll could not see is no longer unknown. Low
    is true at or under the low_balance_usd threshold (a sprint row setting, default 5 dollars) as
    well as at or under zero or under an hour's spend, and raises the one NProviderLow judgment per
    provider, its words leading with fund <provider> and its decisions naming funded <provider>; its
    routes STILL SERVE, as the owner ruled on 2026-10-10: a balance raises a thing to do, it never
    rests a route by itself. A take the provider refused for credit (class out-of-credit, a 402) on
    a limited route still rests the provider exactly as before, but returns the card through the
    same no-spend path a rate limit uses: withdrawn without FieldTakeEnded, so no redeal and no
    attempt is spent and no failed-work judgment is raised, the refusal still recorded so the
    provider rests; a 401 keeps the path that ends the take. TLA+ model RouteRest, property
    OutOfCreditSpendsNoRedeal (a credit event leaves the redeal count unchanged), with the reversed
    witness MCRouteRestBrokenBalance still failing. Tests TestReadDeepSeekBalance,
    TestALowBalanceAtTheThresholdRaisesOneJudgment and TestAnOutOfCreditTakeSpendsNoRedeal. The run
    loop's nova-secrets exec --only list must include DEEPSEEK_API_KEY for the poll to read it."
   :origin "the owner, 2026-10-11 ~00:20Z, after the fleet stalled (35 ready, 7 up, 0 working, every pro/heavy route resting): \"How can we fix it so this doesn't happen moving forward\"; nova-sprint docs/fixes.sexp entry low-balance-pushes-the-seat-before-the-refusalc")

  (fix "v1-2-12-inventory" :release "v1.2.12" :status "planned"
   :title "Inventory the tests and models before v1.2.12's first test card"
   :text "Coverage per package, the functional tier's state, each TLA+ model against the code it claims
    to model, and each RUNS.tsv record's age and result. The tests and model cards of v1.2.12 are cut
    from it."
   :origin "the owner, 2026-10-11: the v1.2.12 scope (inventory first)")

  (fix "cover-route-table-stats-routes" :release "v1.2.12" :status "planned"
   :title "In-package behaviour tests for the stats route table"
   :text "RouteTable (internal/sprint/routetable.go:25), the table `nova-sprint stats --routes` prints
    (docs/SPEC-SPRINT.md, the verb stats), gets tests from inside its own package in
    internal/sprint/routetable_behaviour_test.go: the window (a finish before since is left out with its
    provider takes, a deal, take or accept before it still counts), the ok/failed split and takes, the
    no-route and no-usage rules, the provider no-result and the more-than-fifty storm, landed and 1stT,
    wrong/reviewed, the money columns and the upper-median wall, and the FLASH (all) and PRO (all) rows.
    RouteTable's in-package statement coverage goes from 0.0% of 163 to 99.4%. No product change."
   :origin "the owner, 2026-10-11: v1.2.12 tests and models only; tmp/v1.2.12-inventory-2026-10-11.md section 8 item C1")

  (fix "roadmap-tool-becomes-nova-roadmap" :release "v1.2.13" :status "planned"
   :title "Extract the roadmap tool into nova-roadmap"
   :text "The roadmap and fixes code exists twice: here as internal/roadmap, internal/roadmapdoc,
    tools/roadmap and the nova-work roadmap verbs (about 3,100 lines with their tests), and in
    nova-tools as internal/roadmap, internal/roadmap/sexp and tools/roadmap (about 1,300 lines with
    their tests, about 930 without), with two diverging copies of the generator. Unify the s-expression format first; then one module and one CLI, nova-roadmap, living
    in this repository (a module and cmd/nova-roadmap): it reads and writes the data, renders
    ROADMAP.md and FIXES.md, and runs the sync check. Both repositories use it and delete their own
    copies: this one its internal copies, nova-tools by importing nova-roadmap from this repository.
    Net fewer lines. Its s-expressions stay importable later as a node by nova-work; that import is a
    v1.3 item, not this one."
   :origin "the owner, 2026-10-11: v1.2.13 contraction, pass 4 (shared code into modules); nova-roadmap lives in nova-sprint")
  (fix "briefs-by-reference" :release "v1.2.13" :status "planned"
   :title "Brief boilerplate moves into the card contract"
   :text "A brief names the card contract instead of carrying its text, so briefs are shorter and the
    contract has one copy. The RULES, LEDGERS, WALL AND TOOLS and BUDGET blocks, about 6 KB of every 10 to 11 KB brief, are referenced once from the card contract instead of repeated in each brief, and a card lint refuses repeated instruction text in a brief. Agreed with the owner on 2026-10-11 as a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)."
   :origin "roadmap item briefs-by-reference"
   :kept (:item-group "sprint"
          :item-date "2026-10-09"
          :item-why "A format change."
          :item-area "nova-card"
          :item-cards "1"
          :item-origin ""
          :earlier-title "Briefs carry the card contract by reference"))
  (fix "a-review-loop-has-a-circuit-breaker" :release "v1.2.14" :status "planned"
   :title "A review loop has a circuit breaker"
   :text "A card is escalated to the coordinator after 4 consecutive review rounds that each bring a new blocking finding, or after 6 substantive review rounds of any kind, instead of going round again."
   :origin "the owner, 2026-10-11: a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)")
  (fix "a-red-base-is-pushed-to-the-coordinator" :release "v1.2.14" :status "planned"
   :title "A red base is pushed to the coordinator"
   :text "When the base a card lands onto is red, the machine pushes the coordinator a judgment naming the base and its red run. This is the push half of card a-landing-never-goes-onto-a-red-base; the landing gate itself stays in v1.3."
   :origin "the owner, 2026-10-11: a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)")
  (fix "verify-defaults-to-the-trees-import-scope" :release "v1.2.14" :status "planned"
   :title "verify compares the scope its tree was imported with, not the whole organization"
   :text "import --repo <org>/<name> now records the repositories it read as the work-tree's :scope, written only for a scoped import; a whole-organization tree stays byte for byte what it was, and a file without :scope reads as a whole-organization tree. verify with no --repo compares the tree's own scope (tla/WorkImport.tla's Verify already read the scope it imported), --repo narrows within it, and a --repo outside it is refused naming the scope before any GitHub call; verify --against does the same. Before, verify with no --repo read the whole organization and reported MISSING for every repository the scoped import left out."
   :origin "the card verify-defaults-to-the-trees-import-scope w2; the v1.2.12 inventory 2026-10-11 section 9 row D12 (code read at nova-sprint main 00c54ba)")
  (fix "ready-over-reads-the-settled-rests" :release "v1.2.14" :status "planned"
   :title "The ready-over judgment reads the deal's settled rests"
   :text "A ready card whose tier is served only by a route resting at the tick's now raises the ready-over
    judgment (a ready card is not dealt), naming the resting route, whether the provider's recorded rest or
    one the rule rests this tick. The ready-over part settles the rests once as the deal does (withRests),
    on the snapshot the conditions are counted on, so the route the judgment names as resting is exactly one
    the deal does not draw; a rest whose time has ended is not named and the card is dealt."
   :origin "card ready-over-reads-the-settled-rests.w2; the seat's v1.2.14 audit, 2026-10-11")

(fix "doctor-reads-nova-config-through-the-seat-wrapper" :release "v1.2.11" :status "planned"
   :title "doctor reads nova-config from the caller's environment"
   :text "doctor now runs where it is typed, not on the sprint server, so its nova-config read uses the caller's NOVA_PG_DSN and NOVA_PG_PASSWORD_ENV. This matches the behavior of fleet sync and friend sync, which also read the config store with their caller's credentials."
   :origin "this card")

  (fix "no-deal-to-a-draining-member" :release "v1.2.10" :status "shipped"
   :title "No deal to a draining member; a held or draining machine's reader is asked nothing"
   :origin "PR #84")

  (fix "dashboard-is-the-live-page" :release "v1.2.10" :status "shipped"
   :title "The dashboard is the live page, byte for byte; the page lock and the card lint hold it"
   :origin "PR #83")

  (fix "dashboard-comments-name-the-owner" :release "v1.2.10" :status "shipped"
   :title "The page's comments name the owner as the owner; the lock takes a proven comment-only change"
   :origin "PR #85")

  (fix "main-green-after-the-night-landings" :release "v1.2.10" :status "shipped"
   :title "Main green: four semantic conflicts between the night's landings resolved"
   :origin "PR #87")

  (fix "two-ci-flakes-text-file-busy-and-git-maintenance" :release "v1.2.10" :status "shipped"
   :title "Two CI flakes: exec'd stubs written under the fork lock, and git's auto maintenance off in tests"
   :origin "PR #88")))
