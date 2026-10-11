------------------------ MODULE MCCardMachineSmall -------------------------
\* The small TLC instance of CardMachine: one card with no DEPENDS-ON,
\* two consumers of one slot each (DealRead needs a consumer other than
\* the author), two copies per primary, one PR head. Constants that are
\* functions cannot be written in the .cfg; they are defined here and
\* substituted (the pattern MCCardMachine uses).
EXTENDS CardMachine

SmallCards     == {"c1"}
SmallConsumers == {"k1", "k2"}
SmallSlots     == [k \in SmallConsumers |-> 1]
SmallDeps      == [c \in SmallCards |-> {}]
SmallHeadBound == HeadBound(1)
=======================================================================
