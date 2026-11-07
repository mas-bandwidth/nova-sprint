package verbs

import (
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
	return cmd.Output()
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
	if err != nil || !filepath.IsAbs(binary) {
		return false, fmt.Errorf("installed version unavailable")
	}
	v.Binary = binary
	out, err := v.command(ctx, binary, "version")
	if err != nil {
		return false, fmt.Errorf("installed version unavailable")
	}
	fields := strings.Fields(string(out))
	if len(fields) != 1 || !validInstalledBuild(fields[0]) {
		return false, fmt.Errorf("installed version unknown")
	}
	if fields[0] != version {
		return false, nil
	}
	if err = v.secretCommand(ctx, "check"); err != nil {
		return false, fmt.Errorf("updated sprint check failed")
	}
	if err = v.secretCommand(ctx, "run", "--shadow"); err != nil {
		return false, fmt.Errorf("updated sprint shadow failed")
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
	if v.Secrets.Store == "" || v.Secrets.KeyFile == "" || v.Secrets.Seat == "" || !slices.Contains(v.Keys, sprintPasswordName) {
		return fmt.Errorf("version check credentials unavailable")
	}
	keys := append([]string(nil), v.Keys...)
	slices.Sort(keys)
	for i, key := range keys {
		if !fleetKey.MatchString(key) || (i > 0 && keys[i-1] == key) {
			return fmt.Errorf("version check credentials unavailable")
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
	return process.Run()
}

func validInstalledBuild(v string) bool { return validBuildVersion(v) }
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
