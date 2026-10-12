// Package gitidentity resolves the identity configured for commits made by
// nova-sprint machinery. It deliberately never falls back to Git's global
// user.name or user.email.
package gitidentity

import (
	"fmt"
	"os"
	"strings"
)

const (
	NameSetting  = "NOVA_SPRINT_GIT_NAME"
	EmailSetting = "NOVA_SPRINT_GIT_EMAIL"
	DefaultName  = "nova-sprint"
	DefaultEmail = "nova-sprint@mas-bandwidth.com"
)

// Identity is the configured name and email for machine-made commits.
type Identity struct {
	Name  string
	Email string
}

// Resolve reads the pair as one setting. When both variables are empty,
// whether unset or explicitly empty, the documented default is the
// configuration. A partial pair is
// refused instead of silently combining it with the host's Git config.
func Resolve(getenv func(string) string) (Identity, error) {
	name, email := strings.TrimSpace(getenv(NameSetting)), strings.TrimSpace(getenv(EmailSetting))
	if name == "" && email == "" {
		return Identity{Name: DefaultName, Email: DefaultEmail}, nil
	}
	if name == "" {
		return Identity{}, fmt.Errorf("%s is empty; set both %s and %s (or unset both to use %s <%s>)", NameSetting, NameSetting, EmailSetting, DefaultName, DefaultEmail)
	}
	if email == "" || !strings.Contains(email, "@") {
		return Identity{}, fmt.Errorf("%s is empty or invalid; set both %s and %s (or unset both to use %s <%s>)", EmailSetting, NameSetting, EmailSetting, DefaultName, DefaultEmail)
	}
	return Identity{Name: name, Email: email}, nil
}

// OverrideEnv replaces Git's author and committer variables with id while
// preserving the rest of env. Git gives these variables precedence over all
// config scopes, including a repository's local config.
func OverrideEnv(env []string, id Identity) []string {
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env)+4)
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL":
			continue
		}
		out = append(out, item)
	}
	return append(out,
		"GIT_AUTHOR_NAME="+id.Name,
		"GIT_AUTHOR_EMAIL="+id.Email,
		"GIT_COMMITTER_NAME="+id.Name,
		"GIT_COMMITTER_EMAIL="+id.Email,
	)
}
