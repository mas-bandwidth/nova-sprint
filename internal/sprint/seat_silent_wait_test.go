package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// An overdue "the seat is not answering" holds its cards from progressing (wait_refused.go;
// docs/SPEC-SPRINT.md section 8, "A judgment that holds a card"), so the inbox prints no wait
// it refuses: the act line and its SeatSilentDecision both name the way out the judgment
// names, nova-sprint inbox (seat_answers.go).
func TestSeatSilentJudgmentPrintsNoRefusedWaitCommand(t *testing.T) {
	t.Parallel()
	n := Note{ID: "j-seat", Kind: Judgment, Type: NSeatSilent, Stream: "s1",
		Primaries: []string{"s1-1", "s1-2"}, StreamLevel: true, Decisions: []string{SeatSilentDecision}}
	g := Group{ID: n.ID, Kind: Judgment, Type: n.Type, Stream: n.Stream, Size: 2, Notes: []string{n.ID},
		Members: []string{"s1-1", "s1-2"}, Decisions: []string{SeatSilentDecision, "act"}}
	onWork := func(id string) bool { return id == "s1-1" || id == "s1-2" }

	var lines []string
	for _, c := range commands(g, n, "", onWork) {
		assert.False(t, c.Decision == "wait" || strings.HasPrefix(c.Decision, "wait "),
			"%s offers a refused decision %q", NSeatSilent, c.Decision)
		for _, l := range c.Lines {
			assert.NotContains(t, l, "nova-sprint wait", "%s offers a refused line %q", NSeatSilent, l)
		}
		lines = append(lines, c.Lines...)
	}
	assert.NotEmpty(t, WaitRefusalOn(n, onWork), "%s holds its cards and must refuse wait", NSeatSilent)
	assert.Contains(t, strings.Join(lines, "\n"), "nova-sprint inbox",
		"%s prints the way out the judgment names", NSeatSilent)
}
