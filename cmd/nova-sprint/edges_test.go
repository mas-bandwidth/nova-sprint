package main

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

// TestIdentityNamesTheChoice: the forge acts as the host's gh login unless
// GH_GATE_TOKEN is set; a GH_TOKEN or GH_CONFIG_DIR in the environment is
// never the identity (directive 59), and the name in messages is the
// choice, never a login it expects.
func TestIdentityNamesTheChoice(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	host := identity(env(map[string]string{"GH_TOKEN": "x", "GH_CONFIG_DIR": "/d"}))
	if host.Token != "" || host.Name != forge.HostLogin {
		t.Fatalf("with GH_TOKEN and GH_CONFIG_DIR only: %+v, want the host's gh login and no token", host)
	}
	over := identity(env(map[string]string{forge.TokenEnv: "tok-secret"}))
	if over.Token != "tok-secret" || !strings.HasPrefix(over.Name, forge.TokenEnv) || strings.Contains(over.Name, "tok-secret") {
		t.Fatalf("with %s: %+v, want the override named by its variable", forge.TokenEnv, over)
	}
	if strings.Contains(host.Name+over.Name, "sprint") {
		t.Fatalf("an identity is named for a login: %q, %q", host.Name, over.Name)
	}
}
