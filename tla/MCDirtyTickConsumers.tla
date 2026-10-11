------------------------ MODULE MCDirtyTickConsumers -------------------------
\* The ConsumersOut instances of DirtyTick (the owner, 2026-10-11: "after the
\* tick, ALL producer cards should have consumer cards out. No exceptions.").
\* Width 2 and above: two machines of width 2, two cards in two streams, two
\* readers. Every control runs with the "consumersout" fix (the settle loop
\* re-pumps the work table, so a card a finish or a lapse leaves without a
\* consumer is re-dealt or re-asked in the same tick) and MaxSub 32 (the
\* owner's settle bound; reaching it is a judgment, SettleBoundJudged). The
\* witnesses turn one rule off and TLC must fail on ConsumersOut.
EXTENDS MCDirtyTick

\* Width 2: each machine holds two cards or reads (a read takes room on its
\* host, so a card and its read fill one machine).
MCWidth2 == [m \in {"m1", "m2"} |-> 2]

\* c1 working on m1; m1 has stopped beating (its lapse queued): with no
\* re-deal its attempt is not re-dealt in the same tick (W4).
ScnConsumersLapse ==
  [Base EXCEPT !.live = {}, !.col = [c \in Cards |-> "working"],
               !.mc = [m \in Machines |-> IF m = "m1" THEN {"c1"} ELSE {}],
               !.q = Queues(<<>>, <<>>, <<>>, <<E("lapse", "-", "m1")>>)]
=============================================================================
