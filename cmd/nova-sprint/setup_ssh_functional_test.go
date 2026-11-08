package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This fixture exercises SSH and strict host-key checking against a private,
// unprivileged local sshd without touching the user's SSH files.
func TestFunctionalPublicVersionSSH(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUNCTIONAL_SSH") != "1" {
		t.Skip("requires an isolated local sshd")
	}
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		t.Fatalf("isolated SSH fixture needs sshd: %v", err)
	}
	dir := t.TempDir()
	for _, key := range []string{"host", "client"} {
		cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, key))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture key generation: %v: %s", err, out)
		}
	}
	account, err := user.Current()
	if err != nil || !safeSSHName(account.Username) {
		t.Fatalf("fixture account unavailable: %v", err)
	}
	port := fixtureSSHPort(t)
	clientPub, err := os.ReadFile(filepath.Join(dir, "client.pub"))
	if err != nil {
		t.Fatal(err)
	}
	writeSSHFixture(t, filepath.Join(dir, "authorized_keys"), clientPub, 0600)
	command := "#!/bin/sh\n[ \"$SSH_ORIGINAL_COMMAND\" = '~/.local/bin/nova-sprint version' ] || exit 1\nprintf 'nova-sprint v2.0.0\\nnext: nova-sprint help\\n'\n"
	writeSSHFixture(t, filepath.Join(dir, "version"), []byte(command), 0700)
	config := fmt.Sprintf("ListenAddress 127.0.0.1\nPort %d\nHostKey %s/host\nPidFile %s/pid\nAuthorizedKeysFile %s/authorized_keys\nStrictModes no\nUsePAM no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\nAllowUsers %s\nForceCommand %s/version\nLogLevel ERROR\n", port, dir, dir, dir, account.Username, dir)
	writeSSHFixture(t, filepath.Join(dir, "sshd_config"), []byte(config), 0600)
	fixtureSSHRun(t, sshd, dir, port)
	hostKey, err := os.ReadFile(filepath.Join(dir, "host.pub"))
	if err != nil {
		t.Fatal(err)
	}
	known := fmt.Sprintf("[127.0.0.1]:%d %s\n", port, strings.TrimSpace(string(hostKey)))
	writeSSHFixture(t, filepath.Join(dir, "known_hosts"), []byte(known), 0600)
	sshConfig := fmt.Sprintf("Host 127.0.0.1\n  Port %d\n  UserKnownHostsFile %s/known_hosts\n  GlobalKnownHostsFile /dev/null\n  IdentityFile %s/client\n  IdentitiesOnly yes\n", port, dir, dir)
	configPath := filepath.Join(dir, "ssh_config")
	writeSSHFixture(t, configPath, []byte(sshConfig), 0600)
	p := productionSetup{cfg: productionSetupConfig{BinaryVersion: "v2.0.0", Host: "control"}}
	p.command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "nova-config" {
			return []byte(fmt.Sprintf(`{"ansible_host":"127.0.0.1","ansible_user":%q}`, account.Username)), nil
		}
		if name != "ssh" {
			return nil, fmt.Errorf("unexpected fixture command")
		}
		return runSetupCommand(ctx, true, name, append([]string{"-F", configPath}, args...)...)
	}
	if err := p.checkServiceVersion(context.Background(), "fixture"); err != nil {
		t.Fatalf("private SSH probe failed: %v", err)
	}
	p.cfg.BinaryVersion = "v2.0.1"
	if err := p.checkServiceVersion(context.Background(), "fixture"); err == nil {
		t.Fatal("wrong remote version accepted")
	}
	writeSSHFixture(t, filepath.Join(dir, "known_hosts"), nil, 0600)
	p.cfg.BinaryVersion = "v2.0.0"
	if err := p.checkServiceVersion(context.Background(), "fixture"); err == nil {
		t.Fatal("untrusted SSH host accepted")
	}
}

func fixtureSSHPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func writeSSHFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func fixtureSSHRun(t *testing.T, sshd, dir string, port int) {
	t.Helper()
	cmd := exec.Command(sshd, "-D", "-e", "-f", filepath.Join(dir, "sshd_config"))
	var log bytes.Buffer
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatalf("start private sshd: %v", err)
	}
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	address := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	finished = true
	t.Fatalf("private sshd did not listen: %s", log.String())
}
