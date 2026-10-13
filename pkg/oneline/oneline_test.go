package oneline

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Escape: every control character, the two Unicode line separators and the bidi controls
// become visible escapes (\xNN below U+0080, \uNNNN above); an invalid byte is escaped by
// its own value; every other rune, non-ASCII and the other format characters included,
// passes through; a backslash is not escaped, so the escape is not injective.
func TestEscapeMakesOneLineAndPassesPrintableTextThrough(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"plain text", "plain text"},
		{"a\nb", `a\x0ab`},
		{"a\r\nb", `a\x0d\x0ab`},
		{"\t", `\x09`},
		{"\x00", `\x00`},
		{"\x1b[31mred", `\x1b[31mred`},
		{"\x7f", `\x7f`},
		{"\u0085", `\u0085`},
		{"\u2028", `\u2028`},
		{"\u2029", `\u2029`},
		{"\u202a\u202e", `\u202a\u202e`},
		{"\u2066\u2069", `\u2066\u2069`},
		{"\u202f", "\u202f"},
		{"\u200d", "\u200d"},
		{"\u00ad\ufeff", "\u00ad\ufeff"},
		{"caf\u00e9 \u65e5\u672c", "caf\u00e9 \u65e5\u672c"},
		{"\xff", `\xff`},
		{"a\xffb\xfe", `a\xffb\xfe`},
		{`back\slash`, `back\slash`},
		{"key=value two words", "key=value two words"},
	} {
		assert.Equal(t, tc.want, Escape(tc.in), "%q", tc.in)
	}
	assert.Equal(t, Escape("\n"), Escape(`\x0a`), "not injective: a stored newline and the four characters print the same")
	assert.Equal(t, Escape("a\nb"), Escape(Escape("a\nb")), "Escape changes nothing it already rendered")
}

// Field is Escape and then every whitespace character and every "=" as well, so a field's
// value is one whitespace-free token holding no "=": a key=value search can only match a
// field the tool wrote.
func TestFieldIsOneTokenHoldingNoEquals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"a b", `a\x20b`},
		{"a=b", `a\x3db`},
		{"a=b c", `a\x3db\x20c`},
		{"\t\n", `\x09\x0a`},
		{"\u00a0", `\u00a0`},
		{"\u3000", `\u3000`},
		{"\u2028", `\u2028`},
		{"\u202e", `\u202e`},
		{"\u0085", `\u0085`},
		{"\xff", `\xff`},
		{"caf\u00e9", "caf\u00e9"},
		{"x lockdown=clear quarantines=0", `x\x20lockdown\x3dclear\x20quarantines\x3d0`},
	} {
		got := Field(tc.in)
		assert.Equal(t, tc.want, got, "%q", tc.in)
		assert.Len(t, strings.Fields(got), min(len(got), 1), "one token")
		assert.NotContains(t, got, "=")
	}
}

// Quote is a Go string literal: one line, pasteable, and injective where Escape is not.
func TestQuoteIsOneLineAndInjective(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `"a b"`, Quote("a b"))
	assert.Equal(t, `""`, Quote(""))
	assert.Equal(t, `"a\nb"`, Quote("a\nb"))
	assert.Equal(t, `"\u2028"`, Quote("\u2028"))
	assert.Equal(t, `"say \"hi\""`, Quote(`say "hi"`))
	assert.NotEqual(t, Quote("\n"), Quote(`\n`), "injective: a backslash is escaped too")
	for _, s := range []string{"a\nb", "\u2028", "\u202e", "\xff"} {
		assert.NotContains(t, Quote(s), "\n")
	}
}

// Err renders an error's text through Escape; nil keeps fmt's own spelling.
func TestErrEscapesTheTextAndSpellsNilAsFmtDoes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "<nil>", Err(nil))
	assert.Equal(t, `open a\x0ab: no`, Err(errors.New("open a\nb: no")))
	assert.Equal(t, "plain", Err(errors.New("plain")))
}

// Cap cuts a tail to about n bytes and marks the cut with `...+<dropped>B`; it cuts on a
// rune boundary, never shortens something to nothing, keeps a first rune wider than the
// budget whole, returns a one-rune input unchanged, and a minimum-int ceiling does not
// overflow into no ceiling at all.
func TestCapCutsOnARuneBoundaryAndSaysHowMuchItDropped(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"", 0, ""},
		{"abc", 3, "abc"},
		{"abc", 100, "abc"},
		{"abcdefghij", 9, "ab...+8B"},
		{"abcdefghij", 8, "a...+9B"},
		{"abcdefghij", 5, "a...+9B"},
		{"abcdefghij", 0, "a...+9B"},
		{"abcdefghij", -1, "a...+9B"},
		{"h\u00e9llo world!", 9, "h...+12B"},
		{"h\u00e9llo world!", 10, "h\u00e9...+10B"},
		{"\u65e5\u672c\u8a9e", 7, "\u65e5...+6B"},
		{"\u65e5", 2, "\u65e5"},
		{"a", -5, "a"},
		{"ab", math.MinInt, "a...+1B"},
		{strings.Repeat("x", 1000), 100, strings.Repeat("x", 91) + "...+909B"},
	} {
		got := Cap(tc.in, tc.n)
		assert.Equal(t, tc.want, got, "Cap(%q, %d)", tc.in, tc.n)
		assert.True(t, got != "" || tc.in == "", "never nothing from something")
		if got != tc.in {
			kept := got[:strings.LastIndex(got, "...+")]
			assert.True(t, strings.HasPrefix(tc.in, kept), "the kept part is a prefix of the input")
			assert.Equal(t, len(tc.in)-len(kept), dropped(t, got), "the mark says what was dropped")
		}
	}
	assert.Equal(t, 500, TailBytes)
	long := strings.Repeat("y", TailBytes+1)
	assert.Equal(t, Cap(long, TailBytes), Escape(Cap(long, TailBytes)), "a capped value is one token through Escape and Field")
	assert.Equal(t, Cap(long, TailBytes), Field(Cap(long, TailBytes)))
}

// dropped reads the byte count off a cut tail's mark.
func dropped(t *testing.T, got string) int {
	t.Helper()
	m := got[strings.LastIndex(got, "...+")+len("...+"):]
	n, err := strconv.Atoi(strings.TrimSuffix(m, "B"))
	if err != nil || !strings.HasSuffix(m, "B") {
		t.Fatalf("no mark on %q", got)
	}
	return n
}

// HasRemedy is true for the house forms only: a marker word, a pointer to a tool's help, an
// imperative that names a flag, a backtick or a number, or see/retry/try with a flag. Bare
// "see", "retry", "want" and "help" are prose.
func TestHasRemedyKnowsTheHouseForms(t *testing.T) {
	t.Parallel()
	for _, yes := range []string{
		"refused: run: nova-x fix",
		"refused; run `nova-x fix`",
		"remedy: x", "remedy=x", "fix: x", "it wants a value", "see `nova-x`", "rerun after",
		"next: do y", "try nova-x -h", "see --help", "run nova-config apply", "the repair is x",
		"requires --force", "needs --as", "give one or the other", "drop one of them", "run this again", "run 'x'",
		"nova-bus help send", "see nova-sprint help take", "nova-x help",
		"pass --overwrite to replace it", "name a --draft-dir", "drop --open", "give 1 to 64", "use it instead",
		"set `x` first", "Pick -f", "install the 2 packages",
		"retry with --force", "try `x`", "see -h", "wants using --json",
	} {
		assert.True(t, HasRemedy(yes), "%q", yes)
	}
	for _, no := range []string{
		"", "see above", "no help available", "retry later", "want more", "help",
		"something went wrong", "set X=Y", "use the force", "runaway", "the flag",
		"nova-x helps", "nova-x helper",
	} {
		assert.False(t, HasRemedy(no), "%q", no)
	}
}

// WithRemedy appends "; run: <next>" to a line carrying no remedy and leaves one that
// carries its own as it is; either way it escapes once.
func TestWithRemedyAddsANextStepOnlyWhereNoneIs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "bad thing; run: nova-x fix", WithRemedy("bad thing", "nova-x fix"))
	assert.Equal(t, "bad thing; run: nova-y fix", WithRemedy("bad thing; run: nova-y fix", "nova-x fix"))
	assert.Equal(t, `bad\x0athing; run: nova-x fix`, WithRemedy("bad\nthing", "nova-x fix"))
	assert.Equal(t, `bad\x0athing; see --help`, WithRemedy("bad\nthing; see --help", "nova-x fix"))
	assert.Equal(t, `a\x0ab; run: x`, WithRemedy(Err(errors.New("a\nb")), "x"), "WithRemedy(Err(err), next) escapes once")
}

// ShellWord prints a value made only of shell-safe characters as it is, and single-quotes
// anything else and the empty value, an embedded single quote written '"'"'.
func TestShellWordQuotesWhatAShellWouldRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"a/b.c", "a/b.c"},
		{"-x", "-x"},
		{"--flag=v,w", "--flag=v,w"},
		{"user@host:1%", "user@host:1%"},
		{"", "''"},
		{"a b", "'a b'"},
		{"it's", `'it'"'"'s'`},
		{"$(x)", "'$(x)'"},
		{"a;b", "'a;b'"},
		{"a\nb", "'a\nb'"},
		{"*", "'*'"},
		{"caf\u00e9", "'caf\u00e9'"},
		{"\\", "'\\'"},
	} {
		assert.Equal(t, tc.want, ShellWord(tc.in), "%q", tc.in)
	}
}
