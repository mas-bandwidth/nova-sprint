package main

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

// TestHostForge: CanAct reads gh's viewerPermission and refuses below
// write, and a gh that cannot read, naming the fix; PR reads head and state.
// gh is a fake (forge.ExecFunc): no network.
func TestHostForge(t *testing.T) {
	out, code := "WRITE\n", 0
	var argv []string
	f := hostForge{exec: forge.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		argv = cmd.Argv
		return edge.Result{Stdout: out, ExitCode: code, Stderr: "HTTP 404"}, nil
	})}
	ctx := context.Background()
	if err := f.CanAct(ctx, "o/r"); err != nil || strings.Join(argv, " ") != "gh repo view o/r --json viewerPermission --jq .viewerPermission" {
		t.Fatalf("write: %v %v", err, argv)
	}
	out = "READ\n"
	if err := f.CanAct(ctx, "o/r"); err == nil || !strings.Contains(err.Error(), "READ permission on o/r") {
		t.Fatalf("read: %v", err)
	}
	out, code = "", 1
	if err := f.CanAct(ctx, "o/r"); err == nil || !strings.Contains(err.Error(), "cannot read o/r") {
		t.Fatalf("no read: %v", err)
	}
	out, code = `{"headRefOid":"ABC","state":"MERGED"}`, 0
	head, merged, err := f.PR(ctx, "o/r", 7)
	if err != nil || head != "abc" || !merged || strings.Join(argv, " ") != "gh pr view 7 -R o/r --json headRefOid,state" {
		t.Fatalf("pr: %q %v %v %v", head, merged, err, argv)
	}
	out = "not json"
	if _, _, err := f.PR(ctx, "o/r", 7); err == nil {
		t.Fatal("a body that is not json read")
	}
}
