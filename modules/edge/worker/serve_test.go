package worker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// A row is joined into the paths the worker clears with os.RemoveAll
// (probe.go), so Check refuses a row that is not a plain name: "..", ".", a
// path separator, or longer than 64 bytes. The refusal names the row and the
// rule, and says to pass --as <machine name>; an empty row keeps the missing
// --as refusal. Valid rows pass.
func TestServeRefusesARowThatIsNotAName(t *testing.T) {
	t.Parallel()
	token := func(k string) (string, bool) { return gitTokenFixture, k == forge.TokenEnv }
	rule := "a row is a name of letters, digits, '-', '_' and '.', not \".\" or \"..\", at most 64 bytes: pass --as <machine name>"
	if err := (Config{Row: "", Harness: "script", LookupEnv: token}).Check(); err == nil || !strings.Contains(err.Error(), "missing --as <row>") {
		t.Fatalf("empty row: want the missing --as refusal, got %v", err)
	}
	for _, row := range []string{"..", ".", "a/b", "a\\b", strings.Repeat("a", 65)} {
		err := (Config{Row: row, Harness: "script", LookupEnv: token}).Check()
		if err == nil || !strings.Contains(err.Error(), rule) || !strings.Contains(err.Error(), fmt.Sprintf("--as %q", row)) || strings.Contains(err.Error(), "\n") {
			t.Fatalf("row %q: want a one-line refusal naming the row and %q, got %v", row, rule, err)
		}
	}
	for _, row := range []string{"amd-1", "mini-m5", "hetzner1", strings.Repeat("a", 64)} {
		if err := (Config{Row: row, Harness: "script", LookupEnv: token}).Check(); err != nil {
			t.Fatalf("row %q refused: %v", row, err)
		}
	}
}
