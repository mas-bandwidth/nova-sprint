package worker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A JOB'S PROCESSES END WITH IT (2026-11-11). Every wall command runs as
// nice, then nova-reserve, which starts nova-sandbox and the command in a
// process group of its own (Setpgid), not in the group of the process the
// worker started; the worker's KillGroup killed only that first group, so
// whatever the harness or a gate left running (a shell a tool started, go
// test, dotnet build, a build server that left the group) outlived the
// command. nova-reserve's watchdog kills its command's group only when
// nova-reserve itself dies, and nova-sandbox's Pdeathsig reaches only its
// direct child. On 11-11 vision and hulk each held a returned job's tree
// (bash, go test, dotnet) reparented to the user's systemd, its job
// directory already gone, and the benches ran at load 115-200.
//
// A job's processes are found by what the wall gives each of them: its
// environment names the job directory (SanitizeWallEnv sets HOME, TMPDIR
// and XDG_DATA_HOME under it for every wall command, and every child
// inherits them, a daemon that called setsid included); on Linux, also a
// process whose working directory is in the job directory. Not found: a
// process that both cleared or replaced those three names and, on Linux,
// left the job directory (on macOS: cleared them). They are ended, TERM
// then KILL after the grace, at the end of every wall command (the job's
// steps run one at a time, so nothing of the job runs then) and before the
// worker lets a job directory go: the clean, a retire (a card returned or
// withdrawn), a quarantine. Model: tla/Runner.tla, NoProcessOutlivesItsJob.

// jobMarks_internal are the environment entries every wall command of the
// job at jobDir carries (SanitizeWallEnv), in both spellings of jobDir.
func jobMarks_internal(jobDir string) []string {
	var marks []string
	for _, d := range []string{filepath.Clean(jobDir), resolvePath_internal(jobDir)} {
		for _, kv := range []string{"HOME=" + filepath.Join(d, "home"), "TMPDIR=" + filepath.Join(d, "tmp"), "XDG_DATA_HOME=" + filepath.Join(d, "data")} {
			if !slices.Contains(marks, kv) {
				marks = append(marks, kv)
			}
		}
	}
	return marks
}

// jobProcesses_internal are the pids of the job at jobDir, never this
// process's own.
func jobProcesses_internal(jobDir string) ([]int, error) {
	if jobDir == "" {
		return nil, nil
	}
	marks := jobMarks_internal(jobDir)
	if runtime.GOOS == "linux" {
		return linuxJobProcesses_internal(jobDir, marks)
	}
	return psJobProcesses_internal(marks)
}

// linuxJobProcesses_internal reads /proc: a process whose environment holds
// a mark, or whose working directory is under jobDir. A process this user
// cannot read is not this worker's job.
func linuxJobProcesses_internal(jobDir string, marks []string) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self, dir := os.Getpid(), resolvePath_internal(jobDir)
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self || pid <= 1 {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		found := err == nil && slices.ContainsFunc(strings.Split(string(env), "\x00"), func(kv string) bool { return slices.Contains(marks, kv) })
		if !found {
			cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
			found = err == nil && (pathUnder_internal(cwd, dir) || pathUnder_internal(cwd, filepath.Clean(jobDir)))
		}
		if found {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// psJobProcesses_internal asks ps for every process's environment (-E: this
// user's processes show theirs), where /proc is not: macOS.
func psJobProcesses_internal(marks []string) ([]int, error) {
	out, err := exec.Command("ps", "-E", "-A", "-ww", "-o", "pid=", "-o", "command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps -E: %w", err)
	}
	self := os.Getpid()
	var pids []int
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil || pid == self || pid <= 1 {
			continue
		}
		if slices.ContainsFunc(f[1:], func(w string) bool { return slices.Contains(marks, w) }) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// OSKillJob ends every process of the job at jobDir (jobProcesses_internal):
// TERM, then KILL to whatever is still there after grace, and is how many
// it found.
func OSKillJob(jobDir string, grace time.Duration) (int, error) {
	pids, err := jobProcesses_internal(jobDir)
	if err != nil || len(pids) == 0 {
		return 0, err
	}
	signal := func(pids []int, sig syscall.Signal) error {
		var errs []error
		for _, pid := range pids {
			if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
				errs = append(errs, fmt.Errorf("kill -%d %d: %w", sig, pid, err))
			}
		}
		return errors.Join(errs...)
	}
	errs := []error{signal(pids, syscall.SIGTERM)}
	left := pids
	for deadline := time.Now().Add(grace); len(left) > 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if left, err = jobProcesses_internal(jobDir); err != nil {
			left = pids
		}
	}
	if len(left) > 0 {
		errs = append(errs, signal(left, syscall.SIGKILL))
	}
	return len(pids), errors.Join(errs...)
}

// endJobTree_internal ends what is left running of the job at jobDir before
// the worker lets the job go (what: the step), and says it on the step log.
func (r *Runner) endJobTree_internal(jobDir string, rec JobRecord, what string) {
	if r.KillJob == nil || jobDir == "" {
		return
	}
	n, err := r.KillJob(jobDir)
	r.report("ending the processes of "+rec.Consumer+" ("+what+")", err)
	if n > 0 {
		r.logStep_internal(rec.Row, rec.Consumer, "end-processes", fmt.Sprintf("%s: %d left running, ended", what, n), 0)
	}
}
