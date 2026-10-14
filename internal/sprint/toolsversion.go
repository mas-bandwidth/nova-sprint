package sprint

import (
	"fmt"
	"runtime/debug"

	"golang.org/x/mod/semver"

	"github.com/mas-bandwidth/nova-tools/pkg/buildinfo"
)

// The nova-tools nova-sprint runs on (docs/SPEC-SPRINT.md, "The nova-tools it runs
// on"; the owner, 2026-10-06 08:50: "ensure that anybody installing nova-sprint does so
// on top of the latest nova-tools"). nova-sprint depends on nova-tools: go.mod's
// requirement of NovaToolsModule is the one source of the version, and a binary reads it
// back from its own build record (NovaToolsRequired), so nothing else restates it. The
// check is pure: what each binary's --version printed in, one refusal line per binary
// out; the lookup is the caller's.

// NovaToolsModule is the module nova-sprint depends on and the nova-tools binaries
// install from.
const NovaToolsModule = "github.com/mas-bandwidth/nova-tools"

// NovaToolsBinaries are the nova-tools commands nova-sprint calls: the friends, the
// bus and the config store, in the order they are checked.
var NovaToolsBinaries = []string{"nova-friend", "nova-bus", "nova-config"}

// NovaToolsRequired is the nova-tools version this binary was built against: go.mod's
// requirement as the build recorded it. ok is false when the build records none (a
// binary built outside module mode), which the caller refuses.
func NovaToolsRequired() (version string, ok bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	return novaToolsIn(bi.Deps)
}

// novaToolsIn is NovaToolsModule's version among deps, the replacement's when replaced.
func novaToolsIn(deps []*debug.Module) (string, bool) {
	for _, d := range deps {
		if d.Path != NovaToolsModule {
			continue
		}
		if d.Replace != nil {
			d = d.Replace
		}
		return d.Version, d.Version != ""
	}
	return "", false
}

// NovaToolsInstall is the command that installs bin at the required version, never
// @latest.
func NovaToolsInstall(bin, version string) string {
	return "go install " + NovaToolsModule + "/cmd/" + bin + "@" + version
}

// NovaToolsProbe is one binary as the lookup found it: on PATH or not, and what its
// --version printed (Err when it ran and failed).
type NovaToolsProbe struct {
	Bin    string
	Found  bool
	Output string
	Err    string
}

// NovaToolsRefusal is the one line that refuses p against the required version, ""
// when p passes: found, a version line naming a release at or past required. A build
// that names no release (devel, a vcs stamp) is refused, since nothing says it is not
// older.
func NovaToolsRefusal(p NovaToolsProbe, required string) string {
	install := "install: " + NovaToolsInstall(p.Bin, required)
	if !p.Found {
		return fmt.Sprintf("%s not found on PATH; nova-tools %s required (go.mod); %s", p.Bin, required, install)
	}
	found := "no version line"
	if f, ok := buildinfo.Parse(p.Output); ok {
		found = f.Version
		if semver.IsValid(found) && semver.Compare(found, required) >= 0 {
			return ""
		}
	} else if p.Err != "" {
		found = "no version line (" + p.Err + ")"
	}
	return fmt.Sprintf("%s %s found, older than nova-tools %s required (go.mod); %s", p.Bin, found, required, install)
}

// NovaToolsRefusals is every refusal of probes against required, in probe order;
// none is the install passing.
func NovaToolsRefusals(probes []NovaToolsProbe, required string) []string {
	var out []string
	for _, p := range probes {
		if r := NovaToolsRefusal(p, required); r != "" {
			out = append(out, r)
		}
	}
	return out
}
