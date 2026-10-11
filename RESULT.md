head: 0000000000000000000000000000000000000000
branch: sprint/fn-generated-directory-test-proves-the-seat-by-hook.w2.g1.e17
verdict: not-done
gate: go test -count=1 -tags functional -timeout 600s ./cmd/nova-card/
output: -
report: The seat cannot be proven test-side: the built nova-sprint's proof is the hook, and `hook` REFUSES without a running server ("the hook is a subscription the sprint's server holds: set NOVA_SPRINT_SERVER=<host:port>"); the functional test starts no server and the wall forbids one, so TestAGeneratedDirectoryIsAdmittedWholeAndItsWavesHold stays red with no allowed product change.

## Body

### What the card asks

`TestAGeneratedDirectoryIsAdmittedWholeAndItsWavesHold`
(`cmd/nova-card/generate_functional_test.go`) proves the seat before any
coordinator verb with verbs and a flag the product removed: `seat push --sent
<nonce>` (line 116), `seat pong <nonce>` (line 121), and a bare `seat push`
expecting `PUSH OK name=coordinator` (lines 123-125). 41f0be8 removed
`seat push --sent`/`--failed` and `seat pong`; `seat push` now takes `--harness`,
`--target`, `--session`, `--dry-run` (`cmd/nova-sprint/pushproof.go:172`), and
the seat proof is the hook (`cmd/nova-sprint/hook.go`; docs/SPEC-SPRINT.md, "The
hook").

### Red first (reproduced)

```
$ nice -n 19 go test -count=1 -tags functional \
    -run '^TestAGeneratedDirectoryIsAdmittedWholeAndItsWavesHold$' ./cmd/nova-card/
--- FAIL: TestAGeneratedDirectoryIsAdmittedWholeAndItsWavesHold (8.96s)
    generate_functional_test.go:119:
        Error: []int{0, 1} does not contain 2
        Messages: nova-sprint seat push --sent 0123456789abcdef: nova-sprint seat push REFUSED:
          unknown flag --sent; the flags of seat push are --actor, --dry-run, --epoch,
          --harness, --json, --max, --op, --redis, --session, --target
FAIL
```

### Why the prescribed test-side fix cannot be made

The card's step (1) says to start `nova-sprint hook` as a child process and
answer its `HOOK CHALLENGE` with `nova-sprint hook --prove <c>`. That is not
possible in this test:

- `hook` is a subscription the *server* holds. With no `NOVA_SPRINT_SERVER`,
  the built binary refuses before it can print a challenge:

  ```
  $ nova-sprint hook --actor coordinator
  nova-sprint hook REFUSED: the hook is a subscription the sprint's server holds:
    set NOVA_SPRINT_SERVER=<host:port> (run --listen prints it); run:
    NOVA_SPRINT_SERVER=127.0.0.1:<port> nova-sprint hook
  ```

- The test's store is a by-hand twin (`NOVA_SPRINT_REDIS=mem:<file>`,
  `internal/sprint/store/engine.go:111`): no machine runs between commands, so
  there is no server for the hook to hold a subscription on, and no challenge to
  read.

- The hook record (`seat-hook`) is written by the server alone
  (`cmd/nova-sprint/hook.go`, `writeHook`); no verb the test may run writes it.
  Every coordinator verb is gated (`hookedIn`, `hookFirst`), so the `add`/`card`
  assertions that follow the proof cannot run until a live, proven hook exists.

- The card's step (3) and the from-sprint note both say the wall forbids the
  test starting a server. Respecting that, the proof cannot be made from this
  test process, and no product or test change is allowed to force it.

Per step (3): the test is left red, no product code is changed, no test is
changed, and this card ends not-done.

### Diff stat

```
RESULT.md | 1 file changed (this report)
```

### Not done

- `cmd/nova-card/generate_functional_test.go` is unchanged (left red, per step 3).
- No `docs/fixes.sexp` / `FIXES.md` entry: the card makes the ledger edit the
  last commit after the gate is green, and the gate is red.
- `pushproof.go`, `hook.go` and every other product file are untouched.

### What the verb wants (for the next card)

The proof needs a server: `nova-sprint run --listen <addr>` (or an installed
seat's server) with the store the coordinator verbs use, then `nova-sprint hook
--actor <seat>` as a child of the test with `NOVA_SPRINT_SERVER` set, its
`HOOK CHALLENGE` read from stdout, answered with `nova-sprint hook --prove <c>
--actor <seat>`, and the coordinator verbs sent to that same server. That is a
functional-test harness change (and starts a server), which this card's wall and
step (3) exclude.
