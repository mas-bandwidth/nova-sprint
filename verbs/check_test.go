package verbs

import (
	"context"
	"testing"
)

type fakeHealth struct {
	version string
	issues  []OperationError
}

func (f fakeHealth) Check(context.Context, bool) ([]OperationError, error) { return f.issues, nil }
func (f fakeHealth) NovaToolsVersion(context.Context) (string, error)      { return f.version, nil }
func TestToolPin(t *testing.T) {
	for _, tc := range []struct {
		have, pin string
		ok, valid bool
	}{{"v2.1.0", "v2.0.9", true, true}, {"v1.9.9", "v2.0.0", false, true}, {"v2.0.0", "v2.0.0", true, true}, {"v2.0.0-rc1", "v2.0.0", false, false}, {"v02.0.0", "v2.0.0", false, false}} {
		got, err := atLeast(tc.have, tc.pin)
		if got != tc.ok || (err == nil) != tc.valid {
			t.Fatal(tc, got, err)
		}
	}
	health := fakeHealth{version: "nova-version v1.0.0 linux/amd64 go1.26.10"}
	e := Engine{Health: health, ToolPin: "v2.0.0"}
	r := e.Execute(context.Background(), []string{"check"})
	if r.Exit != 1 || r.Output != "SPRINT REFUSED check: nova-tools v1.0.0 is below pin v2.0.0: nova-fleet up\n" {
		t.Fatal(r)
	}
}

func TestUnknownToolVersionExactFix(t *testing.T) {
	e := Engine{Health: fakeHealth{version: "unrecognized"}, ToolPin: "v1.6.29"}
	r := e.Execute(context.Background(), []string{"check"})
	if r.Exit != 1 || r.Output != "SPRINT REFUSED check: nova-tools version unknown: install nova-tools >= v1.6.29 with nova-update\n" {
		t.Fatal(r)
	}
	if v, ok := parseNovaVersion("nova-version v1.6.29 linux/amd64 go1.26.10"); !ok || v != "v1.6.29" {
		t.Fatal(v, ok)
	}
}

func TestBuildVersionOutput(t *testing.T) {
	for _, v := range []string{"v2.0.0", "dev+abcdef123456"} {
		e := Engine{Version: v}
		r := e.Execute(context.Background(), []string{"version"})
		if r.Exit != 0 || r.Output != v+"\n" {
			t.Fatal(r)
		}
	}
	if validBuildVersion("dev+bad") || validBuildVersion("2.0.0") {
		t.Fatal("invalid build version accepted")
	}
}

type historyFake struct {
	calls int
	err   error
}

func (h *historyFake) CheckHistory(context.Context) error { h.calls++; return h.err }
func TestCheckHistoryIsStoreOnlyAndIntegrityChecksItFirst(t *testing.T) {
	h := &historyFake{}
	e := Engine{History: h}
	r := e.Execute(context.Background(), []string{"check", "--history"})
	if r.Exit != 0 || h.calls != 1 {
		t.Fatal(r, h.calls)
	}
	h.err = &OperationError{Problem: "history differs", Fix: "restore a verified backup", Exit: 1}
	r = e.Execute(context.Background(), []string{"check", "--integrity"})
	if r.Exit != 1 || h.calls != 2 || r.Output != "SPRINT REFUSED check: history differs: restore a verified backup\n" {
		t.Fatal(r, h.calls)
	}
}
