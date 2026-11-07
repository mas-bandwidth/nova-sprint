package verbs

import (
	"context"
	"strings"
	"testing"
)

func TestRowConfigurationMovesToNovaConfig(t *testing.T) {
	e := Engine{}
	for _, kind := range []string{"worker", "friend"} {
		got := e.row(context.Background(), []string{"add", "r", "--kind", kind, "--width", "2"})
		if got.Exit != 1 || !strings.Contains(got.Output, "set it in nova-config") || !strings.Contains(got.Output, "nova-sprint hook") {
			t.Fatal(got)
		}
	}
}
