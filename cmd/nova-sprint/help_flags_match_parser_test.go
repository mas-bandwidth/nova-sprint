package main

import (
	"bytes"
	"flag"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpUsageFlagRe reads a flag the help's usage line names: `--name`.
var helpUsageFlagRe = regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

// helpListFlagRe reads one flag the help's flag list names: `  --name ...`.
var helpListFlagRe = regexp.MustCompile(`^  --([a-zA-Z][a-zA-Z0-9_-]*)(?:\s|$)`)

// helpFlagNames is the flags a verb's help names in its usage line and its
// flag list.
func helpFlagNames(help string) map[string]bool {
	flags := map[string]bool{}
	usage, _, _ := strings.Cut(help, "\n")
	for _, m := range helpUsageFlagRe.FindAllStringSubmatch(usage, -1) {
		flags[m[1]] = true
	}
	_, after, ok := strings.Cut(help, "\nflags:\n")
	if !ok {
		return flags
	}
	for _, line := range strings.Split(after, "\n") {
		if !strings.HasPrefix(line, "  --") {
			break
		}
		if m := helpListFlagRe.FindStringSubmatch(line); m != nil {
			flags[m[1]] = true
		}
	}
	return flags
}

// parserFlagNames is the flags a verb's parser accepts.
func parserFlagNames(fs flagSet) map[string]bool {
	flags := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { flags[f.Name] = true })
	return flags
}

// TestHelpFlagsMatchParser holds continue's help to its parser: every --flag
// its usage line and its flag list name is a flag the parser accepts, and
// every flag the parser accepts is named there. The global flags every store
// verb takes, and the help flags, are on neither side. The remaining flags
// are continue's own, so its usage line cannot promise one the parser refuses.
func TestHelpFlagsMatchParser(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"continue"} {
		var out, errb bytes.Buffer
		code := helpCommand(strings.Fields(name), &out, &errb)
		require.Equal(t, 0, code, "nova-sprint help %s: %s", name, errb.String())

		helpFlags := helpFlagNames(out.String())
		fs := verbFlags(name)
		require.NotNil(t, fs, "the parser of %s", name)
		parserFlags := parserFlagNames(fs)

		for flag := range commonFlags {
			delete(helpFlags, flag)
			delete(parserFlags, flag)
		}
		for _, flag := range []string{"h", "help"} {
			delete(helpFlags, flag)
			delete(parserFlags, flag)
		}

		var promised, accepted []string
		for flag := range helpFlags {
			promised = append(promised, flag)
		}
		for flag := range parserFlags {
			accepted = append(accepted, flag)
		}
		slices.Sort(promised)
		slices.Sort(accepted)
		assert.Equal(t, accepted, promised, "nova-sprint %s: help and parser disagree: %s", name, disagreeReason(promised, accepted))
	}
}

func disagreeReason(help, parser []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "help lists %v; the parser accepts %v", help, parser)
	if extra := missing(help, parser); len(extra) > 0 {
		fmt.Fprintf(&b, "; help names what the parser refuses: %v", extra)
	}
	if extra := missing(parser, help); len(extra) > 0 {
		fmt.Fprintf(&b, "; help omits what the parser accepts: %v", extra)
	}
	return b.String()
}

func missing(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}
