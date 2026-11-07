package git

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

// GitHubHost is where a stream whose forge is github clones from.
const GitHubHost = "https://github.com/"

// CloneURL is the one place a stream's repository becomes what git clones
// and ls-remotes (DEFECT 21: `stream add --repo <org>/<repo> --forge github`
// handed git the bare org/repo). The stream keeps repo as it was given: it
// is the forge's identity of the repository (gh -R <org>/<repo>).
//
//   - a URL (scheme://...), an scp-like address (user@host:path) or an
//     absolute or ./ path is a clone address already and is returned as is;
//   - an <org>/<repo> with forge github is https://github.com/<org>/<repo>.git;
//   - anything else with forge github is refused, naming the form;
//   - with no forge the repo is returned as is: a hand stream names its
//     repository by URL or path.
//
// The scheme is https, never ssh: the sprint's GitHub identity is the
// host's gh login (or the GH_GATE_TOKEN override), and git reads it
// through gh as its credential helper for github.com
// (Config.GitHubCredential), so no host needs an ssh key registered with
// GitHub.
func CloneURL(repo, forge string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", fmt.Errorf("git refused: a stream's repository is empty: name it --repo <org>/<repo>")
	}
	if isCloneAddress_internal(repo) || forge == "" {
		return repo, nil
	}
	if forge != "github" {
		return "", fmt.Errorf("git refused: forge %q has no clone address for %s: the forges are github and none", forge, repo)
	}
	if !IsOrgRepo(repo) {
		return "", fmt.Errorf("git refused: repository %q is neither <org>/<repo> nor a clone URL: name it --repo <org>/<repo>", repo)
	}
	return GitHubHost + strings.TrimSuffix(repo, ".git") + ".git", nil
}

// IsOrgRepo is whether repo is the <org>/<repo> form a forge names a
// repository by: two non-empty segments of a-z A-Z 0-9 . _ - (neither "."
// nor ".."), joined by one slash.
func IsOrgRepo(repo string) bool {
	org, name, ok := strings.Cut(repo, "/")
	return ok && orgRepoPart_internal(org) && orgRepoPart_internal(name)
}

func orgRepoPart_internal(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// isCloneAddress_internal is a URL, an scp-like address or a path git takes
// as it is.
func isCloneAddress_internal(repo string) bool {
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "../") || strings.HasPrefix(repo, "~") {
		return true
	}
	at, colon := strings.Index(repo, "@"), strings.Index(repo, ":")
	return colon > 0 && (at < 0 || at < colon) && !strings.Contains(repo[:colon], "/")
}

// credentialEnv_internal is the git environment that makes gh, as the
// host's own login, the only credential helper for https://github.com (an
// empty helper first clears the host's own, so a person's keychain never
// answers for the sprint). gh runs through env(1) with every GitHub
// credential of the process unset (forge.Unset), so a GH_TOKEN or a
// GH_CONFIG_DIR in the environment cannot make it someone else (directive
// 59). It names no secret.
func credentialEnv_internal(gh string) []string {
	if gh == "" {
		return nil
	}
	key := "credential." + strings.TrimSuffix(GitHubHost, "/") + ".helper"
	helper := "!env " + strings.Join(forge.Unset(), " ") + " " + gh + " auth git-credential"
	return []string{"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=" + key, "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=" + key, "GIT_CONFIG_VALUE_1=" + helper}
}
