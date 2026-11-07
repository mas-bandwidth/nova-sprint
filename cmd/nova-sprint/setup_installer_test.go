package main

import (
	"context"
	"reflect"
	"testing"
)

func TestFleetPublicInstallerArgs(t *testing.T) {
	var got []string
	f := fleetPublicInstaller{command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}}
	if err := f.Install(context.Background(), "public-host", "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	want := []string{"nova-fleet", "install", "--repo", "mas-bandwidth/nova-sprint", "--tag", "v2.0.0", "--names", "nova-sprint", "public-host"}
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
