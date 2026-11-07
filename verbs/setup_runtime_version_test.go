package verbs

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestVersionAdoptionRequiresCheckAndShadow(t *testing.T) {
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
func TestVersionAdoptionChecksActivatedPath(t *testing.T) {
	path := "/releases/old/nova-sprint"
	commands := []string{}
	a := VersionAdoption{Binary: path, ResolvePath: func() (string, error) { return path, nil }, MatchesPath: func(s string) bool { return s == path }, Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Keys: []string{sprintPasswordName}, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		if name == "nova-update" {
			path = "/releases/new/nova-sprint"
		} else if len(args) == 1 && args[0] == "version" {
			return []byte("v2.0.0\n"), nil
		}
		return nil, nil
	}}
	if err := a.Apply(context.Background(), "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	ok, err := a.Ready(context.Background(), "v2.0.0")
	if err != nil || !ok || len(commands) != 4 || !strings.HasPrefix(commands[1], path+" version") || !strings.Contains(commands[2], path+" check") || !strings.Contains(commands[3], path+" run --shadow") {
		t.Fatal("activated binary was not checked", err, commands)
	}
}
func TestFunctionalVersionAdoptionManifest(t *testing.T) {
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("uses private temporary manifest")
	}
	seen := false
	a := VersionAdoption{Binary: "/bin/nova-sprint", MatchesPath: func(string) bool { return true }, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "nova-update" || len(args) != 6 || args[0] != "apply" || args[1] != "--file" || args[3] != "nova-sprint" || args[4] != "--version" || args[5] != "v2.0.0" {
			t.Fatal("wrong command")
		}
		b, err := os.ReadFile(args[2])
		if err != nil || string(b) != sprintUpdateManifest {
			t.Fatal("wrong manifest", err)
		}
		seen = true
		return nil, nil
	}}
	if err := a.Apply(context.Background(), "v2.0.0"); err != nil || !seen {
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
