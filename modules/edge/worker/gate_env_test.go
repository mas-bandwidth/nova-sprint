package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// A done-when gate runs in the bench's toolchain environment: the wall
// sources the bench's env.sh (read-only, under a --read grant) and execs the
// gate's one command, as a worker's own shell does. A bench without it, or
// with it outside every grant, runs the gate as it is, and a failed gate's
// output names the script and why.
func TestGateRunsInTheBenchToolchainEnvironment(t *testing.T) {
	t.Parallel()
	sdk := t.TempDir()
	script := filepath.Join(sdk, "env.sh")
	if err := os.WriteFile(script, []byte("export SQ=sq\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		script, read string
		exit         int
		wantArgv     []string
		wantNote     string
	}{
		"sourced":     {script: script, read: sdk, wantArgv: []string{"/bin/sh", "-c", gateSourceScript_internal, script, "npm", "test"}},
		"no script":   {script: filepath.Join(sdk, "missing", "env.sh"), read: sdk, exit: 127, wantArgv: []string{"npm", "test"}, wantNote: "missing/env.sh is not on this bench"},
		"not granted": {script: script, read: t.TempDir(), exit: 127, wantArgv: []string{"npm", "test"}, wantNote: "no --read grant covers it"},
		"passing":     {script: filepath.Join(sdk, "missing", "env.sh"), read: sdk, wantArgv: []string{"npm", "test"}},
		"unset":       {wantArgv: []string{"npm", "test"}},
	} {
		fx := &wallExec{res: edge.Result{ExitCode: tc.exit, Stderr: "nova-sandbox: npm is on no PATH entry\n"}}
		w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReadPaths: []string{tc.read}, EnvScript: tc.script}}
		res, err := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: t.TempDir()}, "npm test", time.Minute)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		argv := fx.argvs[0]
		if got := argv[slices.Index(argv, "--")+1:]; !slices.Equal(got[slices.Index(got, "--")+1:], tc.wantArgv) {
			t.Errorf("%s: the wall runs %q, want %q", name, got, tc.wantArgv)
		}
		if tc.wantNote == "" && strings.Contains(string(res.Output), "toolchain environment") || tc.wantNote != "" && !strings.Contains(string(res.Output), tc.wantNote) {
			t.Errorf("%s: output %q, want the note %q", name, res.Output, tc.wantNote)
		}
	}
}
