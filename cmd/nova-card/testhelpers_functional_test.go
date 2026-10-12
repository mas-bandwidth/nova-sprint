//go:build functional

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/goenv"
)

// builtSprint is how this package's functional test runs nova-sprint verbs when
// it needs the real binary: it builds it once and runs every verb through a
// scrubbed environment with --redis <the test's own store>, so a verb can never
// reach a live sprint server however the machine is set up (forward.go). It is
// the counterpart of cmd/nova-sprint's in-process helper for a test that lives
// outside that package.
type builtSprint struct {
	t   *testing.T
	bin string
	env []string
}

// newBuiltSprint builds the real nova-sprint binary and makes the helper: an
// environment with no NOVA_* variable from the machine, and a home and config
// directory of the test's own, so the seat record is the test's and there is no
// named server.
func newBuiltSprint(t *testing.T) *builtSprint {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "nova-sprint")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "../nova-sprint")
	build.Env = goenv.Clean(os.Environ())
	out, err := build.CombinedOutput()
	require.NoError(t, err, "building nova-sprint: %s", out)
	home := t.TempDir()
	env := append(withoutNOVA(goenv.Clean(os.Environ())),
		"NOVA_SPRINT_REDIS=mem:"+filepath.Join(t.TempDir(), "twin"),
		"NOVA_SPRINT_ACTOR=coordinator",
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
	)
	return &builtSprint{t: t, bin: bin, env: env}
}

// run runs a verb on the built binary through the helper: --redis <the test's
// own store> is always passed, so the verb runs on the test's store and never
// forwards, and it returns the verb's exit code and combined output.
func (s *builtSprint) run(args ...string) (int, string) {
	s.t.Helper()
	store := "mem:" + filepath.Join(s.t.TempDir(), "twin")
	argv := append([]string{"--redis", store}, args...)
	cmd := exec.Command(s.bin, argv...)
	cmd.Env = s.env
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), b.String()
	}
	require.NoError(s.t, err, "nova-sprint %s", strings.Join(args, " "))
	return 0, b.String()
}

// withoutNOVA is the environment minus every NOVA_* variable: no variable from
// the machine (a server named by NOVA_SPRINT_SERVER, a seat credential, a
// NOVA_* provider key) reaches the built binary. The values are never read.
func withoutNOVA(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(name, "NOVA_") {
			continue
		}
		out = append(out, entry)
	}
	return out
}
