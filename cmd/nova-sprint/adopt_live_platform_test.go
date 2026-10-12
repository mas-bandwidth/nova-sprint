package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// systemdShow is a systemctl show line of one unit.
func systemdShow(exec []string, pid int) string {
	return "LoadState=loaded\nActiveState=active\nMainPID=" + strconv.Itoa(pid) + "\nExecStart={ path=" + exec[0] + " ; argv[]=" + strings.Join(exec, " ") + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=" + strconv.Itoa(pid) + " ; code=(null) ; status=0/0 }\n"
}

// TestLiveLinuxReadsSystemdUnit: on linux live reads the nova-loop-*.service
// units; one whose pid runs a binary the install replaced is stale, and the
// same unit running the installed binary is not.
func TestLiveLinuxReadsSystemdUnit(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	units := filepath.Join(home, "units")
	root := filepath.Join(home, "proc")
	for _, d := range []string{bin, units, filepath.Join(root, "42")} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	installed := filepath.Join(bin, "nova-swarm")
	require.NoError(t, os.WriteFile(installed, []byte("x"), 0o755))
	other := filepath.Join(home, "old-swarm")
	require.NoError(t, os.WriteFile(other, []byte("y"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(units, "nova-loop-member-b1.service"), []byte("[Service]\nExecStart="+installed+" member\n"), 0o644))

	run := func(exe string) liveProbe {
		require.NoError(t, os.RemoveAll(filepath.Join(root, "42", "exe")))
		require.NoError(t, os.Symlink(exe, filepath.Join(root, "42", "exe")))
		fake := adoptRunner(func(_ context.Context, name string, args ...string) (string, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			switch {
			case strings.HasPrefix(line, "systemctl --user show nova-loop-member-b1.service"):
				return systemdShow([]string{installed, "member"}, 42), nil
			case line == bin+"/nova-sprint version":
				return "nova-sprint v1.2.10 linux/amd64", nil
			case strings.HasPrefix(line, "ps -A"):
				return "", nil
			}
			return "", errors.New("exit status 1")
		})
		return liveProbe{goos: "linux", userUnitsDir: units, systemUnitsDir: filepath.Join(home, "none"), procRoot: root, binDir: bin, uid: os.Getuid(), run: fake, now: func() time.Time { return time.Unix(0, 0) }}
	}

	m, err := run(other).read(context.Background())
	require.NoError(t, err)
	require.Len(t, m.Agents, 1)
	a := m.Agents[0]
	assert.Equal(t, "systemd", a.Manager)
	assert.Equal(t, "nova-loop-member-b1", a.Label)
	assert.Equal(t, "user/nova-loop-member-b1", a.Target)
	assert.Equal(t, []string{installed, "member"}, a.Program)
	assert.True(t, a.Loaded)
	assert.Equal(t, 42, a.PID)
	assert.True(t, a.Installed)
	assert.False(t, a.Fresh)
	assert.True(t, a.Stale)
	assert.Equal(t, "runs a binary the install replaced", a.Why)

	m, err = run(installed).read(context.Background())
	require.NoError(t, err)
	require.Len(t, m.Agents, 1)
	assert.True(t, m.Agents[0].Fresh, "the pid runs the installed bytes")
	assert.False(t, m.Agents[0].Stale)
}

// TestLiveLinuxWithNoUnitsIsAnEmptyHost: on linux a host with no unit
// directories and no ~/Library/LaunchAgents reads as an empty manifest, exit 0.
func TestLiveLinuxWithNoUnitsIsAnEmptyHost(t *testing.T) {
	home := t.TempDir()
	fake := adoptRunner(func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		switch {
		case line == filepath.Join(home, ".local", "bin")+"/nova-sprint version":
			return "nova-sprint v1.2.10 linux/amd64", nil
		case strings.HasPrefix(line, "ps -A"):
			return "", nil
		}
		return "", errors.New("exit status 1")
	})
	a := newApp(func(k string) string { return map[string]string{"HOME": home}[k] })
	a.goos = "linux"
	liveRunnerOf.Store(a, fake)
	t.Cleanup(func() { liveRunnerOf.Delete(a) })

	var out, errs bytes.Buffer
	require.Equal(t, 0, runInto(a, []string{"live", "--json"}, &out, &errs), errs.String())
	assert.Contains(t, out.String(), `"agents":[]`)
}

// TestLiveLinuxIgnoresMissingAgentsDir: on linux a named agents directory that
// is not there is ignored, not a refusal, so the play's old call to
// ~/Library/LaunchAgents stops refusing.
func TestLiveLinuxIgnoresMissingAgentsDir(t *testing.T) {
	home := t.TempDir()
	fake := adoptRunner(func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		switch {
		case line == filepath.Join(home, ".local", "bin")+"/nova-sprint version":
			return "nova-sprint v1.2.10 linux/amd64", nil
		case strings.HasPrefix(line, "ps -A"):
			return "", nil
		}
		return "", errors.New("exit status 1")
	})
	a := newApp(func(k string) string { return map[string]string{"HOME": home}[k] })
	a.goos = "linux"
	liveRunnerOf.Store(a, fake)
	t.Cleanup(func() { liveRunnerOf.Delete(a) })

	var out, errs bytes.Buffer
	missing := filepath.Join(home, "Library", "LaunchAgents")
	require.Equal(t, 0, runInto(a, []string{"live", "--json", "--agents-dir", missing}, &out, &errs), errs.String())
	assert.Contains(t, out.String(), `"agents":[]`)
}

// TestLiveDarwinReadsDaemonsDirectory: on darwin a daemon in the daemons
// directory is listed next to the (empty) agents directory with target
// system/<label>.
func TestLiveDarwinReadsDaemonsDirectory(t *testing.T) {
	home := t.TempDir()
	agents := filepath.Join(home, "agents")
	daemons := filepath.Join(home, "daemons")
	for _, d := range []string{agents, daemons} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(daemons, "com.nova.loop.member-x.plist"), []byte(livePlist("/x/nova-swarm", "member")), 0o644))
	fake := adoptRunner(func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		switch {
		case line == "launchctl print system/com.nova.loop.member-x":
			return "\tpid = 7\n", nil
		case strings.HasPrefix(line, "ps -A"):
			return "", nil
		case strings.HasPrefix(line, "ps -o args= -p 7"):
			return "/x/nova-swarm member", nil
		case strings.HasPrefix(line, "lsof"):
			return "", errors.New("exit status 1")
		case strings.HasSuffix(line, "/nova-sprint version"):
			return "nova-sprint v1.2.10 darwin/arm64", nil
		}
		return "", errors.New("exit status 1")
	})
	a := newApp(func(k string) string { return map[string]string{"HOME": home}[k] })
	a.goos = "darwin"
	liveRunnerOf.Store(a, fake)
	t.Cleanup(func() { liveRunnerOf.Delete(a) })

	var out, errs bytes.Buffer
	require.Equal(t, 0, runInto(a, []string{"live", "--json", "--agents-dir", agents, "--daemons-dir", daemons}, &out, &errs), errs.String())
	var m liveManifest
	require.NoError(t, json.Unmarshal(out.Bytes(), &m))
	require.Len(t, m.Agents, 1)
	assert.Equal(t, "system/com.nova.loop.member-x", m.Agents[0].Target)
	assert.Equal(t, "launchd", m.Agents[0].Manager)
}

// TestLiveDarwinStillRefusesMissingAgentsDir: on darwin a named agents
// directory that is not there is still no answer.
func TestLiveDarwinStillRefusesMissingAgentsDir(t *testing.T) {
	home := t.TempDir()
	fake := adoptRunner(func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		if strings.HasSuffix(line, "/nova-sprint version") {
			return "nova-sprint v1.2.10 darwin/arm64", nil
		}
		return "", errors.New("exit status 1")
	})
	a := newApp(func(k string) string { return map[string]string{"HOME": home}[k] })
	a.goos = "darwin"
	liveRunnerOf.Store(a, fake)
	t.Cleanup(func() { liveRunnerOf.Delete(a) })

	var out, errs bytes.Buffer
	missing := filepath.Join(home, "no-such-dir")
	assert.Equal(t, 1, runInto(a, []string{"live", "--agents-dir", missing}, &out, &errs))
	assert.Contains(t, errs.String(), "the agents directory "+missing+" is not a directory that reads")
}
