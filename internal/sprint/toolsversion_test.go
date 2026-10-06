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
	r, err := ParseNovaToolsVersion(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != NovaToolsVersion {
		t.Fatalf("%s names %s, the binary needs %s: a re-seed updates NovaToolsVersion with the file", NovaToolsVersionFile, r.Tag, NovaToolsVersion)
	}
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

func TestNovaToolsVersionFileRoundTripsAndRefusesWhatIsNoRecord(t *testing.T) {
	const sha = "52046bd9a0ebf726b40617a81e678d2855047a60"
	r, err := ParseNovaToolsVersion(FormatNovaToolsVersion("v1.1.0", sha))
	if err != nil || r != (NovaToolsRecord{Tag: "v1.1.0", Commit: sha}) {
		t.Fatalf("round trip: %+v %v", r, err)
	}
	for _, bad := range []string{
		"",
		"# a comment alone\n",
		"commit " + sha + "\n",
		"tag 1.1.0\n",
		"tag v1.1\n",
		"tag v1.1.0-rc.1\n",
		"tag v1.1.0\ncommit 52046bd\n",
		"tag v1.1.0\nversion v1.1.0\n",
	} {
		if r, err := ParseNovaToolsVersion(bad); err == nil {
			t.Errorf("%q read as %+v", bad, r)
		}
	}
}
