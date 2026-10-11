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
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
)

// fakeStore answers a Redis client for the unit tier: it speaks the wire
// protocol over net.Pipe, so a test reaches libraryMatches with no socket,
// no subprocess, no Redis and no Postgres. Each command is answered by the
// first word of its name from scripts, and a command with no script gets a
// bulk string "OK".
type fakeStore struct {
	mu      sync.Mutex
	scripts map[string]string
}

func newFakeStore(scripts map[string]string) *fakeStore {
	return &fakeStore{scripts: scripts}
}

func (s *fakeStore) reply(word string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.scripts[word]; ok {
		return r
	}
	return "$2\r\nOK\r\n"
}

// Dial is redis.Options.Dialer: one net.Pipe per dial, no socket.
func (s *fakeStore) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	client, server := net.Pipe()
	go s.serve(server)
	return client, nil
}

func (s *fakeStore) serve(c net.Conn) {
	defer c.Close()
	rd := bufio.NewReader(c)
	for {
		word, err := readFakeCommand(rd)
		if err != nil {
			return
		}
		if _, err := io.WriteString(c, s.reply(word)); err != nil {
			return
		}
	}
}

// readFakeCommand reads one client command as an array of bulk strings and
// answers its name, uppercased from the first word.
func readFakeCommand(rd *bufio.Reader) (string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "*"), "\r\n"))
	if err != nil {
		return "", err
	}
	first := ""
	for i := range n {
		head, err := rd.ReadString('\n')
		if err != nil {
			return "", err
		}
		size, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(head, "$"), "\r\n"))
		if err != nil {
			return "", err
		}
		body := make([]byte, size+2)
		if _, err := io.ReadFull(rd, body); err != nil {
			return "", err
		}
		if i == 0 {
			first = strings.ToUpper(string(body[:size]))
		}
	}
	return first, nil
}

// helloAccepted is the RESP3 map a store sends for HELLO 3.
const helloAccepted = "%7\r\n$6\r\nserver\r\n$5\r\nredis\r\n$7\r\nversion\r\n$5\r\n8.0.0\r\n$5\r\nproto\r\n:3\r\n$2\r\nid\r\n:7\r\n$4\r\nmode\r\n$10\r\nstandalone\r\n$4\r\nrole\r\n$6\r\nmaster\r\n$7\r\nmodules\r\n*0\r\n"

// functionListReply is a RESP3 FUNCTION LIST reply: an array of one library
// map naming library, holding code.
func functionListReply(library, code string) string {
	return fmt.Sprintf("*1\r\n%%2\r\n$12\r\nlibrary_name\r\n$%d\r\n%s\r\n$12\r\nlibrary_code\r\n$%d\r\n%s\r\n",
		len(library), library, len(code), code)
}

// TestMainCoverOpenConnRefusesAStoreThatIsNotThereAndResolvesTheLogin
// covers openConn (cmd/nova-sprint/main.go:229, the finding's 0.0%). Its main
// path opens a store; the unit tier owns no socket to open, and the
// package's Open exposes no dialer to inject, so the path exercised here is
// the one a store that is not there takes: the login is resolved from the
// environment (both branches: a user with a named password variable, and no
// user) and Open is called, its refusal returned unchanged and nothing left
// open. The successful open needs a live store or a redisconn dial seam and
// is named in the report as not-done.
func TestMainCoverOpenConnRefusesAStoreThatIsNotThereAndResolvesTheLogin(t *testing.T) {
	t.Parallel()
	// a port no store listens on: the dial is refused at once, no process,
	// no bind and nothing started
	const absent = "127.0.0.1:1"
	t.Run("a user with a named password variable is resolved, then the dial is refused", func(t *testing.T) {
		env := map[string]string{
			"NOVA_SPRINT_REDIS_USER":         "sprint-user",
			"NOVA_SPRINT_REDIS_PASSWORD_ENV": "SPRINT_PW",
			"SPRINT_PW":                      "s3cr3t",
		}
		a := &app{getenv: func(k string) string { return env[k] }}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		conn, err := a.openConn(ctx, absent)
		require.Error(t, err)
		assert.Nil(t, conn)
		assert.Contains(t, err.Error(), absent, err.Error())
	})
	t.Run("no user and no password variable: the default login, then the dial is refused", func(t *testing.T) {
		a := &app{getenv: func(string) string { return "" }}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		conn, err := a.openConn(ctx, absent)
		require.Error(t, err)
		assert.Nil(t, conn)
		assert.Contains(t, err.Error(), absent, err.Error())
	})
}

// TestMainCoverLibraryMatchesAcceptsTheEmbeddedLibraryAndRefusesTheRest
// covers libraryMatches (cmd/nova-sprint/main.go:245, the finding's other
// 0.0%): the main path accepts a store holding this build's embedded
// library, and two refusals -- a store holding none, and one holding another
// build's. The client is built over the fake's net.Pipe dialer, and the
// embedded source is fn.Source, the same function the code under test reads.
func TestMainCoverLibraryMatchesAcceptsTheEmbeddedLibraryAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	source, err := fn.Source()
	require.NoError(t, err)
	other := "#!lua name=" + fn.Library + "\nredis.register_function('ns_ping', function() return 'PONG' end)\n"
	ctx := context.Background()
	cases := []struct {
		name    string
		reply   string
		refused bool
		holds   []string
	}{
		{
			name:  "the store holds this build's library: accepted",
			reply: functionListReply(fn.Library, source),
		},
		{
			name:    "the store holds none: refused, naming the load",
			reply:   "*0\r\n",
			refused: true,
			holds:   []string{"holds no " + fn.Library + " function library", "nova-redis fn load --addr 127.0.0.1:6379"},
		},
		{
			name:    "the store holds another build's: refused, naming both sums",
			reply:   functionListReply(fn.Library, other),
			refused: true,
			holds:   []string{"holds " + fn.Library + " library " + fn.Sum(other), "this build is " + fn.Sum(source)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore(map[string]string{"HELLO": helloAccepted, "FUNCTION": tc.reply})
			err := libraryMatches(ctx, fakeClient(t, store), "127.0.0.1:6379")
			if !tc.refused {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tc.holds {
				assert.Contains(t, err.Error(), want, err.Error())
			}
		})
	}
}

// fakeClient is a go-redis client over the fake's net.Pipe dialer, closed at
// the test's end.
func fakeClient(t *testing.T, store *fakeStore) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Dialer: store.dial})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestLibraryMatchesAcceptsACommentOnlyChange pins what libraryMatches
// (cmd/nova-sprint/main.go), the check every verb runs when it opens the store,
// judges a library by: its code. A store whose library differs from this
// build's only in comments or blank space (v1.2.3's adopt refused the store
// over six comment lines, PR #33) matches; one whose code differs is refused,
// naming both sums as nova-redis fn load and fn check print them.
func TestLibraryMatchesAcceptsACommentOnlyChange(t *testing.T) {
	t.Parallel()
	source, err := fn.Source()
	require.NoError(t, err)
	header := "-- lua/00_ping.lua\n"
	require.Contains(t, source, header)
	commentOnly := []struct{ name, store string }{
		{"a comment line edited in place", strings.Replace(source, header, "-- lua/00_ping.lua (a comment-only edit)\n", 1)},
		{"a comment line added", strings.Replace(source, header, header+"-- a comment line added\n", 1)},
		{"a comment line removed", strings.Replace(source, header, "", 1)},
		{"a long comment added", strings.Replace(source, header, header+"--[==[ a long\ncomment, ]] and on ]==]\n", 1)},
		{"blank lines and trailing space", strings.Replace(source, header, "\n\n"+strings.TrimSuffix(header, "\n")+"   \n\n", 1)},
	}
	for _, tc := range commentOnly {
		require.NotEqual(t, source, tc.store, tc.name)
		store := newFakeStore(map[string]string{"HELLO": helloAccepted, "FUNCTION": functionListReply(fn.Library, tc.store)})
		require.NoError(t, libraryMatches(context.Background(), fakeClient(t, store), "127.0.0.1:6379"), "%s is not a change of the library", tc.name)
	}

	codeChanged := source + "\nredis.register_function('ns_probe', function() return 'x' end)\n"
	store := newFakeStore(map[string]string{"HELLO": helloAccepted, "FUNCTION": functionListReply(fn.Library, codeChanged)})
	err = libraryMatches(context.Background(), fakeClient(t, store), "127.0.0.1:6379")
	require.Error(t, err, "a change of code is a change of the library: refused")
	require.Contains(t, err.Error(), "holds "+fn.Library+" library "+fn.Sum(codeChanged))
	require.Contains(t, err.Error(), "this build is "+fn.Sum(source))
}

// TestLibrarySumIsTheCodeAlone pins librarySum and luaCode (cmd/nova-sprint/
// main.go): comments of either form and blank space outside strings are not
// code, a string's bytes (quoted or long, with a "--" inside) are, tokens stay
// apart, and a source that never closes a string or comment still has a sum.
func TestLibrarySumIsTheCodeAlone(t *testing.T) {
	t.Parallel()
	same := []struct{ name, a, b string }{
		{"blank lines and indentation", "local a = 1\n\n  return a  \n", "local a = 1\nreturn a"},
		{"a line comment", "local a = 1 -- one\nreturn a", "local a = 1\nreturn a"},
		{"a whole comment line", "local a = 1\n-- the answer\nreturn a", "local a = 1\nreturn a"},
		{"a long comment", "local a = 1 --[[ one\ntwo ]] return a", "local a = 1 return a"},
		{"a long comment of level 2 holding ]]", "a --[==[ x ]] y ]==] b", "a b"},
		{"a comment with no newline after it", "return a -- the end", "return a"},
	}
	for _, tc := range same {
		require.Equal(t, librarySum(tc.b), librarySum(tc.a), tc.name)
	}
	differ := []struct{ name, a, b string }{
		{"a token changed", "return a", "return b"},
		{"tokens a comment kept apart", "a --c\nb", "ab"},
		{"a -- inside a quoted string", `x = "a -- b"`, `x = "a -- c"`},
		{"a -- after an escaped quote", `x = "a \" -- b"`, `x = "a \" -- c"`},
		{"a -- inside a long string", "x = [==[ -- a ]==]", "x = [==[ -- b ]==]"},
		{"blank space inside a string", `x = 'a  b'`, `x = 'a b'`},
		{"a newline inside a long string", "x = [[a\nb]]", "x = [[a b]]"},
	}
	for _, tc := range differ {
		require.NotEqual(t, librarySum(tc.b), librarySum(tc.a), tc.name)
	}
	for _, src := range []string{`x = "abc\`, `x = 'abc`, "x = [==[ abc ]]", "x --[[ abc", `\`} {
		require.NotPanics(t, func() { _ = librarySum(src) }, "an unclosed source %q", src)
	}
}
