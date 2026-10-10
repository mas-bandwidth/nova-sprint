package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
)

// TestAdoptLibraryDigestCoversCodeNotComments pins the digest the adopt's
// shadow tick (libraryMatches, cmd/nova-sprint/main.go) judges the store's
// function library by: it covers code bytes, not comment bytes, so a build
// that only changed a comment still matches and the adopt's server step is
// not refused on it, while a change of code is a change of the library.
func TestAdoptLibraryDigestCoversCodeNotComments(t *testing.T) {
	t.Parallel()
	source, err := fn.Source()
	require.NoError(t, err)

	// a comment-only change does not change the digest
	commentOnly := strings.Replace(source, "-- lua/00_ping.lua", "-- lua/00_ping.lua (a comment-only edit)", 1)
	require.Equal(t, librarySum(source), librarySum(commentOnly), "a comment-only change must not change the digest")

	// a change of code does
	codeChanged := source + "\nredis.register_function('ns_probe', function() return 'x' end)\n"
	require.NotEqual(t, librarySum(source), librarySum(codeChanged), "a change of code is a change of the library")

	// the shadow tick accepts a store whose library differs only in a comment
	store := newFakeStore(map[string]string{"HELLO": helloAccepted, "FUNCTION": functionListReply(fn.Library, commentOnly)})
	require.NoError(t, libraryMatches(context.Background(), fakeClient(t, store), "127.0.0.1:6379"),
		"a comment-only change must not refuse the shadow tick")

	// and refuses one whose code differs
	store = newFakeStore(map[string]string{"HELLO": helloAccepted, "FUNCTION": functionListReply(fn.Library, codeChanged)})
	require.Error(t, libraryMatches(context.Background(), fakeClient(t, store), "127.0.0.1:6379"),
		"a change of code is still refused")
}
