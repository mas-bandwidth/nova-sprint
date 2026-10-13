package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// identityRemedy is the one remedy every machine-identity refusal names.
const identityRemedy = "run: nova-config sprint set --git_identity 'nova-sprint <nova-sprint@example.invalid>' then nova-config apply"

// readMachineIdentity reads the sprint row's git_identity and returns the author and
// committer environment it spells (docs/SPEC-SPRINT.md, the machine's commit identity); an
// unapplied row reads config.DefaultGitIdentity (store.Bars.WithDefaults), and why is a
// refusal only for a value that does not parse as Name <email>, naming the setting. what is the machine's
// verb the refusal speaks of (a landing, a promotion), so the words name what refused.
func readMachineIdentity(ctx context.Context, st *store.Store, what string) ([]string, string) {
	id, err := st.GitIdentity(ctx)
	if err != nil {
		return nil, "the git identity could not be read: " + oneline.Err(err) + "; " + identityRemedy
	}
	parsed, ok := config.ParseGitIdentity(id)
	if !ok {
		return nil, "the sprint row's git_identity " + strconv.Quote(id) + " is not 'Name <email>', so the " + what + " would commit as the host's global git config; " + identityRemedy
	}
	return parsed.Env(), ""
}

// withMachineGitIdentity returns env with the configured identity's author and committer
// set, dropping any the caller's environment carries first, so the configured identity
// wins over the host's environment whatever it held.
func withMachineGitIdentity(env, identity []string) []string {
	if len(identity) == 0 {
		return env
	}
	out := make([]string, 0, len(env)+len(identity))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL":
			continue
		}
		out = append(out, kv)
	}
	return append(out, identity...)
}
