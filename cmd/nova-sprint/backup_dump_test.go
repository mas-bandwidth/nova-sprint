package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dumpFake answers DUMP and PTTL per key over net.Pipe (no socket, no Redis,
// no subprocess), the fakeStore pattern of main_cover_test.go widened to keyed
// replies: a key absent from payloads answers DUMP with the nil bulk string,
// as a key gone between the SCAN and its DUMP does. Replies are written by
// their own goroutine so a pipeline's writes never wait on its replies.
type dumpFake struct {
	mu       sync.Mutex
	payloads map[string]string
	ttls     map[string]int64
	dumpErr  string // when set, every DUMP is refused with this error line
	cmds     []string
}

func newDumpFake(payloads map[string]string, ttls map[string]int64) *dumpFake {
	return &dumpFake{payloads: payloads, ttls: ttls}
}

func (s *dumpFake) dial(_ context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	go s.serve(server)
	return client, nil
}

func (s *dumpFake) serve(c net.Conn) {
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
		words, err := readDumpCommand(rd)
		if err != nil {
			close(replies)
			return
		}
		replies <- s.answer(words)
	}
}

func (s *dumpFake) answer(words []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.ToUpper(words[0])
	s.cmds = append(s.cmds, name)
	switch {
	case name == "HELLO":
		return helloAccepted
	case name == "DUMP" && len(words) > 1:
		if s.dumpErr != "" {
			return "-ERR " + s.dumpErr + "\r\n"
		}
		if payload, ok := s.payloads[words[1]]; ok {
			return fmt.Sprintf("$%d\r\n%s\r\n", len(payload), payload)
		}
		return "$-1\r\n"
	case name == "PTTL" && len(words) > 1:
		return fmt.Sprintf(":%d\r\n", s.ttls[words[1]])
	default:
		return "+OK\r\n"
	}
}

func (s *dumpFake) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.cmds {
		if c == name {
			n++
		}
	}
	return n
}

// readDumpCommand reads one RESP array of bulk strings whole.
func readDumpCommand(rd *bufio.Reader) ([]string, error) {
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

func dumpClient(t *testing.T, store *dumpFake) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Dialer: store.dial, MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// dumpKeys (backup_out.go, audit entry 10) is DUMP and PTTL of each key: the
// payload and the positive TTL in milliseconds, in the names' order, a key
// with no expiry (PTTL -1) holding TTL 0.
func TestDumpKeysReturnsPayloadAndTTLPerKeyInOrder(t *testing.T) {
	t.Parallel()
	store := newDumpFake(
		map[string]string{"sprint:a": "payload-a", "sprint:b": "pay\x00load-b", "sprint:c": "payload-c"},
		map[string]int64{"sprint:a": 90_000, "sprint:b": -1, "sprint:c": 1_500},
	)
	keys, err := dumpKeys(context.Background(), dumpClient(t, store), []string{"sprint:a", "sprint:b", "sprint:c"})
	require.NoError(t, err)
	require.Len(t, keys, 3)
	assert.Equal(t, "sprint:a", keys[0].Key)
	assert.Equal(t, []byte("payload-a"), keys[0].Payload)
	assert.Equal(t, int64(90_000), keys[0].TTL)
	assert.Equal(t, "sprint:b", keys[1].Key)
	assert.Equal(t, []byte("pay\x00load-b"), keys[1].Payload)
	assert.Equal(t, int64(0), keys[1].TTL, "a key with no expiry holds TTL 0")
	assert.Equal(t, "sprint:c", keys[2].Key)
	assert.Equal(t, int64(1_500), keys[2].TTL)
	assert.Equal(t, 3, store.count("DUMP"))
	assert.Equal(t, 3, store.count("PTTL"))
}

// A key gone between the SCAN and its DUMP is left out, and the pipeline's
// redis.Nil for it does not fail the dump of the keys that remain.
func TestDumpKeysSkipsAKeyGoneBetweenScanAndDump(t *testing.T) {
	t.Parallel()
	store := newDumpFake(
		map[string]string{"sprint:a": "payload-a", "sprint:c": "payload-c"},
		map[string]int64{"sprint:a": 5_000, "sprint:c": 7_000},
	)
	keys, err := dumpKeys(context.Background(), dumpClient(t, store), []string{"sprint:a", "sprint:gone", "sprint:c"})
	require.NoError(t, err)
	require.Len(t, keys, 2, "the gone key is left out: %v", keys)
	assert.Equal(t, "sprint:a", keys[0].Key)
	assert.Equal(t, "sprint:c", keys[1].Key)
}

// A store refusal that is not a missing key is the caller's error, and no
// keys are handed back with it.
func TestDumpKeysReturnsTheStoresRefusal(t *testing.T) {
	t.Parallel()
	store := newDumpFake(map[string]string{"sprint:a": "payload-a"}, map[string]int64{"sprint:a": 5_000})
	store.dumpErr = "dump refused in the test"
	keys, err := dumpKeys(context.Background(), dumpClient(t, store), []string{"sprint:a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dump refused in the test")
	assert.Empty(t, keys)
}

// Over one pipeline batch of 500, every key still comes back in order: the
// second batch is neither dropped nor merged into the first.
func TestDumpKeysCrossesThePipelineBatch(t *testing.T) {
	t.Parallel()
	payloads := make(map[string]string, 501)
	ttls := make(map[string]int64, 501)
	names := make([]string, 0, 501)
	for i := range 501 {
		k := fmt.Sprintf("sprint:k%03d", i)
		names = append(names, k)
		payloads[k] = fmt.Sprintf("payload-%d", i)
		ttls[k] = int64(i)
	}
	store := newDumpFake(payloads, ttls)
	keys, err := dumpKeys(context.Background(), dumpClient(t, store), names)
	require.NoError(t, err)
	require.Len(t, keys, 501)
	assert.Equal(t, "sprint:k000", keys[0].Key)
	assert.Equal(t, "sprint:k500", keys[500].Key)
	assert.Equal(t, []byte("payload-500"), keys[500].Payload)
	assert.Equal(t, 501, store.count("DUMP"))
	assert.Equal(t, 501, store.count("PTTL"))
}

// compactStrings (backup_out.go, audit entry 10) drops the repeats of the
// sorted names a SCAN hands Take, keeping the first of each run and the
// order; an empty scan stays empty.
func TestCompactStringsDropsRepeatsOfSortedNames(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		[]string{"sprint:a", "sprint:b", "sprint:c"},
		compactStrings([]string{"sprint:a", "sprint:a", "sprint:b", "sprint:c", "sprint:c", "sprint:c"}))
	assert.Equal(t, []string{"sprint:a"}, compactStrings([]string{"sprint:a"}))
	assert.Empty(t, compactStrings(nil))
	assert.Empty(t, compactStrings([]string{}))
}
