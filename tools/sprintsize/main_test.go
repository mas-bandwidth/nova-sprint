package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/testkit/fsx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBin is a nova-sprint that exits with code.
func fakeBin(t *testing.T, code string) string {
	t.Helper()
	dir := fsx.Tree(t, map[string]string{"nova-sprint": fsx.Exec("#!/bin/sh\nexit " + code + "\n")})
	return dir + "/nova-sprint"
}

func TestSprintExistsReadsWhere(t *testing.T) {
	t.Parallel()
	assert.True(t, sprintExists(fakeBin(t, "0"), nil), "where succeeded: a sprint exists")
	assert.False(t, sprintExists(fakeBin(t, "1"), nil), "where refused: no sprint")
}

func TestLocalStoreAcceptsOnlyThisMachine(t *testing.T) {
	t.Parallel()
	for addr, want := range map[string]bool{"127.0.0.1:6401": true, "localhost:6401": true, "[::1]:6401": true, "10.0.0.5:6401": false, "127.0.0.1": false} {
		assert.Equalf(t, want, localStore(addr), "localStore(%q)", addr)
	}
}

// runMain runs main() in a child of the test binary with args, and returns its
// exit code and stderr.
func runMain(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainChild$")
	cmd.Env = append(os.Environ(), "SPRINTSIZE_CHILD="+strings.Join(args, "\x1f"))
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		require.NoError(t, err)
	}
	return code, errb.String()
}

// TestMainChild is the child of runMain: it is main() with the args it is given.
func TestMainChild(t *testing.T) {
	args := os.Getenv("SPRINTSIZE_CHILD")
	if args == "" {
		t.Skip("the child of runMain")
	}
	os.Args = append([]string{"sprintsize"}, strings.Split(args, "\x1f")...)
	main()
}

func TestMainRefusesAnExistingSprintUnlessReplace(t *testing.T) {
	t.Parallel()
	bin := fakeBin(t, "0")
	code, errs := runMain(t, "--bin", bin, "--redis", "127.0.0.1:1")
	require.Equalf(t, 2, code, "an existing sprint: %s", errs)
	assert.Contains(t, errs, "a sprint already exists on 127.0.0.1:1")
	assert.Contains(t, errs, "--replace")
	code, errs = runMain(t, "--bin", bin, "--redis", "127.0.0.1:1", "--replace")
	assert.NotEqualf(t, 2, code, "--replace was refused: %s", errs)
	code, errs = runMain(t, "--bin", fakeBin(t, "1"), "--redis", "127.0.0.1:1")
	assert.NotEqualf(t, 2, code, "no sprint was refused: %s", errs)
}
