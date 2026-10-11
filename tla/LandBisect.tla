------------------------------ MODULE LandBisect ------------------------------
\* nova-sprint land, the blame of a red batch (cmd/nova-sprint/landpass.go, bisect;
\* 2026-10-10, fault item 4: "landings refused on go build, streams stall in merging").
\* LandPass.tla holds the pass over the streams at the grain of one batch; this module
\* holds what happens inside one batch when its one gate is red.
\*
\* BEFORE. A red batch gate gated every head again, one after another from the base
\* (build with gateEach): N gates for N heads. v1-4's batch of 99 cards ran past
\* LandDeadline on that walk, was abandoned with nothing blamed, and was cut again the
\* next pass the same way: the stream sat in merging for hours, no card returned. And a
\* bench that could not run the gate at all (`sh: go: not found`, `disk quota exceeded`)
\* answered red, so the walk blamed whichever head it was gating.
\*
\* THE WORLD. The batch is N heads merged in order onto a base whose tree passed the gate;
\* tip(i) is the tree after head i. Whether each tip is red is the oracle red, chosen at
\* Init over every assignment and then fixed (a gate answers what the tree is); red[N] is
\* TRUE, the batch's own gate. Red need not be monotone: a later head may fix what an
\* earlier one broke. A probe of a tip may instead meet a bench fault (Faults): the bench
\* did not run the gate, which says nothing about the tree. A probe of a red tip may also
\* answer with a test's own output that carries the words a bench fault prints (a test that
\* wrote "no space left on device" and failed): that is a red tree, not a fault.
\*
\* THE STATE.
\*   lo, hi    the search: tip(lo - 1) passed (or is the base, lo = 1) and tip(hi) is red
\*   probes    the gates the search ran
\*   faulted   a probe met a real bench fault (the bench did not run the gate)
\*   envfail   the environment refused the batch (a missing tip, a failed diff or reset, a
\*             cancelled gate, or a remote bench that never ran the gate)
\*   state     search, blamed (head blamed returned with its finding; heads 1..blamed-1,
\*             tip(blamed - 1) gated green, go on to land), refused (a bench fault or an
\*             environment refusal: the batch is refused for the pass, no head blamed, its
\*             cards still queued)
\*
\* THE RULES.
\*   BlameSound: a blamed head is the one whose merge turned a passing tree red: tip(k)
\*     red and tip(k - 1) passing (or the base). The prefix that lands passed a gate.
\*   ProbesBounded: at most ceil(log2 N) gates after the batch's own: 99 heads, 7 gates.
\*   Ends (liveness, under fairness): the search ends, blamed or refused.
\*   RefusedOnlyByBenchOrEnv: a batch is refused only on a probe that met a real bench
\*     fault (faulted) or an environment refusal (envfail); a red tree whose test printed
\*     a fault's words is searched and blamed like any red tree (benchFault reads only the
\*     bench's own lines).
\*
\* Broken: "none" is the design.
\*   "each"         the walk of every head in order (probe lo, not the middle): the gate
\*                  per head of before (ProbesBounded fails).
\*   "faultblames"  a bench fault answers red (BlameSound fails: a head blamed whose tree
\*                  never failed).
\*   "last"         the last head merged is blamed with the batch's finding, no search
\*                  (BlameSound fails when the tree was red before it).
\*   "anyline"      the fault words matched on any output line, a test's included: a red
\*                  tip whose test printed them is read as a bench fault and the batch is
\*                  refused, no head blamed, every pass (RefusedOnlyByBenchOrEnv fails).
\*   "envblames"    an environment refusal blames the head it was about to probe
\*                  (BlameSound fails).
\*
\* WHAT IS NOT MODELLED. The merges and checks (a head that does not merge ends the batch
\* before any gate, as before), the base's gate and cure, the deadline (LandPass.tla), the
\* push and the report (Land.tla), and which gates run the tree tests: a tip is gated with
\* them only when the heads up to it changed a file they read, so a batch that changes none
\* is gated once more without them before the search, and green there lands whole with
\* nothing blamed; red[N] here is the batch's red under the gate the search uses.
\*
\* TLC, 2026-10-10, hetzner2, tla2tools.jar as tla/tla2tools.sha256 pins it: MCLandBisect
\* (N = 6, faults on) passes TypeOK, BlameSound, ProbesBounded and Ends; the three reversed
\* witnesses each fail the property their configuration names. The records are tla/RUNS.tsv.
\* Extended 2026-10-10 (v1.2.6, nova-sprint #48) with Words, faulted, RefusedOnlyByBench and
\* the "anyline" witness; all five cases rerun on the bench space, records in tla/RUNS.tsv.
\* Extended 2026-10-11 (v1.2.12) with envfail and the Env refusal (the bisect's other
\* refusals: a missing tip, a failed diff or reset, a cancelled gate, a remote bench that
\* never ran the gate), RefusedOnlyByBench restated RefusedOnlyByBenchOrEnv, and the
\* "envblames" witness. Env is enabled for a one-head batch and after the search narrows to
\* one head (the code's missing-tip, initial-diff and final-reset refusals, landpass.go
\* 898-899, 926-927, 953-954), and it never runs a gate, so it leaves probes unchanged and
\* ProbesBounded keeps its meaning.
EXTENDS Integers, FiniteSets

CONSTANTS N, Faults, Broken

ASSUME N \in Nat \ {0}
ASSUME Faults \in BOOLEAN

VARIABLES red, lo, hi, probes, state, faulted, envfail

vars == <<red, lo, hi, probes, state, faulted, envfail>>

Heads == 1..N

\* The least k with 2^k >= n.
RECURSIVE Pow2(_)
Pow2(k) == IF k = 0 THEN 1 ELSE 2 * Pow2(k - 1)
Log2Ceil(n) == CHOOSE k \in 0..n : Pow2(k) >= n /\ (k = 0 \/ Pow2(k - 1) < n)

TypeOK ==
  /\ red \in [Heads -> BOOLEAN]
  /\ lo \in Heads /\ hi \in Heads
  /\ probes \in Nat
  /\ state \in {"search", "blamed", "refused"}
  /\ faulted \in BOOLEAN
  /\ envfail \in BOOLEAN

Init ==
  /\ red \in {r \in [Heads -> BOOLEAN] : r[N]}
  /\ lo = 1 /\ hi = N
  /\ probes = 0
  /\ state = "search"
  /\ faulted = FALSE
  /\ envfail = FALSE

\* The tip probed: the middle of the span, or (each) its first.
Mid == IF Broken = "each" THEN lo ELSE (lo + hi) \div 2

\* One gate of tip(Mid): red moves hi down to it, passing moves lo past it.
Probe ==
  /\ state = "search" /\ lo < hi /\ Broken # "last"
  /\ IF red[Mid] THEN hi' = Mid /\ lo' = lo ELSE lo' = Mid + 1 /\ hi' = hi
  /\ probes' = probes + 1
  /\ UNCHANGED <<red, state, faulted, envfail>>

\* The bench did not run the gate: the batch is refused for the pass, nothing blamed
\* (faultblames: the fault is read as a red tree).
Fault ==
  /\ Faults /\ state = "search" /\ lo < hi /\ Broken # "last"
  /\ IF Broken = "faultblames"
       THEN hi' = Mid /\ lo' = lo /\ UNCHANGED state
       ELSE state' = "refused" /\ UNCHANGED <<lo, hi>>
  /\ probes' = probes + 1
  /\ faulted' = TRUE
  /\ UNCHANGED <<red, envfail>>

\* A red tip's gate answers with a test's output that carries a bench fault's words. The
\* bench ran the gate: the tree is red, and the search goes on as for any red tip (anyline:
\* the words matched on the test's line, read as a fault, the batch refused, no head blamed).
Words ==
  /\ state = "search" /\ lo < hi /\ Broken # "last" /\ red[Mid]
  /\ IF Broken = "anyline"
       THEN state' = "refused" /\ UNCHANGED <<lo, hi>>
       ELSE hi' = Mid /\ lo' = lo /\ UNCHANGED state
  /\ probes' = probes + 1
  /\ UNCHANGED <<red, faulted, envfail>>

\* The environment refused the batch: a tip whose merge was not recorded, a git diff or
\* reset that failed, a gate that was cancelled, or a remote bench that never ran the gate.
\* It refuses before the search probes (a missing tip, the initial diff), during it, and
\* once the search narrowed to one head (a failed final reset), so a one-head batch can be
\* refused too; none of these ran a gate, so probes is unchanged and a failed diff or reset
\* is not counted as another gate after the completed search. Nothing is blamed (envblames:
\* the refusal blames the head it was about to probe).
Env ==
  /\ state = "search" /\ Broken # "last"
  /\ IF Broken = "envblames"
       THEN state' = "blamed" /\ lo' = Mid /\ hi' = Mid
       ELSE state' = "refused" /\ UNCHANGED <<lo, hi>>
  /\ envfail' = TRUE
  /\ UNCHANGED <<red, probes, faulted>>

\* The span is one head: it is blamed (last: the last head, whatever the span).
Blame ==
  /\ state = "search" /\ (lo = hi \/ Broken = "last")
  /\ state' = "blamed"
  /\ IF Broken = "last" THEN lo' = N /\ hi' = N ELSE UNCHANGED <<lo, hi>>
  /\ UNCHANGED <<red, probes, faulted, envfail>>

Next == Probe \/ Fault \/ Words \/ Env \/ Blame

Spec == Init /\ [][Next]_vars

FairSpec == Spec /\ WF_vars(Probe) /\ WF_vars(Blame)

\* ---- the rules ----

BlameSound == state = "blamed" => red[lo] /\ (lo = 1 \/ ~red[lo - 1])

ProbesBounded == probes <= Log2Ceil(N)

Ends == <>(state # "search")

RefusedOnlyByBenchOrEnv == state = "refused" => (faulted \/ envfail)

=============================================================================
