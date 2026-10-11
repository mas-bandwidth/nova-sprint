package ci

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestTLCWorkflowRunsOnHostedRunners pins .github/workflows/tlc.yml: the TLC
// cases run on GitHub-hosted runners only (this is a public repository, so the
// fleet's self-hosted runners are never used), every job is held to the
// five-minute wall, the models job checks the downloaded jar against
// tla/tla2tools.sha256 before it calls make tlc, and it calls make tlc with the
// fixed 110-second budget. It reads the file as text and parses it; no network.
func TestTLCWorkflowRunsOnHostedRunners(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/tlc.yml")
	if err != nil {
		t.Fatalf("read .github/workflows/tlc.yml: %v", err)
	}
	s := string(b)

	if strings.Contains(s, "self-hosted") {
		t.Error("tlc.yml must not name a self-hosted runner")
	}

	var wf struct {
		Jobs map[string]struct {
			RunsOn         string `yaml:"runs-on"`
			TimeoutMinutes int    `yaml:"timeout-minutes"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("parse tlc.yml: %v", err)
	}
	if len(wf.Jobs) == 0 {
		t.Fatal("tlc.yml has no jobs")
	}
	for name, job := range wf.Jobs {
		if job.RunsOn != "ubuntu-latest" {
			t.Errorf("job %q runs on %q, want ubuntu-latest", name, job.RunsOn)
		}
		if job.TimeoutMinutes < 1 || job.TimeoutMinutes > 5 {
			t.Errorf("job %q timeout-minutes is %d, want at most 5", name, job.TimeoutMinutes)
		}
	}

	shaAt := strings.Index(s, "tla/tla2tools.sha256")
	tlcAt := strings.Index(s, "make tlc TLC_JAR")
	if shaAt < 0 {
		t.Error("tlc.yml does not check the jar against tla/tla2tools.sha256")
	}
	if tlcAt < 0 {
		t.Error("tlc.yml does not call make tlc")
	}
	if shaAt >= 0 && tlcAt >= 0 && shaAt > tlcAt {
		t.Error("tlc.yml calls make tlc before it checks the jar against tla/tla2tools.sha256")
	}
	if !strings.Contains(s, "sha256sum") {
		t.Error("tlc.yml does not run sha256sum on the jar")
	}
	if !strings.Contains(s, "TLC_BUDGET=110") {
		t.Error("tlc.yml does not call make tlc with TLC_BUDGET=110")
	}
}
