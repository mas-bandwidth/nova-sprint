package main

import (
	"context"
	"fmt"
	"regexp"

	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// This is the release-tag grammar enforced by nova-fleet's installer.
var publicRelease = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

type fleetPublicInstaller struct{ command verbs.CommandOutput }

func (f fleetPublicInstaller) Install(ctx context.Context, host, version string) error {
	if f.command == nil || !safeSSHName(host) || !publicRelease.MatchString(version) {
		return fmt.Errorf("public dashboard installer needs a valid host and release version")
	}
	_, err := f.command(ctx, "nova-fleet", "install", "--repo", "mas-bandwidth/nova-sprint", "--tag", version, "--names", "nova-sprint", host)
	return err
}
