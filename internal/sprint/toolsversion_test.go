package sprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNovaToolsVersionIsTheFilesTag(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", NovaToolsVersionFile))
	require.NoError(t, err)
	r, err := ParseNovaToolsVersion(string(data))
	require.NoError(t, err)
	assert.Equal(t, NovaToolsVersion, r.Tag, "%s names %s, the binary needs %s: a re-seed updates NovaToolsVersion with the file", NovaToolsVersionFile, r.Tag, NovaToolsVersion)
}

func TestNovaToolsVersionRoundTrips(t *testing.T) {
	t.Parallel()
	const sha = "52046bd9a0ebf726b40617a81e678d2855047a60"
	cases := []struct {
		name string
		tag  string
	}{
		{name: "a canary tag", tag: "v1.2.0-local.20261008.1"},
		{name: "a released version", tag: "v1.1.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseNovaToolsVersion(FormatNovaToolsVersion(c.tag, sha))
			require.NoError(t, err)
			assert.Equal(t, NovaToolsRecord{Tag: c.tag, Commit: sha}, got)
		})
	}
}

func TestNovaToolsRefusalIsPureOverTheVersionLine(t *testing.T) {
	t.Parallel()
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
		if c.refuse == "" {
			assert.Empty(t, got, c.name)
		} else {
			assert.True(t, strings.HasPrefix(got, c.refuse), "%s: got %q, want it to start %q", c.name, got, c.refuse)
		}
	}
}

func TestNovaToolsVersionFileRefusesWhatIsNoRecord(t *testing.T) {
	t.Parallel()
	const sha = "52046bd9a0ebf726b40617a81e678d2855047a60"
	for _, bad := range []string{
		"",
		"# a comment alone\n",
		"commit " + sha + "\n",
		"tag 1.1.0\n",
		"tag v1.1\n",
		"tag v1.1.0+build.1\n",
		"tag v1.1.0\ncommit 52046bd\n",
		"tag v1.1.0\nversion v1.1.0\n",
	} {
		r, err := ParseNovaToolsVersion(bad)
		assert.Error(t, err, "%q read as %+v", bad, r)
	}
}
