------------------------- MODULE MCDirtyTickConsumers --------------------------
\* The ConsumersOut instances of DirtyTick (the owner, 2026-10-01: "after the
\* tick, ALL producer cards should have consumer cards out. No exceptions.").
\* Width 2 and above: two cards on one machine, so a member that goes down
\* leaves two cards for the settle loop to re-deal in the same tick. Each
\* configuration names the scenario (Scn) and the witness it turns on
\* (Broken; "none" for the control); the control passes ConsumersOut and each
\* witness fails it.
EXTENDS MCDirtyTick

\* Width 2 on both machines (MCDirtyTick's MCWidth is 1): a machine holds two
\* cards, so a member down is a re-deal of more than one.
MCWidth2 == [m \in {"m1", "m2"} |-> 2]

\* c1 and c2 working on m1; m1 lapses (already queued to the fleet), m2 up
\* with room: the settle loop re-deals both to m2 in the same tick. The
\* control of the witnesses "noredeal" and "settleonce". No machine beats
\* (live = {}): the outside stays quiet, only the queued lapse runs.
ScnConsumers == [Base EXCEPT !.live = {}, !.col = [c \in Cards |-> "working"],
                              !.mc = [m \in Machines |-> IF m = "m1" THEN {"c1", "c2"} ELSE {}],
                              !.q = Queues(<<>>, <<>>, <<>>, <<E("lapse", "-", "m1")>>)]

\* c1 in review, its read asked (already queued to the readers), its reader up:
\* the read is asked this tick. The control of the witness "readnext". No
\* machine beats, so no report takes the read back.
ScnConsumersRead == [Base EXCEPT !.live = {}, !.col = [c \in Cards |-> IF c = "c1" THEN "review" ELSE "none"],
                                    !.q = Queues(<<>>, <<E("ask", "c1", "-")>>, <<>>, <<>>)]
=============================================================================
