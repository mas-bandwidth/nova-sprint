package onboardtest

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/pkg/testkit"
)

// standard is a fake command that meets the onboarding standard: a bare
// refusal in one line naming its door, a help banner whose first line names the
// tool with an example: block, and every verb's -h at exit 0.
func standard(name string, verbs []string) testkit.Main {
	return func(args []string, _ io.Reader, out, errb io.Writer) int {
		if len(args) == 0 {
			fmt.Fprintf(errb, "REFUSED: no verb; run: %s help\n", name)
			return 2
		}
		if args[0] == "help" {
			fmt.Fprintf(out, "%s: does the one thing\n\nusage:\n  %s %s\n\nflags:\n  -h\n\nexample:\n  %s %s\n",
				name, name, strings.Join(verbs, " "), name, firstVerb(verbs))
			return 0
		}
		if args[len(args)-1] == "-h" {
			fmt.Fprintf(out, "%s: help\n", strings.Join(args, " "))
			return 0
		}
		return 0
	}
}

func firstVerb(verbs []string) string {
	if len(verbs) == 0 {
		return ""
	}
	return verbs[0]
}

func wrap(m testkit.Main, f func(args []string, out, errb io.Writer) (int, bool)) testkit.Main {
	return func(args []string, in io.Reader, out, errb io.Writer) int {
		if code, done := f(args, out, errb); done {
			return code
		}
		return m(args, in, out, errb)
	}
}

// recordingTB is a testing.TB fake that records what a real subtest would have
// failed with, so a broken check is observed without failing the run.
type recordingTB struct {
	failed   bool
	messages []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failed = true
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func (r *recordingTB) joined() string { return strings.Join(r.messages, "\n") }

// TestOnboardtestReportsEachBrokenCheck drives the checks against a fake
// command that meets the standard and one variant per check that breaks it
// once; every breaking row must be reported through the recording fake, and
// the standard command must report nothing.
func TestOnboardtestReportsEachBrokenCheck(t *testing.T) {
	t.Parallel()
	spec := Spec{Name: "nova-x", Verbs: []string{"list", "show"}, Examples: true}
	ok := standard("nova-x", spec.Verbs)
	rows := []struct {
		name   string
		main   testkit.Main
		broken string
	}{
		{"standard", ok, ""},
		{"bare command answers 0", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) == 0 {
				fmt.Fprintln(out, "nothing to do")
				return 0, true
			}
			return 0, false
		}), "the bare command exited 0"},
		{"bare refusal is two lines", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) == 0 {
				fmt.Fprintf(errb, "no verb\nrun: nova-x help\n")
				return 2, true
			}
			return 0, false
		}), "more than one line"},
		{"bare refusal does not name its door", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) == 0 {
				fmt.Fprintln(errb, "REFUSED: no verb")
				return 2, true
			}
			return 0, false
		}), "does not name its door"},
		{"help exits non-zero", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) > 0 && args[0] == "help" {
				return 1, true
			}
			return 0, false
		}), "nova-x help: exit 1"},
		{"banner first line names nothing", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) > 0 && args[0] == "help" {
				fmt.Fprintf(out, "\n\nusage:\n  nova-x list\n")
				return 0, true
			}
			return 0, false
		}), "first line does not name the tool"},
		{"a verb -h exits non-zero", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) >= 2 && args[0] == "show" && args[len(args)-1] == "-h" {
				return 2, true
			}
			return 0, false
		}), "show -h: exit 2"},
		{"example block missing", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) > 0 && args[0] == "help" {
				fmt.Fprintf(out, "nova-x: does the one thing\n\nusage:\n  nova-x list\n")
				return 0, true
			}
			return 0, false
		}), "example:"},
		{"example line answers 2", wrap(ok, func(args []string, out, errb io.Writer) (int, bool) {
			if len(args) == 1 && args[0] == "list" {
				return 2, true
			}
			return 0, false
		}), "want 0 or 1"},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingTB{}
			for _, c := range spec.checks() {
				c.run(rec, row.main)
			}
			if row.broken == "" {
				assert.Falsef(t, rec.failed, "the standard command reported: %s", rec.joined())
				return
			}
			assert.Truef(t, rec.failed, "the broken command was not reported")
			assert.Containsf(t, rec.joined(), row.broken, "the finding is not the broken check's")
		})
	}
}

// Example prints the name of every check Check runs for a spec that runs the
// example block.
func Example() {
	spec := Spec{Name: "nova-x", Verbs: []string{"list"}, Examples: true}
	for _, c := range spec.checks() {
		fmt.Println(c.name)
	}
	// Output:
	// bare command refuses in one line naming its door
	// help exits 0 and its first line names the tool
	// every verb answers -h
	// the example: block holds this tool's lines
	// the example: block's lines run and answer 0 or 1
}
