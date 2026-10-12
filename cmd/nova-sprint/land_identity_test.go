package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLandCommitCarriesTheConfiguredIdentity proves a landing commit's author and committer
// are the sprint row's git_identity even when HOME carries a different global user.email
// (docs/SPEC-SPRINT.md, the machine's commit identity). The rig's environment already names a
// different author (lander@example.invalid); the test plants a global git config under a fresh
// HOME naming yet another, and the landing must still commit as the configured identity.
func TestLandCommitCarriesTheConfiguredIdentity(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.m.SetGitIdentity("nova-sprint <nova-sprint@example.invalid>")

	// A fresh HOME with a global git config naming a different identity: the host's global
	// config the machine must never commit as.
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Owner\n\temail = owner@example.com\n"), 0o600))
	env := make([]string, 0, len(r.env))
	for _, kv := range r.env {
		if strings.HasPrefix(kv, "GIT_CONFIG_GLOBAL=") || strings.HasPrefix(kv, "GIT_CONFIG_NOSYSTEM=") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home)
	r.a.gitEnv = env

	r.ok("add --stream s1 --count 1 --one")
	heads := map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}
	r.queued(heads, "s1-1")
	r.ok("land --repo-dir " + r.clone + " --base main")

	// The landing commit's author and committer are the configured identity, not the rig's
	// env identity and not the planted global config.
	got := r.git(r.remote, "log", "-1", "--format=%an <%ae>|%cn <%ce>", "main")
	assert.Equal(t, "nova-sprint <nova-sprint@example.invalid>|nova-sprint <nova-sprint@example.invalid>", got)
	r.clean()
}

// TestLandRefusesWhenNoIdentityIsConfigured proves the landing refuses to commit, naming the
// setting, when the sprint row names no git_identity.
func TestLandRefusesWhenNoIdentityIsConfigured(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.m.SetGitIdentity("")
	r.ok("add --stream s1 --count 1 --one")
	heads := map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}
	r.queued(heads, "s1-1")
	before := r.git(r.remote, "rev-parse", "main")
	code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "git_identity")
	assert.Contains(t, errs, "nova-config sprint set --git_identity")
	assert.Equal(t, before, r.git(r.remote, "rev-parse", "main"), "nothing was pushed")
	r.clean()
}
