package tlc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const cleanFinish = "Model checking completed. No error has been found.\n1 states generated, 1 distinct states found\n"

// TestParseAcceptsACleanFinish: a clean finish is completed, counts the states
// and violates nothing.
func TestParseAcceptsACleanFinish(t *testing.T) {
	t.Parallel()
	o := Parse(cleanFinish)
	assert.True(t, o.Completed, "clean finish is not completed")
	assert.Equal(t, "1", o.Generated)
	assert.Equal(t, "1", o.Distinct)
	assert.Empty(t, o.Violations, "clean finish reported violations")
	assert.False(t, o.InitialState, "clean finish marked an initial-state violation")
}

// TestViolatedNamesAnInvariant: Parse reads a named invariant, and Violated
// reports it.
func TestViolatedNamesAnInvariant(t *testing.T) {
	t.Parallel()
	out := "Error: Invariant Safety is violated.\n1 states generated, 1 distinct states found\n"
	o := Parse(out)
	assert.False(t, o.Completed, "a violation is not completed")
	assert.True(t, o.Violated("Invariant", "Safety"), "Violated did not see Invariant Safety")
	assert.False(t, o.Violated("Invariant", "Other"), "Violated saw a different invariant")
	assert.False(t, o.Violated("Action property", "Safety"), "Violated conflated kind")
	assert.True(t, anyViolated(o, "Invariant", "Safety|Another"), "anyViolated missed a name in the | list")
	assert.False(t, anyViolated(o, "Invariant", "Nope|AlsoNope"), "anyViolated saw a name that was not violated")
}

// TestParseMarksAnInitialStateViolation: "violated by the initial state" sets
// InitialState and reports 1 state as generated and distinct.
func TestParseMarksAnInitialStateViolation(t *testing.T) {
	t.Parallel()
	out := "Error: Invariant InitOK is violated by the initial state:\n"
	o := Parse(out)
	assert.True(t, o.InitialState, "initial-state violation not marked")
	assert.Equal(t, "1", o.Generated)
	assert.Equal(t, "1", o.Distinct)
	assert.True(t, o.Violated("Invariant", "InitOK"), "initial-state invariant not read")
}

// TestTemporalViolationsReadNamedAndGeneric: a named temporal property and the
// older generic line both appear in TemporalViolations.
func TestTemporalViolationsReadNamedAndGeneric(t *testing.T) {
	t.Parallel()
	named := "Error: Temporal property EventuallyAlways was violated.\n" +
		"Error: The behavior up to this point is:\n" +
		"State 1: <1 back-to-state loop> x = 1\n"
	o := Parse(named)
	tv := o.TemporalViolations()
	assert.Len(t, tv, 1)
	assert.Equal(t, "Temporal property", tv[0].Kind)
	assert.Equal(t, "EventuallyAlways", tv[0].Name)

	generic := "Temporal properties were violated.\n"
	o = Parse(generic)
	tv = o.TemporalViolations()
	assert.Len(t, tv, 1)
	assert.Equal(t, "Temporal property", tv[0].Kind)
	assert.Equal(t, "", tv[0].Name)
}

// TestParseReadsADeadlock: a deadlock fragment is read without being mistaken
// for a completion.
func TestParseReadsADeadlock(t *testing.T) {
	t.Parallel()
	out := "Error: Deadlock reached.\n" +
		"The error occurred when TLC was evaluating the next-state relation.\n" +
		"1 states generated, 1 distinct states found\n"
	o := Parse(out)
	assert.False(t, o.Completed, "a deadlock is not a completion")
	assert.Empty(t, o.Violations, "deadlock reported violations")
}

// TestParseDoesNotCompleteACutShortOutput: output cut short by a timeout never
// completes.
func TestParseDoesNotCompleteACutShortOutput(t *testing.T) {
	t.Parallel()
	o := Parse("java.lang.OutOfMemoryError\n... (output truncated by timeout)\n")
	assert.False(t, o.Completed, "cut-short output completed")
	assert.Empty(t, o.Violations, "cut-short output reported violations")
}

// TestAcceptsPinsPassAndViolations: Accepts accepts a pass on exit 0 plus the
// completion line, an invariant on exit 12 plus the named violation, and a
// temporal on exit 13 plus the named property selected by the config.
func TestAcceptsPinsPassAndViolations(t *testing.T) {
	t.Parallel()
	passCase := Case{Expected: "pass"}
	assert.True(t, Accepts(passCase, ExitPass, cleanFinish, ""), "Accepts refused a clean pass")
	assert.False(t, Accepts(passCase, ExitInvariant, cleanFinish, ""), "Accepts accepted pass with exit 12")

	inv := Case{Expected: "invariant", Property: "Safety"}
	assert.True(t, Accepts(inv, ExitInvariant, "Error: Invariant Safety is violated.\n", ""), "Accepts refused a matching invariant")
	assert.False(t, Accepts(inv, ExitPass, cleanFinish, ""), "Accepts accepted an invariant on a clean finish")

	act := Case{Expected: "action", Property: "A"}
	assert.True(t, Accepts(act, ExitProperty, "Error: Action property A is violated.\n", ""), "Accepts refused a matching action property")

	tmp := Case{Expected: "temporal", Property: "EventuallyAlways"}
	cfg := "COMPUTE\nPROPERTY EventuallyAlways\n"
	assert.True(t, Accepts(tmp, ExitProperty, "Error: Temporal property EventuallyAlways was violated.\n", cfg), "Accepts refused a matching temporal property")
	assert.False(t, Accepts(tmp, ExitProperty, "Error: Temporal property SomethingElse was violated.\n", cfg), "Accepts accepted a temporal violation of another property")
}
