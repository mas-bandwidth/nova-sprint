package ci

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-17 (Glenn): the allowance only shrinks, by use and not only by file.
// unit_rule_allowed.txt allowed a listed file any number of new forbidden uses
// (its documented limit); a counted allowance refuses a use above the file's count
// for its kind, and a networked program run by exec is a socket.
func TestTheUnitRuleAllowanceIsCountedAndSeesANetworkedExec(t *testing.T) {
	t.Run("a use above the file's count is refused", func(t *testing.T) {
		findings := []unitRuleFinding{
			{File: "p/x_test.go", Line: 3, Kind: "time", What: "time.Sleep"},
			{File: "p/x_test.go", Line: 9, Kind: "time", What: "time.Now"},
			{File: "p/y_test.go", Line: 4, Kind: "time", What: "time.Now"},
		}
		allowed := map[string]map[string]int{"p/x_test.go": {"time": 1}, "p/y_test.go": {"time": 1}}
		assert.Equal(t, []string{"p/x_test.go: time uses 2 > allowed 1"}, unitRuleOverAllowance(findings, allowed))
	})
	t.Run("a networked exec is a socket", func(t *testing.T) {
		got, err := scanUnitRule(map[string][]byte{"p/x_test.go": []byte(
			"package p\nimport \"os/exec\"\nfunc TestX() { exec.Command(\"bash\", \"-c\", \"exec 3<>/dev/tcp/127.0.0.1/1\") }\n")})
		require.NoError(t, err)
		var out []string
		for _, f := range got {
			out = append(out, fmt.Sprintf("%d %s %s", f.Line, f.Kind, f.What))
		}
		assert.Equal(t, []string{"3 socket os/exec.Command bash /dev/tcp"}, out)
	})
}
