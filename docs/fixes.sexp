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
  (release "v1.2.6" :status "planned"
   :text "The fixes held during the split, the point-release candidates found on 2026-10-10, the owed
    tests, and the sprint store cards that are point-release work. Each is re-applied or written in
    this repository, then cut as one release."
   :date "2026-10-10")
  (release "v1.2.9" :status "planned" :date "2026-10-10"
   :text "Inbox push writes each judgment once without concurrent writers sharing a temporary path."))
 :items
 ((fix "reseed-from-nova-tools" :release "v1.2.3" :status "shipped"
   :title "The sprint tools re-seeded from nova-tools"
   :text "The three commands, their packages, models and docs, at nova-tools dev, under the re-seed recipe."
   :origin "the split, layer L0; PR #28")
  (fix "lua-digest-restore" :release "v1.2.4" :status "shipped"
   :title "Six Lua comment lines restored"
   :text "The function library's digest is its v1.2.2 value again, so the adopt's shadow tick accepts it."
   :origin "PR #33, head 1f403558d")
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
    lanes begin its reads, and stop-return is served as a worker's verb."
   :origin "a one-shot friend host, 2026-10-10: took logged while the server showed the card ready, progress sent at epoch 0, every stop-return refused by the server")

  (fix "draining-member-clears-no-room" :release "v1.2.6" :status "shipped"
   :title "A draining member does not keep a stale no-room word"
   :text "The room check runs after the drain return, so a draining member does not keep a stale no-room
    word; the deal and read disk floor fix also gets its TLA+ reversed witness."
   :origin "v1.2.5 candidate list; nova-tools PR #5569 follow-up")

  (fix "bench-tree-standalone" :release "v1.2.6" :status "shipped"
   :title "A bench tree is a standalone clone, and one that is not is refused with its reason"
   :text "The mirror stage ends by checking that the staged tree's .git is a directory in the tree, and a
    .git file (a worktree pointer at a path of another machine) is refused naming the gitdir and whether
    it exists on the host. The tar copy refuses a worktree's .git file instead of carrying it over, so git
    no longer exits 128 on a fleet host with nothing saying why."
   :origin "lander fault 5579, space git exit 128 (held-PR ledger row 5579)")

  (fix "worker-brief-never-rewrites-history" :release "v1.2.6" :status "planned"
   :title "The worker brief says never amend, rebase or reset onto origin"
   :text "One line in the worker brief (the child rule no-rewrite-history, internal/fleetrules/child-rules.txt,
    byte for byte nova-tools' fleet/child-rules.txt, with a test pinning the digest in both repositories)
    stops children rewriting history, which caused the does-not-descend refusals."
   :origin "v1.2.5 candidate list")

  (fix "working-is-a-live-run" :release "v1.2.6" :status "shipped"
   :title "Working means a live run, RUNNING or STOPPED; a returned card no longer holds the STOP"
   :text "A worker's beat carries its live set (fleet beat and friend beat --live), and the tick returns a
    working card its row's live set has not named for 60 s to ready, while running and while stopped; the
    dashboard's working cell counts live runs. A STOPPED reconcile's return writes the STOP receipt and
    settles its debt as a stop-return does, so a hold of the row and a clear are not refused before
    start; a report refused for the stop is kept, held, and taken after start at its generation.
    Modelled in tla/LiveRuns.tla (Settle, ReturnedFreesItsRow, AllReturnedEmptiesDebt)."
   :origin "the STOPPED sprint of 2026-10-10: 59 stale takes, and the hold of friend.zhi refused; nova-sprint PR #54")

   (fix "inbox-push-write-once-race" :release "v1.2.9" :status "planned"
    :title "Inbox push cannot overwrite a published judgment"
    :text "Two overlapping clients share one temporary path, so a second write can change the published
     file. Use a unique temporary path per writer."
    :origin "issue #5160")

   (fix "rest-properties-cap" :release "v1.2.9" :status "planned"
    :title "Rested routes must not overflow the fleet table property cap"
    :text "Rule 3 writes one property per rested route, so the cap is reached at about 58 routes and the
     tick writes nothing. Use one property for all."
    :origin "issue #5210")))
