package main

import "testing"

// TestGitHubCredentialFollowsTheIdentity: gh answers github.com's
// credential only when the process carries a GitHub identity.
func TestGitHubCredentialFollowsTheIdentity(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	cases := []struct {
		kv   map[string]string
		want string
	}{
		{kv: map[string]string{"GH_TOKEN": "x"}, want: "gh"},
		{kv: map[string]string{"GH_CONFIG_DIR": "/d"}, want: "gh"},
		{kv: map[string]string{}, want: ""},
	}
	for _, c := range cases {
		if got := githubCredential(env(c.kv)); got != c.want {
			t.Errorf("githubCredential(%v) = %q, want %q", c.kv, got, c.want)
		}
	}
}
