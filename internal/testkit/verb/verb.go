// Package verb runs a tool's entry point and checks the run as a fluent chain,
// and runs a table of refusals as parallel subtests. It is the front door to
// pkg/testkit's capture: testkit.Main.RunIn owns the two buffers and the call,
// and this package never copies that code. Every chain link is a require, so a
// broken link stops the test and prints the full captured stdout and stderr;
// require for the chain, assert beside it for independent findings.
//
//	verb.Run(t, main, "inspect", "--json").Code(0).Stdout("ok").NotStderr("panic")
//	verb.Refusals(t, main, []verb.Refusal{{Name: "no door", Args: nil, Code: 2, Stderr: "REFUSED"}})
//
// The raw streams stay readable through the chain's Exit, Out and Err for a
// caller's own assertions. The package imports testify and never reimplements
// it.
package verb

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/testkit"
)

// tb is the slice of testing.TB the chain reports through: require's own
// interface plus Helper. *testing.T satisfies it, as does the module's own
// recording fake.
type tb interface {
	require.TestingT
	Helper()
}

// Chain is one run of a tool's entry point: its exit code and everything each
// stream got. Run builds it, the check methods assert on it as require links,
// and Exit, Out and Err are read directly for a caller's own assertions.
type Chain struct {
	t    tb
	args []string
	Exit int
	Out  string
	Err  string
}

// Run calls main with args and an empty stdin and returns the chain. It never
// fails the test by itself: the first check link does.
func Run(t tb, main testkit.Main, args ...string) *Chain {
	t.Helper()
	r := main.Run(args...)
	return &Chain{t: t, args: args, Exit: r.Code, Out: r.Stdout, Err: r.Stderr}
}

// Code requires the exit code to be want.
func (c *Chain) Code(want int) *Chain {
	c.t.Helper()
	require.Equal(c.t, want, c.Exit, "%s: exit %d, want %d\n%s", c.verb(), c.Exit, want, c.dump())
	return c
}

// Stdout requires stdout to be exactly want.
func (c *Chain) Stdout(want string) *Chain {
	c.t.Helper()
	require.Equal(c.t, want, c.Out, "%s: stdout differs\n%s", c.verb(), c.dump())
	return c
}

// NotStdout requires stdout not to contain forbidden.
func (c *Chain) NotStdout(forbidden string) *Chain {
	c.t.Helper()
	require.NotContains(c.t, c.Out, forbidden, "%s: stdout holds %q\n%s", c.verb(), forbidden, c.dump())
	return c
}

// Stderr requires stderr to be exactly want.
func (c *Chain) Stderr(want string) *Chain {
	c.t.Helper()
	require.Equal(c.t, want, c.Err, "%s: stderr differs\n%s", c.verb(), c.dump())
	return c
}

// NotStderr requires stderr not to contain forbidden.
func (c *Chain) NotStderr(forbidden string) *Chain {
	c.t.Helper()
	require.NotContains(c.t, c.Err, forbidden, "%s: stderr holds %q\n%s", c.verb(), forbidden, c.dump())
	return c
}

// JSON requires stdout to decode as JSON into into.
func (c *Chain) JSON(into any) *Chain {
	c.t.Helper()
	require.NoError(c.t, json.Unmarshal([]byte(c.Out), into), "%s: stdout is not JSON into %T\n%s", c.verb(), into, c.dump())
	return c
}

// Lines requires stdout to be exactly these lines, a trailing newline
// ignored. No output matches no lines.
func (c *Chain) Lines(want ...string) *Chain {
	c.t.Helper()
	got := splitLines(c.Out)
	require.Equal(c.t, want, got, "%s: stdout lines differ\n%s", c.verb(), c.dump())
	return c
}

// Refusal is one row of a refusal table: the name a subtest runs under (the
// joined args when empty), the arguments, the wanted exit code, and a text
// stderr must contain.
type Refusal struct {
	Name   string
	Args   []string
	Code   int
	Stderr string
}

// Refusals runs every row against main as its own parallel subtest: the exit
// code is required and stderr must contain the row's text. Setup is require,
// so a broken row is named and no later row is hidden.
func Refusals(t *testing.T, main testkit.Main, rows []Refusal) {
	t.Helper()
	for _, row := range rows {
		row := row
		name := row.Name
		if name == "" {
			name = strings.Join(row.Args, " ")
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := Run(t, main, row.Args...)
			c.Code(row.Code)
			require.Contains(t, c.Err, row.Stderr, "%s: stderr does not hold %q\n%s", c.verb(), row.Stderr, c.dump())
		})
	}
}

// verb is the command as it was run, for a finding's first line.
func (c *Chain) verb() string {
	if len(c.args) == 0 {
		return "(no args)"
	}
	return strings.Join(c.args, " ")
}

// dump is what a failed link prints: the command, its exit code and both
// streams whole.
func (c *Chain) dump() string {
	return fmt.Sprintf("verb.Run %s\nexit: %d\nstdout:\n%s\nstderr:\n%s", c.verb(), c.Exit, c.Out, c.Err)
}

// splitLines splits captured output into lines, dropping one trailing newline.
func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
