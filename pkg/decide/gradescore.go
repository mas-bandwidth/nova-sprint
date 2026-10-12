package decide

// Grading the grades (SPEC-NOVA-DECIDE section 11, "Scoring the grades"): the grade
// decisions made in one UTC day, joined by card to what the sprint log says happened to
// the card, tabled by Jev's grade against the tier the card was first dealt on, and by the
// p of its grade against whether the flash first attempt failed.

// LogLine is one line of `nova-sprint log --json`, the fields the score reads.
type LogLine struct {
	Card    string            `json:"card"`
	Primary string            `json:"primary"`
	Table   string            `json:"table"`
	To      string            `json:"to"`
	Removed bool              `json:"removed"`
	Set     map[string]string `json:"set"`
}

// CardFacts is what the log holds of one card: where it ended, the tier of its first
// attempt ("" when it was never dealt or attempt 1 is before the log), the highest attempt
// number, whether attempt 1 failed, and whether a pro attempt ran.
type CardFacts struct {
	State        string // landed, dropped, or the column it sits in
	Start        string
	MaxAttempt   int
	FirstFailed  bool
	Escalated    bool
	attempts     map[int]costRecord
	dealtOnFleet bool
}

type costRecord struct {
	attempt          int
	tier, end, route string
}

// GradeRow is one line of the grade-by-dealt table: the cards Jev graded Grade that were
// first dealt on Dealt.
type GradeRow struct {
	Grade, Dealt                             string
	N, Landed2, Landed, ToPro, Dropped, Open int
}

// BucketRow is one line of the calibration table: the cards graded Grade, dealt on flash,
// whose p for the grade fell in Bucket.
type BucketRow struct {
	Grade, Bucket           string
	N, FirstFailed, Landed2 int
}

// GradeScore is the day's score.
type GradeScore struct {
	Decisions, Cards, NoLog int // grade decisions in the day, the cards they grade, cards the log lacks
	Rows                    []GradeRow
	Buckets                 []BucketRow
}

var gradeBuckets = []struct {
	name   string
	lo, hi float64
}{{"<0.7", 0, 0.7}, {"0.7-0.85", 0.7, 0.85}, {"0.85-0.95", 0.85, 0.95}, {">=0.95", 0.95, 1.01}}
