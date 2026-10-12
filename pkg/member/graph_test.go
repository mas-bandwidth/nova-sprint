package member

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSprintUsesItsMember pins the command's package graph to this loop, the
// same package its functional member tests exercise.
func TestSprintUsesItsMember(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "list", "-deps", "./cmd/nova-sprint")
	cmd.Dir = "../.."
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	const owner = "github.com/mas-bandwidth/nova-sprint/pkg/member"
	var members []string
	for _, path := range strings.Fields(string(output)) {
		if strings.HasSuffix(path, "/pkg/member") {
			members = append(members, path)
		}
	}
	require.Equal(t, []string{owner}, members, "the sprint command must have one member loop")
}
