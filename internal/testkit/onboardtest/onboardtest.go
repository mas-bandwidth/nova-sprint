// Package onboardtest runs the onboarding standard's checks for one command
// as one call, so a tool's test stops carrying its own copy. The standard is
// what a stranger meets first: the bare command refuses in one line naming its
// door, `help` exits 0 and its banner's first line names what the tool does,
// every verb answers -h at exit 0, and (opted in, because a tool's example may
// block or reach a store) the `example:` block's lines run and answer 0 or 1.
//
// A tool keeps one adapter of its own, the entry point as a testkit.Main, and
// calls Check once. The checks run as subtests, in parallel, and report through
// testify's assert so one broken check does not hide the rest:
//
//	onboardtest.Check(t, main, onboardtest.Spec{Name: "nova-x", Verbs: []string{"list", "show"}})
//
// Help runs one verb's help at exit 0 and returns the banner for a caller that
// reads it. The package imports testify and never reimplements it.
package onboardtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/onboarding"
	"github.com/mas-bandwidth/nova-sprint/pkg/testkit"
)

// Spec is the one command under the onboarding standard: its name as it is
// typed ("nova-x"), every verb it answers (group verbs as their words, "fleet
// sync"), and whether the banner's example: block is run. Examples is off by
// default: a first-run transcript may wait on a server or a store, which is a
// functional act, so the caller opts in to running it.
type Spec struct {
	Name     string
	Verbs    []string
	Examples bool
}

// Check runs every onboarding check for spec against main, each as a parallel
// subtest. Setup failures (a banner that cannot be read) are reported with
// testify's assert, not require, so every check is still reached.
func Check(t *testing.T, main testkit.Main, spec Spec) {
	t.Helper()
	for _, c := range spec.checks() {
		c := c
		t.Run(spec.Name+": "+c.name, func(t *testing.T) {
			t.Parallel()
			c.run(t, main)
		})
	}
}

// Help runs one verb's help (`help <verb>`), requires exit 0 and returns what
// it printed. It is what a caller that reads a banner uses in place of its own
// exit-code check.
func Help(t testing.TB, main testkit.Main, verb ...string) string {
	t.Helper()
	args := append([]string{"help"}, verb...)
	r := main.Run(args...)
	require.Equal(t, 0, r.Code, "%s: %s", strings.Join(args, " "), r.Stderr)
	return r.Stdout
}

// reporter is the slice of testing.TB the checks report through. *testing.T
// satisfies it, as does the module's own recording fake.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

type check struct {
	name string
	run  func(reporter, testkit.Main)
}

func (s Spec) checks() []check {
	cs := []check{
		{"bare command refuses in one line naming its door", s.checkBare},
		{"help exits 0 and its first line names the tool", s.checkHelp},
		{"every verb answers -h", s.checkVerbs},
		{"the example: block holds this tool's lines", s.checkExampleBlock},
	}
	if s.Examples {
		cs = append(cs, check{"the example: block's lines run and answer 0 or 1", s.checkExamplesRun})
	}
	return cs
}

func (s Spec) checkBare(t reporter, main testkit.Main) {
	t.Helper()
	r := main.Run()
	assert.NotEqual(t, 0, r.Code, "%s: the bare command exited 0; it must refuse", s.Name)
	line := onlyLine(r.Stderr, r.Stdout)
	assert.NotEmpty(t, line, "%s: the bare command printed no refusal", s.Name)
	assert.NotContains(t, line, "\n", "%s: the bare refusal is more than one line:\n%s", s.Name, line)
	assert.Contains(t, line, s.Name, "%s: the bare refusal does not name the tool", s.Name)
	assert.Contains(t, line, "help", "%s: the bare refusal does not name its door (%s help)", s.Name, s.Name)
}

func (s Spec) checkHelp(t reporter, main testkit.Main) {
	t.Helper()
	r := main.Run("help")
	assert.Equal(t, 0, r.Code, "%s help: exit %d, stderr %q", s.Name, r.Code, r.Stderr)
	first := firstLine(r.Stdout)
	assert.NotEmpty(t, first, "%s help: the banner has no first line", s.Name)
	assert.Contains(t, first, s.Name, "%s help: the banner's first line does not name the tool: %q", s.Name, first)
}

func (s Spec) checkVerbs(t reporter, main testkit.Main) {
	t.Helper()
	for _, v := range s.Verbs {
		args := append(strings.Fields(v), "-h")
		r := main.Run(args...)
		assert.Equal(t, 0, r.Code, "%s: exit %d, stderr %q", strings.Join(args, " "), r.Code, r.Stderr)
	}
}

func (s Spec) checkExampleBlock(t reporter, main testkit.Main) {
	t.Helper()
	r := main.Run("help")
	if r.Code != 0 {
		t.Errorf("%s help: exit %d, stderr %q", s.Name, r.Code, r.Stderr)
		return
	}
	if _, err := onboarding.ExampleLines(r.Stdout, s.Name); err != nil {
		t.Errorf("%s: %s", s.Name, err)
	}
}

func (s Spec) checkExamplesRun(t reporter, main testkit.Main) {
	t.Helper()
	r := main.Run("help")
	lines, err := onboarding.ExampleLines(r.Stdout, s.Name)
	if err != nil {
		t.Errorf("%s: %s", s.Name, err)
		return
	}
	for _, line := range lines {
		args := strings.Fields(line)[1:]
		got := main.Run(args...)
		if got.Code != 0 && got.Code != 1 {
			t.Errorf("%s: exit %d, want 0 or 1\n%s%s", line, got.Code, got.Stdout, got.Stderr)
		}
	}
}

// onlyLine returns the single non-empty line across the two streams, or
// everything when there is more than one.
func onlyLine(stderr, stdout string) string {
	var lines []string
	for _, s := range []string{stdout, stderr} {
		for _, l := range strings.Split(s, "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, strings.TrimSpace(l))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return strings.TrimSpace(l)
		}
	}
	return ""
}
