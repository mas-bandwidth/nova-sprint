package policy

import (
	"os"
	"testing"
)

// TestMain is also the policy evaluator: a process engine runs this test
// binary as its child, "policy.test policy-eval", as layer 6's binary runs
// "nova-sprint policy-eval".
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == EvalArg {
		os.Exit(ServeMain())
	}
	os.Exit(m.Run())
}
