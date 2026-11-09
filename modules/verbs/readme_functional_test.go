package verbs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The public guides: the root welcome and the shared detailed guide.
var publicGuides = []string{"../../README.md", "../../docs/README.md"}

var (
	exitRow     = regexp.MustCompile(`^\| ([0-9]+) \| ([^|]+) \|`)
	otherVerb   = regexp.MustCompile("^\\| `([a-z][a-z ]*)` \\|")
	assetRun    = regexp.MustCompile(`^"\./\$asset" (.+)$`)
	indexVerb   = regexp.MustCompile(`^  ([a-z]+(?: [a-z]+)?)  +\S`)
	refusalForm = regexp.MustCompile(`^SPRINT REFUSED ([a-z-]+(?: [a-z-]+)?): (.+): (.+)$`)
)

// coldRun is one run of the built binary: its exit code and both streams.
type coldRun struct {
	code        int
	out, errOut string
}

// TestFunctionalREADMECommandsRunCold holds the guides to the built binary,
// run cold as the guide's "Inspect a release" step runs it: no store, bus,
// login or credential, an empty PATH and a HOME of its own. The guide's
// exit-code table is the oracle for every exit code. version and help, and
// help <verb> for every verb a guide names (in a command, or the "Other
// verbs" table), complete with a next line; every other verb a guide shows
// refuses in the documented form, as does a run off the bus and a bad usage.
func TestFunctionalREADMECommandsRunCold(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const version = "v0.0.0-readme"
	bin := filepath.Join(t.TempDir(), "nova-sprint")
	if out, err := exec.CommandContext(ctx, "go", "build", "-ldflags", "-X main.binaryVersion="+version, "-o", bin, "../../cmd/nova-sprint").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	home := t.TempDir()
	run := func(args ...string) coldRun {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = []string{"HOME=" + home, "PATH=/nonexistent"}
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Fatalf("nova-sprint %s: %v", strings.Join(args, " "), err)
		}
		return coldRun{cmd.ProcessState.ExitCode(), out.String(), errOut.String()}
	}

	guide, cmds, named := readGuides(t)
	completed, refused, failed := exitCodes(t, guide)

	// completes: exit completed, the last stdout line a next, nothing on stderr.
	completes := func(r coldRun, args ...string) {
		t.Helper()
		lines := strings.Split(strings.TrimRight(r.out, "\n"), "\n")
		if r.code != completed || r.errOut != "" || !strings.HasPrefix(lines[len(lines)-1], "next: ") {
			t.Errorf("nova-sprint %s: exit %d (the guide's table: %d for a completed command), stderr %q, last line %q", strings.Join(args, " "), r.code, completed, r.errOut, lines[len(lines)-1])
		}
	}
	// refuses: exit code, and one line in the documented form (SPRINT
	// REFUSED <verb>: <problem>: <fix>) whose fix names fix, alone on one
	// stream. The guide names no stream: the bus gate writes its refusals to
	// stderr, the verbs theirs to stdout.
	refuses := func(r coldRun, code int, fix string, args ...string) {
		t.Helper()
		line := r.errOut
		if line == "" {
			line = r.out
		}
		m := refusalForm.FindStringSubmatch(strings.TrimSuffix(line, "\n"))
		verb := "nova-sprint"
		if len(args) > 1 && find(args[0]+" "+args[1]) != nil {
			verb = args[0] + " " + args[1]
		} else if len(args) > 0 && find(args[0]) != nil || len(args) > 0 && len(sub(args[0])) > 0 {
			verb = args[0]
		}
		if r.code != code || r.out != "" && r.errOut != "" || m == nil || m[1] != verb || !strings.Contains(m[3], fix) {
			t.Errorf("nova-sprint %s: exit %d (want %d), stdout %q, stderr %q; want one line SPRINT REFUSED %s: <problem>: <fix naming %q>", strings.Join(args, " "), r.code, code, r.out, r.errOut, verb, fix)
		}
	}

	v := run("version")
	completes(v, "version")
	if first := strings.SplitN(v.out, "\n", 2)[0]; first != "nova-sprint "+version {
		t.Errorf("nova-sprint version: first line %q, want %q (version identifies the release)", first, "nova-sprint "+version)
	}

	index := run("help")
	completes(index, "help")
	listed, cold := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(index.out, "\n") {
		if m := indexVerb.FindStringSubmatch(line); m != nil {
			listed[m[1]] = true
			cold[m[1]] = strings.Contains(line, "[cold")
		}
	}

	// help <verb> for every verb named: listed by help, its table complete,
	// its synopsis first.
	for _, name := range named {
		if !listed[name] {
			t.Errorf("the guide names the verb %q; nova-sprint help does not list it", name)
		}
		args := append([]string{"help"}, strings.Fields(name)...)
		r := run(args...)
		completes(r, args...)
		if !strings.HasPrefix(r.out, "nova-sprint "+name) {
			t.Errorf("nova-sprint help %s: first line %q, want its synopsis (nova-sprint %s ...)", name, strings.SplitN(r.out, "\n", 2)[0], name)
		}
	}

	// The commands the guides show: help and version exactly as shown (they
	// run cold); every other verb, but the ones that act without a sprint
	// hook, refuses without an acting name.
	tried := map[string]bool{}
	for _, words := range cmds {
		switch verb := verbOf(words); {
		case verb == "help" || verb == "version":
			if !slices.ContainsFunc(words, func(w string) bool {
				return strings.ContainsAny(w, "<$") || words[0] == "help" && strings.HasPrefix(w, "-")
			}) {
				completes(run(words...), words...)
			}
		case !cold[verb] && !tried[verb]:
			tried[verb] = true
			refuses(run(strings.Fields(verb)...), refused, "--as <you>", strings.Fields(verb)...)
		}
	}

	if strings.Contains(guide, "Bare `nova-sprint` means `next`") {
		if bare, next := run(), run("next"); bare != next {
			t.Errorf("bare nova-sprint is not next: %+v, next %+v", bare, next)
		}
	}
	// Not on the bus: the bus is an operation that failed; the fix starts the
	// bus hook. next reads the bus itself and answers exit 0 with a next line.
	refuses(run("take", "--as", "cold-reader"), failed, "nova-bus hook --as cold-reader", "take")
	completes(run("next", "--as", "cold-reader"), "next", "--as", "cold-reader")
	// Bad usage: the fix points at help.
	refuses(run("frob"), failed, "nova-sprint help", "frob")
	refuses(run("version", "--frob"), failed, "nova-sprint help version", "version", "--frob")
}

// readGuides is the shared guide's text, the commands both guides show (the
// words after nova-sprint, or after the release asset the inspect step
// runs), and every verb they name, without repeats.
func readGuides(t *testing.T) (string, [][]string, []string) {
	t.Helper()
	var guide string
	var cmds [][]string
	var named []string
	name := func(n string) {
		if !slices.Contains(named, n) {
			named = append(named, n)
		}
	}
	for _, path := range publicGuides {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		if strings.HasSuffix(path, "docs/README.md") {
			guide = text
		}
		shown := taughtCommands(text)
		for _, line := range strings.Split(text, "\n") {
			if m := assetRun.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				shown = append(shown, m[1])
			}
			if m := otherVerb.FindStringSubmatch(line); m != nil {
				name(m[1])
			}
		}
		for _, cmd := range shown {
			words := strings.Fields(cmd)
			first := strings.Trim(words[0], "[]().,:")
			if first == "" || strings.ContainsAny(first[:1], "<.-") || first != words[0] && !isVerbWord(first) {
				continue // a placeholder (<verb>) or prose about the binary, as checkTaught reads it
			}
			cmds = append(cmds, words)
			switch {
			case words[0] == "help" && len(words) > 1 && words[1] != "playbook" && !strings.ContainsAny(words[1][:1], "<$-"):
				name(verbOf(words[1:]))
			case words[0] != "help":
				name(verbOf(words))
			}
		}
	}
	if guide == "" || len(cmds) == 0 {
		t.Fatal("no command found in the guides; the extractor is broken")
	}
	return guide, cmds, named
}

// verbOf is the verb words name: two words for a subcommand (stream add).
func verbOf(words []string) string {
	if len(words) > 1 && find(words[0]+" "+words[1]) != nil {
		return words[0] + " " + words[1]
	}
	return words[0]
}

// exitCodes reads the guide's exit-code table: the codes of a completed
// command, a refusal, and a bad usage or failed operation.
func exitCodes(t *testing.T, guide string) (completed, refused, failed int) {
	t.Helper()
	meaning := map[string]int{}
	for _, line := range strings.Split(guide, "\n") {
		if m := exitRow.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatal(err)
			}
			meaning[strings.TrimSpace(m[2])] = n
		}
	}
	code := func(phrase string) int {
		for text, n := range meaning {
			if strings.Contains(text, phrase) {
				return n
			}
		}
		t.Fatalf("the guide's exit-code table has no row for %q: %v", phrase, meaning)
		return -1
	}
	return code("completed"), code("refused"), code("Bad usage")
}
