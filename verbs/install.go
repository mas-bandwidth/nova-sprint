package verbs

import (
	"context"
	"fmt"
	"strings"
)

// Installer installs one named release and checks it before activation.
type Installer interface {
	Install(context.Context, string, string) (string, error)
}

func (e *Engine) install(ctx context.Context, args []string) Result {
	version, dir, ok := parseInstall(args)
	if !ok {
		return refused("install", "invalid arguments", "nova-sprint help install")
	}
	if e.Install == nil {
		return unavailable("install")
	}
	previous, err := e.Install.Install(ctx, version, dir)
	if err != nil {
		return operationalFailure("install", err)
	}
	if previous != "" {
		return Result{fmt.Sprintf("INSTALLED %s and checked\nrollback: %s\nnext: nova-sprint next\n", version, previous), 0}
	}
	return Result{fmt.Sprintf("INSTALLED %s and checked\nnext: nova-sprint next\n", version), 0}
}

func parseInstall(args []string) (string, string, bool) {
	var version, dir string
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) || args[i+1] == "" {
			return "", "", false
		}
		switch args[i] {
		case "--version":
			if version != "" {
				return "", "", false
			}
			version = args[i+1]
		case "--dir":
			if dir != "" {
				return "", "", false
			}
			dir = args[i+1]
		default:
			return "", "", false
		}
	}
	if !strings.HasPrefix(version, "v") {
		return "", "", false
	}
	for _, part := range strings.Split(strings.TrimPrefix(version, "v"), ".") {
		for _, c := range part {
			if c < '0' || c > '9' {
				return "", "", false
			}
		}
	}
	if _, err := releaseVersion(version); err != nil {
		return "", "", false
	}
	return version, dir, true
}
