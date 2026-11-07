package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// toolCommand keeps database credentials out of fleet and bus subprocesses.
func toolCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = toolEnvironment(os.Environ(), name)
	return cmd.Output()
}
func toolEnvironment(source []string, name string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TZ": true, "TMPDIR": true, "XDG_STATE_HOME": true, "XDG_CONFIG_HOME": true, "SSL_CERT_DIR": true, "SSL_CERT_FILE": true, "SSH_AUTH_SOCK": true, "NOVA_MACHINE": true}
	switch filepath.Base(name) {
	case "nova-bus":
		for _, k := range []string{"NOVA_BUS_REDIS", "NOVA_BUS_USER", "NOVA_BUS_PASSWORD", "CODEX_THREAD_ID"} {
			allowed[k] = true
		}
	case "nova-config": // its own store: the DSN and the password, or the variable that names it
		for _, k := range []string{"NOVA_PG_DSN", "NOVA_PG_PASSWORD", "NOVA_PG_PASSWORD_ENV"} {
			allowed[k] = true
		}
	case "nova-fleet":
		for _, k := range []string{"NOVA_SECRETS_STORE", "NOVA_SECRETS_KEY", "NOVA_SECRETS_PINS", "NOVA_SECRETS_SEAT"} {
			allowed[k] = true
		}
	}
	out := []string{}
	for _, entry := range source {
		k, _, ok := strings.Cut(entry, "=")
		if ok && allowed[k] {
			out = append(out, entry)
		}
	}
	return out
}
