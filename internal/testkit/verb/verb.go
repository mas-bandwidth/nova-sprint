// Package verb runs a tool's entry point in process and checks the run as a
// fluent chain, and runs a refusal table one row per parallel subtest. It is a
// thin front door over pkg/testkit, which owns the capture (Main, Result, Run).
//
//	verb.Run(t, main, "inspect", "--json").Code(0).Stdout("ok").NotStderr("panic")
//	verb.Refusals(t, main, []verb.Refusal{{Name: "no door", Args: nil, Code: 2, Stderr: "REFUSED"}})
package verb

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/testkit"
	"github.com/stretchr/testify/require"
)

// Ran is one run of a Main, made by Run, with its checks as chain links.
type Ran struct {
	testkit.Result
	Args []string
	t    testing.TB
}

// Run calls the entry point with args and an empty stdin, capturing both streams.
func Run(t testing.TB, m testkit.Main, args ...string) Ran {
	t.Helper()
	return Ran{Result: m.Run(args...), Args: args, t: t}
}

// String is the run as a failure prints it: args, exit code and both streams.
func (r Ran) String() string {
	return fmt.Sprintf("run %q: exit=%d stdout=%q stderr=%q", r.Args, r.Result.Code, r.Result.Stdout, r.Result.Stderr)
}

// Code is require.Equal on the exit code.
func (r Ran) Code(want int) Ran {
	r.t.Helper()
	require.Equal(r.t, want, r.Result.Code, r)
	return r
}

// Stdout is require.Contains on stdout, for each of wants.
func (r Ran) Stdout(wants ...string) Ran {
	r.t.Helper()
	for _, w := range wants {
		require.Contains(r.t, r.Result.Stdout, w, r)
	}
	return r
}

// NotStdout is require.NotContains on stdout, for each of nots.
func (r Ran) NotStdout(nots ...string) Ran {
	r.t.Helper()
	for _, n := range nots {
		require.NotContains(r.t, r.Result.Stdout, n, r)
	}
	return r
}

// Stderr is require.Contains on stderr, for each of wants.
func (r Ran) Stderr(wants ...string) Ran {
	r.t.Helper()
	for _, w := range wants {
		require.Contains(r.t, r.Result.Stderr, w, r)
	}
	return r
}

// NotStderr is require.NotContains on stderr, for each of nots.
func (r Ran) NotStderr(nots ...string) Ran {
	r.t.Helper()
	for _, n := range nots {
		require.NotContains(r.t, r.Result.Stderr, n, r)
	}
	return r
}

// JSON decodes stdout into into and fails the test when it does not decode.
func (r Ran) JSON(into any) Ran {
	r.t.Helper()
	require.NoError(r.t, json.Unmarshal([]byte(r.Result.Stdout), into), r)
	return r
}

// Lines is require.Equal on stdout's newline-terminated lines.
func (r Ran) Lines(want ...string) Ran {
	r.t.Helper()
	var got []string
	if r.Result.Stdout != "" {
		got = strings.Split(strings.TrimSuffix(r.Result.Stdout, "\n"), "\n")
	}
	require.Equal(r.t, want, got, r)
	return r
}

// Refusal is one row of a tool's refusal table.
type Refusal struct {
	Name   string
	Args   []string
	Code   int
	Stderr string
}

// Refusals runs each row as its own t.Run with t.Parallel.
func Refusals(t *testing.T, m testkit.Main, rows []Refusal) {
	t.Helper()
	requireRows(t, rows)
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()
			Run(t, m, row.Args...).Code(row.Code).Stderr(row.Stderr)
		})
	}
}

func requireRows(t testing.TB, rows []Refusal) {
	t.Helper()
	require.NotEmpty(t, rows, "no refusal rows: a table of none checks nothing")
}
