package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGateChildEnvDropsGODEBUGAndUnneededVariables(t *testing.T) {
	t.Parallel()
	env := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/test",
		"GODEBUG=gctrace=1",
		"NOVA_UNNEEDED=x",
		"FOO=bar",
	}
	got := gateChildEnv(env)
	assert.NotContains(t, got, "GODEBUG=gctrace=1")
	assert.NotContains(t, got, "NOVA_UNNEEDED=x")
	assert.NotContains(t, got, "FOO=bar")
	assert.Contains(t, got, "PATH=/usr/bin:/bin")
	assert.Contains(t, got, "HOME=/home/test")
}

func TestGateChildEnvKeepsGitIdentityAndGoCache(t *testing.T) {
	t.Parallel()
	env := []string{
		"GIT_AUTHOR_NAME=lander",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GOCACHE=/cache/go-build",
		"GOFLAGS=-mod=readonly",
	}
	got := gateChildEnv(env)
	assert.Contains(t, got, "GIT_AUTHOR_NAME=lander")
	assert.Contains(t, got, "GIT_CONFIG_GLOBAL=/dev/null")
	assert.Contains(t, got, "GOCACHE=/cache/go-build")
	assert.Contains(t, got, "GOFLAGS=-mod=readonly")
}

func TestGofmtJudgedOnStdoutOnly(t *testing.T) {
	t.Parallel()
	stderr := "gc 39 @0.739s 1%: 0.14+1.6+0.038 ms clock, 1.1+0.40/1.2/0+0.30 ms cpu, 3->3->2 MB, 4 MB goal, 0 MB stacks\n"
	out := gofmtOutput("", stderr, nil)
	assert.NoError(t, gateRunError([]string{"gofmt", "-l", "."}, out, nil))
}

func TestGofmtStdoutListStillRed(t *testing.T) {
	t.Parallel()
	stderr := "gc 39 @0.739s 1%: 0.14+1.6+0.038 ms clock, 1.1+0.40/1.2/0+0.30 ms cpu, 3->3->2 MB, 4 MB goal, 0 MB stacks\n"
	out := gofmtOutput("bad.go\n", stderr, nil)
	assert.Error(t, gateRunError([]string{"gofmt", "-l", "."}, out, nil))
}
