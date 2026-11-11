package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Every wall command ends what its job left running: the command's own
// group is nova-reserve's child's, which KillGroup never reached.
func TestEveryWallCommandEndsWhatItsJobLeftRunning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var ended []string
	w := &OSReserveWall{Exec: cr2OKExec_internal(), Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox"},
		KillJob: func(jobDir string) (int, error) { ended = append(ended, jobDir); return 2, nil }}
	j := Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}
	res, err := w.RunHarness(context.Background(), j, []string{"opencode", "run"}, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.RunTest(context.Background(), j, "go test ./...", time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(ended) != 2 || ended[0] != dir || ended[1] != dir {
		t.Errorf("the job's processes were ended for %q, want after the harness and after the gate, at %s", ended, dir)
	}
	if res.Leftovers != 2 {
		t.Errorf("leftovers %d, want the 2 KillJob ended on the step log", res.Leftovers)
	}
}

func TestJobMarksAreTheWallsJobDirectoryNames(t *testing.T) {
	t.Parallel()
	env := SanitizeWallEnv([]string{"PATH=/usr/bin"}, "/j/c1")
	for _, m := range jobMarks_internal("/j/c1") {
		found := false
		for _, kv := range env {
			found = found || kv == m
		}
		if !found {
			t.Errorf("mark %q is in no wall environment %q: a job's processes would not be found by it", m, env)
		}
	}
}

// processGone_internal: pid has exited (reaped, or a zombie waiting for its
// parent).
func processGone_internal(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return true
	}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if i := strings.LastIndexByte(string(b), ')'); i > 0 && i+2 < len(b) {
			return b[i+2] == 'Z'
		}
	}
	return false
}

// startJobProcess_internal starts argv as a job's process would run: the
// wall's environment for jobDir, a process group (or a session) of its own.
func startJobProcess_internal(t *testing.T, jobDir string, env []string, setsid bool, argv ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = jobDir, env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: !setsid, Setsid: setsid}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd
}

// A card returned from the row (a hold, a withdraw): the worker retires the
// job, and nothing of the job runs after it. Real processes, so functional:
// a harness that ignores HUP and TERM with a grandchild that does too (its
// group is not the one the worker started, as under nova-reserve), a child
// that left for a session of its own (a build server), and on Linux one that
// cleared its environment but works in the job directory.
func TestFunctionalReturnedJobLeavesNoProcessRunning(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	jobDir := filepath.Join(root, "jobs", "c1.w1")
	for _, d := range []string{"home", "tmp", "data"} {
		if err := os.MkdirAll(filepath.Join(jobDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := SanitizeWallEnv([]string{"PATH=" + os.Getenv("PATH")}, jobDir)
	harness := startJobProcess_internal(t, jobDir, env, false, "sh", "-c", `trap '' HUP TERM; sleep 300 & echo $! > "$TMPDIR/grandchild"; wait`)
	server := startJobProcess_internal(t, jobDir, env, true, "sleep", "301")
	pids := []int{harness.Process.Pid, server.Process.Pid}
	if runtime.GOOS == "linux" {
		pids = append(pids, startJobProcess_internal(t, jobDir, []string{"PATH=" + os.Getenv("PATH")}, true, "sleep", "302").Process.Pid)
	}
	var grandchild int
	for deadline := time.Now().Add(5 * time.Second); grandchild == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(filepath.Join(jobDir, "tmp", "grandchild"))
		grandchild, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if grandchild == 0 {
		t.Fatal("the harness's grandchild did not start")
	}
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })
	pids = append(pids, grandchild)

	r, err := NewRunner(Runner{Host: "b1", RootDir: root, Wall: &fakeWall{}, Now: time.Now, Sleep: time.Sleep,
		KillJob: func(d string) (int, error) { return OSKillJob(d, time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	r.retireJob_internal(jobDir, JobRecord{Epoch: 1, Consumer: "c1.w1", Kind: "work", Gen: 1}, "the card was returned")

	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		alive := 0
		for _, pid := range pids {
			if !processGone_internal(pid) {
				alive++
			}
		}
		if alive == 0 {
			return
		}
	}
	for _, pid := range pids {
		if !processGone_internal(pid) {
			t.Errorf("process %d of the returned job c1.w1 is still running after its retire", pid)
		}
	}
}
