package bench

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// benchCall is one call a run made through a fakeTransport.
type benchCall struct {
	op, host, line, src, dst string
	withGit                  bool
}

// fakeTransport is a recording Transport: it remembers every call and answers
// each with the test's closures, so a run is driven end to end without ssh.
type fakeTransport struct {
	mu    sync.Mutex
	calls []benchCall
	shell func(ctx context.Context, host, line string, stdout, stderr io.Writer) (int, error)
	copy  func(ctx context.Context, host, src, dst string, withGit bool, stderr io.Writer) error
}

func (f *fakeTransport) record(c benchCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeTransport) Shell(ctx context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
	f.record(benchCall{op: "shell", host: host, line: line})
	if f.shell != nil {
		return f.shell(ctx, host, line, stdout, stderr)
	}
	return 0, nil
}

func (f *fakeTransport) Copy(ctx context.Context, host, src, dst string, withGit bool, stderr io.Writer) error {
	f.record(benchCall{op: "copy", host: host, src: src, dst: dst, withGit: withGit})
	if f.copy != nil {
		return f.copy(ctx, host, src, dst, withGit, stderr)
	}
	return nil
}

func (f *fakeTransport) snapshot() []benchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]benchCall(nil), f.calls...)
}

// testRunDir is the one directory a fake make step prints, under the default
// root, shaped like mktemp's output.
const testRunDir = DefaultRoot + "/run.abc123"

// answerRun is a fake shell for the make, exec and remove steps: make prints
// testRunDir, exec returns execCode, remove returns removeCode. A line that is
// none of those (a mirror stage) gets stageCode.
func answerRun(stageCode, execCode, removeCode int) func(context.Context, string, string, io.Writer, io.Writer) (int, error) {
	return func(ctx context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		switch {
		case line == MakeLine(DefaultRoot):
			io.WriteString(stdout, testRunDir+"\n")
			return 0, nil
		case strings.HasPrefix(line, "rm -rf "):
			if removeCode != 0 {
				io.WriteString(stderr, "boom\n")
			}
			return removeCode, nil
		case strings.Contains(line, "nice -n "):
			return execCode, nil
		default:
			return stageCode, nil
		}
	}
}

// runOptions is one valid run on one bench, its local tree a fresh temp dir.
func runOptions(t *testing.T, argv ...string) Options {
	t.Helper()
	if len(argv) == 0 {
		argv = []string{"go", "test", "./..."}
	}
	return Options{Hosts: []string{"bench1"}, Dir: t.TempDir(), Argv: argv}
}

func TestRunMakesCopiesRunsAndRemovesInOrder(t *testing.T) {
	t.Parallel()
	argv := []string{"go", "test", "./..."}
	ft := &fakeTransport{shell: answerRun(0, 0, 0)}
	o := runOptions(t, argv...)
	res, err := Run(context.Background(), ft, o)
	if err != nil {
		t.Fatalf("a run that answers: %v", err)
	}
	if res.Host != "bench1" || res.RunDir != testRunDir {
		t.Fatalf("run on %q in %q, want bench1 in %q", res.Host, res.RunDir, testRunDir)
	}
	if res.Code != 0 || !res.Removed || res.RemoveErr != nil {
		t.Fatalf("code=%d removed=%v removeErr=%v, want 0, true, nil", res.Code, res.Removed, res.RemoveErr)
	}
	if res.Stage.Via != "tar" || res.Stage.Err != nil {
		t.Fatalf("stage=%+v, want a tar stage with no error", res.Stage)
	}
	got := ft.snapshot()
	if len(got) != 4 {
		t.Fatalf("%d transport calls, want 4 (make, copy, exec, remove): %+v", len(got), got)
	}
	if got[0].op != "shell" || got[0].line != MakeLine(DefaultRoot) {
		t.Fatalf("call 0 = %+v, want the make line %q", got[0], MakeLine(DefaultRoot))
	}
	if got[1].op != "copy" || got[1].src != o.Dir || got[1].dst != testRunDir+"/repo" || got[1].withGit {
		t.Fatalf("call 1 = %+v, want the copy of %q into %q without .git", got[1], o.Dir, testRunDir+"/repo")
	}
	if got[2].op != "shell" || got[2].line != ExecLine(testRunDir, DefaultCache, argv) {
		t.Fatalf("call 2 = %+v, want the exec line %q", got[2], ExecLine(testRunDir, DefaultCache, argv))
	}
	if got[3].op != "shell" || got[3].line != RemoveLine(testRunDir) {
		t.Fatalf("call 3 = %+v, want the remove line %q", got[3], RemoveLine(testRunDir))
	}
}

func TestRunRemovesTheRunDirectoryWhenTheCommandFails(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: answerRun(0, 7, 0)}
	res, err := Run(context.Background(), ft, runOptions(t))
	if err != nil {
		t.Fatalf("a command that ran and failed is not a run error: %v", err)
	}
	if res.Code != 7 {
		t.Fatalf("code %d, want the command's exit 7", res.Code)
	}
	if !res.Removed || res.RemoveErr != nil {
		t.Fatalf("removed=%v removeErr=%v, want the directory removed", res.Removed, res.RemoveErr)
	}
	last := ft.snapshot()
	if last[len(last)-1].line != RemoveLine(testRunDir) {
		t.Fatalf("the last call %+v is not the remove of the run directory", last[len(last)-1])
	}
}

func TestRunRemovesTheRunDirectoryWhenStagingFails(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{
		shell: answerRun(0, 0, 0),
		copy: func(context.Context, string, string, string, bool, io.Writer) error {
			return errors.New("the bench stopped reading")
		},
	}
	res, err := Run(context.Background(), ft, runOptions(t))
	if err == nil || res.Stage.Err == nil {
		t.Fatalf("a stage that refused: err=%v stage=%+v", err, res.Stage)
	}
	if !res.Removed {
		t.Fatalf("the run directory was left behind after a failed stage")
	}
	for _, c := range ft.snapshot() {
		if c.op == "shell" && strings.Contains(c.line, "nice -n ") {
			t.Fatalf("the command ran although the stage refused: %q", c.line)
		}
	}
}

func TestRunRemovesTheRunDirectoryWhenTheContextIsCancelledMidRun(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		switch {
		case line == MakeLine(DefaultRoot):
			io.WriteString(stdout, testRunDir+"\n")
			return 0, nil
		case strings.Contains(line, "nice -n "):
			cancel()
			<-c.Done()
			return -1, c.Err()
		default:
			return 0, nil
		}
	}}
	res, err := Run(ctx, ft, runOptions(t))
	if err == nil {
		t.Fatalf("a cancelled run is an error")
	}
	if !res.Removed || res.RemoveErr != nil {
		t.Fatalf("removed=%v removeErr=%v, want the directory removed even though the run was cancelled", res.Removed, res.RemoveErr)
	}
	last := ft.snapshot()
	if last[len(last)-1].line != RemoveLine(testRunDir) {
		t.Fatalf("the last call %+v is not the remove of the run directory", last[len(last)-1])
	}
}

func TestRunRemovesTheRunDirectoryWhenTheExecTransportErrors(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		switch {
		case line == MakeLine(DefaultRoot):
			io.WriteString(stdout, testRunDir+"\n")
			return 0, nil
		case strings.Contains(line, "nice -n "):
			return -1, errors.New("ssh could not start")
		default:
			return 0, nil
		}
	}}
	res, err := Run(context.Background(), ft, runOptions(t))
	if err == nil || !strings.Contains(err.Error(), "starting the command") {
		t.Fatalf("a transport that could not start the command: %v", err)
	}
	if !res.Removed || res.RemoveErr != nil {
		t.Fatalf("removed=%v removeErr=%v, want the directory removed anyway", res.Removed, res.RemoveErr)
	}
}

func TestRunReportsARemoveFailureWithoutLosingTheExitCode(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: answerRun(0, 7, 1)}
	res, err := Run(context.Background(), ft, runOptions(t))
	if err != nil {
		t.Fatalf("a failed remove is reported in Result, not as a run error: %v", err)
	}
	if res.Code != 7 {
		t.Fatalf("code %d, want the command's exit 7 kept although remove failed", res.Code)
	}
	if res.Removed || res.RemoveErr == nil {
		t.Fatalf("removed=%v removeErr=%v, want the remove failure reported", res.Removed, res.RemoveErr)
	}
	if !strings.Contains(res.RemoveErr.Error(), "rm exit 1") {
		t.Fatalf("removeErr %q does not name the rm exit", res.RemoveErr)
	}
}

func TestRunFallsBackWhenAHostDoesNotAnswer(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		switch {
		case line == MakeLine(DefaultRoot) && host == "dead":
			return NoAnswer, nil
		case line == MakeLine(DefaultRoot):
			io.WriteString(stdout, testRunDir+"\n")
			return 0, nil
		case strings.HasPrefix(line, "rm -rf "):
			return 0, nil
		default:
			return 0, nil
		}
	}}
	var notes bytes.Buffer
	o := runOptions(t)
	o.Hosts = []string{"dead", "bench1"}
	o.Notes = &notes
	res, err := Run(context.Background(), ft, o)
	if err != nil {
		t.Fatalf("the fallback answered: %v", err)
	}
	if res.Host != "bench1" {
		t.Fatalf("the run was on %q, want the fallback bench1", res.Host)
	}
	if !strings.Contains(notes.String(), "CI BENCH PASSED host=dead") || !strings.Contains(notes.String(), "next=bench1") {
		t.Fatalf("the notes do not say the first host was passed over: %q", notes.String())
	}
}

func TestRunDoesNotFallBackWhenAHostAnswersAndRefuses(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		if line == MakeLine(DefaultRoot) {
			io.WriteString(stderr, "mkdir: permission denied\n")
			return 1, nil
		}
		return 0, nil
	}}
	o := runOptions(t)
	o.Hosts = []string{"refuser", "bench1"}
	_, err := Run(context.Background(), ft, o)
	if err == nil || !strings.Contains(err.Error(), "making the run directory") {
		t.Fatalf("a host that answered and refused is the end of the run: %v", err)
	}
	for _, c := range ft.snapshot() {
		if c.host == "bench1" {
			t.Fatalf("the run fell back to bench1 after a refusal: %+v", c)
		}
	}
}

func TestRunReturnsErrNoBenchWhenNoHostAnswers(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(context.Context, string, string, io.Writer, io.Writer) (int, error) {
		return NoAnswer, nil
	}}
	var notes bytes.Buffer
	o := runOptions(t)
	o.Hosts = []string{"dead1", "dead2"}
	o.Notes = &notes
	_, err := Run(context.Background(), ft, o)
	if !errors.Is(err, ErrNoBench) {
		t.Fatalf("err %v, want it to wrap ErrNoBench", err)
	}
	if !strings.Contains(notes.String(), "next=dead2") {
		t.Fatalf("the notes do not name the next host: %q", notes.String())
	}
}

func TestRunValidatesBeforeItReachesTheTransport(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{}
	if _, err := Run(context.Background(), ft, Options{}); err == nil {
		t.Fatalf("an empty Options passed validation")
	}
	if len(ft.snapshot()) != 0 {
		t.Fatalf("the transport was reached before validation: %+v", ft.snapshot())
	}
}

func TestValidateRefusesEachMissingOrMalformedField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		o    Options
		want string
	}{
		{"no host", Options{Dir: "/tmp/x", Argv: []string{"true"}}, "no bench named"},
		{"bad host", Options{Hosts: []string{"host;rm -rf /"}, Dir: "/tmp/x", Argv: []string{"true"}}, "is not a host name"},
		{"bad root", Options{Hosts: []string{"b"}, Root: "../escape", Dir: "/tmp/x", Argv: []string{"true"}}, "climbs with a .. element"},
		{"bad cache", Options{Hosts: []string{"b"}, Cache: "c he'q", Dir: "/tmp/x", Argv: []string{"true"}}, "a bench path is letters"},
		{"no tree", Options{Hosts: []string{"b"}, Argv: []string{"true"}}, "no local tree named"},
		{"no command", Options{Hosts: []string{"b"}, Dir: "/tmp/x"}, "no command given"},
		{"empty command", Options{Hosts: []string{"b"}, Dir: "/tmp/x", Argv: []string{""}}, "no command given"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.o.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateRefusesAStageInsteadOfTheLocalTree(t *testing.T) {
	t.Parallel()
	o := Options{Hosts: []string{"b"}, Stage: &MirrorStage{}, Argv: []string{"true"}}
	err := o.Validate()
	if err == nil || strings.Contains(err.Error(), "no local tree named") {
		t.Fatalf("a stage in place of Dir: %v", err)
	}
	if !strings.Contains(err.Error(), "mirror") {
		t.Fatalf("the stage's own refusal is not in %v", err)
	}
}

func TestValidateFillsTheDefaultsAndTrimsTheRoot(t *testing.T) {
	t.Parallel()
	o := Options{Hosts: []string{"b"}, Root: "nova-bench/runs/", Dir: "/tmp/x", Argv: []string{"true"}}
	if err := o.Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if o.Root != "nova-bench/runs" {
		t.Fatalf("root %q, want the trailing slash trimmed", o.Root)
	}
	if o.Cache != DefaultCache {
		t.Fatalf("cache %q, want the default %q", o.Cache, DefaultCache)
	}
	empty := Options{Hosts: []string{"b"}, Dir: "/tmp/x", Argv: []string{"true"}}
	if err := empty.Validate(); err != nil {
		t.Fatalf("empty root and cache: %v", err)
	}
	if empty.Root != DefaultRoot || empty.Cache != DefaultCache {
		t.Fatalf("root=%q cache=%q, want the defaults", empty.Root, empty.Cache)
	}
}

func TestCheckHostRefusesNamesSSHWouldReadAsMoreThanAHost(t *testing.T) {
	t.Parallel()
	for _, h := range []string{"", "-oProxyCommand=x", "h;rm", "h x", "h\nx", "h`x`", "$h"} {
		if err := CheckHost(h); err == nil {
			t.Fatalf("CheckHost(%q) accepted a host ssh would misread", h)
		}
	}
	for _, h := range []string{"hetzner2", "b1", "user@host", "host.example", "A1", "h-ost"} {
		if err := CheckHost(h); err != nil {
			t.Fatalf("CheckHost(%q) refused a plain host: %v", h, err)
		}
	}
}

func TestCheckPathRefusesAPathTheRemoteShellWouldReadDifferently(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"", ".", "/", "..", "../x", "a/../b", "~/x", "a b", "a'b", "a\nb", "a`b`", "$x"} {
		if err := CheckPath("root", p); err == nil {
			t.Fatalf("CheckPath(%q) accepted a path the remote shell would misread", p)
		}
	}
	for _, p := range []string{"nova-bench/runs", "/home/nova/x", "a/b-c_d/e", "x.git"} {
		if err := CheckPath("root", p); err != nil {
			t.Fatalf("CheckPath(%q) refused a plain path: %v", p, err)
		}
	}
}

func TestMakeRunDirAcceptsAFreshRunDirectoryUnderTheRoot(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		if line != MakeLine(DefaultRoot) {
			t.Fatalf("makeRunDir ran %q, want the make line", line)
		}
		io.WriteString(stdout, testRunDir+"\n")
		return 0, nil
	}}
	dir, answered, err := makeRunDir(context.Background(), ft, "bench1", DefaultRoot)
	if err != nil || !answered || dir != testRunDir {
		t.Fatalf("dir=%q answered=%v err=%v, want %q, true, nil", dir, answered, err, testRunDir)
	}
}

func TestMakeRunDirTreatsANoAnswerAsNotAnswered(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		io.WriteString(stderr, "ssh: connect to host bench1 port 22: Connection refused\n")
		return NoAnswer, nil
	}}
	dir, answered, err := makeRunDir(context.Background(), ft, "bench1", DefaultRoot)
	if answered || dir != "" || err == nil {
		t.Fatalf("dir=%q answered=%v err=%v, want not answered with the ssh refusal", dir, answered, err)
	}
	if !strings.Contains(err.Error(), "ssh exit 255") || !strings.Contains(err.Error(), "Connection refused") {
		t.Fatalf("the no-answer error does not name the ssh exit and its stderr: %v", err)
	}
}

func TestMakeRunDirReportsARefusedMakeStepAsAnswered(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		io.WriteString(stderr, "mkdir: cannot create directory: Permission denied\n")
		return 1, nil
	}}
	_, answered, err := makeRunDir(context.Background(), ft, "bench1", DefaultRoot)
	if !answered || err == nil {
		t.Fatalf("answered=%v err=%v, want an answering host that refused", answered, err)
	}
	if !strings.Contains(err.Error(), "making the run directory under") || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("the refusal does not name the step and its stderr: %v", err)
	}
}

func TestMakeRunDirRefusesOutputThatIsNotARunDirectory(t *testing.T) {
	t.Parallel()
	for _, out := range []string{"/tmp/evil", DefaultRoot + "/run.x/y", "not/a/run/dir"} {
		out := out
		t.Run(out, func(t *testing.T) {
			t.Parallel()
			ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
				io.WriteString(stdout, out+"\n")
				return 0, nil
			}}
			dir, answered, err := makeRunDir(context.Background(), ft, "bench1", DefaultRoot)
			if !answered || dir != "" || err == nil {
				t.Fatalf("mktemp output %q: dir=%q answered=%v err=%v", out, dir, answered, err)
			}
			if !strings.Contains(err.Error(), "not a run directory") {
				t.Fatalf("the refusal of %q does not say so: %v", out, err)
			}
		})
	}
}

func TestMakeRunDirReportsATransportThatCannotStart(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(context.Context, string, string, io.Writer, io.Writer) (int, error) {
		return -1, errors.New("exec: ssh: executable file not found")
	}}
	_, answered, err := makeRunDir(context.Background(), ft, "bench1", DefaultRoot)
	if answered || err == nil {
		t.Fatalf("answered=%v err=%v, want a transport that could not start", answered, err)
	}
}

func TestMakeLineMakesUnderARootTheShellReadsVerbatim(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "a b'c\nd")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := runLine(t, MakeLine(root))
	if code != 0 {
		t.Fatalf("MakeLine with a space, a quote and a newline: exit %d\n%s", code, out)
	}
	dir := strings.TrimSpace(out)
	if !strings.HasPrefix(dir, root+"/run.") {
		t.Fatalf("mktemp made %q, not a run directory under %q", dir, root)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("mktemp printed %q but it is not a directory: %v", dir, err)
	}
}

func TestRemoveLineRemovesExactlyTheQuotedPath(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	p := filepath.Join(base, "x y'z\nw")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(base, "keep")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := runLine(t, RemoveLine(p))
	if code != 0 {
		t.Fatalf("RemoveLine with a space, a quote and a newline: exit %d\n%s", code, out)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("the path with a space, a quote and a newline is not removed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("RemoveLine removed a sibling: %v", err)
	}
}

func TestExecLineRunsInTheQuotedDirectory(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dir := filepath.Join(base, "x y'z\nw")
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "marker"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runLine(t, ExecLine(dir, "/cache", []string{"cat", "marker"}))
	if code != 0 {
		t.Fatalf("ExecLine with a space, a quote and a newline: exit %d\n%s", code, out)
	}
	if out != "here" {
		t.Fatalf("the command did not run in the named repo: %q", out)
	}
}

func TestExecLineQuotesTheCache(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dir := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(dir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(base, "c he'q\nche")
	out, code := runLine(t, ExecLine(dir, cache, []string{"sh", "-c", `printf '%s' "$GOCACHE"`}))
	if code != 0 {
		t.Fatalf("ExecLine with a cache holding a space, a quote and a newline: exit %d\n%s", code, out)
	}
	if out != cache {
		t.Fatalf("GOCACHE was %q, want %q", out, cache)
	}
}

func TestExecLineAnchorsARelativeCacheAtHome(t *testing.T) {
	t.Parallel()
	line := ExecLine("dir", "nova-bench/cache/go-build", []string{"true"})
	if !strings.Contains(line, `GOCACHE="$HOME"/`+Quote("nova-bench/cache/go-build")) {
		t.Fatalf("a relative cache is not anchored at the login's home:\n%s", line)
	}
}

func TestQuoteRoundTripsThroughSh(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "plain", "a b", "a'b", "a\nb", "a'b\nc d", `"\$`} {
		s := s
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			out, code := runLine(t, "printf '%s' "+Quote(s))
			if code != 0 || out != s {
				t.Fatalf("Quote(%q) through sh: exit %d out %q", s, code, out)
			}
		})
	}
}

// testMirrorStage is one valid stage of a run from the bench's mirror.
func testMirrorStage() *MirrorStage {
	return &MirrorStage{
		Mirror: "nova-bench/mirror/x.git",
		Remote: "https://example.invalid/x.git",
		Ref:    GateRef("bench", strings.Repeat("a", 40)),
		Sha:    strings.Repeat("a", 40),
	}
}

func TestRunStagesFromTheBenchMirrorInPlaceOfTheCopy(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: answerRun(0, 0, 0)}
	o := Options{Hosts: []string{"bench1"}, Stage: testMirrorStage(), Argv: []string{"true"}}
	var staged []Stage
	o.Staged = func(s Stage) { staged = append(staged, s) }
	res, err := Run(context.Background(), ft, o)
	if err != nil {
		t.Fatalf("a mirror stage that answered: %v", err)
	}
	if res.Stage.Via != "mirror" || res.Stage.Err != nil {
		t.Fatalf("stage=%+v, want a mirror stage with no error", res.Stage)
	}
	if want := int64(len(StageLine(*o.Stage, testRunDir+"/repo"))); res.Stage.Bytes != want {
		t.Fatalf("stage bytes %d, want the line's %d", res.Stage.Bytes, want)
	}
	if len(staged) != 1 || staged[0].Via != "mirror" {
		t.Fatalf("the run told Staged %+v, want the mirror stage once", staged)
	}
	got := ft.snapshot()
	if len(got) < 2 || got[1].line != StageLine(*o.Stage, testRunDir+"/repo") {
		t.Fatalf("the stage call %+v is not the mirror's line", got)
	}
}

func TestRunRemovesTheRunDirectoryWhenTheMirrorStageRefuses(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: answerRun(NoMirror, 0, 0)}
	o := Options{Hosts: []string{"bench1"}, Stage: testMirrorStage(), Argv: []string{"true"}}
	res, err := Run(context.Background(), ft, o)
	if err == nil || res.Stage.Err == nil {
		t.Fatalf("a mirror stage that refused: err=%v stage=%+v", err, res.Stage)
	}
	if res.Stage.Err.Code != NoMirror {
		t.Fatalf("stage refusal code %d, want %d", res.Stage.Err.Code, NoMirror)
	}
	if line := res.Stage.Line(); !strings.Contains(line, "copy refused") {
		t.Fatalf("the stage line %q does not say the copy was refused", line)
	}
	if !res.Removed {
		t.Fatalf("the run directory was left behind after a refused mirror stage")
	}
}

func TestRunReportsARemoveTransportError(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{shell: func(c context.Context, host, line string, stdout, stderr io.Writer) (int, error) {
		switch {
		case line == MakeLine(DefaultRoot):
			io.WriteString(stdout, testRunDir+"\n")
			return 0, nil
		case strings.HasPrefix(line, "rm -rf "):
			return -1, errors.New("ssh could not start for the remove")
		default:
			return 0, nil
		}
	}}
	res, err := Run(context.Background(), ft, runOptions(t))
	if err != nil {
		t.Fatalf("a remove transport error is reported in Result, not as a run error: %v", err)
	}
	if res.Removed || res.RemoveErr == nil {
		t.Fatalf("removed=%v removeErr=%v, want the remove error reported", res.Removed, res.RemoveErr)
	}
}

// sizedFake is a Transport whose copy says how many bytes crossed the wire.
type sizedFake struct {
	fakeTransport
	n int64
}

func (s *sizedFake) CopySized(ctx context.Context, host, src, dst string, withGit bool, stderr io.Writer) (int64, error) {
	s.record(benchCall{op: "copysized", host: host, src: src, dst: dst, withGit: withGit})
	return s.n, nil
}

func TestRunReportsTheByteCountOfASizedCopy(t *testing.T) {
	t.Parallel()
	ft := &sizedFake{fakeTransport: fakeTransport{shell: answerRun(0, 0, 0)}, n: 4096}
	res, err := Run(context.Background(), ft, runOptions(t))
	if err != nil {
		t.Fatalf("a sized copy that answered: %v", err)
	}
	if res.Stage.Bytes != 4096 || res.Stage.Via != "tar" {
		t.Fatalf("stage=%+v, want 4096 bytes over tar", res.Stage)
	}
}

func TestLastLineTakesTheLastNonBlankLine(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"", ""},
		{"no newline", "no newline"},
		{"a\nb\n", "b"},
		{"a\r\nb\r\n", "b"},
		{"a\n\n", "a"},
		{"  \n  ", ""},
		{"a\nb\r\n", "b"},
	}
	for _, tc := range cases {
		if got := lastLine(tc.in); got != tc.want {
			t.Fatalf("lastLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
