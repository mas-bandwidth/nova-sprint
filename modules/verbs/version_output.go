package verbs

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// versionPrefix starts the version verb's one line: "nova-sprint <version>".
const versionPrefix = "nova-sprint "

// VersionLine is the version verb's line for version.
func VersionLine(version string) string { return versionPrefix + orWord(version, "(unknown build)") }

// ParseVersionOutput is the version a `nova-sprint version` printed: its
// first line is VersionLine, the rest the verb's "next:" line. The one
// parser for that output (the setup checks read installed binaries with
// it); anything else, an older binary's bare word included, is refused
// with what it saw.
func ParseVersionOutput(out []byte) (string, error) {
	first, rest, _ := strings.Cut(string(out), "\n")
	version, ok := strings.CutPrefix(strings.TrimRight(first, "\r"), versionPrefix)
	if !ok || version == "" || strings.ContainsAny(version, " \t") || !validBuildVersion(version) {
		return "", fmt.Errorf("not a nova-sprint version line: %s", edge.ParseRefused(`first line is not "nova-sprint <version>"`, 0, out))
	}
	for _, line := range strings.Split(strings.TrimSpace(rest), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "next: ") {
			return "", fmt.Errorf("not a nova-sprint version output: %s", edge.ParseRefused("a line after the version that is not next:", len(first)+1, out))
		}
	}
	return version, nil
}
