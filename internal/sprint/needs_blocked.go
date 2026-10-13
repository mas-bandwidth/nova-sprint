package sprint

// NNeedBlocked is the judgment raised when an unlanded card's need is held, dropped or absent.
const NNeedBlocked = "a need is blocked: its dependents cannot move"

// Blocker is a card that blocks unlanded dependents.
type Blocker struct {
	Card       *Card
	Dependents []Dependent
}

// Dependent is an unlanded card waiting on a blocker.
type Dependent struct {
	ID     string
	Stream string
}

// Blockers computes all blockers and their transitive dependents in s.
func Blockers(s *Snapshot) []Blocker {
	return nil
}

// TickNeedBlocked raises and retires NNeedBlocked judgments.
func TickNeedBlocked(s *Snapshot, r TickReq) (Plan, int) {
	return Plan{}, 0
}
