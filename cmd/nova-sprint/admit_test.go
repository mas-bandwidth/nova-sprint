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

// TestHostForgeRequiredChecks: the required checks are the branch
// protection's contexts and the rulesets' contexts, joined without repeats;
// either read failing is an error. gh is a fake: no network.
func TestHostForgeRequiredChecks(t *testing.T) {
	var argvs []string
	fail := ""
	f := hostForge{exec: forge.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		line := strings.Join(cmd.Argv, " ")
		argvs = append(argvs, line)
		switch {
		case fail != "" && strings.Contains(line, fail):
			return edge.Result{ExitCode: 1, Stderr: "HTTP 403"}, nil
		case strings.Contains(line, "/rules/branches/"):
			return edge.Result{Stdout: "Gates\nContributor Assignment Agreement\n"}, nil
		}
		return edge.Result{Stdout: "Gates\n\nLinux Debug\n"}, nil
	})}
	got, err := f.RequiredChecks(context.Background(), "o/r", "main")
	if err != nil || strings.Join(got, "|") != "Gates|Linux Debug|Contributor Assignment Agreement" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(argvs) != 2 || !strings.HasPrefix(argvs[0], "gh api repos/o/r/branches/main --jq") || !strings.HasPrefix(argvs[1], "gh api repos/o/r/rules/branches/main --jq") {
		t.Fatalf("argvs %q", argvs)
	}
	for _, which := range []string{"/rules/branches/", "/branches/main --jq"} {
		fail = which
		if _, err := f.RequiredChecks(context.Background(), "o/r", "main"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
			t.Fatalf("%s: %v", which, err)
		}
	}
}
