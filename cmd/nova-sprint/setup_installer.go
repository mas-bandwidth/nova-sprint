package main

import (
	"context"
	"fmt"
	"regexp"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// This is the release-tag grammar enforced by nova-fleet's installer.
var publicRelease = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// fleetPublicInstaller installs a release on a host through nova-fleet. repo
// is the release repository (<org>/<repo>); empty leaves nova-fleet to its own
// default, as every other nova-fleet install in this tool does.
type fleetPublicInstaller struct {
	command verbs.CommandOutput
	repo    string
}

func (f fleetPublicInstaller) Install(ctx context.Context, host, version string) error {
	if f.command == nil {
		return fmt.Errorf("public dashboard installer needs a valid host and release version: no command is wired")
	}
	if !safeSSHName(host) || !publicRelease.MatchString(version) {
		return fmt.Errorf("public dashboard installer needs a valid host and release version: host %q (valid %t), version %q (valid %t)", edge.Redact(host), safeSSHName(host), edge.Redact(version), publicRelease.MatchString(version))
	}
	args := []string{"install"}
	if f.repo != "" {
		args = append(args, "--repo", f.repo)
	}
	_, err := f.command(ctx, "nova-fleet", append(args, "--tag", version, "--names", "nova-sprint", host)...)
	return err
}
