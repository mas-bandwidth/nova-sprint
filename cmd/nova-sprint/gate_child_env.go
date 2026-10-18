package main

import "strings"

func gateChildEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		if name == "GODEBUG" {
			continue
		}
		switch name {
		case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TZ", "LANG",
			"SSH_AUTH_SOCK", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "GH_CONFIG_DIR",
			"NOVA_TEST_NO_HOST", "CGO_ENABLED", "CC", "CXX", "KEEP":
			out = append(out, e)
		default:
			if strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "GIT_") || strings.HasPrefix(name, "GO") {
				out = append(out, e)
			}
		}
	}
	return out
}

func gofmtOutput(stdout, stderr string, err error) string {
	if err != nil {
		return stdout + stderr
	}
	return stdout
}
