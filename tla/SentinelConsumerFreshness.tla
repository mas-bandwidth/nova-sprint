-------------------- MODULE SentinelConsumerFreshness --------------------
\* P8 companion: exact released-barrier input and one observed (repo,trunk)
\* snapshot within a callback containing two consumer streams. Existing
\* SentinelRelease covers release authorization and successful/new merges.
\* This model abstracts those successful merges, not Git/store atomicity.
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Consumers, FirstConsumer, MaxCallbacks, Broken
ASSUME /\ Cardinality(Consumers) = 2
       /\ FirstConsumer \in Consumers
       /\ MaxCallbacks = 2
       /\ Broken \in {"none", "barrier", "mixed"}

CardKey(c) == [epoch |-> 1, id |-> c, rev |-> 1]
A(c) == [id |-> <<"a", c>>, rev |-> 1, state |-> "released",
         releaseKind |-> "auto", producers |-> <<>>,
         targets |-> {c}, position |-> 1]
B == [id |-> <<"b", FirstConsumer>>, rev |-> 1, state |-> "released",
      releaseKind |-> "auto", producers |-> <<>>,
      targets |-> {FirstConsumer}, position |-> 2]
BaseVector(c) == <<A(c)>>
ExtendedVector(c) == IF c = FirstConsumer THEN <<A(c), B>> ELSE BaseVector(c)
EmptyReceipt(c) == [round |-> 0, card |-> CardKey(c),
                    barriers |-> BaseVector(c), tip |-> 0]

VARIABLES barriers, remoteDev, callback, callbackOpen, callbackBarriers,
          callbackTip, previousCallbackTip, latestObserved, seen, receipt, consumerObserved,
          dealt, everyDealBarrier, everyDealObserved
vars == <<barriers, remoteDev, callback, callbackOpen, callbackBarriers,
          callbackTip, previousCallbackTip, latestObserved, seen, receipt, consumerObserved,
          dealt, everyDealBarrier, everyDealObserved>>

Init ==
  /\ barriers = [c \in Consumers |-> BaseVector(c)]
  /\ remoteDev = 1
  /\ callback = 0 /\ callbackOpen = FALSE
  /\ callbackBarriers = barriers
  /\ callbackTip = 0 /\ previousCallbackTip = 0 /\ latestObserved = 0
  /\ seen = {}
  /\ receipt = [c \in Consumers |-> EmptyReceipt(c)]
  /\ consumerObserved = [c \in Consumers |-> 0]
  /\ dealt = {}
  /\ everyDealBarrier = TRUE /\ everyDealObserved = TRUE

\* A released positional sentinel is inserted before the consumer without
\* changing that consumer's card identity/revision. The vector represents
\* full ID/revision/release/producers/targets/position evidence, not a count.
InsertReleasedBarrier ==
  /\ barriers[FirstConsumer] = BaseVector(FirstConsumer)
  /\ barriers' = [barriers EXCEPT ![FirstConsumer] = ExtendedVector(FirstConsumer)]
  /\ UNCHANGED <<remoteDev, callback, callbackOpen, callbackBarriers,
      callbackTip, previousCallbackTip, latestObserved, seen, receipt, consumerObserved,
      dealt, everyDealBarrier, everyDealObserved>>

\* An unobserved remote advance is allowed. It does not invalidate a
\* successful merge of the callback's freshly captured snapshot.
AdvanceRemote ==
  /\ remoteDev = 1 /\ remoteDev' = 2
  /\ UNCHANGED <<barriers, callback, callbackOpen, callbackBarriers,
      callbackTip, previousCallbackTip, latestObserved, seen, receipt, consumerObserved,
      dealt, everyDealBarrier, everyDealObserved>>

BeginCallback ==
  /\ ~callbackOpen /\ callback < MaxCallbacks
  /\ callback' = callback + 1 /\ callbackOpen' = TRUE
  /\ callbackBarriers' = barriers
  /\ previousCallbackTip' = callbackTip
  /\ callbackTip' = 0 /\ latestObserved' = 0 /\ seen' = {}
  /\ UNCHANGED <<barriers, remoteDev, receipt, consumerObserved,
      dealt, everyDealBarrier, everyDealObserved>>

\* Both consumers share a (repo,trunk) key, but each has its own successful
\* branch merge/receipt. The first fetch is reused within this invocation.
\* "mixed" restores the old per-consumer fetch schedule without invalidating
\* that earlier consumer's success receipt.
CaptureMergedReceipt(c) ==
  /\ callbackOpen /\ c \notin seen
  /\ LET tip == IF callbackTip = 0 \/ Broken = "mixed"
                   THEN remoteDev ELSE callbackTip
     IN /\ callbackTip' = IF callbackTip = 0 THEN tip ELSE callbackTip
        /\ latestObserved' = tip
        /\ consumerObserved' = [consumerObserved EXCEPT ![c] = tip]
        /\ receipt' = [receipt EXCEPT ![c] =
             [round |-> callback, card |-> CardKey(c),
              barriers |-> callbackBarriers[c], tip |-> tip]]
  /\ seen' = seen \cup {c}
  /\ UNCHANGED <<barriers, remoteDev, callback, callbackOpen, callbackBarriers,
      previousCallbackTip, dealt, everyDealBarrier, everyDealObserved>>

EndCallback ==
  /\ callbackOpen /\ seen = Consumers /\ callbackOpen' = FALSE
  /\ UNCHANGED <<barriers, remoteDev, callback, callbackBarriers,
      callbackTip, previousCallbackTip, latestObserved, seen, receipt, consumerObserved,
      dealt, everyDealBarrier, everyDealObserved>>

\* The store's retry consumes evidence against CURRENT relevant barriers.
\* Card identity alone remains unchanged when another sentinel is inserted.
\* Per-consumer observed success is not proof of a common callback snapshot
\* if that callback has freshly observed a newer same-key tip elsewhere.
Deal(c) ==
  /\ ~callbackOpen /\ callback > 0 /\ c \notin dealt
  /\ receipt[c].round = callback /\ receipt[c].card = CardKey(c)
  /\ receipt[c].tip = consumerObserved[c]
  /\ receipt[c].barriers = barriers[c] \/ Broken = "barrier"
  /\ dealt' = dealt \cup {c}
  /\ everyDealBarrier' = (everyDealBarrier /\ receipt[c].barriers = barriers[c])
  /\ everyDealObserved' = (everyDealObserved /\ receipt[c].tip = latestObserved)
  /\ UNCHANGED <<barriers, remoteDev, callback, callbackOpen, callbackBarriers,
      callbackTip, previousCallbackTip, latestObserved, seen, receipt, consumerObserved>>

Next == InsertReleasedBarrier \/ AdvanceRemote \/ BeginCallback
        \/ (\E c \in Consumers : CaptureMergedReceipt(c) \/ Deal(c))
        \/ EndCallback
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ \A c \in Consumers : barriers[c] \in {BaseVector(c), ExtendedVector(c)}
  /\ \A c \in Consumers : callbackBarriers[c] \in {BaseVector(c), ExtendedVector(c)}
  /\ remoteDev \in 1..2 /\ callback \in 0..MaxCallbacks
  /\ callbackOpen \in BOOLEAN
  /\ callbackTip \in 0..2 /\ previousCallbackTip \in 0..2 /\ latestObserved \in 0..2
  /\ seen \subseteq Consumers /\ dealt \subseteq Consumers
  /\ consumerObserved \in [Consumers -> 0..2]
  /\ \A c \in Consumers :
       /\ receipt[c].round \in 0..MaxCallbacks
       /\ receipt[c].card = CardKey(c)
       /\ receipt[c].barriers \in {BaseVector(c), ExtendedVector(c)}
       /\ receipt[c].tip \in 0..2
  /\ everyDealBarrier \in BOOLEAN /\ everyDealObserved \in BOOLEAN

DealsUseCurrentBarrierVector == everyDealBarrier
DealsUseKnownCallbackSnapshot == everyDealObserved
CallbackUsesOneObservedTip ==
  seen = Consumers => \A c \in Consumers : consumerObserved[c] = callbackTip
NeverDealt == dealt = {}
NeverObservedNextCallback ==
  ~(callback = 2 /\ previousCallbackTip = 1 /\ seen = Consumers /\ latestObserved = 2
    /\ \A c \in Consumers : consumerObserved[c] = 2)
=============================================================================
