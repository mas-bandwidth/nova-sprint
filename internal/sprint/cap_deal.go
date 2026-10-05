package sprint

// PartCapDeal is the attempt cap's default answer, the pump's part before the deal
// (TickCapDeal, brief_bound.go): a card past its cap goes to a frontier or heavy friend
// with room before the deal could give it to a machine.
const PartCapDeal = "cap deal"

// TickCapDeal is the attempt cap's default answer as a part of the tick (AttemptCapDeal).
// The model is tla/CapDeal.tla (the cap deal and the deal's side of the same cards, beside
// the tick's shape, tla/DirtyTick.tla, which holds none of their state). Its rules: a card
// past its cap is never dealt to a machine while a frontier or heavy friend the tick read
// up has room (CapDealtToFriendFirst), the machine never starts an attempt past the cap
// (NoMachineAttemptPastCap), a capped card at its redeal bound with no such friend leaves
// the deal with exactly one judgment open (BoundJudgedOnce), and no ready card is left
// neither placeable nor judged (NoCardLost). MCCapDeal.cfg checks the design; the code
// keeps three gaps outside this part, each a recorded counterexample: TickDeal deals a
// capped card to a machine when the stream's hold or cap changed between the two parts
// (MCCapDealBrokenBetween), deals a capped card that was not withdrawn as its next attempt
// (MCCapDealBrokenNewAttempt), and nothing deals or judges a capped card taken back from
// the friend its brief names (MCCapDealBrokenPinned).
func TickCapDeal(s *Snapshot, r TickReq) (Plan, int) { return AttemptCapDeal(s, r), 0 }
