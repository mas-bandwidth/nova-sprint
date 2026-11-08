package forge

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

const (
	actHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	actTip  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	actMB   = "cccccccccccccccccccccccccccccccccccccccc"
)

// actionsGH_internal is gh with canned JSON for compare, runs, jobs and
// logs (as --jq projects them), keyed by the api path; it records each path.
func actionsGH_internal(canned map[string]string, paths *[]string) *Client {
	return New(Config{Identity: Identity{Name: HostLogin}, Now: func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			path := cmd.Argv[2]
			if cmd.Argv[1] == "pr" {
				path = "pr " + cmd.Argv[3]
			}
			*paths = append(*paths, path)
			if out, ok := canned[path]; ok {
				return edge.Result{Stdout: out}, nil
			}
			return edge.Result{ExitCode: 1, Stderr: "HTTP 404: Not Found (" + path + ")"}, nil
		})})
}

func actionsCanned_internal() map[string]string {
	r := "repos/o/r/"
	return map[string]string{
		r + "compare/" + actTip + "..." + actHead: actMB + "\n",
		r + "actions/runs?per_page=100&head_sha=" + actHead: `[
			{"id":11,"name":"hostile-corpus","workflow_id":101,"status":"completed","conclusion":"failure"},
			{"id":12,"name":"Gates","workflow_id":102,"status":"completed","conclusion":"failure"},
			{"id":13,"name":"dart","workflow_id":103,"status":"completed","conclusion":"failure"},
			{"id":14,"name":"Build","workflow_id":104,"status":"in_progress","conclusion":null},
			{"id":15,"name":"Lint","workflow_id":105,"status":"completed","conclusion":"success"},
			{"id":16,"name":"Docs","workflow_id":106,"status":"queued","conclusion":null}]`,
		r + "actions/runs?per_page=100&head_sha=" + actMB: `[
			{"id":21,"name":"hostile-corpus","workflow_id":101,"status":"completed","conclusion":"failure"},
			{"id":22,"name":"Gates","workflow_id":102,"status":"completed","conclusion":"success"},
			{"id":24,"name":"Build","workflow_id":104,"status":"in_progress","conclusion":null}]`,
		r + "actions/workflows/103/runs?per_page=1&status=completed&branch=sprint%2Fschema-100": `[{"id":33,"name":"dart","workflow_id":103,"status":"completed","conclusion":"failure"}]`,
		r + "actions/workflows/104/runs?per_page=1&status=completed&branch=sprint%2Fschema-100": `[]`,
		r + "actions/runs/11/jobs?per_page=100":                                                 `[{"id":111,"name":"corpus","conclusion":"failure","runner_name":"bench-a"}]`,
		r + "actions/runs/12/jobs?per_page=100":                                                 `[{"id":121,"name":"Linux Debug","conclusion":"failure","runner_name":"bench-b","html_url":"https://github.com/o/r/actions/runs/12/job/121"},{"id":122,"name":"Linux Release","conclusion":"success","runner_name":"bench-c"}]`,
		r + "actions/runs/13/jobs?per_page=100":                                                 `[{"id":131,"name":"dart","conclusion":"failure","runner_name":"bench-a"}]`,
		r + "actions/runs/14/jobs?per_page=100":                                                 `[{"id":141,"name":"Windows Release","conclusion":"failure","runner_name":"win-1"},{"id":142,"name":"Linux Debug","conclusion":null,"runner_name":""}]`,
		r + "actions/runs/16/jobs?per_page=100":                                                 `[]`,
		r + "actions/jobs/121/logs":                                                             "2026-11-07T08:00:00.0000000Z ##[group]Run go test ./...\n2026-11-07T08:00:01.1234567Z --- FAIL: TestSchemaRoundTrip (0.01s)\n2026-11-07T08:00:01.2000000Z     roundtrip_test.go:41: field 3 lost\n2026-11-07T08:00:02.0000000Z FAIL\tgithub.com/o/r/schema\t0.4s\n2026-11-07T08:00:03.0000000Z ##[error]Process completed with exit code 1.\n",
		r + "actions/jobs/141/logs":                                                             "2026-11-07T08:00:00.0000000Z cl : error C2065: 'x': undeclared identifier\n",
	}
}

// TestInheritanceSplitsByTheMergeBase (directives 59, 76): the head's
// failing workflows split by the merge base (compare, then runs at both);
// a workflow with no completed run at the merge base is compared with the
// base branch's last completed run of it and says so; one with none there
// either is new. A running workflow with a failed job is failing; a running
// one without is not. Only new red's logs are read, timestamps stripped.
func TestInheritanceSplitsByTheMergeBase(t *testing.T) {
	t.Parallel()
	var paths []string
	c := actionsGH_internal(actionsCanned_internal(), &paths)
	split, err := c.Inheritance(context.Background(), "o/r", "sprint/schema-100", actTip, actHead)
	if err != nil {
		t.Fatalf("Inheritance: %v", err)
	}
	if split.MergeBase != actMB {
		t.Fatalf("merge base %s, want %s", split.MergeBase, actMB)
	}
	names := func(ws []WorkflowRed) (out []string) {
		for _, w := range ws {
			out = append(out, w.Workflow+": "+w.Basis)
		}
		return out
	}
	wantInherited := []string{"hostile-corpus: the merge base cccccccccccc", "dart: sprint/schema-100's last completed run 33 (the merge base cccccccccccc ran none)"}
	wantNew := []string{"Gates: the merge base cccccccccccc", "Build: no completed run on sprint/schema-100 to compare with"}
	if !slices.Equal(names(split.Inherited), wantInherited) || !slices.Equal(names(split.New), wantNew) {
		t.Fatalf("inherited %q new %q; want %q and %q", names(split.Inherited), names(split.New), wantInherited, wantNew)
	}
	gates, build := split.New[0], split.New[1]
	if len(gates.Jobs) != 1 || gates.Jobs[0].Name != "Linux Debug" || gates.Jobs[0].Runner != "bench-b" || gates.Jobs[0].URL == "" || gates.Running {
		t.Fatalf("Gates jobs %+v, want the one failed job with its runner and page", gates)
	}
	wantLines := []string{"--- FAIL: TestSchemaRoundTrip (0.01s)", "FAIL\tgithub.com/o/r/schema\t0.4s", "##[error]Process completed with exit code 1."}
	if !slices.Equal(gates.Jobs[0].Lines, wantLines) {
		t.Fatalf("Gates lines %q, want %q", gates.Jobs[0].Lines, wantLines)
	}
	if !build.Running || len(build.Jobs) != 1 || build.Jobs[0].Runner != "win-1" || !strings.Contains(strings.Join(build.Jobs[0].Lines, " "), "error C2065") {
		t.Fatalf("Build %+v, want running with its failed Windows job and its error line", build)
	}
	for _, p := range paths {
		if strings.Contains(p, "jobs/111/logs") || strings.Contains(p, "jobs/131/logs") || strings.Contains(p, "runs/15/jobs") {
			t.Fatalf("read %s: inherited red's logs and green runs' jobs are never read", p)
		}
	}
}

// A compare that fails, or prints no sha, is an error naming it: the
// landing then says the red was not compared.
func TestInheritanceFaultsNameTheCall(t *testing.T) {
	t.Parallel()
	var paths []string
	canned := actionsCanned_internal()
	delete(canned, "repos/o/r/compare/"+actTip+"..."+actHead)
	if _, err := actionsGH_internal(canned, &paths).Inheritance(context.Background(), "o/r", "main", actTip, actHead); err == nil || !strings.Contains(err.Error(), "compare") {
		t.Fatalf("missing compare: err %v, want one naming the compare", err)
	}
	canned["repos/o/r/compare/"+actTip+"..."+actHead] = "null\n"
	if _, err := actionsGH_internal(canned, &paths).Inheritance(context.Background(), "o/r", "main", actTip, actHead); err == nil || !strings.Contains(err.Error(), "no merge base") {
		t.Fatalf("no sha: err %v, want no merge base", err)
	}
}

// A job log that cannot be read is said in its lines, never dropped.
func TestInheritanceUnreadLogIsSaid(t *testing.T) {
	t.Parallel()
	var paths []string
	canned := actionsCanned_internal()
	delete(canned, "repos/o/r/actions/jobs/121/logs")
	split, err := actionsGH_internal(canned, &paths).Inheritance(context.Background(), "o/r", "sprint/schema-100", actTip, actHead)
	if err != nil || len(split.New) == 0 || !strings.HasPrefix(strings.Join(split.New[0].Jobs[0].Lines, ""), "(the log was not read: ") {
		t.Fatalf("split %+v err %v, want the unread log said", split, err)
	}
}

// TestAuthorsAndAgreements (directive 63): an agreement check is named
// like one; Authors is the PR's distinct commit authors.
func TestAuthorsAndAgreements(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{"Contributor Assignment Agreement": true, "license/cla": true, "CLA": true, "cla-check": true,
		"Gates / Linux Debug": false, "clang-tidy": false, "declare": false} {
		if IsAgreement(name) != want {
			t.Errorf("IsAgreement(%q) = %v, want %v", name, !want, want)
		}
	}
	var paths []string
	canned := map[string]string{"pr 2830": `[{"login":"aurora-x","name":"Aurora","email":"aurora@example.invalid"},
		{"login":"","name":"Bench","email":"bench@example.invalid"},{"login":"aurora-x","name":"Aurora","email":"aurora@example.invalid"}]`}
	got, err := actionsGH_internal(canned, &paths).Authors(context.Background(), "o/r", 2830)
	want := []string{"Aurora <aurora@example.invalid> (aurora-x)", "Bench <bench@example.invalid>"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Authors = %q, %v; want %q", got, err, want)
	}
	if _, err := actionsGH_internal(map[string]string{"pr 1": "not json"}, &paths).Authors(context.Background(), "o/r", 1); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("bad json: %v", err)
	}
}

// TestAPIPathTakesOwnerRepoFromAURL: a stream whose repository is a clone
// URL gets owner/repo in every api path, never the URL pasted in.
func TestAPIPathTakesOwnerRepoFromAURL(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"o/r":                         "o/r",
		"https://github.com/o/r":      "o/r",
		"https://github.com/o/r.git":  "o/r",
		"https://github.com/o/r.git/": "o/r",
		"git@github.com:o/r.git":      "o/r",
		"ssh://git@github.com/o/r":    "o/r",
		"/srv/git/r.git":              "/srv/git/r.git",
	} {
		if got := RepoPath(in); got != want {
			t.Errorf("RepoPath(%q) = %q, want %q", in, got, want)
		}
	}
	var paths []string
	c := actionsGH_internal(actionsCanned_internal(), &paths)
	if _, err := c.Inheritance(context.Background(), "https://github.com/o/r.git", "sprint/schema-100", actTip, actHead); err != nil {
		t.Fatal(err)
	}
	if _, err := c.JobLines(context.Background(), "https://github.com/o/r", 121); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if !strings.HasPrefix(p, "repos/o/r/") && !strings.HasPrefix(p, "pr ") {
			t.Fatalf("an api path from a URL: %q (all %q)", p, paths)
		}
	}
}
