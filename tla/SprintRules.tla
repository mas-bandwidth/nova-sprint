----------------------------- MODULE SprintRules -----------------------------
\* The machine feeds itself (the coordinator, for the owner, 2026-10-04 at 1:36 PM: "I want this
\* sort of oh no fleet is idle, do judgement, release more cards thing -- i want this
\* more automated."). Ten small state machines of nova-sprint, each its own Part, each
\* checked alone; docs/SPEC-SPRINT.md section 2 ("A card replaced by its twin") and
\* section 8 ("Answered by rule", and the idle alarm in section 14), internal/sprint
\* twins.go, rules.go and idle.go, cmd/nova-sprint landgo.go (the base gate).
\*
\* Part "twins": an old card o, its twin t, and two waiting cards that need o.
\*   Replace (add --replaces) re-points every waiting need of o to t, drops o if it is
\*   still open, and answers the blocked judgments that named o, in one step; Relink
\*   does the same for a drop and an add made apart. Drop raises a blocked judgment on
\*   every waiting card that needs the dropped card (the code's Drop).
\* Part "rules": one card's attempts under the failed and bound rules: tiers 1 (flash),
\*   2 (pro), 3 (heavy), 4 (a friend's card, where no rule answers: a mind does). A rule
\*   escalates to the first served tier above (a route serves it: Served), skipping tiers
\*   no route serves, and to a friend's card past the last served tier; the fleet's failure
\*   (the same failure on three cards), a card at its brief's bound, and a budget
\*   exhaustion at its second attempt all take the failed and bound rules' place.
\* Part "late": one work card past its deadline, its holder's progress stamps (the `progress`
\*   verb, an outside event: Stamp, and Silence as the stamp ages past the window), the one
\*   wait a generation, the hold of a card whose holder never stamped (the default: wait
\*   only), and the return and redeal of one whose holder stamped and went silent, once its
\*   holder has had its own whole deadline (own counts it, Window is it). A take is withdrawn
\*   from outside (Withdraw: the provider failed it, or the environment exited) with no
\*   attempt spent; the late judgment (judged) exists only while the take is live, and one
\*   open when its take is withdrawn closes (steps_tick.go TickDeadlines, LateStands).
\* Part "reads": one card's attempts under a reader's broken reads (rules.go, ruleReadBroken):
\*   the rule reworks it with the finding as the fix, on the same tier; a finding that names
\*   a file outside PATHS widens the card's PATHS in place by that file (the same card, never
\*   a twin), at most MaxWidens times a card, past which the brief is wrong and the card a
\*   mind's. Every attempt counts toward the cap, a widened brief's too (the owner,
\*   2026-10-12: "max attempts before you do judgement and decide what to do"): a card at
\*   its bound (the same finding twice, or Cap attempts since the coordinator's last
\*   decision) is a mind's whatever the finding, never the rule's, and the mind's decision
\*   (a rework, a re-brief, a re-tier) opens a fresh window of Cap attempts; a drop ends it.
\* Part "gate": the lander's base tree gate on one base commit: red or green each time
\*   it is gated; the third failure stops the stream.
\* Part "idle": the fleet idle or working each tick; the alarm once an episode after
\*   Window ticks, and the clear note when it recovers.
\* Part "answers": one card's attempts under the three answers that took the seat's hand
\*   loops (rules_read.go ruleReadBroken, harness_fault.go ruleHarness, steps_work.go
\*   lateFinish): a broken read with a finding reworks the card with it below the cap; at the
\*   cap (Cap attempts since the coordinator's last decision, a deadline's attempt counted as
\*   any other) the card is a mind's, whose rework is the decision that opens a fresh window
\*   (andec); a broken read with none is a mind's;
\*   work failed on a harness fault or a HOLD with findings is reworked with the failure, a
\*   harness fault at most once a card (the second identical one, or the same class on
\*   RuleSameFailureCards cards within the window, is judged, never redealt), any
\*   other failure is a mind's; a deadline fails an attempt while the worker's report is still
\*   on its way, and that report, a LAND or a HOLD, arriving while no later attempt has
\*   started, finishes the attempt. Every rework is one attempt more and starts from the
\*   head the attempt before pushed (a head here is its attempt's number).
\*
\* Part "episodes": a hard pin released past its bound on a running clock that stops
\*   while the machine is STOPPED (PinReleased reads the NPinReleased record the tick
\*   writes), the cannot-ask judgment once a wait, and a rule's answer consumed by the
\*   attempt it started (its own comment and reversed witnesses below, with the part).
\*
\* Part "judgments": the three rules that answer the judgments the tick raises
\*   (judgment_rules.go ruleFriendTake, ruleReadLate, ruleHoldNeed): a friend's card she
\*   has not started taken back and dealt again, a late read asked of another reader once
\*   an attempt, and a HOLD that waits for its card and reworks the primary once it lands.
\*
\* Part "stall": the landing stall (judgments.go TickLandingStall, run from the deadlines
\*   part): when cards wait in review or merging and the last landing is StallBound of
\*   running time ago, the tick end raises one NLandingStall judgment, re-raised each bound,
\*   cleared on a landing; stopped time does not count (lsclk is running time).
\*
\* Part "conflict": one card whose head the lander refused (internal/sprint rules.go,
\*   ruleConflict, ruleRedo, ruleBrief; redo.go Return). The card is on a tier (flash, pro,
\*   heavy; the failed and bound rules raised it, Part "rules"): the lander refuses its head
\*   one of the ways RefusalWay places (a file conflict, files outside PATHS, the lander's
\*   other checks, the tree gate, a merge clash) and the stream stops. The conflict rule
\*   returns the card and sets its tier to flash (FieldTierNow flash), recording the way;
\*   the redo reworks it on the current tip, one attempt more; the same way twice is a brief
\*   defect, marked and held for a mind. A refusal the rule cannot place (a generated
\*   ledger, a head origin does not hold) is a mind's, not modelled.
\*
\* Part "noroute": one card whose tier no route serves (internal/sprint seat_answers.go
\*   TickRuleNoRoute, RuleNoRoute). The card sits on a tier of the ladder (1 flash, 2 pro,
\*   3 heavy, 4 frontier or a friend's card); Served is the tiers a route or a friend up
\*   serves. An overdue no-route judgment (njudged, an outside event: the deal raised it
\*   when no route serves the tier) is what the rule answers: each ready primary it holds
\*   is raised to the nearest served tier above, pinned there (npinned) and the seat told
\*   in a note naming both tiers. Never down, so a heavy card never goes to flash; a
\*   pinned card (pinnedTier), a friend's card, a brief defect, a card not ready, or one
\*   with no served tier above stays held.
\*
\* Part "alert": the owner alert's send-once (cmd/nova-sprint seat_silent.go tellSeatSilent,
\*   seatSilentToken). A raise of the seat-not-answering judgment is sent to the owner on
\*   the bus and marked told only once its send succeeds; a failed send is retried at the
\*   next look under the same token, so a send whose response was lost, or a server
\*   restarted between a send and its mark, never puts a second message on the bus.
\*
\* Part "late" also has a reachability witness, MCSprintRulesLateReachReturn: the design
\* still returns a card (gen > 0), so NeverReturned fails there.
\*
\* Broken = "none" is the design. Every other value is a reversed witness, each broken
\* by one property:
\*   "norepoint"  replace drops and adds without re-pointing: TwinsInherit
\*   "silent"     replace drops raising no blocked judgment, without re-pointing:
\*                NoDanglingNeed (a waiting card needs a dropped card and no judgment
\*                says so)
\*   "nocap"      the failed rule redeals on the same tier for ever: RuleAttemptsBounded
\*   "down"       the bound rule may lower the tier: LadderClimbs
\*   "unservedtier" a rule climbs to the next tier whether a route serves it or not:
\*                EscalatesOnlyToServed
\*   "waitalways" the late rule waits whenever there was progress: WaitOnce
\*   "returnunstamped" the late rule returns a late card with no progress, stamped or not
\*                (the rule before the stamp existed): NeverStampedNeverReturned
\*   "returnearly" the late rule returns a stamped card before its holder's own deadline:
\*                HeldToItsOwnDeadline
\*   "withdrawnlate" the deadlines part raises a late judgment on a withdrawn take and keeps
\*                one open across the withdrawal (the code before v1.2.17, which raised
\*                "a work card is past its deadline ... at withdrawn" while its primary sat
\*                ready): LateOnlyWhileLive
\*   "stopfirst"  the base gate stops the stream on its first failure: BaseStopsOnThird
\*   "everytick"  the idle alarm is pushed every tick of an episode: AlarmOncePerEpisode
\*   "nobound"    the read-broken rule reworks a card at its brief's bound: ReadAnswersBounded
\*   "widenall"   the read-broken rule widens a card whose finding names no file outside
\*                PATHS: WidensWiden
\*   "widenresets"  the read-broken rule's widen resets the cap's count, as the brief edit it
\*                makes reset it until 2026-10-12: NoAttemptPastCapWithoutDecision
\*   "ruleatbound"  the read-broken rule reworks a card at its cap when the finding is inside
\*                PATHS, the rule of 2026-10-06 that outweighed the bound with the broken
\*                verdict: NoAttemptPastCapWithoutDecision
\*   "deadlinefree" a deadline's attempt does not count toward the cap, as the attempt record
\*                of 2026-10-11 counted only attempts whose worker reported:
\*                NoAttemptPastCapWithoutDecision
\*   "boundoutside" the read stops a finding naming a file outside PATHS at the brief's
\*                bound instead of letting the rule widen it: OutsideNeverBound
\*   "nowidencap" the rule widens on every finding outside PATHS, past MaxWidens:
\*                WidensBounded
\*   "nofinding"  the read-broken rule reworks a broken read that carries no finding:
\*                ReworkHasAFix
\*   "recount"    a rework starts its attempt without counting it: ReworkKeepsCount
\*   "nocarry"    a rework starts from the base, not the head the attempt before pushed:
\*                ReworkCarriesHead
\*   "stale"      finish refuses the report of an attempt the deadline failed, and the card
\*                goes round again: LateReportFinishes
\*   "faultnobound" the harness rule reworks a card at its attempt bound: AnswerAttemptsBounded
\*   "takestarted" the friend-take rule takes back a card the friend started:
\*                FriendKeepsStarted
\*   "conflictkeeps" the conflict rule leaves the tier a failure raised it to:
\*                ConflictResetsToFlash
\*   "answersmind" the read-broken answer leaves a non-empty finding at the brief's bound to a
\*                mind instead of reworking it: PartsAgree
\*   "parkfailed" failed work below its bound that no harness class names is left to the seat,
\*                who may park it (the wait loop of 2026-10-11, about 22 of 42 review cards
\*                frozen): FailedWithAFindingNeverParked
\*   "noreportrework" the failed rule reworks failed work that carries no report, with no fix:
\*                ReworkHasAFix
\*   "identicalrework" the second identical failure is reworked as failed work on its tier
\*                instead of stopping at the bound (rules.go AtIdenticalFailure): IdenticalIsBound
\*   "nostall"    the tick never raises the landing-stall judgment: StallLive
\*   "noclear"    a landing does not clear the episode: StallNotYoung
\*   "harnessredeal" the harness rule reworks the second identical harness fault a third time
\*                instead of judging it: HarnessFaultBounded
\*   "groupredeal" the harness rule redeals a card the fleet's grouped harness-fault judgment
\*                holds (the same harness-fault class on RuleSameFailureCards cards):
\*                HarnessFleetHeld
\*   "lowers"     the no-route rule moves a card to a weaker tier: NoRouteNeverLowers
\*   "pinnedraise" the no-route rule raises a card whose tier is pinned:
\*                NoRouteRaisesOnlyUnpinnedReady
\*   "stoppedcounts" the pin's running clock advances while the machine is STOPPED:
\*                PinClockSkipsStopped
\*   "toldbeforesend" the alert marks a raise told before its send succeeds:
\*                ToldOnlyAfterSend
\*   "newtoken"   the alert sends a fresh bus token each retry: NoSecondMessage
\*   "noquiet"    the tick raises a card or stream judgment while the tier rests:
\*                QuietWhileUnfunded
\*   "harnessvotes" the brief rule marks a card at its cap a brief defect whatever ended its
\*                attempts, harness faults included (the code before PR #273): MarkOnlyOnVotes

EXTENDS Integers, FiniteSets

CONSTANTS Part, Broken, Cap, MaxFails, MaxGen, Window, MaxClock, Served

None == "none"

\* -- twins
TwinIds == {"o", "t", "d1", "d2"}
Waiters == {"d1", "d2"}
Cols == {"absent", "waiting", "open", "landed", "dropped"}

\* -- reads: the files a finding may name outside the card's first PATHS, and findings
\* inside them (or naming no file)
RdFiles == {"f1", "f2", "f3", "f4"}

\* -- reads: the most times rules widen one card's PATHS in place (the code's
\* MaxWidens; 3 here, so a fourth file outside PATHS reaches the cap)
MaxWidens == 3
RdFindings == RdFiles \cup {"in1", "in2"}

\* -- every part's variables
VARIABLES col, need, blocked, twin,               \* twins
          tier, fails, st, attempts, envFails, fclass, same, bbound, \* rules
          gen, waited, progress, late, waits, lst, stamped, badReturn, own, badDeadline, judged, \* late
          rdst, rdatt, rdlast, rdfind, rdpaths, rdwidens, rdtotal, rdfixed, rdsince, rdminds, \* reads
          gfails, stopped,                         \* gate
          idle, since, said, alarms, clk, nalarm, nclear, \* idle
          anst, anatt, anreworks, anhead, ancarry, anfind, anfail, anlast, anland, anlost, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anvotes, anmarked, \* answers
          pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt, \* episodes
          jf, jfstarted, jr, jrasked, jh, jhlanded, \* judgments
          lsclk, lsstopped, lswait, lslast, lssince, lssaid, lsjudged, \* stall
          ctier, cst, crefused, cway, catt,         \* conflict
          ntier, nready, npinned, nfriend, nbrief, njudged, \* noroute
          alopen, almsg, altold,                      \* alert
          rest, card, tierJ, cardJ, staleJ            \* tier

twinVars == <<col, need, blocked, twin>>
ruleVars == <<tier, fails, st, attempts, envFails, fclass, same, bbound>>
lateVars == <<gen, waited, progress, late, waits, lst, stamped, badReturn, own, badDeadline, judged>>
rdVars == <<rdst, rdatt, rdlast, rdfind, rdpaths, rdwidens, rdtotal, rdfixed, rdsince, rdminds>>
gateVars == <<gfails, stopped>>
idleVars == <<idle, since, said, alarms, clk, nalarm, nclear>>
anVars == <<anst, anatt, anreworks, anhead, ancarry, anfind, anfail, anlast, anland, anlost, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anvotes, anmarked>>
epVars == <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>
judgVars == <<jf, jfstarted, jr, jrasked, jh, jhlanded>>
stallVars == <<lsclk, lsstopped, lswait, lslast, lssince, lssaid, lsjudged>>
conflictVars == <<ctier, cst, crefused, cway, catt>>
norouteVars == <<ntier, nready, npinned, nfriend, nbrief, njudged>>
alertVars == <<alopen, almsg, altold>>
tierVars == <<rest, card, tierJ, cardJ, staleJ>>
vars == <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>

\* -- answers: a reader's findings ("empty" is a broken verdict that carries none), and the
\* ways an attempt fails
AnFindings == {"f1", "f2", "empty"}
\* the ways work fails: a harness fault, a HOLD with findings, any other failure with a report,
\* a failure with no report to take as a fix, and the same failure as the attempt before
\* (sprint.AtIdenticalFailure: the bound's, never a rework on its tier)
AnFails == {"harness", "hold", "other", "noreport", "same"}

\* -- conflict: the ways RefusalWay places a refusal (rules.go)
ConflictWays == {"conflict", "paths", "checks", "gate", "clash"}

TypeOK ==
  /\ col \in [TwinIds -> Cols]
  /\ need \in [TwinIds -> SUBSET TwinIds]
  /\ blocked \in [TwinIds -> SUBSET TwinIds]
  /\ twin \in [TwinIds -> TwinIds \cup {None}]
  /\ tier \in 1..4
  /\ fails \in 0..Cap
  /\ st \in {"working", "failed", "bound", "done", "judged"}
  /\ attempts \in Nat
  /\ envFails \in 0..MaxFails
  /\ fclass \in {"budget", "other", None}
  /\ same \in BOOLEAN
  /\ bbound \in BOOLEAN
  /\ gen \in 0..MaxGen
  /\ waited \in (0..MaxGen) \cup {-1}
  /\ progress \in BOOLEAN
  /\ late \in BOOLEAN
  /\ waits \in 0..2
  /\ lst \in {"working", "withdrawn"}
  /\ stamped \in BOOLEAN
  /\ badReturn \in BOOLEAN
  /\ own \in 0..Window
  /\ badDeadline \in BOOLEAN
  /\ judged \in BOOLEAN
  /\ rdst \in {"working", "broken", "bound", "done", "judged"}
  /\ rdatt \in Nat
  /\ rdlast \in RdFindings \cup {None}
  /\ rdfind \in RdFindings \cup {None}
  /\ rdpaths \subseteq RdFiles
  /\ rdwidens \in Nat
  /\ rdtotal \in Nat
  /\ rdfixed \subseteq RdFindings
  /\ rdsince \in Nat /\ rdminds \in 0..MaxGen
  /\ gfails \in 0..3
  /\ stopped \in BOOLEAN
  /\ idle \in BOOLEAN
  /\ since \in (0..MaxClock) \cup {-1}
  /\ said \in BOOLEAN
  /\ alarms \in 0..MaxClock
  /\ clk \in 0..MaxClock
  /\ anst \in {"working", "broken", "failed", "bound", "done", "judged"}
  /\ anatt \in Nat /\ anreworks \in Nat /\ anhead \in Nat /\ ancarry \in Nat /\ andec \in Nat
  /\ anfind \in AnFindings \cup {None}
  /\ anfail \in AnFails \cup {None}
  /\ anlast \in AnFindings \cup {None}
  /\ anland \in BOOLEAN /\ anlost \in BOOLEAN /\ anfixless \in BOOLEAN
  /\ anhold \in BOOLEAN /\ anfixed \subseteq AnFindings /\ anharness \in Nat
  /\ anfleet \in 0..2 /\ anredealt \in BOOLEAN
  /\ anvotes \in Nat /\ anmarked \in BOOLEAN
  /\ pup \in BOOLEAN /\ pcol \in {"ready", "mine", "away"} /\ pwait \in (0..MaxClock) \cup {-1}
  /\ pign \in BOOLEAN /\ pdw \in (0..MaxClock) \cup {-1} /\ prec \in BOOLEAN /\ pstopped \in BOOLEAN
  /\ eclk \in 0..MaxClock
  /\ ast \in {"waits", "asked", "gone"} /\ aatt \in 1..Cap /\ areason \in {"noreader", "room"}
  /\ aflapped \in BOOLEAN
  /\ aopen \in BOOLEAN /\ aseen \in BOOLEAN /\ anotes \in 0..MaxClock
  /\ rcol \in {"review", "ready", "working"} /\ rans \in BOOLEAN /\ ratt \in 1..Cap
  /\ jf \in {"open", "taken"} /\ jfstarted \in BOOLEAN
  /\ jr \in {"open", "asked", "mind"} /\ jrasked \in Nat
  /\ jh \in {"waiting", "off", "reworked", "mind"} /\ jhlanded \in BOOLEAN
  /\ lsclk \in 0..MaxClock
  /\ lsstopped \in BOOLEAN
  /\ lswait \in 0..2
  /\ lslast \in (0..MaxClock) \cup {-1}
  /\ lssince \in (0..MaxClock) \cup {-1}
  /\ lssaid \in (0..MaxClock) \cup {-1}
  /\ lsjudged \in BOOLEAN
  /\ ctier \in 1..3 /\ cst \in {"working", "refused", "returned", "brief", "done"}
  /\ crefused \in ConflictWays \cup {None} /\ cway \in ConflictWays \cup {None}
  /\ catt \in 1..Cap
  /\ ntier \in 1..4 /\ nready \in BOOLEAN /\ npinned \in BOOLEAN /\ nfriend \in BOOLEAN
  /\ nbrief \in BOOLEAN /\ njudged \in BOOLEAN
  /\ alopen \in BOOLEAN /\ almsg \in 0..2 /\ altold \in BOOLEAN
  /\ rest \in BOOLEAN
  /\ card \in {"ready", "bound", "working"}
  /\ tierJ \in BOOLEAN
  /\ cardJ \in BOOLEAN
  /\ staleJ \in BOOLEAN

Init ==
  /\ col = [x \in TwinIds |-> CASE x = "o" -> "open" [] x = "t" -> "absent" [] OTHER -> "waiting"]
  /\ need = [x \in TwinIds |-> IF x \in Waiters THEN {"o"} ELSE {}]
  /\ blocked = [x \in TwinIds |-> {}]
  /\ twin = [x \in TwinIds |-> None]
  /\ tier = 1 /\ fails = 0 /\ st = "working" /\ attempts = 0 /\ envFails = 0
  /\ fclass = None /\ same = FALSE /\ bbound = FALSE
  /\ gen = 0 /\ waited = -1 /\ progress = FALSE /\ late = FALSE /\ waits = 0 /\ lst = "working"
  /\ stamped = FALSE /\ badReturn = FALSE /\ own = 0 /\ badDeadline = FALSE /\ judged = FALSE
  /\ rdst = "working" /\ rdatt = 1 /\ rdlast = None /\ rdfind = None /\ rdpaths = {}
  /\ rdwidens = 0 /\ rdtotal = 0 /\ rdfixed = {} /\ rdsince = 1 /\ rdminds = 0
  /\ gfails = 0 /\ stopped = FALSE
  /\ idle = FALSE /\ since = -1 /\ said = FALSE /\ alarms = 0 /\ clk = 0 /\ nalarm = 0 /\ nclear = 0
  /\ anst = "working" /\ anatt = 1 /\ anreworks = 0 /\ anhead = 0 /\ ancarry = 0
  /\ anfind = None /\ anfail = None /\ anlast = None
  /\ anland = FALSE /\ anlost = FALSE /\ anfixless = FALSE
  /\ anhold = FALSE /\ anfixed = {} /\ anharness = 0 /\ anfleet = 0 /\ anredealt = FALSE /\ andec = 0
  /\ anvotes = 0 /\ anmarked = FALSE
  /\ pup = FALSE /\ pcol = "ready" /\ pwait = -1 /\ pign = FALSE /\ pdw = -1 /\ prec = FALSE /\ pstopped = FALSE
  /\ eclk = 0
  /\ ast = "waits" /\ aatt = 1 /\ areason = "noreader" /\ aflapped = FALSE /\ aopen = FALSE /\ aseen = FALSE /\ anotes = 0
  /\ rcol = "review" /\ rans = FALSE /\ ratt = 1
  /\ jf = "open" /\ jfstarted = FALSE
  /\ jr = "open" /\ jrasked = 0
  /\ jh = "waiting" /\ jhlanded = FALSE
  /\ lsclk = 0 /\ lsstopped = FALSE /\ lswait = 1
  /\ lslast = -1 /\ lssince = -1 /\ lssaid = -1 /\ lsjudged = FALSE
  /\ ctier = 1 /\ cst = "working" /\ crefused = None /\ cway = None /\ catt = 1
  /\ ntier \in 1..3 /\ nready = TRUE /\ npinned = FALSE /\ nfriend = FALSE /\ nbrief = FALSE /\ njudged = FALSE
  /\ alopen = FALSE /\ almsg = 0 /\ altold = FALSE
  /\ rest = FALSE /\ card = "ready" /\ tierJ = FALSE /\ cardJ = FALSE /\ staleJ = FALSE

-----------------------------------------------------------------------------
\* Part "twins".

\* The waiting cards that need x and would be re-pointed.
Dependents(x) == {d \in Waiters : col[d] = "waiting" /\ x \in need[d]}

\* drop x: off the table; every waiting card that needs it is told (Drop's blocked
\* judgment, one per card, naming the dropped need)
DropOld ==
  /\ col["o"] \in {"open", "waiting"}
  /\ col' = [col EXCEPT !["o"] = "dropped"]
  /\ blocked' = [d \in TwinIds |-> IF d \in Dependents("o") THEN blocked[d] \cup {"o"} ELSE blocked[d]]
  /\ UNCHANGED <<need, twin>>

AddTwin ==
  /\ col["t"] = "absent"
  /\ col' = [col EXCEPT !["t"] = "open"]
  /\ UNCHANGED <<need, blocked, twin>>

\* every dependent of o needs t in o's place, and its judgments that named o are answered
Repoint(n) ==
  /\ need' = [d \in TwinIds |-> IF d \in Dependents("o") THEN (need[d] \ {"o"}) \cup {n} ELSE need[d]]
  /\ blocked' = [d \in TwinIds |-> IF d \in Dependents("o") THEN blocked[d] \ {"o"} ELSE blocked[d]]

\* add --replaces o: one step
Replace ==
  /\ col["t"] = "absent"
  /\ col["o"] \in {"open", "waiting", "dropped"}
  /\ twin' = [twin EXCEPT !["o"] = "t"]
  /\ IF Broken = "norepoint"
       THEN \* the drop and the add, the edges left on o: the drop's judgments raised
            /\ col' = [col EXCEPT !["t"] = "open", !["o"] = "dropped"]
            /\ blocked' = [d \in TwinIds |-> IF d \in Dependents("o") /\ col["o"] # "dropped" THEN blocked[d] \cup {"o"} ELSE blocked[d]]
            /\ UNCHANGED need
     ELSE IF Broken = "silent"
       THEN \* the drop told no one, and nothing was re-pointed
            /\ col' = [col EXCEPT !["t"] = "open", !["o"] = "dropped"]
            /\ UNCHANGED <<need, blocked>>
     ELSE /\ col' = [col EXCEPT !["t"] = "open", !["o"] = "dropped"]
          /\ Repoint("t")

\* relink o t: the repair of a drop and an add made apart
Relink ==
  /\ col["o"] = "dropped"
  /\ col["t"] \in {"open", "landed"}
  /\ Dependents("o") # {}
  /\ twin' = [twin EXCEPT !["o"] = "t"]
  /\ Repoint("t")
  /\ UNCHANGED col

LandTwin(x) ==
  /\ x \in {"o", "t"}
  /\ col[x] = "open"
  /\ col' = [col EXCEPT ![x] = "landed"]
  /\ UNCHANGED <<need, blocked, twin>>

Resolve(d) ==
  /\ d \in Waiters
  /\ col[d] = "waiting"
  /\ \A x \in need[d] : col[x] = "landed"
  /\ col' = [col EXCEPT ![d] = "open"]
  /\ UNCHANGED <<need, blocked, twin>>

TwinNext ==
  \/ DropOld \/ AddTwin \/ Replace \/ Relink
  \/ \E x \in TwinIds : LandTwin(x) \/ Resolve(x)

\* No waiting card needs an id that has a twin: the twin took over every edge.
TwinsInherit ==
  \A x \in TwinIds : twin[x] # None => \A d \in Waiters : col[d] = "waiting" => x \notin need[d]

\* No blocked judgment names a replaced card.
NoBlockedForReplaced ==
  \A x \in TwinIds : twin[x] # None => \A d \in TwinIds : x \notin blocked[d]

\* A waiting card that needs a dropped card has a judgment that says so: nothing waits
\* in silence on a card that will never land.
NoDanglingNeed ==
  \A d \in Waiters : col[d] = "waiting" => \A x \in need[d] : col[x] = "dropped" => x \in blocked[d]

-----------------------------------------------------------------------------
\* Part "rules": one card's attempts.

\* the classes a failed or bound attempt can carry: budget (a prompt's budget
\* exhausted, the cost rule), or any other failure
FailureClasses == {"budget", "other"}

\* the tier a rule escalates to: the first served tier of the ladder above (a route
\* serves it, Served), skipping tiers no route serves, and a friend's card (4) past the
\* last served tier. The unservedtier defect climbs to tier+1 whatever its route.
NextTier(t) ==
  IF Broken = "unservedtier" THEN t + 1
  ELSE IF \E s \in Served : s > t
       THEN CHOOSE s \in Served : s > t /\ \A u \in Served : u > t => u >= s
       ELSE 4

\* the outside: an attempt comes back failed (its class, and whether the fleet holds the
\* same failure on three cards, and whether the card is at its brief's bound), reaches its
\* bound, or lands
WorkFails ==
  /\ st = "working" /\ envFails < MaxFails
  /\ st' = "failed" /\ envFails' = envFails + 1
  /\ fclass' \in FailureClasses /\ same' \in BOOLEAN /\ bbound' \in BOOLEAN
  /\ UNCHANGED <<tier, fails, attempts>>
WorkBounds ==
  /\ st = "working" /\ envFails < MaxFails
  /\ st' = "bound" /\ envFails' = envFails + 1
  /\ fclass' \in FailureClasses /\ same' \in BOOLEAN /\ bbound' \in BOOLEAN
  /\ UNCHANGED <<tier, fails, attempts>>
WorkLands ==
  /\ st = "working"
  /\ st' = "done"
  /\ UNCHANGED <<tier, fails, attempts, envFails, fclass, same, bbound>>

\* the failed rule: below its brief's bound, and not the fleet's failure (the same
\* failure on three cards; a budget exhaustion follows tier escalation instead), the next
\* route on the tier, the Cap-th failure a tier up
RuleFailed ==
  /\ st = "failed" /\ tier <= 3
  /\ ~bbound
  /\ ~same \/ fclass = "budget"
  /\ IF fails + 1 < Cap \/ Broken = "nocap"
       THEN /\ fails' = IF Broken = "nocap" THEN fails ELSE fails + 1
            /\ UNCHANGED tier
       ELSE /\ tier' = NextTier(tier) /\ fails' = 0
  /\ st' = "working" /\ attempts' = attempts + 1
  /\ fclass' = None
  /\ UNCHANGED <<envFails, same, bbound>>

\* the bound rule: below its brief's bound, and not the fleet's failure except a budget
\* exhaustion at its second attempt (which escalates), a tier up (heavy's next is a
\* friend's card)
RuleBound ==
  /\ st = "bound" /\ tier <= 3
  /\ ~bbound
  /\ ~same \/ (fclass = "budget" /\ attempts = 1)
  /\ tier' = IF Broken = "down" /\ tier > 1 THEN tier - 1 ELSE NextTier(tier)
  /\ fails' = 0 /\ st' = "working" /\ attempts' = attempts + 1
  /\ fclass' = None
  /\ UNCHANGED <<envFails, same, bbound>>

\* a friend's card, a card at its brief's bound, or the fleet's failure is answered by a
\* mind, never by rule
Mind ==
  /\ st \in {"failed", "bound"}
  /\ \/ tier = 4
     \/ bbound
     \/ st = "failed" /\ same /\ fclass # "budget"
     \/ st = "bound" /\ same /\ ~(fclass = "budget" /\ attempts = 1)
  /\ st' = "judged"
  /\ UNCHANGED <<tier, fails, attempts, envFails, fclass, same, bbound>>

RuleNext == WorkFails \/ WorkBounds \/ WorkLands \/ RuleFailed \/ RuleBound \/ Mind

\* The rules make at most Cap attempts a tier on three tiers: they never loop.
RuleAttemptsBounded == attempts <= 3 * Cap

\* No rule but the conflict rule lowers a card's tier: the failed and bound rules here
\* only raise it (Part "conflict" is the one rule that resets a card to flash).
LadderClimbs == [][tier' >= tier]_ruleVars

\* A rule escalates only to a tier a route serves, or to a friend's card.
EscalatesOnlyToServed == [][tier' = tier \/ tier' \in Served \/ tier' = 4]_ruleVars

\* Every failure or bound is answered: by rule below a friend's card, by a mind there.
Answered == (st \in {"failed", "bound"}) ~> (st \notin {"failed", "bound"})

-----------------------------------------------------------------------------
\* Part "late": one work card past its deadline (internal/sprint rules.go, ruleLate).

\* the deadlines part raises the late judgment (steps_tick.go TickDeadlines, N4): only on a
\* live take (LateOnlyWhileLive); "withdrawnlate" raises it on a withdrawn one too
GoLate ==
  /\ (lst = "working" \/ Broken = "withdrawnlate") /\ ~late
  /\ late' = TRUE /\ judged' = TRUE
  /\ UNCHANGED <<gen, waited, progress, waits, lst, stamped, badReturn, own, badDeadline>>

\* the holder's `progress` verb (the member while its child prints, the friend daemon while a
\* lane's turn prints): a fresh stamp, at the server's time, on the card it works
Stamp ==
  /\ lst = "working"
  /\ stamped' = TRUE /\ progress' = TRUE
  /\ UNCHANGED <<gen, waited, late, waits, lst, badReturn, own, badDeadline, judged>>

\* the holder goes silent: its last stamp ages past RuleProgressWindow
Silence ==
  /\ progress
  /\ progress' = FALSE
  /\ UNCHANGED <<gen, waited, late, waits, lst, stamped, badReturn, own, badDeadline, judged>>

\* the holder's own deadline passes: own counts its time, Window is its deadline
OwnTick ==
  /\ lst = "working" /\ own < Window
  /\ own' = own + 1
  /\ UNCHANGED <<gen, waited, progress, late, waits, lst, stamped, badReturn, badDeadline, judged>>

\* the late rule: a wait once a generation with progress; a card whose holder stamped and went
\* silent, or whose one wait is spent, returned and redealt, once its holder has had its own
\* whole deadline (own = Window) or its one wait is spent; a card whose holder never stamped
\* this take held (the default, wait only: the judgment closed, raised again later). Every
\* answer closes the judgment.
RuleLate ==
  /\ late
  /\ judged' = FALSE
  /\ IF progress /\ (waited # gen \/ Broken = "waitalways")
       THEN /\ waited' = gen /\ waits' = waits + 1 /\ late' = FALSE
            /\ UNCHANGED <<gen, lst, progress, stamped, badReturn, own, badDeadline>>
     ELSE IF (stamped \/ waited = gen \/ Broken = "returnunstamped")
                /\ (own >= Window \/ waited = gen \/ Broken = "returnearly")
       THEN /\ gen < MaxGen
            /\ gen' = gen + 1 /\ waits' = 0 /\ late' = FALSE /\ lst' = "withdrawn"
            /\ badReturn' = (badReturn \/ ~stamped)
            /\ badDeadline' = (badDeadline \/ (own < Window /\ waited # gen))
            /\ stamped' = FALSE /\ progress' = FALSE \* the next take's holder has stamped nothing
            /\ UNCHANGED <<waited, own>>
     ELSE /\ late' = FALSE
          /\ UNCHANGED <<gen, waited, progress, waits, lst, stamped, badReturn, own, badDeadline>>

\* an outside event: the take is withdrawn (the provider failed it, or the environment exited
\* 134), the work card to withdrawn and its primary back to ready, with no attempt spent (gen
\* and waits unchanged). The tick closes a late judgment open on it (LateStands: a withdrawn
\* work card never stands); "withdrawnlate" keeps it open.
Withdraw ==
  /\ lst = "working"
  /\ lst' = "withdrawn"
  /\ IF Broken = "withdrawnlate"
       THEN UNCHANGED <<late, judged>>
       ELSE late' = FALSE /\ judged' = FALSE
  /\ UNCHANGED <<gen, waited, progress, waits, stamped, badReturn, own, badDeadline>>

Redeal ==
  /\ lst = "withdrawn"
  /\ lst' = "working" /\ own' = 0
  /\ UNCHANGED <<gen, waited, progress, late, waits, stamped, badReturn, badDeadline, judged>>

LateNext == (GoLate \/ Stamp \/ Silence \/ OwnTick \/ RuleLate \/ Withdraw \/ Redeal) /\ waits <= 1

\* One wait a generation, whatever the progress.
WaitOnce == waits <= 1

\* A card whose holder never stamped is never returned by the late rule.
NeverStampedNeverReturned == ~badReturn

\* A stamped card is not returned before its holder's own whole deadline (or the late
\* rule's one wait is spent).
HeldToItsOwnDeadline == ~badDeadline

\* Progress is a stamp of this take's holder.
ProgressIsStamped == progress => stamped

\* A late judgment is open only while its take is live (steps_tick.go TickDeadlines N4 and
\* LateStands): a withdrawn take raises none, and one open when its take is withdrawn closes.
LateOnlyWhileLive == judged => lst = "working"

\* The reachability witness's invariant: no card is ever returned. The design returns a card
\* whose holder stamped and went silent, so TLC's counterexample is the proof it is reached.
NeverReturned == gen = 0

-----------------------------------------------------------------------------
\* Part "gate": one base commit gated again and again.

GateRed ==
  /\ ~stopped
  /\ gfails' = gfails + 1
  /\ stopped' = (gfails + 1 >= 3 \/ Broken = "stopfirst")
GateGreen ==
  /\ ~stopped /\ gfails < 3
  /\ gfails' = 0
  /\ UNCHANGED stopped

GateNext == GateRed \/ GateGreen

\* The stream stops only on the base's third failure in a row.
BaseStopsOnThird == stopped => gfails >= 3

-----------------------------------------------------------------------------
\* Part "idle": the alarm once an episode, the clear once it recovers.

Flip ==
  /\ idle' = ~idle
  /\ UNCHANGED <<since, said, alarms, clk, nalarm, nclear>>

IdleTick ==
  /\ clk < MaxClock
  /\ clk' = clk + 1
  /\ IF idle
       THEN IF since = -1
              THEN /\ since' = clk /\ UNCHANGED <<said, alarms, nalarm, nclear>>
            ELSE IF clk - since >= Window /\ (~said \/ Broken = "everytick")
              THEN /\ said' = TRUE /\ alarms' = alarms + 1 /\ nalarm' = nalarm + 1
                   /\ UNCHANGED <<since, nclear>>
            ELSE UNCHANGED <<since, said, alarms, nalarm, nclear>>
       ELSE IF since # -1
              THEN /\ since' = -1 /\ said' = FALSE /\ alarms' = 0
                   /\ nclear' = IF said THEN nclear + 1 ELSE nclear
                   /\ UNCHANGED nalarm
            ELSE UNCHANGED <<since, said, alarms, nalarm, nclear>>
  /\ UNCHANGED idle

IdleNext == Flip \/ IdleTick

\* One alarm an episode.
AlarmOncePerEpisode == alarms <= 1

\* A clear note follows an alarm: never more clears than alarms, never a clear for an
\* episode that raised none.
ClearFollowsAlarm == nclear <= nalarm

-----------------------------------------------------------------------------
\* Part "reads": one card's attempts under a reader's broken reads (internal/sprint rules.go,
\* ruleReadBroken; brief_bound.go, AtBriefBound; steps_review.go, Read).

\* the finding names a file outside the card's PATHS
RdOutside(f) == f \in RdFiles /\ f \notin rdpaths

\* the card's brief is at its bound for finding f: the same finding as the attempt before, or
\* Cap attempts since the coordinator's last decision (rdatt is the code's count, attempt less
\* decided_attempt: brief_bound.go AttemptsSince; rdsince is the truth the invariant reads)
RdAtBound(f) == f = rdlast \/ rdatt >= Cap

\* the finding names a file outside PATHS and the card's widenings are under the cap: the rule
\* may widen it in place (a widening is never reset by the brief it writes)
RdWidenable(f) == RdOutside(f) /\ (rdwidens < MaxWidens \/ Broken = "nowidencap")

\* the outside: a reader finds the attempt broken with finding f (Read writes the brief's
\* bound judgment instead at the bound, whatever f names: steps_review.go briefStopAt; and for
\* a file outside PATHS past the widen cap, or below the bound for the boundoutside defect), a
\* finding whose fix already stands is not read again (it is in rdfixed), or the attempt lands
RdReadBroken(f) ==
  /\ rdst = "working"
  /\ f \notin rdfixed
  /\ rdfind' = f
  /\ rdst' = IF Broken # "nobound"
                  /\ \/ RdAtBound(f)
                     \/ RdOutside(f) /\ ~RdWidenable(f)
                     \/ RdOutside(f) /\ Broken = "boundoutside"
                THEN "bound" ELSE "broken"
  /\ UNCHANGED <<rdatt, rdlast, rdpaths, rdwidens, rdtotal, rdfixed, rdsince, rdminds>>
RdLands ==
  /\ rdst = "working"
  /\ rdst' = "done"
  /\ UNCHANGED <<rdatt, rdlast, rdfind, rdpaths, rdwidens, rdtotal, rdfixed, rdsince, rdminds>>

\* the rule, below the bound: a finding naming a file outside PATHS widens the card's PATHS in
\* place by it (a machine's act: the cap's count stands, BriefReq.ByRule; the widenresets
\* defect resets it as the brief edit did until 2026-10-12); any other finding is the fix of a
\* rework on the same tier. The ruleatbound defect reworks an inside finding at the bound too
\* (rules_read.go until 2026-10-12: the broken verdict outweighed the cap).
RuleReadBroken ==
  /\ \/ rdst = "broken"
     \/ rdst = "bound" /\ ~RdOutside(rdfind) /\ Broken = "ruleatbound"
  /\ IF RdWidenable(rdfind) \/ Broken = "widenall"
       THEN /\ rdpaths' = rdpaths \cup ({rdfind} \cap RdFiles)
            /\ rdatt' = IF Broken = "widenresets" THEN 1 ELSE rdatt + 1
            /\ rdlast' = None /\ rdwidens' = rdwidens + 1
       ELSE /\ rdatt' = rdatt + 1 /\ rdlast' = rdfind
            /\ UNCHANGED <<rdpaths, rdwidens>>
  /\ rdst' = "working" /\ rdtotal' = rdtotal + 1 /\ rdsince' = rdsince + 1
  /\ UNCHANGED <<rdfind, rdfixed, rdminds>>

\* a card at its bound is the coordinator's: the decision (brief, rework --tier, rework
\* --carry, by a mind, steps_edit.go and steps_review.go) opens a fresh window of Cap
\* attempts (decided_attempt = attempt: rdatt' = 1 for the attempt it starts, rdsince' = 1),
\* the finding marked fixed so it is not read again; or the drop ends the card. At most
\* MaxGen decisions, so the model is finite.
RdMind ==
  /\ rdst = "bound"
  /\ \/ /\ rdminds < MaxGen
        /\ rdst' = "working" /\ rdfixed' = rdfixed \cup {rdfind}
        /\ rdatt' = 1 /\ rdsince' = 1 /\ rdminds' = rdminds + 1
     \/ /\ rdst' = "judged"
        /\ UNCHANGED <<rdfixed, rdatt, rdsince, rdminds>>
  /\ UNCHANGED <<rdlast, rdfind, rdpaths, rdwidens, rdtotal>>

RdNext == RdLands \/ RuleReadBroken \/ RdMind \/ \E f \in RdFindings : RdReadBroken(f)

\* The rule's answers never loop: under Cap attempts between two decisions.
ReadAnswersBounded == rdtotal <= Cap * (rdminds + 1)

\* Every widening widens PATHS by a file.
WidensWiden == rdwidens <= Cardinality(rdpaths)

\* Below the bound, a finding naming a file outside PATHS is never stopped at the brief's
\* bound while the card's widenings are under the cap: the rule widens the brief in place.
OutsideNeverBound == rdst = "bound" /\ RdOutside(rdfind) /\ ~RdAtBound(rdfind) => rdwidens >= MaxWidens

\* THE ATTEMPT CAP (the owner, 2026-10-12: "11/22 attempts is tooooo many"): no card runs more
\* than Cap attempts without a coordinator decision in between, in the reads part (rdsince:
\* attempts since the mind last decided) and the answers part (anatt - andec: the attempt less
\* the attempt at the last decision) alike. brief_bound.go AttemptsSince, AtBriefBound.
NoAttemptPastCapWithoutDecision == rdsince <= Cap /\ anatt - andec <= Cap

\* Readers' findings widen one card's PATHS at most MaxWidens times: a reader naming a new
\* file outside PATHS every round never widens it for ever.
WidensBounded == rdwidens <= MaxWidens

\* Every broken read is answered: by rule below the bound, by a mind at it.
ReadAnswered == (rdst \in {"broken", "bound"}) ~> (rdst \notin {"broken", "bound"})

-----------------------------------------------------------------------------
\* Part "answers": one card's attempts under the broken-read, harness-fault and late-report
\* answers (internal/sprint rules_read.go, harness_fault.go, steps_work.go lateFinish).

\* the card's brief is at its bound for finding f: the same finding as the attempt before, or
\* Cap attempts since the coordinator's last decision (andec: the attempt at that decision)
AnAtBound(f) == (f # "empty" /\ f = anlast) \/ anatt - andec >= Cap

\* THE VOTES (PR #273, v1.2.26: "a harness fault never counts toward the brief-defect bound";
\* internal/sprint brief_bound.go harnessEnds and AtBriefBound's Harness, rules.go classifyEnd,
\* ruleBrief and harnessAtCap). Every attempt counts toward the cap (anatt - andec), but only a
\* worker's or the brief's end is a vote that the brief is wrong (anvotes, since andec): a
\* broken read, a HOLD or any other failure the worker reported. A harness end (EndHarness:
\* the worker never ran, ran=false in FieldRanAttempts; here a harness fault and both deadline
\* ends) is an attempt and never a vote. harnessEnds keys on ran alone, so a failure with no
\* report is split as the code splits it: its worker ran (a vote) or did not (no vote).
\* AnVotes(k, ran) is whether the attempt's end k is a vote: a harness fault never ran, a
\* no-report end is ran's, every other end (a HOLD, another failure, the same failure) ran.
AnVotes(k, ran) ==
  CASE k = "harness" -> FALSE
    [] k = "noreport" -> ran
    [] OTHER -> TRUE

\* the brief's own bounds, votes by themselves: the same finding as the attempt before (the
\* same-finding-twice bound) or the same failure twice (AtIdenticalFailure, and the lander's
\* same refusal way twice)
AnSameBound == (anfind \notin {None, "empty"} /\ anfind = anlast) \/ anfail = "same"

\* the window's ends that were the harness's: every attempt since the decision less the votes
AnHarnessCount == (anatt - andec) - anvotes

\* ruleBrief marks the card a brief defect at its bound unless the cap was reached mostly on
\* harness ends (harnessAtCap: harness > since - harness, worded "harness fault ... the brief
\* is not the cause", offered rework --carry or drop, never marked); the harnessvotes defect is
\* the code before PR #273, which marked on the cap alone
AnMarks ==
  \/ AnSameBound
  \/ anvotes >= AnHarnessCount
  \/ Broken = "harnessvotes" /\ anatt - andec >= Cap

\* the brief rule marks a card at its bound a brief defect, once a window; the card stays at
\* its bound for the mind (brief, rework, drop), whose decision opens a fresh window
AnMark ==
  /\ anst = "bound" /\ ~anmarked
  /\ AnMarks
  /\ anmarked' = TRUE
  /\ UNCHANGED <<anst, anatt, anreworks, anhead, ancarry, anfind, anfail, anlast, anland, anlost, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anvotes>>

\* the outside: a reader finds the attempt broken with finding f (a finding whose fix already
\* stands is not read again; at the bound the read raises the brief's judgment, which the mind
\* then reworks); the attempt pushed its head
AnReadBroken(f) ==
  /\ anst = "working"
  /\ f \notin anfixed
  /\ anhead' = anatt /\ anfind' = f
  /\ anst' = IF f # "empty" /\ AnAtBound(f) THEN "bound" ELSE "broken"
  /\ anvotes' = anvotes + 1  \* the worker ran and a reader read it: a vote
  /\ UNCHANGED <<anatt, anreworks, ancarry, anfail, anlast, anland, anlost, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anmarked>>

\* the outside: the attempt comes back failed the way k (at the attempt cap the finish raises
\* the brief's judgment instead); ran is whether its worker ran (FieldRanAttempts), free only
\* for a no-report end
AnWorkFails(k, ran) ==
  /\ anst = "working"
  /\ k = "same" => anreworks > 0
  /\ k # "noreport" => ran = (k # "harness")
  /\ anhead' = anatt /\ anfail' = k
  /\ anst' = IF anatt - andec >= Cap \/ (k = "same" /\ Broken # "identicalrework") THEN "bound" ELSE "failed"
  /\ anvotes' = IF AnVotes(k, ran) THEN anvotes + 1 ELSE anvotes
  /\ UNCHANGED <<anatt, anreworks, ancarry, anfind, anlast, anland, anlost, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anmarked>>

\* the outside: the deadline fails the attempt (a harness fault) while the worker's LAND is
\* still on its way; its attempt counts toward the cap as any other (the deadlinefree defect
\* is the attempt record of 2026-10-11, which counted only attempts whose worker reported)
AnDeadline ==
  /\ anst = "working" /\ ~anland
  /\ anhead' = anatt /\ anfail' = "harness" /\ anland' = TRUE
  /\ anst' = IF anatt - andec >= Cap /\ Broken # "deadlinefree" THEN "bound" ELSE "failed"
  \* a deadline's end is the harness's (no worker report): an attempt, never a vote
  /\ UNCHANGED <<anatt, anreworks, ancarry, anfind, anlast, anlost, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anvotes, anmarked>>

\* the outside: the deadline fails the attempt (a harness fault) while the worker's HOLD is
\* still on its way
AnDeadlineHold ==
  /\ anst = "working" /\ ~anhold
  /\ anhead' = anatt /\ anfail' = "hold" /\ anhold' = TRUE
  /\ anst' = IF anatt - andec >= Cap /\ Broken # "deadlinefree" THEN "bound" ELSE "failed"
  \* a deadline's end is the harness's (no worker report): an attempt, never a vote
  /\ UNCHANGED <<anatt, anreworks, ancarry, anfind, anlast, anland, anlost, anfixless, anfixed, anharness, anfleet, anredealt, andec, anvotes, anmarked>>

\* the outside: the worker's late LAND arrives. While its attempt is failed and no later
\* attempt has started, finish takes it: the attempt is done. Once a later attempt has
\* started it is refused, the old attempt staying failed.
AnLateLand ==
  /\ anland
  /\ anland' = FALSE
  /\ IF anst \in {"failed", "bound"}
       THEN IF Broken = "stale"
              THEN /\ anlost' = TRUE
                   /\ UNCHANGED <<anst, anfail>>
              ELSE /\ anst' = "done" /\ anfail' = None
                   /\ UNCHANGED anlost
       ELSE UNCHANGED <<anst, anfail, anlost>>
  /\ UNCHANGED <<anatt, anreworks, anhead, ancarry, anfind, anlast, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anvotes, anmarked>>

\* the outside: the worker's late HOLD arrives; it finishes the attempt a late LAND does
AnLateHold ==
  /\ anhold
  /\ anhold' = FALSE
  /\ IF anst \in {"failed", "bound"}
       THEN IF Broken = "stale"
              THEN /\ anlost' = TRUE
                   /\ UNCHANGED <<anst, anfail>>
              ELSE /\ anst' = "done" /\ anfail' = None
                   /\ UNCHANGED anlost
       ELSE UNCHANGED <<anst, anfail, anlost>>
  /\ UNCHANGED <<anatt, anreworks, anhead, ancarry, anfind, anlast, anland, anfixless, anfixed, anharness, anfleet, anredealt, andec, anvotes, anmarked>>

\* a rework with fix f ("empty": no fix): one attempt more, from the head the attempt before
\* pushed; a later attempt started, so a late report still on its way is for the attempt the
\* rework ended and is refused (anland, anhold cleared), never finishing the new attempt
AnRework(f) ==
  /\ anreworks < MaxFails
  /\ anatt' = IF Broken = "recount" THEN anatt ELSE anatt + 1
  /\ anreworks' = anreworks + 1
  /\ ancarry' = IF Broken = "nocarry" THEN 0 ELSE anhead
  /\ anfixless' = (anfixless \/ f = "empty")
  /\ anst' = "working" /\ anfind' = None /\ anfail' = None
  /\ anland' = FALSE /\ anhold' = FALSE
  /\ UNCHANGED <<anhead, anlost, anfixed, andec, anvotes, anmarked>>  \* a rule's rework is no decision: andec and the window's votes stand

\* the read-broken rule: a broken read with a finding reworks the card with it
RuleAnBroken ==
  /\ anst = "broken"
  /\ anfind # "empty" \/ Broken = "nofinding"
  /\ AnRework(anfind)
  /\ anlast' = anfind
  /\ anharness' = anharness
  /\ anfleet' = anfleet /\ anredealt' = anredealt

\* the failed rule: a harness fault, or a HOLD with findings, reworks the card with the
\* failure as its fix. A harness fault is reworked on the same tier at most once per card
\* (HarnessFaultBounded): the second identical one is judged, never redealt a third time; and
\* the fleet's grouped judgment holds a card whose class RuleSameFailureCards cards hold
\* (HarnessFleetHeld): the groupredeal defect redeals it anyway.
RuleAnHarness ==
  /\ \/ anst = "failed"
     \/ Broken = "faultnobound" /\ anst = "bound"
  /\ anfail \in {"harness", "hold"}
  \* the fleet's grouped one is judged, never reworked (the groupredeal defect redeals it)
  /\ anfail = "hold" \/ anfleet < 2 \/ Broken = "groupredeal"
  /\ anfail = "hold" \/ anharness = 0 \/ Broken = "harnessredeal"
  /\ AnRework(anfail)
  /\ anharness' = IF anfail = "harness" THEN anharness + 1 ELSE anharness
  /\ anredealt' = (anredealt \/ (anfail = "harness" /\ anfleet = 2))
  /\ anfleet' = anfleet
  /\ UNCHANGED anlast

\* the failed rule on any other failure below the bound (rules.go ruleFailed; the owner,
\* 2026-10-11: "i would very much like it if this was not possible to happen again"): the
\* report is the fix, the next attempt redealt, a tier up, or on its tier where the ladder has
\* no rung or the failure is the fleet's; never left to the seat, who could park it with a wait
\* (wait_refused.go: wait is refused on failed work and broken reads)
RuleAnFailed ==
  /\ anst = "failed" /\ Broken # "parkfailed"
  /\ \/ anfail = "other"
     \/ anfail = "noreport" /\ Broken = "noreportrework"
     \/ anfail = "same" /\ Broken = "identicalrework"
  /\ AnRework(IF anfail = "noreport" THEN "empty" ELSE anfail)
  /\ UNCHANGED anlast
  /\ anharness' = anharness
  /\ anfleet' = anfleet /\ anredealt' = anredealt

\* a mind: a broken read with no finding, a card at its bound (and, broken, a failure no class
\* names, left to the seat below its bound), the second identical harness fault (which the
\* harness bound judges, never redealt a third time), and the fleet's grouped harness-fault
\* judgment (the same class on RuleSameFailureCards cards within the window); and
\* at the bound a non-empty finding is still reworked with it (as Part reads does), the
\* finding marked fixed so it is not read again
AnMind ==
  /\ \/ anst = "broken" /\ anfind = "empty"
     \/ anst = "failed" /\ anfail = "other" /\ Broken = "parkfailed"
     \/ anst = "failed" /\ anfail = "noreport" /\ Broken # "noreportrework"
     \/ anst = "failed" /\ anfail = "harness" /\ anharness = 1
     \/ anst = "failed" /\ anfail = "harness" /\ anfleet = 2
     \/ anst = "bound" /\ (anfind = None \/ Broken = "answersmind")
     \/ anst = "bound" /\ andec > 0 \* a second bound after the one decision modelled: dropped
  /\ anst' = "judged"
  /\ UNCHANGED <<anatt, anreworks, anhead, ancarry, anfind, anfail, anlast, anland, anlost, anfixless, anhold, anfixed, anharness, anfleet, anredealt, andec, anvotes, anmarked>>

\* the mind's rework at the bound (the seat's rework, a coordinator decision, steps_review.go
\* Rework with no Rule): one attempt more from the head the attempt before pushed, as every
\* rework, the finding marked fixed, and the decision recorded (andec' = anatt), so the next
\* Cap attempts are a fresh window. One decision in this model (andec = 0), so it is finite.
AnMindRework ==
  /\ anst = "bound" /\ anfind \notin {None, "empty"} /\ Broken # "answersmind"
  /\ andec = 0 /\ anreworks < MaxFails
  /\ andec' = anatt
  /\ anatt' = anatt + 1 /\ anreworks' = anreworks + 1 /\ ancarry' = anhead
  /\ anst' = "working" /\ anfixed' = anfixed \cup {anfind} /\ anlast' = anfind
  /\ anfind' = None /\ anfail' = None /\ anland' = FALSE /\ anhold' = FALSE
  \* the decision opens a fresh window: its votes start again, and it answers the window's mark
  /\ anvotes' = 0 /\ anmarked' = FALSE
  /\ UNCHANGED <<anhead, anlost, anfixless, anharness, anfleet, anredealt>>

\* the outside: another card of the fleet fails the same harness-fault class, counted by
\* TickHarnessFleet toward RuleSameFailureCards; two such cards and this one are the fleet's
\* grouped judgment, which holds this card. It happens only while this card works: the tick
\* reads the fleet whole, before the rules answer this card's failure.
AnFleetFails ==
  /\ anst = "working"
  /\ anfleet < 2
  /\ anfleet' = anfleet + 1
  /\ UNCHANGED <<anst, anatt, anreworks, anhead, ancarry, anfind, anfail, anlast, anland, anlost, anfixless, anhold, anfixed, anharness, anredealt, andec, anvotes, anmarked>>

AnNext ==
  \/ AnDeadline \/ AnDeadlineHold \/ AnLateLand \/ AnLateHold
  \/ RuleAnBroken \/ RuleAnHarness \/ RuleAnFailed \/ AnMind \/ AnMindRework \/ AnFleetFails
  \/ AnMark
  \/ \E f \in AnFindings : AnReadBroken(f)
  \/ \E k \in AnFails, ran \in BOOLEAN : AnWorkFails(k, ran)

\* The answers never pass the attempt bound: Cap attempts since the coordinator's last decision.
AnswerAttemptsBounded == anatt - andec <= Cap

\* A reworked card keeps its attempt count: every rework is one attempt more.
ReworkKeepsCount == anatt = anreworks + 1

\* A reworked card carries its head: its attempt starts from the head the one before pushed.
ReworkCarriesHead == anreworks > 0 => ancarry = anatt - 1

\* No rule reworks a card without a fix: a broken read with no finding is a mind's.
ReworkHasAFix == ~anfixless

\* A report that arrives for an attempt the deadline failed, before a later attempt started,
\* finishes it: it is never refused while it is the attempt's to finish.
LateReportFinishes == ~anlost

\* The answers Part's verdict on a read-broken finding agrees with the reads Part's: a
\* non-empty finding at the brief's bound is reworked with it, never left to a mind (the
\* reads Part's ReworkAtBound).
PartsAgree == (anst = "bound" /\ anfind \notin {None, "empty"} /\ andec = 0) ~> (anst = "working")

\* Every broken read, failure and bound is answered: by rule where a rule answers, by a mind
\* where none does.
AnAnswered == (anst \in {"broken", "failed", "bound"}) ~> (anst \notin {"broken", "failed", "bound"})

\* Failed work, and a broken read with a finding, below the bound is never held waiting on the
\* seat: the machine reworks it with its report or its finding (or a late report finishes it).
\* Only past the bound, or with no finding, is it a mind's (rules.go ruleFailed and
\* ruleReadBroken; wait_refused.go).
\* Failed work with no report, the second identical failure (the bound's), and the second
\* identical harness fault (the harness bound's judgment: HarnessFaultBounded) and the fleet's
\* grouped harness-fault judgment (HarnessFleetHeld) are outside it.
FailedWithAFindingNeverParked ==
  ((anst = "failed" /\ anfail \in {"harness", "hold", "other"} /\ ~(anfail = "harness" /\ (anharness = 1 \/ anfleet = 2))) \/ (anst = "broken" /\ anfind # "empty"))
    ~> (anst \in {"working", "done"})

\* The second identical failure is never failed work the rule reworks on its tier: it is the
\* bound's (rules.go ruleFailed leaves it to ruleBound).
IdenticalIsBound == anfail = "same" => anst # "failed"

\* A harness fault is reworked on the same tier at most once per card: the second identical
\* one raises the NHarnessFault judgment, never a third redeal (harness_fault.go ruleHarness).
HarnessFaultBounded == anharness <= 1

\* A card whose harness-fault class the fleet holds (the same class on RuleSameFailureCards
\* cards within the window) is held by the one grouped NHarnessFault judgment and never
\* redealt; the groupredeal defect redeals it (harness_fault.go ruleHarness; rules.go
\* TickHarnessFleet).
HarnessFleetHeld == ~anredealt

\* A harness fault never votes the brief wrong (PR #273): a card is marked a brief defect only
\* on its own bound (the same finding or failure twice) or when the worker's and the brief's
\* ends since the last decision are at least the harness's. The harnessvotes defect marks on
\* the cap alone, and TLC's counterexample is the base behaviour: harness ends to the cap,
\* marked with no vote among them.
MarkOnlyOnVotes == anmarked => (AnSameBound \/ anvotes >= AnHarnessCount)

\* The window's votes never exceed its attempts.
VotesWithinWindow == anvotes <= anatt - andec

\* The reversed witness: a mind still answers a card past the bound or with nothing to take as
\* a fix (MCSprintRulesAnswersReachMindJudges: TLC reaches "judged" and so breaks it). "judged"
\* is the seat's to answer at once: no wait parks it (wait_refused.go).
ReachMindJudges == anst # "judged"

-----------------------------------------------------------------------------
\* Part "episodes": three judgments' episodes the fault inventory of 2026-10-10 found broken
\* (items 12, 22, 23), each a small machine; the tick (EpTick) is one step for all three, the
\* outside moves between ticks. eclk is running time, one step a tick.
\*
\* The hard pin (stops.go PinReleased, pinStops; seat_answers.go releasePins; friend_deal.go
\* friendDealPass; steps_tick.go TickDeal; coordinator_pass.go pinConds): a card pinned to one
\* friend alone, ready.
\*   pup    she is up (an outside event: held, down, back).
\*   pcol   the card: ready, dealt to her ("mine"), or dealt to another worker ("away").
\*   pwait  the running clock its pin-waits stop (NStopPinWaits) was raised at; -1 none.
\*   pign   the pin-ignored judgment (NPinIgnored) is open.
\*   pdw    how long the stop had stood when the card was dealt away; -1 never.
\*   prec   the NPinReleased record: the tick records the release once the stop note has
\*          stood PinBound of running time; PinReleased reads it.
\*   pstopped the machine is STOPPED (an outside event): the running clock does not advance.
\* The tick deals it to her while she is up; while she is not, the pass keeps the stop, and
\* once the stop has stood PinBound (Window here; the code's PinReleaseBound, one hour) of
\* running time the tick records the release (prec) and the deal offers it on, and the pass
\* keeps the pin-ignored judgment while it is away. STOPPED time does not count: while the
\* machine is stopped the running clock stands still, so the tick writes no record and a
\* machine stopped past the bound releases nothing on resume. Not modelled: the
\* coordinator's answer to the stop. An ack or a wait writes a new stop note at its own time,
\* and the code's clock (PinReleased) is that note's At, so each answer starts the hour again;
\* the release comes PinReleaseBound after the last answer. Here pwait is set once.
\*
\* Cannot ask (read_cards.go readCardsAskPart, steps_tick.go cannotAskCond and notify): a
\* primary in review that wants a read.
\*   ast     it waits for a reader, a read is dealt it ("asked"), or it left review.
\*   aatt    its attempt.
\*   areason why it waits this tick: no reader may read it, or every reader who may is at
\*           its room (an outside event: readers rest, come up, fill and empty).
\*   aopen   the cannot-ask judgment is open; aseen: the last tick saw the card waiting.
\*   anotes  the cannot-ask judgments written this episode.
\*   aflapped eligibility has changed; the reversed witness re-raises on every tick after it.
\* The episode is the card waiting at one attempt: the judgment is raised when no reader may
\* read it, closed on the next tick with an eligible reader even at room, or when a read is dealt, the
\* card leaves review or its attempt changes.
\*
\* The rule's answer (rules.go TickRuleRework, rules_read.go TickRuleTwin; steps_work.go
\* finishPlan, ruleAnswerConsumed): the primary's rule_answer and note.
\*   rcol  the primary: review, ready (its next attempt waits to be dealt), working.
\*   rans  rule_answer (and its note) is on the card.
\*   ratt  its attempt.
\* A rule answers a card in review by moving it, in the same step, to its next attempt and
\* writing the answer; that attempt's finish consumes the answer.
\*
\* Reversed witnesses:
\*   "pinforever"  a hard pin is never released (the code before): PinWaitBounded
\*   "pinsilent"   released without the pin-ignored judgment: AwayIsJudged
\*   "pinearly"    released at once, the bound not waited: PinHonouredWithinBound
\*   "stoppedcounts" the running clock advances while STOPPED: PinClockSkipsStopped
\*   "askflap"     after eligibility changes, repeats the judgment on unchanged ticks:
\*                 OneCannotAskAnEpisode (legacy witness name retained)
\*   "askheldeligible" an eligible reader leaves the episode open: CannotAskClosedWhenEligible
\*   "askflapeligible" after a flap, raises again on unchanged no-reader ticks: OneCannotAskAnEpisode
\*   "asknoclose"  the judgment stays open after the card is asked: CannotAskClosedWhenAsked
\*   "keepanswer"  the finish keeps the answer (the code before): AnswerApplies
\*   "answernomove" a rule writes its answer and leaves the card in review: AnswerApplies

PinBound == Window

\* the tick records the release once the stop note has stood PinBound of running time
\* (the pinearly defect at once; pinforever never). The running clock stands still while
\* the machine is STOPPED, so a machine stopped short of the bound writes no record.
PinRecordDue ==
  IF Broken = "pinearly" THEN TRUE
  ELSE IF Broken = "pinforever" THEN FALSE
  ELSE eclk - pwait >= PinBound

\* the hard pin is a preference now: she is not up, and its stop's NPinReleased record is
\* there (written by the tick this step or an earlier one)
PinReleased ==
  /\ ~pup /\ pwait # -1
  /\ prec \/ PinRecordDue

\* the outside: she is held or down, or back up
PinFlip ==
  /\ pup' = ~pup
  /\ UNCHANGED <<pcol, pwait, pign, pdw, prec, pstopped, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>

\* the outside: the machine is STOPPED or starts again (stopped time does not count)
PinStop ==
  /\ ~pstopped /\ pstopped' = TRUE
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>
PinStart ==
  /\ pstopped /\ pstopped' = FALSE
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>

\* the outside: the card's attempt ends wherever it ran; its next attempt is pinned to her
\* again (ReworkPinned), ready, a new episode
PinNextAttempt ==
  /\ pcol \in {"mine", "away"}
  /\ pcol' = "ready" /\ pdw' = -1 /\ prec' = FALSE
  /\ UNCHANGED <<pup, pwait, pign, pstopped, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>

\* the outside: the reason the card waits changes while it waits
AskReason ==
  /\ ast = "waits"
  /\ areason' = IF areason = "noreader" THEN "room" ELSE "noreader"
  /\ aflapped' = TRUE
  /\ anotes' = 0
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, ast, aatt, aopen, aseen, rcol, rans, ratt>>

\* the outside: a read is dealt it; the read comes back with no verdict and it waits again
\* (a new episode: a read was dealt); its attempt is answered and the next one waits; it
\* leaves review
AskDealt ==
  /\ ast = "waits" /\ ast' = "asked"
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>
AskBack ==
  /\ ast = "asked" /\ ast' = "waits"
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>
AskNextAttempt ==
  /\ ast # "gone" /\ aatt < Cap
  /\ aatt' = aatt + 1 /\ ast' = "waits" /\ aflapped' = FALSE
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, areason, aseen, rcol, rans, ratt>>
AskGone ==
  /\ ast # "gone" /\ ast' = "gone"
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, aatt, areason, aflapped, aopen, aseen, anotes, rcol, rans, ratt>>

\* a rule answers the card in review: its next attempt, ready, and the answer, in one step
RuleAnswers ==
  /\ rcol = "review" /\ ratt < Cap
  /\ rans' = TRUE
  /\ IF Broken = "answernomove" THEN UNCHANGED <<rcol, ratt>>
     ELSE rcol' = "ready" /\ ratt' = ratt + 1
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes>>

\* the outside: the attempt is dealt and taken; it finishes, back in review, the answer
\* consumed
RDealt ==
  /\ rcol = "ready" /\ rcol' = "working"
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, rans, ratt>>
RFinish ==
  /\ rcol = "working" /\ rcol' = "review"
  /\ rans' = (Broken = "keepanswer" /\ rans)
  /\ UNCHANGED <<pup, pcol, pwait, pign, pdw, prec, pstopped, eclk, ast, aatt, areason, aflapped, aopen, aseen, anotes, ratt>>

\* the cannot-ask condition the tick reads, and whether it raises one
AskHolds == ast = "waits" /\ (areason = "noreader" \/ Broken = "askheldeligible")
AskRaise == ast = "waits" /\ areason = "noreader"

\* one tick: the deal and the pass for the pin, the ask's judgment, nothing for the answer
EpTick ==
  /\ eclk < MaxClock
  /\ LET now == IF pstopped /\ Broken # "stoppedcounts" THEN eclk ELSE eclk + 1
     IN /\ eclk' = now
        \* the tick records the release once the stop note has stood the bound of running
        \* time (a stopped machine's clock is frozen, so it records nothing)
        /\ prec' = (~pup /\ pwait # -1 /\ PinRecordDue)
        \* the pin: dealt to her while she is up; released past its bound; the stop kept
        \* while it waits ready on her not up; the pin-ignored judgment kept while it is away
        /\ pcol' = IF pcol = "ready" /\ pup THEN "mine"
                   ELSE IF pcol = "ready" /\ PinReleased THEN "away"
                   ELSE pcol
        /\ pdw' = IF pcol = "ready" /\ ~pup /\ PinReleased THEN eclk - pwait ELSE pdw
        /\ pwait' = IF pcol' = "ready" /\ ~pup THEN (IF pwait = -1 THEN now ELSE pwait) ELSE -1
        /\ pign' = (pcol' = "away" /\ Broken # "pinsilent")
  \* cannot ask: raised once an episode, held while it holds, closed when it clears
  /\ aseen' = (ast = "waits")
  /\ IF AskHolds
       THEN IF aopen
            THEN IF Broken \in {"askflap", "askflapeligible"} /\ aflapped
                 THEN aopen' = TRUE /\ anotes' = anotes + 1
                 ELSE UNCHANGED <<aopen, anotes>>
            ELSE IF AskRaise THEN aopen' = TRUE /\ anotes' = anotes + 1
            ELSE UNCHANGED <<aopen, anotes>>
       ELSE IF Broken = "asknoclose" THEN UNCHANGED <<aopen, anotes>>
            ELSE /\ aopen' = FALSE
                 \* the episode ends when the card no longer waits (a flap keeps counting)
                 /\ anotes' = IF ast = "waits" THEN anotes ELSE 0
  /\ UNCHANGED <<pup, pstopped, ast, aatt, areason, aflapped, rcol, rans, ratt>>

\* the attempt the card waits at changes: a new episode (the pass ends the judgment of the
\* attempt before on the next tick, cannotAskEpisodes; modelled by ending the episode on the
\* attempt step)
EpNext ==
  \/ PinFlip \/ PinStop \/ PinStart \/ PinNextAttempt
  \/ AskReason \/ AskDealt \/ AskBack \/ AskGone
  \/ AskNextAttempt /\ aopen' = FALSE /\ anotes' = 0
  \/ RuleAnswers \/ RDealt \/ RFinish
  \/ EpTick

\* A hard pin waits on a friend who is not up no longer than its bound: the tick that finds
\* the bound passed deals it on.
PinWaitBounded == pwait # -1 => eclk - pwait <= PinBound

\* A hard pin dealt away from its friend is judged: the pin-ignored judgment is open.
AwayIsJudged == pcol = "away" => pign

\* A hard pin is honoured for its bound: dealt away only after its stop stood PinBound.
PinHonouredWithinBound == pdw # -1 => pdw >= PinBound

\* STOPPED time does not count toward the pin's deadline: while the machine is STOPPED
\* the running clock stands still, so a machine stopped short of the bound releases
\* nothing on resume.
PinClockSkipsStopped == [][pstopped => eclk' = eclk]_epVars

\* One cannot-ask judgment per unchanged eligible set at one attempt.
\* AskReason starts another episode when that set changes.
OneCannotAskAnEpisode == anotes <= 1

\* A cannot-ask judgment is open only while the last tick saw the card waiting.
CannotAskClosedWhenAsked == aopen => aseen

\* Outside eligibility changes may precede the tick: closure is required by that tick,
\* including a reader at room (the eligible reason is "room").
CannotAskClosedWhenEligible ==
  [][(EpTick /\ ast = "waits" /\ areason = "room") => ~aopen']_epVars

\* A rule's answer on a card is the answer of the attempt on its way: the rule moved the
\* card in the step it answered, and the attempt's finish consumed it.
AnswerApplies == rans => rcol \in {"ready", "working"}

-----------------------------------------------------------------------------
\* Part "judgments": the three rules that answer the judgments the tick raises
\* (internal/sprint judgment_rules.go: ruleFriendTake, ruleReadLate, ruleHoldNeed).
\* Each rule is one judgment, its guard decided on the judgment's own facts and its
\* answer one verb the house already has. A judgment is answered at most once, and an
\* answered judgment is never reopened by the same rule: each rule's answer is a
\* terminal state the module never leaves.

\* -- friend-take: a work card on a friend's row, dealt and never taken or working and
\*    not finished. jf is the card, jfstarted that she started it (an outside event).
\*    One she has not started the rule takes back to ready, and the deal places it
\*    again, never on her ("taken" is the answer and the end). One she started stays
\*    hers: friends keep the cards they started.

\* the outside: she starts the card
FriendStarts ==
  /\ jf = "open" /\ ~jfstarted
  /\ jfstarted' = TRUE
  /\ UNCHANGED <<jf, jr, jrasked, jh, jhlanded>>

\* the rule: take back the card she has not started; the "takestarted" witness drops the
\* started guard and takes back the card she started
RuleFriendTake ==
  /\ jf = "open"
  /\ IF Broken = "takestarted" THEN TRUE ELSE ~jfstarted
  /\ jf' = "taken"
  /\ UNCHANGED <<jfstarted, jr, jrasked, jh, jhlanded>>

\* -- read-late: a read card past its deadline (asked and not begun, or begun and not
\*    reported) of the primary's live attempt. jr is the read; the rule asks another
\*    reader (ask --instead), once an attempt (jrasked); the second late read of the
\*    attempt, and one no other reader can take, are a mind's.

\* the rule: ask another reader, once an attempt
RuleReadLate ==
  /\ jr = "open" /\ jrasked = 0
  /\ jr' = "asked" /\ jrasked' = 1
  /\ UNCHANGED <<jf, jfstarted, jh, jhlanded>>

\* a second late read of the attempt, and one no other reader can take, are a mind's
ReadLateMind ==
  /\ jr \in {"open", "asked"}
  /\ jr' = "mind"
  /\ UNCHANGED <<jf, jfstarted, jrasked, jh, jhlanded>>

\* -- hold-need: work came back failed and its report HOLDs naming a card on the table
\*    that has not landed. jh is the HOLD's card; while it waits the rule keeps the
\*    judgment open (marked "waiting on <card>"); once the card lands (jhlanded) the rule
\*    reworks the primary on the current tip; a card dropped since (jh = "off") is a
\*    mind's.

\* the outside: the card the HOLD names lands
NeedLands ==
  /\ jh = "waiting" /\ ~jhlanded
  /\ jhlanded' = TRUE
  /\ UNCHANGED <<jf, jfstarted, jr, jrasked, jh>>

\* the outside: the card the HOLD names is dropped off the table
NeedDropped ==
  /\ jh = "waiting"
  /\ jh' = "off"
  /\ UNCHANGED <<jf, jfstarted, jr, jrasked, jhlanded>>

\* the rule: once the card lands the primary is reworked on the current tip
RuleHoldNeedRework ==
  /\ jh = "waiting" /\ jhlanded
  /\ jh' = "reworked"
  /\ UNCHANGED <<jf, jfstarted, jr, jrasked, jhlanded>>

\* the rule: a card dropped since is a mind's
RuleHoldNeedMind ==
  /\ jh = "off"
  /\ jh' = "mind"
  /\ UNCHANGED <<jf, jfstarted, jr, jrasked, jhlanded>>

JudgNext ==
  \/ FriendStarts \/ RuleFriendTake
  \/ RuleReadLate \/ ReadLateMind
  \/ NeedLands \/ NeedDropped \/ RuleHoldNeedRework \/ RuleHoldNeedMind

\* One she started stays hers: the rule never takes back a card the friend started.
FriendKeepsStarted == jfstarted => jf # "taken"

\* The read-late rule asks another reader once an attempt: the second late read of the
\* attempt is a mind's, never a second ask.
AskedOnceAnAttempt == jrasked <= 1

\* The hold-need rule reworks a card only once it has landed: a card dropped since is a
\* mind's, never reworked.
HoldNeedReworksOnlyLanded == jh = "reworked" => jhlanded

-----------------------------------------------------------------------------
\* Part "stall": the landing stall (internal/sprint judgments.go, TickLandingStall; run from
\* the deadlines part). When cards wait in review or merging and the work table's last
\* landing is StallBound (the code's LandingStallBound, Window here) or more of running time
\* ago, the tick end raises one NLandingStall judgment to the coordinator; it is re-raised
\* each bound it still holds, and a landing breaks the episode and clears it (the code's
\* NLandingAgain). The code's clear is the tick after the landing; here the landing clears in
\* one step, so StallNotYoung holds at every state. Stopped time does not count: lsclk is
\* running time, one step a tick, advanced only while the machine runs.
\*   lsclk    running time, one step a tick.
\*   lsstopped the machine is STOPPED (an outside event); a stopped tick adds no running time.
\*   lswait   the cards waiting in review or merging (0, 1 or 2); the model's only way a card
\*            leaves waiting is to land, so "nothing waits" is the landing's own clear.
\*   lslast   the running-time stamp of the last landing; -1 none.
\*   lssince  the episode's start stamp; -1 no episode.
\*   lssaid   the episode's last push stamp; -1 none.
\*   lsjudged the NLandingStall judgment is open.

StallBound == Window

\* the outside: a card enters review or merging
LsWait ==
  /\ lswait < 2
  /\ lswait' = lswait + 1
  /\ UNCHANGED <<lsclk, lsstopped, lslast, lssince, lssaid, lsjudged>>

\* the outside: a card lands (the merge step writes last_landing); a landing breaks the
\* episode and clears it (the "noclear" defect leaves the judgment open)
LsLand ==
  /\ lswait > 0
  /\ lslast' = lsclk /\ lswait' = lswait - 1
  /\ IF Broken = "noclear"
       THEN UNCHANGED <<lssince, lssaid, lsjudged>>
       ELSE /\ lssince' = -1 /\ lssaid' = -1 /\ lsjudged' = FALSE
  /\ UNCHANGED <<lsclk, lsstopped>>

\* the outside: the machine is STOPPED or starts again
LsStop ==
  /\ ~lsstopped /\ lsstopped' = TRUE
  /\ UNCHANGED <<lsclk, lswait, lslast, lssince, lssaid, lsjudged>>
LsStart ==
  /\ lsstopped /\ lsstopped' = FALSE
  /\ UNCHANGED <<lsclk, lswait, lslast, lssince, lssaid, lsjudged>>

\* the tick end's alarm: the first tick that sees waiting cards with no landing starts the
\* episode; later ticks raise once the running-time age reaches the bound, re-raising each
\* bound the condition still holds (the "nostall" defect raises none)
LsTick ==
  /\ lsclk < MaxClock
  /\ LET now == IF lsstopped THEN lsclk ELSE lsclk + 1
         waiting == lswait > 0
         hasLand == lslast # -1
         starts == waiting /\ ~hasLand /\ lssince = -1
         age == now - (IF hasLand THEN lslast ELSE lssince)
         stalled == waiting /\ (hasLand \/ lssince # -1) /\ age >= StallBound /\ Broken # "nostall"
         due == (lssaid = -1) \/ (now - lssaid >= StallBound)
     IN /\ lsclk' = now
        /\ IF starts
             THEN /\ lssince' = now
                  /\ UNCHANGED <<lsstopped, lswait, lslast, lssaid, lsjudged>>
           ELSE IF stalled
             THEN /\ lssince' = IF lssince = -1 THEN now ELSE lssince
                  /\ IF due
                       THEN /\ lsjudged' = TRUE /\ lssaid' = now
                       ELSE UNCHANGED <<lsjudged, lssaid>>
                  /\ UNCHANGED <<lsstopped, lswait, lslast>>
           ELSE UNCHANGED <<lsstopped, lswait, lslast, lssince, lssaid, lsjudged>>

LsNext == LsWait \/ LsLand \/ LsStop \/ LsStart \/ LsTick

\* The landing-stall judgment is never open while the last landing is younger than the
\* bound: it opens only once the last landing is StallBound or more of running time ago (or
\* there has been none).
StallNotYoung == lsjudged => (lslast = -1 \/ lsclk - lslast >= StallBound)

\* A card in review with its reads done and ok (it can land) is landed or the stall is
\* raised: with no landing yet, waiting cards are judged once the bound of running time
\* passes, under fairness of the running tick and the machine starting again.
StallLive == (lswait > 0 /\ lslast = -1) ~> (lsjudged \/ lslast # -1)
\* Part "conflict": one card whose head the lander refused (internal/sprint rules.go
\* ruleConflict, ruleRedo, ruleBrief; redo.go Return). The card is on a tier (flash, pro,
\* heavy; the failed and bound rules raised it, Part "rules"): the lander refuses its head
\* one of the ways RefusalWay places, and the stream stops on the conflict. The conflict
\* rule returns the card and sets its tier to flash, recording the way (RuleConflictReturn);
\* the redo reworks it on the current tip, one attempt more (RuleConflictRedo); the same
\* way twice is a brief defect, marked and held for a mind (RuleConflictBrief). A refusal
\* the rule cannot place (a generated ledger, a head origin does not hold) is a mind's,
\* not modelled.

\* the outside: the failed and bound rules raised the card a tier (Part "rules")
Rise ==
  /\ cst = "working" /\ ctier < 3
  /\ ctier' = ctier + 1
  /\ UNCHANGED <<cst, crefused, cway, catt>>

\* the outside: the lander refuses the head the way w; the stream stops on the
\* conflict, the card stuck in its merge
Refuse(w) ==
  /\ cst = "working" /\ w \in ConflictWays
  /\ cst' = "refused" /\ cway' = w
  /\ UNCHANGED <<ctier, crefused, catt>>

\* the outside: the head lands, no conflict
Lands ==
  /\ cst = "working"
  /\ cst' = "done"
  /\ UNCHANGED <<ctier, crefused, cway, catt>>

\* the conflict rule: the first refusal of a way returns the card and sets its tier to
\* flash; the conflictkeeps defect leaves the tier the failure raised it to
RuleConflictReturn ==
  /\ cst = "refused" /\ crefused # cway
  /\ ctier' = IF Broken = "conflictkeeps" THEN ctier ELSE 1
  /\ crefused' = cway /\ cst' = "returned" /\ cway' = None
  /\ UNCHANGED catt

\* the conflict rule's brief-defect answer: the same way twice is the same finding twice,
\* the brief is wrong, not the worker: marked, the stream held for a mind
RuleConflictBrief ==
  /\ cst = "refused" /\ crefused = cway
  /\ cst' = "brief" /\ cway' = None
  /\ UNCHANGED <<ctier, crefused, catt>>

\* the redo: the returned card reworked on the current tip, one attempt more, on flash
\* (where the return left it)
RuleConflictRedo ==
  /\ cst = "returned" /\ catt < Cap
  /\ cst' = "working" /\ catt' = catt + 1
  /\ UNCHANGED <<ctier, crefused, cway>>

ConflictNext ==
  \/ Rise \/ Lands
  \/ \E w \in ConflictWays : Refuse(w)
  \/ RuleConflictReturn \/ RuleConflictBrief \/ RuleConflictRedo

\* The conflict rule's return resets the card to flash: whatever tier a failure left it
\* on, a returned card is redone at flash.
ConflictResetsToFlash == cst = "returned" => ctier = 1

\* LadderClimbs restated for Part "conflict": no rule but the conflict rule lowers a tier;
\* the tier falls only on the conflict rule's return.
OnlyConflictLowers == [][ctier' < ctier => cst = "refused" /\ cst' = "returned"]_conflictVars

-----------------------------------------------------------------------------
\* Part "noroute": one card whose tier no route serves (internal/sprint
\* seat_answers.go TickRuleNoRoute, RuleNoRoute). The card sits on a tier of the
\* ladder (1 flash, 2 pro, 3 heavy, 4 frontier or a friend's card); Served is the
\* tiers a route or a friend up serves. An overdue no-route judgment (njudged, an
\* outside event: the deal raised it when no route serves the tier) is what the rule
\* answers: each ready primary it holds is raised to the nearest served tier above,
\* pinned there (npinned) and the seat told in a note naming both tiers. Never down,
\* so a heavy card never goes to flash; a pinned card (pinnedTier: FieldTier, a pinned
\* model, or frontier), a friend's card, a brief defect, a card not ready, or one with
\* no served tier above stays held.

\* a served tier stands above t
ServedAbove(t) == \E s \in Served : s > t

\* the nearest served tier above t, or t when none does (frontier and heavy have no
\* served tier above, so the rule leaves them)
NearestServed(t) ==
  IF ServedAbove(t)
  THEN CHOOSE s \in Served : s > t /\ \A u \in Served : u > t => u >= s
  ELSE t

\* the outside: the card's status changes, and the overdue judgment appears
NoRouteReady ==
  /\ nready' = ~nready
  /\ UNCHANGED <<ntier, npinned, nfriend, nbrief, njudged>>
NoRoutePin ==
  /\ npinned' = ~npinned
  /\ UNCHANGED <<ntier, nready, nfriend, nbrief, njudged>>
NoRouteFriend ==
  /\ nfriend' = ~nfriend
  /\ UNCHANGED <<ntier, nready, npinned, nbrief, njudged>>
NoRouteBrief ==
  /\ nbrief' = ~nbrief
  /\ UNCHANGED <<ntier, nready, npinned, nfriend, njudged>>
NoRouteJudge ==
  /\ ~njudged
  /\ njudged' = TRUE
  /\ UNCHANGED <<ntier, nready, npinned, nfriend, nbrief>>

\* the tier the rule raises the card to: the nearest served tier above, or a weaker
\* tier for the lowers defect (the guard keeps it above the ladder's floor)
NoRouteTarget ==
  IF Broken = "lowers" THEN ntier - 1 ELSE NearestServed(ntier)

\* the rule: an overdue judgment holds a ready, unpinned card that is not a friend's
\* and not a brief defect, and a served tier stands above it; the card is raised and
\* pinned there, and the seat told. The pinnedraise defect raises a pinned card too.
NoRouteRaise ==
  /\ njudged
  /\ nready /\ ~nfriend /\ ~nbrief
  /\ IF Broken = "pinnedraise" THEN TRUE ELSE ~npinned
  /\ IF Broken = "lowers" THEN ntier > 1 ELSE ServedAbove(ntier)
  /\ ntier' = NoRouteTarget
  /\ npinned' = TRUE
  /\ njudged' = FALSE
  /\ UNCHANGED <<nready, nfriend, nbrief>>

NoRouteNext == NoRouteReady \/ NoRoutePin \/ NoRouteFriend \/ NoRouteBrief \/ NoRouteJudge \/ NoRouteRaise

\* The rule never lowers a tier: a card no route serves is raised to a stronger tier,
\* never a weaker one (a heavy card never goes to flash).
NoRouteNeverLowers == [][ntier' >= ntier]_norouteVars

\* The rule raises only a card that is ready and whose tier is not pinned: a pinned
\* tier is the coordinator's word, and the rule never overrides it.
NoRouteRaisesOnlyUnpinnedReady == [][ntier' = ntier \/ (~npinned /\ nready)]_norouteVars

\* The rule raises to the nearest served tier above the card's own, and nowhere else.
NoRouteRaisesToNearestServed == [][ntier' = ntier \/ ntier' = NearestServed(ntier)]_norouteVars

-----------------------------------------------------------------------------
\* Part "alert": the owner alert's send-once (cmd/nova-sprint seat_silent.go
\* tellSeatSilent, seatSilentToken). A raise of the seat-not-answering judgment is
\* sent to the owner on the bus and marked told only once its send succeeds; a failed
\* send is retried at the next look under the same token (seatSilentToken: the sprint's
\* key prefix, its epoch, the judgment's id and the raise), so a send whose response was
\* lost, or a server restarted between a send and its mark, never puts a second message
\* on the bus (the bus answers a token it has seen with the message it sent). A raise no
\* longer open is forgotten at each look (not modelled: the one raise stays open).

\* the message the bus records: one for a token (a retry is answered with the message
\* already sent), a fresh one each look for a fresh token (newtoken)
PutMsg == IF Broken = "newtoken" THEN almsg + 1 ELSE 1

\* the outside: the raise appears (the judgment opens)
RaiseAppears ==
  /\ ~alopen
  /\ alopen' = TRUE
  /\ UNCHANGED <<almsg, altold>>

\* the look sends the raise; it succeeds and is marked told, or the toldbeforesend
\* defect marks it told before its send succeeded
SendOK ==
  /\ alopen /\ ~altold
  /\ IF Broken = "newtoken" THEN almsg < 2 ELSE TRUE
  /\ IF Broken = "toldbeforesend"
       THEN /\ altold' = TRUE /\ almsg' = almsg
       ELSE /\ almsg' = PutMsg /\ altold' = TRUE
  /\ UNCHANGED alopen

\* the look's send fails: no mark, retried at the next look under the same token
SendFail ==
  /\ alopen /\ ~altold
  /\ IF Broken = "newtoken" THEN almsg < 2 ELSE TRUE
  /\ almsg' = PutMsg
  /\ UNCHANGED <<altold, alopen>>

\* the server restarts: the told map (in memory) is lost, the bus keeps its message
Restart ==
  /\ altold' = FALSE
  /\ UNCHANGED <<alopen, almsg>>

AlertNext == RaiseAppears \/ SendOK \/ SendFail \/ Restart

\* A raise is marked told only after its send succeeded (a message is on the bus).
ToldOnlyAfterSend == altold => almsg >= 1

\* A raise puts at most one message on the bus: a retry under one token, or a restart
\* between a send and its mark, never sends twice.
NoSecondMessage == almsg <= 1

\* Every raise is told, under fairness of the retry (the send eventually succeeds).
EveryRaiseTold == alopen ~> altold

-----------------------------------------------------------------------------
\* Part "tier": a tier with no live route is one judgment and quiets its cards
\* (internal/sprint route.go, steps_tick.go, inbox.go, seat_answers.go).
\*
\* While every route rests (rest), exactly one NNoRoute judgment is open (tierJ).
\* While unfunded (rest), no per-card deadline/raised-again push is raised (cardJ)
\* and no stream stale judgment is shown (staleJ) for any non-working card.
\* Once a route returns (~rest), NNoRoute closes and per-card checks resume.

RestAll ==
  /\ ~rest
  /\ ~tierJ
  /\ rest' = TRUE
  /\ tierJ' = TRUE
  /\ cardJ' = (IF card # "working" THEN FALSE ELSE cardJ)
  /\ staleJ' = (IF card # "working" THEN FALSE ELSE staleJ)
  /\ UNCHANGED card

Wake ==
  /\ rest
  /\ rest' = FALSE
  /\ UNCHANGED <<card, tierJ, cardJ, staleJ>>

Take ==
  /\ ~rest
  /\ card = "ready"
  /\ card' = "working"
  /\ UNCHANGED <<rest, tierJ, cardJ, staleJ>>

TierTick ==
  /\ IF rest
       THEN /\ tierJ' = TRUE
            /\ IF Broken = "noquiet"
                 THEN /\ cardJ' = TRUE
                      /\ staleJ' = staleJ
                 ELSE /\ cardJ' = (IF card # "working" THEN FALSE ELSE cardJ)
                      /\ staleJ' = (IF card # "working" THEN FALSE ELSE staleJ)
       ELSE /\ tierJ' = FALSE
            /\ cardJ' \in BOOLEAN
            /\ staleJ' \in BOOLEAN
  /\ UNCHANGED <<rest, card>>

TierNext == RestAll \/ Wake \/ Take \/ TierTick

\* A tier with no live route is one judgment.
OneTierJudgment == rest => tierJ

\* While an unfunded tier has no live route, its non-working cards and streams are quiet.
QuietWhileUnfunded == (rest /\ card # "working") => (~cardJ /\ ~staleJ)

\* Once funded again, the tier judgment closes under fairness of the tick.
ResumesWhenFunded == (~rest /\ card = "ready") ~> (~tierJ)

-----------------------------------------------------------------------------

Next ==
  \/ Part = "twins" /\ TwinNext /\ UNCHANGED <<ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "rules" /\ RuleNext /\ UNCHANGED <<twinVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "late" /\ LateNext /\ UNCHANGED <<twinVars, ruleVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "reads" /\ RdNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "gate" /\ GateNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "idle" /\ IdleNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "answers" /\ AnNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "episodes" /\ EpNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "judgments" /\ JudgNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "stall" /\ LsNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, conflictVars, norouteVars, alertVars, tierVars>>
  \/ Part = "conflict" /\ ConflictNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, norouteVars, alertVars, tierVars>>
  \/ Part = "noroute" /\ NoRouteNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, alertVars, tierVars>>
  \/ Part = "alert" /\ AlertNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, tierVars>>
  \/ Part = "tier" /\ TierNext /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars>>

\* The rules and a mind act when they may; the outside is unfair. The stall's running tick
\* and the machine's start are fair (a stopped machine adds no running time, so a stall can
\* only be owed once the machine has run the bound); the alert's send is fair so a raise is
\* eventually told; the tier's tick is fair so the tier judgment closes when routes wake.
Fairness ==
  /\ WF_vars(Part = "rules" /\ (RuleFailed \/ RuleBound \/ Mind) /\ UNCHANGED <<twinVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>)
  /\ WF_vars(Part = "reads" /\ (RuleReadBroken \/ RdMind) /\ UNCHANGED <<twinVars, ruleVars, lateVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>)
  /\ WF_vars(Part = "answers" /\ (RuleAnBroken \/ RuleAnHarness \/ RuleAnFailed \/ AnMind \/ AnMindRework) /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>)
  /\ WF_vars(Part = "judgments" /\ (RuleFriendTake \/ RuleReadLate \/ RuleHoldNeedRework \/ RuleHoldNeedMind) /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, stallVars, conflictVars, norouteVars, alertVars, tierVars>>)
  /\ SF_vars(Part = "stall" /\ LsTick /\ ~lsstopped /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, conflictVars, norouteVars, alertVars, tierVars>>)
  /\ WF_vars(Part = "stall" /\ LsStart /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, conflictVars, norouteVars, alertVars, tierVars>>)
  /\ SF_vars(Part = "alert" /\ SendOK /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, tierVars>>)
  /\ WF_vars(Part = "tier" /\ TierTick /\ UNCHANGED <<twinVars, ruleVars, lateVars, rdVars, gateVars, idleVars, anVars, epVars, judgVars, stallVars, conflictVars, norouteVars, alertVars>>)

Spec == Init /\ [][Next]_vars /\ Fairness

=============================================================================
