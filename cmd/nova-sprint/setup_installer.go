package main

import (
	"context"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// This is the release-tag grammar enforced by nova-fleet's installer.
var publicRelease = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// RepoEnv names the GitHub repository (org/repo) whose releases nova-fleet
// installs; unset, it is this binary's own module path's org/repo, so a fork
// under its own module path needs nothing and one that kept ours sets it.
const RepoEnv = "NOVA_SPRINT_REPO"

var orgRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// installRepo is the org/repo nova-fleet installs from: env when set, else
// the github.com module path this binary was built from (its /vN suffix
// dropped), else "" (Install then refuses, naming RepoEnv).
func installRepo(env string, info *debug.BuildInfo) string {
	if env != "" {
		return env
	}
	if info == nil {
		return ""
	}
	rest, ok := strings.CutPrefix(info.Main.Path, "github.com/")
	if !ok {
		return ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// builtRepo is installRepo for this process: RepoEnv, else the build info.
func builtRepo(getenv func(string) string) string {
	info, _ := debug.ReadBuildInfo()
	return installRepo(getenv(RepoEnv), info)
}

type fleetPublicInstaller struct {
	command verbs.CommandOutput
	repo    string // org/repo, installRepo
}

func (f fleetPublicInstaller) Install(ctx context.Context, host, version string) error {
	if f.command == nil {
		return fmt.Errorf("public dashboard installer needs a valid host and release version: no command is wired")
	}
	if !orgRepo.MatchString(f.repo) {
		return fmt.Errorf("public dashboard installer needs the repository to install from: %q is not <org/repo>; set %s=<org/repo> (the GitHub repository whose releases nova-fleet installs), then nova-sprint up again", edge.Redact(f.repo), RepoEnv)
	}
	if !safeSSHName(host) || !publicRelease.MatchString(version) {
		return fmt.Errorf("public dashboard installer needs a valid host and release version: host %q (valid %t), version %q (valid %t)", edge.Redact(host), safeSSHName(host), edge.Redact(version), publicRelease.MatchString(version))
	}
	_, err := f.command(ctx, "nova-fleet", "install", "--repo", f.repo, "--tag", version, "--names", "nova-sprint", host)
	return err
}
