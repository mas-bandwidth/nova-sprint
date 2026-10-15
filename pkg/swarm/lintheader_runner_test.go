package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The TEST line names a test in any runner the gate runs: beside the Go form
// `[-tags <tags>] <package> <TestName>` and `none <why>`, test-named accepts
// `<runner> <file or target> <case>` for make, ctest, cargo, dotnet and pytest, so a
// C++ or C# card names its test instead of writing TEST: none. A line in neither form
// is still refused.
func TestTheTestLineNamesAnyRunner(t *testing.T) {
	t.Parallel()
	card := func(test string) []byte {
		return []byte("RESULT: c1 sha=0\nKIND: fix-red\nPATHS: src/x.cpp\nTEST: " + test + "\n\nT.\n")
	}
	accepted := []string{
		"make test/delta_test.cpp DeltaBitsWiderThan32",
		"dotnet tests/Netcode.Tests NoncesDifferBetweenServers",
		"ctest build ReliableAcksWrap",
		"cargo tests/rocketnet.rs acks_wrap_at_u16",
		"pytest tests/test_fixed3d.py test_dot_product",
		"pkg/swarm TestTheTestLineNamesAnyRunner",
		"-tags functional ./pkg/swarm TestA",
		"ctest DeltaBits",
	}
	for _, line := range accepted {
		got := findingsFor(LintCardHeader(card(line), nil, false), "test-named")
		assert.Empty(t, got, "TEST: %s drew %+v; want accepted", line, got)
	}
	refused := []string{
		"some prose",
		"make test/delta_test.cpp",                 // no case
		"make test/delta_test.cpp Delta Bits",      // a fourth word
		"gradle test/delta_test.cpp DeltaBits",     // not a runner the gate runs
		"make ../outside/delta_test.cpp DeltaBits", // the target walks out
		"make /abs/delta_test.cpp DeltaBits",       // an absolute target
		"dotnet tests/Netcode.Tests Nonces;rm",     // a case that is not a name
		"pkg/swarm notAGoTestName",                 // the Go form is unchanged
	}
	for _, line := range refused {
		got := findingsFor(LintCardHeader(card(line), nil, false), "test-named")
		assert.NotEmpty(t, got, "TEST: %s drew no test-named finding; want refused", line)
	}
}
