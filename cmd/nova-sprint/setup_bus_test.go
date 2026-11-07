package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetupBusSecretSelection(t *testing.T) {
	for _, tc := range []struct {
		name, names, user           string
		declared, failAuth, wantErr bool
		last                        string
	}{
		{"named authenticated", "NOVA_BUS_PASSWORD\n", "bus", false, false, false, "nova-secrets exec"},
		{"default authenticated", "NOVA_BUS_PASSWORD\n", "", false, false, false, "nova-secrets exec"},
		{"authentication failure has no fallback", "NOVA_BUS_PASSWORD\n", "", false, true, true, "nova-secrets exec"},
		{"named key missing", "OTHER\n", "bus", false, false, true, "nova-secrets names"},
		{"declared key missing", "OTHER\n", "", true, false, true, "nova-secrets names"},
		{"unauthenticated", "OTHER\n", "", false, false, false, "env NOVA_BUS_REDIS="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := configForSetup()
			cfg.BusRedis, cfg.BusUser = "127.0.0.1:6379", tc.user
			var calls []string
			run := func(_ context.Context, clean bool, name string, args ...string) ([]byte, error) {
				if !clean {
					t.Fatal("unsanitized subprocess")
				}
				call := strings.Join(append([]string{name}, args...), " ")
				calls = append(calls, call)
				if strings.HasPrefix(call, "nova-secrets names ") {
					return []byte(tc.names), nil
				}
				if tc.failAuth {
					return nil, errors.New("fixture authentication failure")
				}
				return []byte("WHO OK present=0\n"), nil
			}
			probe := setupBusCommand(cfg, func(context.Context) (bool, error) { return tc.declared, nil }, run)
			_, err := probe(context.Background(), "nova-bus", "who")
			if (err != nil) != tc.wantErr || len(calls) == 0 || !strings.HasPrefix(calls[len(calls)-1], tc.last) {
				t.Fatal(err, calls)
			}
			if tc.failAuth && len(calls) != 2 {
				t.Fatal("authentication fallback", calls)
			}
			if strings.Contains(calls[len(calls)-1], "NOVA_PG_PASSWORD") {
				t.Fatal("database credential passed to bus")
			}
		})
	}
}

func TestFunctionalSetupBusRedis(t *testing.T) {
	if os.Getenv("NOVA_FUNCTIONAL_BUS") != "1" {
		t.Skip("requires isolated Redis and secrets tools")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dir := t.TempDir()
	key, store, pins := filepath.Join(dir, "seat.key"), filepath.Join(dir, "store"), filepath.Join(dir, "pins")
	fixtureCommand(t, ctx, nil, "age-keygen", "-o", key)
	recovery := filepath.Join(dir, "recovery.key")
	fixtureCommand(t, ctx, nil, "age-keygen", "-o", recovery)
	public := strings.TrimSpace(string(fixtureCommand(t, ctx, nil, "age-keygen", "-y", key)))
	recoveryPublic := strings.TrimSpace(string(fixtureCommand(t, ctx, nil, "age-keygen", "-y", recovery)))
	fixtureCommand(t, ctx, nil, "nova-secrets", "init", "--store", store, "--recovery", recoveryPublic, "--pins", pins)
	fixtureCommand(t, ctx, nil, "nova-secrets", "seat", "add", "fixture", public, "--store", store)
	addr := fixtureRedis(t, ctx, dir)
	cfg := configForSetup()
	cfg.BusRedis, cfg.BusUser = addr, ""
	cfg.Secrets.Store, cfg.Secrets.KeyFile, cfg.Secrets.Pins, cfg.Secrets.Seat = store, key, pins, "fixture"
	probe := setupBusCommand(cfg, func(context.Context) (bool, error) { return false, nil }, runSetupCommand)
	if _, err := probe(ctx, "nova-bus", "who"); err == nil {
		t.Fatal("authentication-required Redis accepted no password")
	}
	fixtureCommand(t, ctx, strings.NewReader("isolated-fixture-only\n"), "nova-secrets", "set", "NOVA_BUS_PASSWORD", "--store", store, "--key", key, "--pins", pins, "--as", "fixture")
	output, err := probe(ctx, "nova-bus", "who")
	if err != nil || !strings.HasPrefix(string(output), "WHO OK present=") {
		t.Fatalf("secret-backed Redis probe failed: %v", err)
	}
}

func fixtureCommand(t *testing.T, ctx context.Context, input io.Reader, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = input
	cmd.Env = append(continuationEnv(os.Environ()), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("isolated fixture %s failed: %v", name, err)
	}
	return out
}

func fixtureRedis(t *testing.T, ctx context.Context, dir string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	path := filepath.Join(dir, "redis.conf")
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode no\nsave \"\"\nappendonly no\nrequirepass isolated-fixture-only\n", port)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "redis-server", path)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("isolated Redis did not start")
	return ""
}

func TestSetupBusMetadataAndCancellationFailClosed(t *testing.T) {
	cfg := configForSetup()
	for _, metadata := range []string{"NOVA_BUS_PASSWORD", "NOVA_BUS_PASSWORD\nBAD KEY\n", "\n"} {
		run := func(context.Context, bool, string, ...string) ([]byte, error) { return []byte(metadata), nil }
		probe := setupBusCommand(cfg, func(context.Context) (bool, error) { return false, nil }, run)
		if _, err := probe(context.Background(), "nova-bus", "who"); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := func(ctx context.Context, _ bool, _ string, _ ...string) ([]byte, error) { return nil, ctx.Err() }
	probe := setupBusCommand(cfg, func(context.Context) (bool, error) { t.Fatal("inventory after cancellation"); return false, nil }, run)
	if _, err := probe(ctx, "nova-bus", "who"); err == nil {
		t.Fatal("cancellation accepted")
	}
}
