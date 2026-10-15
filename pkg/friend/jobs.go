package friend

import (
	"regexp"
)

// The directories under her working directory that a staged job uses.
const (
	JobsDir = "jobs"
	JobFile = "JOB.md"
)

// LaneMarkFile is a job's lane mark, under jobs/<job>/.
const LaneMarkFile = "LANE"

// Card is one card a lane hands: its id (the queue file's), its brief, and
// the outbox directory its REPORT.md and RESULT.md go to.
type Card struct {
	ID     string `json:"id"`
	Brief  string `json:"brief"`
	Outbox string `json:"outbox"`
}

var (
	statusBranchRE = regexp.MustCompile(`push your work to the branch ([^\s;]+);`)
	fullSha        = regexp.MustCompile(`^[0-9a-f]{40}$`)
)
