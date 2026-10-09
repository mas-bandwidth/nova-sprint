package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// fakeNovaTools is a PATH as a map: a binary present prints its version line, one
// absent is not found.
func fakeNovaTools(versions map[string]string) func(string) sprint.NovaToolsProbe {
	return func(bin string) sprint.NovaToolsProbe {
		v, ok := versions[bin]
		if !ok {
			return sprint.NovaToolsProbe{Bin: bin}
		}
		return sprint.NovaToolsProbe{Bin: bin, Found: true, Output: bin + " " + v + " linux/amd64 go1.26.6\n"}
	}
}

func allNovaTools(v string) map[string]string {
	m := map[string]string{}
	for _, b := range sprint.NovaToolsBinaries {
		m[b] = v
	}
	return m
}

func TestSeatCheckRefusesWithoutTheRequiredNovaTools(t *testing.T) {
	t.Parallel()
	req := sprint.NovaToolsVersion
	older := "v1.0.0"
	newer := "v1.3.0"
	require.Equal(t, -1, semver.Compare(older, req), "the fixture's older version is older than %s", req)

	seat := func(t *testing.T, versions map[string]string) (int, string, string) {
		ta := newTestApp(t)
		ta.ok("init --readers reader-a --members m1")
		ta.ok("start")
		ta.ok("tick")
		o := mockHealthyOutside()
		o.novaTools = fakeNovaTools(versions)
		ta.a.outside = o
		return ta.do("seat check")
	}

	t.Run("no nova-friend on PATH refuses naming the install command", func(t *testing.T) {
		t.Parallel()
		v := allNovaTools(req)
		delete(v, "nova-friend")
		code, out, errOut := seat(t, v)
		assert.Equal(t, 1, code)
		assert.Empty(t, out, "a refused seat check measures nothing")
		lines := strings.Split(strings.TrimSpace(errOut), "\n")
		require.Len(t, lines, 1, "one line, the one binary missing: %q", errOut)
		assert.Contains(t, lines[0], "nova-friend not found on PATH")
		assert.Contains(t, lines[0], req+" required")
		assert.Contains(t, lines[0], "go install github.com/mas-bandwidth/nova-tools/cmd/nova-friend@"+req)
	})

	t.Run("an older nova-bus refuses naming both versions", func(t *testing.T) {
		t.Parallel()
		v := allNovaTools(req)
		v["nova-bus"] = older
		code, _, errOut := seat(t, v)
		assert.Equal(t, 1, code)
		lines := strings.Split(strings.TrimSpace(errOut), "\n")
		require.Len(t, lines, 1, "%q", errOut)
		assert.Contains(t, lines[0], "nova-bus "+older+" found")
		assert.Contains(t, lines[0], "nova-tools "+req+" required")
		assert.Contains(t, lines[0], "go install github.com/mas-bandwidth/nova-tools/cmd/nova-bus@"+req)
	})

	t.Run("every binary missing is one line each", func(t *testing.T) {
		t.Parallel()
		code, _, errOut := seat(t, map[string]string{})
		assert.Equal(t, 1, code)
		lines := strings.Split(strings.TrimSpace(errOut), "\n")
		require.Len(t, lines, len(sprint.NovaToolsBinaries), "%q", errOut)
		for i, b := range sprint.NovaToolsBinaries {
			assert.Contains(t, lines[i], b+" not found on PATH")
		}
	})

	t.Run("a build naming no release refuses", func(t *testing.T) {
		t.Parallel()
		v := allNovaTools(req)
		v["nova-config"] = "devel"
		code, _, errOut := seat(t, v)
		assert.Equal(t, 1, code)
		assert.Contains(t, errOut, "nova-config devel found")
	})

	for _, v := range []string{req, newer} {
		t.Run("nova-tools "+v+" passes", func(t *testing.T) {
			t.Parallel()
			code, out, errOut := seat(t, allNovaTools(v))
			assert.Equal(t, 0, code, "out %q err %q", out, errOut)
			assert.NotContains(t, errOut, "REFUSED")
			assert.Contains(t, out, sprint.SeatCheckToken+" ")
		})
	}
}
