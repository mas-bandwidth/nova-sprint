package main

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/docs"
)

// playbook_test.go holds docs/COORDINATOR-PLAYBOOK.md, the page
// `nova-sprint help playbook` prints, to the help it cites, so it cannot go
// stale: the embedded bytes are the file's, every usage line it quotes is a
// line of `nova-sprint help all` byte for byte, and every command it names runs a
// verb the verb table carries with flags that verb takes. The flags are read
// from each verb's own help (its FlagSet, as `nova-sprint help <verb>` prints
// it), never copied here. The verbs of the nova-tools tools the page names are
// held to foreignVerbs and to playbookForeignVerbs below; nova-tools'
// docs/TOOLS-PLAYBOOK.md holds those lines to the binaries' own help.

// playbookPath is the page, relative to this package.
const playbookPath = "../../docs/COORDINATOR-PLAYBOOK.md"

// playbookUsageRe reads a quoted usage line: the word usage, then the line in
// a code span, across a line break where the page wraps.
var playbookUsageRe = regexp.MustCompile("usage:?\\s+`(nova-sprint [^`]+)`")

// playbookForeignVerbs is the verbs of nova-tools' tools the playbook names
// beyond foreignVerbs (the runbook's list), read from each tool's help in
// nova-tools v1.3/comms on 2026-10-16. nova-work is built here
// (cmd/nova-work) and is listed for the same check.
var playbookForeignVerbs = map[string]map[string]bool{
	"nova-friend": {"lanes": true, "resume": true},
	"nova-doctor": {"run": true},
	"nova-update": {"release": true},
	"nova-work":   {"roadmap": true},
	"nova-redis":  {"install": true},
}

// helpFlagRe reads one flag line of a verb's help: `  --name <type>  ...`.
var helpFlagRe = regexp.MustCompile(`(?m)^  --([a-zA-Z][a-zA-Z0-9_-]*)`)

// playbookVerbs is the verb table with each verb's flags as its own help
// prints them (its FlagSet), plus the synopsis's, so a flag the help lists
// but the synopsis leaves out (--needs, --keep-head, --carry, --cause) counts.
func playbookVerbs(t *testing.T) map[string]map[string]bool {
	t.Helper()
	table := sprintVerbs()
	for _, v := range verbs {
		var out, errb bytes.Buffer
		code := helpCommand(strings.Fields(v.name), &out, &errb)
		require.Equal(t, 0, code, "nova-sprint help %s: %s", v.name, errb.String())
		for _, m := range helpFlagRe.FindAllStringSubmatch(out.String(), -1) {
			table[v.name][m[1]] = true
		}
	}
	// the selector's words, which the selector verbs parse apart from their
	// FlagSet (cmd/nova-sprint/select.go; docs/SPEC-SPRINT.md section 2,
	// "One selector, one step")
	for _, name := range []string{"brief", "recut", "rework", "return", "release", "rank", "drop", "priority"} {
		require.Contains(t, table, name, "the selector verb %s is in the verb table", name)
		for _, w := range append(selWords(name), "stream", "dry-run") {
			table[name][w] = true
		}
	}
	return table
}

// checkPlaybook is every problem of the page's text: a usage line that is not
// a line of the banner, and a command naming a verb or a flag the tool does
// not carry.
func checkPlaybook(verbs map[string]map[string]bool, bannerText, text string) []string {
	var problems []string
	lines := map[string]bool{}
	for _, l := range strings.Split(bannerText, "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	usages := 0
	for _, m := range playbookUsageRe.FindAllStringSubmatch(text, -1) {
		usages++
		if !lines[m[1]] {
			problems = append(problems, "usage line not in nova-sprint help all: "+m[1])
		}
	}
	if usages == 0 {
		problems = append(problems, "the page quotes no nova-sprint usage line")
	}
	for _, s := range codeSpanRe.FindAllStringSubmatch(text, -1) {
		words := strings.Fields(s[1])
		if len(words) < 2 || !strings.HasPrefix(words[0], "nova-") {
			continue // a tool's name alone, a flag, a placeholder
		}
		if words[1] == "help" || strings.HasPrefix(words[1], "<") || strings.HasPrefix(words[1], "[") {
			continue // every tool's help, and a usage line's placeholder or optional verb
		}
		for i, w := range words {
			if strings.HasPrefix(w, "--") {
				words[i] = strings.TrimRight(w, ")]}.,;:") // a usage line's closing brackets
			}
		}
		if extra, ok := playbookForeignVerbs[words[0]]; ok && extra[words[1]] {
			continue
		}
		if _, ok := foreignVerbs[words[0]]; !ok && words[0] != "nova-sprint" {
			if _, ok := playbookForeignVerbs[words[0]]; ok {
				problems = append(problems, words[0]+" has no verb "+words[1]+" in the playbook's list")
				continue
			}
		}
		_, ps := checkCommand(verbs, words)
		for _, p := range ps {
			problems = append(problems, p+" (`"+s[1]+"`)")
		}
	}
	return problems
}

// `nova-sprint help playbook` prints the page, the page compiled in is the
// file on disk, and the banner says where the playbook is.
func TestHelpPlaybookPrintsTheCoordinatorPlaybook(t *testing.T) {
	t.Parallel()
	onDisk, err := os.ReadFile(playbookPath)
	require.NoError(t, err)
	assert.Equal(t, string(onDisk), docs.CoordinatorPlaybook, "the embedded playbook equals docs/COORDINATOR-PLAYBOOK.md")

	var out, errb bytes.Buffer
	code := helpCommand([]string{"playbook"}, &out, &errb)
	require.Equal(t, 0, code, errb.String())
	assert.Equal(t, docs.CoordinatorPlaybook, out.String(), "help playbook prints the page whole")
	assert.Contains(t, out.String(), "## Before you do anything by hand", "the playbook opens with the rule")

	assert.Contains(t, banner(), "nova-sprint help playbook", "nova-sprint help all names the playbook")
	assert.Contains(t, helpIndex(), "nova-sprint help playbook", "nova-sprint help names the playbook")
	for _, v := range verbs {
		assert.NotEqual(t, "playbook", v.name, "a verb named playbook would shadow the topic")
	}
}

// Every usage line the playbook quotes is nova-sprint help all's, and every
// command it names is a verb with flags the verb takes. The witnesses run the
// same check on lines that must be refused, so a check that passes everything
// fails here.
func TestThePlaybookNamesOnlyRealVerbsAndUsageLines(t *testing.T) {
	t.Parallel()
	table := playbookVerbs(t)
	b := banner()

	t.Run("the page", func(t *testing.T) {
		for _, p := range checkPlaybook(table, b, docs.CoordinatorPlaybook) {
			t.Errorf("%s %s", playbookPath, p)
		}
	})

	t.Run("the witnesses", func(t *testing.T) {
		good := "usage: `nova-sprint inbox [--open <group>] [--read] [--wait [--timeout <duration>] [--push <dir>]] [--deadline <duration>] [--stale <duration>]`\n" +
			"run `nova-sprint view coordinator --needs`, `nova-sprint brief <card> --keep-head --brief-file <f>`, `nova-friend lanes --as <me> --clear-faults`, `nova-sprint help playbook`\n"
		assert.Empty(t, checkPlaybook(table, b, good), "real usage lines and verbs are refused")

		for text, want := range map[string]string{
			"usage: `nova-sprint inbox [--fly]`":                    "usage line not in nova-sprint help all",
			"usage: `nova-sprint where`\n`nova-sprint fly`":         `nova-sprint has no verb "fly"`,
			"usage: `nova-sprint where`\n`nova-sprint where --fly`": "nova-sprint where has no flag --fly",
			"usage: `nova-sprint where`\n`nova-friend fly`":         `nova-friend has no verb "fly"`,
			"usage: `nova-sprint where`\n`nova-doctor fly`":         "nova-doctor has no verb fly",
			"no usage line at all":                                  "quotes no nova-sprint usage line",
		} {
			ps := checkPlaybook(table, b, text)
			assert.NotEmpty(t, ps, "the text is not refused: %s", text)
			assert.Contains(t, strings.Join(ps, "\n"), want, "the text is refused for another reason: %s", text)
		}
	})
}
