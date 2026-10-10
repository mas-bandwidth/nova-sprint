; The nova-sprint point releases: the data FIXES.md is generated from.
; Read by internal/roadmapdoc through internal/worklang (data, never evaluated).
; Edit this file, then run `make roadmap`; never edit FIXES.md by hand.
; The shape is in internal/roadmapdoc/fixes.go.
(fixes "v1"
 :title "nova-sprint fixes"
 :text "The point releases from v1.2.3, the first release from this repository: what each one shipped, is
  shipping or will ship. A point release carries fixes only; new work is in ROADMAP.md. nova-tools' point
  releases are in that repository's FIXES.md."
 :releases
 ((release "v1.2.3" :status "shipped" :date "2026-10-10"
   :text "The first release from this repository: nova-sprint, nova-card and nova-work, re-seeded from nova-tools.")
  (release "v1.2.4" :status "shipped" :date "2026-10-10"
   :text "One fix: the sprint store's function library is back at its v1.2.2 digest, so an adopt can load it.")
  (release "v1.2.5" :status "in-progress"
   :text "The roadmap and this page come back as data, and the open issues move into the roadmap."))
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
   :origin "this pull request; the machinery from commit b559389")))
