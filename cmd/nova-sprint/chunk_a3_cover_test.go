package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDumpRedis speaks DUMP/PTTL over net.Pipe (no socket): payloads maps key->payload, ttls key->ms.
type fakeDumpRedis struct {
	payloads map[string]string
	ttls     map[string]int64
}

func (f *fakeDumpRedis) client(t *testing.T) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Dialer: func(ctx context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go f.serve(server)
		return client, nil
	}})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func (f *fakeDumpRedis) serve(c net.Conn) {
	defer c.Close()
	replies := make(chan string, 64)
	go func() {
		for r := range replies {
			if _, err := io.WriteString(c, r); err != nil {
				return
			}
		}
	}()
	rd := bufio.NewReader(c)
	for {
		words, err := readChunkA3Command(rd)
		if err != nil {
			close(replies)
			return
		}
		replies <- f.answer(words)
	}
}

func (f *fakeDumpRedis) answer(words []string) string {
	if len(words) == 0 {
		return "-ERR empty\r\n"
	}
	switch strings.ToUpper(words[0]) {
	case "HELLO":
		return helloAccepted
	case "DUMP":
		if v, ok := f.payloads[words[1]]; ok {
			return fmt.Sprintf("$%d\r\n%s\r\n", len(v), v)
		}
		return "$-1\r\n"
	case "PTTL":
		if ms, ok := f.ttls[words[1]]; ok {
			return fmt.Sprintf(":%d\r\n", ms)
		}
		return ":-1\r\n"
	default:
		return "+OK\r\n"
	}
}

func readChunkA3Command(rd *bufio.Reader) ([]string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	if err != nil {
		return nil, err
	}
	words := make([]string, 0, n)
	for range n {
		head, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(head, "$")))
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

// Entry 10 (backup_out.go): dumpKeys + compactStrings.
func TestChunkA3DumpKeysReturnsPayloadAndTTLAndSkipsGoneKeys(t *testing.T) {
	t.Parallel()
	c := (&fakeDumpRedis{payloads: map[string]string{"k1": "payload1", "k2": "payload2"}, ttls: map[string]int64{"k1": 90000}}).client(t)
	keys, err := dumpKeys(context.Background(), c, []string{"k1", "gone", "k2"})
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, "k1", keys[0].Key)
	assert.Equal(t, []byte("payload1"), keys[0].Payload)
	assert.Greater(t, keys[0].TTL, int64(0))
	assert.Equal(t, "k2", keys[1].Key)
	assert.Equal(t, int64(0), keys[1].TTL)
}

func TestChunkA3DumpKeysEmptyNames(t *testing.T) {
	t.Parallel()
	c := (&fakeDumpRedis{payloads: map[string]string{}, ttls: map[string]int64{}}).client(t)
	keys, err := dumpKeys(context.Background(), c, nil)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestChunkA3CompactStringsDedupesSortedInputOnly(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"a", "b", "c"}, compactStrings([]string{"a", "a", "b", "b", "b", "c"}))
	assert.Equal(t, []string{"a", "b", "a"}, compactStrings([]string{"a", "b", "a"}), "non-consecutive duplicates are kept")
	assert.Empty(t, compactStrings(nil))
	assert.Equal(t, []string{"x"}, compactStrings([]string{"x", "x"}))
}

// Entry 12 (dashboard.go): dashboardUpstream + dashboardAddrs flag parsing.
func TestChunkA3DashboardUpstreamNormalizesAndRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"127.0.0.1:7395", ""},
		{"none", ""},
		{"http://127.0.0.1:7390", "http://127.0.0.1:7390/api/sprint"},
		{"http://127.0.0.1:7390/", "http://127.0.0.1:7390/api/sprint"},
		{"https://dash.example/api/sprint", "https://dash.example/api/sprint"},
		{"https://dash.example/api/sprint/", "https://dash.example/api/sprint"},
	} {
		got, err := dashboardUpstream(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}
	for _, bad := range []string{
		"ftp://127.0.0.1:7390", "http://user:pass@127.0.0.1:7390",
		"http://127.0.0.1:7390?x=1", "http://127.0.0.1:7390#frag", "http://",
	} {
		_, err := dashboardUpstream(bad)
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "--pull")
	}
}

func TestChunkA3DashboardAddrsValidatesAndDedupes(t *testing.T) {
	t.Parallel()
	got, err := dashboardAddrs("--listen", "127.0.0.1:7390, 127.0.0.1:7390, localhost:7391")
	require.NoError(t, err)
	assert.Equal(t, []string{"127.0.0.1:7390", "localhost:7391"}, got)

	got, err = dashboardAddrs("--listen", "none")
	require.NoError(t, err)
	assert.Nil(t, got)

	for _, tc := range []struct{ in, want string }{
		{"", "names no address"},
		{"127.0.0.1", "wants address:port"},
		{"0.0.0.0:7390", "does not listen on every network"},
		{"1.1.1.1:7390", "a public address"},
		{"169.254.1.1:7390", "a link-local address"},
		{"example.com:7390", "is an IP address"},
	} {
		_, err := dashboardAddrs("--listen", tc.in)
		require.Error(t, err, tc.in)
		assert.Contains(t, err.Error(), tc.want, tc.in)
	}
	// Private and tailnet ranges are accepted.
	for _, ok := range []string{"10.0.0.5:7390", "192.168.1.5:7390", "100.64.1.5:7390"} {
		_, err := dashboardAddrs("--pull", ok)
		assert.NoError(t, err, ok)
	}
}

// Entry 18 (land.go): returnCard dry/nil-store path + baseGone empty-base guard.
func TestChunkA3ReturnCardDryRunRecordsRefusedBatchWithoutStoreWrite(t *testing.T) {
	t.Parallel()
	l := &lander{dry: true}
	f := conflictCard{landCard: landCard{id: "s1-1.w1", head: "abc123", attempt: "1", repo: "mas-bandwidth/nova-sprint", base: "main"}, why: "empty commit"}
	assert.False(t, l.returnCard("s1", f), "a dry run never records a return")
	require.Len(t, l.out, 1)
	b := l.out[0]
	assert.Equal(t, "s1", b.Stream)
	assert.Equal(t, "refused", b.Status)
	assert.Equal(t, 1, b.Cards)
	assert.Equal(t, []string{"s1-1.w1"}, b.IDs)
	assert.Equal(t, "mas-bandwidth/nova-sprint", b.Repo)
	assert.Equal(t, "main", b.Base)
	assert.Equal(t, "empty commit", b.Reason)
	assert.True(t, b.DryRun)
}

func TestChunkA3ReturnCardNilStoreRecordsRefusedBatch(t *testing.T) {
	t.Parallel()
	l := &lander{} // st is nil: same no-write path as dry
	f := conflictCard{landCard: landCard{id: "s2-3.w2", repo: "r", base: "dev"}, why: "no diff"}
	assert.False(t, l.returnCard("s2", f))
	require.Len(t, l.out, 1)
	assert.Equal(t, []string{"s2-3.w2"}, l.out[0].IDs)
	assert.Equal(t, "no diff", l.out[0].Reason)
}

func TestChunkA3BaseGoneEmptyBaseIsFalseWithoutGit(t *testing.T) {
	t.Parallel()
	l := &lander{}
	assert.False(t, l.baseGone(context.Background(), t.TempDir(), ""))
}
