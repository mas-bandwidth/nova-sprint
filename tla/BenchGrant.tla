------------------------------- MODULE BenchGrant -------------------------------
EXTENDS Naturals, FiniteSets
CONSTANTS MaxTime, Life, Cleanup, Broken, StopForever
VARIABLES grants, now, machine, mem, busRecs, copies, judged, actor,
          writer, lateSend, obligation, used
vars == <<grants, now, machine, mem, busRecs, copies, judged, actor,
          writer, lateSend, obligation, used>>
\* One immutable grant, two frozen recipients. Copies count the grant/expiry
\* recipient across tokens; busRecs independently deduplicates each token.
\* Time units shrink 24h/7d to Life/Cleanup. User extension is bounded by MaxTime.
Recipients == {"grantee", "seat"}
None == [state |-> "none", token |-> 0, committed |-> 0, retryBefore |-> 0]
Empty == [present |-> FALSE, deadline |-> 0, ended |-> FALSE, owed |-> 0,
          changed |-> 0, delivery |-> None]
Init == /\ grants = Empty /\ now = 0 /\ machine = "STOPPED"
        /\ mem = "empty" /\ busRecs = {} /\ copies = [r \in Recipients |-> 0]
        /\ judged = FALSE /\ actor = "coordinator" /\ writer = "coordinator"
        /\ lateSend = FALSE /\ obligation = FALSE /\ used = FALSE
Due == grants.present /\ (grants.owed > 0 \/ (~grants.ended /\ now >= grants.deadline))
Owed == IF grants.owed > 0 THEN grants.owed
        ELSE IF Due THEN grants.deadline ELSE 0
Terminal == grants.delivery.state = "sent" \/ judged \/
            (grants.ended /\ grants.owed = 0 /\ grants.delivery.state = "none")
Add == /\ actor = "coordinator" /\ ~used /\ now = 0
       /\ grants' = [Empty EXCEPT !.present = TRUE, !.deadline = 1]
       /\ used' = TRUE /\ writer' = actor
       /\ UNCHANGED <<now, machine, mem, busRecs, copies, judged, actor, lateSend, obligation>>
Extend(d) == /\ actor = "coordinator" /\ grants.present /\ ~grants.ended
          /\ d \in (now + 1)..MaxTime
          /\ now < MaxTime
          /\ grants' = [grants EXCEPT !.owed = Owed, !.deadline = d, !.changed = now]
          /\ obligation' = (obligation \/ Due) /\ writer' = actor
          /\ UNCHANGED <<now, machine, mem, busRecs, copies, judged, actor, lateSend, used>>
End == /\ actor = "coordinator" /\ grants.present /\ ~grants.ended
       /\ grants' = [grants EXCEPT !.owed = Owed, !.ended = TRUE, !.changed = now]
       /\ obligation' = (obligation \/ Due) /\ writer' = actor
       /\ UNCHANGED <<now, machine, mem, busRecs, copies, judged, actor, lateSend, used>>
\* Unauthorized argv leaves both stores untouched.
WorkerAttempt == /\ actor = "worker" /\ UNCHANGED vars
Tick == /\ now < MaxTime /\ now' = now + 1
        /\ obligation' = (obligation \/ (grants.present /\ ~grants.ended /\ now' >= grants.deadline))
        /\ busRecs' = {r \in busRecs : now + 1 < r.at + Cleanup}
        /\ UNCHANGED <<grants, machine, mem, copies, judged, actor, writer, lateSend, used>>
Wake == /\ Due /\ grants.delivery.state = "none"
        /\ ("running-only" \notin Broken \/ machine = "RUNNING")
        /\ grants' = [grants EXCEPT !.owed = Owed, !.changed = now,
              !.delivery = [state |-> IF "ack-before" \in Broken THEN "sent" ELSE "pending",
                            token |-> 1, committed |-> now, retryBefore |-> now + Life]]
        /\ obligation' = TRUE /\ mem' = "ready"
        /\ UNCHANGED <<now, machine, busRecs, copies, judged, actor, writer, lateSend, used>>
CanSend == grants.present /\ ~judged /\ mem # "answered" /\ now < grants.delivery.retryBefore /\
           (grants.delivery.state = "pending" \/
             ("ack-before" \in Broken /\ mem = "ready"))
Tokens == {r.token : r \in busRecs}
Token == IF "fresh-token" \in Broken /\ mem = "lostafter" /\ 1 \in Tokens THEN 2 ELSE grants.delivery.token
WriteBus == /\ busRecs' = IF Token \in Tokens THEN busRecs ELSE busRecs \cup {[token |-> Token, at |-> now]}
            /\ copies' = [r \in Recipients |-> copies[r] + (IF Token \in Tokens THEN 0 ELSE 1)]
            /\ lateSend' = (lateSend \/ now >= grants.delivery.retryBefore)
Send == /\ CanSend /\ WriteBus /\ mem' = "answered"
        /\ UNCHANGED <<grants, now, machine, judged, actor, writer, obligation, used>>
SendLostAfter == /\ CanSend /\ WriteBus /\ mem' = "lostafter"
                 /\ UNCHANGED <<grants, now, machine, judged, actor, writer, obligation, used>>
SendLostBefore == /\ CanSend /\ mem' = "empty"
                  /\ UNCHANGED <<grants, now, machine, busRecs, copies, judged, actor, writer, lateSend, obligation, used>>
Ack == /\ grants.delivery.state = "pending" /\ mem = "answered" /\ ~judged
       /\ grants' = [grants EXCEPT !.delivery.state = "sent", !.changed = now]
       /\ mem' = "empty"
       /\ UNCHANGED <<now, machine, busRecs, copies, judged, actor, writer, lateSend, obligation, used>>
GiveUp == /\ grants.present /\ grants.delivery.state = "pending" /\ ~judged
          /\ now >= grants.delivery.retryBefore
          /\ judged' = TRUE /\ grants' = [grants EXCEPT !.changed = now]
          /\ mem' = "empty"
          /\ UNCHANGED <<now, machine, busRecs, copies, actor, writer, lateSend, obligation, used>>
Restart == /\ mem # "empty" /\ mem' = "empty"
           /\ UNCHANGED <<grants, now, machine, busRecs, copies, judged, actor, writer, lateSend, obligation, used>>
Prune == /\ grants.present /\ Terminal /\ now >= grants.changed + Cleanup
         /\ grants' = [grants EXCEPT !.present = FALSE]
         /\ UNCHANGED <<now, machine, mem, busRecs, copies, judged, actor, writer, lateSend, obligation, used>>
Outside == /\ machine' \in (IF StopForever THEN {"STOPPED"} ELSE {"RUNNING", "STOPPED"}) /\ actor' \in {"coordinator", "worker"}
           /\ UNCHANGED <<grants, now, mem, busRecs, copies, judged, writer, lateSend, obligation, used>>
Next == Add \/ (\E d \in 1..MaxTime : Extend(d)) \/ End \/ WorkerAttempt \/ Tick \/ Wake \/ Send \/ SendLostBefore \/
        SendLostAfter \/ Ack \/ GiveUp \/ Restart \/ Prune \/ Outside
Spec == Init /\ [][Next]_vars /\ WF_vars(Tick) /\ WF_vars(Wake) /\
        SF_vars(Send) /\ SF_vars(Ack) /\ WF_vars(GiveUp)
TypeOK == /\ now \in 0..MaxTime /\ mem \in {"empty", "ready", "answered", "lostafter"}
          /\ machine \in {"RUNNING", "STOPPED"} /\ actor \in {"coordinator", "worker"}
          /\ busRecs \subseteq [token : {1, 2}, at : 0..MaxTime] /\ copies \in [Recipients -> Nat]
OnlyTheCoordinatorWrites == writer = "coordinator"
NeverDeliveredTwice == \A r \in Recipients : copies[r] <= 1
NoSendAfterRetryBefore == ~lateSend
RetryInsideCleanup == Life <= Cleanup /\
    (grants.delivery.state # "none" => grants.delivery.retryBefore - grants.delivery.committed = Life)
EveryExpiryIsToldOrJudged == obligation ~> ((\A r \in Recipients : copies[r] >= 1) \/ judged)
=============================================================================
