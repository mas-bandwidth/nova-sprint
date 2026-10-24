------------------------ MODULE FriendRedealProgress ------------------------
EXTENDS Integers
\* The friend redeal on progress (internal/sprint/verdicts.go TickFriendRedeal,
\* rules.go progressOf; packet.go PushedHead). 2026-10-24: the 2h redeal fired
\* on attempts whose workers had posted progress minutes before. One work card
\* on a friend's row, taken at 0, running time in units. Its worker, while
\* alive, stamps progress and names the head it has pushed (progress --head);
\* it may die at any time. The tick redeals the card when it is past its
\* deadline (Bound) and its last progress is older than the window (Window),
\* and the redealt attempt carries the head its progress named, which the next
\* attempt starts from. It checks that no redeal takes an attempt with progress
\* inside the window, that a late attempt with no recent progress never stays,
\* and that the redeal carries the pushed head; not the deal of the next
\* attempt, the lanes or the transport. Broken: "ignoreprogress" redeals on the
\* deadline alone (the 2026-10-24 bug), "neverredeal" keeps any attempt that
\* ever stamped, "dropcarry" redeals and carries nothing.
CONSTANTS Bound, Window, MaxT, Broken
None == "none"
Head == "h"
VARIABLES clock, place, progressAt, progressHead, carried, alive, redealtFresh
vars == <<clock, place, progressAt, progressHead, carried, alive, redealtFresh>>

Late(c) == c > Bound
Fresh(c) == progressAt # -1 /\ c - progressAt <= Window

Init == /\ clock = 0 /\ place = "friend" /\ progressAt = -1 /\ progressHead = None
        /\ carried = None /\ alive = TRUE /\ redealtFresh = FALSE

\* the worker stamps progress, naming the head it has pushed (rules.go Progress)
Stamp == /\ alive /\ place = "friend"
         /\ progressAt' = clock /\ progressHead' = Head
         /\ UNCHANGED <<clock, place, carried, alive, redealtFresh>>

Die == /\ alive /\ alive' = FALSE
       /\ UNCHANGED <<clock, place, progressAt, progressHead, carried, redealtFresh>>

\* the tick: time passes, then TickFriendRedeal on the new clock
Tick == /\ clock < MaxT
        /\ clock' = clock + 1
        /\ LET c == clock + 1
               keep == CASE Broken = "ignoreprogress" -> FALSE
                         [] Broken = "neverredeal" -> progressAt # -1
                         [] OTHER -> Fresh(c)
               redeal == place = "friend" /\ Late(c) /\ ~keep
           IN /\ place' = IF redeal THEN "redealt" ELSE place
              /\ carried' = IF redeal /\ Broken # "dropcarry" THEN progressHead ELSE carried
              /\ redealtFresh' = (redealtFresh \/ (redeal /\ Fresh(c)))
        /\ UNCHANGED <<progressAt, progressHead, alive>>

Next == Stamp \/ Die \/ Tick
Spec == Init /\ [][Next]_vars

TypeOK == /\ clock \in 0..MaxT /\ place \in {"friend", "redealt"}
          /\ progressAt \in -1..MaxT /\ progressHead \in {None, Head}
          /\ carried \in {None, Head} /\ alive \in BOOLEAN /\ redealtFresh \in BOOLEAN
\* no redeal takes an attempt whose progress is inside the window
NoRedealWhileProgressing == ~redealtFresh
\* a late attempt with no recent progress never stays on the friend's row past a tick
StaleIsRedealt == place = "friend" /\ clock > 0 => ~Late(clock) \/ Fresh(clock)
\* the redealt attempt carries the head its progress named
CarriedHeadKept == place = "redealt" => carried = progressHead
=============================================================================
