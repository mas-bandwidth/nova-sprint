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
  releases are in that repository's FIXES.md."
 :releases
 ((release "v1.2.3" :status "shipped" :date "2026-10-10"
   :text "The first release from this repository: nova-sprint, nova-card and nova-work, re-seeded from nova-tools.")
  (release "v1.2.4" :status "shipped" :date "2026-10-10"
   :text "One fix: the sprint store's function library is back at its v1.2.2 digest, so an adopt can load it.")
  (release "v1.2.5" :status "in-progress"
   :text "The roadmap and this page come back as data, and the open issues move into the roadmap.")
  (release "v1.2.6" :status "planned"
   :text "The fixes held during the split, the point-release candidates found on 2026-10-10, the owed
    tests, and the sprint store cards that are point-release work. Each is re-applied or written in
    this repository, then cut as one release.")
  (release "v1.2.7" :status "planned"
   :text "Fixes found while the final dogfood lands through the lander."))
 :items
 ((fix "reseed-from-nova-tools" :release "v1.2.3" :status "shipped"
   :title "The sprint tools re-seeded from nova-tools"
   :text "The three commands, their packages, models and docs, at nova-tools dev, under the re-seed recipe."
   :origin "the split, layer L0; PR #28")
  (fix "lua-digest-restore" :release "v1.2.4" :status "shipped"
   :title "Six Lua comment lines restored"
   :text "The function library's digest is its v1.2.2 value again, so the adopt's shadow tick accepts it."
   :origin "PR #33, head 1f403558d")
  (fix "roadmap-and-fixes-as-data" :release "v1.2.5" :status "in-progress"
   :title "ROADMAP.md and FIXES.md generated from s-expression data"
   :text "docs/roadmap.sexp and docs/fixes.sexp are the data; `make roadmap` writes both pages, and a test
    fails when a page differs from what its data renders. The roadmap machinery the v1.2.3 re-seed left
    out comes back."
   :origin "this pull request; the machinery from commit b559389")
  (fix "frontier-read-judged-by-real-cause" :release "v1.2.6" :status "planned"
   :title "A frontier read no one may take is judged once, by its real cause"
   :text "When no frontier reader has room the read raises one judgment naming its real cause, rewritten
    in place so a stale judgment does not linger."
   :origin "nova-sprint commits 39e4253 (judgment half) and 118bc63 (not carried by the re-seed)")
  (fix "land-fenced-while-server-lands" :release "v1.2.6" :status "planned"
   :title "Land refuses while the server is landing"
   :text "The clone lock exists; the missing half is a server-record fence, so a hand land cannot run
    while the server lands."
   :origin "nova-sprint commit 089322b (not carried by the re-seed, nova-sprint PR #28)")
  (fix "add-refuses-card-off-sprint-base" :release "v1.2.6" :status "planned"
   :title "add and brief refuse a card cut off the sprint base"
   :text "The sprint records its base, and add and brief refuse a card whose BASE is not it, so off-base
    cards stop causing hand conflicts."
   :origin "nova-sprint commit 0d3f68a (not carried by the re-seed, nova-sprint PR #28)")
  (fix "overload-alarm-covers-friends" :release "v1.2.6" :status "planned"
   :title "The overload alarm counts friend AI timeouts too"
   :text "A friend AI whose cards keep timing out raises the overload alarm, as a member does today."
   :origin "nova-sprint commit 1c758aa (not carried by the re-seed, nova-sprint PR #28)")
  (fix "ok-percent-counts-reader-verdicts" :release "v1.2.6" :status "planned"
   :title "ok% counts reader verdicts; a friend card past its deadline is a redeal"
   :text "ok% counts reader verdicts only, and a friend card past its deadline is a redeal, not a failure,
    with a redealt column. It needs a table amendment and its migration, run on a stopped machine."
   :origin "nova-sprint commit 97afe45 (not carried by the re-seed, nova-sprint PR #28)")
  (fix "starving-judgment-carries-numbers" :release "v1.2.6" :status "planned"
   :title "The starving judgment carries working and width"
   :text "The starving text names working over width and says release a wave only with the numbers."
   :origin "nova-sprint commit e72b098 (not carried by the re-seed, nova-sprint PR #28)")
  (fix "friend-card-carries-own-rules" :release "v1.2.6" :status "planned"
   :title "add accepts a friend card that carries its own RULES line"
   :text "A friend card with its own RULES line is accepted instead of being held for the child RULES
    paragraph."
   :origin "nova-sprint commit b657f0f (not carried by the re-seed, nova-sprint PR #28)")
  (fix "server-build-descends-from-base" :release "v1.2.6" :status "planned"
   :title "A sprint server build must descend from the origin base"
   :text "server switch refuses a build that does not descend from the origin base, so an off-base build
    cannot become the server."
   :origin "nova-sprint commit 9338f08 (not carried by the re-seed, nova-sprint PR #28)")
  (fix "provider-balance-test-restored" :release "v1.2.6" :status "planned"
   :title "The provider balance package has its test again"
   :text "The provider balance package lost its test in the re-seed; restore it."
   :origin "nova-sprint re-seed carry review (dropped file group)")
  (fix "provider-key-refusal-rests-until-woken" :release "v1.2.6" :status "planned"
   :title "A provider that refuses its key rests until woken, never for a time"
   :text "A route whose provider refuses the key rests until routes wake, never for a time, and the rest
    reason reads until woken rather than until paid: the providers table, the stop and the judgment all
    say woken. TLA+ model RouteRest, property AuthEndsOnlyWoken, with a reversed witness. Tests cover
    routes wake on a key rest."
   :origin "nova-tools PR #5574, head 0c6706f2d; the providers table's until-paid wording is v1.2.5 candidate 6")
  (fix "bare-merge-stream-refused" :release "v1.2.6" :status "planned"
   :title "A bare merge --stream is refused on a real store"
   :text "Landings are recorded by name or by land; a bare merge --stream on a real store is refused."
   :origin "nova-tools PR #5565, head 52618fa02")
  (fix "pin-release-and-rule-answers" :release "v1.2.6" :status "planned"
   :title "A hard pin past its bound is offered on; a rule answer is consumed by its attempt"
   :text "A hard pin past its bound is offered to others, a card cannot ask once it waits, and a rule
    answer is consumed by its attempt. The pin release clock comment is corrected or the clock keyed
    on the pin."
   :origin "nova-tools PR #5573, head daab75763")
  (fix "paths-widening-edits-card-in-place" :release "v1.2.6" :status "planned"
   :title "A PATHS widening on a reader finding edits the card in place"
   :text "A reader finding that widens PATHS edits the card in place, never a twin. Three review notes are
    fixed before merge: the finding reader unset, the bound subtest, and the friend card claim."
   :origin "nova-tools PR #5570, head 22d72154d")
  (fix "deal-draws-launchable-route" :release "v1.2.6" :status "planned"
   :title "A deal draws only a route its member can launch"
   :text "The deal draws only routes whose harness the member can launch; the adopt must set each member
    harnesses or the deal draws nothing for it."
   :origin "nova-tools PR #5576, head 065bf6b73")
  (fix "lander-bench-fault-and-bisect" :release "v1.2.6" :status "planned"
   :title "The lander blames no head for a bench fault and bisects a red batch"
   :text "A bench fault blames no head, a red batch is bisected, and landings go as batches are built. The
    bench fault match reads only the bench error line, so a real failure that prints a disk string
    is still blamed."
   :origin "nova-tools PR #5579, head 5bb7de4f2")
  (fix "check-waived-read-and-where-tiers" :release "v1.2.6" :status "planned"
   :title "check honours a waived second read; where shows tiers once"
   :text "check judges an accept made before the second-read count by the count then; where shows a tier
    once and hides an expired until. A stale health seen time beside a live status is also fixed."
   :origin "nova-tools PR #5584, head 881df6ff")
  (fix "where-json-landed-series-fast" :release "v1.2.6" :status "planned"
   :title "where --json reads no log line for its landed series"
   :text "The tick counts landings into the where record, so where --json answers in well under a second;
    released sentinels are not counted as landings."
   :origin "nova-tools PR #5588, head 08f6af5a8")
  (fix "empty-run-harness-fault" :release "v1.2.6" :status "planned"
   :title "An empty run is its own harness fault"
   :text "An empty run is classed as a harness fault, and a rework never returns to the friend AI whose
    lane ran it empty, with a TLA+ model and witness."
   :origin "nova-tools PR #5583, head e8fdaecc")
  (fix "done-at-base-is-brief-duplicate" :release "v1.2.6" :status "in-progress"
   :title "A card already done at its base is a duplicate brief, not a worker failure"
   :text "Nothing to do because the base already contains the change is a brief defect, a duplicate of
    landed work, outside ok%, and raises a re-cut judgment."
   :origin "nova-tools PR #5585, head 0beb0508")
  (fix "fleet-verbs-skip-member-under-disk-floor" :release "v1.2.6" :status "planned"
   :title "Fleet verbs give a member under its disk floor nothing"
   :text "The coordinator fleet verbs give nothing to a member under its disk floor, stacked on the deal
    and read fix that already merged."
   :origin "nova-tools PR #5578, head fcdbaa441")
  (fix "down-member-is-down-not-fault" :release "v1.2.6" :status "planned"
   :title "A member whose host is offline is shown down, not as a fault"
   :text "Doctor and machinery report a member whose host is offline as down; FAULT only when the host is
    reachable and the member does not beat."
   :origin "v1.2.5 candidate list")
  (fix "worker-brief-never-rewrites-history" :release "v1.2.6" :status "planned"
   :title "The worker brief says never amend, rebase or reset onto origin"
   :text "One line in the worker brief stops children rewriting history, which caused the does-not-descend
    refusals."
   :origin "v1.2.5 candidate list")
  (fix "draining-member-clears-no-room" :release "v1.2.6" :status "planned"
   :title "A draining member does not keep a stale no-room word"
   :text "The room check runs after the drain return, so a draining member does not keep a stale no-room
    word; the deal and read disk floor fix also gets its TLA+ reversed witness."
   :origin "v1.2.5 candidate list; nova-tools PR #5569 follow-up")
  (fix "dashboard-pie-slices-meet-centre" :release "v1.2.6" :status "planned"
   :title "The dashboard pie slices meet at the centre"
   :text "Check the live pie after the next adopt; if the slice borders still miss the centre, fix it."
   :origin "v1.2.5 candidate list")
  (fix "restore-tests-owed-from-split" :release "v1.2.6" :status "planned"
   :title "Restore the tests the repository split moved out of nova-tools"
   :text "Five tests (coordinator rules, processor doc, coordinator tools, tool class, seat play), the
    member and contract functional tests and eight contract cases that build nova-sprint, and the
    tick wall-clock gate job, restored in nova-sprint."
   :origin "repository split cold reads (nova-tools PR #5571 and PR #5591)")
  (fix "defer-cards-to-the-roadmap" :release "v1.2.5" :status "planned"
   :title "A verb defers waiting cards to the roadmap with their whole briefs"
   :text "nova-sprint defer writes the named waiting cards, or a stream or repository of them, into the
    roadmap data with each whole brief, and drops them from the store; --expect checks the count."
   :origin "card from the sprint store (2026-10-10); the roadmap item roadmap-defer-verb, folded in")
  (fix "bench-tree-standalone" :release "v1.2.7" :status "planned"
   :title "A bench tree is a standalone clone, and one that is not is refused with its reason"
   :text "The mirror stage ends by checking that the staged tree's .git is a directory in the tree, and a
    .git file (a worktree pointer at a path of another machine) is refused naming the gitdir and whether
    it exists on the host. The tar copy refuses a worktree's .git file instead of carrying it over, so git
    no longer exits 128 on a fleet host with nothing saying why."
   :origin "lander fault 5579, space git exit 128 (held-PR ledger row 5579)")))
