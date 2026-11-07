package main

import "testing"

func TestSelfBusReplyStopsWithoutSending(t *testing.T) {
	t.Setenv("NOVA_BUS_FROM", "sprint")
	t.Setenv("NOVA_BUS_ID", "1-1")
	t.Setenv("PATH", t.TempDir())
	if code := command([]string{"bus-reply"}); code != 0 {
		t.Fatalf("self callback exit %d", code)
	}
}
func TestDashRejectsConflictingModes(t *testing.T) {
	if code := command([]string{"dash", "--pull", "http://example.test", "--pg", "fake"}); code != 1 {
		t.Fatalf("exit %d", code)
	}
}
