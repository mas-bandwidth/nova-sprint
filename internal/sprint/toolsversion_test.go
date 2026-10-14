package sprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNovaToolsVersionIsTheFilesTag(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", NovaToolsVersionFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "tag ") {
			if strings.TrimSpace(line) != "tag "+NovaToolsVersion {
				t.Fatalf("%s names wrong tag, the binary needs %s", NovaToolsVersionFile, NovaToolsVersion)
			}
			return
		}
	}
	t.Fatalf("%s names no tag", NovaToolsVersionFile)
}

func TestNovaToolsRefusalIsPureOverTheVersionLine(t *testing.T) {
	line := func(v string) string { return "nova-bus " + v + " darwin/arm64 go1.26.6 build=0123456789ab\n" }
	for _, c := range []struct {
		name   string
		p      NovaToolsProbe
		refuse string // "" passes
	}{
		{"missing", NovaToolsProbe{Bin: "nova-bus"}, "nova-bus not found on PATH; nova-tools v1.1.0 required (NOVA-TOOLS-VERSION); install (once nova-tools v1.1.0 is published): go install github.com/mas-bandwidth/nova-tools/cmd/nova-bus@v1.1.0"},
		{"older", NovaToolsProbe{Bin: "nova-bus", Found: true, Output: line("v1.0.0")}, "nova-bus v1.0.0 found, older than nova-tools v1.1.0 required (NOVA-TOOLS-VERSION); install (once nova-tools v1.1.0 is published): go install github.com/mas-bandwidth/nova-tools/cmd/nova-bus@v1.1.0"},
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

