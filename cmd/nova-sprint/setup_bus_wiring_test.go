package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBootstrapBusProbeDoesNotReadUnavailableInventory(t *testing.T) {
	var inventory int
	fleet := func(context.Context, string, ...string) ([]byte, error) {
		inventory++
		return nil, errors.New("unavailable")
	}
	run := func(_ context.Context, _ bool, name string, args ...string) ([]byte, error) {
		if name == "nova-secrets" {
			return nil, nil
		}
		if name == "env" && strings.Contains(strings.Join(args, " "), "nova-bus who") {
			return nil, errors.New("NOAUTH")
		}
		return nil, errors.New("unexpected command")
	}
	probe := setupCommandWithBusRunner(productionSetupConfig{BusRedis: "localhost:1"}, fleet, run)
	if _, err := probe(context.Background(), "nova-bus", "who"); err == nil || inventory != 0 {
		t.Fatalf("err=%v inventory calls=%d", err, inventory)
	}
}

func TestEstablishedBusProbeRefusesInventoryFailure(t *testing.T) {
	fleet := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("inventory unavailable")
	}
	run := func(_ context.Context, _ bool, name string, _ ...string) ([]byte, error) {
		if name == "nova-secrets" {
			return nil, nil
		}
		t.Fatal("must not probe without inventory")
		return nil, nil
	}
	probe := setupCommandWithBusRunner(productionSetupConfig{SprintPassword: "present"}, fleet, run)
	if _, err := probe(context.Background(), "nova-bus", "who"); err == nil {
		t.Fatal("inventory failure accepted")
	}
}
