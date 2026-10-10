package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

func TestParseSaveMark(t *testing.T) {
	m := parseSaveMark("# Persistence\r\nrdb_saves:7\r\nrdb_last_save_time:1760000000\r\nrdb_bgsave_in_progress:0\r\n")
	assert.True(t, m.hasSaves)
	assert.EqualValues(t, 7, m.saves)
	assert.True(t, m.hasTime)
	assert.EqualValues(t, 1760000000, m.lastTime)
	assert.True(t, m.usable())

	// Redis before 7 has no rdb_saves.
	old := parseSaveMark("rdb_last_save_time:100\r\n")
	assert.False(t, old.hasSaves)
	assert.True(t, old.usable())

	// Nothing usable, or garbage values.
	assert.False(t, parseSaveMark("").usable())
	assert.False(t, parseSaveMark("rdb_saves:x\r\nrdb_last_save_time:\r\n").usable())
}

func TestSaveMarkMovedSince(t *testing.T) {
	before := parseSaveMark("rdb_saves:5\r\nrdb_last_save_time:100\r\n")
	// The counter moves even inside the same second.
	assert.True(t, parseSaveMark("rdb_saves:6\r\nrdb_last_save_time:100\r\n").movedSince(before))
	assert.False(t, parseSaveMark("rdb_saves:5\r\nrdb_last_save_time:100\r\n").movedSince(before))
	// Counter wins over time when both sides carry it.
	assert.False(t, parseSaveMark("rdb_saves:5\r\nrdb_last_save_time:200\r\n").movedSince(before))

	// Time only (Redis before 7).
	tb := parseSaveMark("rdb_last_save_time:100\r\n")
	assert.True(t, parseSaveMark("rdb_last_save_time:101\r\n").movedSince(tb))
	assert.False(t, parseSaveMark("rdb_last_save_time:100\r\n").movedSince(tb))
	// A side with no marker never counts as moved.
	assert.False(t, parseSaveMark("").movedSince(tb))
}

// fakeRedis speaks just enough RESP for redisSource.Save: INFO persistence,
// BGSAVE, CONFIG GET. LASTSAVE answers NOPERM like the coordinator's ACL user,
// and is counted. A BGSAVE bumps rdb_saves.
type fakeRedis struct {
	mu       sync.Mutex
	saves    int
	lastsave int
	bgsaves  int
	dir      string
	failInfo string
}

func (f *fakeRedis) serve(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.conn(c)
		}
	}()
	return ln.Addr().String()
}

func (f *fakeRedis) conn(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "*") {
			return
		}
		n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
		args := make([]string, 0, n)
		for i := 0; i < n; i++ {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			sz, _ := strconv.Atoi(strings.TrimSpace(l[1:]))
			buf := make([]byte, sz+2)
			if _, err := io.ReadFull(r, buf); err != nil {
				return
			}
			args = append(args, string(buf[:sz]))
		}
		f.mu.Lock()
		switch strings.ToUpper(args[0]) {
		case "LASTSAVE":
			f.lastsave++
			fmt.Fprint(c, "-NOPERM this user has no permissions to run the 'lastsave' command\r\n")
		case "BGSAVE":
			f.bgsaves++
			f.saves++
			fmt.Fprint(c, "+Background saving started\r\n")
		case "INFO":
			body := "# Persistence\r\nrdb_bgsave_in_progress:0\r\nrdb_last_bgsave_status:" + f.statusLocked() + "\r\nrdb_saves:" + strconv.Itoa(f.saves) + "\r\nrdb_last_save_time:1760000000\r\n"
			fmt.Fprintf(c, "$%d\r\n%s\r\n", len(body), body)
		case "CONFIG":
			key := args[2]
			val := f.dir
			if key == "dbfilename" {
				val = "dump.rdb"
			}
			fmt.Fprintf(c, "*2\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(key), key, len(val), val)
		default:
			fmt.Fprint(c, "-ERR unknown command\r\n")
		}
		f.mu.Unlock()
	}
}

func (f *fakeRedis) statusLocked() string {
	if f.failInfo != "" {
		return f.failInfo
	}
	return "ok"
}

// TestRedisSourceSaveNeverCallsLastSave: the save completes and the file is
// returned although LASTSAVE is refused, which is the coordinator's ACL.
func TestRedisSourceSaveNeverCallsLastSave(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dump.rdb"), []byte("RDBDATA"), 0o600))
	f := &fakeRedis{saves: 3, dir: dir}
	addr := f.serve(t)
	cl := redis.NewClient(&redis.Options{Addr: addr, Protocol: 2, DisableIdentity: true})
	t.Cleanup(func() { cl.Close() })

	b, _, err := (&redisSource{b: &store.Redis{C: cl}}).Save(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "RDBDATA", string(b))
	assert.Equal(t, 0, f.lastsave, "LASTSAVE must never be called")
	assert.Equal(t, 1, f.bgsaves)
}

// TestRedisSourceSaveReportsBgsaveFailure: a failed save is still an error.
func TestRedisSourceSaveReportsBgsaveFailure(t *testing.T) {
	f := &fakeRedis{saves: 3, dir: t.TempDir(), failInfo: "err"}
	addr := f.serve(t)
	cl := redis.NewClient(&redis.Options{Addr: addr, Protocol: 2, DisableIdentity: true})
	t.Cleanup(func() { cl.Close() })

	_, _, err := (&redisSource{b: &store.Redis{C: cl}}).Save(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BGSAVE failed")
	assert.Equal(t, 0, f.lastsave)
}
