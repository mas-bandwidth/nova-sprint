package run

import (
	"strings"
	"testing"
)

func TestMachineIdentityIgnoresDisplaySuffixes(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"x", "x.local", "X.local", "x.tail1234.ts.net"} {
		if !SameMachine(host, "x") || MachineName(host) != "x" {
			t.Fatalf("%q is not machine x", host)
		}
	}
	if SameMachine("amd-2", "amd-1") || SameMachine("", "") {
		t.Fatal("different machines agree")
	}
}

func TestResolveMachine(t *testing.T) {
	t.Parallel()
	machines := []string{"amd-1", "x"}
	if got, err := ResolveMachine("", "x.local", machines); err != nil || got != "x" {
		t.Fatal(got, err)
	}
	if got, err := ResolveMachine("amd-1", "other.local", machines); err != nil || got != "amd-1" {
		t.Fatal(got, err)
	}
	_, err := ResolveMachine("", "amd-2.local", machines)
	if err == nil || !strings.Contains(err.Error(), `"amd-2.local"`) || !strings.Contains(err.Error(), "no machine named \"amd-2\"") {
		t.Fatal(err)
	}
	if _, err = ResolveMachine("amd-2", "x.local", machines); err == nil || !strings.Contains(err.Error(), "amd-2") {
		t.Fatal(err)
	}
}
