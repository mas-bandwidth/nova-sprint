; synthetic roadmap: the shape of roadmaps/*.sexp, invented text only.
; Every card below is made up; strings carry "quotes", a backslash, (parens) and ; semicolons.
(:roadmap 1 :repo "example/repo" :product "example" :generated "2026-10-06"
 :source "internal/roadmap tests"
 :releases
 ((:release "v1.1.0" :status :planned :cards 102
   :streams
   ((:stream "stream-00" :cards
     ((:id "v11-card-001" :tier "flash" :needs ()
       :title "Synthetic card 1 (stream 0); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 1 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard1

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 1: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-002" :tier "pro" :needs ()
       :title "Synthetic card 2 (stream 0); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 2 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard2

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 2: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-003" :tier "-" :needs ()
       :title "Synthetic card 3 (stream 0); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 3 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard3

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 3: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-004" :tier "flash" :needs ()
       :title "Synthetic card 4 (stream 0); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 4 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard4

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 4: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-005" :tier "pro" :needs ()
       :title "Synthetic card 5 (stream 0); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 5 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard5

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 5: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-01" :cards
     ((:id "v11-card-006" :tier "-" :needs ()
       :title "Synthetic card 6 (stream 1); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 6 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard6

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 6: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-007" :tier "flash" :needs ("v11-card-006")
       :title "Synthetic card 7 (stream 1); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 7 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard7

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 7: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-008" :tier "pro" :needs ()
       :title "Synthetic card 8 (stream 1); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 8 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard8

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 8: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-009" :tier "-" :needs ()
       :title "Synthetic card 9 (stream 1); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 9 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard9

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 9: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-010" :tier "flash" :needs ()
       :title "Synthetic card 10 (stream 1); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 10 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard10

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 10: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-02" :cards
     ((:id "v11-card-011" :tier "pro" :needs ()
       :title "Synthetic card 11 (stream 2); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 11 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard11

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 11: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-012" :tier "-" :needs ()
       :title "Synthetic card 12 (stream 2); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 12 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard12

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 12: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-013" :tier "flash" :needs ()
       :title "Synthetic card 13 (stream 2); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 13 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard13

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 13: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-014" :tier "pro" :needs ("v11-card-013")
       :title "Synthetic card 14 (stream 2); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 14 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard14

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 14: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-03" :cards
     ((:id "v11-card-015" :tier "-" :needs ()
       :title "Synthetic card 15 (stream 3); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 15 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard15

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 15: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-016" :tier "flash" :needs ()
       :title "Synthetic card 16 (stream 3); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 16 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard16

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 16: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-017" :tier "pro" :needs ()
       :title "Synthetic card 17 (stream 3); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 17 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard17

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 17: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-018" :tier "-" :needs ()
       :title "Synthetic card 18 (stream 3); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 18 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard18

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 18: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-04" :cards
     ((:id "v11-card-019" :tier "flash" :needs ()
       :title "Synthetic card 19 (stream 4); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 19 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard19

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 19: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-020" :tier "pro" :needs ()
       :title "Synthetic card 20 (stream 4); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 20 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard20

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 20: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-021" :tier "-" :needs ("v11-card-020")
       :title "Synthetic card 21 (stream 4); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 21 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard21

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 21: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-022" :tier "flash" :needs ()
       :title "Synthetic card 22 (stream 4); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 22 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard22

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 22: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-05" :cards
     ((:id "v11-card-023" :tier "pro" :needs ()
       :title "Synthetic card 23 (stream 5); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 23 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard23

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 23: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-024" :tier "-" :needs ()
       :title "Synthetic card 24 (stream 5); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 24 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard24

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 24: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-025" :tier "flash" :needs ()
       :title "Synthetic card 25 (stream 5); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 25 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard25

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 25: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-026" :tier "pro" :needs ()
       :title "Synthetic card 26 (stream 5); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 26 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard26

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 26: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-06" :cards
     ((:id "v11-card-027" :tier "-" :needs ()
       :title "Synthetic card 27 (stream 6); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 27 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard27

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 27: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-028" :tier "flash" :needs ("v11-card-027")
       :title "Synthetic card 28 (stream 6); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 28 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard28

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 28: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-029" :tier "pro" :needs ()
       :title "Synthetic card 29 (stream 6); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 29 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard29

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 29: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-030" :tier "-" :needs ()
       :title "Synthetic card 30 (stream 6); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 30 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard30

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 30: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-07" :cards
     ((:id "v11-card-031" :tier "flash" :needs ()
       :title "Synthetic card 31 (stream 7); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 31 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard31

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 31: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-032" :tier "pro" :needs ()
       :title "Synthetic card 32 (stream 7); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 32 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard32

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 32: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-033" :tier "-" :needs ()
       :title "Synthetic card 33 (stream 7); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 33 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard33

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 33: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-034" :tier "flash" :needs ()
       :title "Synthetic card 34 (stream 7); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 34 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard34

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 34: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-08" :cards
     ((:id "v11-card-035" :tier "pro" :needs ("v11-card-034")
       :title "Synthetic card 35 (stream 8); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 35 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard35

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 35: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-036" :tier "-" :needs ()
       :title "Synthetic card 36 (stream 8); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 36 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard36

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 36: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-037" :tier "flash" :needs ()
       :title "Synthetic card 37 (stream 8); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 37 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard37

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 37: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-038" :tier "pro" :needs ()
       :title "Synthetic card 38 (stream 8); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 38 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard38

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 38: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-09" :cards
     ((:id "v11-card-039" :tier "-" :needs ()
       :title "Synthetic card 39 (stream 9); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 39 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard39

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 39: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-040" :tier "flash" :needs ()
       :title "Synthetic card 40 (stream 9); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 40 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard40

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 40: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-041" :tier "pro" :needs ()
       :title "Synthetic card 41 (stream 9); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 41 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard41

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 41: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-042" :tier "-" :needs ("v11-card-041")
       :title "Synthetic card 42 (stream 9); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 42 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard42

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 42: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-10" :cards
     ((:id "v11-card-043" :tier "flash" :needs ()
       :title "Synthetic card 43 (stream 10); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 43 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard43

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 43: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-044" :tier "pro" :needs ()
       :title "Synthetic card 44 (stream 10); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 44 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard44

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 44: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-045" :tier "-" :needs ()
       :title "Synthetic card 45 (stream 10); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 45 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard45

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 45: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-046" :tier "flash" :needs ()
       :title "Synthetic card 46 (stream 10); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 46 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard46

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 46: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-11" :cards
     ((:id "v11-card-047" :tier "pro" :needs ()
       :title "Synthetic card 47 (stream 11); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 47 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard47

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 47: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-048" :tier "-" :needs ()
       :title "Synthetic card 48 (stream 11); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 48 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard48

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 48: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-049" :tier "flash" :needs ("v11-card-048")
       :title "Synthetic card 49 (stream 11); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 49 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard49

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 49: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-050" :tier "pro" :needs ()
       :title "Synthetic card 50 (stream 11); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 50 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard50

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 50: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-12" :cards
     ((:id "v11-card-051" :tier "-" :needs ()
       :title "Synthetic card 51 (stream 12); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 51 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard51

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 51: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-052" :tier "flash" :needs ()
       :title "Synthetic card 52 (stream 12); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 52 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard52

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 52: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-053" :tier "pro" :needs ()
       :title "Synthetic card 53 (stream 12); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 53 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard53

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 53: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-054" :tier "-" :needs ()
       :title "Synthetic card 54 (stream 12); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 54 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard54

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 54: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-13" :cards
     ((:id "v11-card-055" :tier "flash" :needs ()
       :title "Synthetic card 55 (stream 13); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 55 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard55

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 55: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-056" :tier "pro" :needs ("v11-card-055")
       :title "Synthetic card 56 (stream 13); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 56 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard56

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 56: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-057" :tier "-" :needs ()
       :title "Synthetic card 57 (stream 13); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 57 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard57

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 57: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-058" :tier "flash" :needs ()
       :title "Synthetic card 58 (stream 13); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 58 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard58

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 58: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-14" :cards
     ((:id "v11-card-059" :tier "pro" :needs ()
       :title "Synthetic card 59 (stream 14); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 59 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard59

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 59: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-060" :tier "-" :needs ()
       :title "Synthetic card 60 (stream 14); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 60 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard60

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 60: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-061" :tier "flash" :needs ()
       :title "Synthetic card 61 (stream 14); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 61 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard61

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 61: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-062" :tier "pro" :needs ()
       :title "Synthetic card 62 (stream 14); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 62 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard62

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 62: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-15" :cards
     ((:id "v11-card-063" :tier "-" :needs ("v11-card-062")
       :title "Synthetic card 63 (stream 15); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 63 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard63

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 63: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-064" :tier "flash" :needs ()
       :title "Synthetic card 64 (stream 15); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 64 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard64

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 64: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-065" :tier "pro" :needs ()
       :title "Synthetic card 65 (stream 15); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 65 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard65

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 65: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-066" :tier "-" :needs ()
       :title "Synthetic card 66 (stream 15); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 66 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard66

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 66: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-16" :cards
     ((:id "v11-card-067" :tier "flash" :needs ()
       :title "Synthetic card 67 (stream 16); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 67 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard67

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 67: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-068" :tier "pro" :needs ()
       :title "Synthetic card 68 (stream 16); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 68 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard68

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 68: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-069" :tier "-" :needs ()
       :title "Synthetic card 69 (stream 16); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 69 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard69

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 69: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-070" :tier "flash" :needs ("v11-card-069")
       :title "Synthetic card 70 (stream 16); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 70 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard70

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 70: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-17" :cards
     ((:id "v11-card-071" :tier "pro" :needs ()
       :title "Synthetic card 71 (stream 17); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 71 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard71

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 71: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-072" :tier "-" :needs ()
       :title "Synthetic card 72 (stream 17); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 72 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard72

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 72: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-073" :tier "flash" :needs ()
       :title "Synthetic card 73 (stream 17); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 73 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard73

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 73: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-074" :tier "pro" :needs ()
       :title "Synthetic card 74 (stream 17); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 74 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard74

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 74: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-18" :cards
     ((:id "v11-card-075" :tier "-" :needs ()
       :title "Synthetic card 75 (stream 18); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 75 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard75

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 75: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-076" :tier "flash" :needs ()
       :title "Synthetic card 76 (stream 18); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 76 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard76

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 76: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-077" :tier "pro" :needs ("v11-card-076")
       :title "Synthetic card 77 (stream 18); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 77 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard77

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 77: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-078" :tier "-" :needs ()
       :title "Synthetic card 78 (stream 18); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 78 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard78

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 78: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-19" :cards
     ((:id "v11-card-079" :tier "flash" :needs ()
       :title "Synthetic card 79 (stream 19); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 79 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard79

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 79: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-080" :tier "pro" :needs ()
       :title "Synthetic card 80 (stream 19); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 80 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard80

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 80: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-081" :tier "-" :needs ()
       :title "Synthetic card 81 (stream 19); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 81 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard81

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 81: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-082" :tier "flash" :needs ()
       :title "Synthetic card 82 (stream 19); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 82 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard82

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 82: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-20" :cards
     ((:id "v11-card-083" :tier "pro" :needs ()
       :title "Synthetic card 83 (stream 20); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 83 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard83

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 83: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-084" :tier "-" :needs ("v11-card-083")
       :title "Synthetic card 84 (stream 20); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 84 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard84

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 84: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-085" :tier "flash" :needs ()
       :title "Synthetic card 85 (stream 20); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 85 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard85

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 85: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-086" :tier "pro" :needs ()
       :title "Synthetic card 86 (stream 20); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 86 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard86

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 86: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-21" :cards
     ((:id "v11-card-087" :tier "-" :needs ()
       :title "Synthetic card 87 (stream 21); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 87 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard87

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 87: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-088" :tier "flash" :needs ()
       :title "Synthetic card 88 (stream 21); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 88 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard88

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 88: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-089" :tier "pro" :needs ()
       :title "Synthetic card 89 (stream 21); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 89 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard89

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 89: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-090" :tier "-" :needs ()
       :title "Synthetic card 90 (stream 21); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 90 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard90

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 90: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-22" :cards
     ((:id "v11-card-091" :tier "flash" :needs ("v11-card-090")
       :title "Synthetic card 91 (stream 22); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 91 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard91

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 91: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-092" :tier "pro" :needs ()
       :title "Synthetic card 92 (stream 22); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 92 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard92

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 92: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-093" :tier "-" :needs ()
       :title "Synthetic card 93 (stream 22); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 93 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard93

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 93: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-094" :tier "flash" :needs ()
       :title "Synthetic card 94 (stream 22); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 94 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard94

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 94: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-23" :cards
     ((:id "v11-card-095" :tier "pro" :needs ()
       :title "Synthetic card 95 (stream 23); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 95 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard95

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 95: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-096" :tier "-" :needs ()
       :title "Synthetic card 96 (stream 23); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 96 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard96

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 96: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-097" :tier "flash" :needs ()
       :title "Synthetic card 97 (stream 23); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 97 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard97

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 97: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-098" :tier "pro" :needs ("v11-card-097")
       :title "Synthetic card 98 (stream 23); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 98 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard98

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 98: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))
    (:stream "stream-24" :cards
     ((:id "v11-card-099" :tier "-" :needs ()
       :title "Synthetic card 99 (stream 24); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 99 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard99

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 99: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-100" :tier "flash" :needs ()
       :title "Synthetic card 100 (stream 24); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 100 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard100

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 100: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-101" :tier "pro" :needs ()
       :title "Synthetic card 101 (stream 24); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 101 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard101

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 101: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")
      (:id "v11-card-102" :tier "-" :needs ()
       :title "Synthetic card 102 (stream 24); it says \"why\" in quotes"
       :brief "REPO: example/repo
BASE: main
START: cmd/example/verbs.go
STOP: card 102 is done, or the report names what blocks it
PATHS: cmd/example/**,docs/CLI.md
TEST: ./cmd/example TestCard102

RULES.
Work only in the job directory (this one).
No `rm -rf` outside it; a path like C:\\tmp stays as written.

THE TASK. Synthetic card 102: the owner said \"do it\" (once) ; then stopped.
STEP 1. Read the code.
STEP 2. Write the test first.")))))))
