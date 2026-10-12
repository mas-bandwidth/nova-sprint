package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

func TestDoctorFailsWhenMachineEmailMatchesHostGlobal(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	config := filepath.Join(xdg, "git", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(config), 0o700))
	require.NoError(t, os.WriteFile(config, []byte("[user]\n\temail = machine@example.invalid\n"), 0o600))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	a := newApp(func(k string) string {
		switch k {
		case "NOVA_SPRINT_GIT_NAME":
			return "Machine"
		case "NOVA_SPRINT_GIT_EMAIL":
			return "machine@example.invalid"
		default:
			return os.Getenv(k)
		}
	})
	check := a.doctorMachineGitIdentity(context.Background())
	require.Equal(t, sprint.DoctorFail, check.Status)
	require.Contains(t, check.Evidence, "host global user.email equals")
}
