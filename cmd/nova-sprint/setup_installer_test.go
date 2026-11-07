package main

import (
	"context"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

func TestFleetPublicInstallerArgs(t *testing.T) {
	t.Parallel()
	var got []string
	f := fleetPublicInstaller{command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}, repo: "someone/fork"}
	if err := f.Install(context.Background(), "public-host", "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	want := []string{"nova-fleet", "install", "--repo", "someone/fork", "--tag", "v2.0.0", "--names", "nova-sprint", "public-host"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if err := f.Install(context.Background(), "public-host", "v2.0.0-rc.1"); err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := f.Install(context.Background(), "public-host", "dev+123456789abc"); err == nil || got != nil {
		t.Fatalf("invalid version: got %q err %v", got, err)
	}
}

// TestInstallerUsesConfiguredRepo: the repository nova-fleet installs from
// is NOVA_SPRINT_REPO, else the binary's own module path, never a literal;
// with neither, Install refuses naming the variable and runs nothing.
func TestInstallerUsesConfiguredRepo(t *testing.T) {
	t.Parallel()
	info := func(path string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Path: path}} }
	for _, c := range []struct {
		env  string
		info *debug.BuildInfo
		want string
	}{
		{"", info("github.com/acme/sprint-fork"), "acme/sprint-fork"},
		{"", info("github.com/acme/sprint-fork/v3"), "acme/sprint-fork"},
		{"other/repo", info("github.com/acme/sprint-fork"), "other/repo"},
		{"", info("example.org/acme/sprint"), ""},
		{"", info("command-line-arguments"), ""},
		{"", nil, ""},
	} {
		if got := installRepo(c.env, c.info); got != c.want {
			t.Errorf("installRepo(%q, %v) = %q, want %q", c.env, c.info, got, c.want)
		}
	}
	ran := false
	f := fleetPublicInstaller{command: func(context.Context, string, ...string) ([]byte, error) { ran = true; return nil, nil }}
	err := f.Install(context.Background(), "public-host", "v2.0.0")
	if err == nil || ran || !strings.Contains(err.Error(), RepoEnv+"=<org/repo>") {
		t.Fatalf("no repo: ran %t err %v", ran, err)
	}
}
