package cardhdr

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// KeyValue reads one `KEY: value` line by the header's own rule: a key word at column 0
// (a letter, then letters, digits or dashes), a colon, then the value with its blanks and
// any trailing CR trimmed. A line indented, with no colon, or whose key starts with a digit
// is no header line.
func TestKeyValueReadsAHeaderLineAtColumnZeroOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line, key, value string
		ok               bool
	}{
		{"KIND: fix", "KIND", "fix", true},
		{"KIND:fix", "KIND", "fix", true},
		{"KIND:   fix  ", "KIND", "fix", true},
		{"KIND: fix\r", "KIND", "fix", true},
		{"KIND:", "KIND", "", true},
		{"kind-2: x y", "kind-2", "x y", true},
		{"K: a: b", "K", "a: b", true},
		{" KIND: fix", "", "", false},
		{"KIND fix", "", "", false},
		{"2KIND: x", "", "", false},
		{"-KIND: x", "", "", false},
		{"", "", "", false},
		{"## Body", "", "", false},
	} {
		k, v, ok := KeyValue(tc.line)
		assert.Equal(t, tc.ok, ok, "%q", tc.line)
		assert.Equal(t, tc.key, k, "%q", tc.line)
		assert.Equal(t, tc.value, v, "%q", tc.line)
	}
}

// Value is the first line carrying the key as written, case and all; a later line with
// the same key never overrides it, and a key in another case is another key.
func TestValueIsTheFirstLineWithTheKeyAsWritten(t *testing.T) {
	t.Parallel()
	text := "card line 1\nREPO: a/b\nrepo: c/d\nREPO: e/f\n  BASE: x\n"
	v, ok := Value(text, "REPO")
	require.True(t, ok)
	assert.Equal(t, "a/b", v)
	v, ok = Value(text, "repo")
	require.True(t, ok)
	assert.Equal(t, "c/d", v)
	_, ok = Value(text, "BASE")
	assert.False(t, ok, "an indented line is no header line")
	_, ok = Value(text, "KIND")
	assert.False(t, ok)
	_, ok = Value("", "REPO")
	assert.False(t, ok)
}

// ParseBase reads `<ref>` or `<ref>@<sha40>`: an empty ref, or anything after the @ that is
// no full lowercase sha, is refused, so a pin is never read as its ref.
func TestParseBaseRefusesAnEmptyRefAndAPinThatIsNoFullSha(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("0123456789abcdef", 2) + "01234567"
	require.Len(t, sha, 40)
	for _, tc := range []struct {
		in, ref, sha string
		ok           bool
	}{
		{"sprint/x", "sprint/x", "", true},
		{"  main  ", "main", "", true},
		{"sprint/x@" + sha, "sprint/x", sha, true},
		{"v1.2.3", "v1.2.3", "", true},
		{"", "", "", false},
		{"   ", "", "", false},
		{"@" + sha, "", "", false},
		{"main@", "", "", false},
		{"main@abc1234", "", "", false},
		{"main@" + strings.ToUpper(sha), "", "", false},
		{"main@" + sha + "0", "", "", false},
		{"main@" + sha[:39], "", "", false},
	} {
		ref, got, ok := ParseBase(tc.in)
		assert.Equal(t, tc.ok, ok, "%q", tc.in)
		assert.Equal(t, tc.ref, ref, "%q", tc.in)
		assert.Equal(t, tc.sha, got, "%q", tc.in)
	}
}

// IsRoute is exactly the four model classes, in their own case; Routes lists them in rank
// order and RouteList names them as a refusal does.
func TestIsRouteIsTheFourClassesOnly(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"frontier", "heavy", "pro", "flash"}, Routes)
	assert.Equal(t, "frontier, heavy, pro or flash", RouteList)
	for _, r := range Routes {
		assert.True(t, IsRoute(r), r)
	}
	for _, s := range []string{"", "Flash", "FLASH", "heavy ", " pro", "astra", "-"} {
		assert.False(t, IsRoute(s), "%q", s)
	}
}

// IsModelID is `<provider>/<model>`: a provider word with no slash, a model that may hold
// slashes, neither empty, no blank or tab anywhere.
func TestIsModelIDWantsProviderSlashModelWithNoBlank(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"openai/gpt-5", "a/b/c", "x/y.z:1"} {
		assert.True(t, IsModelID(ok), ok)
	}
	for _, bad := range []string{"", "gpt-5", "/gpt-5", "openai/", "/", "open ai/gpt", "openai/gpt 5", "openai/gpt\t5"} {
		assert.False(t, IsModelID(bad), "%q", bad)
	}
}

// ParseTest accepts `[-tags <tags>] <package> <TestName>` and `none <why>`, and refuses
// everything else with the remedy: an empty value, a bare none, a package pattern, a
// package with .., a tag list that is not one, and a name that is no Go test name.
func TestParseTestAcceptsOnePackageAndOneTestOrNoneWithAWhy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want TestLine
	}{
		{"./pkg/x TestFoo", TestLine{Package: "./pkg/x", Name: "TestFoo"}},
		{"pkg/x TestFoo", TestLine{Package: "pkg/x", Name: "TestFoo"}},
		{". TestFoo", TestLine{Package: ".", Name: "TestFoo"}},
		{"pkg/x/ TestFoo", TestLine{Package: "pkg/x/", Name: "TestFoo"}},
		{"  pkg/x   ExampleFoo  ", TestLine{Package: "pkg/x", Name: "ExampleFoo"}},
		{"pkg/x FuzzFoo_1", TestLine{Package: "pkg/x", Name: "FuzzFoo_1"}},
		{"pkg/x Test", TestLine{Package: "pkg/x", Name: "Test"}},
		{"-tags functional pkg/x TestFoo", TestLine{Package: "pkg/x", Name: "TestFoo", Tags: "functional"}},
		{"-tags=functional,slow pkg/x TestFoo", TestLine{Package: "pkg/x", Name: "TestFoo", Tags: "functional,slow"}},
		{"none the change is documentation only", TestLine{None: true, Why: "the change is documentation only"}},
		{"none: docs only", TestLine{None: true, Why: "docs only"}},
		{"none - docs only", TestLine{None: true, Why: "docs only"}},
		{"none (docs only)", TestLine{None: true, Why: "docs only)"}},
	} {
		got, why := ParseTest(tc.in)
		assert.Empty(t, why, "%q", tc.in)
		assert.Equal(t, tc.want, got, "%q", tc.in)
	}
}

func TestParseTestRefusalsNameTheRemedy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"", "DONE-WHEN cannot be turned into a test (no TEST line): " + TestRemedy},
		{"   ", "DONE-WHEN cannot be turned into a test (no TEST line): " + TestRemedy},
		{"none", "TEST: none says no why: write TEST: none <why the change has no test>, or TEST: <package> <TestName>"},
		{"none:", "TEST: none says no why"},
		{"none-why", "is not `[-tags <tags>] <package> <TestName>` or `none <why>`"},
		{"./... TestFoo", "TEST ./... TestFoo names a package pattern (./...), not one package"},
		{"pkg/... TestFoo", "names a package pattern (pkg/...)"},
		{"../x TestFoo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"pkg/x/../y TestFoo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"pkg/x testFoo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"pkg/x Test-Foo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"pkg/x TestFoo extra", "is not `[-tags <tags>] <package> <TestName>`"},
		{"TestFoo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"-tags pkg/x TestFoo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"-tags= pkg/x TestFoo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"-x pkg/x TestFoo", "is not `[-tags <tags>] <package> <TestName>`"},
		{"-tags functional", "is not `[-tags <tags>] <package> <TestName>`"},
	} {
		got, why := ParseTest(tc.in)
		assert.Equal(t, TestLine{}, got, "%q", tc.in)
		require.NotEmpty(t, why, "%q", tc.in)
		assert.Contains(t, why, tc.want, "%q", tc.in)
		assert.Contains(t, why, "TEST: none <why", "%q: every refusal carries the remedy", tc.in)
	}
}

// ReadClass: a brief with no CLASS line is a model card; CLASS: script needs a SCRIPT; a
// SCRIPT on a model card is refused; the first CLASS, SCRIPT and DEADLINE lines win; lines
// after the first STEP and inside a fenced block are never read; keys read in any case.
func TestReadClassReadsTheHeaderBeforeTheFirstStepOutsideFences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, brief string
		want        Class
		why         string
	}{
		{"no class line", "card\nKIND: fix\n", Class{Name: ClassModel}, ""},
		{"model", "card\nCLASS: model\n", Class{Name: ClassModel}, ""},
		{"script with deadline in minutes", "card\nCLASS: Script\nSCRIPT: go run ./x\nDEADLINE: finish within 5 minutes\n", Class{Name: ClassScript, Script: "go run ./x", Deadline: 5 * time.Minute}, ""},
		{"deadline seconds", "card\nCLASS: script\nSCRIPT: ./x\nDEADLINE: 30s\n", Class{Name: ClassScript, Script: "./x", Deadline: 30 * time.Second}, ""},
		{"deadline hours", "card\nCLASS: script\nSCRIPT: ./x\nDEADLINE: 2 hours\n", Class{Name: ClassScript, Script: "./x", Deadline: 2 * time.Hour}, ""},
		{"deadline m is minutes", "card\nCLASS: script\nSCRIPT: ./x\nDEADLINE: 5m\n", Class{Name: ClassScript, Script: "./x", Deadline: 5 * time.Minute}, ""},
		{"deadline secs", "card\nCLASS: script\nSCRIPT: ./x\nDEADLINE: 90 secs\n", Class{Name: ClassScript, Script: "./x", Deadline: 90 * time.Second}, ""},
		{"deadline unreadable is none", "card\nCLASS: script\nSCRIPT: ./x\nDEADLINE: soon\n", Class{Name: ClassScript, Script: "./x"}, ""},
		{"keys in any case", "card\nclass: script\nscript: ./x\ndeadline: 1h\n", Class{Name: ClassScript, Script: "./x", Deadline: time.Hour}, ""},
		{"first class wins", "card\nCLASS: script\nCLASS: model\nSCRIPT: ./x\n", Class{Name: ClassScript, Script: "./x"}, ""},
		{"first script wins", "card\nCLASS: script\nSCRIPT: ./a\nSCRIPT: ./b\n", Class{Name: ClassScript, Script: "./a"}, ""},
		{"after STEP is not read", "card\nSTEP 1. do it\nCLASS: script\nSCRIPT: ./x\n", Class{Name: ClassModel}, ""},
		{"deadline after STEP is not read", "card\nCLASS: script\nSCRIPT: ./x\nSTEP 1\nDEADLINE: 1h\n", Class{Name: ClassScript, Script: "./x"}, ""},
		{"fenced lines are not read", "card\n```\nCLASS: script\nSCRIPT: ./x\n```\n", Class{Name: ClassModel}, ""},
		{"indented fence still toggles", "card\n  ```sh\nSCRIPT: ./x\n  ```\nCLASS: model\n", Class{Name: ClassModel}, ""},
		{"script with no program", "card\nCLASS: script\n", Class{Name: ClassModel}, "CLASS: script names no program; write SCRIPT: <command> (run from the repository root at the start commit)"},
		{"unknown class", "card\nCLASS: other\n", Class{Name: ClassModel}, "CLASS: other is not model or script"},
		{"script on a model card", "card\nSCRIPT: ./x\n", Class{Name: ClassModel}, "SCRIPT: ./x names a program on a card whose class is model; write CLASS: script or drop the SCRIPT line"},
		{"script on an explicit model card", "card\nCLASS: model\nSCRIPT: ./x\n", Class{Name: ClassModel}, "SCRIPT: ./x names a program on a card whose class is model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, why := ReadClass(tc.brief)
			assert.Equal(t, tc.want, got)
			if tc.why == "" {
				assert.Empty(t, why)
			} else {
				assert.Contains(t, why, tc.why)
			}
			assert.Equal(t, got.Name == ClassScript && got.Script != "", got.IsScript())
		})
	}
}

// ReadModel reads the tier off line 1 and a pin off the header block under it: a pin
// needs tokens and deadline on the lines under it; tokens and deadline without a pin are
// the brief's own words and never read; the block ends at the first line that is no
// `key: value`; an unknown tier is read and refused.
func TestReadModelReadsTheTierFromLineOneAndThePinFromTheBlockUnderIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, brief string
		want        Model
		why         []string
	}{
		{"tier only", "card x tier: pro\nKIND: fix\n", Model{Tier: "pro"}, nil},
		{"no tier", "card x\nKIND: fix\n", Model{}, nil},
		{"tier dash is read and refused", "card x tier: -\n", Model{Tier: "-"}, []string{"line 1 names tier -; want frontier, heavy, pro or flash"}},
		{"unknown tier is refused", "card x tier: fast\n", Model{Tier: "fast"}, []string{"line 1 names tier fast; want frontier, heavy, pro or flash"}},
		{"tier on line 2 is not the tier", "card\ntier: pro\n", Model{}, nil},
		{"pin with count and seconds", "card tier: pro\nmodel: openai/gpt-5\ntokens: 400000\ndeadline: 3600\n", Model{Tier: "pro", Pin: "openai/gpt-5", Tokens: "400000", Deadline: 3600}, nil},
		{"pin unmetered with a duration", "card tier: pro\nMODEL: openai/gpt-5\nTOKENS: unmetered\nDEADLINE: 1h30m\n", Model{Tier: "pro", Pin: "openai/gpt-5", Tokens: "unmetered", Deadline: 5400}, nil},
		{"pin with nothing under it", "card tier: pro\nmodel: openai/gpt-5\n", Model{Tier: "pro", Pin: "openai/gpt-5"}, []string{"model: openai/gpt-5 pins the card without tokens: <n>|unmetered and deadline: <seconds>; a pin carries its budget and deadline on the lines under it"}},
		{"pin without deadline", "card tier: pro\nmodel: openai/gpt-5\ntokens: 1\n", Model{Tier: "pro", Pin: "openai/gpt-5", Tokens: "1"}, []string{"pins the card without deadline: <seconds>;"}},
		{"pin without tokens", "card tier: pro\nmodel: openai/gpt-5\ndeadline: 1\n", Model{Tier: "pro", Pin: "openai/gpt-5", Deadline: 1}, []string{"pins the card without tokens: <n>|unmetered;"}},
		{"pin that is no model id", "card tier: pro\nmodel: gpt-5\ntokens: 1\ndeadline: 1\n", Model{Tier: "pro", Pin: "gpt-5", Tokens: "1", Deadline: 1}, []string{"model: gpt-5 is not <provider>/<model>"}},
		{"tokens zero", "card tier: pro\nmodel: a/b\ntokens: 0\ndeadline: 1\n", Model{Tier: "pro", Pin: "a/b", Tokens: "0", Deadline: 1}, []string{"tokens: 0 is not a count of at least 1 or the word unmetered"}},
		{"tokens a word", "card tier: pro\nmodel: a/b\ntokens: many\ndeadline: 1\n", Model{Tier: "pro", Pin: "a/b", Tokens: "many", Deadline: 1}, []string{"tokens: many is not a count of at least 1 or the word unmetered"}},
		{"deadline zero", "card tier: pro\nmodel: a/b\ntokens: 1\ndeadline: 0\n", Model{Tier: "pro", Pin: "a/b", Tokens: "1"}, []string{"deadline: 0 is not seconds or a duration of at least 1s"}},
		{"deadline under a second", "card tier: pro\nmodel: a/b\ntokens: 1\ndeadline: 500ms\n", Model{Tier: "pro", Pin: "a/b", Tokens: "1"}, []string{"deadline: 500ms is not seconds or a duration of at least 1s"}},
		{"deadline a word", "card tier: pro\nmodel: a/b\ntokens: 1\ndeadline: soon\n", Model{Tier: "pro", Pin: "a/b", Tokens: "1"}, []string{"deadline: soon is not seconds or a duration of at least 1s"}},
		{"every problem is named", "card tier: fast\nmodel: b\n", Model{Tier: "fast", Pin: "b"}, []string{"line 1 names tier fast", "model: b is not <provider>/<model>", "pins the card without tokens: <n>|unmetered and deadline: <seconds>"}},
		{"tokens and deadline without a pin are never read", "card tier: pro\ntokens: 0\ndeadline: 0\n", Model{Tier: "pro"}, nil},
		{"the block ends at a blank line", "card tier: pro\n\nmodel: a/b\ntokens: 1\ndeadline: 1\n", Model{Tier: "pro"}, nil},
		{"the block ends at prose", "card tier: pro\nKIND: fix\nSome prose here.\nmodel: a/b\n", Model{Tier: "pro"}, nil},
		{"empty brief", "", Model{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, why := ReadModel(tc.brief)
			assert.Equal(t, tc.want, got)
			if len(tc.why) == 0 {
				assert.Empty(t, why)
				return
			}
			for _, w := range tc.why {
				assert.Contains(t, why, w)
			}
		})
	}
}

// ReadWho: no WHO line or `WHO: -` is the zero Who; `friend`, `friend <name>` and
// `only friend <name>` are the three forms, the words in any case, the name a friend
// row's name; anything else is refused with the three forms; the line is read from the
// header block only.
func TestReadWhoReadsTheThreeFormsAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, brief string
		want        Who
		refused     bool
	}{
		{"no line", "card\nKIND: fix\n", Who{}, false},
		{"dash", "card\nWHO: -\n", Who{}, false},
		{"any friend", "card\nWHO: friend\n", Who{Friend: true}, false},
		{"named friend", "card\nWHO: friend worker-1\n", Who{Friend: true, Name: "worker-1"}, false},
		{"only friend", "card\nWHO: only friend worker_1\n", Who{Friend: true, Only: true, Name: "worker_1"}, false},
		{"words in any case", "card\nwho: Only FRIEND w1\n", Who{Friend: true, Only: true, Name: "w1"}, false},
		{"after a blank line is not the header", "card\n\nWHO: friend\n", Who{}, false},
		{"after prose is not the header", "card\nKIND: fix\nprose\nWHO: friend\n", Who{}, false},
		{"empty value", "card\nWHO:\n", Who{}, true},
		{"only without friend", "card\nWHO: only w1\n", Who{}, true},
		{"only friend unnamed", "card\nWHO: only friend\n", Who{}, true},
		{"too many words", "card\nWHO: friend a b\n", Who{}, true},
		{"name with a bad first character", "card\nWHO: friend -w1\n", Who{}, true},
		{"name with a dot", "card\nWHO: friend w.1\n", Who{}, true},
		{"fleet is no form", "card\nWHO: fleet\n", Who{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, why := ReadWho(tc.brief)
			assert.Equal(t, tc.want, got)
			if tc.refused {
				assert.Contains(t, why, "is not `friend` or `friend <name>` or `only friend <name>`")
			} else {
				assert.Empty(t, why)
			}
		})
	}
}

// The member's failed-finish prefixes and the header keys are fixed spellings other
// packages match on; a change here is a protocol change.
func TestFixedSpellings(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "PATHS", KeyPaths)
	assert.Equal(t, "provider failure", EndProvider)
	assert.Equal(t, "no result", EndNoResult)
	assert.Equal(t, "staging refused", EndStaging)
	assert.Equal(t, "launch refused", EndLaunch)
	assert.Equal(t, "pre-existing", EndPreExisting)
	assert.Equal(t, "nothing to do", EndNothing)
	assert.Equal(t, "no commit", EndNoCommit)
	assert.Equal(t, "remainder=", RemainderKey)
}
