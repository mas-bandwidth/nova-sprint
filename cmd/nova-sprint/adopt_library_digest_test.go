package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
)

// TestLibraryMatchesAcceptsACommentOnlyChange pins what libraryMatches
// (cmd/nova-sprint/main.go), the check every verb and the adopt's shadow tick
// run when they open the store, judges a library by: its code. A store whose
// library differs from this build's only in comments or blank space (v1.2.3's
// adopt refused the store over six comment lines, PR #33) matches; one whose
// code differs is refused, naming both sums as nova-redis fn load and fn check
// print them.
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
