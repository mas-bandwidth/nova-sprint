package sprint

import (
	"fmt"
	"strconv"
	"strings"
)

// THE DEAL THROTTLES NEW WORK WHEN LANDING BACKS UP (card
// v17-deal-throttles-new-work-when-landing-backs-up; the model is tla/DealThrottle.tla). The
// owner, Glenn, 2026-10-18: "landing ... must be the highest priority thing because it's
// things that will free up more work, and unblock dependencies, and they are DONE we just
// have to land them", and "if reading, fixing and landing are all higher priority than
// work, the system balances. if the same, or lower, then the system backs up with more work
// coming in until it collapses."
//
// The ladder already deals reads and fixes before new work in each tick (priority.go,
// tla/DirtyTick.tla ReadsBeforeWork). The throttle is the next rung: while the landing
// queue (the work table's merging column) holds more primaries than its bound, or review
// holds more than its bound, the tick's deal starts no NEW work attempt (NewWorkAttempt):
// reads, fix-level cards and every next attempt still deal, and nothing working or dealt is
// cut, recalled or told to stop (InFlightNeverCut). A held card stays ready and is dealt on
// the first tick after the count falls to or under its bound (WorkResumes). The throttle
// raises no judgment: the ready-over part explains a held card by it and never alarms on it
// (ready_over.go), and where shows it once as a line (ThrottleLine). Both bounds are off by
// default (set --land-backlog-bound, --review-backlog-bound), so nothing changes until the
// coordinator sets one.

const (
	// PropLandBacklogBound is the work table's property: the land backlog bound, the
	// primaries merging above which the deal starts no new work; off (the default), 0 or
	// absent never throttles.
	PropLandBacklogBound = "land_backlog_bound"
	// PropReviewBacklogBound is the work table's property: the review backlog bound, the
	// primaries in review above which the deal starts no new work; off as the land bound.
	PropReviewBacklogBound = "review_backlog_bound"
	// BacklogBoundOff is a backlog bound's word for never throttling, its default.
	BacklogBoundOff = "off"
)

// backlogBoundValid says v is a value set --land-backlog-bound or --review-backlog-bound
// takes: a whole number from 0 (0 is off), off, or default (off).
func backlogBoundValid(v string) bool {
	if v == BacklogBoundOff || v == ReadTierDefault {
		return true
	}
	n, err := strconv.Atoi(v)
	return err == nil && n >= 0
}

// backlogBound is a backlog bound as the work table's properties hold it: the count, and
// 0 when it is off (absent, off, default, 0, or a value it does not take).
func backlogBound(props map[string]string, prop string) int {
	n, err := strconv.Atoi(props[prop])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// workThrottled is the throttle's rule over the work table's properties and its two counts
// (primaries in review and merging, sentinels left out): true while a bound that is on is
// exceeded, with the reason naming each count and its bound. WorkThrottled (the deal) and
// ThrottleLine (where) both read it, so the deal and the line never disagree.
// tla/DealThrottle.tla Throttled.
func workThrottled(props map[string]string, review, merging int) (bool, string) {
	var why []string
	if b := backlogBound(props, PropLandBacklogBound); b > 0 && merging > b {
		why = append(why, fmt.Sprintf("%d primaries merging, above the land backlog bound of %d", merging, b))
	}
	if b := backlogBound(props, PropReviewBacklogBound); b > 0 && review > b {
		why = append(why, fmt.Sprintf("%d primaries in review, above the review backlog bound of %d", review, b))
	}
	if len(why) == 0 {
		return false, ""
	}
	return true, strings.Join(why, "; ")
}

// backlogCounts is the primaries in review and merging on the work table, sentinels left
// out (a sentinel is a stop, not a card: the owner, 2026-10-14); a read card lives on the
// fleet table and is never counted.
func (s *Snapshot) backlogCounts() (review, merging int) {
	if s == nil || s.Work == nil {
		return 0, 0
	}
	for _, c := range s.Work.Column(Review) {
		if !IsSentinel(c) {
			review++
		}
	}
	for _, c := range s.Work.Column(Merging) {
		if !IsSentinel(c) {
			merging++
		}
	}
	return review, merging
}

// WorkThrottled says the deal starts no new work now: merging is above the land backlog
// bound, or review above the review backlog bound (a bound off never throttles); why names
// the count and the bound. tla/DealThrottle.tla Throttled.
func (s *Snapshot) WorkThrottled() (bool, string) {
	if s == nil || s.Work == nil {
		return false, ""
	}
	review, merging := s.backlogCounts()
	return workThrottled(s.Work.Props(), review, merging)
}

// NewWorkAttempt says the ready primary c would start a NEW work attempt: its next deal
// cuts attempt 1 (it has run none: not a rework, a redo or a redeal), it is not at a fix
// level (blocker, critical, fix; its set level, as the ladder orders it, cardRank), and it
// is not a read card. Only such a card is held by the throttle. tla/DealThrottle.tla IsNew.
func NewWorkAttempt(c *Card) bool {
	if c == nil || c.F("kind") == "read" || IsSentinel(c) || c.Int("attempt") > 0 {
		return false
	}
	switch c.F(FieldPriority) {
	case PriorityBlocker, PriorityCritical, PriorityFix:
		return false
	}
	return true
}

// throttleHolds says the throttle holds the ready primary c in ready now, with why.
func (s *Snapshot) throttleHolds(c *Card) (bool, string) {
	if !NewWorkAttempt(c) {
		return false, ""
	}
	on, why := s.WorkThrottled()
	if !on {
		return false, ""
	}
	return true, throttleWhy(why)
}

// throttleWhy is the sentence for a new work card the throttle holds in ready.
func throttleWhy(why string) string {
	return "the deal starts no new work while landing backs up (" + why + "): it stays ready and is dealt on the first tick the count is at or under its bound; reads, fixes and next attempts still deal"
}

// throttledOut is the tick deal's filter (TickDeal, friends and machines alike): the ready
// cards with every new work attempt taken out while the throttle stands, and the one
// refusal that names the held cards' count and the reason (none when nothing is held). It
// writes no card: a card working or dealt is never touched (tla/DealThrottle.tla
// InFlightNeverCut).
func throttledOut(s *Snapshot, ready []*Card) ([]*Card, []Refusal) {
	on, why := s.WorkThrottled()
	if !on {
		return ready, nil
	}
	out := make([]*Card, 0, len(ready))
	held := 0
	for _, c := range ready {
		if NewWorkAttempt(c) {
			held++
			continue
		}
		out = append(out, c)
	}
	if held == 0 {
		return out, nil
	}
	return out, []Refusal{{Key: "throttle", Why: fmt.Sprintf("%d new work cards held in ready: %s", held, throttleWhy(why))}}
}

// ThrottleLine is where's one line for the throttle while it stands, "" else: the reason
// over the same counts where shows (review and merging, sentinels left out).
func ThrottleLine(props map[string]string, review, merging int) string {
	on, why := workThrottled(props, review, merging)
	if !on {
		return ""
	}
	return "throttle: no new work is dealt: " + why + " (nova-sprint set --land-backlog-bound, --review-backlog-bound)"
}

// init declares the two bounds' working defaults and their by-set declaration here, so the
// card's PATHS hold them (defaults.go and defaults_working.go are left unchanged): off, and
// carried across a clear as the coordinator's choice, as the backlog alarms' thresholds are.
func init() {
	Defaults = append(Defaults,
		Default{Name: PropLandBacklogBound, Home: HomeCarry, Value: BacklogBoundOff, Why: "the deal throttles new work only once the coordinator sets a land backlog bound"},
		Default{Name: PropReviewBacklogBound, Home: HomeCarry, Value: BacklogBoundOff, Why: "the deal throttles new work only once the coordinator sets a review backlog bound"})
	workingChecks[PropLandBacklogBound] = workingCheck{BySet: true}
	workingChecks[PropReviewBacklogBound] = workingCheck{BySet: true}
}
