package verbs

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Health supplies fleet/tool and endpoint checks without exposing credentials.
type Health interface {
	Check(context.Context, bool) ([]OperationError, error)
	NovaToolsVersion(context.Context) (string, error)
}

func (e *Engine) check(ctx context.Context, args []string) Result {
	integrity := false
	if len(args) == 1 && args[0] == "--integrity" {
		integrity = true
	} else if len(args) != 0 {
		return refused("check", "invalid arguments", "nova-sprint help check")
	}
	if e.Health == nil {
		return unavailable("check")
	}
	issues, err := e.Health.Check(ctx, integrity)
	if err != nil {
		return operationalFailure("check", err)
	}
	if len(issues) > 0 {
		return operationalFailure("check", &issues[0])
	}
	line, err := e.Health.NovaToolsVersion(ctx)
	version, parsed := parseNovaVersion(line)
	if err != nil || !parsed {
		return versionUnknown(e.ToolPin)
	}
	ok, err := atLeast(version, e.ToolPin)
	if err != nil {
		return versionUnknown(e.ToolPin)
	}
	if !ok {
		return refused("check", fmt.Sprintf("nova-tools %s is below pin %s", version, e.ToolPin), "nova-fleet up")
	}

	return Result{"CHECK fleet and tools verified\nnext: nova-sprint next\n", 0}
}

// atLeast compares releases in vMAJOR.MINOR.PATCH form. Prereleases and
// malformed versions are rejected rather than accidentally treating them as a release.
func atLeast(have, pin string) (bool, error) {
	a, err := releaseVersion(have)
	if err != nil {
		return false, err
	}
	b, err := releaseVersion(pin)
	if err != nil {
		return false, err
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return true, nil
}
func releaseVersion(s string) ([3]uint64, error) {
	var out [3]uint64
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid version")
	}
	for i, p := range parts {
		if !decimalDigits(p) || (len(p) > 1 && p[0] == '0') {
			return out, fmt.Errorf("invalid version")
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return out, fmt.Errorf("invalid version")
		}
		out[i] = n
	}
	return out, nil
}

func parseNovaVersion(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "nova-version" {
		return "", false
	}
	if _, err := releaseVersion(fields[1]); err != nil {
		return "", false
	}
	return fields[1], true
}
func versionUnknown(pin string) Result {
	return refused("check", "nova-tools version unknown", "install nova-tools >= "+pin+" with nova-update")
}

// NovaToolsVersion reads the trusted release version without mutating the fleet.
func NovaToolsVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "nova-version", "version").Output()
	return string(out), err
}
