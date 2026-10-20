----------------------------- MODULE MCWaitFor ------------------------------
(***************************************************************************)
(* Model-checking harness for WaitFor.tla.                                 *)
(*                                                                         *)
(* Three cards (MCCards = {1, 2, 3}), one shared file (MCFiles = {1}),     *)
(* MaxAttempt = 3, MaxNow = 3.                                             *)
(*                                                                         *)
(* What it abstracts:                                                      *)
(* - Three cards are the minimum to witness a two-card landed cycle        *)
(*   (cards 1 and 2) poisoning an unrelated edit on an open third card     *)
(*   (card 3) in Broken = "landedpoisons", while two cards suffice for     *)
(*   a hard+soft cycle ("hardonlycycle"), gate-fix HOLD loop ("gatefix"),  *)
(*   hidden-cause HOLD loop ("hiddencause"), and hot-file contention       *)
(*   ("hotlivelock").                                                      *)
(* - One shared file (MCFiles = {1}) suffices to witness conflict-only     *)
(*   brief-bound holds ("conflictbound") and repeated conflict rework      *)
(*   without hot-file ordering ("hotlivelock").                            *)
(* - Member pools, reader rooms, provider credits, and bench leases are    *)
(*   folded into the boolean environment signals `facts`, `readerFree`,    *)
(*   and `baseMoved`; their internal resource contention is modeled in     *)
(*   v17-waitfor-model-resources.                                          *)
(* - Time uses the bounded shift abstraction (now in 0..MaxNow): advancing *)
(*   at MaxNow shifts open judgment dues down by one step so relative time *)
(*   to deadline is preserved in a finite state space.                     *)
(***************************************************************************)
EXTENDS WaitFor

MCCards == {1, 2, 3}
MCFiles == {1}
=============================================================================
