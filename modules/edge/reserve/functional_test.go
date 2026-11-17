package reserve

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionalFetchOnceTCPAndUnix(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				rd := bufio.NewReader(conn)
				l1, err := rd.ReadBytes('\n')
				if err != nil {
					return
				}
				var auth AuthMsg
				_ = json.Unmarshal(l1, &auth)
				if auth.Password != "pw123" || auth.Version != 3 {
					_, _ = conn.Write([]byte(`{"op":"refused","code":"unauthorized","message":"bad password"}` + "\n"))
					return
				}
				_, _ = rd.ReadBytes('\n') // subscribe
				_, _ = conn.Write([]byte(`{"op":"snapshot","machines":[{"host":"vision","online":true,"tags":["linux","amd64"],"resources":{"cpu":24,"memory":54},"free":{"cpu":24,"memory":54}}]}` + "\n"))
			}(c)
		}
	}()

	_, err = FetchOnce(context.Background(), Config{Addr: ln.Addr().String(), Password: "wrong", Client: "check"})
	require.ErrorContains(t, err, "unauthorized")

	snap, err := FetchOnce(context.Background(), Config{Addr: ln.Addr().String(), Password: "pw123", Client: "check"})
	require.NoError(t, err)
	require.True(t, snap.Connected)
	require.Len(t, snap.Machines, 1)
	assert.Equal(t, "vision", snap.Machines[0].Host)

	sock := filepath.Join(t.TempDir(), "sock")
	uln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	defer func() { _ = uln.Close() }()

	go func() {
		c, err := uln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		rd := bufio.NewReader(c)
		l1, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var sub SubscribeMsg
		_ = json.Unmarshal(l1, &sub)
		if sub.Op == "subscribe" {
			_, _ = c.Write([]byte(`{"op":"snapshot","machines":[{"host":"local-bench","online":true,"resources":{"cpu":8,"memory":16},"free":{"cpu":8,"memory":16}}]}` + "\n"))
		}
	}()

	usnap, err := FetchOnce(context.Background(), Config{Socket: sock, Client: "check"})
	require.NoError(t, err)
	require.Len(t, usnap.Machines, 1)
	assert.Equal(t, "local-bench", usnap.Machines[0].Host)
}
