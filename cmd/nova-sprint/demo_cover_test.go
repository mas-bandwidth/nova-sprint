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
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
)

// fakeDemoRedis answers a Redis client over net.Pipe (no socket, no Redis,
// no subprocess), the fakeStore pattern of main_cover_test.go widened to
// whole commands: it records every command, counts RESTOREs for DBSIZE,
// and can refuse a load, hold a different library, refuse one RESTORE, or
// misreport its size. Replies are written by their own goroutine so a
// pipeline's writes never wait on its replies.
type fakeDemoRedis struct {
	mu            sync.Mutex
	cmds          [][]string
	restored      int
	loadRefused   bool
	listReply     string
	restoreRefuse string
	dbSize        int // -1: answer the restored count
}

func newFakeDemoRedis(listReply string) *fakeDemoRedis {
	return &fakeDemoRedis{listReply: listReply, dbSize: -1}
}

func (s *fakeDemoRedis) dial(_ context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	go s.serve(server)
	return client, nil
}

func (s *fakeDemoRedis) serve(c net.Conn) {
	defer c.Close()
	replies := make(chan string, 4096)
	go func() {
		for r := range replies {
			if _, err := io.WriteString(c, r); err != nil {
				return
			}
		}
	}()
	rd := bufio.NewReader(c)
	for {
		words, err := readFakeFullCommand(rd)
		if err != nil {
			close(replies)
			return
		}
		replies <- s.answer(words)
	}
}

func (s *fakeDemoRedis) answer(words []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cmds = append(s.cmds, words)
	switch name := strings.ToUpper(words[0]); {
	case name == "HELLO":
		return helloAccepted
	case name == "FUNCTION" && len(words) > 1 && strings.EqualFold(words[1], "LOAD"):
		if s.loadRefused {
			return "-ERR bad library\r\n"
		}
		return "$11\r\nnova_sprint\r\n"
	case name == "FUNCTION" && len(words) > 1 && strings.EqualFold(words[1], "LIST"):
		return s.listReply
	case name == "RESTORE":
		if len(words) > 1 && words[1] == s.restoreRefuse {
			return "-ERR restore refused in the test\r\n"
		}
		s.restored++
		return "+OK\r\n"
	case name == "DBSIZE":
		if s.dbSize >= 0 {
			return fmt.Sprintf(":%d\r\n", s.dbSize)
		}
		return fmt.Sprintf(":%d\r\n", s.restored)
	default:
		return "+OK\r\n"
	}
}

func (s *fakeDemoRedis) commands(name string) [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][]string
	for _, w := range s.cmds {
		if strings.EqualFold(w[0], name) {
			out = append(out, w)
		}
	}
	return out
}

func (s *fakeDemoRedis) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.cmds))
	for i, w := range s.cmds {
		out[i] = strings.ToUpper(w[0])
		if out[i] == "FUNCTION" && len(w) > 1 {
			out[i] += " " + strings.ToUpper(w[1])
		}
	}
	return out
}

// readFakeFullCommand reads one RESP array of bulk strings whole.
func readFakeFullCommand(rd *bufio.Reader) ([]string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "*"), "\r\n"))
	if err != nil {
		return nil, err
	}
	words := make([]string, 0, n)
	for range n {
		head, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(head, "$"), "\r\n"))
		if err != nil {
			return nil, err
		}
		body := make([]byte, size+2)
		if _, err := io.ReadFull(rd, body); err != nil {
			return nil, err
		}
		words = append(words, string(body[:size]))
	}
	return words, nil
}

func fakeDemoClient(t *testing.T, store *fakeDemoRedis) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Dialer: store.dial, MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func restoreLine(key, payload string) string {
	return "RESTORE " + demoQuote(key) + " 0 " + demoQuote(payload) + " REPLACE\n"
}

// fakeCatXZ is an xz stand-in: a script that copies its input, so a part
// holds plain RESTORE text; fakeBadXZ fails as a damaged backup's xz does.
func fakeXZ(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xz")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700))
	return path
}

func writeParts(t *testing.T, text string) []string {
	t.Helper()
	part := filepath.Join(t.TempDir(), "s.redis.txt.xz.part-aa")
	require.NoError(t, os.WriteFile(part, []byte(text), 0o600))
	return []string{part}
}

func TestDemoReplayLinesReplaysRestoresAndSkipsComments(t *testing.T) {
	t.Parallel()
	store := newFakeDemoRedis("")
	c := fakeDemoClient(t, store)
	dump := "# a backup header\n\n" +
		restoreLine("demo:k1", "payload one") +
		"   \n" +
		restoreLine("demo:k2", "payload\x00two")
	n, err := demoReplayLines(context.Background(), c, strings.NewReader(dump))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	restores := store.commands("RESTORE")
	require.Len(t, restores, 2)
	assert.Equal(t, []string{"RESTORE", "demo:k1", "0", "payload one", "REPLACE"}, restores[0])
	assert.Equal(t, []string{"RESTORE", "demo:k2", "0", "payload\x00two", "REPLACE"}, restores[1])
}

func TestDemoReplayLinesRefusesLinesThatAreNotRestores(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, dump, holds string }{
		{"another command", "SET demo:k v\n", "line 1 of the dump is not a RESTORE"},
		{"a short RESTORE", restoreLine("demo:k1", "p") + "RESTORE demo:k2 0\n", "line 2 of the dump is not a RESTORE"},
		{"an unreadable line", "RESTORE \"open 0 x\n", "line 1 of the dump cannot be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeDemoRedis("")
			n, err := demoReplayLines(context.Background(), fakeDemoClient(t, store), strings.NewReader(tc.dump))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.holds)
			assert.Zero(t, n, "an unflushed RESTORE was counted")
			assert.Empty(t, store.commands("RESTORE"), "a refused dump sent a RESTORE")
		})
	}
}

func TestDemoReplayLinesNamesARestoreTheStoreRefuses(t *testing.T) {
	t.Parallel()
	store := newFakeDemoRedis("")
	store.restoreRefuse = "demo:bad"
	dump := restoreLine("demo:good", "p") + restoreLine("demo:bad", "p")
	n, err := demoReplayLines(context.Background(), fakeDemoClient(t, store), strings.NewReader(dump))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RESTORE demo:bad: ERR restore refused in the test")
	assert.Zero(t, n)
}

func TestDemoReplayLinesFlushesFullBatches(t *testing.T) {
	t.Parallel()
	store := newFakeDemoRedis("")
	var dump strings.Builder
	for i := range demoReplayBatch + 1 {
		fmt.Fprintf(&dump, "RESTORE demo:k%d 0 %s REPLACE\n", i, demoQuote("p"))
	}
	n, err := demoReplayLines(context.Background(), fakeDemoClient(t, store), strings.NewReader(dump.String()))
	require.NoError(t, err)
	assert.Equal(t, demoReplayBatch+1, n)
	assert.Len(t, store.commands("RESTORE"), demoReplayBatch+1)
}

func TestDemoReplayDecompressesAndReplaysTheParts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	parts := writeParts(t, restoreLine("demo:k1", "one")+restoreLine("demo:k2", "two"))

	store := newFakeDemoRedis("")
	n, err := demoReplay(ctx, fakeDemoClient(t, store), fakeXZ(t, "cat\n"), parts)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Len(t, store.commands("RESTORE"), 2)

	// no xz to decompress with: nothing is replayed
	store = newFakeDemoRedis("")
	_, err = demoReplay(ctx, fakeDemoClient(t, store), filepath.Join(t.TempDir(), "no-xz"), parts)
	require.Error(t, err)
	assert.Empty(t, store.commands("RESTORE"))

	// an xz that fails on the parts: its own words are the error
	store = newFakeDemoRedis("")
	_, err = demoReplay(ctx, fakeDemoClient(t, store), fakeXZ(t, "echo xz-broke >&2\nexit 3\n"), parts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-dc failed")
	assert.Contains(t, err.Error(), "xz-broke")

	// a part that is not there is refused before xz starts
	_, err = demoReplay(ctx, fakeDemoClient(t, newFakeDemoRedis("")), fakeXZ(t, "cat\n"),
		[]string{filepath.Join(t.TempDir(), "gone.part-aa")})
	require.Error(t, err)
}

func TestDemoFillLoadsThisBuildsLibraryThenReplays(t *testing.T) {
	t.Parallel()
	source, err := fn.Source()
	require.NoError(t, err)
	store := newFakeDemoRedis(functionListReply(fn.Library, source))
	parts := writeParts(t, restoreLine("demo:k1", "one")+restoreLine("demo:k2", "two"))
	n, lib, err := demoFill(context.Background(), fakeDemoClient(t, store), fakeXZ(t, "cat\n"), parts)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, fn.Sum(source), lib)
	var flow []string
	for _, name := range store.names() {
		if name == "FUNCTION LOAD" || name == "FUNCTION LIST" || name == "RESTORE" {
			flow = append(flow, name)
		}
	}
	require.GreaterOrEqual(t, len(flow), 3)
	assert.Equal(t, []string{"FUNCTION LOAD", "FUNCTION LIST", "RESTORE"}, flow[:3],
		"the library was not loaded and checked before the replay")
	assert.Len(t, store.commands("RESTORE"), 2)
}

func TestDemoFillRefusesAStoreThatDoesNotHoldThisBuildsLibrary(t *testing.T) {
	t.Parallel()
	source, err := fn.Source()
	require.NoError(t, err)
	other := "#!lua name=" + fn.Library + "\n-- another build\n"
	for _, tc := range []struct{ name, reply string }{
		{"no library", "*0\r\n"},
		{"another build's library", functionListReply(fn.Library, other)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeDemoRedis(tc.reply)
			parts := writeParts(t, restoreLine("demo:k1", "one"))
			n, lib, err := demoFill(context.Background(), fakeDemoClient(t, store), fakeXZ(t, "cat\n"), parts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "does not hold this build's "+fn.Library+" library "+fn.Sum(source))
			assert.Zero(t, n)
			assert.Empty(t, lib)
			assert.Empty(t, store.commands("RESTORE"), "a dump was replayed under the wrong library")
		})
	}
}

func TestDemoFillRefusesWhenTheLibraryLoadFails(t *testing.T) {
	t.Parallel()
	store := newFakeDemoRedis("")
	store.loadRefused = true
	parts := writeParts(t, restoreLine("demo:k1", "one"))
	_, _, err := demoFill(context.Background(), fakeDemoClient(t, store), fakeXZ(t, "cat\n"), parts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load "+fn.Library+" function library")
	assert.Empty(t, store.commands("RESTORE"))
}

func TestDemoFillRefusesWhenTheStoreSizeIsNotTheReplayCount(t *testing.T) {
	t.Parallel()
	source, err := fn.Source()
	require.NoError(t, err)
	store := newFakeDemoRedis(functionListReply(fn.Library, source))
	store.dbSize = 99
	parts := writeParts(t, restoreLine("demo:k1", "one"))
	_, _, err = demoFill(context.Background(), fakeDemoClient(t, store), fakeXZ(t, "cat\n"), parts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the dump restored 1 keys and the demo's Redis holds 99")
}

func TestDemoUndoStopsAndRemovesAFailedLoad(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "redis-undo1")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "redis.log"), []byte("x"), 0o600))
	st := demoState{Addr: "127.0.0.1:6399", PID: 4242, Dir: dir}
	require.NoError(t, writeDemoState(root, st))
	stubDemo(t, root, nil, func(int) bool { return false }, func(demoState) error {
		t.Error("a pid that is gone was killed")
		return nil
	})
	assert.Equal(t, "; the demo's Redis was stopped and its directory removed", demoUndo(root, st))
	assert.NoDirExists(t, dir)
	assert.NoFileExists(t, filepath.Join(root, demoStateFile))
}

func TestDemoUndoReportsWhenNothingCanBeCleanedUp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	st := demoState{Addr: "127.0.0.1:6399", PID: 4242, Dir: outside}
	got := demoUndo(root, st)
	assert.True(t, strings.HasPrefix(got, "; the demo was not cleaned up ("), got)
	assert.Contains(t, got, "is not one demo load makes")
	assert.Contains(t, got, "); run: nova-sprint demo stop")
	assert.DirExists(t, outside)
}

func TestDemoLogTailIsTheLastThreeLinesOrNoLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	assert.Equal(t, "no log", demoLogTail(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "redis.log"), []byte("one\ntwo\nthree\nfour\nfive\n"), 0o600))
	assert.Equal(t, "three | four | five", demoLogTail(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "redis.log"), []byte("only\n"), 0o600))
	assert.Equal(t, "only", demoLogTail(dir))
}

func TestDemoProcessAliveNamesThisProcessAndNotAnImpossiblePid(t *testing.T) {
	t.Parallel()
	assert.True(t, demoProcessAlive(os.Getpid()))
	assert.False(t, demoProcessAlive(1<<30))
}

// hungConn is a connection whose Read answers readErr at once: the peer's
// hang-up (EOF) or a read timeout (silence), with no socket and no wait.
type hungConn struct {
	net.Conn
	readErr error
}

func (c hungConn) SetReadDeadline(time.Time) error { return nil }
func (c hungConn) Read([]byte) (int, error)        { return 0, c.readErr }

type hungTimeout struct{}

func (hungTimeout) Error() string   { return "read timed out in the test" }
func (hungTimeout) Timeout() bool   { return true }
func (hungTimeout) Temporary() bool { return true }

func TestDemoHungUpIsThePeerClosingAndNotSilence(t *testing.T) {
	t.Parallel()
	assert.True(t, demoHungUp(hungConn{readErr: io.EOF}, time.Second), "a closed connection was not a hang-up")
	assert.False(t, demoHungUp(hungConn{readErr: hungTimeout{}}, time.Second), "silence was taken for a hang-up")
}

func TestStartDemoRedisRefusesAProgramThatIsNotThere(t *testing.T) {
	t.Parallel()
	_, err := startDemoRedis(context.Background(), "nova-no-such-redis-server", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no nova-no-such-redis-server to run the demo")
}
