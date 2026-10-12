package main

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/pkg/gitidentity"
)

func (a *app) configuredMachineIdentity() (gitidentity.Identity, error) {
	getenv := a.getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if a.machineGitIdentity != nil {
		return *a.machineGitIdentity, nil
	}
	return gitidentity.Resolve(getenv)
}

func isMachineCommit(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "commit", "commit-tree":
		return true
	case "merge":
		return !slices.Contains(args, "--abort") && !slices.Contains(args, "--quit")
	default:
		return false
	}
}

func (a *app) machineCommitEnv(args []string, env []string) ([]string, error) {
	if !isMachineCommit(args) {
		return env, nil
	}
	id, err := a.configuredMachineIdentity()
	if err != nil {
		return nil, err
	}
	return gitidentity.OverrideEnv(env, id), nil
}
