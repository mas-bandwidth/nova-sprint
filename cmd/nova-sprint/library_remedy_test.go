package main

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheLibraryRemedyNamesTheRedisFlag pins the remedy the library checks
// print: nova-redis fn load takes its address as --redis, never --addr, so a
// store that holds no library and one that holds another build's must both
// name `nova-redis fn load --redis <host:port>`. The client is the fake's
// net.Pipe dialer, as main_cover_test.go builds it: no socket, no server.
func TestTheLibraryRemedyNamesTheRedisFlag(t *testing.T) {
	t.Parallel()
	other := "#!lua name=" + fn.Library + "\nredis.register_function('ns_ping', function() return 'PONG' end)\n"
	ctx := context.Background()
	cases := []struct {
		name  string
		reply string
	}{
		{
			name:  "the store holds no library",
			reply: "*0\r\n",
		},
		{
			name:  "the store holds another build's library",
			reply: functionListReply(fn.Library, other),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore(map[string]string{"HELLO": helloAccepted, "FUNCTION": tc.reply})
			err := libraryMatches(ctx, fakeClient(t, store), "127.0.0.1:6379")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "nova-redis fn load --redis 127.0.0.1:6379", err.Error())
			assert.NotContains(t, err.Error(), "--addr", err.Error())
		})
	}
}
