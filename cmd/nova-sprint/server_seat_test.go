package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// hook-finds-the-server-itself: when NOVA_SPRINT_SERVER is unset, the server
// address falls back to the seat record; hook reports where it came from.
func TestServerFromSeatRecord(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	
	// Create a mock login file so seat record can be read
	loginDir := filepath.Join(cfg, "nova-sprint")
	require.NoError(t, os.MkdirAll(loginDir, 0o700))
	loginFile := filepath.Join(loginDir, "login.json")
	require.NoError(t, os.WriteFile(loginFile, []byte(`{"as": "test", "store": "mem"}`), 0o600))
	
	// Create seat record with server address
	recordFile := filepath.Join(loginDir, "seat.json")
	record := seatRecord{Server: "127.0.0.1:7399"}
	recBytes, _ := json.Marshal(record)
	require.NoError(t, os.WriteFile(recordFile, recBytes, 0o600))
	
	env := map[string]string{
		"NOVA_SPRINT_REDIS":   "mem:0",
		"NOVA_SPRINT_ACTOR":   "coordinator",
		"XDG_CONFIG_HOME":     cfg,
	}
	a := newApp(func(k string) string { return env[k] })
	a.seatLoginOn()
	a.goos = "linux"
	
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		return []sprintwire.Result{{}}, nil
	}
	
	// Test 1: Without NOVA_SPRINT_SERVER, server() should fall back to seat record
	env["NOVA_SPRINT_SERVER"] = ""
	assert.Equal(t, "127.0.0.1:7399", a.server(nil))
	
	// Test 2: With NOVA_SPRINT_SERVER set, it wins over the seat record
	env["NOVA_SPRINT_SERVER"] = "127.0.0.1:7500"
	assert.Equal(t, "127.0.0.1:7500", a.server(nil))
	
	// Test 3: With serveAddr set (this process IS the server), server() returns ""
	a.serveAddr = "127.0.0.1:6390"
	assert.Equal(t, "", a.server(nil))
	
	// Test 4: With --redis flag, server() returns ""
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("redis", "127.0.0.1:6381", "redis")
	assert.Equal(t, "", a.server(fs))
}
