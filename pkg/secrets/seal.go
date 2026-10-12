package secrets

import (
	"bytes"
	"context"
	"io"

	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
)

// execCommand runs one helper process and returns its stdout. The encrypt step
// takes it as a parameter so a test can supply a pure-Go fake on every platform
// instead of a shell script Windows cannot execute.
type execCommand func(stdin io.Reader, env []string, dir, name string, args ...string) ([]byte, error)

// stderrCarrier is a helper child's error that also carries the stderr the child wrote, so
// a verb can name the class of a failure without the seam's signature growing a return. The
// real runner returns a childError; a test's fake over the same seam returns its own.
type stderrCarrier interface{ Stderr() string }

// childError is a helper child's failure with the stderr it wrote. The transcript is kept
// rather than passed through, so a verb names the class of the failure (a wrong key's, for
// one) and never echoes a value the transcript might hold (STEP 3, "wrong key").
type childError struct {
	err    error
	stderr string
}

func (e *childError) Error() string  { return e.err.Error() }
func (e *childError) Unwrap() error  { return e.err }
func (e *childError) ExitCode() int  { return exitCodeOf(e.err, 1) }
func (e *childError) Stderr() string { return e.stderr }

func realExecCommand(stdin io.Reader, env []string, dir, name string, args ...string) ([]byte, error) {
	// The deadline is the kind of the program named: git, gh, sops and the rest each have
	// their own default (subproc.KindOf).
	cmd, cancel := subproc.CommandFor(context.Background(), subproc.BudgetOf(name, args), name, args...)
	defer cancel()
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = stdin
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		return out.Bytes(), &childError{err: err, stderr: errBuf.String()}
	}
	return out.Bytes(), nil
}

