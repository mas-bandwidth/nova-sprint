package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/testbin"
	"github.com/stretchr/testify/require"
)

func TestSeatForgeEnvironmentPrecedenceAndIsolation(t *testing.T) {
	for _, tc := range []struct{ name, process, scoped, seat, want string }{
		{"process overrides scoped and seat", "/process", "/scoped", "/seat", "/process"},
		{"scoped overrides seat", "", "/scoped", "/seat", "/scoped"},
		{"seat default", "", "", "/seat", "/seat"},
		{"normal gh default", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(func(key string) string {
				if key == "GH_CONFIG_DIR" {
					return tc.process
				}
				return ""
			})
			a.seatEnvironment = &seatEnvironment{Version: 1, ForgeIdentity: tc.seat}
			env := []string{"PATH=/private/bin", "GIT_AUTHOR_NAME=scoped", "KEEP=yes"}
			if tc.scoped != "" {
				env = append(env, "GH_CONFIG_DIR=/obsolete", "GH_CONFIG_DIR="+tc.scoped)
			}
			original := append([]string(nil), env...)
			got := a.forgeCommandEnv(env)
			require.Equal(t, original, env, "caller environment is immutable")
			require.Contains(t, got, "GIT_AUTHOR_NAME=scoped")
			require.Contains(t, got, "KEEP=yes")
			count := 0
			for _, value := range got {
				if strings.HasPrefix(value, "GH_CONFIG_DIR=") {
					count++
					require.Equal(t, "GH_CONFIG_DIR="+tc.want, value)
				}
			}
			if tc.want == "" {
				require.Zero(t, count)
			} else {
				require.Equal(t, 1, count)
			}
		})
	}
}

func TestSeatForgeChildrenReceiveIdentityWithoutChangingGit(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, testbin.WriteExecutable(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nif [ \"$1\" = api ]; then [ \"$GH_CONFIG_DIR\" = /seat ] && [ \"$KEEP\" = yes ] || exit 8; printf '1\\n'; else printf '%s:%s\\n' \"$GH_CONFIG_DIR\" \"$KEEP\"; fi\n"), 0o755))
	require.NoError(t, testbin.WriteExecutable(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf '%s:%s\\n' \"$GH_CONFIG_DIR\" \"$KEEP\"\n"), 0o755))
	a := newApp(func(string) string { return "" })
	a.seatEnvironment = &seatEnvironment{Version: 1, ForgeIdentity: "/seat"}
	a.gitEnv = []string{"PATH=" + bin, "KEEP=yes"}
	queue := a.mergeQueue
	held, err := queue.HoldsGroup(context.Background(), "https://github.com/o/r.git", "dev")
	require.NoError(t, err)
	require.True(t, held)
	p := &promoter{dir: t.TempDir(), env: a.gitEnv, forgeEnv: a.forgeCommandEnv(a.gitEnv)}
	got, err := p.gh(context.Background(), "pr", "view")
	require.NoError(t, err)
	require.Equal(t, "/seat:yes", got)
	got, err = p.git(context.Background(), "status")
	require.NoError(t, err)
	require.Equal(t, ":yes", got, "seat forge default is not applied to Git")
	p.check = `printf '%s:%s' "$GH_CONFIG_DIR" "$KEEP"`
	got, err = p.runGate(context.Background(), "0123456789ab")
	require.NoError(t, err)
	require.Equal(t, ":yes", got, "seat forge default is not applied to an arbitrary check")
	require.Equal(t, []string{"PATH=" + bin, "KEEP=yes"}, a.gitEnv)
}
