package sprint

import (
	"runtime/debug"
	"strings"
	"testing"
)

// NovaToolsRequiredFrom reads the dependency record it is given: the version of
// NovaToolsModule among the deps, none when there is no record. The real executable's
// record against go.mod is the functional tier's (TestTheBuiltExecutableRecordsGoModsNovaTools).
func TestNovaToolsRequiredFromReadsTheInjectedRecord(t *testing.T) {
	record := func(bi *debug.BuildInfo, ok bool) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) { return bi, ok }
	}
	const v = "v1.2.26-0.20261014184205-74cef8c16f41"
	got, ok := NovaToolsRequiredFrom(record(&debug.BuildInfo{Deps: []*debug.Module{{Path: "golang.org/x/mod", Version: "v0.41.0"}, {Path: NovaToolsModule, Version: v}}}, true))
	if !ok || got != v {
		t.Errorf("a record listing nova-tools: %q %v", got, ok)
	}
	if got, ok := NovaToolsRequiredFrom(record(nil, false)); ok || got != "" {
		t.Errorf("no build record: %q %v", got, ok)
	}
	if got, ok := NovaToolsRequiredFrom(record(&debug.BuildInfo{}, true)); ok || got != "" {
		t.Errorf("a record with no deps (a test binary's may be one): %q %v", got, ok)
	}
}

func TestNovaToolsInTakesTheReplacementAndRefusesAbsence(t *testing.T) {
	if v, ok := novaToolsIn(nil); ok || v != "" {
		t.Errorf("no deps: %q %v", v, ok)
	}
	deps := []*debug.Module{{Path: "x"}, {Path: NovaToolsModule, Version: "v1.2.3"}}
	if v, ok := novaToolsIn(deps); !ok || v != "v1.2.3" {
		t.Errorf("plain: %q %v", v, ok)
	}
	deps[1].Replace = &debug.Module{Path: NovaToolsModule, Version: "v1.2.4"}
	if v, ok := novaToolsIn(deps); !ok || v != "v1.2.4" {
		t.Errorf("replaced: %q %v", v, ok)
	}
	deps[1].Replace = &debug.Module{Path: "../nova-tools"}
	if v, ok := novaToolsIn(deps); ok {
		t.Errorf("a directory replacement names no version: %q", v)
	}
}

func TestNovaToolsRefusalIsPureOverTheVersionLine(t *testing.T) {
	line := func(v string) string { return "nova-bus " + v + " darwin/arm64 go1.26.6 build=0123456789ab\n" }
	for _, c := range []struct {
		name   string
		p      NovaToolsProbe
		refuse string // "" passes
	}{
		{"missing", NovaToolsProbe{Bin: "nova-bus"}, "nova-bus not found on PATH; nova-tools v1.1.0 required (go.mod); install: go install github.com/mas-bandwidth/nova-tools/cmd/nova-bus@v1.1.0"},
		{"older", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("v1.0.0")}, "nova-bus v1.0.0 found, older than nova-tools v1.1.0 required (go.mod); install: go install github.com/mas-bandwidth/nova-tools/cmd/nova-bus@v1.1.0"},
		{"a prerelease of the tag is older", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("v1.1.0-rc.1")}, "nova-bus v1.1.0-rc.1 found"},
		{"a pseudo-version before the tag is older", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("v1.0.1-0.20261005165330-ca8bb8cc36d5")}, "nova-bus v1.0.1-0.20261005165330-ca8bb8cc36d5 found"},
		{"a vcs stamp names no release", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("20261005T165330Z-ca8bb8cc36d5")}, "nova-bus 20261005T165330Z-ca8bb8cc36d5 found"},
		{"no version line", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: "usage: nova-bus <verb>\n", Err: "exit status 2"}, "nova-bus no version line (exit status 2) found"},
		{"equal", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("v1.1.0")}, ""},
		{"newer patch", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("v1.1.3")}, ""},
		{"newer major", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("v2.0.0")}, ""},
	} {
		got := NovaToolsRefusal(c.p, "v1.1.0")
		switch {
		case c.refuse == "" && got != "":
			t.Errorf("%s: refused %q", c.name, got)
		case c.refuse != "" && !strings.HasPrefix(got, c.refuse):
			t.Errorf("%s: got %q, want it to start %q", c.name, got, c.refuse)
		}
	}
}
