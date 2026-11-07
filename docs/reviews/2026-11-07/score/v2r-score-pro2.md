# Tool score: nova-sprint v2 as a tool for AIs

Review of `mas-bandwidth/nova-sprint` at `43d849e29d0e4568ba6a1348a076a59259639938`
(branch `sprint/v2-review/v2r-score-pro2`), walked as a cold AI user: `help`,
`help <verb>`, the playbooks, the card contract, the layer docs, the code, and
the test suites. No sibling reviews existed under `docs/reviews/` at this base,
so there is nothing to agree or disagree with there.

## Score: 7/10

nova-sprint v2 is the rare agent tool whose safety story is enforced in code
rather than asserted in prose — prerequisite-checked verbs, teaching refusals,
an admission lint that proves a brief against origin and roster before a card
exists, a TLA+-modelled store, and a `next` verb that always names the one
command to run — and on the strength of that alone it earns most of its score.
It loses the rest on cold-start weight and unproven operability: a new AI must
absorb roughly 82 KB of playbooks and contract before its first card, setup
presupposes an entire nova-tools fleet (Tailscale, Redis, PostgreSQL, Ansible)
that the tool does not itself provide, four of six layer designs still say
"not approved" while the code beneath them is built and shipping, and the
project's own release gates — a cold AI setting up a fresh fleet twice from
the docs alone, plus a full dogfood sprint — are by its own account still
owed. It is a well-built engine that has not yet proved a stranger can drive
it for days without a human nearby.

## What is good

- **Refusals teach.** Every verb I sampled (`take`, `finish`, `read`, `add`,
  `judge`, `hook`, `check`, `run`, `backup`) prints its prerequisite table and,
  for each refusal, the exact command that makes it true — e.g. `help finish`
  names "not on the dealt branch at origin: git push origin <branch>" and
  "a commit since the base by another author or committer email: the two git
  config lines hook prints, then rewrite those commits". This is the
  `SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>`
  form specified in `docs/CORRECT-USE.md`, and the worker playbook's refusal
  catalogue (`docs/PLAYBOOK-WORKER.md`, "The refusals you will meet") shows the
  same lines an agent will actually see. An AI can recover from almost any
  refusal without asking a human.
- **`next` is the right front door.** `nova-sprint` with no arguments runs
  `next`, which "never refuses a missing prerequisite (the answer names it)"
  (`go run ./cmd/nova-sprint help next`). For a tool an AI meets cold, one
  verb that always answers "do this one thing, here is its command" is worth
  more than any amount of documentation, and `hook` printing both playbooks
  plus the sprint's two git-config lines means identity is configured by the
  tool, not remembered by the model.
- **JOB.md is generated, not remembered.** The bench worker writes the card's
  facts and finish rules into the job directory at staging
  (`edge/worker/jobmd.go:21` `writeJobMD_internal`), and the harness prompt
  carries the same finish rules word for word (`edge/worker/jobmd.go:50`
  `finishRules_internal`), so the two can never disagree. The card, its base,
  branch, prior head and finding, and the `result.json` verdict forms are all
  in the file the agent is already looking at; nothing depends on context the
  agent no longer has. This review card itself arrived in that form and was
  workable cold.
- **Admission finds defects before they cost a worker.** `add` parses the
  brief against `docs/CARD-CONTRACT.md` and then proves it against origin and
  the roster — PATHS entries exist at the base, a row serves the tier and
  every `NEEDS-ROUTE`, the work is not already at the base, DONE-WHEN is one
  shell-free command parsed by the single `edge.ParseGate` that the worker's
  `RunTest` also calls. `check` re-proves every waiting or ready producer the
  same way. Brief defects become coordinator fixes at add time instead of
  three workers rediscovering them.
- **The enforcement ledger is honest about its gaps.** `docs/ENFORCED.md`
  maps every claimed guarantee to the invariant, witness, test, or schema
  check that holds it, and prints its own bottom line: "held by nothing: 20"
  (`docs/ENFORCED.md:184`), each with a "What Would Hold Each" entry naming
  the exact function and test to write. Most tools hide this table; this one
  publishes it, which is what makes the rest of the ledger believable.
- **Test discipline is real and layered.** Unit tests run with no clock,
  sockets, or network (`docs/TESTING.md`), functional tests each start their
  own PostgreSQL, `tools/silentlint` refuses silent-error forms in the tree,
  and the store carries 38 TLA+ modules with reversed witnesses. In this
  review `go build ./...` and `go vet ./...` were clean and
  `go test -count=1 -skip Functional ./...` passed all 19 packages.
- **The deal is the walkthrough.** A deal message carries the `take` command
  with card, generation, and epoch; `take` prints the whole brief and ends
  with the exact `finish` or `read` command. Combined with push-not-pull
  delivery and idempotent `take` ("a repeat prints the same and writes
  nothing"), a crashed or wrapped session re-enters by re-running three
  documented steps and loses nothing.

## What gets it to 10

1. **Cut the cold-start reading load to one page.** Today a worker is handed
   `docs/PLAYBOOK-WORKER.md` (23 KB), a coordinator both playbooks plus
   `docs/CORRECT-USE.md` and `docs/CARD-CONTRACT.md` (~82 KB total) before
   doing useful work. Change: make `hook` print a one-screen quickstart (the
   three hook-in steps, the deal shapes, the four report forms, the refusal
   rule) and link the full playbooks by section; move the refusal catalogue
   into `help <verb>` output where it is already duplicated. Done when: a
   fresh AI completes hook → take → finish using only `hook`'s output and
   `next` lines, and the playbooks are reference, not required reading.
2. **Ship a zero-fleet trial mode.** `docs/SETUP.md` requires nova-tools'
   fleet (Tailscale, Redis, PostgreSQL, Ansible) before anything works, and
   `docs/README.md` states the dependency plainly. Change: a
   `nova-sprint up --local` (or `demo`) path that runs store, bus, run, one
   worker, and dash on one machine with embedded or single-binary
   substitutes, clearly labelled non-production. Done when: an AI with no
   fleet goes from clone to one card dealt, worked, read, and landed locally
   using only the tool's help, in under 30 minutes.
3. **Close the 20 `nothing` rows in `docs/ENFORCED.md`, or downgrade the
   claims.** Each row already names its fix (e.g. `L2-U6`: implement
   `AskReads{Rows}` in `card/move.go` with `test TestAskReads`; `L1-P2`: a
   backup/restore verb test). Change: work the list in ledger order, one card
   per row, updating the "Held by" cell as each lands. Done when:
   `grep -c '| nothing' docs/ENFORCED.md` is 0, or each surviving row's
   source claim in the layer doc is rewritten to match what is held.
4. **Reconcile layer-doc status with the built code.** `docs/LAYER-3-STEPS.md:3`
   says "not yet approved", `docs/LAYER-5-POLICY.md:3` says "not approved,
   not built", and `docs/LAYER-6-RUN.md:3` says "not approved", yet `steps/`,
   `policy/`, and `run/` are built, tested, and dealing real cards. Change:
   either approve and date each design, or add a "Built" section per layer
   stating where code has diverged from the draft. Done when: no layer doc's
   status line contradicts the tree, checked by a doc test that greps the
   status lines against a manifest.
5. **Meet the project's own two release gates and record them.** `docs/SETUP.md`
   owes (a) a cold AI setting up a fresh fleet from the docs alone, twice,
   and (b) a real dogfood sprint run to completion with the dashboard live;
   `docs/README.md` repeats both under the v2.0.0 gate. Change: run them,
   and commit the transcripts (commands, timings, where the AI stalled) under
   `docs/reviews/`. Done when: two cold-setup transcripts and one completed
   dogfood sprint are in the tree, and every stall they found is either fixed
   or filed as a card.
6. **Make the functional suite pass on a stock developer machine.**
   `go test ./...` on this Mac failed across `store`, `run`, `dash`,
   `cmd/nova-sprint`, and others with PostgreSQL's "could not create shared
   memory segment: No space left on device" because each functional test
   starts its own server and macOS's default `kern.sysv.shmmni` (32) runs
   out; `docs/TESTING.md` documents running with `-parallel 4` as the
   workaround. Change: have the functional test harness cap its own
   parallelism (e.g. a semaphore in the shared test helper) or set
   `-parallel` via a `make test` target the docs name first. Done when:
   plain `go test ./...` is green on a default-configured Mac and on Linux
   CI with no environment tuning.
7. **Add a dry-run "explain my day" for coordinators.** The coordinator's
   situational awareness is spread across `check`, `judge`, `show run`,
   `show <stream>`, and the dashboard. Change: one verb (e.g.
   `nova-sprint brief` or `next --all`) printing, in deal order: open
   judgements with their commands, cards working/ready/waiting per stream
   with why-waiting, rows present versus expected, and the next landing per
   stream. Done when: the session-start sequence in
   `docs/PLAYBOOK-COORDINATOR.md` section 1 collapses to that one command
   plus `judge`, verified against the playbook's own checklist.
8. **Publish per-verb cost and latency budgets.** The engine is engineered
   for a one-second tick at thousands of cards (`docs/README.md`,
   "The performance bar is a gate"), but an AI choosing between polling
   `show` and waiting for a push gets no numbers. Change: record tick,
   `take`, `finish`, and `check` timings at the 5,000-producer load test in
   `docs/LAYER-6-RUN.md` next to the existing layer-1 figures, and have
   `check` warn when a live tick exceeds budget. Done when: the load-test
   table covers verbs as well as the tick and is regenerated by the test
   that measures it.

## Where I would be lost cold

1. **Before the first verb runs: which world am I in?** `help` says every
   verb "needs only the store up makes at its step 2", but a cold AI does
   not yet know whether it is on a fleet, a bench, or a laptop with nothing.
   The first stall is deciding whether to run `up`, `check`, or `hook`
   first when all three refuse informatively but none says "you have no
   fleet; start at `docs/SETUP.md` step 1". `next` mitigates this once the
   store exists; before it exists, the ordering across nova-tools and
   nova-sprint is the gap.
2. **The credential wrapper.** The worker playbook reveals mid-procedure
   that "every nova-sprint verb needs the sprint's credential in its
   process: run it under `nova-secrets exec --only
   NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint <verb> ...`".
   A cold AI that runs a bare verb first meets a refusal it cannot map to
   the playbook step it is on, and the bus-hook form differs per harness
   (Monitor tool, `--harness grok|antigravity`, a Codex service) in ways
   only the playbook's step 1 enumerates. This is the densest paragraph in
   the cold path.
3. **`help <verb>` for multi-word verbs.** The index lists `stream add`,
   `policy set`, `policy activate`, and friends, but `help policy-set`
   refuses with "no verb policy-set: nova-sprint help help" (exit 2), and
   discovering that the spelling is `help policy set` costs a round trip.
   Small, but it is exactly the kind of friction a cold AI hits in its
   first minute, and the fix (accept the hyphenated alias) is trivial.

## Tokens and time

The most expensive thing to understand was the documentation corpus itself:
roughly 8,900 lines under `docs/`, of which a working AI is expected to have
read the two playbooks, CORRECT-USE, and the card contract (~82 KB) before
its first card, with the six layer designs (~5,700 lines) behind them. The
writing is good and the redundancy is deliberate (the same finish rules in
JOB.md, prompt, and playbook), but the token cost is paid by every fresh
context on every card, and it dwarfs the cost of the verbs themselves. The
6.4 KB `help` index is well spent by comparison: one read orients the whole
verb surface.

In execution, the suites dominated wall-clock: the unit suite
(`go test -count=1 -skip Functional ./...`) took several minutes, with
`card` at ~98 s and `steps` at ~55 s, and the full `go test ./...` run
additionally failed on this machine for the shared-memory reason in item 6
above — environmental, but the time was spent either way. Build and vet were
seconds. Nothing in the tool's own verb path was slow; the cost of using
nova-sprint today is reading about it and setting up the fleet beneath it,
not running it.

---

Verification for this review: `go build ./...` clean; `go vet ./...` clean;
`go test -count=1 -skip Functional ./...` passed all 19 packages;
`go test ./...` failed only with PostgreSQL shared-memory exhaustion
("could not create shared memory segment: No space left on device") across
functional packages on this macOS host, the failure `docs/TESTING.md`
attributes to default parallelism against `kern.sysv.shmmni`. NOT RUN: any
verb against a live store or bus, `up`, the dashboard, TLC, and the
functional suite under `-parallel 4`.
