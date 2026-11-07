package verbs

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestScramVerifierKnownVector(t *testing.T) {
	salt, _ := base64.StdEncoding.DecodeString("W22ZaJ0SNY7soEsUEjb6gQ==")
	got, err := scramVerifier("pencil", salt, 4096)
	want := "SCRAM-SHA-256$4096:W22ZaJ0SNY7soEsUEjb6gQ==$WG5d8oPm3OtcPnkdi4Uo7BkeZkBFzpcXkuLmtbsT4qY=:wfPLwcE6nTWhTAmQ7tl2KeoiWGPlZqQxSrmfPwDl2dU="
	if err != nil || got != want {
		t.Fatal("SCRAM vector mismatch", err)
	}
}

func TestRoleBootstrapSavesOnceAndPassesOnlyNames(t *testing.T) {
	sink := &sinkFake{}
	var calls [][]string
	grant := FleetGrants{Store: "/store", KeyFile: "/control.key", Pins: "/pins", ControlSeat: "studio", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	}}
	s := SprintRoleBootstrap{Sink: sink, Grants: grant, Secrets: NovaSecrets{Store: "/store", KeyFile: "/control.key", Seat: "studio"}, Binary: "/bin/nova-sprint", Random: bytes.NewReader(make([]byte, 32)), Inspect: func(context.Context, string, ...string) ([]byte, error) { return []byte(`{"exists":false}`), nil }, Execute: func(_ context.Context, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
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
	for _, call := range calls {
		for _, arg := range call {
			if strings.Contains(arg, first) {
				t.Fatal("credential reached argv")
			}
		}
	}
}

func TestRoleBootstrapDoesNotRotateAnExistingRole(t *testing.T) {
	sink := &sinkFake{}
	s := SprintRoleBootstrap{Sink: sink, Grants: FleetGrants{Store: "/store", KeyFile: "/key", Pins: "/pins", ControlSeat: "studio"}, Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Binary: "/bin/nova-sprint", Random: bytes.NewReader(make([]byte, 32)), Inspect: func(context.Context, string, ...string) ([]byte, error) { return []byte(`{"exists":true}`), nil }}
	err := s.Run(context.Background(), []SprintSeat{{Seat: "studio", Machine: "studio"}})
	if err == nil || sink.saved {
		t.Fatal("existing role was rotated", err)
	}
}
