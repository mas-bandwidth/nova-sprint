------------------------ MODULE MCCardMachineBrokenReadsInReview -------------------------
\* The reversed witness on the small instance: DealRead without its
\* where[c] = "review" guard, so a ready card gets a read copy cut while
\* its primary is not in review. Expected: ReadsInReview is violated.
EXTENDS CardMachine

SmallCards     == {"c1"}
SmallConsumers == {"k1", "k2"}
SmallSlots     == [k \in SmallConsumers |-> 1]
SmallDeps      == [c \in SmallCards |-> {}]
SmallHeadBound == HeadBound(1)

\* CardMachine.DealRead minus the where[c] = "review" guard.
DealReadBroken(c, k) ==
  /\ Bare(c)
  /\ up[k] /\ Room(k) > 0 /\ k /= author[c] /\ CanCut(c)
  /\ reads' = [reads EXCEPT ![c] = {NextCopy(c)}]
  /\ ncut' = [ncut EXCEPT ![c] = @ + 1]
  /\ cw' = CutCW(c, k) /\ ck' = CutCK(c, k) /\ cl' = CutCL(c, "read")
  /\ UNCHANGED <<where, ok, copy, pending, low, author, head, prHead, ci, leased, up>>

NextBroken ==
  \/ \E c \in Cards : Push(c) \/ Release(c) \/ DealReturn(c) \/ CIWord(c) \/ PRHeadMoves(c)
                      \/ Verdict(c) \/ CancelPrimary(c) \/ Land(c) \/ LandEvent(c)
  \/ \E c \in Cards, k \in Consumers : DealWork(c, k) \/ DealReadBroken(c, k)
  \/ \E i \in CopyId : Work(i) \/ Beat(i) \/ Lapse(i) \/ Expire(i) \/ DownReady(i)
                       \/ GiveBack(i) \/ EndFail(i) \/ EndWorkPR(i) \/ EndWorkDone(i)
                       \/ EndReadHigh(i) \/ EndReadStale(i) \/ EndReadLow(i) \/ EndReadFail(i)
                       \/ EndFixOK(i)
  \/ \E k \in Consumers : Down(k) \/ Up(k)

BrokenSpec == Init /\ [][NextBroken]_vars
========================================================================================
