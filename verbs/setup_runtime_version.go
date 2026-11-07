package verbs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

)

// VersionAdoption checks that the nova-sprint on PATH is the release up was
// asked for (--version), then its check and validated shadow. It installs
// nothing: nova-fleet installs nova-sprint, and Apply names that command.
type VersionAdoption struct {
	Binary      string
	Command     CommandOutput
	ResolvePath func() (string, error)
	Secrets     NovaSecrets
	Keys        []string
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
		return false, fmt.Errorf("installed version unavailable: %w", err)
	}
	installed, err := ParseVersionOutput(out)
	if err != nil {
		return false, fmt.Errorf("installed version unknown: %w", err)
	}
	if installed != version {
		return false, nil
	}
	if err = v.secretCommand(ctx, "check"); err != nil {
		return false, cause("updated sprint check failed", err)
	}
	if err = v.secretCommand(ctx, "run", "--shadow"); err != nil {
		return false, cause("updated sprint shadow failed", err)
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

func (v VersionAdoption) secretCommand(ctx context.Context, args ...string) error {
	if v.Secrets.Store == "" || v.Secrets.KeyFile == "" || v.Secrets.Seat == "" {
		return fmt.Errorf("version check credentials unavailable: the secret store, key file or seat is not configured")
	}
	if !slices.Contains(v.Keys, sprintPasswordName) {
		return fmt.Errorf("version check credentials unavailable: %s is not among the keys", sprintPasswordName)
	}
	keys := append([]string(nil), v.Keys...)
	slices.Sort(keys)
	for i, key := range keys {
		if !fleetKey.MatchString(key) || (i > 0 && keys[i-1] == key) {
			return fmt.Errorf("version check credentials unavailable: key name %q is invalid or repeated", key)
		}
	}
	cmd := []string{"exec", "--only", strings.Join(keys, ","), "--store", v.Secrets.Store, "--key", v.Secrets.KeyFile, "--as", v.Secrets.Seat, "--", v.Binary}
	cmd = append(cmd, args...)
	if v.Command != nil {
		_, err := v.Command(ctx, "nova-secrets", cmd...)
		return err
	}
	process := exec.CommandContext(ctx, "nova-secrets", cmd...)
	process.Env = safeProcessEnv(os.Environ())
	for _, name := range []string{"NOVA_PG_DSN", "NOVA_BUS_REDIS", "NOVA_BUS_USER", "NOVA_SPRINT_BACKUP_DIR"} {
		if value, ok := os.LookupEnv(name); ok {
			process.Env = append(process.Env, name+"="+value)
		}
	}
	return runTool(process, nil)
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
