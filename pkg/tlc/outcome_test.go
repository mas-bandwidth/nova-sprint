package tlc

import (
	"testing"
)

const cleanFinish = "Model checking completed. No error has been found.\n1 states generated, 1 distinct states found\n"

// TestParseAcceptsACleanFinish: a clean finish is completed, counts the states
// and violates nothing.
func TestParseAcceptsACleanFinish(t *testing.T) {
	t.Parallel()
	o := Parse(cleanFinish)
	if !o.Completed {
		t.Fatal("clean finish is not completed")
	}
	if o.Generated != "1" || o.Distinct != "1" {
		t.Fatalf("stats = %s/%s want 1/1", o.Generated, o.Distinct)
	}
	if len(o.Violations) != 0 {
		t.Fatalf("clean finish reported violations: %+v", o.Violations)
	}
	if o.InitialState {
		t.Fatal("clean finish marked an initial-state violation")
	}
}

// TestViolatedNamesAnInvariant: Parse reads a named invariant, and Violated
// reports it.
func TestViolatedNamesAnInvariant(t *testing.T) {
	t.Parallel()
	out := "Error: Invariant Safety is violated.\n1 states generated, 1 distinct states found\n"
	o := Parse(out)
	if o.Completed {
		t.Fatal("a violation is not completed")
	}
	if !o.Violated("Invariant", "Safety") {
		t.Fatal("Violated did not see Invariant Safety")
	}
	if o.Violated("Invariant", "Other") {
		t.Fatal("Violated saw a different invariant")
	}
	if o.Violated("Action property", "Safety") {
		t.Fatal("Violated conflated kind")
	}
	if !anyViolated(o, "Invariant", "Safety|Another") {
		t.Fatal("anyViolated missed a name in the | list")
	}
	if anyViolated(o, "Invariant", "Nope|AlsoNope") {
		t.Fatal("anyViolated saw a name that was not violated")
	}
}

// TestParseMarksAnInitialStateViolation: "violated by the initial state" sets
// InitialState and reports 1 state as generated and distinct.
func TestParseMarksAnInitialStateViolation(t *testing.T) {
	t.Parallel()
	out := "Error: Invariant InitOK is violated by the initial state:\n"
	o := Parse(out)
	if !o.InitialState {
		t.Fatal("initial-state violation not marked")
	}
	if o.Generated != "1" || o.Distinct != "1" {
		t.Fatalf("initial-state stats = %s/%s want 1/1", o.Generated, o.Distinct)
	}
	if !o.Violated("Invariant", "InitOK") {
		t.Fatal("initial-state invariant not read")
	}
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
	if len(tv) != 1 || tv[0].Kind != "Temporal property" || tv[0].Name != "EventuallyAlways" {
		t.Fatalf("TemporalViolations = %+v", tv)
	}

	generic := "Temporal properties were violated.\n"
	o = Parse(generic)
	tv = o.TemporalViolations()
	if len(tv) != 1 || tv[0].Kind != "Temporal property" || tv[0].Name != "" {
		t.Fatalf("generic TemporalViolations = %+v", tv)
	}
}

// TestParseReadsADeadlock: a deadlock fragment is read without being mistaken
// for a completion.
func TestParseReadsADeadlock(t *testing.T) {
	t.Parallel()
	out := "Error: Deadlock reached.\n" +
		"The error occurred when TLC was evaluating the next-state relation.\n" +
		"1 states generated, 1 distinct states found\n"
	o := Parse(out)
	if o.Completed {
		t.Fatal("a deadlock is not a completion")
	}
	if len(o.Violations) != 0 {
		t.Fatalf("deadlock reported violations: %+v", o.Violations)
	}
}

// TestParseDoesNotCompleteACutShortOutput: output cut short by a timeout never
// completes.
func TestParseDoesNotCompleteACutShortOutput(t *testing.T) {
	t.Parallel()
	o := Parse("java.lang.OutOfMemoryError\n... (output truncated by timeout)\n")
	if o.Completed {
		t.Fatal("cut-short output completed")
	}
	if len(o.Violations) != 0 {
		t.Fatalf("cut-short output reported violations: %+v", o.Violations)
	}
}

// TestAcceptsPinsPassAndViolations: Accepts accepts a pass on exit 0 plus the
// completion line, an invariant on exit 12 plus the named violation, and a
// temporal on exit 13 plus the named property selected by the config.
func TestAcceptsPinsPassAndViolations(t *testing.T) {
	t.Parallel()
	passCase := Case{Expected: "pass"}
	if !Accepts(passCase, ExitPass, cleanFinish, "") {
		t.Fatal("Accepts refused a clean pass")
	}
	if Accepts(passCase, ExitInvariant, cleanFinish, "") {
		t.Fatal("Accepts accepted pass with exit 12")
	}

	inv := Case{Expected: "invariant", Property: "Safety"}
	if !Accepts(inv, ExitInvariant, "Error: Invariant Safety is violated.\n", "") {
		t.Fatal("Accepts refused a matching invariant")
	}
	if Accepts(inv, ExitPass, cleanFinish, "") {
		t.Fatal("Accepts accepted an invariant on a clean finish")
	}

	act := Case{Expected: "action", Property: "A"}
	if !Accepts(act, ExitProperty, "Error: Action property A is violated.\n", "") {
		t.Fatal("Accepts refused a matching action property")
	}

	tmp := Case{Expected: "temporal", Property: "EventuallyAlways"}
	cfg := "COMPUTE\nPROPERTY EventuallyAlways\n"
	if !Accepts(tmp, ExitProperty, "Error: Temporal property EventuallyAlways was violated.\n", cfg) {
		t.Fatal("Accepts refused a matching temporal property")
	}
	if Accepts(tmp, ExitProperty, "Error: Temporal property SomethingElse was violated.\n", cfg) {
		t.Fatal("Accepts accepted a temporal violation of another property")
	}
}

// TestAcceptsRefusesAPassWithAViolation: the pass branch refuses an output that
// carries a violation line, even under the completion line and exit 0. A
// violation read as a pass is the worst case this API can make.
func TestAcceptsRefusesAPassWithAViolation(t *testing.T) {
	t.Parallel()
	passCase := Case{Expected: "pass"}
	mixed := cleanFinish + "Error: Invariant Safety is violated.\n"
	if Accepts(passCase, ExitPass, mixed, "") {
		t.Fatal("Accepts accepted a pass that also reads as an invariant violation")
	}
}
