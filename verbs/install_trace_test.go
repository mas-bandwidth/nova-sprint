package verbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The refinement mapping records real callback and return boundaries, not
// inferred intermediate writes or renames. candidate is an actual byte check;
// active is obtained from Readlink. checkOK and version record callback results.
// HTTP and exec are fakes; process-crash, concurrent filesystem interference,
// and power-loss behavior are outside this trace's observation.
type installTraceState struct {
	action, active, version, problem    string
	candidate, checkOK, previousPresent bool
}
type installTraceWalk struct {
	t         *testing.T
	home, dir string
	checkOK   bool
	version   string
	states    []installTraceState
}
type installTraceTransport struct {
	archive     []byte
	badChecksum bool
}

func (f installTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if strings.HasSuffix(req.URL.Path, "/SHA256SUMS") {
		body = []byte(installSums(f.archive))
		if f.badChecksum {
			body = []byte(strings.Repeat("0", 64) + "  nova-sprint_v1.2.3_linux_amd64.tar.gz\n")
		}
	} else {
		body = f.archive
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: req}, nil
}

func (w *installTraceWalk) observe(action, problem string) {
	w.t.Helper()
	target := filepath.Join(w.dir, "v1.2.3", "nova-sprint")
	info, err := os.Lstat(target)
	if err != nil && !os.IsNotExist(err) {
		w.t.Fatal(err)
	}
	candidate := err == nil
	if candidate {
		if !info.Mode().IsRegular() {
			w.t.Fatal("candidate is not a regular file")
		}
		data, err := os.ReadFile(target)
		if err != nil {
			w.t.Fatal(err)
		}
		if !bytes.Equal(data, []byte("fixture binary")) {
			w.t.Fatal("candidate bytes differ from fixture")
		}
	}
	link := filepath.Join(w.home, ".local", "bin", "nova-sprint")
	actual, err := os.Readlink(link)
	active := "none"
	if err == nil {
		switch actual {
		case target:
			active = "candidate"
		case filepath.Join(w.home, "old", "nova-sprint"):
			active = "old"
		default:
			w.t.Fatalf("unmapped active target %q", actual)
		}
	} else if !os.IsNotExist(err) {
		w.t.Fatal(err)
	}
	old, oldErr := os.ReadFile(filepath.Join(w.home, "old", "nova-sprint"))
	if oldErr != nil && !os.IsNotExist(oldErr) {
		w.t.Fatal(oldErr)
	}
	previousPresent := oldErr == nil && bytes.Equal(old, []byte("previous binary"))
	w.states = append(w.states, installTraceState{previousPresent: previousPresent, action: action, active: active, candidate: candidate, checkOK: w.checkOK, version: w.version, problem: problem})
}

func installTracePrevious(t *testing.T, home string, previous bool) {
	t.Helper()
	if !previous {
		return
	}
	bin := filepath.Join(home, ".local", "bin")
	old := filepath.Join(home, "old", "nova-sprint")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("previous binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, filepath.Join(bin, "nova-sprint")); err != nil {
		t.Fatal(err)
	}
}

func installTraceRelease(w *installTraceWalk, scenario int, cancel context.CancelFunc) ReleaseInstaller {
	r := ReleaseInstaller{Home: w.home, BaseURL: "https://fixture.invalid", OS: "linux", Arch: "amd64", Client: &http.Client{Transport: installTraceTransport{archive: installFixture(w.t, "nova-sprint"), badChecksum: scenario == 0}}}
	r.Check = func(context.Context, string) error {
		w.checkOK = scenario != 1
		problem := "none"
		if !w.checkOK {
			problem = "check"
		}
		w.observe("CheckCall", problem)
		if !w.checkOK {
			return errors.New("fixture check failure")
		}
		return nil
	}
	r.Version = func(context.Context, string) (string, error) {
		w.version = "requested-version"
		actual := "v1.2.3\n"
		if scenario == 2 {
			w.version = "other-version"
			actual = "v9.0.0\n"
		}
		problem := "none"
		if w.version == "other-version" {
			problem = "version"
		}
		w.observe("VersionCall", problem)
		if scenario == 3 {
			cancel()
		}
		return actual, nil
	}
	return r
}

func installTraceAttempt(t *testing.T, scenario int, previous bool) []installTraceState {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "versions")
	w := &installTraceWalk{t: t, home: home, dir: dir, version: "unread"}
	installTracePrevious(t, home, previous)
	w.observe("Init", "none")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := installTraceRelease(w, scenario, cancel)
	_, err := r.Install(ctx, "v1.2.3", dir)
	problem := "none"
	action := "ReturnSuccess"
	if err != nil {
		action = "ReturnFailure"
		problem = "external"
		var op *OperationError
		if errors.As(err, &op) {
			if op.Problem == "staged binary check failed" {
				problem = "check"
			}
			if op.Problem == "staged binary version mismatch" {
				problem = "version"
			}
		}
	}
	w.observe(action, problem)
	if err != nil && w.states[len(w.states)-1].active != w.states[0].active {
		t.Fatal("failed install changed active link")
	}
	if err == nil && w.states[len(w.states)-1].active != "candidate" {
		t.Fatal("success without candidate link")
	}
	return w.states
}

func installTraceWrite(t *testing.T, root string, n, scenario int, previous bool, states []installTraceState) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprintf("walk-%03d", n))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---- MODULE InstallTraceLog ----\nEXTENDS Naturals\n\\* Fixed seed 179401, walk %d, scenario %d; actual callback and return observations.\nTraceHavePrevious == %s\nTraceLog == <<\n", n, scenario, strings.ToUpper(fmt.Sprint(previous)))
	for i, s := range states {
		comma := ","
		if i == len(states)-1 {
			comma = ""
		}
		fmt.Fprintf(&b, "  [i |-> %d, action |-> %q, active |-> %q, candidate |-> %s, checkOK |-> %s, version |-> %q, problem |-> %q, previousPresent |-> %s]%s\n", i+1, s.action, s.active, strings.ToUpper(fmt.Sprint(s.candidate)), strings.ToUpper(fmt.Sprint(s.checkOK)), s.version, s.problem, strings.ToUpper(fmt.Sprint(s.previousPresent)), comma)
	}
	b.WriteString(">>\n====\n")
	if err := os.WriteFile(filepath.Join(dir, "InstallTraceLog.tla"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallTraceWalk(t *testing.T) {
	root := os.Getenv("NOVA_INSTALL_TRACE_DIR")
	if root == "" {
		t.Skip("set NOVA_INSTALL_TRACE_DIR to emit trace observations")
	}
	rng := rand.New(rand.NewSource(179401))
	for i := 0; i < 64; i++ {
		scenario := rng.Intn(5)
		if i < 5 {
			scenario = i
		}
		previous := rng.Intn(2) == 1
		states := installTraceAttempt(t, scenario, previous)
		installTraceWrite(t, root, i, scenario, previous, states)
	}
}
