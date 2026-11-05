//go:build unix

package worker

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

type osProc struct{ cmd *exec.Cmd }

// ExecProc is the real StartProc: argv in its own process group, stdin empty, its
// output appended to log.
func ExecProc(argv, env []string, dir string, log *os.File) (Proc, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env, cmd.Dir, cmd.Stdout, cmd.Stderr = env, dir, log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProc{cmd: cmd}, nil
}

func (p *osProc) Wait() int {
	err := p.cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return exit.ExitCode()
	}
	return -1
}

func (p *osProc) Kill() { swarm.KillGroup(p.cmd.Process.Pid, "") }

// FreeBytes is the space free to this user on dir's volume.
func FreeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
