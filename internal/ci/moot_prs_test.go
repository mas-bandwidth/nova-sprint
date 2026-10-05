package ci

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMootPRsAreClosedWithAPointer pins docs/SPLIT-MOOT-PRS.md: a PR is marked closed only
// with a pointer comment, and a PR whose change is absent from main is never marked closed.
func TestMootPRsAreClosedWithAPointer(t *testing.T) {
	b, err := os.ReadFile("../../docs/SPLIT-MOOT-PRS.md")
	require.NoError(t, err)
	seen := map[string]bool{}
	closed := 0
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| PR ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		f := strings.Split(strings.Trim(line, "| "), " | ")
		switch len(f) {
		case 4:
			pr, decision, evidence := f[0], f[2], f[3]
			require.False(t, seen[pr], "PR %s listed twice", pr)
			seen[pr] = true
			switch decision {
			case "closed":
				closed++
				require.Contains(t, evidence, "present in main", pr)
				require.Contains(t, evidence, "https://github.com/mas-bandwidth/nova-tools/pull/"+pr+"#issuecomment-", pr)
				require.Len(t, f[1], 40, "a closed PR names its full head: %s", pr)
			case "already-closed", "open-absent":
				require.NotContains(t, evidence, "present in main", pr)
			default:
				require.Failf(t, "unknown decision", "%s: %s", pr, decision)
			}
		case 5:
			pr, head, decision, splitDisp, transferPtr := f[0], f[1], f[2], f[3], f[4]
			require.False(t, seen[pr], "PR %s listed twice", pr)
			seen[pr] = true
			require.Len(t, head, 40, "a mixed PR names its full head: %s", pr)
			require.Equal(t, "open-split", decision, pr)
			require.NotEmpty(t, splitDisp, pr)
			require.Contains(t, transferPtr, "https://github.com/mas-bandwidth/nova-tools/pull/"+pr, pr)
		default:
			require.Failf(t, "unexpected table columns", "%d in line: %s", len(f), line)
		}
	}
	require.Equal(t, 2, closed)
	for _, pr := range []string{"5306", "5234"} {
		require.True(t, seen[pr], pr)
	}
	for _, pr := range []string{"5275", "5307"} {
		require.True(t, seen[pr], pr)
	}
}
