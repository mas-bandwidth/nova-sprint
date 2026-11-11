package verbs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// VersionAdoption checks that the nova-sprint on PATH is the release up was
// asked for (--version) and is the build running up, so up's own steps are
// that build's check of the build: the fleet and nova-tools against its pin
// (step 1), the store at its schema (step 4), the bus (step 5); its workers
// step restarts a worker still beating an older release. The sprint's
// health (open judgements, landings, stalls) is check's, never adoption's.
// It installs nothing: nova-fleet installs nova-sprint, and Apply names
// that command.
type VersionAdoption struct {
	Binary      string
	Running     string // the build running up (versionString)
	Command     CommandOutput
	ResolvePath func() (string, error)
}

func (v VersionAdoption) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if v.Command != nil {
		return v.Command(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = safeProcessEnv(os.Environ())
	var out bytes.Buffer
	err := runTool(cmd, &out)
	return out.Bytes(), err
}
func (v VersionAdoption) Ready(ctx context.Context, version string) (bool, error) {
	if !validRelease(version) || !filepath.IsAbs(v.Binary) {
		return false, fmt.Errorf("version adoption configuration invalid")
	}
	resolve := v.ResolvePath
	if resolve == nil {
		resolve = resolvedSprintPath
	}
	binary, err := resolve()
	if err != nil {
		return false, cause("installed version unavailable", err)
	}
	if !filepath.IsAbs(binary) {
		return false, fmt.Errorf("installed version unavailable: %q is not an absolute path", binary)
	}
	v.Binary = binary
	out, err := v.command(ctx, binary, "version")
	if err != nil {
		return false, cause("installed version unavailable", err)
	}
	installed, err := ParseVersionOutput(out)
	if err != nil {
		return false, cause("installed version unknown", err)
	}
	if installed != version {
		return false, nil
	}
	if v.Running != version {
		return false, &OperationError{Problem: "up is running nova-sprint " + orWord(v.Running, "(unknown)") + ", not " + version + ", the release on PATH, so its steps would check another build", Fix: binary + " up --as <coordinator> --version " + version, Exit: 1}
	}
	return true, nil
}

func resolvedSprintPath() (string, error) {
	path, err := exec.LookPath("nova-sprint")
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func validRelease(v string) bool {
	if !strings.HasPrefix(v, "v") {
		return false
	}
	_, err := releaseVersion(v)
	return err == nil
}

// Apply refuses: the release asked for is not the one on PATH, and only
// nova-fleet installs it.
func (v VersionAdoption) Apply(_ context.Context, version string) error {
	if !validRelease(version) {
		return fmt.Errorf("version adoption configuration invalid")
	}
	return &OperationError{Problem: "nova-sprint " + version + " is not the one installed here", Fix: "nova-fleet install --repo <org/repo> --tag " + version + " --names nova-sprint <machine>, then nova-sprint up again", Exit: 1}
}
