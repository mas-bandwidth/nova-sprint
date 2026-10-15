--------------------------- MODULE StateEpisode ---------------------------
\* A frozen note delivery retries only inside its persisted acceptance window
\* (pkg/bus.DefaultTokenLife, 24 hours). An unknown receipt beyond that window
\* remains pending for manual resolution, even after the bus removes its token.
EXTENDS Naturals
CONSTANT Broken
VARIABLES open, pending, receipt, confirmed, window, busToken, deliveries, source, frozen, wrongArgs
vars == <<open, pending, receipt, confirmed, window, busToken, deliveries, source, frozen, wrongArgs>>
Init == /\ open = FALSE /\ pending = FALSE /\ receipt = FALSE /\ confirmed = FALSE /\ window = TRUE
        /\ busToken = FALSE /\ deliveries = 0 /\ source = 0 /\ frozen = 0 /\ wrongArgs = FALSE
Raise == /\ ~open /\ open' = TRUE
         /\ UNCHANGED <<pending, receipt, confirmed, window, busToken, deliveries, source, frozen, wrongArgs>>
Prepare == /\ open /\ ~pending /\ ~receipt
           /\ pending' = TRUE /\ frozen' = source
           /\ UNCHANGED <<open, receipt, confirmed, window, busToken, deliveries, source, wrongArgs>>
Rewrite == /\ open /\ source' = 1 - source
           /\ UNCHANGED <<open, pending, receipt, confirmed, window, busToken, deliveries, frozen, wrongArgs>>
\* A committed send can lose its reply. Changed arguments under the same token
\* are refused by the bus, not delivered a second time.
Send(reply) == /\ open /\ pending /\ ~receipt /\ deliveries < 2
               /\ (window \/ Broken = "lateretry")
               /\ LET changed == Broken = "changedretry" /\ source # frozen
                  IN /\ wrongArgs' = changed
                     /\ deliveries' = IF busToken \/ changed THEN deliveries ELSE deliveries + 1
                     /\ busToken' = IF changed THEN busToken ELSE TRUE
                     /\ receipt' = (reply /\ ~changed)
                     /\ confirmed' = (confirmed \/ (reply /\ ~changed))
               /\ UNCHANGED <<open, pending, window, source, frozen>>
PrematureAck == /\ Broken = "ackfirst" /\ open /\ pending /\ ~receipt
                /\ receipt' = TRUE
                /\ UNCHANGED <<open, pending, confirmed, window, busToken, deliveries, source, frozen, wrongArgs>>
Restart == /\ receipt' = IF Broken = "volatile" THEN FALSE ELSE receipt
           /\ UNCHANGED <<open, pending, confirmed, window, busToken, deliveries, source, frozen, wrongArgs>>
Expire == /\ window /\ window' = FALSE /\ busToken' = FALSE
          /\ UNCHANGED <<open, pending, receipt, confirmed, deliveries, source, frozen, wrongArgs>>
Close == /\ open /\ open' = FALSE /\ pending' = FALSE /\ receipt' = FALSE /\ confirmed' = FALSE /\ window' = TRUE
         /\ busToken' = FALSE /\ deliveries' = 0 /\ source' = 0 /\ frozen' = 0 /\ wrongArgs' = FALSE
Next == Raise \/ Prepare \/ Rewrite \/ Send(TRUE) \/ Send(FALSE) \/ PrematureAck \/ Restart \/ Expire \/ Close
Spec == Init /\ [][Next]_vars
TypeOK == /\ open \in BOOLEAN /\ pending \in BOOLEAN /\ receipt \in BOOLEAN /\ confirmed \in BOOLEAN
          /\ window \in BOOLEAN /\ busToken \in BOOLEAN /\ wrongArgs \in BOOLEAN
          /\ deliveries \in 0..2 /\ source \in 0..1 /\ frozen \in 0..1
OneMessagePerEpisode == deliveries <= 1
ReceiptMeansSent == receipt => deliveries = 1
DurableReceipt == confirmed => receipt
FrozenArguments == ~wrongArgs
=============================================================================
