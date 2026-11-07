package verbs

import (
	"context"
	"errors"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionAdoptionRequiresCheckAndShadow(t *testing.T) {
	t.Parallel()
	calls := []string{}
	installed := "v2.0.0"
	shadowErr := false
	a := VersionAdoption{Binary: "/bin/nova-sprint", ResolvePath: func() (string, error) { return "/bin/nova-sprint", nil }, Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Keys: []string{sprintPasswordName}, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if args[0] == "version" {
			return []byte(installed + "\n"), nil
		}
		if name == "nova-secrets" && args[len(args)-1] == "--shadow" && shadowErr {
			return nil, errors.New("failed")
		}
		return nil, nil
	}}
	ok, err := a.Ready(context.Background(), "v2.0.0")
	if err != nil || !ok || len(calls) != 3 {
		t.Fatal(ok, err, calls)
	}
	installed = "dev+0123456789ab"
	ok, err = a.Ready(context.Background(), "v2.0.0")
	if err != nil || ok {
		t.Fatal(ok, err)
	}
	installed = "v1.0.0"
	ok, err = a.Ready(context.Background(), "v2.0.0")
	if err != nil || ok {
		t.Fatal(ok, err)
	}
	installed = "v2.0.0"
	shadowErr = true
	ok, err = a.Ready(context.Background(), "v2.0.0")
	if err == nil || ok {
		t.Fatal(ok, err)
	}
}

// A release that is not the one on PATH is never installed by up (the
// retired nova-update): up refuses naming nova-fleet install, and runs
// nothing.
func TestVersionAdoptionApplyNamesNovaFleet(t *testing.T) {
	t.Parallel()
	ran := []string{}
	a := VersionAdoption{Binary: "/bin/nova-sprint", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		ran = append(ran, name)
		return nil, nil
	}}
	err := a.Apply(context.Background(), "v2.0.1")
	var op *OperationError
	if !errors.As(err, &op) || op.Exit != 1 || op.Fix != "nova-fleet install --repo <org/repo> --tag v2.0.1 --names nova-sprint <machine>, then nova-sprint up again" || len(ran) != 0 {
		t.Fatal(err, ran)
	}
	if strings.Contains(err.Error(), "nova-update") {
		t.Fatal(err)
	}
}

type adoptionStub struct {
	ready   bool
	applied int
}

func (a *adoptionStub) Ready(context.Context, string) (bool, error) { return a.ready, nil }
func (a *adoptionStub) Apply(context.Context, string) error         { a.applied++; a.ready = true; return nil }
func TestVersionAdoptionRunsWithStoreStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &runtimeStore{ready: true}
	adopt := &adoptionStub{}
	r := RuntimeSetup{Store: store, Adoption: adopt}
	in := SetupInput{Version: "v2.0.0"}
	check, err := r.Verify(ctx, 2, in)
	if err != nil || !check.Action {
		t.Fatal(check, err)
	}
	if err = r.Act(ctx, 2, in); err != nil {
		t.Fatal(err)
	}
	check, err = r.Verify(ctx, 2, in)
	if err != nil || !check.Verified || adopt.applied != 1 || store.actions != 0 {
		t.Fatal(check, err, adopt.applied, store.actions)
	}
}

// nova-tools' release is the module version stamped in the installed
// binary's build info; a development or pseudo-version build, or another
// module's binary, is unknown.
func TestToolsReleaseFromBuildInfo(t *testing.T) {
	t.Parallel()
	info := func(path, version string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: path, Version: version}}
	}
	for _, tc := range []struct {
		info *debug.BuildInfo
		want string
	}{
		{info("github.com/mas-bandwidth/nova-tools/v2", "v2.0.2"), "v2.0.2"},
		{info("github.com/mas-bandwidth/nova-tools/v3", "v3.1.0"), "v3.1.0"},
		{info("github.com/mas-bandwidth/nova-tools/v2", "(devel)"), ""},
		{info("github.com/mas-bandwidth/nova-tools/v2", "v2.0.3-0.20261106000000-0123456789ab"), ""},
		{info("github.com/mas-bandwidth/nova-tools/v2", "v2.0.2+dirty"), ""},
		{info("example.com/other", "v2.0.2"), ""},
		{nil, ""},
	} {
		got, err := toolsRelease(tc.info)
		if got != tc.want || (err == nil) != (tc.want != "") {
			t.Fatalf("%+v: %q %v", tc.info, got, err)
		}
	}
}
