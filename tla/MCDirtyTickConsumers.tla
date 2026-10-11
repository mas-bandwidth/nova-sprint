----------------------- MODULE MCDirtyTickConsumers -----------------------
\* The instances of ConsumersOut (DirtyTick): a control with a card whose
\* needs gate its deal, and the reversed witnesses. Each configuration names
\* its cards, machines and readers, the scenario it starts from (Scn), and
\* the witness it turns on (Broken; "none" for none). The witness "cutsettle"
\* cuts the settle loop after one sub-tick; "asknext" asks a read in the next
\* tick instead of this one; "noredeal" does not re-deal a member-down's
\* attempts; "perrow" (W1) deals one card a tick.
EXTENDS MCDirtyTick

\* Two machines of width 2: ConsumersOut's config is width 2 and above.
MCWidth2 == [m \in {"m1", "m2"} |-> 2]

\* c2 needs c1 (its DEPENDS-ON): c2 is dealt only once c1 has landed. The
\* whole life with takes, a beat, a lapse and the coordinator's verbs.
ScnConsumers ==
  [Base EXCEPT !.needs = [c \in Cards |-> IF c = "c2" THEN {"c1"} ELSE {}]]

\* c1 in review waiting for a read, c2 working on m1 (filling m1, the only
\* reader's host): the settle loop must place c1's read once c2's finish frees
\* m1's room. The witness "cutsettle" ends the tick before that drain.
ScnConsumersCut ==
  [Base EXCEPT !.col = [c \in Cards |-> IF c = "c1" THEN "review" ELSE "working"],
              !.mc = [m \in Machines |-> IF m = "m1" THEN {"c2"} ELSE {}],
              !.q = Queues(<<>>, <<E("ask", "c1", "-")>>, <<>>, <<E("fin", "c2", "m1")>>)]

\* c1 in review, its read asked: the readers update asks it this tick. The
\* witness "asknext" leaves it asked in the next tick instead.
ScnConsumersAsk ==
  [Base EXCEPT !.col = [c \in Cards |-> "review"],
              !.q = Queues(<<>>, <<E("ask", "c1", "-")>>, <<>>, <<>>)]

\* c1 working on m1, m1 lapses: the next pump re-deals c1. The witness
\* "noredeal" leaves the returned attempt un-dealt.
ScnConsumersNoRedeal ==
  [Base EXCEPT !.col = [c \in Cards |-> "working"],
              !.mc = [m \in Machines |-> IF m = "m1" THEN {"c1"} ELSE {}],
              !.q = Queues(<<>>, <<>>, <<>>, <<E("lapse", "-", "m1")>>)]
=============================================================================
