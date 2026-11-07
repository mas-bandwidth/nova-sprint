package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/verbs"
)

type credentialEnvironment struct{ inner verbs.ExecEnvironment }

func (e credentialEnvironment) Fleet(ctx context.Context) (bool, string, error) {
	return e.inner.Fleet(ctx)
}
func (e credentialEnvironment) Hooked(ctx context.Context, actor string) (bool, string, error) {
	return e.inner.Hooked(ctx, actor)
}
func (e credentialEnvironment) Coordinator(ctx context.Context) (string, error) {
	return e.inner.Coordinator(ctx)
}
func (e credentialEnvironment) Schema(ctx context.Context) (bool, error) {
	if e.inner.DSN == "" {
		return false, nil
	}
	if e.inner.Password == "" {
		return false, fmt.Errorf("NOVA_SPRINT_PG_PASSWORD is required")
	}
	dsn, err := setupRoleDSN(e.inner.DSN, "nova_sprint")
	if err != nil {
		return false, fmt.Errorf("configure a passwordless NOVA_PG_DSN")
	}
	e.inner.DSN = dsn
	return e.inner.Schema(ctx)
}

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
