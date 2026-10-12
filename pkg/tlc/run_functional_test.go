//go:build functional

// A real child process outliving a wall-clock deadline: a functional test (RULES 6a:
// no real time in a unit test).
package tlc

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestExecuteTimesOutWhenTheContextEnds: a run that outlives its context is
// reported as ExitTimeout.
func TestExecuteTimesOutWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runJavaScript(t, "exec sleep 5\n", func(java string) {
		r := Run{Java: java, Jar: filepath.Join(dir, "tlc.jar"), Dir: dir}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		code := Execute(ctx, r, filepath.Join(dir, "out.log"))
		if code != ExitTimeout {
			t.Fatalf("Execute=%d want %d", code, ExitTimeout)
		}
	})
}
