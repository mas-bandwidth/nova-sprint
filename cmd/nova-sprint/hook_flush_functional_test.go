//go:build functional

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus/bustest"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// TestHookFlushesAfterChallengeOnTheStore is the red test on a real store: the
// hook's HOOK CHALLENGE line is readable on a real OS pipe within 1s while the
// hook is still running, on a private redis-server with an explicit --redis and
// a scrubbed environment. A regression that buffered stdout without flushing it
// would make this test time out.
func TestHookFlushesAfterChallengeOnTheStore(t *testing.T) {
	const name = "hook-flush-func"
	ctx := context.Background()
	addr := testutil.Start(t)
	busAddr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, fn.Load(ctx, c))

	home := t.TempDir()
	var serverAddr string
	env := func(k string) string {
		switch k {
		case "NOVA_SPRINT_REDIS":
			return addr
		case busRedisEnv:
			return busAddr
		case "NOVA_SPRINT_ACTOR":
			return name
		case "NOVA_SPRINT_SERVER":
			return serverAddr
		case "HOME":
			return home
		case "XDG_CONFIG_HOME":
			return filepath.Join(home, ".config")
		}
		return ""
	}
	a := newApp(env)
	t.Cleanup(a.close)
	// the bus store the hook's shared record and bus push live on, the way
	// hookSprint gives its in-process rig one: an in-memory bustest fake behind
	// the app's dial, never a socket, shared by the server and the hook process.
	fake := bustest.NewFake(t0, name, "glenn")
	fake.Sleep = func(time.Duration) { time.Sleep(5 * time.Millisecond) }
	busOpen := func(context.Context, string, string) (*bus.Bus, func(), error) {
		return &bus.Bus{Store: fake}, func() {}, nil
	}
	a.busOpen = busOpen
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, server string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("fixture command was forwarded to server %s: %q", server, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	var out, errs strings.Builder
	require.Equal(t, 0, runInto(a, []string{"init", "--members", "m1", "--readers", "reader-a", "--owner", name, "--redis", addr}, &out, &errs), "%s%s", out.String(), errs.String())
	a.serveAddr = addr
	srv := httptest.NewServer(localHandler{a})
	t.Cleanup(srv.Close)
	serverAddr = srv.Listener.Addr().String()

	client := newApp(env)
	client.hookSock = shortSock(t)
	client.busOpen = busOpen

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pr.Close()
	done := make(chan int, 1)
	go func() {
		var errb bytes.Buffer
		code := runInto(client, []string{"hook"}, pw, &errb)
		_ = pw.Close()
		done <- code
	}()

	challenge := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "HOOK CHALLENGE ") {
				challenge <- sc.Text()
				return
			}
		}
	}()

	select {
	case line := <-challenge:
		assert.Contains(t, line, "nova-sprint hook --prove ")
	case <-time.After(time.Second):
		t.Fatal("HOOK CHALLENGE was not readable on the OS pipe within 1s on a real store")
	}

	select {
	case code := <-done:
		t.Fatalf("the hook exited (code %d) before the challenge was read", code)
	default:
	}

	var uout, uerrs bytes.Buffer
	ucode := runInto(client, strings.Fields("unhook"), &uout, &uerrs)
	require.Equal(t, 0, ucode, uout.String()+uerrs.String())
	select {
	case code := <-done:
		require.Equal(t, 0, code)
	case <-time.After(10 * time.Second):
		t.Fatal("the hook did not end after unhook")
	}
}
