// nova-card version: which build is running.
//
// A refusal, a green or a line pasted into a note is evidence about a BUILD, so the binary
// says which one it is. The version is not a constant kept by hand, which is wrong at the
// commit after a release: it is read from the build by pkg/buildinfo, which holds the
// resolution order and the shape of the line for every nova tool, so that each answers the
// question in one spelling.
package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mas-bandwidth/nova-sprint/pkg/buildinfo"
)

// version is empty in every ordinary build and is the one override: a release stamps it
// with -ldflags "-X main.version=<tag>". It is a var rather than a const because -X can
// only write a string var, and it is package-level and unexported for the same reason.
var version string

// versionJSON is the object version --json prints: the version line's four facts,
// named (docs/CLI.md, nova-card; docs/STANDARD.md section 3, --json is the same
// result as one object).
type versionJSON struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	GoVersion string `json:"go_version"`
}

// cmdVersion prints the one line, or, with --json, the same four facts as one
// object. It takes no other flags and no arguments: a second output SHAPE is one
// thing, agreed once by the buildinfo grammar, where a second line is a second
// thing to agree about and this verb exists to end an argument rather than to
// start one.
func cmdVersion(args []string, stdout, stderr io.Writer) int {
	jsonOut := false
	for _, a := range args {
		switch a {
		case "--json", "-json":
			jsonOut = true
		default:
			fmt.Fprintf(stderr, "nova-card version REFUSED: takes no flags but --json and no arguments, got %q; run: nova-card version -h\n", a)
			return 2
		}
	}
	line := buildinfo.Line("nova-card", version)
	if !jsonOut {
		fmt.Fprintln(stdout, line)
		return 0
	}
	f, ok := buildinfo.Parse(line)
	if !ok {
		fmt.Fprintf(stderr, "nova-card version REFUSED: the version line does not parse: %s; run: nova-card version -h\n", line)
		return 2
	}
	b, err := json.Marshal(versionJSON{Tool: f.Tool, Version: f.Version, Platform: f.Platform, GoVersion: f.GoVersion})
	if err != nil {
		fmt.Fprintf(stderr, "nova-card version REFUSED: %s; run: nova-card version -h\n", err)
		return 2
	}
	fmt.Fprintln(stdout, string(b))
	return 0
}
