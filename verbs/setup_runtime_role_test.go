package verbs

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestScramVerifierKnownVector(t *testing.T) {
	t.Parallel()
	salt, _ := base64.StdEncoding.DecodeString("W22ZaJ0SNY7soEsUEjb6gQ==")
	got, err := scramVerifier("pencil", salt, 4096)
	want := "SCRAM-SHA-256$4096:W22ZaJ0SNY7soEsUEjb6gQ==$WG5d8oPm3OtcPnkdi4Uo7BkeZkBFzpcXkuLmtbsT4qY=:wfPLwcE6nTWhTAmQ7tl2KeoiWGPlZqQxSrmfPwDl2dU="
	if err != nil || got != want {
		t.Fatal("SCRAM vector mismatch", err)
	}
}

func TestRoleBootstrapSavesOnceAndPassesOnlyNames(t *testing.T) {
	t.Parallel()
	sink := &sinkFake{}
	var calls [][]string
	var modes []string
	grant := FleetGrants{Store: "/store", KeyFile: "/control.key", Pins: "/pins", ControlSeat: "studio", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	}}
	s := SprintRoleBootstrap{Sink: sink, Grants: grant, Secrets: NovaSecrets{Store: "/store", KeyFile: "/control.key", Seat: "studio"}, Binary: "/bin/nova-sprint", Random: bytes.NewReader(make([]byte, 32)), Execute: func(_ context.Context, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		modes = append(modes, args[len(args)-1])
		return nil
	}}
	seats := []SprintSeat{{Seat: "studio", Machine: "studio"}}
	if err := s.Run(context.Background(), seats); err != nil {
		t.Fatal(err)
	}
	first := sink.value
	if first == "" {
		t.Fatal("missing fixture")
	}
	if err := s.Run(context.Background(), seats); err != nil || sink.value != first {
		t.Fatal(err)
	}
	if len(modes) != 2 || modes[0] != "--internal-setup-role-fresh" || modes[1] != "--internal-setup-role" {
		t.Fatal("wrong role child modes", modes)
	}
	for _, call := range calls {
		for _, arg := range call {
			if strings.Contains(arg, first) {
				t.Fatal("credential reached argv")
			}
		}
	}
}

type roleFixtureExit int

func (e roleFixtureExit) Error() string { return "fixture exit" }
func (e roleFixtureExit) ExitCode() int { return int(e) }

func TestRoleBootstrapRefusesRejectedStoredSecret(t *testing.T) {
	t.Parallel()
	sink := &sinkFake{saved: true, value: "stored-fixture"}
	grant := FleetGrants{Store: "/store", KeyFile: "/key", Pins: "/pins", ControlSeat: "studio", Command: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}
	b := SprintRoleBootstrap{Sink: sink, Grants: grant, Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Binary: "/bin/nova-sprint", Execute: func(_ context.Context, _ string, args ...string) error {
		if args[len(args)-1] != "--internal-setup-role" {
			t.Fatalf("unexpected child: %v", args)
		}
		return roleFixtureExit(SprintRoleCredentialMismatchExit)
	}}
	err := b.Run(context.Background(), []SprintSeat{{Seat: "studio", Machine: "studio"}})
	var op *OperationError
	if !errors.As(err, &op) || op.Problem != "stored sprint credential was rejected" || !strings.Contains(op.Fix, "delete the stale NOVA_SPRINT_PG_PASSWORD") {
		t.Fatalf("wrong refusal: %v", err)
	}
	if sink.value != "stored-fixture" {
		t.Fatal("stored secret changed")
	}
}
