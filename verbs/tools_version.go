package verbs

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"os/exec"
	"regexp"
	"runtime/debug"
)

// toolsModule is nova-tools' module path at any major version.
var toolsModule = regexp.MustCompile(`^github\.com/mas-bandwidth/nova-tools(/v[0-9]+)?$`)

// InstalledTools is nova-tools' release as the installed binaries carry it:
// the module version Go stamps into nova-config (the build info a release
// build records from its tag), read from the binary on PATH without running
// it.
func InstalledTools(context.Context) (string, error) {
	path, err := exec.LookPath("nova-config")
	if err != nil {
		return "", cause("nova-config is not on PATH", err)
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return "", cause("nova-config carries no build info", err)
	}
	return toolsRelease(info)
}

// toolsRelease is a nova-tools binary's release from its build info: a
// vMAJOR.MINOR.PATCH module version; a development or pseudo-version build
// is unknown.
func toolsRelease(info *debug.BuildInfo) (string, error) {
	if info == nil || !toolsModule.MatchString(info.Main.Path) {
		return "", fmt.Errorf("not a nova-tools binary")
	}
	v, ok := parseToolsRelease(info.Main.Version)
	if !ok {
		return "", fmt.Errorf("nova-tools build %q is not a release", info.Main.Version)
	}
	return v, nil
}

// parseToolsRelease is a release vMAJOR.MINOR.PATCH, and nothing else.
func parseToolsRelease(s string) (string, bool) {
	if len(s) < 2 || s[0] != 'v' {
		return "", false
	}
	_, err := releaseVersion(s)
	return s, err == nil
}

// toolsFix is the one command that installs nova-tools at the pin.
func toolsFix(pin string) string { return "nova-fleet install --tag " + pin + " <machine>" }
