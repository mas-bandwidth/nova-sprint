package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 2026-10-17: fuzz-viewrowswire-prefix-oracle.w1 ended 137 three times (hetzner2, amd-1)
// and its bound judgment said only "exits 137, 137, 137; last: }": the first line of
// the harness tail. The tail's last lines named the cause, the sandbox wall's memory
// cap: `SANDBOX RUNAWAY runaway: 8714186752 bytes of memory (cap 8589934592); the
// process tree was killed`. The judgment names the wall's cap and its numbers.
func TestAnEnvExitBoundNamesTheWallsCapAndItsNumbers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, line string
		want       []string
	}{
		{"memory", "SANDBOX RUNAWAY runaway: 8714186752 bytes of memory (cap 8589934592); the process tree was killed",
			[]string{"memory cap", "8714186752", "8589934592"}},
		{"processes", "SANDBOX RUNAWAY runaway: 714 processes (cap 512); the process tree was killed",
			[]string{"process cap", "714", "512"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tail := "\t}\n}\nGOEOF\nok  \tgithub.com/mas-bandwidth/nova-sprint/internal/ci\t9.185s\n" + tc.line + "\nSANDBOX DONE exit=137 cmd=opencode\n"
			wc := &Card{ID: "p1.w1", Row: "m1", Col: Withdrawn, Fields: map[string]string{
				"attempt": "1", FieldEnvExitHistory: "m1=137,m2=137,m1=137", FieldHarnessTail: tail}}
			what := envExitBoundWhat(wc, "p1")
			for _, w := range tc.want {
				assert.Contains(t, what, w, "the judgment names the wall's cap: %s", what)
			}
		})
	}
	t.Run("no wall line keeps the first line", func(t *testing.T) {
		t.Parallel()
		wc := &Card{ID: "p1.w1", Row: "m1", Col: Withdrawn, Fields: map[string]string{
			"attempt": "1", FieldEnvExitHistory: "m1=134,m2=134,m1=134", FieldHarnessTail: "Error: Unknown: ChildProcess.spawn\nmore\n"}}
		assert.Contains(t, envExitBoundWhat(wc, "p1"), "last: Error: Unknown: ChildProcess.spawn")
	})
}
