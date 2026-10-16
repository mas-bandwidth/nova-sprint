---------------------------- MODULE MCQueueWait ----------------------------
EXTENDS QueueWait
Owners == [c \in Cards |-> IF c = CHOOSE x \in Cards : TRUE
                          THEN CHOOSE w \in Workers : TRUE
                          ELSE CHOOSE w \in Workers : w # (CHOOSE x \in Workers : TRUE)]
=============================================================================
